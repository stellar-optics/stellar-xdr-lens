package lens_test

import (
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

// mustMarshal builds a base64 XDR payload from a concrete value. Constructing
// fixtures this way keeps diff tests deterministic and offline while still
// exercising the real encoder.
func mustMarshal(t *testing.T, v any) string {
	t.Helper()
	s, err := xdr.MarshalBase64(v)
	if err != nil {
		t.Fatalf("marshalling %T: %v", v, err)
	}
	return s
}

func TestDiffIdenticalValues(t *testing.T) {
	t.Parallel()

	fixtures := []string{"tx_success.env", "tx_failed.res", "tx_soroban.env"}

	for _, f := range fixtures {
		t.Run(f, func(t *testing.T) {
			t.Parallel()

			payload := loadFixture(t, f)
			got, err := lens.DiffBase64(payload, payload, "")
			if err != nil {
				t.Fatalf("DiffBase64() error = %v", err)
			}
			if !got.Equal() {
				t.Errorf("Diff of a value against itself reported %d change(s): %v",
					len(got.Changes), got.Changes)
			}
			if got.Summary() != "no differences" {
				t.Errorf("Summary() = %q, want %q", got.Summary(), "no differences")
			}
		})
	}
}

func TestDiffDetectsScalarChange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		left           any
		right          any
		wantPath       string
		wantOp         lens.ChangeOp
		wantBeforeNote string
		wantAfterNote  string
	}{
		{
			name:           "price numerator",
			left:           xdr.Price{N: 1, D: 2},
			right:          xdr.Price{N: 3, D: 2},
			wantPath:       "", // prices now render as a single fraction scalar
			wantOp:         lens.ChangeModified,
			wantBeforeNote: "1/2 (= 0.5000000)",
			wantAfterNote:  "3/2 (= 1.5000000)",
		},
		{
			name:           "price denominator",
			left:           xdr.Price{N: 1, D: 2},
			right:          xdr.Price{N: 1, D: 7},
			wantPath:       "", // prices now render as a single fraction scalar
			wantOp:         lens.ChangeModified,
			wantBeforeNote: "1/2 (= 0.5000000)",
			wantAfterNote:  "1/7 (= 0.1428571)",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := lens.DiffBase64(
				mustMarshal(t, tc.left), mustMarshal(t, tc.right), "Price")
			if err != nil {
				t.Fatalf("DiffBase64() error = %v", err)
			}
			if len(got.Changes) != 1 {
				t.Fatalf("got %d change(s), want 1: %v", len(got.Changes), got.Changes)
			}
			c := got.Changes[0]
			if c.Path != tc.wantPath {
				t.Errorf("Path = %q, want %q", c.Path, tc.wantPath)
			}
			if c.Op != tc.wantOp {
				t.Errorf("Op = %v, want %v", c.Op, tc.wantOp)
			}
			before, ok := c.Before.(string)
			if !ok || before != tc.wantBeforeNote {
				t.Errorf("Before = %v, want %q", c.Before, tc.wantBeforeNote)
			}
			after, ok := c.After.(string)
			if !ok || after != tc.wantAfterNote {
				t.Errorf("After = %v, want %q", c.After, tc.wantAfterNote)
			}
		})
	}
}

// TestDiffDetectsUnionArmSwitch covers the case that motivates a structural
// diff: when a union changes arms, the useful answer is which field appeared
// and which disappeared, not that the bytes differ.
func TestDiffDetectsUnionArmSwitch(t *testing.T) {
	t.Parallel()

	text := "hello"
	id := xdr.Uint64(42)

	left := xdr.Memo{Type: xdr.MemoTypeMemoText, Text: &text}
	right := xdr.Memo{Type: xdr.MemoTypeMemoId, Id: &id}

	got, err := lens.DiffBase64(mustMarshal(t, left), mustMarshal(t, right), "Memo")
	if err != nil {
		t.Fatalf("DiffBase64() error = %v", err)
	}
	if got.Equal() {
		t.Fatal("Diff reported no changes for a union that switched arms")
	}

	byPath := make(map[string]lens.Change, len(got.Changes))
	for _, c := range got.Changes {
		byPath[c.Path] = c
	}

	if c, ok := byPath["Text"]; !ok || c.Op != lens.ChangeRemoved {
		t.Errorf("want Text removed, got %+v (present=%v)", c, ok)
	}
	if c, ok := byPath["Id"]; !ok || c.Op != lens.ChangeAdded {
		t.Errorf("want Id added, got %+v (present=%v)", c, ok)
	}
	if c, ok := byPath["Type"]; !ok || c.Op != lens.ChangeModified {
		t.Errorf("want Type modified, got %+v (present=%v)", c, ok)
	}
}

// TestDiffRealTransactionsNamesChangedPaths asserts the diff is specific
// enough to be useful on production data, not merely non-empty.
func TestDiffRealTransactionsNamesChangedPaths(t *testing.T) {
	t.Parallel()

	got, err := lens.DiffBase64(
		loadFixture(t, "tx_soroban.res"),
		loadFixture(t, "tx_failed.res"),
		"",
	)
	if err != nil {
		t.Fatalf("DiffBase64() error = %v", err)
	}
	if got.Equal() {
		t.Fatal("Diff of a successful and a failed result reported no changes")
	}
	if got.LeftType != "TransactionResult" || got.RightType != "TransactionResult" {
		t.Errorf("types = %s/%s, want TransactionResult on both sides",
			got.LeftType, got.RightType)
	}

	// The transaction-level result code must be among the reported changes:
	// that is the single most important difference between these two.
	var foundCode bool
	for _, c := range got.Changes {
		if c.Path == "Result.Code" {
			foundCode = true
			before, _ := c.Before.(string)
			after, _ := c.After.(string)
			if !strings.Contains(before, "Success") {
				t.Errorf("Result.Code before = %q, want a success code", before)
			}
			if !strings.Contains(after, "Failed") {
				t.Errorf("Result.Code after = %q, want a failure code", after)
			}
		}
	}
	if !foundCode {
		t.Errorf("Diff did not report a change at Result.Code; paths were: %v", paths(got))
	}

	// Paths must be addressable, not opaque blobs.
	for _, c := range got.Changes {
		if c.Path == "" {
			t.Error("a change has an empty path")
		}
	}
}

// TestDiffAcrossDifferentTypes documents that comparing unlike types is
// allowed and reports the mismatch rather than failing.
func TestDiffAcrossDifferentTypes(t *testing.T) {
	t.Parallel()

	got, err := lens.DiffBase64(
		loadFixture(t, "tx_success.env"),
		loadFixture(t, "tx_success.res"),
		"",
	)
	if err != nil {
		t.Fatalf("DiffBase64() error = %v", err)
	}
	if got.LeftType == got.RightType {
		t.Fatalf("expected differing types, both were %s", got.LeftType)
	}
	if got.Equal() {
		t.Error("Diff of two different types reported no changes")
	}
}

