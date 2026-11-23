package lens

import (
	"fmt"
	"sort"
	"strings"

	"github.com/stellar/go-stellar-sdk/xdr"
)

// Static analysis for transaction envelopes.
//
// Lint runs entirely offline: it inspects the decoded envelope and reports
// conditions that would make Stellar Core reject the transaction before it
// ever reaches a ledger, or that indicate a likely authoring mistake. It does
// not simulate execution, so a clean result means "no known antipattern was
// found", not "this transaction will succeed".
//
// Rules are deliberately small, independent functions registered in one slice
// so that adding a check never requires touching the others. The engine itself
// only orders and returns their findings.

// DefaultBaseFee is the network base fee in stroops (one stroop is 1e-7 XLM).
// Stellar Core requires fee >= op_count * base_fee.
const DefaultBaseFee uint64 = 100

// LintSeverity classifies a finding.
type LintSeverity string

const (
	// LintError marks a condition that will make the transaction invalid.
	LintError LintSeverity = "ERROR"
	// LintWarning marks a suspicious condition that may still be valid.
	LintWarning LintSeverity = "WARN"
)

// LintIssue is one finding produced by the linter.
type LintIssue struct {
	// Rule is the stable identifier of the rule that produced the finding.
	Rule string `json:"rule"`
	// Severity is ERROR or WARN.
	Severity LintSeverity `json:"severity"`
	// Path locates the finding within the envelope, using the same dotted
	// notation as the decoder and differ (e.g. "V1.Tx.Operations[0]").
	Path string `json:"path"`
	// Line is a 1-based position in the decoded transaction: 1 for
	// transaction-level findings, or the 1-based operation index for
	// findings attached to an operation.
	Line int `json:"line"`
	// Message is the plain-language description of the problem.
	Message string `json:"message"`
	// Hint is the remediation advice.
	Hint string `json:"hint"`
}

// String renders the issue as a single line, matching the CLI layout.
func (i LintIssue) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "line %d: %s %s", i.Line, i.Severity, i.Rule)
	if i.Path != "" {
		fmt.Fprintf(&b, " (%s)", i.Path)
	}
	fmt.Fprintf(&b, ": %s", i.Message)
	if i.Hint != "" {
		fmt.Fprintf(&b, " — %s", i.Hint)
	}
	return b.String()
}

// LintRule is one named static check.
type LintRule struct {
	// Name is the stable identifier used in LintIssue.Rule.
	Name string
	// Description explains what the rule detects.
	Description string
	// Check inspects an envelope and returns zero or more findings.
	Check func(env *xdr.TransactionEnvelope) []LintIssue
}

// HasErrors reports whether any finding is an error.
func HasLintErrors(issues []LintIssue) bool {
	for _, issue := range issues {
		if issue.Severity == LintError {
			return true
		}
	}
	return false
}

// lintRules is the rule registry. Order defines the tie-break order of
// findings that share a line, so output stays deterministic.
var lintRules = []LintRule{
	{
		Name:        "invalid-envelope",
		Description: "the envelope is nil or has no populated variant",
		Check:       lintInvalidEnvelope,
	},
	{
		Name:        "zero-fee-bid",
		Description: "the transaction bids a zero fee",
		Check:       lintZeroFeeBid,
	},
	{
		Name:        "fee-below-minimum",
		Description: "the fee bid is below base fee times operation count",
		Check:       lintFeeSufficiency,
	},
	{
		Name:        "fee-bump-fee-too-low",
		Description: "the fee-bump fee is below the inner transaction fee",
		Check:       lintFeeBumpMinimum,
	},
	{
		Name:        "missing-signatures",
		Description: "the envelope carries no signatures",
		Check:       lintMissingSignatures,
	},
	{
		Name:        "inverted-timebounds",
		Description: "a time or ledger bound has max before min",
		Check:       lintInvertedBounds,
	},
	{
		Name:        "unreachable-operation",
		Description: "an operation follows an account merge",
		Check:       lintUnreachableOperations,
	},
}

// LintRules returns a copy of the registered rules, for documentation,
// introspection and tests.
func LintRules() []LintRule {
	rules := make([]LintRule, len(lintRules))
	copy(rules, lintRules)
	return rules
}

// RegisterLintRule adds a rule to the registry. It is intended for init-time
// extension, like the enricher registry, and panics on an empty or duplicate
// name so a wiring mistake fails loudly at startup rather than silently.
func RegisterLintRule(rule LintRule) {
	if rule.Name == "" {
		panic("lens: RegisterLintRule called with an empty name")
	}
	if rule.Check == nil {
		panic("lens: RegisterLintRule called with a nil Check for " + rule.Name)
	}
	for _, existing := range lintRules {
		if existing.Name == rule.Name {
			panic("lens: duplicate lint rule " + rule.Name)
		}
	}
	lintRules = append(lintRules, rule)
}

