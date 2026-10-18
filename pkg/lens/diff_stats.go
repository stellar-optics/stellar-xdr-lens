package lens

import (
	"fmt"
	"strconv"
	"strings"
)

// DiffStats is a compact statistical summary of a structural diff.
//
// Where DiffResult lists every changed path, DiffStats answers the
// higher-level question a reviewer or a CI job asks first: how much changed,
// what kind of changes they were, and which parts of the value moved. It is
// derived entirely from an existing DiffResult and adds no new decoding, so it
// stays offline and deterministic.
type DiffStats struct {
	// LeftType and RightType are the XDR types that were compared.
	LeftType  string `json:"leftType"`
	RightType string `json:"rightType"`
	// Equal reports whether the two values were identical.
	Equal bool `json:"equal"`
	// Total is the number of changes across every kind.
	Total int `json:"total"`
	// Modified counts paths present on both sides with different values.
	Modified int `json:"modified"`
	// Added counts paths that exist only on the right-hand side.
	Added int `json:"added"`
	// Removed counts paths that exist only on the left-hand side.
	Removed int `json:"removed"`
	// TypeChanged counts paths whose value changed kind, such as a union
	// switching arms.
	TypeChanged int `json:"typeChanged"`
	// Sections aggregates the changes by their top-level path segment, in
	// the order they first appear in the diff.
	Sections []DiffSection `json:"sections,omitempty"`
	// DeepestPath is the path of the most deeply nested change. It is empty
	// when there are no changes.
	DeepestPath string `json:"deepestPath,omitempty"`
}

// DiffSection aggregates the changes that fall under one top-level path
// segment of a compared value, for example "V1" in a transaction envelope or
// "Result" in a transaction result.
type DiffSection struct {
	// Path is the top-level segment, or "(root)" for changes at the root.
	Path string `json:"path"`
	// Total is the number of changes under this section.
	Total int `json:"total"`
	// Modified, Added, Removed and TypeChanged break Total down by kind.
	Modified    int `json:"modified"`
	Added       int `json:"added"`
	Removed     int `json:"removed"`
	TypeChanged int `json:"typeChanged"`
}

// SummarizeDiff computes statistics for a DiffResult. A nil result is treated
// as no differences, which makes the helper safe to call on the optional
// output of a comparison.
func SummarizeDiff(result *DiffResult) DiffStats {
	if result == nil {
		return DiffStats{Equal: true}
	}
	return SummarizeChanges(result.LeftType, result.RightType, result.Changes)
}

// SummarizeChanges computes statistics for a raw change list. It is the
// primitive behind SummarizeDiff and is useful to callers that already hold a
// []Change rather than a DiffResult.
func SummarizeChanges(leftType, rightType string, changes []Change) DiffStats {
	stats := DiffStats{
		LeftType:  leftType,
		RightType: rightType,
		Total:     len(changes),
		Equal:     len(changes) == 0,
	}

	var order []string
	byPath := make(map[string]*DiffSection, len(changes))
	deepestDepth := -1

	for _, change := range changes {
		stats.Modified, stats.Added, stats.Removed, stats.TypeChanged = addChange(
			stats.Modified, stats.Added, stats.Removed, stats.TypeChanged, change.Op)

		section := topLevelSection(change.Path)
		aggregate, ok := byPath[section]
		if !ok {
			aggregate = &DiffSection{Path: section}
			byPath[section] = aggregate
			order = append(order, section)
		}
		aggregate.Modified, aggregate.Added, aggregate.Removed, aggregate.TypeChanged = addChange(
			aggregate.Modified, aggregate.Added, aggregate.Removed, aggregate.TypeChanged, change.Op)
		aggregate.Total++

		if depth := pathDepth(change.Path); depth > deepestDepth {
			deepestDepth = depth
			stats.DeepestPath = change.Path
		}
	}

	if len(order) > 0 {
		stats.Sections = make([]DiffSection, 0, len(order))
		for _, path := range order {
			stats.Sections = append(stats.Sections, *byPath[path])
		}
	}
	return stats
}

// addChange folds one change operation into a running set of counters.
func addChange(modified, added, removed, typeChanged int, op ChangeOp) (int, int, int, int) {
	switch op {
	case ChangeAdded:
		added++
	case ChangeRemoved:
		removed++
	case ChangeTypeChanged:
		typeChanged++
	default:
		modified++
	}
	return modified, added, removed, typeChanged
}

// Section returns the aggregate for a top-level path segment, if present.
func (s DiffStats) Section(path string) (DiffSection, bool) {
	for _, section := range s.Sections {
		if section.Path == path {
			return section, true
		}
	}
	return DiffSection{}, false
}

// Summary renders a one-line description of the statistics, suitable for a CI
// log. It mirrors DiffResult.Summary but includes type changes explicitly and
// the total number of changes.
func (s DiffStats) Summary() string {
	if s.Total == 0 {
		return "no differences"
	}
	parts := make([]string, 0, 4)
	if s.Modified > 0 {
		parts = append(parts, strconv.Itoa(s.Modified)+" modified")
	}
	if s.Added > 0 {
		parts = append(parts, strconv.Itoa(s.Added)+" added")
	}
	if s.Removed > 0 {
		parts = append(parts, strconv.Itoa(s.Removed)+" removed")
	}
	if s.TypeChanged > 0 {
		parts = append(parts, strconv.Itoa(s.TypeChanged)+" type changed")
	}
	return fmt.Sprintf("%d change(s): %s", s.Total, strings.Join(parts, ", "))
}

// String renders a multi-line report: the compared type, the one-line
// summary, and the per-section breakdown. Sections are emitted in the order
// they first appear in the diff, so the output is deterministic.
func (s DiffStats) String() string {
	var b strings.Builder
	header := s.LeftType
	switch {
	case s.RightType != "" && s.RightType != s.LeftType:
		header = s.LeftType + " vs " + s.RightType
	case header == "":
		header = s.RightType
	}
	if header != "" {
		b.WriteString(header)
		b.WriteString("\n")
	}
	b.WriteString(s.Summary())
	for _, section := range s.Sections {
		fmt.Fprintf(&b, "\n  %s: %d", section.Path, section.Total)
	}
	return b.String()
}

// topLevelSection returns the first path segment of a change path, or
// "(root)" when the change sits at the root. List indexes are rendered as
// "[N]" so changes to the elements of a top-level list group together.
func topLevelSection(path string) string {
	segments := splitPath(path)
	if len(segments) == 0 {
		return "(root)"
	}
	first := segments[0]
	if first.name == "" && first.index >= 0 {
		return "[" + strconv.Itoa(first.index) + "]"
	}
	return first.name
}

// pathDepth counts the segments in a change path, used to find the deepest
// change. An empty path has depth 0.
func pathDepth(path string) int {
	return len(splitPath(path))
}
