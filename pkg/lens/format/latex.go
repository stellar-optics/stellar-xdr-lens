package format

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

// LaTeXFormatter renders decoded XDR as a LaTeX booktabs table, the form a
// paper, report or generated appendix can paste straight into a document.
//
// Transactions are exported as the high-level fields lens.Explain resolves —
// source, fee, sequence, memo and one row per operation — which is what a
// technical write-up needs. Any other registered type is flattened into
// dotted-path/value rows, so every input produces a table.
//
// Every cell is escaped for LaTeX, so a memo containing "_", "&" or "%" is
// typeset literally instead of breaking the build. Output is deterministic
// and offline: it depends only on the value handed to Format.
type LaTeXFormatter struct {
	caption string
}

// LaTeXOption configures a LaTeXFormatter.
type LaTeXOption func(*LaTeXFormatter)

// WithLaTeXCaption overrides the table caption. The default caption names the
// decoded XDR type.
func WithLaTeXCaption(caption string) LaTeXOption {
	return func(f *LaTeXFormatter) { f.caption = caption }
}

// NewLaTeXFormatter returns a LaTeXFormatter.
func NewLaTeXFormatter(opts ...LaTeXOption) *LaTeXFormatter {
	f := &LaTeXFormatter{}
	for _, o := range opts {
		o(f)
	}
	return f
}

// latexTable is one exported table: a caption and ordered Field/Value rows.
type latexTable struct {
	caption string
	rows    [][2]string
}

// Format writes v as a booktabs tabular environment, satisfying the Formatter
// interface.
//
// The whole table is built before anything is written, so a failure part-way
// through leaves w untouched rather than holding truncated LaTeX.
func (f *LaTeXFormatter) Format(w io.Writer, v *lens.Value) error {
	if v == nil || v.Node == nil {
		return fmt.Errorf("format: latex: %w: nil value", lens.ErrInvalidArgument)
	}

	table := f.buildTable(v)
	if table.caption == "" {
		table.caption = v.Type
	}

	out := f.render(table)
	if _, err := io.WriteString(w, out); err != nil {
		return fmt.Errorf("format: write latex: %w", err)
	}
	return nil
}

// buildTable produces the rows for a value, preferring the transaction summary
// and falling back to a flattened tree for every other XDR type.
func (f *LaTeXFormatter) buildTable(v *lens.Value) latexTable {
	table := latexTable{caption: f.caption}

	if summary, err := lens.Explain(v); err == nil && summary != nil {
		table.rows = append(table.rows, [2]string{"Type", summary.Kind})
		table.rows = append(table.rows, [2]string{"Headline", summary.Headline})
		table.rows = appendNonEmpty(table.rows, "Source", summary.Source)
		table.rows = appendNonEmpty(table.rows, "Fee-bump source", summary.FeeBumpSource)
		table.rows = appendNonEmpty(table.rows, "Sequence", strconv.FormatInt(summary.SeqNum, 10))
		table.rows = appendNonEmpty(table.rows, "Fee bid (stroops)", stroopText(summary.Fee))
		table.rows = appendNonEmpty(table.rows, "Fee charged (stroops)", stroopText(summary.FeeCharged))
		table.rows = appendNonEmpty(table.rows, "Memo", summary.Memo)
		table.rows = appendNonEmpty(table.rows, "Signatures", strconv.Itoa(summary.SignatureCt))
		for i, precond := range summary.Preconds {
			table.rows = append(table.rows, [2]string{"Precondition " + strconv.Itoa(i+1), precond})
		}

		if summary.Outcome != nil {
			status := "success"
			if !summary.Outcome.Success {
				status = "failed"
			}
			table.rows = append(table.rows, [2]string{"Outcome", status})
			table.rows = appendNonEmpty(table.rows, "Result code", summary.Outcome.Reason.Code)
			if inner := summary.Outcome.InnerReason; inner != nil {
				table.rows = appendNonEmpty(table.rows, "Inner result code", inner.Code)
			}
			if len(summary.Outcome.FailedOps) > 0 {
				table.rows = append(table.rows, [2]string{"Failed operations", joinInts(summary.Outcome.FailedOps)})
			}
		}

		for _, op := range summary.Operations {
			label := "Operation " + strconv.Itoa(op.Index)
			value := op.Type
			if op.Detail != "" {
				value += ": " + op.Detail
			}
			if op.Source != "" {
				value += " (source " + op.Source + ")"
			}
			if op.Result != nil && !op.Result.Success {
				value += " — FAILED: " + op.Result.Code
			}
			table.rows = append(table.rows, [2]string{label, value})
		}

		return table
	}

	table.rows = flattenRows(v.Node, "", nil)
	if len(table.rows) == 0 {
		table.rows = append(table.rows, [2]string{"Type", v.Type})
	}
	return table
}

