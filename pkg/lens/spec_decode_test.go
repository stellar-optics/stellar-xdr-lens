package lens_test

import (
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

func writeVarUint32(buf *bytes.Buffer, val uint32) {
	for {
		b := byte(val & 0x7f)
		val >>= 7
		if val != 0 {
			b |= 0x80
		}
		buf.WriteByte(b)
		if val == 0 {
			break
		}
	}
}

// Helper to construct a mock WASM binary with a contractspecv0 custom section
func buildMockWasm(specBytes []byte) []byte {
	var buf bytes.Buffer
	// Magic header: \0asm
	buf.Write([]byte{0x00, 0x61, 0x73, 0x6d})
	// Version: 1
	buf.Write([]byte{0x01, 0x00, 0x00, 0x00})

	// Custom section (ID = 0)
	buf.WriteByte(0x00)

	name := "contractspecv0"
	var payloadBuf bytes.Buffer
	writeVarUint32(&payloadBuf, uint32(len(name)))
	payloadBuf.WriteString(name)
	payloadBuf.Write(specBytes)

	// Section length
	writeVarUint32(&buf, uint32(payloadBuf.Len()))
	buf.Write(payloadBuf.Bytes())

	return buf.Bytes()
}

func buildSampleSpecEntries() ([]xdr.ScSpecEntry, []byte) {
	symPail := xdr.ScSpecTypeDef{Type: xdr.ScSpecTypeScSpecTypeSymbol}
	u32Type := xdr.ScSpecTypeDef{Type: xdr.ScSpecTypeScSpecTypeU32}
	addrType := xdr.ScSpecTypeDef{Type: xdr.ScSpecTypeScSpecTypeAddress}

	entries := []xdr.ScSpecEntry{
		{
			Kind: xdr.ScSpecEntryKindScSpecEntryFunctionV0,
			FunctionV0: &xdr.ScSpecFunctionV0{
				Name: "swap",
				Inputs: []xdr.ScSpecFunctionInputV0{
					{Name: "token_in", Type: addrType},
					{Name: "amount_in", Type: u32Type},
					{Name: "tag", Type: symPail},
				},
			},
		},
		{
			Kind: xdr.ScSpecEntryKindScSpecEntryUdtStructV0,
			UdtStructV0: &xdr.ScSpecUdtStructV0{
				Name: "LiquidityPair",
				Fields: []xdr.ScSpecUdtStructFieldV0{
					{Name: "token_a", Type: addrType},
					{Name: "token_b", Type: addrType},
					{Name: "reserve_a", Type: u32Type},
					{Name: "reserve_b", Type: u32Type},
				},
			},
		},
		{
			Kind: xdr.ScSpecEntryKindScSpecEntryUdtEnumV0,
			UdtEnumV0: &xdr.ScSpecUdtEnumV0{
				Name: "PoolStatus",
				Cases: []xdr.ScSpecUdtEnumCaseV0{
					{Name: "Active", Value: 0},
					{Name: "Paused", Value: 1},
					{Name: "Deprecated", Value: 2},
				},
			},
		},
		{
			Kind: xdr.ScSpecEntryKindScSpecEntryUdtUnionV0,
			UdtUnionV0: &xdr.ScSpecUdtUnionV0{
				Name: "SwapOutcome",
				Cases: []xdr.ScSpecUdtUnionCaseV0{
					{
						Kind: xdr.ScSpecUdtUnionCaseV0KindScSpecUdtUnionCaseVoidV0,
						VoidCase: &xdr.ScSpecUdtUnionCaseVoidV0{
							Name: "NoOp",
						},
					},
					{
						Kind: xdr.ScSpecUdtUnionCaseV0KindScSpecUdtUnionCaseTupleV0,
						TupleCase: &xdr.ScSpecUdtUnionCaseTupleV0{
							Name: "Executed",
							Type: []xdr.ScSpecTypeDef{u32Type},
						},
					},
				},
			},
		},
		{
			Kind: xdr.ScSpecEntryKindScSpecEntryUdtErrorEnumV0,
			UdtErrorEnumV0: &xdr.ScSpecUdtErrorEnumV0{
				Name: "PoolError",
				Cases: []xdr.ScSpecUdtErrorEnumCaseV0{
					{Name: "Underfunded", Value: 100},
				},
			},
		},
		{
			Kind: xdr.ScSpecEntryKindScSpecEntryEventV0,
			EventV0: &xdr.ScSpecEventV0{
				Name: "SwapEvent",
			},
		},
	}

	var buf bytes.Buffer
	for _, e := range entries {
		b, _ := e.MarshalBinary()
		buf.Write(b)
	}

	return entries, buf.Bytes()
}

func TestParseContractSpecFromBytesAndBase64(t *testing.T) {
	_, rawBytes := buildSampleSpecEntries()

	// Parse from raw bytes
	cs, err := lens.ParseContractSpec(rawBytes)
	if err != nil {
		t.Fatalf("ParseContractSpec() raw error = %v", err)
	}

	if _, ok := cs.Functions["swap"]; !ok {
		t.Error("expected function 'swap' in spec")
	}
	if _, ok := cs.Structs["LiquidityPair"]; !ok {
		t.Error("expected struct 'LiquidityPair' in spec")
	}
	if _, ok := cs.Enums["PoolStatus"]; !ok {
		t.Error("expected enum 'PoolStatus' in spec")
	}
	if _, ok := cs.Unions["SwapOutcome"]; !ok {
		t.Error("expected union 'SwapOutcome' in spec")
	}
	if _, ok := cs.ErrorEnums["PoolError"]; !ok {
		t.Error("expected error enum 'PoolError' in spec")
	}
	if _, ok := cs.Events["SwapEvent"]; !ok {
		t.Error("expected event 'SwapEvent' in spec")
	}

	// Parse from base64
	b64 := base64.StdEncoding.EncodeToString(rawBytes)
	csB64, err := lens.ParseContractSpec([]byte(b64))
	if err != nil {
		t.Fatalf("ParseContractSpec() base64 error = %v", err)
	}
	if len(csB64.Functions) != len(cs.Functions) {
		t.Errorf("expected matching function count from base64")
	}
}

func TestExtractWasmContractSpec(t *testing.T) {
	_, rawBytes := buildSampleSpecEntries()
	wasmData := buildMockWasm(rawBytes)

	// Extract and parse
	extracted, err := lens.ExtractWasmContractSpec(wasmData)
	if err != nil {
		t.Fatalf("ExtractWasmContractSpec error = %v", err)
	}

	if !bytes.Equal(extracted, rawBytes) {
		t.Errorf("extracted bytes do not match original spec bytes")
	}

	// Directly pass WASM data to ParseContractSpec
	cs, err := lens.ParseContractSpec(wasmData)
	if err != nil {
		t.Fatalf("ParseContractSpec(wasm) error = %v", err)
	}
	if _, ok := cs.Functions["swap"]; !ok {
		t.Error("expected function 'swap' extracted from wasm")
	}
}

func TestExtractWasmContractSpecErrors(t *testing.T) {
	// Not wasm
	_, err := lens.ExtractWasmContractSpec([]byte("not a wasm binary"))
	if !errors.Is(err, lens.ErrInvalidWasm) {
		t.Errorf("expected ErrInvalidWasm, got: %v", err)
	}

	// Wasm header but no contractspec section
	dummyWasm := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	_, err = lens.ExtractWasmContractSpec(dummyWasm)
	if !errors.Is(err, lens.ErrNoContractSpecSection) {
		t.Errorf("expected ErrNoContractSpecSection, got: %v", err)
	}

	malformedSection := append(append([]byte(nil), dummyWasm...), 0, 1, 1, 'x')
	_, err = lens.ExtractWasmContractSpec(malformedSection)
	if !errors.Is(err, lens.ErrMalformedWASM) {
		t.Errorf("expected ErrMalformedWASM, got: %v", err)
	}
}

func TestLoadContractSpecFile(t *testing.T) {
	_, rawBytes := buildSampleSpecEntries()
	wasmData := buildMockWasm(rawBytes)

	tmpFile := filepath.Join(t.TempDir(), "contract.wasm")
	if err := os.WriteFile(tmpFile, wasmData, 0600); err != nil {
		t.Fatalf("writing temp wasm file: %v", err)
	}

	cs, err := lens.LoadContractSpecFile(tmpFile)
	if err != nil {
		t.Fatalf("LoadContractSpecFile error = %v", err)
	}
	if len(cs.Functions) == 0 {
		t.Error("expected loaded functions from temp wasm file")
	}

	// Non-existent file
	if _, err := lens.LoadContractSpecFile("/non/existent/path.wasm"); err == nil {
		t.Error("expected error for non-existent file")
	}
}

func TestDecodeScValNamedStructAndEnum(t *testing.T) {
	_, rawBytes := buildSampleSpecEntries()
	cs, err := lens.ParseContractSpec(rawBytes)
	if err != nil {
		t.Fatalf("parse spec error: %v", err)
	}

	// Test Enum decoding
	u32One := xdr.Uint32(1)
	enumVal := &xdr.ScVal{
		Type: xdr.ScValTypeScvU32,
		U32:  &u32One,
	}
	enumNode, err := cs.DecodeScValNamed(enumVal, "PoolStatus")
	if err != nil {
		t.Fatalf("DecodeScValNamed(Enum) error = %v", err)
	}
	if enumNode.Value != "Paused" {
		t.Errorf("expected enum variant 'Paused', got: %v", enumNode.Value)
	}

	// Test Struct decoding with Vec
	u32Val := xdr.Uint32(500)
	scvU32 := xdr.ScVal{Type: xdr.ScValTypeScvU32, U32: &u32Val}
	vecObj := xdr.ScVec{scvU32, scvU32, scvU32, scvU32}
	pVec := &vecObj
	structVal := &xdr.ScVal{
		Type: xdr.ScValTypeScvVec,
		Vec:  &pVec,
	}

	structNode, err := cs.DecodeScValNamed(structVal, "LiquidityPair")
	if err != nil {
		t.Fatalf("DecodeScValNamed(Struct) error = %v", err)
	}
	if len(structNode.Children) != 4 {
		t.Fatalf("expected 4 struct fields, got %d", len(structNode.Children))
	}
	if structNode.Children[0].Name != "token_a" {
		t.Errorf("expected first field named 'token_a', got: %s", structNode.Children[0].Name)
	}
	if structNode.Children[2].Name != "reserve_a" {
		t.Errorf("expected third field named 'reserve_a', got: %s", structNode.Children[2].Name)
	}

	// Test Union decoding (Void case)
	symNoOp := xdr.ScSymbol("NoOp")
	unionVec := xdr.ScVec{
		{Type: xdr.ScValTypeScvSymbol, Sym: &symNoOp},
	}
	pUnionVec := &unionVec
	unionVal := &xdr.ScVal{
		Type: xdr.ScValTypeScvVec,
		Vec:  &pUnionVec,
	}
	unionNode, err := cs.DecodeScValNamed(unionVal, "SwapOutcome")
	if err != nil {
		t.Fatalf("DecodeScValNamed(Union) error = %v", err)
	}
	if unionNode.Name != "NoOp" {
		t.Errorf("expected union case NoOp, got: %s", unionNode.Name)
	}

	// Test Unknown Type
	if _, err := cs.DecodeScValNamed(structVal, "NonExistentType"); !errors.Is(err, lens.ErrTypeNotFound) {
		t.Errorf("expected ErrTypeNotFound, got: %v", err)
	}
}

func TestDecodeInvocation(t *testing.T) {
	_, rawBytes := buildSampleSpecEntries()
	cs, err := lens.ParseContractSpec(rawBytes)
	if err != nil {
		t.Fatalf("parse spec error: %v", err)
	}

	u32Amt := xdr.Uint32(1000)
	symTag := xdr.ScSymbol("Pail")
	args := []xdr.ScVal{
		{Type: xdr.ScValTypeScvU32, U32: &u32Amt},
		{Type: xdr.ScValTypeScvU32, U32: &u32Amt},
		{Type: xdr.ScValTypeScvSymbol, Sym: &symTag},
	}

	invNode, err := cs.DecodeInvocation("swap", args)
	if err != nil {
		t.Fatalf("DecodeInvocation error = %v", err)
	}

	if len(invNode.Children) != 3 {
		t.Fatalf("expected 3 input args, got %d", len(invNode.Children))
	}
	if invNode.Children[0].Name != "token_in" {
		t.Errorf("expected first arg named 'token_in', got: %s", invNode.Children[0].Name)
	}
	if invNode.Children[1].Name != "amount_in" {
		t.Errorf("expected second arg named 'amount_in', got: %s", invNode.Children[1].Name)
	}

	// Non-existent function
	if _, err := cs.DecodeInvocation("non_existent_fn", args); err == nil {
		t.Error("expected error for non-existent function")
	}
}

func TestApplyToValueWithSorobanFixture(t *testing.T) {
	_, rawBytes := buildSampleSpecEntries()
	cs, _ := lens.ParseContractSpec(rawBytes)

	fixturePath := filepath.Join("..", "..", "testdata", "tx_soroban.env.txt")
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	val, err := lens.Decode(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("decoding soroban fixture: %v", err)
	}

	// Apply spec to value
	cs.ApplyToValue(val)

	// Verify no crash and structure decorated
	if val.Node == nil {
		t.Fatal("expected non-nil node")
	}
}

func TestDecodeScValMapAndUnionTuple(t *testing.T) {
	_, rawBytes := buildSampleSpecEntries()
	cs, err := lens.ParseContractSpec(rawBytes)
	if err != nil {
		t.Fatalf("parse spec error: %v", err)
	}

	symTokenA := xdr.ScSymbol("token_a")
	symReserveA := xdr.ScSymbol("reserve_a")
	u32Val := xdr.Uint32(1234)
	strVal := xdr.ScString("GAA...")

	scMap := xdr.ScMap{
		{
			Key: xdr.ScVal{Type: xdr.ScValTypeScvSymbol, Sym: &symTokenA},
			Val: xdr.ScVal{Type: xdr.ScValTypeScvString, Str: &strVal},
		},
		{
			Key: xdr.ScVal{Type: xdr.ScValTypeScvSymbol, Sym: &symReserveA},
			Val: xdr.ScVal{Type: xdr.ScValTypeScvU32, U32: &u32Val},
		},
	}
	pMap := &scMap
	mapScVal := &xdr.ScVal{
		Type: xdr.ScValTypeScvMap,
		Map:  &pMap,
	}

	node, err := cs.DecodeScValNamed(mapScVal, "LiquidityPair")
	if err != nil {
		t.Fatalf("DecodeScValNamed(Map struct) error = %v", err)
	}
	if len(node.Children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(node.Children))
	}
	if node.Children[0].Name != "token_a" {
		t.Errorf("expected token_a, got %s", node.Children[0].Name)
	}

	// Test Union with Tuple case: "Executed"
	symExecuted := xdr.ScSymbol("Executed")
	tupleVec := xdr.ScVec{
		{Type: xdr.ScValTypeScvSymbol, Sym: &symExecuted},
		{Type: xdr.ScValTypeScvU32, U32: &u32Val},
	}
	pTupleVec := &tupleVec
	tupleVal := &xdr.ScVal{
		Type: xdr.ScValTypeScvVec,
		Vec:  &pTupleVec,
	}

	uNode, err := cs.DecodeScValNamed(tupleVal, "SwapOutcome")
	if err != nil {
		t.Fatalf("DecodeScValNamed(Tuple Union) error = %v", err)
	}
	if uNode.Name != "Executed" {
		t.Errorf("expected union name Executed, got: %s", uNode.Name)
	}
	if len(uNode.Children) != 1 {
		t.Fatalf("expected 1 tuple child, got %d", len(uNode.Children))
	}
}

