package lens

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/stellar/go-stellar-sdk/xdr"
)

var (
	// ErrInvalidWASMMagic is returned when the binary does not start with \0asm.
	ErrInvalidWASMMagic = errors.New("invalid WebAssembly magic header")
	// ErrUnsupportedWASMVersion is returned when the WASM version is not 1.
	ErrUnsupportedWASMVersion = errors.New("unsupported WebAssembly version")
	// ErrNoWASMFound is returned when no WebAssembly bytecode could be extracted from XDR.
	ErrNoWASMFound = errors.New("no WebAssembly bytecode payload found in XDR")
)

// WASM magic bytes: "\0asm"
var wasmMagic = []byte{0x00, 0x61, 0x73, 0x6D}

// WASMValType represents WebAssembly value types.
type WASMValType byte

const (
	ValTypeI32       WASMValType = 0x7F
	ValTypeI64       WASMValType = 0x7E
	ValTypeF32       WASMValType = 0x7D
	ValTypeF64       WASMValType = 0x7C
	ValTypeV128      WASMValType = 0x7B
	ValTypeFuncRef   WASMValType = 0x70
	ValTypeExternRef WASMValType = 0x6F
)

func (v WASMValType) String() string {
	switch v {
	case ValTypeI32:
		return "i32"
	case ValTypeI64:
		return "i64"
	case ValTypeF32:
		return "f32"
	case ValTypeF64:
		return "f64"
	case ValTypeV128:
		return "v128"
	case ValTypeFuncRef:
		return "funcref"
	case ValTypeExternRef:
		return "externref"
	default:
		return fmt.Sprintf("0x%02x", byte(v))
	}
}

// WASMFunctionType represents a WebAssembly function signature.
type WASMFunctionType struct {
	Params  []WASMValType
	Results []WASMValType
}

// Signature formats the function signature as (param1, param2) -> (result1).
func (ft WASMFunctionType) Signature() string {
	var sb strings.Builder
	sb.WriteString("(")
	for i, p := range ft.Params {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(p.String())
	}
	sb.WriteString(") -> (")
	for i, r := range ft.Results {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(r.String())
	}
	sb.WriteString(")")
	return sb.String()
}

// WASMExportKind represents kind of export.
type WASMExportKind byte

const (
	ExportKindFunction WASMExportKind = 0x00
	ExportKindTable    WASMExportKind = 0x01
	ExportKindMemory   WASMExportKind = 0x02
	ExportKindGlobal   WASMExportKind = 0x03
)

func (k WASMExportKind) String() string {
	switch k {
	case ExportKindFunction:
		return "function"
	case ExportKindTable:
		return "table"
	case ExportKindMemory:
		return "memory"
	case ExportKindGlobal:
		return "global"
	default:
		return fmt.Sprintf("unknown(%d)", k)
	}
}

// WASMExport represents an export in the WASM module.
type WASMExport struct {
	Name      string
	Kind      WASMExportKind
	Index     uint32
	Signature string
	Type      *WASMFunctionType
}

// WASMImport represents an imported declaration.
type WASMImport struct {
	Module    string
	Field     string
	Kind      WASMExportKind
	TypeIndex uint32
	Signature string
}

// WASMCustomSection represents a custom section (e.g. contractspecv0).
type WASMCustomSection struct {
	Name string
	Data []byte
}

// WASMInstruction represents a disassembled bytecode instruction.
type WASMInstruction struct {
	Offset   int
	Opcode   byte
	Mnemonic string
	Operand  string
}

// WASMFunctionBody represents the disassembled body of a function.
type WASMFunctionBody struct {
	Index        uint32
	Name         string
	Locals       []WASMValType
	Instructions []WASMInstruction
}

// WASMModule represents a parsed WebAssembly binary module.
type WASMModule struct {
	Version            uint32
	Types              []WASMFunctionType
	Imports            []WASMImport
	ImportedFuncCount  int
	Functions          []uint32 // type index for each defined function
	Exports            []WASMExport
	CustomSections     []WASMCustomSection
	FunctionBodies     []WASMFunctionBody
	ExportedSignatures map[string]string
}