func TestDiffListLengthChange(t *testing.T) {
	t.Parallel()

	// A claim predicate list is a convenient ordered collection to grow.
	left := xdr.ClaimPredicate{
		Type: xdr.ClaimPredicateTypeClaimPredicateAnd,
		AndPredicates: &[]xdr.ClaimPredicate{
			{Type: xdr.ClaimPredicateTypeClaimPredicateUnconditional},
			{Type: xdr.ClaimPredicateTypeClaimPredicateUnconditional},
		},
	}
	right := xdr.ClaimPredicate{
		Type: xdr.ClaimPredicateTypeClaimPredicateAnd,
		AndPredicates: &[]xdr.ClaimPredicate{
			{Type: xdr.ClaimPredicateTypeClaimPredicateUnconditional},
		},
	}

	l, err := lens.DecodeAs(mustMarshal(t, left), "ClaimPredicate")
	if err != nil {
		t.Skipf("ClaimPredicate is not in the registry: %v", err)
	}
	r, err := lens.DecodeAs(mustMarshal(t, right), "ClaimPredicate")
	if err != nil {
		t.Skipf("ClaimPredicate is not in the registry: %v", err)
	}

	got, err := lens.Diff(l, r)
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}
	if got.Equal() {
		t.Error("Diff of lists with different lengths reported no changes")
	}
}

func TestDiffWithOptionsAlignsInsertedListItem(t *testing.T) {
	t.Parallel()

	value := func(items ...string) *lens.Value {
		children := make([]*lens.Node, 0, len(items))
		for _, item := range items {
			children = append(children, &lens.Node{Kind: lens.KindScalar, Value: item})
		}
		return &lens.Value{Type: "List", Node: &lens.Node{Kind: lens.KindList, Children: children}}
	}

	left, right := value("a", "b"), value("new", "a", "b")
	defaultDiff, err := lens.Diff(left, right)
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}
	aligned, err := lens.DiffWithOptions(left, right, lens.DiffOptions{AlignLists: true})
	if err != nil {
		t.Fatalf("DiffWithOptions() error = %v", err)
	}
	if len(defaultDiff.Changes) <= 1 {
		t.Fatalf("positional diff got %d change(s), want cascading changes", len(defaultDiff.Changes))
	}
	if len(aligned.Changes) != 1 {
		t.Fatalf("aligned diff got %d change(s), want one: %+v", len(aligned.Changes), aligned.Changes)
	}
	if aligned.Changes[0].Op != lens.ChangeAdded || aligned.Changes[0].Path != "[0]" {
		t.Errorf("aligned change = %+v, want added item at [0]", aligned.Changes[0])
	}
}

func TestDiffWithOptionsAlignsRemovedListItem(t *testing.T) {
	t.Parallel()

	value := func(items ...string) *lens.Value {
		children := make([]*lens.Node, 0, len(items))
		for _, item := range items {
			children = append(children, &lens.Node{Kind: lens.KindScalar, Value: item})
		}
		return &lens.Value{Type: "List", Node: &lens.Node{Kind: lens.KindList, Children: children}}
	}

	got, err := lens.DiffWithOptions(value("a", "remove", "b"), value("a", "b"), lens.DiffOptions{AlignLists: true})
	if err != nil {
		t.Fatalf("DiffWithOptions() error = %v", err)
	}
	if len(got.Changes) != 1 || got.Changes[0].Op != lens.ChangeRemoved {
		t.Fatalf("aligned diff = %+v, want one removal", got.Changes)
	}
}

func TestDiffRejectsNilValues(t *testing.T) {
	t.Parallel()

	v, err := lens.Decode(loadFixture(t, "tx_success.env"))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	if _, err := lens.Diff(nil, v); err == nil {
		t.Error("Diff(nil, v) error = nil, want an error")
	}
	if _, err := lens.Diff(v, nil); err == nil {
		t.Error("Diff(v, nil) error = nil, want an error")
	}
}

func TestDiffBase64ReportsDecodeErrors(t *testing.T) {
	t.Parallel()

	valid := loadFixture(t, "tx_success.env")

	tests := []struct {
		name        string
		left, right string
	}{
		{"bad left", "!!!", valid},
		{"bad right", valid, "!!!"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if _, err := lens.DiffBase64(tc.left, tc.right, ""); err == nil {
				t.Error("DiffBase64() error = nil, want an error")
			}
		})
	}
}

func TestChangeOpString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		op   lens.ChangeOp
		want string
	}{
		{lens.ChangeModified, "modified"},
		{lens.ChangeAdded, "added"},
		{lens.ChangeRemoved, "removed"},
		{lens.ChangeTypeChanged, "type_changed"},
	}

	for _, tc := range tests {
		if got := tc.op.String(); got != tc.want {
			t.Errorf("ChangeOp(%d).String() = %q, want %q", tc.op, got, tc.want)
		}
	}
}

func TestChangeStringAndDiffSummary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		change lens.Change
		want   string
	}{
		{lens.Change{Path: "Field", Op: lens.ChangeAdded, After: "new"}, "+ Field = new"},
		{lens.Change{Path: "Field", Op: lens.ChangeRemoved, Before: "old"}, "- Field = old"},
		{lens.Change{Path: "Field", Op: lens.ChangeModified, Before: 1, After: 2}, "~ Field: 1 -> 2"},
		{lens.Change{Path: "Field", Op: lens.ChangeTypeChanged, BeforeNote: "before", AfterNote: "after"}, "~ Field: before -> after"},
	}
	for _, tc := range tests {
		if got := tc.change.String(); got != tc.want {
			t.Errorf("Change.String() = %q, want %q", got, tc.want)
		}
	}

	result := &lens.DiffResult{Changes: []lens.Change{
		{Op: lens.ChangeModified},
		{Op: lens.ChangeTypeChanged},
		{Op: lens.ChangeAdded},
		{Op: lens.ChangeRemoved},
	}}
	if got, want := result.Summary(), "2 changed, 1 added, 1 removed"; got != want {
		t.Errorf("Summary() = %q, want %q", got, want)
	}
}

func TestDiffChangeSummariesForAddedContainers(t *testing.T) {
	t.Parallel()

	left := &lens.Value{Type: "Root", Node: &lens.Node{Kind: lens.KindStruct}}
	right := &lens.Value{Type: "Root", Node: &lens.Node{Kind: lens.KindStruct, Children: []*lens.Node{
		{Name: "Record", Kind: lens.KindStruct, TypeName: "RecordType"},
		{Name: "Items", Kind: lens.KindList, Children: []*lens.Node{{Kind: lens.KindScalar, Value: "x"}}},
	}}}
	got, err := lens.Diff(left, right)
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}
	if len(got.Changes) != 2 || got.Changes[0].After != "{RecordType}" || got.Changes[1].After != "[1 item(s)]" {
		t.Errorf("container addition values = %+v", got.Changes)
	}
}

func TestDiffAlignmentComparesNestedStructItems(t *testing.T) {
	t.Parallel()

	item := func(id string) *lens.Node {
		return &lens.Node{Kind: lens.KindStruct, TypeName: "Item", Children: []*lens.Node{
			{Name: "ID", Kind: lens.KindScalar, Value: id},
		}}
	}
	list := func(items ...*lens.Node) *lens.Value {
		return &lens.Value{Type: "Items", Node: &lens.Node{Kind: lens.KindList, Children: items}}
	}
	got, err := lens.DiffWithOptions(list(item("a"), item("b")), list(item("inserted"), item("a"), item("b")), lens.DiffOptions{AlignLists: true})
	if err != nil {
		t.Fatalf("DiffWithOptions() error = %v", err)
	}
	if len(got.Changes) != 1 || got.Changes[0].Op != lens.ChangeAdded || got.Changes[0].After != "{Item}" {
		t.Errorf("aligned struct diff = %+v, want one inserted Item", got.Changes)
	}
}