func TestApplyToNodeWithInvokeContract(t *testing.T) {
	_, rawBytes := buildSampleSpecEntries()
	cs, err := lens.ParseContractSpec(rawBytes)
	if err != nil {
		t.Fatalf("parse spec error: %v", err)
	}

	invokeNode := &lens.Node{
		Name: "InvokeContract",
		Kind: lens.KindStruct,
		Children: []*lens.Node{
			{Name: "FunctionName", Kind: lens.KindScalar, Value: "swap"},
			{
				Name: "Args",
				Kind: lens.KindList,
				Children: []*lens.Node{
					{Name: "[0]", Kind: lens.KindScalar, Value: "addr1"},
					{Name: "[1]", Kind: lens.KindScalar, Value: int64(100)},
				},
			},
		},
	}

	cs.ApplyToNode(invokeNode)

	argsNode := invokeNode.Child("Args")
	if argsNode.Children[0].Name != "token_in" {
		t.Errorf("expected arg 0 renamed to 'token_in', got: %s", argsNode.Children[0].Name)
	}
	if argsNode.Children[1].Name != "amount_in" {
		t.Errorf("expected arg 1 renamed to 'amount_in', got: %s", argsNode.Children[1].Name)
	}
}

func TestParseContractSpecEdgeCases(t *testing.T) {
	if _, err := lens.ParseContractSpec(nil); err == nil {
		t.Error("expected error for nil data")
	}

	// Base64 encoded wasm
	_, rawBytes := buildSampleSpecEntries()
	wasmData := buildMockWasm(rawBytes)
	b64Wasm := base64.StdEncoding.EncodeToString(wasmData)
	cs, err := lens.ParseContractSpec([]byte(b64Wasm))
	if err != nil {
		t.Fatalf("ParseContractSpec(b64 wasm) error = %v", err)
	}
	if len(cs.Functions) == 0 {
		t.Error("expected functions parsed from b64 wasm")
	}
}