// ParseWASM parses a raw WebAssembly bytecode binary.
func ParseWASM(data []byte) (*WASMModule, error) {
	if len(data) < 8 {
		return nil, fmt.Errorf("%w: input too short (%d bytes)", ErrInvalidWASMMagic, len(data))
	}

	if !bytes.Equal(data[:4], wasmMagic) {
		return nil, fmt.Errorf("%w: got %x, expected %x", ErrInvalidWASMMagic, data[:4], wasmMagic)
	}

	version := binary.LittleEndian.Uint32(data[4:8])
	if version != 1 {
		return nil, fmt.Errorf("%w: version %d", ErrUnsupportedWASMVersion, version)
	}

	mod := &WASMModule{
		Version:            version,
		ExportedSignatures: make(map[string]string),
	}

	reader := bytes.NewReader(data[8:])

	for reader.Len() > 0 {
		secID, err := reader.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("%w: reading section id: %w", ErrMalformedWASM, err)
		}

		secSize, err := readU32Leb128(reader)
		if err != nil {
			return nil, fmt.Errorf("%w: reading section %d size: %w", ErrMalformedWASM, secID, err)
		}

		secData := make([]byte, secSize)
		if _, err := io.ReadFull(reader, secData); err != nil {
			return nil, fmt.Errorf("%w: reading section %d payload: %w", ErrMalformedWASM, secID, err)
		}

		if err := parseSection(mod, secID, secData); err != nil {
			return nil, err
		}
	}

	// Link exports with signatures
	for i := range mod.Exports {
		exp := &mod.Exports[i]
		if exp.Kind == ExportKindFunction {
			sig, ft := resolveFunctionSignature(mod, exp.Index)
			exp.Signature = sig
			exp.Type = ft
			mod.ExportedSignatures[exp.Name] = sig
		}
	}

	return mod, nil
}

