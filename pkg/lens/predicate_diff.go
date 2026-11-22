package lens

import (
	"fmt"
	"time"

	"github.com/stellar/go-stellar-sdk/xdr"
)

// PredicateChange describes one semantic difference found by DiffClaimPredicate.
type PredicateChange struct {
	// Path locates the change within the predicate tree, e.g. "AND[0]" or "NOT.inner".
	Path string
	// Description is a human-readable summary of the change.
	Description string
	// Before is the before-value rendered as a string, empty when the predicate was added.
	Before string
	// After is the after-value rendered as a string, empty when the predicate was removed.
	After string
}

// DiffClaimPredicate semantically compares two ClaimPredicate trees, aligning
// corresponding sub-expressions (AND, OR, NOT, BEFORE_ABSOLUTE, BEFORE_RELATIVE)
// and describing changes in human-readable terms.
//
// Unlike a node-level diff, this walks the XDR values directly so that changes
// to time bounds produce deltas like "+3600s" rather than raw integer differences.
func DiffClaimPredicate(left, right xdr.ClaimPredicate) []PredicateChange {
	var changes []PredicateChange
	diffPredicateAt(left, right, "", &changes)
	return changes
}

// SummarizePredicateDiff returns a one-line human-readable summary of the
// semantic differences between two predicates, suitable for embedding in a
// claimable-balance diff report. Returns an empty string when the predicates
// are identical.
func SummarizePredicateDiff(left, right xdr.ClaimPredicate) string {
	changes := DiffClaimPredicate(left, right)
	if len(changes) == 0 {
		return ""
	}
	if len(changes) == 1 {
		return changes[0].Description
	}
	out := fmt.Sprintf("%d predicate changes", len(changes))
	for _, c := range changes {
		out += "; " + c.Description
	}
	return out
}

// diffPredicateSemantic builds a *DiffResult by constructing custom predicate
// nodes (bypassing the claimable_balance.go enricher) and walking them with the
// standard differ. This is used by DiffWithOptions when both inputs are
// ClaimPredicate values.
func diffPredicateSemantic(left, right xdr.ClaimPredicate) *DiffResult {
	leftNode := predicateNode("", left)
	rightNode := predicateNode("", right)
	res := &DiffResult{LeftType: "ClaimPredicate", RightType: "ClaimPredicate"}
	diffNodesWithOptions(leftNode, rightNode, "", &res.Changes, DiffOptions{})
	return res
}

// predicateNode converts a ClaimPredicate into a Node tree suitable for the
// standard differ, without triggering the enricher that collapses the whole
// predicate to a single string scalar.
func predicateNode(name string, p xdr.ClaimPredicate) *Node {
	switch p.Type {
	case xdr.ClaimPredicateTypeClaimPredicateUnconditional:
		return &Node{Name: name, Kind: KindScalar, TypeName: "ClaimPredicate", Value: "unconditional"}

	case xdr.ClaimPredicateTypeClaimPredicateBeforeAbsoluteTime:
		if p.AbsBefore != nil {
			ts := int64(*p.AbsBefore)
			return &Node{
				Name:     name,
				Kind:     KindScalar,
				TypeName: "ClaimPredicate",
				Value:    fmt.Sprintf("before_absolute(%d)", ts),
				Note:     "before " + time.Unix(ts, 0).UTC().Format(time.RFC3339), //nolint:gosec
			}
		}
		return &Node{Name: name, Kind: KindScalar, TypeName: "ClaimPredicate", Value: "before_absolute(?)"}

	case xdr.ClaimPredicateTypeClaimPredicateBeforeRelativeTime:
		if p.RelBefore != nil {
			secs := int64(*p.RelBefore)
			return &Node{
				Name:     name,
				Kind:     KindScalar,
				TypeName: "ClaimPredicate",
				Value:    fmt.Sprintf("before_relative(%d)", secs),
				Note:     fmt.Sprintf("before %d seconds after creation", secs),
			}
		}
		return &Node{Name: name, Kind: KindScalar, TypeName: "ClaimPredicate", Value: "before_relative(?)"}

	case xdr.ClaimPredicateTypeClaimPredicateNot:
		n := &Node{Name: name, Kind: KindStruct, TypeName: "ClaimPredicate", Note: "NOT"}
		if p.NotPredicate != nil && *p.NotPredicate != nil {
			inner := predicateNode("inner", **p.NotPredicate)
			if inner != nil {
				n.Children = []*Node{inner}
			}
		}
		return n

	case xdr.ClaimPredicateTypeClaimPredicateAnd:
		n := &Node{Name: name, Kind: KindList, TypeName: "ClaimPredicate", Note: "AND"}
		if p.AndPredicates != nil {
			for i, ap := range *p.AndPredicates {
				child := predicateNode(fmt.Sprintf("[%d]", i), ap)
				if child != nil {
					n.Children = append(n.Children, child)
				}
			}
		}
		return n

	case xdr.ClaimPredicateTypeClaimPredicateOr:
		n := &Node{Name: name, Kind: KindList, TypeName: "ClaimPredicate", Note: "OR"}
		if p.OrPredicates != nil {
			for i, op := range *p.OrPredicates {
				child := predicateNode(fmt.Sprintf("[%d]", i), op)
				if child != nil {
					n.Children = append(n.Children, child)
				}
			}
		}
		return n

	default:
		return &Node{
			Name:     name,
			Kind:     KindScalar,
			TypeName: "ClaimPredicate",
			Value:    fmt.Sprintf("unknown(%d)", int(p.Type)),
		}
	}
}

