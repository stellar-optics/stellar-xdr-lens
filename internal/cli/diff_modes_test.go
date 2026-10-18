package cli_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/internal/cli"
)

const diffTestSource = "GAPB7YMUQ6MPQ23PTFDNBZP7HGGTZPI2DT5JJURUOIXNPLGHW2O3WYXZ"

func signature(hint, payload byte) xdr.DecoratedSignature {
	return xdr.DecoratedSignature{
		Hint:      xdr.SignatureHint{hint, hint, hint, hint},
		Signature: []byte{payload, payload},
	}
}

func signedEnvelope(t *testing.T, signatures ...xdr.DecoratedSignature) string {
	t.Helper()
	op := xdr.Operation{Body: xdr.OperationBody{
		Type:           xdr.OperationTypeBumpSequence,
		BumpSequenceOp: &xdr.BumpSequenceOp{BumpTo: xdr.SequenceNumber(7)},
	}}
	env := xdr.TransactionEnvelope{
		Type: xdr.EnvelopeTypeEnvelopeTypeTx,
		V1: &xdr.TransactionV1Envelope{
			Tx: xdr.Transaction{
				SourceAccount: xdr.MustMuxedAddress(diffTestSource),
				Fee:           100,
				SeqNum:        xdr.SequenceNumber(1),
				Cond:          xdr.Preconditions{Type: xdr.PreconditionTypePrecondNone},
				Memo:          xdr.Memo{Type: xdr.MemoTypeMemoNone},
				Operations:    []xdr.Operation{op},
				Ext:           xdr.TransactionExt{V: 0},
			},
			Signatures: signatures,
		},
	}
	payload, err := xdr.MarshalBase64(env)
	if err != nil {
		t.Fatalf("MarshalBase64(envelope): %v", err)
	}
	return payload
}

func TestDiffStatsFlag(t *testing.T) {
	t.Parallel()

	stdout, _, err := run(t, "", "diff", "--stats",
		fixturePath("tx_soroban.res"), fixturePath("tx_failed.res"))
	if err != nil {
		t.Fatalf("diff --stats error = %v", err)
	}
	for _, want := range []string{"change(s):", "Result"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("--stats output does not contain %q:\n%s", want, stdout)
		}
	}
}

func TestDiffStatsJSON(t *testing.T) {
	t.Parallel()

	stdout, _, err := run(t, "", "diff", "--stats", "--json",
		fixturePath("tx_soroban.res"), fixturePath("tx_failed.res"))
	if err != nil {
		t.Fatalf("diff --stats --json error = %v", err)
	}
	var doc struct {
		Total    int  `json:"total"`
		Equal    bool `json:"equal"`
		Modified int  `json:"modified"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("stats JSON is invalid: %v\n%s", err, stdout)
	}
	if doc.Total == 0 || doc.Equal {
		t.Errorf("stats = %+v, want a non-empty difference", doc)
	}
}

func TestDiffUnorderedSignersFlag(t *testing.T) {
	t.Parallel()

	left := signedEnvelope(t, signature(1, 10), signature(2, 20))
	right := signedEnvelope(t, signature(2, 20), signature(1, 10))

	positional, _, err := run(t, "", "diff", left, right)
	if err != nil {
		t.Fatalf("diff error = %v", err)
	}
	if strings.Contains(positional, "no differences") {
		t.Error("positional diff treated reordered signatures as equal")
	}

	unordered, _, err := run(t, "", "diff", "--unordered-signers", left, right)
	if err != nil {
		t.Fatalf("diff --unordered-signers error = %v", err)
	}
	if !strings.Contains(unordered, "no differences") {
		t.Errorf("--unordered-signers output = %q, want no differences", unordered)
	}
}

func TestDiffFootprintFlag(t *testing.T) {
	t.Parallel()

	stdout, _, err := run(t, "", "diff", "--footprint",
		fixturePath("tx_soroban.env"), fixturePath("tx_failed.env"))
	if err != nil {
		t.Fatalf("diff --footprint error = %v", err)
	}
	for _, want := range []string{"read-only:", "read-write:", "common"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("--footprint output does not contain %q:\n%s", want, stdout)
		}
	}

	jsonOut, _, err := run(t, "", "diff", "--footprint", "--json",
		fixturePath("tx_soroban.env"), fixturePath("tx_failed.env"))
	if err != nil {
		t.Fatalf("diff --footprint --json error = %v", err)
	}
	var doc struct {
		ReadOnly struct {
			Added []string `json:"added"`
		} `json:"readOnly"`
		ReadWrite struct {
			Removed []string `json:"removed"`
		} `json:"readWrite"`
	}
	if err := json.Unmarshal([]byte(jsonOut), &doc); err != nil {
		t.Fatalf("footprint JSON is invalid: %v\n%s", err, jsonOut)
	}
	if len(doc.ReadOnly.Added) == 0 && len(doc.ReadWrite.Removed) == 0 {
		t.Errorf("footprint JSON reported no set differences: %s", jsonOut)
	}
}

func TestDiffFootprintFlagRejectsTypelessInput(t *testing.T) {
	t.Parallel()

	_, _, err := run(t, "", "diff", "--footprint",
		fixturePath("asset_usdc"), fixturePath("asset_native"))
	if err == nil {
		t.Fatal("diff --footprint error = nil, want an error for values without footprints")
	}
}

func TestResultDiffCommand(t *testing.T) {
	t.Parallel()

	stdout, _, err := run(t, "", "result-diff",
		fixturePath("tx_failed.env"), fixturePath("tx_failed.res"))
	if err != nil {
		t.Fatalf("result-diff error = %v", err)
	}
	for _, want := range []string{"tx_fee_bump_inner_failed", "Operations (1)", "invoke_host_function_trapped"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("result-diff output does not contain %q:\n%s", want, stdout)
		}
	}
}

func TestResultDiffJSONAndExitCode(t *testing.T) {
	t.Parallel()

	stdout, _, err := run(t, "", "result-diff", "--json",
		fixturePath("tx_soroban.env"), fixturePath("tx_soroban.res"))
	if err != nil {
		t.Fatalf("result-diff --json error = %v", err)
	}
	var doc struct {
		Success    bool `json:"success"`
		Operations []struct {
			Status string `json:"status"`
		} `json:"operations"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("result-diff JSON is invalid: %v\n%s", err, stdout)
	}
	if !doc.Success {
		t.Errorf("result-diff JSON success = false for a successful transaction:\n%s", stdout)
	}
	for _, op := range doc.Operations {
		if op.Status != "success" {
			t.Errorf("operation status = %q, want success", op.Status)
		}
	}

	_, _, err = run(t, "", "result-diff", "--exit-code",
		fixturePath("tx_failed.env"), fixturePath("tx_failed.res"))
	if code, _ := cli.ExitCode(err); code != 1 {
		t.Errorf("result-diff --exit-code exit = %d, want 1 (err=%v)", code, err)
	}

	_, _, err = run(t, "", "result-diff", "--exit-code",
		fixturePath("tx_soroban.env"), fixturePath("tx_soroban.res"))
	if code, _ := cli.ExitCode(err); code != 0 {
		t.Errorf("result-diff --exit-code on a clean pair exit = %d, want 0 (err=%v)", code, err)
	}
}

