package lens_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

func encodeUpgrade(t *testing.T, upgrade xdr.LedgerUpgrade) string {
	t.Helper()
	raw, err := upgrade.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func TestDecodeProtocolVersionUpgradeNamedProtocol(t *testing.T) {
	t.Parallel()

	// Protocol 20 (Soroban Launch).
	v := xdr.Uint32(20)
	payload := encodeUpgrade(t, xdr.LedgerUpgrade{
		Type:             xdr.LedgerUpgradeTypeLedgerUpgradeVersion,
		NewLedgerVersion: &v,
	})

	val, err := lens.DecodeAs(payload, "LedgerUpgrade")
	if err != nil {
		t.Fatalf("DecodeAs() error = %v", err)
	}
	if val.Type != "LedgerUpgrade" {
		t.Errorf("Type = %q, want LedgerUpgrade", val.Type)
	}

	// Root Note from enrichLedgerUpgrade should contain "20".
	if !strings.Contains(val.Node.Note, "20") {
		t.Errorf("root Note %q does not contain protocol number 20", val.Node.Note)
	}
}

func TestDecodeProtocolVersionUpgradeUnnamedProtocol(t *testing.T) {
	t.Parallel()

	// Protocol 21 has no milestone name.
	v := xdr.Uint32(21)
	payload := encodeUpgrade(t, xdr.LedgerUpgrade{
		Type:             xdr.LedgerUpgradeTypeLedgerUpgradeVersion,
		NewLedgerVersion: &v,
	})

	val, err := lens.DecodeAs(payload, "LedgerUpgrade")
	if err != nil {
		t.Fatalf("DecodeAs() error = %v", err)
	}

	// Should mention protocol 21 but no parenthetical name.
	if !strings.Contains(val.Node.Note, "21") {
		t.Errorf("root Note %q does not contain protocol number 21", val.Node.Note)
	}
	if strings.Contains(val.Node.Note, "(") {
		t.Errorf("unnamed protocol Note %q should not contain a parenthetical name", val.Node.Note)
	}
}

func TestDecodeProtocolVersionUpgradeSorobanLaunch(t *testing.T) {
	t.Parallel()

	v := xdr.Uint32(20)
	payload := encodeUpgrade(t, xdr.LedgerUpgrade{
		Type:             xdr.LedgerUpgradeTypeLedgerUpgradeVersion,
		NewLedgerVersion: &v,
	})

	val, err := lens.DecodeAs(payload, "LedgerUpgrade")
	if err != nil {
		t.Fatalf("DecodeAs() error = %v", err)
	}

	// Root Note should include "Soroban Launch" for protocol 20.
	if !strings.Contains(val.Node.Note, "Soroban Launch") {
		t.Errorf("root Note %q does not contain 'Soroban Launch'", val.Node.Note)
	}
}
