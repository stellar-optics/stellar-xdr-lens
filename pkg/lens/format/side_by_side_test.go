package format

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

// ansiSGRPattern matches the SGR sequences the formatters emit.
var ansiSGRPattern = regexp.MustCompile("\x1b\\[[0-9;]*m")

func stripANSI(s string) string {
	return ansiSGRPattern.ReplaceAllString(s, "")
}

func TestChangedSpansIsolatesModifiedRun(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		left          string
		right         string
		leftChanged   string
		rightChanged  string
		expectNoRange bool
	}{
		{
			name:          "identical cells have no changed run",
			left:          "Amount = 10.5   ",
			right:         "Amount = 10.5   ",
			expectNoRange: true,
		},
		{
			name:         "single differing character",
			left:         "Amount = 10.5   ",
			right:        "Amount = 10.7   ",
			leftChanged:  "5",
			rightChanged: "7",
		},
		{
			name:         "replacement shares no characters",
			left:         "Status = SUCCESS ",
			right:        "Status = FAILED  ",
			leftChanged:  "SUCCESS",
			rightChanged: "FAILED ",
		},
		{
			name:         "shared suffix outside the changed run",
			left:         "Seq = 12345 ",
			right:        "Seq = 92345 ",
			leftChanged:  "1",
			rightChanged: "9",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			leftSpan, rightSpan := changedSpans(tc.left, tc.right)
			if tc.expectNoRange {
				if leftSpan.end != leftSpan.start || rightSpan.end != rightSpan.start {
					t.Fatalf("spans = %+v/%+v, want empty ranges", leftSpan, rightSpan)
				}
				return
			}

			if got := string([]rune(tc.left)[leftSpan.start:leftSpan.end]); got != tc.leftChanged {
				t.Errorf("left changed run = %q, want %q", got, tc.leftChanged)
			}
			if got := string([]rune(tc.right)[rightSpan.start:rightSpan.end]); got != tc.rightChanged {
				t.Errorf("right changed run = %q, want %q", got, tc.rightChanged)
			}
		})
	}
}

func TestStyleCellPlacesInlineSequences(t *testing.T) {
	t.Parallel()

	// Single-letter sequences make the expected byte layout readable.
	p := Palette{
		Add:          "<A>",
		Remove:       "<R>",
		AddInline:    "<AI>",
		RemoveInline: "<RI>",
		Reset:        "<z>",
	}

	cell := "abXcd"
	got := styleCell(cell, p.Remove, p.RemoveInline, inlineSpan{start: 2, end: 3}, p.Reset)
	want := "<R>ab<z><RI>X<z><R>cd<z>"
	if got != want {
		t.Errorf("styleCell() = %q, want %q", got, want)
	}

	// An empty inline style must behave exactly like the old whole-cell wrap.
	got = styleCell(cell, p.Remove, "", inlineSpan{start: 2, end: 3}, p.Reset)
	if want := "<R>abXcd<z>"; got != want {
		t.Errorf("styleCell() without inline style = %q, want %q", got, want)
	}

	// The no-colour palette renders the plain cell with no escapes at all.
	got = styleCell(cell, NoColor.Remove, NoColor.RemoveInline, inlineSpan{start: 2, end: 3}, NoColor.Reset)
	if got != cell {
		t.Errorf("styleCell() with NoColor = %q, want %q", got, cell)
	}
}

func TestSideBySideFormatterHighlightsOnlyChangedCharacters(t *testing.T) {
	t.Parallel()

	left := loadSideBySideValue(t, "tx_soroban.res")
	right := loadSideBySideValue(t, "tx_failed.res")

	const width = 240
	column := (width - 3) / 2

	var buf bytes.Buffer
	f := NewSideBySideFormatter(
		WithSideBySideWidth(width),
		WithSideBySideContext(200),
		WithSideBySidePalette(ColorPalette),
	)
	if err := f.Format(&buf, left, right); err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, ColorPalette.RemoveInline) || !strings.Contains(out, ColorPalette.AddInline) {
		t.Fatalf("output has no intra-line highlighting:\n%s", out)
	}

	sawModifiedPair := false
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if !strings.Contains(line, " | ") {
			continue
		}
		parts := strings.SplitN(line, " | ", 2)
		if len(parts) != 2 {
			t.Fatalf("row has no column separator: %q", line)
		}
		leftPlain := stripANSI(parts[0])
		rightPlain := stripANSI(parts[1])

		// Escape sequences are applied after padding, so the visible cell
		// must stay exactly one column wide.
		if got := len([]rune(leftPlain)); got != column {
			t.Errorf("left cell visible width = %d, want %d: %q", got, column, leftPlain)
		}
		if got := len([]rune(rightPlain)); got != column {
			t.Errorf("right cell visible width = %d, want %d: %q", got, column, rightPlain)
		}

		if leftPlain == rightPlain {
			continue
		}
		sawModifiedPair = true

		leftSpan, rightSpan := changedSpans(leftPlain, rightPlain)
		wantLeft := string([]rune(leftPlain)[leftSpan.start:leftSpan.end])
		wantRight := string([]rune(rightPlain)[rightSpan.start:rightSpan.end])

		if got := highlightedRuns(parts[0], ColorPalette.RemoveInline, ColorPalette.Reset); len(got) > 0 && got[0] != wantLeft {
			t.Errorf("left inline highlight = %q, want %q (line %q)", got[0], wantLeft, line)
		}
		if got := highlightedRuns(parts[1], ColorPalette.AddInline, ColorPalette.Reset); len(got) > 0 && got[0] != wantRight {
			t.Errorf("right inline highlight = %q, want %q (line %q)", got[0], wantRight, line)
		}
	}
	if !sawModifiedPair {
		t.Fatal("fixture pair produced no replaced row to highlight")
	}
}

func TestSideBySideFormatterWithoutColorEmitsNoEscapes(t *testing.T) {
	t.Parallel()

	left := loadSideBySideValue(t, "tx_soroban.res")
	right := loadSideBySideValue(t, "tx_failed.res")

	var buf bytes.Buffer
	f := NewSideBySideFormatter(WithSideBySideWidth(200), WithSideBySideContext(200))
	if err := f.Format(&buf, left, right); err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	if strings.Contains(buf.String(), "\x1b[") {
		t.Errorf("no-colour output contains ANSI escapes:\n%s", buf.String())
	}
}

// highlightedRuns returns the text wrapped in open…reset, in order.
func highlightedRuns(cell, open, reset string) []string {
	var runs []string
	for {
		start := strings.Index(cell, open)
		if start < 0 {
			return runs
		}
		cell = cell[start+len(open):]
		end := strings.Index(cell, reset)
		if end < 0 {
			return runs
		}
		runs = append(runs, cell[:end])
		cell = cell[end+len(reset):]
	}
}

func loadSideBySideValue(t *testing.T, name string) *lens.Value {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", name+".txt"))
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}
	v, err := lens.Decode(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatalf("decoding fixture %s: %v", name, err)
	}
	return v
}