func TestDecodeGenericScValAllTypesAndUdtInput(t *testing.T) {
	_, rawBytes := buildSampleSpecEntries()
	cs, err := lens.ParseContractSpec(rawBytes)
	if err != nil {
		t.Fatalf("parse spec error: %v", err)
	}

	bTrue := true
	i32Val := xdr.Int32(-42)
	i64Val := xdr.Int64(-9999)
	u64Val := xdr.Uint64(8888)
	addr := xdr.MustAddress("GCJNHFXJEQEY54NPKU4DBMT5WOBJUHMYSFBDAQF4OMTEGJN4BQU7ZST6")
	scAddr := xdr.ScAddress{Type: xdr.ScAddressTypeScAddressTypeAccount, AccountId: &addr}

	vals := []xdr.ScVal{
		{Type: xdr.ScValTypeScvBool, B: &bTrue},
		{Type: xdr.ScValTypeScvVoid},
		{Type: xdr.ScValTypeScvI32, I32: &i32Val},
		{Type: xdr.ScValTypeScvI64, I64: &i64Val},
		{Type: xdr.ScValTypeScvU64, U64: &u64Val},
		{Type: xdr.ScValTypeScvAddress, Address: &scAddr},
	}

	for _, v := range vals {
		node, err := cs.DecodeInvocation("swap", []xdr.ScVal{v, v, v})
		if err != nil {
			t.Fatalf("decode invocation: %v", err)
		}
		if len(node.Children) != 3 {
			t.Errorf("expected 3 children")
		}
	}

	// Test enrichUdtChild via InvokeContract
	invokeNode := &lens.Node{
		Name: "InvokeContract",
		Kind: lens.KindStruct,
		Children: []*lens.Node{
			{Name: "FunctionName", Kind: lens.KindScalar, Value: "swap"},
			{
				Name: "Args",
				Kind: lens.KindList,
				Children: []*lens.Node{
					{
						Name: "[0]",
						Kind: lens.KindStruct,
						Children: []*lens.Node{
							{Name: "[0]", Kind: lens.KindScalar, Value: "a"},
							{Name: "[1]", Kind: lens.KindScalar, Value: "b"},
						},
					},
				},
			},
		},
	}
	// Add a function with UDT input
	cs.Functions["udt_fn"] = xdr.ScSpecFunctionV0{
		Name: "udt_fn",
		Inputs: []xdr.ScSpecFunctionInputV0{
			{
				Name: "pair",
				Type: xdr.ScSpecTypeDef{
					Type: xdr.ScSpecTypeScSpecTypeUdt,
					Udt:  &xdr.ScSpecTypeUdt{Name: "LiquidityPair"},
				},
			},
		},
	}
	invokeNode.Children[0].Value = "udt_fn"
	cs.ApplyToNode(invokeNode)

	argsNode := invokeNode.Child("Args")
	if argsNode.Children[0].Children[0].Name != "token_a" {
		t.Errorf("expected field renamed to token_a, got %s", argsNode.Children[0].Children[0].Name)
	}
}

