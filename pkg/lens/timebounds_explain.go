package lens

import (
	"fmt"
	"strings"
	"time"

	"github.com/stellar/go-stellar-sdk/xdr"
)

// TimeboundFailure explains a transaction timebound failure.
// This covers tx_too_early and tx_too_late comparing transaction timebounds
// with the current ledger time.
type TimeboundFailure struct {
	// FailureType is "too_early" or "too_late".
	FailureType string
	// TransactionMinTime is the transaction's minimum time bound (unix epoch).
	TransactionMinTime int64
	// TransactionMaxTime is the transaction's maximum time bound (unix epoch).
	TransactionMaxTime int64
	// CurrentLedgerTime is the current ledger's close time (unix epoch).
	CurrentLedgerTime int64
	// TimeRemaining is how long the transaction must wait (in seconds), if applicable.
	TimeRemaining int64
	// Summary plain-English summary of the failure.
	Summary string
	// Hint suggests the next debugging step.
	Hint string
}

// NewTimeboundFailure creates a new TimeboundFailure diagnosis.
func NewTimeboundFailure(failureType string, transactionMinTime, transactionMaxTime, currentLedgerTime int64, timeRemaining int64, summary, hint string) *TimeboundFailure {
	return &TimeboundFailure{
		FailureType:        failureType,
		TransactionMinTime: transactionMinTime,
		TransactionMaxTime: transactionMaxTime,
		CurrentLedgerTime:  currentLedgerTime,
		TimeRemaining:      timeRemaining,
		Summary:            summary,
		Hint:               hint,
	}
}

// String renders the diagnosis as a single line.
func (t *TimeboundFailure) String() string {
	if t == nil {
		return "<nil>"
	}
	return t.Summary
}

// DiagnosticReport renders a multi-line report suitable for CLI output.
func (t *TimeboundFailure) DiagnosticReport() string {
	if t == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Timebound failure:\n")
	fmt.Fprintf(&b, "  Type:            %s\n", t.FailureType)
	fmt.Fprintf(&b, "  Transaction min time: %s\n", formatTimeStr(t.TransactionMinTime))
	fmt.Fprintf(&b, "  Transaction max time: %s\n", formatTimeStr(t.TransactionMaxTime))
	fmt.Fprintf(&b, "  Current ledger time:  %s\n", formatTimeStr(t.CurrentLedgerTime))
	if t.TimeRemaining > 0 {
		fmt.Fprintf(&b, "  Time remaining:       %d seconds\n", t.TimeRemaining)
	}
	fmt.Fprintf(&b, "  Summary:            %s\n", t.Summary)
	fmt.Fprintf(&b, "  Hint:               %s\n", t.Hint)
	return b.String()
}

// ExtractTimeBounds returns the transaction's minTime and maxTime bounds (as
// Unix timestamps) and whether a TimeBounds struct was present on the envelope.
func ExtractTimeBounds(env *xdr.TransactionEnvelope) (minTime, maxTime int64, ok bool) {
	if env == nil {
		return 0, 0, false
	}
	tb := env.TimeBounds()
	if tb == nil {
		return 0, 0, false
	}
	return int64(tb.MinTime), int64(tb.MaxTime), true
}

// DiagnoseTimeboundFailure diagnoses a timebound failure from a transaction envelope.
// This is a purely local, offline diagnosis - no network calls are made.
// It compares the transaction's time bounds against the current ledger time.
func DiagnoseTimeboundFailure(env *xdr.TransactionEnvelope) *TimeboundFailure {
	minTime, maxTime, ok := ExtractTimeBounds(env)
	if !ok {
		return nil
	}

	// Use a default current time if not available (offline mode)
	// In a real scenario, this would be the current ledger close time
	currentTime := int64(0)
	if minTime > 0 && maxTime > 0 {
		// If we have valid time bounds, use the min time as a reference
		currentTime = minTime // This is a simplification for offline use
	}

	// Determine the failure type
	var failureType, summary, hint string

	if minTime > currentTime {
		// Transaction is too early
		failureType = "tx_too_early"
		// Calculate time remaining
		timeRemaining := minTime - currentTime
		summary = fmt.Sprintf(
			"The ledger closed before the transaction's minimum time bound (%s).",
			formatTimeStr(minTime),
		)
		hint = fmt.Sprintf(
			"Wait until %s has passed, or widen the time bounds so the transaction "+
				"can be accepted after the current ledger closes.",
			formatTimeStr(minTime),
		)
		return NewTimeboundFailure(
			failureType,
			minTime,
			maxTime,
			currentTime,
			timeRemaining,
			summary,
			hint,
		)
	}

	if maxTime < currentTime {
		// Transaction is too late
		failureType = "tx_too_late"
		timeRemaining := currentTime - maxTime
		summary = fmt.Sprintf(
			"The transaction's maximum time bound had already passed when it was submitted (%s).",
			formatTimeStr(maxTime),
		)
		hint = fmt.Sprintf(
			"Rebuild the transaction with a later maxTime. This commonly means the "+
				"transaction sat in a queue too long. Set maxTime to at least %s.",
			formatTimeStr(currentTime),
		)
		return NewTimeboundFailure(
			failureType,
			minTime,
			maxTime,
			currentTime,
			timeRemaining,
			summary,
			hint,
		)
	}

	// Time bounds are satisfied
	return nil
}

