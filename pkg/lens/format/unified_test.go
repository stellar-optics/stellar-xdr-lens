package format_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens/format"
)

func TestUnifiedFormatterEmitsStandardPatch(t *testing.T) {
	t.Parallel()

	left := loadValue(t, "tx_soroban.res")
	right := loadValue(t, "tx_failed.res")

	var buf bytes.Buffer
	if err := format.NewUnifiedFormatter().Format(&buf, left, right); err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	out := buf.String()

	for _, want := range []string{
		"--- a/TransactionResult",
		"+++ b/TransactionResult",
		"@@ -",
		"-FeeCharged",
		"+FeeCharged",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("unified diff does not contain %q\n%s", want, out)
		}
	}

	// Every body line must start with a unified-diff marker.
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "@@") {
			continue
		}
		if line == "" {
			continue
		}
		switch line[0] {
		case ' ', '-', '+':
		default:
			t.Errorf("body line has no diff marker: %q", line)
		}
	}
}

func TestUnifiedFormatterIdenticalValuesEmitNothing(t *testing.T) {
	t.Parallel()

	v := loadValue(t, "tx_success.env")

	var buf bytes.Buffer
	if err := format.NewUnifiedFormatter().Format(&buf, v, v); err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("identical values produced output:\n%s", buf.String())
	}
}

func TestUnifiedFormatterContextBoundsOutput(t *testing.T) {
	t.Parallel()

	left := loadValue(t, "tx_soroban.res")
	right := loadValue(t, "tx_failed.res")

	var full, tight bytes.Buffer
	if err := format.NewUnifiedFormatter().Format(&full, left, right); err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	if err := format.NewUnifiedFormatter(format.WithUnifiedContext(0)).Format(&tight, left, right); err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	if len(tight.String()) >= len(full.String()) {
		t.Errorf("zero context (%d bytes) is not shorter than default context (%d bytes)",
			len(tight.String()), len(full.String()))
	}
}

func TestUnifiedFormatterCustomLabels(t *testing.T) {
	t.Parallel()

	left := loadValue(t, "tx_soroban.res")
	right := loadValue(t, "tx_failed.res")

	var buf bytes.Buffer
	f := format.NewUnifiedFormatter(format.WithUnifiedLabels("before.xdr", "after.xdr"))
	if err := f.Format(&buf, left, right); err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	if !strings.Contains(buf.String(), "--- before.xdr") || !strings.Contains(buf.String(), "+++ after.xdr") {
		t.Errorf("custom labels missing:\n%s", buf.String())
	}
}

func TestUnifiedFormatterFilter(t *testing.T) {
	t.Parallel()

	left := loadValue(t, "tx_soroban.res")
	right := loadValue(t, "tx_failed.res")

	filter, err := lens.NewPathFilter([]string{"FeeCharged"}, nil)
	if err != nil {
		t.Fatalf("NewPathFilter() error = %v", err)
	}

	var buf bytes.Buffer
	f := format.NewUnifiedFormatter(format.WithUnifiedFilter(filter))
	if err := f.Format(&buf, left, right); err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "FeeCharged") {
		t.Errorf("filtered unified diff dropped the selected path:\n%s", out)
	}
	if strings.Contains(out, "-Result.Code") || strings.Contains(out, "+Result.Code") {
		t.Errorf("filtered unified diff reported an excluded change:\n%s", out)
	}
}

func TestUnifiedFormatterIsDeterministic(t *testing.T) {
	t.Parallel()

	left := loadValue(t, "tx_soroban.res")
	right := loadValue(t, "tx_failed.res")

	var first, second bytes.Buffer
	f := format.NewUnifiedFormatter()
	if err := f.Format(&first, left, right); err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	if err := f.Format(&second, left, right); err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	if first.String() != second.String() {
		t.Error("unified diff is not deterministic")
	}
}

func TestUnifiedFormatterRejectsNil(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := format.NewUnifiedFormatter().Format(&buf, nil, loadValue(t, "tx_failed.res")); err == nil {
		t.Error("Format(nil, v) error = nil, want an error")
	}
	if err := format.NewUnifiedFormatter().Format(&buf, loadValue(t, "tx_failed.res"), nil); err == nil {
		t.Error("Format(v, nil) error = nil, want an error")
	}
}

func TestSideBySideFormatterShape(t *testing.T) {
	t.Parallel()

	left := loadValue(t, "tx_soroban.res")
	right := loadValue(t, "tx_failed.res")

	var buf bytes.Buffer
	f := format.NewSideBySideFormatter(format.WithSideBySidePalette(format.NoColor))
	if err := f.Format(&buf, left, right); err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	out := buf.String()

	if !strings.Contains(out, "|") {
		t.Errorf("side-by-side output has no column separator:\n%s", out)
	}
	if !strings.Contains(out, "@@ -") {
		t.Errorf("side-by-side output has no hunk header:\n%s", out)
	}
	if !strings.Contains(out, "FeeCharged") {
		t.Errorf("side-by-side output does not mention the changed field:\n%s", out)
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("NoColor side-by-side output emitted escapes:\n%s", out)
	}
}

