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
