package lens

import (
	"fmt"
	"strings"

	"github.com/stellar/go-stellar-sdk/xdr"
)

// ProtocolUpgradeDiagnosis correlates a rejected transaction with a protocol
// version change between adjacent ledgers. A correlation is not proof that the
// upgrade caused the rejection.
type ProtocolUpgradeDiagnosis struct {
	BoundaryLedger       uint32 `json:"boundaryLedger"`
	PreviousProtocol     uint32 `json:"previousProtocol"`
	CurrentProtocol      uint32 `json:"currentProtocol"`
	RejectionCode        string `json:"rejectionCode"`
	LikelyUpgradeRelated bool   `json:"likelyUpgradeRelated"`
	Summary              string `json:"summary"`
	Hint                 string `json:"hint"`
}

// DiagnoseProtocolUpgrade reports whether a failed transaction coincided with
// a protocol-version change between adjacent ledger headers. It returns
// (nil, nil) for unchanged protocol versions or successful transactions. No
// network lookups are performed.
func DiagnoseProtocolUpgrade(previous, current *xdr.LedgerHeader, result *xdr.TransactionResult) (*ProtocolUpgradeDiagnosis, error) {
	if previous == nil || current == nil || result == nil {
		return nil, fmt.Errorf("%w: previous header, current header, and transaction result are required", ErrInvalidArgument)
	}
	if uint64(current.LedgerSeq) != uint64(previous.LedgerSeq)+1 {
		return nil, fmt.Errorf("%w: ledger headers must be adjacent (got %d then %d)", ErrInvalidArgument, previous.LedgerSeq, current.LedgerSeq)
	}
	if current.LedgerVersion < previous.LedgerVersion {
		return nil, fmt.Errorf("%w: protocol version decreased from %d to %d", ErrInvalidArgument, previous.LedgerVersion, current.LedgerVersion)
	}
	if current.LedgerVersion == previous.LedgerVersion || result.Successful() {
		return nil, nil
	}

	resultConstant, err := resultCodeName(result)
	if err != nil {
		return nil, err
	}
	if inner, ok := innerResultCodeName(result); ok && inner != "TransactionResultCodeTxFailed" {
		resultConstant = inner
	}
	reason := ExplainCode(resultConstant)
	constants := []string{resultConstant}
	if inner, ok := innerResultCodeName(result); ok {
		constants = append(constants, inner)
	}
	if operationResults, ok := result.OperationResults(); ok {
		for _, operationResult := range operationResults {
			operationReason := explainOperationResult(operationResult)
			if !operationReason.Success {
				constants = append(constants, operationReason.Constant)
			}
		}
	}
	likelyRelated := false
	for _, constant := range constants {
		switch constant {
		case "TransactionResultCodeTxNotSupported", "TransactionResultCodeTxSorobanInvalid", "OperationResultCodeOpNotSupported":
			likelyRelated = true
		}
	}
	d := &ProtocolUpgradeDiagnosis{
		BoundaryLedger:       uint32(current.LedgerSeq),
		PreviousProtocol:     uint32(previous.LedgerVersion),
		CurrentProtocol:      uint32(current.LedgerVersion),
		RejectionCode:        reason.Code,
		LikelyUpgradeRelated: likelyRelated,
	}
	if likelyRelated {
		d.Summary = fmt.Sprintf("Transaction rejection %s coincided with the protocol upgrade from %d to %d at ledger %d. This is consistent with a newly unsupported or invalid feature, but the timing alone does not prove the upgrade caused it.", reason.Code, d.PreviousProtocol, d.CurrentProtocol, d.BoundaryLedger)
		d.Hint = "Check the protocol changes active at the boundary, then rebuild and re-simulate the transaction against the upgraded network before resubmitting."
	} else {
		d.Summary = fmt.Sprintf("Protocol upgraded from %d to %d at ledger %d while the transaction was rejected with %s; this result does not specifically implicate the upgrade.", d.PreviousProtocol, d.CurrentProtocol, d.BoundaryLedger, reason.Code)
		d.Hint = "Diagnose the reported result code first. Compare the transaction's simulation and protocol assumptions only if the failure began at the upgrade boundary."
	}
	if strings.TrimSpace(d.RejectionCode) == "" {
		return nil, fmt.Errorf("%w: could not resolve transaction rejection code", ErrInvalidXDR)
	}
	return d, nil
}
