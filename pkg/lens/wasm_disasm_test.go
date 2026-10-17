package lens

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"
)

// buildSampleWASM generates a valid WebAssembly module with imports, types, functions, exports, and bytecode.
func buildSampleWASM() []byte {
	var buf bytes.Buffer

	// 1. Magic and Version
	buf.Write([]byte{0x00, 0x61, 0x73, 0x6D, 0x01, 0x00, 0x00, 0x00})

	// 2. Custom Section ("contractspecv0")
	var customSec bytes.Buffer
	name := "contractspecv0"
	customSec.WriteByte(byte(len(name)))
	customSec.WriteString(name)
	customSec.Write([]byte{0xDE, 0xAD, 0xBE, 0xEF})
	buf.WriteByte(0) // sec id
	buf.WriteByte(byte(customSec.Len()))
	buf.Write(customSec.Bytes())

	// 3. Type Section:
	// Type 0: (i32, i32) -> (i32)
	// Type 1: () -> ()
	var typeSec bytes.Buffer
	typeSec.WriteByte(2) // 2 types
	// Type 0
	typeSec.WriteByte(0x60)
	typeSec.WriteByte(2)    // 2 params
	typeSec.WriteByte(0x7F) // i32
	typeSec.WriteByte(0x7F) // i32
	typeSec.WriteByte(1)    // 1 result
	typeSec.WriteByte(0x7F) // i32
	// Type 1
	typeSec.WriteByte(0x60)
	typeSec.WriteByte(0) // 0 params
	typeSec.WriteByte(0) // 0 results
	buf.WriteByte(1)
	buf.WriteByte(byte(typeSec.Len()))
	buf.Write(typeSec.Bytes())

	// 4. Import Section:
	// "env"."log" func type 1
	var impSec bytes.Buffer
	impSec.WriteByte(1) // 1 import
	impSec.WriteByte(3)
	impSec.WriteString("env")
	impSec.WriteByte(3)
	impSec.WriteString("log")
	impSec.WriteByte(0) // func kind
	impSec.WriteByte(1) // type index 1
	buf.WriteByte(2)
	buf.WriteByte(byte(impSec.Len()))
	buf.Write(impSec.Bytes())

	// 5. Function Section:
	// 1 defined function using type 0
	var funcSec bytes.Buffer
	funcSec.WriteByte(1)
	funcSec.WriteByte(0) // type 0
	buf.WriteByte(3)
	buf.WriteByte(byte(funcSec.Len()))
	buf.Write(funcSec.Bytes())

	// 6. Export Section:
	// Export "add" -> func 1 (since func 0 is imported)
	var expSec bytes.Buffer
	expSec.WriteByte(1) // 1 export
	expSec.WriteByte(3)
	expSec.WriteString("add")
	expSec.WriteByte(0) // func
	expSec.WriteByte(1) // func index 1
	buf.WriteByte(7)
	buf.WriteByte(byte(expSec.Len()))
	buf.Write(expSec.Bytes())

	// 7. Code Section:
	// Body for func 1
	var codeSec bytes.Buffer
	codeSec.WriteByte(1) // 1 body
	var body bytes.Buffer
	body.WriteByte(0) // 0 local declarations
	// Instructions: local.get 0, local.get 1, i32.add, end
	body.WriteByte(0x20)
	body.WriteByte(0x00) // local 0
	body.WriteByte(0x20)
	body.WriteByte(0x01) // local 1
	body.WriteByte(0x6A) // i32.add
	body.WriteByte(0x0B) // end
	codeSec.WriteByte(byte(body.Len()))
	codeSec.Write(body.Bytes())
	buf.WriteByte(10)
	buf.WriteByte(byte(codeSec.Len()))
	buf.Write(codeSec.Bytes())

	return buf.Bytes()
}

