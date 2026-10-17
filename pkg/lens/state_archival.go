package lens

import (
	"fmt"
	"reflect"
	"time"

	"github.com/stellar/go-stellar-sdk/xdr"
)

// Soroban's state archival keeps the live ledger bounded by giving every
// contract entry a time to live. A TTLEntry records when the entry it belongs
// to expires; a TTL ledger key identifies that record; and the eviction
// iterator is the cursor validators use to walk the bucket list looking for
// entries whose TTL has run out. All three are plain numbers and hashes on
// the wire, so the useful human rendering is naming what the numbers mean.
func init() {
	// A TTL is a ledger sequence, not a duration: rendering the field as the
	// absolute ledger it points at answers "when does this expire" directly.
	fieldNotes["LiveUntilLedgerSeq"] = liveUntilLedgerNote
	nodeHooks[reflect.TypeOf(xdr.TtlEntry{})] = enrichTtlEntry
	nodeHooks[reflect.TypeOf(xdr.LedgerKeyTtl{})] = enrichTtlEntry
}

// LedgerCloseInterval is the protocol's target time between ledger closes.
// Estimates derived from it are approximate: observed Mainnet closes average
// slightly above the target, and the interval has been lowered by protocol
// upgrades, so the mark is labelled as an estimate wherever it is rendered.
const LedgerCloseInterval = 5 * time.Second

// ttlReferenceLedgerSeq and ttlReferenceCloseTime anchor TTL estimates to a
// recorded Mainnet observation: ledger 64,990,097 closed at
// 2026-10-17T11:24:27Z. Both are constants so the estimate is deterministic
// and computed without network access; refresh them only with fresh Horizon
// data, accepting that tests assert on the derived dates.
const ttlReferenceLedgerSeq uint32 = 64_990_097

var ttlReferenceCloseTime = time.Date(2026, time.October, 17, 11, 24, 27, 0, time.UTC)

// TTLExpiryAt estimates the close time of liveUntilLedgerSeq given a reference
// ledger and the time that ledger closed. Ledger sequences advance by the
// target interval, so the offset from the reference is
// (liveUntilLedgerSeq - referenceLedgerSeq) * LedgerCloseInterval.
//
// The estimate drifts from wall-clock time as the reference ages, so callers
// should pass a reference they know is recent — EstimateTTLExpiry uses a
// recorded Mainnet one. A zero liveUntilLedgerSeq means the entry has already
// expired and has no future expiry to estimate; ok is false then.
func TTLExpiryAt(referenceLedgerSeq, liveUntilLedgerSeq uint32, referenceCloseTime time.Time) (time.Time, bool) {
	if liveUntilLedgerSeq == 0 || referenceCloseTime.IsZero() {
		return time.Time{}, false
	}
	delta := int64(liveUntilLedgerSeq) - int64(referenceLedgerSeq)
	// Seconds arithmetic rather than time.Duration: a full uint32 ledger
	// delta can exceed the ~292-year range of a nanosecond Duration.
	seconds := delta * int64(LedgerCloseInterval/time.Second)
	return time.Unix(referenceCloseTime.Unix()+seconds, 0).UTC(), true
}

// EstimateTTLExpiry estimates when an entry that is live through
// liveUntilLedgerSeq expires, using the Mainnet reference ledger close
// recorded in this package. It is offline and deterministic; the result is an
// estimate because real close intervals vary around the target.
func EstimateTTLExpiry(liveUntilLedgerSeq uint32) (time.Time, bool) {
	return TTLExpiryAt(ttlReferenceLedgerSeq, liveUntilLedgerSeq, ttlReferenceCloseTime)
}

// liveUntilLedgerNote renders the TTL field as an expiry in ledger terms plus
// the estimated date that ledger closes. The entry is live through the named
// ledger, so it expires once the ledger sequence passes it. A TTL of zero
// means the entry has already expired — archived if persistent, deleted
// forever if temporary — and has no future date to estimate.
func liveUntilLedgerNote(rv reflect.Value) (string, bool) {
	v, ok := rv.Interface().(xdr.Uint32)
	if !ok {
		return "", false
	}
	if v == 0 {
		return "already expired (evict at next close)", true
	}
	note := fmt.Sprintf("live through ledger %d", uint64(v))
	if expiry, ok := EstimateTTLExpiry(uint32(v)); ok {
		note += fmt.Sprintf(" (~%s)", expiry.Format(time.RFC3339))
	}
	return note, true
}

// enrichTtlEntry annotates a TTL record's KeyHash leaf. The hash is the
// SHA-256 of the tracked entry's LedgerKey. Every TTL entry tracks contract
// code or contract data, but which of the two — and whether contract data is
// persistent or temporary — is not recoverable from the record, so the
// annotation names the set instead of guessing a specific storage type.
func enrichTtlEntry(n *Node, v any) {
	switch entry := v.(type) {
	case xdr.TtlEntry:
		ttlKeyHashNote(n, entry.KeyHash)
	case xdr.LedgerKeyTtl:
		ttlKeyHashNote(n, entry.KeyHash)
	}
}

// ttlKeyHashNote gives the hash a hex string value — the walker's default for
// a Hash is a byte-array dump — plus a note naming what the hash identifies.
func ttlKeyHashNote(n *Node, hash xdr.Hash) {
	kh := n.Child("KeyHash")
	if kh == nil {
		return
	}
	kh.Value = hash.HexString()
	kh.Note = "state archival key hash: contract code or contract data"
}
