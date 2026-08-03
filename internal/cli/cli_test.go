package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odusanya03/stellar-xdr-lens/internal/cli"
)

const fixtureDir = "../../testdata"

func fixturePath(name string) string {
	return filepath.Join(fixtureDir, name+".txt")
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(fixturePath(name))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	return strings.TrimSpace(string(b))
}

// run executes the CLI exactly as main does, returning what the user would
// see. Driving the real entry point keeps these tests honest about flag
// wiring, which is where CLI bugs actually live.
func run(t *testing.T, stdin string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errBuf bytes.Buffer
	err = cli.Execute(args, &out, &errBuf, strings.NewReader(stdin))
	return out.String(), errBuf.String(), err
}

func TestDecodeCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		args        []string
		stdin       string
		wantContain []string
	}{
		{
			name:        "from argument",
			args:        []string{"decode", fixture(t, "tx_success.env")},
			wantContain: []string{"TransactionEnvelope", "SourceAccount"},
		},
		{
			name:        "from file flag",
			args:        []string{"decode", "--file", fixturePath("tx_success.env")},
			wantContain: []string{"TransactionEnvelope", "SourceAccount"},
		},
		{
			name:        "from stdin",
			args:        []string{"decode", "-"},
			stdin:       fixture(t, "tx_success.env"),
			wantContain: []string{"TransactionEnvelope"},
		},
		{
			name:        "explicit type",
			args:        []string{"decode", "--type", "TransactionResult", fixture(t, "tx_failed.res")},
			wantContain: []string{"TransactionResult", "TransactionResultCode"},
		},
		{
			name:        "depth limit",
			args:        []string{"decode", "--depth", "2", fixture(t, "tx_success.env")},
			wantContain: []string{"hidden"},
		},
		{
			name:        "candidates for ambiguous input",
			args:        []string{"decode", "--candidates", "AAAAAA=="},
			wantContain: []string{"Memo", "Asset", "types matched"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			stdout, _, err := run(t, tc.stdin, tc.args...)
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			for _, want := range tc.wantContain {
				if !strings.Contains(stdout, want) {
					t.Errorf("output does not contain %q\n%s", want, stdout)
				}
			}
		})
	}
}

func TestDecodeJSONIsPipeable(t *testing.T) {
	t.Parallel()

	stdout, _, err := run(t, "", "decode", "--json", fixture(t, "tx_success.env"))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var doc struct {
		Type  string          `json:"type"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("--json output is not valid JSON: %v\n%s", err, stdout)
	}
	if doc.Type != "TransactionEnvelope" {
		t.Errorf("type = %q, want TransactionEnvelope", doc.Type)
	}
	if len(doc.Value) == 0 {
		t.Error("value is empty")
	}
}

func TestExplainCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		args        []string
		wantContain []string
	}{
		{
			name: "envelope alone",
			args: []string{"explain", fixture(t, "tx_soroban.env")},
			wantContain: []string{
				"Transaction from", "Operations", "invoke_host_function",
			},
		},
		{
			name: "result alone",
			args: []string{"explain", fixture(t, "tx_failed.res")},
			wantContain: []string{
				"Outcome", "tx_fee_bump_inner_failed",
			},
		},
		{
			name: "paired envelope and result",
			args: []string{
				"explain", fixture(t, "tx_failed.env"),
				"--result", fixture(t, "tx_failed.res"),
			},
			wantContain: []string{
				"failed at operation 0",
				"invoke_host_function",
				"trapped",
				"Outcome",
			},
		},
		{
			name: "paired via files",
			args: []string{
				"explain", "--file", fixturePath("tx_failed.env"),
				"--result-file", fixturePath("tx_failed.res"),
			},
			wantContain: []string{"failed at operation 0"},
		},
		{
			name: "verbose shows constants",
			args: []string{
				"explain", fixture(t, "tx_failed.res"), "--verbose",
			},
			wantContain: []string{"TransactionResultCode"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			stdout, _, err := run(t, "", tc.args...)
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			for _, want := range tc.wantContain {
				if !strings.Contains(stdout, want) {
					t.Errorf("output does not contain %q\n%s", want, stdout)
				}
			}
		})
	}
}

func TestExplainJSON(t *testing.T) {
	t.Parallel()

	stdout, _, err := run(t, "", "explain", "--json",
		fixture(t, "tx_failed.env"), "--result", fixture(t, "tx_failed.res"))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var doc struct {
		Kind       string `json:"Kind"`
		Headline   string `json:"Headline"`
		Operations []struct {
			Type   string `json:"Type"`
			Result *struct {
				Code    string `json:"Code"`
				Success bool   `json:"Success"`
			} `json:"Result"`
		} `json:"Operations"`
		Outcome *struct {
			Success bool `json:"Success"`
		} `json:"Outcome"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("--json output is not valid JSON: %v\n%s", err, stdout)
	}
	if doc.Outcome == nil || doc.Outcome.Success {
		t.Error("Outcome.Success should be false for the failed fixture")
	}
	if len(doc.Operations) == 0 || doc.Operations[0].Result == nil {
		t.Fatal("operations carry no result")
	}
	if doc.Operations[0].Result.Code != "invoke_host_function_trapped" {
		t.Errorf("operation code = %q, want invoke_host_function_trapped",
			doc.Operations[0].Result.Code)
	}
}

