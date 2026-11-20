package lens

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/stellar/go-stellar-sdk/xdr"
)

// ContractSpec holds parsed Soroban contract specifications.
type ContractSpec struct {
	Functions  map[string]xdr.ScSpecFunctionV0
	Structs    map[string]xdr.ScSpecUdtStructV0
	Unions     map[string]xdr.ScSpecUdtUnionV0
	Enums      map[string]xdr.ScSpecUdtEnumV0
	ErrorEnums map[string]xdr.ScSpecUdtErrorEnumV0
	Events     map[string]xdr.ScSpecEventV0
	Entries    []xdr.ScSpecEntry
}

// NewContractSpec allocates an empty ContractSpec.
func NewContractSpec() *ContractSpec {
	return &ContractSpec{
		Functions:  make(map[string]xdr.ScSpecFunctionV0),
		Structs:    make(map[string]xdr.ScSpecUdtStructV0),
		Unions:     make(map[string]xdr.ScSpecUdtUnionV0),
		Enums:      make(map[string]xdr.ScSpecUdtEnumV0),
		ErrorEnums: make(map[string]xdr.ScSpecUdtErrorEnumV0),
		Events:     make(map[string]xdr.ScSpecEventV0),
	}
}

// AddEntry adds an ScSpecEntry into the specification index.
func (cs *ContractSpec) AddEntry(e xdr.ScSpecEntry) {
	cs.Entries = append(cs.Entries, e)
	switch e.Kind {
	case xdr.ScSpecEntryKindScSpecEntryFunctionV0:
		if fn, ok := e.GetFunctionV0(); ok {
			cs.Functions[string(fn.Name)] = fn
		}
	case xdr.ScSpecEntryKindScSpecEntryUdtStructV0:
		if st, ok := e.GetUdtStructV0(); ok {
			cs.Structs[st.Name] = st
		}
	case xdr.ScSpecEntryKindScSpecEntryUdtUnionV0:
		if un, ok := e.GetUdtUnionV0(); ok {
			cs.Unions[un.Name] = un
		}
	case xdr.ScSpecEntryKindScSpecEntryUdtEnumV0:
		if en, ok := e.GetUdtEnumV0(); ok {
			cs.Enums[en.Name] = en
		}
	case xdr.ScSpecEntryKindScSpecEntryUdtErrorEnumV0:
		if errEn, ok := e.GetUdtErrorEnumV0(); ok {
			cs.ErrorEnums[errEn.Name] = errEn
		}
	case xdr.ScSpecEntryKindScSpecEntryEventV0:
		if ev, ok := e.GetEventV0(); ok {
			cs.Events[string(ev.Name)] = ev
		}
	}
}

// LoadContractSpecFile loads and parses a contract specification from a local file.
// The file can be a contract WASM (.wasm), the JSON interface export printed by
// `stellar contract info interface --output json`, or base64/raw XDR
// ScSpecEntry data.
func LoadContractSpecFile(path string) (*ContractSpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading spec file %s: %w", path, err)
	}
	return ParseContractSpec(data)
}

// ParseContractSpec parses a contract specification from raw bytes or base64 text.
// If the payload is a WASM binary, it extracts the contractspecv0 section first.
// A JSON spec export (see LoadContractSpecFile) is also accepted.
func ParseContractSpec(data []byte) (*ContractSpec, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("%w: empty contract spec data", ErrInvalidContractSpec)
	}

	if isSpecJSON(data) {
		entries, err := parseSpecJSON(data)
		if err != nil {
			return nil, err
		}
		if len(entries) == 0 {
			return nil, fmt.Errorf("%w: no ScSpecEntry definitions found", ErrInvalidContractSpec)
		}
		cs := NewContractSpec()
		for _, e := range entries {
			cs.AddEntry(e)
		}
		return cs, nil
	}

	// If it's a WASM binary, extract custom section
	if isWasm(data) {
		specBytes, err := ExtractWasmContractSpec(data)
		if err != nil {
			return nil, err
		}
		data = specBytes
	} else if isBase64(data) {
		// Tolerates base64 string
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
		if err == nil && len(decoded) > 0 {
			if isWasm(decoded) {
				specBytes, err := ExtractWasmContractSpec(decoded)
				if err != nil {
					return nil, err
				}
				data = specBytes
			} else {
				data = decoded
			}
		}
	}

	cs := NewContractSpec()
	buf := bytes.NewReader(data)

	// Decode repeated ScSpecEntry until EOF
	for buf.Len() > 0 {
		var entry xdr.ScSpecEntry
		_, err := xdr.Unmarshal(buf, &entry)
		if err != nil {
			if len(cs.Entries) > 0 {
				// We already decoded at least one entry
				break
			}
			return nil, fmt.Errorf("%w: xdr unmarshal ScSpecEntry: %w", ErrInvalidContractSpec, err)
		}
		cs.AddEntry(entry)
	}

	if len(cs.Entries) == 0 {
		return nil, fmt.Errorf("%w: no ScSpecEntry definitions found", ErrInvalidContractSpec)
	}

	return cs, nil
}

