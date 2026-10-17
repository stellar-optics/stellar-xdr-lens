package lens

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

// feeBumpChildNames lists a node's child names, scoped to the fee-bump tests
// so the derivation stands alone in this file.
func feeBumpChildNames(node *Node) []string {
	names := make([]string, 0, len(node.Children))
	for _, child := range node.Children {
		names = append(names, child.Name)
	}
	return names
}

// decodeFixtureValue reads a checked-in fixture and runs it through the same
// decode path the CLI uses.
func decodeFixtureValue(t *testing.T, name string) *Value {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}
	value, err := Decode(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("decoding fixture %s: %v", name, err)
	}
	return value
}

// decodeFeeBumpFixture loads the canonical mainnet fee-bump envelope both as
// the decoded tree and as the concrete SDK value, which is what the derived
// fee-source hint and inner hash have to be checked against.
func decodeFeeBumpFixture(t *testing.T) (*xdr.TransactionEnvelope, *Value) {
	t.Helper()
	value := decodeFixtureValue(t, "tx_feebump.env.txt")
	env, ok := value.Raw.(*xdr.TransactionEnvelope)
	if !ok {
		t.Fatalf("fixture decoded as %T, want *xdr.TransactionEnvelope", value.Raw)
	}
	return env, value
}

// expectedInnerDigest independently recomputes the inner transaction digest:
// sha256 over the four bytes of ENVELOPE_TYPE_TX followed by the inner
// Transaction's XDR, without the network id.
func expectedInnerDigest(t *testing.T, inner xdr.FeeBumpTransactionInnerTx) string {
	t.Helper()
	if inner.V1 == nil {
		t.Fatal("fixture inner union carries no v1 arm")
	}
	txBytes, err := inner.V1.Tx.MarshalBinary()
	if err != nil {
		t.Fatalf("marshalling inner transaction: %v", err)
	}
	preimage := make([]byte, 4, 4+len(txBytes))
	binary.BigEndian.PutUint32(preimage, uint32(xdr.EnvelopeTypeEnvelopeTypeTx))
	preimage = append(preimage, txBytes...)
	sum := sha256.Sum256(preimage)
	return hex.EncodeToString(sum[:])
}

