package lens

import (
	"reflect"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"
)

func TestEnrichBucketEntryType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		value  xdr.BucketEntryType
		want   string
		wantOK bool
	}{
		{"metadata", xdr.BucketEntryTypeMetaentry, "metadata entry", true},
		{"live", xdr.BucketEntryTypeLiveentry, "live entry", true},
		{"init", xdr.BucketEntryTypeInitentry, "new entry", true},
		{"dead tombstone", xdr.BucketEntryTypeDeadentry, "dead entry (tombstone)", true},
		{"unknown declines", xdr.BucketEntryType(7), "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			display, raw, ok := enrichBucketEntryType(reflect.ValueOf(tc.value))
			if ok != tc.wantOK || (tc.wantOK && raw != tc.want) {
				t.Errorf("enrichBucketEntryType(%d) = (%q, %v, %v), want (_, %q, %v)",
					tc.value, display, raw, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestEnrichBucketListType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value  xdr.BucketListType
		want   string
		wantOK bool
	}{
		{xdr.BucketListTypeLive, "live", true},
		{xdr.BucketListTypeHotArchive, "hot archive", true},
		{xdr.BucketListType(9), "", false},
	}

	for _, tc := range tests {
		_, raw, ok := enrichBucketListType(reflect.ValueOf(tc.value))
		if ok != tc.wantOK || (tc.wantOK && raw != tc.want) {
			t.Errorf("enrichBucketListType(%d) = (_, %v, %v), want (_, %q, %v)",
				tc.value, raw, ok, tc.want, tc.wantOK)
		}
	}
}

func TestEnrichHotArchiveBucketEntryType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value  xdr.HotArchiveBucketEntryType
		want   string
		wantOK bool
	}{
		{xdr.HotArchiveBucketEntryTypeHotArchiveMetaentry, "metadata entry", true},
		{xdr.HotArchiveBucketEntryTypeHotArchiveArchived, "archived entry", true},
		{xdr.HotArchiveBucketEntryTypeHotArchiveLive, "restored key", true},
		{xdr.HotArchiveBucketEntryType(9), "", false},
	}

	for _, tc := range tests {
		_, raw, ok := enrichHotArchiveBucketEntryType(reflect.ValueOf(tc.value))
		if ok != tc.wantOK || (tc.wantOK && raw != tc.want) {
			t.Errorf("enrichHotArchiveBucketEntryType(%d) = (_, %v, %v), want (_, %q, %v)",
				tc.value, raw, ok, tc.want, tc.wantOK)
		}
	}
}

func TestEnrichLedgerEntryChangeType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		value  xdr.LedgerEntryChangeType
		want   string
		wantOK bool
	}{
		{"created", xdr.LedgerEntryChangeTypeLedgerEntryCreated, "created", true},
		{"updated", xdr.LedgerEntryChangeTypeLedgerEntryUpdated, "updated", true},
		{"removed", xdr.LedgerEntryChangeTypeLedgerEntryRemoved, "deleted (removed)", true},
		{"state", xdr.LedgerEntryChangeTypeLedgerEntryState, "state (snapshot)", true},
		{"restored", xdr.LedgerEntryChangeTypeLedgerEntryRestored, "restored from archive", true},
		{"unknown declines", xdr.LedgerEntryChangeType(99), "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			display, raw, ok := enrichLedgerEntryChangeType(reflect.ValueOf(tc.value))
			if ok != tc.wantOK || (tc.wantOK && raw != tc.want) {
				t.Errorf("enrichLedgerEntryChangeType(%d) = (%q, %v, %v), want (_, %q, %v)",
					tc.value, display, raw, ok, tc.want, tc.wantOK)
			}
		})
	}
}

// TestBucketEntryEnrichersDeclineUnexpectedTypes pins that each enricher is
// safe with a value it does not own — the registry is keyed by type, but the
// interface assertions are the last line of defence.
func TestBucketEntryEnrichersDeclineUnexpectedTypes(t *testing.T) {
	t.Parallel()

	wrong := reflect.ValueOf(xdr.AccountId{})
	for name, fn := range map[string]Enricher{
		"enrichBucketEntryType":           enrichBucketEntryType,
		"enrichBucketListType":            enrichBucketListType,
		"enrichHotArchiveBucketEntryType": enrichHotArchiveBucketEntryType,
		"enrichLedgerEntryChangeType":     enrichLedgerEntryChangeType,
	} {
		if display, raw, ok := fn(wrong); ok {
			t.Errorf("%s(AccountId) = (%q, %v, true), want ok=false", name, display, raw)
		}
	}
}