// ExtractWasmContractSpec extracts the "contractspecv0" custom section from WebAssembly binary.
func ExtractWasmContractSpec(wasmData []byte) ([]byte, error) {
	if len(wasmData) < 8 || !isWasm(wasmData) {
		return nil, ErrInvalidWasm
	}

	r := bytes.NewReader(wasmData[8:]) // Skip 4-byte magic + 4-byte version

	for {
		sectionID, err := r.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("%w: reading wasm section id: %w", ErrMalformedWASM, err)
		}

		sectionLen, err := readVarUint32(r)
		if err != nil {
			return nil, fmt.Errorf("%w: reading section length: %w", ErrMalformedWASM, err)
		}
		if uint64(sectionLen) > uint64(r.Len()) {
			return nil, fmt.Errorf("%w: section %d claims %d byte(s), %d remain",
				ErrMalformedWASM, sectionID, sectionLen, r.Len())
		}

		if sectionID == 0 { // Custom section
			startPos, err := r.Seek(0, io.SeekCurrent)
			if err != nil {
				return nil, fmt.Errorf("%w: seeking section: %w", ErrMalformedWASM, err)
			}
			nameLen, err := readVarUint32(r)
			if err != nil {
				return nil, fmt.Errorf("%w: reading custom section name len: %w", ErrMalformedWASM, err)
			}
			nameBytes := make([]byte, nameLen)
			if _, err := io.ReadFull(r, nameBytes); err != nil {
				return nil, fmt.Errorf("%w: reading custom section name: %w", ErrMalformedWASM, err)
			}

			afterNamePos, err := r.Seek(0, io.SeekCurrent)
			if err != nil {
				return nil, fmt.Errorf("%w: seeking section: %w", ErrMalformedWASM, err)
			}
			nameHeaderSize := uint32(afterNamePos - startPos)
			if nameHeaderSize > sectionLen {
				return nil, fmt.Errorf("%w: custom section name uses %d byte(s), section has %d",
					ErrMalformedWASM, nameHeaderSize, sectionLen)
			}

			if string(nameBytes) == "contractspecv0" {
				payloadLen := sectionLen - nameHeaderSize
				payload := make([]byte, payloadLen)
				if _, err := io.ReadFull(r, payload); err != nil {
					return nil, fmt.Errorf("%w: reading contractspecv0 payload: %w", ErrMalformedWASM, err)
				}
				return payload, nil
			}

			// Skip remaining custom section
			remaining := int64(sectionLen - nameHeaderSize)
			if _, err := r.Seek(remaining, io.SeekCurrent); err != nil {
				return nil, fmt.Errorf("%w: seeking past custom section: %w", ErrMalformedWASM, err)
			}
		} else {
			// Skip other sections
			if _, err := r.Seek(int64(sectionLen), io.SeekCurrent); err != nil {
				return nil, fmt.Errorf("%w: seeking past section %d: %w", ErrMalformedWASM, sectionID, err)
			}
		}
	}

	return nil, ErrNoContractSpecSection
}

func isWasm(b []byte) bool {
	return len(b) >= 4 && b[0] == 0x00 && b[1] == 'a' && b[2] == 's' && b[3] == 'm'
}

func isBase64(b []byte) bool {
	s := strings.TrimSpace(string(b))
	if len(s) == 0 || len(s)%4 != 0 {
		return false
	}
	for _, r := range s {
		if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '+' && r != '/' && r != '=' {
			return false
		}
	}
	return true
}

func readVarUint32(r io.ByteReader) (uint32, error) {
	var result uint32
	var shift uint
	for {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		result |= uint32(b&0x7f) << shift
		if b&0x80 == 0 {
			break
		}
		shift += 7
		if shift > 35 {
			return 0, fmt.Errorf("%w: varuint32 overflow", ErrMalformedWASM)
		}
	}
	return result, nil
}

// DecodeScValNamed decodes an ScVal using a named UDT type from the spec.
func (cs *ContractSpec) DecodeScValNamed(val *xdr.ScVal, typeName string) (*Node, error) {
	if val == nil {
		return nil, fmt.Errorf("%w: nil ScVal", ErrInvalidArgument)
	}

	if st, ok := cs.Structs[typeName]; ok {
		return cs.decodeStruct(val, st), nil
	}
	if en, ok := cs.Enums[typeName]; ok {
		return cs.decodeEnum(val, en), nil
	}
	if un, ok := cs.Unions[typeName]; ok {
		return cs.decodeUnion(val, un), nil
	}

	return nil, fmt.Errorf("%w: %q", ErrTypeNotFound, typeName)
}

// DecodeInvocation decodes function arguments for a named contract function invocation.
func (cs *ContractSpec) DecodeInvocation(funcName string, args []xdr.ScVal) (*Node, error) {
	fn, ok := cs.Functions[funcName]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrFunctionNotFound, funcName)
	}

	node := &Node{
		Name:     funcName,
		Kind:     KindStruct,
		TypeName: "InvokeContractArgs",
	}

	for i, arg := range args {
		var argNode *Node
		argName := fmt.Sprintf("arg[%d]", i)
		if i < len(fn.Inputs) {
			input := fn.Inputs[i]
			argName = input.Name
			argNode = cs.DecodeTypeDef(&arg, input.Type)
		} else {
			argVal := arg
			argNode = cs.decodeGenericScVal(&argVal)
		}
		argNode.Name = argName
		node.Children = append(node.Children, argNode)
	}

	return node, nil
}

// ApplyToValue enriches any Soroban nodes in v using the contract specification.
func (cs *ContractSpec) ApplyToValue(v *Value) {
	if v == nil || v.Node == nil || cs == nil {
		return
	}
	cs.ApplyToNode(v.Node)
}

// ApplyToNode recursively searches for Soroban structures (such as InvokeContract or ScVal)
// and decorates them with real field names, function arguments, and enum variant names.
// Decoded ScVal subtrees are mapped by a SpecResolver; values the spec does not
// describe keep their wire shape.
func (cs *ContractSpec) ApplyToNode(n *Node) {
	if n == nil || cs == nil {
		return
	}
	cs.applyToNode(n, NewSpecResolver(cs))
}

func (cs *ContractSpec) applyToNode(n *Node, r *SpecResolver) {
	if n == nil {
		return
	}

	// Look for InvokeContract nodes
	if n.Name == "InvokeContract" && cs.applyToInvocation(n, r) {
		for _, child := range n.Children {
			if child.Name != "Args" {
				cs.applyToNode(child, r)
			}
		}
		return
	}

	// A decoded ScVal is resolved as a whole: the resolver knows which
	// positions are typed, so nothing below it is guessed at again.
	if isScValNode(n) {
		r.MapScValWithSpec(n, nil)
		return
	}

	for _, child := range n.Children {
		cs.applyToNode(child, r)
	}
}