func TestSpecTypeNameAndToUint32(t *testing.T) {
	_, rawBytes := buildSampleSpecEntries()
	cs, _ := lens.ParseContractSpec(rawBytes)

	// Call ApplyToValue on nil
	cs.ApplyToValue(nil)

	types := []xdr.ScSpecType{
		xdr.ScSpecTypeScSpecTypeU64,
		xdr.ScSpecTypeScSpecTypeI64,
		xdr.ScSpecTypeScSpecTypeBool,
		xdr.ScSpecTypeScSpecTypeString,
		xdr.ScSpecTypeScSpecTypeVec,
		xdr.ScSpecTypeScSpecTypeMap,
		xdr.ScSpecTypeScSpecTypeVal,
	}
	for _, typ := range types {
		scVal := xdr.ScVal{Type: xdr.ScValTypeScvVoid}
		node := cs.DecodeTypeDef(&scVal, xdr.ScSpecTypeDef{Type: typ})
		if node.Note == "" {
			t.Errorf("expected non-empty type name for %v", typ)
		}
	}
}

// fixtureContractSpec returns the spec entries embedded in the
// testdata/contract_code.txt module, read through the real decode path.
func fixtureContractSpec(t *testing.T) []xdr.ScSpecEntry {
	t.Helper()
	v, err := lens.DecodeAs(loadFixture(t, "contract_code"), "ContractCodeEntry")
	if err != nil {
		t.Fatalf("DecodeAs(contract_code) error = %v", err)
	}
	entry, ok := v.Raw.(*xdr.ContractCodeEntry)
	if !ok {
		t.Fatalf("Raw = %T, want *xdr.ContractCodeEntry", v.Raw)
	}
	parsed, err := lens.ParseContractCode(entry.Code)
	if err != nil {
		t.Fatalf("ParseContractCode() error = %v", err)
	}
	return parsed.Spec
}

