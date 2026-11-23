package cli_test

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stellar-optics/stellar-xdr-lens/internal/cli"
	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

func TestConvertCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		args  []string
		stdin string
		want  string
	}{
		{
			name: "base64 to hex by default",
			args: []string{"convert", "AAAAAA=="},
			want: "00000000\n",
		},
		{
			name: "hex to base64",
			args: []string{"convert", "--from", "hex", "--to", "base64", "00000000"},
			want: "AAAAAA==\n",
		},
		{
			name: "hex shortcut",
			args: []string{"convert", "--hex", "AQIDBA=="},
			want: "01020304\n",
		},
		{
			name: "base64 shortcut",
			args: []string{"convert", "--from", "hex", "--base64", "01020304"},
			want: "AQIDBA==\n",
		},
		{
			name:  "stdin",
			args:  []string{"convert", "-"},
			stdin: "AQIDBA==",
			want:  "01020304\n",
		},
		{
			name:  "multiline input and whitespace",
			args:  []string{"convert", "-"},
			stdin: "AQID\n BA==\n",
			want:  "01020304\n",
		},
		{
			name: "0x prefix is accepted",
			args: []string{"convert", "--from", "hex", "--base64", "0x01020304"},
			want: "AQIDBA==\n",
		},
		{
			name: "real envelope round-trips through hex",
			args: []string{"convert", "--from", "base64", "--to", "hex", fixture(t, "tx_success.env")},
			want: hexOfFixture(t, "tx_success.env") + "\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			stdout, _, err := run(t, tc.stdin, tc.args...)
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if stdout != tc.want {
				t.Errorf("output = %q, want %q", stdout, tc.want)
			}
		})
	}
}

func hexOfFixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(fixture(t, name))
	if err != nil {
		t.Fatalf("decoding fixture: %v", err)
	}
	const digits = "0123456789abcdef"
	out := make([]byte, 0, len(raw)*2)
	for _, b := range raw {
		out = append(out, digits[b>>4], digits[b&0x0f])
	}
	return string(out)
}

func TestConvertRawInputAndOutput(t *testing.T) {
	t.Parallel()

	raw := []byte{0x00, 0x01, 0xfe, 0xff}
	path := filepath.Join(t.TempDir(), "payload.bin")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("writing raw fixture: %v", err)
	}

	stdout, _, err := run(t, "", "convert", "--file", path, "--from", "raw", "--to", "hex")
	if err != nil {
		t.Fatalf("convert raw to hex: %v", err)
	}
	if stdout != "0001feff\n" {
		t.Errorf("raw to hex = %q, want 0001feff", stdout)
	}

	stdout, _, err = run(t, "", "convert", "--file", path, "--from", "raw", "--to", "base64")
	if err != nil {
		t.Fatalf("convert raw to base64: %v", err)
	}
	if stdout != "AAH+/w==\n" {
		t.Errorf("raw to base64 = %q, want AAH+/w==", stdout)
	}

	stdout, _, err = run(t, "", "convert", "--from", "hex", "--to", "raw", "0001feff")
	if err != nil {
		t.Fatalf("convert hex to raw: %v", err)
	}
	if stdout != string(raw) {
		t.Errorf("hex to raw = %q, want %q", stdout, raw)
	}
}

func TestConvertReportsMalformedInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		args      []string
		wantInErr []string
		wantIs    error
	}{
		{
			name:      "illegal base64 character",
			args:      []string{"convert", "!!!!"},
			wantInErr: []string{"illegal character", "line 1, column 1", "byte offset 0"},
			wantIs:    lens.ErrInvalidBase64,
		},
		{
			name:      "illegal base64 character after newline",
			args:      []string{"convert", "-"},
			wantInErr: []string{"illegal character", "line 2, column 1", "byte offset"},
			wantIs:    lens.ErrInvalidBase64,
		},
		{
			name:      "missing base64 padding",
			args:      []string{"convert", "AAAAAA"},
			wantInErr: []string{"invalid base64 padding", "byte offset"},
			wantIs:    lens.ErrInvalidBase64,
		},
		{
			name:      "illegal hex character",
			args:      []string{"convert", "--from", "hex", "zz"},
			wantInErr: []string{"illegal character", "byte offset 0"},
			wantIs:    lens.ErrInvalidHex,
		},
		{
			name:      "odd hex length",
			args:      []string{"convert", "--from", "hex", "abc"},
			wantInErr: []string{"odd number of hex digits"},
			wantIs:    lens.ErrOddLengthHex,
		},
		{
			name:      "unknown format",
			args:      []string{"convert", "--from", "yaml", "AAAA"},
			wantInErr: []string{"unknown conversion format"},
			wantIs:    lens.ErrInvalidArgument,
		},
		{
			name:      "conflicting shortcuts",
			args:      []string{"convert", "--hex", "--base64", "AAAA"},
			wantInErr: []string{"mutually exclusive"},
			wantIs:    lens.ErrInvalidArgument,
		},
	}

	// The multiline case needs stdin, which the table cannot carry alongside a
	// unique arg list, so it is handled after the table.
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			stdin := ""
			if tc.name == "illegal base64 character after newline" {
				stdin = "AAAA\n!AAA"
			}
			_, _, err := run(t, stdin, tc.args...)
			if err == nil {
				t.Fatal("Execute() error = nil, want an error")
			}
			if tc.wantIs != nil && !errors.Is(err, tc.wantIs) {
				t.Errorf("error = %v, want it to wrap %v", err, tc.wantIs)
			}
			for _, want := range tc.wantInErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}

			code, _ := cli.ExitCode(err)
			if errors.Is(tc.wantIs, lens.ErrInvalidArgument) {
				if code != cli.ExitError {
					t.Errorf("exit code = %d, want %d", code, cli.ExitError)
				}
				return
			}
			if code != cli.ExitInvalidXDR {
				t.Errorf("exit code = %d, want %d", code, cli.ExitInvalidXDR)
			}
		})
	}
}
