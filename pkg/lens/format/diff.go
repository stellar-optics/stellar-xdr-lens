package format

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

// DiffFormatter renders a lens.DiffResult.
type DiffFormatter struct {
	palette Palette
	summary bool
}

// DiffOption configures a DiffFormatter.
type DiffOption func(*DiffFormatter)

// WithDiffPalette sets the colour palette.
func WithDiffPalette(p Palette) DiffOption {
	return func(d *DiffFormatter) { d.palette = p }
}

// WithSummary enables ASCII summary table output.
func WithSummary(enabled bool) DiffOption {
	return func(d *DiffFormatter) { d.summary = enabled }
}

// NewDiffFormatter returns a DiffFormatter.
func NewDiffFormatter(opts ...DiffOption) *DiffFormatter {
	d := &DiffFormatter{palette: NoColor}
	for _, o := range opts {
		o(d)
	}
	return d
}

// Format writes a readable rendering of the diff to w.
func (f *DiffFormatter) Format(w io.Writer, d *lens.DiffResult) error {
	if d == nil {
		return fmt.Errorf("format: nil diff")
	}
	p := f.palette
	var b strings.Builder

	if d.LeftType != d.RightType {
		fmt.Fprintf(&b, "%stype differs: %s vs %s%s\n\n", p.Change, d.LeftType, d.RightType, p.Reset)
	} else {
		fmt.Fprintf(&b, "%s%s%s\n\n", p.Type, d.LeftType, p.Reset)
	}

	if d.Equal() {
		fmt.Fprintf(&b, "%sno differences%s\n", p.Add, p.Reset)
		if len(d.Warnings) > 0 {
			fmt.Fprintf(&b, "\n")
			for _, warn := range d.Warnings {
				fmt.Fprintf(&b, "%swarning: %s%s\n", p.Change, warn, p.Reset)
			}
		}
		if _, err := io.WriteString(w, b.String()); err != nil {
			return fmt.Errorf("format: write diff: %w", err)
		}
		return nil
	}

	for _, c := range d.Changes {
		f.change(&b, c)
	}

	if len(d.Warnings) > 0 {
		fmt.Fprintf(&b, "\n")
		for _, warn := range d.Warnings {
			fmt.Fprintf(&b, "%swarning: %s%s\n", p.Change, warn, p.Reset)
		}
	}

	fmt.Fprintf(&b, "\n%s%s%s\n", p.Dim, d.Summary(), p.Reset)

	if _, err := io.WriteString(w, b.String()); err != nil {
		return fmt.Errorf("format: write diff: %w", err)
	}
	return nil
}

func (f *DiffFormatter) change(b *strings.Builder, c lens.Change) {
	p := f.palette

	switch c.Op {
	case lens.ChangeAdded:
		fmt.Fprintf(b, "%s+ %s%s = %s\n", p.Add, c.Path, p.Reset, f.value(c.After, c.AfterNote))
	case lens.ChangeRemoved:
		fmt.Fprintf(b, "%s- %s%s = %s\n", p.Remove, c.Path, p.Reset, f.value(c.Before, c.BeforeNote))
	case lens.ChangeReordered:
		fmt.Fprintf(b, "%s~ %s%s: reordered from index %v to %v\n",
			p.Change, c.Path, p.Reset, c.Before, c.After)
	default:
		fmt.Fprintf(b, "%s~ %s%s\n", p.Change, c.Path, p.Reset)
		fmt.Fprintf(b, "    %s- %s%s\n", p.Remove, f.value(c.Before, c.BeforeNote), p.Reset)
		fmt.Fprintf(b, "    %s+ %s%s\n", p.Add, f.value(c.After, c.AfterNote), p.Reset)
	}
}

func (f *DiffFormatter) value(v any, note string) string {
	if v == nil && note == "" {
		return "(none)"
	}
	if v == nil {
		return note
	}
	s := fmt.Sprintf("%v", v)
	if note != "" {
		return s + " " + f.palette.Note + note + f.palette.Reset
	}
	return s
}

// DiffSummaryFormatter renders a lens.DiffResult as a concise ASCII summary table.
type DiffSummaryFormatter struct {
	palette Palette
}

// DiffSummaryOption configures a DiffSummaryFormatter.
type DiffSummaryOption func(*DiffSummaryFormatter)

// WithDiffSummaryPalette sets the colour palette.
func WithDiffSummaryPalette(p Palette) DiffSummaryOption {
	return func(d *DiffSummaryFormatter) { d.palette = p }
}

// NewDiffSummaryFormatter returns a DiffSummaryFormatter.
func NewDiffSummaryFormatter(opts ...DiffSummaryOption) *DiffSummaryFormatter {
	d := &DiffSummaryFormatter{palette: NoColor}
	for _, o := range opts {
		o(d)
	}
	return d
}