func TestFormatContractSpecFromFixture(t *testing.T) {
	t.Parallel()

	got := lens.FormatContractSpec(fixtureContractSpec(t))
	want := `/// Adds one to the counter
/// caller: account paying for the call
fn increment(caller: Address) -> u32

/// A 2-D point
struct Point {
    x: u32,
    y: u32,
}

/// Maybe a counter
union MaybeCounter {
    None,
    Some(u32),
}

/// Traffic light states
enum Color {
    Red = 0,
    Green = 1,
}

/// Errors the contract can raise
error Error {
    NotInitialized = 1,
}

/// Emitted on every increment
event increment {
    topic "counter",
    data value: u32,
}
`
	if got != want {
		t.Errorf("FormatContractSpec() mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestContractCodeDecodeListsInterface(t *testing.T) {
	t.Parallel()

	v, err := lens.DecodeAs(loadFixture(t, "contract_code"), "ContractCodeEntry")
	if err != nil {
		t.Fatalf("DecodeAs(contract_code) error = %v", err)
	}
	iface := v.Node.Child("Interface")
	if iface == nil {
		t.Fatal("Interface node is missing")
	}
	want := []string{
		"fn increment(caller: Address) -> u32",
		"struct Point { x: u32, y: u32 }",
		"union MaybeCounter { None, Some(u32) }",
		"enum Color { Red = 0, Green = 1 }",
		"error Error { NotInitialized = 1 }",
		`event increment { topic "counter", data value: u32 }`,
	}
	if len(iface.Children) != len(want) {
		t.Fatalf("Interface has %d entries, want %d", len(iface.Children), len(want))
	}
	for i, w := range want {
		if got := iface.Children[i].Value; got != w {
			t.Errorf("Interface[%d] = %q, want %q", i, got, w)
		}
	}
}

func TestSpecTypeSignature(t *testing.T) {
	t.Parallel()

	u32 := xdr.ScSpecTypeDef{Type: xdr.ScSpecTypeScSpecTypeU32}
	addr := xdr.ScSpecTypeDef{Type: xdr.ScSpecTypeScSpecTypeAddress}
	sym := xdr.ScSpecTypeDef{Type: xdr.ScSpecTypeScSpecTypeSymbol}
	point := xdr.ScSpecTypeDef{Type: xdr.ScSpecTypeScSpecTypeUdt, Udt: &xdr.ScSpecTypeUdt{Name: "Point"}}

	tests := []struct {
		name string
		td   xdr.ScSpecTypeDef
		want string
	}{
		{name: "primitive", td: u32, want: "u32"},
		{name: "address", td: addr, want: "Address"},
		{name: "void", td: xdr.ScSpecTypeDef{Type: xdr.ScSpecTypeScSpecTypeVoid}, want: "()"},
		{name: "udt", td: point, want: "Point"},
		{name: "option", td: xdr.ScSpecTypeDef{Type: xdr.ScSpecTypeScSpecTypeOption, Option: &xdr.ScSpecTypeOption{ValueType: point}}, want: "Option<Point>"},
		{name: "result", td: xdr.ScSpecTypeDef{Type: xdr.ScSpecTypeScSpecTypeResult, Result: &xdr.ScSpecTypeResult{OkType: u32, ErrorType: xdr.ScSpecTypeDef{Type: xdr.ScSpecTypeScSpecTypeError}}}, want: "Result<u32, Error>"},
		{name: "vec", td: xdr.ScSpecTypeDef{Type: xdr.ScSpecTypeScSpecTypeVec, Vec: &xdr.ScSpecTypeVec{ElementType: sym}}, want: "Vec<Symbol>"},
		{name: "map", td: xdr.ScSpecTypeDef{Type: xdr.ScSpecTypeScSpecTypeMap, Map: &xdr.ScSpecTypeMap{KeyType: addr, ValueType: u32}}, want: "Map<Address, u32>"},
		{name: "tuple", td: xdr.ScSpecTypeDef{Type: xdr.ScSpecTypeScSpecTypeTuple, Tuple: &xdr.ScSpecTypeTuple{ValueTypes: []xdr.ScSpecTypeDef{u32, sym}}}, want: "(u32, Symbol)"},
		{name: "bytes_n", td: xdr.ScSpecTypeDef{Type: xdr.ScSpecTypeScSpecTypeBytesN, BytesN: &xdr.ScSpecTypeBytesN{N: 32}}, want: "BytesN<32>"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := lens.SpecTypeSignature(tc.td); got != tc.want {
				t.Errorf("SpecTypeSignature() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFormatSpecEntrySignatureFunctionOutputs(t *testing.T) {
	t.Parallel()

	u32 := xdr.ScSpecTypeDef{Type: xdr.ScSpecTypeScSpecTypeU32}
	tests := []struct {
		name    string
		outputs []xdr.ScSpecTypeDef
		want    string
	}{
		{name: "no output", outputs: nil, want: "fn reset(n: u32)"},
		{name: "one output", outputs: []xdr.ScSpecTypeDef{u32}, want: "fn reset(n: u32) -> u32"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			entry := xdr.ScSpecEntry{
				Kind: xdr.ScSpecEntryKindScSpecEntryFunctionV0,
				FunctionV0: &xdr.ScSpecFunctionV0{
					Name:    "reset",
					Inputs:  []xdr.ScSpecFunctionInputV0{{Name: "n", Type: u32}},
					Outputs: tc.outputs,
				},
			}
			if got := lens.FormatSpecEntrySignature(entry); got != tc.want {
				t.Errorf("FormatSpecEntrySignature() = %q, want %q", got, tc.want)
			}
		})
	}
}
