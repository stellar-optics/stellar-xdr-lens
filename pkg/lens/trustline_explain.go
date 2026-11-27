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

// paymentResultCodes lists the payment failures DiagnosePaymentFailure explains,
// so a Reason's constant name can be resolved back to its typed code.
var paymentResultCodes = [...]xdr.PaymentResultCode{
	xdr.PaymentResultCodePaymentMalformed,
	xdr.PaymentResultCodePaymentUnderfunded,
	xdr.PaymentResultCodePaymentSrcNoTrust,
	xdr.PaymentResultCodePaymentSrcNotAuthorized,
	xdr.PaymentResultCodePaymentNoDestination,
	xdr.PaymentResultCodePaymentNoTrust,
	xdr.PaymentResultCodePaymentNotAuthorized,
	xdr.PaymentResultCodePaymentLineFull,
	xdr.PaymentResultCodePaymentNoIssuer,
}

// deepenPaymentFailure attaches the operation-specific diagnosis of a failed
// payment to r.Context, leaving the generic per-code summary and hint in place.
func deepenPaymentFailure(o xdr.PaymentOp, r Reason) Reason {
	for _, code := range paymentResultCodes {
		if code.String() == r.Constant {
			r.Context = DiagnosePaymentFailure(&o, code)
			break
		}
	}
	return r
}

// DiagnosePaymentFailure explains a failed payment using the destination, asset
// and amount in the operation, and names the step that unblocks it.
//
// The diagnosis is built from the operation alone: no ledger state is read and
// no network call is made. It returns "" when there is nothing specific to add:
// for a nil operation, a successful code, or a trustline failure on an asset
// that has no trustlines.
func DiagnosePaymentFailure(op *xdr.PaymentOp, code xdr.PaymentResultCode) string {
	if op == nil {
		return ""
	}
	destination := paymentDestinationLabel(op.Destination)
	sent := amount.String(op.Amount)
	assetCode, issuer, isCredit := creditAssetParts(op.Asset)
	native := op.Asset.Type == xdr.AssetTypeAssetTypeNative

	switch code {
	case xdr.PaymentResultCodePaymentMalformed:
		switch {
		case op.Amount <= 0:
			return fmt.Sprintf("the payment amount is %s, but a payment must send a positive amount.", sent)
		case !native && !isCredit:
			return "the payment asset is not a valid asset: a credit asset needs a 1-12 character alphanumeric code and an issuer account."
		}

	case xdr.PaymentResultCodePaymentUnderfunded:
		if native {
			return fmt.Sprintf("the source account cannot spend %s XLM: the amount must fit within its balance after the minimum reserve and selling liabilities. Fund the account or lower the amount.", sent)
		}
		if isCredit {
			return fmt.Sprintf("the source account cannot spend %s %s issued by %s: the amount must fit within its trustline balance after selling liabilities. Acquire more %s or lower the amount.",
				sent, assetCode, issuer, assetCode)
		}

	case xdr.PaymentResultCodePaymentSrcNoTrust:
		if isCredit {
			return fmt.Sprintf("the source account has no trustline for %s issued by %s, so it cannot send %s %s. It must submit a ChangeTrust operation for %s:%s and hold a balance first.",
				assetCode, issuer, sent, assetCode, assetCode, issuer)
		}

	case xdr.PaymentResultCodePaymentSrcNotAuthorized:
		if isCredit {
			return fmt.Sprintf("the source account's trustline for %s is not authorized, so it cannot send %s %s. Issuer %s must set AUTHORIZED_FLAG on that trustline with a SetTrustLineFlagsOp.",
				assetCode, sent, assetCode, issuer)
		}

	case xdr.PaymentResultCodePaymentNoDestination:
		text := fmt.Sprintf("destination account %s has not been created on-chain yet, so it cannot receive %s %s. Submit a CreateAccount operation funding it with at least 1 XLM, then retry the payment.",
			destination, sent, paymentAssetLabel(assetCode, native))
		if isCredit {
			text += fmt.Sprintf(" The new account will also need a trustline for %s:%s before it can receive this asset.", assetCode, issuer)
		}
		return text

	case xdr.PaymentResultCodePaymentNoTrust:
		if isCredit {
			return fmt.Sprintf("destination account %s has no trustline for %s issued by %s, so it cannot receive %s %s. The recipient must submit a ChangeTrust operation for %s:%s first, then the payment can be retried.",
				destination, assetCode, issuer, sent, assetCode, assetCode, issuer)
		}

	case xdr.PaymentResultCodePaymentNotAuthorized:
		if isCredit {
			return fmt.Sprintf("destination account %s is not authorized to receive %s %s. Issuer %s controls this asset with AUTH_REQUIRED_FLAG, so a new trustline starts unauthorized, and an existing one can be revoked. The issuer must authorize the recipient's trustline by setting AUTHORIZED_FLAG with a SetTrustLineFlagsOp.",
				destination, sent, assetCode, issuer)
		}

	case xdr.PaymentResultCodePaymentLineFull:
		if native {
			return fmt.Sprintf("receiving %s XLM would push destination account %s past the maximum balance its XLM buying liabilities allow. The recipient must cancel offers buying XLM, or the sender must lower the amount.",
				sent, destination)
		}
		if isCredit {
			return fmt.Sprintf("receiving %s %s would exceed what destination account %s can hold: its balance, buying liabilities and this payment must fit within its trustline limit for %s:%s. The recipient must raise the limit with ChangeTrust or cancel offers buying %s, or the sender must lower the amount.",
				sent, assetCode, destination, assetCode, issuer, assetCode)
		}

	case xdr.PaymentResultCodePaymentNoIssuer:
		if isCredit {
			return fmt.Sprintf("issuer account %s for %s does not exist on this network. Check the asset's issuer and the network the transaction targets.", issuer, assetCode)
		}
	}
	return ""
}

// paymentDestinationLabel names the account behind a payment destination. A
// muxed destination resolves to its underlying G... account, because that is
// the account that needs creating or holds the trustline.
func paymentDestinationLabel(m xdr.MuxedAccount) string {
	var key *xdr.Uint256
	switch m.Type {
	case xdr.CryptoKeyTypeKeyTypeEd25519:
		key = m.Ed25519
	case xdr.CryptoKeyTypeKeyTypeMuxedEd25519:
		if m.Med25519 != nil {
			key = &m.Med25519.Ed25519
		}
	}
	if addr := accountKeyAddress(key); addr != "" {
		return addr
	}
	return "(unknown)"
}

// paymentAssetLabel names the paid asset for prose that does not need its issuer.
func paymentAssetLabel(assetCode string, native bool) string {
	switch {
	case native:
		return "XLM"
	case assetCode != "":
		return assetCode
	}
	return "of the asset"
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
