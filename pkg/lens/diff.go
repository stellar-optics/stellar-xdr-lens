package lens

import (
	"fmt"
	"strconv"
	"strings"
)

// ChangeOp classifies a single difference between two XDR values.
type ChangeOp uint8

const (
	// ChangeModified means the path exists on both sides with different values.
	ChangeModified ChangeOp = iota
	// ChangeAdded means the path exists only on the right-hand side.
	ChangeAdded
	// ChangeRemoved means the path exists only on the left-hand side.
	ChangeRemoved
	// ChangeTypeChanged means the path exists on both sides but holds a
	// different kind of value, such as a scalar replaced by a struct. This
	// is the usual signal that a union switched arms.
	ChangeTypeChanged
)

func (c ChangeOp) String() string {
	switch c {
	case ChangeModified:
		return "modified"
	case ChangeAdded:
		return "added"
	case ChangeRemoved:
		return "removed"
	case ChangeTypeChanged:
		return "type_changed"
	default:
		return "unknown"
	}
}

// Change is one difference between two decoded XDR values.
type Change struct {
	// Path locates the change, e.g. "V1.Tx.Operations[0].Body.PaymentOp.Amount".
	Path string
	// Op is the kind of change.
	Op ChangeOp
	// Before is the left-hand value; nil when Op is ChangeAdded.
	Before any
	// After is the right-hand value; nil when Op is ChangeRemoved.
	After any
	// BeforeNote and AfterNote carry the enriched renderings, so a diff of
	// two amounts can show "100.0000000" rather than only the stroop count.
	BeforeNote string
	AfterNote  string
}

// String renders a change as a single line.
func (c Change) String() string {
	switch c.Op {
	case ChangeAdded:
		return fmt.Sprintf("+ %s = %s", c.Path, display(c.After, c.AfterNote))
	case ChangeRemoved:
		return fmt.Sprintf("- %s = %s", c.Path, display(c.Before, c.BeforeNote))
	default:
		return fmt.Sprintf("~ %s: %s -> %s", c.Path,
			display(c.Before, c.BeforeNote), display(c.After, c.AfterNote))
	}
}

func display(v any, note string) string {
	if v == nil && note == "" {
		return "(none)"
	}
	if v == nil {
		return note
	}
	s := fmt.Sprintf("%v", v)
	if note != "" {
		return s + " (" + note + ")"
	}
	return s
}

// DiffResult is the outcome of comparing two decoded values.
type DiffResult struct {
	// LeftType and RightType are the XDR types compared. They may differ,
	// which is itself worth reporting.
	LeftType  string
	RightType string
	// Changes lists every difference, in document order.
	Changes []Change
}

// Equal reports whether the two values were structurally identical.
func (d *DiffResult) Equal() bool { return len(d.Changes) == 0 }

// Diff structurally compares two decoded XDR values and reports every
// differing path.
//
// Unlike a byte or ordering comparison, this walks both trees together and
// names the fields that actually changed, which is what makes it useful for
// answering "what is different about these two envelopes".
//
// Values of different XDR types can still be compared; the type difference is
// reported and the walk proceeds, since a TransactionV0 and a Transaction
// often share most of their structure.
func Diff(left, right *Value) (*DiffResult, error) {
	if left == nil || right == nil {
		return nil, fmt.Errorf("diff requires two decoded values")
	}
	res := &DiffResult{LeftType: left.Type, RightType: right.Type}
	diffNodes(left.Node, right.Node, "", &res.Changes)
	return res, nil
}

// diffNodes walks two subtrees in parallel, appending changes as it goes.
func diffNodes(a, b *Node, path string, out *[]Change) {
	switch {
	case a == nil && b == nil:
		return
	case a == nil:
		*out = append(*out, Change{Path: path, Op: ChangeAdded, After: leafValue(b), AfterNote: b.Note})
		return
	case b == nil:
		*out = append(*out, Change{Path: path, Op: ChangeRemoved, Before: leafValue(a), BeforeNote: a.Note})
		return
	}

	if a.Kind != b.Kind {
		*out = append(*out, Change{
			Path:       path,
			Op:         ChangeTypeChanged,
			Before:     leafValue(a),
			After:      leafValue(b),
			BeforeNote: a.Note,
			AfterNote:  b.Note,
		})
		return
	}

	switch a.Kind {
	case KindScalar:
		if !sameScalar(a, b) {
			*out = append(*out, Change{
				Path:       path,
				Op:         ChangeModified,
				Before:     a.Value,
				After:      b.Value,
				BeforeNote: a.Note,
				AfterNote:  b.Note,
			})
		}

	case KindList:
		diffLists(a, b, path, out)

	case KindStruct:
		diffStructs(a, b, path, out)
	}
}

