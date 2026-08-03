package format

import (
	"fmt"
	"io"
	"strings"

	"github.com/odusanya03/stellar-xdr-lens/pkg/lens"
)

// DiffFormatter renders a lens.DiffResult.
type DiffFormatter struct {
	palette Palette
}

// DiffOption configures a DiffFormatter.
type DiffOption func(*DiffFormatter)

// WithDiffPalette sets the colour palette.
func WithDiffPalette(p Palette) DiffOption {
	return func(d *DiffFormatter) { d.palette = p }
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
		if _, err := io.WriteString(w, b.String()); err != nil {
			return fmt.Errorf("format: write diff: %w", err)
		}
		return nil
	}

	for _, c := range d.Changes {
		f.change(&b, c)
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
