package lens_test

import (
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

const ttlKeyHashNote = "333435363738393a3b3c3d3e3f404142434445464748494a4b4c4d4e4f505152"

func TestDecodeTtlEntry(t *testing.T) {
	t.Parallel()

	v, err := lens.DecodeAs(loadFixture(t, "ttl_entry"), "TtlEntry")
	if err != nil {
		t.Fatalf("DecodeAs() error = %v", err)
	}
	if v.Type != "TtlEntry" {
		t.Errorf("type = %q, want TtlEntry", v.Type)
	}
	raw, ok := v.Raw.(*xdr.TtlEntry)
	if !ok {
		t.Fatalf("Raw is %T, want *xdr.TtlEntry", v.Raw)
	}
	if raw.LiveUntilLedgerSeq != 55281000 {
		t.Errorf("LiveUntilLedgerSeq = %d, want 55281000", raw.LiveUntilLedgerSeq)
	}

	keyHash := v.Node.Find("KeyHash")
	if keyHash == nil || keyHash.Value != ttlKeyHashNote ||
		!strings.Contains(keyHash.Note, "state archival key hash") {
		t.Errorf("KeyHash = %v, want value %q and the storage annotation", keyHash, ttlKeyHashNote)
	}

	// A TTL is an absolute ledger sequence; naming the expiry plus an
	// estimated close date is the whole human-readable value of the field.
	live := v.Node.Find("LiveUntilLedgerSeq")
	if live == nil || live.Note != "live through ledger 55281000 (~2025-04-03T14:33:02Z)" {
		t.Errorf("LiveUntilLedgerSeq = %v, want a live-through note with an estimated date", live)
	}
}

func TestDecodeLedgerKeyTtl(t *testing.T) {
	t.Parallel()

	v, err := lens.DecodeAs(loadFixture(t, "ledger_key_ttl"), "LedgerKeyTtl")
	if err != nil {
		t.Fatalf("DecodeAs() error = %v", err)
	}
	if v.Type != "LedgerKeyTtl" {
		t.Errorf("type = %q, want LedgerKeyTtl", v.Type)
	}
	if _, ok := v.Raw.(*xdr.LedgerKeyTtl); !ok {
		t.Fatalf("Raw is %T, want *xdr.LedgerKeyTtl", v.Raw)
	}
	keyHash := v.Node.Find("KeyHash")
	if keyHash == nil || keyHash.Value != ttlKeyHashNote ||
		!strings.Contains(keyHash.Note, "state archival key hash") {
		t.Errorf("KeyHash = %v, want value %q and the storage annotation", keyHash, ttlKeyHashNote)
	}
}

// TestDecodeTtlInsideLedgerEntry proves TTL records are not only decodable
// standalone: they are ledger entries, and they must render inside the
// LedgerEntry wrapper that carries them in the bucket list.
func TestDecodeTtlInsideLedgerEntry(t *testing.T) {
	t.Parallel()

	v, err := lens.DecodeAs(loadFixture(t, "ledger_entry_ttl"), "LedgerEntry")
	if err != nil {
		t.Fatalf("DecodeAs() error = %v", err)
	}
	dataType := v.Node.Find("Data.Type")
	if dataType == nil || dataType.Value != "LedgerEntryTypeTtl" {
		t.Errorf("Data.Type = %v, want LedgerEntryTypeTtl", dataType)
	}
	live := v.Node.Find("Data.Ttl.LiveUntilLedgerSeq")
	if live == nil || live.Note != "live through ledger 55281000 (~2025-04-03T14:33:02Z)" {
		t.Errorf("Data.Ttl.LiveUntilLedgerSeq = %v, want a live-through note with an estimated date", live)
	}
}

func TestDecodeEvictionIterator(t *testing.T) {
	t.Parallel()

	v, err := lens.DecodeAs(loadFixture(t, "eviction_iterator"), "EvictionIterator")
	if err != nil {
		t.Fatalf("DecodeAs() error = %v", err)
	}
	if v.Type != "EvictionIterator" {
		t.Errorf("type = %q, want EvictionIterator", v.Type)
	}
	if level := v.Node.Find("BucketListLevel"); level == nil || level.Value != uint64(6) {
		t.Errorf("BucketListLevel = %v, want 6", level)
	}
	if cur := v.Node.Find("IsCurrBucket"); cur == nil || cur.Value != true {
		t.Errorf("IsCurrBucket = %v, want true", cur)
	}
	if offset := v.Node.Find("BucketFileOffset"); offset == nil || offset.Value != uint64(1234567890) {
		t.Errorf("BucketFileOffset = %v, want 1234567890", offset)
	}
}

func TestDecodeStateArchivalRejectsTruncatedInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		fixture  string
		typeName string
	}{
		{"ttl_entry", "TtlEntry"},
		{"ledger_key_ttl", "LedgerKeyTtl"},
		{"ledger_entry_ttl", "LedgerEntry"},
		{"eviction_iterator", "EvictionIterator"},
	}

	for _, tc := range tests {
		t.Run(tc.fixture, func(t *testing.T) {
			t.Parallel()

			if _, err := lens.DecodeAs(loadFixtureRaw(tc.fixture)[:16], tc.typeName); err == nil {
				t.Errorf("DecodeAs(%s truncated) error = nil, want an error", tc.fixture)
			}
		})
	}
}

