package lens

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/stellar/go-stellar-sdk/xdr"
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
	// ChangeReordered means the element exists on both sides with identical
	// content but at different sequence positions.
	ChangeReordered
)

// LedgerEntryTransitionType classifies high-level LedgerEntry state transitions.
type LedgerEntryTransitionType string

const (
	// TransitionCreated indicates a LedgerEntry was created from scratch.
	TransitionCreated LedgerEntryTransitionType = "created"
	// TransitionUpdated indicates a LedgerEntry was updated in place.
	TransitionUpdated LedgerEntryTransitionType = "updated"
	// TransitionDeleted indicates a LedgerEntry was deleted.
	TransitionDeleted LedgerEntryTransitionType = "deleted"
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
	case ChangeReordered:
		return "reordered"
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
	case ChangeReordered:
		return fmt.Sprintf("~ %s: reordered from index %v to %v", c.Path, c.Before, c.After)
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
	// Redacted lists the paths whose values were normalized away by
	// DiffOptions.Redaction, in first-seen document order. It is nil unless
	// redaction was requested, and is reported for transparency: those paths
	// may still differ on the wire even though the diff does not show it.
	Redacted []string
	// Warnings lists non-fatal security or configuration warnings discovered
	// during diff analysis, such as quorum degradation or threshold violations.
	Warnings []string
}

// DiffOptions configures structural comparison behavior.
type DiffOptions struct {
	// AlignLists matches unchanged list elements with a longest-common-
	// subsequence pass before comparing them. It is useful when insertions
	// would otherwise shift every later positional diff.
	AlignLists bool
	// Semantic compares supported ScVal integers as exact decimal values and
	// pairs ScVal map entries by canonical key instead of list position.
	Semantic bool
	// UnorderedSigners compares signature and signer collections as unordered
	// multisets, so reordering them is not reported as a difference. Only the
	// fields the protocol defines as sets (Signatures, Signers and
	// ExtraSigners) are affected; every other list stays positional.
	UnorderedSigners bool
	// Filter restricts the reported changes to the paths it selects. A nil
	// filter reports every change. Filtering happens after the walk, so it
	// only narrows the output; it never changes how the values are compared.
	Filter *PathFilter
	// Mask excludes matching paths from diff evaluation entirely. Unlike
	// Filter which only narrows output, masked paths are never compared,
	// so differences in masked paths do not affect the Equal() result.
	// Patterns use the same glob syntax as Filter (*, ?, **, [index]).
	Mask *PathFilter
	// Threshold ignores scalar numeric differences below or equal to the given delta.
	Threshold float64
	// DiffThreshold is an alias for Threshold.
	DiffThreshold float64
	// Redaction normalizes dynamic cryptographic values — hashes, signatures
	// and derived identifiers — before the walk, so that two values which
	// differ only in those fields compare equal. The zero value redacts
	// nothing and preserves the historical behavior. See diff_redact.go.
	Redaction RedactionPolicy
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
	return DiffWithOptions(left, right, DiffOptions{})
}

// DiffWithOptions structurally compares two values using the supplied options.
// The default Diff function remains positional for ordered XDR sequences.
func DiffWithOptions(left, right *Value, options DiffOptions) (*DiffResult, error) {
	if left == nil || right == nil {
		return nil, fmt.Errorf("%w: diff requires two decoded values", ErrInvalidArgument)
	}
	leftNode, rightNode := left.Node, right.Node
	if options.Semantic {
		var err error
		leftNode, err = scValNodeForValue(left, true)
		if err != nil {
			return nil, fmt.Errorf("preparing left semantic value: %w", err)
		}
		rightNode, err = scValNodeForValue(right, true)
		if err != nil {
			return nil, fmt.Errorf("preparing right semantic value: %w", err)
		}
	}
	res := &DiffResult{LeftType: left.Type, RightType: right.Type}
	if options.Redaction.Enabled() {
		var leftPaths, rightPaths []string
		leftNode = redactNode(leftNode, options.Redaction, "", &leftPaths)
		rightNode = redactNode(rightNode, options.Redaction, "", &rightPaths)
		res.Redacted = mergedPaths(leftPaths, rightPaths)
	}
	diffNodesWithOptions(leftNode, rightNode, "", &res.Changes, options)
	if !options.Filter.Empty() {
		res.Changes = FilterChanges(res.Changes, options.Filter)
	}
	analyzeAccountQuorum(left, right, leftNode, rightNode, &res.Changes, &res.Warnings)
	return res, nil
}

// diffNodesWithOptions walks two subtrees in parallel, appending changes as it goes.
func diffNodesWithOptions(a, b *Node, path string, out *[]Change, options DiffOptions) {
	// Check if this path is masked - if so, skip comparison entirely.
	if options.Mask != nil && !options.Mask.Empty() && options.Mask.Matches(path) {
		return
	}

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
		if nodesBinaryEqual(a, b) {
			return
		}
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
		if !sameScalarWithOptions(a, b, options) {
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
		if options.UnorderedSigners && isSignerCollection(signerCollectionName(path, a, b)) {
			diffMultisetLists(a, b, path, out)
		} else if isOperationCollection(operationCollectionName(path, a, b)) {
			diffOperationLists(a, b, path, out, options)
		} else if isEventCollection(eventCollectionName(path, a, b), a, b) {
			diffEventLists(a, b, path, out, options)
		} else {
			diffListsWithOptions(a, b, path, out, options)
		}

	case KindStruct:
		diffStructsWithOptions(a, b, path, out, options)
	}
}