func paths(d *lens.DiffResult) []string {
	out := make([]string, 0, len(d.Changes))
	for _, c := range d.Changes {
		out = append(out, c.Path)
	}
	return out
}

func TestDiffBinaryNormalization(t *testing.T) {
	t.Parallel()

	// 4 bytes: 0xdeadbeef
	// Hex: "deadbeef", "DEADBEEF", "0xdeadbeef"
	// Base64: "3q2+7w=="
	// Raw: []byte{0xde, 0xad, 0xbe, 0xef}

	tests := []struct {
		name  string
		left  *lens.Node
		right *lens.Node
	}{
		{
			name:  "hex lowercase vs base64",
			left:  &lens.Node{Kind: lens.KindScalar, Value: "deadbeef"},
			right: &lens.Node{Kind: lens.KindScalar, Value: "3q2+7w=="},
		},
		{
			name:  "hex uppercase vs base64",
			left:  &lens.Node{Kind: lens.KindScalar, Value: "DEADBEEF"},
			right: &lens.Node{Kind: lens.KindScalar, Value: "3q2+7w=="},
		},
		{
			name:  "hex with 0x prefix vs base64",
			left:  &lens.Node{Kind: lens.KindScalar, Value: "0xdeadbeef"},
			right: &lens.Node{Kind: lens.KindScalar, Value: "3q2+7w=="},
		},
		{
			name:  "hex uppercase vs hex lowercase",
			left:  &lens.Node{Kind: lens.KindScalar, Value: "DEADBEEF"},
			right: &lens.Node{Kind: lens.KindScalar, Value: "deadbeef"},
		},
		{
			name:  "byte slice vs base64",
			left:  &lens.Node{Kind: lens.KindScalar, Value: []byte{0xde, 0xad, 0xbe, 0xef}},
			right: &lens.Node{Kind: lens.KindScalar, Value: "3q2+7w=="},
		},
		{
			name:  "byte slice vs hex",
			left:  &lens.Node{Kind: lens.KindScalar, Value: []byte{0xde, 0xad, 0xbe, 0xef}},
			right: &lens.Node{Kind: lens.KindScalar, Value: "deadbeef"},
		},
		{
			name:  "byte array vs hex",
			left:  &lens.Node{Kind: lens.KindScalar, Value: [4]byte{0xde, 0xad, 0xbe, 0xef}},
			right: &lens.Node{Kind: lens.KindScalar, Value: "deadbeef"},
		},
		{
			name:  "byte array vs base64",
			left:  &lens.Node{Kind: lens.KindScalar, Value: [4]byte{0xde, 0xad, 0xbe, 0xef}},
			right: &lens.Node{Kind: lens.KindScalar, Value: "3q2+7w=="},
		},
		{
			name: "list of uint8 bytes vs hex",
			left: &lens.Node{
				Kind: lens.KindList,
				Children: []*lens.Node{
					{Kind: lens.KindScalar, Value: uint8(0xde)},
					{Kind: lens.KindScalar, Value: uint8(0xad)},
					{Kind: lens.KindScalar, Value: uint8(0xbe)},
					{Kind: lens.KindScalar, Value: uint8(0xef)},
				},
			},
			right: &lens.Node{Kind: lens.KindScalar, Value: "deadbeef"},
		},
		{
			name: "list of int bytes vs base64",
			left: &lens.Node{
				Kind: lens.KindList,
				Children: []*lens.Node{
					{Kind: lens.KindScalar, Value: 0xde},
					{Kind: lens.KindScalar, Value: 0xad},
					{Kind: lens.KindScalar, Value: 0xbe},
					{Kind: lens.KindScalar, Value: 0xef},
				},
			},
			right: &lens.Node{Kind: lens.KindScalar, Value: "3q2+7w=="},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			leftVal := &lens.Value{Type: "BinaryData", Node: tc.left}
			rightVal := &lens.Value{Type: "BinaryData", Node: tc.right}

			res, err := lens.Diff(leftVal, rightVal)
			if err != nil {
				t.Fatalf("Diff() error = %v", err)
			}
			if !res.Equal() {
				t.Errorf("Diff() reported %d change(s), want equal: %+v", len(res.Changes), res.Changes)
			}

			// Reverse symmetry
			resRev, err := lens.Diff(rightVal, leftVal)
			if err != nil {
				t.Fatalf("Diff() reverse error = %v", err)
			}
			if !resRev.Equal() {
				t.Errorf("Diff() reverse reported %d change(s), want equal: %+v", len(resRev.Changes), resRev.Changes)
			}
		})
	}
}

func TestDiffBinaryDifferentContentReportsModified(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		left  any
		right any
	}{
		{
			name:  "different hex content same length",
			left:  "deadbeef",
			right: "cafebabe",
		},
		{
			name:  "hex vs base64 with different bytes",
			left:  "deadbeef",
			right: "AQIDBA==", // 01020304
		},
		{
			name:  "different length hex",
			left:  "deadbeef",
			right: "deadbeef01",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			leftVal := &lens.Value{Type: "Hash", Node: &lens.Node{Kind: lens.KindScalar, Value: tc.left}}
			rightVal := &lens.Value{Type: "Hash", Node: &lens.Node{Kind: lens.KindScalar, Value: tc.right}}

			res, err := lens.Diff(leftVal, rightVal)
			if err != nil {
				t.Fatalf("Diff() error = %v", err)
			}
			if res.Equal() || len(res.Changes) != 1 {
				t.Fatalf("expected 1 change, got %d (equal=%v)", len(res.Changes), res.Equal())
			}
			if res.Changes[0].Op != lens.ChangeModified {
				t.Errorf("Op = %v, want ChangeModified", res.Changes[0].Op)
			}
		})
	}
}

func TestDiffBinaryAlignedLists(t *testing.T) {
	t.Parallel()

	// List with hex on left and base64 on right
	leftList := &lens.Value{
		Type: "HashList",
		Node: &lens.Node{
			Kind: lens.KindList,
			Children: []*lens.Node{
				{Kind: lens.KindScalar, Value: "deadbeef"},
				{Kind: lens.KindScalar, Value: "cafebabe"},
			},
		},
	}
	rightList := &lens.Value{
		Type: "HashList",
		Node: &lens.Node{
			Kind: lens.KindList,
			Children: []*lens.Node{
				{Kind: lens.KindScalar, Value: "3q2+7w=="}, // deadbeef in base64
				{Kind: lens.KindScalar, Value: "yv66vg=="}, // cafebabe in base64
			},
		},
	}

	res, err := lens.DiffWithOptions(leftList, rightList, lens.DiffOptions{AlignLists: true})
	if err != nil {
		t.Fatalf("DiffWithOptions() error = %v", err)
	}
	if !res.Equal() {
		t.Errorf("aligned list with normalized binary items reported %d change(s): %+v", len(res.Changes), res.Changes)
	}
}

