package format_test

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens/format"
)

const fixtureDir = "../../../testdata"

func loadValue(t *testing.T, name string) *lens.Value {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(fixtureDir, name+".txt"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	v, err := lens.Decode(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatalf("decoding fixture %s: %v", name, err)
	}
	return v
}

func TestTreeFormatter(t *testing.T) {
	t.Parallel()

	v := loadValue(t, "tx_success.env")

	var buf bytes.Buffer
	f := format.NewTreeFormatter(format.WithPalette(format.NoColor))
	if err := f.Format(&buf, v); err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	out := buf.String()

	// The type header, real field names and an enriched address must appear.
	for _, want := range []string{"TransactionEnvelope", "SourceAccount", "Fee", "SeqNum", "G"} {
		if !strings.Contains(out, want) {
			t.Errorf("tree output does not contain %q\n%s", want, out)
		}
	}
	// Box drawing indicates the tree actually nested.
	if !strings.Contains(out, "└─") && !strings.Contains(out, "├─") {
		t.Error("tree output has no branch characters; it did not nest")
	}
}

func TestTreeFormatterWithoutColorEmitsNoEscapes(t *testing.T) {
	t.Parallel()

	v := loadValue(t, "tx_success.env")

	var buf bytes.Buffer
	f := format.NewTreeFormatter(format.WithPalette(format.NoColor))
	if err := f.Format(&buf, v); err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	if strings.Contains(buf.String(), "\x1b[") {
		t.Error("NoColor palette still emitted ANSI escape sequences")
	}
}

func TestTreeFormatterWithColorEmitsEscapes(t *testing.T) {
	t.Parallel()

	v := loadValue(t, "tx_success.env")

	var buf bytes.Buffer
	f := format.NewTreeFormatter(format.WithPalette(format.ColorPalette))
	if err := f.Format(&buf, v); err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	if !strings.Contains(buf.String(), "\x1b[") {
		t.Error("ColorPalette emitted no ANSI escape sequences")
	}
}

func TestTreeFormatterRespectsMaxDepth(t *testing.T) {
	t.Parallel()

	v := loadValue(t, "tx_success.env")

	deep := renderTree(t, v, 0)
	shallow := renderTree(t, v, 2)

	if len(shallow) >= len(deep) {
		t.Errorf("depth-limited output (%d bytes) is not shorter than full output (%d bytes)",
			len(shallow), len(deep))
	}
	if !strings.Contains(shallow, "hidden") {
		t.Error("depth-limited output does not say that levels were hidden")
	}
}

func renderTree(t *testing.T, v *lens.Value, depth int) string {
	t.Helper()
	var buf bytes.Buffer
	f := format.NewTreeFormatter(format.WithPalette(format.NoColor), format.WithMaxDepth(depth))
	if err := f.Format(&buf, v); err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	return buf.String()
}

// TestJSONFormatterShapeIsStable pins the documented output contract, which
// scripts and jq pipelines depend on.
func TestJSONFormatterShapeIsStable(t *testing.T) {
	t.Parallel()

	v := loadValue(t, "tx_success.env")

	var buf bytes.Buffer
	f := format.NewJSONFormatter()
	if err := f.Format(&buf, v); err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	var doc struct {
		Type  string         `json:"type"`
		Value map[string]any `json:"value"`
	}
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}
	if doc.Type != "TransactionEnvelope" {
		t.Errorf("type = %q, want TransactionEnvelope", doc.Type)
	}
	if len(doc.Value) == 0 {
		t.Error("value object is empty")
	}
	if _, ok := doc.Value["Type"]; !ok {
		t.Errorf("value has no Type discriminant; keys were %v", keys(doc.Value))
	}
}

