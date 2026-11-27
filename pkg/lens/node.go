package lens

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Kind classifies a Node in the neutral tree.
type Kind uint8

const (
	// KindScalar is a leaf value: number, string, bool, enum name, hex blob.
	KindScalar Kind = iota
	// KindStruct is a named record with ordered fields.
	KindStruct
	// KindList is an ordered sequence of values.
	KindList
	// KindBytes is a leaf binary value whose Value is []byte. Using a distinct
	// kind allows the differ to normalize representations (hex, base64, raw
	// byte slices) before comparing, so that encoding differences are not
	// reported as semantic changes.
	KindBytes
)

func (k Kind) String() string {
	switch k {
	case KindScalar:
		return "scalar"
	case KindStruct:
		return "struct"
	case KindList:
		return "list"
	case KindBytes:
		return "bytes"
	default:
		return "unknown"
	}
}

// Node is the neutral, format-independent representation of a decoded XDR
// value. Every formatter and the differ consume Node trees rather than the
// concrete xdr.* Go types, which keeps rendering and diffing decoupled from
// the 400-odd generated XDR structs.
//
// A Node is a tree: Children is populated for KindStruct and KindList, and
// Value is populated for KindScalar.
type Node struct {
	// Name is the field name within the parent. Empty at the root and for
	// list elements (which are addressed by index).
	Name string
	// Kind determines whether Value or Children carries the payload.
	Kind Kind
	// TypeName is the originating XDR type, e.g. "TransactionEnvelope".
	// It is best-effort and may be empty for anonymous values.
	TypeName string
	// Value holds the payload for KindScalar. It is one of: string, bool,
	// int64, uint64, or float64, so that formatters need no reflection.
	Value any
	// Note carries a human-friendly rendering derived by an enricher, such
	// as a strkey address or a formatted amount. Formatters show it
	// alongside the raw Value.
	Note string
	// Children holds the members of a struct or the elements of a list.
	Children []*Node
}

// IsZero reports whether the node carries no meaningful payload.
func (n *Node) IsZero() bool {
	return n == nil || (n.Kind == KindScalar && n.Value == nil && n.Note == "")
}

