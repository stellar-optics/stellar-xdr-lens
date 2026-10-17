package lens_test

import (
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

func TestClaimableBalanceEntryDecoding(t *testing.T) {
	t.Parallel()

	v, err := lens.DecodeAs(loadFixture(t, "claimable_balance_entry"), "ClaimableBalanceEntry")
	if err != nil {
		t.Fatalf("DecodeAs() error = %v", err)
	}
	if v.Type != "ClaimableBalanceEntry" {
		t.Errorf("Type = %q, want ClaimableBalanceEntry", v.Type)
	}

	// Verify BalanceId is rendered as a B... strkey
	balanceID := v.Node.Find("BalanceId")
	if balanceID == nil {
		t.Fatal("BalanceId not found in tree")
	}
	if balanceID.Kind != lens.KindScalar {
		t.Errorf("BalanceId Kind = %v, want KindScalar", balanceID.Kind)
	}
	if !strings.HasPrefix(balanceID.Value.(string), "B") {
		t.Errorf("BalanceId Value = %q, want B... strkey", balanceID.Value)
	}

	// Verify Claimants array
	claimants := v.Node.Find("Claimants")
	if claimants == nil {
		t.Fatal("Claimants not found in tree")
	}
	if claimants.Kind != lens.KindList {
		t.Errorf("Claimants Kind = %v, want KindList", claimants.Kind)
	}
	if len(claimants.Children) != 3 {
		t.Errorf("Claimants has %d children, want 3", len(claimants.Children))
	}

	// Check first claimant (before absolute time)
	claimant0 := claimants.Children[0]
	if claimant0 == nil {
		t.Fatal("Claimant[0] is nil")
	}
	v0 := claimant0.Child("V0")
	if v0 == nil {
		t.Fatal("Claimant[0].V0 not found")
	}
	dest0 := v0.Child("Destination")
	if dest0 == nil {
		t.Fatal("Claimant[0].V0.Destination not found")
	}
	if !strings.HasPrefix(dest0.Value.(string), "GAPB") {
		t.Errorf("Claimant[0].Destination = %q, want GAPB...", dest0.Value)
	}
	pred0 := v0.Child("Predicate")
	if pred0 == nil {
		t.Fatal("Claimant[0].V0.Predicate not found")
	}
	if pred0.Value != "before 2025-01-01T00:00:00Z" {
		t.Errorf("Claimant[0].Predicate = %q, want 'before 2025-01-01T00:00:00Z'", pred0.Value)
	}

	// Check second claimant (unconditional)
	claimant1 := claimants.Children[1]
	if claimant1 == nil {
		t.Fatal("Claimant[1] is nil")
	}
	v0 = claimant1.Child("V0")
	if v0 == nil {
		t.Fatal("Claimant[1].V0 not found")
	}
	pred1 := v0.Child("Predicate")
	if pred1 == nil {
		t.Fatal("Claimant[1].V0.Predicate not found")
	}
	if pred1.Value != "unconditional" {
		t.Errorf("Claimant[1].Predicate = %q, want 'unconditional'", pred1.Value)
	}

	// Check third claimant (OR predicate)
	claimant2 := claimants.Children[2]
	if claimant2 == nil {
		t.Fatal("Claimant[2] is nil")
	}
	v0 = claimant2.Child("V0")
	if v0 == nil {
		t.Fatal("Claimant[2].V0 not found")
	}
	pred2 := v0.Child("Predicate")
	if pred2 == nil {
		t.Fatal("Claimant[2].V0.Predicate not found")
	}
	want2 := "(before 2026-01-01T00:00:00Z or unconditional)"
	if pred2.Value != want2 {
		t.Errorf("Claimant[2].Predicate = %q, want %q", pred2.Value, want2)
	}

	// Verify Asset
	asset := v.Node.Find("Asset")
	if asset == nil {
		t.Fatal("Asset not found in tree")
	}
	if !strings.Contains(asset.Value.(string), "USDC") {
		t.Errorf("Asset Value = %q, want to contain USDC", asset.Value)
	}

	// Verify Amount
	amountNode := v.Node.Find("Amount")
	if amountNode == nil {
		t.Fatal("Amount not found in tree")
	}
	if amountNode.Value != int64(10_000_000_000) {
		t.Errorf("Amount Value = %v, want 10000000000", amountNode.Value)
	}
	if !strings.Contains(amountNode.Note, "1000.0000000") {
		t.Errorf("Amount Note = %q, want formatted amount", amountNode.Note)
	}
}

func TestClaimableBalanceEntryIDAnnotation(t *testing.T) {
	t.Parallel()

	v, err := lens.DecodeAs(loadFixture(t, "claimable_balance_entry"), "ClaimableBalanceEntry")
	if err != nil {
		t.Fatalf("DecodeAs() error = %v", err)
	}

	id := v.Node.Find("BalanceId")
	if id == nil {
		t.Fatal("BalanceId not found in tree")
	}
	want := "3 claimants; asset USDC:GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAW"
	if id.Note != want {
		t.Errorf("BalanceId Note = %q, want %q", id.Note, want)
	}
}

func TestClaimableBalanceEntrySimpleDecoding(t *testing.T) {
	t.Parallel()

	v, err := lens.DecodeAs(loadFixture(t, "claimable_balance_entry_simple"), "ClaimableBalanceEntry")
	if err != nil {
		t.Fatalf("DecodeAs() error = %v", err)
	}
	if v.Type != "ClaimableBalanceEntry" {
		t.Errorf("Type = %q, want ClaimableBalanceEntry", v.Type)
	}

	// Verify simple claimant with relative time predicate
	claimants := v.Node.Find("Claimants")
	if claimants == nil {
		t.Fatal("Claimants not found")
	}
	if len(claimants.Children) != 1 {
		t.Errorf("Claimants has %d children, want 1", len(claimants.Children))
	}

	v0 := claimants.Children[0].Child("V0")
	if v0 == nil {
		t.Fatal("V0 not found")
	}
	pred := v0.Child("Predicate")
	if pred == nil {
		t.Fatal("Predicate not found")
	}
	if pred.Value != "before 86400 seconds after creation" {
		t.Errorf("Predicate = %q, want relative time", pred.Value)
	}

	// Verify no extension (V=0)
	ext := v.Node.Find("Ext")
	if ext == nil {
		t.Fatal("Ext not found")
	}
	vNode := ext.Child("V")
	if vNode == nil || vNode.Value != int64(0) {
		t.Errorf("Ext.V = %v, want 0", vNode.Value)
	}
}

func TestClaimableBalanceClawbackFlag(t *testing.T) {
	t.Parallel()

	var balanceHash xdr.Hash
	copy(balanceHash[:], strings.Repeat("A", 32))

	entry := xdr.ClaimableBalanceEntry{
		BalanceId: xdr.ClaimableBalanceId{
			Type: xdr.ClaimableBalanceIdTypeClaimableBalanceIdTypeV0,
			V0:   &balanceHash,
		},
		Claimants: []xdr.Claimant{{
			Type: xdr.ClaimantTypeClaimantTypeV0,
			V0: &xdr.ClaimantV0{
				Destination: mustAccountID(t, testDest),
				Predicate:   xdr.ClaimPredicate{Type: xdr.ClaimPredicateTypeClaimPredicateUnconditional},
			},
		}},
		Asset:  creditAsset(t, "USDC", testIssuer),
		Amount: xdr.Int64(10_000_000),
		Ext: xdr.ClaimableBalanceEntryExt{
			V: 1,
			V1: &xdr.ClaimableBalanceEntryExtensionV1{
				Flags: xdr.Uint32(xdr.ClaimableBalanceFlagsClaimableBalanceClawbackEnabledFlag),
			},
		},
	}

	v, err := lens.DecodeAs(mustMarshal(t, entry), "ClaimableBalanceEntry")
	if err != nil {
		t.Fatalf("DecodeAs() error = %v", err)
	}
	flags := v.Node.Find("Ext.V1.Flags")
	if flags == nil {
		t.Fatal("Ext.V1.Flags not found")
	}
	if flags.Value != uint64(1) {
		t.Errorf("Flags Value = %v, want 1", flags.Value)
	}
	if flags.Note != "clawback enabled" {
		t.Errorf("Flags Note = %q, want 'clawback enabled'", flags.Note)
	}
}

func TestClaimableBalanceEntryDetection(t *testing.T) {
	t.Parallel()

	cands, err := lens.Detect(loadFixture(t, "claimable_balance_entry"))
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	found := false
	for _, c := range cands {
		if c.Type == "ClaimableBalanceEntry" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Detect() did not find ClaimableBalanceEntry among candidates: %v", cands)
	}
}

func TestDescribeClaimPredicate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		predicate xdr.ClaimPredicate
		want      string
	}{
		{
			name: "unconditional",
			predicate: xdr.ClaimPredicate{
				Type: xdr.ClaimPredicateTypeClaimPredicateUnconditional,
			},
			want: "unconditional",
		},
		{
			name: "before absolute time",
			predicate: xdr.ClaimPredicate{
				Type:      xdr.ClaimPredicateTypeClaimPredicateBeforeAbsoluteTime,
				AbsBefore: ptr(xdr.Int64(1735689600)),
			},
			want: "before 2025-01-01T00:00:00Z",
		},
		{
			name: "before relative time",
			predicate: xdr.ClaimPredicate{
				Type:      xdr.ClaimPredicateTypeClaimPredicateBeforeRelativeTime,
				RelBefore: ptr(xdr.Int64(86400)),
			},
			want: "before 86400 seconds after creation",
		},
		{
			name: "not predicate",
			predicate: xdr.ClaimPredicate{
				Type: xdr.ClaimPredicateTypeClaimPredicateNot,
				NotPredicate: ptr(&xdr.ClaimPredicate{
					Type: xdr.ClaimPredicateTypeClaimPredicateUnconditional,
				}),
			},
			want: "not (unconditional)",
		},
		{
			name: "and predicate",
			predicate: xdr.ClaimPredicate{
				Type: xdr.ClaimPredicateTypeClaimPredicateAnd,
				AndPredicates: &[]xdr.ClaimPredicate{
					{Type: xdr.ClaimPredicateTypeClaimPredicateUnconditional},
					{Type: xdr.ClaimPredicateTypeClaimPredicateBeforeRelativeTime, RelBefore: ptr(xdr.Int64(3600))},
				},
			},
			want: "(unconditional and before 3600 seconds after creation)",
		},
		{
			name: "or predicate",
			predicate: xdr.ClaimPredicate{
				Type: xdr.ClaimPredicateTypeClaimPredicateOr,
				OrPredicates: &[]xdr.ClaimPredicate{
					{Type: xdr.ClaimPredicateTypeClaimPredicateUnconditional},
					{Type: xdr.ClaimPredicateTypeClaimPredicateBeforeRelativeTime, RelBefore: ptr(xdr.Int64(3600))},
				},
			},
			want: "(unconditional or before 3600 seconds after creation)",
		},
		{
			name: "dangling absolute time arm",
			predicate: xdr.ClaimPredicate{
				Type: xdr.ClaimPredicateTypeClaimPredicateBeforeAbsoluteTime,
			},
			want: "before (absolute time)",
		},
		{
			name: "dangling relative time arm",
			predicate: xdr.ClaimPredicate{
				Type: xdr.ClaimPredicateTypeClaimPredicateBeforeRelativeTime,
			},
			want: "before (relative time)",
		},
		{
			name: "dangling not arm",
			predicate: xdr.ClaimPredicate{
				Type: xdr.ClaimPredicateTypeClaimPredicateNot,
			},
			want: "not",
		},
		{
			name: "dangling and arm",
			predicate: xdr.ClaimPredicate{
				Type: xdr.ClaimPredicateTypeClaimPredicateAnd,
			},
			want: "and",
		},
		{
			name: "dangling or arm",
			predicate: xdr.ClaimPredicate{
				Type: xdr.ClaimPredicateTypeClaimPredicateOr,
			},
			want: "or",
		},
		{
			name: "unknown predicate type",
			predicate: xdr.ClaimPredicate{
				Type: xdr.ClaimPredicateType(99),
			},
			want: "unknown predicate type (99)",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := lens.DescribeClaimPredicate(tc.predicate); got != tc.want {
				t.Errorf("DescribeClaimPredicate() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClaimPredicateDecodesToExpression(t *testing.T) {
	t.Parallel()

	p := xdr.ClaimPredicate{
		Type:      xdr.ClaimPredicateTypeClaimPredicateBeforeAbsoluteTime,
		AbsBefore: ptr(xdr.Int64(1735689600)),
	}
	v, err := lens.DecodeAs(mustMarshal(t, p), "ClaimPredicate")
	if err != nil {
		t.Fatalf("DecodeAs() error = %v", err)
	}
	if v.Node.Value != "before 2025-01-01T00:00:00Z" {
		t.Errorf("Predicate Value = %q, want expression", v.Node.Value)
	}
}
