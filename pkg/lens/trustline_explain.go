package lens

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/stellar/go-stellar-sdk/amount"
	"github.com/stellar/go-stellar-sdk/xdr"
)

// TrustlineFailure explains a trustline-related failure.
// This covers op_no_trust and op_not_authorized errors, and provides
// step-by-step guidance on creating and fixing trustlines.
type TrustlineFailure struct {
	// FailureType is "no_trust" or "not_authorized".
	FailureType string
	// OperationIndex is the index of the failing operation.
	OperationIndex int
	// AssetCode is the asset the trustline is missing for, or the asset in question.
	AssetCode string
	// AssetIssuer is the issuer of the asset, when applicable.
	AssetIssuer string
	// Summary plain-English summary of the failure.
	Summary string
	// Hint suggests step-by-step guidance for fixing the issue.
	Hint string
}

// NewTrustlineFailure creates a new TrustlineFailure diagnosis.
func NewTrustlineFailure(failureType string, operationIndex int, assetCode, assetIssuer, summary, hint string) *TrustlineFailure {
	return &TrustlineFailure{
		FailureType:    failureType,
		OperationIndex: operationIndex,
		AssetCode:      assetCode,
		AssetIssuer:    assetIssuer,
		Summary:        summary,
		Hint:           hint,
	}
}

// String renders the diagnosis as a single line.
func (t *TrustlineFailure) String() string {
	if t == nil {
		return "<nil>"
	}
	return t.Summary
}

// DiagnosticReport renders a multi-line report suitable for CLI output.
func (t *TrustlineFailure) DiagnosticReport() string {
	if t == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Trustline failure:\n")
	fmt.Fprintf(&b, "  Type:          %s\n", t.FailureType)
	fmt.Fprintf(&b, "  Operation:     %d\n", t.OperationIndex)
	fmt.Fprintf(&b, "  Asset:         %s", t.AssetCode)
	if t.AssetIssuer != "" {
		fmt.Fprintf(&b, " (%s)", t.AssetIssuer)
	}
	fmt.Fprintf(&b, "\n  Summary:       %s\n", t.Summary)
	fmt.Fprintf(&b, "  Hint:          %s\n", t.Hint)
	return b.String()
}

// DiagnoseTrustlineFailure diagnoses a trustline failure from an operation result.
// This is a purely local, offline diagnosis - no network calls are made.
//
// It handles trustline failures by examining the operation result code.
// It identifies two failure types:
//  1. op_no_trust: The account has no trustline for the requested asset
//  2. op_not_authorized: The account is not authorized to transact this asset
func DiagnoseTrustlineFailure(opResult xdr.OperationResult, operationIndex int) *TrustlineFailure {
	// Use ExplainCode to get the plain-English reason from the result code
	reason := ExplainCode(opResult.Code.String())

	// Determine the failure type based on the reason summary
	switch {
	case strings.Contains(reason.Summary, "no trustline") ||
		strings.Contains(reason.Summary, "has no trustline") ||
		strings.Contains(reason.Summary, "no trust for") ||
		strings.Contains(opResult.Code.String(), "NoTrust") ||
		strings.Contains(opResult.Code.String(), "no_trust"):
		// Extract asset info from the operation result node tree
		node := buildNode("", reflect.ValueOf(opResult), 0)
		assetCode := "unknown asset"
		assetIssuer := ""

		if node != nil {
			if assetNode := node.Child("Asset"); assetNode != nil {
				if codeNode := assetNode.Child("Code"); codeNode != nil {
					if code, ok := codeNode.Value.(string); ok {
						assetCode = code
					}
				}
			}

			if issuerNode := node.Child("Issuer"); issuerNode != nil {
				if issuer, ok := issuerNode.Value.(string); ok {
					assetIssuer = issuer
				}
			}
		}

		return NewTrustlineFailure(
			"no_trust",
			operationIndex,
			assetCode,
			assetIssuer,
			reason.Summary,
			"Create a trustline for this asset using the change-trust operation "+
				"before attempting the transaction.",
		)

	case strings.Contains(reason.Summary, "not authorised") ||
		strings.Contains(reason.Summary, "not authorized") ||
		strings.Contains(reason.Summary, "not authorised to hold") ||
		strings.Contains(reason.Summary, "not authorized to transact") ||
		strings.Contains(opResult.Code.String(), "NotAuthorized") ||
		strings.Contains(opResult.Code.String(), "not_authorized"):
		// Extract asset info from the operation result node tree
		node := buildNode("", reflect.ValueOf(opResult), 0)
		assetCode := "unknown asset"
		assetIssuer := ""

		if node != nil {
			if assetNode := node.Child("Asset"); assetNode != nil {
				if codeNode := assetNode.Child("Code"); codeNode != nil {
					if code, ok := codeNode.Value.(string); ok {
						assetCode = code
					}
				}
			}

			if issuerNode := node.Child("Issuer"); issuerNode != nil {
				if issuer, ok := issuerNode.Value.(string); ok {
					assetIssuer = issuer
				}
			}
		}

		return NewTrustlineFailure(
			"not_authorized",
			operationIndex,
			assetCode,
			assetIssuer,
			reason.Summary,
			"The asset issuer must authorize this account's trustline. "+
				"Ensure the issuer has set the auth-required flag and "+
				"that the trustline is properly authorized.",
		)
	}

	return nil
}

