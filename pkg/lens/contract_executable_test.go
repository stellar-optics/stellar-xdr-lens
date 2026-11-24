package lens

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/network"
	"github.com/stellar/go-stellar-sdk/xdr"
)

func TestFormatSACNote(t *testing.T) {
	t.Parallel()

	// 1. Native asset
	nativeAsset := xdr.MustNewNativeAsset()
	gotNative := FormatSACNote(nativeAsset)
	wantNative := "Built-in Stellar Asset Contract (SAC) wrapping Native XLM"
	if gotNative != wantNative {
		t.Errorf("FormatSACNote(native) = %q, want %q", gotNative, wantNative)
	}

	// 2. AlphaNum4 asset
	issuer := xdr.MustAddress("GAPB7YMUQ6MPQ23PTFDNBZP7HGGTZPI2DT5JJURUOIXNPLGHW2O3WYXZ")
	usdcAsset := xdr.MustNewCreditAsset("USDC", issuer.Address())
	gotUSDC := FormatSACNote(usdcAsset)
	wantUSDC := "Built-in Stellar Asset Contract (SAC) wrapping asset USDC:GAPB7YMUQ6MPQ23PTFDNBZP7HGGTZPI2DT5JJURUOIXNPLGHW2O3WYXZ"
	if gotUSDC != wantUSDC {
		t.Errorf("FormatSACNote(USDC) = %q, want %q", gotUSDC, wantUSDC)
	}
}

func TestFormatSACContractIDNote(t *testing.T) {
	t.Parallel()

	nativeAsset := xdr.MustNewNativeAsset()
	note, err := FormatSACContractIDNote(nativeAsset, network.PublicNetworkPassphrase)
	if err != nil {
		t.Fatalf("FormatSACContractIDNote() error = %v", err)
	}
	if !strings.HasPrefix(note, "contract address: C") {
		t.Errorf("FormatSACContractIDNote() = %q, want prefix 'contract address: C...'", note)
	}
}

func TestEnrichSACExecutable(t *testing.T) {
	t.Parallel()

	nativeAsset := xdr.MustNewNativeAsset()
	note, contractID, err := EnrichSACExecutable(nativeAsset, network.PublicNetworkPassphrase)
	if err != nil {
		t.Fatalf("EnrichSACExecutable() error = %v", err)
	}

	if !strings.HasPrefix(contractID, "C") {
		t.Errorf("contractID = %q, want starting with 'C'", contractID)
	}
	if !strings.Contains(note, "Built-in Stellar Asset Contract (SAC) wrapping Native XLM") {
		t.Errorf("note = %q does not contain expected SAC description", note)
	}
	if !strings.Contains(note, contractID) {
		t.Errorf("note = %q does not contain contract address %s", note, contractID)
	}
}

func TestEnrichContractExecutableWithAsset(t *testing.T) {
	t.Parallel()

	sacExec := xdr.ContractExecutable{
		Type: xdr.ContractExecutableTypeContractExecutableStellarAsset,
	}
	nativeAsset := xdr.MustNewNativeAsset()
	note, contractID, ok := EnrichContractExecutableWithAsset(sacExec, nativeAsset, network.PublicNetworkPassphrase)
	if !ok {
		t.Fatal("EnrichContractExecutableWithAsset returned ok = false")
	}
	if !strings.Contains(note, "Built-in Stellar Asset Contract (SAC) wrapping Native XLM") {
		t.Errorf("unexpected note: %s", note)
	}
	if !strings.HasPrefix(contractID, "C") {
		t.Errorf("unexpected contractID: %s", contractID)
	}

	wasmHash := xdr.Hash{0x1}
	wasmExec := xdr.ContractExecutable{
		Type:     xdr.ContractExecutableTypeContractExecutableWasm,
		WasmHash: &wasmHash,
	}
	if _, _, ok := EnrichContractExecutableWithAsset(wasmExec, nativeAsset, network.PublicNetworkPassphrase); ok {
		t.Errorf("EnrichContractExecutableWithAsset on WASM should return ok = false")
	}
}