func parseSection(mod *WASMModule, secID byte, data []byte) error {
	r := bytes.NewReader(data)

	switch secID {
	case 0: // Custom Section
		name, err := readName(r)
		if err != nil {
			return fmt.Errorf("%w: custom section name: %w", ErrMalformedWASM, err)
		}
		rem := make([]byte, r.Len())
		if _, err := io.ReadFull(r, rem); err != nil {
			return fmt.Errorf("%w: reading custom section payload: %w", ErrMalformedWASM, err)
		}
		mod.CustomSections = append(mod.CustomSections, WASMCustomSection{
			Name: name,
			Data: rem,
		})

	case 1: // Type Section
		count, err := readU32Leb128(r)
		if err != nil {
			return fmt.Errorf("%w: type section count: %w", ErrMalformedWASM, err)
		}
		for i := uint32(0); i < count; i++ {
			form, err := r.ReadByte()
			if err != nil || form != 0x60 {
				return fmt.Errorf("%w: type %d invalid form 0x%02x", ErrMalformedWASM, i, form)
			}
			paramCount, err := readU32Leb128(r)
			if err != nil {
				return err
			}
			params := make([]WASMValType, paramCount)
			for p := uint32(0); p < paramCount; p++ {
				b, err := r.ReadByte()
				if err != nil {
					return err
				}
				params[p] = WASMValType(b)
			}
			resCount, err := readU32Leb128(r)
			if err != nil {
				return err
			}
			results := make([]WASMValType, resCount)
			for p := uint32(0); p < resCount; p++ {
				b, err := r.ReadByte()
				if err != nil {
					return err
				}
				results[p] = WASMValType(b)
			}
			mod.Types = append(mod.Types, WASMFunctionType{
				Params:  params,
				Results: results,
			})
		}

	case 2: // Import Section
		count, err := readU32Leb128(r)
		if err != nil {
			return fmt.Errorf("%w: import section count: %w", ErrMalformedWASM, err)
		}
		for i := uint32(0); i < count; i++ {
			moduleName, err := readName(r)
			if err != nil {
				return err
			}
			fieldName, err := readName(r)
			if err != nil {
				return err
			}
			kind, err := r.ReadByte()
			if err != nil {
				return err
			}
			imp := WASMImport{
				Module: moduleName,
				Field:  fieldName,
				Kind:   WASMExportKind(kind),
			}
			switch WASMExportKind(kind) {
			case ExportKindFunction:
				typeIdx, err := readU32Leb128(r)
				if err != nil {
					return err
				}
				imp.TypeIndex = typeIdx
				mod.ImportedFuncCount++
				if int(typeIdx) < len(mod.Types) {
					imp.Signature = mod.Types[typeIdx].Signature()
				}
			case ExportKindTable:
				if _, err := r.ReadByte(); err != nil {
					return fmt.Errorf("%w: table element type: %w", ErrMalformedWASM, err)
				}
				if err := skipLimits(r); err != nil {
					return fmt.Errorf("%w: table limits: %w", ErrMalformedWASM, err)
				}
			case ExportKindMemory:
				if err := skipLimits(r); err != nil {
					return fmt.Errorf("%w: memory limits: %w", ErrMalformedWASM, err)
				}
			case ExportKindGlobal:
				if _, err := r.ReadByte(); err != nil {
					return fmt.Errorf("%w: global content type: %w", ErrMalformedWASM, err)
				}
				if _, err := r.ReadByte(); err != nil {
					return fmt.Errorf("%w: global mutability: %w", ErrMalformedWASM, err)
				}
			}
			mod.Imports = append(mod.Imports, imp)
		}

	case 3: // Function Section
		count, err := readU32Leb128(r)
		if err != nil {
			return fmt.Errorf("%w: function section count: %w", ErrMalformedWASM, err)
		}
		for i := uint32(0); i < count; i++ {
			typeIdx, err := readU32Leb128(r)
			if err != nil {
				return err
			}
			mod.Functions = append(mod.Functions, typeIdx)
		}

	case 7: // Export Section
		count, err := readU32Leb128(r)
		if err != nil {
			return fmt.Errorf("%w: export section count: %w", ErrMalformedWASM, err)
		}
		for i := uint32(0); i < count; i++ {
			name, err := readName(r)
			if err != nil {
				return err
			}
			kind, err := r.ReadByte()
			if err != nil {
				return err
			}
			idx, err := readU32Leb128(r)
			if err != nil {
				return err
			}
			mod.Exports = append(mod.Exports, WASMExport{
				Name:  name,
				Kind:  WASMExportKind(kind),
				Index: idx,
			})
		}

	case 10: // Code Section
		count, err := readU32Leb128(r)
		if err != nil {
			return fmt.Errorf("%w: code section count: %w", ErrMalformedWASM, err)
		}
		for i := uint32(0); i < count; i++ {
			bodySize, err := readU32Leb128(r)
			if err != nil {
				return err
			}
			bodyBytes := make([]byte, bodySize)
			if _, err := io.ReadFull(r, bodyBytes); err != nil {
				return err
			}
			body, err := disassembleFunctionBody(uint32(mod.ImportedFuncCount)+i, bodyBytes)
			if err == nil {
				mod.FunctionBodies = append(mod.FunctionBodies, body)
			}
		}
	}

	return nil
}

func skipLimits(r *bytes.Reader) error {
	flags, err := r.ReadByte()
	if err != nil {
		return err
	}
	_, err = readU32Leb128(r)
	if err != nil {
		return err
	}
	if flags == 1 {
		_, err = readU32Leb128(r)
		if err != nil {
			return err
		}
	}
	return nil
}

func readName(r *bytes.Reader) (string, error) {
	lenU32, err := readU32Leb128(r)
	if err != nil {
		return "", err
	}
	buf := make([]byte, lenU32)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	return string(buf), nil
}

func readU32Leb128(r io.ByteReader) (uint32, error) {
	var result uint32
	var shift uint
	for {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		result |= uint32(b&0x7F) << shift
		if (b & 0x80) == 0 {
			break
		}
		shift += 7
		if shift >= 35 {
			return 0, errors.New("LEB128 integer overflow")
		}
	}
	return result, nil
}

func readI32Leb128(r io.ByteReader) (int32, error) {
	var result int32
	var shift uint
	var b byte
	var err error
	for {
		b, err = r.ReadByte()
		if err != nil {
			return 0, err
		}
		result |= int32(b&0x7F) << shift
		shift += 7
		if (b & 0x80) == 0 {
			break
		}
		if shift >= 35 {
			return 0, errors.New("LEB128 integer overflow")
		}
	}
	if shift < 32 && (b&0x40) != 0 {
		result |= -(1 << shift)
	}
	return result, nil
}