// applyToInvocation names the arguments of a call to a function the spec
// declares and resolves each argument against its declared type. It reports
// false when the function is unknown, leaving the node to the generic walk.
func (cs *ContractSpec) applyToInvocation(n *Node, r *SpecResolver) bool {
	funcNode := n.Child("FunctionName")
	argsNode := n.Child("Args")
	if funcNode == nil || argsNode == nil {
		return false
	}
	fn, ok := cs.Functions[fmt.Sprintf("%v", funcNode.Value)]
	if !ok {
		return false
	}
	funcNode.Note = "contract method"
	for i, argChild := range argsNode.Children {
		if i >= len(fn.Inputs) {
			cs.applyToNode(argChild, r)
			continue
		}
		input := fn.Inputs[i]
		argChild.Name = input.Name
		typeName := specTypeName(input.Type)
		if argChild.Note == "" {
			argChild.Note = typeName
		} else {
			argChild.Note = typeName + " (" + argChild.Note + ")"
		}
		if isScValNode(argChild) {
			td := input.Type
			if !r.MapScValWithSpec(argChild, &td) {
				// The argument does not have the declared shape; still
				// resolve anything recognisable inside it.
				r.MapScValWithSpec(argChild, nil)
			}
			continue
		}
		// If the input type is a UDT, enrich inner fields
		if input.Type.Type == xdr.ScSpecTypeScSpecTypeUdt && input.Type.Udt != nil {
			cs.enrichUdtChild(argChild, input.Type.Udt.Name)
		}
	}
	return true
}

// SpecResolver maps decoded ScVal nodes onto the named types of a contract
// spec. On the wire a struct is a map keyed by field-name symbols (or a vector
// for tuple structs), a union is a vector led by the case-name symbol, and an
// enum is a bare u32; the resolver recognises those shapes and decorates the
// node tree with the struct, case and variant names while leaving the wire
// values themselves untouched.
type SpecResolver struct {
	spec *ContractSpec
}

// NewSpecResolver returns a resolver for the types declared in cs.
func NewSpecResolver(cs *ContractSpec) *SpecResolver {
	return &SpecResolver{spec: cs}
}

// MapScValWithSpec decorates n, a decoded ScVal node, with names from the
// spec. Map entries and tuple elements of a recognised struct are renamed to
// the struct's field names, and the ScVal's Type leaf gains a note naming the
// struct, union case or enum variant.
//
// When td is nil the type is inferred from the value's shape: a map whose
// symbol keys are exactly the fields of one struct, or a vector led by a case
// name of one union. A value that matches no declaration, or more than one,
// keeps its wire shape. MapScValWithSpec reports whether n was matched; when
// it returns false for a typed call, n is unchanged.
func (r *SpecResolver) MapScValWithSpec(n *Node, td *xdr.ScSpecTypeDef) bool {
	if r == nil || r.spec == nil || !isScValNode(n) {
		return false
	}
	if td != nil {
		return r.mapTyped(n, *td)
	}
	return r.mapInferred(n)
}

func (r *SpecResolver) mapTyped(n *Node, td xdr.ScSpecTypeDef) bool {
	wire, arm := scValArm(n)
	switch td.Type {
	case xdr.ScSpecTypeScSpecTypeUdt:
		if td.Udt == nil {
			return false
		}
		return r.mapUdt(n, td.Udt.Name)
	case xdr.ScSpecTypeScSpecTypeOption:
		if td.Option == nil {
			return false
		}
		if wire == "ScValTypeScvVoid" {
			return true
		}
		return r.mapTyped(n, td.Option.ValueType)
	case xdr.ScSpecTypeScSpecTypeResult:
		if td.Result == nil {
			return false
		}
		return r.mapTyped(n, td.Result.OkType) || r.mapTyped(n, td.Result.ErrorType)
	case xdr.ScSpecTypeScSpecTypeVec:
		if td.Vec == nil || wire != "ScValTypeScvVec" || arm == nil {
			return false
		}
		for _, elem := range arm.Children {
			r.mapTyped(elem, td.Vec.ElementType)
		}
		return true
	case xdr.ScSpecTypeScSpecTypeMap:
		if td.Map == nil || wire != "ScValTypeScvMap" || arm == nil {
			return false
		}
		for _, entry := range arm.Children {
			if key := entry.Child("Key"); key != nil {
				r.mapTyped(key, td.Map.KeyType)
			}
			if val := entry.Child("Val"); val != nil {
				r.mapTyped(val, td.Map.ValueType)
			}
		}
		return true
	case xdr.ScSpecTypeScSpecTypeTuple:
		if td.Tuple == nil || wire != "ScValTypeScvVec" || arm == nil || len(arm.Children) != len(td.Tuple.ValueTypes) {
			return false
		}
		for i, elem := range arm.Children {
			r.mapTyped(elem, td.Tuple.ValueTypes[i])
		}
		return true
	case xdr.ScSpecTypeScSpecTypeError:
		return wire == "ScValTypeScvError"
	}
	// Primitive types need no renaming; they match when the wire type
	// agrees.
	want, ok := primitiveWireTypes[td.Type]
	return ok && want == wire
}

// primitiveWireTypes maps the payload-free spec types to the ScVal type that
// carries them.
var primitiveWireTypes = map[xdr.ScSpecType]string{
	xdr.ScSpecTypeScSpecTypeBool:         "ScValTypeScvBool",
	xdr.ScSpecTypeScSpecTypeVoid:         "ScValTypeScvVoid",
	xdr.ScSpecTypeScSpecTypeU32:          "ScValTypeScvU32",
	xdr.ScSpecTypeScSpecTypeI32:          "ScValTypeScvI32",
	xdr.ScSpecTypeScSpecTypeU64:          "ScValTypeScvU64",
	xdr.ScSpecTypeScSpecTypeI64:          "ScValTypeScvI64",
	xdr.ScSpecTypeScSpecTypeTimepoint:    "ScValTypeScvTimepoint",
	xdr.ScSpecTypeScSpecTypeDuration:     "ScValTypeScvDuration",
	xdr.ScSpecTypeScSpecTypeU128:         "ScValTypeScvU128",
	xdr.ScSpecTypeScSpecTypeI128:         "ScValTypeScvI128",
	xdr.ScSpecTypeScSpecTypeU256:         "ScValTypeScvU256",
	xdr.ScSpecTypeScSpecTypeI256:         "ScValTypeScvI256",
	xdr.ScSpecTypeScSpecTypeBytes:        "ScValTypeScvBytes",
	xdr.ScSpecTypeScSpecTypeBytesN:       "ScValTypeScvBytes",
	xdr.ScSpecTypeScSpecTypeString:       "ScValTypeScvString",
	xdr.ScSpecTypeScSpecTypeSymbol:       "ScValTypeScvSymbol",
	xdr.ScSpecTypeScSpecTypeAddress:      "ScValTypeScvAddress",
	xdr.ScSpecTypeScSpecTypeMuxedAddress: "ScValTypeScvAddress",
}

