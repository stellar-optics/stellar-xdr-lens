package lens_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

func TestNewPathFilterRejectsMalformedPattern(t *testing.T) {
	t.Parallel()

	_, err := lens.NewPathFilter([]string{"Result.["}, nil)
	if err == nil {
		t.Fatal("NewPathFilter() error = nil, want an error for a malformed pattern")
	}
	if !errors.Is(err, lens.ErrInvalidArgument) {
		t.Errorf("error = %v, want it to wrap ErrInvalidArgument", err)
	}
}

func TestNewPathFilterSplitsAndTrimsCommas(t *testing.T) {
	t.Parallel()

	filter, err := lens.NewPathFilter([]string{" FeeCharged , Result.Code "}, nil)
	if err != nil {
		t.Fatalf("NewPathFilter() error = %v", err)
	}
	if !filter.Matches("FeeCharged") {
		t.Error("filter does not select FeeCharged")
	}
	if !filter.Matches("Result.Code") {
		t.Error("filter does not select Result.Code")
	}
	if filter.Matches("Result.InnerResultPair.TransactionHash") {
		t.Error("filter selected an excluded path")
	}
}

func TestPathFilterEmptySelectsEverything(t *testing.T) {
	t.Parallel()

	var nilFilter *lens.PathFilter
	if !nilFilter.Empty() || !nilFilter.Matches("Anything") {
		t.Error("nil filter should select every path")
	}

	empty, err := lens.NewPathFilter(nil, nil)
	if err != nil {
		t.Fatalf("NewPathFilter() error = %v", err)
	}
	if !empty.Empty() || !empty.Matches("Anything") {
		t.Error("empty filter should select every path")
	}
}

