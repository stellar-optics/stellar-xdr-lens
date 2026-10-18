package lens_test

import (
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

const (
	signerTestAddrA = "GAPB7YMUQ6MPQ23PTFDNBZP7HGGTZPI2DT5JJURUOIXNPLGHW2O3WYXZ"
	signerTestAddrB = "GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAW"
)

func mustSignerKey(t *testing.T, address string) xdr.SignerKey {
	t.Helper()
	var key xdr.SignerKey
	if err := key.SetAddress(address); err != nil {
		t.Fatalf("SetAddress(%s): %v", address, err)
	}
	return key
}

func decoratedSignature(hint byte, payload byte) xdr.DecoratedSignature {
	return xdr.DecoratedSignature{
		Hint:      xdr.SignatureHint{hint, hint + 1, hint + 2, hint + 3},
		Signature: []byte{payload, payload + 1, payload + 2},
	}
}

func TestDiffDecoratedSignaturesIsOrderInsensitive(t *testing.T) {
	t.Parallel()

	sigA := decoratedSignature(1, 10)
	sigB := decoratedSignature(2, 20)

	diff, err := lens.DiffDecoratedSignatures(
		[]xdr.DecoratedSignature{sigA, sigB},
		[]xdr.DecoratedSignature{sigB, sigA},
	)
	if err != nil {
		t.Fatalf("DiffDecoratedSignatures() error = %v", err)
	}
	if !diff.Equal() {
		t.Errorf("reordered signatures reported a difference: %+v", diff)
	}
	if len(diff.Common) != 2 {
		t.Errorf("Common = %v, want both signatures", diff.Common)
	}
	if got := diff.Summary(); got != "no signer differences" {
		t.Errorf("Summary() = %q", got)
	}
}

func TestDiffDecoratedSignaturesAddedAndRemoved(t *testing.T) {
	t.Parallel()

	sigA := decoratedSignature(1, 10)
	sigB := decoratedSignature(2, 20)
	sigC := decoratedSignature(3, 30)

	diff, err := lens.DiffDecoratedSignatures(
		[]xdr.DecoratedSignature{sigA, sigB},
		[]xdr.DecoratedSignature{sigB, sigC},
	)
	if err != nil {
		t.Fatalf("DiffDecoratedSignatures() error = %v", err)
	}
	if diff.Equal() {
		t.Fatal("Equal = true, want false")
	}
	if len(diff.Removed) != 1 || len(diff.Added) != 1 {
		t.Fatalf("diff = %+v, want one removed and one added", diff)
	}
	if diff.Removed[0] == diff.Added[0] {
		t.Errorf("removed and added are identical: %q", diff.Removed[0])
	}
	if len(diff.Common) != 1 {
		t.Errorf("Common = %v, want the shared signature", diff.Common)
	}
	if got := diff.Summary(); got != "1 removed, 1 added" {
		t.Errorf("Summary() = %q", got)
	}
}

func TestDiffDecoratedSignaturesCountsDuplicates(t *testing.T) {
	t.Parallel()

	sigA := decoratedSignature(1, 10)

	diff, err := lens.DiffDecoratedSignatures(
		[]xdr.DecoratedSignature{sigA, sigA},
		[]xdr.DecoratedSignature{sigA},
	)
	if err != nil {
		t.Fatalf("DiffDecoratedSignatures() error = %v", err)
	}
	if len(diff.Removed) != 1 || len(diff.Added) != 0 {
		t.Errorf("diff = %+v, want exactly one surplus removal", diff)
	}

	reverse, err := lens.DiffDecoratedSignatures(
		[]xdr.DecoratedSignature{sigA},
		[]xdr.DecoratedSignature{sigA, sigA},
	)
	if err != nil {
		t.Fatalf("DiffDecoratedSignatures() error = %v", err)
	}
	if len(reverse.Added) != 1 || len(reverse.Removed) != 0 {
		t.Errorf("reverse diff = %+v, want exactly one surplus addition", reverse)
	}
}

func TestDiffSignersWeightIsIdentity(t *testing.T) {
	t.Parallel()

	key := mustSignerKey(t, signerTestAddrA)

	diff, err := lens.DiffSigners(
		[]xdr.Signer{{Key: key, Weight: 1}},
		[]xdr.Signer{{Key: key, Weight: 2}},
	)
	if err != nil {
		t.Fatalf("DiffSigners() error = %v", err)
	}
	if diff.Equal() {
		t.Fatal("Equal = true, want a weight change to be reported")
	}
	if len(diff.Removed) != 1 || len(diff.Added) != 1 {
		t.Errorf("diff = %+v, want one removed and one added", diff)
	}
}

func TestDiffSignerKeysUnordered(t *testing.T) {
	t.Parallel()

	keyA := mustSignerKey(t, signerTestAddrA)
	keyB := mustSignerKey(t, signerTestAddrB)

	diff, err := lens.DiffSignerKeys([]xdr.SignerKey{keyA, keyB}, []xdr.SignerKey{keyB, keyA})
	if err != nil {
		t.Fatalf("DiffSignerKeys() error = %v", err)
	}
	if !diff.Equal() {
		t.Errorf("reordered signer keys reported a difference: %+v", diff)
	}
	if len(diff.Common) != 2 {
		t.Errorf("Common = %v, want both keys", diff.Common)
	}
}

func TestDiffOptionsUnorderedSignersOnEnvelope(t *testing.T) {
	t.Parallel()

	sigA := decoratedSignature(1, 10)
	sigB := decoratedSignature(2, 20)

	left := buildSignedEnvelope(t, []xdr.DecoratedSignature{sigA, sigB})
	right := buildSignedEnvelope(t, []xdr.DecoratedSignature{sigB, sigA})

	positional, err := lens.DiffBase64(left, right, "TransactionEnvelope")
	if err != nil {
		t.Fatalf("DiffBase64() error = %v", err)
	}
	if positional.Equal() {
		t.Fatal("positional diff treated reordered signatures as equal")
	}

	unordered, err := lens.DiffBase64WithOptions(left, right, "TransactionEnvelope",
		lens.DiffOptions{UnorderedSigners: true})
	if err != nil {
		t.Fatalf("DiffBase64WithOptions() error = %v", err)
	}
	if !unordered.Equal() {
		t.Errorf("unordered diff reported %d change(s): %+v", len(unordered.Changes), unordered.Changes)
	}
}

func TestDiffOptionsUnorderedSignersReportsAddedSignature(t *testing.T) {
	t.Parallel()

	left := buildSignedEnvelope(t, []xdr.DecoratedSignature{decoratedSignature(1, 10)})
	right := buildSignedEnvelope(t, []xdr.DecoratedSignature{decoratedSignature(1, 10), decoratedSignature(2, 20)})

	diff, err := lens.DiffBase64WithOptions(left, right, "TransactionEnvelope",
		lens.DiffOptions{UnorderedSigners: true})
	if err != nil {
		t.Fatalf("DiffBase64WithOptions() error = %v", err)
	}
	if len(diff.Changes) != 1 || diff.Changes[0].Op != lens.ChangeAdded {
		t.Fatalf("diff = %+v, want one addition", diff.Changes)
	}
	label, ok := diff.Changes[0].After.(string)
	if !ok || !strings.Contains(label, "Hint=") || !strings.Contains(label, "Signature=") {
		t.Errorf("added signature label = %v, want rendered Hint/Signature fields", diff.Changes[0].After)
	}
	if !strings.Contains(diff.Changes[0].Path, "Signatures") {
		t.Errorf("path = %q, want the signature list path", diff.Changes[0].Path)
	}
}

func TestDiffOptionsUnorderedSignersLeavesOtherListsPositional(t *testing.T) {
	t.Parallel()

	value := func(field string, items ...string) *lens.Value {
		children := make([]*lens.Node, 0, len(items))
		for _, item := range items {
			children = append(children, &lens.Node{Kind: lens.KindScalar, Value: item})
		}
		list := &lens.Node{Name: field, Kind: lens.KindList, Children: children}
		return &lens.Value{Type: "X", Node: &lens.Node{Kind: lens.KindStruct, Children: []*lens.Node{list}}}
	}

	options := lens.DiffOptions{UnorderedSigners: true}

	signers, err := lens.DiffWithOptions(value("Signatures", "a", "b"), value("Signatures", "c", "a", "b"), options)
	if err != nil {
		t.Fatalf("DiffWithOptions() error = %v", err)
	}
	if len(signers.Changes) != 1 || signers.Changes[0].Op != lens.ChangeAdded {
		t.Errorf("signature list diff = %+v, want one addition", signers.Changes)
	}

	operations, err := lens.DiffWithOptions(value("Operations", "a", "b"), value("Operations", "c", "a", "b"), options)
	if err != nil {
		t.Fatalf("DiffWithOptions() error = %v", err)
	}
	if len(operations.Changes) <= 1 {
		t.Errorf("operation list diff = %+v, want positional cascading changes", operations.Changes)
	}
}

// buildSignedEnvelope constructs a minimal, valid V1 envelope carrying the
// supplied signatures.
func buildSignedEnvelope(t *testing.T, signatures []xdr.DecoratedSignature) string {
	t.Helper()
	op := xdr.Operation{Body: xdr.OperationBody{
		Type:           xdr.OperationTypeBumpSequence,
		BumpSequenceOp: &xdr.BumpSequenceOp{BumpTo: xdr.SequenceNumber(5)},
	}}
	env := xdr.TransactionEnvelope{
		Type: xdr.EnvelopeTypeEnvelopeTypeTx,
		V1: &xdr.TransactionV1Envelope{
			Tx: xdr.Transaction{
				SourceAccount: xdr.MustMuxedAddress(signerTestAddrA),
				Fee:           100,
				SeqNum:        xdr.SequenceNumber(1),
				Cond:          xdr.Preconditions{Type: xdr.PreconditionTypePrecondNone},
				Memo:          xdr.Memo{Type: xdr.MemoTypeMemoNone},
				Operations:    []xdr.Operation{op},
				Ext:           xdr.TransactionExt{V: 0},
			},
			Signatures: signatures,
		},
	}
	return mustMarshal(t, env)
}