// TestJSONFormatterOmitsAbsentUnionArms guards the property that keeps the
// JSON readable: the 27 unset arms of an operation union must not appear.
func TestJSONFormatterOmitsAbsentUnionArms(t *testing.T) {
	t.Parallel()

	v := loadValue(t, "tx_failed.res")

	var buf bytes.Buffer
	if err := format.NewJSONFormatter().Format(&buf, v); err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	out := buf.String()

	if strings.Contains(out, "null") {
		t.Errorf("JSON output contains null; absent union arms should be omitted\n%s", out)
	}
	// A result for an invoke-host-function op must not carry a PaymentResult.
	if strings.Contains(out, "PaymentResult") {
		t.Error("JSON output contains PaymentResult for a Soroban transaction")
	}
}

func TestJSONFormatterIsDeterministic(t *testing.T) {
	t.Parallel()

	v := loadValue(t, "tx_soroban.env")

	var first, second bytes.Buffer
	f := format.NewJSONFormatter()
	if err := f.Format(&first, v); err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	if err := f.Format(&second, v); err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	if first.String() != second.String() {
		t.Error("JSON output differs between runs; it must be deterministic for CI use")
	}
}

func TestJSONFormatterCompact(t *testing.T) {
	t.Parallel()

	v := loadValue(t, "tx_success.env")

	var buf bytes.Buffer
	f := format.NewJSONFormatter(format.WithIndent(""))
	if err := f.Format(&buf, v); err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	// One trailing newline from the encoder, no internal ones.
	if strings.Count(strings.TrimRight(buf.String(), "\n"), "\n") != 0 {
		t.Error("compact JSON spans multiple lines")
	}
}

func TestJSONFormatterAnnotations(t *testing.T) {
	t.Parallel()

	v := loadValue(t, "tx_failed.res")

	var buf bytes.Buffer
	f := format.NewJSONFormatter(format.WithAnnotations(true))
	if err := f.Format(&buf, v); err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	if !strings.Contains(buf.String(), `"display"`) {
		t.Error("annotated JSON has no display field; enrichment was dropped")
	}
}

func TestSummaryFormatter(t *testing.T) {
	t.Parallel()

	env := loadValue(t, "tx_failed.env")
	res := loadValue(t, "tx_failed.res")

	summary, err := lens.ExplainPair(env, res)
	if err != nil {
		t.Fatalf("ExplainPair() error = %v", err)
	}

	var buf bytes.Buffer
	f := format.NewSummaryFormatter(format.WithSummaryPalette(format.NoColor))
	if err := f.Format(&buf, summary); err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	out := buf.String()

	for _, want := range []string{
		"Source",
		"Operations",
		"Outcome",
		"invoke_host_function",
		"trapped",
	} {
		if !strings.Contains(strings.ToLower(out), strings.ToLower(want)) {
			t.Errorf("summary output does not mention %q\n%s", want, out)
		}
	}
	// A failed operation must be marked as such.
	if !strings.Contains(out, "✗") {
		t.Error("summary does not mark the failed operation")
	}
}

func TestSummaryFormatterVerboseIncludesConstants(t *testing.T) {
	t.Parallel()

	res := loadValue(t, "tx_failed.res")
	summary, err := lens.Explain(res)
	if err != nil {
		t.Fatalf("Explain() error = %v", err)
	}

	var plain, verbose bytes.Buffer
	if err := format.NewSummaryFormatter().Format(&plain, summary); err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	if err := format.NewSummaryFormatter(format.WithVerbose(true)).Format(&verbose, summary); err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	if !strings.Contains(verbose.String(), "TransactionResultCode") {
		t.Error("verbose summary does not include the XDR constant")
	}
	if strings.Contains(plain.String(), "TransactionResultCode") {
		t.Error("non-verbose summary leaked the XDR constant")
	}
}

