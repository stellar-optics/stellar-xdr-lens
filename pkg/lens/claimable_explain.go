package lens

import (
	"fmt"
	"math"
	"strings"

	"github.com/stellar/go-stellar-sdk/xdr"
)

// ClaimablePredicateContext supplies the ledger times required to evaluate a
// claim predicate. Both values are Unix seconds; the creation time is the
// close time of the ledger that created the claimable balance.
type ClaimablePredicateContext struct {
	LedgerCloseTime         int64
	BalanceCreatedCloseTime int64
}

// ClaimablePredicateDiagnosis explains whether a claim predicate is currently
// satisfied and lists the conditions preventing a claim when it is not.
type ClaimablePredicateDiagnosis struct {
	Predicate        string   `json:"predicate"`
	Satisfied        bool     `json:"satisfied"`
	Summary          string   `json:"summary"`
	Hint             string   `json:"hint,omitempty"`
	FailedConditions []string `json:"failedConditions,omitempty"`
}

// DiagnoseClaimablePredicate evaluates a nested Stellar claim predicate at
// the supplied ledger close time. Relative predicates use the balance's
// creation-ledger close time. The evaluation is local and deterministic.
func DiagnoseClaimablePredicate(predicate xdr.ClaimPredicate, context ClaimablePredicateContext) (*ClaimablePredicateDiagnosis, error) {
	if context.LedgerCloseTime < 0 || context.BalanceCreatedCloseTime < 0 {
		return nil, fmt.Errorf("%w: claim predicate ledger times must not be negative", ErrInvalidArgument)
	}
	satisfied, failures, err := evaluateClaimPredicate(predicate, context, 0)
	if err != nil {
		return nil, err
	}
	d := &ClaimablePredicateDiagnosis{
		Predicate:        DescribeClaimPredicate(predicate),
		Satisfied:        satisfied,
		FailedConditions: failures,
	}
	if satisfied {
		d.Summary = fmt.Sprintf("Claim predicate %s is satisfied at ledger close time %d.", d.Predicate, context.LedgerCloseTime)
		return d, nil
	}
	d.Summary = "Claim predicate is not satisfied: " + strings.Join(failures, "; ") + "."
	d.Hint = "Wait for the required ledger close time, or claim through another claimant whose predicate is satisfied."
	return d, nil
}

const maxClaimPredicateDepth = 64

func evaluateClaimPredicate(predicate xdr.ClaimPredicate, context ClaimablePredicateContext, depth int) (bool, []string, error) {
	if depth > maxClaimPredicateDepth {
		return false, nil, fmt.Errorf("%w: claim predicate exceeds maximum depth %d", ErrInvalidXDR, maxClaimPredicateDepth)
	}
	switch predicate.Type {
	case xdr.ClaimPredicateTypeClaimPredicateUnconditional:
		return true, nil, nil
	case xdr.ClaimPredicateTypeClaimPredicateBeforeAbsoluteTime:
		if predicate.AbsBefore == nil {
			return false, nil, fmt.Errorf("%w: absolute-time claim predicate has no threshold", ErrInvalidXDR)
		}
		threshold := int64(*predicate.AbsBefore)
		if context.LedgerCloseTime < threshold {
			return true, nil, nil
		}
		return false, []string{fmt.Sprintf("ledger close time %d is not before %d", context.LedgerCloseTime, threshold)}, nil
	case xdr.ClaimPredicateTypeClaimPredicateBeforeRelativeTime:
		if predicate.RelBefore == nil {
			return false, nil, fmt.Errorf("%w: relative-time claim predicate has no duration", ErrInvalidXDR)
		}
		duration := int64(*predicate.RelBefore)
		if duration < 0 || context.BalanceCreatedCloseTime > math.MaxInt64-duration {
			return false, nil, fmt.Errorf("%w: invalid relative-time claim predicate duration", ErrInvalidXDR)
		}
		threshold := context.BalanceCreatedCloseTime + duration
		if context.LedgerCloseTime < threshold {
			return true, nil, nil
		}
		return false, []string{fmt.Sprintf("ledger close time %d is not before creation time %d plus %d seconds", context.LedgerCloseTime, context.BalanceCreatedCloseTime, duration)}, nil
	case xdr.ClaimPredicateTypeClaimPredicateNot:
		if predicate.NotPredicate == nil || *predicate.NotPredicate == nil {
			return false, nil, fmt.Errorf("%w: NOT claim predicate has no child", ErrInvalidXDR)
		}
		childSatisfied, _, err := evaluateClaimPredicate(**predicate.NotPredicate, context, depth+1)
		if err != nil {
			return false, nil, err
		}
		if childSatisfied {
			return false, []string{"the negated condition is true"}, nil
		}
		return true, nil, nil
	case xdr.ClaimPredicateTypeClaimPredicateAnd, xdr.ClaimPredicateTypeClaimPredicateOr:
		var children []xdr.ClaimPredicate
		if predicate.Type == xdr.ClaimPredicateTypeClaimPredicateAnd {
			if predicate.AndPredicates == nil {
				return false, nil, fmt.Errorf("%w: AND claim predicate has no children", ErrInvalidXDR)
			}
			children = *predicate.AndPredicates
		} else {
			if predicate.OrPredicates == nil {
				return false, nil, fmt.Errorf("%w: OR claim predicate has no children", ErrInvalidXDR)
			}
			children = *predicate.OrPredicates
		}
		if len(children) == 0 {
			return false, nil, fmt.Errorf("%w: boolean claim predicate has no children", ErrInvalidXDR)
		}
		allSatisfied := true
		anySatisfied := false
		var failures []string
		for _, child := range children {
			childSatisfied, childFailures, err := evaluateClaimPredicate(child, context, depth+1)
			if err != nil {
				return false, nil, err
			}
			allSatisfied = allSatisfied && childSatisfied
			anySatisfied = anySatisfied || childSatisfied
			if !childSatisfied {
				failures = append(failures, childFailures...)
			}
		}
		if predicate.Type == xdr.ClaimPredicateTypeClaimPredicateAnd {
			return allSatisfied, failures, nil
		}
		if anySatisfied {
			return true, nil, nil
		}
		return false, failures, nil
	default:
		return false, nil, fmt.Errorf("%w: unsupported claim predicate type %s", ErrUnsupportedType, predicate.Type.String())
	}
}