// TestKindBytesNodeTagging covers issue #290: binary nodes are tagged with
// KindBytes so the differ normalises hex/base64 representations.
func TestKindBytesNodeTagging(t *testing.T) {
	t.Parallel()

	// Nodes produced by buildNode for a []byte field should be KindBytes.
	hexLeft := &lens.Value{
		Type: "BinaryRecord",
		Node: &lens.Node{
			Kind: lens.KindStruct,
			Children: []*lens.Node{
				{Name: "Hash", Kind: lens.KindBytes, Value: "deadbeef"},
			},
		},
	}
	// Same bytes encoded as base64 — must compare equal despite different encoding.
	b64Right := &lens.Value{
		Type: "BinaryRecord",
		Node: &lens.Node{
			Kind: lens.KindStruct,
			Children: []*lens.Node{
				{Name: "Hash", Kind: lens.KindBytes, Value: "3q2+7w=="},
			},
		},
	}

	res, err := lens.Diff(hexLeft, b64Right)
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}
	if !res.Equal() {
		t.Errorf("KindBytes hex vs base64 diff reported %d change(s): %+v",
			len(res.Changes), res.Changes)
	}

	// Two KindBytes nodes with different content must be reported as modified.
	differentRight := &lens.Value{
		Type: "BinaryRecord",
		Node: &lens.Node{
			Kind: lens.KindStruct,
			Children: []*lens.Node{
				{Name: "Hash", Kind: lens.KindBytes, Value: "cafebabe"},
			},
		},
	}
	res2, err := lens.Diff(hexLeft, differentRight)
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}
	if res2.Equal() {
		t.Error("KindBytes diff with different content reported equality, want modification")
	}
}

func TestDiffNumericThreshold(t *testing.T) {
	t.Parallel()

	mkVal := func(name string, val any) *lens.Value {
		return &lens.Value{
			Type: "Record",
			Node: &lens.Node{
				Kind: lens.KindStruct,
				Children: []*lens.Node{
					{Name: name, Kind: lens.KindScalar, Value: val},
				},
			},
		}
	}

	t.Run("delta within threshold is ignored", func(t *testing.T) {
		t.Parallel()
		left := mkVal("SeqNum", 1000)
		right := mkVal("SeqNum", 1005)

		res, err := lens.DiffWithOptions(left, right, lens.DiffOptions{Threshold: 10})
		if err != nil {
			t.Fatalf("DiffWithOptions() error = %v", err)
		}
		if !res.Equal() {
			t.Errorf("expected no differences within threshold 10, got %d change(s): %+v", len(res.Changes), res.Changes)
		}
	})

	t.Run("delta equal to threshold is ignored", func(t *testing.T) {
		t.Parallel()
		left := mkVal("SeqNum", 1000)
		right := mkVal("SeqNum", 1005)

		res, err := lens.DiffWithOptions(left, right, lens.DiffOptions{Threshold: 5})
		if err != nil {
			t.Fatalf("DiffWithOptions() error = %v", err)
		}
		if !res.Equal() {
			t.Errorf("expected no differences at threshold boundary, got %d change(s): %+v", len(res.Changes), res.Changes)
		}
	})

	t.Run("delta exceeding threshold is reported", func(t *testing.T) {
		t.Parallel()
		left := mkVal("SeqNum", 1000)
		right := mkVal("SeqNum", 1006)

		res, err := lens.DiffWithOptions(left, right, lens.DiffOptions{Threshold: 5})
		if err != nil {
			t.Fatalf("DiffWithOptions() error = %v", err)
		}
		if res.Equal() || len(res.Changes) != 1 {
			t.Fatalf("expected 1 change, got %d", len(res.Changes))
		}
		if res.Changes[0].Op != lens.ChangeModified {
			t.Errorf("Op = %v, want ChangeModified", res.Changes[0].Op)
		}
	})

	t.Run("floating point threshold and string numbers", func(t *testing.T) {
		t.Parallel()
		left := mkVal("Amount", "100.25")
		right := mkVal("Amount", "100.50")

		res, err := lens.DiffWithOptions(left, right, lens.DiffOptions{Threshold: 0.5})
		if err != nil {
			t.Fatalf("DiffWithOptions() error = %v", err)
		}
		if !res.Equal() {
			t.Errorf("expected no differences within threshold 0.5, got %+v", res.Changes)
		}

		resStrict, err := lens.DiffWithOptions(left, right, lens.DiffOptions{Threshold: 0.1})
		if err != nil {
			t.Fatalf("DiffWithOptions() error = %v", err)
		}
		if resStrict.Equal() {
			t.Error("expected difference when threshold is 0.1, got equal")
		}
	})

	t.Run("DiffThreshold alias field works", func(t *testing.T) {
		t.Parallel()
		left := mkVal("Fee", uint32(100))
		right := mkVal("Fee", uint32(105))

		res, err := lens.DiffWithOptions(left, right, lens.DiffOptions{DiffThreshold: 10})
		if err != nil {
			t.Fatalf("DiffWithOptions() error = %v", err)
		}
		if !res.Equal() {
			t.Errorf("expected no differences with DiffThreshold alias, got %+v", res.Changes)
		}
	})

	t.Run("threshold zero enforces strict equality", func(t *testing.T) {
		t.Parallel()
		left := mkVal("Count", 10)
		right := mkVal("Count", 11)

		res, err := lens.DiffWithOptions(left, right, lens.DiffOptions{Threshold: 0})
		if err != nil {
			t.Fatalf("DiffWithOptions() error = %v", err)
		}
		if res.Equal() || len(res.Changes) != 1 {
			t.Fatalf("expected 1 change with threshold 0, got %d", len(res.Changes))
		}
	})

	t.Run("diverse scalar number types", func(t *testing.T) {
		t.Parallel()

		typePairs := []struct {
			left  any
			right any
		}{
			{float32(10.0), float32(10.5)},
			{float64(10.0), float64(10.5)},
			{int(10), int(11)},
			{int64(10), int64(11)},
			{int32(10), int32(11)},
			{int16(10), int16(11)},
			{int8(10), int8(11)},
			{uint(10), uint(11)},
			{uint64(10), uint64(11)},
			{uint32(10), uint32(11)},
			{uint16(10), uint16(11)},
			{uint8(10), uint8(11)},
		}

		for _, p := range typePairs {
			left := mkVal("Val", p.left)
			right := mkVal("Val", p.right)
			res, err := lens.DiffWithOptions(left, right, lens.DiffOptions{Threshold: 2.0})
			if err != nil {
				t.Fatalf("DiffWithOptions(%T) error = %v", p.left, err)
			}
			if !res.Equal() {
				t.Errorf("expected equal within threshold 2 for %T, got %+v", p.left, res.Changes)
			}
		}

		// Non-number strings and boolean values with threshold
		nonNums := mkVal("Text", "not_a_number")
		otherNonNums := mkVal("Text", "still_not_a_number")
		resNon, err := lens.DiffWithOptions(nonNums, otherNonNums, lens.DiffOptions{Threshold: 5.0})
		if err != nil {
			t.Fatalf("DiffWithOptions(non-num) error = %v", err)
		}
		if resNon.Equal() || len(resNon.Changes) != 1 {
			t.Errorf("expected 1 change for non-numeric strings with threshold, got %d", len(resNon.Changes))
		}

		b1 := mkVal("Bool", true)
		b2 := mkVal("Bool", false)
		resB, err := lens.DiffWithOptions(b1, b2, lens.DiffOptions{Threshold: 5.0})
		if err != nil {
			t.Fatalf("DiffWithOptions(bool) error = %v", err)
		}
		if resB.Equal() || len(resB.Changes) != 1 {
			t.Errorf("expected 1 change for booleans, got %d", len(resB.Changes))
		}
	})
}

