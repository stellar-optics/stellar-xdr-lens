package format

import (
	"fmt"
	"io"
	"strings"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

// diffStatsColumns describes the fixed-width layout of the diff statistics
// table: the section name followed by one numeric column per change kind and a
// total.
const (
	diffStatsSectionWidth = 30
	diffStatsColumnWidth  = 9
	diffStatsColumnCount  = 5
)

// FormatDiffStatsTable renders lens.DiffStats as a fixed-width table with one
// row per top-level path section, followed by a TOTAL row. Type changes are
// counted as modifications, matching how the diff commands describe them.
//
// The row order is the order the sections first appear in the diff, so the
// table is deterministic and mirrors the document. A nil or empty statistic
// set renders the same "no differences" line the other diff formatters use.
//
//nolint:revive // FormatDiffStatsTable is the public API name pinned by the issue spec.
func FormatDiffStatsTable(stats *lens.DiffStats) string {
	if stats == nil || stats.Total == 0 {
		return "no differences\n"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%-*s", diffStatsSectionWidth, "SECTION")
	for _, heading := range []string{"ADDED", "REMOVED", "MODIFIED", "REORDERED", "TOTAL"} {
		fmt.Fprintf(&b, " %*s", diffStatsColumnWidth, heading)
	}
	b.WriteByte('\n')

	divider := strings.Repeat("\u2500", diffStatsSectionWidth+diffStatsColumnCount*(diffStatsColumnWidth+1))
	b.WriteString(divider + "\n")

	for _, section := range stats.Sections {
		fmt.Fprintf(&b, "%-*s", diffStatsSectionWidth, truncate(section.Path, diffStatsSectionWidth))
		writeDiffStatsRow(&b, section.Added, section.Removed,
			section.Modified+section.TypeChanged, section.Reordered, section.Total)
	}

	fmt.Fprintf(&b, "%-*s", diffStatsSectionWidth, "TOTAL")
	writeDiffStatsRow(&b, stats.Added, stats.Removed,
		stats.Modified+stats.TypeChanged, stats.Reordered, stats.Total)

	return b.String()
}

// writeDiffStatsRow writes the numeric columns of one table row.
func writeDiffStatsRow(b *strings.Builder, added, removed, modified, reordered, total int) {
	for _, value := range []int{added, removed, modified, reordered, total} {
		fmt.Fprintf(b, " %*d", diffStatsColumnWidth, value)
	}
	b.WriteByte('\n')
}

// CompactFormatter renders decoded XDR as a terse, single-line summary
// suitable for grep, awk, cut, and shell pipelines.
//
// Every output is strictly a single line followed by a newline, with no
// internal line breaks.
type CompactFormatter struct {
	palette     Palette
	separator   string
	includeType bool
}

// CompactOption configures a CompactFormatter.
type CompactOption func(*CompactFormatter)

// WithCompactPalette sets the colour palette for the compact formatter.
func WithCompactPalette(p Palette) CompactOption {
	return func(c *CompactFormatter) { c.palette = p }
}

// WithCompactSeparator sets the field separator (default is a single space).
func WithCompactSeparator(sep string) CompactOption {
	return func(c *CompactFormatter) { c.separator = sep }
}

// WithCompactIncludeType controls whether the top-level XDR type name is prepended.
func WithCompactIncludeType(include bool) CompactOption {
	return func(c *CompactFormatter) { c.includeType = include }
}

// NewCompactFormatter creates a new CompactFormatter.
func NewCompactFormatter(opts ...CompactOption) *CompactFormatter {
	c := &CompactFormatter{
		palette:     NoColor,
		separator:   " ",
		includeType: true,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// SingleLineFormatter is an alias for CompactFormatter.
type SingleLineFormatter = CompactFormatter

// CompactSummaryFormatter is an alias for CompactFormatter.
type CompactSummaryFormatter = CompactFormatter

// NewSingleLineFormatter creates a new SingleLineFormatter.
func NewSingleLineFormatter(opts ...CompactOption) *CompactFormatter {
	return NewCompactFormatter(opts...)
}

// NewCompactSummaryFormatter creates a new CompactSummaryFormatter.
func NewCompactSummaryFormatter(opts ...CompactOption) *CompactFormatter {
	return NewCompactFormatter(opts...)
}

// Format renders v as a single line to w, satisfying the Formatter interface.
func (c *CompactFormatter) Format(w io.Writer, v *lens.Value) error {
	if v == nil || v.Node == nil {
		return fmt.Errorf("format: nil value")
	}

	line := c.renderValue(v)
	if _, err := fmt.Fprintln(w, line); err != nil {
		return fmt.Errorf("format: write compact summary: %w", err)
	}
	return nil
}

// FormatSummary renders a lens.Summary as a single line to w.
func (c *CompactFormatter) FormatSummary(w io.Writer, s *lens.Summary) error {
	if s == nil {
		return fmt.Errorf("format: nil summary")
	}

	line := c.renderSummary(s)
	if _, err := fmt.Fprintln(w, line); err != nil {
		return fmt.Errorf("format: write compact summary: %w", err)
	}
	return nil
}

// FormatNode renders a single lens.Node as a single line string.
func (c *CompactFormatter) FormatNode(n *lens.Node) string {
	if n == nil {
		return ""
	}
	var pairs []string
	c.collectNodePairs(n, "", &pairs, 0)
	return strings.Join(pairs, c.separator)
}

func (c *CompactFormatter) renderValue(v *lens.Value) string {
	p := c.palette
	var b strings.Builder

	if c.includeType {
		b.WriteString(p.Type + v.Type + p.Reset)
	}

	var parts []string

	switch v.Type {
	case "TransactionEnvelope":
		parts = c.summarizeEnvelope(v.Node)
	case "TransactionResult":
		parts = c.summarizeResult(v.Node)
	default:
		c.collectNodePairs(v.Node, "", &parts, 0)
	}

	if len(parts) > 0 {
		if b.Len() > 0 {
			b.WriteString(c.separator)
		}
		b.WriteString(strings.Join(parts, c.separator))
	}

	return b.String()
}

func (c *CompactFormatter) summarizeEnvelope(n *lens.Node) []string {
	var parts []string

	// Detect envelope version: V0, V1, FeeBump
	if v1 := n.Child("V1"); v1 != nil {
		parts = append(parts, c.fmtPair("type", "v1"))
		if tx := v1.Child("Tx"); tx != nil {
			c.appendEnvelopeTxFields(tx, &parts)
		}
		if sigs := v1.Child("Signatures"); sigs != nil {
			parts = append(parts, c.fmtPair("sigs", fmt.Sprintf("%d", len(sigs.Children))))
		}
	} else if feeBump := n.Child("FeeBump"); feeBump != nil {
		parts = append(parts, c.fmtPair("type", "fee_bump"))
		if tx := feeBump.Child("Tx"); tx != nil {
			if feeSource := tx.Child("FeeSource"); feeSource != nil {
				parts = append(parts, c.fmtPair("fee_source", c.nodeDisplay(feeSource)))
			}
			if fee := tx.Child("Fee"); fee != nil {
				parts = append(parts, c.fmtPair("fee", c.nodeDisplay(fee)))
			}
			if inner := tx.Child("InnerTx"); inner != nil {
				if innerV1 := inner.Child("V1"); innerV1 != nil {
					if innerTx := innerV1.Child("Tx"); innerTx != nil {
						if src := innerTx.Child("SourceAccount"); src != nil {
							parts = append(parts, c.fmtPair("inner_source", c.nodeDisplay(src)))
						}
						c.appendEnvelopeOpsAndMemo(innerTx, &parts)
					}
				}
			}
		}
		if sigs := feeBump.Child("Signatures"); sigs != nil {
			parts = append(parts, c.fmtPair("sigs", fmt.Sprintf("%d", len(sigs.Children))))
		}
	} else if v0 := n.Child("V0"); v0 != nil {
		parts = append(parts, c.fmtPair("type", "v0"))
		if tx := v0.Child("Tx"); tx != nil {
			c.appendEnvelopeTxFields(tx, &parts)
		}
		if sigs := v0.Child("Signatures"); sigs != nil {
			parts = append(parts, c.fmtPair("sigs", fmt.Sprintf("%d", len(sigs.Children))))
		}
	} else {
		// Fallback to standard node walker
		c.collectNodePairs(n, "", &parts, 0)
	}

	return parts
}

func (c *CompactFormatter) appendEnvelopeTxFields(tx *lens.Node, parts *[]string) {
	if src := tx.Child("SourceAccount"); src != nil {
		*parts = append(*parts, c.fmtPair("source", c.nodeDisplay(src)))
	}
	if seq := tx.Child("SeqNum"); seq != nil {
		*parts = append(*parts, c.fmtPair("seq", c.nodeDisplay(seq)))
	}
	if fee := tx.Child("Fee"); fee != nil {
		*parts = append(*parts, c.fmtPair("fee", c.nodeDisplay(fee)))
	}
	c.appendEnvelopeOpsAndMemo(tx, parts)
}

func (c *CompactFormatter) appendEnvelopeOpsAndMemo(tx *lens.Node, parts *[]string) {
	if memo := tx.Child("Memo"); memo != nil {
		if text := memo.Child("Text"); text != nil {
			*parts = append(*parts, c.fmtPair("memo", fmt.Sprintf("%q", text.Value)))
		} else if id := memo.Child("Id"); id != nil {
			*parts = append(*parts, c.fmtPair("memo_id", c.nodeDisplay(id)))
		}
	}

	if ops := tx.Child("Operations"); ops != nil {
		*parts = append(*parts, c.fmtPair("ops", fmt.Sprintf("%d", len(ops.Children))))
		var opTypes []string
		for _, op := range ops.Children {
			if body := op.Child("Body"); body != nil {
				// Find body child that is not type
				for _, bc := range body.Children {
					if bc.Name != "Type" {
						opTypes = append(opTypes, bc.Name)
						break
					}
				}
			}
		}
		if len(opTypes) > 0 {
			*parts = append(*parts, fmt.Sprintf("[%s]", strings.Join(opTypes, ",")))
		}
	}
}

func (c *CompactFormatter) summarizeResult(n *lens.Node) []string {
	var parts []string

	if feeCharged := n.Child("FeeCharged"); feeCharged != nil {
		parts = append(parts, c.fmtPair("fee_charged", c.nodeDisplay(feeCharged)))
	}

	if result := n.Child("Result"); result != nil {
		if code := result.Child("Code"); code != nil {
			parts = append(parts, c.fmtPair("status", c.nodeDisplay(code)))
		}

		if results := result.Child("Results"); results != nil {
			parts = append(parts, c.fmtPair("ops", fmt.Sprintf("%d", len(results.Children))))
			var opResults []string
			for _, r := range results.Children {
				if tr := r.Child("Tr"); tr != nil {
					for _, tc := range tr.Children {
						if tc.Name != "Type" {
							if opCode := tc.Child("Code"); opCode != nil {
								opResults = append(opResults, fmt.Sprintf("%v", opCode.Value))
							} else {
								opResults = append(opResults, tc.Name)
							}
							break
						}
					}
				}
			}
			if len(opResults) > 0 {
				parts = append(parts, fmt.Sprintf("[%s]", strings.Join(opResults, ",")))
			}
		}
	}

	return parts
}

func (c *CompactFormatter) renderSummary(s *lens.Summary) string {
	p := c.palette
	var b strings.Builder

	if c.includeType {
		b.WriteString(p.Type + s.Kind + p.Reset)
	}

	var parts []string
	if s.Outcome != nil {
		status := "FAILED"
		if s.Outcome.Success {
			status = "SUCCESS"
		}
		parts = append(parts, c.fmtPair("status", status))
		parts = append(parts, c.fmtPair("reason", s.Outcome.Reason.Code))
	}

	if s.Source != "" {
		parts = append(parts, c.fmtPair("source", s.Source))
	}
	if s.FeeBumpSource != "" {
		parts = append(parts, c.fmtPair("fee_source", s.FeeBumpSource))
	}
	if s.SeqNum != 0 {
		parts = append(parts, c.fmtPair("seq", fmt.Sprintf("%d", s.SeqNum)))
	}
	if s.FeeCharged != 0 {
		parts = append(parts, c.fmtPair("fee_charged", fmt.Sprintf("%d", s.FeeCharged)))
	} else if s.Fee != 0 {
		parts = append(parts, c.fmtPair("fee", fmt.Sprintf("%d", s.Fee)))
	}
	if s.Memo != "" {
		parts = append(parts, c.fmtPair("memo", fmt.Sprintf("%q", s.Memo)))
	}

	if len(s.Operations) > 0 {
		parts = append(parts, c.fmtPair("ops", fmt.Sprintf("%d", len(s.Operations))))
		var opSummaries []string
		for _, op := range s.Operations {
			opDesc := op.Type
			if op.Result != nil {
				opDesc += ":" + op.Result.Code
			}
			opSummaries = append(opSummaries, opDesc)
		}
		parts = append(parts, fmt.Sprintf("[%s]", strings.Join(opSummaries, ",")))
	}

	if len(parts) > 0 {
		if b.Len() > 0 {
			b.WriteString(c.separator)
		}
		b.WriteString(strings.Join(parts, c.separator))
	}

	return b.String()
}

func (c *CompactFormatter) collectNodePairs(n *lens.Node, prefix string, parts *[]string, depth int) {
	if n == nil || depth > 8 {
		return
	}

	switch n.Kind {
	case lens.KindScalar, lens.KindBytes:
		key := prefix
		if key == "" {
			key = n.Name
		}
		if key != "" && n.Value != nil {
			valStr := c.nodeDisplay(n)
			if strings.ContainsAny(valStr, " \t\n") {
				valStr = fmt.Sprintf("%q", valStr)
			}
			*parts = append(*parts, c.fmtPair(key, valStr))
		}

	case lens.KindStruct:
		for _, ch := range n.Children {
			nextPrefix := ch.Name
			if prefix != "" {
				nextPrefix = prefix + "." + ch.Name
			}
			c.collectNodePairs(ch, nextPrefix, parts, depth+1)
		}

	case lens.KindList:
		key := prefix
		if key == "" {
			key = n.Name
		}
		if key != "" {
			*parts = append(*parts, c.fmtPair(key+"_len", fmt.Sprintf("%d", len(n.Children))))
		}
		for i, ch := range n.Children {
			nextPrefix := fmt.Sprintf("%s[%d]", prefix, i)
			c.collectNodePairs(ch, nextPrefix, parts, depth+1)
		}
	}
}

func (c *CompactFormatter) fmtPair(k, v string) string {
	p := c.palette
	return p.Key + k + p.Reset + "=" + p.Str + v + p.Reset
}

func (c *CompactFormatter) nodeDisplay(n *lens.Node) string {
	if n == nil {
		return ""
	}
	if n.Note != "" {
		return n.Note
	}
	if n.Value != nil {
		return fmt.Sprintf("%v", n.Value)
	}
	return ""
}
