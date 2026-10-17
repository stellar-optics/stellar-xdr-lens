package lens

import (
	"reflect"
	"testing"

	"github.com/stellar/go-stellar-sdk/amount"
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

// TestTrustLineFlagNoteCombinations pins the expansion of every documented
// trustline flag bit, including the combined bitmask used by a clawback-
// enabled, fully authorized line.
func TestTrustLineFlagNoteCombinations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		flags xdr.Uint32
		want  string
		ok    bool
	}{
		{"zero declines", 0, "", false},
		{
			"authorized",
			xdr.Uint32(xdr.TrustLineFlagsAuthorizedFlag),
			"authorized",
			true,
		},
		{
			"maintain liabilities only",
			xdr.Uint32(xdr.TrustLineFlagsAuthorizedToMaintainLiabilitiesFlag),
			"authorized_to_maintain_liabilities",
			true,
		},
		{
			"clawback only",
			xdr.Uint32(xdr.TrustLineFlagsTrustlineClawbackEnabledFlag),
			"trustline_clawback_enabled",
			true,
		},
		{
			"authorized and clawback",
			xdr.Uint32(xdr.TrustLineFlagsAuthorizedFlag |
				xdr.TrustLineFlagsTrustlineClawbackEnabledFlag),
			"authorized | trustline_clawback_enabled",
			true,
		},
		{
			"all three flags",
			xdr.Uint32(xdr.TrustLineFlagsAuthorizedFlag |
				xdr.TrustLineFlagsAuthorizedToMaintainLiabilitiesFlag |
				xdr.TrustLineFlagsTrustlineClawbackEnabledFlag),
			"authorized | authorized_to_maintain_liabilities | trustline_clawback_enabled",
			true,
		},
		{"unknown bits alone decline", xdr.Uint32(0x8), "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			note, ok := trustLineFlagNote(reflect.ValueOf(tc.flags))
			if ok != tc.ok || note != tc.want {
				t.Errorf("trustLineFlagNote(%d) = (%q, %v), want (%q, %v)",
					tc.flags, note, ok, tc.want, tc.ok)
			}
		})
	}
}

// TestTrustLineLiabilitiesAmountFormatting pins the shared amount-field
// enricher for the V1 liabilities: Buying and Selling are Int64 token
// balances and must render as decimal units alongside the raw stroop integer,
// exactly like every other amount field in the protocol.
func TestTrustLineLiabilitiesAmountFormatting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		field string
		value xdr.Int64
		want  string
	}{
		{"buying", "Buying", xdr.Int64(100 * amount.One), "100.0000000 (stroops: 1000000000)"},
		{"selling", "Selling", xdr.Int64(200 * amount.One), "200.0000000 (stroops: 2000000000)"},
		{"zero buying", "Buying", xdr.Int64(0), "0.0000000 (stroops: 0)"},
		{"balance", "Balance", xdr.Int64(50 * amount.One), "50.0000000 (stroops: 500000000)"},
		{"limit", "Limit", xdr.Int64(1000 * amount.One), "1000.0000000 (stroops: 10000000000)"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			note, ok := enrichAmountField(tc.field, reflect.ValueOf(tc.value))
			if !ok || note != tc.want {
				t.Errorf("enrichAmountField(%q, %d) = (%q, %v), want (%q, true)",
					tc.field, tc.value, note, ok, tc.want)
			}
		})
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
