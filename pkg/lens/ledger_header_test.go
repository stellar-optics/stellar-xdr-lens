package lens_test

import (
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

const (
	ledgerHeaderHashNote = "08090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f2021222324252627"
	previousHashNote     = "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"
	closeTimeNote        = "2026-09-21T14:13:20Z"
)

func TestDecodeLedgerHeaderFixture(t *testing.T) {
	t.Parallel()

	v, err := lens.DecodeAs(loadFixture(t, "ledger_header"), "LedgerHeader")
	if err != nil {
		t.Fatalf("DecodeAs() error = %v", err)
	}
	if v.Type != "LedgerHeader" {
		t.Errorf("type = %q, want LedgerHeader", v.Type)
	}
	raw, ok := v.Raw.(*xdr.LedgerHeader)
	if !ok {
		t.Fatalf("Raw is %T, want *xdr.LedgerHeader", v.Raw)
	}
	if raw.LedgerSeq != 55278432 {
		t.Errorf("LedgerSeq = %d, want 55278432", raw.LedgerSeq)
	}
	if raw.LedgerVersion != 22 {
		t.Errorf("LedgerVersion = %d, want 22", raw.LedgerVersion)
	}
}

// TestLedgerHeaderFieldsRenderHumanValues is the heart of the specialised
// rendering: a ledger header's totals and protocol parameters are stroop
// amounts, and reading them as XLM rather than raw integers is the point.
func TestLedgerHeaderFieldsRenderHumanValues(t *testing.T) {
	t.Parallel()

	v, err := lens.DecodeAs(loadFixture(t, "ledger_header"), "LedgerHeader")
	if err != nil {
		t.Fatalf("DecodeAs() error = %v", err)
	}

	tests := []struct {
		path     string
		wantNote string
	}{
		{"LedgerVersion", "protocol 22"},
		{"TotalCoins", "104591297310.9999952 (stroops: 1045912973109999952)"},
		{"FeePool", "1234.0000000 (stroops: 12340000000)"},
		{"BaseFee", "0.0000100 (stroops: 100)"},
		{"BaseReserve", "0.5000000 (stroops: 5000000)"},
		{"ScpValue.CloseTime", closeTimeNote},
		{"PreviousLedgerHash", previousHashNote},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()

			n := v.Node.Find(tc.path)
			if n == nil {
				t.Fatalf("could not find %s in the decoded tree", tc.path)
			}
			if n.Note != tc.wantNote {
				t.Errorf("%s note = %q, want %q", tc.path, n.Note, tc.wantNote)
			}
		})
	}
}

func TestDecodeLedgerHeaderHistoryEntryFixture(t *testing.T) {
	t.Parallel()

	v, err := lens.DecodeAs(loadFixture(t, "ledger_header_history"), "LedgerHeaderHistoryEntry")
	if err != nil {
		t.Fatalf("DecodeAs() error = %v", err)
	}
	if v.Type != "LedgerHeaderHistoryEntry" {
		t.Errorf("type = %q, want LedgerHeaderHistoryEntry", v.Type)
	}
	raw, ok := v.Raw.(*xdr.LedgerHeaderHistoryEntry)
	if !ok {
		t.Fatalf("Raw is %T, want *xdr.LedgerHeaderHistoryEntry", v.Raw)
	}
	if raw.Header.LedgerSeq != 55278432 {
		t.Errorf("Header.LedgerSeq = %d, want 55278432", raw.Header.LedgerSeq)
	}

	hash := v.Node.Find("Hash")
	if hash == nil {
		t.Fatal("could not find Hash in the decoded history entry")
	}
	if !strings.Contains(hash.Note, ledgerHeaderHashNote) {
		t.Errorf("Hash note = %q, want it to contain %q", hash.Note, ledgerHeaderHashNote)
	}

	// The embedded header gets the same field renderings as a standalone one.
	seq := v.Node.Find("Header.LedgerSeq")
	if seq == nil {
		t.Fatal("could not find Header.LedgerSeq in the decoded history entry")
	}
	if got, ok := seq.Value.(uint64); !ok || got != 55278432 {
		t.Errorf("Header.LedgerSeq = %v, want 55278432", seq.Value)
	}
	total := v.Node.Find("Header.TotalCoins")
	if total == nil || !strings.Contains(total.Note, "104591297310.9999952") {
		t.Errorf("Header.TotalCoins = %v, want a stroop rendering", total)
	}
}