// Format writes a summary table of the diff to w.
func (f *DiffSummaryFormatter) Format(w io.Writer, d *lens.DiffResult) error {
	if d == nil {
		return fmt.Errorf("format: nil diff")
	}
	p := f.palette
	var b strings.Builder

	if d.LeftType != d.RightType {
		fmt.Fprintf(&b, "%stype differs: %s vs %s%s\n\n", p.Change, d.LeftType, d.RightType, p.Reset)
	} else {
		fmt.Fprintf(&b, "%s%s%s\n\n", p.Type, d.LeftType, p.Reset)
	}

	if d.Equal() {
		fmt.Fprintf(&b, "%sno differences%s\n", p.Add, p.Reset)
		if len(d.Warnings) > 0 {
			fmt.Fprintf(&b, "\n")
			for _, warn := range d.Warnings {
				fmt.Fprintf(&b, "%swarning: %s%s\n", p.Change, warn, p.Reset)
			}
		}
		if _, err := io.WriteString(w, b.String()); err != nil {
			return fmt.Errorf("format: write diff summary: %w", err)
		}
		return nil
	}

	// The statistics table is the single implementation of the per-path
	// breakdown, shared by --summary and --format summary.
	stats := lens.SummarizeDiff(d)
	b.WriteString(FormatDiffStatsTable(&stats))

	if len(d.Warnings) > 0 {
		fmt.Fprintf(&b, "\n")
		for _, warn := range d.Warnings {
			fmt.Fprintf(&b, "%swarning: %s%s\n", p.Change, warn, p.Reset)
		}
	}

	fmt.Fprintf(&b, "\n%s%s%s\n", p.Dim, d.Summary(), p.Reset)

	if _, err := io.WriteString(w, b.String()); err != nil {
		return fmt.Errorf("format: write diff summary: %w", err)
	}
	return nil
}

// truncate truncates a string to maxLen characters.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// DiffChangeDoc represents a single difference in the structured JSON diff schema.
type DiffChangeDoc struct {
	Path       string `json:"path"`
	Op         string `json:"op"`
	Before     any    `json:"before,omitempty"`
	After      any    `json:"after,omitempty"`
	BeforeNote string `json:"before_note,omitempty"`
	AfterNote  string `json:"after_note,omitempty"`
}

// DiffDocument represents the full structured diff in the stable JSON schema.
type DiffDocument struct {
	LeftType  string          `json:"left_type"`
	RightType string          `json:"right_type"`
	Equal     bool            `json:"equal"`
	Summary   string          `json:"summary"`
	Changes   []DiffChangeDoc `json:"changes"`
	Redacted  []string        `json:"redacted,omitempty"`
	Warnings  []string        `json:"warnings,omitempty"`
}

// BuildDiffDocument converts a lens.DiffResult into the stable JSON DiffDocument.
func BuildDiffDocument(d *lens.DiffResult) DiffDocument {
	if d == nil {
		return DiffDocument{}
	}
	changes := make([]DiffChangeDoc, 0, len(d.Changes))
	for _, c := range d.Changes {
		changes = append(changes, DiffChangeDoc{
			Path:       c.Path,
			Op:         c.Op.String(),
			Before:     c.Before,
			After:      c.After,
			BeforeNote: c.BeforeNote,
			AfterNote:  c.AfterNote,
		})
	}
	return DiffDocument{
		LeftType:  d.LeftType,
		RightType: d.RightType,
		Equal:     d.Equal(),
		Summary:   d.Summary(),
		Changes:   changes,
		Redacted:  d.Redacted,
		Warnings:  d.Warnings,
	}
}

// DiffJSONFormatter renders a lens.DiffResult as structured JSON adhering to the stable schema.
type DiffJSONFormatter struct {
	indent string
}

// DiffJSONOption configures a DiffJSONFormatter.
type DiffJSONOption func(*DiffJSONFormatter)

// WithDiffJSONIndent configures indentation for DiffJSONFormatter.
func WithDiffJSONIndent(indent string) DiffJSONOption {
	return func(f *DiffJSONFormatter) { f.indent = indent }
}

// NewDiffJSONFormatter returns a new DiffJSONFormatter.
func NewDiffJSONFormatter(opts ...DiffJSONOption) *DiffJSONFormatter {
	f := &DiffJSONFormatter{indent: "  "}
	for _, opt := range opts {
		opt(f)
	}
	return f
}

// Format writes structured JSON representation of the diff result to w.
func (f *DiffJSONFormatter) Format(w io.Writer, d *lens.DiffResult) error {
	if d == nil {
		return fmt.Errorf("format: nil diff")
	}
	doc := BuildDiffDocument(d)
	enc := json.NewEncoder(w)
	if f.indent != "" {
		enc.SetIndent("", f.indent)
	}
	if err := enc.Encode(doc); err != nil {
		return fmt.Errorf("format: encoding json diff: %w", err)
	}
	return nil
}
