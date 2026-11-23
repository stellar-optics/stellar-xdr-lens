package lens

import (
	"encoding/hex"
	"reflect"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"
)

// Protocol version 21 as a validator would cast it: the union discriminant
// followed by the new ledger version.
var versionVote = xdr.UpgradeType{0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x15}

func TestEnrichUpgradeTypeRendersVotes(t *testing.T) {
	t.Parallel()

	display, raw, ok := enrichUpgradeType(reflect.ValueOf(versionVote))
	if !ok {
		t.Fatal("enrichUpgradeType() declined a well-formed vote")
	}
	if display != "Protocol 21" {
		t.Errorf("display = %q, want %q", display, "Protocol 21")
	}
	if got, ok := raw.(string); !ok || got != hex.EncodeToString(versionVote) {
		t.Errorf("raw = %v, want hex string of original vote bytes", raw)
	}
}

func TestEnrichUpgradeTypeDeclines(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		rv   reflect.Value
	}{
		{"wrong type", reflect.ValueOf(xdr.Price{N: 1, D: 21})},
		{"nil bytes", reflect.ValueOf(xdr.UpgradeType(nil))},
		{"empty bytes", reflect.ValueOf(xdr.UpgradeType{})},
		{"not a ledger upgrade", reflect.ValueOf(xdr.UpgradeType{0xde, 0xad, 0xbe, 0xef})},
		{"unknown upgrade type", reflect.ValueOf(xdr.UpgradeType{0x00, 0x00, 0x00, 99, 0, 0, 0, 0})},
		{"trailing bytes", reflect.ValueOf(append(append(xdr.UpgradeType{}, versionVote...), 0x00))},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if display, raw, ok := enrichUpgradeType(tc.rv); ok {
				t.Errorf("enrichUpgradeType() = (%q, %v, true), want a decline", display, raw)
			}
		})
	}
}

func TestLedgerUpgradeSummaryDeclinesIncompleteArms(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		upgrade xdr.LedgerUpgrade
	}{
		{"version arm absent", xdr.LedgerUpgrade{Type: xdr.LedgerUpgradeTypeLedgerUpgradeVersion}},
		{"base fee arm absent", xdr.LedgerUpgrade{Type: xdr.LedgerUpgradeTypeLedgerUpgradeBaseFee}},
		{"max tx set size arm absent", xdr.LedgerUpgrade{Type: xdr.LedgerUpgradeTypeLedgerUpgradeMaxTxSetSize}},
		{"base reserve arm absent", xdr.LedgerUpgrade{Type: xdr.LedgerUpgradeTypeLedgerUpgradeBaseReserve}},
		{"flags arm absent", xdr.LedgerUpgrade{Type: xdr.LedgerUpgradeTypeLedgerUpgradeFlags}},
		{"config arm absent", xdr.LedgerUpgrade{Type: xdr.LedgerUpgradeTypeLedgerUpgradeConfig}},
		{"soroban size arm absent", xdr.LedgerUpgrade{Type: xdr.LedgerUpgradeTypeLedgerUpgradeMaxSorobanTxSetSize}},
		{"unknown upgrade type", xdr.LedgerUpgrade{Type: xdr.LedgerUpgradeType(99)}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if summary, ok := ledgerUpgradeSummary(tc.upgrade); ok {
				t.Errorf("ledgerUpgradeSummary() = (%q, true), want a decline", summary)
			}
		})
	}
}

func TestLedgerUpgradeSummaryBaseFee(t *testing.T) {
	t.Parallel()

	fee := xdr.Uint32(1000)
	summary, ok := ledgerUpgradeSummary(xdr.LedgerUpgrade{
		Type:       xdr.LedgerUpgradeTypeLedgerUpgradeBaseFee,
		NewBaseFee: &fee,
	})
	if !ok {
		t.Fatal("ledgerUpgradeSummary() declined a well-formed base fee vote")
	}
	if want := "base fee 1000 stroops (0.0001000 XLM)"; summary != want {
		t.Errorf("summary = %q, want %q", summary, want)
	}
}
