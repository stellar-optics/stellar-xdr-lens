package lens

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/stellar/go-stellar-sdk/xdr"
)

// Claimable balance entries, claimants and the predicate trees that guard
// them.
//
// A claim predicate is a recursive union: unconditional, absolute time,
// relative time, not, and, or. Rendered arm by arm it is close to unreadable,
// so the enricher here folds it into a boolean expression that preserves the
// nesting but reads left to right, for example
// "(before 2026-01-01T00:00:00Z or unconditional)".

func init() {
	registerEnricher(reflect.TypeOf(xdr.ClaimPredicate{}), enrichClaimPredicate)
	registerEnricher(reflect.TypeOf(xdr.ClaimableBalanceId{}), enrichClaimableBalanceID)
	registerFieldNote(reflect.TypeOf(xdr.ClaimableBalanceEntryExtensionV1{}), "Flags", claimableBalanceFlagNote)
	nodeHooks[reflect.TypeOf(xdr.ClaimableBalanceEntry{})] = decorateClaimableBalanceEntry
}

// DescribeClaimPredicate renders a claim predicate union as a boolean
// expression. A predicate with a dangling or unknown arm falls back to the
// constant name rather than to an invented rendering.
func DescribeClaimPredicate(p xdr.ClaimPredicate) string {
	switch p.Type {
	case xdr.ClaimPredicateTypeClaimPredicateUnconditional:
		return "unconditional"
	case xdr.ClaimPredicateTypeClaimPredicateBeforeAbsoluteTime:
		if p.AbsBefore != nil {
			return "before " + time.Unix(int64(*p.AbsBefore), 0).UTC().Format(time.RFC3339)
		}
		return "before (absolute time)"
	case xdr.ClaimPredicateTypeClaimPredicateBeforeRelativeTime:
		if p.RelBefore != nil {
			return fmt.Sprintf("before %d seconds after creation", *p.RelBefore)
		}
		return "before (relative time)"
	case xdr.ClaimPredicateTypeClaimPredicateNot:
		if p.NotPredicate != nil && *p.NotPredicate != nil {
			return fmt.Sprintf("not (%s)", DescribeClaimPredicate(**p.NotPredicate))
		}
		return "not"
	case xdr.ClaimPredicateTypeClaimPredicateAnd:
		if p.AndPredicates != nil {
			parts := make([]string, 0, len(*p.AndPredicates))
			for _, ap := range *p.AndPredicates {
				parts = append(parts, DescribeClaimPredicate(ap))
			}
			return "(" + strings.Join(parts, " and ") + ")"
		}
		return "and"
	case xdr.ClaimPredicateTypeClaimPredicateOr:
		if p.OrPredicates != nil {
			parts := make([]string, 0, len(*p.OrPredicates))
			for _, op := range *p.OrPredicates {
				parts = append(parts, DescribeClaimPredicate(op))
			}
			return "(" + strings.Join(parts, " or ") + ")"
		}
		return "or"
	default:
		// All known predicate types are handled above, so this is an arm
		// added by a newer protocol. Use its constant name when the SDK
		// knows it; the generated String() is empty for those it does not.
		if name := p.Type.String(); name != "" {
			return name
		}
		return fmt.Sprintf("unknown predicate type (%d)", p.Type)
	}
}

// enrichClaimPredicate returns the expression as the scalar payload, not just
// as a note, so JSON output shows "(before ... or unconditional)" instead of
// a nest of union arms.
func enrichClaimPredicate(rv reflect.Value) (string, any, bool) {
	p, valid := rv.Interface().(xdr.ClaimPredicate)
	if !valid {
		return "", nil, false
	}
	return "", DescribeClaimPredicate(p), true
}

// enrichClaimableBalanceID renders the payload hash as the canonical B...
// strkey, which is the form Horizon and wallets show.
func enrichClaimableBalanceID(rv reflect.Value) (string, any, bool) {
	id, valid := rv.Interface().(xdr.ClaimableBalanceId)
	if !valid {
		return "", nil, false
	}
	str, err := id.EncodeToStrkey()
	if err != nil || str == "" {
		return "", nil, false
	}
	return "", str, true
}

// decorateClaimableBalanceEntry annotates the BalanceId leaf with the two
// facts the id cannot carry: how many claimants share it and which asset it
// pays. The B... strkey stays the scalar value; the note is the context that
// makes a bare balance id placeable in a wallet or explorer view.
func decorateClaimableBalanceEntry(n *Node, v any) {
	entry, ok := v.(xdr.ClaimableBalanceEntry)
	if !ok {
		return
	}
	idNode := n.Child("BalanceId")
	if idNode == nil {
		return
	}
	idNode.Note = claimantCountLabel(len(entry.Claimants))
	if asset, ok := canonicalAssetName(entry.Asset); ok {
		idNode.Note += "; asset " + asset
	}
}

// claimantCountLabel renders a claimant count with the right plural. The
// protocol requires at least one claimant to create a balance, but a malformed
// entry can carry none, and saying "0 claimants" is still more useful than
// silence.
func claimantCountLabel(count int) string {
	if count == 1 {
		return "1 claimant"
	}
	return strconv.Itoa(count) + " claimants"
}

// canonicalAssetName renders an asset in its SEP-11 canonical form, declining
// unknown types and malformed credit arms instead of panicking. Generated
// asset accessors dereference union arms without checking the discriminant, so
// the call is guarded even after the type check.
func canonicalAssetName(a xdr.Asset) (name string, ok bool) {
	switch a.Type {
	case xdr.AssetTypeAssetTypeNative,
		xdr.AssetTypeAssetTypeCreditAlphanum4,
		xdr.AssetTypeAssetTypeCreditAlphanum12:
	default:
		return "", false
	}
	defer func() {
		if r := recover(); r != nil {
			name, ok = "", false
		}
	}()
	return a.StringCanonical(), true
}

// claimableBalanceFlagNote names the only defined claimable balance flag.
func claimableBalanceFlagNote(rv reflect.Value) (string, bool) {
	flags, ok := rv.Interface().(xdr.Uint32)
	if !ok {
		return "", false
	}
	if xdr.ClaimableBalanceFlags(flags).IsClawbackEnabled() {
		return "clawback enabled", true
	}
	return "", false
}
