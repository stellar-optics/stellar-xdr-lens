package lens_test

import (
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

func strictSendOp(t *testing.T) *xdr.PathPaymentStrictSendOp {
	t.Helper()
	return &xdr.PathPaymentStrictSendOp{
		SendAsset:   xdr.Asset{Type: xdr.AssetTypeAssetTypeNative},
		SendAmount:  10_0000000,
		Destination: mustMuxed(t, testDest),
		DestAsset:   creditAsset(t, "USDC", testIssuer),
		DestMin:     5_0000000,
		Path:        []xdr.Asset{creditAsset(t, "EURC", testIssuer)},
	}
}

func TestPathPaymentStrictSendFailureContext(t *testing.T) {
	t.Parallel()

	eurc := creditAsset(t, "EURC", testIssuer)
	eurcLabel := "EURC:" + testIssuer
	usdcLabel := "USDC:" + testIssuer

	tests := []struct {
		name     string
		mutate   func(*xdr.PathPaymentStrictSendOp)
		res      xdr.PathPaymentStrictSendResult
		want     []string
		wantHint []string
	}{
		{
			name: "under dest min states the exact threshold and remedies",
			res:  xdr.PathPaymentStrictSendResult{Code: xdr.PathPaymentStrictSendResultCodePathPaymentStrictSendUnderDestmin},
			want: []string{
				"destMin of 5.0000000 " + usdcLabel,
				"at least 0.5000000 " + usdcLabel + " per 1 XLM",
				testDest[:5],
			},
			wantHint: []string{
				"Relax destMin below 5.0000000 " + usdcLabel,
				"increase sendAmount above 10.0000000 XLM",
				"inspect liquidity on these books: XLM -> " + eurcLabel + ", " + eurcLabel + " -> " + usdcLabel,
			},
		},
		{
			name: "too few offers lists the books to check",
			res:  xdr.PathPaymentStrictSendResult{Code: xdr.PathPaymentStrictSendResultCodePathPaymentStrictSendTooFewOffers},
			want: []string{"lacked the depth", "XLM -> " + eurcLabel},
		},
		{
			name:     "no issuer pinpoints the hop",
			res:      xdr.PathPaymentStrictSendResult{Code: xdr.PathPaymentStrictSendResultCodePathPaymentStrictSendNoIssuer, NoIssuer: &eurc},
			want:     []string{"issuer of " + eurcLabel + " does not exist", "hop 1"},
			wantHint: []string{"choose a route that avoids it"},
		},
		{
			name: "no issuer without an asset stays generic",
			res:  xdr.PathPaymentStrictSendResult{Code: xdr.PathPaymentStrictSendResultCodePathPaymentStrictSendNoIssuer},
			want: []string{"Route XLM"},
		},
		{
			name: "malformed names non-positive amounts",
			mutate: func(op *xdr.PathPaymentStrictSendOp) {
				op.SendAmount = 0
				op.DestMin = 0
			},
			res:  xdr.PathPaymentStrictSendResult{Code: xdr.PathPaymentStrictSendResultCodePathPaymentStrictSendMalformed},
			want: []string{"sendAmount is 0.0000000", "destMin is 0.0000000"},
		},
		{
			name: "other failures keep the route context",
			res:  xdr.PathPaymentStrictSendResult{Code: xdr.PathPaymentStrictSendResultCodePathPaymentStrictSendUnderfunded},
			want: []string{"send exactly 10.0000000 XLM", "deliver at least 5.0000000 " + usdcLabel},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			op := strictSendOp(t)
			if tc.mutate != nil {
				tc.mutate(op)
			}
			d := lens.DiagnosePathPaymentStrictSendFailure(op, &tc.res)
			if d == nil {
				t.Fatal("diagnosis is nil")
			}
			for _, want := range tc.want {
				if !strings.Contains(d.Context, want) {
					t.Errorf("Context = %q, want it to contain %q", d.Context, want)
				}
			}
			for _, want := range tc.wantHint {
				if !strings.Contains(d.Hint, want) {
					t.Errorf("Hint = %q, want it to contain %q", d.Hint, want)
				}
			}
		})
	}
}