func TestPathFilterMatchingSemantics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		include []string
		exclude []string
		path    string
		want    bool
	}{
		{"exact include", []string{"Result.Code"}, nil, "Result.Code", true},
		{"include misses", []string{"Result.Code"}, nil, "FeeCharged", false},
		{"include selects subtree", []string{"Result"}, nil, "Result.Code", true},
		{"include double star subtree", []string{"Result.**"}, nil, "Result.Code", true},
		{"deep glob suffix", []string{"**.Code"}, nil, "Result.Inner.Code", true},
		{"bracket index glob", []string{"Operations[*].Body"}, nil, "Operations[0].Body", true},
		{"bracket index mismatch", []string{"Operations[*].Body"}, nil, "Operations[0].Source", false},
		{"exclude wins over include", []string{"Result"}, []string{"Result.Code"}, "Result.Code", false},
		{"exclude keeps siblings", []string{"Result"}, []string{"Result.Code"}, "Result.FeeCharged", true},
		{"exclude only", nil, []string{"Signatures"}, "Signatures", false},
		{"exclude only keeps others", nil, []string{"Signatures"}, "Result", true},
		{"double star anywhere", []string{"**.Signatures"}, nil, "V1.Signatures", true},
		{"no match at all", []string{"A.B"}, nil, "A.C", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			filter, err := lens.NewPathFilter(tc.include, tc.exclude)
			if err != nil {
				t.Fatalf("NewPathFilter() error = %v", err)
			}
			if got := filter.Matches(tc.path); got != tc.want {
				t.Errorf("Matches(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

func TestFilterChangesPreservesOrderAndSelection(t *testing.T) {
	t.Parallel()

	changes := []lens.Change{
		{Path: "FeeCharged", Op: lens.ChangeModified},
		{Path: "Result.Code", Op: lens.ChangeModified},
		{Path: "Result.Results[0].Code", Op: lens.ChangeRemoved},
	}

	filter, err := lens.NewPathFilter(nil, []string{"FeeCharged"})
	if err != nil {
		t.Fatalf("NewPathFilter() error = %v", err)
	}

	got := lens.FilterChanges(changes, filter)
	if len(got) != 2 {
		t.Fatalf("FilterChanges() returned %d change(s), want 2", len(got))
	}
	if got[0].Path != "Result.Code" || got[1].Path != "Result.Results[0].Code" {
		t.Errorf("FilterChanges() order = %v", []string{got[0].Path, got[1].Path})
	}

	if unfiltered := lens.FilterChanges(changes, nil); len(unfiltered) != len(changes) {
		t.Errorf("nil filter changed %d change(s)", len(changes)-len(unfiltered))
	}
}

// TestDiffWithOptionsFilterIncludesNarrowsOutput exercises the filter through
// the public DiffWithOptions entry point against real captured XDR.
func TestDiffWithOptionsFilterIncludesNarrowsOutput(t *testing.T) {
	t.Parallel()

	left := loadFixture(t, "tx_soroban.res")
	right := loadFixture(t, "tx_failed.res")

	unfiltered, err := lens.DiffBase64(left, right, "")
	if err != nil {
		t.Fatalf("DiffBase64() error = %v", err)
	}
	if len(unfiltered.Changes) < 2 {
		t.Fatalf("fixtures produced %d change(s); the test needs at least two", len(unfiltered.Changes))
	}

	filter, err := lens.NewPathFilter([]string{"Result.Code"}, nil)
	if err != nil {
		t.Fatalf("NewPathFilter() error = %v", err)
	}

	leftValue, err := lens.Decode(left)
	if err != nil {
		t.Fatalf("Decode(left) error = %v", err)
	}
	rightValue, err := lens.Decode(right)
	if err != nil {
		t.Fatalf("Decode(right) error = %v", err)
	}

	result, err := lens.DiffWithOptions(leftValue, rightValue, lens.DiffOptions{Filter: filter})
	if err != nil {
		t.Fatalf("DiffWithOptions() error = %v", err)
	}
	if len(result.Changes) != 1 {
		t.Fatalf("filtered diff has %d change(s), want 1: %v", len(result.Changes), changePaths(result.Changes))
	}
	if result.Changes[0].Path != "Result.Code" {
		t.Errorf("filtered change path = %q, want Result.Code", result.Changes[0].Path)
	}
}

func TestDiffResultFilterExcludesAndDoesNotMutate(t *testing.T) {
	t.Parallel()

	left, err := lens.Decode(loadFixture(t, "tx_soroban.res"))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	right, err := lens.Decode(loadFixture(t, "tx_failed.res"))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	result, err := lens.Diff(left, right)
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}
	original := len(result.Changes)

	filter, err := lens.NewPathFilter(nil, []string{"Result.Code"})
	if err != nil {
		t.Fatalf("NewPathFilter() error = %v", err)
	}
	filtered := result.Filter(filter)

	if len(result.Changes) != original {
		t.Errorf("Filter() mutated the receiver: %d changes, want %d", len(result.Changes), original)
	}
	for _, change := range filtered.Changes {
		if change.Path == "Result.Code" {
			t.Errorf("excluded path Result.Code is still present")
		}
	}
	if filtered == result {
		t.Error("Filter() returned the same pointer, want a copy")
	}
	if result.Filter(nil) != result {
		t.Error("Filter(nil) should return the receiver")
	}
}

func TestFilterCanProduceEqualResult(t *testing.T) {
	t.Parallel()

	left, err := lens.Decode(loadFixture(t, "tx_soroban.res"))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	right, err := lens.Decode(loadFixture(t, "tx_failed.res"))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	// Excluding everything with ** should yield an "equal" diff.
	filter, err := lens.NewPathFilter(nil, []string{"**"})
	if err != nil {
		t.Fatalf("NewPathFilter() error = %v", err)
	}
	result, err := lens.DiffWithOptions(left, right, lens.DiffOptions{Filter: filter})
	if err != nil {
		t.Fatalf("DiffWithOptions() error = %v", err)
	}
	if !result.Equal() {
		t.Errorf("excluding everything still reported %d change(s): %v",
			len(result.Changes), changePaths(result.Changes))
	}
}

func changePaths(changes []lens.Change) string {
	parts := make([]string, 0, len(changes))
	for _, c := range changes {
		parts = append(parts, c.Path)
	}
	return strings.Join(parts, ", ")
}
