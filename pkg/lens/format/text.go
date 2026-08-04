package format

import (
	"fmt"
	"io"
	"strings"

	"github.com/stellar/go-stellar-sdk/amount"
	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/Stellar-optics/stellar-xdr-lens/pkg/lens"
)

// SummaryFormatter renders a lens.Summary as readable prose.
type SummaryFormatter struct {
	palette Palette
	verbose bool
}

// SummaryOption configures a SummaryFormatter.
type SummaryOption func(*SummaryFormatter)

// WithSummaryPalette sets the colour palette.
func WithSummaryPalette(p Palette) SummaryOption {
	return func(s *SummaryFormatter) { s.palette = p }
}

// WithVerbose includes the underlying XDR constant alongside each short code,
// which is useful when cross-referencing the protocol definition.
func WithVerbose(on bool) SummaryOption {
	return func(s *SummaryFormatter) { s.verbose = on }
}

// NewSummaryFormatter returns a SummaryFormatter.
func NewSummaryFormatter(opts ...SummaryOption) *SummaryFormatter {
	s := &SummaryFormatter{palette: NoColor}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Format writes a human-readable rendering of s to w.
func (f *SummaryFormatter) Format(w io.Writer, s *lens.Summary) error {
	if s == nil {
		return fmt.Errorf("format: nil summary")
	}
	p := f.palette
	var b strings.Builder

	// Headline, coloured by outcome so failures are obvious at a glance.
	headColor := p.Note
	if s.Outcome != nil {
		if s.Outcome.Success {
			headColor = p.Add
		} else {
			headColor = p.Remove
		}
	}
	b.WriteString(headColor + s.Headline + p.Reset + "\n")

	// Transaction-level facts.
	b.WriteString("\n")
	if s.Source != "" {
		f.field(&b, "Source", s.Source)
	}
	if s.FeeBumpSource != "" {
		f.field(&b, "Fee-bump source", s.FeeBumpSource)
	}
	if s.SeqNum != 0 {
		f.field(&b, "Sequence", fmt.Sprintf("%d", s.SeqNum))
	}
	if s.Fee != 0 {
		f.field(&b, "Fee bid", stroops(s.Fee))
	}
	if s.FeeCharged != 0 {
		f.field(&b, "Fee charged", stroops(s.FeeCharged))
	}
	if s.Memo != "" {
		f.field(&b, "Memo", s.Memo)
	}
	if s.SignatureCt > 0 {
		f.field(&b, "Signatures", fmt.Sprintf("%d", s.SignatureCt))
	}
	for _, pc := range s.Preconds {
		f.field(&b, "Precondition", pc)
	}

	// Operations.
	if len(s.Operations) > 0 {
		fmt.Fprintf(&b, "\n%sOperations (%d)%s\n", p.Key, len(s.Operations), p.Reset)
		for _, op := range s.Operations {
			f.operation(&b, op)
		}
	}

	// Outcome, including the actionable hint.
	if s.Outcome != nil {
		f.outcome(&b, s.Outcome)
	}

	if _, err := io.WriteString(w, b.String()); err != nil {
		return fmt.Errorf("format: write summary: %w", err)
	}
	return nil
}

func (f *SummaryFormatter) field(b *strings.Builder, label, value string) {
	p := f.palette
	fmt.Fprintf(b, "  %s%-16s%s %s\n", p.Key, label, p.Reset, value)
}

func (f *SummaryFormatter) operation(b *strings.Builder, op lens.OpSummary) {
	p := f.palette

	marker, colour := " ", p.Reset
	if op.Result != nil {
		if op.Result.Success {
			marker, colour = "✓", p.Add
		} else {
			marker, colour = "✗", p.Remove
		}
	}

	fmt.Fprintf(b, "  %s%s%s %s[%d]%s %s%s%s",
		colour, marker, p.Reset, p.Dim, op.Index, p.Reset, p.Type, op.Type, p.Reset)
	if op.Detail != "" && op.Detail != op.Type {
		fmt.Fprintf(b, " — %s", op.Detail)
	}
	b.WriteString("\n")

	if op.Source != "" {
		fmt.Fprintf(b, "      %ssource: %s%s\n", p.Dim, op.Source, p.Reset)
	}
	if op.Result != nil && !op.Result.Success {
		fmt.Fprintf(b, "      %s%s%s: %s\n", p.Remove, op.Result.Code, p.Reset, op.Result.Summary)
		if f.verbose {
			fmt.Fprintf(b, "      %s%s%s\n", p.Dim, op.Result.Constant, p.Reset)
		}
		if op.Result.Hint != "" {
			fmt.Fprintf(b, "      %s→ %s%s\n", p.Note, op.Result.Hint, p.Reset)
		}
	}
}

func (f *SummaryFormatter) outcome(b *strings.Builder, o *lens.Outcome) {
	p := f.palette
	fmt.Fprintf(b, "\n%sOutcome%s\n", p.Key, p.Reset)

	f.reason(b, "  ", o.Reason)
	if o.InnerReason != nil {
		fmt.Fprintf(b, "  %sinner transaction:%s\n", p.Dim, p.Reset)
		f.reason(b, "  ", *o.InnerReason)
	}
}

func (f *SummaryFormatter) reason(b *strings.Builder, indent string, r lens.Reason) {
	p := f.palette
	colour := p.Remove
	if r.Success {
		colour = p.Add
	}
	fmt.Fprintf(b, "%s%s%s%s: %s\n", indent, colour, r.Code, p.Reset, r.Summary)
	if f.verbose {
		fmt.Fprintf(b, "%s  %s%s%s\n", indent, p.Dim, r.Constant, p.Reset)
	}
	if r.Hint != "" {
		fmt.Fprintf(b, "%s  %s→ %s%s\n", indent, p.Note, r.Hint, p.Reset)
	}
}

// stroops renders a stroop count as both XLM and the raw integer, since fee
// discussions happen in both units.
func stroops(v int64) string {
	return fmt.Sprintf("%s XLM (%d stroops)", amount.String(xdr.Int64(v)), v)
}
