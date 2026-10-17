package lens

import (
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

// testAccountID builds an account id for the internal tests, which cannot use
// the lens_test package helpers.
func testAccountID(t *testing.T, addr string) xdr.AccountId {
	t.Helper()
	var aid xdr.AccountId
	if err := aid.SetAddress(addr); err != nil {
		t.Fatalf("parsing address %s: %v", addr, err)
	}
	return aid
}

// testCreditAsset builds a classic credit asset for the internal tests.
func testCreditAsset(t *testing.T, code, issuer string) xdr.Asset {
	t.Helper()
	var a xdr.Asset
	if err := a.SetCredit(code, testAccountID(t, issuer)); err != nil {
		t.Fatalf("building asset %s: %v", code, err)
	}
	return a
}

const (
	testClaimDestA  = "GAPB7YMUQ6MPQ23PTFDNBZP7HGGTZPI2DT5JJURUOIXNPLGHW2O3WYXZ"
	testClaimDestB  = "GCJNHFXJEQEY54NPKU4DBMT5WOBJUHMYSFBDAQF4OMTEGJN4BQU7ZST6"
	testClaimIssuer = "GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAW"
)

func unconditionalClaimant(t *testing.T, dest string) xdr.Claimant {
	t.Helper()
	return xdr.Claimant{
		Type: xdr.ClaimantTypeClaimantTypeV0,
		V0: &xdr.ClaimantV0{
			Destination: testAccountID(t, dest),
			Predicate:   xdr.ClaimPredicate{Type: xdr.ClaimPredicateTypeClaimPredicateUnconditional},
		},
	}
}

func TestDecorateClaimableBalanceEntry(t *testing.T) {
	t.Parallel()

	hash := xdr.Hash{0xAB}
	entry := xdr.ClaimableBalanceEntry{
		BalanceId: xdr.ClaimableBalanceId{
			Type: xdr.ClaimableBalanceIdTypeClaimableBalanceIdTypeV0,
			V0:   &hash,
		},
		Claimants: []xdr.Claimant{
			unconditionalClaimant(t, testClaimDestA),
			unconditionalClaimant(t, testClaimDestB),
		},
		Asset:  testCreditAsset(t, "USDC", testClaimIssuer),
		Amount: xdr.Int64(10_000_000),
	}

	node := buildNode("ClaimableBalanceEntry", reflect.ValueOf(entry), 0)
	if node == nil {
		t.Fatal("buildNode(ClaimableBalanceEntry) returned nil")
	}
	id := node.Child("BalanceId")
	if id == nil {
		t.Fatal("BalanceId child not found")
	}
	want := "2 claimants; asset USDC:" + testClaimIssuer
	if id.Note != want {
		t.Errorf("BalanceId note = %q, want %q", id.Note, want)
	}
	if s, ok := id.Value.(string); !ok || !strings.HasPrefix(s, "B") {
		t.Errorf("BalanceId value = %v, want B... strkey", id.Value)
	}
}

func TestDecorateClaimableBalanceEntryMalformedAssetStillCountsClaimants(t *testing.T) {
	t.Parallel()

	entry := xdr.ClaimableBalanceEntry{
		BalanceId: xdr.ClaimableBalanceId{
			Type: xdr.ClaimableBalanceIdTypeClaimableBalanceIdTypeV0,
			V0:   &xdr.Hash{0x01},
		},
		Claimants: []xdr.Claimant{unconditionalClaimant(t, testClaimDestA)},
		Asset:     xdr.Asset{Type: xdr.AssetType(99)},
	}

	node := buildNode("ClaimableBalanceEntry", reflect.ValueOf(entry), 0)
	if node == nil {
		t.Fatal("buildNode(ClaimableBalanceEntry) returned nil")
	}
	id := node.Child("BalanceId")
	if id == nil {
		t.Fatal("BalanceId child not found")
	}
	if id.Note != "1 claimant" {
		t.Errorf("BalanceId note = %q, want %q", id.Note, "1 claimant")
	}
}

func TestClaimantCountLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		count int
		want  string
	}{
		{0, "0 claimants"},
		{1, "1 claimant"},
		{3, "3 claimants"},
	}
	for _, tc := range tests {
		if got := claimantCountLabel(tc.count); got != tc.want {
			t.Errorf("claimantCountLabel(%d) = %q, want %q", tc.count, got, tc.want)
		}
	}
}

func TestCanonicalAssetName(t *testing.T) {
	t.Parallel()

	if name, ok := canonicalAssetName(xdr.MustNewNativeAsset()); !ok || name != "native" {
		t.Errorf("canonicalAssetName(native) = %q, %v; want native, true", name, ok)
	}
	if name, ok := canonicalAssetName(testCreditAsset(t, "USDC", testClaimIssuer)); !ok || name != "USDC:"+testClaimIssuer {
		t.Errorf("canonicalAssetName(USDC) = %q, %v; want the canonical form", name, ok)
	}
	if name, ok := canonicalAssetName(xdr.Asset{Type: xdr.AssetType(99)}); ok || name != "" {
		t.Errorf("canonicalAssetName(unknown type) = %q, %v; want \"\", false", name, ok)
	}
	// A credit union missing its arm must decline rather than panic.
	if name, ok := canonicalAssetName(xdr.Asset{Type: xdr.AssetTypeAssetTypeCreditAlphanum4}); ok || name != "" {
		t.Errorf("canonicalAssetName(dangling credit arm) = %q, %v; want \"\", false", name, ok)
	}
}

func TestDescribeClaimPredicateNested(t *testing.T) {
	t.Parallel()

	p := xdr.ClaimPredicate{
		Type: xdr.ClaimPredicateTypeClaimPredicateAnd,
		AndPredicates: &[]xdr.ClaimPredicate{
			{
				Type: xdr.ClaimPredicateTypeClaimPredicateOr,
				OrPredicates: &[]xdr.ClaimPredicate{
					{Type: xdr.ClaimPredicateTypeClaimPredicateUnconditional},
					{
						Type: xdr.ClaimPredicateTypeClaimPredicateNot,
						NotPredicate: ptrTo(&xdr.ClaimPredicate{
							Type:      xdr.ClaimPredicateTypeClaimPredicateBeforeAbsoluteTime,
							AbsBefore: ptrTo(xdr.Int64(1735689600)),
						}),
					},
				},
			},
			{
				Type:      xdr.ClaimPredicateTypeClaimPredicateBeforeAbsoluteTime,
				AbsBefore: ptrTo(xdr.Int64(1767225600)),
			},
		},
	}

	want := "((unconditional or not (before 2025-01-01T00:00:00Z)) and before 2026-01-01T00:00:00Z)"
	if got := DescribeClaimPredicate(p); got != want {
		t.Errorf("DescribeClaimPredicate(nested) = %q, want %q", got, want)
	}
}