// Child returns the first direct child with the given name, or nil.
func (n *Node) Child(name string) *Node {
	if n == nil {
		return nil
	}
	for _, c := range n.Children {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// Find walks a dotted path such as "Result.Results[0].Code" and returns the
// node at that location, or nil when the path does not resolve.
func (n *Node) Find(path string) *Node {
	cur := n
	for _, seg := range splitPath(path) {
		if cur == nil {
			return nil
		}
		if seg.index >= 0 {
			if seg.name != "" {
				cur = cur.Child(seg.name)
				if cur == nil {
					return nil
				}
			}
			if seg.index >= len(cur.Children) {
				return nil
			}
			cur = cur.Children[seg.index]
			continue
		}
		cur = cur.Child(seg.name)
	}
	return cur
}

// String renders the scalar payload for display. Struct and list nodes render
// as a short type summary rather than their full contents.
func (n *Node) String() string {
	if n == nil {
		return "<nil>"
	}
	switch n.Kind {
	case KindScalar, KindBytes:
		if n.Value == nil {
			return n.Note
		}
		s := fmt.Sprintf("%v", n.Value)
		if n.Note != "" {
			return s + " (" + n.Note + ")"
		}
		return s
	case KindList:
		return fmt.Sprintf("[%d item(s)]", len(n.Children))
	default:
		if n.TypeName != "" {
			return n.TypeName
		}
		return "{...}"
	}
}

// maxDepth bounds the tree walk. XDR itself is decoded with the SDK's own
// depth limit, so this is a second belt-and-braces guard against a
// pathological value producing an unbounded walk.
const maxDepth = 200

// Buffers larger than these limits are left to the garbage collector, so one
// unusually wide struct or large WASM blob does not stay pinned in a pool.
const (
	maxPooledChildren = 256
	maxPooledBytes    = 64 << 10
)

// childScratchPool and byteScratchPool hold the temporary buffers used while a
// tree is built. Nothing reachable from a finished Node refers to them. They
// store pointers so that returning a buffer does not itself allocate.
var (
	childScratchPool sync.Pool // of *[]*Node
	byteScratchPool  sync.Pool // of *[]byte
)

// getChildScratch returns an empty buffer for collecting a node's children.
func getChildScratch() *[]*Node {
	if scratch, ok := childScratchPool.Get().(*[]*Node); ok {
		return scratch
	}
	scratch := make([]*Node, 0, 32)
	return &scratch
}

// releaseChildScratch copies children into an exactly sized slice and returns
// the buffer to the pool. The buffer is cleared first so the pool never keeps a
// finished tree alive. With no children the result is nil, as for a node that
// was never appended to.
func releaseChildScratch(scratch *[]*Node, children []*Node) []*Node {
	var out []*Node
	if len(children) > 0 {
		out = make([]*Node, len(children))
		copy(out, children)
	}
	if cap(children) > maxPooledChildren {
		return out
	}
	clear(children)
	*scratch = children[:0]
	childScratchPool.Put(scratch)
	return out
}

// getByteScratch returns an empty buffer for rendering a byte sequence.
func getByteScratch() *[]byte {
	if scratch, ok := byteScratchPool.Get().(*[]byte); ok {
		return scratch
	}
	scratch := make([]byte, 0, 128)
	return &scratch
}

// releaseByteScratch returns buf, which may have grown from scratch, to the pool.
func releaseByteScratch(scratch *[]byte, buf []byte) {
	if cap(buf) > maxPooledBytes {
		return
	}
	*scratch = buf[:0]
	byteScratchPool.Put(scratch)
}

// nodeHooks maps a concrete XDR type to a function that appends derived
// children to its struct node once the declared fields have been walked. It is
// the extension point for content that is not itself an XDR field but is
// embedded in one: a Soroban contract's WASM code section, for example. A hook
// receives the concrete decoded value, not a reflect.Value, so feature files
// stay free of reflection.
//
// Hooks are registered from feature init functions before any decoding
// happens and the map is read-only afterwards, so it needs no locking.
var nodeHooks = map[reflect.Type]func(*Node, any){}

// runNodeHook invokes the hook registered for rv's type, if any. Hooks walk
// untrusted input, so one that panics is treated as absent rather than being
// allowed to take the process down.
func runNodeHook(n *Node, rv reflect.Value) {
	hook, found := nodeHooks[rv.Type()]
	if !found {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			return
		}
	}()
	hook(n, rv.Interface())
}

// nodeFor builds a Node tree for an arbitrary decoded value. It is the
// entry point node hooks use to graft values they find inside a field, such
// as the spec entries embedded in contract bytecode, onto the main tree.
func nodeFor(name string, v any) *Node {
	if v == nil {
		return nil
	}
	return buildNode(name, reflect.ValueOf(v), 0)
}

// buildNode converts an arbitrary decoded XDR value into a neutral Node tree.
//
// This function, and the enricher lookup it calls, are the only places in the
// project that use reflection. Everything downstream — formatters, the
// differ, explain — operates on Node. Keeping the reflection confined here is
// deliberate: adding a new output format never requires touching reflect.
func buildNode(name string, rv reflect.Value, depth int) *Node {
	if depth > maxDepth {
		return &Node{Name: name, Kind: KindScalar, Value: "<max depth exceeded>"}
	}

	// Unwrap pointers and interfaces. A nil pointer means an absent union arm
	// or optional field, which we prune entirely rather than render as null.
	for rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	if !rv.IsValid() {
		return nil
	}

	typeName := rv.Type().Name()

	// A registered enricher wins over the generic rendering: this is how
	// AccountId becomes G..., Asset becomes CODE:ISSUER, and so on.
	if note, raw, ok := enrich(rv); ok {
		return &Node{Name: name, Kind: KindScalar, TypeName: typeName, Value: raw, Note: note}
	}

	// Named enum types carry a String() method generated by the XDR
	// toolchain. Using it turns "Code: -13" into a readable constant name,
	// which is most of the value of the tool.
	if s, ok := stringerName(rv); ok {
		return &Node{Name: name, Kind: KindScalar, TypeName: typeName, Value: s}
	}

	switch rv.Kind() {
	case reflect.Struct:
		n := &Node{Name: name, Kind: KindStruct, TypeName: typeName}
		rt := rv.Type()
		scratch := getChildScratch()
		children := *scratch
		for i := range rv.NumField() {
			f := rt.Field(i)
			if f.PkgPath != "" { // unexported
				continue
			}
			child := buildNode(f.Name, rv.Field(i), depth+1)
			if child == nil {
				continue
			}
			// Field-name formatting (amounts, protocol versions, ledger
			// counts) depends on the field, not just the type, so it is
			// applied here rather than in the type registry.
			if note, ok := enrichField(f.Name, rv.Field(i)); ok {
				child.Note = note
			}
			// Field notes go further and key on the containing struct too,
			// which is how TrustLineEntry.Flags gets its flag names without
			// every other uint32 field named "Flags" being misread.
			if note, ok := noteForField(rt, f.Name, rv.Field(i)); ok {
				child.Note = note
			}
			children = append(children, child)
		}
		n.Children = releaseChildScratch(scratch, children)
		runNodeHook(n, rv)
		return n

	case reflect.Slice, reflect.Array:
		// Byte sequences are hashes, signatures and keys. Rendering them as
		// a list of 32 integers is useless, so they become KindBytes nodes
		// whose Value carries the hex-encoded string. KindBytes distinguishes
		// binary fields from arbitrary scalars so the differ can normalise
		// cross-encoding comparisons (hex vs base64 vs raw bytes) in one place.
		if rv.Type().Elem().Kind() == reflect.Uint8 {
			if rv.Kind() == reflect.Slice && rv.IsNil() {
				return nil
			}
			value, note := renderByteSequence(rv)
			return &Node{
				Name:     name,
				Kind:     KindBytes,
				TypeName: typeName,
				Value:    value,
				Note:     note,
			}
		}
		if rv.Kind() == reflect.Slice && rv.IsNil() {
			return nil
		}
		n := &Node{Name: name, Kind: KindList, TypeName: typeName}
		if rv.Len() > 0 {
			n.Children = make([]*Node, 0, rv.Len())
		}
		for i := range rv.Len() {
			child := buildNode("", rv.Index(i), depth+1)
			if child == nil {
				child = &Node{Kind: KindScalar, Value: nil}
			}
			n.Children = append(n.Children, child)
		}
		return n

	case reflect.Map:
		n := &Node{Name: name, Kind: KindStruct, TypeName: typeName}
		keys := rv.MapKeys()
		sort.Slice(keys, func(i, j int) bool {
			ki := fmt.Sprintf("%v", keys[i])
			kj := fmt.Sprintf("%v", keys[j])
			if ki != kj {
				return ki < kj
			}
			return fmt.Sprintf("%T|%#v", keys[i].Interface(), keys[i].Interface()) < fmt.Sprintf("%T|%#v", keys[j].Interface(), keys[j].Interface())
		})
		if len(keys) > 0 {
			n.Children = make([]*Node, 0, len(keys))
		}
		for _, k := range keys {
			child := buildNode(fmt.Sprintf("%v", k), rv.MapIndex(k), depth+1)
			if child == nil {
				continue
			}
			n.Children = append(n.Children, child)
		}
		if len(n.Children) == 0 {
			n.Children = nil
		}
		return n

	case reflect.Bool:
		return &Node{Name: name, Kind: KindScalar, TypeName: typeName, Value: rv.Bool()}

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return &Node{Name: name, Kind: KindScalar, TypeName: typeName, Value: rv.Int()}

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return &Node{Name: name, Kind: KindScalar, TypeName: typeName, Value: rv.Uint()}

	case reflect.Float32, reflect.Float64:
		return &Node{Name: name, Kind: KindScalar, TypeName: typeName, Value: rv.Float()}

	case reflect.String:
		return &Node{Name: name, Kind: KindScalar, TypeName: typeName, Value: rv.String()}

	default:
		return &Node{Name: name, Kind: KindScalar, TypeName: typeName, Value: fmt.Sprintf("%v", rv)}
	}
}

// renderByteSequence returns the hex form of a byte slice or array and its
// printable note. Arrays are copied element by element because a value reached
// through a struct obtained by value is not addressable. The copy and the hex
// text share one pooled buffer, so only the returned strings are allocated.
func renderByteSequence(rv reflect.Value) (value, note string) {
	scratch := getByteScratch()
	buf := *scratch

	var raw []byte
	if rv.Kind() == reflect.Slice {
		raw = rv.Bytes()
	} else {
		for i := range rv.Len() {
			buf = append(buf, byte(rv.Index(i).Uint()))
		}
		raw = buf
	}

	start := len(buf)
	buf = hex.AppendEncode(buf, raw)
	value = string(buf[start:])
	note = printableASCII(raw)
	releaseByteScratch(scratch, buf)
	return value, note
}

// printableASCII returns the string form of b when it is entirely printable
// ASCII, which makes asset codes and text memos readable. Otherwise it
// returns "" and the caller shows hex alone.
func printableASCII(b []byte) string {
	if len(b) == 0 || len(b) > 64 {
		return ""
	}
	trimmed := bytes.TrimRight(b, "\x00")
	if len(trimmed) == 0 {
		return ""
	}
	for _, c := range trimmed {
		if c < 0x20 || c > 0x7e {
			return ""
		}
	}
	return strconv.Quote(string(trimmed))
}

type pathSeg struct {
	name  string
	index int
}

// splitPath parses "a.b[2].c" into segments. An index of -1 means the segment
// addresses a named field rather than a list element.
func splitPath(path string) []pathSeg {
	var segs []pathSeg
	for _, part := range strings.Split(path, ".") {
		if part == "" {
			continue
		}
		name := part
		for {
			open := strings.IndexByte(name, '[')
			if open < 0 {
				if name != "" {
					segs = append(segs, pathSeg{name: name, index: -1})
				}
				break
			}
			closeIdx := strings.IndexByte(name[open:], ']')
			if closeIdx < 0 {
				segs = append(segs, pathSeg{name: name, index: -1})
				break
			}
			closeIdx += open
			idx := 0
			if _, err := fmt.Sscanf(name[open+1:closeIdx], "%d", &idx); err != nil {
				idx = -1
			}
			segs = append(segs, pathSeg{name: name[:open], index: idx})
			name = name[closeIdx+1:]
			if name == "" {
				break
			}
		}
	}
	return segs
}

// stringerName returns the String() rendering for named integer types, which
// is how the generated XDR enums expose their constant names. It deliberately
// does not fire for plain numeric types like Uint32 or Int64, whose String()
// output would be no more informative than the number itself.
func stringerName(rv reflect.Value) (string, bool) {
	t := rv.Type()
	if t.Name() == "" || t.PkgPath() == "" {
		return "", false
	}
	switch t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
	default:
		return "", false
	}
	if !isEnumType(t) {
		return "", false
	}
	s, ok := rv.Interface().(fmt.Stringer)
	if !ok {
		return "", false
	}
	name := safeString(s)
	if name == "" {
		return "", false
	}
	return name, true
}