func resolveFunctionSignature(mod *WASMModule, funcIdx uint32) (string, *WASMFunctionType) {
	if int(funcIdx) < mod.ImportedFuncCount {
		for _, imp := range mod.Imports {
			if imp.Kind == ExportKindFunction {
				if funcIdx == 0 {
					if int(imp.TypeIndex) < len(mod.Types) {
						ft := &mod.Types[imp.TypeIndex]
						return ft.Signature(), ft
					}
					return imp.Signature, nil
				}
				funcIdx--
			}
		}
	}

	definedIdx := int(funcIdx) - mod.ImportedFuncCount
	if definedIdx >= 0 && definedIdx < len(mod.Functions) {
		typeIdx := mod.Functions[definedIdx]
		if int(typeIdx) < len(mod.Types) {
			ft := &mod.Types[typeIdx]
			return ft.Signature(), ft
		}
	}

	return "() -> ()", nil
}

func disassembleFunctionBody(funcIdx uint32, bodyBytes []byte) (WASMFunctionBody, error) {
	r := bytes.NewReader(bodyBytes)
	body := WASMFunctionBody{
		Index: funcIdx,
	}

	localCount, err := readU32Leb128(r)
	if err != nil {
		return body, err
	}

	for i := uint32(0); i < localCount; i++ {
		count, err := readU32Leb128(r)
		if err != nil {
			return body, err
		}
		vtByte, err := r.ReadByte()
		if err != nil {
			return body, err
		}
		for c := uint32(0); c < count; c++ {
			body.Locals = append(body.Locals, WASMValType(vtByte))
		}
	}

	offset := len(bodyBytes) - r.Len()
	for r.Len() > 0 {
		currOffset := offset + (len(bodyBytes) - r.Len())
		op, err := r.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return body, err
		}

		inst := WASMInstruction{
			Offset:   currOffset,
			Opcode:   op,
			Mnemonic: opcodeMnemonic(op),
		}

		// Read immediate operands for common opcodes
		switch op {
		case 0x02, 0x03, 0x04: // block, loop, if (blocktype)
			bt, err := r.ReadByte()
			if err != nil {
				return body, err
			}
			inst.Operand = fmt.Sprintf("blocktype:0x%02x", bt)
		case 0x0C, 0x0D: // br, br_if
			target, err := readU32Leb128(r)
			if err != nil {
				return body, err
			}
			inst.Operand = fmt.Sprintf("label:%d", target)
		case 0x10: // call
			target, err := readU32Leb128(r)
			if err != nil {
				return body, err
			}
			inst.Operand = fmt.Sprintf("func:%d", target)
		case 0x20, 0x21, 0x22: // local.get, local.set, local.tee
			target, err := readU32Leb128(r)
			if err != nil {
				return body, err
			}
			inst.Operand = fmt.Sprintf("local:%d", target)
		case 0x23, 0x24: // global.get, global.set
			target, err := readU32Leb128(r)
			if err != nil {
				return body, err
			}
			inst.Operand = fmt.Sprintf("global:%d", target)
		case 0x41: // i32.const
			val, err := readI32Leb128(r)
			if err != nil {
				return body, err
			}
			inst.Operand = fmt.Sprintf("%d", val)
		}

		body.Instructions = append(body.Instructions, inst)
		if op == 0x0B && r.Len() == 0 { // end of function
			break
		}
	}

	return body, nil
}

func opcodeMnemonic(op byte) string {
	switch op {
	case 0x00:
		return "unreachable"
	case 0x01:
		return "nop"
	case 0x02:
		return "block"
	case 0x03:
		return "loop"
	case 0x04:
		return "if"
	case 0x05:
		return "else"
	case 0x0B:
		return "end"
	case 0x0C:
		return "br"
	case 0x0D:
		return "br_if"
	case 0x0F:
		return "return"
	case 0x10:
		return "call"
	case 0x1A:
		return "drop"
	case 0x1B:
		return "select"
	case 0x20:
		return "local.get"
	case 0x21:
		return "local.set"
	case 0x22:
		return "local.tee"
	case 0x23:
		return "global.get"
	case 0x24:
		return "global.set"
	case 0x41:
		return "i32.const"
	case 0x42:
		return "i64.const"
	case 0x45:
		return "i32.eqz"
	case 0x46:
		return "i32.eq"
	case 0x47:
		return "i32.ne"
	case 0x6A:
		return "i32.add"
	case 0x6B:
		return "i32.sub"
	case 0x6C:
		return "i32.mul"
	case 0x7C:
		return "i64.add"
	case 0x7D:
		return "i64.sub"
	default:
		return fmt.Sprintf("0x%02x", op)
	}
}

