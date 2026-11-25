package lens

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"reflect"

	"github.com/stellar/go-stellar-sdk/xdr"
	xdr3 "github.com/stellar/go-xdr/xdr3"
)

// A Soroban contract is WASM bytecode with its interface and build metadata
// embedded in WASM custom sections: contractspecv0 holds the ScSpecEntry
// stream that describes every exported function, type and event; contractenv
// metav0 holds the host interface version the contract was built against; and
// the optional contractmetav0 holds free-form key/value metadata such as the
// SDK version. A ContractCodeEntry on the wire is just the module hash plus an
// opaque byte array, which is why decoding one is only half the job — this
// file extracts the declarations that make the bytecode legible.
//
// The parser implements the subset of the WASM binary format needed to walk
// sections and read custom-section payloads. It is deliberately total and
// offline: no part of it executes the module, and malformed input yields an
// error rather than a panic.
// WASM custom section names that carry Soroban contract metadata, per
// SEP-48 and CAP-0046-01.
const (
	contractSpecSection    = "contractspecv0"
	contractEnvMetaSection = "contractenvmetav0"
	contractMetaSection    = "contractmetav0"
)

// wasmHeaderSize is the magic plus version that opens every module.
const wasmHeaderSize = 8

// wasmSectionCustom is the section id reserved for custom sections, whose
// payload begins with a name. All other section ids carry executable code or
// layout tables that this package does not interpret.
const wasmSectionCustom = 0

// ContractCode is the metadata embedded in a Soroban contract's WASM bytecode.
// Empty slices mean the corresponding custom section was absent, which is
// legitimate: older contracts predate the optional metadata section, and a
// contract may export nothing.
type ContractCode struct {
	// Size is the length of the module in bytes.
	Size int
	// Spec holds the contract interface declarations: the streamed
	// ScSpecEntry values from contractspecv0.
	Spec []xdr.ScSpecEntry
	// EnvMeta holds the host interface versions from contractenvmetav0.
	EnvMeta []xdr.ScEnvMetaEntry
	// Meta holds the free-form contract metadata from contractmetav0.
	Meta []xdr.ScMetaEntry
}