// FilterByAccount returns a new Node tree containing only nodes that reference
// the given account strkey address (G...). It recursively walks the tree and
// keeps nodes where an AccountID enricher matches the target, plus their
// ancestors up to the root. If the target is empty, the original tree is
// returned unchanged.
func FilterByAccount(root *Node, targetAccount string) *Node {
	if root == nil || targetAccount == "" {
		return root
	}
	return filterByAccountRecursive(root, targetAccount, nil)
}

// filterByAccountRecursive walks the tree and marks nodes to keep.
func filterByAccountRecursive(n *Node, targetAccount string, parent *Node) *Node {
	if n == nil {
		return nil
	}

	// Check if this node or any descendant matches the target account
	matches := nodeOrDescendantMatchesAccount(n, targetAccount)

	// Also check if this node itself is an AccountID or MuxedAccount that matches
	if n.Kind == KindScalar && (n.Note == targetAccount || nodeValueEquals(n.Value, targetAccount)) && (n.TypeName == "AccountId" || n.TypeName == "MuxedAccount") {
		matches = true
	}

	if !matches {
		return nil
	}

	// Create a copy of this node with filtered children
	newNode := &Node{
		Name:     n.Name,
		Kind:     n.Kind,
		TypeName: n.TypeName,
		Value:    n.Value,
		Note:     n.Note,
		Children: nil,
	}

	// Recursively filter children
	for _, child := range n.Children {
		filteredChild := filterByAccountRecursive(child, targetAccount, newNode)
		if filteredChild != nil {
			newNode.Children = append(newNode.Children, filteredChild)
		}
	}

	return newNode
}