// diffStructs compares two records field by field, preserving the field order
// of the left-hand side so output reads top-to-bottom like the source.
func diffStructsWithOptions(a, b *Node, path string, out *[]Change, options DiffOptions) {
	// Special handling for Memo union type: compare semantically when both
	// sides have a memo but different arms (e.g., MemoText "100" vs MemoId 100).
	if isMemoStruct(a, b) {
		diffMemoSemantic(a, b, path, out, options)
		return
	}

	// Special handling for LedgerEntry: classify state transitions (created, updated, deleted).
	if isLedgerEntryStruct(a, b) {
		diffLedgerEntryTransition(a, b, path, out, options)
		return
	}

	seen := make(map[string]bool, len(a.Children))

	for _, ac := range a.Children {
		seen[ac.Name] = true
		diffNodesWithOptions(ac, b.Child(ac.Name), join(path, ac.Name), out, options)
	}
	// Fields present only on the right: a union that switched arms shows up
	// here as a removal plus an addition.
	for _, bc := range b.Children {
		if seen[bc.Name] {
			continue
		}
		diffNodesWithOptions(nil, bc, join(path, bc.Name), out, options)
	}
}

// isMemoStruct reports whether the nodes represent a Memo union (XDR type "Memo").
func isMemoStruct(a, b *Node) bool {
	if a == nil || b == nil {
		return false
	}
	return a.TypeName == "Memo" && b.TypeName == "Memo"
}

// diffMemoSemantic compares two Memo unions semantically, recognizing
// equivalent content across different arms (e.g., MemoText "100" ~ MemoId 100).
func diffMemoSemantic(a, b *Node, path string, out *[]Change, options DiffOptions) {
	// Extract memo type and value from each side
	leftType, leftVal := extractMemoArm(a)
	rightType, rightVal := extractMemoArm(b)

	if leftType == rightType {
		// Same arm, do normal field-by-field comparison
		diffStructsWithOptionsFallback(a, b, path, out, options)
		return
	}

	// Different arms - check for semantic equivalence
	if memoValuesEquivalent(leftType, leftVal, rightType, rightVal) {
		// Semantically equivalent: report as a single modified change with a note
		*out = append(*out, Change{
			Path:       path,
			Op:         ChangeModified,
			Before:     leftVal,
			After:      rightVal,
			BeforeNote: fmt.Sprintf("%s (semantically equivalent to %s)", leftType, rightType),
			AfterNote:  fmt.Sprintf("%s (semantically equivalent to %s)", rightType, leftType),
		})
		return
	}

	// Not equivalent - fall back to normal union diff (removal + addition)
	diffStructsWithOptionsFallback(a, b, path, out, options)
}

// diffStructsWithOptionsFallback is the normal struct diff without memo special-casing.
func diffStructsWithOptionsFallback(a, b *Node, path string, out *[]Change, options DiffOptions) {
	seen := make(map[string]bool, len(a.Children))

	for _, ac := range a.Children {
		seen[ac.Name] = true
		diffNodesWithOptions(ac, b.Child(ac.Name), join(path, ac.Name), out, options)
	}
	for _, bc := range b.Children {
		if seen[bc.Name] {
			continue
		}
		diffNodesWithOptions(nil, bc, join(path, bc.Name), out, options)
	}
}

// extractMemoArm extracts the active arm from a Memo node.
func extractMemoArm(n *Node) (string, any) {
	if n == nil {
		return "None", nil
	}
	for _, child := range n.Children {
		if child.Kind != KindScalar {
			continue
		}
		// Memo arms: Type (enum), Text, Id, Hash, Return, HashX
		switch child.Name {
		case "Type":
			continue // We'll determine type from which value arm is present
		case "Text", "Id", "Hash", "Return", "HashX":
			if child.Value != nil {
				return child.Name, child.Value
			}
		}
	}
	return "None", nil
}

// memoValuesEquivalent checks if two memo arms represent semantically equivalent values.
// Currently recognizes: MemoText "N" == MemoId N (for numeric text).
func memoValuesEquivalent(leftType string, leftVal any, rightType string, rightVal any) bool {
	// MemoText "N" <-> MemoId N
	if (leftType == "Text" && rightType == "Id") || (leftType == "Id" && rightType == "Text") {
		var textVal string
		var idVal any
		if leftType == "Text" {
			textVal = fmt.Sprintf("%v", leftVal)
			idVal = rightVal
		} else {
			textVal = fmt.Sprintf("%v", rightVal)
			idVal = leftVal
		}
		// Parse text as integer and compare with id
		textNum, err := strconv.ParseUint(textVal, 10, 64)
		if err != nil {
			return false
		}
		idNum, ok := asNumber(idVal)
		if !ok {
			return false
		}
		return textNum == uint64(idNum)
	}
	return false
}

// LedgerEntryTransition represents the high-level state transition of a LedgerEntry.
type LedgerEntryTransition string

const (
	LedgerEntryCreated LedgerEntryTransition = "CREATED"
	LedgerEntryUpdated LedgerEntryTransition = "UPDATED"
	LedgerEntryDeleted LedgerEntryTransition = "DELETED"
	LedgerEntryUnknown LedgerEntryTransition = "UNKNOWN"
)

// String returns the string representation of the transition.
func (t LedgerEntryTransition) String() string {
	return string(t)
}

// isLedgerEntryStruct reports whether the nodes represent a LedgerEntry (XDR type "LedgerEntry").
func isLedgerEntryStruct(a, b *Node) bool {
	if a == nil || b == nil {
		return false
	}
	return a.TypeName == "LedgerEntry" && b.TypeName == "LedgerEntry"
}