// ValidateClaimPredicateContradiction evaluates whether a claim predicate contains
// mathematical contradictions, impossible time windows, negative/zero relative offsets,
// direct double negations, or permanently unsatisfiable conditions.
// It returns true and a diagnostic reason if a contradiction is detected.
func ValidateClaimPredicateContradiction(predicate xdr.ClaimPredicate) (bool, string) {
	return checkPredicateContradiction(predicate, 0)
}

func checkPredicateContradiction(predicate xdr.ClaimPredicate, depth int) (bool, string) {
	if depth > maxClaimPredicateDepth {
		return true, fmt.Sprintf("claim predicate exceeds maximum depth %d", maxClaimPredicateDepth)
	}

	switch predicate.Type {
	case xdr.ClaimPredicateTypeClaimPredicateUnconditional:
		return false, ""

	case xdr.ClaimPredicateTypeClaimPredicateBeforeAbsoluteTime:
		if predicate.AbsBefore == nil {
			return true, "absolute-time predicate has no threshold"
		}
		if *predicate.AbsBefore <= 0 {
			return true, fmt.Sprintf("absBefore threshold %d is <= 0; epoch time window is unsatisfiable", *predicate.AbsBefore)
		}
		return false, ""

	case xdr.ClaimPredicateTypeClaimPredicateBeforeRelativeTime:
		if predicate.RelBefore == nil {
			return true, "relative-time predicate has no duration"
		}
		if *predicate.RelBefore <= 0 {
			return true, fmt.Sprintf("relBefore duration %d is <= 0; funds cannot be claimed before creation", *predicate.RelBefore)
		}
		return false, ""

	case xdr.ClaimPredicateTypeClaimPredicateNot:
		if predicate.NotPredicate == nil || *predicate.NotPredicate == nil {
			return true, "not predicate has no child"
		}
		child := **predicate.NotPredicate
		// Direct double negation: NOT(NOT(P))
		if child.Type == xdr.ClaimPredicateTypeClaimPredicateNot {
			return true, "direct double negation detected: not(not(...))"
		}
		// Impossible predicate: NOT(unconditional) is always false
		if child.Type == xdr.ClaimPredicateTypeClaimPredicateUnconditional {
			return true, "not(unconditional) is mathematically unsatisfiable"
		}
		return false, ""

	case xdr.ClaimPredicateTypeClaimPredicateAnd:
		if predicate.AndPredicates == nil || len(*predicate.AndPredicates) == 0 {
			return true, "and predicate has no children"
		}
		children := *predicate.AndPredicates

		// Check if any child itself has an inherent contradiction
		for _, child := range children {
			if bad, reason := checkPredicateContradiction(child, depth+1); bad {
				return true, reason
			}
		}

		// Flatten direct conjunctions to analyze conflicting time boundaries
		flat := flattenAndPredicates(children)

		// Check for complementary contradictions P and NOT(P)
		if hasDirectComplementaryConflict(flat) {
			return true, "conflicting conjunction: predicate contains both P and not(P)"
		}

		// Check conflicting absolute time boundaries: absBefore <= absAfter
		// absBefore(B) requires closeTime < B
		// NOT(absBefore(A)) requires closeTime >= A (absAfter)
		var minAbsBefore *int64
		var maxAbsAfter *int64

		var minRelBefore *int64
		var maxRelAfter *int64

		for _, p := range flat {
			switch p.Type {
			case xdr.ClaimPredicateTypeClaimPredicateBeforeAbsoluteTime:
				if p.AbsBefore != nil {
					val := int64(*p.AbsBefore)
					if minAbsBefore == nil || val < *minAbsBefore {
						minAbsBefore = &val
					}
				}
			case xdr.ClaimPredicateTypeClaimPredicateBeforeRelativeTime:
				if p.RelBefore != nil {
					val := int64(*p.RelBefore)
					if minRelBefore == nil || val < *minRelBefore {
						minRelBefore = &val
					}
				}
			case xdr.ClaimPredicateTypeClaimPredicateNot:
				if p.NotPredicate != nil && *p.NotPredicate != nil {
					inner := **p.NotPredicate
					if inner.Type == xdr.ClaimPredicateTypeClaimPredicateBeforeAbsoluteTime && inner.AbsBefore != nil {
						val := int64(*inner.AbsBefore)
						if maxAbsAfter == nil || val > *maxAbsAfter {
							maxAbsAfter = &val
						}
					} else if inner.Type == xdr.ClaimPredicateTypeClaimPredicateBeforeRelativeTime && inner.RelBefore != nil {
						val := int64(*inner.RelBefore)
						if maxRelAfter == nil || val > *maxRelAfter {
							maxRelAfter = &val
						}
					}
				}
			}
		}

		if minAbsBefore != nil && maxAbsAfter != nil && *minAbsBefore <= *maxAbsAfter {
			return true, fmt.Sprintf("conflicting time boundaries: absBefore %d <= absAfter %d", *minAbsBefore, *maxAbsAfter)
		}

		if minRelBefore != nil && maxRelAfter != nil && *minRelBefore <= *maxRelAfter {
			return true, fmt.Sprintf("conflicting relative time boundaries: relBefore %d <= relAfter %d", *minRelBefore, *maxRelAfter)
		}

		return false, ""

	case xdr.ClaimPredicateTypeClaimPredicateOr:
		if predicate.OrPredicates == nil || len(*predicate.OrPredicates) == 0 {
			return true, "or predicate has no children"
		}
		children := *predicate.OrPredicates
		allUnsatisfiable := true
		var lastReason string
		for _, child := range children {
			bad, reason := checkPredicateContradiction(child, depth+1)
			if !bad {
				allUnsatisfiable = false
				break
			}
			lastReason = reason
		}
		if allUnsatisfiable {
			return true, fmt.Sprintf("all branches of or predicate are unsatisfiable: %s", lastReason)
		}
		return false, ""

	default:
		return false, ""
	}
}

