package lens

import (
	"fmt"
	"path"
	"strings"
)

// PathFilter selects which changed paths a structural diff reports.
//
// A filter is built from include and exclude patterns. A pattern is a
// dot-separated field path with optional [index] segments, for example
// "V1.Tx.SeqNum" or "V1.Tx.Operations[0].Body". Within a segment the usual
// shell glob metacharacters are understood (*, ?, [...]), and a segment of
// "**" matches any number of path segments.
//
// A pattern matches a change when it matches the change's path or a prefix of
// it, so "V1.Tx" selects every change anywhere beneath V1.Tx while
// "V1.Tx.SeqNum" selects exactly that scalar. An empty include set selects
// everything. A path is reported when it matches at least one include pattern
// (or there are no includes) and matches no exclude pattern: excludes always
// take precedence over includes.
//
// A filter is read-only once built and safe to use from multiple goroutines.
type PathFilter struct {
	includes []string
	excludes []string
}

// NewPathFilter validates include and exclude patterns and returns a filter.
// Each element may itself contain several comma-separated patterns. A
// malformed pattern returns an error wrapping ErrInvalidArgument.
func NewPathFilter(include, exclude []string) (*PathFilter, error) {
	includes, err := normalizePatterns(include)
	if err != nil {
		return nil, err
	}
	excludes, err := normalizePatterns(exclude)
	if err != nil {
		return nil, err
	}
	return &PathFilter{includes: includes, excludes: excludes}, nil
}

// Empty reports whether the filter selects every path, that is, it has no
// include and no exclude patterns.
func (f *PathFilter) Empty() bool {
	return f == nil || (len(f.includes) == 0 && len(f.excludes) == 0)
}

// Matches reports whether a change at the given path is selected by the filter.
// A nil filter selects every path.
func (f *PathFilter) Matches(changePath string) bool {
	if f == nil {
		return true
	}
	if len(f.includes) > 0 {
		selected := false
		for _, pattern := range f.includes {
			if matchChangePattern(pattern, changePath) {
				selected = true
				break
			}
		}
		if !selected {
			return false
		}
	}
	for _, pattern := range f.excludes {
		if matchChangePattern(pattern, changePath) {
			return false
		}
	}
	return true
}

// FilterChanges returns the subset of changes selected by filter, preserving
// the original order. A nil or empty filter returns the changes unchanged.
func FilterChanges(changes []Change, filter *PathFilter) []Change {
	if filter.Empty() {
		return changes
	}
	out := make([]Change, 0, len(changes))
	for _, change := range changes {
		if filter.Matches(change.Path) {
			out = append(out, change)
		}
	}
	return out
}

// Filter returns a copy of the diff with only the changes selected by filter.
// The receiver is not modified. A nil filter or receiver returns the receiver
// unchanged.
func (d *DiffResult) Filter(filter *PathFilter) *DiffResult {
	if d == nil || filter.Empty() {
		return d
	}
	filtered := *d
	filtered.Changes = FilterChanges(d.Changes, filter)
	return &filtered
}

// normalizePatterns trims, splits on commas and validates patterns.
func normalizePatterns(patterns []string) ([]string, error) {
	var out []string
	for _, raw := range patterns {
		for _, piece := range strings.Split(raw, ",") {
			pattern := strings.TrimSpace(piece)
			if pattern == "" {
				continue
			}
			if err := validatePattern(pattern); err != nil {
				return nil, err
			}
			out = append(out, pattern)
		}
	}
	return out, nil
}

// validatePattern rejects a malformed glob before it can reach the matcher.
func validatePattern(pattern string) error {
	for _, segment := range pathSegments(pattern) {
		if segment == "**" {
			continue
		}
		if _, err := path.Match(segment, ""); err != nil {
			return fmt.Errorf("%w: invalid path pattern %q: %w", ErrInvalidArgument, pattern, err)
		}
	}
	return nil
}

// matchChangePattern reports whether pattern matches changePath, or matches a
// proper prefix of it so that a field pattern also selects its subtree.
func matchChangePattern(pattern, changePath string) bool {
	return matchSegments(pathSegments(pattern), pathSegments(changePath))
}

func matchSegments(pattern, segments []string) bool {
	if len(pattern) == 0 {
		return true
	}
	if pattern[0] == "**" {
		for i := 0; i <= len(segments); i++ {
			if matchSegments(pattern[1:], segments[i:]) {
				return true
			}
		}
		return false
	}
	if len(segments) == 0 {
		return false
	}
	if matched, err := path.Match(pattern[0], segments[0]); err != nil || !matched {
		return false
	}
	return matchSegments(pattern[1:], segments[1:])
}

// pathSegments splits a field path or pattern into its dot-separated segments,
// rendering bracketed indexes as their own segment: "Ops[0].Code" becomes
// ["Ops", "0", "Code"]. Bracket contents are preserved verbatim so a pattern
// such as "Ops[*]" stays mappable.
func pathSegments(s string) []string {
	var segments []string
	for _, part := range strings.Split(s, ".") {
		for part != "" {
			open := strings.IndexByte(part, '[')
			if open < 0 {
				segments = append(segments, part)
				break
			}
			if open > 0 {
				segments = append(segments, part[:open])
			}
			closeIdx := strings.IndexByte(part[open:], ']')
			if closeIdx < 0 {
				segments = append(segments, part[open:])
				break
			}
			closeIdx += open
			segments = append(segments, part[open+1:closeIdx])
			part = part[closeIdx+1:]
		}
	}
	return segments
}
