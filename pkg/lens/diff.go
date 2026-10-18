package lens

import (
	"fmt"
	"strconv"
	"strings"
)

// ChangeOp classifies a single difference between two XDR values.
type ChangeOp uint8

const (
	// ChangeModified means the path exists on both sides with different values.
	ChangeModified ChangeOp = iota
	// ChangeAdded means the path exists only on the right-hand side.
	ChangeAdded
	// ChangeRemoved means the path exists only on the left-hand side.
	ChangeRemoved
	// ChangeTypeChanged means the path exists on both sides but holds a
	// different kind of value, such as a scalar replaced by a struct. This
	// is the usual signal that a union switched arms.
	ChangeTypeChanged
)

func (c ChangeOp) String() string {
	switch c {
	case ChangeModified:
		return "modified"
	case ChangeAdded:
		return "added"
	case ChangeRemoved:
		return "removed"
	case ChangeTypeChanged:
		return "type_changed"
	default:
		return "unknown"
	}
}

// Change is one difference between two decoded XDR values.
type Change struct {
	// Path locates the change, e.g. "V1.Tx.Operations[0].Body.PaymentOp.Amount".
	Path string
	// Op is the kind of change.
	Op ChangeOp
	// Before is the left-hand value; nil when Op is ChangeAdded.
	Before any
	// After is the right-hand value; nil when Op is ChangeRemoved.
	After any
	// BeforeNote and AfterNote carry the enriched renderings, so a diff of
	// two amounts can show "100.0000000" rather than only the stroop count.
	BeforeNote string
	AfterNote  string
}

// String renders a change as a single line.
func (c Change) String() string {
	switch c.Op {
	case ChangeAdded:
		return fmt.Sprintf("+ %s = %s", c.Path, display(c.After, c.AfterNote))
	case ChangeRemoved:
		return fmt.Sprintf("- %s = %s", c.Path, display(c.Before, c.BeforeNote))
	default:
		return fmt.Sprintf("~ %s: %s -> %s", c.Path,
			display(c.Before, c.BeforeNote), display(c.After, c.AfterNote))
	}
}

func display(v any, note string) string {
	if v == nil && note == "" {
		return "(none)"
	}
	if v == nil {
		return note
	}
	s := fmt.Sprintf("%v", v)
	if note != "" {
		return s + " (" + note + ")"
	}
	return s
}

// DiffResult is the outcome of comparing two decoded values.
type DiffResult struct {
	// LeftType and RightType are the XDR types compared. They may differ,
	// which is itself worth reporting.
	LeftType  string
	RightType string
	// Changes lists every difference, in document order.
	Changes []Change
}

// DiffOptions configures structural comparison behavior.
type DiffOptions struct {
	// AlignLists matches unchanged list elements with a longest-common-
	// subsequence pass before comparing them. It is useful when insertions
	// would otherwise shift every later positional diff.
	AlignLists bool
	// Semantic compares supported ScVal integers as exact decimal values and
	// pairs ScVal map entries by canonical key instead of list position.
	Semantic bool
	// UnorderedSigners compares signature and signer collections as unordered
	// multisets, so reordering them is not reported as a difference. Only the
	// fields the protocol defines as sets (Signatures, Signers and
	// ExtraSigners) are affected; every other list stays positional.
	UnorderedSigners bool
	// Filter restricts the reported changes to the paths it selects. A nil
	// filter reports every change. Filtering happens after the walk, so it
	// only narrows the output; it never changes how the values are compared.
	Filter *PathFilter
}

// Equal reports whether the two values were structurally identical.
func (d *DiffResult) Equal() bool { return len(d.Changes) == 0 }

// Diff structurally compares two decoded XDR values and reports every
// differing path.
//
// Unlike a byte or ordering comparison, this walks both trees together and
// names the fields that actually changed, which is what makes it useful for
// answering "what is different about these two envelopes".
//
// Values of different XDR types can still be compared; the type difference is
// reported and the walk proceeds, since a TransactionV0 and a Transaction
// often share most of their structure.
func Diff(left, right *Value) (*DiffResult, error) {
	return DiffWithOptions(left, right, DiffOptions{})
}