// TestLedgerHeaderFlagsAndUpgradeVotes builds a header carrying V1
// liquidity-pool control flags and two upgrade votes, then decodes it through
// the public API. This is the enrichment the canonical fixture does not
// exercise: flags expanded to names, and opaque upgrade blobs decoded into
// the protocol parameter each proposes.
func TestLedgerHeaderFlagsAndUpgradeVotes(t *testing.T) {
	t.Parallel()

	versionVote := xdr.LedgerUpgrade{
		Type:             xdr.LedgerUpgradeTypeLedgerUpgradeVersion,
		NewLedgerVersion: ptrToUint32(23),
	}
	baseReserveVote := xdr.LedgerUpgrade{
		Type:           xdr.LedgerUpgradeTypeLedgerUpgradeBaseReserve,
		NewBaseReserve: ptrToUint32(10000000),
	}
	versionBlob, err := versionVote.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary(version) error = %v", err)
	}
	reserveBlob, err := baseReserveVote.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary(baseReserve) error = %v", err)
	}

	header := xdr.LedgerHeader{
		LedgerVersion: 23,
		PreviousLedgerHash: xdr.Hash{
			0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
			0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
			0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
			0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20,
		},
		ScpValue: xdr.StellarValue{
			TxSetHash: xdr.Hash{
				0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28,
				0x29, 0x2a, 0x2b, 0x2c, 0x2d, 0x2e, 0x2f, 0x30,
				0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38,
				0x39, 0x3a, 0x3b, 0x3c, 0x3d, 0x3e, 0x3f, 0x40,
			},
			CloseTime: xdr.TimePoint(1700000000),
			Upgrades:  []xdr.UpgradeType{versionBlob, reserveBlob},
			Ext:       xdr.StellarValueExt{V: 0},
		},
		TxSetResultHash: xdr.Hash{
			0x41, 0x42, 0x43, 0x44, 0x45, 0x46, 0x47, 0x48,
			0x49, 0x4a, 0x4b, 0x4c, 0x4d, 0x4e, 0x4f, 0x50,
			0x51, 0x52, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58,
			0x59, 0x5a, 0x5b, 0x5c, 0x5d, 0x5e, 0x5f, 0x60,
		},
		BucketListHash: xdr.Hash{
			0x61, 0x62, 0x63, 0x64, 0x65, 0x66, 0x67, 0x68,
			0x69, 0x6a, 0x6b, 0x6c, 0x6d, 0x6e, 0x6f, 0x70,
			0x71, 0x72, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78,
			0x79, 0x7a, 0x7b, 0x7c, 0x7d, 0x7e, 0x7f, 0x80,
		},
		LedgerSeq:    1000,
		TotalCoins:   xdr.Int64(100000000000000000),
		FeePool:      xdr.Int64(100000000),
		BaseFee:      100,
		BaseReserve:  5000000,
		MaxTxSetSize: 1000,
		Ext: xdr.LedgerHeaderExt{
			V: 1,
			V1: &xdr.LedgerHeaderExtensionV1{
				Flags: xdr.Uint32(
					xdr.LedgerHeaderFlagsDisableLiquidityPoolTradingFlag |
						xdr.LedgerHeaderFlagsDisableLiquidityPoolDepositFlag),
			},
		},
	}

	v, err := lens.DecodeAs(mustMarshal(t, header), "LedgerHeader")
	if err != nil {
		t.Fatalf("DecodeAs() error = %v", err)
	}

	baseReserve := v.Node.Find("BaseReserve")
	if baseReserve == nil || baseReserve.Note != "0.5000000 (stroops: 5000000)" {
		t.Errorf("BaseReserve = %v, want the XLM decimal rendering", baseReserve)
	}

	flags := v.Node.Find("Ext.V1.Flags")
	if flags == nil {
		t.Fatal("Ext.V1.Flags not found")
	}
	if flags.Value != uint64(xdr.Uint32(
		xdr.LedgerHeaderFlagsDisableLiquidityPoolTradingFlag|
			xdr.LedgerHeaderFlagsDisableLiquidityPoolDepositFlag)) {
		t.Errorf("Flags Value = %v, want the raw bitmask", flags.Value)
	}
	if flags.Note != "disable_liquidity_pool_trading | disable_liquidity_pool_deposit" {
		t.Errorf("Flags Note = %q, want expanded flag names", flags.Note)
	}

	upgrades := v.Node.Find("ScpValue.Upgrades")
	if upgrades == nil || len(upgrades.Children) != 2 {
		t.Fatalf("ScpValue.Upgrades = %v, want 2 votes", upgrades)
	}
	if note := upgrades.Children[0].Note; note != "protocol version 23" {
		t.Errorf("Upgrades[0] Note = %q, want %q", note, "protocol version 23")
	}
	if note := upgrades.Children[1].Note; note != "base reserve 1.0000000 XLM (10000000 stroops)" {
		t.Errorf("Upgrades[1] Note = %q, want the base reserve proposal", note)
	}

	// The raw vote bytes remain available for machine-readable output.
	if b, ok := upgrades.Children[0].Value.(xdr.UpgradeType); !ok || string(b) != string(versionBlob) {
		t.Errorf("Upgrades[0] Value = %v, want the original upgrade blob", upgrades.Children[0].Value)
	}
}

func TestDecodeLedgerHeaderRejectsTruncatedInput(t *testing.T) {
	t.Parallel()

	if _, err := lens.DecodeAs(loadFixtureRaw("ledger_header")[:64], "LedgerHeader"); err == nil {
		t.Error("DecodeAs() error = nil, want an error for a truncated ledger header")
	}
	if _, err := lens.DecodeAs(loadFixtureRaw("ledger_header_history")[:64], "LedgerHeaderHistoryEntry"); err == nil {
		t.Error("DecodeAs() error = nil, want an error for a truncated history entry")
	}
}

// TestDetectReportsLedgerHeader guards that the registered ledger types are
// unambiguous for a full payload: a header must not be mistaken for any other
// type in the registry, which is what makes `lens decode` trustworthy.
func TestDetectReportsLedgerHeader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		fixture  string
		wantType string
	}{
		{"ledger_header", "LedgerHeader"},
		{"ledger_header_history", "LedgerHeaderHistoryEntry"},
	}

	for _, tc := range tests {
		t.Run(tc.fixture, func(t *testing.T) {
			t.Parallel()

			cands, err := lens.Detect(loadFixture(t, tc.fixture))
			if err != nil {
				t.Fatalf("Detect() error = %v", err)
			}
			if len(cands) != 1 {
				names := make([]string, 0, len(cands))
				for _, c := range cands {
					names = append(names, c.Type)
				}
				t.Errorf("Detect() matched %d types (%s), want exactly 1",
					len(cands), strings.Join(names, ", "))
			}
			if cands[0].Type != tc.wantType {
				t.Errorf("Detect() best candidate = %q, want %q", cands[0].Type, tc.wantType)
			}
		})
	}
}

func ptrToUint32(v uint32) *xdr.Uint32 {
	u := xdr.Uint32(v)
	return &u
}
