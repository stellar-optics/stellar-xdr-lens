package lens

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/stellar/go-stellar-sdk/xdr"
)

// Trust line ledger entries, including the V1 liabilities extension and the
// V2 liquidity pool use extension.
//
// TrustLineEntry.Flags is a bit mask whose meaning is invisible in the value
// alone — "3" tells a reader nothing — so the field note registered here
// spells the set flags out while leaving the raw number in place.

func init() {
	registerFieldNote(reflect.TypeOf(xdr.TrustLineEntry{}), "Flags", trustLineFlagNote)
}

// trustLineFlagNames decodes the trust line flag bit mask into the canonical
// names used by Horizon. Unknown bits are ignored rather than guessed at.
func trustLineFlagNames(flags xdr.Uint32) []string {
	var names []string
	if flags&xdr.Uint32(xdr.TrustLineFlagsAuthorizedFlag) != 0 {
		names = append(names, "authorized")
	}
	if flags&xdr.Uint32(xdr.TrustLineFlagsAuthorizedToMaintainLiabilitiesFlag) != 0 {
		names = append(names, "authorized_to_maintain_liabilities")
	}
	if flags&xdr.Uint32(xdr.TrustLineFlagsTrustlineClawbackEnabledFlag) != 0 {
		names = append(names, "trustline_clawback_enabled")
	}
	return names
}

// trustLineFlagNote renders the set flags for display.
func trustLineFlagNote(rv reflect.Value) (string, bool) {
	flags, ok := rv.Interface().(xdr.Uint32)
	if !ok {
		return "", false
	}
	names := trustLineFlagNames(flags)
	if len(names) == 0 {
		return "", false
	}
	return strings.Join(names, " | "), true
}

const (
	flagAuthorized = uint32(xdr.TrustLineFlagsAuthorizedFlag)
	flagMaintain   = uint32(xdr.TrustLineFlagsAuthorizedToMaintainLiabilitiesFlag)
	flagClawback   = uint32(xdr.TrustLineFlagsTrustlineClawbackEnabledFlag)
)

// ClassifyFlagTransition describes the security implication of a trustline
// flags change from oldFlags to newFlags. The returned string is intended
// for display in a diff annotation.
//
// Recognized transitions (non-exhaustive):
//   - Freeze: authorized → unauthorized (fully revoked)
//   - Restricted: authorized → maintain_liabilities only
//   - Granted: any state → authorized
//   - ClawbackEnabled / ClawbackDisabled: clawback bit flip
//   - Unchanged: same effective flags
func ClassifyFlagTransition(oldFlags, newFlags uint32) string {
	if oldFlags == newFlags {
		return "unchanged"
	}

	oldAuth := oldFlags&flagAuthorized != 0
	newAuth := newFlags&flagAuthorized != 0
	oldMaintain := oldFlags&flagMaintain != 0
	newMaintain := newFlags&flagMaintain != 0
	oldClaw := oldFlags&flagClawback != 0
	newClaw := newFlags&flagClawback != 0

	// Authorization transitions (most security-relevant, checked first).
	if oldAuth && !newAuth && !newMaintain {
		return "freeze: trustline fully revoked (AUTHORIZED → UNAUTHORIZED)"
	}
	if oldAuth && !newAuth && newMaintain {
		return "restricted: downgraded to maintain_liabilities only (AUTHORIZED → AUTHORIZED_TO_MAINTAIN_LIABILITIES)"
	}
	if !oldAuth && newAuth {
		return "granted: trustline authorized"
	}
	if !oldMaintain && newMaintain && !newAuth {
		return "restricted: authorized_to_maintain_liabilities granted without full authorization"
	}
	if oldMaintain && !newMaintain && !newAuth {
		return "freeze: maintain_liabilities flag removed"
	}

	// Clawback transitions.
	if !oldClaw && newClaw {
		return "clawback_enabled: issuer may now claw back this trustline"
	}
	if oldClaw && !newClaw {
		return "clawback_disabled: clawback capability removed"
	}

	// Generic fallback with bit-level description.
	added := newFlags &^ oldFlags
	removed := oldFlags &^ newFlags
	var parts []string
	if added != 0 {
		parts = append(parts, fmt.Sprintf("flags added: %s", strings.Join(trustLineFlagNames(xdr.Uint32(added)), " | ")))
	}
	if removed != 0 {
		parts = append(parts, fmt.Sprintf("flags removed: %s", strings.Join(trustLineFlagNames(xdr.Uint32(removed)), " | ")))
	}
	return strings.Join(parts, "; ")
}
