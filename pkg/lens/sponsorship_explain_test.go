package lens_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

func TestDiagnoseSponsorshipSequence(t *testing.T) {
	t.Parallel()
	envelope := sponsorshipEnvelope(
		xdr.Operation{Body: xdr.OperationBody{Type: xdr.OperationTypeBeginSponsoringFutureReserves}},
		xdr.Operation{Body: xdr.OperationBody{Type: xdr.OperationTypeBeginSponsoringFutureReserves}},
	)
	result := &xdr.TransactionResult{Result: xdr.TransactionResultResult{Code: xdr.TransactionResultCodeTxBadSponsorship}}
	d, err := lens.DiagnoseSponsorship(envelope, result)
	if err != nil {
		t.Fatalf("DiagnoseSponsorship() error = %v", err)
	}
	if d.TransactionCode != "tx_bad_sponsorship" || len(d.Conflicts) != 2 || len(d.Operations) != 2 {
		t.Errorf("diagnosis = %+v, want nested and unmatched sponsorship conflicts", d)
	}
	if !strings.Contains(d.Summary, "before operation 0") {
		t.Errorf("Summary = %q, want nested-operation detail", d.Summary)
	}
}

func TestDiagnoseSponsorshipUnmatchedEndAndNonSponsorship(t *testing.T) {
	t.Parallel()
	endOnly := sponsorshipEnvelope(xdr.Operation{Body: xdr.OperationBody{Type: xdr.OperationTypeEndSponsoringFutureReserves}})
	failed := &xdr.TransactionResult{Result: xdr.TransactionResultResult{Code: xdr.TransactionResultCodeTxBadSponsorship}}
	d, err := lens.DiagnoseSponsorship(endOnly, failed)
	if err != nil || len(d.Conflicts) != 1 || !strings.Contains(d.Conflicts[0], "no preceding") {
		t.Errorf("unmatched end diagnosis = %+v, %v", d, err)
	}
	emptyOperations := []xdr.OperationResult{}
	nonSponsorship := &xdr.TransactionResult{Result: xdr.TransactionResultResult{Code: xdr.TransactionResultCodeTxFailed, Results: &emptyOperations}}
	if d, err := lens.DiagnoseSponsorship(endOnly, nonSponsorship); err != nil || d != nil {
		t.Errorf("non-sponsorship diagnosis = %+v, %v; want nil, nil", d, err)
	}
	if _, err := lens.DiagnoseSponsorship(nil, failed); !errors.Is(err, lens.ErrInvalidArgument) {
		t.Errorf("nil envelope error = %v, want ErrInvalidArgument", err)
	}
}

func TestDiagnoseSponsorshipOperationFailureAndBalancedSequence(t *testing.T) {
	t.Parallel()
	key := xdr.Uint256{}
	beginOp := xdr.Operation{Body: xdr.OperationBody{
		Type: xdr.OperationTypeBeginSponsoringFutureReserves,
		BeginSponsoringFutureReservesOp: &xdr.BeginSponsoringFutureReservesOp{
			SponsoredId: xdr.AccountId{Type: xdr.PublicKeyTypePublicKeyTypeEd25519, Ed25519: &key},
		},
	}}
	endOp := xdr.Operation{Body: xdr.OperationBody{Type: xdr.OperationTypeEndSponsoringFutureReserves}}
	revokeOp := xdr.Operation{Body: xdr.OperationBody{Type: xdr.OperationTypeRevokeSponsorship}}
	envelope := sponsorshipEnvelope(beginOp, endOp, revokeOp)
	beginResult := xdr.BeginSponsoringFutureReservesResult{Code: xdr.BeginSponsoringFutureReservesResultCodeBeginSponsoringFutureReservesAlreadySponsored}
	tr := xdr.OperationResultTr{Type: xdr.OperationTypeBeginSponsoringFutureReserves, BeginSponsoringFutureReservesResult: &beginResult}
	operations := []xdr.OperationResult{{Code: xdr.OperationResultCodeOpInner, Tr: &tr}}
	result := &xdr.TransactionResult{Result: xdr.TransactionResultResult{Code: xdr.TransactionResultCodeTxFailed, Results: &operations}}
	d, err := lens.DiagnoseSponsorship(envelope, result)
	if err != nil {
		t.Fatalf("DiagnoseSponsorship(operation result) error = %v", err)
	}
	if d.OperationIndex == nil || *d.OperationIndex != 0 || d.OperationCode != "begin_sponsoring_future_reserves_already_sponsored" || len(d.Conflicts) != 0 {
		t.Errorf("diagnosis = %+v, want failed begin and balanced sequence", d)
	}
	if len(d.Operations) != 3 || d.Operations[0].ResultCode != d.OperationCode {
		t.Errorf("sponsorship steps = %+v, want operation result attribution", d.Operations)
	}
}

func sponsorshipEnvelope(operations ...xdr.Operation) *xdr.TransactionEnvelope {
	return &xdr.TransactionEnvelope{
		Type: xdr.EnvelopeTypeEnvelopeTypeTx,
		V1:   &xdr.TransactionV1Envelope{Tx: xdr.Transaction{Operations: operations}},
	}
}
