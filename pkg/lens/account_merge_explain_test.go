package lens_test

import (
	"strings"
	"testing"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

func TestDiagnoseAccountMergeFailureMalformed(t *testing.T) {
	t.Parallel()

	f := lens.DiagnoseAccountMergeFailure("GABC", "GABC", "account_merge_malformed")
	if f == nil {
		t.Fatal("DiagnoseAccountMergeFailure() returned nil")
	}
	if f.FailureCode != "account_merge_malformed" {
		t.Errorf("FailureCode = %q, want %q", f.FailureCode, "account_merge_malformed")
	}
	if !strings.Contains(f.Summary, "invalid") {
		t.Errorf("Summary %q should mention 'invalid'", f.Summary)
	}
	if f.Hint == "" {
		t.Error("Hint must not be empty")
	}
}

func TestDiagnoseAccountMergeFailureNoAccount(t *testing.T) {
	t.Parallel()

	f := lens.DiagnoseAccountMergeFailure("GABC", "GDEST", "account_merge_no_account")
	if f == nil {
		t.Fatal("DiagnoseAccountMergeFailure() returned nil")
	}
	if !strings.Contains(f.Summary, "does not exist") {
		t.Errorf("Summary %q should mention 'does not exist'", f.Summary)
	}
	if f.Hint == "" {
		t.Error("Hint must not be empty")
	}
}

func TestDiagnoseAccountMergeFailureHasSubEntries(t *testing.T) {
	t.Parallel()

	f := lens.DiagnoseAccountMergeFailure("GABC", "GDEST", "account_merge_has_sub_entries")
	if f == nil {
		t.Fatal("DiagnoseAccountMergeFailure() returned nil")
	}
	if !strings.Contains(f.Summary, "subentries") {
		t.Errorf("Summary %q should mention 'subentries'", f.Summary)
	}
	if !strings.Contains(strings.ToLower(f.Hint), "trustline") {
		t.Errorf("Hint %q should mention trustlines", f.Hint)
	}
}

func TestDiagnoseAccountMergeFailureImmutableSet(t *testing.T) {
	t.Parallel()

	f := lens.DiagnoseAccountMergeFailure("GABC", "GDEST", "account_merge_immutable_set")
	if f == nil {
		t.Fatal("DiagnoseAccountMergeFailure() returned nil")
	}
	if !strings.Contains(f.Summary, "AUTH_IMMUTABLE") {
		t.Errorf("Summary %q should mention 'AUTH_IMMUTABLE'", f.Summary)
	}
}

func TestDiagnoseAccountMergeFailureSeqnumTooFar(t *testing.T) {
	t.Parallel()

	f := lens.DiagnoseAccountMergeFailure("GABC", "GDEST", "account_merge_seqnum_too_far")
	if f == nil {
		t.Fatal("DiagnoseAccountMergeFailure() returned nil")
	}
	if !strings.Contains(f.Summary, "sequence") {
		t.Errorf("Summary %q should mention sequence number", f.Summary)
	}
}

func TestDiagnoseAccountMergeFailureDestFull(t *testing.T) {
	t.Parallel()

	f := lens.DiagnoseAccountMergeFailure("GABC", "GDEST", "account_merge_dest_full")
	if f == nil {
		t.Fatal("DiagnoseAccountMergeFailure() returned nil")
	}
	if !strings.Contains(f.Summary, "overflow") {
		t.Errorf("Summary %q should mention 'overflow'", f.Summary)
	}
}

func TestDiagnoseAccountMergeFailureIsSponsor(t *testing.T) {
	t.Parallel()

	f := lens.DiagnoseAccountMergeFailure("GABC", "GDEST", "account_merge_is_sponsor")
	if f == nil {
		t.Fatal("DiagnoseAccountMergeFailure() returned nil")
	}
	if !strings.Contains(f.Summary, "sponsoring") {
		t.Errorf("Summary %q should mention 'sponsoring'", f.Summary)
	}
	if !strings.Contains(f.Hint, "RevokeSponsorship") {
		t.Errorf("Hint %q should mention 'RevokeSponsorship'", f.Hint)
	}
}

func TestDiagnoseAccountMergeFailureEmptyAddresses(t *testing.T) {
	t.Parallel()

	f := lens.DiagnoseAccountMergeFailure("", "", "account_merge_no_account")
	if f == nil {
		t.Fatal("DiagnoseAccountMergeFailure() returned nil for empty addresses")
	}
	if f.Summary == "" {
		t.Error("Summary must not be empty even with empty addresses")
	}
}

func TestDiagnoseAccountMergeFailureUnknownCode(t *testing.T) {
	t.Parallel()

	f := lens.DiagnoseAccountMergeFailure("GABC", "GDEST", "account_merge_unknown_xyz")
	if f == nil {
		t.Fatal("DiagnoseAccountMergeFailure() returned nil for unknown code")
	}
	if f.Summary == "" {
		t.Error("Summary must not be empty for unknown codes")
	}
}

func TestAccountMergeFailureString(t *testing.T) {
	t.Parallel()

	f := lens.DiagnoseAccountMergeFailure("GABC", "GDEST", "account_merge_no_account")
	if s := f.String(); s != f.Summary {
		t.Errorf("String() = %q, want Summary %q", s, f.Summary)
	}

	var nilF *lens.AccountMergeFailure
	if s := nilF.String(); s != "<nil>" {
		t.Errorf("nil.String() = %q, want \"<nil>\"", s)
	}
}