func TestDiffFormatter(t *testing.T) {
	t.Parallel()

	left := loadValue(t, "tx_soroban.res")
	right := loadValue(t, "tx_failed.res")

	d, err := lens.Diff(left, right)
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}

	var buf bytes.Buffer
	f := format.NewDiffFormatter(format.WithDiffPalette(format.NoColor))
	if err := f.Format(&buf, d); err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	out := buf.String()

	if !strings.Contains(out, "Result.Code") {
		t.Errorf("diff output does not name the changed path\n%s", out)
	}
	if !strings.Contains(out, "~") {
		t.Error("diff output has no modification marker")
	}
	if !strings.Contains(out, "changed") {
		t.Error("diff output has no summary line")
	}
}

func TestDiffFormatterEqualValues(t *testing.T) {
	t.Parallel()

	v := loadValue(t, "tx_success.env")
	d, err := lens.Diff(v, v)
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}

	var buf bytes.Buffer
	if err := format.NewDiffFormatter().Format(&buf, d); err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	if !strings.Contains(buf.String(), "no differences") {
		t.Errorf("diff of identical values does not say so\n%s", buf.String())
	}
}

func TestFormattersRejectNil(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	if err := format.NewTreeFormatter().Format(&buf, nil); err == nil {
		t.Error("TreeFormatter.Format(nil) error = nil, want an error")
	}
	if err := format.NewJSONFormatter().Format(&buf, nil); err == nil {
		t.Error("JSONFormatter.Format(nil) error = nil, want an error")
	}
	if err := format.NewSummaryFormatter().Format(&buf, nil); err == nil {
		t.Error("SummaryFormatter.Format(nil) error = nil, want an error")
	}
	if err := format.NewDiffFormatter().Format(&buf, nil); err == nil {
		t.Error("DiffFormatter.Format(nil) error = nil, want an error")
	}
}

func TestPaletteFor(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	tests := []struct {
		mode      string
		wantColor bool
	}{
		{"always", true},
		{"never", false},
		{"auto", false}, // a bytes.Buffer is not a terminal
	}

	for _, tc := range tests {
		t.Run(tc.mode, func(t *testing.T) {
			got := format.PaletteFor(&buf, tc.mode)
			hasColor := got.Reset != ""
			if hasColor != tc.wantColor {
				t.Errorf("PaletteFor(%q) colour = %v, want %v", tc.mode, hasColor, tc.wantColor)
			}
		})
	}
}

// TestShouldColorHonoursNoColor covers the https://no-color.org convention.
func TestShouldColorHonoursNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	if format.ShouldColor(os.Stdout) {
		t.Error("ShouldColor() = true with NO_COLOR set, want false")
	}

	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "dumb")
	if format.ShouldColor(os.Stdout) {
		t.Error("ShouldColor() = true with TERM=dumb, want false")
	}
}

func TestHTMLFormatter(t *testing.T) {
	t.Parallel()

	v := loadValue(t, "tx_success.env")

	var buf bytes.Buffer
	f := format.NewHTMLFormatter(format.WithHTMLTitle("Test Audit Report"))
	if err := f.Format(&buf, v); err != nil {
		t.Fatalf("HTMLFormatter.Format() error = %v", err)
	}
	out := buf.String()

	for _, want := range []string{
		"<!DOCTYPE html>",
		"<title>Test Audit Report - TransactionEnvelope</title>",
		"<h1>Test Audit Report</h1>",
		"Transaction Overview",
		"Decoded Node Tree",
		"tree-container",
		"SourceAccount",
		"Generated offline",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("HTML output missing expected string %q", want)
		}
	}

	// Verify nil value handling
	if err := f.Format(&buf, nil); err == nil {
		t.Error("HTMLFormatter.Format(nil) error = nil, want error")
	}
	if err := f.FormatSummary(&buf, nil); err == nil {
		t.Error("HTMLFormatter.FormatSummary(nil) error = nil, want error")
	}

	// Verify FormatSummary
	summary, err := lens.Explain(v)
	if err != nil {
		t.Fatalf("Explain() error = %v", err)
	}
	var sumBuf bytes.Buffer
	if err := f.FormatSummary(&sumBuf, summary); err != nil {
		t.Fatalf("FormatSummary() error = %v", err)
	}
	if !strings.Contains(sumBuf.String(), "Transaction Overview") {
		t.Error("FormatSummary output missing overview section")
	}
}

