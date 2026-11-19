package lens

import (
	"fmt"
	"math"
	"strings"

	"github.com/stellar/go-stellar-sdk/amount"
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

// DiagnoseClawbackFailure explains a failed Clawback or ClawbackClaimableBalance
// operation, pointing at the issuer's AUTH_CLAWBACK_ENABLED_FLAG requirement
// where that is the cause. code is the result code in either spelling: the XDR
// constant or the short form, such as "clawback_not_clawback_enabled".
//
// For a Clawback, fromAccount is the account being clawed back from. A
// ClawbackClaimableBalance has no such account and carries no asset or amount,
// so for those codes fromAccount holds the balance id and the other arguments
// are ignored. The diagnosis is built from these arguments alone, without any
// ledger or network access, and is "" for a code it does not explain.
func DiagnoseClawbackFailure(fromAccount string, asset xdr.Asset, clawbackAmount xdr.Int64, code string) string {
	switch code {
	case "ClawbackResultCodeClawbackMalformed", "clawback_malformed":
		assetName, issuer := clawbackAssetLabels(asset)
		return fmt.Sprintf("the clawback of %s %s from %s is malformed: the source must be %s, the from account must be a different account, and the amount must be positive.",
			amount.String(clawbackAmount), assetName, clawbackAccountLabel(fromAccount), issuer)

	case "ClawbackResultCodeClawbackNotClawbackEnabled", "clawback_not_clawback_enabled":
		assetName, issuer := clawbackAssetLabels(asset)
		return fmt.Sprintf("the trustline %s holds for %s does not have clawback enabled, so %s of it cannot be clawed back. The protocol requires %s to set AUTH_CLAWBACK_ENABLED_FLAG, together with AUTH_REVOCABLE_FLAG, on its account before funds are issued: a trustline only inherits clawback when it is created, so an existing trustline must be removed and opened again after the flag is set.",
			clawbackAccountLabel(fromAccount), assetName, amount.String(clawbackAmount), issuer)

	case "ClawbackResultCodeClawbackNoTrust", "clawback_no_trust":
		assetName, issuer := clawbackAssetLabels(asset)
		return fmt.Sprintf("%s has no trustline for %s, so there is no balance to claw back. Check the from account and asset; a trustline opened from now on can only be clawed back if %s has AUTH_CLAWBACK_ENABLED_FLAG set when it is created.",
			clawbackAccountLabel(fromAccount), assetName, issuer)

	case "ClawbackResultCodeClawbackUnderfunded", "clawback_underfunded":
		assetName, _ := clawbackAssetLabels(asset)
		return fmt.Sprintf("%s does not have %s %s available to claw back: the amount must fit within its trustline balance after selling liabilities. Lower the amount to what the account holds.",
			clawbackAccountLabel(fromAccount), amount.String(clawbackAmount), assetName)

	case "ClawbackClaimableBalanceResultCodeClawbackClaimableBalanceNotClawbackEnabled", "clawback_claimable_balance_not_clawback_enabled":
		return fmt.Sprintf("%s does not have clawback enabled. A claimable balance must have clawback enabled upon creation: it inherits CLAIMABLE_BALANCE_CLAWBACK_ENABLED_FLAG from a clawback-enabled trustline, or from an issuer with AUTH_CLAWBACK_ENABLED_FLAG set, and the flag cannot be added afterwards.",
			claimableBalanceLabel(fromAccount))

	case "ClawbackClaimableBalanceResultCodeClawbackClaimableBalanceNotIssuer", "clawback_claimable_balance_not_issuer":
		return fmt.Sprintf("the source account did not issue the asset held by %s. Only the asset issuer can claw back a claimable balance, so submit the operation from the issuer account.",
			claimableBalanceLabel(fromAccount))

	case "ClawbackClaimableBalanceResultCodeClawbackClaimableBalanceDoesNotExist", "clawback_claimable_balance_does_not_exist":
		return fmt.Sprintf("%s does not exist on this network. It may already have been claimed or clawed back, or the id may belong to another network.",
			claimableBalanceLabel(fromAccount))
	}
	return ""
}

// clawbackAssetLabels names an asset and its issuer for clawback prose. An asset
// without an issuer, which clawback cannot apply to, degrades to generic wording.
func clawbackAssetLabels(asset xdr.Asset) (assetName, issuer string) {
	code, address, ok := creditAssetParts(asset)
	if !ok {
		return "the asset", "the asset issuer"
	}
	return code, "issuer " + address
}

// clawbackAccountLabel names the account a clawback targets.
func clawbackAccountLabel(account string) string {
	if account == "" {
		return "the from account"
	}
	return account
}

// claimableBalanceLabel names a claimable balance by id when one is known.
func claimableBalanceLabel(balanceID string) string {
	if balanceID == "" {
		return "the claimable balance"
	}
	return "claimable balance " + balanceID
}

// claimableBalanceIDLabel renders a balance id as its B... strkey, or "" when the
// id has no populated arm to encode.
func claimableBalanceIDLabel(id xdr.ClaimableBalanceId) string {
	if id.Type != xdr.ClaimableBalanceIdTypeClaimableBalanceIdTypeV0 || id.V0 == nil {
		return ""
	}
	encoded, err := id.EncodeToStrkey()
	if err != nil {
		return ""
	}
	return encoded
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
