package lens_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

const lintAccount = "GAPB7YMUQ6MPQ23PTFDNBZP7HGGTZPI2DT5JJURUOIXNPLGHW2O3WYXZ"

func lintBumpOp() xdr.Operation {
	return xdr.Operation{Body: xdr.OperationBody{
		Type:           xdr.OperationTypeBumpSequence,
		BumpSequenceOp: &xdr.BumpSequenceOp{BumpTo: xdr.SequenceNumber(7)},
	}}
}

func lintMergeOp() xdr.Operation {
	dest := xdr.MustMuxedAddress(lintAccount)
	return xdr.Operation{Body: xdr.OperationBody{
		Type:        xdr.OperationTypeAccountMerge,
		Destination: &dest,
	}}
}

func lintSignature() xdr.DecoratedSignature {
	return xdr.DecoratedSignature{
		Hint:      xdr.SignatureHint{1, 2, 3, 4},
		Signature: []byte{0xde, 0xad, 0xbe, 0xef},
	}
}

// lintEnvelope builds a signed v1 envelope with the supplied fee, operations
// and preconditions.
func lintEnvelope(fee uint32, ops []xdr.Operation, cond xdr.Preconditions) *xdr.TransactionEnvelope {
	if cond.Type == xdr.PreconditionTypePrecondNone && cond.TimeBounds == nil && cond.V2 == nil {
		cond = xdr.Preconditions{Type: xdr.PreconditionTypePrecondNone}
	}
	env := &xdr.TransactionEnvelope{
		Type: xdr.EnvelopeTypeEnvelopeTypeTx,
		V1: &xdr.TransactionV1Envelope{
			Tx: xdr.Transaction{
				SourceAccount: xdr.MustMuxedAddress(lintAccount),
				Fee:           xdr.Uint32(fee),
				SeqNum:        xdr.SequenceNumber(1),
				Cond:          cond,
				Memo:          xdr.Memo{Type: xdr.MemoTypeMemoNone},
				Operations:    ops,
				Ext:           xdr.TransactionExt{V: 0},
			},
			Signatures: []xdr.DecoratedSignature{lintSignature()},
		},
	}
	return env
}

func lintFeeBumpEnvelope(outerFee int64, innerFee uint32, ops []xdr.Operation) *xdr.TransactionEnvelope {
	return &xdr.TransactionEnvelope{
		Type: xdr.EnvelopeTypeEnvelopeTypeTxFeeBump,
		FeeBump: &xdr.FeeBumpTransactionEnvelope{
			Tx: xdr.FeeBumpTransaction{
				FeeSource: xdr.MustMuxedAddress(lintAccount),
				Fee:       xdr.Int64(outerFee),
				InnerTx: xdr.FeeBumpTransactionInnerTx{
					Type: xdr.EnvelopeTypeEnvelopeTypeTx,
					V1: &xdr.TransactionV1Envelope{
						Tx: xdr.Transaction{
							SourceAccount: xdr.MustMuxedAddress(lintAccount),
							Fee:           xdr.Uint32(innerFee),
							SeqNum:        xdr.SequenceNumber(1),
							Cond:          xdr.Preconditions{Type: xdr.PreconditionTypePrecondNone},
							Memo:          xdr.Memo{Type: xdr.MemoTypeMemoNone},
							Operations:    ops,
							Ext:           xdr.TransactionExt{V: 0},
						},
						Signatures: []xdr.DecoratedSignature{lintSignature()},
					},
				},
				Ext: xdr.FeeBumpTransactionExt{V: 0},
			},
			Signatures: []xdr.DecoratedSignature{lintSignature()},
		},
	}
}

func findLintIssue(issues []lens.LintIssue, rule string) *lens.LintIssue {
	for i := range issues {
		if issues[i].Rule == rule {
			return &issues[i]
		}
	}
	return nil
}

