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

func TestDiagnoseCreateAccount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		starting xdr.Int64
		code     xdr.CreateAccountResultCode
		want     []string
		wantNone bool
	}{
		{
			name: "already exists recommends a payment",
			code: xdr.CreateAccountResultCodeCreateAccountAlreadyExist,
			want: []string{"Account already exists on the ledger. Use a Payment operation to fund existing accounts rather than CreateAccount."},
		},
		{
			name:     "low reserve reports the exact shortfall",
			starting: 2500000,
			code:     xdr.CreateAccountResultCodeCreateAccountLowReserve,
			want:     []string{"1.0 XLM minimum", "10000000 stroops", "shortfall of 7500000 stroops (0.7500000 XLM)"},
		},
		{
			name:     "one stroop under the minimum",
			starting: 9999999,
			code:     xdr.CreateAccountResultCodeCreateAccountLowReserve,
			want:     []string{"shortfall of 1 stroops"},
		},
		{
			name:     "a balance at the default minimum implies a higher reserve",
			starting: 10000000,
			code:     xdr.CreateAccountResultCodeCreateAccountLowReserve,
			want:     []string{"meets the 1.0 XLM minimum", "higher base reserve"},
		},
		{
			name:     "other codes are not diagnosed",
			code:     xdr.CreateAccountResultCodeCreateAccountUnderfunded,
			wantNone: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := lens.DiagnoseCreateAccount(
				&xdr.CreateAccountOp{Destination: mustAccountID(t, testDest), StartingBalance: tc.starting},
				&xdr.CreateAccountResult{Code: tc.code})
			if tc.wantNone && got != "" {
				t.Errorf("DiagnoseCreateAccount() = %q, want empty", got)
			}
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("DiagnoseCreateAccount() = %q, want it to contain %q", got, want)
				}
			}
		})
	}
}

func TestDiagnoseCreateAccountNilInputs(t *testing.T) {
	t.Parallel()

	if got := lens.DiagnoseCreateAccount(nil, &xdr.CreateAccountResult{}); got != "" {
		t.Errorf("nil op: got %q, want empty", got)
	}
	if got := lens.DiagnoseCreateAccount(&xdr.CreateAccountOp{}, nil); got != "" {
		t.Errorf("nil result: got %q, want empty", got)
	}
}

// TestCreateAccountExplainOutputs checks the diagnosis in text and JSON output.
func TestCreateAccountExplainOutputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		code       xdr.CreateAccountResultCode
		want       string
		wantHint   string
		wantNoDiag bool
	}{
		{
			name:     "already exists",
			code:     xdr.CreateAccountResultCodeCreateAccountAlreadyExist,
			want:     "Use a Payment operation",
			wantHint: "Use a payment",
		},
		{
			name:     "low reserve",
			code:     xdr.CreateAccountResultCodeCreateAccountLowReserve,
			want:     "1.0 XLM minimum",
			wantHint: "at least two base reserves (1.0 XLM on Mainnet)",
		},
		{
			name:       "underfunded keeps the generic explanation",
			code:       xdr.CreateAccountResultCodeCreateAccountUnderfunded,
			wantNoDiag: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			op := xdr.Operation{Body: xdr.OperationBody{
				Type: xdr.OperationTypeCreateAccount,
				CreateAccountOp: &xdr.CreateAccountOp{
					Destination:     mustAccountID(t, testDest),
					StartingBalance: 5000000,
				},
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
							Type:                xdr.OperationTypeCreateAccount,
							CreateAccountResult: &xdr.CreateAccountResult{Code: tc.code},
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

			got := summary.Operations[0].Result
			if tc.wantNoDiag {
				if got.Context != "" {
					t.Errorf("Context = %q, want empty", got.Context)
				}
				return
			}
			if !strings.Contains(got.Hint, tc.wantHint) {
				t.Errorf("Hint = %q, want it to contain %q", got.Hint, tc.wantHint)
			}

			var text bytes.Buffer
			if err := format.NewSummaryFormatter().Format(&text, summary); err != nil {
				t.Fatalf("Format: %v", err)
			}
			if !strings.Contains(text.String(), tc.want) {
				t.Errorf("text output = %q, want it to contain %q", text.String(), tc.want)
			}

			raw, err := json.Marshal(summary)
			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}
			var decoded struct {
				Operations []struct {
					Result struct {
						Context string `json:"context"`
					} `json:"result"`
				} `json:"operations"`
			}
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Fatalf("json.Unmarshal: %v", err)
			}
			if !strings.Contains(decoded.Operations[0].Result.Context, tc.want) {
				t.Errorf("JSON context = %q, want it to contain %q", decoded.Operations[0].Result.Context, tc.want)
			}
		})
	}
}
