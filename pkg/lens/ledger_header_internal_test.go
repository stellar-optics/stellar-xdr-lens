package lens

import (
	"reflect"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"
)

func TestProtocolVersionNote(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		value    any
		wantNote string
		wantOK   bool
	}{
		{"released version", xdr.Uint32(22), "protocol 22", true},
		{"zero version declines", xdr.Uint32(0), "", false},
		{"wrong type declines", xdr.Int64(22), "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			note, ok := protocolVersionNote(reflect.ValueOf(tc.value))
			if ok != tc.wantOK || note != tc.wantNote {
				t.Errorf("protocolVersionNote(%v) = (%q, %v), want (%q, %v)",
					tc.value, note, ok, tc.wantNote, tc.wantOK)
			}
		})
	}
}

// TestEnrichAmountFieldAcceptShapes pins the three shapes a stroop amount
// arrives in: a plain Int64, a Uint32 protocol parameter, and a pointer-
// wrapped union arm.
func TestEnrichAmountFieldAcceptShapes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		field    string
		value    any
		wantNote string
		wantOK   bool
	}{
		{"int64 amount", "TotalCoins", xdr.Int64(1045912973109999952), "104591297310.9999952 (stroops: 1045912973109999952)", true},
		{"uint32 parameter", "BaseReserve", xdr.Uint32(5000000), "0.5000000 (stroops: 5000000)", true},
		{"pointer parameter", "BaseFee", ptrTo(xdr.Uint32(100)), "0.0000100 (stroops: 100)", true},
		{"unknown field declines", "NotAnAmount", xdr.Int64(1), "", false},
		{"wrong type declines", "TotalCoins", "not an amount", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			note, ok := enrichAmountField(tc.field, reflect.ValueOf(tc.value))
			if ok != tc.wantOK || note != tc.wantNote {
				t.Errorf("enrichAmountField(%q, %T) = (%q, %v), want (%q, %v)",
					tc.field, tc.value, note, ok, tc.wantNote, tc.wantOK)
			}
		})
	}
}

func TestEnrichAmountFieldDeclinesNilPointer(t *testing.T) {
	t.Parallel()

	var p *xdr.Uint32
	if note, ok := enrichAmountField("BaseFee", reflect.ValueOf(p)); ok {
		t.Errorf("enrichAmountField(nil pointer) = (%q, true), want ok=false", note)
	}
}

// TestEnrichFieldAppliesRegisteredNotes covers the fieldNotes seam itself:
// registered fields get their note, unregistered fields decline.
func TestEnrichFieldAppliesRegisteredNotes(t *testing.T) {
	t.Parallel()

	note, ok := enrichField("LedgerVersion", reflect.ValueOf(xdr.Uint32(22)))
	if !ok || note != "protocol 22" {
		t.Errorf("enrichField(LedgerVersion) = (%q, %v), want (protocol 22, true)", note, ok)
	}

	if note, ok := enrichField("Unregistered", reflect.ValueOf(xdr.Uint32(1))); ok {
		t.Errorf("enrichField(unregistered) = (%q, true), want ok=false", note)
	}
}

// TestEnrichFieldRecoversFromPanickingNote proves a note function that panics
// on hostile input degrades to no note instead of taking the process down.
//
// It deliberately does not run in parallel: it registers a note in the shared
// fieldNotes map, and parallel tests are sequenced after sequential ones so
// the registry is never written while another test reads it.
func TestEnrichFieldRecoversFromPanickingNote(t *testing.T) {
	const field = "TestPanickingNote"
	fieldNotes[field] = func(reflect.Value) (string, bool) { panic("boom") }
	t.Cleanup(func() { delete(fieldNotes, field) })

	if note, ok := enrichField(field, reflect.ValueOf(1)); ok {
		t.Errorf("enrichField(panicking note) = (%q, true), want ok=false", note)
	}
}

func TestLedgerHeaderFlagNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		flags xdr.Uint32
		want  []string
	}{
		{"none", 0, nil},
		{"trading only", xdr.Uint32(xdr.LedgerHeaderFlagsDisableLiquidityPoolTradingFlag), []string{"disable_liquidity_pool_trading"}},
		{"deposit only", xdr.Uint32(xdr.LedgerHeaderFlagsDisableLiquidityPoolDepositFlag), []string{"disable_liquidity_pool_deposit"}},
		{"withdrawal only", xdr.Uint32(xdr.LedgerHeaderFlagsDisableLiquidityPoolWithdrawalFlag), []string{"disable_liquidity_pool_withdrawal"}},
		{
			"all flags",
			xdr.Uint32(xdr.LedgerHeaderFlagsDisableLiquidityPoolTradingFlag |
				xdr.LedgerHeaderFlagsDisableLiquidityPoolDepositFlag |
				xdr.LedgerHeaderFlagsDisableLiquidityPoolWithdrawalFlag),
			[]string{
				"disable_liquidity_pool_trading",
				"disable_liquidity_pool_deposit",
				"disable_liquidity_pool_withdrawal",
			},
		},
		{"unknown bits ignored", xdr.Uint32(0x8), nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := ledgerHeaderFlagNames(tc.flags)
			if len(got) != len(tc.want) {
				t.Fatalf("ledgerHeaderFlagNames(%d) = %v, want %v", tc.flags, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("ledgerHeaderFlagNames(%d)[%d] = %q, want %q", tc.flags, i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestLedgerHeaderFlagNote(t *testing.T) {
	t.Parallel()

	note, ok := ledgerHeaderFlagNote(reflect.ValueOf(xdr.Uint32(
		xdr.LedgerHeaderFlagsDisableLiquidityPoolTradingFlag |
			xdr.LedgerHeaderFlagsDisableLiquidityPoolDepositFlag)))
	if !ok || note != "disable_liquidity_pool_trading | disable_liquidity_pool_deposit" {
		t.Errorf("ledgerHeaderFlagNote = (%q, %v), want combined names", note, ok)
	}
	if _, ok := ledgerHeaderFlagNote(reflect.ValueOf(xdr.Uint32(0))); ok {
		t.Error("ledgerHeaderFlagNote produced a note for an empty flag set")
	}
	if _, ok := ledgerHeaderFlagNote(reflect.ValueOf("x")); ok {
		t.Error("ledgerHeaderFlagNote accepted a non-Uint32 value")
	}
}

func TestDescribeLedgerUpgrade(t *testing.T) {
	t.Parallel()

	version := xdr.LedgerUpgrade{
		Type:             xdr.LedgerUpgradeTypeLedgerUpgradeVersion,
		NewLedgerVersion: ptrTo(xdr.Uint32(23)),
	}
	baseFee := xdr.LedgerUpgrade{
		Type:       xdr.LedgerUpgradeTypeLedgerUpgradeBaseFee,
		NewBaseFee: ptrTo(xdr.Uint32(200)),
	}
	maxTxSet := xdr.LedgerUpgrade{
		Type:            xdr.LedgerUpgradeTypeLedgerUpgradeMaxTxSetSize,
		NewMaxTxSetSize: ptrTo(xdr.Uint32(1200)),
	}
	baseReserve := xdr.LedgerUpgrade{
		Type:           xdr.LedgerUpgradeTypeLedgerUpgradeBaseReserve,
		NewBaseReserve: ptrTo(xdr.Uint32(10000000)),
	}
	flags := xdr.LedgerUpgrade{
		Type: xdr.LedgerUpgradeTypeLedgerUpgradeFlags,
		NewFlags: ptrTo(xdr.Uint32(
			xdr.LedgerHeaderFlagsDisableLiquidityPoolTradingFlag)),
	}
	soroban := xdr.LedgerUpgrade{
		Type:                   xdr.LedgerUpgradeTypeLedgerUpgradeMaxSorobanTxSetSize,
		NewMaxSorobanTxSetSize: ptrTo(xdr.Uint32(50)),
	}

	tests := []struct {
		name    string
		upgrade xdr.LedgerUpgrade
		want    string
	}{
		{"version", version, "protocol version 23"},
		{"base fee", baseFee, "base fee 0.0000200 XLM (200 stroops)"},
		{"max tx set size", maxTxSet, "max tx set size 1200"},
		{"base reserve", baseReserve, "base reserve 1.0000000 XLM (10000000 stroops)"},
		{"flags", flags, "flags disable_liquidity_pool_trading"},
		{"max soroban tx set size", soroban, "max soroban tx set size 50"},
		{"unknown type falls back", xdr.LedgerUpgrade{Type: xdr.LedgerUpgradeType(99)}, "upgrade type 99"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			u := tc.upgrade
			if got := describeLedgerUpgrade(&u); got != tc.want {
				t.Errorf("describeLedgerUpgrade() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEnrichUpgradeType(t *testing.T) {
	t.Parallel()

	upgrade := xdr.LedgerUpgrade{
		Type:             xdr.LedgerUpgradeTypeLedgerUpgradeVersion,
		NewLedgerVersion: ptrTo(xdr.Uint32(23)),
	}
	blob, err := upgrade.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}

	note, raw, ok := enrichUpgradeType(reflect.ValueOf(xdr.UpgradeType(blob)))
	if !ok || note != "protocol version 23" {
		t.Errorf("enrichUpgradeType(version) = (%q, %v), want protocol version 23", note, ok)
	}
	if b, okRaw := raw.(xdr.UpgradeType); !okRaw || string(b) != string(blob) {
		t.Errorf("raw = %v, want the original upgrade bytes", raw)
	}

	// Empty and undecodable blobs decline so the generic hex view remains.
	if _, _, ok := enrichUpgradeType(reflect.ValueOf(xdr.UpgradeType{})); ok {
		t.Error("enrichUpgradeType(empty) ok = true, want false")
	}
	if _, _, ok := enrichUpgradeType(reflect.ValueOf(xdr.UpgradeType{0x7f, 0x7f})); ok {
		t.Error("enrichUpgradeType(garbage) ok = true, want false")
	}
	if _, _, ok := enrichUpgradeType(reflect.ValueOf("not an upgrade")); ok {
		t.Error("enrichUpgradeType(string) ok = true, want false")
	}
}

func TestEnrichLedgerUpgrade(t *testing.T) {
	t.Parallel()

	upgrade := xdr.LedgerUpgrade{
		Type:            xdr.LedgerUpgradeTypeLedgerUpgradeMaxTxSetSize,
		NewMaxTxSetSize: ptrTo(xdr.Uint32(1500)),
	}
	note, raw, ok := enrichLedgerUpgrade(reflect.ValueOf(upgrade))
	if !ok || note != "max tx set size 1500" {
		t.Errorf("enrichLedgerUpgrade() = (%q, %v), want max tx set size 1500", note, ok)
	}
	if raw != upgrade {
		t.Error("raw value altered")
	}
	if _, _, ok := enrichLedgerUpgrade(reflect.ValueOf("x")); ok {
		t.Error("enrichLedgerUpgrade(string) ok = true, want false")
	}
}

func ptrTo[T any](v T) *T { return &v }
