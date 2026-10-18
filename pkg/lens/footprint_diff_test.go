package lens_test

import (
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

const footprintAddrC = "GCJNHFXJEQEY54NPKU4DBMT5WOBJUHMYSFBDAQF4OMTEGJN4BQU7ZST6"

func accountLedgerKey(address string) xdr.LedgerKey {
	return xdr.LedgerKey{
		Type:    xdr.LedgerEntryTypeAccount,
		Account: &xdr.LedgerKeyAccount{AccountId: xdr.MustAddress(address)},
	}
}

func decodeEnvelopeFixture(t *testing.T, fixture string) *xdr.TransactionEnvelope {
	t.Helper()
	value, err := lens.DecodeAs(loadFixture(t, fixture), "TransactionEnvelope")
	if err != nil {
		t.Fatalf("DecodeAs(%s): %v", fixture, err)
	}
	env, ok := value.Raw.(*xdr.TransactionEnvelope)
	if !ok {
		t.Fatalf("fixture %s decoded as %T", fixture, value.Raw)
	}
	return env
}

func TestDiffFootprintKeysSetSemantics(t *testing.T) {
	t.Parallel()

	keyA := accountLedgerKey(signerTestAddrA)
	keyB := accountLedgerKey(signerTestAddrB)
	keyC := accountLedgerKey(footprintAddrC)

	t.Run("reordered is equal", func(t *testing.T) {
		t.Parallel()
		diff, err := lens.DiffFootprintKeys([]xdr.LedgerKey{keyA, keyB}, []xdr.LedgerKey{keyB, keyA})
		if err != nil {
			t.Fatalf("DiffFootprintKeys() error = %v", err)
		}
		if !diff.Equal() || len(diff.Common) != 2 || len(diff.Added) != 0 || len(diff.Removed) != 0 {
			t.Errorf("diff = %+v, want two common keys", diff)
		}
	})

	t.Run("additions and removals", func(t *testing.T) {
		t.Parallel()
		diff, err := lens.DiffFootprintKeys([]xdr.LedgerKey{keyA, keyB}, []xdr.LedgerKey{keyB, keyC})
		if err != nil {
			t.Fatalf("DiffFootprintKeys() error = %v", err)
		}
		if len(diff.Removed) != 1 || len(diff.Added) != 1 || len(diff.Common) != 1 {
			t.Fatalf("diff = %+v, want 1 removed, 1 added, 1 common", diff)
		}
		if diff.Equal() {
			t.Error("Equal = true, want false")
		}
	})

	t.Run("duplicates collapse", func(t *testing.T) {
		t.Parallel()
		diff, err := lens.DiffFootprintKeys([]xdr.LedgerKey{keyA, keyA}, []xdr.LedgerKey{keyA})
		if err != nil {
			t.Fatalf("DiffFootprintKeys() error = %v", err)
		}
		if !diff.Equal() || len(diff.Common) != 1 {
			t.Errorf("diff = %+v, want a single common key", diff)
		}
	})

	t.Run("summary", func(t *testing.T) {
		t.Parallel()
		diff, err := lens.DiffFootprintKeys([]xdr.LedgerKey{keyA}, []xdr.LedgerKey{keyB})
		if err != nil {
			t.Fatalf("DiffFootprintKeys() error = %v", err)
		}
		if got := diff.Summary(); got != "1 removed, 1 added, 0 common" {
			t.Errorf("Summary() = %q", got)
		}
		if got := diff.Removed[0]; got == "" {
			t.Error("removed label is empty")
		}
	})
}

func TestDiffFootprintKeysLabelsAreHumanReadable(t *testing.T) {
	t.Parallel()

	diff, err := lens.DiffFootprintKeys(
		[]xdr.LedgerKey{accountLedgerKey(signerTestAddrA)},
		nil,
	)
	if err != nil {
		t.Fatalf("DiffFootprintKeys() error = %v", err)
	}
	if len(diff.Removed) != 1 {
		t.Fatalf("Removed = %v, want one", diff.Removed)
	}
	if want := "account key: " + signerTestAddrA; diff.Removed[0] != want {
		t.Errorf("label = %q, want %q", diff.Removed[0], want)
	}
}

func TestFootprintFromEnvelopeAndDiff(t *testing.T) {
	t.Parallel()

	soroban := decodeEnvelopeFixture(t, "tx_soroban.env")
	failed := decodeEnvelopeFixture(t, "tx_failed.env")

	footprint, ok := lens.FootprintFromEnvelope(soroban)
	if !ok {
		t.Fatal("FootprintFromEnvelope(tx_soroban.env) reported no footprint")
	}
	if len(footprint.ReadOnly)+len(footprint.ReadWrite) == 0 {
		t.Fatal("tx_soroban.env footprint is empty, fixture unsuitable")
	}

	self, err := lens.DiffEnvelopeFootprints(soroban, soroban)
	if err != nil {
		t.Fatalf("DiffEnvelopeFootprints(self) error = %v", err)
	}
	if !self.Equal() {
		t.Errorf("self diff = %+v, want equal", self)
	}
	if got := self.Summary(); got != "no footprint changes" {
		t.Errorf("Summary() = %q, want no footprint changes", got)
	}

	cross, err := lens.DiffEnvelopeFootprints(soroban, failed)
	if err != nil {
		t.Fatalf("DiffEnvelopeFootprints(cross) error = %v", err)
	}
	if cross.Equal() {
		t.Log("note: the two Soroban fixtures declare identical footprints")
	} else if cross.ReadOnly.Equal() && cross.ReadWrite.Equal() {
		t.Errorf("cross footprint diff = %+v, want a non-empty half", cross)
	}
	if cross.Summary() == "" {
		t.Error("Summary() is empty")
	}
}

func TestDiffEnvelopeFootprintsRequiresSorobanData(t *testing.T) {
	t.Parallel()

	classic := &xdr.TransactionEnvelope{
		Type: xdr.EnvelopeTypeEnvelopeTypeTx,
		V1: &xdr.TransactionV1Envelope{
			Tx: xdr.Transaction{
				SourceAccount: xdr.MustMuxedAddress(signerTestAddrA),
				Cond:          xdr.Preconditions{Type: xdr.PreconditionTypePrecondNone},
				Memo:          xdr.Memo{Type: xdr.MemoTypeMemoNone},
				Ext:           xdr.TransactionExt{V: 0},
			},
		},
	}
	if _, ok := lens.FootprintFromEnvelope(classic); ok {
		t.Error("FootprintFromEnvelope reported a footprint for a classic envelope")
	}
	if _, err := lens.DiffEnvelopeFootprints(classic, classic); err == nil {
		t.Error("DiffEnvelopeFootprints() error = nil, want an error without Soroban data")
	}
	if _, err := lens.DiffEnvelopeFootprints(nil, classic); err == nil {
		t.Error("DiffEnvelopeFootprints(nil, ...) error = nil, want an error")
	}
}

func TestDiffSorobanFootprintsAndValueExtraction(t *testing.T) {
	t.Parallel()

	left := &xdr.SorobanTransactionData{Resources: xdr.SorobanResources{
		Footprint: xdr.LedgerFootprint{ReadOnly: []xdr.LedgerKey{accountLedgerKey(signerTestAddrA)}},
	}}
	right := &xdr.SorobanTransactionData{Resources: xdr.SorobanResources{
		Footprint: xdr.LedgerFootprint{ReadOnly: []xdr.LedgerKey{accountLedgerKey(signerTestAddrB)}},
	}}

	diff, err := lens.DiffSorobanFootprints(left, right)
	if err != nil {
		t.Fatalf("DiffSorobanFootprints() error = %v", err)
	}
	if diff.Equal() {
		t.Error("Equal = true, want different read-only sets")
	}
	if len(diff.ReadOnly.Added) != 1 || len(diff.ReadOnly.Removed) != 1 {
		t.Errorf("ReadOnly = %+v, want one added and one removed", diff.ReadOnly)
	}

	if _, err := lens.DiffSorobanFootprints(nil, right); err == nil {
		t.Error("DiffSorobanFootprints(nil, ...) error = nil, want an error")
	}

	value := &lens.Value{Type: "SorobanTransactionData", Raw: left}
	footprint, ok := lens.FootprintFromValue(value)
	if !ok || len(footprint.ReadOnly) != 1 {
		t.Errorf("FootprintFromValue() = %+v, %v", footprint, ok)
	}
	if _, ok := lens.FootprintFromValue(nil); ok {
		t.Error("FootprintFromValue(nil) reported ok")
	}
	if _, ok := lens.FootprintFromValue(&lens.Value{Type: "Asset"}); ok {
		t.Error("FootprintFromValue(Asset) reported ok")
	}
	if _, ok := lens.FootprintFromValue(&lens.Value{Type: "SorobanTransactionData", Raw: (*xdr.SorobanTransactionData)(nil)}); ok {
		t.Error("FootprintFromValue(typed nil) reported ok")
	}
}

func TestFootprintFromEnvelopeMalformedArms(t *testing.T) {
	t.Parallel()

	cases := map[string]*xdr.TransactionEnvelope{
		"nil":            nil,
		"v1 missing":     {Type: xdr.EnvelopeTypeEnvelopeTypeTx},
		"fee bump":       {Type: xdr.EnvelopeTypeEnvelopeTypeTxFeeBump},
		"fee bump inner": {Type: xdr.EnvelopeTypeEnvelopeTypeTxFeeBump, FeeBump: &xdr.FeeBumpTransactionEnvelope{Tx: xdr.FeeBumpTransaction{InnerTx: xdr.FeeBumpTransactionInnerTx{Type: xdr.EnvelopeTypeEnvelopeTypeTx}}}},
		"v0":             {Type: xdr.EnvelopeTypeEnvelopeTypeTxV0},
	}
	for name, env := range cases {
		if _, ok := lens.FootprintFromEnvelope(env); ok {
			t.Errorf("%s: FootprintFromEnvelope reported a footprint", name)
		}
	}
}

func TestFootprintFromEnvelopeFeeBump(t *testing.T) {
	t.Parallel()

	env := decodeEnvelopeFixture(t, "tx_soroban.env")
	if !env.IsFeeBump() {
		t.Fatal("fixture is not a fee bump")
	}
	if _, ok := lens.FootprintFromEnvelope(env); !ok {
		t.Error("FootprintFromEnvelope(fee bump) reported no footprint")
	}
}

func TestDiffFootprintsWhole(t *testing.T) {
	t.Parallel()

	left := xdr.LedgerFootprint{
		ReadOnly:  []xdr.LedgerKey{accountLedgerKey(signerTestAddrA)},
		ReadWrite: []xdr.LedgerKey{accountLedgerKey(signerTestAddrB)},
	}
	right := xdr.LedgerFootprint{
		ReadOnly:  []xdr.LedgerKey{accountLedgerKey(signerTestAddrA)},
		ReadWrite: []xdr.LedgerKey{accountLedgerKey(footprintAddrC)},
	}
	diff, err := lens.DiffFootprints(left, right)
	if err != nil {
		t.Fatalf("DiffFootprints() error = %v", err)
	}
	if !diff.ReadOnly.Equal() {
		t.Errorf("ReadOnly = %+v, want equal", diff.ReadOnly)
	}
	if diff.ReadWrite.Equal() {
		t.Error("ReadWrite Equal = true, want a change")
	}
	if diff.Summary() == "" || !strings.Contains(diff.Summary(), "read-write") {
		t.Errorf("Summary() = %q", diff.Summary())
	}
}
