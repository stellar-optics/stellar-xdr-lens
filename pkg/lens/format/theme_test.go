package format_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens/format"
)

func TestThemeFromEnv(t *testing.T) {
	t.Run("default dark theme", func(t *testing.T) {
		setThemeDefaults(t, "dark")

		got, err := format.ThemeFromEnv()
		if err != nil {
			t.Fatalf("ThemeFromEnv() error = %v", err)
		}
		if got != format.ColorPalette {
			t.Errorf("ThemeFromEnv() = %#v, want default dark palette %#v", got, format.ColorPalette)
		}
	})

	t.Run("light theme and overrides", func(t *testing.T) {
		setThemeDefaults(t, "light")
		t.Setenv("LENS_THEME_LIGHT_KEY", "38;5;39")
		t.Setenv("LENS_THEME_LIGHT_NUM", "")

		got, err := format.ThemeFromEnv()
		if err != nil {
			t.Fatalf("ThemeFromEnv() error = %v", err)
		}
		if got.Key != "\x1b[38;5;39m" {
			t.Errorf("Key = %q, want custom ANSI style", got.Key)
		}
		if got.Num != "" {
			t.Errorf("Num = %q, want disabled style", got.Num)
		}
		if got.Type != "\x1b[35m" {
			t.Errorf("Type = %q, want light theme default", got.Type)
		}
	})

	t.Run("theme is case and whitespace insensitive", func(t *testing.T) {
		setThemeDefaults(t, "light")
		t.Setenv("LENS_THEME", "  LIGHT ")

		got, err := format.ThemeFromEnv()
		if err != nil {
			t.Fatalf("ThemeFromEnv() error = %v", err)
		}
		if got.Key != "\x1b[34m" {
			t.Errorf("Key = %q, want light theme default", got.Key)
		}
	})

	t.Run("invalid theme", func(t *testing.T) {
		t.Setenv("LENS_THEME", "neon")
		got, err := format.ThemeFromEnv()
		if !errors.Is(err, format.ErrInvalidTheme) {
			t.Fatalf("ThemeFromEnv() error = %v, want ErrInvalidTheme", err)
		}
		if got != (format.Palette{}) {
			t.Errorf("ThemeFromEnv() palette = %#v, want zero value on error", got)
		}
	})

	for _, tc := range []struct {
		name  string
		value string
	}{
		{"escape injection", "\x1b[31m"},
		{"text injection", "31m"},
		{"empty SGR parameter", "38;;5"},
		{"out of range parameter", "38;256"},
		{"negative parameter", "-1"},
	} {
		t.Run("invalid style "+tc.name, func(t *testing.T) {
			setThemeDefaults(t, "dark")
			t.Setenv("LENS_THEME_DARK_KEY", tc.value)
			if _, err := format.ThemeFromEnv(); !errors.Is(err, format.ErrInvalidTheme) {
				t.Fatalf("ThemeFromEnv() error = %v, want ErrInvalidTheme", err)
			}
		})
	}
}

func setThemeDefaults(t *testing.T, theme string) {
	t.Helper()
	t.Setenv("LENS_THEME", theme)

	values := map[string][]string{
		"dark":  {"36", "35", "32", "33", "90", "90", "32", "31", "33"},
		"light": {"34", "35", "32", "31", "30", "30", "32", "31", "35"},
	}
	styles := []string{"KEY", "TYPE", "STR", "NUM", "NOTE", "DIM", "ADD", "REMOVE", "CHANGE"}
	for i, style := range styles {
		t.Setenv("LENS_THEME_"+strings.ToUpper(theme)+"_"+style, values[theme][i])
	}
}

func TestPaletteForTheme(t *testing.T) {
	palette := format.Palette{Key: "\x1b[34m", Reset: "\x1b[0m"}
	var buffer bytes.Buffer

	tests := []struct {
		name      string
		mode      string
		wantColor bool
	}{
		{name: "always", mode: "always", wantColor: true},
		{name: "never", mode: "never"},
		{name: "auto on non-terminal", mode: "auto"},
		{name: "unknown mode defaults to auto", mode: "unexpected"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := format.PaletteForTheme(&buffer, tc.mode, palette)
			if (got.Reset != "") != tc.wantColor {
				t.Errorf("PaletteForTheme() colored = %v, want %v", got.Reset != "", tc.wantColor)
			}
			if tc.wantColor && got.Key != palette.Key {
				t.Errorf("PaletteForTheme() key = %q, want %q", got.Key, palette.Key)
			}
		})
	}
}

