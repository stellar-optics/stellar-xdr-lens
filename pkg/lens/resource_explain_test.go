package lens_test

import (
	"errors"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

func TestDiagnoseResourceFailure(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		code xdr.InvokeHostFunctionResultCode
		kind lens.ResourceFailureKind
	}{
		{"limit", xdr.InvokeHostFunctionResultCodeInvokeHostFunctionResourceLimitExceeded, lens.ResourceFailureLimitExceeded},
		{"refundable fee", xdr.InvokeHostFunctionResultCodeInvokeHostFunctionInsufficientRefundableFee, lens.ResourceFailureInsufficientFee},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			invoke := xdr.InvokeHostFunctionResult{Code: tc.code}
			tr := xdr.OperationResultTr{Type: xdr.OperationTypeInvokeHostFunction, InvokeHostFunctionResult: &invoke}
			operations := []xdr.OperationResult{{Code: xdr.OperationResultCodeOpInner, Tr: &tr}}
			result := &xdr.TransactionResult{Result: xdr.TransactionResultResult{Code: xdr.TransactionResultCodeTxFailed, Results: &operations}}
			d, err := lens.DiagnoseResourceFailure(result)
			if err != nil {
				t.Fatalf("DiagnoseResourceFailure() error = %v", err)
			}
			if d == nil || d.Kind != tc.kind || d.OperationIndex == nil || *d.OperationIndex != 0 || d.Summary == "" || d.Hint == "" {
				t.Errorf("diagnosis = %+v, want %s at operation 0", d, tc.kind)
			}
		})
	}

	invalid := &xdr.TransactionResult{Result: xdr.TransactionResultResult{Code: xdr.TransactionResultCodeTxSorobanInvalid}}
	d, err := lens.DiagnoseResourceFailure(invalid)
	if err != nil || d == nil || d.Kind != lens.ResourceFailureInvalidDeclaration {
		t.Errorf("invalid-declaration diagnosis = %+v, %v", d, err)
	}
	emptyOperations := []xdr.OperationResult{}
	nonResource := &xdr.TransactionResult{Result: xdr.TransactionResultResult{Code: xdr.TransactionResultCodeTxFailed, Results: &emptyOperations}}
	if d, err := lens.DiagnoseResourceFailure(nonResource); err != nil || d != nil {
		t.Errorf("non-resource diagnosis = %+v, %v; want nil, nil", d, err)
	}
	if _, err := lens.DiagnoseResourceFailure(nil); !errors.Is(err, lens.ErrInvalidArgument) {
		t.Errorf("nil result error = %v, want ErrInvalidArgument", err)
	}
}

func TestDiagnoseResourceFailureCanonicalFixture(t *testing.T) {
	t.Parallel()
	result, err := lens.DecodeAs(loadFixture(t, "tx_soroban.res"), "TransactionResult")
	if err != nil {
		t.Fatalf("DecodeAs(tx_soroban.res) error = %v", err)
	}
	d, err := lens.DiagnoseResourceFailure(result.Raw.(*xdr.TransactionResult))
	if err != nil {
		t.Fatalf("DiagnoseResourceFailure(fixture) error = %v", err)
	}
	if d != nil {
		t.Errorf("successful Soroban fixture diagnosis = %+v, want nil", d)
	}
}
