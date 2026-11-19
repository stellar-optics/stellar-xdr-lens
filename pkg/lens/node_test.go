package lens

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"
)

// nodeTestFixture decodes a checked-in fixture for the node walker tests.
func nodeTestFixture(tb testing.TB, name, typeName string) *Value {
	tb.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", name+".txt"))
	if err != nil {
		tb.Fatalf("reading fixture %s: %v", name, err)
	}
	v, err := DecodeAs(strings.TrimSpace(string(raw)), typeName)
	if err != nil {
		tb.Fatalf("DecodeAs(%s) error = %v", name, err)
	}
	return v
}

func TestBuildNodeRendersByteSequences(t *testing.T) {
	t.Parallel()

	type namedByte uint8
	large := make([]byte, maxPooledBytes+1)
	for i := range large {
		large[i] = byte(i)
	}

	tests := []struct {
		name      string
		value     any
		wantValue string
		wantNote  string
	}{
		{name: "printable slice", value: []byte("USDC"), wantValue: "55534443", wantNote: `"USDC"`},
		{name: "trailing padding is trimmed from the note", value: [4]byte{'A', 'B', 0, 0}, wantValue: "41420000", wantNote: `"AB"`},
		{name: "array is hex encoded", value: [3]byte{0xde, 0xad, 0xbe}, wantValue: "deadbe"},
		{name: "named byte array", value: [2]namedByte{0x41, 0x42}, wantValue: "4142", wantNote: `"AB"`},
		{name: "empty slice", value: []byte{}, wantValue: ""},
		{name: "all padding has no note", value: [2]byte{}, wantValue: "0000"},
		{name: "control byte has no note", value: []byte{'A', 0x1f}, wantValue: "411f"},
		{name: "high byte has no note", value: []byte{'A', 0x7f}, wantValue: "417f"},
		{name: "interior nul has no note", value: []byte{'A', 0, 'B'}, wantValue: "410042"},
		{name: "quote is escaped in the note", value: []byte(`a"b`), wantValue: "612262", wantNote: `"a\"b"`},
		{name: "longest printable note", value: []byte(strings.Repeat("a", 64)), wantValue: strings.Repeat("61", 64), wantNote: strconv.Quote(strings.Repeat("a", 64))},
		{name: "too long for a note", value: []byte(strings.Repeat("a", 65)), wantValue: strings.Repeat("61", 65)},
		{name: "larger than the pooled buffer limit", value: large, wantValue: hex.EncodeToString(large)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			n := nodeFor("Field", tc.value)
			if n == nil {
				t.Fatal("nodeFor() = nil")
			}
			if n.Kind != KindScalar || n.Name != "Field" {
				t.Errorf("node = %v %q, want scalar named Field", n.Kind, n.Name)
			}
			if n.Value != tc.wantValue {
				t.Errorf("Value = %v, want %q", n.Value, tc.wantValue)
			}
			if n.Note != tc.wantNote {
				t.Errorf("Note = %q, want %q", n.Note, tc.wantNote)
			}
		})
	}
}

func TestBuildNodePrunesNilByteSlice(t *testing.T) {
	t.Parallel()

	var nilBytes []byte
	if n := buildNode("Field", reflect.ValueOf(nilBytes), 0); n != nil {
		t.Errorf("buildNode(nil slice) = %+v, want nil", n)
	}
}

// TestBuildNodeChildrenDoNotAliasScratch proves a finished tree owns its child
// slices. If a node kept the pooled buffer, the next walk would overwrite it.
func TestBuildNodeChildrenDoNotAliasScratch(t *testing.T) {
	t.Parallel()

	type record struct {
		A, B, C string
		Nested  struct{ D, E int }
		Absent  *string
	}

	first := nodeFor("", record{A: "a", B: "b", C: "c"})
	var want strings.Builder
	dumpNode(first, &want)

	for range 50 {
		nodeFor("", record{A: "x", B: "y", C: "z"})
	}

	var got strings.Builder
	dumpNode(first, &got)
	if got.String() != want.String() {
		t.Errorf("tree changed after later walks:\n got %s\nwant %s", got.String(), want.String())
	}
	if len(first.Children) != 4 || cap(first.Children) != 4 {
		t.Errorf("Children len/cap = %d/%d, want an exact 4/4", len(first.Children), cap(first.Children))
	}
	// Appending must reallocate rather than write into a shared backing array.
	grown := append(first.Children, &Node{Name: "extra"})
	if &grown[0] == &first.Children[0] {
		t.Error("appending to Children reused the backing array")
	}
}

