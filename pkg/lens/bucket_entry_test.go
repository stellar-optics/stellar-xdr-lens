package lens_test

import (
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

const (
	bucketAccountAddr = "GAIBCEQTCQKRMFYYDENBWHA5DYPSAIJCEMSCKJRHFAUSUKZMFUXC75MW"
	bucketKeyHashNote = "4445464748494a4b4c4d4e4f505152535455565758595a5b5c5d5e5f60616263"
)

func TestDecodeBucketEntryLive(t *testing.T) {
	t.Parallel()

	v, err := lens.DecodeAs(loadFixture(t, "bucket_entry_live"), "BucketEntry")
	if err != nil {
		t.Fatalf("DecodeAs() error = %v", err)
	}
	if v.Type != "BucketEntry" {
		t.Errorf("type = %q, want BucketEntry", v.Type)
	}
	raw, ok := v.Raw.(*xdr.BucketEntry)
	if !ok {
		t.Fatalf("Raw is %T, want *xdr.BucketEntry", v.Raw)
	}
	if raw.Type != xdr.BucketEntryTypeLiveentry {
		t.Errorf("Type = %v, want LIVEENTRY", raw.Type)
	}
	if raw.LiveEntry == nil {
		t.Fatal("LiveEntry = nil, want the wrapped ledger entry")
	}

	// The dead-arm label is the point of the specialised rendering: a raw
	// enum constant is not how anyone refers to a bucket entry.
	typ := v.Node.Find("Type")
	if typ == nil {
		t.Fatal("could not find Type in the decoded bucket entry")
	}
	if got, ok := typ.Value.(string); !ok || got != "live entry" {
		t.Errorf("Type = %v, want %q", typ.Value, "live entry")
	}

	// A live entry carries a ledger entry, rendered with the usual account
	// and amount enrichment.
	dataType := v.Node.Find("LiveEntry.Data.Type")
	if dataType == nil {
		t.Fatal("could not find LiveEntry.Data.Type")
	}
	if got, ok := dataType.Value.(string); !ok || got != "LedgerEntryTypeAccount" {
		t.Errorf("LiveEntry.Data.Type = %v, want LedgerEntryTypeAccount", dataType.Value)
	}
	acct := v.Node.Find("LiveEntry.Data.Account.AccountId")
	if acct == nil {
		t.Fatal("could not find LiveEntry.Data.Account.AccountId")
	}
	if got, ok := acct.Value.(string); !ok || got != bucketAccountAddr {
		t.Errorf("AccountId = %v, want %q", acct.Value, bucketAccountAddr)
	}
	balance := v.Node.Find("LiveEntry.Data.Account.Balance")
	if balance == nil || !strings.Contains(balance.Note, "1000.0000000") {
		t.Errorf("Balance = %v, want a stroop rendering", balance)
	}
}

// TestDecodeBucketEntryDead is the tombstone case: the entry records that a
// key was removed, and the key is a TTL key from the state-archival layer.
func TestDecodeBucketEntryDead(t *testing.T) {
	t.Parallel()

	v, err := lens.DecodeAs(loadFixture(t, "bucket_entry_dead"), "BucketEntry")
	if err != nil {
		t.Fatalf("DecodeAs() error = %v", err)
	}
	raw, ok := v.Raw.(*xdr.BucketEntry)
	if !ok {
		t.Fatalf("Raw is %T, want *xdr.BucketEntry", v.Raw)
	}
	if raw.Type != xdr.BucketEntryTypeDeadentry {
		t.Errorf("Type = %v, want DEADENTRY", raw.Type)
	}

	typ := v.Node.Find("Type")
	if typ == nil || typ.Value != "dead entry (tombstone)" {
		t.Errorf("Type = %v, want %q", typ, "dead entry (tombstone)")
	}

	keyType := v.Node.Find("DeadEntry.Type")
	if keyType == nil {
		t.Fatal("could not find DeadEntry.Type")
	}
	if got, ok := keyType.Value.(string); !ok || got != "LedgerEntryTypeTtl" {
		t.Errorf("DeadEntry.Type = %v, want LedgerEntryTypeTtl", keyType.Value)
	}
	keyHash := v.Node.Find("DeadEntry.Ttl.KeyHash")
	if keyHash == nil || keyHash.Value != bucketKeyHashNote {
		t.Errorf("DeadEntry.Ttl.KeyHash = %v, want value %q", keyHash, bucketKeyHashNote)
	}
}

func TestDecodeBucketEntryMeta(t *testing.T) {
	t.Parallel()

	v, err := lens.DecodeAs(loadFixture(t, "bucket_entry_meta"), "BucketEntry")
	if err != nil {
		t.Fatalf("DecodeAs() error = %v", err)
	}
	raw, ok := v.Raw.(*xdr.BucketEntry)
	if !ok {
		t.Fatalf("Raw is %T, want *xdr.BucketEntry", v.Raw)
	}
	if raw.Type != xdr.BucketEntryTypeMetaentry {
		t.Errorf("Type = %v, want METAENTRY", raw.Type)
	}

	if typ := v.Node.Find("Type"); typ == nil || typ.Value != "metadata entry" {
		t.Errorf("Type = %v, want %q", typ, "metadata entry")
	}
	if version := v.Node.Find("MetaEntry.LedgerVersion"); version == nil || version.Note != "protocol 22" {
		t.Errorf("MetaEntry.LedgerVersion = %v, want a protocol 22 note", version)
	}
	if list := v.Node.Find("MetaEntry.Ext.BucketListType"); list == nil || list.Value != "live" {
		t.Errorf("MetaEntry.Ext.BucketListType = %v, want %q", list, "live")
	}
}

func TestDecodeBucketMetadata(t *testing.T) {
	t.Parallel()

	v, err := lens.DecodeAs(loadFixture(t, "bucket_metadata"), "BucketMetadata")
	if err != nil {
		t.Fatalf("DecodeAs() error = %v", err)
	}
	if v.Type != "BucketMetadata" {
		t.Errorf("type = %q, want BucketMetadata", v.Type)
	}
	if version := v.Node.Find("LedgerVersion"); version == nil || version.Note != "protocol 22" {
		t.Errorf("LedgerVersion = %v, want a protocol 22 note", version)
	}
	if list := v.Node.Find("Ext.BucketListType"); list == nil || list.Value != "live" {
		t.Errorf("Ext.BucketListType = %v, want %q", list, "live")
	}
}

func TestDecodeHotArchiveBucketEntry(t *testing.T) {
	t.Parallel()

	// The hot archive entry shares its wire encoding with BucketEntry, so the
	// type must be pinned explicitly.
	v, err := lens.DecodeAs(loadFixture(t, "hot_archive_bucket_entry"), "HotArchiveBucketEntry")
	if err != nil {
		t.Fatalf("DecodeAs() error = %v", err)
	}
	if v.Type != "HotArchiveBucketEntry" {
		t.Errorf("type = %q, want HotArchiveBucketEntry", v.Type)
	}
	if typ := v.Node.Find("Type"); typ == nil || typ.Value != "archived entry" {
		t.Errorf("Type = %v, want %q", typ, "archived entry")
	}
	if acct := v.Node.Find("ArchivedEntry.Data.Account.AccountId"); acct == nil || acct.Value != bucketAccountAddr {
		t.Errorf("ArchivedEntry.Data.Account.AccountId = %v, want %q", acct, bucketAccountAddr)
	}
}

// TestBucketEntryDetectionDocumentsHotArchiveAmbiguity records the one honest
// ambiguity in this area: a hot-archive bucket entry is byte-identical to the
// live bucket entry of the corresponding arm, so detection cannot tell them
// apart and ranks BucketEntry first. Callers that know they hold an archive
// entry use --type.
func TestBucketEntryDetectionDocumentsHotArchiveAmbiguity(t *testing.T) {
	t.Parallel()

	cands, err := lens.Detect(loadFixture(t, "hot_archive_bucket_entry"))
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	names := make(map[string]bool, len(cands))
	for _, c := range cands {
		names[c.Type] = true
	}
	for _, want := range []string{"BucketEntry", "HotArchiveBucketEntry"} {
		if !names[want] {
			t.Errorf("Detect() did not report %s as a candidate", want)
		}
	}
	if cands[0].Type != "BucketEntry" {
		t.Errorf("Detect() best candidate = %q, want BucketEntry", cands[0].Type)
	}
}

func TestDecodeBucketEntryRejectsTruncatedInput(t *testing.T) {
	t.Parallel()

	fixtures := []struct {
		fixture  string
		typeName string
	}{
		{"bucket_entry_live", "BucketEntry"},
		{"bucket_entry_dead", "BucketEntry"},
		{"bucket_entry_meta", "BucketEntry"},
		{"bucket_metadata", "BucketMetadata"},
		{"hot_archive_bucket_entry", "HotArchiveBucketEntry"},
	}

	for _, tc := range fixtures {
		t.Run(tc.fixture, func(t *testing.T) {
			t.Parallel()

			if _, err := lens.DecodeAs(loadFixtureRaw(tc.fixture)[:8], tc.typeName); err == nil {
				t.Errorf("DecodeAs(%s truncated) error = nil, want an error", tc.fixture)
			}
		})
	}
}