func TestMuxedAccountHint(t *testing.T) {
	t.Parallel()

	var key xdr.Uint256
	for i := range key {
		key[i] = byte(i + 1)
	}
	want := xdr.SignatureHint{key[28], key[29], key[30], key[31]}

	tests := []struct {
		name    string
		account xdr.MuxedAccount
		want    xdr.SignatureHint
		ok      bool
	}{
		{
			name:    "ed25519 arm",
			account: xdr.MuxedAccount{Type: xdr.CryptoKeyTypeKeyTypeEd25519, Ed25519: &key},
			want:    want,
			ok:      true,
		},
		{
			name: "muxed ed25519 arm",
			account: xdr.MuxedAccount{
				Type:     xdr.CryptoKeyTypeKeyTypeMuxedEd25519,
				Med25519: &xdr.MuxedAccountMed25519{Id: 42, Ed25519: key},
			},
			want: want,
			ok:   true,
		},
		{name: "unset ed25519 arm", account: xdr.MuxedAccount{Type: xdr.CryptoKeyTypeKeyTypeEd25519}},
		{name: "unset muxed arm", account: xdr.MuxedAccount{Type: xdr.CryptoKeyTypeKeyTypeMuxedEd25519}},
		{name: "zero account", account: xdr.MuxedAccount{}},
		{name: "unknown key type", account: xdr.MuxedAccount{Type: xdr.CryptoKeyType(99)}},
	}
	for _, tc := range tests {
		got, ok := muxedAccountHint(tc.account)
		if ok != tc.ok {
			t.Errorf("%s: muxedAccountHint ok = %v, want %v", tc.name, ok, tc.ok)
		}
		if got != tc.want {
			t.Errorf("%s: muxedAccountHint = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestInnerTransactionDigest(t *testing.T) {
	t.Parallel()

	env, _ := decodeFeeBumpFixture(t)
	digest, ok := innerTransactionDigest(env.FeeBump.Tx.InnerTx)
	if !ok {
		t.Fatal("innerTransactionDigest declined the fixture's inner transaction")
	}
	if want := expectedInnerDigest(t, env.FeeBump.Tx.InnerTx); digest != want {
		t.Errorf("innerTransactionDigest = %q, want %q", digest, want)
	}
	if digest == "" || len(digest) != 64 {
		t.Errorf("innerTransactionDigest = %q, want 64 hex characters", digest)
	}

	noArm := xdr.FeeBumpTransactionInnerTx{Type: xdr.EnvelopeTypeEnvelopeTypeTx}
	if _, ok := innerTransactionDigest(noArm); ok {
		t.Error("innerTransactionDigest accepted a fee-bump union with no v1 arm")
	}
	wrongArm := xdr.FeeBumpTransactionInnerTx{Type: xdr.EnvelopeTypeEnvelopeTypeTxFeeBump}
	if _, ok := innerTransactionDigest(wrongArm); ok {
		t.Error("innerTransactionDigest accepted a non-tx envelope arm")
	}
}

func TestFeeBumpTransactionDerivedChildren(t *testing.T) {
	t.Parallel()

	env, value := decodeFeeBumpFixture(t)
	tx := value.Node.Find("FeeBump.Tx")
	if tx == nil {
		t.Fatal("FeeBump.Tx not found in decoded fixture")
	}

	// Declared fields first, derived children appended after: the wire shape
	// machine-readable output keeps is byte-faithful.
	wantOrder := []string{"FeeSource", "Fee", "InnerTx", "Ext", "FeeSourceHint", "InnerTxHash"}
	if got := feeBumpChildNames(tx); !reflect.DeepEqual(got, wantOrder) {
		t.Errorf("FeeBump.Tx children = %v, want %v", got, wantOrder)
	}

	feeHint, ok := muxedAccountHint(env.FeeBump.Tx.FeeSource)
	if !ok {
		t.Fatal("fee source carries no key")
	}
	hintNode := tx.Child("FeeSourceHint")
	if hintNode == nil {
		t.Fatal("FeeSourceHint missing from FeeBump.Tx")
	}
	if got, want := hintNode.Value, hex.EncodeToString(feeHint[:]); got != want {
		t.Errorf("FeeSourceHint = %v, want %q", got, want)
	}
	addr := addressOf(env.FeeBump.Tx.FeeSource)
	if want := "4-byte key hint of fee source " + shortAddr(addr); hintNode.Note != want {
		t.Errorf("FeeSourceHint note = %q, want %q", hintNode.Note, want)
	}

	hashNode := tx.Child("InnerTxHash")
	if hashNode == nil {
		t.Fatal("InnerTxHash missing from FeeBump.Tx")
	}
	if got, want := hashNode.Value, expectedInnerDigest(t, env.FeeBump.Tx.InnerTx); got != want {
		t.Errorf("InnerTxHash = %v, want %q", got, want)
	}
	if !strings.Contains(hashNode.Note, "network id") {
		t.Errorf("InnerTxHash note = %q, want it to say the network id is not prefixed", hashNode.Note)
	}
}

func TestFeeBumpEnvelopeSignatureHints(t *testing.T) {
	t.Parallel()

	env, value := decodeFeeBumpFixture(t)
	list := value.Node.Find("FeeBump.SignatureHints")
	if list == nil {
		t.Fatal("SignatureHints missing from decoded envelope")
	}
	if len(list.Children) != len(env.FeeBump.Signatures) {
		t.Fatalf("SignatureHints has %d entries, want %d", len(list.Children), len(env.FeeBump.Signatures))
	}
	feeHint, ok := muxedAccountHint(env.FeeBump.Tx.FeeSource)
	if !ok {
		t.Fatal("fee source carries no key")
	}
	feeAddr := shortAddr(addressOf(env.FeeBump.Tx.FeeSource))
	matched := false
	for i, sig := range env.FeeBump.Signatures {
		got, ok := list.Children[i].Value.(string)
		if !ok {
			t.Fatalf("SignatureHints[%d] value has type %T, want string", i, list.Children[i].Value)
		}
		if want := hex.EncodeToString(sig.Hint[:]); got != want {
			t.Errorf("SignatureHints[%d] = %q, want %q", i, got, want)
		}
		wantNote := "does not match " + feeAddr + " key hint; another fee-source signer"
		if sig.Hint == feeHint {
			wantNote = "matches " + feeAddr + " key hint"
			matched = true
		}
		if note := list.Children[i].Note; note != wantNote {
			t.Errorf("SignatureHints[%d] note = %q, want %q", i, note, wantNote)
		}
	}
	if !matched {
		t.Error("no fixture signature matches the fee source key hint; stale test input")
	}
}

func TestFeeBumpEnvelopeSignatureHintMatch(t *testing.T) {
	t.Parallel()

	var key xdr.Uint256
	key[31] = 0xAB
	feeSource := xdr.MuxedAccount{Type: xdr.CryptoKeyTypeKeyTypeEd25519, Ed25519: &key}
	feeHint, ok := muxedAccountHint(feeSource)
	if !ok {
		t.Fatal("muxedAccountHint declined an ed25519 account")
	}

	node := &Node{Name: "FeeBump", Kind: KindStruct}
	decorateFeeBumpEnvelope(node, xdr.FeeBumpTransactionEnvelope{
		Tx: xdr.FeeBumpTransaction{FeeSource: feeSource},
		Signatures: []xdr.DecoratedSignature{
			{Hint: xdr.SignatureHint{1, 2, 3, 4}},
			{Hint: feeHint},
		},
	})

	list := node.Child("SignatureHints")
	if list == nil || len(list.Children) != 2 {
		t.Fatalf("SignatureHints = %v, want exactly two entries", list)
	}
	addr := shortAddr(addressOf(feeSource))
	if got, want := list.Children[0].Note, "does not match "+addr+" key hint; another fee-source signer"; got != want {
		t.Errorf("unmatched note = %q, want %q", got, want)
	}
	if got, want := list.Children[1].Note, "matches "+addr+" key hint"; got != want {
		t.Errorf("matched note = %q, want %q", got, want)
	}
}

func TestFeeBumpEnvelopeOmitsHintsWhenNothingCanMatch(t *testing.T) {
	t.Parallel()

	node := &Node{Name: "FeeBump", Kind: KindStruct}
	decorateFeeBumpEnvelope(node, xdr.FeeBumpTransactionEnvelope{})
	if len(node.Children) != 0 {
		t.Errorf("envelope without signatures gained %d derived children", len(node.Children))
	}

	node = &Node{Name: "FeeBump", Kind: KindStruct}
	decorateFeeBumpEnvelope(node, xdr.FeeBumpTransactionEnvelope{
		Signatures: []xdr.DecoratedSignature{{Hint: xdr.SignatureHint{9, 9, 9, 9}}},
	})
	list := node.Child("SignatureHints")
	if list == nil || len(list.Children) != 1 {
		t.Fatalf("SignatureHints = %v, want one entry for a keyless fee source", list)
	}
	if got, want := list.Children[0].Note, "fee source carries no key; hint not matched"; got != want {
		t.Errorf("note = %q, want %q", got, want)
	}
}

func TestFeeBumpHooksIgnoresOtherValues(t *testing.T) {
	t.Parallel()

	node := &Node{Name: "Tx", Kind: KindStruct}
	decorateFeeBumpTransaction(node, "not a fee bump")
	decorateFeeBumpEnvelope(node, 42)
	if len(node.Children) != 0 {
		t.Errorf("fee-bump hooks accepted foreign values and added %d children", len(node.Children))
	}
}

func TestFeeBumpTransactionStandaloneDecode(t *testing.T) {
	t.Parallel()

	env, _ := decodeFeeBumpFixture(t)
	value, err := DecodeAs(TestMarshalBase64(&env.FeeBump.Tx), "FeeBumpTransaction")
	if err != nil {
		t.Fatalf("decoding standalone FeeBumpTransaction: %v", err)
	}
	if value.Node.Child("FeeSourceHint") == nil || value.Node.Child("InnerTxHash") == nil {
		t.Errorf("standalone decode children = %v, want FeeSourceHint and InnerTxHash", feeBumpChildNames(value.Node))
	}
	if value.Node.Child("SignatureHints") != nil {
		t.Error("standalone FeeBumpTransaction gained envelope-level SignatureHints")
	}
}

func TestNonFeeBumpEnvelopeGainsNoDerivedNodes(t *testing.T) {
	t.Parallel()

	value := decodeFixtureValue(t, "tx_success.env.txt")
	for _, name := range []string{"FeeSourceHint", "InnerTxHash", "SignatureHints"} {
		if value.Node.Find(name) != nil {
			t.Errorf("non fee-bump envelope carries derived node %q", name)
		}
	}
}
