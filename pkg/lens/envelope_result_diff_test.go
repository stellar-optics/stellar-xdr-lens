package lens_test

import (
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

func decodeResultFixture(t *testing.T, fixture string) (*xdr.TransactionResult, *lens.Value) {
	t.Helper()
	value, err := lens.DecodeAs(loadFixture(t, fixture), "TransactionResult")
	if err != nil {
		t.Fatalf("DecodeAs(%s): %v", fixture, err)
	}
	result, ok := value.Raw.(*xdr.TransactionResult)
	if !ok {
		t.Fatalf("fixture %s decoded as %T", fixture, value.Raw)
	}
	return result, value
}

func bumpSequenceEnvelope(operationCount int) *xdr.TransactionEnvelope {
	operations := make([]xdr.Operation, 0, operationCount)
	for i := 0; i < operationCount; i++ {
		operations = append(operations, xdr.Operation{Body: xdr.OperationBody{
			Type:           xdr.OperationTypeBumpSequence,
			BumpSequenceOp: &xdr.BumpSequenceOp{BumpTo: xdr.SequenceNumber(i + 1)},
		}})
	}
	return &xdr.TransactionEnvelope{
		Type: xdr.EnvelopeTypeEnvelopeTypeTx,
		V1: &xdr.TransactionV1Envelope{
			Tx: xdr.Transaction{
				SourceAccount: xdr.MustMuxedAddress(signerTestAddrA),
				Fee:           100,
				SeqNum:        xdr.SequenceNumber(1),
				Cond:          xdr.Preconditions{Type: xdr.PreconditionTypePrecondNone},
				Memo:          xdr.Memo{Type: xdr.MemoTypeMemoNone},
				Operations:    operations,
				Ext:           xdr.TransactionExt{V: 0},
			},
		},
	}
}

func TestDiffEnvelopeResultFailedPair(t *testing.T) {
	t.Parallel()

	env := decodeEnvelopeFixture(t, "tx_failed.env")
	result, _ := decodeResultFixture(t, "tx_failed.res")

	diff, err := lens.DiffEnvelopeResult(env, result)
	if err != nil {
		t.Fatalf("DiffEnvelopeResult() error = %v", err)
	}

	if diff.Success {
		t.Error("Success = true, want false")
	}
	if diff.TransactionCode != "tx_fee_bump_inner_failed" {
		t.Errorf("TransactionCode = %q", diff.TransactionCode)
	}
	if !diff.OperationsRun {
		t.Error("OperationsRun = false, want true")
	}
	if len(diff.Operations) != 1 {
		t.Fatalf("Operations = %d, want 1", len(diff.Operations))
	}
	pair := diff.Operations[0]
	if pair.Status != lens.OperationStatusFailed || pair.Success {
		t.Errorf("pair = %+v, want failed", pair)
	}
	if pair.ResultCode != "invoke_host_function_trapped" {
		t.Errorf("ResultCode = %q, want invoke_host_function_trapped", pair.ResultCode)
	}
	if pair.ResultSummary == "" || pair.ResultHint == "" {
		t.Errorf("pair result prose is empty: %+v", pair)
	}
	if len(diff.FailedOperations) != 1 || diff.FailedOperations[0] != 0 {
		t.Errorf("FailedOperations = %v, want [0]", diff.FailedOperations)
	}
	if len(diff.Changes) == 0 {
		t.Error("Changes is empty, want at least the failure change")
	}
	if !strings.Contains(diff.Summary(), "operation 0") {
		t.Errorf("Summary() = %q, want it to name operation 0", diff.Summary())
	}
	if !strings.Contains(diff.String(), "invoke_host_function") {
		t.Errorf("String() = %q, want the operation name", diff.String())
	}

	structural := diff.DiffResult()
	if structural == nil || structural.Equal() {
		t.Fatalf("DiffResult() = %+v, want non-empty changes", structural)
	}
	if structural.LeftType != "TransactionEnvelope" || structural.RightType != "TransactionResult" {
		t.Errorf("DiffResult types = %s/%s", structural.LeftType, structural.RightType)
	}
	stats := diff.Summarize()
	if stats.Total != len(diff.Changes) {
		t.Errorf("Summarize().Total = %d, want %d", stats.Total, len(diff.Changes))
	}
	if diff.Tree() == nil {
		t.Fatal("Tree() = nil")
	}
}

func TestDiffEnvelopeResultSuccessPair(t *testing.T) {
	t.Parallel()

	env := decodeEnvelopeFixture(t, "tx_soroban.env")
	result, _ := decodeResultFixture(t, "tx_soroban.res")

	diff, err := lens.DiffEnvelopeResult(env, result)
	if err != nil {
		t.Fatalf("DiffEnvelopeResult() error = %v", err)
	}
	if !diff.Success {
		t.Errorf("Success = false for %s", diff.TransactionCode)
	}
	if diff.InnerTransactionCode == "" {
		t.Error("InnerTransactionCode is empty for a fee bump")
	}
	if len(diff.FailedOperations) != 0 {
		t.Errorf("FailedOperations = %v, want none", diff.FailedOperations)
	}
	for _, pair := range diff.Operations {
		if pair.Status != lens.OperationStatusSuccess {
			t.Errorf("pair status = %q, want success", pair.Status)
		}
	}
	if !strings.Contains(diff.Summary(), "succeeded") {
		t.Errorf("Summary() = %q", diff.Summary())
	}
}

func TestDiffEnvelopeResultPreExecutionFailure(t *testing.T) {
	t.Parallel()

	env := bumpSequenceEnvelope(1)
	result := &xdr.TransactionResult{
		FeeCharged: 100,
		Result:     xdr.TransactionResultResult{Code: xdr.TransactionResultCodeTxBadSeq},
	}

	diff, err := lens.DiffEnvelopeResult(env, result)
	if err != nil {
		t.Fatalf("DiffEnvelopeResult() error = %v", err)
	}
	if diff.OperationsRun {
		t.Error("OperationsRun = true, want false")
	}
	if len(diff.Operations) != 1 || diff.Operations[0].Status != lens.OperationStatusNotRun {
		t.Errorf("Operations = %+v, want one not_run entry", diff.Operations)
	}
	if len(diff.Changes) != 0 {
		t.Errorf("Changes = %+v, want none for a pre-execution rejection", diff.Changes)
	}
	if !strings.Contains(diff.Summary(), "before any operation ran") {
		t.Errorf("Summary() = %q", diff.Summary())
	}
	if !strings.Contains(diff.String(), "did not run") {
		t.Errorf("String() = %q, want the not_run note", diff.String())
	}
}

func TestDiffEnvelopeResultOperationWrapperFailure(t *testing.T) {
	t.Parallel()

	env := bumpSequenceEnvelope(1)
	results := []xdr.OperationResult{{Code: xdr.OperationResultCodeOpBadAuth}}
	result := &xdr.TransactionResult{
		FeeCharged: 100,
		Result: xdr.TransactionResultResult{
			Code:    xdr.TransactionResultCodeTxFailed,
			Results: &results,
		},
	}

	diff, err := lens.DiffEnvelopeResult(env, result)
	if err != nil {
		t.Fatalf("DiffEnvelopeResult() error = %v", err)
	}
	if diff.OperationsRun != true {
		t.Error("OperationsRun = false, want true")
	}
	pair := diff.Operations[0]
	if pair.Status != lens.OperationStatusFailed || pair.ResultCode != "op_bad_auth" {
		t.Errorf("pair = %+v, want a failed op_bad_auth wrapper", pair)
	}
	if pair.Mismatched {
		t.Errorf("pair.Mismatched = true, want false when the operation never ran its arm")
	}
}

func TestDiffEnvelopeResultMissingAndUnpaired(t *testing.T) {
	t.Parallel()

	_, resultValue := decodeResultFixture(t, "tx_success.res")

	t.Run("missing result", func(t *testing.T) {
		t.Parallel()
		env := bumpSequenceEnvelope(2)
		result, _ := resultValue.Raw.(*xdr.TransactionResult)
		diff, err := lens.DiffEnvelopeResult(env, result)
		if err != nil {
			t.Fatalf("DiffEnvelopeResult() error = %v", err)
		}
		if len(diff.Operations) != 2 {
			t.Fatalf("Operations = %d, want 2", len(diff.Operations))
		}
		if diff.Operations[1].Status != lens.OperationStatusMissingResult {
			t.Errorf("Operations[1].Status = %q, want missing_result", diff.Operations[1].Status)
		}
		if len(diff.UnpairedOperations) != 1 || diff.UnpairedOperations[0] != 1 {
			t.Errorf("UnpairedOperations = %v, want [1]", diff.UnpairedOperations)
		}
		if !strings.Contains(diff.String(), "no result entry") {
			t.Errorf("String() = %q, want the missing-result note", diff.String())
		}
		if diff.Tree() == nil {
			t.Error("Tree() = nil")
		}
		// The bump-sequence envelope does not match the result's arm.
		if !diff.Operations[0].Mismatched {
			t.Errorf("Operations[0] = %+v, want a mismatch", diff.Operations[0])
		}
	})

	t.Run("unpaired result", func(t *testing.T) {
		t.Parallel()
		env := bumpSequenceEnvelope(0)
		result, _ := resultValue.Raw.(*xdr.TransactionResult)
		diff, err := lens.DiffEnvelopeResult(env, result)
		if err != nil {
			t.Fatalf("DiffEnvelopeResult() error = %v", err)
		}
		if len(diff.Operations) != 1 || diff.Operations[0].Status != lens.OperationStatusUnpairedResult {
			t.Fatalf("Operations = %+v, want one unpaired_result", diff.Operations)
		}
		if len(diff.UnpairedResults) != 1 || diff.UnpairedResults[0] != 0 {
			t.Errorf("UnpairedResults = %v, want [0]", diff.UnpairedResults)
		}
		if !strings.Contains(diff.String(), "no matching operation") {
			t.Errorf("String() = %q, want the unpaired-result note", diff.String())
		}
		if diff.Tree() == nil {
			t.Error("Tree() = nil")
		}
	})
}

func TestDiffEnvelopeResultRejectsBadArguments(t *testing.T) {
	t.Parallel()

	env := bumpSequenceEnvelope(1)
	result, resultValue := decodeResultFixture(t, "tx_success.res")
	envelopeValue, err := lens.DecodeAs(buildSignedEnvelope(t, nil), "TransactionEnvelope")
	if err != nil {
		t.Fatalf("DecodeAs envelope: %v", err)
	}

	if _, err := lens.DiffEnvelopeResult(nil, result); err == nil {
		t.Error("DiffEnvelopeResult(nil, result) error = nil")
	}
	if _, err := lens.DiffEnvelopeResult(env, nil); err == nil {
		t.Error("DiffEnvelopeResult(env, nil) error = nil")
	}
	if _, err := lens.DiffEnvelopeResultValues(nil, resultValue); err == nil {
		t.Error("DiffEnvelopeResultValues(nil, ...) error = nil")
	}
	if _, err := lens.DiffEnvelopeResultValues(resultValue, resultValue); err == nil {
		t.Error("DiffEnvelopeResultValues(result, result) error = nil")
	}
	if _, err := lens.DiffEnvelopeResultBase64("!!!", loadFixture(t, "tx_success.res")); err == nil {
		t.Error("DiffEnvelopeResultBase64(bad envelope) error = nil")
	}
	if _, err := lens.DiffEnvelopeResultBase64(loadFixture(t, "tx_success.env"), "!!!"); err == nil {
		t.Error("DiffEnvelopeResultBase64(bad result) error = nil")
	}

	got, err := lens.DiffEnvelopeResultValues(envelopeValue, resultValue)
	if err != nil {
		t.Fatalf("DiffEnvelopeResultValues() error = %v", err)
	}
	if got == nil {
		t.Fatal("DiffEnvelopeResultValues() = nil")
	}
}

func TestEnvelopeResultDiffNilHelpers(t *testing.T) {
	t.Parallel()

	var diff *lens.EnvelopeResultDiff
	if diff.Summary() != "" || diff.String() != "" || diff.DiffResult() != nil || diff.Tree() != nil {
		t.Error("nil receiver helpers returned non-empty values")
	}
	if stats := diff.Summarize(); stats.Total != 0 || !stats.Equal {
		t.Errorf("Summarize() = %+v, want empty", stats)
	}
}
