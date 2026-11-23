package lens

import (
	"fmt"
	"reflect"
	"time"

	"github.com/stellar/go-stellar-sdk/xdr"
)

// DiagnosticEventTimestamp correlates a Soroban DiagnosticEvent with the
// wall-clock time at which its enclosing ledger closed. The timestamp is not
// embedded in the event itself; it lives in the ledger header and must be
// supplied by the caller.
type DiagnosticEventTimestamp struct {
	// LedgerSeq is the ledger sequence that closed with this event.
	LedgerSeq uint32
	// CloseTime is the UTC wall-clock time at which that ledger closed.
	CloseTime time.Time
	// Note is the formatted enrichment string ready to attach to a Node.
	Note string
}

// String returns the formatted Note.
func (d *DiagnosticEventTimestamp) String() string {
	if d == nil {
		return "<nil>"
	}
	return d.Note
}

// EnrichDiagnosticEventWithTimestamp formats a timestamp-correlation note for
// a DiagnosticEvent given its enclosing ledger sequence and Unix close time.
// It returns nil when closeTime is zero (unknown ledger context).
func EnrichDiagnosticEventWithTimestamp(event xdr.DiagnosticEvent, ledgerSeq uint32, closeTime int64) *DiagnosticEventTimestamp {
	if closeTime == 0 {
		return nil
	}
	t := time.Unix(closeTime, 0).UTC()
	return &DiagnosticEventTimestamp{
		LedgerSeq: ledgerSeq,
		CloseTime: t,
		Note:      fmt.Sprintf("Emitted at %s (Ledger #%d)", t.Format(time.RFC3339), ledgerSeq),
	}
}

func init() {
	// A nodeHook for DiagnosticEvent adds a quick-read note describing whether
	// the event was emitted inside a successful or failed contract call. This
	// note is set without ledger context; callers who also have the ledger
	// close time should call EnrichDiagnosticEventWithTimestamp and attach the
	// returned Note to the node themselves.
	nodeHooks[reflect.TypeOf(xdr.DiagnosticEvent{})] = func(n *Node, v any) {
		event, ok := v.(xdr.DiagnosticEvent)
		if !ok || n.Note != "" {
			return
		}
		if event.InSuccessfulContractCall {
			n.Note = "emitted in successful contract call"
		} else {
			n.Note = "emitted in failed contract call"
		}
	}
}
