package lens

import (
	"fmt"
)

// AccountMergeFailure explains why an AccountMerge operation was rejected.
// This covers every failure variant produced by stellar-core's MergeOpFrame.
type AccountMergeFailure struct {
	// FailureCode is the snake_case result code constant,
	// e.g. "account_merge_no_account".
	FailureCode string
	// Source is the address of the account being merged.
	Source string
	// Destination is the address of the account to merge into.
	Destination string
	// Summary is a plain-English explanation of the failure.
	Summary string
	// Hint suggests the next debugging step.
	Hint string
}

// String renders the diagnosis as a single line.
func (a *AccountMergeFailure) String() string {
	if a == nil {
		return "<nil>"
	}
	return a.Summary
}

// DiagnoseAccountMergeFailure interprets an AccountMerge result code and
// returns an AccountMergeFailure with actionable context derived entirely
// from the operation body. No network calls are made; all output is
// deterministic and offline-safe.
//
// source and dest may be empty strings when the caller cannot recover the
// addresses (e.g. a hand-built or truncated body); the diagnosis is still
// returned with generic subject labels.
func DiagnoseAccountMergeFailure(source, dest, code string) *AccountMergeFailure {
	merger := "the source account"
	if source != "" {
		merger = shortAddr(source)
	}
	destination := "the destination"
	if dest != "" {
		destination = shortAddr(dest)
	}

	f := &AccountMergeFailure{
		FailureCode: code,
		Source:      source,
		Destination: dest,
	}

	switch code {
	case "account_merge_malformed":
		f.Summary = fmt.Sprintf(
			"The merge from %s is structurally invalid — an account cannot be merged into itself.",
			merger)
		f.Hint = "Set the Destination to a different account address than the operation source and resubmit."

	case "account_merge_no_account":
		f.Summary = fmt.Sprintf(
			"The destination account %s does not exist on the ledger and cannot receive the merged balance.",
			destination)
		f.Hint = "Create the destination account with a CreateAccount operation before retrying the merge, or choose an existing account as the target."

	case "account_merge_has_sub_entries":
		f.Summary = fmt.Sprintf(
			"The merging account %s still holds non-signer subentries (open offers, non-zero trustlines, or data entries) that must be removed before it can be merged.",
			merger)
		f.Hint = "Remove all trustlines, cancel open and passive sell offers, and delete any data entries. Signers may remain — they are removed automatically as part of a successful merge."

	case "account_merge_immutable_set":
		f.Summary = fmt.Sprintf(
			"The merging account %s has the AUTH_IMMUTABLE flag set; the network permanently rejects merges from immutable accounts.",
			merger)
		f.Hint = "AUTH_IMMUTABLE is permanent and can never be cleared. This account can never be merged or have its flags changed. Move its balance with ordinary Payment operations instead."

	case "account_merge_seqnum_too_far":
		f.Summary = fmt.Sprintf(
			"The merging account %s carries a sequence number at or above the closing ledger's starting sequence (ledgerSeq << 32); merging it now would corrupt sequence ordering if the account were recreated later.",
			merger)
		f.Hint = "An account whose sequence was bumped past ledgerSeq × 2³² can never be merged. Transfer its balance with Payment operations instead."

	case "account_merge_dest_full":
		f.Summary = fmt.Sprintf(
			"Merging into %s would overflow its XLM balance beyond the ceiling set by its lumen buying liabilities.",
			destination)
		f.Hint = "Cancel or reduce open offers that buy XLM on the destination until its buying liabilities leave room for the incoming balance, then retry the merge — or choose a destination that can receive the full balance."

	case "account_merge_is_sponsor":
		f.Summary = fmt.Sprintf(
			"The merging account %s is currently sponsoring one or more ledger entries and cannot be deleted while sponsorships remain active.",
			merger)
		f.Hint = "Issue RevokeSponsorship operations to transfer or remove every entry this account sponsors, then resubmit the merge."

	default:
		f.Summary = fmt.Sprintf("AccountMerge failed with result code %q.", code)
		f.Hint = "Check the result code against the Stellar protocol specification."
	}

	return f
}