func TestDiffJSONPatchFlag(t *testing.T) {
	t.Parallel()

	stdout, _, err := run(t, "", "diff", "--json-patch",
		fixturePath("tx_soroban.res"), fixturePath("tx_failed.res"))
	if err != nil {
		t.Fatalf("diff --json-patch error = %v", err)
	}
	var ops []struct {
		Op    string `json:"op"`
		Path  string `json:"path"`
		Value any    `json:"value"`
	}
	if err := json.Unmarshal([]byte(stdout), &ops); err != nil {
		t.Fatalf("--json-patch output is not a JSON array: %v\n%s", err, stdout)
	}
	if len(ops) == 0 {
		t.Fatal("--json-patch emitted no operations")
	}
	found := false
	for _, op := range ops {
		if op.Path == "/Result/Code" && op.Op == "replace" {
			found = true
		}
	}
	if !found {
		t.Errorf("--json-patch has no replace for /Result/Code:\n%s", stdout)
	}
}

func TestDiffUnifiedFlag(t *testing.T) {
	t.Parallel()

	stdout, _, err := run(t, "", "diff", "--unified",
		fixturePath("tx_soroban.res"), fixturePath("tx_failed.res"))
	if err != nil {
		t.Fatalf("diff --unified error = %v", err)
	}
	for _, want := range []string{"--- a/TransactionResult", "+++ b/TransactionResult", "@@ -"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("--unified output does not contain %q:\n%s", want, stdout)
		}
	}
}

func TestDiffSideBySideFlag(t *testing.T) {
	t.Parallel()

	stdout, _, err := run(t, "", "diff", "--side-by-side", "--color", "never",
		fixturePath("tx_soroban.res"), fixturePath("tx_failed.res"))
	if err != nil {
		t.Fatalf("diff --side-by-side error = %v", err)
	}
	if !strings.Contains(stdout, "|") || !strings.Contains(stdout, "FeeCharged") {
		t.Errorf("--side-by-side output is missing columns or changes:\n%s", stdout)
	}
}

func TestDiffPathFilters(t *testing.T) {
	t.Parallel()

	excluded, _, err := run(t, "", "diff", "--exclude-paths", "Result.Code",
		fixturePath("tx_soroban.res"), fixturePath("tx_failed.res"))
	if err != nil {
		t.Fatalf("diff --exclude-paths error = %v", err)
	}
	// The top-level Result.Code change must be gone, but its deeper namesake
	// (Result.InnerResultPair.Result.Result.Code) must remain.
	if strings.Contains(excluded, "TxFeeBumpInnerFailed") {
		t.Errorf("--exclude-paths Result.Code still reported the top-level code:\n%s", excluded)
	}
	if !strings.Contains(excluded, "TransactionResultCodeTxFailed") {
		t.Errorf("--exclude-paths dropped unrelated changes:\n%s", excluded)
	}

	included, _, err := run(t, "", "diff", "--include-paths", "FeeCharged",
		fixturePath("tx_soroban.res"), fixturePath("tx_failed.res"))
	if err != nil {
		t.Fatalf("diff --include-paths error = %v", err)
	}
	if !strings.Contains(included, "FeeCharged") || strings.Contains(included, "TransactionResultCodeTxFailed") {
		t.Errorf("--include-paths FeeCharged output is wrong:\n%s", included)
	}
}

func TestDiffModeValidation(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{"diff", "--json", "--unified"},
		{"diff", "--json-patch", "--side-by-side"},
		{"diff", "--stats", "--json-patch"},
	} {
		full := append(args, fixturePath("tx_soroban.res"), fixturePath("tx_failed.res"))
		if _, _, err := run(t, "", full...); err == nil {
			t.Errorf("%v error = nil, want a mode-conflict error", args)
		}
	}
}
