package format

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// ErrInvalidTheme indicates that a terminal theme environment variable is
// invalid.
var ErrInvalidTheme = errors.New("format: invalid terminal theme")

// OSC 8 hyperlink framing. The opening sequence carries the URL, the visible
// text follows the string terminator, and an empty URL closes the link.
const (
	osc8Start = "\x1b]8;;"
	osc8End   = "\x1b\\"
)

// Stellar Expert bases used for clickable addresses, keyed by the network
// label `lens` accepts on the command line.
const (
	explorerPublicBase  = "https://stellar.expert/explorer/public"
	explorerTestnetBase = "https://stellar.expert/explorer/testnet"
)

// FormatHyperlink wraps text in an OSC 8 terminal hyperlink, so terminals that
// support the sequence (iTerm2, Windows Terminal, Ghostty, Alacritty, kitty,
// WezTerm) render it clickable while others show the plain text.
//
// The escapes are returned unconditionally: choosing whether to emit them is
// the caller's policy. The formatters in this package only ask for a link once
// colour is enabled for the writer, so `--color never` and non-TTY output stay
// free of escape artifacts. An empty text or URL returns text unchanged, and a
// URL containing control characters is refused so untrusted input cannot
// smuggle additional terminal escapes into the output.
//
//nolint:revive // FormatHyperlink is the public API name pinned by the CLI spec.
func FormatHyperlink(text, url string) string {
	if text == "" || url == "" || strings.ContainsAny(url, "\x1b\x07") {
		return text
	}
	return osc8Start + url + osc8End + text + osc8Start + osc8End
}

// ExplorerBaseURL maps a network name to the Stellar Expert base URL for that
// network. It accepts "public" (the mainnet label, also reachable as
// "mainnet") and "testnet". Any other value — including the empty string and
// a raw network passphrase — reports false, which leaves links disabled.
func ExplorerBaseURL(network string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(network)) {
	case "public", "mainnet":
		return explorerPublicBase, true
	case "testnet":
		return explorerTestnetBase, true
	default:
		return "", false
	}
}

// ExplorerURLForAddress returns the Stellar Expert URL for a strkey address:
// an account (G... or a muxed M... address) or a Soroban contract (C...). Text
// that is not a strkey address reports false, so plain values gain no link.
func ExplorerURLForAddress(base, address string) (string, bool) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" || !isStrkeyAddress(address) {
		return "", false
	}
	segment := "account"
	if address[0] == 'C' {
		segment = "contract"
	}
	return base + "/" + segment + "/" + address, true
}

// isStrkeyAddress reports whether s has the shape of a strkey address: 56
// base32 characters beginning with G (account), M (muxed account) or C
// (contract). The checksum is not verified — this only decides whether a
// decoded string is worth linking, and a wrong guess would produce a dead link
// rather than a wrong decode.
func isStrkeyAddress(s string) bool {
	if len(s) != 56 {
		return false
	}
	switch s[0] {
	case 'G', 'M', 'C':
	default:
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' || c >= '2' && c <= '7' {
			continue
		}
		return false
	}
	return true
}

// hyperlinkAddress links a strkey address when colour is enabled — the same
// switch that --color never and non-TTY output turn off — and an explorer base
// is configured. Everything else is returned untouched.
func hyperlinkAddress(p Palette, base, text string) string {
	if p == NoColor || base == "" {
		return text
	}
	url, ok := ExplorerURLForAddress(base, text)
	if !ok {
		return text
	}
	return FormatHyperlink(text, url)
}

// ThemeFromEnv returns the palette selected and customized by environment
// variables. LENS_THEME accepts "dark" (the default) or "light". Individual
// styles can be overridden with LENS_THEME_<THEME>_<STYLE>, where STYLE is
// KEY, TYPE, STR, NUM, NOTE, DIM, ADD, REMOVE, or CHANGE. Values are ANSI SGR
// parameters without the escape prefix (for example, "38;5;208"); an empty
// value disables that style.
func ThemeFromEnv() (Palette, error) {
	theme := strings.ToLower(strings.TrimSpace(os.Getenv("LENS_THEME")))
	if theme == "" {
		theme = "dark"
	}

	var palette Palette
	switch theme {
	case "dark":
		palette = ColorPalette
	case "light":
		palette = Palette{
			Key:    "\x1b[34m",
			Type:   "\x1b[35m",
			Str:    "\x1b[32m",
			Num:    "\x1b[31m",
			Note:   "\x1b[30m",
			Dim:    "\x1b[30m",
			Add:    "\x1b[32m",
			Remove: "\x1b[31m",
			Change: "\x1b[35m",
			Reset:  "\x1b[0m",
		}
	default:
		return Palette{}, fmt.Errorf("%w: LENS_THEME=%q (want dark or light)", ErrInvalidTheme, theme)
	}

	prefix := "LENS_THEME_" + strings.ToUpper(theme) + "_"
	overrides := []struct {
		name  string
		style *string
	}{
		{"KEY", &palette.Key},
		{"TYPE", &palette.Type},
		{"STR", &palette.Str},
		{"NUM", &palette.Num},
		{"NOTE", &palette.Note},
		{"DIM", &palette.Dim},
		{"ADD", &palette.Add},
		{"REMOVE", &palette.Remove},
		{"CHANGE", &palette.Change},
	}
	for _, override := range overrides {
		name := prefix + override.name
		value, ok := os.LookupEnv(name)
		if !ok {
			continue
		}

		sequence, err := ansiSGR(value)
		if err != nil {
			return Palette{}, fmt.Errorf("%w: %s=%q: %w", ErrInvalidTheme, name, value, err)
		}
		*override.style = sequence
	}

	return palette, nil
}

// PaletteForTheme applies the same output-mode and TTY policy as PaletteFor
// to a caller-supplied palette.
func PaletteForTheme(w io.Writer, mode string, palette Palette) Palette {
	switch mode {
	case "always":
		return palette
	case "never":
		return NoColor
	default:
		if ShouldColor(w) {
			return palette
		}
		return NoColor
	}
}

func ansiSGR(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	for _, part := range strings.Split(value, ";") {
		if part == "" {
			return "", errors.New("expected semicolon-separated integers from 0 to 255")
		}
		for _, digit := range part {
			if digit < '0' || digit > '9' {
				return "", errors.New("expected semicolon-separated integers from 0 to 255")
			}
		}
		code, err := strconv.Atoi(part)
		if err != nil || code > 255 {
			return "", errors.New("expected semicolon-separated integers from 0 to 255")
		}
	}
	return "\x1b[" + value + "m", nil
}
