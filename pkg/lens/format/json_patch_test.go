package format_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens/format"
)

func TestJSONPatchFormatterShape(t *testing.T) {
	t.Parallel()

	left := loadValue(t, "tx_soroban.res")
	right := loadValue(t, "tx_failed.res")
	diff, err := lens.Diff(left, right)
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}

	var buf bytes.Buffer
	f := format.NewJSONPatchFormatter(format.WithJSONPatchSources(left, right))
	if err := f.Format(&buf, diff); err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	var ops []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &ops); err != nil {
		t.Fatalf("patch is not a JSON array: %v\n%s", err, buf.String())
	}
	if len(ops) != len(diff.Changes) {
		t.Fatalf("patch has %d operation(s), want %d", len(ops), len(diff.Changes))
	}

	byPath := make(map[string]map[string]any, len(ops))
	for _, op := range ops {
		path, ok := op["path"].(string)
		if !ok {
			t.Fatalf("operation has no string path: %v", op)
		}
		if _, ok := op["op"].(string); !ok {
			t.Fatalf("operation has no string op: %v", op)
		}
		byPath[path] = op
	}

	code, ok := byPath["/Result/Code"]
	if !ok {
		t.Fatalf("patch has no operation for /Result/Code; paths were %v", byPath)
	}
	if code["op"] != "replace" {
		t.Errorf("/Result/Code op = %v, want replace", code["op"])
	}
	if code["value"] != "TransactionResultCodeTxFeeBumpInnerFailed" {
		t.Errorf("/Result/Code value = %v, want the right-hand constant", code["value"])
	}

	// The removed Success arm must be a remove with no value.
	var removed map[string]any
	for _, op := range ops {
		if op["op"] == "remove" {
			removed = op
			break
		}
	}
	if removed == nil {
		t.Fatal("patch has no remove operation for the dropped Success arm")
	}
	if !strings.HasSuffix(removed["path"].(string), "/Success") {
		t.Errorf("remove path = %v, want it to end in /Success", removed["path"])
	}
	if _, present := removed["value"]; present {
		t.Errorf("remove operation carries a value: %v", removed)
	}
}

func TestJSONPatchFormatterWithoutSourcesUsesScalarValues(t *testing.T) {
	t.Parallel()

	left := loadValue(t, "tx_soroban.res")
	right := loadValue(t, "tx_failed.res")
	diff, err := lens.Diff(left, right)
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}

	f := format.NewJSONPatchFormatter()
	ops := f.Operations(diff)

	var feeCharged *format.JSONPatchOperation
	for i := range ops {
		if ops[i].Path == "/FeeCharged" {
			feeCharged = &ops[i]
			break
		}
	}
	if feeCharged == nil {
		t.Fatalf("no /FeeCharged operation; ops = %v", ops)
	}
	if feeCharged.Op != "replace" {
		t.Errorf("/FeeCharged op = %q, want replace", feeCharged.Op)
	}
	if feeCharged.Value == nil {
		t.Error("/FeeCharged operation has no scalar value")
	}
}

func TestJSONPatchFormatterCompactAndDeterministic(t *testing.T) {
	t.Parallel()

	left := loadValue(t, "tx_soroban.res")
	right := loadValue(t, "tx_failed.res")
	diff, err := lens.Diff(left, right)
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}

	var first, second bytes.Buffer
	f := format.NewJSONPatchFormatter(format.WithJSONPatchIndent(""), format.WithJSONPatchSources(left, right))
	if err := f.Format(&first, diff); err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	if err := f.Format(&second, diff); err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	if first.String() != second.String() {
		t.Error("JSON Patch output is not deterministic")
	}
	if strings.Count(strings.TrimRight(first.String(), "\n"), "\n") != 0 {
		t.Errorf("compact patch spans multiple lines:\n%s", first.String())
	}
}

func TestJSONPatchFormatterEqualDiffIsEmptyArray(t *testing.T) {
	t.Parallel()

	v := loadValue(t, "tx_success.env")
	diff, err := lens.Diff(v, v)
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}

	var buf bytes.Buffer
	if err := format.NewJSONPatchFormatter().Format(&buf, diff); err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	if strings.TrimSpace(buf.String()) != "[]" {
		t.Errorf("equal diff patch = %q, want []", strings.TrimSpace(buf.String()))
	}
}

func TestJSONPatchFormatterRejectsNil(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := format.NewJSONPatchFormatter().Format(&buf, nil); err == nil {
		t.Error("Format(nil) error = nil, want an error")
	}
	if ops := format.NewJSONPatchFormatter().Operations(nil); len(ops) != 0 {
		t.Errorf("Operations(nil) = %v, want empty", ops)
	}
}

func TestJSONPointerEncoding(t *testing.T) {
	t.Parallel()

	left := loadValue(t, "tx_soroban.res")
	right := loadValue(t, "tx_failed.res")
	diff, err := lens.Diff(left, right)
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}

	want := []string{
		"/Result/Code",
		"/Result/InnerResultPair/Result/Result/Results/0/Tr/InvokeHostFunctionResult/Code",
		"/Result/InnerResultPair/TransactionHash",
	}

	ops := format.NewJSONPatchFormatter().Operations(diff)
	seen := make(map[string]bool, len(ops))
	for _, op := range ops {
		seen[op.Path] = true
	}
	for _, pointer := range want {
		if !seen[pointer] {
			t.Errorf("no operation with pointer %q; pointers were %v", pointer, patchPaths(seen))
		}
	}
}

// TestJSONPatchPointerEscaping exercises the "~" and "/" escaping rules.
func TestJSONPatchPointerEscaping(t *testing.T) {
	t.Parallel()

	diff := &lens.DiffResult{
		LeftType:  "T",
		RightType: "T",
		Changes: []lens.Change{
			{Path: "A~B/C.D", Op: lens.ChangeAdded, After: "x"},
		},
	}

	ops := format.NewJSONPatchFormatter().Operations(diff)
	if len(ops) != 1 {
		t.Fatalf("Operations() = %v, want one", ops)
	}
	if ops[0].Path != "/A~0B~1C/D" {
		t.Errorf("escaped pointer = %q, want /A~0B~1C/D", ops[0].Path)
	}
	if ops[0].Op != "add" {
		t.Errorf("op = %q, want add", ops[0].Op)
	}
}

func patchPaths(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