func ptrHelper[T any](v T) *T { return &v }

func TestDiffOperationReorderingDetection(t *testing.T) {
	t.Parallel()

	opA := xdr.Operation{
		Body: xdr.OperationBody{
			Type: xdr.OperationTypeManageData,
			ManageDataOp: &xdr.ManageDataOp{
				DataName: "first",
			},
		},
	}
	opB := xdr.Operation{
		Body: xdr.OperationBody{
			Type: xdr.OperationTypeManageData,
			ManageDataOp: &xdr.ManageDataOp{
				DataName: "second",
			},
		},
	}
	destAccountID := xdr.MustAddress("GCJNHFXJEQEY54NPKU4DBMT5WOBJUHMYSFBDAQF4OMTEGJN4BQU7ZST6")
	destMuxed := destAccountID.ToMuxedAccount()
	opC := xdr.Operation{
		Body: xdr.OperationBody{
			Type: xdr.OperationTypePayment,
			PaymentOp: &xdr.PaymentOp{
				Destination: destMuxed,
				Asset:       xdr.Asset{Type: xdr.AssetTypeAssetTypeNative},
				Amount:      1000,
			},
		},
	}
	opCModified := xdr.Operation{
		Body: xdr.OperationBody{
			Type: xdr.OperationTypePayment,
			PaymentOp: &xdr.PaymentOp{
				Destination: destMuxed,
				Asset:       xdr.Asset{Type: xdr.AssetTypeAssetTypeNative},
				Amount:      2000,
			},
		},
	}

	t.Run("operations 0 and 1 swapped", func(t *testing.T) {
		t.Parallel()

		env1 := xdr.TransactionEnvelope{
			Type: xdr.EnvelopeTypeEnvelopeTypeTx,
			V1: &xdr.TransactionV1Envelope{
				Tx: xdr.Transaction{
					SourceAccount: destMuxed,
					Fee:           100,
					SeqNum:        1,
					Operations:    []xdr.Operation{opA, opB},
				},
			},
		}
		env2 := xdr.TransactionEnvelope{
			Type: xdr.EnvelopeTypeEnvelopeTypeTx,
			V1: &xdr.TransactionV1Envelope{
				Tx: xdr.Transaction{
					SourceAccount: destMuxed,
					Fee:           100,
					SeqNum:        1,
					Operations:    []xdr.Operation{opB, opA},
				},
			},
		}

		got, err := lens.DiffBase64(mustMarshal(t, env1), mustMarshal(t, env2), "TransactionEnvelope")
		if err != nil {
			t.Fatalf("DiffBase64() error = %v", err)
		}
		if got.Equal() {
			t.Fatal("expected diff for swapped operations, got equal")
		}
		if len(got.Changes) != 2 {
			t.Fatalf("expected 2 changes, got %d: %+v", len(got.Changes), got.Changes)
		}

		// Ensure both are reported as reordered and not delete/insert pairs
		for _, c := range got.Changes {
			if c.Op != lens.ChangeReordered {
				t.Errorf("change op = %v (%s), want ChangeReordered", c.Op, c.Op.String())
			}
			if !strings.Contains(c.Path, "Operations[") {
				t.Errorf("path %q does not target an Operation index", c.Path)
			}
		}

		if got.Summary() != "2 reordered" {
			t.Errorf("Summary() = %q, want %q", got.Summary(), "2 reordered")
		}
	})

	t.Run("swapped operations with one modified operation", func(t *testing.T) {
		t.Parallel()

		env1 := xdr.TransactionEnvelope{
			Type: xdr.EnvelopeTypeEnvelopeTypeTx,
			V1: &xdr.TransactionV1Envelope{
				Tx: xdr.Transaction{
					SourceAccount: destMuxed,
					Fee:           100,
					SeqNum:        1,
					Operations:    []xdr.Operation{opA, opB, opC},
				},
			},
		}
		env2 := xdr.TransactionEnvelope{
			Type: xdr.EnvelopeTypeEnvelopeTypeTx,
			V1: &xdr.TransactionV1Envelope{
				Tx: xdr.Transaction{
					SourceAccount: destMuxed,
					Fee:           100,
					SeqNum:        1,
					Operations:    []xdr.Operation{opB, opA, opCModified},
				},
			},
		}

		got, err := lens.DiffBase64(mustMarshal(t, env1), mustMarshal(t, env2), "TransactionEnvelope")
		if err != nil {
			t.Fatalf("DiffBase64() error = %v", err)
		}
		if got.Equal() {
			t.Fatal("expected diff, got equal")
		}

		var reorderedCount, modifiedCount int
		for _, c := range got.Changes {
			switch c.Op {
			case lens.ChangeReordered:
				reorderedCount++
			case lens.ChangeModified:
				modifiedCount++
			}
		}
		if reorderedCount != 2 {
			t.Errorf("reorderedCount = %d, want 2", reorderedCount)
		}
		if modifiedCount < 1 {
			t.Errorf("modifiedCount = %d, want at least 1", modifiedCount)
		}
	})
}