// diffLedgerEntryTransition classifies the state transition between two LedgerEntries
// and adds a high-level transition header change before delegating to normal struct diff.
func diffLedgerEntryTransition(a, b *Node, path string, out *[]Change, options DiffOptions) {
	// First, do the detailed field-by-field diff to a temporary slice
	var detailedChanges []Change
	diffStructsWithOptionsFallback(a, b, path, &detailedChanges, options)

	// Determine the transition type
	transition := classifyLedgerEntryTransition(a, b)

	// Only add transition header if there are actual detailed changes,
	// or if the transition is CREATED/DELETED (structural changes).
	// For UPDATED with no detailed changes, don't add a header (they're equal).
	hasDetailedChanges := len(detailedChanges) > 0
	isStructuralChange := transition == LedgerEntryCreated || transition == LedgerEntryDeleted

	if hasDetailedChanges || isStructuralChange {
		// Add a high-level transition change at the LedgerEntry path
		*out = append(*out, Change{
			Path:       path,
			Op:         ChangeModified,
			Before:     transition.String(),
			After:      transition.String(),
			BeforeNote: fmt.Sprintf("LedgerEntry transition: %s", transition),
			AfterNote:  fmt.Sprintf("LedgerEntry transition: %s", transition),
		})

		// Also include the LedgerKey and entry type for context
		ledgerKey := extractLedgerKey(a)
		entryType := extractLedgerEntryType(a)
		if ledgerKey != "" && entryType != "" {
			*out = append(*out, Change{
				Path:       path + ".LedgerKey",
				Op:         ChangeModified,
				Before:     ledgerKey,
				After:      ledgerKey,
				BeforeNote: fmt.Sprintf("%s (%s)", entryType, ledgerKey),
				AfterNote:  fmt.Sprintf("%s (%s)", entryType, ledgerKey),
			})
		}
	}

	// Append the detailed changes
	*out = append(*out, detailedChanges...)
}

// classifyLedgerEntryTransition determines whether a LedgerEntry was created, updated, or deleted.
func classifyLedgerEntryTransition(a, b *Node) LedgerEntryTransition {
	aHasData := hasLedgerEntryData(a)
	bHasData := hasLedgerEntryData(b)

	switch {
	case !aHasData && bHasData:
		return LedgerEntryCreated
	case aHasData && !bHasData:
		return LedgerEntryDeleted
	case aHasData && bHasData:
		// Both have data - classify as UPDATED (the detailed diff will show if there are actual changes)
		return LedgerEntryUpdated
	default:
		return LedgerEntryUnknown
	}
}

// hasLedgerEntryData reports whether a LedgerEntry node has its Data field populated.
func hasLedgerEntryData(n *Node) bool {
	if n == nil {
		return false
	}
	for _, child := range n.Children {
		if child.Name == "Data" && child.Kind == KindStruct {
			// Check if Data has a non-None type (i.e., not empty/void)
			for _, dc := range child.Children {
				if dc.Name == "Type" && dc.Value != nil {
					typeStr := fmt.Sprintf("%v", dc.Value)
					if typeStr != "LedgerEntryTypeAccount" && typeStr != "" {
						// Actually, even Account is valid data. Just check it's not empty.
						return true
					}
					// All LedgerEntryType values indicate valid data
					return true
				}
			}
		}
	}
	return false
}

// extractLedgerKey extracts a human-readable ledger key from a LedgerEntry node.
func extractLedgerKey(n *Node) string {
	if n == nil {
		return ""
	}
	for _, child := range n.Children {
		if child.Name == "Data" && child.Kind == KindStruct {
			// The key depends on the entry type
			for _, dc := range child.Children {
				if dc.Name == "Type" && dc.Value != nil {
					entryType := fmt.Sprintf("%v", dc.Value)
					// Extract key based on entry type
					switch entryType {
					case "LedgerEntryTypeAccount":
						return extractAccountKey(child)
					case "LedgerEntryTypeTrustline":
						return extractTrustlineKey(child)
					case "LedgerEntryTypeOffer":
						return extractOfferKey(child)
					case "LedgerEntryTypeData":
						return extractDataKey(child)
					case "LedgerEntryTypeClaimableBalance":
						return extractClaimableBalanceKey(child)
					case "LedgerEntryTypeLiquidityPool":
						return extractLiquidityPoolKey(child)
					case "LedgerEntryTypeContractData":
						return extractContractDataKey(child)
					case "LedgerEntryTypeContractCode":
						return extractContractCodeKey(child)
					case "LedgerEntryTypeConfigSetting":
						return extractConfigSettingKey(child)
					case "LedgerEntryTypeTTL":
						return extractTTLKey(child)
					}
				}
			}
		}
	}
	return ""
}

// extractLedgerEntryType extracts the LedgerEntry type as a short string.
func extractLedgerEntryType(n *Node) string {
	if n == nil {
		return ""
	}
	for _, child := range n.Children {
		if child.Name == "Data" && child.Kind == KindStruct {
			for _, dc := range child.Children {
				if dc.Name == "Type" && dc.Value != nil {
					typeStr := fmt.Sprintf("%v", dc.Value)
					// Remove "LedgerEntryType" prefix for readability
					return strings.TrimPrefix(typeStr, "LedgerEntryType")
				}
			}
		}
	}
	return ""
}

// Key extraction helpers for each LedgerEntry type

func extractAccountKey(dataNode *Node) string {
	for _, child := range dataNode.Children {
		if child.Name == "Account" && child.Kind == KindStruct {
			for _, ac := range child.Children {
				if ac.Name == "AccountId" && ac.Value != nil {
					return fmt.Sprintf("Account(%v)", ac.Value)
				}
			}
		}
	}
	return "Account(<unknown>)"
}

func extractTrustlineKey(dataNode *Node) string {
	var account, asset string
	for _, child := range dataNode.Children {
		if child.Name == "Trustline" && child.Kind == KindStruct {
			for _, tc := range child.Children {
				if tc.Name == "AccountId" && tc.Value != nil {
					account = fmt.Sprintf("%v", tc.Value)
				}
				if tc.Name == "Asset" && tc.Kind == KindStruct {
					asset = extractAssetSummary(tc)
				}
			}
		}
	}
	if account != "" && asset != "" {
		return fmt.Sprintf("Trustline(%s, %s)", account, asset)
	}
	return "Trustline(<unknown>)"
}

func extractOfferKey(dataNode *Node) string {
	var sellerID string
	var offerID uint64
	for _, child := range dataNode.Children {
		if child.Name == "Offer" && child.Kind == KindStruct {
			for _, oc := range child.Children {
				if oc.Name == "SellerID" && oc.Value != nil {
					sellerID = fmt.Sprintf("%v", oc.Value)
				}
				if oc.Name == "OfferID" && oc.Value != nil {
					if num, ok := asNumber(oc.Value); ok {
						offerID = uint64(num)
					}
				}
			}
		}
	}
	if sellerID != "" {
		return fmt.Sprintf("Offer(%s, #%d)", sellerID, offerID)
	}
	return "Offer(<unknown>)"
}

