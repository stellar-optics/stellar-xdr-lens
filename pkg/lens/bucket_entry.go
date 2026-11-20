package lens

import (
	"reflect"

	"github.com/stellar/go-stellar-sdk/xdr"
)

// Bucket entries are the unit of the bucket list, the hash tree a validator
// keeps of all ledger state. A bucket entry is either a live ledger entry, a
// dead entry that records a key as tombstoned, or metadata describing the
// bucket itself. The archival bucket list introduced by Protocol 23 has its
// own entry type, whose live arm is a key that has moved back to the live
// bucket list.
//
// The generated enum constants are accurate but read like identifiers
// (BucketEntryTypeDeadentry). This file registers renderings in the terms the
// protocol documentation uses, so a tombstone is recognisable at a glance.
func init() {
	enricherFor[reflect.TypeOf(xdr.BucketEntryType(0))] = enrichBucketEntryType
	enricherFor[reflect.TypeOf(xdr.BucketListType(0))] = enrichBucketListType
	enricherFor[reflect.TypeOf(xdr.HotArchiveBucketEntryType(0))] = enrichHotArchiveBucketEntryType
	enricherFor[reflect.TypeOf(xdr.LedgerEntryChangeType(0))] = enrichLedgerEntryChangeType
}

// enrichBucketEntryType names the state each bucket entry records. DEADENTRY
// is called out as a tombstone because that is what it is: the key is gone
// from the live ledger state, and the entry exists only to say so. An unknown
// discriminant declines and keeps the generic enum rendering.
func enrichBucketEntryType(rv reflect.Value) (string, any, bool) {
	t, ok := rv.Interface().(xdr.BucketEntryType)
	if !ok {
		return "", nil, false
	}
	switch t {
	case xdr.BucketEntryTypeMetaentry:
		return "", "metadata entry", true
	case xdr.BucketEntryTypeLiveentry:
		return "", "live entry", true
	case xdr.BucketEntryTypeInitentry:
		return "", "new entry", true
	case xdr.BucketEntryTypeDeadentry:
		return "", "dead entry (tombstone)", true
	default:
		return "", nil, false
	}
}

// enrichBucketListType names which bucket list a metadata entry describes:
// the live state, or the hot archive that holds evicted persistent entries.
func enrichBucketListType(rv reflect.Value) (string, any, bool) {
	t, ok := rv.Interface().(xdr.BucketListType)
	if !ok {
		return "", nil, false
	}
	switch t {
	case xdr.BucketListTypeLive:
		return "", "live", true
	case xdr.BucketListTypeHotArchive:
		return "", "hot archive", true
	default:
		return "", nil, false
	}
}

// enrichHotArchiveBucketEntryType names the state of an archival bucket
// entry. HOT_ARCHIVE_LIVE means the entry was archived and has since been
// restored to the live bucket list; the entry in the archive is a key, not
// the value, which is why it reads as a key rather than an entry.
func enrichHotArchiveBucketEntryType(rv reflect.Value) (string, any, bool) {
	t, ok := rv.Interface().(xdr.HotArchiveBucketEntryType)
	if !ok {
		return "", nil, false
	}
	switch t {
	case xdr.HotArchiveBucketEntryTypeHotArchiveMetaentry:
		return "", "metadata entry", true
	case xdr.HotArchiveBucketEntryTypeHotArchiveArchived:
		return "", "archived entry", true
	case xdr.HotArchiveBucketEntryTypeHotArchiveLive:
		return "", "restored key", true
	default:
		return "", nil, false
	}
}

// enrichLedgerEntryChangeType names the transition type of a ledger change:
// created, updated, removed/deleted, state snapshot, or restored from archive.
// This clarifies LedgerCloseMeta and TransactionMeta lifecycle transitions
// for ledger indexers (#346).
func enrichLedgerEntryChangeType(rv reflect.Value) (string, any, bool) {
	t, ok := rv.Interface().(xdr.LedgerEntryChangeType)
	if !ok {
		return "", nil, false
	}
	switch t {
	case xdr.LedgerEntryChangeTypeLedgerEntryCreated:
		return "", "created", true
	case xdr.LedgerEntryChangeTypeLedgerEntryUpdated:
		return "", "updated", true
	case xdr.LedgerEntryChangeTypeLedgerEntryRemoved:
		return "", "deleted (removed)", true
	case xdr.LedgerEntryChangeTypeLedgerEntryState:
		return "", "state (snapshot)", true
	case xdr.LedgerEntryChangeTypeLedgerEntryRestored:
		return "", "restored from archive", true
	default:
		return "", nil, false
	}
}
