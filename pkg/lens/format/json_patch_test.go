package format_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"

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

// TestJSONPatchMoveForReorderedOperations checks the RFC 6902 semantics of a
// reordered operation list: each shifted element is a move whose from pointer
// names its old index and whose path names its new one, and no add/remove pair
// is emitted in its place.
func TestJSONPatchMoveForReorderedOperations(t *testing.T) {
	t.Parallel()

	bump := func(to int64) xdr.Operation {
		return xdr.Operation{Body: xdr.OperationBody{
			Type:           xdr.OperationTypeBumpSequence,
			BumpSequenceOp: &xdr.BumpSequenceOp{BumpTo: xdr.SequenceNumber(to)},
		}}
	}
	envelope := func(ops []xdr.Operation) *lens.Value {
		raw, err := xdr.TransactionEnvelope{
			Type: xdr.EnvelopeTypeEnvelopeTypeTx,
			V1: &xdr.TransactionV1Envelope{
				Tx: xdr.Transaction{
					SourceAccount: xdr.MustMuxedAddress("GAPB7YMUQ6MPQ23PTFDNBZP7HGGTZPI2DT5JJURUOIXNPLGHW2O3WYXZ"),
					Fee:           100,
					SeqNum:        1,
					Cond:          xdr.Preconditions{Type: xdr.PreconditionTypePrecondNone},
					Memo:          xdr.Memo{Type: xdr.MemoTypeMemoNone},
					Operations:    ops,
					Ext:           xdr.TransactionExt{V: 0},
				},
			},
		}.MarshalBinary()
		if err != nil {
			t.Fatalf("MarshalBinary() error = %v", err)
		}
		v, err := lens.Decode(base64.StdEncoding.EncodeToString(raw))
		if err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		return v
	}

	left := envelope([]xdr.Operation{bump(1), bump(2)})
	right := envelope([]xdr.Operation{bump(2), bump(1)})
	diff, err := lens.Diff(left, right)
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}

	var reordered int
	for _, change := range diff.Changes {
		if change.Op == lens.ChangeReordered {
			reordered++
		}
	}
	if reordered == 0 {
		t.Fatalf("diff reported no reordered changes: %+v", diff.Changes)
	}

	ops := format.NewJSONPatchFormatter().Operations(diff)
	moves := make(map[string]string, len(ops))
	for _, op := range ops {
		switch op.Op {
		case "move":
			if op.From == "" {
				t.Errorf("move operation has no from pointer: %+v", op)
			}
			if op.Value != nil {
				t.Errorf("move operation carries a value: %+v", op)
			}
			moves[op.Path] = op.From
		case "add", "remove":
			t.Errorf("reordering emitted an add/remove pair: %+v", op)
		}
	}

	want := map[string]string{
		"/V1/Tx/Operations/0": "/V1/Tx/Operations/1",
		"/V1/Tx/Operations/1": "/V1/Tx/Operations/0",
	}
	if len(moves) != len(want) {
		t.Fatalf("move operations = %v, want %v", moves, want)
	}
	for path, from := range want {
		if moves[path] != from {
			t.Errorf("move %q from = %q, want %q", path, moves[path], from)
		}
	}

	// The pointers must have the RFC 6901 shape and must round-trip through
	// the JSON encoding used on the wire.
	var buf bytes.Buffer
	if err := format.NewJSONPatchFormatter().Format(&buf, diff); err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	var decoded []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("patch is not a JSON array: %v\n%s", err, buf.String())
	}
	for _, op := range decoded {
		if op["op"] != "move" {
			continue
		}
		for _, key := range []string{"from", "path"} {
			pointer, ok := op[key].(string)
			if !ok || !strings.HasPrefix(pointer, "/") {
				t.Errorf("move %s = %v, want a non-empty JSON Pointer", key, op[key])
			}
		}
	}
}

func TestJSONPatchMoveWithoutIndexFallsBackToPath(t *testing.T) {
	t.Parallel()

	diff := &lens.DiffResult{
		LeftType:  "T",
		RightType: "T",
		Changes: []lens.Change{
			{Path: "Ops", Op: lens.ChangeReordered, Before: 1, After: 0},
		},
	}
	ops := format.NewJSONPatchFormatter().Operations(diff)
	if len(ops) != 1 || ops[0].Op != "move" {
		t.Fatalf("Operations() = %+v, want one move", ops)
	}
	if ops[0].From != "/Ops" || ops[0].Path != "/Ops" {
		t.Errorf("move = %+v, want from and path /Ops", ops[0])
	}
}

func patchPaths(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