func TestLintFeeSufficiency(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		fee          uint32
		ops          int
		wantRule     string
		wantSeverity lens.LintSeverity
	}{
		{name: "single operation below base fee", fee: 99, ops: 1, wantRule: "fee-below-minimum", wantSeverity: lens.LintWarning},
		{name: "single operation at base fee", fee: 100, ops: 1},
		{name: "multi operation below minimum", fee: 250, ops: 3, wantRule: "fee-below-minimum", wantSeverity: lens.LintWarning},
		{name: "multi operation at minimum", fee: 300, ops: 3},
		{name: "zero fee is an error", fee: 0, ops: 1, wantRule: "zero-fee-bid", wantSeverity: lens.LintError},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ops := make([]xdr.Operation, tc.ops)
			for i := range ops {
				ops[i] = lintBumpOp()
			}
			issues := lens.LintEnvelope(lintEnvelope(tc.fee, ops, xdr.Preconditions{}))
			if tc.wantRule == "" {
				if len(issues) != 0 {
					t.Fatalf("clean envelope produced %d issue(s): %v", len(issues), issues)
				}
				return
			}
			issue := findLintIssue(issues, tc.wantRule)
			if issue == nil {
				t.Fatalf("missing %q rule; issues were %v", tc.wantRule, issues)
			}
			if issue.Severity != tc.wantSeverity {
				t.Errorf("severity = %q, want %q", issue.Severity, tc.wantSeverity)
			}
			if tc.wantRule == "fee-below-minimum" && !strings.Contains(issue.Message, strconv.Itoa(tc.ops*100)) {
				t.Errorf("message %q should name the %d stroop minimum", issue.Message, tc.ops*100)
			}
			if issue.Hint == "" {
				t.Error("finding has no remediation hint")
			}
		})
	}
}

func TestLintFeeBumpMinimum(t *testing.T) {
	t.Parallel()

	underpaid := lens.LintEnvelope(lintFeeBumpEnvelope(400, 500, []xdr.Operation{lintBumpOp()}))
	issue := findLintIssue(underpaid, "fee-bump-fee-too-low")
	if issue == nil {
		t.Fatalf("missing fee-bump-fee-too-low; issues were %v", underpaid)
	}
	if issue.Severity != lens.LintError {
		t.Errorf("severity = %q, want ERROR", issue.Severity)
	}

	covered := lens.LintEnvelope(lintFeeBumpEnvelope(500, 500, []xdr.Operation{lintBumpOp()}))
	if issue := findLintIssue(covered, "fee-bump-fee-too-low"); issue != nil {
		t.Errorf("a covered fee bump was flagged: %v", *issue)
	}
	if len(covered) != 0 {
		t.Errorf("covered fee bump produced %d issue(s): %v", len(covered), covered)
	}
}

func TestLintMissingSignatures(t *testing.T) {
	t.Parallel()

	env := lintEnvelope(100, []xdr.Operation{lintBumpOp()}, xdr.Preconditions{})
	env.V1.Signatures = nil
	issues := lens.LintEnvelope(env)
	if findLintIssue(issues, "missing-signatures") == nil {
		t.Fatalf("missing-signatures was not reported: %v", issues)
	}
}

func TestLintMissingInnerFeeBumpSignatures(t *testing.T) {
	t.Parallel()

	env := lintFeeBumpEnvelope(500, 500, []xdr.Operation{lintBumpOp()})
	env.FeeBump.Tx.InnerTx.V1.Signatures = nil
	issues := lens.LintEnvelope(env)
	issue := findLintIssue(issues, "missing-signatures")
	if issue == nil {
		t.Fatalf("missing inner signatures were not reported: %v", issues)
	}
	if !strings.Contains(issue.Path, "InnerTx") {
		t.Errorf("path = %q, want the inner transaction signatures", issue.Path)
	}
}

