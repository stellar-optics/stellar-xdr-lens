package lens_test

import (
	"strings"
	"testing"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

func TestSummarizeDiffNil(t *testing.T) {
	t.Parallel()

	stats := lens.SummarizeDiff(nil)
	if !stats.Equal {
		t.Error("Equal = false, want true for a nil diff")
	}
	if stats.Total != 0 {
		t.Errorf("Total = %d, want 0", stats.Total)
	}
	if got := stats.Summary(); got != "no differences" {
		t.Errorf("Summary() = %q, want %q", got, "no differences")
	}
}

func TestSummarizeChangesCountsAndSections(t *testing.T) {
	t.Parallel()

	changes := []lens.Change{
		{Path: "Result.Code", Op: lens.ChangeModified},
		{Path: "Result.Code", Op: lens.ChangeTypeChanged},
		{Path: "Signatures", Op: lens.ChangeAdded},
		{Path: "Signatures", Op: lens.ChangeRemoved},
		{Path: "V1.Tx.Operations[0].Body.PaymentOp.Amount", Op: lens.ChangeModified},
	}
	stats := lens.SummarizeChanges("TransactionEnvelope", "TransactionEnvelope", changes)

	if stats.Total != 5 {
		t.Fatalf("Total = %d, want 5", stats.Total)
	}
	if stats.Modified != 2 || stats.TypeChanged != 1 || stats.Added != 1 || stats.Removed != 1 {
		t.Errorf("counts = modified:%d typeChanged:%d added:%d removed:%d, want 2/1/1/1",
			stats.Modified, stats.TypeChanged, stats.Added, stats.Removed)
	}
	if stats.Equal {
		t.Error("Equal = true, want false")
	}
	if stats.DeepestPath != "V1.Tx.Operations[0].Body.PaymentOp.Amount" {
		t.Errorf("DeepestPath = %q, want the deepest path", stats.DeepestPath)
	}
	if len(stats.Sections) != 3 {
		t.Fatalf("Sections = %d, want 3: %+v", len(stats.Sections), stats.Sections)
	}
	if stats.Sections[0].Path != "Result" || stats.Sections[1].Path != "Signatures" || stats.Sections[2].Path != "V1" {
		t.Errorf("section order = %v, want Result, Signatures, V1", sectionPaths(stats))
	}
	if stats.Sections[0].Total != 2 || stats.Sections[0].Modified != 1 || stats.Sections[0].TypeChanged != 1 {
		t.Errorf("Result section = %+v, want 2 changes (1 modified, 1 type changed)", stats.Sections[0])
	}

	section, ok := stats.Section("Signatures")
	if !ok || section.Added != 1 || section.Removed != 1 {
		t.Errorf("Section(Signatures) = %+v, %v", section, ok)
	}
	if _, ok := stats.Section("Nope"); ok {
		t.Error("Section(Nope) reported present")
	}
}

func TestSummarizeChangesRootAndListSections(t *testing.T) {
	t.Parallel()

	stats := lens.SummarizeChanges("A", "B", []lens.Change{
		{Path: "", Op: lens.ChangeModified},
		{Path: "[0]", Op: lens.ChangeAdded},
	})
	if len(stats.Sections) != 2 {
		t.Fatalf("Sections = %+v, want two", stats.Sections)
	}
	if stats.Sections[0].Path != "(root)" {
		t.Errorf("first section = %q, want (root)", stats.Sections[0].Path)
	}
	if stats.Sections[1].Path != "[0]" {
		t.Errorf("second section = %q, want [0]", stats.Sections[1].Path)
	}
}

func TestSummarizeDiffFromRealFixtures(t *testing.T) {
	t.Parallel()

	diff, err := lens.DiffBase64(
		loadFixture(t, "tx_soroban.res"),
		loadFixture(t, "tx_failed.res"),
		"",
	)
	if err != nil {
		t.Fatalf("DiffBase64() error = %v", err)
	}

	stats := lens.SummarizeDiff(diff)
	if stats.Total != len(diff.Changes) {
		t.Errorf("Total = %d, want %d", stats.Total, len(diff.Changes))
	}
	if stats.Modified+stats.Added+stats.Removed+stats.TypeChanged != stats.Total {
		t.Errorf("kind counts do not sum to Total: %+v", stats)
	}
	if stats.Equal {
		t.Error("Equal = true for two different results")
	}
	if stats.LeftType != "TransactionResult" || stats.RightType != "TransactionResult" {
		t.Errorf("types = %s/%s, want TransactionResult on both sides", stats.LeftType, stats.RightType)
	}
	if len(stats.Sections) == 0 {
		t.Error("Sections is empty, want at least one section")
	}
	if !strings.Contains(stats.String(), "TransactionResult") {
		t.Errorf("String() = %q, want it to name the compared type", stats.String())
	}
	if !strings.Contains(stats.Summary(), "change(s)") {
		t.Errorf("Summary() = %q, want it to count changes", stats.Summary())
	}
}

func TestSummarizeDiffIdenticalFixture(t *testing.T) {
	t.Parallel()

	payload := loadFixture(t, "tx_success.res")
	diff, err := lens.DiffBase64(payload, payload, "")
	if err != nil {
		t.Fatalf("DiffBase64() error = %v", err)
	}
	stats := lens.SummarizeDiff(diff)
	if !stats.Equal || stats.Total != 0 {
		t.Errorf("stats = %+v, want equal with zero changes", stats)
	}
	if !strings.Contains(stats.String(), "no differences") {
		t.Errorf("String() = %q, want no differences", stats.String())
	}
}

func TestSummarizeStatsStringTypeMismatch(t *testing.T) {
	t.Parallel()

	stats := lens.SummarizeChanges("TransactionEnvelope", "TransactionResult", []lens.Change{
		{Path: "Root", Op: lens.ChangeModified},
	})
	if !strings.Contains(stats.String(), "TransactionEnvelope vs TransactionResult") {
		t.Errorf("String() = %q, want it to show both types", stats.String())
	}
}

func sectionPaths(stats lens.DiffStats) []string {
	out := make([]string, 0, len(stats.Sections))
	for _, s := range stats.Sections {
		out = append(out, s.Path)
	}
	return out
}