func (r *SpecResolver) mapUdt(n *Node, name string) bool {
	cs := r.spec
	if st, ok := cs.Structs[name]; ok {
		return r.mapStruct(n, st)
	}
	if un, ok := cs.Unions[name]; ok {
		return r.mapUnion(n, un)
	}
	if en, ok := cs.Enums[name]; ok {
		return mapEnumCase(n, en.Name, enumCases(en))
	}
	if en, ok := cs.ErrorEnums[name]; ok {
		return mapEnumCase(n, en.Name, errorEnumCases(en))
	}
	return false
}

// mapStruct matches a struct in either wire form: a map with one symbol key
// per field, or, for tuple structs, a vector with one element per field.
func (r *SpecResolver) mapStruct(n *Node, st xdr.ScSpecUdtStructV0) bool {
	wire, arm := scValArm(n)
	if arm == nil || len(arm.Children) != len(st.Fields) {
		return false
	}
	switch wire {
	case "ScValTypeScvMap":
		fields := make(map[string]xdr.ScSpecUdtStructFieldV0, len(st.Fields))
		for _, f := range st.Fields {
			fields[f.Name] = f
		}
		for _, entry := range arm.Children {
			if _, ok := fields[mapEntrySymbol(entry)]; !ok {
				return false
			}
		}
		for _, entry := range arm.Children {
			f := fields[mapEntrySymbol(entry)]
			entry.Name = f.Name
			if val := entry.Child("Val"); val != nil {
				r.mapTyped(val, f.Type)
			}
		}
	case "ScValTypeScvVec":
		for i, elem := range arm.Children {
			elem.Name = st.Fields[i].Name
			r.mapTyped(elem, st.Fields[i].Type)
		}
	default:
		return false
	}
	setTypeNote(n, st.Name)
	return true
}

// mapUnion matches a union value: a vector whose first element is the case
// name and whose remaining elements are the case's tuple payload.
func (r *SpecResolver) mapUnion(n *Node, un xdr.ScSpecUdtUnionV0) bool {
	c, ok := unionCaseFor(n, un)
	if !ok {
		return false
	}
	_, arm := scValArm(n)
	if c.TupleCase != nil {
		for i, t := range c.TupleCase.Type {
			r.mapTyped(arm.Children[i+1], t)
		}
	}
	setTypeNote(n, un.Name+"::"+unionCaseName(c))
	return true
}

// unionCaseFor returns the case of un that n encodes, if any.
func unionCaseFor(n *Node, un xdr.ScSpecUdtUnionV0) (xdr.ScSpecUdtUnionCaseV0, bool) {
	wire, arm := scValArm(n)
	if wire != "ScValTypeScvVec" || arm == nil || len(arm.Children) == 0 {
		return xdr.ScSpecUdtUnionCaseV0{}, false
	}
	tag, ok := scValSymbol(arm.Children[0])
	if !ok {
		return xdr.ScSpecUdtUnionCaseV0{}, false
	}
	for _, c := range un.Cases {
		if unionCaseName(c) != tag {
			continue
		}
		arity := 0
		if c.TupleCase != nil {
			arity = len(c.TupleCase.Type)
		}
		if len(arm.Children) == arity+1 {
			return c, true
		}
	}
	return xdr.ScSpecUdtUnionCaseV0{}, false
}

func unionCaseName(c xdr.ScSpecUdtUnionCaseV0) string {
	switch {
	case c.VoidCase != nil:
		return c.VoidCase.Name
	case c.TupleCase != nil:
		return c.TupleCase.Name
	}
	return ""
}

type namedCase struct {
	name  string
	value uint32
}

func enumCases(en xdr.ScSpecUdtEnumV0) []namedCase {
	out := make([]namedCase, 0, len(en.Cases))
	for _, c := range en.Cases {
		out = append(out, namedCase{c.Name, uint32(c.Value)})
	}
	return out
}

func errorEnumCases(en xdr.ScSpecUdtErrorEnumV0) []namedCase {
	out := make([]namedCase, 0, len(en.Cases))
	for _, c := range en.Cases {
		out = append(out, namedCase{c.Name, uint32(c.Value)})
	}
	return out
}

// mapEnumCase matches an enum variant carried as a u32, or a contract error
// code carried as Error(Contract, #n), and notes the variant name on it.
func mapEnumCase(n *Node, enumName string, cases []namedCase) bool {
	wire, arm := scValArm(n)
	var leaf *Node
	switch wire {
	case "ScValTypeScvU32":
		leaf = arm
	case "ScValTypeScvError":
		if arm != nil {
			leaf = arm.Child("ContractCode")
		}
	}
	if leaf == nil {
		return false
	}
	v, ok := toUint32(leaf.Value)
	if !ok {
		return false
	}
	for _, c := range cases {
		if c.value == v {
			note := enumName + "::" + c.name
			leaf.Note = note
			setTypeNote(n, note)
			return true
		}
	}
	return false
}

// mapInferred resolves an ScVal whose type is not declared at this position.
// Only shapes that identify exactly one declaration are mapped; everything
// else is searched for nested values that do.
func (r *SpecResolver) mapInferred(n *Node) bool {
	wire, arm := scValArm(n)
	if arm == nil {
		return false
	}
	switch wire {
	case "ScValTypeScvMap":
		if st, ok := r.uniqueStructFor(arm); ok {
			return r.mapStruct(n, st)
		}
		matched := false
		for _, entry := range arm.Children {
			for _, part := range []string{"Key", "Val"} {
				if child := entry.Child(part); child != nil && r.mapInferred(child) {
					matched = true
				}
			}
		}
		return matched
	case "ScValTypeScvVec":
		if un, ok := r.uniqueUnionFor(n); ok {
			return r.mapUnion(n, un)
		}
		matched := false
		for _, elem := range arm.Children {
			if r.mapInferred(elem) {
				matched = true
			}
		}
		return matched
	case "ScValTypeScvU32":
		return r.noteEnumCandidates(arm)
	}
	return false
}

