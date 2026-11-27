package lens_test

import (
	"strconv"
	"testing"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

// Diff benchmarks for large and deeply nested trees.
//
// The structural differ is recursive and allocates one Change per difference.
// Those two properties are what make it fast on ordinary transactions, and
// also what could make it expensive on a ledger-close-sized value with a deep
// union nest. The benchmarks here cover both ends: canonical fixtures that
// exercise real protocol shapes, and synthetic trees that isolate depth,
// breadth and alignment cost. Everything is local and deterministic; no
// benchmark touches the network.

// ledgerCloseDiffFixtures are the largest checked-in protocol payloads. They
// exercise transaction envelopes, Soroban resource data, generalized
// transaction sets and history headers, which together cover the deep union
// nesting a ledger close carries.
var ledgerCloseDiffFixtures = []struct {
	name     string
	fixture  string
	typeName string
}{
	{name: "SorobanEnvelope", fixture: "tx_soroban.env", typeName: "TransactionEnvelope"},
	{name: "FeeBumpEnvelope", fixture: "tx_failed.env", typeName: "TransactionEnvelope"},
	{name: "GeneralizedTxSet", fixture: "generalized_tx_set", typeName: "GeneralizedTransactionSet"},
	{name: "LedgerHeaderHistory", fixture: "ledger_header_history", typeName: "LedgerHeaderHistoryEntry"},
	{name: "TransactionResult", fixture: "tx_failed.res", typeName: "TransactionResult"},
}

// nestedBenchmarkValue builds a struct tree that is depth levels deep with
// breadth scalar siblings at every level. It is deliberately shaped like a
// union nest: each level has a named struct child and a scalar child, so the
// differ walks both a chain and sibling fields.
func nestedBenchmarkValue(depth, breadth int) *lens.Value {
	root := &lens.Node{Kind: lens.KindStruct, TypeName: "BenchmarkTree"}
	current := root
	for level := 0; level < depth; level++ {
		levelNode := &lens.Node{
			Name:     "Level" + strconv.Itoa(level),
			Kind:     lens.KindStruct,
			TypeName: "BenchmarkLevel",
		}
		for field := 0; field < breadth; field++ {
			levelNode.Children = append(levelNode.Children, &lens.Node{
				Name:     "Field" + strconv.Itoa(field),
				Kind:     lens.KindScalar,
				TypeName: "Uint32",
				Value:    uint64(level*1000 + field),
			})
		}
		current.Children = append(current.Children, levelNode)
		current = levelNode
	}
	current.Children = append(current.Children, &lens.Node{
		Name:     "Leaf",
		Kind:     lens.KindScalar,
		TypeName: "Int64",
		Value:    int64(42),
	})
	return &lens.Value{Type: "BenchmarkTree", Node: root}
}

// cloneBenchmarkNode deep-copies a node tree so a benchmark can mutate one
// side without disturbing the other or the shared fixture value.
func cloneBenchmarkNode(n *lens.Node) *lens.Node {
	if n == nil {
		return nil
	}
	clone := *n
	if len(n.Children) > 0 {
		clone.Children = make([]*lens.Node, len(n.Children))
		for i, child := range n.Children {
			clone.Children[i] = cloneBenchmarkNode(child)
		}
	}
	return &clone
}

// mutateFirstScalar changes the first scalar it finds, returning true when it
// made a change. It is used to produce a one-field difference in an otherwise
// identical large tree.
func mutateFirstScalar(n *lens.Node, value any) bool {
	if n == nil {
		return false
	}
	if n.Kind == lens.KindScalar {
		n.Value = value
		n.Note = ""
		return true
	}
	for _, child := range n.Children {
		if mutateFirstScalar(child, value) {
			return true
		}
	}
	return false
}

// mutateDeepestScalar changes the scalar with the greatest depth, which is the
// worst case for the recursive walk: the change is found only after descending
// the entire tree.
func mutateDeepestScalar(n *lens.Node, value any) bool {
	leaf := deepestScalar(n)
	if leaf == nil {
		return false
	}
	leaf.Value = value
	leaf.Note = ""
	return true
}

// deepestScalar returns the deepest scalar in the tree, breaking ties by last
// appearance.
func deepestScalar(n *lens.Node) *lens.Node {
	var best *lens.Node
	bestDepth := -1
	var walk func(*lens.Node, int)
	walk = func(node *lens.Node, depth int) {
		if node == nil {
			return
		}
		if node.Kind == lens.KindScalar {
			if depth >= bestDepth {
				best, bestDepth = node, depth
			}
			return
		}
		for _, child := range node.Children {
			walk(child, depth+1)
		}
	}
	walk(n, 0)
	return best
}

func TestNestedBenchmarkValueShape(t *testing.T) {
	t.Parallel()

	v := nestedBenchmarkValue(4, 3)
	if v.Node == nil {
		t.Fatal("nestedBenchmarkValue() returned a nil node")
	}
	lead := v.Node.Child("Level0")
	if lead == nil {
		t.Fatal("Level0 is missing")
	}
	if got := len(lead.Children); got != 4 {
		t.Errorf("Level0 has %d children, want 3 fields plus the next level", got)
	}
	leaf := v.Node.Find("Level0.Level1.Level2.Level3.Leaf")
	if leaf == nil {
		t.Fatal("deep Leaf is missing")
	}
	if leaf.Value != int64(42) {
		t.Errorf("Leaf = %v, want 42", leaf.Value)
	}
}

func TestDiffDeepNestedTreeReportsDeepChange(t *testing.T) {
	t.Parallel()

	left := nestedBenchmarkValue(32, 2)
	right := nestedBenchmarkValue(32, 2)
	mutateDeepestScalar(right.Node, int64(99))

	result, err := lens.Diff(left, right)
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}
	if len(result.Changes) != 1 {
		t.Fatalf("got %d change(s), want 1: %v", len(result.Changes), result.Changes)
	}
	if result.Changes[0].Path != "Level0.Level1.Level2.Level3.Level4.Level5.Level6.Level7.Level8.Level9.Level10.Level11.Level12.Level13.Level14.Level15.Level16.Level17.Level18.Level19.Level20.Level21.Level22.Level23.Level24.Level25.Level26.Level27.Level28.Level29.Level30.Level31.Leaf" {
		t.Errorf("change path = %q, want the deepest Leaf", result.Changes[0].Path)
	}
}

