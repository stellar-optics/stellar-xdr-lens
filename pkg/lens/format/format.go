// Package format renders decoded XDR into human- and machine-readable output.
//
// Formatters consume the neutral lens.Node tree rather than the concrete
// xdr.* types, so adding an output format never requires touching the
// decoder or knowing anything about XDR. Implement Formatter and the new
// format works for every registered type at once.
package format

import (
	"io"
	"os"

	"github.com/odusanya03/stellar-xdr-lens/pkg/lens"
)

// Formatter renders a decoded XDR value to a writer.
//
// This is the extension seam for output formats. See docs/architecture.md.
type Formatter interface {
	// Format writes v to w. Implementations must not write to w on error
	// beyond what they have already emitted.
	Format(w io.Writer, v *lens.Value) error
}

// Palette holds the ANSI escape sequences used for colourised output. The
// zero value renders without colour, which is what non-TTY output uses.
type Palette struct {
	Key    string
	Type   string
	Str    string
	Num    string
	Note   string
	Dim    string
	Add    string
	Remove string
	Change string
	Reset  string
}

// ColorPalette is the default 16-colour scheme, chosen to stay legible on
// both light and dark terminal backgrounds.
var ColorPalette = Palette{
	Key:    "\x1b[36m",
	Type:   "\x1b[35m",
	Str:    "\x1b[32m",
	Num:    "\x1b[33m",
	Note:   "\x1b[90m",
	Dim:    "\x1b[90m",
	Add:    "\x1b[32m",
	Remove: "\x1b[31m",
	Change: "\x1b[33m",
	Reset:  "\x1b[0m",
}

// NoColor is the empty palette: every sequence is a no-op.
var NoColor = Palette{}

// ShouldColor reports whether colourised output is appropriate for w.
//
// Colour is suppressed when the writer is not a terminal (so pipes and CI
// logs stay clean) and when NO_COLOR is set, per https://no-color.org.
func ShouldColor(w io.Writer) bool {
	if _, set := os.LookupEnv("NO_COLOR"); set {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// PaletteFor returns the colour palette appropriate for w, honouring an
// explicit override. mode is one of "auto", "always" or "never".
func PaletteFor(w io.Writer, mode string) Palette {
	switch mode {
	case "always":
		return ColorPalette
	case "never":
		return NoColor
	default:
		if ShouldColor(w) {
			return ColorPalette
		}
		return NoColor
	}
}