// diffPredicateAt recursively compares two predicates at a given path,
// appending semantic PredicateChange entries for every difference found.
func diffPredicateAt(left, right xdr.ClaimPredicate, path string, out *[]PredicateChange) {
	if left.Type != right.Type {
		*out = append(*out, PredicateChange{
			Path:        path,
			Description: fmt.Sprintf("predicate type changed from %s to %s", left.Type, right.Type),
			Before:      DescribeClaimPredicate(left),
			After:       DescribeClaimPredicate(right),
		})
		return
	}
	switch left.Type {
	case xdr.ClaimPredicateTypeClaimPredicateUnconditional:
		// identical

	case xdr.ClaimPredicateTypeClaimPredicateBeforeAbsoluteTime:
		if left.AbsBefore != nil && right.AbsBefore != nil {
			lv, rv := int64(*left.AbsBefore), int64(*right.AbsBefore)
			if lv != rv {
				delta := rv - lv
				var desc string
				if delta > 0 {
					desc = fmt.Sprintf("absolute time bound extended by +%ds", delta)
				} else {
					desc = fmt.Sprintf("absolute time bound tightened by %ds", delta)
				}
				*out = append(*out, PredicateChange{
					Path:        path,
					Description: desc,
					Before:      fmt.Sprintf("%d", lv),
					After:       fmt.Sprintf("%d", rv),
				})
			}
		}

	case xdr.ClaimPredicateTypeClaimPredicateBeforeRelativeTime:
		if left.RelBefore != nil && right.RelBefore != nil {
			lv, rv := int64(*left.RelBefore), int64(*right.RelBefore)
			if lv != rv {
				delta := rv - lv
				var desc string
				if delta > 0 {
					desc = fmt.Sprintf("relative time bound extended by +%ds", delta)
				} else {
					desc = fmt.Sprintf("relative time bound tightened by %ds", delta)
				}
				*out = append(*out, PredicateChange{
					Path:        path,
					Description: desc,
					Before:      fmt.Sprintf("%d", lv),
					After:       fmt.Sprintf("%d", rv),
				})
			}
		}

	case xdr.ClaimPredicateTypeClaimPredicateNot:
		innerPath := predicatePath(path, "NOT")
		if left.NotPredicate != nil && right.NotPredicate != nil &&
			*left.NotPredicate != nil && *right.NotPredicate != nil {
			diffPredicateAt(**left.NotPredicate, **right.NotPredicate, innerPath, out)
		}

	case xdr.ClaimPredicateTypeClaimPredicateAnd:
		if left.AndPredicates != nil && right.AndPredicates != nil {
			diffPredicateSlices(*left.AndPredicates, *right.AndPredicates, predicatePath(path, "AND"), out)
		}

	case xdr.ClaimPredicateTypeClaimPredicateOr:
		if left.OrPredicates != nil && right.OrPredicates != nil {
			diffPredicateSlices(*left.OrPredicates, *right.OrPredicates, predicatePath(path, "OR"), out)
		}
	}
}

// diffPredicateSlices aligns two predicate slices by position and reports
// additions, removals and modifications.
func diffPredicateSlices(lefts, rights []xdr.ClaimPredicate, basePath string, out *[]PredicateChange) {
	shared := len(lefts)
	if len(rights) < shared {
		shared = len(rights)
	}
	for i := range shared {
		diffPredicateAt(lefts[i], rights[i], fmt.Sprintf("%s[%d]", basePath, i), out)
	}
	for i := shared; i < len(rights); i++ {
		*out = append(*out, PredicateChange{
			Path:        fmt.Sprintf("%s[%d]", basePath, i),
			Description: "predicate added: " + DescribeClaimPredicate(rights[i]),
			After:       DescribeClaimPredicate(rights[i]),
		})
	}
	for i := shared; i < len(lefts); i++ {
		*out = append(*out, PredicateChange{
			Path:        fmt.Sprintf("%s[%d]", basePath, i),
			Description: "predicate removed: " + DescribeClaimPredicate(lefts[i]),
			Before:      DescribeClaimPredicate(lefts[i]),
		})
	}
}

// predicatePath builds a dotted path string.
func predicatePath(base, label string) string {
	if base == "" {
		return label
	}
	return base + "." + label
}