// uniqueStructFor returns the only struct whose field names are exactly the
// symbol keys of the map arm.
func (r *SpecResolver) uniqueStructFor(arm *Node) (xdr.ScSpecUdtStructV0, bool) {
	keys := make(map[string]bool, len(arm.Children))
	for _, entry := range arm.Children {
		sym := mapEntrySymbol(entry)
		if sym == "" {
			return xdr.ScSpecUdtStructV0{}, false
		}
		keys[sym] = true
	}
	var found []xdr.ScSpecUdtStructV0
	for _, name := range sortedKeys(r.spec.Structs) {
		st := r.spec.Structs[name]
		if len(st.Fields) != len(keys) {
			continue
		}
		all := true
		for _, f := range st.Fields {
			if !keys[f.Name] {
				all = false
				break
			}
		}
		if all {
			found = append(found, st)
		}
	}
	if len(found) != 1 {
		return xdr.ScSpecUdtStructV0{}, false
	}
	return found[0], true
}

// uniqueUnionFor returns the only union with a case that n encodes.
func (r *SpecResolver) uniqueUnionFor(n *Node) (xdr.ScSpecUdtUnionV0, bool) {
	var found []xdr.ScSpecUdtUnionV0
	for _, name := range sortedKeys(r.spec.Unions) {
		un := r.spec.Unions[name]
		if _, ok := unionCaseFor(n, un); ok {
			found = append(found, un)
		}
	}
	if len(found) != 1 {
		return xdr.ScSpecUdtUnionV0{}, false
	}
	return found[0], true
}