func TestPathPaymentStrictSendFailureNilCases(t *testing.T) {
	t.Parallel()

	op := strictSendOp(t)
	failed := &xdr.PathPaymentStrictSendResult{Code: xdr.PathPaymentStrictSendResultCodePathPaymentStrictSendNoTrust}
	success := &xdr.PathPaymentStrictSendResult{Code: xdr.PathPaymentStrictSendResultCodePathPaymentStrictSendSuccess}

	if d := lens.DiagnosePathPaymentStrictSendFailure(nil, failed); d != nil {
		t.Errorf("nil op: got %+v, want nil", d)
	}
	if d := lens.DiagnosePathPaymentStrictSendFailure(op, nil); d != nil {
		t.Errorf("nil result: got %+v, want nil", d)
	}
	if d := lens.DiagnosePathPaymentStrictSendFailure(op, success); d != nil {
		t.Errorf("success: got %+v, want nil", d)
	}
}

func TestPathPaymentStrictSendDestMinShortfall(t *testing.T) {
	t.Parallel()

	failed := &xdr.PathPaymentStrictSendResult{Code: xdr.PathPaymentStrictSendResultCodePathPaymentStrictSendUnderDestmin}
	d := lens.DiagnosePathPaymentStrictSendFailure(strictSendOp(t), failed)

	tests := []struct {
		name   string
		d      *lens.PathPaymentStrictSendDiagnosis
		quoted xdr.Int64
		want   int64
	}{
		{name: "quote below destMin", d: d, quoted: 4_7500000, want: 2500000},
		{name: "quote equal to destMin", d: d, quoted: 5_0000000, want: 0},
		{name: "quote above destMin", d: d, quoted: 6_0000000, want: 0},
		{name: "nil diagnosis", quoted: 1, want: 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.d.DestMinShortfall(tc.quoted); got != tc.want {
				t.Errorf("DestMinShortfall(%d) = %d, want %d", tc.quoted, got, tc.want)
			}
		})
	}
}

// TestPathPaymentStrictSendExplainPair checks that the remediation replaces
// the generic slippage hint on the public explain path.
func TestPathPaymentStrictSendExplainPair(t *testing.T) {
	t.Parallel()

	op := xdr.Operation{Body: xdr.OperationBody{
		Type:                    xdr.OperationTypePathPaymentStrictSend,
		PathPaymentStrictSendOp: strictSendOp(t),
	}}
	env, err := lens.DecodeAs(buildEnvelope(t, noMemo(), noCond(), op), "TransactionEnvelope")
	if err != nil {
		t.Fatalf("DecodeAs(env): %v", err)
	}
	txRes := xdr.TransactionResult{
		FeeCharged: 100,
		Result: xdr.TransactionResultResult{
			Code: xdr.TransactionResultCodeTxFailed,
			Results: &[]xdr.OperationResult{{
				Code: xdr.OperationResultCodeOpInner,
				Tr: &xdr.OperationResultTr{
					Type: xdr.OperationTypePathPaymentStrictSend,
					PathPaymentStrictSendResult: &xdr.PathPaymentStrictSendResult{
						Code: xdr.PathPaymentStrictSendResultCodePathPaymentStrictSendUnderDestmin,
					},
				},
			}},
		},
	}
	res, err := lens.DecodeAs(mustMarshal(t, txRes), "TransactionResult")
	if err != nil {
		t.Fatalf("DecodeAs(res): %v", err)
	}

	summary, err := lens.ExplainPair(env, res)
	if err != nil {
		t.Fatalf("ExplainPair: %v", err)
	}
	got := summary.Operations[0]
	if got.PathPayment == nil || got.PathPayment.StrictSend == nil {
		t.Fatalf("PathPayment = %+v, want a strict-send diagnosis", got.PathPayment)
	}
	if !strings.Contains(got.Result.Context, "destMin of 5.0000000 USDC:"+testIssuer) {
		t.Errorf("Context = %q, want the exact destMin and asset", got.Result.Context)
	}
	if !strings.Contains(got.Result.Hint, "Relax destMin below 5.0000000") {
		t.Errorf("Hint = %q, want the destMin remediation", got.Result.Hint)
	}
}
