package format

import (
	"fmt"
	"io"
	"strings"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

// MarkdownFormatter renders transaction summaries as GitHub-flavored Markdown
// tables.
type MarkdownFormatter struct{}

// NewMarkdownFormatter returns a MarkdownFormatter.
func NewMarkdownFormatter() *MarkdownFormatter {
	return &MarkdownFormatter{}
}

// Format explains a transaction value and writes its summary as Markdown.
func (f *MarkdownFormatter) Format(w io.Writer, v *lens.Value) error {
	if v == nil || v.Node == nil {
		return fmt.Errorf("format: nil value")
	}
	summary, err := lens.Explain(v)
	if err != nil {
		return fmt.Errorf("format: explain %s: %w", v.Type, err)
	}
	return f.FormatSummary(w, summary)
}

// FormatSummary writes a summary as a transaction details table and an
// operations table.
func (f *MarkdownFormatter) FormatSummary(w io.Writer, s *lens.Summary) error {
	if s == nil {
		return fmt.Errorf("format: nil summary")
	}

	var b strings.Builder
	b.WriteString("| Transaction | Details |\n")
	b.WriteString("| --- | --- |\n")
	markdownRow(&b, "Summary", s.Headline)
	if s.Outcome != nil {
		status := "Failed"
		if s.Outcome.Success {
			status = "Succeeded"
		}
		markdownRow(&b, "Status", status)
		markdownRow(&b, "Result", s.Outcome.Reason.Code+": "+s.Outcome.Reason.Summary)
		if s.Outcome.InnerReason != nil {
			markdownRow(&b, "Inner result", s.Outcome.InnerReason.Code+": "+s.Outcome.InnerReason.Summary)
		}
	}
	if s.Source != "" {
		markdownRow(&b, "Source", s.Source)
	}
	if s.FeeBumpSource != "" {
		markdownRow(&b, "Fee-bump source", s.FeeBumpSource)
	}
	if s.SeqNum != 0 {
		markdownRow(&b, "Sequence", fmt.Sprintf("%d", s.SeqNum))
	}
	if s.Fee != 0 {
		markdownRow(&b, "Fee bid (stroops)", fmt.Sprintf("%d", s.Fee))
	}
	if s.FeeCharged != 0 {
		markdownRow(&b, "Fee charged (stroops)", fmt.Sprintf("%d", s.FeeCharged))
	}
	if s.Memo != "" {
		markdownRow(&b, "Memo", s.Memo)
	}
	if s.SignatureCt > 0 {
		markdownRow(&b, "Signatures", fmt.Sprintf("%d", s.SignatureCt))
	}
	for _, precondition := range s.Preconds {
		markdownRow(&b, "Precondition", precondition)
	}

	b.WriteString("\n| # | Operation | Source | Description | Result | Status | Hint |\n")
	b.WriteString("| ---: | --- | --- | --- | --- | --- | --- |\n")
	if len(s.Operations) == 0 {
		b.WriteString("| - | No operations | - | - | - | - | - |\n")
	} else {
		for _, op := range s.Operations {
			result, status, hint := "-", "Not reported", "-"
			if op.Result != nil {
				result = op.Result.Code + ": " + op.Result.Summary
				if op.Result.Context != "" {
					result += " — " + op.Result.Context
				}
				status = "Failed"
				if op.Result.Success {
					status = "Succeeded"
				}
				if op.Result.Hint != "" {
					hint = op.Result.Hint
				}
			}
			description := op.Detail
			if description == "" {
				description = "-"
			}
			if op.Deprecation != "" {
				description += " — " + op.Deprecation
			}
			source := op.Source
			if source == "" {
				source = "-"
			}
			markdownCells(&b,
				fmt.Sprintf("%d", op.Index),
				op.Type,
				source,
				description,
				result,
				status,
				hint,
			)
		}
	}

	if _, err := io.WriteString(w, b.String()); err != nil {
		return fmt.Errorf("format: write markdown summary: %w", err)
	}
	return nil
}

func markdownRow(b *strings.Builder, label, value string) {
	markdownCells(b, label, value)
}

func markdownCells(b *strings.Builder, cells ...string) {
	b.WriteString("| ")
	for i, cell := range cells {
		if i > 0 {
			b.WriteString(" | ")
		}
		b.WriteString(escapeMarkdownCell(cell))
	}
	b.WriteString(" |\n")
}

func escapeMarkdownCell(value string) string {
	replacer := strings.NewReplacer(
		"\\", "\\\\",
		"|", "\\|",
		"\r\n", "<br>",
		"\r", "<br>",
		"\n", "<br>",
	)
	return replacer.Replace(value)
}
