package format

import (
	"strings"
	"testing"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

func TestAlignLinesEdgeCases(t *testing.T) {
	t.Parallel()

	if got := alignLines(nil, nil); len(got) != 0 {
		t.Errorf("alignLines(nil,nil) = %v, want empty", got)
	}

	deletes := alignLines([]string{"a"}, nil)
	if len(deletes) != 1 || deletes[0].kind != lineDelete || deletes[0].left != "a" {
		t.Errorf("alignLines left-only = %+v", deletes)
	}

	inserts := alignLines(nil, []string{"a"})
	if len(inserts) != 1 || inserts[0].kind != lineInsert || inserts[0].right != "a" {
		t.Errorf("alignLines right-only = %+v", inserts)
	}

	mixed := alignLines([]string{"a", "b"}, []string{"a", "c"})
	want := []lineKind{lineEqual, lineDelete, lineInsert}
	if len(mixed) != len(want) {
		t.Fatalf("alignLines mixed = %+v, want %d pairs", mixed, len(want))
	}
	for i, kind := range want {
		if mixed[i].kind != kind {
			t.Errorf("pair %d kind = %v, want %v", i, mixed[i].kind, kind)
		}
	}
}

// TestAlignLinesFallsBackWhenTooLarge covers the bounded-allocation guard.
func TestAlignLinesFallsBackWhenTooLarge(t *testing.T) {
	t.Parallel()

	lines := make([]string, 1025)
	for i := range lines {
		lines[i] = "x"
	}
	got := alignLines(lines, lines)
	if len(got) != len(lines)*2 {
		t.Fatalf("fallback alignment has %d pairs, want %d", len(got), len(lines)*2)
	}
	if got[0].kind != lineDelete {
		t.Errorf("fallback first kind = %v, want delete", got[0].kind)
	}
}

func TestBuildHunksMergesAndSplits(t *testing.T) {
	t.Parallel()

	pairs := []alignedPair{
		{kind: lineEqual, left: "0", right: "0"},
		{kind: lineDelete, left: "1"},
		{kind: lineEqual, left: "2", right: "2"},
		{kind: lineEqual, left: "3", right: "3"},
		{kind: lineEqual, left: "4", right: "4"},
		{kind: lineEqual, left: "5", right: "5"},
		{kind: lineInsert, right: "6"},
		{kind: lineEqual, left: "7", right: "7"},
	}

	tight := buildHunks(pairs, 1)
	if len(tight) != 2 {
		t.Fatalf("context 1 produced %d hunk(s), want 2: %+v", len(tight), tight)
	}
	if tight[0].lines[1].kind != lineDelete {
		t.Errorf("first hunk does not contain the deletion: %+v", tight[0])
	}

	loose := buildHunks(pairs, 3)
	if len(loose) != 1 {
		t.Fatalf("context 3 produced %d hunk(s), want 1: %+v", len(loose), loose)
	}
}

func TestBuildHunksEmptyRangeStart(t *testing.T) {
	t.Parallel()

	// A single insertion at the start of an empty document: the old range is
	// empty and must be reported as "0,0".
	pairs := []alignedPair{{kind: lineInsert, right: "new"}}
	hunks := buildHunks(pairs, 3)
	if len(hunks) != 1 {
		t.Fatalf("buildHunks() = %+v, want one hunk", hunks)
	}
	if hunks[0].oldStart != 0 || hunks[0].oldCount != 0 {
		t.Errorf("old range = (%d,%d), want (0,0)", hunks[0].oldStart, hunks[0].oldCount)
	}
	if hunks[0].newStart != 1 || hunks[0].newCount != 1 {
		t.Errorf("new range = (%d,%d), want (1,1)", hunks[0].newStart, hunks[0].newCount)
	}
}

func TestFormatRange(t *testing.T) {
	t.Parallel()

	if got := formatRange(5, 1); got != "5" {
		t.Errorf("formatRange(5,1) = %q, want 5", got)
	}
	if got := formatRange(0, 0); got != "0,0" {
		t.Errorf("formatRange(0,0) = %q, want 0,0", got)
	}
	if got := formatRange(2, 4); got != "2,4" {
		t.Errorf("formatRange(2,4) = %q, want 2,4", got)
	}
}

func TestAllEqual(t *testing.T) {
	t.Parallel()

	if !allEqual(nil) {
		t.Error("allEqual(nil) = false, want true")
	}
	if !allEqual([]alignedPair{{kind: lineEqual}}) {
		t.Error("allEqual(equal) = false, want true")
	}
	if allEqual([]alignedPair{{kind: lineDelete}}) {
		t.Error("allEqual(delete) = true, want false")
	}
}

func TestHunkRowsZipsChanges(t *testing.T) {
	t.Parallel()

	rows := hunkRows([]alignedPair{
		{kind: lineEqual, left: "a", right: "a"},
		{kind: lineDelete, left: "b"},
		{kind: lineInsert, right: "B"},
		{kind: lineEqual, left: "c", right: "c"},
	})
	if len(rows) != 3 {
		t.Fatalf("hunkRows() = %+v, want 3 rows", rows)
	}
	if rows[1].left != "b" || !rows[1].leftChanged || rows[1].right != "B" || !rows[1].rightChanged {
		t.Errorf("change row = %+v, want b paired with B", rows[1])
	}

	// A removal with no matching addition leaves the right cell empty.
	rows = hunkRows([]alignedPair{{kind: lineDelete, left: "gone"}})
	if len(rows) != 1 || rows[0].left != "gone" || rows[0].right != "" || rows[0].rightChanged {
		t.Errorf("unpaired removal = %+v", rows)
	}
}

func TestPadCell(t *testing.T) {
	t.Parallel()

	if got := padCell("ab", 4); got != "ab  " {
		t.Errorf("padCell(ab,4) = %q, want %q", got, "ab  ")
	}
	if got := padCell("abcdef", 4); !strings.HasSuffix(got, "…") || len([]rune(got)) != 4 {
		t.Errorf("padCell(abcdef,4) = %q, want a 4-rune ellipsised cell", got)
	}
	if got := padCell("abc", 1); got != "…" {
		t.Errorf("padCell(abc,1) = %q, want ellipsis", got)
	}
	if got := padCell("abc", 0); got != "" {
		t.Errorf("padCell(abc,0) = %q, want empty", got)
	}
}

func TestFlattenValue(t *testing.T) {
	t.Parallel()

	if lines := flattenValue(nil); lines != nil {
		t.Errorf("flattenValue(nil) = %v, want nil", lines)
	}
}

func TestFilterAlignment(t *testing.T) {
	t.Parallel()

	pairs := []alignedPair{
		{kind: lineEqual, left: "a", right: "a"},
		{kind: lineDelete, left: "Keep = 1"},
		{kind: lineInsert, right: "Drop = 2"},
		{kind: lineDelete, left: "NoSep"},
	}

	if got := filterAlignment(pairs, nil); len(got) != len(pairs) {
		t.Errorf("nil filter changed the alignment: %d pairs", len(got))
	}

	filter, err := lens.NewPathFilter([]string{"Keep"}, nil)
	if err != nil {
		t.Fatalf("NewPathFilter() error = %v", err)
	}
	got := filterAlignment(pairs, filter)
	if len(got) != 2 {
		t.Fatalf("filterAlignment() = %+v, want context plus Keep", got)
	}
	if got[0].kind != lineEqual || got[1].left != "Keep = 1" {
		t.Errorf("filterAlignment() = %+v", got)
	}
}

func TestLinePath(t *testing.T) {
	t.Parallel()

	if got := linePath("A.B = value"); got != "A.B" {
		t.Errorf("linePath() = %q, want A.B", got)
	}
	if got := linePath("NoSeparator"); got != "NoSeparator" {
		t.Errorf("linePath() = %q, want NoSeparator", got)
	}
}
