package format_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Odusanya03/stellar-xdr-lens/pkg/lens"
	"github.com/Odusanya03/stellar-xdr-lens/pkg/lens/format"
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
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