// Inspect formats a human-readable summary of the WASM module.
func (mod *WASMModule) Inspect() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "WebAssembly Module (version %d)\n", mod.Version)
	sb.WriteString("===============================\n")
	fmt.Fprintf(&sb, "Types: %d, Imports: %d, Functions: %d, Exports: %d\n\n",
		len(mod.Types), len(mod.Imports), len(mod.Functions), len(mod.Exports))

	if len(mod.Exports) > 0 {
		sb.WriteString("Exported Functions & Signatures:\n")
		for _, exp := range mod.Exports {
			if exp.Kind == ExportKindFunction {
				fmt.Fprintf(&sb, "  • %-20s %s (index %d)\n", exp.Name, exp.Signature, exp.Index)
			} else {
				fmt.Fprintf(&sb, "  • %-20s [%s %d]\n", exp.Name, exp.Kind, exp.Index)
			}
		}
		sb.WriteString("\n")
	}

	if len(mod.Imports) > 0 {
		sb.WriteString("Imports:\n")
		for _, imp := range mod.Imports {
			fmt.Fprintf(&sb, "  • %s::%s [%s %s]\n", imp.Module, imp.Field, imp.Kind, imp.Signature)
		}
		sb.WriteString("\n")
	}

	if len(mod.CustomSections) > 0 {
		sb.WriteString("Custom Sections:\n")
		for _, cs := range mod.CustomSections {
			fmt.Fprintf(&sb, "  • %-20s (%d bytes)\n", cs.Name, len(cs.Data))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// ExtractWASMFromXDR attempts to locate and extract embedded WASM bytecode from
// a decoded XDR Value or raw struct.
func ExtractWASMFromXDR(v *Value) ([]byte, error) {
	if v == nil || (v.Raw == nil && v.Node == nil) {
		return nil, ErrNoWASMFound
	}

	// 1. Check if Value is ContractCodeEntry
	if entry, ok := v.Raw.(*xdr.ContractCodeEntry); ok {
		if len(entry.Code) >= 4 && bytes.Equal(entry.Code[:4], wasmMagic) {
			return entry.Code, nil
		}
	}

	// 2. Check if Value is TransactionEnvelope
	if env, ok := v.Raw.(*xdr.TransactionEnvelope); ok {
		var ops []xdr.Operation
		switch env.Type {
		case xdr.EnvelopeTypeEnvelopeTypeTxV0:
			ops = env.V0.Tx.Operations
		case xdr.EnvelopeTypeEnvelopeTypeTx:
			ops = env.V1.Tx.Operations
		case xdr.EnvelopeTypeEnvelopeTypeTxFeeBump:
			ops = env.FeeBump.Tx.InnerTx.V1.Tx.Operations
		}
		for _, op := range ops {
			if op.Body.Type == xdr.OperationTypeInvokeHostFunction && op.Body.InvokeHostFunctionOp != nil {
				hf := op.Body.InvokeHostFunctionOp.HostFunction
				if hf.Type == xdr.HostFunctionTypeHostFunctionTypeUploadContractWasm && hf.Wasm != nil {
					wasmBytes := *hf.Wasm
					if len(wasmBytes) >= 4 && bytes.Equal(wasmBytes[:4], wasmMagic) {
						return wasmBytes, nil
					}
				}
			}
		}
	}

	// 3. Fallback: recursively search Node for any byte slice starting with wasmMagic
	if v.Node != nil {
		if found := searchWasmInNode(v.Node); len(found) > 0 {
			return found, nil
		}
	}

	return nil, ErrNoWASMFound
}

func searchWasmInNode(n *Node) []byte {
	if n == nil {
		return nil
	}
	if b, ok := n.Value.([]byte); ok && len(b) >= 4 && bytes.Equal(b[:4], wasmMagic) {
		return b
	}
	for _, c := range n.Children {
		if found := searchWasmInNode(c); len(found) > 0 {
			return found
		}
	}
	return nil
}
