package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func fixturePath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "testdata", name+".txt")
}

func fixtureValue(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(fixturePath(t, name))
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}
	return strings.TrimSpace(string(b))
}

func lensBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "lens")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/lens")
	cmd.Dir = repoRoot(t)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("building lens binary: %v\n%s", err, output)
	}
	return bin
}

func runLens(t *testing.T, env []string, args ...string) (string, string, error) {
	t.Helper()
	cmd := exec.Command(lensBinary(t), args...)
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(), env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func TestDecodeCommandE2E(t *testing.T) {
	t.Parallel()

	stdout, stderr, err := runLens(t, nil, "decode", fixtureValue(t, "tx_success.env"))
	if err != nil {
		t.Fatalf("decode command failed: %v\n%s", err, stderr)
	}
	for _, want := range []string{"TransactionEnvelope", "SourceAccount", "Operations"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("decode output does not contain %q\n%s", want, stdout)
		}
	}
}

func TestDecodeJSONCommandE2E(t *testing.T) {
	t.Parallel()

	stdout, stderr, err := runLens(t, nil, "decode", "--json", fixtureValue(t, "tx_success.env"))
	if err != nil {
		t.Fatalf("decode --json failed: %v\n%s", err, stderr)
	}

	var doc struct {
		Type  string          `json:"type"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("decode --json output is not valid JSON: %v\n%s", err, stdout)
	}
	if doc.Type != "TransactionEnvelope" {
		t.Fatalf("type = %q, want TransactionEnvelope", doc.Type)
	}
	if len(doc.Value) == 0 {
		t.Fatal("json value is empty")
	}
}

func TestExplainCommandE2E(t *testing.T) {
	t.Parallel()

	stdout, stderr, err := runLens(t, nil,
		"explain",
		fixtureValue(t, "tx_failed.env"),
		"--result", fixtureValue(t, "tx_failed.res"),
	)
	if err != nil {
		t.Fatalf("explain command failed: %v\n%s", err, stderr)
	}
	for _, want := range []string{
		"failed at operation 0",
		"invoke_host_function",
		"Outcome",
		"tx_fee_bump_inner_failed",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("explain output does not contain %q\n%s", want, stdout)
		}
	}
}

func TestDiffCommandE2E(t *testing.T) {
	t.Parallel()

	stdout, stderr, err := runLens(t, nil,
		"diff",
		fixturePath(t, "tx_success.env"),
		fixturePath(t, "tx_failed.env"),
	)
	if err != nil {
		t.Fatalf("diff command failed: %v\n%s", err, stderr)
	}
	if !strings.Contains(stdout, "TransactionEnvelope") && !strings.Contains(stdout, "Result") {
		t.Fatalf("diff output missing expected structure\n%s", stdout)
	}
	if !strings.Contains(stdout, "changed") && !strings.Contains(stdout, "~") {
		t.Fatalf("diff output missing changes\n%s", stdout)
	}
}

func TestResultDiffCommandE2E(t *testing.T) {
	t.Parallel()

	stdout, stderr, err := runLens(t, nil,
		"result-diff",
		fixturePath(t, "tx_failed.env"),
		fixturePath(t, "tx_failed.res"),
	)
	if err != nil {
		t.Fatalf("result-diff command failed: %v\n%s", err, stderr)
	}
	for _, want := range []string{"tx_fee_bump_inner_failed", "Operations (1)", "invoke_host_function_trapped"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("result-diff output does not contain %q\n%s", want, stdout)
		}
	}
}

func TestDiffModesCommandE2E(t *testing.T) {
	t.Parallel()

	stats, stderr, err := runLens(t, nil, "diff", "--stats",
		fixturePath(t, "tx_soroban.res"), fixturePath(t, "tx_failed.res"))
	if err != nil {
		t.Fatalf("diff --stats failed: %v\n%s", err, stderr)
	}
	if !strings.Contains(stats, "change(s):") {
		t.Fatalf("diff --stats output missing summary\n%s", stats)
	}

	footprint, stderr, err := runLens(t, nil, "diff", "--footprint",
		fixturePath(t, "tx_soroban.env"), fixturePath(t, "tx_failed.env"))
	if err != nil {
		t.Fatalf("diff --footprint failed: %v\n%s", err, stderr)
	}
	for _, want := range []string{"read-only:", "read-write:"} {
		if !strings.Contains(footprint, want) {
			t.Fatalf("diff --footprint output does not contain %q\n%s", want, footprint)
		}
	}
}

func TestCLIIgnoresProxyEnvironmentE2E(t *testing.T) {
	t.Parallel()

	stdout, stderr, err := runLens(t,
		[]string{
			"HTTP_PROXY=http://proxy.invalid:8080",
			"HTTPS_PROXY=http://proxy.invalid:8080",
			"ALL_PROXY=http://proxy.invalid:8080",
			"NO_PROXY=",
		},
		"decode", fixtureValue(t, "tx_success.env"),
	)
	if err != nil {
		t.Fatalf("decode with proxy env failed: %v\n%s", err, stderr)
	}
	if !strings.Contains(stdout, "TransactionEnvelope") {
		t.Fatalf("proxy-environmented decode output missing expected payload\n%s", stdout)
	}
}