// DiffWithOptions structurally compares two values using the supplied options.
// The default Diff function remains positional for ordered XDR sequences.
func DiffWithOptions(left, right *Value, options DiffOptions) (*DiffResult, error) {
	if left == nil || right == nil {
		return nil, fmt.Errorf("%w: diff requires two decoded values", ErrInvalidArgument)
	}
	leftNode, rightNode := left.Node, right.Node
	if options.Semantic {
		var err error
		leftNode, err = scValNodeForValue(left, true)
		if err != nil {
			return nil, fmt.Errorf("preparing left semantic value: %w", err)
		}
		rightNode, err = scValNodeForValue(right, true)
		if err != nil {
			return nil, fmt.Errorf("preparing right semantic value: %w", err)
		}
	}
	res := &DiffResult{LeftType: left.Type, RightType: right.Type}
	diffNodesWithOptions(leftNode, rightNode, "", &res.Changes, options)
	if !options.Filter.Empty() {
		res.Changes = FilterChanges(res.Changes, options.Filter)
	}
	return res, nil
}

// diffNodesWithOptions walks two subtrees in parallel, appending changes as it goes.
func diffNodesWithOptions(a, b *Node, path string, out *[]Change, options DiffOptions) {
	switch {
	case a == nil && b == nil:
		return
	case a == nil:
		*out = append(*out, Change{Path: path, Op: ChangeAdded, After: leafValue(b), AfterNote: b.Note})
		return
	case b == nil:
		*out = append(*out, Change{Path: path, Op: ChangeRemoved, Before: leafValue(a), BeforeNote: a.Note})
		return
	}

	if a.Kind != b.Kind {
		*out = append(*out, Change{
			Path:       path,
			Op:         ChangeTypeChanged,
			Before:     leafValue(a),
			After:      leafValue(b),
			BeforeNote: a.Note,
			AfterNote:  b.Note,
		})
		return
	}

	switch a.Kind {
	case KindScalar:
		if !sameScalar(a, b) {
			*out = append(*out, Change{
				Path:       path,
				Op:         ChangeModified,
				Before:     a.Value,
				After:      b.Value,
				BeforeNote: a.Note,
				AfterNote:  b.Note,
			})
		}

	case KindList:
		if options.UnorderedSigners && isSignerCollection(signerCollectionName(path, a, b)) {
			diffMultisetLists(a, b, path, out)
		} else {
			diffListsWithOptions(a, b, path, out, options)
		}

	case KindStruct:
		diffStructsWithOptions(a, b, path, out, options)
	}
}

// diffStructs compares two records field by field, preserving the field order
// of the left-hand side so output reads top-to-bottom like the source.
func diffStructsWithOptions(a, b *Node, path string, out *[]Change, options DiffOptions) {
	seen := make(map[string]bool, len(a.Children))

	for _, ac := range a.Children {
		seen[ac.Name] = true
		diffNodesWithOptions(ac, b.Child(ac.Name), join(path, ac.Name), out, options)
	}
	// Fields present only on the right: a union that switched arms shows up
	// here as a removal plus an addition.
	for _, bc := range b.Children {
		if seen[bc.Name] {
			continue
		}
		diffNodesWithOptions(nil, bc, join(path, bc.Name), out, options)
	}
}

const maxAlignmentCells = 1 << 20

// diffListsWithOptions compares sequences positionally unless alignment was
// requested. Very large products fall back to positional comparison to bound
// memory use on untrusted XDR input.
func diffListsWithOptions(a, b *Node, path string, out *[]Change, options DiffOptions) {
	if options.AlignLists && len(a.Children) > 0 && len(b.Children) > 0 &&
		len(a.Children) <= maxAlignmentCells/len(b.Children) {
		diffAlignedLists(a.Children, b.Children, path, out, options)
		return
	}
	maxLen := len(a.Children)
	if len(b.Children) > maxLen {
		maxLen = len(b.Children)
	}
	for i := range maxLen {
		var ac, bc *Node
		if i < len(a.Children) {
			ac = a.Children[i]
		}
		if i < len(b.Children) {
			bc = b.Children[i]
		}
		diffNodesWithOptions(ac, bc, path+"["+strconv.Itoa(i)+"]", out, options)
	}
}