func extractDataKey(dataNode *Node) string {
	var account, name string
	for _, child := range dataNode.Children {
		if child.Name == "Data" && child.Kind == KindStruct {
			for _, dc := range child.Children {
				if dc.Name == "AccountId" && dc.Value != nil {
					account = fmt.Sprintf("%v", dc.Value)
				}
				if dc.Name == "DataName" && dc.Value != nil {
					name = fmt.Sprintf("%v", dc.Value)
				}
			}
		}
	}
	if account != "" && name != "" {
		return fmt.Sprintf("Data(%s, %q)", account, name)
	}
	return "Data(<unknown>)"
}

func extractClaimableBalanceKey(dataNode *Node) string {
	for _, child := range dataNode.Children {
		if child.Name == "ClaimableBalance" && child.Kind == KindStruct {
			for _, bc := range child.Children {
				if bc.Name == "BalanceId" && bc.Value != nil {
					return fmt.Sprintf("ClaimableBalance(%v)", bc.Value)
				}
			}
		}
	}
	return "ClaimableBalance(<unknown>)"
}

func extractLiquidityPoolKey(dataNode *Node) string {
	for _, child := range dataNode.Children {
		if child.Name == "LiquidityPool" && child.Kind == KindStruct {
			for _, lc := range child.Children {
				if lc.Name == "LiquidityPoolId" && lc.Value != nil {
					return fmt.Sprintf("LiquidityPool(%v)", lc.Value)
				}
			}
		}
	}
	return "LiquidityPool(<unknown>)"
}

func extractContractDataKey(dataNode *Node) string {
	var contract, key string
	for _, child := range dataNode.Children {
		if child.Name == "ContractData" && child.Kind == KindStruct {
			for _, dc := range child.Children {
				if dc.Name == "Contract" && dc.Value != nil {
					contract = fmt.Sprintf("%v", dc.Value)
				}
				if dc.Name == "Key" && dc.Kind == KindStruct {
					key = extractScValSummary(dc)
				}
			}
		}
	}
	if contract != "" && key != "" {
		return fmt.Sprintf("ContractData(%s, %s)", contract, key)
	}
	return "ContractData(<unknown>)"
}

func extractContractCodeKey(dataNode *Node) string {
	for _, child := range dataNode.Children {
		if child.Name == "ContractCode" && child.Kind == KindStruct {
			for _, cc := range child.Children {
				if cc.Name == "Hash" && cc.Value != nil {
					return fmt.Sprintf("ContractCode(%v)", cc.Value)
				}
			}
		}
	}
	return "ContractCode(<unknown>)"
}

func extractConfigSettingKey(dataNode *Node) string {
	for _, child := range dataNode.Children {
		if child.Name == "ConfigSetting" && child.Kind == KindStruct {
			for _, cc := range child.Children {
				if cc.Name == "ConfigSettingId" && cc.Value != nil {
					return fmt.Sprintf("ConfigSetting(%v)", cc.Value)
				}
			}
		}
	}
	return "ConfigSetting(<unknown>)"
}

func extractTTLKey(dataNode *Node) string {
	for _, child := range dataNode.Children {
		if child.Name == "TTL" && child.Kind == KindStruct {
			for _, tc := range child.Children {
				if tc.Name == "KeyHash" && tc.Value != nil {
					return fmt.Sprintf("TTL(%v)", tc.Value)
				}
			}
		}
	}
	return "TTL(<unknown>)"
}

func extractAssetSummary(assetNode *Node) string {
	var assetType, code, issuer string
	for _, child := range assetNode.Children {
		if child.Name == "Type" && child.Value != nil {
			assetType = fmt.Sprintf("%v", child.Value)
		}
		if child.Name == "AlphaNum4" && child.Kind == KindStruct {
			for _, ac := range child.Children {
				if ac.Name == "AssetCode" && ac.Value != nil {
					code = fmt.Sprintf("%v", ac.Value)
				}
				if ac.Name == "Issuer" && ac.Value != nil {
					issuer = fmt.Sprintf("%v", ac.Value)
				}
			}
		}
		if child.Name == "AlphaNum12" && child.Kind == KindStruct {
			for _, ac := range child.Children {
				if ac.Name == "AssetCode" && ac.Value != nil {
					code = fmt.Sprintf("%v", ac.Value)
				}
				if ac.Name == "Issuer" && ac.Value != nil {
					issuer = fmt.Sprintf("%v", ac.Value)
				}
			}
		}
	}
	if assetType == "AssetTypeNative" || assetType == "LedgerEntryTypeNative" {
		return "native"
	}
	if code != "" && issuer != "" {
		return fmt.Sprintf("%s:%s", code, issuer)
	}
	return assetType
}

func extractScValSummary(n *Node) string {
	if n == nil {
		return ""
	}
	for _, arm := range []string{"Sym", "Str", "Address", "U32", "U64", "I32", "I64", "U128", "I128", "Bool", "Bytes"} {
		if c := n.Child(arm); c != nil && c.Value != nil {
			return fmt.Sprintf("%s:%v", arm, c.Value)
		}
	}
	if n.Value != nil {
		return fmt.Sprintf("%v", n.Value)
	}
	return ""
}

const maxAlignmentCells = 1 << 20

// diffListsWithOptions compares sequences positionally unless alignment was
// requested. Very large products fall back to positional comparison to bound
// memory use on untrusted XDR input.
func diffListsWithOptions(a, b *Node, path string, out *[]Change, options DiffOptions) {
	if options.AlignLists && len(a.Children) > 0 && len(b.Children) > 0 &&
		len(a.Children) <= maxAlignmentCells/len(b.Children) {
		diffAlignedLists(a.Children, b.Children, path, out, options)
		return
	}
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
		diffNodesWithOptions(ac, bc, path+"["+strconv.Itoa(i)+"]", out, options)
	}
}