func TestDiffDeepNestedTreeIsDeterministic(t *testing.T) {
	t.Parallel()

	build := func() (*lens.Value, *lens.Value) {
		left := nestedBenchmarkValue(20, 3)
		right := nestedBenchmarkValue(20, 3)
		mutateFirstScalar(right.Node, uint64(999))
		return left, right
	}

	firstLeft, firstRight := build()
	first, err := lens.Diff(firstLeft, firstRight)
	if err != nil {
		t.Fatalf("first Diff() error = %v", err)
	}
	secondLeft, secondRight := build()
	second, err := lens.Diff(secondLeft, secondRight)
	if err != nil {
		t.Fatalf("second Diff() error = %v", err)
	}
	if len(first.Changes) != len(second.Changes) {
		t.Fatalf("change counts differ: %d vs %d", len(first.Changes), len(second.Changes))
	}
	for i := range first.Changes {
		if first.Changes[i].Path != second.Changes[i].Path {
			t.Errorf("change %d path = %q, want %q", i, first.Changes[i].Path, second.Changes[i].Path)
		}
	}
}

func TestDiffLedgerCloseBenchmarkFixturesDecode(t *testing.T) {
	t.Parallel()

	for _, fixture := range ledgerCloseDiffFixtures {
		t.Run(fixture.name, func(t *testing.T) {
			t.Parallel()
			payload := loadFixture(t, fixture.fixture)
			value, err := lens.DecodeAs(payload, fixture.typeName)
			if err != nil {
				t.Fatalf("DecodeAs(%s) error = %v", fixture.typeName, err)
			}
			if value.Node == nil {
				t.Fatal("decoded value has a nil node")
			}
			if _, err := lens.Diff(value, value); err != nil {
				t.Fatalf("Diff() error = %v", err)
			}
		})
	}
}

func TestMutateFirstScalar(t *testing.T) {
	t.Parallel()

	v := nestedBenchmarkValue(2, 1)
	if !mutateFirstScalar(v.Node, uint64(7)) {
		t.Fatal("mutateFirstScalar() found no scalar")
	}
	field := v.Node.Find("Level0.Field0")
	if field == nil {
		t.Fatal("Field0 is missing")
	}
	if field.Value != uint64(7) {
		t.Errorf("Field0 = %v, want 7", field.Value)
	}
}

