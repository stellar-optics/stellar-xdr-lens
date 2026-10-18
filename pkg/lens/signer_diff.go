package lens

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/stellar/go-stellar-sdk/xdr"
)

// Signer-set diffing.
//
// Signature and signer collections are semantically unordered: a transaction
// signed by the same keys is the same transaction regardless of the order the
// signatures appear in, and an account's signer set is a set, not a sequence.
// A positional diff disagrees — it reports a cascade of modified paths when a
// signature sort order changes — so these helpers compare collections as
// multisets instead, matching duplicates by count and ignoring order.

// SignerSetDiff is the multiset difference between two signer, signature or
// signature-key collections. Order is irrelevant: collections that contain the
// same elements the same number of times are equal.
type SignerSetDiff struct {
	// Path locates the collection within the compared tree, when known.
	Path string `json:"path,omitempty"`
	// Added holds elements present only on the right-hand side.
	Added []string `json:"added,omitempty"`
	// Removed holds elements present only on the left-hand side.
	Removed []string `json:"removed,omitempty"`
	// Common holds elements present on both sides, matched by multiplicity.
	Common []string `json:"common,omitempty"`
}

// Equal reports whether the two collections held the same elements.
func (d SignerSetDiff) Equal() bool { return len(d.Added) == 0 && len(d.Removed) == 0 }

// Summary renders a one-line description of the difference.
func (d SignerSetDiff) Summary() string {
	if d.Equal() {
		return "no signer differences"
	}
	parts := make([]string, 0, 2)
	if len(d.Removed) > 0 {
		parts = append(parts, strconv.Itoa(len(d.Removed))+" removed")
	}
	if len(d.Added) > 0 {
		parts = append(parts, strconv.Itoa(len(d.Added))+" added")
	}
	return strings.Join(parts, ", ")
}

// DiffSignerKeys compares two signer-key collections as unordered multisets.
func DiffSignerKeys(left, right []xdr.SignerKey) (*SignerSetDiff, error) {
	leftItems, err := signerKeyItems(left)
	if err != nil {
		return nil, err
	}
	rightItems, err := signerKeyItems(right)
	if err != nil {
		return nil, err
	}
	return signerSetDiff(leftItems, rightItems), nil
}

// DiffSigners compares two account-signer collections as unordered multisets.
// Weight is part of each signer's identity, so changing a signer's weight is
// reported as one removal plus one addition rather than a reordering.
func DiffSigners(left, right []xdr.Signer) (*SignerSetDiff, error) {
	leftItems, err := signerItems(left)
	if err != nil {
		return nil, err
	}
	rightItems, err := signerItems(right)
	if err != nil {
		return nil, err
	}
	return signerSetDiff(leftItems, rightItems), nil
}

// DiffDecoratedSignatures compares two signature collections as unordered
// multisets. The signature hint is not unique, so identity uses the full
// encoded signature.
func DiffDecoratedSignatures(left, right []xdr.DecoratedSignature) (*SignerSetDiff, error) {
	leftItems, err := decoratedSignatureItems(left)
	if err != nil {
		return nil, err
	}
	rightItems, err := decoratedSignatureItems(right)
	if err != nil {
		return nil, err
	}
	return signerSetDiff(leftItems, rightItems), nil
}

// multisetItem pairs a canonical identity with a human-readable rendering.
type multisetItem struct {
	key   string
	label string
}

func signerKeyItems(keys []xdr.SignerKey) ([]multisetItem, error) {
	items := make([]multisetItem, 0, len(keys))
	for i := range keys {
		bytes, err := keys[i].MarshalBinary()
		if err != nil {
			return nil, fmt.Errorf("encoding signer key %d: %w", i, err)
		}
		label := hex.EncodeToString(bytes)
		if address, err := keys[i].GetAddress(); err == nil && address != "" {
			label = address
		}
		items = append(items, multisetItem{key: hex.EncodeToString(bytes), label: label})
	}
	return items, nil
}

func signerItems(signers []xdr.Signer) ([]multisetItem, error) {
	items := make([]multisetItem, 0, len(signers))
	for i := range signers {
		bytes, err := signers[i].MarshalBinary()
		if err != nil {
			return nil, fmt.Errorf("encoding signer %d: %w", i, err)
		}
		label := hex.EncodeToString(bytes)
		if address, err := signers[i].Key.GetAddress(); err == nil && address != "" {
			label = fmt.Sprintf("%s (weight %d)", address, uint32(signers[i].Weight))
		}
		items = append(items, multisetItem{key: hex.EncodeToString(bytes), label: label})
	}
	return items, nil
}

func decoratedSignatureItems(signatures []xdr.DecoratedSignature) ([]multisetItem, error) {
	items := make([]multisetItem, 0, len(signatures))
	for i := range signatures {
		bytes, err := signatures[i].MarshalBinary()
		if err != nil {
			return nil, fmt.Errorf("encoding signature %d: %w", i, err)
		}
		hint := hex.EncodeToString(signatures[i].Hint[:])
		label := fmt.Sprintf("hint %s, signature 0x%s", hint, hex.EncodeToString(signatures[i].Signature))
		items = append(items, multisetItem{key: hex.EncodeToString(bytes), label: label})
	}
	return items, nil
}