func TestSideBySideFormatterColour(t *testing.T) {
	t.Parallel()

	left := loadValue(t, "tx_soroban.res")
	right := loadValue(t, "tx_failed.res")

	var buf bytes.Buffer
	f := format.NewSideBySideFormatter(format.WithSideBySidePalette(format.ColorPalette))
	if err := f.Format(&buf, left, right); err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	if !strings.Contains(buf.String(), "\x1b[") {
		t.Error("coloured side-by-side output has no escape sequences")
	}
}

func TestSideBySideFormatterIdenticalValues(t *testing.T) {
	t.Parallel()

	v := loadValue(t, "tx_success.env")

	var buf bytes.Buffer
	if err := format.NewSideBySideFormatter().Format(&buf, v, v); err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	if !strings.Contains(buf.String(), "no differences") {
		t.Errorf("identical values did not report no differences:\n%s", buf.String())
	}
}

func TestSideBySideFormatterWidthIsHonoured(t *testing.T) {
	t.Parallel()

	left := loadValue(t, "tx_soroban.res")
	right := loadValue(t, "tx_failed.res")

	var wide, narrow bytes.Buffer
	if err := format.NewSideBySideFormatter(format.WithSideBySideWidth(160)).Format(&wide, left, right); err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	if err := format.NewSideBySideFormatter(format.WithSideBySideWidth(40)).Format(&narrow, left, right); err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	maxWide, maxNarrow := longestLine(wide.String()), longestLine(narrow.String())
	if maxNarrow >= maxWide {
		t.Errorf("narrow width (%d) is not shorter than wide width (%d)", maxNarrow, maxWide)
	}
}

func TestSideBySideFormatterLabelsAndNil(t *testing.T) {
	t.Parallel()

	left := loadValue(t, "tx_soroban.res")
	right := loadValue(t, "tx_failed.res")

	var buf bytes.Buffer
	f := format.NewSideBySideFormatter(format.WithSideBySideLabels("left.txt", "right.txt"))
	if err := f.Format(&buf, left, right); err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	if !strings.Contains(buf.String(), "left.txt") || !strings.Contains(buf.String(), "right.txt") {
		t.Errorf("custom labels missing:\n%s", buf.String())
	}

	if err := format.NewSideBySideFormatter().Format(&buf, nil, right); err == nil {
		t.Error("Format(nil, v) error = nil, want an error")
	}
	if err := format.NewSideBySideFormatter().Format(&buf, left, nil); err == nil {
		t.Error("Format(v, nil) error = nil, want an error")
	}
}

func TestSideBySideFormatterZeroWidthUsesDefault(t *testing.T) {
	t.Parallel()

	left := loadValue(t, "tx_soroban.res")
	right := loadValue(t, "tx_failed.res")

	var zero, explicit bytes.Buffer
	if err := format.NewSideBySideFormatter(format.WithSideBySideWidth(0)).Format(&zero, left, right); err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	if err := format.NewSideBySideFormatter(format.WithSideBySideWidth(160)).Format(&explicit, left, right); err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	if zero.String() != explicit.String() {
		t.Error("width 0 did not select the same layout as the default width")
	}
}

func longestLine(s string) int {
	max := 0
	for _, line := range strings.Split(s, "\n") {
		if n := len([]rune(line)); n > max {
			max = n
		}
	}
	return max
}

// TestSideBySideFormatterNoOrdinalLeak guards that the layout uses only the
// supplied palette colours.
func TestSideBySideFormatterNoOrdinalLeak(t *testing.T) {
	t.Parallel()

	left := loadValue(t, "tx_soroban.res")
	right := loadValue(t, "tx_failed.res")

	var buf bytes.Buffer
	f := format.NewSideBySideFormatter(format.WithSideBySidePalette(format.ColorPalette))
	if err := f.Format(&buf, left, right); err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	// Layout characters must survive colouring.
	if !strings.Contains(buf.String(), "|") {
		t.Error("colouring removed the column separator")
	}
}

func TestSideBySideFormatterContextAndFilter(t *testing.T) {
	t.Parallel()

	left := loadValue(t, "tx_soroban.res")
	right := loadValue(t, "tx_failed.res")

	filter, err := lens.NewPathFilter([]string{"FeeCharged"}, nil)
	if err != nil {
		t.Fatalf("NewPathFilter() error = %v", err)
	}

	var buf bytes.Buffer
	f := format.NewSideBySideFormatter(
		format.WithSideBySideContext(0),
		format.WithSideBySideFilter(filter),
	)
	if err := f.Format(&buf, left, right); err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "FeeCharged") {
		t.Errorf("filtered side-by-side diff dropped the selected path:\n%s", out)
	}
	if strings.Contains(out, "-Result.Code") || strings.Contains(out, "+Result.Code") {
		t.Errorf("filtered side-by-side diff reported an excluded change:\n%s", out)
	}
}
