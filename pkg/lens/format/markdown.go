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

// FormatSummary writes transaction badges, a details table, an operations
// review table, and any available collapsible diagnostic trace.
func (f *MarkdownFormatter) FormatSummary(w io.Writer, s *lens.Summary) error {
	if s == nil {
		return fmt.Errorf("format: nil summary")
	}

	var b strings.Builder
	status := "NOT REPORTED"
	if s.Outcome != nil {
		status = "FAILED"
		if s.Outcome.Success {
			status = "SUCCESS"
		}
	}
	fmt.Fprintf(&b, "[STATUS: %s]", status)
	fee := s.FeeCharged
	if fee == 0 {
		fee = s.Fee
	}
	if fee > 0 {
		fmt.Fprintf(&b, " [FEE: %d stroops]", fee)
	}
	b.WriteString("\n\n")

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

	b.WriteString("\n| # | Operation | Source | Status | Details |\n")
	b.WriteString("| ---: | --- | --- | --- | --- |\n")
	if len(s.Operations) == 0 {
		b.WriteString("| - | No operations | - | - | - |\n")
	} else {
		for _, op := range s.Operations {
			status := "Not reported"
			var details []string
			if op.Detail != "" {
				details = append(details, op.Detail)
			}
			if op.Result != nil {
				result := op.Result.Summary
				if op.Result.Code != "" {
					result = op.Result.Code + ": " + result
				}
				if op.Result.Context != "" {
					result += " — " + op.Result.Context
				}
				details = appendMarkdownDetail(details, result)
				status = "Failed"
				if op.Result.Success {
					status = "Succeeded"
				}
				if op.Result.Hint != "" {
					details = appendMarkdownDetail(details, "Hint: "+op.Result.Hint)
				}
			}
			if len(op.Warnings) > 0 {
				for _, warning := range op.Warnings {
					details = appendMarkdownDetail(details, "Warning: "+warning)
				}
			}
			if len(details) == 0 {
				details = append(details, "-")
			}
			source := op.Source
			if source == "" {
				source = "-"
			}
			markdownCells(&b,
				fmt.Sprintf("%d", op.Index),
				op.Type,
				source,
				status,
				strings.Join(details, "<br>"),
			)
		}
	}

	if len(s.DiagnosticTrace) > 0 {
		b.WriteString("\n<details>\n<summary>Diagnostic Trace</summary>\n\n")
		lines := make([]string, 0, len(s.DiagnosticTrace))
		for _, event := range s.DiagnosticTrace {
			lines = append(lines, formatDiagnosticDetail(event))
		}
		fence := markdownFence(lines)
		b.WriteString(fence + "text\n")
		for _, line := range lines {
			b.WriteString(line + "\n")
		}
		b.WriteString(fence + "\n</details>\n")
	}

	if _, err := io.WriteString(w, b.String()); err != nil {
		return fmt.Errorf("format: write markdown summary: %w", err)
	}
	return nil
}

func appendMarkdownDetail(details []string, value string) []string {
	if value == "" {
		return details
	}
	return append(details, value)
}

func formatDiagnosticDetail(event lens.DiagnosticDetail) string {
	status := "successful"
	if !event.InSuccessfulCall {
		status = "failed"
	}
	line := fmt.Sprintf("[%s] %s event", status, event.Type)
	if event.ContractID != "" {
		line += " from " + event.ContractID
	}
	if len(event.Topics) > 0 {
		line += " topics=[" + strings.Join(event.Topics, ", ") + "]"
	}
	if event.Data != "" {
		line += fmt.Sprintf(" data=%q", event.Data)
	}
	return line
}

func markdownFence(lines []string) string {
	longest := 2
	for _, line := range lines {
		current := 0
		for _, r := range line {
			if r == '`' {
				current++
				if current > longest {
					longest = current
				}
			} else {
				current = 0
			}
		}
	}
	return strings.Repeat("`", longest+1)
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