// signerSetDiff performs the multiset difference. Removed and Common preserve
// left-hand order; Added preserves right-hand order, so the output is
// deterministic for a given input.
func signerSetDiff(left, right []multisetItem) *SignerSetDiff {
	leftCounts := make(map[string]int, len(left))
	for _, item := range left {
		leftCounts[item.key]++
	}
	remaining := make(map[string]int, len(right))
	for _, item := range right {
		remaining[item.key]++
	}

	diff := &SignerSetDiff{
		Added:   []string{},
		Removed: []string{},
		Common:  []string{},
	}
	for _, item := range left {
		if remaining[item.key] > 0 {
			remaining[item.key]--
			diff.Common = append(diff.Common, item.label)
			continue
		}
		diff.Removed = append(diff.Removed, item.label)
	}
	consumed := make(map[string]int, len(right))
	for _, item := range right {
		if consumed[item.key] < leftCounts[item.key] {
			consumed[item.key]++
			continue
		}
		diff.Added = append(diff.Added, item.label)
	}
	if len(diff.Added) == 0 {
		diff.Added = nil
	}
	if len(diff.Removed) == 0 {
		diff.Removed = nil
	}
	if len(diff.Common) == 0 {
		diff.Common = nil
	}
	return diff
}

// isSignerCollection reports whether a field name holds an unordered signer or
// signature collection. Only the fields the protocol defines as sets are
// listed; everything else keeps positional comparison.
func isSignerCollection(name string) bool {
	switch name {
	case "Signatures", "Signers", "ExtraSigners":
		return true
	default:
		return false
	}
}

// signerCollectionName resolves the field name of a list node, falling back to
// the final path segment for anonymous roots.
func signerCollectionName(path string, a, b *Node) string {
	if a != nil && a.Name != "" {
		return a.Name
	}
	if b != nil && b.Name != "" {
		return b.Name
	}
	segments := splitPath(path)
	if len(segments) == 0 {
		return ""
	}
	return segments[len(segments)-1].name
}

// diffMultisetLists reports the multiset difference between two node lists,
// ignoring order. It emits one removal per surplus left element and one
// addition per surplus right element, both located at the list's own path.
func diffMultisetLists(a, b *Node, path string, out *[]Change) {
	left := make([]multisetItem, 0, len(a.Children))
	for _, child := range a.Children {
		left = append(left, multisetItem{key: canonicalNodeKey(child), label: renderNodeLabel(child)})
	}
	right := make([]multisetItem, 0, len(b.Children))
	for _, child := range b.Children {
		right = append(right, multisetItem{key: canonicalNodeKey(child), label: renderNodeLabel(child)})
	}
	diff := signerSetDiff(left, right)
	for _, label := range diff.Removed {
		*out = append(*out, Change{Path: path, Op: ChangeRemoved, Before: label})
	}
	for _, label := range diff.Added {
		*out = append(*out, Change{Path: path, Op: ChangeAdded, After: label})
	}
}

// canonicalNodeKey renders a node as a stable identity string. It deliberately
// ignores Node.Note, which is a derived human rendering: two byte-identical
// signatures must compare equal even if only one carries a note.
func canonicalNodeKey(n *Node) string {
	var b strings.Builder
	writeCanonicalNode(&b, n)
	return b.String()
}

func writeCanonicalNode(b *strings.Builder, n *Node) {
	if n == nil {
		b.WriteString("<nil>;")
		return
	}
	fmt.Fprintf(b, "%d:%d:%s:", n.Kind, len(n.TypeName), n.TypeName)
	if n.Kind == KindScalar {
		fmt.Fprintf(b, "%T=%v", n.Value, n.Value)
	} else {
		for _, child := range n.Children {
			fmt.Fprintf(b, "%d:%s", len(child.Name), child.Name)
			writeCanonicalNode(b, child)
		}
	}
	b.WriteString(";")
}

// renderNodeLabel renders a node as a compact single-line label for reporting
// an added or removed element. Scalars use their normal rendering; structs and
// lists flatten their scalar children into key=value pairs.
func renderNodeLabel(n *Node) string {
	if n == nil {
		return "(none)"
	}
	if n.Kind == KindScalar {
		return n.String()
	}
	parts := make([]string, 0, len(n.Children))
	for _, child := range n.Children {
		if child == nil {
			continue
		}
		if child.Kind != KindScalar {
			continue
		}
		if child.Name == "" {
			parts = append(parts, child.String())
			continue
		}
		parts = append(parts, child.Name+"="+child.String())
	}
	if len(parts) == 0 {
		return n.String()
	}
	return strings.Join(parts, " ")
}
