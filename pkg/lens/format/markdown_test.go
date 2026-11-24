package format_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens/format"
)

func TestMarkdownFormatterCanonicalFixtures(t *testing.T) {
	tests := []struct {
		name        string
		result      string
		wantContain []string
	}{
		{
			name:   "tx_success.env",
			result: "tx_success.res",
			wantContain: []string{
				"[STATUS: SUCCESS]",
				"| Status | Succeeded |",
				"| # | Operation | Source | Status | Details |",
				"| Succeeded |",
			},
		},
		{
			name:   "tx_failed.env",
			result: "tx_failed.res",
			wantContain: []string{
				"[STATUS: FAILED]",
				"| Status | Failed |",
				"invoke_host_function_trapped",
				"| Failed |",
			},
		},
		{
			name:   "tx_feebump.env",
			result: "tx_failed.res",
			wantContain: []string{
				"| Fee-bump source |",
				"| Inner result |",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			envelope := decodeFixture(t, tc.name)
			result := decodeFixture(t, tc.result)
			summary, err := lens.ExplainPair(envelope, result)
			if err != nil {
				t.Fatalf("ExplainPair() error = %v", err)
			}

			var out bytes.Buffer
			if err := format.NewMarkdownFormatter().FormatSummary(&out, summary); err != nil {
				t.Fatalf("FormatSummary() error = %v", err)
			}
			got := out.String()
			for _, want := range tc.wantContain {
				if !strings.Contains(got, want) {
					t.Errorf("Markdown output does not contain %q:\n%s", want, got)
				}
			}
		})
	}
}

func TestMarkdownFormatterFormatValue(t *testing.T) {
	value := decodeFixture(t, "tx_success.env")

	var out bytes.Buffer
	if err := format.NewMarkdownFormatter().Format(&out, value); err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	if !strings.Contains(out.String(), "| Transaction | Details |") {
		t.Errorf("Format() output is not a Markdown table:\n%s", out.String())
	}

	if err := format.NewMarkdownFormatter().Format(&out, nil); err == nil {
		t.Error("Format(nil) error = nil, want error")
	}
	if err := format.NewMarkdownFormatter().Format(&out, &lens.Value{Type: "ScVal"}); err == nil {
		t.Error("Format(value without a node) error = nil, want error")
	}
	if err := format.NewMarkdownFormatter().Format(&out, &lens.Value{
		Type: "ScVal",
		Node: &lens.Node{},
	}); err == nil {
		t.Error("Format(non-transaction) error = nil, want error")
	}
}

func TestMarkdownFormatterFormatSummaryEscapesAndHandlesOptionalFields(t *testing.T) {
	summary := &lens.Summary{
		Headline:    "failed | transaction\\path\nwith a line break",
		Source:      "source|account",
		SeqNum:      17,
		Fee:         100,
		FeeCharged:  80,
		Memo:        "first line\nsecond | line",
		SignatureCt: 1,
		Preconds:    []string{"valid | condition"},
		Operations: []lens.OpSummary{
			{
				Index:  0,
				Type:   "payment",
				Source: "op|source",
				Detail: "send | funds\nnow",
				Result: &lens.Reason{
					Code:    "payment_underfunded",
					Summary: "not enough | funds",
					Hint:    "add funds\nfirst",
				},
			},
			{
				Index:  1,
				Type:   "manage_data",
				Detail: "manage_data",
				Result: &lens.Reason{Code: "success", Success: true},
			},
			{Index: 2, Type: "unknown"},
		},
		Outcome: &lens.Outcome{
			Success: false,
			Reason: lens.Reason{
				Code:    "tx_failed",
				Summary: "some operation failed",
			},
			InnerReason: &lens.Reason{
				Code:    "inner_failed",
				Summary: "inner result",
			},
		},
		DiagnosticTrace: []lens.DiagnosticDetail{{
			InSuccessfulCall: false,
			Type:             "diagnostic",
			ContractID:       "CABC123",
			Topics:           []string{"panic"},
			Data:             "balance | too low",
		}},
	}

	var out bytes.Buffer
	if err := format.NewMarkdownFormatter().FormatSummary(&out, summary); err != nil {
		t.Fatalf("FormatSummary() error = %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"[STATUS: FAILED] [FEE: 80 stroops]",
		"| Summary | failed \\| transaction\\\\path<br>with a line break |",
		"| Source | source\\|account |",
		"| Fee bid (stroops) | 100 |",
		"| Fee charged (stroops) | 80 |",
		"| Memo | first line<br>second \\| line |",
		"| Precondition | valid \\| condition |",
		"| # | Operation | Source | Status | Details |",
		"| 0 | payment | op\\|source | Failed | send \\| funds<br>now<br>payment_underfunded: not enough \\| funds<br>Hint: add funds<br>first |",
		"| 1 | manage_data | - | Succeeded | manage_data<br>success:  |",
		"| 2 | unknown | - | Not reported | - |",
		"| Inner result | inner_failed: inner result |",
		"<details>\n<summary>Diagnostic Trace</summary>",
		"[failed] diagnostic event from CABC123 topics=[panic] data=\"balance | too low\"",
		"</details>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Markdown output does not contain %q:\n%s", want, got)
		}
	}
}

func TestMarkdownFormatterWithoutOutcomeOrOperations(t *testing.T) {
	var out bytes.Buffer
	err := format.NewMarkdownFormatter().FormatSummary(&out, &lens.Summary{
		Headline: "Envelope only",
	})
	if err != nil {
		t.Fatalf("FormatSummary() error = %v", err)
	}
	if !strings.Contains(out.String(), "| - | No operations | - | - | - |") {
		t.Errorf("output should explicitly show the empty operation list:\n%s", out.String())
	}

	if err := format.NewMarkdownFormatter().FormatSummary(&out, nil); err == nil {
		t.Error("FormatSummary(nil) error = nil, want error")
	}
}

func TestMarkdownFormatterWriteError(t *testing.T) {
	wantErr := errors.New("closed writer")
	err := format.NewMarkdownFormatter().FormatSummary(errorWriter{err: wantErr}, &lens.Summary{
		Headline: "test",
	})
	if !errors.Is(err, wantErr) {
		t.Errorf("FormatSummary() error = %v, want wrapped %v", err, wantErr)
	}
}

type errorWriter struct {
	err error
}

func (w errorWriter) Write([]byte) (int, error) {
	return 0, w.err
}

func decodeFixture(t *testing.T, name string) *lens.Value {
	t.Helper()
	path := filepath.Join("..", "..", "..", "testdata", name+".txt")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading fixture %s: %v", path, err)
	}
	value, err := lens.Decode(strings.TrimSpace(string(content)))
	if err != nil {
		t.Fatalf("decoding fixture %s: %v", name, err)
	}
	return value
}
