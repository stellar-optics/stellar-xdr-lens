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

// SignerDrift describes a quorum change detected between two decoded account
// entries or signer lists. It is returned by SignerDriftDetector.
type SignerDrift struct {
	// Added holds signers present only in the right-hand value.
	Added []string `json:"added,omitempty"`
	// Removed holds signers present only in the left-hand value.
	Removed []string `json:"removed,omitempty"`
	// WeightChanged holds signers whose key is in both sides but with a
	// different weight, rendered as "address: N→M".
	WeightChanged []string `json:"weightChanged,omitempty"`
	// ThresholdNote summarises the threshold change, when both sides carry
	// AccountEntry Thresholds fields. Empty when thresholds are unchanged or
	// unavailable.
	ThresholdNote string `json:"thresholdNote,omitempty"`
}

// Equal reports whether no drift was detected.
func (d SignerDrift) Equal() bool {
	return len(d.Added) == 0 && len(d.Removed) == 0 && len(d.WeightChanged) == 0 && d.ThresholdNote == ""
}

// Summary returns a one-line description of the drift.
func (d SignerDrift) Summary() string {
	if d.Equal() {
		return "no signer drift"
	}
	parts := make([]string, 0, 4)
	if n := len(d.Removed); n > 0 {
		parts = append(parts, fmt.Sprintf("%d removed", n))
	}
	if n := len(d.Added); n > 0 {
		parts = append(parts, fmt.Sprintf("%d added", n))
	}
	if n := len(d.WeightChanged); n > 0 {
		parts = append(parts, fmt.Sprintf("%d weight changed", n))
	}
	if d.ThresholdNote != "" {
		parts = append(parts, d.ThresholdNote)
	}
	return strings.Join(parts, "; ")
}

// SignerDriftDetector compares two signer lists and optional threshold
// configurations, reporting semantic drift: added/removed signers, weight
// changes, and quorum-threshold shifts. It is intended for account-level
// diffing where both inputs are decoded AccountEntry or raw Signer slices.
type SignerDriftDetector struct{}

// DetectFromSigners compares two Signer slices and returns the drift.
func (SignerDriftDetector) DetectFromSigners(left, right []xdr.Signer) (SignerDrift, error) {
	return detectSignerDrift(left, right, nil, nil)
}

// DetectFromAccounts compares two AccountEntry values, capturing both signer
// set changes and threshold shifts.
func (SignerDriftDetector) DetectFromAccounts(left, right *xdr.AccountEntry) (SignerDrift, error) {
	if left == nil || right == nil {
		return SignerDrift{}, fmt.Errorf("nil AccountEntry")
	}
	return detectSignerDrift(left.Signers, right.Signers, &left.Thresholds, &right.Thresholds)
}

// detectSignerDrift performs the shared comparison logic.
func detectSignerDrift(left, right []xdr.Signer, leftTh, rightTh *xdr.Thresholds) (SignerDrift, error) {
	// Build key → weight maps for both sides.
	leftMap := make(map[string]uint32, len(left))
	for i := range left {
		b, err := left[i].Key.MarshalBinary()
		if err != nil {
			return SignerDrift{}, fmt.Errorf("encoding left signer %d: %w", i, err)
		}
		leftMap[hex.EncodeToString(b)] = uint32(left[i].Weight)
	}
	rightMap := make(map[string]uint32, len(right))
	rightLabels := make(map[string]string, len(right))
	for i := range right {
		b, err := right[i].Key.MarshalBinary()
		if err != nil {
			return SignerDrift{}, fmt.Errorf("encoding right signer %d: %w", i, err)
		}
		k := hex.EncodeToString(b)
		rightMap[k] = uint32(right[i].Weight)
		label := k
		if addr, err := right[i].Key.GetAddress(); err == nil && addr != "" {
			label = addr
		}
		rightLabels[k] = label
	}
	// Left-side labels (for removed/changed).
	leftLabels := make(map[string]string, len(left))
	for i := range left {
		b, _ := left[i].Key.MarshalBinary()
		k := hex.EncodeToString(b)
		label := k
		if addr, err := left[i].Key.GetAddress(); err == nil && addr != "" {
			label = addr
		}
		leftLabels[k] = label
	}

	var drift SignerDrift

	// Check left signers: removed or weight-changed.
	for k, lw := range leftMap {
		rw, inRight := rightMap[k]
		if !inRight {
			drift.Removed = append(drift.Removed, leftLabels[k])
			continue
		}
		if lw != rw {
			label := leftLabels[k]
			drift.WeightChanged = append(drift.WeightChanged,
				fmt.Sprintf("%s: %d→%d", label, lw, rw))
		}
	}
	// Check right signers: added.
	for k := range rightMap {
		if _, inLeft := leftMap[k]; !inLeft {
			drift.Added = append(drift.Added, rightLabels[k])
		}
	}

	// Threshold comparison.
	if leftTh != nil && rightTh != nil && *leftTh != *rightTh {
		lm, ll, lmed, lh := leftTh[0], leftTh[1], leftTh[2], leftTh[3]
		rm, rl, rmed, rh := rightTh[0], rightTh[1], rightTh[2], rightTh[3]
		drift.ThresholdNote = fmt.Sprintf("thresholds changed: low %d→%d med %d→%d high %d→%d master %d→%d",
			ll, rl, lmed, rmed, lh, rh, lm, rm)
	}

	return drift, nil
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
	if n.Kind == KindScalar || n.Kind == KindBytes {
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
	if n.Kind == KindScalar || n.Kind == KindBytes {
		return n.String()
	}
	parts := make([]string, 0, len(n.Children))
	for _, child := range n.Children {
		if child == nil {
			continue
		}
		if child.Kind != KindScalar && child.Kind != KindBytes {
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