func TestCreateContractArgsDecoratesSAC(t *testing.T) {
	t.Parallel()

	issuer := xdr.MustAddress("GAPB7YMUQ6MPQ23PTFDNBZP7HGGTZPI2DT5JJURUOIXNPLGHW2O3WYXZ")
	usdcAsset := xdr.MustNewCreditAsset("USDC", issuer.Address())

	args := xdr.CreateContractArgs{
		ContractIdPreimage: xdr.ContractIdPreimage{
			Type:      xdr.ContractIdPreimageTypeContractIdPreimageFromAsset,
			FromAsset: &usdcAsset,
		},
		Executable: xdr.ContractExecutable{
			Type: xdr.ContractExecutableTypeContractExecutableStellarAsset,
		},
	}

	expectedContractID, err := SACContractID(usdcAsset, network.PublicNetworkPassphrase)
	if err != nil {
		t.Fatalf("SACContractID error = %v", err)
	}

	node := buildNode("CreateContractArgs", reflect.ValueOf(args), 0)
	if node == nil {
		t.Fatal("buildNode(CreateContractArgs) returned nil")
	}

	execNode := node.Child("Executable")
	if execNode == nil {
		t.Fatal("Executable node not found")
	}
	if !strings.Contains(execNode.Note, "Built-in Stellar Asset Contract (SAC) wrapping asset USDC:") {
		t.Errorf("Executable.Note = %q, want SAC wrapping asset", execNode.Note)
	}
	if !strings.Contains(execNode.Note, expectedContractID) {
		t.Errorf("Executable.Note = %q, want to contain contract ID %s", execNode.Note, expectedContractID)
	}

	piNode := node.Child("ContractIdPreimage")
	if piNode == nil {
		t.Fatal("ContractIdPreimage node not found")
	}
	if !strings.Contains(piNode.Note, expectedContractID) {
		t.Errorf("ContractIdPreimage.Note = %q, want contract ID %s", piNode.Note, expectedContractID)
	}

	idNode := node.Child("ContractID")
	if idNode == nil {
		t.Fatal("ContractID child node not attached")
	}
	if idNode.Value != expectedContractID {
		t.Errorf("ContractID.Value = %v, want %s", idNode.Value, expectedContractID)
	}
}

func TestCreateContractArgsV2DecoratesSAC(t *testing.T) {
	t.Parallel()

	nativeAsset := xdr.MustNewNativeAsset()
	args := xdr.CreateContractArgsV2{
		ContractIdPreimage: xdr.ContractIdPreimage{
			Type:      xdr.ContractIdPreimageTypeContractIdPreimageFromAsset,
			FromAsset: &nativeAsset,
		},
		Executable: xdr.ContractExecutable{
			Type: xdr.ContractExecutableTypeContractExecutableStellarAsset,
		},
	}

	node := buildNode("CreateContractArgsV2", reflect.ValueOf(args), 0)
	if node == nil {
		t.Fatal("buildNode(CreateContractArgsV2) returned nil")
	}

	execNode := node.Child("Executable")
	if execNode == nil {
		t.Fatal("Executable node not found")
	}
	if !strings.Contains(execNode.Note, "Built-in Stellar Asset Contract (SAC) wrapping Native XLM") {
		t.Errorf("Executable.Note = %q, want Native XLM SAC note", execNode.Note)
	}

	expectedID, _ := SACContractID(nativeAsset, network.PublicNetworkPassphrase)
	idNode := node.Child("ContractID")
	if idNode == nil {
		t.Fatal("ContractID child node not attached")
	}
	if idNode.Value != expectedID {
		t.Errorf("ContractID.Value = %v, want %s", idNode.Value, expectedID)
	}
}

func TestStandaloneContractIdPreimageDecoratesSAC(t *testing.T) {
	t.Parallel()

	nativeAsset := xdr.MustNewNativeAsset()
	preimage := xdr.ContractIdPreimage{
		Type:      xdr.ContractIdPreimageTypeContractIdPreimageFromAsset,
		FromAsset: &nativeAsset,
	}

	node := buildNode("ContractIdPreimage", reflect.ValueOf(preimage), 0)
	if node == nil {
		t.Fatal("buildNode(preimage) returned nil")
	}

	if !strings.Contains(node.Note, "Built-in Stellar Asset Contract (SAC) wrapping Native XLM") {
		t.Errorf("Preimage.Note = %q, want Native XLM SAC note", node.Note)
	}
	idNode := node.Child("ContractID")
	if idNode == nil {
		t.Fatal("ContractID child not attached to preimage node")
	}
}

func TestContractExecutableWasmAndExternalRef(t *testing.T) {
	t.Parallel()

	// 1. WASM
	hash := xdr.Hash{0xaa, 0xbb, 0xcc}
	wasmExec := xdr.ContractExecutable{
		Type:     xdr.ContractExecutableTypeContractExecutableWasm,
		WasmHash: &hash,
	}
	wasmNode := buildNode("Executable", reflect.ValueOf(wasmExec), 0)
	if wasmNode == nil {
		t.Fatal("buildNode(wasmExec) returned nil")
	}
	if wasmNode.Value != hash.HexString() {
		t.Errorf("WASM value = %v, want %s", wasmNode.Value, hash.HexString())
	}

	// 2. SAC standalone
	sacExec := xdr.ContractExecutable{
		Type: xdr.ContractExecutableTypeContractExecutableStellarAsset,
	}
	sacNode := buildNode("Executable", reflect.ValueOf(sacExec), 0)
	if sacNode == nil {
		t.Fatal("buildNode(sacExec) returned nil")
	}
	if sacNode.Value != "stellar_asset" {
		t.Errorf("SAC value = %v, want 'stellar_asset'", sacNode.Value)
	}
	if sacNode.Note != "stellar asset contract (built-in token)" {
		t.Errorf("SAC note = %v, want 'stellar asset contract (built-in token)'", sacNode.Note)
	}
}
