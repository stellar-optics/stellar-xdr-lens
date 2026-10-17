package lens

import (
	"reflect"
	"testing"
	"time"

	"github.com/stellar/go-stellar-sdk/xdr"
)

func TestLiveUntilLedgerNote(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		value    any
		wantNote string
		wantOK   bool
	}{
		{"live entry", xdr.Uint32(55281000), "live through ledger 55281000 (~2025-04-03T14:33:02Z)", true},
		{"expired entry", xdr.Uint32(0), "already expired (evict at next close)", true},
		{"wrong type declines", xdr.Int64(1), "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			note, ok := liveUntilLedgerNote(reflect.ValueOf(tc.value))
			if ok != tc.wantOK || note != tc.wantNote {
				t.Errorf("liveUntilLedgerNote(%v) = (%q, %v), want (%q, %v)",
					tc.value, note, ok, tc.wantNote, tc.wantOK)
			}
		})
	}
}

// TestLiveUntilLedgerNoteReachesTheWalker pins that the note is registered
// under the field name the XDR actually uses, so the registration cannot
// silently drift from the schema.
func TestLiveUntilLedgerNoteReachesTheWalker(t *testing.T) {
	t.Parallel()

	entry := xdr.TtlEntry{KeyHash: xdr.Hash{}, LiveUntilLedgerSeq: 1000}
	node := buildNode("TtlEntry", reflect.ValueOf(entry), 0)
	if node == nil {
		t.Fatal("buildNode(TtlEntry) returned nil")
	}
	live := node.Child("LiveUntilLedgerSeq")
	if live == nil {
		t.Fatal("could not find LiveUntilLedgerSeq on the built node")
	}
	want := "live through ledger 1000 (~2016-06-30T12:46:22Z)"
	if live.Note != want {
		t.Errorf("LiveUntilLedgerSeq note = %q, want %q", live.Note, want)
	}
}

func TestTTLExpiryAt(t *testing.T) {
	t.Parallel()

	reference := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		reference uint32
		liveUntil uint32
		want      time.Time
		wantOK    bool
	}{
		{"same ledger closes now", 100, 100, reference, true},
		{"next ledger closes in one interval", 100, 101, reference.Add(5 * time.Second), true},
		{"past ledger closed before the reference", 100, 99, reference.Add(-5 * time.Second), true},
		{"zero has no future expiry", 100, 0, time.Time{}, false},
		{"max uint32 does not overflow", 0, ^uint32(0), time.Unix(reference.Unix()+int64(^uint32(0))*5, 0).UTC(), true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := TTLExpiryAt(tc.reference, tc.liveUntil, reference)
			if ok != tc.wantOK {
				t.Fatalf("TTLExpiryAt ok = %v, want %v", ok, tc.wantOK)
			}
			if ok && !got.Equal(tc.want) {
				t.Errorf("TTLExpiryAt = %s, want %s", got, tc.want)
			}
		})
	}

	if _, ok := TTLExpiryAt(100, 200, time.Time{}); ok {
		t.Error("TTLExpiryAt accepted a zero reference close time")
	}
}

func TestEstimateTTLExpiry(t *testing.T) {
	t.Parallel()

	anchor := time.Date(2026, time.October, 17, 11, 24, 27, 0, time.UTC)

	if got, ok := EstimateTTLExpiry(ttlReferenceLedgerSeq); !ok || !got.Equal(anchor) {
		t.Errorf("EstimateTTLExpiry(anchor) = (%s, %v), want (%s, true)", got, ok, anchor)
	}
	if got, ok := EstimateTTLExpiry(ttlReferenceLedgerSeq + 1); !ok || !got.Equal(anchor.Add(5*time.Second)) {
		t.Errorf("EstimateTTLExpiry(anchor+1) = (%s, %v), want %s", got, ok, anchor.Add(5*time.Second))
	}
	if _, ok := EstimateTTLExpiry(0); ok {
		t.Error("EstimateTTLExpiry(0) returned an estimate for an expired entry")
	}
}

func TestEnrichTtlEntry(t *testing.T) {
	t.Parallel()

	const wantHashHex = "0100000000000000000000000000000000000000000000000000000000000000"
	const wantHashNote = "state archival key hash: contract code or contract data"

	entry := xdr.TtlEntry{
		KeyHash:            xdr.Hash{0x01},
		LiveUntilLedgerSeq: 55281000,
	}
	node := buildNode("TtlEntry", reflect.ValueOf(entry), 0)
	if node == nil {
		t.Fatal("buildNode(TtlEntry) returned nil")
	}
	keyHash := node.Child("KeyHash")
	if keyHash == nil {
		t.Fatal("KeyHash child not found")
	}
	if keyHash.Value != wantHashHex {
		t.Errorf("KeyHash value = %v, want hex %q", keyHash.Value, wantHashHex)
	}
	if keyHash.Note != wantHashNote {
		t.Errorf("KeyHash note = %q, want %q", keyHash.Note, wantHashNote)
	}

	key := xdr.LedgerKeyTtl{KeyHash: xdr.Hash{0x01}}
	keyNode := buildNode("LedgerKeyTtl", reflect.ValueOf(key), 0)
	if keyNode == nil {
		t.Fatal("buildNode(LedgerKeyTtl) returned nil")
	}
	if kh := keyNode.Child("KeyHash"); kh == nil || kh.Value != wantHashHex || kh.Note != wantHashNote {
		t.Errorf("LedgerKeyTtl KeyHash = %v, want value %q and note %q", keyNode.Child("KeyHash"), wantHashHex, wantHashNote)
	}
}

// TestEnrichTtlEntryToleratesMissingChild pins that the hook cannot panic the
// walker on a node without the KeyHash child, and ignores unrelated types.
func TestEnrichTtlEntryToleratesMissingChild(t *testing.T) {
	t.Parallel()

	enrichTtlEntry(&Node{}, xdr.TtlEntry{KeyHash: xdr.Hash{0x01}})
	enrichTtlEntry(&Node{}, xdr.LedgerKeyTtl{KeyHash: xdr.Hash{0x01}})
	enrichTtlEntry(&Node{}, "not a ttl record")
}
