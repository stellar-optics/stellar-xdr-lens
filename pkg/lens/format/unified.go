package format

import (
	"fmt"
	"io"
	"strings"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

// UnifiedFormatter renders the difference between two decoded values as a
// standard unified diff: "---"/"+++" file headers followed by "@@" hunks with
// context, removal and addition lines. That is the format code review tools,
// `patch`-style consumers and pull-request comment bot helpers expect, so a
// structural XDR change can be attached to a review verbatim.
//
// The compared text is a deterministic one-line-per-node rendering of each
// value (paths on the left, rendered values on the right), so the patch is
// stable for a given pair of inputs and requires no network or clock. Values
// that are identical produce no output, matching diff(1).
type UnifiedFormatter struct {
	context   int
	fromLabel string
	toLabel   string
	filter    *lens.PathFilter
}

// UnifiedOption configures a UnifiedFormatter.
type UnifiedOption func(*UnifiedFormatter)

// WithUnifiedContext sets the number of context lines around each change.
// Negative values are treated as zero.
func WithUnifiedContext(n int) UnifiedOption {
	return func(f *UnifiedFormatter) { f.context = n }
}

// WithUnifiedLabels overrides the "---"/"+++" header labels. Empty labels fall
// back to the compared XDR type names.
func WithUnifiedLabels(from, to string) UnifiedOption {
	return func(f *UnifiedFormatter) {
		f.fromLabel = from
		f.toLabel = to
	}
}

// WithUnifiedFilter restricts the diff to the paths the filter selects. A nil
// filter reports every change.
func WithUnifiedFilter(filter *lens.PathFilter) UnifiedOption {
	return func(f *UnifiedFormatter) { f.filter = filter }
}

// NewUnifiedFormatter returns a UnifiedFormatter with three context lines.
func NewUnifiedFormatter(opts ...UnifiedOption) *UnifiedFormatter {
	f := &UnifiedFormatter{context: 3}
	for _, o := range opts {
		o(f)
	}
	return f
}

// Format writes the unified diff of left and right to w. It returns an error
// when either value is nil; identical values write nothing.
func (f *UnifiedFormatter) Format(w io.Writer, left, right *lens.Value) error {
	if left == nil || right == nil {
		return fmt.Errorf("format: unified diff requires two decoded values")
	}

	pairs := filterAlignment(alignValues(left, right), f.filter)
	if allEqual(pairs) {
		return nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "--- %s\n", f.label(f.fromLabel, "a", left))
	fmt.Fprintf(&b, "+++ %s\n", f.label(f.toLabel, "b", right))

	for _, hunk := range buildHunks(pairs, f.context) {
		fmt.Fprintf(&b, "@@ -%s +%s @@\n",
			formatRange(hunk.oldStart, hunk.oldCount),
			formatRange(hunk.newStart, hunk.newCount))
		for _, pair := range hunk.lines {
			switch pair.kind {
			case lineDelete:
				b.WriteString("-" + pair.left + "\n")
			case lineInsert:
				b.WriteString("+" + pair.right + "\n")
			default:
				b.WriteString(" " + pair.left + "\n")
			}
		}
	}

	if _, err := io.WriteString(w, b.String()); err != nil {
		return fmt.Errorf("format: write unified diff: %w", err)
	}
	return nil
}

func (f *UnifiedFormatter) label(override, prefix string, v *lens.Value) string {
	if override != "" {
		return override
	}
	if v.Type != "" {
		return prefix + "/" + v.Type
	}
	return prefix
}