// TestExplainFailOnErrorExitCode covers the CI-gate behaviour.
func TestExplainFailOnErrorExitCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		env, res string
		wantCode int
	}{
		{"failed transaction exits 2", "tx_failed.env", "tx_failed.res", 2},
		{"successful transaction exits 0", "tx_soroban.env", "tx_soroban.res", 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, _, err := run(t, "", "explain", fixture(t, tc.env),
				"--result", fixture(t, tc.res), "--fail-on-error")
			gotCode, printable := cli.ExitCode(err)
			if gotCode != tc.wantCode {
				t.Errorf("exit code = %d, want %d (err=%v)", gotCode, tc.wantCode, err)
			}
			if tc.wantCode != 0 && printable {
				t.Error("a --fail-on-error exit should not print as an error message")
			}
		})
	}
}

func TestDiffCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		args        []string
		wantContain []string
	}{
		{
			name:        "differing files",
			args:        []string{"diff", fixturePath("tx_soroban.res"), fixturePath("tx_failed.res")},
			wantContain: []string{"Result.Code", "changed"},
		},
		{
			name:        "identical files",
			args:        []string{"diff", fixturePath("tx_failed.res"), fixturePath("tx_failed.res")},
			wantContain: []string{"no differences"},
		},
		{
			name: "inline base64 operands",
			args: []string{"diff", fixture(t, "tx_soroban.res"), fixture(t, "tx_failed.res")},
			wantContain: []string{
				"Result.Code",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			stdout, _, err := run(t, "", tc.args...)
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			for _, want := range tc.wantContain {
				if !strings.Contains(stdout, want) {
					t.Errorf("output does not contain %q\n%s", want, stdout)
				}
			}
		})
	}
}

func TestDiffExitCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		left, right string
		wantCode    int
	}{
		{"differing values exit 1", "tx_soroban.res", "tx_failed.res", 1},
		{"identical values exit 0", "tx_failed.res", "tx_failed.res", 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, _, err := run(t, "", "diff", "--exit-code",
				fixturePath(tc.left), fixturePath(tc.right))
			gotCode, _ := cli.ExitCode(err)
			if gotCode != tc.wantCode {
				t.Errorf("exit code = %d, want %d (err=%v)", gotCode, tc.wantCode, err)
			}
		})
	}
}

func TestDiffQuietPrintsNothing(t *testing.T) {
	t.Parallel()

	stdout, _, err := run(t, "", "diff", "--quiet",
		fixturePath("tx_soroban.res"), fixturePath("tx_failed.res"))
	if stdout != "" {
		t.Errorf("--quiet printed output:\n%s", stdout)
	}
	if code, _ := cli.ExitCode(err); code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

func TestDiffJSON(t *testing.T) {
	t.Parallel()

	stdout, _, err := run(t, "", "diff", "--json",
		fixturePath("tx_soroban.res"), fixturePath("tx_failed.res"))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var doc struct {
		LeftType  string `json:"left_type"`
		RightType string `json:"right_type"`
		Equal     bool   `json:"equal"`
		Changes   []struct {
			Path string `json:"path"`
			Op   string `json:"op"`
		} `json:"changes"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("--json output is not valid JSON: %v\n%s", err, stdout)
	}
	if doc.Equal {
		t.Error("equal = true for differing values")
	}
	if len(doc.Changes) == 0 {
		t.Fatal("no changes reported")
	}
	for _, c := range doc.Changes {
		if c.Path == "" || c.Op == "" {
			t.Errorf("change has empty path or op: %+v", c)
		}
	}
}

func TestTypesCommand(t *testing.T) {
	t.Parallel()

	stdout, _, err := run(t, "", "types")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	for _, want := range []string{"TransactionEnvelope", "TransactionResult", "ScVal"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("types output does not list %q", want)
		}
	}
}

func TestVersionCommand(t *testing.T) {
	t.Parallel()

	stdout, _, err := run(t, "", "version")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if strings.TrimSpace(stdout) == "" {
		t.Error("version printed nothing")
	}
}

// TestErrorsAreReported covers the paths a user hits by mistake.
func TestErrorsAreReported(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
	}{
		{"invalid base64", []string{"decode", "!!!not base64!!!"}},
		{"unknown type", []string{"decode", "--type", "NoSuchType", "AAAAAA=="}},
		{"missing file", []string{"decode", "--file", "/nonexistent/path.xdr"}},
		{"explain unsupported type", []string{"explain", "AAAAAA=="}},
		{"diff needs two operands", []string{"diff", "AAAAAA=="}},
		{"explain bad result", []string{"explain", fixture(t, "tx_failed.env"), "--result", "!!!"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, _, err := run(t, "", tc.args...)
			if err == nil {
				t.Fatal("Execute() error = nil, want an error")
			}
			code, printable := cli.ExitCode(err)
			if code == 0 {
				t.Error("exit code = 0 for an error")
			}
			if !printable {
				t.Error("a genuine error should be printable")
			}
		})
	}
}

// TestNoInputIsAnError guards against the CLI hanging or silently succeeding
// when given nothing to do.
func TestNoInputIsAnError(t *testing.T) {
	t.Parallel()

	_, _, err := run(t, "", "decode", "-")
	if err == nil {
		t.Error("decode with empty stdin error = nil, want an error")
	}
}

func TestBareInvocationPrintsHelp(t *testing.T) {
	t.Parallel()

	stdout, _, err := run(t, "")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	for _, want := range []string{"decode", "explain", "diff", "Usage"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("bare invocation help does not mention %q", want)
		}
	}
}

func TestColorFlagIsHonoured(t *testing.T) {
	t.Parallel()

	always, _, err := run(t, "", "decode", "--color", "always", fixture(t, "tx_success.env"))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	never, _, err := run(t, "", "decode", "--color", "never", fixture(t, "tx_success.env"))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if !strings.Contains(always, "\x1b[") {
		t.Error("--color always produced no escape sequences")
	}
	if strings.Contains(never, "\x1b[") {
		t.Error("--color never produced escape sequences")
	}
}
