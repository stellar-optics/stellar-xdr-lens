package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/network"
	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/internal/cli"
	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
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
			name:        "contract data instance storage",
			args:        []string{"decode", "--type", "ContractDataEntry", fixture(t, "contract_data_instance")},
			wantContain: []string{"ContractDataEntry", "instance storage key", "Durability: persistent", "Contract: CDFP5OV6AAAQEAYEAUDAOCAJBIFQYDIOB4IBCEQTCQKRMFYYDENBWGZ3"},
		},
		{
			name:        "contract data temporary storage",
			args:        []string{"decode", "--type", "ContractDataEntry", fixture(t, "contract_data_temporary")},
			wantContain: []string{"Durability: temporary", "Sym: SEEN"},
		},
		{
			name:        "ledger header",
			args:        []string{"decode", "--type", "LedgerHeader", fixture(t, "ledger_header")},
			wantContain: []string{"LedgerHeader", "protocol 22", "104591297310.9999952", "0.5000000 (stroops: 5000000)"},
		},
		{
			name:        "ledger header history entry",
			args:        []string{"decode", "--type", "LedgerHeaderHistoryEntry", fixture(t, "ledger_header_history")},
			wantContain: []string{"LedgerHeaderHistoryEntry", "Header", "protocol 22"},
		},
		{
			name:        "bucket entry tombstone",
			args:        []string{"decode", "--type", "BucketEntry", fixture(t, "bucket_entry_dead")},
			wantContain: []string{"BucketEntry", "dead entry (tombstone)", "LedgerEntryTypeTtl"},
		},
		{
			name:        "bucket metadata hot archive",
			args:        []string{"decode", "--type", "BucketMetadata", fixture(t, "bucket_metadata")},
			wantContain: []string{"BucketMetadata", "protocol 22", "BucketListType"},
		},
		{
			name:        "scp envelope",
			args:        []string{"decode", "--type", "ScpEnvelope", fixture(t, "scp_envelope")},
			wantContain: []string{"ScpEnvelope", "externalize", "GBIFCUSTKRKVMV2YLFNFWXC5LZPWAYLCMNSGKZTHNBUWU23MNVXG6NJZ"},
		},
		{
			name:        "scp quorum set",
			args:        []string{"decode", "--type", "ScpQuorumSet", fixture(t, "scp_quorum_set")},
			wantContain: []string{"ScpQuorumSet", "InnerSets", "GBYHC4TTOR2XM53YPF5HW7D5PZ7YBAMCQOCILBUHRCEYVC4MRWHI66TB"},
		},
		{
			name:        "ttl entry",
			args:        []string{"decode", "--type", "TtlEntry", fixture(t, "ttl_entry")},
			wantContain: []string{"TtlEntry", "live through ledger 55281000"},
		},
		{
			name:        "eviction iterator",
			args:        []string{"decode", "--type", "EvictionIterator", fixture(t, "eviction_iterator")},
			wantContain: []string{"EvictionIterator", "BucketFileOffset", "IsCurrBucket"},
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
		{
			name:        "offer entry ledger type",
			args:        []string{"decode", "--type", "OfferEntry", fixture(t, "offer_entry")},
			wantContain: []string{"OfferEntry", "OfferId", "7/3 (= 2.3333333)", "GAISEM2EKVTHPCEZVK54ZXPO74ABCIRTIRKWM54ITGVLXTG5537QAQZ5"},
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

func TestDecodeUsesEnvironmentTheme(t *testing.T) {
	t.Setenv("LENS_THEME", "light")
	t.Setenv("LENS_THEME_LIGHT_KEY", "38;5;39")

	stdout, _, err := run(t, "", "decode", "--color", "always", fixture(t, "tx_success.env"))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(stdout, "\x1b[38;5;39m") {
		t.Error("decode output did not use the environment-configured theme")
	}
}

func TestDecodeReportsInvalidEnvironmentTheme(t *testing.T) {
	t.Setenv("LENS_THEME", "unknown")

	_, _, err := run(t, "", "decode", fixture(t, "tx_success.env"))
	if err == nil || !strings.Contains(err.Error(), "LENS_THEME") {
		t.Fatalf("Execute() error = %v, want invalid LENS_THEME error", err)
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
			name: "paired markdown report",
			args: []string{
				"explain", fixture(t, "tx_failed.env"),
				"--result", fixture(t, "tx_failed.res"), "--markdown",
			},
			wantContain: []string{
				"| Transaction | Details |",
				"| Status | Failed |",
				"| Operation | Source | Description | Result | Status | Hint |",
				"invoke_host_function_trapped",
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
		{
			name: "isolate flag pinpoints failing op",
			args: []string{
				"explain", fixture(t, "tx_failed.env"),
				"--result", fixture(t, "tx_failed.res"),
				"--isolate",
			},
			wantContain: []string{
				"Failure Isolation Report",
				"Failing Op Index:  0",
				"Execution Failure: true",
				"invoke_host_function_trapped",
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

func TestExplainJSON(t *testing.T) {
	t.Parallel()

	stdout, _, err := run(t, "", "explain", "--json",
		fixture(t, "tx_failed.env"), "--result", fixture(t, "tx_failed.res"))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var doc struct {
		Kind       string `json:"kind"`
		Headline   string `json:"headline"`
		Operations []struct {
			Type   string `json:"type"`
			Result *struct {
				Code    string `json:"code"`
				Success bool   `json:"success"`
			} `json:"result"`
		} `json:"operations"`
		Outcome *struct {
			Success bool `json:"success"`
		} `json:"outcome"`
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

func TestDiffSemanticFlag(t *testing.T) {
	t.Parallel()

	leftRaw := xdr.ScVal{Type: xdr.ScValTypeScvI128, I128: &xdr.Int128Parts{Hi: 1, Lo: 5}}
	rightRaw := xdr.ScVal{Type: xdr.ScValTypeScvI128, I128: &xdr.Int128Parts{Hi: 1, Lo: 6}}
	left, err := xdr.MarshalBase64(leftRaw)
	if err != nil {
		t.Fatalf("MarshalBase64(left) error = %v", err)
	}
	right, err := xdr.MarshalBase64(rightRaw)
	if err != nil {
		t.Fatalf("MarshalBase64(right) error = %v", err)
	}
	stdout, _, err := run(t, "", "diff", "--type", "ScVal", "--semantic", left, right)
	if err != nil {
		t.Fatalf("diff --semantic error = %v", err)
	}
	for _, want := range []string{"18446744073709551621", "18446744073709551622"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("semantic diff output does not contain %q:\n%s", want, stdout)
		}
	}
}

func TestMetaDiffCommand(t *testing.T) {
	t.Parallel()

	meta := xdr.TransactionMeta{V: 0, Operations: &[]xdr.OperationMeta{}}
	payload, err := xdr.MarshalBase64(meta)
	if err != nil {
		t.Fatalf("MarshalBase64() error = %v", err)
	}

	stdout, _, err := run(t, "", "meta-diff", payload)
	if err != nil {
		t.Fatalf("meta-diff error = %v", err)
	}
	if !strings.Contains(stdout, "no balance changes") {
		t.Errorf("meta-diff output = %q, want empty-delta message", stdout)
	}

	stdout, _, err = run(t, payload, "meta-diff", "--json", "-")
	if err != nil {
		t.Fatalf("meta-diff --json from stdin error = %v", err)
	}
	var result struct {
		Changes []json.RawMessage `json:"changes"`
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("meta-diff JSON is invalid: %v\n%s", err, stdout)
	}
	if len(result.Changes) != 0 {
		t.Errorf("meta-diff JSON has %d changes, want 0", len(result.Changes))
	}

	metaPath := filepath.Join(t.TempDir(), "transaction-meta.xdr")
	if err := os.WriteFile(metaPath, []byte(payload), 0o600); err != nil {
		t.Fatalf("WriteFile(metadata fixture) error = %v", err)
	}
	stdout, _, err = run(t, "", "meta-diff", "--file", metaPath)
	if err != nil {
		t.Fatalf("meta-diff --file error = %v", err)
	}
	if !strings.Contains(stdout, "no balance changes") {
		t.Errorf("meta-diff --file output = %q, want empty-delta message", stdout)
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
		{"asset invalid base64", []string{"asset", "!!!not base64!!!"}},
		{"asset non-asset payload", []string{"asset", fixture(t, "overlay_hello")}},
		{"asset empty network", []string{"asset", "--network", "", fixture(t, "asset_native")}},
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
	for _, want := range []string{"decode", "explain", "diff", "asset", "Usage"} {
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

// TestExplainJSONShapeIsCamelCase pins the documented JSON contract for
// `explain --json`.
//
// The field names are part of the public interface: scripts and sibling tools
// address them with jq, so a rename is a breaking change and belongs in a
// release note rather than in a refactor. camelCase matches Horizon and
// Soroban RPC, which is what users already have in their fingers.
func TestExplainJSONShapeIsCamelCase(t *testing.T) {
	t.Parallel()

	stdout, _, err := run(t, "", "explain", "--json",
		fixture(t, "tx_failed.env"), "--result", fixture(t, "tx_failed.res"))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("--json output is not valid JSON: %v\n%s", err, stdout)
	}

	for _, want := range []string{"kind", "headline", "fee", "feeCharged", "operations", "outcome"} {
		if _, ok := doc[want]; !ok {
			t.Errorf("top-level JSON has no %q field; keys were %v", want, mapKeys(doc))
		}
	}
	// Go field names must not leak through.
	for _, unwanted := range []string{"Kind", "Headline", "Operations", "Outcome", "FeeCharged"} {
		if _, ok := doc[unwanted]; ok {
			t.Errorf("JSON leaked the Go field name %q", unwanted)
		}
	}

	outcome, ok := doc["outcome"].(map[string]any)
	if !ok {
		t.Fatalf("outcome is %T, want an object", doc["outcome"])
	}
	for _, want := range []string{"success", "reason", "innerReason", "failedOperations"} {
		if _, ok := outcome[want]; !ok {
			t.Errorf("outcome has no %q field; keys were %v", want, mapKeys(outcome))
		}
	}

	reason, ok := outcome["reason"].(map[string]any)
	if !ok {
		t.Fatalf("outcome.reason is %T, want an object", outcome["reason"])
	}
	for _, want := range []string{"code", "constant", "summary", "success"} {
		if _, ok := reason[want]; !ok {
			t.Errorf("reason has no %q field; keys were %v", want, mapKeys(reason))
		}
	}

	ops, ok := doc["operations"].([]any)
	if !ok || len(ops) == 0 {
		t.Fatalf("operations is %T, want a non-empty array", doc["operations"])
	}
	op, ok := ops[0].(map[string]any)
	if !ok {
		t.Fatalf("operations[0] is %T, want an object", ops[0])
	}
	for _, want := range []string{"index", "type", "result"} {
		if _, ok := op[want]; !ok {
			t.Errorf("operations[0] has no %q field; keys were %v", want, mapKeys(op))
		}
	}
}

func TestAssetCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		args        []string
		stdin       string
		wantContain []string
	}{
		{
			name:        "native asset on mainnet",
			args:        []string{"asset", fixture(t, "asset_native")},
			wantContain: []string{"native", "CAS3J7GYLGXMF6TDJBBYYSE3HQ6BBSMLNUQ34T6TZMYMW2EVH34XOWMA"},
		},
		{
			name:        "credit asset on mainnet",
			args:        []string{"asset", fixture(t, "asset_usdc")},
			wantContain: []string{"USDC", "credit_alphanum4", "CCW67TSZV3SSS2HXMBQ5JFGCKJNXKZM7UQUWUZPUTHXSTZLEO7SJMI75"},
		},
		{
			name:        "native asset on testnet",
			args:        []string{"asset", "--network", "testnet", fixture(t, "asset_native")},
			wantContain: []string{"testnet", "CDLZFC3SYJYDZT7K67VZ75HPJVIEUVNIXF47ZG2FB2RMQQVU2HHGCYSC"},
		},
		{
			name:        "custom passphrase",
			args:        []string{"asset", "--network", "Integration Test Network", fixture(t, "asset_native")},
			wantContain: []string{"Integration Test Network"},
		},
		{
			name:        "from stdin",
			args:        []string{"asset", "-"},
			stdin:       fixture(t, "asset_native"),
			wantContain: []string{"CAS3J7GYLGXMF6TDJBBYYSE3HQ6BBSMLNUQ34T6TZMYMW2EVH34XOWMA"},
		},
		{
			name:        "from file flag",
			args:        []string{"asset", "--file", fixturePath("asset_usdc")},
			wantContain: []string{"USDC:GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN"},
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

func TestAssetCommandJSON(t *testing.T) {
	t.Parallel()

	stdout, _, err := run(t, "", "asset", "--json", fixture(t, "asset_usdc"))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var doc struct {
		Type       string `json:"type"`
		Code       string `json:"code"`
		Issuer     string `json:"issuer"`
		Canonical  string `json:"canonical"`
		Native     bool   `json:"native"`
		ContractID string `json:"contractId"`
		Network    string `json:"network"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("--json output is not valid JSON: %v\n%s", err, stdout)
	}
	if doc.Type != "credit_alphanum4" || doc.Code != "USDC" || doc.Native {
		t.Errorf("asset = %+v, want a non-native USDC asset", doc)
	}
	if doc.ContractID != "CCW67TSZV3SSS2HXMBQ5JFGCKJNXKZM7UQUWUZPUTHXSTZLEO7SJMI75" {
		t.Errorf("contractId = %q, want the mainnet USDC contract", doc.ContractID)
	}
	if doc.Network != network.PublicNetworkPassphrase {
		t.Errorf("network = %q, want the mainnet passphrase", doc.Network)
	}
}

func TestTypesJSONCommand(t *testing.T) {
	t.Parallel()

	plain, _, err := run(t, "", "types")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	plainCount := len(strings.Fields(plain))

	stdout, _, err := run(t, "", "types", "--json")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var doc []struct {
		Name        string `json:"name"`
		Category    string `json:"category"`
		Description string `json:"description"`
		Priority    int    `json:"priority"`
		MinWireSize int    `json:"minWireSize"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("types --json output is not valid JSON: %v\n%s", err, stdout)
	}
	if len(doc) != plainCount {
		t.Errorf("types --json lists %d types, plain output lists %d", len(doc), plainCount)
	}

	var hello *struct {
		Name        string `json:"name"`
		Category    string `json:"category"`
		Description string `json:"description"`
		Priority    int    `json:"priority"`
		MinWireSize int    `json:"minWireSize"`
	}
	for i := range doc {
		if doc[i].Name == "Hello" {
			hello = &doc[i]
		}
	}
	if hello == nil {
		t.Fatal("types --json does not include Hello")
	}
	if hello.Category != "overlay" || hello.Priority != 35 || hello.MinWireSize != 164 {
		t.Errorf("Hello entry = %+v, want overlay/35/164", hello)
	}
}

func mapKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestDecodeNewFormatsAndCommands(t *testing.T) {
	t.Parallel()

	fixturePayload := fixture(t, "tx_success.env")

	t.Run("decode --summary", func(t *testing.T) {
		stdout, _, err := run(t, "", "decode", "--summary", fixturePayload)
		if err != nil {
			t.Fatalf("decode --summary: %v", err)
		}
		if !strings.Contains(stdout, "TransactionEnvelope") || !strings.Contains(stdout, "type=v1") {
			t.Errorf("unexpected summary output: %s", stdout)
		}
	})

	t.Run("decode --format summary", func(t *testing.T) {
		stdout, _, err := run(t, "", "decode", "--format", "summary", fixturePayload)
		if err != nil {
			t.Fatalf("decode --format summary: %v", err)
		}
		if !strings.Contains(stdout, "TransactionEnvelope") {
			t.Errorf("unexpected summary output: %s", stdout)
		}
	})

	t.Run("decode --txrep", func(t *testing.T) {
		stdout, _, err := run(t, "", "decode", "--txrep", fixturePayload)
		if err != nil {
			t.Fatalf("decode --txrep: %v", err)
		}
		if !strings.Contains(stdout, "type: ENVELOPE_TYPE_TX") {
			t.Errorf("unexpected txrep output: %s", stdout)
		}
	})

	t.Run("decode --format txrep", func(t *testing.T) {
		stdout, _, err := run(t, "", "decode", "--format", "txrep", fixturePayload)
		if err != nil {
			t.Fatalf("decode --format txrep: %v", err)
		}
		if !strings.Contains(stdout, "type: ENVELOPE_TYPE_TX") {
			t.Errorf("unexpected txrep output: %s", stdout)
		}
	})

	t.Run("decode --csv", func(t *testing.T) {
		stdout, _, err := run(t, "", "decode", "--csv", fixturePayload)
		if err != nil {
			t.Fatalf("decode --csv: %v", err)
		}
		lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
		if len(lines) != 2 {
			t.Fatalf("decode --csv should emit a header and one row, got %d lines: %s", len(lines), stdout)
		}
		if !strings.HasPrefix(lines[0], "type,variant,source,fee_bump_source,") ||
			!strings.HasSuffix(lines[0], ",detail") {
			t.Errorf("unexpected csv header: %s", lines[0])
		}
		if !strings.HasPrefix(lines[1], "TransactionEnvelope,v1,") {
			t.Errorf("unexpected csv row: %s", lines[1])
		}
	})

	t.Run("decode --format csv", func(t *testing.T) {
		stdout, _, err := run(t, "", "decode", "--format", "csv", fixturePayload)
		if err != nil {
			t.Fatalf("decode --format csv: %v", err)
		}
		if !strings.Contains(stdout, "TransactionEnvelope") {
			t.Errorf("unexpected csv output: %s", stdout)
		}
	})

	t.Run("explore command", func(t *testing.T) {
		stdout, _, err := run(t, "q\n", "explore", fixturePayload)
		if err != nil {
			t.Fatalf("explore: %v", err)
		}
		if !strings.Contains(stdout, "=== TransactionEnvelope Explorer ===") {
			t.Errorf("unexpected explore output: %s", stdout)
		}
	})

	t.Run("decode -i interactive", func(t *testing.T) {
		stdout, _, err := run(t, "q\n", "decode", "-i", fixturePayload)
		if err != nil {
			t.Fatalf("decode -i: %v", err)
		}
		if !strings.Contains(stdout, "Explorer") {
			t.Errorf("unexpected interactive output: %s", stdout)
		}
	})
}

func TestDecodeStreamAndCanonical(t *testing.T) {
	t.Parallel()

	rawFixture := fixture(t, "tx_success.env")

	// 1. Decode with --canonical flag
	stdout, stderr, err := run(t, "", "decode", "--canonical", rawFixture)
	if err != nil {
		t.Fatalf("expected --canonical to succeed on real fixture: %v, stderr: %s", err, stderr)
	}
	if !strings.Contains(stdout, "TransactionEnvelope") {
		t.Errorf("stdout missing TransactionEnvelope: %s", stdout)
	}

	// 2. Decode with --canonical failure
	badCanonical := rawFixture + "===" // corrupt padding
	_, _, err = run(t, "", "decode", "--canonical", badCanonical)
	if err == nil {
		t.Errorf("expected error on non-canonical input")
	}

	// 3. Decode with --stream base64 lines
	streamInput := rawFixture + "\n" + rawFixture + "\n"
	stdoutStream, stderrStream, err := run(t, streamInput, "decode", "--stream", "--json", "-")
	if err != nil {
		t.Fatalf("expected --stream to succeed: %v, stderr: %s", err, stderrStream)
	}
	if strings.Count(stdoutStream, "TransactionEnvelope") < 2 {
		t.Errorf("expected at least 2 occurrences in stream output, got: %s", stdoutStream)
	}
}

func TestEncodeCommand(t *testing.T) {
	t.Parallel()

	validAccount := "GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAW"
	txJSON := `{
		"source_account": "` + validAccount + `",
		"fee": 100,
		"sequence": 12345,
		"operations": [
			{
				"type": "payment",
				"destination": "` + validAccount + `",
				"amount": 1000
			}
		]
	}`

	t.Run("encode_transaction_envelope", func(t *testing.T) {
		stdout, _, err := run(t, "", "encode", txJSON)
		if err != nil {
			t.Fatalf("encode failed: %v", err)
		}
		b64 := strings.TrimSpace(stdout)
		if len(b64) == 0 {
			t.Fatalf("expected base64 output")
		}
		// verify decoding back
		val, err := lens.Decode(b64)
		if err != nil || val.Type != "TransactionEnvelope" {
			t.Fatalf("failed decoding built transaction: %v", err)
		}
	})

	t.Run("encode_transaction_strict", func(t *testing.T) {
		stdout, _, err := run(t, "", "encode", "--strict", txJSON)
		if err != nil {
			t.Fatalf("strict encode failed: %v", err)
		}
		if len(strings.TrimSpace(stdout)) == 0 {
			t.Fatalf("expected base64 output")
		}
	})

	t.Run("encode_scval", func(t *testing.T) {
		stdout, _, err := run(t, "", "encode", "--scval", `{"symbol": "hello"}`)
		if err != nil {
			t.Fatalf("encode scval failed: %v", err)
		}
		b64 := strings.TrimSpace(stdout)
		val, err := lens.Decode(b64)
		if err != nil || val.Type != "ScVal" {
			t.Fatalf("failed decoding scval: %v", err)
		}
	})

	t.Run("encode_from_stdin", func(t *testing.T) {
		stdout, _, err := run(t, txJSON, "encode", "-")
		if err != nil {
			t.Fatalf("encode from stdin failed: %v", err)
		}
		if len(strings.TrimSpace(stdout)) == 0 {
			t.Fatalf("expected output from stdin")
		}
	})

	t.Run("encode_strict_error", func(t *testing.T) {
		badJSON := `{"source_account": "` + validAccount + `", "unknown_field": 123}`
		_, _, err := run(t, "", "encode", "--strict", badJSON)
		if err == nil {
			t.Fatalf("expected strict validation error")
		}
	})
}