func TestBuildNodeEmptyContainersHaveNilChildren(t *testing.T) {
	t.Parallel()

	type onlyAbsent struct{ Absent *string }

	tests := []struct {
		name  string
		value any
		kind  Kind
	}{
		{name: "struct with only pruned fields", value: onlyAbsent{}, kind: KindStruct},
		{name: "empty list", value: []string{}, kind: KindList},
		{name: "empty map", value: map[string]int{}, kind: KindStruct},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			n := nodeFor("", tc.value)
			if n == nil || n.Kind != tc.kind {
				t.Fatalf("nodeFor() = %+v, want kind %v", n, tc.kind)
			}
			if n.Children != nil {
				t.Errorf("Children = %#v, want nil", n.Children)
			}
		})
	}
}

func TestBuildNodeListKeepsPlaceholderForNilElement(t *testing.T) {
	t.Parallel()

	text := "kept"
	n := nodeFor("", []*string{&text, nil})
	if len(n.Children) != 2 {
		t.Fatalf("Children = %d, want 2", len(n.Children))
	}
	if n.Children[0].Value != "kept" || n.Children[1].Value != nil {
		t.Errorf("elements = %v, %v; want kept, nil", n.Children[0].Value, n.Children[1].Value)
	}
}

func TestBuildNodeMapPrunesNilValues(t *testing.T) {
	t.Parallel()

	text := "kept"
	n := nodeFor("", map[string]*string{"a": &text, "b": nil})
	if len(n.Children) != 1 || n.Children[0].Name != "a" {
		t.Errorf("Children = %+v, want only key a", n.Children)
	}
}

func TestBuildNodeScalarKindsAndDepthLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value any
		want  any
	}{
		{name: "bool", value: true, want: true},
		{name: "int", value: int32(-3), want: int64(-3)},
		{name: "uint", value: uint16(3), want: uint64(3)},
		{name: "float", value: 1.5, want: 1.5},
		{name: "string", value: "text", want: "text"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := nodeFor("", tc.value); got.Value != tc.want {
				t.Errorf("Value = %#v, want %#v", got.Value, tc.want)
			}
		})
	}

	if nodeFor("", nil) != nil {
		t.Error("nodeFor(nil) is not nil")
	}
	if got := nodeFor("", make(chan int)); got == nil || got.Kind != KindScalar {
		t.Errorf("unsupported kind = %+v, want a scalar fallback", got)
	}
	over := buildNode("deep", reflect.ValueOf("x"), maxDepth+1)
	if over.Value != "<max depth exceeded>" {
		t.Errorf("Value = %v, want the depth marker", over.Value)
	}
}

// TestReleaseChildScratchClearsPointers inspects a buffer after it has gone back
// to the pool, so it must not run alongside the parallel tests that reuse it.
func TestReleaseChildScratchClearsPointers(t *testing.T) {
	scratch := getChildScratch()
	children := append(*scratch, &Node{Name: "a"}, &Node{Name: "b"})
	backing := children[:2]

	out := releaseChildScratch(scratch, children)

	if len(out) != 2 || out[0].Name != "a" || out[1].Name != "b" {
		t.Fatalf("released children = %+v, want a, b", out)
	}
	if backing[0] != nil || backing[1] != nil {
		t.Error("scratch still references nodes, which would keep whole trees alive")
	}
}