func diffAlignedLists(left, right []*Node, path string, out *[]Change, options DiffOptions) {
	rows, columns := len(left)+1, len(right)+1
	lcs := make([][]int, rows)
	for i := range rows {
		lcs[i] = make([]int, columns)
	}
	for i := len(left) - 1; i >= 0; i-- {
		for j := len(right) - 1; j >= 0; j-- {
			if sameNode(left[i], right[j]) {
				lcs[i][j] = 1 + lcs[i+1][j+1]
			} else if lcs[i+1][j] > lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}

	i, j := 0, 0
	for i < len(left) && j < len(right) {
		if sameNode(left[i], right[j]) {
			diffNodesWithOptions(left[i], right[j], path+"["+strconv.Itoa(j)+"]", out, options)
			i++
			j++
		} else if lcs[i][j+1] >= lcs[i+1][j] {
			diffNodesWithOptions(nil, right[j], path+"["+strconv.Itoa(j)+"]", out, options)
			j++
		} else {
			diffNodesWithOptions(left[i], nil, path+"["+strconv.Itoa(i)+"]", out, options)
			i++
		}
	}
	for ; i < len(left); i++ {
		diffNodesWithOptions(left[i], nil, path+"["+strconv.Itoa(i)+"]", out, options)
	}
	for ; j < len(right); j++ {
		diffNodesWithOptions(nil, right[j], path+"["+strconv.Itoa(j)+"]", out, options)
	}
}

func sameNode(a, b *Node) bool {
	if a == nil || b == nil || a.Kind != b.Kind || a.Name != b.Name {
		return a == b
	}
	if a.Kind == KindScalar {
		return sameScalar(a, b)
	}
	if len(a.Children) != len(b.Children) {
		return false
	}
	for i := range a.Children {
		if !sameNode(a.Children[i], b.Children[i]) {
			return false
		}
	}
	return true
}

func sameScalar(a, b *Node) bool {
	if a.Note != b.Note {
		return false
	}
	return fmt.Sprintf("%v", a.Value) == fmt.Sprintf("%v", b.Value)
}

// leafValue summarises a node for reporting in a change. Struct and list
// nodes are described rather than expanded, since the change line names the
// path and the detail lives in the tree output.
func leafValue(n *Node) any {
	if n == nil {
		return nil
	}
	switch n.Kind {
	case KindScalar:
		return n.Value
	case KindList:
		return fmt.Sprintf("[%d item(s)]", len(n.Children))
	default:
		if n.TypeName != "" {
			return "{" + n.TypeName + "}"
		}
		return "{...}"
	}
}

func join(path, name string) string {
	if path == "" {
		return name
	}
	if name == "" {
		return path
	}
	return path + "." + name
}

// DiffBase64 is a convenience wrapper that decodes both payloads and diffs
// them. When typeName is empty the type is auto-detected for each side
// independently.
func DiffBase64(leftPayload, rightPayload, typeName string) (*DiffResult, error) {
	return DiffBase64WithOptions(leftPayload, rightPayload, typeName, DiffOptions{})
}

// DiffBase64WithOptions decodes and compares two payloads using the supplied
// structural diff options. When typeName is empty, each side is detected
// independently.
func DiffBase64WithOptions(leftPayload, rightPayload, typeName string, options DiffOptions) (*DiffResult, error) {
	decode := func(p string) (*Value, error) {
		if typeName != "" {
			return DecodeAs(p, typeName)
		}
		return Decode(p)
	}
	left, err := decode(leftPayload)
	if err != nil {
		return nil, fmt.Errorf("decoding left value: %w", err)
	}
	right, err := decode(rightPayload)
	if err != nil {
		return nil, fmt.Errorf("decoding right value: %w", err)
	}
	return DiffWithOptions(left, right, options)
}

// Summary renders a one-line description of the diff, suitable for a CI log.
func (d *DiffResult) Summary() string {
	if d.Equal() {
		return "no differences"
	}
	var added, removed, modified int
	for _, c := range d.Changes {
		switch c.Op {
		case ChangeAdded:
			added++
		case ChangeRemoved:
			removed++
		default:
			modified++
		}
	}
	var parts []string
	if modified > 0 {
		parts = append(parts, strconv.Itoa(modified)+" changed")
	}
	if added > 0 {
		parts = append(parts, strconv.Itoa(added)+" added")
	}
	if removed > 0 {
		parts = append(parts, strconv.Itoa(removed)+" removed")
	}
	return strings.Join(parts, ", ")
}