func TestHTMLFormatterFailedTx(t *testing.T) {
	t.Parallel()

	v := loadValue(t, "tx_failed.res")
	var buf bytes.Buffer
	f := format.NewHTMLFormatter()
	if err := f.Format(&buf, v); err != nil {
		t.Fatalf("HTMLFormatter.Format() error = %v", err)
	}
	out := buf.String()

	if !strings.Contains(out, "TransactionResult") {
		t.Errorf("HTML output missing TransactionResult type badge")
	}
}

func TestMermaidFormatter(t *testing.T) {
	t.Parallel()

	v := loadValue(t, "tx_success.env")

	var buf bytes.Buffer
	f := format.NewMermaidFormatter(format.WithMermaidDirection("TD"))
	if err := f.Format(&buf, v); err != nil {
		t.Fatalf("MermaidFormatter.Format() error = %v", err)
	}
	out := buf.String()

	if !strings.HasPrefix(out, "graph TD\n") {
		t.Errorf("Mermaid output does not start with 'graph TD', got:\n%s", out)
	}
	if !strings.Contains(out, "Source") {
		t.Errorf("Mermaid output does not contain source account:\n%s", out)
	}
	if !strings.Contains(out, "-->|") {
		t.Errorf("Mermaid output does not contain operation arrow:\n%s", out)
	}

	// Test custom direction
	var lrBuf bytes.Buffer
	fLR := format.NewMermaidFormatter(format.WithMermaidDirection("LR"))
	if err := fLR.Format(&lrBuf, v); err != nil {
		t.Fatalf("MermaidFormatter.Format(LR) error = %v", err)
	}
	if !strings.HasPrefix(lrBuf.String(), "graph LR\n") {
		t.Errorf("Mermaid output with LR does not start with 'graph LR'")
	}

	// Test fallback for non-envelope type
	assetVal := loadValue(t, "asset_usdc")
	var assetBuf bytes.Buffer
	if err := f.Format(&assetBuf, assetVal); err != nil {
		t.Fatalf("MermaidFormatter.Format(asset) error = %v", err)
	}
	if !strings.Contains(assetBuf.String(), "Asset") || !strings.Contains(assetBuf.String(), "USDC") {
		t.Errorf("Mermaid fallback does not render node structure:\n%s", assetBuf.String())
	}

	// Test transaction result with operations
	resVal := loadValue(t, "tx_failed.res")
	var resBuf bytes.Buffer
	if err := f.Format(&resBuf, resVal); err != nil {
		t.Fatalf("MermaidFormatter.Format(res) error = %v", err)
	}
	if !strings.Contains(resBuf.String(), "op_0") {
		t.Errorf("Mermaid output missing op_0:\n%s", resBuf.String())
	}

	// Test fallback for non-transaction struct with children
	hdrVal := loadValue(t, "ledger_header")
	var hdrBuf bytes.Buffer
	if err := f.Format(&hdrBuf, hdrVal); err != nil {
		t.Fatalf("MermaidFormatter.Format(ledger_header) error = %v", err)
	}
	if !strings.Contains(hdrBuf.String(), "LedgerHeader") || !strings.Contains(hdrBuf.String(), "root --> child_") {
		t.Errorf("Mermaid fallback does not render node children:\n%s", hdrBuf.String())
	}

	// Test nil value handling
	if err := f.Format(&buf, nil); err == nil {
		t.Error("MermaidFormatter.Format(nil) error = nil, want error")
	}
}