func TestReleaseChildScratchEmptyReturnsNil(t *testing.T) {
	t.Parallel()

	scratch := getChildScratch()
	if out := releaseChildScratch(scratch, *scratch); out != nil {
		t.Errorf("released children = %#v, want nil", out)
	}
}

func TestReleaseChildScratchDropsOversizedBuffers(t *testing.T) {
	t.Parallel()

	oversized := make([]*Node, 0, maxPooledChildren+1)
	scratch := &oversized
	releaseChildScratch(scratch, oversized)

	for range 64 {
		if got := getChildScratch(); got == scratch {
			t.Fatal("an oversized child buffer was returned to the pool")
		}
	}
}

func TestReleaseByteScratchDropsOversizedBuffers(t *testing.T) {
	t.Parallel()

	oversized := make([]byte, 0, maxPooledBytes+1)
	scratch := &oversized
	releaseByteScratch(scratch, oversized)

	for range 64 {
		if got := getByteScratch(); got == scratch {
			t.Fatal("an oversized byte buffer was returned to the pool")
		}
	}
}

// TestBuildNodeConcurrentWalksAreIndependent shares the pools across goroutines
// walking different fixtures. Run with -race it also proves the buffers are
// never used by two walks at once.
func TestBuildNodeConcurrentWalksAreIndependent(t *testing.T) {
	t.Parallel()

	values := []*Value{
		nodeTestFixture(t, "tx_success.env", "TransactionEnvelope"),
		nodeTestFixture(t, "tx_soroban.env", "TransactionEnvelope"),
		nodeTestFixture(t, "ledger_header", "LedgerHeader"),
		nodeTestFixture(t, "overlay_stellar_message", "StellarMessage"),
	}
	want := make([]string, len(values))
	for i, v := range values {
		var sb strings.Builder
		dumpNode(v.Node, &sb)
		want[i] = sb.String()
	}

	var wg sync.WaitGroup
	for worker := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for round := range 25 {
				i := (worker + round) % len(values)
				var sb strings.Builder
				dumpNode(nodeFor("", values[i].Raw), &sb)
				if sb.String() != want[i] {
					t.Errorf("worker %d round %d: tree for value %d differs", worker, round, i)
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestKindString(t *testing.T) {
	t.Parallel()

	for kind, want := range map[Kind]string{KindScalar: "scalar", KindStruct: "struct", KindList: "list", Kind(9): "unknown"} {
		if got := kind.String(); got != want {
			t.Errorf("Kind(%d).String() = %q, want %q", kind, got, want)
		}
	}
}

func TestNodeAccessors(t *testing.T) {
	t.Parallel()

	tree := &Node{Kind: KindStruct, TypeName: "Root", Children: []*Node{
		{Name: "items", Kind: KindList, Children: []*Node{
			{Kind: KindStruct, Children: []*Node{{Name: "code", Kind: KindScalar, Value: "ok"}}},
		}},
		{Name: "noted", Kind: KindScalar, Value: int64(5), Note: "five"},
		{Name: "noteOnly", Kind: KindScalar, Note: "just a note"},
	}}

	if got := tree.Find("items[0].code"); got == nil || got.Value != "ok" {
		t.Errorf("Find(items[0].code) = %+v, want ok", got)
	}
	for _, path := range []string{"items[3]", "missing[0]", "missing.code", "items[0].missing.deeper"} {
		if got := tree.Find(path); got != nil {
			t.Errorf("Find(%q) = %+v, want nil", path, got)
		}
	}
	if got := tree.Find(""); got != tree {
		t.Errorf("Find(\"\") = %+v, want the root", got)
	}

	var absent *Node
	if !absent.IsZero() || absent.Child("x") != nil || absent.String() != "<nil>" {
		t.Error("nil node accessors did not degrade safely")
	}
	if !(&Node{Kind: KindScalar}).IsZero() || tree.IsZero() {
		t.Error("IsZero misclassified a node")
	}

	tests := []struct {
		name string
		node *Node
		want string
	}{
		{name: "scalar with note", node: tree.Child("noted"), want: "5 (five)"},
		{name: "scalar without value", node: tree.Child("noteOnly"), want: "just a note"},
		{name: "plain scalar", node: &Node{Kind: KindScalar, Value: "v"}, want: "v"},
		{name: "list", node: tree.Child("items"), want: "[1 item(s)]"},
		{name: "named struct", node: tree, want: "Root"},
		{name: "anonymous struct", node: &Node{Kind: KindStruct}, want: "{...}"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.node.String(); got != tc.want {
				t.Errorf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSplitPathToleratesMalformedSegments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path string
		want []pathSeg
	}{
		{path: "a.b", want: []pathSeg{{name: "a", index: -1}, {name: "b", index: -1}}},
		{path: "a[2]", want: []pathSeg{{name: "a", index: 2}}},
		{path: "a[1][2]", want: []pathSeg{{name: "a", index: 1}, {name: "", index: 2}}},
		{path: "a[1]b", want: []pathSeg{{name: "a", index: 1}, {name: "b", index: -1}}},
		{path: "a[1", want: []pathSeg{{name: "a[1", index: -1}}},
		{path: "a[x]", want: []pathSeg{{name: "a", index: -1}}},
		{path: "..a", want: []pathSeg{{name: "a", index: -1}}},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			if got := splitPath(tc.path); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("splitPath(%q) = %+v, want %+v", tc.path, got, tc.want)
			}
		})
	}
}

func TestFilterByAccount(t *testing.T) {
	t.Parallel()

	const target = "GTARGET"
	tree := &Node{Name: "root", Kind: KindStruct, Children: []*Node{
		{Name: "match", Kind: KindScalar, TypeName: "AccountId", Value: target},
		{Name: "noted", Kind: KindScalar, TypeName: "MuxedAccount", Value: "raw", Note: target},
		{Name: "other", Kind: KindScalar, TypeName: "AccountId", Value: "GOTHER"},
		{Name: "sameTextWrongType", Kind: KindScalar, TypeName: "String32", Value: target},
		{Name: "number", Kind: KindScalar, TypeName: "AccountId", Value: int64(1)},
		{Name: "nested", Kind: KindStruct, Children: []*Node{
			{Name: "deep", Kind: KindScalar, TypeName: "AccountId", Value: target},
		}},
	}}

	got := FilterByAccount(tree, target)
	if got == nil {
		t.Fatal("FilterByAccount() = nil")
	}
	var names []string
	for _, c := range got.Children {
		names = append(names, c.Name)
	}
	if want := []string{"match", "noted", "nested"}; !reflect.DeepEqual(names, want) {
		t.Errorf("kept %v, want %v", names, want)
	}
	if got == tree {
		t.Error("FilterByAccount() returned the input instead of a copy")
	}

	if FilterByAccount(tree, "GNOBODY") != nil {
		t.Error("an unmatched account should filter the whole tree away")
	}
	if FilterByAccount(tree, "") != tree {
		t.Error("an empty target should return the tree unchanged")
	}
	if FilterByAccount(nil, target) != nil {
		t.Error("a nil tree should stay nil")
	}
	if filterByAccountRecursive(nil, target, nil) != nil || nodeOrDescendantMatchesAccount(nil, target) {
		t.Error("nil nodes should never match")
	}
	if nodeValueEquals(nil, target) {
		t.Error("a nil value should never equal the target")
	}
}

func opTestNode(opType string) *Node {
	return &Node{Kind: KindStruct, TypeName: "Operation", Children: []*Node{
		{Name: "Body", Kind: KindStruct, Children: []*Node{
			{Name: "Type", Kind: KindScalar, Value: opType},
		}},
	}}
}

func TestFilterByOpType(t *testing.T) {
	t.Parallel()

	payment := opTestNode("OperationTypePayment")
	invoke := opTestNode("OperationTypeInvokeHostFunction")
	tree := &Node{Name: "root", Kind: KindStruct, Children: []*Node{
		{Name: "Fee", Kind: KindScalar, Value: int64(100)},
		{Name: "Operations", Kind: KindList, Children: []*Node{payment, invoke}},
	}}

	tests := []struct {
		name   string
		filter string
		want   *Node
	}{
		{name: "snake case", filter: "invoke_host_function", want: invoke},
		{name: "camel case", filter: "InvokeHostFunction", want: invoke},
		{name: "kebab case", filter: "invoke-host-function", want: invoke},
		{name: "spaced", filter: "invoke host function", want: invoke},
		{name: "simple name", filter: "payment", want: payment},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := FilterByOpType(tree, tc.filter)
			ops := got.Find("Operations")
			if ops == nil || len(ops.Children) != 1 || ops.Children[0] != tc.want {
				t.Errorf("FilterByOpType(%q) kept %+v", tc.filter, ops)
			}
		})
	}

	if got := FilterByOpType(tree, "bump_sequence"); got.Find("Operations") != nil {
		t.Errorf("an unmatched type kept the operation list: %+v", got)
	}
	if FilterByOpType(tree, "") != tree {
		t.Error("an empty filter should return the tree unchanged")
	}
	if FilterByOpType(nil, "payment") != nil {
		t.Error("a nil tree should stay nil")
	}
	if filterByOpTypeRecursive(nil, "payment") != nil {
		t.Error("a nil node should stay nil")
	}
	if got := filterByOpTypeRecursive(payment, "payment"); got != payment {
		t.Errorf("a matching bare operation = %+v, want it kept", got)
	}
	if filterByOpTypeRecursive(payment, "bump_sequence") != nil {
		t.Error("a non-matching bare operation should be dropped")
	}
}

