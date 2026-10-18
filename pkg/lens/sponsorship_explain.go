package lens

import (
	"fmt"
	"strings"

	"github.com/stellar/go-stellar-sdk/xdr"
)

// SponsorshipStep records one sponsorship-related transaction operation.
type SponsorshipStep struct {
	Index            int    `json:"index"`
	Type             string `json:"type"`
	Source           string `json:"source,omitempty"`
	SponsoredAccount string `json:"sponsoredAccount,omitempty"`
	ResultCode       string `json:"resultCode,omitempty"`
}

// SponsorshipDiagnosis explains a sponsorship operation failure or invalid
// begin/end sequence in a transaction.
type SponsorshipDiagnosis struct {
	TransactionCode string            `json:"transactionCode,omitempty"`
	OperationIndex  *int              `json:"operationIndex,omitempty"`
	OperationCode   string            `json:"operationCode,omitempty"`
	Summary         string            `json:"summary"`
	Hint            string            `json:"hint,omitempty"`
	Conflicts       []string          `json:"conflicts,omitempty"`
	Operations      []SponsorshipStep `json:"operations,omitempty"`
}

// DiagnoseSponsorship explains sponsorship result codes and checks the
// transaction's begin/end sponsorship instructions for obvious conflicts.
// It returns (nil, nil) when neither the result nor operation sequence points
// to a sponsorship failure. All inputs are inspected locally; no ledger lookup
// is performed.
func DiagnoseSponsorship(envelope *xdr.TransactionEnvelope, result *xdr.TransactionResult) (*SponsorshipDiagnosis, error) {
	if envelope == nil || result == nil {
		return nil, fmt.Errorf("%w: envelope and result are required for sponsorship diagnosis", ErrInvalidArgument)
	}
	transactionConstant, err := resultCodeName(result)
	if err != nil {
		return nil, err
	}
	if inner, ok := innerResultCodeName(result); ok {
		transactionConstant = inner
	}
	operationIndex := -1
	var operationConstant string
	operationResults, _ := result.OperationResults()
	for index, operationResult := range operationResults {
		reason := explainOperationResult(operationResult)
		if !strings.Contains(reason.Constant, "SponsoringFutureReserves") && !strings.Contains(reason.Constant, "RevokeSponsorship") {
			continue
		}
		if reason.Success {
			continue
		}
		operationIndex = index
		operationConstant = reason.Constant
		break
	}
	if transactionConstant != "TransactionResultCodeTxBadSponsorship" && operationIndex < 0 {
		return nil, nil
	}

	d := &SponsorshipDiagnosis{}
	if transactionConstant == "TransactionResultCodeTxBadSponsorship" {
		reason := ExplainCode(transactionConstant)
		d.TransactionCode = reason.Code
		d.Summary = reason.Summary
		d.Hint = reason.Hint
	} else {
		d.OperationIndex = &operationIndex
		d.OperationCode = ExplainCode(operationConstant).Code
		reason := ExplainCode(operationConstant)
		d.Summary = fmt.Sprintf("Sponsorship operation %d failed: %s", operationIndex, reason.Summary)
		d.Hint = reason.Hint
	}

	d.Operations, d.Conflicts = inspectSponsorshipSequence(envelope, operationResults)
	if len(d.Conflicts) > 0 {
		d.Summary += " Transaction sequence issue: " + strings.Join(d.Conflicts, "; ") + "."
		if d.Hint == "" {
			d.Hint = "Pair every begin-sponsoring instruction with the correct end-sponsoring instruction in the same transaction."
		}
	}
	return d, nil
}

func inspectSponsorshipSequence(envelope *xdr.TransactionEnvelope, operationResults []xdr.OperationResult) ([]SponsorshipStep, []string) {
	var steps []SponsorshipStep
	var conflicts []string
	pendingBegin := -1
	for index, operation := range envelope.Operations() {
		resultCode := ""
		if index < len(operationResults) {
			resultCode = explainOperationResult(operationResults[index]).Code
		}
		switch operation.Body.Type {
		case xdr.OperationTypeBeginSponsoringFutureReserves:
			step := SponsorshipStep{Index: index, Type: "begin_sponsoring_future_reserves", Source: operationSource(operation), ResultCode: resultCode}
			if begin := operation.Body.BeginSponsoringFutureReservesOp; begin != nil {
				if begin.SponsoredId.Type == xdr.PublicKeyTypePublicKeyTypeEd25519 && begin.SponsoredId.Ed25519 != nil {
					if address, err := begin.SponsoredId.GetAddress(); err == nil {
						step.SponsoredAccount = address
					}
				}
			}
			steps = append(steps, step)
			if pendingBegin >= 0 {
				conflicts = append(conflicts, fmt.Sprintf("begin-sponsoring at operation %d occurs before operation %d was ended", index, pendingBegin))
			} else {
				pendingBegin = index
			}
		case xdr.OperationTypeEndSponsoringFutureReserves:
			steps = append(steps, SponsorshipStep{Index: index, Type: "end_sponsoring_future_reserves", Source: operationSource(operation), ResultCode: resultCode})
			if pendingBegin < 0 {
				conflicts = append(conflicts, fmt.Sprintf("end-sponsoring at operation %d has no preceding begin-sponsoring", index))
			} else {
				pendingBegin = -1
			}
		case xdr.OperationTypeRevokeSponsorship:
			steps = append(steps, SponsorshipStep{Index: index, Type: "revoke_sponsorship", Source: operationSource(operation), ResultCode: resultCode})
		}
	}
	if pendingBegin >= 0 {
		conflicts = append(conflicts, fmt.Sprintf("begin-sponsoring at operation %d has no matching end-sponsoring", pendingBegin))
	}
	return steps, conflicts
}