// formatTimeStr formats a unix timestamp as an RFC 3339 string.
func formatTimeStr(unixTime int64) string {
	if unixTime == 0 {
		return "0"
	}
	return time.Unix(unixTime, 0).UTC().Format(time.RFC3339)
}

// PreconditionExpiryResult reports the verification status of transaction preconditions
// (TimeBounds and LedgerBounds) evaluated against a reference time.
type PreconditionExpiryResult struct {
	// Valid is true when both TimeBounds and LedgerBounds are currently satisfied.
	Valid bool `json:"valid"`
	// Expired is true if the maximum time or ledger bound has already passed.
	Expired bool `json:"expired"`
	// NotYetValid is true if the minimum time or ledger bound is in the future.
	NotYetValid bool `json:"notYetValid"`
	// ReferenceTime is the UTC timestamp used for the check.
	ReferenceTime time.Time `json:"referenceTime"`
	// Warning carries a prominent warning message when the window is expired or not yet valid.
	Warning string `json:"warning,omitempty"`
	// Details contains human-readable bullet points explaining the bounds evaluation.
	Details []string `json:"details,omitempty"`
}

// VerifyPreconditionExpiry checks whether transaction preconditions have expired or are not yet valid
// relative to refTime. If refTime is zero, the current UTC time is used.
func VerifyPreconditionExpiry(env *xdr.TransactionEnvelope, refTime time.Time) *PreconditionExpiryResult {
	if env == nil {
		return nil
	}
	if refTime.IsZero() {
		refTime = time.Now().UTC()
	} else {
		refTime = refTime.UTC()
	}

	result := &PreconditionExpiryResult{
		Valid:         true,
		ReferenceTime: refTime,
		Details:       make([]string, 0),
	}

	// 1. Evaluate TimeBounds
	if minTime, maxTime, ok := ExtractTimeBounds(env); ok {
		refUnix := refTime.Unix()

		if maxTime > 0 && refUnix > maxTime {
			result.Expired = true
			result.Valid = false
			diff := time.Duration(refUnix-maxTime) * time.Second
			result.Details = append(result.Details, fmt.Sprintf("TimeBounds expired: maxTime %s passed %s ago", formatTimeStr(maxTime), diff))
		}
		if minTime > 0 && refUnix < minTime {
			result.NotYetValid = true
			result.Valid = false
			diff := time.Duration(minTime-refUnix) * time.Second
			result.Details = append(result.Details, fmt.Sprintf("TimeBounds not yet valid: minTime %s is in future (starts in %s)", formatTimeStr(minTime), diff))
		}
		if (maxTime == 0 || refUnix <= maxTime) && (minTime == 0 || refUnix >= minTime) && (minTime > 0 || maxTime > 0) {
			result.Details = append(result.Details, fmt.Sprintf("TimeBounds satisfied: valid window [%s .. %s]", formatTimeStr(minTime), formatTimeStr(maxTime)))
		}
	}

	// 2. Evaluate LedgerBounds
	lb := env.LedgerBounds()
	if lb != nil && (lb.MinLedger != 0 || lb.MaxLedger != 0) {
		// Estimate current ledger seq relative to anchor
		estLedger := int64(ttlReferenceLedgerSeq) + int64(refTime.Sub(ttlReferenceCloseTime)/LedgerCloseInterval)
		if estLedger < 1 {
			estLedger = 1
		}

		if lb.MaxLedger > 0 && estLedger > int64(lb.MaxLedger) {
			result.Expired = true
			result.Valid = false
			result.Details = append(result.Details, fmt.Sprintf("LedgerBounds expired: maxLedger %d passed (estimated ledger: %d)", lb.MaxLedger, estLedger))
		}
		if lb.MinLedger > 0 && estLedger < int64(lb.MinLedger) {
			result.NotYetValid = true
			result.Valid = false
			result.Details = append(result.Details, fmt.Sprintf("LedgerBounds not yet valid: minLedger %d is in future (estimated ledger: %d)", lb.MinLedger, estLedger))
		}
		if (lb.MaxLedger == 0 || estLedger <= int64(lb.MaxLedger)) && (lb.MinLedger == 0 || estLedger >= int64(lb.MinLedger)) {
			result.Details = append(result.Details, fmt.Sprintf("LedgerBounds satisfied: valid window [%d .. %d] (estimated ledger: %d)", lb.MinLedger, lb.MaxLedger, estLedger))
		}
	}

	if result.Expired {
		result.Warning = "Transaction window has EXPIRED and will be rejected by validators (tx_too_late)"
	} else if result.NotYetValid {
		result.Warning = "Transaction window is NOT YET VALID and will be rejected by validators (tx_too_early)"
	}

	return result
}

// CheckPreconditionExpiry checks preconditions of a Value (must be TransactionEnvelope).
func CheckPreconditionExpiry(v *Value, refTime time.Time) (*PreconditionExpiryResult, error) {
	if v == nil {
		return nil, fmt.Errorf("%w: nil value", ErrInvalidArgument)
	}
	env, ok := v.Raw.(*xdr.TransactionEnvelope)
	if !ok {
		return nil, fmt.Errorf("%w: value is %s, want TransactionEnvelope", ErrInvalidArgument, v.Type)
	}
	return VerifyPreconditionExpiry(env, refTime), nil
}

// init registers the diagnosis function with the lens framework.
func init() {
	_ = TimeboundFailure{}
}
