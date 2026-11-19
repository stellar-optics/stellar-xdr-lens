package lens_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens/format"
)

func strictReceiveOp(t *testing.T) *xdr.PathPaymentStrictReceiveOp {
	t.Helper()
	return &xdr.PathPaymentStrictReceiveOp{
		SendAsset:   xdr.Asset{Type: xdr.AssetTypeAssetTypeNative},
		SendMax:     10_0000000,
		Destination: mustMuxed(t, testDest),
		DestAsset:   creditAsset(t, "USDC", testIssuer),
		DestAmount:  5_0000000,
		Path:        []xdr.Asset{creditAsset(t, "EURC", testIssuer)},
	}
}

func TestPathPaymentStrictReceiveFailureContext(t *testing.T) {
	t.Parallel()

	eurc := creditAsset(t, "EURC", testIssuer)
	eurcLabel := "EURC:" + testIssuer
	usdcLabel := "USDC:" + testIssuer
	unrelated := creditAsset(t, "GBPX", testIssuer)

	tests := []struct {
		name     string
		mutate   func(*xdr.PathPaymentStrictReceiveOp)
		res      xdr.PathPaymentStrictReceiveResult
		want     []string
		wantHint string
		wantHop  int
	}{
		{
			name: "over send max names the limit and every book",
			res:  xdr.PathPaymentStrictReceiveResult{Code: xdr.PathPaymentStrictReceiveResultCodePathPaymentStrictReceiveOverSendmax},
			want: []string{
				"XLM -> " + eurcLabel + " -> " + usdcLabel + " (2 hop(s))",
				"sendMax of 10.0000000 XLM",
				"at most 2.0000000 XLM per 1 " + usdcLabel,
				"XLM -> " + eurcLabel + ", " + eurcLabel + " -> " + usdcLabel,
			},
			wantHop: -1,
		},
		{
			name:    "too few offers lists the books to check",
			res:     xdr.PathPaymentStrictReceiveResult{Code: xdr.PathPaymentStrictReceiveResultCodePathPaymentStrictReceiveTooFewOffers},
			want:    []string{"lacked the depth", eurcLabel + " -> " + usdcLabel},
			wantHop: -1,
		},
		{
			name:     "no issuer pinpoints the hop",
			res:      xdr.PathPaymentStrictReceiveResult{Code: xdr.PathPaymentStrictReceiveResultCodePathPaymentStrictReceiveNoIssuer, NoIssuer: &eurc},
			want:     []string{"issuer of " + eurcLabel + " does not exist", "hop 1 (XLM -> " + eurcLabel + ")"},
			wantHint: "choose a route that avoids it",
			wantHop:  0,
		},
		{
			name:     "no issuer for an asset off the route has no hop",
			res:      xdr.PathPaymentStrictReceiveResult{Code: xdr.PathPaymentStrictReceiveResultCodePathPaymentStrictReceiveNoIssuer, NoIssuer: &unrelated},
			want:     []string{"issuer of GBPX:" + testIssuer + " does not exist."},
			wantHint: "choose a route that avoids it",
			wantHop:  -1,
		},
		{
			name:    "no issuer without an asset stays generic",
			res:     xdr.PathPaymentStrictReceiveResult{Code: xdr.PathPaymentStrictReceiveResultCodePathPaymentStrictReceiveNoIssuer},
			want:    []string{"Route XLM"},
			wantHop: -1,
		},
		{
			name: "malformed names non-positive amounts and omits the rate",
			mutate: func(op *xdr.PathPaymentStrictReceiveOp) {
				op.SendMax = 0
				op.DestAmount = -1
			},
			res:     xdr.PathPaymentStrictReceiveResult{Code: xdr.PathPaymentStrictReceiveResultCodePathPaymentStrictReceiveMalformed},
			want:    []string{"sendMax is 0.0000000", "destAmount is -0.0000001"},
			wantHop: -1,
		},
		{
			name:    "other failures keep the route context",
			res:     xdr.PathPaymentStrictReceiveResult{Code: xdr.PathPaymentStrictReceiveResultCodePathPaymentStrictReceiveUnderfunded},
			want:    []string{"send at most 10.0000000 XLM", "deliver exactly 5.0000000 " + usdcLabel},
			wantHop: -1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			op := strictReceiveOp(t)
			if tc.mutate != nil {
				tc.mutate(op)
			}
			d := lens.DiagnosePathPaymentStrictReceiveFailure(op, &tc.res)
			if d == nil {
				t.Fatal("diagnosis is nil")
			}
			for _, want := range tc.want {
				if !strings.Contains(d.Context, want) {
					t.Errorf("Context = %q, want it to contain %q", d.Context, want)
				}
			}
			if !strings.Contains(d.Hint, tc.wantHint) {
				t.Errorf("Hint = %q, want it to contain %q", d.Hint, tc.wantHint)
			}
			if tc.wantHop < 0 && d.NoIssuerHop != nil {
				t.Errorf("NoIssuerHop = %+v, want nil", d.NoIssuerHop)
			}
			if tc.wantHop >= 0 && (d.NoIssuerHop == nil || d.NoIssuerHop.Index != tc.wantHop) {
				t.Errorf("NoIssuerHop = %+v, want index %d", d.NoIssuerHop, tc.wantHop)
			}
			if tc.mutate != nil && d.MaxSendPerDest != "" {
				t.Errorf("MaxSendPerDest = %q, want empty for non-positive amounts", d.MaxSendPerDest)
			}
		})
	}
}