func diffAlignedLists(left, right []*Node, path string, out *[]Change, options DiffOptions) {
	rows, columns := len(left)+1, len(right)+1
	lcs := make([][]int, rows)
	for i := range rows {
		lcs[i] = make([]int, columns)
	}
	for i := len(left) - 1; i >= 0; i-- {
		for j := len(right) - 1; j >= 0; j-- {
			if sameNodeWithOptions(left[i], right[j], options) {
				lcs[i][j] = 1 + lcs[i+1][j+1]
			} else if lcs[i+1][j] > lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}

	i, j := 0, 0
	for i < len(left) && j < len(right) {
		if sameNodeWithOptions(left[i], right[j], options) {
			diffNodesWithOptions(left[i], right[j], path+"["+strconv.Itoa(j)+"]", out, options)
			i++
			j++
		} else if lcs[i][j+1] >= lcs[i+1][j] {
			diffNodesWithOptions(nil, right[j], path+"["+strconv.Itoa(j)+"]", out, options)
			j++
		} else {
			diffNodesWithOptions(left[i], nil, path+"["+strconv.Itoa(i)+"]", out, options)
			i++
		}
	}
	for ; i < len(left); i++ {
		diffNodesWithOptions(left[i], nil, path+"["+strconv.Itoa(i)+"]", out, options)
	}
	for ; j < len(right); j++ {
		diffNodesWithOptions(nil, right[j], path+"["+strconv.Itoa(j)+"]", out, options)
	}
}

func sameNodeWithOptions(a, b *Node, options DiffOptions) bool {
	if a == nil || b == nil || a.Name != b.Name {
		return a == b
	}
	if a.Kind != b.Kind {
		return nodesBinaryEqual(a, b)
	}
	if a.Kind == KindScalar {
		return sameScalarWithOptions(a, b, options)
	}
	if len(a.Children) != len(b.Children) {
		return false
	}
	for i := range a.Children {
		if !sameNodeWithOptions(a.Children[i], b.Children[i], options) {
			return false
		}
	}
	return true
}

func sameScalarWithOptions(a, b *Node, options DiffOptions) bool {
	threshold := options.Threshold
	if threshold == 0 && options.DiffThreshold > 0 {
		threshold = options.DiffThreshold
	}

	// 1. Direct scalar value and note match
	if a.Note == b.Note {
		if reflect.DeepEqual(a.Value, b.Value) || fmt.Sprintf("%v", a.Value) == fmt.Sprintf("%v", b.Value) {
			return true
		}
	}

	// 2. Numeric delta threshold filter (--diff-threshold)
	if threshold > 0 {
		if numA, okA := asNumber(a.Value); okA {
			if numB, okB := asNumber(b.Value); okB {
				if math.Abs(numA-numB) <= threshold {
					return true
				}
			}
		}
	}

	// 3. Binary representation normalization (Hex vs Base64 vs Bytes)
	if nodesBinaryEqual(a, b) {
		if a.Note == b.Note || a.Note == "" || b.Note == "" || notesCompatible(a.Note, b.Note) {
			return true
		}
	}

	return false
}

func notesCompatible(aNote, bNote string) bool {
	if aNote == bNote {
		return true
	}
	aClean := strings.Trim(aNote, "\"")
	bClean := strings.Trim(bNote, "\"")
	return aClean == bClean
}

func asNumber(v any) (float64, bool) {
	if v == nil {
		return 0, false
	}
	switch val := v.(type) {
	case int:
		return float64(val), true
	case int8:
		return float64(val), true
	case int16:
		return float64(val), true
	case int32:
		return float64(val), true
	case int64:
		return float64(val), true
	case uint:
		return float64(val), true
	case uint8:
		return float64(val), true
	case uint16:
		return float64(val), true
	case uint32:
		return float64(val), true
	case uint64:
		return float64(val), true
	case float32:
		return float64(val), true
	case float64:
		return val, true
	case string:
		f, err := strconv.ParseFloat(val, 64)
		if err == nil {
			return f, true
		}
		return 0, false
	default:
		return 0, false
	}
}

func bytesFromHex(s string) ([]byte, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "0x")
	s = strings.TrimPrefix(s, "0X")
	if len(s) == 0 || len(s)%2 != 0 {
		return nil, false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return nil, false
		}
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, false
	}
	return b, true
}

func bytesFromBase64(s string) ([]byte, bool) {
	s = strings.TrimSpace(s)
	if len(s) == 0 {
		return nil, false
	}
	if b, err := base64.StdEncoding.DecodeString(s); err == nil && len(b) > 0 {
		return b, true
	}
	if b, err := base64.RawStdEncoding.DecodeString(s); err == nil && len(b) > 0 {
		return b, true
	}
	return nil, false
}

func extractDirectBytes(v any) ([]byte, bool) {
	if v == nil {
		return nil, false
	}
	switch val := v.(type) {
	case []byte:
		if len(val) == 0 {
			return nil, false
		}
		return val, true
	default:
		rv := reflect.ValueOf(v)
		if rv.IsValid() && rv.Kind() == reflect.Array && rv.Type().Elem().Kind() == reflect.Uint8 {
			if rv.Len() == 0 {
				return nil, false
			}
			b := make([]byte, rv.Len())
			for i := 0; i < rv.Len(); i++ {
				b[i] = byte(rv.Index(i).Uint())
			}
			return b, true
		}
		return nil, false
	}
}

func extractListBytes(n *Node) ([]byte, bool) {
	if n == nil || n.Kind != KindList || len(n.Children) == 0 {
		return nil, false
	}
	b := make([]byte, len(n.Children))
	for i, child := range n.Children {
		if child.Kind != KindScalar {
			return nil, false
		}
		num, ok := asNumber(child.Value)
		if !ok || num < 0 || num > 255 || math.Floor(num) != num {
			return nil, false
		}
		b[i] = byte(num)
	}
	return b, true
}