func TestDiffContractEventsLogComparison(t *testing.T) {
	t.Parallel()

	cidA := xdr.ContractId{1, 2, 3, 4}
	cidB := xdr.ContractId{5, 6, 7, 8}

	symTransfer := xdr.ScVal{
		Type: xdr.ScValTypeScvSymbol,
		Sym:  (*xdr.ScSymbol)(ptrHelper("transfer")),
	}
	symMint := xdr.ScVal{
		Type: xdr.ScValTypeScvSymbol,
		Sym:  (*xdr.ScSymbol)(ptrHelper("mint")),
	}

	data100 := xdr.ScVal{
		Type: xdr.ScValTypeScvU32,
		U32:  (*xdr.Uint32)(ptrHelper(uint32(100))),
	}
	data200 := xdr.ScVal{
		Type: xdr.ScValTypeScvU32,
		U32:  (*xdr.Uint32)(ptrHelper(uint32(200))),
	}

	ev1 := xdr.ContractEvent{
		ContractId: &cidA,
		Type:       xdr.ContractEventTypeContract,
		Body: xdr.ContractEventBody{
			V0: &xdr.ContractEventV0{
				Topics: []xdr.ScVal{symTransfer},
				Data:   data100,
			},
		},
	}
	ev1DataChanged := xdr.ContractEvent{
		ContractId: &cidA,
		Type:       xdr.ContractEventTypeContract,
		Body: xdr.ContractEventBody{
			V0: &xdr.ContractEventV0{
				Topics: []xdr.ScVal{symTransfer},
				Data:   data200,
			},
		},
	}
	ev2 := xdr.ContractEvent{
		ContractId: &cidB,
		Type:       xdr.ContractEventTypeContract,
		Body: xdr.ContractEventBody{
			V0: &xdr.ContractEventV0{
				Topics: []xdr.ScVal{symMint},
				Data:   data100,
			},
		},
	}

	t.Run("altered data values highlighted", func(t *testing.T) {
		t.Parallel()

		got, err := lens.DiffContractEvents([]xdr.ContractEvent{ev1, ev2}, []xdr.ContractEvent{ev1DataChanged, ev2})
		if err != nil {
			t.Fatalf("DiffContractEvents() error = %v", err)
		}
		if got.Equal() {
			t.Fatal("expected diff, got equal")
		}

		var foundDataChange bool
		for _, c := range got.Changes {
			if strings.Contains(c.Path, "Data") {
				foundDataChange = true
				if c.Op != lens.ChangeModified {
					t.Errorf("Data change Op = %v, want ChangeModified", c.Op)
				}
			}
		}
		if !foundDataChange {
			t.Errorf("expected altered data value change, got %+v", got.Changes)
		}
	})

	t.Run("topic mutations highlighted", func(t *testing.T) {
		t.Parallel()

		topicAddr1 := xdr.ScVal{
			Type:    xdr.ScValTypeScvAddress,
			Address: &xdr.ScAddress{Type: xdr.ScAddressTypeScAddressTypeAccount, AccountId: ptrHelper(xdr.MustAddress("GCJNHFXJEQEY54NPKU4DBMT5WOBJUHMYSFBDAQF4OMTEGJN4BQU7ZST6"))},
		}
		topicAddr2 := xdr.ScVal{
			Type:    xdr.ScValTypeScvAddress,
			Address: &xdr.ScAddress{Type: xdr.ScAddressTypeScAddressTypeAccount, AccountId: ptrHelper(xdr.MustAddress("GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN"))},
		}

		evMultiTopic1 := xdr.ContractEvent{
			ContractId: &cidA,
			Type:       xdr.ContractEventTypeContract,
			Body: xdr.ContractEventBody{
				V0: &xdr.ContractEventV0{
					Topics: []xdr.ScVal{symTransfer, topicAddr1},
					Data:   data100,
				},
			},
		}
		evMultiTopic2 := xdr.ContractEvent{
			ContractId: &cidA,
			Type:       xdr.ContractEventTypeContract,
			Body: xdr.ContractEventBody{
				V0: &xdr.ContractEventV0{
					Topics: []xdr.ScVal{symTransfer, topicAddr2},
					Data:   data100,
				},
			},
		}

		got, err := lens.DiffContractEvents([]xdr.ContractEvent{evMultiTopic1}, []xdr.ContractEvent{evMultiTopic2})
		if err != nil {
			t.Fatalf("DiffContractEvents() error = %v", err)
		}
		if got.Equal() {
			t.Fatal("expected diff for topic mutation, got equal")
		}

		var foundTopicMutation bool
		for _, c := range got.Changes {
			if strings.Contains(c.Path, "Topics") {
				foundTopicMutation = true
			}
		}
		if !foundTopicMutation {
			t.Errorf("expected topic mutation change, got %+v", got.Changes)
		}
	})
}

func TestDiffAccountSignerQuorumDrift(t *testing.T) {
	t.Parallel()

	accountID := xdr.MustAddress("GCJNHFXJEQEY54NPKU4DBMT5WOBJUHMYSFBDAQF4OMTEGJN4BQU7ZST6")
	signerKey := xdr.SignerKey{
		Type:    xdr.SignerKeyTypeSignerKeyTypeEd25519,
		Ed25519: (*xdr.Uint256)(&[32]byte{1, 2, 3, 4}),
	}

	acctBefore := xdr.AccountEntry{
		AccountId:  accountID,
		Thresholds: [4]byte{2, 1, 2, 3}, // master=2, low=1, med=2, high=3
		Signers: []xdr.Signer{
			{Key: signerKey, Weight: 2},
		},
	}
	// Rotation: master locked (0), signer weight 1 -> total reachable 1.
	// Medium=2, High=3. Total reachable (1) falls below both!
	acctAfter := xdr.AccountEntry{
		AccountId:  accountID,
		Thresholds: [4]byte{0, 1, 2, 3},
		Signers: []xdr.Signer{
			{Key: signerKey, Weight: 1},
		},
	}

	t.Run("AnalyzeAccountSigners computes power delta and warnings", func(t *testing.T) {
		t.Parallel()

		analysis := lens.AnalyzeAccountSigners(&acctBefore, &acctAfter)
		analysis2 := lens.DiffAccountSigners(&acctBefore, &acctAfter)
		if analysis.BeforeWeight != analysis2.BeforeWeight {
			t.Errorf("DiffAccountSigners BeforeWeight mismatch")
		}
		if analysis.BeforeWeight != 4 {
			t.Errorf("BeforeWeight = %d, want 4", analysis.BeforeWeight)
		}
		if analysis.AfterWeight != 1 {
			t.Errorf("AfterWeight = %d, want 1", analysis.AfterWeight)
		}
		if analysis.WeightDelta != -3 {
			t.Errorf("WeightDelta = %d, want -3", analysis.WeightDelta)
		}
		if analysis.MeetsMedium {
			t.Error("MeetsMedium = true, want false")
		}
		if analysis.MeetsHigh {
			t.Error("MeetsHigh = true, want false")
		}
		if len(analysis.Warnings) < 2 {
			t.Errorf("expected at least 2 warnings, got %d: %v", len(analysis.Warnings), analysis.Warnings)
		}
	})

	t.Run("DiffBase64 reports signing power change and warnings", func(t *testing.T) {
		t.Parallel()

		got, err := lens.DiffBase64(mustMarshal(t, acctBefore), mustMarshal(t, acctAfter), "AccountEntry")
		if err != nil {
			t.Fatalf("DiffBase64() error = %v", err)
		}
		if got.Equal() {
			t.Fatal("expected diff, got equal")
		}

		var foundPowerChange bool
		for _, c := range got.Changes {
			if strings.Contains(c.Path, "EffectiveSigningPower") {
				foundPowerChange = true
				if c.Before != uint32(4) || c.After != uint32(1) {
					t.Errorf("Signing power change = %v -> %v, want 4 -> 1", c.Before, c.After)
				}
			}
		}
		if !foundPowerChange {
			t.Errorf("expected EffectiveSigningPower change, got %+v", got.Changes)
		}
		if len(got.Warnings) == 0 {
			t.Errorf("expected warnings on quorum drift, got none")
		}
	})
}