// TestFormattersRenderOperationContext ensures the argument-specific
// diagnostics resolved by Explain (here, the manage_data entry key and the
// reserve it locks) survive into both human-facing renderings.
func TestFormattersRenderOperationContext(t *testing.T) {
	t.Parallel()

	summary := &lens.Summary{
		Kind:     "transaction",
		Headline: `Transaction failed at operation 0, manage_data (set data entry "profile" (7 bytes)): The account cannot meet the reserve required for another data entry.`,
		Operations: []lens.OpSummary{{
			Index:  0,
			Type:   "manage_data",
			Detail: `set data entry "profile" (7 bytes)`,
			Result: &lens.Reason{
				Code:    "manage_data_low_reserve",
				Summary: "The account cannot meet the reserve required for another data entry.",
				Context: `data entry "profile" adds one subentry, and each subentry locks 0.5 XLM of reserve; the account cannot lock another 0.5 XLM.`,
				Hint:    "Each data entry is a subentry and locks 0.5 XLM of base reserve. Free up reserve or remove another entry, then retry.",
			},
		}},
		Outcome: &lens.Outcome{
			Success: false,
			Reason:  lens.Reason{Code: "tx_failed", Summary: "The transaction was well-formed and authorised, but at least one of its operations failed."},
		},
	}

	var textOut bytes.Buffer
	if err := format.NewSummaryFormatter().Format(&textOut, summary); err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	if !strings.Contains(textOut.String(), `data entry "profile" adds one subentry`) {
		t.Errorf("text output does not render the operation context:\n%s", textOut.String())
	}

	var markdownOut bytes.Buffer
	if err := format.NewMarkdownFormatter().FormatSummary(&markdownOut, summary); err != nil {
		t.Fatalf("FormatSummary() error = %v", err)
	}
	if !strings.Contains(markdownOut.String(), `data entry "profile" adds one subentry`) {
		t.Errorf("markdown output does not render the operation context:\n%s", markdownOut.String())
	}

	// JSON consumers get the same detail, under a stable field name.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	if err := enc.Encode(summary); err != nil {
		t.Fatalf("json encode error = %v", err)
	}
	if !strings.Contains(buf.String(), `"context"`) {
		t.Errorf("json output does not include the context field:\n%s", buf.String())
	}
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestDiffJSONFormatter(t *testing.T) {
	t.Parallel()

	t.Run("empty diff schema adherence", func(t *testing.T) {
		t.Parallel()

		d := &lens.DiffResult{
			LeftType:  "Price",
			RightType: "Price",
		}
		var buf bytes.Buffer
		f := format.NewDiffJSONFormatter()
		if err := f.Format(&buf, d); err != nil {
			t.Fatalf("Format() error = %v", err)
		}

		var doc format.DiffDocument
		if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
			t.Fatalf("unmarshal json: %v\nJSON:\n%s", err, buf.String())
		}
		if doc.LeftType != "Price" || doc.RightType != "Price" {
			t.Errorf("types = %s/%s, want Price/Price", doc.LeftType, doc.RightType)
		}
		if !doc.Equal {
			t.Errorf("Equal = false, want true")
		}
		if doc.Summary != "no differences" {
			t.Errorf("Summary = %q, want 'no differences'", doc.Summary)
		}
		if len(doc.Changes) != 0 {
			t.Errorf("Changes len = %d, want 0", len(doc.Changes))
		}
	})

	t.Run("diff with changes, reordered operations, and warnings", func(t *testing.T) {
		t.Parallel()

		d := &lens.DiffResult{
			LeftType:  "TransactionEnvelope",
			RightType: "TransactionEnvelope",
			Changes: []lens.Change{
				{
					Path:       "Tx.Operations[0]",
					Op:         lens.ChangeReordered,
					Before:     1,
					After:      0,
					BeforeNote: "index 1",
					AfterNote:  "index 0",
				},
				{
					Path:       "Tx.Fee",
					Op:         lens.ChangeModified,
					Before:     uint32(100),
					After:      uint32(200),
					BeforeNote: "100",
					AfterNote:  "200",
				},
				{
					Path:      "Tx.Memo",
					Op:        lens.ChangeAdded,
					After:     "test",
					AfterNote: "text memo",
				},
				{
					Path:       "Tx.Signatures[0]",
					Op:         lens.ChangeRemoved,
					Before:     "sig",
					BeforeNote: "sig note",
				},
			},
			Redacted: []string{"Signatures[0].Signature"},
			Warnings: []string{"warning: quorum weakened"},
		}

		var buf bytes.Buffer
		f := format.NewDiffJSONFormatter(format.WithDiffJSONIndent("  "))
		if err := f.Format(&buf, d); err != nil {
			t.Fatalf("Format() error = %v", err)
		}

		var doc format.DiffDocument
		if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
			t.Fatalf("unmarshal json: %v\nJSON:\n%s", err, buf.String())
		}
		if doc.Equal {
			t.Error("Equal = true, want false")
		}
		if len(doc.Changes) != 4 {
			t.Fatalf("Changes len = %d, want 4", len(doc.Changes))
		}
		if doc.Changes[0].Op != "reordered" {
			t.Errorf("Change[0].Op = %q, want 'reordered'", doc.Changes[0].Op)
		}
		if len(doc.Redacted) != 1 || doc.Redacted[0] != "Signatures[0].Signature" {
			t.Errorf("Redacted = %v, want ['Signatures[0].Signature']", doc.Redacted)
		}
		if len(doc.Warnings) != 1 || doc.Warnings[0] != "warning: quorum weakened" {
			t.Errorf("Warnings = %v, want ['warning: quorum weakened']", doc.Warnings)
		}

		// Also verify via generic map to confirm json tag names
		var m map[string]any
		if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
			t.Fatalf("unmarshal map: %v", err)
		}
		for _, key := range []string{"left_type", "right_type", "equal", "summary", "changes", "redacted", "warnings"} {
			if _, ok := m[key]; !ok {
				t.Errorf("JSON schema missing expected key %q", key)
			}
		}
	})

	t.Run("nil diff returns error", func(t *testing.T) {
		t.Parallel()

		f := format.NewDiffJSONFormatter()
		var buf bytes.Buffer
		if err := f.Format(&buf, nil); err == nil {
			t.Error("Format(nil) returned nil, want error")
		}
	})

	t.Run("text diff formatter with reordered, warnings, and type difference", func(t *testing.T) {
		t.Parallel()

		d := &lens.DiffResult{
			LeftType:  "TransactionV0",
			RightType: "TransactionV1",
			Changes: []lens.Change{
				{
					Path:       "Operations[0]",
					Op:         lens.ChangeReordered,
					Before:     1,
					After:      0,
					BeforeNote: "index 1",
					AfterNote:  "index 0",
				},
				{
					Path:      "Operations[1]",
					Op:        lens.ChangeAdded,
					After:     "op_val",
					AfterNote: "added op",
				},
				{
					Path:   "Operations[2]",
					Op:     lens.ChangeRemoved,
					Before: "old_op",
				},
			},
			Warnings: []string{"warning: quorum dropped below medium threshold"},
		}

		var buf bytes.Buffer
		f := format.NewDiffFormatter(format.WithDiffPalette(format.NoColor))
		if err := f.Format(&buf, d); err != nil {
			t.Fatalf("Format() error = %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "type differs") {
			t.Errorf("output missing type differs: %s", out)
		}
		if !strings.Contains(out, "reordered from index 1 to 0") {
			t.Errorf("output missing reordered text: %s", out)
		}
		if !strings.Contains(out, "warning: quorum dropped") {
			t.Errorf("output missing warning: %s", out)
		}

		// Also test identical with warnings
		dEqual := &lens.DiffResult{
			LeftType:  "AccountEntry",
			RightType: "AccountEntry",
			Warnings:  []string{"warning: quorum drift"},
		}
		var bufEq bytes.Buffer
		if err := f.Format(&bufEq, dEqual); err != nil {
			t.Fatalf("Format() error = %v", err)
		}
		if !strings.Contains(bufEq.String(), "no differences") || !strings.Contains(bufEq.String(), "warning: quorum drift") {
			t.Errorf("equal diff format with warning mismatch: %s", bufEq.String())
		}
	})
}