func TestWASMDisassembler(t *testing.T) {
	wasmBytes := buildSampleWASM()

	mod, err := ParseWASM(wasmBytes)
	if err != nil {
		t.Fatalf("ParseWASM failed: %v", err)
	}

	if mod.Version != 1 {
		t.Errorf("version = %d, want 1", mod.Version)
	}

	if len(mod.Exports) != 1 {
		t.Fatalf("expected 1 export, got %d", len(mod.Exports))
	}

	exp := mod.Exports[0]
	if exp.Name != "add" {
		t.Errorf("export name = %q, want 'add'", exp.Name)
	}
	expectedSig := "(i32, i32) -> (i32)"
	if exp.Signature != expectedSig {
		t.Errorf("export signature = %q, want %q", exp.Signature, expectedSig)
	}

	if len(mod.Imports) != 1 {
		t.Fatalf("expected 1 import, got %d", len(mod.Imports))
	}
	imp := mod.Imports[0]
	if imp.Module != "env" || imp.Field != "log" {
		t.Errorf("import %s::%s, want env::log", imp.Module, imp.Field)
	}

	if len(mod.CustomSections) != 1 || mod.CustomSections[0].Name != "contractspecv0" {
		t.Errorf("custom section mismatch: %v", mod.CustomSections)
	}

	if len(mod.FunctionBodies) != 1 {
		t.Fatalf("expected 1 function body, got %d", len(mod.FunctionBodies))
	}
	if len(mod.FunctionBodies[0].Instructions) == 0 {
		t.Errorf("expected instructions in function body")
	}

	// Test Inspect
	summary := mod.Inspect()
	if !strings.Contains(summary, "add") || !strings.Contains(summary, expectedSig) {
		t.Errorf("Inspect output missing expected signature: %s", summary)
	}
	if !strings.Contains(summary, "contractspecv0") {
		t.Errorf("Inspect output missing custom section: %s", summary)
	}
}

func TestWASMValTypeAndExportKind(t *testing.T) {
	types := []WASMValType{ValTypeI32, ValTypeI64, ValTypeF32, ValTypeF64, ValTypeV128, ValTypeFuncRef, ValTypeExternRef, 0x00}
	for _, vt := range types {
		if vt.String() == "" {
			t.Errorf("empty string for ValType %v", vt)
		}
	}

	kinds := []WASMExportKind{ExportKindFunction, ExportKindTable, ExportKindMemory, ExportKindGlobal, 0xFF}
	for _, k := range kinds {
		if k.String() == "" {
			t.Errorf("empty string for ExportKind %v", k)
		}
	}
}

func TestWASMExtractFromXDR(t *testing.T) {
	wasmData := buildSampleWASM()

	// 1. ContractCodeEntry
	codeEntry := &xdr.ContractCodeEntry{
		Code: wasmData,
	}
	val := &Value{
		Type: "ContractCodeEntry",
		Raw:  codeEntry,
	}

	extracted, err := ExtractWASMFromXDR(val)
	if err != nil {
		t.Fatalf("ExtractWASMFromXDR from ContractCodeEntry failed: %v", err)
	}
	if !bytes.Equal(extracted, wasmData) {
		t.Fatalf("extracted WASM mismatch")
	}

	// 2. TransactionEnvelope with UploadContractWasm
	env := &xdr.TransactionEnvelope{
		Type: xdr.EnvelopeTypeEnvelopeTypeTx,
		V1: &xdr.TransactionV1Envelope{
			Tx: xdr.Transaction{
				Operations: []xdr.Operation{
					{
						Body: xdr.OperationBody{
							Type: xdr.OperationTypeInvokeHostFunction,
							InvokeHostFunctionOp: &xdr.InvokeHostFunctionOp{
								HostFunction: xdr.HostFunction{
									Type: xdr.HostFunctionTypeHostFunctionTypeUploadContractWasm,
									Wasm: &wasmData,
								},
							},
						},
					},
				},
			},
		},
	}
	valEnv := &Value{
		Type: "TransactionEnvelope",
		Raw:  env,
	}
	extractedEnv, err := ExtractWASMFromXDR(valEnv)
	if err != nil {
		t.Fatalf("ExtractWASMFromXDR from TransactionEnvelope failed: %v", err)
	}
	if !bytes.Equal(extractedEnv, wasmData) {
		t.Fatalf("extracted envelope WASM mismatch")
	}

	// 3. Fallback search in Node
	nodeVal := &Value{
		Type: "CustomNode",
		Raw:  nil,
		Node: &Node{
			Value: wasmData,
		},
	}
	extractedNode, err := ExtractWASMFromXDR(nodeVal)
	if err != nil {
		t.Fatalf("ExtractWASMFromXDR from Node failed: %v", err)
	}
	if !bytes.Equal(extractedNode, wasmData) {
		t.Fatalf("extracted node WASM mismatch")
	}

	// 4. No WASM found
	emptyVal := &Value{
		Type: "Memo",
		Raw:  &xdr.Memo{},
	}
	_, err = ExtractWASMFromXDR(emptyVal)
	if !errors.Is(err, ErrNoWASMFound) {
		t.Errorf("expected ErrNoWASMFound, got %v", err)
	}
}

