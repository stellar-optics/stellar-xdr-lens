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

func TestValidateClaimPredicateContradiction(t *testing.T) {
	t.Parallel()

	t.Run("absBefore <= absAfter contradiction in AND", func(t *testing.T) {
		abs100 := xdr.Int64(100)
		abs200 := xdr.Int64(200)

		before100 := xdr.ClaimPredicate{
			Type:      xdr.ClaimPredicateTypeClaimPredicateBeforeAbsoluteTime,
			AbsBefore: &abs100,
		}
		before200 := xdr.ClaimPredicate{
			Type:      xdr.ClaimPredicateTypeClaimPredicateBeforeAbsoluteTime,
			AbsBefore: &abs200,
		}
		before200Ptr := &before200
		after200 := xdr.ClaimPredicate{
			Type:         xdr.ClaimPredicateTypeClaimPredicateNot,
			NotPredicate: &before200Ptr,
		}

		// absBefore <= 100 AND absAfter >= 200 (impossible because 100 <= 200)
		children := []xdr.ClaimPredicate{before100, after200}
		contradictoryAnd := xdr.ClaimPredicate{
			Type:          xdr.ClaimPredicateTypeClaimPredicateAnd,
			AndPredicates: &children,
		}

		bad, reason := lens.ValidateClaimPredicateContradiction(contradictoryAnd)
		if !bad {
			t.Errorf("expected contradiction for absBefore <= absAfter, got valid")
		}
		if !strings.Contains(reason, "conflicting time boundaries") {
			t.Errorf("expected conflicting time boundaries reason, got %q", reason)
		}

		// Equal boundaries: absBefore <= 100 AND absAfter >= 100
		before100Ptr := &before100
		after100 := xdr.ClaimPredicate{
			Type:         xdr.ClaimPredicateTypeClaimPredicateNot,
			NotPredicate: &before100Ptr,
		}
		eqChildren := []xdr.ClaimPredicate{before100, after100}
		eqAnd := xdr.ClaimPredicate{
			Type:          xdr.ClaimPredicateTypeClaimPredicateAnd,
			AndPredicates: &eqChildren,
		}
		bad, _ = lens.ValidateClaimPredicateContradiction(eqAnd)
		if !bad {
			t.Errorf("expected contradiction for equal absBefore and absAfter, got valid")
		}

		// Valid window: absAfter >= 100 AND absBefore <= 200
		validChildren := []xdr.ClaimPredicate{before200, after100}
		validAnd := xdr.ClaimPredicate{
			Type:          xdr.ClaimPredicateTypeClaimPredicateAnd,
			AndPredicates: &validChildren,
		}
		bad, reason = lens.ValidateClaimPredicateContradiction(validAnd)
		if bad {
			t.Errorf("expected valid window [100, 200), got contradiction: %s", reason)
		}
	})

	t.Run("relBefore <= 0 contradiction", func(t *testing.T) {
		zeroRel := xdr.Int64(0)
		pZero := xdr.ClaimPredicate{
			Type:      xdr.ClaimPredicateTypeClaimPredicateBeforeRelativeTime,
			RelBefore: &zeroRel,
		}
		bad, reason := lens.ValidateClaimPredicateContradiction(pZero)
		if !bad {
			t.Errorf("expected contradiction for relBefore <= 0, got valid")
		}
		if !strings.Contains(reason, "relBefore") {
			t.Errorf("expected relBefore reason, got %q", reason)
		}

		negRel := xdr.Int64(-10)
		pNeg := xdr.ClaimPredicate{
			Type:      xdr.ClaimPredicateTypeClaimPredicateBeforeRelativeTime,
			RelBefore: &negRel,
		}
		bad, _ = lens.ValidateClaimPredicateContradiction(pNeg)
		if !bad {
			t.Errorf("expected contradiction for negative relBefore, got valid")
		}

		posRel := xdr.Int64(3600)
		pPos := xdr.ClaimPredicate{
			Type:      xdr.ClaimPredicateTypeClaimPredicateBeforeRelativeTime,
			RelBefore: &posRel,
		}
		bad, _ = lens.ValidateClaimPredicateContradiction(pPos)
		if bad {
			t.Errorf("expected positive relBefore to be valid, got contradiction")
		}
	})

	t.Run("direct double negation and impossible predicates", func(t *testing.T) {
		uncond := xdr.ClaimPredicate{Type: xdr.ClaimPredicateTypeClaimPredicateUnconditional}
		uncondPtr := &uncond
		notUncond := xdr.ClaimPredicate{
			Type:         xdr.ClaimPredicateTypeClaimPredicateNot,
			NotPredicate: &uncondPtr,
		}
		bad, reason := lens.ValidateClaimPredicateContradiction(notUncond)
		if !bad {
			t.Errorf("expected contradiction for not(unconditional), got valid")
		}
		if !strings.Contains(reason, "not(unconditional)") {
			t.Errorf("expected not(unconditional) reason, got %q", reason)
		}

		notUncondPtr := &notUncond
		doubleNot := xdr.ClaimPredicate{
			Type:         xdr.ClaimPredicateTypeClaimPredicateNot,
			NotPredicate: &notUncondPtr,
		}
		bad, reason = lens.ValidateClaimPredicateContradiction(doubleNot)
		if !bad {
			t.Errorf("expected contradiction for double negation, got valid")
		}
		if !strings.Contains(reason, "double negation") {
			t.Errorf("expected double negation reason, got %q", reason)
		}
	})

	t.Run("ValidateClaimantPredicates warning format", func(t *testing.T) {
		dest := xdr.MustAddress("GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN")
		zeroRel := xdr.Int64(0)
		badPred := xdr.ClaimPredicate{
			Type:      xdr.ClaimPredicateTypeClaimPredicateBeforeRelativeTime,
			RelBefore: &zeroRel,
		}
		claimants := []xdr.Claimant{
			{
				Type: xdr.ClaimantTypeClaimantTypeV0,
				V0: &xdr.ClaimantV0{
					Destination: dest,
					Predicate:   badPred,
				},
			},
		}

		warnings := lens.ValidateClaimantPredicates(claimants)
		if len(warnings) != 1 {
			t.Fatalf("expected 1 warning, got %d", len(warnings))
		}
		expectedMsg := "Warning: Claim predicate for claimant GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN is unsatisfiable; funds will be permanently unclaimable."
		if warnings[0] != expectedMsg {
			t.Errorf("warning = %q, want %q", warnings[0], expectedMsg)
		}
	})
}
