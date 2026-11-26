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

// ValidateClaimPredicate verifies that a ClaimPredicate has valid discriminants,
// non-nil inner fields for active arms, non-negative relative offsets, and does
// not exceed the Stellar protocol maximum recursion depth of 4.
func ValidateClaimPredicate(p xdr.ClaimPredicate) error {
	return validateClaimPredicateDepth(p, 0)
}

func validateClaimPredicateDepth(p xdr.ClaimPredicate, depth int) error {
	if depth > maxClaimPredicateDepth {
		return fmt.Errorf("%w: claim predicate exceeds maximum depth %d", ErrInvalidXDR, maxClaimPredicateDepth)
	}

	switch p.Type {
	case xdr.ClaimPredicateTypeClaimPredicateUnconditional:
		return nil

	case xdr.ClaimPredicateTypeClaimPredicateBeforeAbsoluteTime:
		if p.AbsBefore == nil {
			return fmt.Errorf("%w: absolute-time claim predicate has no threshold", ErrInvalidXDR)
		}
		if *p.AbsBefore < 0 {
			return fmt.Errorf("%w: absolute-time claim predicate cannot be negative: %d", ErrInvalidXDR, *p.AbsBefore)
		}
		return nil

	case xdr.ClaimPredicateTypeClaimPredicateBeforeRelativeTime:
		if p.RelBefore == nil {
			return fmt.Errorf("%w: relative-time claim predicate has no duration", ErrInvalidXDR)
		}
		if *p.RelBefore < 0 {
			return fmt.Errorf("%w: relative-time claim predicate duration cannot be negative: %d", ErrInvalidXDR, *p.RelBefore)
		}
		return nil

	case xdr.ClaimPredicateTypeClaimPredicateNot:
		if p.NotPredicate == nil || *p.NotPredicate == nil {
			return fmt.Errorf("%w: NOT claim predicate has no child", ErrInvalidXDR)
		}
		return validateClaimPredicateDepth(**p.NotPredicate, depth+1)

	case xdr.ClaimPredicateTypeClaimPredicateAnd:
		if p.AndPredicates == nil || len(*p.AndPredicates) == 0 {
			return fmt.Errorf("%w: AND claim predicate has no children", ErrInvalidXDR)
		}
		if len(*p.AndPredicates) > 2 {
			return fmt.Errorf("%w: AND claim predicate exceeds maximum children of 2", ErrInvalidXDR)
		}
		for i, child := range *p.AndPredicates {
			if err := validateClaimPredicateDepth(child, depth+1); err != nil {
				return fmt.Errorf("and child [%d]: %w", i, err)
			}
		}
		return nil

	case xdr.ClaimPredicateTypeClaimPredicateOr:
		if p.OrPredicates == nil || len(*p.OrPredicates) == 0 {
			return fmt.Errorf("%w: OR claim predicate has no children", ErrInvalidXDR)
		}
		if len(*p.OrPredicates) > 2 {
			return fmt.Errorf("%w: OR claim predicate exceeds maximum children of 2", ErrInvalidXDR)
		}
		for i, child := range *p.OrPredicates {
			if err := validateClaimPredicateDepth(child, depth+1); err != nil {
				return fmt.Errorf("or child [%d]: %w", i, err)
			}
		}
		return nil

	default:
		return fmt.Errorf("%w: unknown claim predicate type (%d)", ErrInvalidXDR, p.Type)
	}
}

// ValidateClaimant verifies that a Claimant union has a valid discriminant and
// a structurally sound predicate.
func ValidateClaimant(c xdr.Claimant) error {
	switch c.Type {
	case xdr.ClaimantTypeClaimantTypeV0:
		if c.V0 == nil {
			return fmt.Errorf("%w: claimant V0 struct is nil", ErrInvalidXDR)
		}
		return ValidateClaimPredicate(c.V0.Predicate)
	default:
		return fmt.Errorf("%w: unknown claimant type %v", ErrInvalidXDR, c.Type)
	}
}

// NewClaimantV0 constructs a validated xdr.Claimant with destination account and predicate.
func NewClaimantV0(destination string, predicate xdr.ClaimPredicate) (xdr.Claimant, error) {
	acc, err := CoerceAccountID(destination)
	if err != nil {
		return xdr.Claimant{}, fmt.Errorf("claimant destination: %w", err)
	}
	if err := ValidateClaimPredicate(predicate); err != nil {
		return xdr.Claimant{}, fmt.Errorf("claimant predicate: %w", err)
	}
	return xdr.Claimant{
		Type: xdr.ClaimantTypeClaimantTypeV0,
		V0: &xdr.ClaimantV0{
			Destination: acc,
			Predicate:   predicate,
		},
	}, nil
}