// init registers the diagnosis function with the lens framework.
func init() {
	_ = TrustlineFailure{}
}

// DiagnoseChangeTrustFailure explains a failed change_trust operation from the
// operation's own parameters. It returns an empty string for a nil operation
// or a successful code. The diagnosis is offline: the trustline's balance and
// liabilities live on the ledger, so it names the rule that was broken rather
// than the amounts involved.
func DiagnoseChangeTrustFailure(op *xdr.ChangeTrustOp, code xdr.ChangeTrustResultCode) string {
	if op == nil {
		return ""
	}
	r := ExplainCode(code.String())
	if r.Success {
		return ""
	}
	return reasonDiagnosisText(deepenChangeTrustFailure(*op, r))
}

// deepenChangeTrustFailure refines the limit and reserve failures of a
// change_trust operation. stellar-core requires a limit of at least the
// trustline's balance plus its buying liabilities, and a limit of 0 is how a
// trustline is removed, so the two cases need different remedies.
func deepenChangeTrustFailure(o xdr.ChangeTrustOp, r Reason) Reason {
	asset := changeTrustAssetLabel(o.Line)
	switch r.Constant {
	case "ChangeTrustResultCodeChangeTrustInvalidLimit":
		if o.Limit == 0 {
			r.Summary = fmt.Sprintf(
				"Setting the %s limit to 0 asks to remove the trustline, which is only possible once its balance and buying liabilities are zero; a trustline that does not exist cannot be removed either.",
				asset)
			r.Hint = fmt.Sprintf(
				"Cancel open DEX offers for %s to release their liabilities and send any remaining balance away (for example back to the issuer), then retry with limit 0. Claimable balances already created hold no liabilities on this trustline, so they do not block removal.",
				asset)
			return r
		}
		r.Summary = fmt.Sprintf(
			"The new %s limit of %s is below the trustline's minimum: its current balance plus the buying liabilities of open DEX offers.",
			asset, amount.String(o.Limit))
		r.Hint = "Set the limit to at least the balance plus buying liabilities, or cancel open buy offers and reduce the balance before lowering it."

	case "ChangeTrustResultCodeChangeTrustLowReserve":
		// A pool share trustline counts as two subentries, every other
		// trustline as one.
		subentries, added := int64(1), "one subentry"
		if o.Line.Type == xdr.AssetTypeAssetTypePoolShare {
			subentries, added = 2, "two subentries"
		}
		reserve := reserveAmountLabel(subentries * int64(DefaultBaseReserve))
		r.Summary = fmt.Sprintf(
			"Creating the %s trustline adds %s, raising the account's minimum balance by %s at the 0.5 XLM base reserve, and its available XLM cannot cover that.",
			asset, added, reserve)
		r.Hint = fmt.Sprintf(
			"Make at least %s available above the current minimum balance, free reserve by removing an unused offer, trustline, data entry or signer, or have another account sponsor the trustline.",
			reserve)
	}
	return r
}

// reserveAmountLabel renders a reserve in both stroops and XLM.
func reserveAmountLabel(stroops int64) string {
	return fmt.Sprintf("%d stroops (%s)", stroops, compactXLM(stroops))
}

// reasonDiagnosisText joins a Reason into the single string returned by the
// exported per-operation diagnosis helpers.
func reasonDiagnosisText(r Reason) string {
	return strings.TrimSpace(r.Summary + " " + r.Hint)
}
