package lens

import (
	"github.com/stellar/go-stellar-sdk/xdr"
)

// AccountFlagsDiff records which authorization flags were granted and revoked
// between two AccountEntry states.
type AccountFlagsDiff struct {
	// Granted holds flag names that were set in the after state but not before.
	Granted []string `json:"granted,omitempty"`
	// Revoked holds flag names that were set in the before state but not after.
	Revoked []string `json:"revoked,omitempty"`
	// Warnings holds non-fatal security notices about the flag change.
	Warnings []string `json:"warnings,omitempty"`
}

// Equal reports whether no authorization flags changed.
func (d *AccountFlagsDiff) Equal() bool {
	return d == nil || (len(d.Granted) == 0 && len(d.Revoked) == 0)
}

// flagDefs enumerates the four Stellar account authorization flags in display order.
var flagDefs = []struct {
	bit  xdr.AccountFlags
	name string
}{
	{xdr.AccountFlagsAuthRequiredFlag, "AUTH_REQUIRED"},
	{xdr.AccountFlagsAuthRevocableFlag, "AUTH_REVOCABLE"},
	{xdr.AccountFlagsAuthImmutableFlag, "AUTH_IMMUTABLE"},
	{xdr.AccountFlagsAuthClawbackEnabledFlag, "AUTH_CLAWBACK_ENABLED"},
}

// DiffAccountFlags returns the authorization flag changes between before and after.
// It also appends security warnings when dangerous flags like AUTH_IMMUTABLE or
// AUTH_CLAWBACK_ENABLED are granted.
func DiffAccountFlags(before, after xdr.AccountFlags) *AccountFlagsDiff {
	diff := &AccountFlagsDiff{}

	for _, f := range flagDefs {
		hadBefore := (before & f.bit) != 0
		hasAfter := (after & f.bit) != 0
		if !hadBefore && hasAfter {
			diff.Granted = append(diff.Granted, f.name)
			switch f.bit {
			case xdr.AccountFlagsAuthImmutableFlag:
				diff.Warnings = append(diff.Warnings,
					"Account marked immutable: flags can never be modified again.")
			case xdr.AccountFlagsAuthClawbackEnabledFlag:
				diff.Warnings = append(diff.Warnings,
					"AUTH_CLAWBACK_ENABLED granted: issuer can now claw back assets from any trustline.")
			}
		} else if hadBefore && !hasAfter {
			diff.Revoked = append(diff.Revoked, f.name)
		}
	}

	if len(diff.Granted) == 0 {
		diff.Granted = nil
	}
	if len(diff.Revoked) == 0 {
		diff.Revoked = nil
	}
	if len(diff.Warnings) == 0 {
		diff.Warnings = nil
	}
	return diff
}
