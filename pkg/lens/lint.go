package lens

import (
	"fmt"
	"time"

	"github.com/stellar/go-stellar-sdk/xdr"
)

// LintSeverity classifies how serious a static analysis finding is.
type LintSeverity string

const (
	// LintSeverityError indicates a defect that will cause the transaction to
	// fail or behave unexpectedly on-chain.
	LintSeverityError LintSeverity = "error"
	// LintSeverityWarning indicates a suspicious, wasteful, or unsafe pattern.
	LintSeverityWarning LintSeverity = "warning"
	// LintSeverityInfo indicates an informational best-practice suggestion.
	LintSeverityInfo LintSeverity = "info"
)

// LintFinding describes a single issue reported by a static analysis rule.
type LintFinding struct {
	// Rule is the stable identifier of the lint rule that produced this finding.
	Rule string `json:"rule"`
	// Severity is the severity level (error, warning, info).
	Severity LintSeverity `json:"severity"`
	// OpIndex is the 0-based operation index associated with the finding, or -1
	// when the finding applies to the transaction as a whole.
	OpIndex int `json:"op_index"`
	// Message explains what was detected.
	Message string `json:"message"`
	// Suggestion offers actionable remediation guidance.
	Suggestion string `json:"suggestion,omitempty"`
}

// LintTransaction runs all static analysis rules against a transaction envelope
// using the current wall-clock time for time-sensitive checks.
func LintTransaction(env *xdr.TransactionEnvelope) []LintFinding {
	return LintTransactionAt(env, time.Now().UTC())
}

// LintTransactionAt runs all static analysis rules against a transaction
// envelope using now as the reference time (falling back to time.Now().UTC()
// when now is zero).
func LintTransactionAt(env *xdr.TransactionEnvelope, now time.Time) []LintFinding {
	if env == nil {
		return nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var findings []LintFinding
	findings = append(findings, lintTimeBounds(env, now)...)
	return findings
}

// LintValue runs transaction lint rules against a decoded Value.
// The value must hold a TransactionEnvelope.
func LintValue(v *Value) ([]LintFinding, error) {
	if v == nil {
		return nil, fmt.Errorf("%w: nil value", ErrInvalidArgument)
	}
	env, ok := v.Raw.(*xdr.TransactionEnvelope)
	if !ok {
		return nil, fmt.Errorf("%w: value is %s, want TransactionEnvelope", ErrInvalidArgument, v.Type)
	}
	return LintTransaction(env), nil
}

// lintTimeBounds checks transaction preconditions for unbounded (maxTime == 0
// or missing TimeBounds) and already-expired (maxTime < now) time bounds.
func lintTimeBounds(env *xdr.TransactionEnvelope, now time.Time) []LintFinding {
	_, maxTime, ok := ExtractTimeBounds(env)
	if !ok || maxTime == 0 {
		return []LintFinding{{
			Rule:       "time-bounds",
			Severity:   LintSeverityWarning,
			OpIndex:    -1,
			Message:    "transaction has unbounded timebounds (maxTime is 0 or unset) and remains valid indefinitely",
			Suggestion: "Set a finite maxTime expiration in the transaction preconditions to prevent stale submission.",
		}}
	}

	if maxTime < now.Unix() {
		return []LintFinding{{
			Rule:       "time-bounds",
			Severity:   LintSeverityError,
			OpIndex:    -1,
			Message:    fmt.Sprintf("transaction timebounds already expired at %s (maxTime %d < current time %d)", formatTimeStr(maxTime), maxTime, now.Unix()),
			Suggestion: "Rebuild and re-sign the transaction with a future maxTime before submitting.",
		}}
	}

	return nil
}
