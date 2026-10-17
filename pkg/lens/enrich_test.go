package lens

import (
	"reflect"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"
)

func TestEnrichHash(t *testing.T) {
	hash := xdr.Hash{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1A, 0x1B, 0x1C, 0x1D, 0x1E, 0x1F, 0x20}

	display, raw, ok := enrichHash(reflect.ValueOf(hash))
	if !ok {
		t.Fatal("enrichHash failed")
	}

	expected := "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"
	if display != expected {
		t.Errorf("got %q, want %q", display, expected)
	}
	if raw != hash {
		t.Errorf("raw value altered")
	}
}

func TestEnrichPoolID(t *testing.T) {
	poolID := xdr.PoolId{0x0A, 0x0B, 0x0C} // Rest are zero

	display, raw, ok := enrichPoolID(reflect.ValueOf(poolID))
	if !ok {
		t.Fatal("enrichPoolID failed")
	}

	expected := "0a0b0c0000000000000000000000000000000000000000000000000000000000"
	if display != expected {
		t.Errorf("got %q, want %q", display, expected)
	}
	if raw != poolID {
		t.Errorf("raw value altered")
	}
}

func TestEnrichSignature(t *testing.T) {
	sig := xdr.Signature{0xFF, 0xEE, 0xDD}

	display, raw, ok := enrichSignature(reflect.ValueOf(sig))
	if !ok {
		t.Fatal("enrichSignature failed")
	}

	expected := "ffeedd"
	if display != expected {
		t.Errorf("got %q, want %q", display, expected)
	}
	// bytes slice equality
	rawSig := raw.(xdr.Signature)
	if len(rawSig) != len(sig) || rawSig[0] != sig[0] || rawSig[1] != sig[1] || rawSig[2] != sig[2] {
		t.Errorf("raw value altered")
	}
}

// The domain enrichers are registered against exact types, so routing one a
// different value means a bug in registration. They must decline cleanly
// rather than panic or invent a rendering.
func TestDomainEnrichersRejectWrongType(t *testing.T) {
	t.Parallel()

	wrong := reflect.ValueOf("not an xdr value")
	enrichers := map[string]Enricher{
		"enrichPoolParameters":     enrichPoolParameters,
		"enrichClaimPredicate":     enrichClaimPredicate,
		"enrichClaimableBalanceID": enrichClaimableBalanceID,
		"enrichContractID":         enrichContractID,
		"enrichScAddress":          enrichScAddress,
	}
	for name, fn := range enrichers {
		if _, _, ok := fn(wrong); ok {
			t.Errorf("%s accepted a non-XDR value", name)
		}
	}
}

func TestClaimableBalanceIDRejectsUnknownType(t *testing.T) {
	t.Parallel()

	id := xdr.ClaimableBalanceId{Type: xdr.ClaimableBalanceIdType(99)}
	if _, _, ok := enrichClaimableBalanceID(reflect.ValueOf(id)); ok {
		t.Error("enrichClaimableBalanceID accepted an unknown id type")
	}
}

func TestScAddressRejectsUnknownType(t *testing.T) {
	t.Parallel()

	addr := xdr.ScAddress{Type: xdr.ScAddressType(99)}
	if _, _, ok := enrichScAddress(reflect.ValueOf(addr)); ok {
		t.Error("enrichScAddress accepted an unknown address type")
	}
}

func TestTrustLineFlagNote(t *testing.T) {
	t.Parallel()

	if _, ok := trustLineFlagNote(reflect.ValueOf(xdr.Uint32(0))); ok {
		t.Error("trustLineFlagNote produced a note for an empty flag set")
	}
	if _, ok := trustLineFlagNote(reflect.ValueOf("x")); ok {
		t.Error("trustLineFlagNote accepted a non-Uint32 value")
	}
}

func TestClaimableBalanceFlagNote(t *testing.T) {
	t.Parallel()

	clawback := xdr.Uint32(xdr.ClaimableBalanceFlagsClaimableBalanceClawbackEnabledFlag)
	if note, ok := claimableBalanceFlagNote(reflect.ValueOf(clawback)); !ok || note != "clawback enabled" {
		t.Errorf("clawback note = %q, %v; want 'clawback enabled', true", note, ok)
	}
	if _, ok := claimableBalanceFlagNote(reflect.ValueOf(xdr.Uint32(0))); ok {
		t.Error("claimableBalanceFlagNote produced a note for an empty flag set")
	}
	if _, ok := claimableBalanceFlagNote(reflect.ValueOf("x")); ok {
		t.Error("claimableBalanceFlagNote accepted a non-Uint32 value")
	}
}