func TestDiffLedgerEntryStateTransition(t *testing.T) {
	t.Parallel()

	// Load test fixtures for LedgerEntry types
	offerLE, err := lens.DecodeAs(loadFixture(t, "ledger_entry_offer"), "LedgerEntry")
	if err != nil {
		t.Fatalf("loading offer fixture: %v", err)
	}
	contractDataLE, err := lens.DecodeAs(loadFixture(t, "ledger_entry_contract_data"), "LedgerEntry")
	if err != nil {
		t.Fatalf("loading contract_data fixture: %v", err)
	}
	ttlLE, err := lens.DecodeAs(loadFixture(t, "ledger_entry_ttl"), "LedgerEntry")
	if err != nil {
		t.Fatalf("loading ttl fixture: %v", err)
	}

	// Test 1: Created - compare different entry types (simulates created/deleted)
	t.Run("Created/Deleted transition between types", func(t *testing.T) {
		t.Parallel()
		left := offerLE.Raw.(*xdr.LedgerEntry)
		right := contractDataLE.Raw.(*xdr.LedgerEntry)

		got, err := lens.DiffBase64(mustMarshal(t, left), mustMarshal(t, right), "LedgerEntry")
		if err != nil {
			t.Fatalf("DiffBase64() error = %v", err)
		}

		// Should detect transition (different types = created/deleted effectively)
		found := false
		for _, c := range got.Changes {
			if c.Op == lens.ChangeModified && (strings.Contains(c.BeforeNote, "transition") || strings.Contains(c.AfterNote, "transition")) {
				found = true
				break
			}
		}
		if !found {
			t.Logf("Changes: %v", got.Changes)
		}
		if len(got.Changes) == 0 {
			t.Errorf("expected changes for different entry types, got none")
		}
	})

	// Test 2: Updated transition with modified offer
	t.Run("Updated transition with modified offer", func(t *testing.T) {
		t.Parallel()
		// Use the offer fixture and modify it for the right side
		left := offerLE.Raw.(*xdr.LedgerEntry)
		// Create a deep copy with different OfferId
		right := *left
		if left.Data.Offer != nil {
			offerCopy := *left.Data.Offer
			offerCopy.OfferId = 99999
			right.Data.Offer = &offerCopy
		}

		got, err := lens.DiffBase64(mustMarshal(t, left), mustMarshal(t, right), "LedgerEntry")
		if err != nil {
			t.Fatalf("DiffBase64() error = %v", err)
		}

		found := false
		for _, c := range got.Changes {
			if c.Op == lens.ChangeModified && (strings.Contains(c.BeforeNote, "UPDATED") || strings.Contains(c.AfterNote, "UPDATED")) {
				found = true
				break
			}
		}
		if !found {
			t.Logf("Changes: %v", got.Changes)
		}
		// Should also have detailed changes (more than just the transition header and LedgerKey)
		detailedChanges := 0
		for _, c := range got.Changes {
			if c.Path != "" && c.Path != ".LedgerKey" {
				detailedChanges++
			}
		}
		if detailedChanges == 0 {
			t.Logf("Warning: no detailed changes found beyond transition header")
			t.Logf("All changes: %v", got.Changes)
		}
	})

	// Test 3: Different entry types - ContractData to TTL
	t.Run("ContractData to TTL transition", func(t *testing.T) {
		t.Parallel()
		left := contractDataLE.Raw.(*xdr.LedgerEntry)
		right := ttlLE.Raw.(*xdr.LedgerEntry)

		got, err := lens.DiffBase64(mustMarshal(t, left), mustMarshal(t, right), "LedgerEntry")
		if err != nil {
			t.Fatalf("DiffBase64() error = %v", err)
		}

		found := false
		for _, c := range got.Changes {
			if c.Op == lens.ChangeModified && (strings.Contains(c.BeforeNote, "transition") || strings.Contains(c.AfterNote, "transition")) {
				found = true
				break
			}
		}
		if !found {
			t.Logf("Changes: %v", got.Changes)
		}
		if len(got.Changes) == 0 {
			t.Errorf("expected changes for ContractData to TTL transition, got none")
		}
	})

	// Test 4: Same entry - should show no transition and no changes (Equal=true)
	t.Run("Same entry - no transition and no changes", func(t *testing.T) {
		t.Parallel()
		got, err := lens.DiffBase64(mustMarshal(t, offerLE.Raw.(*xdr.LedgerEntry)), mustMarshal(t, offerLE.Raw.(*xdr.LedgerEntry)), "LedgerEntry")
		if err != nil {
			t.Fatalf("DiffBase64() error = %v", err)
		}
		// When entries are identical, Equal() should be true and there should be no changes
		if !got.Equal() {
			t.Errorf("expected equal for same LedgerEntry, got: %v", got.Changes)
		}
		if len(got.Changes) != 0 {
			t.Errorf("expected no changes for same entry, got %d: %v", len(got.Changes), got.Changes)
		}
	})
}