func TestOpMatchesTypeRejectsIncompleteOperations(t *testing.T) {
	t.Parallel()

	nilType := opTestNode("x")
	nilType.Child("Body").Child("Type").Value = nil

	tests := []struct {
		name string
		node *Node
	}{
		{name: "nil", node: nil},
		{name: "scalar", node: &Node{Kind: KindScalar}},
		{name: "no body", node: &Node{Kind: KindStruct}},
		{name: "no type", node: &Node{Kind: KindStruct, Children: []*Node{{Name: "Body", Kind: KindStruct}}}},
		{name: "nil type value", node: nilType},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if opMatchesType(tc.node, "payment") {
				t.Error("opMatchesType() = true, want false")
			}
		})
	}
}

// panicStringer panics from String, like a generated XDR helper reading an
// absent union arm.
type panicStringer struct{}

func (panicStringer) String() string { panic("absent union arm") }

func TestSafeStringRecoversFromPanics(t *testing.T) {
	t.Parallel()

	if got := safeString(panicStringer{}); got != "" {
		t.Errorf("safeString() = %q, want empty", got)
	}
	if got := safeString(xdr.MemoTypeMemoText); got != "MemoTypeMemoText" {
		t.Errorf("safeString() = %q, want MemoTypeMemoText", got)
	}
}

var benchmarkBuiltNode *Node

// BenchmarkBuildNode measures the tree walk alone, on real fixtures, so the
// allocations it reports are the walker's rather than the decoder's.
func BenchmarkBuildNode(b *testing.B) {
	fixtures := []struct{ name, fixture, typeName string }{
		{name: "TransactionEnvelope", fixture: "tx_success.env", typeName: "TransactionEnvelope"},
		{name: "SorobanEnvelope", fixture: "tx_soroban.env", typeName: "TransactionEnvelope"},
		{name: "LedgerHeader", fixture: "ledger_header", typeName: "LedgerHeader"},
		{name: "StellarMessage", fixture: "overlay_stellar_message", typeName: "StellarMessage"},
	}
	for _, fx := range fixtures {
		value := nodeTestFixture(b, fx.fixture, fx.typeName)
		b.Run(fx.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				benchmarkBuiltNode = nodeFor("", value.Raw)
			}
		})
	}
}