func TestDiffWideListBenchmarkValue(t *testing.T) {
	t.Parallel()

	left := wideListValue(100)
	right := wideListValue(100)
	right.Node.Children[99].Value = int64(-1)

	result, err := lens.Diff(left, right)
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}
	if len(result.Changes) != 1 {
		t.Fatalf("got %d change(s), want 1", len(result.Changes))
	}
	if result.Changes[0].Path != "[99]" {
		t.Errorf("path = %q, want [99]", result.Changes[0].Path)
	}
}

func wideListValue(size int) *lens.Value {
	root := &lens.Node{Kind: lens.KindList, TypeName: "BenchmarkList"}
	root.Children = make([]*lens.Node, size)
	for i := range size {
		root.Children[i] = &lens.Node{Kind: lens.KindScalar, TypeName: "Uint32", Value: uint64(i)}
	}
	return &lens.Value{Type: "BenchmarkList", Node: root}
}

// BenchmarkDiffDeeplyNestedTrees isolates recursion cost at increasing depths.
func BenchmarkDiffDeeplyNestedTrees(b *testing.B) {
	for _, depth := range []int{8, 32, 128} {
		b.Run("Depth"+strconv.Itoa(depth), func(b *testing.B) {
			left := nestedBenchmarkValue(depth, 4)
			right := nestedBenchmarkValue(depth, 4)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := lens.Diff(left, right); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkDiffDeepestChange measures the worst case for the recursive walk:
// the only difference is the deepest leaf in a large tree.
func BenchmarkDiffDeepestChange(b *testing.B) {
	for _, depth := range []int{8, 32, 128} {
		b.Run("Depth"+strconv.Itoa(depth), func(b *testing.B) {
			left := nestedBenchmarkValue(depth, 4)
			right := nestedBenchmarkValue(depth, 4)
			mutateDeepestScalar(right.Node, int64(-1))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				result, err := lens.Diff(left, right)
				if err != nil {
					b.Fatal(err)
				}
				if len(result.Changes) != 1 {
					b.Fatalf("got %d changes, want 1", len(result.Changes))
				}
			}
		})
	}
}

// BenchmarkDiffWideLists measures the cost of comparing long sequences, both
// positionally and with the opt-in LCS aligner.
func BenchmarkDiffWideLists(b *testing.B) {
	for _, size := range []int{100, 1000} {
		left := wideListValue(size)
		right := wideListValue(size)
		right.Node.Children[size-1].Value = int64(-1)

		b.Run("Positional/Size"+strconv.Itoa(size), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := lens.Diff(left, right); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run("Aligned/Size"+strconv.Itoa(size), func(b *testing.B) {
			options := lens.DiffOptions{AlignLists: true}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := lens.DiffWithOptions(left, right, options); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkDiffLedgerCloseEnvelopes measures real protocol values, which mix
// deep unions with long vectors. The one-change case mutates a copy so the
// differ cannot short-circuit on equality.
func BenchmarkDiffLedgerCloseEnvelopes(b *testing.B) {
	for _, fixture := range ledgerCloseDiffFixtures {
		payload, err := readFixture(fixture.fixture)
		if err != nil {
			b.Fatalf("reading fixture %s: %v", fixture.fixture, err)
		}
		left, err := lens.DecodeAs(payload, fixture.typeName)
		if err != nil {
			b.Fatalf("decoding fixture %s: %v", fixture.fixture, err)
		}
		right := &lens.Value{Type: left.Type, Node: cloneBenchmarkNode(left.Node)}

		b.Run(fixture.name+"/Equal", func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(payload)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := lens.Diff(left, right); err != nil {
					b.Fatal(err)
				}
			}
		})

		changed := &lens.Value{Type: left.Type, Node: cloneBenchmarkNode(left.Node)}
		mutateFirstScalar(changed.Node, "benchmark-change")
		b.Run(fixture.name+"/OneChange", func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(payload)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := lens.Diff(left, changed); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkDiffRedacted compares the cost of the redaction prepass against the
// plain walk on a real transaction envelope.
func BenchmarkDiffRedacted(b *testing.B) {
	payload, err := readFixture("tx_soroban.env")
	if err != nil {
		b.Fatal(err)
	}
	value, err := lens.DecodeAs(payload, "TransactionEnvelope")
	if err != nil {
		b.Fatal(err)
	}
	options := lens.DiffOptions{Redaction: lens.RedactHashesAndSignatures()}

	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := lens.DiffWithOptions(value, value, options); err != nil {
			b.Fatal(err)
		}
	}
}

// longOperationArrayValue builds a transaction-shaped tree whose operation
// array has size elements. Every element is identical except the scalar at
// mutateAt, which models a large block where one operation changed. The name
// of the list selects which diff path is exercised: "Operations" uses the
// operation-specific matcher, while any other name goes through the generic
// LCS aligner.
func longOperationArrayValue(name string, size, mutateAt int) *lens.Value {
	root := &lens.Node{Kind: lens.KindStruct, TypeName: "Transaction"}
	ops := &lens.Node{Name: name, Kind: lens.KindList, TypeName: "Operation"}
	ops.Children = make([]*lens.Node, size)
	for i := range size {
		amount := uint64(i + 1)
		if i == mutateAt {
			amount = 1 << 40
		}
		ops.Children[i] = &lens.Node{
			Kind:     lens.KindStruct,
			TypeName: "Operation",
			Children: []*lens.Node{
				{
					Name:     "Body",
					Kind:     lens.KindStruct,
					TypeName: "OperationBody",
					Children: []*lens.Node{
						{Name: "Type", Kind: lens.KindScalar, TypeName: "OperationType", Value: "OperationTypePayment"},
						{
							Name:     "PaymentOp",
							Kind:     lens.KindStruct,
							TypeName: "PaymentOp",
							Children: []*lens.Node{
								{Name: "Destination", Kind: lens.KindScalar, TypeName: "MuxedAccount", Value: "GAPB...WYXZ"},
								{Name: "Asset", Kind: lens.KindScalar, TypeName: "Asset", Value: "native"},
								{Name: "Amount", Kind: lens.KindScalar, TypeName: "Int64", Value: amount},
							},
						},
					},
				},
			},
		}
	}
	root.Children = []*lens.Node{ops}
	return &lens.Value{Type: "Transaction", Node: root}
}

// BenchmarkLCSDiffLongArrays measures the hash fast path on 500-element
// arrays, which is the shape that made the O(N*M) LCS table expensive.
//
// Before the fast path every LCS cell compared complete subtrees, so a
// 500-element array cost roughly 250,000 full operation walks. With memoized
// digests a cell is a fixed-size hash comparison and an unchanged subtree is
// skipped entirely, leaving only the changed element to walk in detail.
func BenchmarkLCSDiffLongArrays(b *testing.B) {
	const size = 500
	options := lens.DiffOptions{AlignLists: true}

	cases := []struct {
		name string
		list string
	}{
		{name: "GenericLCS", list: "History"},
		{name: "OperationArray", list: "Operations"},
	}

	for _, tc := range cases {
		left := longOperationArrayValue(tc.list, size, -1)
		right := longOperationArrayValue(tc.list, size, size/2)

		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				result, err := lens.DiffWithOptions(left, right, options)
				if err != nil {
					b.Fatal(err)
				}
				if len(result.Changes) == 0 {
					b.Fatal("long arrays reported no changes")
				}
			}
		})
	}
}

// Ensure the fixtures used above stay large enough to be representative. A
// fixture that shrinks to a few bytes would silently turn the benchmark into a
// no-op; this test makes that drift visible.
func TestLedgerCloseBenchmarkFixturesAreSubstantial(t *testing.T) {
	t.Parallel()

	for _, fixture := range ledgerCloseDiffFixtures {
		t.Run(fixture.name, func(t *testing.T) {
			t.Parallel()
			payload, err := readFixture(fixture.fixture)
			if err != nil {
				t.Fatalf("reading fixture: %v", err)
			}
			if len(payload) < 100 {
				t.Errorf("fixture %s is only %d bytes; the benchmark would not be representative", fixture.fixture, len(payload))
			}
		})
	}
}