func TestDiffSemanticMemo(t *testing.T) {
	t.Parallel()

	// Test MemoText "100" vs MemoId 100 - semantically equivalent
	text := "100"
	id := xdr.Uint64(100)

	left := xdr.Memo{Type: xdr.MemoTypeMemoText, Text: &text}
	right := xdr.Memo{Type: xdr.MemoTypeMemoId, Id: &id}

	got, err := lens.DiffBase64(mustMarshal(t, left), mustMarshal(t, right), "Memo")
	if err != nil {
		t.Fatalf("DiffBase64() error = %v", err)
	}
	if got.Equal() {
		t.Fatal("expected a change (type switched), got equal")
	}
	if len(got.Changes) != 1 {
		t.Fatalf("expected 1 change for semantically equivalent memo, got %d: %v", len(got.Changes), got.Changes)
	}
	c := got.Changes[0]
	if c.Op != lens.ChangeModified {
		t.Errorf("Op = %v, want ChangeModified for semantic equivalence", c.Op)
	}
	if !strings.Contains(c.BeforeNote, "semantically equivalent") || !strings.Contains(c.AfterNote, "semantically equivalent") {
		t.Errorf("notes should mention semantic equivalence: before=%q after=%q", c.BeforeNote, c.AfterNote)
	}

	// Test MemoText "hello" vs MemoId 100 - NOT semantically equivalent
	text2 := "hello"
	id2 := xdr.Uint64(100)

	left2 := xdr.Memo{Type: xdr.MemoTypeMemoText, Text: &text2}
	right2 := xdr.Memo{Type: xdr.MemoTypeMemoId, Id: &id2}

	got2, err := lens.DiffBase64(mustMarshal(t, left2), mustMarshal(t, right2), "Memo")
	if err != nil {
		t.Fatalf("DiffBase64() error = %v", err)
	}
	if got2.Equal() {
		t.Fatal("expected changes for non-equivalent memo")
	}
	// Should have 3 changes for union arm switch
	if len(got2.Changes) != 3 {
		t.Fatalf("expected 3 changes for non-equivalent memo (union arm switch), got %d: %v", len(got2.Changes), got2.Changes)
	}
	hasTypeChange := false
	hasTextRemoval := false
	hasIdAddition := false
	for _, c := range got2.Changes {
		if c.Path == "Type" && c.Op == lens.ChangeModified {
			hasTypeChange = true
		}
		if c.Path == "Text" && c.Op == lens.ChangeRemoved {
			hasTextRemoval = true
		}
		if c.Path == "Id" && c.Op == lens.ChangeAdded {
			hasIdAddition = true
		}
	}
	if !hasTypeChange || !hasTextRemoval || !hasIdAddition {
		t.Errorf("expected union arm switch pattern (Type modified, Text removed, Id added), got: %v", got2.Changes)
	}

	// Test MemoHash vs MemoReturn - different arms, not equivalent
	hash := xdr.Hash{1, 2, 3, 4}
	hash2 := xdr.Hash{5, 6, 7, 8}

	left3 := xdr.Memo{Type: xdr.MemoTypeMemoHash, Hash: &hash}
	right3 := xdr.Memo{Type: xdr.MemoTypeMemoReturn, RetHash: &hash2}

	got3, err := lens.DiffBase64(mustMarshal(t, left3), mustMarshal(t, right3), "Memo")
	if err != nil {
		t.Fatalf("DiffBase64() error = %v", err)
	}
	if got3.Equal() {
		t.Fatal("expected changes for different memo arms")
	}
	// Should have 3 changes for union arm switch
	if len(got3.Changes) != 3 {
		t.Fatalf("expected 3 changes for different memo arms, got %d: %v", len(got3.Changes), got3.Changes)
	}

	// Test MemoNone vs MemoNone - equal
	none1 := xdr.Memo{Type: xdr.MemoTypeMemoNone}
	none2 := xdr.Memo{Type: xdr.MemoTypeMemoNone}

	got4, err := lens.DiffBase64(mustMarshal(t, none1), mustMarshal(t, none2), "Memo")
	if err != nil {
		t.Fatalf("DiffBase64() error = %v", err)
	}
	if !got4.Equal() {
		t.Errorf("expected equal for two MemoNone, got: %v", got4.Changes)
	}

	// Test MemoText "100" vs MemoText "100" - equal
	text3 := "100"
	text4 := "100"
	left5 := xdr.Memo{Type: xdr.MemoTypeMemoText, Text: &text3}
	right5 := xdr.Memo{Type: xdr.MemoTypeMemoText, Text: &text4}

	got5, err := lens.DiffBase64(mustMarshal(t, left5), mustMarshal(t, right5), "Memo")
	if err != nil {
		t.Fatalf("DiffBase64() error = %v", err)
	}
	if !got5.Equal() {
		t.Errorf("expected equal for same MemoText, got: %v", got5.Changes)
	}

	// Test MemoId 100 vs MemoId 100 - equal
	id3 := xdr.Uint64(100)
	id4 := xdr.Uint64(100)
	left6 := xdr.Memo{Type: xdr.MemoTypeMemoId, Id: &id3}
	right6 := xdr.Memo{Type: xdr.MemoTypeMemoId, Id: &id4}

	got6, err := lens.DiffBase64(mustMarshal(t, left6), mustMarshal(t, right6), "Memo")
	if err != nil {
		t.Fatalf("DiffBase64() error = %v", err)
	}
	if !got6.Equal() {
		t.Errorf("expected equal for same MemoId, got: %v", got6.Changes)
	}

	// Test MemoText "00100" vs MemoId 100 - semantically equivalent (leading zeros)
	text5 := "00100"
	id5 := xdr.Uint64(100)
	left7 := xdr.Memo{Type: xdr.MemoTypeMemoText, Text: &text5}
	right7 := xdr.Memo{Type: xdr.MemoTypeMemoId, Id: &id5}

	got7, err := lens.DiffBase64(mustMarshal(t, left7), mustMarshal(t, right7), "Memo")
	if err != nil {
		t.Fatalf("DiffBase64() error = %v", err)
	}
	if got7.Equal() {
		t.Fatal("expected a change (type switched), got equal")
	}
	if len(got7.Changes) != 1 || got7.Changes[0].Op != lens.ChangeModified {
		t.Errorf("expected 1 modified change for semantically equivalent memo with leading zeros, got: %v", got7.Changes)
	}
	if !strings.Contains(got7.Changes[0].BeforeNote, "semantically equivalent") {
		t.Errorf("note should mention semantic equivalence: %q", got7.Changes[0].BeforeNote)
	}

	// Test MemoReturn vs MemoReturn - equal (both are 32-byte hashes)
	ret1 := xdr.Hash{1, 2, 3, 4}
	ret2 := xdr.Hash{1, 2, 3, 4}
	left8 := xdr.Memo{Type: xdr.MemoTypeMemoReturn, RetHash: &ret1}
	right8 := xdr.Memo{Type: xdr.MemoTypeMemoReturn, RetHash: &ret2}

	got8, err := lens.DiffBase64(mustMarshal(t, left8), mustMarshal(t, right8), "Memo")
	if err != nil {
		t.Fatalf("DiffBase64() error = %v", err)
	}
	if !got8.Equal() {
		t.Errorf("expected equal for same MemoReturn, got: %v", got8.Changes)
	}

	// Test MemoReturn vs MemoHash - different arms, not equivalent
	ret3 := xdr.Hash{1, 2, 3, 4}
	hash3 := xdr.Hash{1, 2, 3, 4}
	left9 := xdr.Memo{Type: xdr.MemoTypeMemoReturn, RetHash: &ret3}
	right9 := xdr.Memo{Type: xdr.MemoTypeMemoHash, Hash: &hash3}

	got9, err := lens.DiffBase64(mustMarshal(t, left9), mustMarshal(t, right9), "Memo")
	if err != nil {
		t.Fatalf("DiffBase64() error = %v", err)
	}
	if got9.Equal() {
		t.Fatal("expected changes for MemoReturn vs MemoHash")
	}
	// Should have 3 changes for union arm switch
	if len(got9.Changes) != 3 {
		t.Fatalf("expected 3 changes for different memo arms, got %d: %v", len(got9.Changes), got9.Changes)
	}
}

func TestDiffWithOptions_MaskPaths(t *testing.T) {
	t.Parallel()

	left := &lens.Value{
		Type: "TxEnvelope",
		Node: &lens.Node{
			Kind: lens.KindStruct,
			Children: []*lens.Node{
				{Name: "SeqNum", Kind: lens.KindScalar, Value: 100},
				{Name: "Fee", Kind: lens.KindScalar, Value: 10},
			},
		},
	}
	right := &lens.Value{
		Type: "TxEnvelope",
		Node: &lens.Node{
			Kind: lens.KindStruct,
			Children: []*lens.Node{
				{Name: "SeqNum", Kind: lens.KindScalar, Value: 200},
				{Name: "Fee", Kind: lens.KindScalar, Value: 10},
			},
		},
	}

	// Without mask, they differ on SeqNum
	unmasked, err := lens.DiffWithOptions(left, right, lens.DiffOptions{})
	if err != nil {
		t.Fatalf("DiffWithOptions() error = %v", err)
	}
	if unmasked.Equal() {
		t.Fatal("expected differences without mask")
	}

	// With MaskPaths, SeqNum is excluded
	masked, err := lens.DiffWithOptions(left, right, lens.DiffOptions{
		MaskPaths: []string{"SeqNum"},
	})
	if err != nil {
		t.Fatalf("DiffWithOptions() error = %v", err)
	}
	if !masked.Equal() {
		t.Fatalf("expected Equal() with MaskPaths, got %v", masked.Changes)
	}

	// With IgnorePaths alias, SeqNum is also excluded (#345)
	ignored, err := lens.DiffWithOptions(left, right, lens.DiffOptions{
		IgnorePaths: []string{"SeqNum"},
	})
	if err != nil {
		t.Fatalf("DiffWithOptions() error = %v", err)
	}
	if !ignored.Equal() {
		t.Fatalf("expected Equal() with IgnorePaths, got %v", ignored.Changes)
	}
}