func TestStateArchivalDetectionIsUnambiguous(t *testing.T) {
	t.Parallel()

	tests := []struct {
		fixture  string
		wantType string
	}{
		{"ttl_entry", "TtlEntry"},
		{"ledger_key_ttl", "LedgerKeyTtl"},
		{"ledger_entry_ttl", "LedgerEntry"},
		{"eviction_iterator", "EvictionIterator"},
	}

	for _, tc := range tests {
		t.Run(tc.fixture, func(t *testing.T) {
			t.Parallel()

			cands, err := lens.Detect(loadFixture(t, tc.fixture))
			if err != nil {
				t.Fatalf("Detect() error = %v", err)
			}
			if len(cands) != 1 || cands[0].Type != tc.wantType {
				names := make([]string, 0, len(cands))
				for _, c := range cands {
					names = append(names, c.Type)
				}
				t.Errorf("Detect() = [%s], want exactly [%s]", strings.Join(names, ", "), tc.wantType)
			}
		})
	}
}

func TestDiagnoseStateArchivalOp(t *testing.T) {
	t.Parallel()

	const extendMalformed = "The extend-footprint-TTL operation with extendTo=1000 failed validation: extendTo must be below the network's maximum entry TTL, the read-write footprint must be empty, and the read-only footprint may list only contract data and contract code keys. " +
		"Extending only affects live entries: extendTo must reach past an entry's current live-until ledger to change it, and archived entries are skipped until RestoreFootprint brings them back."
	const restoreMalformed = "The restore-footprint operation failed validation: the read-only footprint must be empty and the read-write footprint may list only persistent contract data and contract code keys; temporary entries can never be restored. " +
		"Restoring only affects archived entries, meaning persistent entries past their live-until ledger. Entries that are still live are skipped, so keep those alive with ExtendFootprintTTL instead."

	tests := []struct {
		name     string
		opType   string
		extendTo uint32
		code     string
		want     string
	}{
		{"extend by short names", "extend_footprint_ttl", 1000, "extend_footprint_ttl_malformed", extendMalformed},
		{"extend by XDR constants", "OperationTypeExtendFootprintTtl", 1000, "ExtendFootprintTtlResultCodeExtendFootprintTtlMalformed", extendMalformed},
		{"restore by short names", "restore_footprint", 0, "restore_footprint_malformed", restoreMalformed},
		{"restore by XDR constants", "OperationTypeRestoreFootprint", 0, "RestoreFootprintResultCodeRestoreFootprintMalformed", restoreMalformed},
		{"success has nothing to diagnose", "restore_footprint", 0, "restore_footprint_success", ""},
		{"code from the other operation", "restore_footprint", 0, "extend_footprint_ttl_malformed", ""},
		{"extend given a restore code", "extend_footprint_ttl", 1000, "restore_footprint_malformed", ""},
		{"unrelated operation", "payment", 0, "restore_footprint_malformed", ""},
		{"unknown code", "restore_footprint", 0, "not_a_code", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := lens.DiagnoseStateArchivalOp(tc.opType, tc.extendTo, tc.code); got != tc.want {
				t.Errorf("DiagnoseStateArchivalOp() =\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}

func TestExplainPairDeepensStateArchivalMalformed(t *testing.T) {
	t.Parallel()

	extend := explainSingleOperation(t,
		xdr.OperationBody{Type: xdr.OperationTypeExtendFootprintTtl, ExtendFootprintTtlOp: &xdr.ExtendFootprintTtlOp{ExtendTo: 1000}},
		xdr.OperationResultTr{
			Type:                     xdr.OperationTypeExtendFootprintTtl,
			ExtendFootprintTtlResult: &xdr.ExtendFootprintTtlResult{Code: xdr.ExtendFootprintTtlResultCodeExtendFootprintTtlMalformed},
		})
	assertReasonContains(t, extend, false, "extendTo=1000", "read-write footprint must be empty")
	if !strings.Contains(extend.Hint, "until RestoreFootprint brings them back") {
		t.Errorf("Hint = %q, want guidance on archived entries", extend.Hint)
	}

	restore := explainSingleOperation(t,
		xdr.OperationBody{Type: xdr.OperationTypeRestoreFootprint, RestoreFootprintOp: &xdr.RestoreFootprintOp{}},
		xdr.OperationResultTr{
			Type:                   xdr.OperationTypeRestoreFootprint,
			RestoreFootprintResult: &xdr.RestoreFootprintResult{Code: xdr.RestoreFootprintResultCodeRestoreFootprintMalformed},
		})
	assertReasonContains(t, restore, false, "read-only footprint must be empty", "temporary entries can never be restored")
	if !strings.Contains(restore.Hint, "ExtendFootprintTTL") {
		t.Errorf("Hint = %q, want guidance on live entries", restore.Hint)
	}
}
