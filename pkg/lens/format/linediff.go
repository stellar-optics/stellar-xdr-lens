package format

import (
	"strconv"
	"strings"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

// lineKind classifies one aligned entry of a line-level diff.
type lineKind uint8

const (
	lineEqual lineKind = iota
	lineDelete
	lineInsert
)

// alignedPair is one line of a line-level diff. An equal pair carries the same
// text on both sides; a delete has only left text; an insert has only right
// text. Preserving both sides lets the unified and side-by-side formatters
// share the same alignment.
type alignedPair struct {
	kind  lineKind
	left  string
	right string
}

// flattenValue renders a decoded value as a deterministic slice of text lines,
// one per node, in document order. Each line is "path = rendering", where the
// rendering is the node's normal display form (an enriched address, a formatted
// amount, a type name, or a list length). This is the line unit both the
// unified and side-by-side diffs compare.
func flattenValue(v *lens.Value) []string {
	var lines []string
	if v == nil || v.Node == nil {
		return lines
	}
	flattenNode("", v.Node, &lines)
	return lines
}

func flattenNode(path string, n *lens.Node, out *[]string) {
	if n == nil {
		return
	}
	label := path
	if label == "" {
		label = "."
	}
	*out = append(*out, label+" = "+n.String())

	for i, child := range n.Children {
		if n.Kind == lens.KindList {
			flattenNode(path+"["+strconv.Itoa(i)+"]", child, out)
			continue
		}
		flattenNode(joinLinePath(path, child.Name), child, out)
	}
}

func joinLinePath(path, name string) string {
	if name == "" {
		return path
	}
	if path == "" {
		return name
	}
	return path + "." + name
}

// maxLineAlignmentCells bounds the LCS table so pathological input cannot
// allocate without limit. Above it the pair falls back to a wholesale
// replacement, which is still a valid diff.
const maxLineAlignmentCells = 1 << 20

// alignValues aligns the flattened line streams of two decoded values.
func alignValues(left, right *lens.Value) []alignedPair {
	return alignLines(flattenValue(left), flattenValue(right))
}

// alignLines computes an LCS-based alignment of two line slices. The result is
// deterministic: when a tie is possible it prefers the deletion, mirroring the
// structural differ's position-first bias.
func alignLines(a, b []string) []alignedPair {
	out := make([]alignedPair, 0, len(a)+len(b))
	if len(a) == 0 {
		for _, line := range b {
			out = append(out, alignedPair{kind: lineInsert, right: line})
		}
		return out
	}
	if len(b) == 0 {
		for _, line := range a {
			out = append(out, alignedPair{kind: lineDelete, left: line})
		}
		return out
	}
	if len(a) > maxLineAlignmentCells/len(b) {
		for _, line := range a {
			out = append(out, alignedPair{kind: lineDelete, left: line})
		}
		for _, line := range b {
			out = append(out, alignedPair{kind: lineInsert, right: line})
		}
		return out
	}

	rows, columns := len(a)+1, len(b)+1
	lcs := make([][]int, rows)
	for i := range lcs {
		lcs[i] = make([]int, columns)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			switch {
			case a[i] == b[j]:
				lcs[i][j] = 1 + lcs[i+1][j+1]
			case lcs[i+1][j] >= lcs[i][j+1]:
				lcs[i][j] = lcs[i+1][j]
			default:
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}

	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			out = append(out, alignedPair{kind: lineEqual, left: a[i], right: b[j]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			out = append(out, alignedPair{kind: lineDelete, left: a[i]})
			i++
		default:
			out = append(out, alignedPair{kind: lineInsert, right: b[j]})
			j++
		}
	}
	for ; i < len(a); i++ {
		out = append(out, alignedPair{kind: lineDelete, left: a[i]})
	}
	for ; j < len(b); j++ {
		out = append(out, alignedPair{kind: lineInsert, right: b[j]})
	}
	return out
}

// alignedHunk is a contiguous region of an alignment, padded with context.
type alignedHunk struct {
	oldStart int
	oldCount int
	newStart int
	newCount int
	lines    []alignedPair
}

// buildHunks groups an alignment into hunks with the requested number of
// context lines, merging change regions that fall within 2*context lines of
// each other. This is the standard unified-diff hunk algorithm.
func buildHunks(pairs []alignedPair, context int) []alignedHunk {
	if context < 0 {
		context = 0
	}
	oldBefore, newBefore := lineOffsets(pairs)

	var hunks []alignedHunk
	for i := 0; i < len(pairs); {
		if pairs[i].kind == lineEqual {
			i++
			continue
		}
		start := i - context
		if start < 0 {
			start = 0
		}
		lastChange := i
		j := i
		for j < len(pairs) {
			if pairs[j].kind != lineEqual {
				lastChange = j
			} else if j-lastChange > 2*context {
				break
			}
			j++
		}
		end := lastChange + 1 + context
		if end > len(pairs) {
			end = len(pairs)
		}
		hunks = append(hunks, alignedHunk{
			oldStart: rangeStart(oldBefore[start], oldBefore[end]-oldBefore[start]),
			oldCount: oldBefore[end] - oldBefore[start],
			newStart: rangeStart(newBefore[start], newBefore[end]-newBefore[start]),
			newCount: newBefore[end] - newBefore[start],
			lines:    pairs[start:end],
		})
		i = end
	}
	return hunks
}

// lineOffsets returns, for every alignment boundary, how many old and new lines
// precede it.
func lineOffsets(pairs []alignedPair) (oldBefore, newBefore []int) {
	oldBefore = make([]int, len(pairs)+1)
	newBefore = make([]int, len(pairs)+1)
	for i, pair := range pairs {
		oldBefore[i+1] = oldBefore[i]
		newBefore[i+1] = newBefore[i]
		if pair.kind != lineInsert {
			oldBefore[i+1]++
		}
		if pair.kind != lineDelete {
			newBefore[i+1]++
		}
	}
	return oldBefore, newBefore
}

// rangeStart returns the starting line number for a hunk range. When the range
// is empty the number is the line before it, per the unified-diff convention.
func rangeStart(linesBefore, count int) int {
	if count == 0 {
		return linesBefore
	}
	return linesBefore + 1
}

// formatRange renders "start,count", omitting the count when it is one because
// that is the conventional short form.
func formatRange(start, count int) string {
	if count == 1 {
		return strconv.Itoa(start)
	}
	return strconv.Itoa(start) + "," + strconv.Itoa(count)
}

// allEqual reports whether an alignment contains no changes.
func allEqual(pairs []alignedPair) bool {
	for _, pair := range pairs {
		if pair.kind != lineEqual {
			return false
		}
	}
	return true
}

// filterAlignment drops changed lines whose path is not selected by filter,
// leaving the surrounding context. A nil or empty filter returns the alignment
// unchanged.
func filterAlignment(pairs []alignedPair, filter *lens.PathFilter) []alignedPair {
	if filter.Empty() {
		return pairs
	}
	out := make([]alignedPair, 0, len(pairs))
	for _, pair := range pairs {
		if pair.kind == lineEqual {
			out = append(out, pair)
			continue
		}
		text := pair.left
		if pair.kind == lineInsert {
			text = pair.right
		}
		if filter.Matches(linePath(text)) {
			out = append(out, pair)
		}
	}
	return out
}

// linePath extracts the field path from a flattened "path = rendering" line.
func linePath(line string) string {
	if i := strings.Index(line, " = "); i >= 0 {
		return line[:i]
	}
	return line
}