// flattenRows turns a node tree into dotted-path/value rows. Array elements
// carry an "[i]" suffix, which keeps ledger entries and operations addressable
// once they are pasted into a document.
func flattenRows(n *lens.Node, prefix string, rows [][2]string) [][2]string {
	if n == nil {
		return rows
	}

	path := prefix
	if path == "" {
		path = n.Name
		if path == "" {
			path = n.TypeName
		}
		if path == "" {
			path = "root"
		}
	}

	if n.Kind == lens.KindScalar || len(n.Children) == 0 {
		return append(rows, [2]string{path, nodeValueText(n)})
	}

	for i, child := range n.Children {
		name := child.Name
		if name == "" {
			name = child.TypeName
		}
		if name == "" {
			name = "[" + strconv.Itoa(i) + "]"
		}
		childPath := path + "." + name
		if name != "" && name[0] == '[' {
			childPath = path + name
		}
		rows = flattenRows(child, childPath, rows)
	}
	return rows
}

// nodeValueText renders a node's scalar value, falling back to its note so a
// node enriched with a human-readable annotation does not export as empty.
func nodeValueText(n *lens.Node) string {
	if n.Value != nil {
		return fmt.Sprint(n.Value)
	}
	if n.Note != "" {
		return n.Note
	}
	return ""
}

// render emits the booktabs table. The column specification is "ll" because
// every row is a field name and its value.
func (f *LaTeXFormatter) render(table latexTable) string {
	var b strings.Builder

	b.WriteString("\\begin{table}[htbp]\n")
	b.WriteString("  \\centering\n")
	fmt.Fprintf(&b, "  \\caption{%s}\n", latexEscape(table.caption))
	b.WriteString("  \\begin{tabular}{ll}\n")
	b.WriteString("    \\toprule\n")
	b.WriteString("    Field & Value \\\\\n")
	b.WriteString("    \\midrule\n")
	for _, row := range table.rows {
		fmt.Fprintf(&b, "    %s & \\texttt{%s} \\\\\n",
			latexEscape(row[0]), latexEscape(row[1]))
	}
	b.WriteString("    \\bottomrule\n")
	b.WriteString("  \\end{tabular}\n")
	b.WriteString("\\end{table}\n")

	return b.String()
}

// latexEscape replaces the ten characters LaTeX treats specially. Backslash
// is listed first, but strings.Replacer is single-pass, so the replacement
// text it introduces is not escaped again.
var latexEscapeReplacer = strings.NewReplacer(
	`\`, `\textbackslash{}`,
	`&`, `\&`,
	`%`, `\%`,
	`$`, `\$`,
	`#`, `\#`,
	`_`, `\_`,
	`{`, `\{`,
	`}`, `\}`,
	`~`, `\textasciitilde{}`,
	`^`, `\textasciicircum{}`,
)

// latexEscape makes s safe to typeset literally inside a table cell.
func latexEscape(s string) string {
	return latexEscapeReplacer.Replace(s)
}

// appendNonEmpty appends a row only when value is non-empty, so tables for
// bare results do not carry columns of blanks.
func appendNonEmpty(rows [][2]string, field, value string) [][2]string {
	if value == "" {
		return rows
	}
	return append(rows, [2]string{field, value})
}

// stroopText renders a stroop count only when it is non-zero, which is the
// same omitempty rule the JSON summary uses.
func stroopText(n int64) string {
	if n == 0 {
		return ""
	}
	return strconv.FormatInt(n, 10)
}

// joinInts renders indexes as a comma-separated list.
func joinInts(values []int) string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		out = append(out, strconv.Itoa(v))
	}
	return strings.Join(out, ", ")
}