// nodeOrDescendantMatchesAccount checks if a node or any of its descendants
// contains an AccountID or MuxedAccount that matches the target account.
func nodeOrDescendantMatchesAccount(n *Node, targetAccount string) bool {
	if n == nil {
		return false
	}

	// Check this node
	if n.Kind == KindScalar && (n.Note == targetAccount || nodeValueEquals(n.Value, targetAccount)) && (n.TypeName == "AccountId" || n.TypeName == "MuxedAccount") {
		return true
	}

	// Check children recursively
	for _, child := range n.Children {
		if nodeOrDescendantMatchesAccount(child, targetAccount) {
			return true
		}
	}

	return false
}

// nodeValueEquals checks if a node's Value equals the target account string.
func nodeValueEquals(value any, target string) bool {
	if value == nil {
		return false
	}
	if s, ok := value.(string); ok {
		return s == target
	}
	return false
}

// FilterByOpType returns a new Node tree containing only operation nodes that
// match the given operation type name (e.g., "payment", "invoke_host_function").
// The match is case-insensitive and supports both snake_case and camelCase
// variants. If opType is empty, the original tree is returned unchanged.
func FilterByOpType(root *Node, opType string) *Node {
	if root == nil || opType == "" {
		return root
	}
	normalized := normalizeOpType(opType)
	return filterByOpTypeRecursive(root, normalized)
}