// junitDocument is the subset of the JUnit schema the formatter emits. It is
// unmarshalled rather than grepped, so the tests prove the output parses as XML
// instead of merely containing the expected substrings.
type junitDocument struct {
	XMLName  xml.Name `xml:"testsuite"`
	Name     string   `xml:"name,attr"`
	Tests    int      `xml:"tests,attr"`
	Failures int      `xml:"failures,attr"`
	Errors   int      `xml:"errors,attr"`
	Skipped  int      `xml:"skipped,attr"`
	Time     string   `xml:"time,attr"`
	Cases    []struct {
		Name      string `xml:"name,attr"`
		ClassName string `xml:"classname,attr"`
		Time      string `xml:"time,attr"`
		Failure   *struct {
			Type    string `xml:"type,attr"`
			Message string `xml:"message,attr"`
			Body    string `xml:",chardata"`
		} `xml:"failure"`
		SystemOut string `xml:"system-out"`
	} `xml:"testcase"`
}

func parseJUnit(t *testing.T, out string) junitDocument {
	t.Helper()
	if !strings.HasPrefix(out, xml.Header) {
		t.Fatalf("JUnit output does not start with the XML declaration:\n%s", out)
	}
	var doc junitDocument
	if err := xml.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("JUnit output is not well-formed XML: %v\n%s", err, out)
	}
	return doc
}