func TestPathPaymentStrictReceiveFailureNilCases(t *testing.T) {
	t.Parallel()

	op := strictReceiveOp(t)
	failed := &xdr.PathPaymentStrictReceiveResult{Code: xdr.PathPaymentStrictReceiveResultCodePathPaymentStrictReceiveNoTrust}
	success := &xdr.PathPaymentStrictReceiveResult{Code: xdr.PathPaymentStrictReceiveResultCodePathPaymentStrictReceiveSuccess}

	if d := lens.DiagnosePathPaymentStrictReceiveFailure(nil, failed); d != nil {
		t.Errorf("nil op: got %+v, want nil", d)
	}
	if d := lens.DiagnosePathPaymentStrictReceiveFailure(op, nil); d != nil {
		t.Errorf("nil result: got %+v, want nil", d)
	}
	if d := lens.DiagnosePathPaymentStrictReceiveFailure(op, success); d != nil {
		t.Errorf("success: got %+v, want nil", d)
	}
}

func TestPathPaymentStrictReceiveSlippageTolerance(t *testing.T) {
	t.Parallel()

	failed := &xdr.PathPaymentStrictReceiveResult{Code: xdr.PathPaymentStrictReceiveResultCodePathPaymentStrictReceiveOverSendmax}
	d := lens.DiagnosePathPaymentStrictReceiveFailure(strictReceiveOp(t), failed)

	tests := []struct {
		name   string
		d      *lens.PathPaymentDiagnosis
		quoted xdr.Price
		want   string
		wantOK bool
	}{
		{name: "limit above the quote", d: d, quoted: xdr.Price{N: 8, D: 5}, want: "25.00%", wantOK: true},
		{name: "limit equal to the quote", d: d, quoted: xdr.Price{N: 2, D: 1}, want: "0.00%", wantOK: true},
		{name: "limit below the quote", d: d, quoted: xdr.Price{N: 4, D: 1}, want: "-50.00%", wantOK: true},
		{name: "zero numerator", d: d, quoted: xdr.Price{N: 0, D: 1}},
		{name: "zero denominator", d: d, quoted: xdr.Price{N: 1, D: 0}},
		{name: "no usable send max", d: &lens.PathPaymentDiagnosis{DestAmountStroops: 1}, quoted: xdr.Price{N: 1, D: 1}},
		{name: "no usable dest amount", d: &lens.PathPaymentDiagnosis{SendMaxStroops: 1}, quoted: xdr.Price{N: 1, D: 1}},
		{name: "nil diagnosis", quoted: xdr.Price{N: 1, D: 1}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := tc.d.SlippageTolerance(tc.quoted)
			if got != tc.want || ok != tc.wantOK {
				t.Errorf("SlippageTolerance() = (%q, %v), want (%q, %v)", got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

// TestPathPaymentStrictReceiveFixtureOutputs drives the registered fixture
// through the decode path and both output formats.
func TestPathPaymentStrictReceiveFixtureOutputs(t *testing.T) {
	t.Parallel()

	env, err := lens.DecodeAs(loadFixture(t, "tx_path_receive_failed.env"), "TransactionEnvelope")
	if err != nil {
		t.Fatalf("DecodeAs(env): %v", err)
	}
	res, err := lens.DecodeAs(loadFixture(t, "tx_path_receive_failed.res"), "TransactionResult")
	if err != nil {
		t.Fatalf("DecodeAs(res): %v", err)
	}
	summary, err := lens.ExplainPair(env, res)
	if err != nil {
		t.Fatalf("ExplainPair: %v", err)
	}

	var text bytes.Buffer
	if err := format.NewSummaryFormatter().Format(&text, summary); err != nil {
		t.Fatalf("Format: %v", err)
	}
	for _, want := range []string{"EURC:" + testIssuer, "send at most 10.0000000 XLM", "hop 1"} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("text output = %q, want it to contain %q", text.String(), want)
		}
	}

	raw, err := json.Marshal(summary)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var decoded struct {
		Operations []struct {
			PathPayment struct {
				PaymentType   string `json:"paymentType"`
				StrictReceive struct {
					SendMax       string   `json:"sendMax"`
					Path          []string `json:"path"`
					NoIssuerAsset string   `json:"noIssuerAsset"`
				} `json:"strictReceive"`
			} `json:"pathPayment"`
		} `json:"operations"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(decoded.Operations) != 1 {
		t.Fatalf("len(operations) = %d, want 1", len(decoded.Operations))
	}
	got := decoded.Operations[0].PathPayment
	if got.PaymentType != "strict_receive" {
		t.Errorf("paymentType = %q, want strict_receive", got.PaymentType)
	}
	if got.StrictReceive.SendMax != "10.0000000" {
		t.Errorf("sendMax = %q, want 10.0000000", got.StrictReceive.SendMax)
	}
	if len(got.StrictReceive.Path) != 1 || got.StrictReceive.Path[0] != "EURC:"+testIssuer {
		t.Errorf("path = %v, want the EURC hop", got.StrictReceive.Path)
	}
	if got.StrictReceive.NoIssuerAsset != "EURC:"+testIssuer {
		t.Errorf("noIssuerAsset = %q, want the EURC asset", got.StrictReceive.NoIssuerAsset)
	}
}