// normalizeOpType converts operation type to a canonical form for comparison.
func normalizeOpType(s string) string {
	// Handle camelCase like "InvokeHostFunction" -> "invoke_host_function"
	// Do this BEFORE ToLower so we can detect uppercase boundaries
	var result strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result.WriteByte('_')
		}
		result.WriteRune(r)
	}
	s = result.String()
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "-", "_")
	s = strings.ReplaceAll(s, " ", "_")
	return s
}

func filterByOpTypeRecursive(n *Node, targetOpType string) *Node {
	if n == nil {
		return nil
	}

	// Check if this node is an operation list (TypeName may be empty)
	if n.Kind == KindList && (n.TypeName == "Operation" || n.Name == "Operations") {
		// Filter list elements
		var filteredChildren []*Node
		for _, child := range n.Children {
			if opMatchesType(child, targetOpType) {
				filteredChildren = append(filteredChildren, child)
			}
		}
		if len(filteredChildren) == 0 {
			return nil
		}
		newNode := &Node{
			Name:     n.Name,
			Kind:     n.Kind,
			TypeName: n.TypeName,
			Value:    n.Value,
			Note:     n.Note,
			Children: filteredChildren,
		}
		return newNode
	}

	// Check if this node is an operation struct
	if n.Kind == KindStruct && n.TypeName == "Operation" {
		if opMatchesType(n, targetOpType) {
			return n
		}
		return nil
	}

	// Recursively filter children
	var filteredChildren []*Node
	for _, child := range n.Children {
		filteredChild := filterByOpTypeRecursive(child, targetOpType)
		if filteredChild != nil {
			filteredChildren = append(filteredChildren, filteredChild)
		}
	}

	if len(filteredChildren) == 0 && n.Kind != KindScalar {
		// If no children match and this isn't a scalar, check if this node itself matches
		if n.Kind == KindStruct && n.TypeName == "Operation" && opMatchesType(n, targetOpType) {
			return n
		}
		return nil
	}

	newNode := &Node{
		Name:     n.Name,
		Kind:     n.Kind,
		TypeName: n.TypeName,
		Value:    n.Value,
		Note:     n.Note,
		Children: filteredChildren,
	}

	return newNode
}

func opMatchesType(opNode *Node, targetOpType string) bool {
	if opNode == nil || opNode.Kind != KindStruct {
		return false
	}
	// Find the "Body" field and then the "Type" field within it
	bodyNode := opNode.Child("Body")
	if bodyNode == nil {
		return false
	}
	typeNode := bodyNode.Child("Type")
	if typeNode == nil {
		return false
	}
	// typeNode.Value should be the enum string like "OperationTypePayment"
	if typeNode.Value == nil {
		return false
	}
	typeStr := fmt.Sprintf("%v", typeNode.Value)
	typeStr = strings.TrimPrefix(typeStr, "OperationType")
	typeStr = normalizeOpType(typeStr)
	return typeStr == targetOpType
}

// safeString calls String() while tolerating a panic. Generated XDR helpers
// can dereference union arms that are absent in a malformed value, and this
// tool is expected to survive hostile input rather than crash on it.
func safeString(s fmt.Stringer) (out string) {
	defer func() {
		if r := recover(); r != nil {
			out = ""
		}
	}()
	return s.String()
}