func TestEnrichContractExecutableWasm(t *testing.T) {
	t.Parallel()

	hash := xdr.Hash{0x4F}
	exec := xdr.ContractExecutable{
		Type:     xdr.ContractExecutableTypeContractExecutableWasm,
		WasmHash: &hash,
	}
	note, raw, ok := enrichContractExecutable(reflect.ValueOf(exec))
	if !ok {
		t.Fatal("enrichContractExecutable declined a wasm executable")
	}
	wantHex := hash.HexString()
	if note != "wasm:"+wantHex {
		t.Errorf("note = %q, want %q", note, "wasm:"+wantHex)
	}
	if raw != wantHex {
		t.Errorf("raw = %v, want %q", raw, wantHex)
	}
}

func TestEnrichContractExecutableStellarAsset(t *testing.T) {
	t.Parallel()

	exec := xdr.ContractExecutable{Type: xdr.ContractExecutableTypeContractExecutableStellarAsset}
	note, raw, ok := enrichContractExecutable(reflect.ValueOf(exec))
	if !ok {
		t.Fatal("enrichContractExecutable declined the stellar asset arm")
	}
	if note != "stellar asset contract (built-in token)" {
		t.Errorf("note = %q, want the built-in token description", note)
	}
	if raw != "stellar_asset" {
		t.Errorf("raw = %v, want stellar_asset", raw)
	}
}

func TestEnrichContractExecutableExternalRef(t *testing.T) {
	t.Parallel()

	owner := xdr.ScAddress{Type: xdr.ScAddressTypeScAddressTypeContract}
	var contractID xdr.ContractId
	contractID[0] = 0xCA
	owner.ContractId = &contractID

	ownerStr, err := owner.String()
	if err != nil {
		t.Fatalf("encoding owner: %v", err)
	}

	exec := xdr.ContractExecutable{
		Type: xdr.ContractExecutableTypeContractExecutableExternalRef,
		ExternalRef: &xdr.ContractExecutableExternalRef{
			ExecutableOwner: owner,
			Tag:             "evm:1",
		},
	}
	note, raw, ok := enrichContractExecutable(reflect.ValueOf(exec))
	if !ok {
		t.Fatal("enrichContractExecutable declined the external ref arm")
	}
	if note != "external contract reference" {
		t.Errorf("note = %q, want 'external contract reference'", note)
	}
	if raw != ownerStr+" tag: evm:1" {
		t.Errorf("raw = %v, want owner and tag", raw)
	}
}

func TestEnrichContractExecutableDeclinesMalformed(t *testing.T) {
	t.Parallel()

	cases := map[string]xdr.ContractExecutable{
		"unknown type":         {Type: xdr.ContractExecutableType(99)},
		"missing wasm hash":    {Type: xdr.ContractExecutableTypeContractExecutableWasm},
		"missing external ref": {Type: xdr.ContractExecutableTypeContractExecutableExternalRef},
		"unknown owner type": {
			Type: xdr.ContractExecutableTypeContractExecutableExternalRef,
			ExternalRef: &xdr.ContractExecutableExternalRef{
				ExecutableOwner: xdr.ScAddress{Type: xdr.ScAddressType(99)},
				Tag:             "x",
			},
		},
	}
	for name, exec := range cases {
		if _, _, ok := enrichContractExecutable(reflect.ValueOf(exec)); ok {
			t.Errorf("%s: enrichContractExecutable produced a rendering, want decline", name)
		}
	}
	if _, _, ok := enrichContractExecutable(reflect.ValueOf("not an executable")); ok {
		t.Error("enrichContractExecutable accepted a non-ContractExecutable value")
	}
}

func TestBuildNodeContractExecutableWasm(t *testing.T) {
	t.Parallel()

	hash := xdr.Hash{0xAB}
	instance := xdr.ScContractInstance{
		Executable: xdr.ContractExecutable{
			Type:     xdr.ContractExecutableTypeContractExecutableWasm,
			WasmHash: &hash,
		},
	}

	node := buildNode("Instance", reflect.ValueOf(instance), 0)
	if node == nil {
		t.Fatal("buildNode(ScContractInstance) returned nil")
	}
	exec := node.Child("Executable")
	if exec == nil {
		t.Fatal("Executable child not found")
	}
	if exec.Note != "wasm:"+hash.HexString() {
		t.Errorf("Executable Note = %q, want the wasm annotation", exec.Note)
	}
}