// noteEnumCandidates annotates an untyped u32 with every enum variant it
// could be. Without a declared type this is a hint, not a resolution, so the
// value is not reported as matched.
func (r *SpecResolver) noteEnumCandidates(leaf *Node) bool {
	v, ok := toUint32(leaf.Value)
	if !ok {
		return false
	}
	var candidates []string
	for _, name := range sortedKeys(r.spec.Enums) {
		for _, c := range r.spec.Enums[name].Cases {
			if uint32(c.Value) == v {
				candidates = append(candidates, name+"::"+c.Name)
				break
			}
		}
	}
	if len(candidates) > 0 {
		leaf.Note = strings.Join(candidates, " | ")
	}
	return false
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// isScValNode reports whether n is the node of a decoded xdr.ScVal.
func isScValNode(n *Node) bool {
	return n != nil && n.Kind == KindStruct && n.TypeName == "ScVal"
}

// scValArm returns an ScVal node's wire type name and its populated arm.
func scValArm(n *Node) (string, *Node) {
	typeNode := n.Child("Type")
	if typeNode == nil {
		return "", nil
	}
	wire, ok := typeNode.Value.(string)
	if !ok {
		return "", nil
	}
	for _, c := range n.Children {
		if c != typeNode {
			return wire, c
		}
	}
	return wire, nil
}

// scValSymbol returns the symbol an ScVal node carries.
func scValSymbol(n *Node) (string, bool) {
	if !isScValNode(n) {
		return "", false
	}
	wire, arm := scValArm(n)
	if wire != "ScValTypeScvSymbol" || arm == nil {
		return "", false
	}
	s, ok := arm.Value.(string)
	return s, ok
}

// mapEntrySymbol returns the symbol key of a decoded ScMapEntry node, or ""
// when the key is not a symbol.
func mapEntrySymbol(entry *Node) string {
	s, _ := scValSymbol(entry.Child("Key"))
	return s
}

func setTypeNote(n *Node, note string) {
	if typeNode := n.Child("Type"); typeNode != nil {
		typeNode.Note = note
	}
}

func (cs *ContractSpec) enrichUdtChild(n *Node, udtName string) {
	if st, ok := cs.Structs[udtName]; ok {
		n.TypeName = st.Name
		for i, field := range st.Fields {
			if i < len(n.Children) {
				n.Children[i].Name = field.Name
			}
		}
	}
}

func (cs *ContractSpec) decodeStruct(val *xdr.ScVal, st xdr.ScSpecUdtStructV0) *Node {
	node := &Node{
		Kind:     KindStruct,
		TypeName: st.Name,
	}

	switch val.Type {
	case xdr.ScValTypeScvMap:
		if val.Map != nil && *val.Map != nil {
			m := **val.Map
			for _, entry := range m {
				keySym := ""
				if entry.Key.Type == xdr.ScValTypeScvSymbol && entry.Key.Sym != nil {
					keySym = string(*entry.Key.Sym)
				}
				// Find matching field
				var matchedField *xdr.ScSpecUdtStructFieldV0
				for _, f := range st.Fields {
					if f.Name == keySym {
						matchedField = &f
						break
					}
				}

				var childNode *Node
				if matchedField != nil {
					childNode = cs.DecodeTypeDef(&entry.Val, matchedField.Type)
					childNode.Name = matchedField.Name
				} else {
					valCopy := entry.Val
					childNode = cs.decodeGenericScVal(&valCopy)
					childNode.Name = keySym
				}
				node.Children = append(node.Children, childNode)
			}
		}

	case xdr.ScValTypeScvVec:
		if val.Vec != nil && *val.Vec != nil {
			v := **val.Vec
			for i, elem := range v {
				var childNode *Node
				if i < len(st.Fields) {
					field := st.Fields[i]
					childNode = cs.DecodeTypeDef(&elem, field.Type)
					childNode.Name = field.Name
				} else {
					elemCopy := elem
					childNode = cs.decodeGenericScVal(&elemCopy)
					childNode.Name = fmt.Sprintf("[%d]", i)
				}
				node.Children = append(node.Children, childNode)
			}
		}
	}

	return node
}

func (cs *ContractSpec) decodeEnum(val *xdr.ScVal, en xdr.ScSpecUdtEnumV0) *Node {
	u32Val := uint32(0)
	if val.Type == xdr.ScValTypeScvU32 && val.U32 != nil {
		u32Val = uint32(*val.U32)
	}

	caseName := fmt.Sprintf("%d", u32Val)
	for _, c := range en.Cases {
		if uint32(c.Value) == u32Val {
			caseName = c.Name
			break
		}
	}

	return &Node{
		Kind:     KindScalar,
		TypeName: en.Name,
		Value:    caseName,
		Note:     fmt.Sprintf("%s::%s (%d)", en.Name, caseName, u32Val),
	}
}

func (cs *ContractSpec) decodeUnion(val *xdr.ScVal, un xdr.ScSpecUdtUnionV0) *Node {
	node := &Node{
		Kind:     KindStruct,
		TypeName: un.Name,
	}

	// Union encoded as vec with [case_symbol, tuple_values...] or map
	if val.Type == xdr.ScValTypeScvVec && val.Vec != nil && *val.Vec != nil && len(**val.Vec) > 0 {
		vec := **val.Vec
		caseName := ""
		if vec[0].Type == xdr.ScValTypeScvSymbol && vec[0].Sym != nil {
			caseName = string(*vec[0].Sym)
		}

		node.Name = caseName
		for _, c := range un.Cases {
			if c.Kind == xdr.ScSpecUdtUnionCaseV0KindScSpecUdtUnionCaseVoidV0 && c.VoidCase != nil {
				if c.VoidCase.Name == caseName {
					node.Note = "void"
					return node
				}
			}
			if c.Kind == xdr.ScSpecUdtUnionCaseV0KindScSpecUdtUnionCaseTupleV0 && c.TupleCase != nil {
				if c.TupleCase.Name == caseName {
					for j, tType := range c.TupleCase.Type {
						if j+1 < len(vec) {
							childVal := vec[j+1]
							cNode := cs.DecodeTypeDef(&childVal, tType)
							cNode.Name = fmt.Sprintf("[%d]", j)
							node.Children = append(node.Children, cNode)
						}
					}
					return node
				}
			}
		}
	}

	valCopy := *val
	return cs.decodeGenericScVal(&valCopy)
}

// DecodeTypeDef decodes an ScVal according to an ScSpecTypeDef definition.
func (cs *ContractSpec) DecodeTypeDef(val *xdr.ScVal, td xdr.ScSpecTypeDef) *Node {
	if td.Type == xdr.ScSpecTypeScSpecTypeUdt && td.Udt != nil {
		if decoded, err := cs.DecodeScValNamed(val, td.Udt.Name); err == nil {
			return decoded
		}
	}

	valCopy := *val
	node := cs.decodeGenericScVal(&valCopy)
	node.Note = specTypeName(td)
	return node
}

func (cs *ContractSpec) decodeGenericScVal(val *xdr.ScVal) *Node {
	switch val.Type {
	case xdr.ScValTypeScvBool:
		if val.B != nil {
			return &Node{Kind: KindScalar, Value: *val.B}
		}
	case xdr.ScValTypeScvVoid:
		return &Node{Kind: KindScalar, Value: "void"}
	case xdr.ScValTypeScvU32:
		if val.U32 != nil {
			return &Node{Kind: KindScalar, Value: int64(*val.U32)}
		}
	case xdr.ScValTypeScvI32:
		if val.I32 != nil {
			return &Node{Kind: KindScalar, Value: int64(*val.I32)}
		}
	case xdr.ScValTypeScvU64:
		if val.U64 != nil {
			return &Node{Kind: KindScalar, Value: uint64(*val.U64)}
		}
	case xdr.ScValTypeScvI64:
		if val.I64 != nil {
			return &Node{Kind: KindScalar, Value: int64(*val.I64)}
		}
	case xdr.ScValTypeScvSymbol:
		if val.Sym != nil {
			return &Node{Kind: KindScalar, Value: string(*val.Sym)}
		}
	case xdr.ScValTypeScvString:
		if val.Str != nil {
			return &Node{Kind: KindScalar, Value: string(*val.Str)}
		}
	case xdr.ScValTypeScvAddress:
		if val.Address != nil {
			addrStr, err := val.Address.String()
			if err != nil {
				addrStr = fmt.Sprintf("%v", val.Address)
			}
			return &Node{Kind: KindScalar, Value: addrStr}
		}
	case xdr.ScValTypeScvVec:
		node := &Node{Kind: KindList}
		if val.Vec != nil && *val.Vec != nil {
			for _, elem := range **val.Vec {
				elemCopy := elem
				node.Children = append(node.Children, cs.decodeGenericScVal(&elemCopy))
			}
		}
		return node
	case xdr.ScValTypeScvMap:
		node := &Node{Kind: KindStruct}
		if val.Map != nil && *val.Map != nil {
			for _, entry := range **val.Map {
				keyStr := fmt.Sprintf("%v", entry.Key)
				if entry.Key.Type == xdr.ScValTypeScvSymbol && entry.Key.Sym != nil {
					keyStr = string(*entry.Key.Sym)
				}
				valCopy := entry.Val
				c := cs.decodeGenericScVal(&valCopy)
				c.Name = keyStr
				node.Children = append(node.Children, c)
			}
		}
		return node
	}
	return &Node{Kind: KindScalar, Value: val.String()}
}

func specTypeName(td xdr.ScSpecTypeDef) string {
	switch td.Type {
	case xdr.ScSpecTypeScSpecTypeUdt:
		if td.Udt != nil {
			return td.Udt.Name
		}
		return "UDT"
	case xdr.ScSpecTypeScSpecTypeU32:
		return "u32"
	case xdr.ScSpecTypeScSpecTypeI32:
		return "i32"
	case xdr.ScSpecTypeScSpecTypeU64:
		return "u64"
	case xdr.ScSpecTypeScSpecTypeI64:
		return "i64"
	case xdr.ScSpecTypeScSpecTypeBool:
		return "bool"
	case xdr.ScSpecTypeScSpecTypeSymbol:
		return "symbol"
	case xdr.ScSpecTypeScSpecTypeString:
		return "string"
	case xdr.ScSpecTypeScSpecTypeAddress:
		return "address"
	case xdr.ScSpecTypeScSpecTypeVec:
		return "vec"
	case xdr.ScSpecTypeScSpecTypeMap:
		return "map"
	default:
		return td.Type.String()
	}
}

func toUint32(v any) (uint32, bool) {
	switch val := v.(type) {
	case uint32:
		return val, true
	case int64:
		return uint32(val), true
	case uint64:
		return uint32(val), true
	case int:
		return uint32(val), true
	default:
		return 0, false
	}
}

// FormatContractSpec renders a contract interface as Rust-like pseudo-code:
// one declaration per spec entry, in stream order, with its doc comment above
// it. Function inputs, struct fields and enum cases keep their own doc
// comments, so the output reads the way the contract author wrote it.
func FormatContractSpec(entries []xdr.ScSpecEntry) string {
	var b strings.Builder
	for i, entry := range entries {
		if i > 0 {
			b.WriteString("\n")
		}
		writeSpecEntry(&b, entry)
	}
	return b.String()
}

// FormatSpecEntrySignature renders a single spec entry as a one-line
// declaration, such as "fn increment(caller: Address) -> u32" or
// "struct Point { x: u32, y: u32 }". Doc comments are left out.
func FormatSpecEntrySignature(entry xdr.ScSpecEntry) string {
	switch entry.Kind {
	case xdr.ScSpecEntryKindScSpecEntryFunctionV0:
		if fn, ok := entry.GetFunctionV0(); ok {
			return functionSignature(fn)
		}
	case xdr.ScSpecEntryKindScSpecEntryUdtStructV0:
		if st, ok := entry.GetUdtStructV0(); ok {
			fields := make([]string, 0, len(st.Fields))
			for _, f := range st.Fields {
				fields = append(fields, f.Name+": "+SpecTypeSignature(f.Type))
			}
			return declarationLine("struct", st.Name, fields)
		}
	case xdr.ScSpecEntryKindScSpecEntryUdtUnionV0:
		if un, ok := entry.GetUdtUnionV0(); ok {
			cases := make([]string, 0, len(un.Cases))
			for _, c := range un.Cases {
				cases = append(cases, unionCaseSignature(c))
			}
			return declarationLine("union", un.Name, cases)
		}
	case xdr.ScSpecEntryKindScSpecEntryUdtEnumV0:
		if en, ok := entry.GetUdtEnumV0(); ok {
			cases := make([]string, 0, len(en.Cases))
			for _, c := range en.Cases {
				cases = append(cases, fmt.Sprintf("%s = %d", c.Name, c.Value))
			}
			return declarationLine("enum", en.Name, cases)
		}
	case xdr.ScSpecEntryKindScSpecEntryUdtErrorEnumV0:
		if en, ok := entry.GetUdtErrorEnumV0(); ok {
			cases := make([]string, 0, len(en.Cases))
			for _, c := range en.Cases {
				cases = append(cases, fmt.Sprintf("%s = %d", c.Name, c.Value))
			}
			return declarationLine("error", en.Name, cases)
		}
	case xdr.ScSpecEntryKindScSpecEntryEventV0:
		if ev, ok := entry.GetEventV0(); ok {
			return declarationLine("event", string(ev.Name), eventMembers(ev))
		}
	}
	return fmt.Sprintf("<%s>", entry.Kind.String())
}

// SpecTypeSignature renders a spec type the way the Soroban SDK spells it in
// Rust source: u32, Address, Vec<Symbol>, Option<Point>, BytesN<32>.
func SpecTypeSignature(td xdr.ScSpecTypeDef) string {
	switch td.Type {
	case xdr.ScSpecTypeScSpecTypeVal:
		return "Val"
	case xdr.ScSpecTypeScSpecTypeBool:
		return "bool"
	case xdr.ScSpecTypeScSpecTypeVoid:
		return "()"
	case xdr.ScSpecTypeScSpecTypeError:
		return "Error"
	case xdr.ScSpecTypeScSpecTypeU32:
		return "u32"
	case xdr.ScSpecTypeScSpecTypeI32:
		return "i32"
	case xdr.ScSpecTypeScSpecTypeU64:
		return "u64"
	case xdr.ScSpecTypeScSpecTypeI64:
		return "i64"
	case xdr.ScSpecTypeScSpecTypeTimepoint:
		return "Timepoint"
	case xdr.ScSpecTypeScSpecTypeDuration:
		return "Duration"
	case xdr.ScSpecTypeScSpecTypeU128:
		return "u128"
	case xdr.ScSpecTypeScSpecTypeI128:
		return "i128"
	case xdr.ScSpecTypeScSpecTypeU256:
		return "U256"
	case xdr.ScSpecTypeScSpecTypeI256:
		return "I256"
	case xdr.ScSpecTypeScSpecTypeBytes:
		return "Bytes"
	case xdr.ScSpecTypeScSpecTypeString:
		return "String"
	case xdr.ScSpecTypeScSpecTypeSymbol:
		return "Symbol"
	case xdr.ScSpecTypeScSpecTypeAddress:
		return "Address"
	case xdr.ScSpecTypeScSpecTypeMuxedAddress:
		return "MuxedAddress"
	case xdr.ScSpecTypeScSpecTypeOption:
		if td.Option != nil {
			return "Option<" + SpecTypeSignature(td.Option.ValueType) + ">"
		}
	case xdr.ScSpecTypeScSpecTypeResult:
		if td.Result != nil {
			return "Result<" + SpecTypeSignature(td.Result.OkType) + ", " + SpecTypeSignature(td.Result.ErrorType) + ">"
		}
	case xdr.ScSpecTypeScSpecTypeVec:
		if td.Vec != nil {
			return "Vec<" + SpecTypeSignature(td.Vec.ElementType) + ">"
		}
	case xdr.ScSpecTypeScSpecTypeMap:
		if td.Map != nil {
			return "Map<" + SpecTypeSignature(td.Map.KeyType) + ", " + SpecTypeSignature(td.Map.ValueType) + ">"
		}
	case xdr.ScSpecTypeScSpecTypeTuple:
		if td.Tuple != nil {
			parts := make([]string, 0, len(td.Tuple.ValueTypes))
			for _, t := range td.Tuple.ValueTypes {
				parts = append(parts, SpecTypeSignature(t))
			}
			return "(" + strings.Join(parts, ", ") + ")"
		}
	case xdr.ScSpecTypeScSpecTypeBytesN:
		if td.BytesN != nil {
			return fmt.Sprintf("BytesN<%d>", td.BytesN.N)
		}
	case xdr.ScSpecTypeScSpecTypeUdt:
		if td.Udt != nil {
			return td.Udt.Name
		}
	}
	return td.Type.String()
}

func functionSignature(fn xdr.ScSpecFunctionV0) string {
	inputs := make([]string, 0, len(fn.Inputs))
	for _, in := range fn.Inputs {
		inputs = append(inputs, in.Name+": "+SpecTypeSignature(in.Type))
	}
	sig := fmt.Sprintf("fn %s(%s)", fn.Name, strings.Join(inputs, ", "))
	switch len(fn.Outputs) {
	case 0:
		return sig
	case 1:
		return sig + " -> " + SpecTypeSignature(fn.Outputs[0])
	default:
		outs := make([]string, 0, len(fn.Outputs))
		for _, o := range fn.Outputs {
			outs = append(outs, SpecTypeSignature(o))
		}
		return sig + " -> (" + strings.Join(outs, ", ") + ")"
	}
}

func unionCaseSignature(c xdr.ScSpecUdtUnionCaseV0) string {
	switch c.Kind {
	case xdr.ScSpecUdtUnionCaseV0KindScSpecUdtUnionCaseVoidV0:
		if c.VoidCase != nil {
			return c.VoidCase.Name
		}
	case xdr.ScSpecUdtUnionCaseV0KindScSpecUdtUnionCaseTupleV0:
		if c.TupleCase != nil {
			types := make([]string, 0, len(c.TupleCase.Type))
			for _, t := range c.TupleCase.Type {
				types = append(types, SpecTypeSignature(t))
			}
			return c.TupleCase.Name + "(" + strings.Join(types, ", ") + ")"
		}
	}
	return c.Kind.String()
}

// eventMembers lists an event's topics and parameters. Prefix topics are
// fixed symbols; each parameter is tagged with where it is published.
func eventMembers(ev xdr.ScSpecEventV0) []string {
	members := make([]string, 0, len(ev.PrefixTopics)+len(ev.Params))
	for _, topic := range ev.PrefixTopics {
		members = append(members, fmt.Sprintf("topic %q", string(topic)))
	}
	for _, p := range ev.Params {
		location := "data"
		if p.Location == xdr.ScSpecEventParamLocationV0ScSpecEventParamLocationTopicList {
			location = "topic"
		}
		members = append(members, fmt.Sprintf("%s %s: %s", location, p.Name, SpecTypeSignature(p.Type)))
	}
	return members
}

func declarationLine(keyword, name string, members []string) string {
	if len(members) == 0 {
		return keyword + " " + name + " {}"
	}
	return keyword + " " + name + " { " + strings.Join(members, ", ") + " }"
}

// writeSpecEntry writes one declaration of FormatContractSpec.
func writeSpecEntry(b *strings.Builder, entry xdr.ScSpecEntry) {
	switch entry.Kind {
	case xdr.ScSpecEntryKindScSpecEntryFunctionV0:
		fn, ok := entry.GetFunctionV0()
		if !ok {
			break
		}
		writeDoc(b, "", fn.Doc)
		for _, in := range fn.Inputs {
			if in.Doc != "" {
				writeDoc(b, "", in.Name+": "+in.Doc)
			}
		}
		b.WriteString(functionSignature(fn) + "\n")
		return
	case xdr.ScSpecEntryKindScSpecEntryUdtStructV0:
		st, ok := entry.GetUdtStructV0()
		if !ok {
			break
		}
		members := make([]specMember, 0, len(st.Fields))
		for _, f := range st.Fields {
			members = append(members, specMember{f.Doc, f.Name + ": " + SpecTypeSignature(f.Type)})
		}
		writeBlock(b, st.Doc, "struct "+st.Name, members)
		return
	case xdr.ScSpecEntryKindScSpecEntryUdtUnionV0:
		un, ok := entry.GetUdtUnionV0()
		if !ok {
			break
		}
		members := make([]specMember, 0, len(un.Cases))
		for _, c := range un.Cases {
			doc := ""
			switch {
			case c.VoidCase != nil:
				doc = c.VoidCase.Doc
			case c.TupleCase != nil:
				doc = c.TupleCase.Doc
			}
			members = append(members, specMember{doc, unionCaseSignature(c)})
		}
		writeBlock(b, un.Doc, "union "+un.Name, members)
		return
	case xdr.ScSpecEntryKindScSpecEntryUdtEnumV0:
		en, ok := entry.GetUdtEnumV0()
		if !ok {
			break
		}
		members := make([]specMember, 0, len(en.Cases))
		for _, c := range en.Cases {
			members = append(members, specMember{c.Doc, fmt.Sprintf("%s = %d", c.Name, c.Value)})
		}
		writeBlock(b, en.Doc, "enum "+en.Name, members)
		return
	case xdr.ScSpecEntryKindScSpecEntryUdtErrorEnumV0:
		en, ok := entry.GetUdtErrorEnumV0()
		if !ok {
			break
		}
		members := make([]specMember, 0, len(en.Cases))
		for _, c := range en.Cases {
			members = append(members, specMember{c.Doc, fmt.Sprintf("%s = %d", c.Name, c.Value)})
		}
		writeBlock(b, en.Doc, "error "+en.Name, members)
		return
	case xdr.ScSpecEntryKindScSpecEntryEventV0:
		ev, ok := entry.GetEventV0()
		if !ok {
			break
		}
		decls := eventMembers(ev)
		members := make([]specMember, 0, len(decls))
		for i, decl := range decls {
			doc := ""
			if i >= len(ev.PrefixTopics) {
				doc = ev.Params[i-len(ev.PrefixTopics)].Doc
			}
			members = append(members, specMember{doc, decl})
		}
		writeBlock(b, ev.Doc, "event "+string(ev.Name), members)
		return
	}
	b.WriteString(FormatSpecEntrySignature(entry) + "\n")
}

type specMember struct {
	doc  string
	decl string
}

func writeBlock(b *strings.Builder, doc, header string, members []specMember) {
	writeDoc(b, "", doc)
	if len(members) == 0 {
		b.WriteString(header + " {}\n")
		return
	}
	b.WriteString(header + " {\n")
	for _, m := range members {
		writeDoc(b, "    ", m.doc)
		b.WriteString("    " + m.decl + ",\n")
	}
	b.WriteString("}\n")
}

func writeDoc(b *strings.Builder, indent, doc string) {
	doc = strings.TrimSpace(doc)
	if doc == "" {
		return
	}
	for _, line := range strings.Split(doc, "\n") {
		b.WriteString(indent + "/// " + strings.TrimRight(line, " \t") + "\n")
	}
}
