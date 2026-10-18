package lens

import (
	"fmt"

	"github.com/stellar/go-stellar-sdk/xdr"
)

// ResourceFailureKind is the machine-readable class of a Soroban resource
// failure found in transaction XDR.
type ResourceFailureKind string

const (
	ResourceFailureInvalidDeclaration ResourceFailureKind = "invalid_declaration"
	ResourceFailureLimitExceeded      ResourceFailureKind = "limit_exceeded"
	ResourceFailureInsufficientFee    ResourceFailureKind = "insufficient_refundable_fee"
)

// ResourceFailureDiagnosis explains a Soroban resource-related transaction or
// operation result. XDR result codes do not carry CPU or memory measurements;
// use DiagnoseSimulationResponse when those counters are available.
type ResourceFailureDiagnosis struct {
	Kind            ResourceFailureKind `json:"kind"`
	TransactionCode string              `json:"transactionCode,omitempty"`
	OperationIndex  *int                `json:"operationIndex,omitempty"`
	OperationCode   string              `json:"operationCode,omitempty"`
	Summary         string              `json:"summary"`
	Hint            string              `json:"hint"`
}

// DiagnoseResourceFailure finds Soroban resource failures in a transaction
// result. It returns (nil, nil) when the result contains no recognised
// resource-related code. The function reads only supplied XDR and stays
// offline.
func DiagnoseResourceFailure(result *xdr.TransactionResult) (*ResourceFailureDiagnosis, error) {
	if result == nil {
		return nil, fmt.Errorf("%w: cannot diagnose resources from nil TransactionResult", ErrInvalidArgument)
	}
	transactionConstant, err := resultCodeName(result)
	if err != nil {
		return nil, err
	}
	transactionReason := ExplainCode(transactionConstant)
	if transactionConstant == "TransactionResultCodeTxSorobanInvalid" {
		return &ResourceFailureDiagnosis{
			Kind:            ResourceFailureInvalidDeclaration,
			TransactionCode: transactionReason.Code,
			Summary:         "The transaction was rejected because its Soroban resource declarations are invalid.",
			Hint:            "Re-simulate against the target network protocol and use the returned footprint, resource limits, and resource fee.",
		}, nil
	}

	operationResults, ok := result.OperationResults()
	if !ok {
		return nil, nil
	}
	for index, operationResult := range operationResults {
		if operationResult.Code != xdr.OperationResultCodeOpInner || operationResult.Tr == nil {
			continue
		}
		invokeResult, ok := operationResult.Tr.GetInvokeHostFunctionResult()
		if !ok {
			continue
		}
		operationConstant := safeString(invokeResult.Code)
		kind, summary, hint := resourceFailureDetails(operationConstant)
		if kind == "" {
			continue
		}
		operationIndex := index
		return &ResourceFailureDiagnosis{
			Kind:           kind,
			OperationIndex: &operationIndex,
			OperationCode:  ExplainCode(operationConstant).Code,
			Summary:        summary,
			Hint:           hint,
		}, nil
	}
	return nil, nil
}

func resourceFailureDetails(code string) (ResourceFailureKind, string, string) {
	switch code {
	case "InvokeHostFunctionResultCodeInvokeHostFunctionResourceLimitExceeded":
		return ResourceFailureLimitExceeded,
			"The contract exceeded a declared Soroban resource limit during execution.",
			"Re-simulate and raise the CPU or memory limit indicated by simulation; if usage is already near the limit, optimize the contract's computation or storage access."
	case "InvokeHostFunctionResultCodeInvokeHostFunctionInsufficientRefundableFee":
		return ResourceFailureInsufficientFee,
			"The refundable resource fee could not cover the transaction's Soroban rent and events.",
			"Use the minimum refundable fee reported by simulation and add headroom for rent and event growth."
	default:
		return "", "", ""
	}
}
