package lens

import (
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"
)

// TestWithPathPaymentFailureDetailKeepsSlippage covers the cases where no
// code-specific diagnosis applies and the existing diagnosis must pass through.
func TestWithPathPaymentFailureDetailKeepsSlippage(t *testing.T) {
	t.Parallel()

	receive := xdr.Operation{Body: xdr.OperationBody{
		Type:                       xdr.OperationTypePathPaymentStrictReceive,
		PathPaymentStrictReceiveOp: &xdr.PathPaymentStrictReceiveOp{},
	}}
	send := xdr.Operation{Body: xdr.OperationBody{
		Type:                    xdr.OperationTypePathPaymentStrictSend,
		PathPaymentStrictSendOp: &xdr.PathPaymentStrictSendOp{},
	}}
	inner := xdr.OperationResult{Code: xdr.OperationResultCodeOpInner, Tr: &xdr.OperationResultTr{}}

	tests := []struct {
		name  string
		op    xdr.Operation
		opRes xdr.OperationResult
		r     Reason
	}{
		{name: "successful operation", op: receive, opRes: inner, r: Reason{Success: true}},
		{name: "operation never ran", op: receive, opRes: xdr.OperationResult{Code: xdr.OperationResultCodeOpBadAuth}},
		{name: "receive without its result arm", op: receive, opRes: inner},
		{name: "send without its result arm", op: send, opRes: inner},
		{name: "not a path payment", op: xdr.Operation{}, opRes: inner},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			slippage := &PathPaymentSlippageDiagnosis{Hint: "keep"}
			r := tc.r
			if got := withPathPaymentFailureDetail(slippage, tc.op, 0, tc.opRes, &r); got != slippage {
				t.Errorf("got %+v, want the original diagnosis", got)
			}
			if slippage.Hint != "keep" || r.Context != "" {
				t.Errorf("hint = %q, context = %q, want both untouched", slippage.Hint, r.Context)
			}
		})
	}
}

// TestWithPathPaymentFailureDetailCreatesDiagnosis covers failure codes the
// slippage diagnosis does not handle.
func TestWithPathPaymentFailureDetailCreatesDiagnosis(t *testing.T) {
	t.Parallel()

	destinationID := xdr.MustAddress("GAPB7YMUQ6MPQ23PTFDNBZP7HGGTZPI2DT5JJURUOIXNPLGHW2O3WYXZ")
	destination := destinationID.ToMuxedAccount()
	receive := xdr.Operation{Body: xdr.OperationBody{
		Type: xdr.OperationTypePathPaymentStrictReceive,
		PathPaymentStrictReceiveOp: &xdr.PathPaymentStrictReceiveOp{
			SendAsset:   xdr.Asset{Type: xdr.AssetTypeAssetTypeNative},
			Destination: destination,
			DestAsset:   xdr.Asset{Type: xdr.AssetTypeAssetTypeNative},
		},
	}}
	receiveRes := xdr.OperationResult{Code: xdr.OperationResultCodeOpInner, Tr: &xdr.OperationResultTr{
		Type: xdr.OperationTypePathPaymentStrictReceive,
		PathPaymentStrictReceiveResult: &xdr.PathPaymentStrictReceiveResult{
			Code: xdr.PathPaymentStrictReceiveResultCodePathPaymentStrictReceiveUnderfunded,
		},
	}}
	send := xdr.Operation{Body: xdr.OperationBody{
		Type: xdr.OperationTypePathPaymentStrictSend,
		PathPaymentStrictSendOp: &xdr.PathPaymentStrictSendOp{
			SendAsset:   xdr.Asset{Type: xdr.AssetTypeAssetTypeNative},
			Destination: destination,
			DestAsset:   xdr.Asset{Type: xdr.AssetTypeAssetTypeNative},
		},
	}}
	sendRes := xdr.OperationResult{Code: xdr.OperationResultCodeOpInner, Tr: &xdr.OperationResultTr{
		Type: xdr.OperationTypePathPaymentStrictSend,
		PathPaymentStrictSendResult: &xdr.PathPaymentStrictSendResult{
			Code: xdr.PathPaymentStrictSendResultCodePathPaymentStrictSendUnderfunded,
		},
	}}

	var r Reason
	got := withPathPaymentFailureDetail(nil, receive, 3, receiveRes, &r)
	if got == nil || got.PaymentType != "strict_receive" || got.OperationIndex != 3 || got.StrictReceive == nil {
		t.Errorf("receive diagnosis = %+v, want a strict_receive diagnosis at index 3", got)
	}
	if r.Context == "" {
		t.Error("receive: Reason.Context is empty")
	}

	r = Reason{}
	got = withPathPaymentFailureDetail(nil, send, 4, sendRes, &r)
	if got == nil || got.PaymentType != "strict_send" || got.OperationIndex != 4 || got.StrictSend == nil {
		t.Errorf("send diagnosis = %+v, want a strict_send diagnosis at index 4", got)
	}
	if r.Context == "" {
		t.Error("send: Reason.Context is empty")
	}
}

// TestNonPositiveAmount covers the malformed-amount note both path payment
// diagnoses append: silent for a positive amount, named for zero or below.
func TestNonPositiveAmount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value xdr.Int64
		want  string
	}{
		{name: "positive", value: 1, want: ""},
		{name: "zero", value: 0, want: " sendMax is 0.0000000, but it must be positive."},
		{name: "negative", value: -10_000_000, want: " sendMax is -1.0000000, but it must be positive."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := nonPositiveAmount("sendMax", tc.value); got != tc.want {
				t.Errorf("nonPositiveAmount(%d) = %q, want %q", tc.value, got, tc.want)
			}
		})
	}
}
