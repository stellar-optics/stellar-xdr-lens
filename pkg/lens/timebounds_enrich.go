package lens

import (
	"fmt"
	"reflect"
	"time"

	"github.com/stellar/go-stellar-sdk/xdr"
)

// FormatTimeBoundsDuration produces a human-readable note for an xdr.TimeBounds
// value. It computes the window duration and labels unbounded windows clearly.
//
// Examples:
//   - "Window duration: 300s (5m 0s) [Expires: 2026-11-19T15:30:00Z]"
//   - "No expiration (unbounded max time)"
//   - "Instantaneous window (minTime == maxTime)"
func FormatTimeBoundsDuration(tb xdr.TimeBounds) string {
	minTime := int64(tb.MinTime)
	maxTime := int64(tb.MaxTime)

	if maxTime == 0 {
		return "No expiration (unbounded max time)"
	}
	if minTime == maxTime {
		return "Instantaneous window (minTime == maxTime)"
	}
	if minTime > maxTime {
		return fmt.Sprintf("Invalid window (minTime %s > maxTime %s)",
			formatTimeStr(minTime), formatTimeStr(maxTime))
	}

	dur := time.Duration(maxTime-minTime) * time.Second
	expiry := time.Unix(maxTime, 0).UTC().Format(time.RFC3339)
	return fmt.Sprintf("Window duration: %ds (%s) [Expires: %s]",
		int64(dur.Seconds()), dur.String(), expiry)
}

// FormatLedgerBoundsDuration produces a human-readable note for an
// xdr.LedgerBounds value.
//
// Examples:
//   - "Ledger window: 10 ledgers (~50s at 5s/ledger)"
//   - "No ledger expiration (unbounded max ledger)"
func FormatLedgerBoundsDuration(lb xdr.LedgerBounds) string {
	if lb.MaxLedger == 0 {
		return "No ledger expiration (unbounded max ledger)"
	}
	if lb.MinLedger >= lb.MaxLedger {
		return fmt.Sprintf("Invalid ledger window (minLedger %d >= maxLedger %d)",
			lb.MinLedger, lb.MaxLedger)
	}
	span := uint32(lb.MaxLedger - lb.MinLedger)
	approxSecs := int64(span) * int64(LedgerCloseInterval.Seconds())
	return fmt.Sprintf("Ledger window: %d ledgers (~%ds at 5s/ledger)",
		span, approxSecs)
}

func init() {
	nodeHooks[reflect.TypeOf(xdr.TimeBounds{})] = func(n *Node, v any) {
		tb, ok := v.(xdr.TimeBounds)
		if !ok || n.Note != "" {
			return
		}
		n.Note = FormatTimeBoundsDuration(tb)
	}

	nodeHooks[reflect.TypeOf(xdr.LedgerBounds{})] = func(n *Node, v any) {
		lb, ok := v.(xdr.LedgerBounds)
		if !ok || n.Note != "" {
			return
		}
		n.Note = FormatLedgerBoundsDuration(lb)
	}
}
