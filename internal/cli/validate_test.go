package cli_test

import (
	"encoding/base64"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/stellar-optics/stellar-xdr-lens/internal/cli"
	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

func TestDecodeValidateOnly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		args        []string
		wantContain []string
	}{
		{
			name:        "detected type",
			args:        []string{"decode", "--validate-only", fixture(t, "tx_success.env")},
			wantContain: []string{"valid XDR: TransactionEnvelope", "bytes"},
		},
		{
			name:        "explicit type",
			args:        []string{"decode", "--validate-only", "--type", "TransactionResult", fixture(t, "tx_success.res")},
			wantContain: []string{"valid XDR: TransactionResult"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			stdout, _, err := run(t, "", tc.args...)
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if strings.Contains(stdout, "├─") || strings.Contains(stdout, "Explorer") {
				t.Errorf("--validate-only rendered a tree:\n%s", stdout)
			}
			for _, want := range tc.wantContain {
				if !strings.Contains(stdout, want) {
					t.Errorf("output does not contain %q: %s", want, stdout)
				}
			}
		})
	}
}

func TestDecodeValidateOnlyQuiet(t *testing.T) {
	t.Parallel()

	stdout, _, err := run(t, "", "decode", "--validate-only", "--quiet", fixture(t, "tx_success.env"))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if stdout != "" {
		t.Errorf("--quiet printed %q, want nothing", stdout)
	}
}

func TestDecodeValidateOnlyReportsOffsets(t *testing.T) {
	t.Parallel()

	raw, err := base64.StdEncoding.DecodeString(fixture(t, "tx_success.env"))
	if err != nil {
		t.Fatalf("decoding fixture: %v", err)
	}
	truncated := base64.StdEncoding.EncodeToString(raw[:40])

	tests := []struct {
		name      string
		payload   string
		wantInErr []string
	}{
		{
			name:      "truncated XDR",
			payload:   truncated,
			wantInErr: []string{"truncated payload", "byte offset 40", "TransactionEnvelope"},
		},
		{
			name:      "illegal base64",
			payload:   "!!!!",
			wantInErr: []string{"invalid base64", "illegal character", "byte offset 0"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, _, err := run(t, "", "decode", "--validate-only", "--type", "TransactionEnvelope", tc.payload)
			if err == nil {
				t.Fatal("Execute() error = nil, want an error")
			}
			for _, want := range tc.wantInErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
			// Invalid payloads keep the established invalid-XDR exit code.
			if code, _ := cli.ExitCode(err); code != cli.ExitInvalidXDR {
				t.Errorf("exit code = %d, want %d", code, cli.ExitInvalidXDR)
			}
		})
	}
}

func TestDecodeValidateOnlyRejectsInteractive(t *testing.T) {
	t.Parallel()

	_, _, err := run(t, "", "decode", "--validate-only", "--interactive", fixture(t, "tx_success.env"))
	if !errors.Is(err, lens.ErrInvalidArgument) {
		t.Fatalf("Execute() error = %v, want ErrInvalidArgument", err)
	}
}

// BenchmarkDecodeFullTree and BenchmarkDecodeValidateOnly show the cost the
// --validate-only path avoids by not building a Node tree. Both pin the type,
// which is how ingestion pipelines use the command.
func BenchmarkDecodeFullTree(b *testing.B) {
	var out, errOut strings.Builder
	payload := loadCLIFixture(b, "tx_success.env")
	b.ReportAllocs()
	for b.Loop() {
		out.Reset()
		if err := cli.Execute([]string{"decode", "--type", "TransactionEnvelope", payload}, &out, &errOut, strings.NewReader("")); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecodeValidateOnly(b *testing.B) {
	var out, errOut strings.Builder
	payload := loadCLIFixture(b, "tx_success.env")
	b.ReportAllocs()
	for b.Loop() {
		out.Reset()
		if err := cli.Execute([]string{"decode", "--validate-only", "--type", "TransactionEnvelope", payload}, &out, &errOut, strings.NewReader("")); err != nil {
			b.Fatal(err)
		}
	}
}

func loadCLIFixture(b *testing.B, name string) string {
	b.Helper()
	data, err := os.ReadFile(fixturePath(name))
	if err != nil {
		b.Fatalf("reading fixture %s: %v", name, err)
	}
	return strings.TrimSpace(string(data))
}