// LintEnvelope runs every registered rule against env and returns the findings
// in deterministic order: by line, then severity, then rule name.
func LintEnvelope(env *xdr.TransactionEnvelope) []LintIssue {
	var issues []LintIssue
	for _, rule := range lintRules {
		issues = append(issues, rule.Check(env)...)
	}
	sort.SliceStable(issues, func(i, j int) bool {
		if issues[i].Line != issues[j].Line {
			return issues[i].Line < issues[j].Line
		}
		if issues[i].Severity != issues[j].Severity {
			return issues[i].Severity < issues[j].Severity
		}
		return issues[i].Rule < issues[j].Rule
	})
	return issues
}

// envelopeVariant identifies the populated arm of a TransactionEnvelope union.
type envelopeVariant string

const (
	variantInvalid envelopeVariant = ""
	variantV0      envelopeVariant = "V0"
	variantV1      envelopeVariant = "V1"
	variantFeeBump envelopeVariant = "FeeBump"
)

// variant reports which arm of the envelope union is populated. It checks the
// pointer rather than trusting Type, so a half-built value cannot cause a nil
// dereference below.
func envelopeArm(env *xdr.TransactionEnvelope) envelopeVariant {
	if env == nil {
		return variantInvalid
	}
	switch {
	case env.V1 != nil:
		return variantV1
	case env.FeeBump != nil && env.FeeBump.Tx.InnerTx.V1 != nil:
		return variantFeeBump
	case env.V0 != nil:
		return variantV0
	default:
		return variantInvalid
	}
}

func lintInvalidEnvelope(env *xdr.TransactionEnvelope) []LintIssue {
	if envelopeArm(env) != variantInvalid {
		return nil
	}
	return []LintIssue{{
		Rule:     "invalid-envelope",
		Severity: LintError,
		Line:     1,
		Message:  "the envelope has no populated transaction variant",
		Hint:     "decode the payload with `lens decode --type TransactionEnvelope` to see which union arm is missing",
	}}
}

// feePath returns the path of the effective (inner for fee bumps) fee field.
func feePath(env *xdr.TransactionEnvelope) string {
	switch envelopeArm(env) {
	case variantV1:
		return "V1.Tx.Fee"
	case variantV0:
		return "V0.Tx.Fee"
	case variantFeeBump:
		return "FeeBump.Tx.InnerTx.V1.Tx.Fee"
	default:
		return ""
	}
}

func envelopeOperations(env *xdr.TransactionEnvelope) []xdr.Operation {
	switch envelopeArm(env) {
	case variantV1:
		return env.V1.Tx.Operations
	case variantV0:
		return env.V0.Tx.Operations
	case variantFeeBump:
		return env.FeeBump.Tx.InnerTx.V1.Tx.Operations
	default:
		return nil
	}
}

// envelopeFee returns the effective (inner for fee bumps) fee bid.
func envelopeFee(env *xdr.TransactionEnvelope) uint64 {
	switch envelopeArm(env) {
	case variantV1:
		return uint64(env.V1.Tx.Fee)
	case variantV0:
		return uint64(env.V0.Tx.Fee)
	case variantFeeBump:
		return uint64(env.FeeBump.Tx.InnerTx.V1.Tx.Fee)
	default:
		return 0
	}
}

func lintZeroFeeBid(env *xdr.TransactionEnvelope) []LintIssue {
	if envelopeArm(env) == variantInvalid {
		return nil
	}
	if envelopeFee(env) != 0 {
		return nil
	}
	return []LintIssue{{
		Rule:     "zero-fee-bid",
		Severity: LintError,
		Path:     feePath(env),
		Line:     1,
		Message:  "the transaction bids a zero fee",
		Hint:     "Stellar Core rejects zero-fee transactions; set the fee to at least the number of operations times the base fee (100 stroops)",
	}}
}

// lintFeeSufficiency implements issue #325: fee >= op_count * base fee.
func lintFeeSufficiency(env *xdr.TransactionEnvelope) []LintIssue {
	if envelopeArm(env) == variantInvalid {
		return nil
	}
	ops := envelopeOperations(env)
	fee := envelopeFee(env)
	if fee == 0 {
		// The zero-fee rule already reports the strongest form of this
		// finding, so do not duplicate it here.
		return nil
	}
	minFee := uint64(len(ops)) * DefaultBaseFee
	if fee >= minFee {
		return nil
	}
	return []LintIssue{{
		Rule:     "fee-below-minimum",
		Severity: LintWarning,
		Path:     feePath(env),
		Line:     1,
		Message: fmt.Sprintf(
			"fee bid of %d stroops is below the minimum of %d stroops for %d operation(s) at the %d stroop base fee",
			fee, minFee, len(ops), DefaultBaseFee,
		),
		Hint: fmt.Sprintf("set the fee to at least %d stroops (operation count times %d)", minFee, DefaultBaseFee),
	}}
}