func renderJUnit(t *testing.T, f *format.JUnitFormatter, vals ...*lens.Value) (string, junitDocument) {
	t.Helper()
	var buf bytes.Buffer
	if err := f.FormatAll(&buf, vals); err != nil {
		t.Fatalf("JUnitFormatter.FormatAll() error = %v", err)
	}
	out := buf.String()
	return out, parseJUnit(t, out)
}

func TestJUnitFormatterPassesDecodedValues(t *testing.T) {
	t.Parallel()

	out, doc := renderJUnit(t,
		format.NewJUnitFormatter(format.WithSuiteName("decode-report")),
		loadValue(t, "tx_success.env"),
		loadValue(t, "tx_success.res"),
	)

	if doc.Name != "decode-report" {
		t.Errorf("suite name = %q, want %q", doc.Name, "decode-report")
	}
	if doc.Tests != 2 || doc.Failures != 0 || doc.Errors != 0 {
		t.Errorf("suite counts = tests %d failures %d errors %d, want 2/0/0", doc.Tests, doc.Failures, doc.Errors)
	}
	if len(doc.Cases) != 2 {
		t.Fatalf("testcase count = %d, want 2", len(doc.Cases))
	}
	for i, tc := range doc.Cases {
		if tc.Failure != nil {
			t.Errorf("testcase %d has a failure element for a successful value", i)
		}
		if tc.SystemOut == "" {
			t.Errorf("testcase %d has no system-out", i)
		}
		if tc.ClassName != "decode-report" {
			t.Errorf("testcase %d classname = %q, want %q", i, tc.ClassName, "decode-report")
		}
	}
	if got := doc.Cases[0].Name; got != "TransactionEnvelope" {
		t.Errorf("first testcase name = %q, want TransactionEnvelope", got)
	}
	if got := doc.Cases[1].Name; got != "TransactionResult" {
		t.Errorf("second testcase name = %q, want TransactionResult", got)
	}
	if got := doc.Cases[1].SystemOut; got != "Transaction succeeded." {
		t.Errorf("result system-out = %q, want the explain headline", got)
	}
	if !strings.Contains(doc.Cases[0].SystemOut, "Transaction from") {
		t.Errorf("envelope system-out = %q, want the explain headline", doc.Cases[0].SystemOut)
	}

	// Timing is fixed so repeated runs produce byte-identical reports.
	if doc.Time != "0.000" || doc.Cases[0].Time != "0.000" {
		t.Errorf("suite/testcase time = %q/%q, want 0.000", doc.Time, doc.Cases[0].Time)
	}
	if strings.Contains(out, "\x1b[") {
		t.Error("JUnit output contains ANSI escape sequences")
	}
}