func flattenAndPredicates(predicates []xdr.ClaimPredicate) []xdr.ClaimPredicate {
	var result []xdr.ClaimPredicate
	for _, p := range predicates {
		if p.Type == xdr.ClaimPredicateTypeClaimPredicateAnd && p.AndPredicates != nil {
			result = append(result, flattenAndPredicates(*p.AndPredicates)...)
		} else {
			result = append(result, p)
		}
	}
	return result
}

func hasDirectComplementaryConflict(predicates []xdr.ClaimPredicate) bool {
	descMap := make(map[string]bool, len(predicates))
	for _, p := range predicates {
		desc := DescribeClaimPredicate(p)
		descMap[desc] = true
	}
	for _, p := range predicates {
		if p.Type == xdr.ClaimPredicateTypeClaimPredicateNot && p.NotPredicate != nil && *p.NotPredicate != nil {
			innerDesc := DescribeClaimPredicate(**p.NotPredicate)
			if descMap[innerDesc] {
				return true
			}
		}
	}
	return false
}

// ValidateClaimantPredicates inspects all claimants of a claimable balance operation,
// returning warning strings for any claimant whose predicate is mathematically unsatisfiable.
func ValidateClaimantPredicates(claimants []xdr.Claimant) []string {
	var warnings []string
	for _, claimant := range claimants {
		v0, ok := claimant.GetV0()
		if !ok {
			continue
		}
		if bad, _ := ValidateClaimPredicateContradiction(v0.Predicate); bad {
			claimantAddr := v0.Destination.Address()
			warnings = append(warnings, fmt.Sprintf("Warning: Claim predicate for claimant %s is unsatisfiable; funds will be permanently unclaimable.", claimantAddr))
		}
	}
	return warnings
}
