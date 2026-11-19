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

// inflationEnvelope builds a one-operation inflation transaction.
func inflationEnvelope() xdr.TransactionEnvelope {
	source := explainTestAccount(0xA1)
	return xdr.TransactionEnvelope{
		Type: xdr.EnvelopeTypeEnvelopeTypeTx,
		V1: &xdr.TransactionV1Envelope{
			Tx: xdr.Transaction{
				SourceAccount: xdr.MuxedAccount{
					Type:    xdr.CryptoKeyTypeKeyTypeEd25519,
					Ed25519: source.Ed25519,
				},
				Fee:        100,
				SeqNum:     4242,
				Operations: []xdr.Operation{{Body: xdr.OperationBody{Type: xdr.OperationTypeInflation}}},
			},
		},
	}
}

// inflationResult builds a failed result carrying one operation result.
func inflationResult(opResult xdr.OperationResult) xdr.TransactionResult {
	opResults := []xdr.OperationResult{opResult}
	return xdr.TransactionResult{
		FeeCharged: 100,
		Result: xdr.TransactionResultResult{
			Code:    xdr.TransactionResultCodeTxFailed,
			Results: &opResults,
		},
	}
}

func inflationNotTimeResult() xdr.OperationResult {
	return xdr.OperationResult{
		Code: xdr.OperationResultCodeOpInner,
		Tr: &xdr.OperationResultTr{
			Type:            xdr.OperationTypeInflation,
			InflationResult: &xdr.InflationResult{Code: xdr.InflationResultCodeInflationNotTime},
		},
	}
}

func TestExplainInflationFailureNotesRetirement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		opResult xdr.OperationResult
		wantCode string
	}{
		{name: "inflation not time", opResult: inflationNotTimeResult(), wantCode: "inflation_not_time"},
		{
			name:     "operation not supported on current networks",
			opResult: xdr.OperationResult{Code: xdr.OperationResultCodeOpNotSupported},
			wantCode: "op_not_supported",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			summary := explainSyntheticPair(t, inflationEnvelope(), inflationResult(tc.opResult))
			op := summary.Operations[0]
			if op.Result == nil || op.Result.Code != tc.wantCode {
				t.Fatalf("Result = %+v, want code %q", op.Result, tc.wantCode)
			}
			for _, want := range []string{"CAP-0026", "Protocol 12"} {
				if !strings.Contains(op.Result.Summary, want) {
					t.Errorf("Summary = %q, want it to contain %q", op.Result.Summary, want)
				}
				if !strings.Contains(op.Deprecation, want) {
					t.Errorf("Deprecation = %q, want it to contain %q", op.Deprecation, want)
				}
			}
		})
	}
}

// TestInflationReasonStandsWithoutEnvelope covers result-only explanations,
// where only the static reason table can carry the retirement note.
func TestInflationReasonStandsWithoutEnvelope(t *testing.T) {
	t.Parallel()

	r := lens.ExplainCode("InflationResultCodeInflationNotTime")
	if !strings.Contains(r.Summary, "CAP-0026") || !strings.Contains(r.Summary, "Protocol 12") {
		t.Errorf("Summary = %q, want the CAP-0026 retirement", r.Summary)
	}
}

func TestNotSupportedStaysGenericForOtherOperations(t *testing.T) {
	t.Parallel()

	env := inflationEnvelope()
	env.V1.Tx.Operations[0].Body = xdr.OperationBody{
		Type:           xdr.OperationTypeBumpSequence,
		BumpSequenceOp: &xdr.BumpSequenceOp{BumpTo: 5000},
	}
	summary := explainSyntheticPair(t, env, inflationResult(xdr.OperationResult{Code: xdr.OperationResultCodeOpNotSupported}))
	op := summary.Operations[0]
	if op.Deprecation != "" {
		t.Errorf("Deprecation = %q, want empty", op.Deprecation)
	}
	if generic := lens.ExplainCode("OperationResultCodeOpNotSupported"); op.Result.Summary != generic.Summary {
		t.Errorf("Summary = %q, want the generic %q", op.Result.Summary, generic.Summary)
	}
}

// TestInflationBannerIsConsistentAcrossFormats renders one summary every way
// the issue names, with and without a paired result.
func TestInflationBannerIsConsistentAcrossFormats(t *testing.T) {
	t.Parallel()

	const banner = "CAP-0026 permanently disabled the inflation mechanism in Protocol 12"

	envelopeOnly, err := lens.Explain(mustDecodeForExplain(t, mustMarshal(t, inflationEnvelope()), "TransactionEnvelope"))
	if err != nil {
		t.Fatalf("Explain() error = %v", err)
	}
	paired := explainSyntheticPair(t, inflationEnvelope(), inflationResult(inflationNotTimeResult()))

	for name, summary := range map[string]*lens.Summary{"envelope only": envelopeOnly, "paired": paired} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var text bytes.Buffer
			if err := format.NewSummaryFormatter(format.WithSummaryPalette(format.NoColor)).Format(&text, summary); err != nil {
				t.Fatalf("text format error = %v", err)
			}
			var markdown bytes.Buffer
			if err := format.NewMarkdownFormatter().FormatSummary(&markdown, summary); err != nil {
				t.Fatalf("markdown format error = %v", err)
			}
			encoded, err := json.Marshal(summary)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}

			for output, got := range map[string]string{
				"text":     text.String(),
				"markdown": markdown.String(),
				"json":     string(encoded),
			} {
				if !strings.Contains(got, banner) {
					t.Errorf("%s output = %q, want the retirement banner", output, got)
				}
			}
		})
	}
}
