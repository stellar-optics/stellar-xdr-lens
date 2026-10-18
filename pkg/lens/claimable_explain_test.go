package lens_test

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

func TestDiagnoseClaimablePredicate(t *testing.T) {
	t.Parallel()
	abs := xdr.Int64(300)
	rel := xdr.Int64(100)
	beforeAbsolute := xdr.ClaimPredicate{Type: xdr.ClaimPredicateTypeClaimPredicateBeforeAbsoluteTime, AbsBefore: &abs}
	beforeRelative := xdr.ClaimPredicate{Type: xdr.ClaimPredicateTypeClaimPredicateBeforeRelativeTime, RelBefore: &rel}
	children := []xdr.ClaimPredicate{beforeAbsolute, beforeRelative}
	predicate := xdr.ClaimPredicate{Type: xdr.ClaimPredicateTypeClaimPredicateAnd, AndPredicates: &children}

	d, err := lens.DiagnoseClaimablePredicate(predicate, lens.ClaimablePredicateContext{
		LedgerCloseTime: 200, BalanceCreatedCloseTime: 100,
	})
	if err != nil {
		t.Fatalf("DiagnoseClaimablePredicate() error = %v", err)
	}
	if d.Satisfied || len(d.FailedConditions) != 1 || !strings.Contains(d.FailedConditions[0], "creation time 100") {
		t.Errorf("diagnosis = %+v, want relative-time failure", d)
	}
	if d.Hint == "" || !strings.Contains(d.Predicate, "and") {
		t.Errorf("diagnosis = %+v, want hint and rendered nested predicate", d)
	}

	anyChildren := []xdr.ClaimPredicate{beforeRelative, {Type: xdr.ClaimPredicateTypeClaimPredicateUnconditional}}
	anyPredicate := xdr.ClaimPredicate{Type: xdr.ClaimPredicateTypeClaimPredicateOr, OrPredicates: &anyChildren}
	d, err = lens.DiagnoseClaimablePredicate(anyPredicate, lens.ClaimablePredicateContext{LedgerCloseTime: 200, BalanceCreatedCloseTime: 100})
	if err != nil || !d.Satisfied {
		t.Errorf("OR predicate diagnosis = %+v, %v; want satisfied", d, err)
	}
}

func TestDiagnoseClaimablePredicateErrorsAndFixture(t *testing.T) {
	t.Parallel()
	if _, err := lens.DiagnoseClaimablePredicate(xdr.ClaimPredicate{}, lens.ClaimablePredicateContext{LedgerCloseTime: -1}); !errors.Is(err, lens.ErrInvalidArgument) {
		t.Errorf("negative time error = %v, want ErrInvalidArgument", err)
	}
	malformed := xdr.ClaimPredicate{Type: xdr.ClaimPredicateTypeClaimPredicateNot}
	if _, err := lens.DiagnoseClaimablePredicate(malformed, lens.ClaimablePredicateContext{}); !errors.Is(err, lens.ErrInvalidXDR) {
		t.Errorf("malformed predicate error = %v, want ErrInvalidXDR", err)
	}
	entryValue, err := lens.DecodeAs(loadFixture(t, "claimable_balance_entry"), "ClaimableBalanceEntry")
	if err != nil {
		t.Fatalf("DecodeAs(claimable fixture) error = %v", err)
	}
	entry, ok := entryValue.Raw.(*xdr.ClaimableBalanceEntry)
	if !ok || len(entry.Claimants) == 0 {
		t.Fatalf("fixture raw = %T, want entry with claimants", entryValue.Raw)
	}
	claimant := entry.Claimants[0].MustV0()
	if _, err := lens.DiagnoseClaimablePredicate(claimant.Predicate, lens.ClaimablePredicateContext{LedgerCloseTime: 1_800_000_000, BalanceCreatedCloseTime: 1_700_000_000}); err != nil {
		t.Fatalf("fixture predicate diagnosis error = %v", err)
	}
}

func TestClaimablePredicateBooleanAndTimeEdges(t *testing.T) {
	t.Parallel()
	context := lens.ClaimablePredicateContext{LedgerCloseTime: 10, BalanceCreatedCloseTime: 0}
	absolute := xdr.Int64(20)
	before := xdr.ClaimPredicate{Type: xdr.ClaimPredicateTypeClaimPredicateBeforeAbsoluteTime, AbsBefore: &absolute}
	d, err := lens.DiagnoseClaimablePredicate(before, context)
	if err != nil || !d.Satisfied || len(d.FailedConditions) != 0 {
		t.Errorf("satisfied absolute predicate = %+v, %v", d, err)
	}
	child := xdr.ClaimPredicate{Type: xdr.ClaimPredicateTypeClaimPredicateUnconditional}
	childPtr := &child
	not := xdr.ClaimPredicate{Type: xdr.ClaimPredicateTypeClaimPredicateNot, NotPredicate: &childPtr}
	d, err = lens.DiagnoseClaimablePredicate(not, context)
	if err != nil || d.Satisfied || len(d.FailedConditions) != 1 {
		t.Errorf("NOT true-child diagnosis = %+v, %v", d, err)
	}
	falseChild := xdr.ClaimPredicate{Type: xdr.ClaimPredicateTypeClaimPredicateBeforeAbsoluteTime, AbsBefore: &absolute}
	falsePtr := &falseChild
	not = xdr.ClaimPredicate{Type: xdr.ClaimPredicateTypeClaimPredicateNot, NotPredicate: &falsePtr}
	d, err = lens.DiagnoseClaimablePredicate(not, lens.ClaimablePredicateContext{LedgerCloseTime: 20})
	if err != nil || !d.Satisfied {
		t.Errorf("NOT false-child diagnosis = %+v, %v", d, err)
	}
	falsePredicates := []xdr.ClaimPredicate{falseChild, falseChild}
	or := xdr.ClaimPredicate{Type: xdr.ClaimPredicateTypeClaimPredicateOr, OrPredicates: &falsePredicates}
	d, err = lens.DiagnoseClaimablePredicate(or, lens.ClaimablePredicateContext{LedgerCloseTime: 20})
	if err != nil || d.Satisfied || len(d.FailedConditions) != 2 {
		t.Errorf("OR false diagnosis = %+v, %v", d, err)
	}
	empty := []xdr.ClaimPredicate{}
	and := xdr.ClaimPredicate{Type: xdr.ClaimPredicateTypeClaimPredicateAnd, AndPredicates: &empty}
	if _, err := lens.DiagnoseClaimablePredicate(and, context); !errors.Is(err, lens.ErrInvalidXDR) {
		t.Errorf("empty AND error = %v, want ErrInvalidXDR", err)
	}
	duration := xdr.Int64(math.MaxInt64)
	relative := xdr.ClaimPredicate{Type: xdr.ClaimPredicateTypeClaimPredicateBeforeRelativeTime, RelBefore: &duration}
	if _, err := lens.DiagnoseClaimablePredicate(relative, lens.ClaimablePredicateContext{BalanceCreatedCloseTime: 1}); !errors.Is(err, lens.ErrInvalidXDR) {
		t.Errorf("relative overflow error = %v, want ErrInvalidXDR", err)
	}
	deep := child
	for range 66 {
		inner := deep
		innerPtr := &inner
		deep = xdr.ClaimPredicate{Type: xdr.ClaimPredicateTypeClaimPredicateNot, NotPredicate: &innerPtr}
	}
	if _, err := lens.DiagnoseClaimablePredicate(deep, context); !errors.Is(err, lens.ErrInvalidXDR) {
		t.Errorf("deep predicate error = %v, want ErrInvalidXDR", err)
	}
}