func TestFormatHyperlink(t *testing.T) {
	const text = "GAPB7YMUQ6MPQ23PTFDNBZP7HGGTZPI2DT5JJURUOIXNPLGHW2O3WYXZ"
	const url = "https://stellar.expert/explorer/public/account/" + text

	tests := []struct {
		name string
		text string
		url  string
		want string
	}{
		{
			name: "OSC 8 sequence",
			text: text,
			url:  url,
			want: "\x1b]8;;" + url + "\x1b\\" + text + "\x1b]8;;\x1b\\",
		},
		{name: "empty text is unchanged", text: "", url: url, want: ""},
		{name: "empty url is unchanged", text: text, url: "", want: text},
		{
			name: "control characters in the url are refused",
			text: text,
			url:  "https://example.com/\x1b]8;;evil",
			want: text,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := format.FormatHyperlink(tc.text, tc.url); got != tc.want {
				t.Errorf("FormatHyperlink() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestExplorerBaseURL(t *testing.T) {
	tests := []struct {
		network string
		want    string
		ok      bool
	}{
		{network: "public", want: "https://stellar.expert/explorer/public", ok: true},
		{network: "public ", want: "https://stellar.expert/explorer/public", ok: true},
		{network: "mainnet", want: "https://stellar.expert/explorer/public", ok: true},
		{network: "TESTNET", want: "https://stellar.expert/explorer/testnet", ok: true},
		{network: ""},
		{network: "futurenet"},
		{network: "Test SDF Network ; September 2015"},
	}
	for _, tc := range tests {
		t.Run(tc.network, func(t *testing.T) {
			got, ok := format.ExplorerBaseURL(tc.network)
			if ok != tc.ok || got != tc.want {
				t.Errorf("ExplorerBaseURL(%q) = (%q, %v), want (%q, %v)", tc.network, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestExplorerURLForAddress(t *testing.T) {
	const base = "https://stellar.expert/explorer/testnet/"
	const account = "GAPB7YMUQ6MPQ23PTFDNBZP7HGGTZPI2DT5JJURUOIXNPLGHW2O3WYXZ"
	const contract = "CBGSBKYMYO6OMGHQXXNOBRGVUDFUDVC2XLC3SXON5R2SNXILR7XCKKY3"

	tests := []struct {
		name    string
		address string
		want    string
		ok      bool
	}{
		{
			name:    "account",
			address: account,
			want:    "https://stellar.expert/explorer/testnet/account/" + account,
			ok:      true,
		},
		{
			name:    "contract",
			address: contract,
			want:    "https://stellar.expert/explorer/testnet/contract/" + contract,
			ok:      true,
		},
		{name: "not an address", address: "native"},
		{name: "hex hash", address: strings.Repeat("ab", 32)},
		{name: "empty"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := format.ExplorerURLForAddress(base, tc.address)
			if ok != tc.ok || got != tc.want {
				t.Errorf("ExplorerURLForAddress(%q) = (%q, %v), want (%q, %v)", tc.address, got, ok, tc.want, tc.ok)
			}
		})
	}

	if _, ok := format.ExplorerURLForAddress("", account); ok {
		t.Error("ExplorerURLForAddress() with no base = ok, want no link")
	}
}

// TestSummaryFormatterHyperlinks covers both halves of the acceptance
// criteria: a coloured terminal gets clickable addresses, and `--color never`
// (the palette PaletteForTheme returns for it) keeps the output clean text.
func TestSummaryFormatterHyperlinks(t *testing.T) {
	env := loadFixtureValue(t, "tx_failed.env.txt")
	res := loadFixtureValue(t, "tx_failed.res.txt")
	summary, err := lens.ExplainPair(env, res)
	if err != nil {
		t.Fatalf("ExplainPair() error = %v", err)
	}
	if summary.Source == "" {
		t.Fatal("fixture summary has no source address to link")
	}
	const base = "https://stellar.expert/explorer/public"

	t.Run("coloured terminal links the source", func(t *testing.T) {
		var buf bytes.Buffer
		f := format.NewSummaryFormatter(
			format.WithSummaryPalette(format.PaletteForTheme(&buf, "always", format.ColorPalette)),
			format.WithSummaryHyperlinks(base),
		)
		if err := f.Format(&buf, summary); err != nil {
			t.Fatalf("Format() error = %v", err)
		}
		want := format.FormatHyperlink(summary.Source, base+"/account/"+summary.Source)
		if !strings.Contains(buf.String(), want) {
			t.Errorf("output does not contain the OSC 8 link %q:\n%s", want, buf.String())
		}
	})

	t.Run("--color never suppresses links", func(t *testing.T) {
		var buf bytes.Buffer
		f := format.NewSummaryFormatter(
			format.WithSummaryPalette(format.PaletteForTheme(&buf, "never", format.ColorPalette)),
			format.WithSummaryHyperlinks(base),
		)
		if err := f.Format(&buf, summary); err != nil {
			t.Fatalf("Format() error = %v", err)
		}
		if strings.Contains(buf.String(), "\x1b]8;;") {
			t.Errorf("--color never output contains a hyperlink escape:\n%s", buf.String())
		}
		if strings.Contains(buf.String(), "\x1b[") {
			t.Errorf("--color never output contains an escape sequence:\n%s", buf.String())
		}
		if !strings.Contains(buf.String(), summary.Source) {
			t.Errorf("output lost the plain source address:\n%s", buf.String())
		}
	})

	t.Run("no explorer leaves addresses plain", func(t *testing.T) {
		var buf bytes.Buffer
		f := format.NewSummaryFormatter(format.WithSummaryPalette(format.ColorPalette))
		if err := f.Format(&buf, summary); err != nil {
			t.Fatalf("Format() error = %v", err)
		}
		if strings.Contains(buf.String(), "\x1b]8;;") {
			t.Errorf("output without an explorer contains a hyperlink escape:\n%s", buf.String())
		}
	})
}

// TestTreeFormatterHyperlinks checks the decoded tree, where public keys and
// contract ids are rendered as strkey scalars.
func TestTreeFormatterHyperlinks(t *testing.T) {
	const base = "https://stellar.expert/explorer/testnet"

	t.Run("accounts and contracts are linked", func(t *testing.T) {
		value := loadFixtureValue(t, "tx_soroban.env.txt")

		var plain, linked bytes.Buffer
		if err := format.NewTreeFormatter(format.WithPalette(format.ColorPalette)).
			Format(&plain, value); err != nil {
			t.Fatalf("Format() error = %v", err)
		}
		if err := format.NewTreeFormatter(
			format.WithPalette(format.ColorPalette),
			format.WithHyperlinks(base),
		).Format(&linked, value); err != nil {
			t.Fatalf("Format() error = %v", err)
		}

		addresses := []string{
			"GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAW",
			"CBGSBKYMYO6OMGHQXXNOBRGVUDFUDVC2XLC3SXON5R2SNXILR7XCKKY3",
		}
		for _, addr := range addresses {
			if !strings.Contains(plain.String(), addr) {
				t.Fatalf("fixture tree does not contain %s", addr)
			}
			want := format.FormatHyperlink(addr, base+"/account/"+addr)
			if addr[0] == 'C' {
				want = format.FormatHyperlink(addr, base+"/contract/"+addr)
			}
			if !strings.Contains(linked.String(), want) {
				t.Errorf("linked tree does not contain %q", want)
			}
		}
	})

	t.Run("non-TTY output stays clean", func(t *testing.T) {
		value := loadFixtureValue(t, "tx_soroban.env.txt")
		var buf bytes.Buffer
		f := format.NewTreeFormatter(
			format.WithPalette(format.PaletteForTheme(&buf, "auto", format.ColorPalette)),
			format.WithHyperlinks(base),
		)
		if err := f.Format(&buf, value); err != nil {
			t.Fatalf("Format() error = %v", err)
		}
		if strings.Contains(buf.String(), "\x1b]8;;") {
			t.Errorf("piped output contains a hyperlink escape:\n%s", buf.String())
		}
	})
}

func loadFixtureValue(t *testing.T, name string) *lens.Value {
	t.Helper()
	path := filepath.Join("..", "..", "..", "testdata", name)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	value, err := lens.Decode(strings.TrimSpace(string(content)))
	if err != nil {
		t.Fatalf("decoding %s: %v", name, err)
	}
	return value
}

func TestThemePaletteFormatsCanonicalFixture(t *testing.T) {
	setThemeDefaults(t, "light")
	t.Setenv("LENS_THEME_LIGHT_KEY", "38;5;39")

	palette, err := format.ThemeFromEnv()
	if err != nil {
		t.Fatalf("ThemeFromEnv() error = %v", err)
	}

	fixturePath := filepath.Join("..", "..", "..", "testdata", "tx_success.env.txt")
	fixture, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("reading canonical fixture: %v", err)
	}
	value, err := lens.Decode(strings.TrimSpace(string(fixture)))
	if err != nil {
		t.Fatalf("decoding canonical fixture: %v", err)
	}

	var output bytes.Buffer
	formatter := format.NewTreeFormatter(
		format.WithPalette(format.PaletteForTheme(&output, "always", palette)),
	)
	if err := formatter.Format(&output, value); err != nil {
		t.Fatalf("formatting canonical fixture: %v", err)
	}
	if !strings.Contains(output.String(), "\x1b[38;5;39m") {
		t.Error("custom light palette was not used for canonical fixture output")
	}
	for _, expected := range []string{"TransactionEnvelope", "SourceAccount", "Fee"} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("fixture output does not contain %q", expected)
		}
	}
}