// lintFeeBumpMinimum verifies that the outer fee bid covers the inner fee.
// Stellar Core requires the fee-bump fee to be at least the inner fee so the
// fee source funds the whole transaction.
func lintFeeBumpMinimum(env *xdr.TransactionEnvelope) []LintIssue {
	if envelopeArm(env) != variantFeeBump {
		return nil
	}
	outer := int64(env.FeeBump.Tx.Fee)
	inner := uint64(env.FeeBump.Tx.InnerTx.V1.Tx.Fee)
	if outer >= 0 && uint64(outer) >= inner {
		return nil
	}
	return []LintIssue{{
		Rule:     "fee-bump-fee-too-low",
		Severity: LintError,
		Path:     "FeeBump.Tx.Fee",
		Line:     1,
		Message: fmt.Sprintf(
			"fee-bump fee bid of %d stroops is below the inner transaction fee of %d stroops",
			outer, inner,
		),
		Hint: "raise the fee-bump fee to at least the inner transaction fee so the fee source covers it",
	}}
}

// lintMissingSignatures checks both the outer and inner signature sets of a
// fee-bump envelope, since Core requires the inner transaction to be signed
// even though the outer fee source sponsors it.
func lintMissingSignatures(env *xdr.TransactionEnvelope) []LintIssue {
	var issues []LintIssue
	add := func(path, what string) {
		issues = append(issues, LintIssue{
			Rule:     "missing-signatures",
			Severity: LintError,
			Path:     path,
			Line:     1,
			Message:  "the envelope has no " + what + " signatures",
			Hint:     "sign the transaction hash with every required key before submitting it",
		})
	}
	switch envelopeArm(env) {
	case variantV1:
		if len(env.V1.Signatures) == 0 {
			add("V1.Signatures", "transaction")
		}
	case variantV0:
		if len(env.V0.Signatures) == 0 {
			add("V0.Signatures", "transaction")
		}
	case variantFeeBump:
		if len(env.FeeBump.Signatures) == 0 {
			add("FeeBump.Signatures", "fee-bump")
		}
		if len(env.FeeBump.Tx.InnerTx.V1.Signatures) == 0 {
			add("FeeBump.Tx.InnerTx.V1.Signatures", "inner transaction")
		}
	}
	return issues
}

// lintInvertedBounds flags preconditions whose maximum precedes their minimum.
// A zero MaxTime or MaxLedger means "no upper bound", so it is not compared.
func lintInvertedBounds(env *xdr.TransactionEnvelope) []LintIssue {
	if envelopeArm(env) == variantInvalid {
		return nil
	}
	var issues []LintIssue

	if tb := env.TimeBounds(); tb != nil {
		if uint64(tb.MaxTime) != 0 && tb.MinTime > tb.MaxTime {
			issues = append(issues, LintIssue{
				Rule:     "inverted-timebounds",
				Severity: LintError,
				Path:     "TimeBounds",
				Line:     1,
				Message: fmt.Sprintf(
					"time bounds are inverted: minTime %d is after maxTime %d",
					tb.MinTime, tb.MaxTime,
				),
				Hint: "set minTime <= maxTime, or set maxTime to 0 to remove the upper bound",
			})
		}
	}

	if lb := env.LedgerBounds(); lb != nil {
		if uint32(lb.MaxLedger) != 0 && lb.MinLedger > lb.MaxLedger {
			issues = append(issues, LintIssue{
				Rule:     "inverted-timebounds",
				Severity: LintError,
				Path:     "LedgerBounds",
				Line:     1,
				Message: fmt.Sprintf(
					"ledger bounds are inverted: minLedger %d is after maxLedger %d",
					lb.MinLedger, lb.MaxLedger,
				),
				Hint: "set minLedger <= maxLedger, or set maxLedger to 0 to remove the upper bound",
			})
		}
	}

	return issues
}

// lintUnreachableOperations reports operations that follow an account merge.
// A merged source account no longer exists, so later operations that debit it
// cannot execute; Core treats the transaction as malformed. The finding is a
// warning because the merge may target a different account than the source,
// in which case only the author can say whether the ordering was intended.
func lintUnreachableOperations(env *xdr.TransactionEnvelope) []LintIssue {
	ops := envelopeOperations(env)
	var issues []LintIssue
	mergedAt := -1
	for i, op := range ops {
		if mergedAt >= 0 {
			issues = append(issues, LintIssue{
				Rule:     "unreachable-operation",
				Severity: LintWarning,
				Path:     fmt.Sprintf("Operations[%d]", i),
				Line:     i + 1,
				Message: fmt.Sprintf(
					"operation %d follows the account merge at operation %d and may never execute",
					i, mergedAt,
				),
				Hint: "place ACCOUNT_MERGE last, or remove the operations after it",
			})
			continue
		}
		if op.Body.Type == xdr.OperationTypeAccountMerge {
			mergedAt = i
		}
	}
	return issues
}