func nodesBinaryEqual(a, b *Node) bool {
	if a == nil || b == nil {
		return false
	}

	// Case 1: Both are scalar strings (one hex, one base64, or hex case diff)
	sA, okSA := a.Value.(string)
	sB, okSB := b.Value.(string)
	if okSA && okSB && a.Kind == KindScalar && b.Kind == KindScalar {
		bHexA, isHexA := bytesFromHex(sA)
		bHexB, isHexB := bytesFromHex(sB)
		bB64A, isB64A := bytesFromBase64(sA)
		bB64B, isB64B := bytesFromBase64(sB)

		// One is Hex, the other is Base64:
		if isHexA && isB64B && bytes.Equal(bHexA, bB64B) {
			return true
		}
		if isB64A && isHexB && bytes.Equal(bB64A, bHexB) {
			return true
		}
		// Both are Hex (case differences):
		if isHexA && isHexB && bytes.Equal(bHexA, bHexB) {
			return true
		}
		return false
	}

	// Case 2: One is raw bytes or byte array or byte list, the other is scalar string (hex or base64)
	var rawBytesA []byte
	if bytesA, ok := extractDirectBytes(a.Value); ok {
		rawBytesA = bytesA
	} else if bytesA, ok := extractListBytes(a); ok {
		rawBytesA = bytesA
	}

	var rawBytesB []byte
	if bytesB, ok := extractDirectBytes(b.Value); ok {
		rawBytesB = bytesB
	} else if bytesB, ok := extractListBytes(b); ok {
		rawBytesB = bytesB
	}

	if rawBytesA != nil && rawBytesB != nil {
		return bytes.Equal(rawBytesA, rawBytesB)
	}

	if rawBytesA != nil && okSB {
		if bHex, ok := bytesFromHex(sB); ok && bytes.Equal(rawBytesA, bHex) {
			return true
		}
		if bB64, ok := bytesFromBase64(sB); ok && bytes.Equal(rawBytesA, bB64) {
			return true
		}
	}

	if rawBytesB != nil && okSA {
		if bHex, ok := bytesFromHex(sA); ok && bytes.Equal(rawBytesB, bHex) {
			return true
		}
		if bB64, ok := bytesFromBase64(sA); ok && bytes.Equal(rawBytesB, bB64) {
			return true
		}
	}

	return false
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
	return DiffBase64WithOptions(leftPayload, rightPayload, typeName, DiffOptions{})
}

// DiffBase64WithOptions decodes and compares two payloads using the supplied
// structural diff options. When typeName is empty, each side is detected
// independently.
func DiffBase64WithOptions(leftPayload, rightPayload, typeName string, options DiffOptions) (*DiffResult, error) {
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
	return DiffWithOptions(left, right, options)
}