func TestJUnitFormatterReportsFailedTransaction(t *testing.T) {
	t.Parallel()

	out, doc := renderJUnit(t, format.NewJUnitFormatter(), loadValue(t, "tx_failed.res"))

	if doc.Tests != 1 || doc.Failures != 1 || doc.Errors != 0 {
		t.Errorf("suite counts = tests %d failures %d errors %d, want 1/1/0", doc.Tests, doc.Failures, doc.Errors)
	}
	if len(doc.Cases) != 1 {
		t.Fatalf("testcase count = %d, want 1", len(doc.Cases))
	}
	tc := doc.Cases[0]
	if tc.Failure == nil {
		t.Fatalf("testcase has no failure element:\n%s", out)
	}
	// The inner transaction's code is the specific cause, so it wins over the
	// fee-bump wrapper's tx_fee_bump_inner_failed.
	if tc.Failure.Type != "tx_failed" {
		t.Errorf("failure type = %q, want tx_failed", tc.Failure.Type)
	}
	if !strings.Contains(tc.Failure.Message, "Transaction failed at operation 0") {
		t.Errorf("failure message = %q, want the explain headline", tc.Failure.Message)
	}
	for _, want := range []string{"Reason:", "Inner reason:", "Operation 0 failed", "Hint:"} {
		if !strings.Contains(tc.Failure.Body, want) {
			t.Errorf("failure body missing %q:\n%s", want, tc.Failure.Body)
		}
	}
}

func TestJUnitFormatterEscapesNamesAndIsDeterministic(t *testing.T) {
	t.Parallel()

	v := &lens.Value{
		Type: `Bad<&"Type`,
		Node: &lens.Node{Kind: lens.KindScalar, Value: "x"},
	}
	f := format.NewJUnitFormatter()

	first, doc := renderJUnit(t, f, v)
	if got := doc.Cases[0].Name; got != v.Type {
		t.Errorf("testcase name = %q, want %q", got, v.Type)
	}

	second, _ := renderJUnit(t, f, v)
	if first != second {
		t.Errorf("JUnit output is not deterministic:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestJUnitFormatterEmptySuite(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := format.NewJUnitFormatter().FormatAll(&buf, nil); err != nil {
		t.Fatalf("FormatAll() error = %v", err)
	}
	doc := parseJUnit(t, buf.String())
	if doc.Tests != 0 || doc.Failures != 0 || doc.Errors != 0 {
		t.Errorf("empty suite counts = tests %d failures %d errors %d, want 0/0/0", doc.Tests, doc.Failures, doc.Errors)
	}
	if len(doc.Cases) != 0 {
		t.Errorf("empty suite has %d testcases", len(doc.Cases))
	}
}

func TestJUnitFormatterRejectsNilValues(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	f := format.NewJUnitFormatter()
	if err := f.Format(&buf, nil); err == nil {
		t.Error("Format(nil) error = nil, want an error")
	}
	if err := f.FormatAll(&buf, []*lens.Value{loadValue(t, "tx_success.env"), nil}); err == nil {
		t.Error("FormatAll() with a nil value error = nil, want an error")
	}
	if buf.Len() != 0 {
		t.Errorf("formatter wrote %d bytes before failing, want none", buf.Len())
	}
}

type junitErrWriter struct{ err error }

func (w junitErrWriter) Write([]byte) (int, error) { return 0, w.err }

func TestJUnitFormatterPropagatesWriteError(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	err := format.NewJUnitFormatter().Format(junitErrWriter{err: boom}, loadValue(t, "tx_success.env"))
	if !errors.Is(err, boom) {
		t.Errorf("Format() error = %v, want it to wrap %v", err, boom)
	}
}
