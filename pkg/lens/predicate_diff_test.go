package lens_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
	"github.com/stellar/go-stellar-sdk/xdr"
)

func absTimePredicate(ts int64) xdr.ClaimPredicate {
	v := xdr.Int64(ts)
	return xdr.ClaimPredicate{
		Type:      xdr.ClaimPredicateTypeClaimPredicateBeforeAbsoluteTime,
		AbsBefore: &v,
	}
}

func relTimePredicate(secs int64) xdr.ClaimPredicate {
	v := xdr.Int64(secs)
	return xdr.ClaimPredicate{
		Type:      xdr.ClaimPredicateTypeClaimPredicateBeforeRelativeTime,
		RelBefore: &v,
	}
}

func unconditionalPredicate() xdr.ClaimPredicate {
	return xdr.ClaimPredicate{Type: xdr.ClaimPredicateTypeClaimPredicateUnconditional}
}

func andPredicateOf(parts ...xdr.ClaimPredicate) xdr.ClaimPredicate {
	return xdr.ClaimPredicate{
		Type:          xdr.ClaimPredicateTypeClaimPredicateAnd,
		AndPredicates: &parts,
	}
}

func marshalB64(t *testing.T, p xdr.ClaimPredicate) string {
	t.Helper()
	b, err := p.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	return base64.StdEncoding.EncodeToString(b)
}

func TestDiffClaimPredicateIdentical(t *testing.T) {
	t.Parallel()

	p := absTimePredicate(1735689600)
	changes := lens.DiffClaimPredicate(p, p)
	if len(changes) != 0 {
		t.Errorf("expected no changes for identical predicates, got %d", len(changes))
	}
}

func TestDiffClaimPredicateAbsoluteTimeExtended(t *testing.T) {
	t.Parallel()

	left := absTimePredicate(1735689600)
	right := absTimePredicate(1735689600 + 3600)
	changes := lens.DiffClaimPredicate(left, right)
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d: %v", len(changes), changes)
	}
	if !strings.Contains(changes[0].Description, "+3600s") {
		t.Errorf("description %q should mention +3600s", changes[0].Description)
	}
	if !strings.Contains(changes[0].Description, "extended") {
		t.Errorf("description %q should say 'extended'", changes[0].Description)
	}
}

func TestDiffClaimPredicateAbsoluteTimeTightened(t *testing.T) {
	t.Parallel()

	left := absTimePredicate(1735689600 + 3600)
	right := absTimePredicate(1735689600)
	changes := lens.DiffClaimPredicate(left, right)
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	if !strings.Contains(changes[0].Description, "tightened") {
		t.Errorf("description %q should say 'tightened'", changes[0].Description)
	}
}

func TestDiffClaimPredicateRelativeTimeAdjusted(t *testing.T) {
	t.Parallel()

	left := relTimePredicate(100)
	right := relTimePredicate(200)
	changes := lens.DiffClaimPredicate(left, right)
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	if !strings.Contains(changes[0].Description, "+100s") {
		t.Errorf("description %q should mention +100s", changes[0].Description)
	}
}

func TestDiffClaimPredicateTypeChanged(t *testing.T) {
	t.Parallel()

	left := absTimePredicate(1735689600)
	right := relTimePredicate(1000)
	changes := lens.DiffClaimPredicate(left, right)
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	if !strings.Contains(changes[0].Description, "type changed") {
		t.Errorf("description %q should mention 'type changed'", changes[0].Description)
	}
}

func TestDiffClaimPredicateAndRemoval(t *testing.T) {
	t.Parallel()

	left := andPredicateOf(unconditionalPredicate(), unconditionalPredicate())
	right := andPredicateOf(unconditionalPredicate())
	changes := lens.DiffClaimPredicate(left, right)
	if len(changes) != 1 {
		t.Fatalf("expected 1 change (removal), got %d: %v", len(changes), changes)
	}
	if !strings.Contains(changes[0].Description, "removed") {
		t.Errorf("description %q should say 'removed'", changes[0].Description)
	}
}

func TestDiffClaimPredicateAndAddition(t *testing.T) {
	t.Parallel()

	left := andPredicateOf(unconditionalPredicate())
	right := andPredicateOf(unconditionalPredicate(), absTimePredicate(1735689600))
	changes := lens.DiffClaimPredicate(left, right)
	if len(changes) != 1 {
		t.Fatalf("expected 1 change (addition), got %d: %v", len(changes), changes)
	}
	if !strings.Contains(changes[0].Description, "added") {
		t.Errorf("description %q should say 'added'", changes[0].Description)
	}
}

func TestSummarizePredicateDiffEmpty(t *testing.T) {
	t.Parallel()

	p := unconditionalPredicate()
	if s := lens.SummarizePredicateDiff(p, p); s != "" {
		t.Errorf("expected empty summary for identical predicates, got %q", s)
	}
}

func TestSummarizePredicateDiffSingle(t *testing.T) {
	t.Parallel()

	left := relTimePredicate(100)
	right := relTimePredicate(200)
	s := lens.SummarizePredicateDiff(left, right)
	if s == "" {
		t.Fatal("expected non-empty summary")
	}
	if !strings.Contains(s, "+100s") {
		t.Errorf("summary %q should contain +100s", s)
	}
}

// TestDiffWithOptionsClaimPredicateSemantic verifies that Diff() on two decoded
// ClaimPredicate Values uses the semantic predicate differ: a removed sub-expression
// produces a non-equal result.
func TestDiffWithOptionsClaimPredicateSemantic(t *testing.T) {
	t.Parallel()

	left := andPredicateOf(unconditionalPredicate(), unconditionalPredicate())
	right := andPredicateOf(unconditionalPredicate())

	lv, err := lens.DecodeAs(marshalB64(t, left), "ClaimPredicate")
	if err != nil {
		t.Skipf("ClaimPredicate not in registry: %v", err)
	}
	rv, err := lens.DecodeAs(marshalB64(t, right), "ClaimPredicate")
	if err != nil {
		t.Fatalf("DecodeAs right: %v", err)
	}
	res, err := lens.Diff(lv, rv)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if res.Equal() {
		t.Error("expected non-equal predicates but Diff reported equal")
	}
}