// Summary renders a one-line description of the diff, suitable for a CI log.
func (d *DiffResult) Summary() string {
	if d.Equal() {
		return "no differences"
	}
	var added, removed, modified, reordered int
	for _, c := range d.Changes {
		switch c.Op {
		case ChangeAdded:
			added++
		case ChangeRemoved:
			removed++
		case ChangeReordered:
			reordered++
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
	if reordered > 0 {
		parts = append(parts, strconv.Itoa(reordered)+" reordered")
	}
	return strings.Join(parts, ", ")
}

// isOperationCollection reports whether name identifies an operations sequence.
func isOperationCollection(name string) bool {
	return name == "Operations"
}

// operationCollectionName resolves the field name of an operations list node.
func operationCollectionName(path string, a, b *Node) string {
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

// diffOperationLists performs permutation detection on operation sequences, identifying
// operations that are identical in body but shifted in index and reporting them as
// ChangeReordered rather than separate delete/insert pairs.
func diffOperationLists(a, b *Node, path string, out *[]Change, options DiffOptions) {
	leftOps := a.Children
	rightOps := b.Children

	matchedLeft := make([]bool, len(leftOps))
	matchedRight := make([]bool, len(rightOps))

	// Pass 1: exact positional match for identical operations.
	minLen := len(leftOps)
	if len(rightOps) < minLen {
		minLen = len(rightOps)
	}
	for i := 0; i < minLen; i++ {
		if sameNodeWithOptions(leftOps[i], rightOps[i], options) {
			matchedLeft[i] = true
			matchedRight[i] = true
		}
	}

	// Pass 2: permutation matching for operations with identical body shifted in index.
	type reorderMatch struct {
		from int
		to   int
	}
	var reordered []reorderMatch
	for i := 0; i < len(leftOps); i++ {
		if matchedLeft[i] {
			continue
		}
		for j := 0; j < len(rightOps); j++ {
			if matchedRight[j] {
				continue
			}
			if sameNodeWithOptions(leftOps[i], rightOps[j], options) {
				matchedLeft[i] = true
				matchedRight[j] = true
				reordered = append(reordered, reorderMatch{from: i, to: j})
				break
			}
		}
	}

	// Pass 3: record reordered changes and diff remaining unmatched operations.
	type indexedChange struct {
		pos int
		c   Change
	}
	var indexed []indexedChange

	for _, rm := range reordered {
		indexed = append(indexed, indexedChange{
			pos: rm.to,
			c: Change{
				Path:       path + "[" + strconv.Itoa(rm.to) + "]",
				Op:         ChangeReordered,
				Before:     rm.from,
				After:      rm.to,
				BeforeNote: fmt.Sprintf("index %d", rm.from),
				AfterNote:  fmt.Sprintf("index %d", rm.to),
			},
		})
	}

	var unmatchedLeft, unmatchedRight []int
	for i, m := range matchedLeft {
		if !m {
			unmatchedLeft = append(unmatchedLeft, i)
		}
	}
	for j, m := range matchedRight {
		if !m {
			unmatchedRight = append(unmatchedRight, j)
		}
	}

	pairs := len(unmatchedLeft)
	if len(unmatchedRight) < pairs {
		pairs = len(unmatchedRight)
	}
	for k := 0; k < pairs; k++ {
		li := unmatchedLeft[k]
		rj := unmatchedRight[k]
		var sub []Change
		diffNodesWithOptions(leftOps[li], rightOps[rj], path+"["+strconv.Itoa(rj)+"]", &sub, options)
		for _, sc := range sub {
			indexed = append(indexed, indexedChange{pos: rj, c: sc})
		}
	}
	for k := pairs; k < len(unmatchedLeft); k++ {
		li := unmatchedLeft[k]
		var sub []Change
		diffNodesWithOptions(leftOps[li], nil, path+"["+strconv.Itoa(li)+"]", &sub, options)
		for _, sc := range sub {
			indexed = append(indexed, indexedChange{pos: li, c: sc})
		}
	}
	for k := pairs; k < len(unmatchedRight); k++ {
		rj := unmatchedRight[k]
		var sub []Change
		diffNodesWithOptions(nil, rightOps[rj], path+"["+strconv.Itoa(rj)+"]", &sub, options)
		for _, sc := range sub {
			indexed = append(indexed, indexedChange{pos: rj, c: sc})
		}
	}

	sort.SliceStable(indexed, func(i, j int) bool {
		return indexed[i].pos < indexed[j].pos
	})
	for _, ic := range indexed {
		*out = append(*out, ic.c)
	}
}

// isEventCollection reports whether name or children describe contract event logs.
func isEventCollection(name string, a, b *Node) bool {
	switch name {
	case "Events", "DiagnosticEvents":
		return true
	}
	if a != nil && len(a.Children) > 0 && isEventNode(a.Children[0]) {
		return true
	}
	if b != nil && len(b.Children) > 0 && isEventNode(b.Children[0]) {
		return true
	}
	return false
}

// eventCollectionName resolves the field name of an events list node.
func eventCollectionName(path string, a, b *Node) string {
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

func isEventNode(n *Node) bool {
	if n == nil {
		return false
	}
	if n.TypeName == "ContractEvent" || n.TypeName == "DiagnosticEvent" {
		return true
	}
	return n.Child("ContractId") != nil || n.Child("Event") != nil
}

type eventLogMeta struct {
	contractID   string
	primaryTopic string
	fullTopics   string
}

func extractEventLogMeta(n *Node) eventLogMeta {
	if n == nil {
		return eventLogMeta{}
	}
	evNode := n
	if child := n.Child("Event"); child != nil {
		evNode = child
	}
	var cid string
	if c := evNode.Child("ContractId"); c != nil && c.Value != nil {
		cid = fmt.Sprint(c.Value)
	}
	body := evNode.Child("Body")
	var v0 *Node
	if body != nil {
		v0 = body.Child("V0")
	}
	var primaryTopic string
	var topicsList []string
	if v0 != nil {
		topicsNode := v0.Child("Topics")
		if topicsNode != nil {
			for _, t := range topicsNode.Children {
				s := scValNodeSummary(t)
				topicsList = append(topicsList, s)
			}
		}
	}
	if len(topicsList) > 0 {
		primaryTopic = topicsList[0]
	}
	return eventLogMeta{
		contractID:   cid,
		primaryTopic: primaryTopic,
		fullTopics:   strings.Join(topicsList, "::"),
	}
}

func scValNodeSummary(n *Node) string {
	if n == nil {
		return ""
	}
	for _, arm := range []string{"Sym", "Str", "Address", "U32", "U64", "I32", "I64", "U128", "I128", "Bool", "Bytes"} {
		if c := n.Child(arm); c != nil && c.Value != nil {
			return fmt.Sprint(c.Value)
		}
	}
	if n.Value != nil {
		return fmt.Sprint(n.Value)
	}
	return ""
}

// diffEventLists performs dedicated Soroban ContractEvent log comparison, matching
// events by contract ID and topic signatures, and highlighting altered data values and topic mutations cleanly.
func diffEventLists(a, b *Node, path string, out *[]Change, options DiffOptions) {
	leftEvents := a.Children
	rightEvents := b.Children

	leftMetas := make([]eventLogMeta, len(leftEvents))
	for i, e := range leftEvents {
		leftMetas[i] = extractEventLogMeta(e)
	}
	rightMetas := make([]eventLogMeta, len(rightEvents))
	for j, e := range rightEvents {
		rightMetas[j] = extractEventLogMeta(e)
	}

	matchedLeft := make([]bool, len(leftEvents))
	matchedRight := make([]bool, len(rightEvents))

	type pairedEvent struct {
		leftIdx  int
		rightIdx int
	}
	var paired []pairedEvent

	// Pass 1: exact match on contract ID and full topic signature.
	for i := range leftEvents {
		for j := range rightEvents {
			if matchedRight[j] {
				continue
			}
			if leftMetas[i].contractID == rightMetas[j].contractID &&
				leftMetas[i].fullTopics == rightMetas[j].fullTopics {
				matchedLeft[i] = true
				matchedRight[j] = true
				paired = append(paired, pairedEvent{leftIdx: i, rightIdx: j})
				break
			}
		}
	}

	// Pass 2: match on contract ID and primary topic signature (highlights topic mutations & data changes).
	for i := range leftEvents {
		if matchedLeft[i] {
			continue
		}
		for j := range rightEvents {
			if matchedRight[j] {
				continue
			}
			if leftMetas[i].contractID == rightMetas[j].contractID &&
				leftMetas[i].primaryTopic != "" &&
				leftMetas[i].primaryTopic == rightMetas[j].primaryTopic {
				matchedLeft[i] = true
				matchedRight[j] = true
				paired = append(paired, pairedEvent{leftIdx: i, rightIdx: j})
				break
			}
		}
	}

	// Diff paired events
	for _, p := range paired {
		diffNodesWithOptions(leftEvents[p.leftIdx], rightEvents[p.rightIdx], path+"["+strconv.Itoa(p.rightIdx)+"]", out, options)
	}

	// Unpaired left events -> removed
	for i, m := range matchedLeft {
		if !m {
			diffNodesWithOptions(leftEvents[i], nil, path+"["+strconv.Itoa(i)+"]", out, options)
		}
	}
	// Unpaired right events -> added
	for j, m := range matchedRight {
		if !m {
			diffNodesWithOptions(nil, rightEvents[j], path+"["+strconv.Itoa(j)+"]", out, options)
		}
	}
}

// DiffContractEvents structurally compares two slices of Soroban ContractEvent elements,
// matching them by contract ID and topic signatures to highlight altered data values and topic mutations cleanly.
func DiffContractEvents(left, right []xdr.ContractEvent) (*DiffResult, error) {
	leftNode := &Node{Name: "Events", Kind: KindList, TypeName: "ContractEventList"}
	for _, ev := range left {
		leftNode.Children = append(leftNode.Children, nodeFor("", ev))
	}
	rightNode := &Node{Name: "Events", Kind: KindList, TypeName: "ContractEventList"}
	for _, ev := range right {
		rightNode.Children = append(rightNode.Children, nodeFor("", ev))
	}
	res := &DiffResult{LeftType: "[]ContractEvent", RightType: "[]ContractEvent"}
	diffEventLists(leftNode, rightNode, "Events", &res.Changes, DiffOptions{})
	return res, nil
}

// SignerQuorumAnalysis details effective signing power and quorum status.
type SignerQuorumAnalysis struct {
	BeforeWeight  uint32   `json:"before_weight"`
	AfterWeight   uint32   `json:"after_weight"`
	WeightDelta   int64    `json:"weight_delta"`
	LowThreshold  uint8    `json:"low_threshold"`
	MedThreshold  uint8    `json:"med_threshold"`
	HighThreshold uint8    `json:"high_threshold"`
	MeetsMedium   bool     `json:"meets_medium"`
	MeetsHigh     bool     `json:"meets_high"`
	Warnings      []string `json:"warnings,omitempty"`
}

// AnalyzeAccountSigners computes effective signing power changes between two
// account entries and warns if total signer weight falls below medium or high threshold.
func AnalyzeAccountSigners(left, right *xdr.AccountEntry) SignerQuorumAnalysis {
	var beforeWeight, afterWeight uint32
	var low, med, high uint8

	if left != nil {
		m, _, _, _ := AccountThresholds(left)
		if m > 0 {
			beforeWeight += uint32(m)
		}
		for _, s := range left.Signers {
			beforeWeight += uint32(s.Weight)
		}
	}
	if right != nil {
		m, l, md, h := AccountThresholds(right)
		low, med, high = l, md, h
		if m > 0 {
			afterWeight += uint32(m)
		}
		for _, s := range right.Signers {
			afterWeight += uint32(s.Weight)
		}
	}

	delta := int64(afterWeight) - int64(beforeWeight)
	meetsMed := afterWeight >= uint32(med)
	meetsHigh := afterWeight >= uint32(high)

	var warnings []string
	if !meetsMed && med > 0 {
		warnings = append(warnings, fmt.Sprintf("total signer weight (%d) falls below medium threshold (%d)", afterWeight, med))
	}
	if !meetsHigh && high > 0 {
		warnings = append(warnings, fmt.Sprintf("total signer weight (%d) falls below high threshold (%d)", afterWeight, high))
	}

	return SignerQuorumAnalysis{
		BeforeWeight:  beforeWeight,
		AfterWeight:   afterWeight,
		WeightDelta:   delta,
		LowThreshold:  low,
		MedThreshold:  med,
		HighThreshold: high,
		MeetsMedium:   meetsMed,
		MeetsHigh:     meetsHigh,
		Warnings:      warnings,
	}
}

// DiffAccountSigners performs signer diff analysis on two AccountEntry values, computing
// effective signing power changes and generating quorum drift warnings.
func DiffAccountSigners(left, right *xdr.AccountEntry) SignerQuorumAnalysis {
	return AnalyzeAccountSigners(left, right)
}

func analyzeAccountQuorum(left, right *Value, leftNode, rightNode *Node, changes *[]Change, warnings *[]string) {
	leftEntry := extractAccountEntry(left, leftNode)
	rightEntry := extractAccountEntry(right, rightNode)
	if leftEntry == nil && rightEntry == nil {
		return
	}
	analysis := AnalyzeAccountSigners(leftEntry, rightEntry)
	if analysis.BeforeWeight != analysis.AfterWeight {
		*changes = append(*changes, Change{
			Path:       "Thresholds.EffectiveSigningPower",
			Op:         ChangeModified,
			Before:     analysis.BeforeWeight,
			After:      analysis.AfterWeight,
			BeforeNote: fmt.Sprintf("total signing weight %d", analysis.BeforeWeight),
			AfterNote:  fmt.Sprintf("total signing weight %d", analysis.AfterWeight),
		})
	}
	if len(analysis.Warnings) > 0 {
		*warnings = append(*warnings, analysis.Warnings...)
	}
}

func extractAccountEntry(v *Value, n *Node) *xdr.AccountEntry {
	if v != nil && v.Raw != nil {
		if ae, ok := v.Raw.(*xdr.AccountEntry); ok {
			return ae
		}
		if le, ok := v.Raw.(*xdr.LedgerEntry); ok && le.Data.Type == xdr.LedgerEntryTypeAccount {
			return le.Data.Account
		}
	}
	if n != nil {
		if n.TypeName == "AccountEntry" || n.Child("Thresholds") != nil {
			var ae xdr.AccountEntry
			if th := n.Child("Thresholds"); th != nil {
				if s, ok := th.Value.(string); ok {
					if b, ok := bytesFromHex(s); ok && len(b) == 4 {
						copy(ae.Thresholds[:], b)
					}
				}
			}
			if s := n.Child("Signers"); s != nil {
				for _, sc := range s.Children {
					var signer xdr.Signer
					if w := sc.Child("Weight"); w != nil {
						if num, ok := asNumber(w.Value); ok {
							signer.Weight = xdr.Uint32(num)
						}
					}
					ae.Signers = append(ae.Signers, signer)
				}
			}
			return &ae
		}
	}
	return nil
}
