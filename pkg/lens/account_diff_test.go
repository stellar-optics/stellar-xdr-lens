package lens_test

import (
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

func TestDiffAccountFlagsNoChange(t *testing.T) {
	t.Parallel()

	flags := xdr.AccountFlagsAuthRequiredFlag | xdr.AccountFlagsAuthRevocableFlag
	diff := lens.DiffAccountFlags(flags, flags)
	if !diff.Equal() {
		t.Errorf("expected Equal() = true when flags unchanged, got Granted=%v Revoked=%v", diff.Granted, diff.Revoked)
	}
	if len(diff.Warnings) != 0 {
		t.Errorf("expected no warnings, got %v", diff.Warnings)
	}
}

func TestDiffAccountFlagsGrantRevoke(t *testing.T) {
	t.Parallel()

	before := xdr.AccountFlagsAuthRequiredFlag | xdr.AccountFlagsAuthRevocableFlag
	after := xdr.AccountFlagsAuthRequiredFlag | xdr.AccountFlagsAuthImmutableFlag

	diff := lens.DiffAccountFlags(before, after)
	if diff.Equal() {
		t.Error("expected diff to be non-equal")
	}

	if len(diff.Granted) != 1 || diff.Granted[0] != "AUTH_IMMUTABLE" {
		t.Errorf("Granted = %v, want [AUTH_IMMUTABLE]", diff.Granted)
	}
	if len(diff.Revoked) != 1 || diff.Revoked[0] != "AUTH_REVOCABLE" {
		t.Errorf("Revoked = %v, want [AUTH_REVOCABLE]", diff.Revoked)
	}
}

func TestDiffAccountFlagsImmutableWarning(t *testing.T) {
	t.Parallel()

	before := xdr.AccountFlags(0)
	after := xdr.AccountFlagsAuthImmutableFlag

	diff := lens.DiffAccountFlags(before, after)
	if len(diff.Warnings) == 0 {
		t.Error("expected warning when AUTH_IMMUTABLE is granted")
	}
	found := false
	for _, w := range diff.Warnings {
		if w == "Account marked immutable: flags can never be modified again." {
			found = true
		}
	}
	if !found {
		t.Errorf("immutable warning not found in %v", diff.Warnings)
	}
}

func TestDiffAccountFlagsClawbackWarning(t *testing.T) {
	t.Parallel()

	before := xdr.AccountFlags(0)
	after := xdr.AccountFlagsAuthClawbackEnabledFlag

	diff := lens.DiffAccountFlags(before, after)
	if len(diff.Warnings) == 0 {
		t.Error("expected warning when AUTH_CLAWBACK_ENABLED is granted")
	}
	found := false
	for _, w := range diff.Warnings {
		if w == "AUTH_CLAWBACK_ENABLED granted: issuer can now claw back assets from any trustline." {
			found = true
		}
	}
	if !found {
		t.Errorf("clawback warning not found in %v", diff.Warnings)
	}
}

func TestDiffAccountFlagsAllGranted(t *testing.T) {
	t.Parallel()

	before := xdr.AccountFlags(0)
	after := xdr.AccountFlagsAuthRequiredFlag |
		xdr.AccountFlagsAuthRevocableFlag |
		xdr.AccountFlagsAuthImmutableFlag |
		xdr.AccountFlagsAuthClawbackEnabledFlag

	diff := lens.DiffAccountFlags(before, after)
	if len(diff.Granted) != 4 {
		t.Errorf("Granted = %v, want 4 flags", diff.Granted)
	}
	if len(diff.Revoked) != 0 {
		t.Errorf("Revoked = %v, want empty", diff.Revoked)
	}
	if len(diff.Warnings) != 2 {
		t.Errorf("Warnings = %v, want 2 (immutable + clawback)", diff.Warnings)
	}
}

func TestDiffAccountFlagsAllRevoked(t *testing.T) {
	t.Parallel()

	before := xdr.AccountFlagsAuthRequiredFlag |
		xdr.AccountFlagsAuthRevocableFlag |
		xdr.AccountFlagsAuthImmutableFlag |
		xdr.AccountFlagsAuthClawbackEnabledFlag
	after := xdr.AccountFlags(0)

	diff := lens.DiffAccountFlags(before, after)
	if len(diff.Granted) != 0 {
		t.Errorf("Granted = %v, want empty", diff.Granted)
	}
	if len(diff.Revoked) != 4 {
		t.Errorf("Revoked = %v, want 4 flags", diff.Revoked)
	}
	if len(diff.Warnings) != 0 {
		t.Errorf("Warnings = %v, want none (no grants)", diff.Warnings)
	}
}
