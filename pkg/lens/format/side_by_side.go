package format

import (
	"fmt"
	"io"
	"strings"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

// SideBySideFormatter renders the difference between two decoded values as a
// colourised two-column view. Removed lines sit on the left, added lines on the
// right, and a removed line is shown next to the added line that replaced it,
// which is the layout a terminal reviewer scans most easily.
//
// Like the unified formatter it compares a deterministic one-line-per-node
// rendering of each value, so the output is stable for a given pair of inputs.
// The context option bounds how much surrounding tree is shown around each
// change; equal context lines are dimmed. Colour is applied only after column
// padding, so escape sequences never disturb the layout.
type SideBySideFormatter struct {
	width     int
	context   int
	palette   Palette
	fromLabel string
	toLabel   string
	filter    *lens.PathFilter
}

// SideBySideOption configures a SideBySideFormatter.
type SideBySideOption func(*SideBySideFormatter)

// WithSideBySideWidth sets the total terminal width used for the two columns.
// Values below the minimum are clamped. A width of zero selects the default.
func WithSideBySideWidth(width int) SideBySideOption {
	return func(f *SideBySideFormatter) { f.width = width }
}

// WithSideBySideContext sets the number of context lines around each change.
func WithSideBySideContext(n int) SideBySideOption {
	return func(f *SideBySideFormatter) { f.context = n }
}

// WithSideBySidePalette sets the colour palette.
func WithSideBySidePalette(p Palette) SideBySideOption {
	return func(f *SideBySideFormatter) { f.palette = p }
}

// WithSideBySideLabels overrides the column header labels. Empty labels fall
// back to the compared XDR type names.
func WithSideBySideLabels(from, to string) SideBySideOption {
	return func(f *SideBySideFormatter) {
		f.fromLabel = from
		f.toLabel = to
	}
}

// WithSideBySideFilter restricts the diff to the paths the filter selects. A
// nil filter reports every change.
func WithSideBySideFilter(filter *lens.PathFilter) SideBySideOption {
	return func(f *SideBySideFormatter) { f.filter = filter }
}

// NewSideBySideFormatter returns a formatter 160 columns wide with three
// context lines and no colour.
func NewSideBySideFormatter(opts ...SideBySideOption) *SideBySideFormatter {
	f := &SideBySideFormatter{width: 160, context: 3, palette: NoColor}
	for _, o := range opts {
		o(f)
	}
	return f
}

// sideRow is one rendered row of the two-column output.
type sideRow struct {
	left         string
	right        string
	leftChanged  bool
	rightChanged bool
}

// Format writes the side-by-side diff of left and right to w.
func (f *SideBySideFormatter) Format(w io.Writer, left, right *lens.Value) error {
	if left == nil || right == nil {
		return fmt.Errorf("format: side-by-side diff requires two decoded values")
	}

	column := f.columnWidth()
	pairs := filterAlignment(alignValues(left, right), f.filter)

	var b strings.Builder
	fmt.Fprintf(&b, "%s | %s\n", padCell(f.fromLabelOf(left), column), padCell(f.toLabelOf(right), column))
	b.WriteString(strings.Repeat("-", column*2+3) + "\n")

	if allEqual(pairs) {
		fmt.Fprintf(&b, "%sno differences%s\n", f.palette.Add, f.palette.Reset)
		if _, err := io.WriteString(w, b.String()); err != nil {
			return fmt.Errorf("format: write side-by-side diff: %w", err)
		}
		return nil
	}

	for i, hunk := range buildHunks(pairs, f.context) {
		if i > 0 {
			b.WriteString(strings.Repeat(".", column*2+3) + "\n")
		}
		fmt.Fprintf(&b, "@@ -%s +%s @@\n",
			formatRange(hunk.oldStart, hunk.oldCount),
			formatRange(hunk.newStart, hunk.newCount))
		for _, row := range hunkRows(hunk.lines) {
			f.writeRow(&b, row, column)
		}
	}

	if _, err := io.WriteString(w, b.String()); err != nil {
		return fmt.Errorf("format: write side-by-side diff: %w", err)
	}
	return nil
}

func (f *SideBySideFormatter) columnWidth() int {
	width := f.width
	if width <= 0 {
		width = 160
	}
	column := (width - 3) / 2
	if column < 10 {
		column = 10
	}
	return column
}

func (f *SideBySideFormatter) writeRow(b *strings.Builder, row sideRow, column int) {
	p := f.palette

	left := padCell(row.left, column)
	switch {
	case row.leftChanged:
		left = p.Remove + left + p.Reset
	case p.Dim != "":
		left = p.Dim + left + p.Reset
	}

	right := padCell(row.right, column)
	switch {
	case row.rightChanged:
		right = p.Add + right + p.Reset
	case p.Dim != "":
		right = p.Dim + right + p.Reset
	}

	fmt.Fprintf(b, "%s | %s\n", left, right)
}

func (f *SideBySideFormatter) fromLabelOf(v *lens.Value) string {
	if f.fromLabel != "" {
		return f.fromLabel
	}
	return "--- a/" + v.Type
}

func (f *SideBySideFormatter) toLabelOf(v *lens.Value) string {
	if f.toLabel != "" {
		return f.toLabel
	}
	return "+++ b/" + v.Type
}

// hunkRows zips a hunk's changed lines into rows, pairing each removed line
// with the added line at the same offset so a modification reads across.
func hunkRows(lines []alignedPair) []sideRow {
	var rows []sideRow
	for i := 0; i < len(lines); {
		if lines[i].kind == lineEqual {
			rows = append(rows, sideRow{left: lines[i].left, right: lines[i].right})
			i++
			continue
		}

		start := i
		for i < len(lines) && lines[i].kind != lineEqual {
			i++
		}
		var removed, added []string
		for _, pair := range lines[start:i] {
			switch pair.kind {
			case lineDelete:
				removed = append(removed, pair.left)
			case lineInsert:
				added = append(added, pair.right)
			}
		}
		count := len(removed)
		if len(added) > count {
			count = len(added)
		}
		for k := 0; k < count; k++ {
			var row sideRow
			if k < len(removed) {
				row.left = removed[k]
				row.leftChanged = true
			}
			if k < len(added) {
				row.right = added[k]
				row.rightChanged = true
			}
			rows = append(rows, row)
		}
	}
	return rows
}

// padCell truncates a cell to width display columns and right-pads it with
// spaces. Truncation uses runes so multi-byte box-drawing characters do not
// break the layout.
func padCell(s string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) > width {
		if width == 1 {
			return "…"
		}
		return string(runes[:width-1]) + "…"
	}
	return s + strings.Repeat(" ", width-len(runes))
}