// diffStructs compares two records field by field, preserving the field order
// of the left-hand side so output reads top-to-bottom like the source.
func diffStructs(a, b *Node, path string, out *[]Change) {
	seen := make(map[string]bool, len(a.Children))

	for _, ac := range a.Children {
		seen[ac.Name] = true
		diffNodes(ac, b.Child(ac.Name), join(path, ac.Name), out)
	}
	// Fields present only on the right: a union that switched arms shows up
	// here as a removal plus an addition.
	for _, bc := range b.Children {
		if seen[bc.Name] {
			continue
		}
		diffNodes(nil, bc, join(path, bc.Name), out)
	}
}

// diffLists compares two sequences positionally.
//
// Positional comparison is the right default for XDR: operations, signatures
// and claimants are ordered, and their index is meaningful. A
// longest-common-subsequence alignment would report an inserted operation as
// one insert rather than a cascade of modifications, which reads better but
// hides the index shift that actually matters when debugging.
func diffLists(a, b *Node, path string, out *[]Change) {
	maxLen := len(a.Children)
	if len(b.Children) > maxLen {
		maxLen = len(b.Children)
	}
	for i := range maxLen {
		var ac, bc *Node
		if i < len(a.Children) {
			ac = a.Children[i]
		}
		if i < len(b.Children) {
			bc = b.Children[i]
		}
		diffNodes(ac, bc, path+"["+strconv.Itoa(i)+"]", out)
	}
}

func sameScalar(a, b *Node) bool {
	if a.Note != b.Note {
		return false
	}
	return fmt.Sprintf("%v", a.Value) == fmt.Sprintf("%v", b.Value)
}

// leafValue summarises a node for reporting in a change. Struct and list
// nodes are described rather than expanded, since the change line names the
// path and the detail lives in the tree output.
func leafValue(n *Node) any {
	if n == nil {
		return nil
	}
	switch n.Kind {
	case KindScalar:
		return n.Value
	case KindList:
		return fmt.Sprintf("[%d item(s)]", len(n.Children))
	default:
		if n.TypeName != "" {
			return "{" + n.TypeName + "}"
		}
		return "{...}"
	}
}

func join(path, name string) string {
	if path == "" {
		return name
	}
	if name == "" {
		return path
	}
	return path + "." + name
}

// DiffBase64 is a convenience wrapper that decodes both payloads and diffs
// them. When typeName is empty the type is auto-detected for each side
// independently.
func DiffBase64(leftPayload, rightPayload, typeName string) (*DiffResult, error) {
	decode := func(p string) (*Value, error) {
		if typeName != "" {
			return DecodeAs(p, typeName)
		}
		return Decode(p)
	}
	left, err := decode(leftPayload)
	if err != nil {
		return nil, fmt.Errorf("decoding left value: %w", err)
	}
	right, err := decode(rightPayload)
	if err != nil {
		return nil, fmt.Errorf("decoding right value: %w", err)
	}
	return Diff(left, right)
}

// Summary renders a one-line description of the diff, suitable for a CI log.
func (d *DiffResult) Summary() string {
	if d.Equal() {
		return "no differences"
	}
	var added, removed, modified int
	for _, c := range d.Changes {
		switch c.Op {
		case ChangeAdded:
			added++
		case ChangeRemoved:
			removed++
		default:
			modified++
		}
	}
	var parts []string
	if modified > 0 {
		parts = append(parts, strconv.Itoa(modified)+" changed")
	}
	if added > 0 {
		parts = append(parts, strconv.Itoa(added)+" added")
	}
	if removed > 0 {
		parts = append(parts, strconv.Itoa(removed)+" removed")
	}
	return strings.Join(parts, ", ")
}
