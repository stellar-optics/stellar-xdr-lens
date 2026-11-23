package lens_test

import (
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

func TestFormatTimeBoundsDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		tb       xdr.TimeBounds
		contains []string
	}{
		{
			name:     "bounded window",
			tb:       xdr.TimeBounds{MinTime: 1_700_000_000, MaxTime: 1_700_000_300},
			contains: []string{"300s", "5m", "Expires:"},
		},
		{
			name:     "unbounded max time",
			tb:       xdr.TimeBounds{MinTime: 1_700_000_000, MaxTime: 0},
			contains: []string{"No expiration", "unbounded"},
		},
		{
			name:     "instantaneous window",
			tb:       xdr.TimeBounds{MinTime: 1_700_000_000, MaxTime: 1_700_000_000},
			contains: []string{"Instantaneous"},
		},
		{
			name:     "invalid window",
			tb:       xdr.TimeBounds{MinTime: 1_700_000_300, MaxTime: 1_700_000_000},
			contains: []string{"Invalid"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := lens.FormatTimeBoundsDuration(tc.tb)
			for _, want := range tc.contains {
				if !strings.Contains(got, want) {
					t.Errorf("FormatTimeBoundsDuration() = %q, want it to contain %q", got, want)
				}
			}
		})
	}
}

func TestFormatLedgerBoundsDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		lb       xdr.LedgerBounds
		contains []string
	}{
		{
			name:     "bounded window",
			lb:       xdr.LedgerBounds{MinLedger: 100, MaxLedger: 110},
			contains: []string{"10 ledgers", "50s"},
		},
		{
			name:     "unbounded max ledger",
			lb:       xdr.LedgerBounds{MinLedger: 100, MaxLedger: 0},
			contains: []string{"No ledger expiration", "unbounded"},
		},
		{
			name:     "invalid window",
			lb:       xdr.LedgerBounds{MinLedger: 110, MaxLedger: 100},
			contains: []string{"Invalid"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := lens.FormatLedgerBoundsDuration(tc.lb)
			for _, want := range tc.contains {
				if !strings.Contains(got, want) {
					t.Errorf("FormatLedgerBoundsDuration() = %q, want it to contain %q", got, want)
				}
			}
		})
	}
}