func TestWASMErrors(t *testing.T) {
	// Too short
	if _, err := ParseWASM([]byte{0x00, 0x61}); !errors.Is(err, ErrInvalidWASMMagic) {
		t.Errorf("expected ErrInvalidWASMMagic, got %v", err)
	}

	// Bad magic
	if _, err := ParseWASM([]byte{0x01, 0x02, 0x03, 0x04, 0x01, 0x00, 0x00, 0x00}); !errors.Is(err, ErrInvalidWASMMagic) {
		t.Errorf("expected ErrInvalidWASMMagic, got %v", err)
	}

	// Unsupported version
	if _, err := ParseWASM([]byte{0x00, 0x61, 0x73, 0x6D, 0x02, 0x00, 0x00, 0x00}); !errors.Is(err, ErrUnsupportedWASMVersion) {
		t.Errorf("expected ErrUnsupportedWASMVersion, got %v", err)
	}

	// Malformed section ID/size
	if _, err := ParseWASM([]byte{0x00, 0x61, 0x73, 0x6D, 0x01, 0x00, 0x00, 0x00, 0x01}); !errors.Is(err, ErrMalformedWASM) {
		t.Errorf("expected ErrMalformedWASM on truncated section, got %v", err)
	}
}

func TestWASMComplexModuleAndInstructions(t *testing.T) {
	var buf bytes.Buffer
	buf.Write([]byte{0x00, 0x61, 0x73, 0x6D, 0x01, 0x00, 0x00, 0x00})

	// Type section: (i32) -> (i32)
	var typeSec bytes.Buffer
	typeSec.WriteByte(1)    // 1 type
	typeSec.WriteByte(0x60) // func
	typeSec.WriteByte(1)    // 1 param
	typeSec.WriteByte(0x7F) // i32
	typeSec.WriteByte(1)    // 1 result
	typeSec.WriteByte(0x7F) // i32
	buf.WriteByte(1)
	buf.WriteByte(byte(typeSec.Len()))
	buf.Write(typeSec.Bytes())

	// Import section with table, memory, global
	var impSec bytes.Buffer
	impSec.WriteByte(3) // 3 imports
	// 1. table
	impSec.WriteByte(1)
	impSec.WriteString("m")
	impSec.WriteByte(1)
	impSec.WriteString("t")
	impSec.WriteByte(1)    // table kind
	impSec.WriteByte(0x70) // funcref
	impSec.WriteByte(0)    // flag 0
	impSec.WriteByte(10)   // initial 10
	// 2. memory
	impSec.WriteByte(1)
	impSec.WriteString("m")
	impSec.WriteByte(3)
	impSec.WriteString("mem")
	impSec.WriteByte(2) // memory kind
	impSec.WriteByte(1) // flag 1 (has max)
	impSec.WriteByte(1) // min 1
	impSec.WriteByte(2) // max 2
	// 3. global
	impSec.WriteByte(1)
	impSec.WriteString("m")
	impSec.WriteByte(1)
	impSec.WriteString("g")
	impSec.WriteByte(3)    // global kind
	impSec.WriteByte(0x7F) // i32
	impSec.WriteByte(0)    // const
	buf.WriteByte(2)
	buf.WriteByte(byte(impSec.Len()))
	buf.Write(impSec.Bytes())

	// Function section: 1 func of type 0
	buf.WriteByte(3)
	buf.WriteByte(2)
	buf.WriteByte(1)
	buf.WriteByte(0)

	// Export section with table, memory, global exports
	var expSec bytes.Buffer
	expSec.WriteByte(3)
	// 1. table
	expSec.WriteByte(4)
	expSec.WriteString("ex_t")
	expSec.WriteByte(1) // table
	expSec.WriteByte(0)
	// 2. memory
	expSec.WriteByte(4)
	expSec.WriteString("ex_m")
	expSec.WriteByte(2) // memory
	expSec.WriteByte(0)
	// 3. global
	expSec.WriteByte(4)
	expSec.WriteString("ex_g")
	expSec.WriteByte(3) // global
	expSec.WriteByte(0)
	buf.WriteByte(7)
	buf.WriteByte(byte(expSec.Len()))
	buf.Write(expSec.Bytes())

	// Code section with diverse instructions
	var codeSec bytes.Buffer
	codeSec.WriteByte(1)
	var body bytes.Buffer
	body.WriteByte(1)    // 1 local group
	body.WriteByte(2)    // 2 locals
	body.WriteByte(0x7E) // i64
	// Instructions: block, br, br_if, call, global.get, i32.const, i32.add, etc.
	body.Write([]byte{
		0x02, 0x40, // block empty
		0x0C, 0x00, // br 0
		0x0D, 0x00, // br_if 0
		0x10, 0x00, // call 0
		0x23, 0x00, // global.get 0
		0x24, 0x00, // global.set 0
		0x41, 0x2A, // i32.const 42
		0x6A, // i32.add
		0x0B, // end
	})
	codeSec.WriteByte(byte(body.Len()))
	codeSec.Write(body.Bytes())
	buf.WriteByte(10)
	buf.WriteByte(byte(codeSec.Len()))
	buf.Write(codeSec.Bytes())

	mod, err := ParseWASM(buf.Bytes())
	if err != nil {
		t.Fatalf("ParseWASM complex failed: %v", err)
	}

	inspect := mod.Inspect()
	if !strings.Contains(inspect, "ex_t") || !strings.Contains(inspect, "ex_m") || !strings.Contains(inspect, "ex_g") {
		t.Errorf("Inspect missing exports: %s", inspect)
	}

	// Test LEB128 overflow
	overflowReader := bytes.NewReader([]byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80})
	if _, err := readU32Leb128(overflowReader); err == nil {
		t.Errorf("readU32Leb128 expected overflow error, got nil")
	}
	overflowIReader := bytes.NewReader([]byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80})
	if _, err := readI32Leb128(overflowIReader); err == nil {
		t.Errorf("readI32Leb128 expected overflow error, got nil")
	}

	// Test opcode mnemonics
	opcodes := []byte{0x00, 0x01, 0x03, 0x04, 0x05, 0x0F, 0x1A, 0x1B, 0x21, 0x22, 0x42, 0x45, 0x46, 0x47, 0x6B, 0x6C, 0x7C, 0x7D, 0xFE}
	for _, op := range opcodes {
		if opcodeMnemonic(op) == "" {
			t.Errorf("empty mnemonic for opcode 0x%02x", op)
		}
	}

	// Test nested Node search in ExtractWASMFromXDR
	nestedVal := &Value{
		Type: "Nested",
		Node: &Node{
			Children: []*Node{
				{
					Children: []*Node{
						{
							Value: buildSampleWASM(),
						},
					},
				},
			},
		},
	}
	extractedNested, err := ExtractWASMFromXDR(nestedVal)
	if err != nil {
		t.Fatalf("ExtractWASMFromXDR nested failed: %v", err)
	}
	if len(extractedNested) == 0 {
		t.Errorf("expected non-empty extracted WASM from nested node")
	}
}