// ParseContractCode extracts the metadata sections from a Soroban contract's
// WASM bytecode.
//
// It returns ErrNotWASM when the bytes do not begin with the WASM magic and
// version 1, and an ErrMalformedWASM-wrapping error when the section structure
// or an embedded XDR stream is corrupt. A valid module with no metadata
// sections parses successfully and yields empty slices.
func ParseContractCode(code []byte) (parsed *ContractCode, err error) {
	defer func() {
		// The section walk indexes attacker-controlled offsets and the
		// embedded streams go through the SDK decoder; a bug or a hostile
		// module must surface as an error, never as a crash.
		if r := recover(); r != nil {
			parsed, err = nil, fmt.Errorf("%w: decoder panicked: %v", ErrMalformedWASM, r)
		}
	}()

	if !isWASM(code) {
		return nil, fmt.Errorf("%w: %d byte(s)", ErrNotWASM, len(code))
	}
	out := &ContractCode{Size: len(code)}

	rest := code[wasmHeaderSize:]
	for len(rest) > 0 {
		sectionID := rest[0]
		size, n, err := readU32(rest[1:])
		if err != nil {
			return nil, fmt.Errorf("%w: section %d size: %w", ErrMalformedWASM, sectionID, err)
		}
		rest = rest[1+n:]
		if uint64(size) > uint64(len(rest)) {
			return nil, fmt.Errorf("%w: section %d claims %d byte(s), %d remain",
				ErrMalformedWASM, sectionID, size, len(rest))
		}
		payload := rest[:size]
		rest = rest[size:]
		if sectionID != wasmSectionCustom {
			continue
		}
		name, content, err := customSection(payload)
		if err != nil {
			return nil, err
		}
		if err := out.addCustomSection(name, content); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// addCustomSection decodes the payload of a custom section this package
// understands and ignores every other name, which is what keeps the parser
// compatible with toolchain-specific sections.
func (c *ContractCode) addCustomSection(name string, content []byte) error {
	switch name {
	case contractSpecSection:
		err := decodeSectionStream(content, func(d *xdr3.Decoder) (int, error) {
			var entry xdr.ScSpecEntry
			n, err := entry.DecodeFrom(d, xdr3.DecodeDefaultMaxDepth)
			if err != nil {
				return n, err
			}
			c.Spec = append(c.Spec, entry)
			return n, nil
		})
		if err != nil {
			return fmt.Errorf("%w: section %s: %w", ErrMalformedWASM, name, err)
		}
	case contractEnvMetaSection:
		err := decodeSectionStream(content, func(d *xdr3.Decoder) (int, error) {
			var entry xdr.ScEnvMetaEntry
			n, err := entry.DecodeFrom(d, xdr3.DecodeDefaultMaxDepth)
			if err != nil {
				return n, err
			}
			c.EnvMeta = append(c.EnvMeta, entry)
			return n, nil
		})
		if err != nil {
			return fmt.Errorf("%w: section %s: %w", ErrMalformedWASM, name, err)
		}
	case contractMetaSection:
		err := decodeSectionStream(content, func(d *xdr3.Decoder) (int, error) {
			var entry xdr.ScMetaEntry
			n, err := entry.DecodeFrom(d, xdr3.DecodeDefaultMaxDepth)
			if err != nil {
				return n, err
			}
			c.Meta = append(c.Meta, entry)
			return n, nil
		})
		if err != nil {
			return fmt.Errorf("%w: section %s: %w", ErrMalformedWASM, name, err)
		}
	}
	return nil
}

// decodeSectionStream consumes a concatenated XDR stream, invoking decode for
// each entry. The sections are streams with no frame, header or length prefix
// (SEP-48), so the loop continues while bytes remain. A decoder that consumes
// no bytes would spin forever, so that is reported as malformed input.
func decodeSectionStream(content []byte, decode func(*xdr3.Decoder) (int, error)) error {
	reader := bytes.NewReader(content)
	opts := xdr3.DefaultDecodeOptions
	opts.MaxInputLen = len(content)
	decoder := xdr3.NewDecoderWithOptions(reader, opts)

	for reader.Len() > 0 {
		n, err := decode(decoder)
		if err != nil {
			return err
		}
		if n <= 0 {
			return fmt.Errorf("%w: decoder made no progress with %d byte(s) remaining", ErrMalformedWASM, reader.Len())
		}
	}
	return nil
}

// customSection splits a custom section payload into its name and contents.
func customSection(payload []byte) (string, []byte, error) {
	nameLen, n, err := readU32(payload)
	if err != nil {
		return "", nil, fmt.Errorf("%w: custom section name: %w", ErrMalformedWASM, err)
	}
	payload = payload[n:]
	if uint64(nameLen) > uint64(len(payload)) {
		return "", nil, fmt.Errorf("%w: custom section name claims %d byte(s), %d remain",
			ErrMalformedWASM, nameLen, len(payload))
	}
	return string(payload[:nameLen]), payload[nameLen:], nil
}

// isWASM reports whether code opens with the WASM magic and version 1.
func isWASM(code []byte) bool {
	return len(code) >= wasmHeaderSize &&
		string(code[:4]) == "\x00asm" &&
		binary.LittleEndian.Uint32(code[4:wasmHeaderSize]) == 1
}

// readU32 reads a WASM u32: an unsigned LEB128 integer occupying at most five
// bytes. It returns the value and the number of bytes consumed.
func readU32(b []byte) (uint32, int, error) {
	v, n := binary.Uvarint(b)
	if n <= 0 {
		return 0, 0, fmt.Errorf("%w: truncated or overlong LEB128 value", ErrMalformedWASM)
	}
	if n > 5 {
		return 0, 0, fmt.Errorf("%w: u32 value spans %d bytes, want at most 5", ErrMalformedWASM, n)
	}
	if v > math.MaxUint32 {
		return 0, 0, fmt.Errorf("%w: u32 value %d out of range", ErrMalformedWASM, v)
	}
	return uint32(v), n, nil
}

func init() {
	nodeHooks[reflect.TypeOf(xdr.ContractCodeEntry{})] = decorateContractCode
	// Spec, EnvMeta, Meta and WASMExports are parsed from the Code bytes and
	// appended to the decoded tree; they are not XDR fields, so the JSON
	// encoder ignores them rather than treating them as unknown keys.
	registerDerivedNodeFields(reflect.TypeOf(xdr.ContractCodeEntry{}), "Spec", "EnvMeta", "Meta", "WASMExports")
}

// decorateContractCode attaches the parsed metadata to a decoded
// ContractCodeEntry: the Code leaf gains a size-and-validity note, and the
// interface declarations, environment metadata and contract metadata become
// child nodes so every formatter and the differ see them.
func decorateContractCode(n *Node, v any) {
	entry, ok := v.(xdr.ContractCodeEntry)
	if !ok {
		return
	}
	parsed, err := ParseContractCode(entry.Code)

	if code := n.Child("Code"); code != nil {
		code.Note = contractCodeNote(entry.Code, err)
	}
	if err != nil {
		return
	}
	if list := specListNode(parsed.Spec); list != nil {
		n.Children = append(n.Children, list)
	}
	if list := envMetaListNode(parsed.EnvMeta); list != nil {
		n.Children = append(n.Children, list)
	}
	if list := metaListNode(parsed.Meta); list != nil {
		n.Children = append(n.Children, list)
	}
	if list := wasmExportsListNode(entry.Code); list != nil {
		n.Children = append(n.Children, list)
	}
}

// wasmExportsListNode parses the WASM binary and returns a node listing every
// exported function and its signature. Memory, table and global exports are
// omitted — only function exports are relevant to Soroban callers.
func wasmExportsListNode(code []byte) *Node {
	if len(code) == 0 || !isWASM(code) {
		return nil
	}
	mod, err := ParseWASM(code)
	if err != nil {
		return nil
	}

	var exports []*Node
	for _, exp := range mod.Exports {
		if exp.Kind != ExportKindFunction {
			continue
		}
		note := exp.Signature
		if note == "" {
			note = "() -> ()"
		}
		exports = append(exports, &Node{
			Name:  exp.Name,
			Kind:  KindScalar,
			Value: note,
		})
	}
	if len(exports) == 0 {
		return nil
	}

	list := &Node{Name: "WASMExports", Kind: KindList, TypeName: "WASMExport"}
	list.Children = exports

	var hostImports []*Node
	for _, imp := range mod.Imports {
		if imp.Kind != ExportKindFunction {
			continue
		}
		if imp.Module != "v" && imp.Module != "x" {
			continue
		}
		sig := imp.Signature
		if sig == "" {
			sig = "() -> ()"
		}
		hostImports = append(hostImports, &Node{
			Name:  fmt.Sprintf("%s::%s", imp.Module, imp.Field),
			Kind:  KindScalar,
			Value: sig,
		})
	}
	if len(hostImports) > 0 {
		importsNode := &Node{Name: "HostImports", Kind: KindList, TypeName: "WASMImport"}
		importsNode.Children = hostImports
		list.Children = append(list.Children, importsNode)
	}

	return list
}

// contractCodeNote summarises the bytecode on its leaf so the raw hex string
// is not the only thing a reader sees. The parse error is folded in rather
// than returned because a node hook cannot fail the surrounding decode: a
// non-contract byte array is legal field content.
func contractCodeNote(code []byte, err error) string {
	switch {
	case len(code) == 0:
		return "empty"
	case !isWASM(code):
		return fmt.Sprintf("not a WASM module, %d byte(s)", len(code))
	case err != nil:
		return fmt.Sprintf("malformed WASM module, %d byte(s)", len(code))
	default:
		return fmt.Sprintf("WASM module, %d byte(s)", len(code))
	}
}

// specListNode builds the Spec child: one entry per declaration, named by the
// declaration kind so the tree reads as an interface rather than an opaque
// union stream.
func specListNode(entries []xdr.ScSpecEntry) *Node {
	if len(entries) == 0 {
		return nil
	}
	list := &Node{Name: "Spec", Kind: KindList, TypeName: "ScSpecEntry"}
	for _, entry := range entries {
		label := specEntryLabel(entry.Kind)
		child := nodeFor(label, entry)
		if child == nil {
			child = &Node{Name: label, Kind: KindScalar, Value: nil}
		}
		list.Children = append(list.Children, child)
	}
	return list
}

// specEntryLabel names a declaration in the lower-case form used by the
// contract interface documentation.
func specEntryLabel(kind xdr.ScSpecEntryKind) string {
	switch kind {
	case xdr.ScSpecEntryKindScSpecEntryFunctionV0:
		return "function"
	case xdr.ScSpecEntryKindScSpecEntryUdtStructV0:
		return "struct"
	case xdr.ScSpecEntryKindScSpecEntryUdtUnionV0:
		return "union"
	case xdr.ScSpecEntryKindScSpecEntryUdtEnumV0:
		return "enum"
	case xdr.ScSpecEntryKindScSpecEntryUdtErrorEnumV0:
		return "error enum"
	case xdr.ScSpecEntryKindScSpecEntryEventV0:
		return "event"
	default:
		return "entry"
	}
}

func envMetaListNode(entries []xdr.ScEnvMetaEntry) *Node {
	if len(entries) == 0 {
		return nil
	}
	list := &Node{Name: "EnvMeta", Kind: KindList, TypeName: "ScEnvMetaEntry"}
	for _, entry := range entries {
		child := nodeFor("", entry)
		if child == nil {
			child = &Node{Kind: KindScalar, Value: nil}
		}
		child.Name = "interface version"
		list.Children = append(list.Children, child)
	}
	return list
}

func metaListNode(entries []xdr.ScMetaEntry) *Node {
	if len(entries) == 0 {
		return nil
	}
	list := &Node{Name: "Meta", Kind: KindList, TypeName: "ScMetaEntry"}
	for _, entry := range entries {
		child := nodeFor("", entry)
		if child == nil {
			child = &Node{Kind: KindScalar, Value: nil}
		}
		child.Name = "meta"
		list.Children = append(list.Children, child)
	}
	return list
}