func TestLintInvertedTimeBounds(t *testing.T) {
	t.Parallel()

	cond := xdr.Preconditions{
		Type:       xdr.PreconditionTypePrecondTime,
		TimeBounds: &xdr.TimeBounds{MinTime: 200, MaxTime: 100},
	}
	issues := lens.LintEnvelope(lintEnvelope(100, []xdr.Operation{lintBumpOp()}, cond))
	if findLintIssue(issues, "inverted-timebounds") == nil {
		t.Fatalf("inverted time bounds were not reported: %v", issues)
	}

	// A zero maxTime means "no upper bound" and must not be inverted.
	unbounded := xdr.Preconditions{
		Type:       xdr.PreconditionTypePrecondTime,
		TimeBounds: &xdr.TimeBounds{MinTime: 200, MaxTime: 0},
	}
	if issues := lens.LintEnvelope(lintEnvelope(100, []xdr.Operation{lintBumpOp()}, unbounded)); len(issues) != 0 {
		t.Errorf("unbounded time bounds produced %d issue(s): %v", len(issues), issues)
	}
}

func TestLintInvertedLedgerBounds(t *testing.T) {
	t.Parallel()

	cond := xdr.Preconditions{
		Type: xdr.PreconditionTypePrecondV2,
		V2: &xdr.PreconditionsV2{
			LedgerBounds: &xdr.LedgerBounds{MinLedger: 500, MaxLedger: 100},
		},
	}
	issues := lens.LintEnvelope(lintEnvelope(100, []xdr.Operation{lintBumpOp()}, cond))
	if findLintIssue(issues, "inverted-timebounds") == nil {
		t.Fatalf("inverted ledger bounds were not reported: %v", issues)
	}
}

func TestLintUnreachableOperations(t *testing.T) {
	t.Parallel()

	ops := []xdr.Operation{lintMergeOp(), lintBumpOp(), lintBumpOp()}
	issues := lens.LintEnvelope(lintEnvelope(300, ops, xdr.Preconditions{}))

	warnings := 0
	for _, issue := range issues {
		if issue.Rule != "unreachable-operation" {
			continue
		}
		warnings++
		if issue.Severity != lens.LintWarning {
			t.Errorf("severity = %q, want WARN", issue.Severity)
		}
	}
	if warnings != 2 {
		t.Errorf("got %d unreachable-operation warning(s), want 2: %v", warnings, issues)
	}
}

func TestLintOrderingIsDeterministic(t *testing.T) {
	t.Parallel()

	cond := xdr.Preconditions{
		Type:       xdr.PreconditionTypePrecondTime,
		TimeBounds: &xdr.TimeBounds{MinTime: 200, MaxTime: 100},
	}
	env := lintEnvelope(50, []xdr.Operation{lintMergeOp(), lintBumpOp()}, cond)
	env.V1.Signatures = nil

	first := lens.LintEnvelope(env)
	second := lens.LintEnvelope(env)
	if len(first) != len(second) {
		t.Fatalf("issue counts differ across runs: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Errorf("issue %d differs across runs: %+v vs %+v", i, first[i], second[i])
		}
	}
}

func TestLintRulesRegistered(t *testing.T) {
	t.Parallel()

	seen := make(map[string]bool)
	for _, rule := range lens.LintRules() {
		if rule.Name == "" || rule.Description == "" || rule.Check == nil {
			t.Errorf("incomplete rule: %+v", rule)
		}
		if seen[rule.Name] {
			t.Errorf("duplicate rule name %q", rule.Name)
		}
		seen[rule.Name] = true
	}
	for _, want := range []string{"zero-fee-bid", "fee-below-minimum", "missing-signatures", "inverted-timebounds", "unreachable-operation"} {
		if !seen[want] {
			t.Errorf("rule %q is not registered", want)
		}
	}
}

func TestLintNilEnvelope(t *testing.T) {
	t.Parallel()

	issues := lens.LintEnvelope(nil)
	issue := findLintIssue(issues, "invalid-envelope")
	if issue == nil {
		t.Fatalf("nil envelope was not reported: %v", issues)
	}
	if issue.Severity != lens.LintError {
		t.Errorf("severity = %q, want ERROR", issue.Severity)
	}
}
