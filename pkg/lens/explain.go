package lens

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/stellar/go-stellar-sdk/amount"
	"github.com/stellar/go-stellar-sdk/xdr"
)

// Summary is a human-readable account of an XDR value.
//
// A Summary is produced from an envelope, from a result, or from both
// together. Pairing the two is what makes the output genuinely useful: an
// envelope alone cannot say what failed, and a result alone cannot say what
// the failing operation was trying to do.
type Summary struct {
	// Kind is the sort of value summarised: "transaction_envelope",
	// "transaction_result", or "transaction" when the two were paired.
	Kind string `json:"kind"`
	// Headline is the single most important sentence, suitable for a log
	// line or a CI failure message.
	Headline string `json:"headline"`
	// Source is the strkey address that submitted the transaction.
	Source string `json:"source,omitempty"`
	// FeeBumpSource is set when the transaction was wrapped in a fee bump.
	FeeBumpSource string `json:"feeBumpSource,omitempty"`
	// Fee is the fee bid in stroops, and FeeCharged what was actually taken.
	Fee         int64    `json:"fee,omitempty"`
	FeeCharged  int64    `json:"feeCharged,omitempty"`
	SeqNum      int64    `json:"seqNum,omitempty"`
	Memo        string   `json:"memo,omitempty"`
	Preconds    []string `json:"preconditions,omitempty"`
	SignatureCt int      `json:"signatureCount,omitempty"`
	// Expiry carries precondition expiry analysis when requested.
	Expiry *PreconditionExpiryResult `json:"expiry,omitempty"`
	// Operations describes each operation, with its outcome when a result
	// was supplied.
	Operations []OpSummary `json:"operations,omitempty"`
	// Outcome is present whenever a result was supplied.
	Outcome *Outcome `json:"outcome,omitempty"`
}

// OpSummary describes one operation.
type OpSummary struct {
	// Index is the zero-based position in the transaction.
	Index int `json:"index"`
	// Type is the snake_case operation name, e.g. "payment".
	Type string `json:"type"`
	// Source is the operation-level source account override, when present.
	Source string `json:"source,omitempty"`
	// Detail is a one-line description of what the operation does.
	Detail string `json:"detail,omitempty"`
	// Result is the operation's outcome, present only when a result was
	// supplied and covers this operation.
	Result *Reason `json:"result,omitempty"`
	// PathPayment provides contextual diagnostics for path payment slippage.
	PathPayment *PathPaymentSlippageDiagnosis `json:"pathPayment,omitempty"`
	// TrustLine provides contextual diagnostics for SetTrustLineFlags failures.
	TrustLine *SetTrustLineFlagsDiagnosis `json:"trustLine,omitempty"`
}

// Outcome is the resolved result of a transaction.
type Outcome struct {
	// Success reports whether the transaction as a whole succeeded.
	Success bool `json:"success"`
	// Reason explains the transaction-level result code.
	Reason Reason `json:"reason"`
	// InnerReason is set for fee-bump transactions, and explains the result
	// of the inner transaction that the fee bump paid for.
	InnerReason *Reason `json:"innerReason,omitempty"`
	// FailedOps lists the indexes of operations that did not succeed.
	FailedOps []int `json:"failedOperations,omitempty"`
}

// Explain summarises a single decoded value.
//
// It accepts a TransactionEnvelope or a TransactionResult. For any other type
// it returns an error, since a general-purpose "exexplanation" of an arbitrary
// XDR struct would be no better than the decoded tree.
func Explain(v *Value) (*Summary, error) {
	if v == nil {
		return nil, fmt.Errorf("%w: nil value", ErrInvalidArgument)
	}
	switch raw := v.Raw.(type) {
	case *xdr.TransactionEnvelope:
		return explainEnvelope(raw, nil)
	case *xdr.TransactionResult:
		return explainResult(raw)
	default:
		return nil, fmt.Errorf("%w: cannot explain %s; explain supports TransactionEnvelope and TransactionResult", ErrUnsupportedType, v.Type)
	}
}

// ExplainPair summarises an envelope together with its result, attributing
// each result code to the operation it belongs to.
//
// This is the most useful form of explanation and the reason the command
// exists: it answers "which operation failed, what was it trying to do, and
// why did it fail" in one pass.
func ExplainPair(envelope, result *Value) (*Summary, error) {
	if envelope == nil || result == nil {
		return nil, fmt.Errorf("%w: both an envelope and a result are required", ErrInvalidArgument)
	}
	env, ok := envelope.Raw.(*xdr.TransactionEnvelope)
	if !ok {
		return nil, fmt.Errorf("%w: first value must be a TransactionEnvelope, got %s", ErrInvalidArgument, envelope.Type)
	}
	res, ok := result.Raw.(*xdr.TransactionResult)
	if !ok {
		return nil, fmt.Errorf("%w: second value must be a TransactionResult, got %s", ErrInvalidArgument, result.Type)
	}
	return explainEnvelope(env, res)
}

// explainEnvelope builds a Summary from an envelope, folding in a result when
// one is available.
func explainEnvelope(env *xdr.TransactionEnvelope, res *xdr.TransactionResult) (*Summary, error) {
	s := &Summary{
		Kind:        "transaction_envelope",
		Source:      addressOf(env.SourceAccount()),
		Fee:         int64(env.Fee()),
		SeqNum:      env.SeqNum(),
		Memo:        describeMemo(env.Memo()),
		SignatureCt: len(env.Signatures()),
	}
	if env.IsFeeBump() {
		s.FeeBumpSource = addressOf(env.FeeBumpAccount())
	}
	s.Preconds = describePreconditions(env)

	ops := env.Operations()
	for i, op := range ops {
		s.Operations = append(s.Operations, OpSummary{
			Index:  i,
			Type:   operationTypeLabel(op.Body.Type),
			Source: operationSource(op),
			Detail: describeOperation(op),
		})
	}

	if res == nil {
		s.Headline = envelopeHeadline(s)
		return s, nil
	}

	s.Kind = "transaction"
	if err := attachResult(s, res, ops, s.Source); err != nil {
		return nil, err
	}
	for i, op := range env.Operations() {
		if i >= len(s.Operations) || s.Operations[i].Result == nil {
			continue
		}
		reason := contextualizeChangeTrustFailure(op, *s.Operations[i].Result)
		s.Operations[i].Result = &reason
	}
	s.Headline = resultHeadline(s, s.Outcome)
	return s, nil
}

// explainResult builds a Summary from a result alone. Without the envelope
// there is nothing to say about what each operation was attempting, so only
// the outcome is populated.
func explainResult(res *xdr.TransactionResult) (*Summary, error) {
	s := &Summary{Kind: "transaction_result"}
	// No envelope means no operation bodies, so failures keep their generic
	// per-code explanation; deepenOperationFailure never runs.
	if err := attachResult(s, res, nil, ""); err != nil {
		return nil, err
	}
	return s, nil
}

// attachResult resolves a TransactionResult into an Outcome and pairs each
// operation result with its operation.
//
// ops carries the envelope's operations when one was supplied. They let a
// failed operation's explanation be deepened with the operation's own
// parameters (see deepenOperationFailure); without them the generic per-code
// reason stands.
func attachResult(s *Summary, res *xdr.TransactionResult, ops []xdr.Operation, source string) error {
	s.FeeCharged = int64(res.FeeCharged)

	code, err := resultCodeName(res)
	if err != nil {
		return err
	}
	outcome := &Outcome{
		Success: res.Successful(),
		Reason:  ExplainCode(code),
	}

	// A fee bump reports its own code plus the inner transaction's, and the
	// inner one is almost always the code the user actually cares about.
	if inner, ok := innerResultCodeName(res); ok {
		r := ExplainCode(inner)
		outcome.InnerReason = &r
	}

	opResults, ok := res.OperationResults()
	if ok {
		for i, opRes := range opResults {
			reason := explainOperationResult(opRes)
			if !reason.Success && i < len(ops) {
				reason = deepenOperationFailure(source, ops[i], reason)
			}
			if !reason.Success {
				outcome.FailedOps = append(outcome.FailedOps, i)
			}
			// Attach to the matching operation when the envelope was given.
			if i < len(s.Operations) {
				if i < len(ops) {
					op := ops[i]
					switch op.Body.Type {
					case xdr.OperationTypePathPaymentStrictReceive,
						xdr.OperationTypePathPaymentStrictSend:
						if diag := withPathPaymentFailureDetail(DiagnosePathPaymentSlippage(op, i, opRes), op, i, opRes, &reason); diag != nil {
							s.Operations[i].PathPayment = diag
							if !reason.Success && diag.Hint != "" {
								reason.Hint = diag.Hint
							}
						}
					case xdr.OperationTypeSetTrustLineFlags:
						if diag := DiagnoseSetTrustLineFlagsOperation(op, i, opRes); diag != nil {
							s.Operations[i].TrustLine = diag
							if !reason.Success && diag.Hint != "" {
								reason.Hint = diag.Hint
							}
						}
					}
				}
				s.Operations[i].Result = &reason
			} else {
				// Result without a paired envelope: still surface it.
				s.Operations = append(s.Operations, OpSummary{
					Index:  i,
					Type:   "unknown",
					Result: &reason,
				})
			}
		}
	}

	s.Outcome = outcome
	s.Headline = resultHeadline(s, outcome)
	return nil
}

// explainOperationResult resolves an operation result to a Reason.
//
// The operation-level code says only whether the operation ran at all. When
// it did (opINNER), the meaningful code lives in the type-specific arm of the
// Tr union. Rather than switch over all 27 arms, the node tree is reused: it
// has already selected the populated arm and rendered its code as the
// constant name.
func explainOperationResult(opRes xdr.OperationResult) Reason {
	node := buildNode("", reflect.ValueOf(opRes), 0)
	if node == nil {
		return ExplainCode(opRes.Code.String())
	}

	// opINNER means "look inside"; any other code is itself the answer.
	if opRes.Code != xdr.OperationResultCodeOpInner {
		return ExplainCode(opRes.Code.String())
	}

	tr := node.Child("Tr")
	if tr == nil {
		return ExplainCode(opRes.Code.String())
	}
	for _, arm := range tr.Children {
		if arm.Name == "Type" {
			continue
		}
		if codeNode := arm.Child("Code"); codeNode != nil {
			if name, ok := codeNode.Value.(string); ok {
				return ExplainCode(name)
			}
		}
	}
	return ExplainCode(opRes.Code.String())
}

// ---------------------------------------------------------------------------
// Context-aware failure deepening
// ---------------------------------------------------------------------------
//
// explainOperationResult answers "what failed". When an envelope is paired
// with its result the operation bodies are also in hand, and for the
// operation families with the richest failure modes that is enough to say
// precisely why: which threshold exceeded 255, which flag bit is
// contradictory, how far the pool price drifted from the window the
// operation accepted.
//
// deepenOperationFailure refines such a Reason using the operation body. It
// only ever rewrites Summary and Hint for codes whose causes are provable
// from the operation's own parameters; Code and Constant are untouched, and
// anything it cannot pin down keeps the generic per-code explanation.
// Everything here is pure analysis of supplied XDR — no network calls, no
// ledger lookups — so output stays deterministic and offline-first.
func deepenOperationFailure(source string, op xdr.Operation, r Reason) Reason {
	if r.Success {
		return r
	}
	switch {
	case strings.HasPrefix(r.Constant, "SetOptionsResultCode"):
		if body, ok := op.Body.GetSetOptionsOp(); ok {
			return deepenSetOptionsFailure(operationEffectiveSource(source, op), body, r)
		}
	case strings.HasPrefix(r.Constant, "LiquidityPoolDepositResultCode"):
		if body, ok := op.Body.GetLiquidityPoolDepositOp(); ok {
			return deepenLiquidityPoolDepositFailure(body, r)
		}
	case strings.HasPrefix(r.Constant, "LiquidityPoolWithdrawResultCode"):
		if body, ok := op.Body.GetLiquidityPoolWithdrawOp(); ok {
			return deepenLiquidityPoolWithdrawFailure(body, r)
		}
	case strings.HasPrefix(r.Constant, "AccountMergeResultCode"):
		return deepenAccountMergeFailure(operationEffectiveSource(source, op), op, r)
	case strings.HasPrefix(r.Constant, "ClawbackResultCode"):
		if body, ok := op.Body.GetClawbackOp(); ok {
			return deepenClawbackFailure(operationEffectiveSource(source, op), body, r)
		}
	case strings.HasPrefix(r.Constant, "ClawbackClaimableBalanceResultCode"):
		if body, ok := op.Body.GetClawbackClaimableBalanceOp(); ok {
			return deepenClawbackClaimableBalanceFailure(body, r)
		}
	case strings.HasPrefix(r.Constant, "ExtendFootprintTtlResultCode"):
		if body, ok := op.Body.GetExtendFootprintTtlOp(); ok {
			return deepenExtendFootprintTtlFailure(body, r)
		}
	case strings.HasPrefix(r.Constant, "RestoreFootprintResultCode"):
		if body, ok := op.Body.GetRestoreFootprintOp(); ok {
			return deepenRestoreFootprintFailure(body, r)
		}
	case strings.HasPrefix(r.Constant, "CreateAccountResultCode"):
		return deepenCreateAccountFailure(op, r)
	case strings.HasPrefix(r.Constant, "ManageSellOfferResultCode"),
		strings.HasPrefix(r.Constant, "ManageBuyOfferResultCode"):
		return deepenManageOfferFailure(op, r)
	case strings.HasPrefix(r.Constant, "ManageDataResultCode"):
		return withOperationContext(r, op, source)
	case strings.HasPrefix(r.Constant, "CreateClaimableBalanceResultCode"):
		return withOperationContext(r, op, source)
	}
	return r
}

func deepenExtendFootprintTtlFailure(o xdr.ExtendFootprintTtlOp, r Reason) Reason {
	switch r.Constant {
	case "ExtendFootprintTtlResultCodeExtendFootprintTtlMalformed":
		if o.ExtendTo == 0 {
			r.Summary = "the operation asks to extend entries to 0 ledgers; a TTL extension must target a positive lifetime"
			r.Hint = "Set extendTo to a positive number of ledgers and retry. A value of zero does not preserve any lifetime."
		}
	case "ExtendFootprintTtlResultCodeExtendFootprintTtlResourceLimitExceeded":
		r.Summary = "the operation tries to extend the TTL of more ledger entries than the transaction's declared resource budget allows"
		r.Hint = "Reduce the footprint or split the operation into smaller TTL extensions so the declared resource limits cover the work."
	case "ExtendFootprintTtlResultCodeExtendFootprintTtlInsufficientRefundableFee":
		r.Summary = fmt.Sprintf("the refundable fee is too small to cover extending the footprint TTL to %d ledgers", o.ExtendTo)
		r.Hint = "Increase the refundable fee or reduce the number of entries in the footprint; simulation reports the rent this extension requires."
	}
	return r
}

func deepenRestoreFootprintFailure(o xdr.RestoreFootprintOp, r Reason) Reason {
	switch r.Constant {
	case "RestoreFootprintResultCodeRestoreFootprintResourceLimitExceeded":
		r.Summary = "the operation tries to restore more archived entries than the transaction's declared resource budget allows"
		r.Hint = "Reduce the footprint or split the restore into smaller operations so the declared resource limits fit the work."
	case "RestoreFootprintResultCodeRestoreFootprintInsufficientRefundableFee":
		r.Summary = "the refundable fee is too small to cover the rent and restoration of the archived entries"
		r.Hint = "Increase the refundable fee to match the restore cost reported by simulation."
	}
	return r
}

// operationEffectiveSource resolves the account an operation runs as: the
// operation-level override when present, otherwise the transaction source.
func operationEffectiveSource(txSource string, op xdr.Operation) string {
	if op.SourceAccount != nil {
		if addr, err := op.SourceAccount.GetAddress(); err == nil && addr != "" {
			return addr
		}
	}
	return txSource
}

// accountAuthFlagBits names the defined account-flag bits in bit order. The
// mask mirrors AUTH_FLAGS in stellar-core; anything outside it is rejected as
// an unknown flag.
var accountAuthFlagBits = [...]struct {
	bit  uint32
	name string
}{
	{0x1, "auth_required"},
	{0x2, "auth_revocable"},
	{0x4, "auth_immutable"},
	{0x8, "auth_clawback_enabled"},
}

const (
	authRequiredFlag        uint32 = 0x1
	authRevocableFlag       uint32 = 0x2
	authImmutableFlag       uint32 = 0x4
	authClawbackEnabledFlag uint32 = 0x8
	authFlagMask                   = authRequiredFlag | authRevocableFlag | authImmutableFlag | authClawbackEnabledFlag
)

// flagBitsNames renders set flag bits as "name (0xN)" pairs joined by
// "and", so a summary can name the exact bits involved in a failure.
func flagBitsNames(flags uint32) string {
	var parts []string
	for _, f := range accountAuthFlagBits {
		if flags&f.bit != 0 {
			parts = append(parts, fmt.Sprintf("%s (0x%x)", f.name, f.bit))
		}
	}
	if unknown := flags &^ authFlagMask; unknown != 0 {
		parts = append(parts, fmt.Sprintf("unrecognised bit(s) 0x%x", unknown))
	}
	return strings.Join(parts, " and ")
}

// deepenSetOptionsFailure refines a failed set_options explanation with the
// operation's own fields. stellar-core validates most of these conditions in
// doCheckValid, from the operation body alone, so every diagnosis below is
// provable offline from the XDR that was submitted.
func deepenSetOptionsFailure(source string, o xdr.SetOptionsOp, r Reason) Reason {
	switch r.Constant {
	case "SetOptionsResultCodeSetOptionsThresholdOutOfRange":
		// XDR carries thresholds as uint32 but the protocol only accepts
		// 0..255; the offending field is visible right here in the op.
		var over []string
		for _, f := range []struct {
			name string
			v    *xdr.Uint32
		}{
			{"master weight", o.MasterWeight},
			{"low threshold", o.LowThreshold},
			{"medium threshold", o.MedThreshold},
			{"high threshold", o.HighThreshold},
		} {
			if f.v != nil && uint64(*f.v) > 255 {
				over = append(over, fmt.Sprintf("%s %d", f.name, *f.v))
			}
		}
		if len(over) > 0 {
			r.Summary = fmt.Sprintf("%s is outside the allowed range 0–255 for threshold weights",
				strings.Join(over, ", "))
			r.Hint = "Threshold weights are a single byte; clamp each value to 0–255 before signing."
		}

	case "SetOptionsResultCodeSetOptionsBadFlags":
		// Core rejects an operation that sets and clears the same bit.
		if o.SetFlags != nil && o.ClearFlags != nil {
			if overlap := uint32(*o.SetFlags) & uint32(*o.ClearFlags); overlap != 0 {
				r.Summary = fmt.Sprintf(
					"the operation sets and clears the same account flag(s) in one change: %s; a flag cannot move in both directions at once",
					flagBitsNames(overlap))
				r.Hint = "Keep each flag on exactly one side of the operation, or split the change into two operations."
			}
		}

	case "SetOptionsResultCodeSetOptionsUnknownFlag":
		var unknown uint32
		if o.SetFlags != nil {
			unknown |= uint32(*o.SetFlags) &^ authFlagMask
		}
		if o.ClearFlags != nil {
			unknown |= uint32(*o.ClearFlags) &^ authFlagMask
		}
		if unknown != 0 {
			r.Summary = fmt.Sprintf(
				"the flags carry unrecognised bit(s) 0x%x; the only defined account flags are %s",
				unknown, flagBitsNames(authFlagMask))
			r.Hint = "Mask the flag value down to the four defined bits before submitting."
		}

	case "SetOptionsResultCodeSetOptionsCantChange":
		// Core returns this only when auth flags are touched on an account
		// whose auth-immutable flag is set, and that flag is permanent.
		var touched uint32
		if o.ClearFlags != nil {
			touched |= uint32(*o.ClearFlags)
			if uint32(*o.ClearFlags)&authImmutableFlag != 0 {
				r.Summary = "the operation tries to clear the auth-immutable flag, but that flag is permanent once set; no operation can clear it"
				r.Hint = "An immutable account's auth flags are locked forever. Use a different account."
				return r
			}
		}
		if o.SetFlags != nil {
			touched |= uint32(*o.SetFlags)
		}
		if touched &= authFlagMask; touched != 0 {
			r.Summary = fmt.Sprintf(
				"the operation changes account auth flags (%s), but the account has auth-immutable set, which permanently locks its flags",
				flagBitsNames(touched))
			r.Hint = "Nothing can change an immutable account's auth flags. Use a different account, or merge into a fresh one."
		}

	case "SetOptionsResultCodeSetOptionsLowReserve":
		// The only reserve a set_options op can add is a new signer subentry.
		if o.Signer != nil && o.Signer.Weight != 0 {
			r.Summary = fmt.Sprintf(
				"adding %s as a new signer subentry raises the account's reserve by one base reserve (0.5 XLM on Mainnet), and the remaining balance cannot cover it",
				signerKeyLabel(o.Signer.Key))
			r.Hint = "Fund the account by at least one more base reserve, or free reserve by removing an offer, trustline, data entry or signer first."
		}

	case "SetOptionsResultCodeSetOptionsTooManySigners":
		// Only a genuinely new signer key hits the cap; updating the weight
		// of an existing signer does not.
		if o.Signer != nil && o.Signer.Weight != 0 {
			r.Summary = fmt.Sprintf(
				"the account already holds the maximum of five signers and %s is not one of them; updating an existing signer's weight would not hit this limit",
				signerKeyLabel(o.Signer.Key))
			r.Hint = "Remove a signer first (the same key with weight 0) before adding a new one."
		}

	case "SetOptionsResultCodeSetOptionsBadSigner":
		if o.Signer == nil {
			break
		}
		if addr, err := o.Signer.Key.GetAddress(); err == nil && addr != "" && addr == source {
			r.Summary = fmt.Sprintf(
				"the operation adds the source account %s as a signer of itself; an account can never sign for its own address",
				shortAddr(addr))
			r.Hint = "Use a distinct key (or a pre-auth transaction hash) as the signer."
			return r
		}
		if o.Signer.Weight > 255 {
			r.Summary = fmt.Sprintf("signer weight %d is outside the allowed range 0–255", o.Signer.Weight)
			r.Hint = "Signer weights are a single byte; clamp the weight to 0–255."
		}

	case "SetOptionsResultCodeSetOptionsInvalidHomeDomain":
		if o.HomeDomain != nil {
			r.Summary = fmt.Sprintf("home domain %q is not a syntactically valid domain name", string(*o.HomeDomain))
			r.Hint = "Use a valid DNS-style name, or the empty string to clear the domain."
		}

	case "SetOptionsResultCodeSetOptionsAuthRevocableRequired":
		// Clawback is only legal on a revocable account.
		if o.SetFlags != nil && uint32(*o.SetFlags)&authClawbackEnabledFlag != 0 {
			r.Summary = "the operation enables auth-clawback on an account that is not auth-revocable; clawback is only allowed on revocable accounts"
			r.Hint = "Include auth_revocable (0x2) in the same change, or set it before enabling clawback."
		}
	}
	return r
}

// signerKeyLabel renders a signer key short enough for prose while keeping
// its kind recognisable. It never panics: nil union arms degrade to the kind
// alone.
func signerKeyLabel(k xdr.SignerKey) string {
	hexOf := func(b *xdr.Uint256) string {
		if b == nil {
			return ""
		}
		return " " + hex.EncodeToString(b[:])[:8] + "…"
	}
	switch k.Type {
	case xdr.SignerKeyTypeSignerKeyTypeEd25519:
		if k.Ed25519 == nil {
			return "signer"
		}
		if addr, err := (&k).GetAddress(); err == nil && addr != "" {
			return "signer " + shortAddr(addr)
		}
		return "signer ed25519" + hexOf(k.Ed25519)
	case xdr.SignerKeyTypeSignerKeyTypePreAuthTx:
		return "pre-auth signer" + hexOf(k.PreAuthTx)
	case xdr.SignerKeyTypeSignerKeyTypeHashX:
		return "hash-x signer" + hexOf(k.HashX)
	case xdr.SignerKeyTypeSignerKeyTypeEd25519SignedPayload:
		if k.Ed25519SignedPayload == nil {
			return "signed-payload signer"
		}
		if addr, err := (&k).GetAddress(); err == nil && addr != "" {
			return "signed-payload signer " + shortAddr(addr)
		}
		return "signed-payload signer"
	}
	return "signer"
}

// deepenLiquidityPoolDepositFailure refines a failed liquidity-pool-deposit
// explanation with the operation's own amounts and price bounds, following
// the checks in stellar-core's LiquidityPoolDepositOpFrame.
func deepenLiquidityPoolDepositFailure(o xdr.LiquidityPoolDepositOp, r Reason) Reason {
	switch r.Constant {
	case "LiquidityPoolDepositResultCodeLiquidityPoolDepositMalformed":
		if summary, ok := liquidityPoolDepositMalformedReason(o); ok {
			r.Summary = summary
			r.Hint = "Fix the deposit parameters: positive max amounts and a strictly positive, non-empty price window (minPrice ≤ maxPrice)."
		}

	case "LiquidityPoolDepositResultCodeLiquidityPoolDepositBadPrice":
		if o.MinPrice.N <= 0 || o.MinPrice.D <= 0 || o.MaxPrice.N <= 0 || o.MaxPrice.D <= 0 {
			break // degenerate bounds; the malformed diagnosis owns this case
		}
		r.Summary = fmt.Sprintf(
			"the pool's ratio at apply time fell outside the price window the deposit accepted (A:B between %s and %s) — slippage between quoting and submission",
			priceRatio(o.MinPrice), priceRatio(o.MaxPrice))
		hint := "Widen minPrice/maxPrice to tolerate pool movement (for example ±1–5% around the quoted ratio) and resubmit; core compares the actual deposit amounts, which follow the pool's current ratio, against the window."
		if o.MinPrice.N == o.MaxPrice.N && o.MinPrice.D == o.MaxPrice.D {
			hint = "The window pins one exact ratio (min == max), so any trade before inclusion breaks it. Widen minPrice/maxPrice around the quoted ratio (for example ±1–5%) and resubmit."
		}
		if dir := depositAmountsVsWindow(o); dir != 0 {
			which := "below"
			if dir > 0 {
				which = "above"
			}
			r.Summary += fmt.Sprintf(
				"; the offered amounts themselves (%s and %s) imply a ratio that already falls %s the window",
				amount.String(o.MaxAmountA), amount.String(o.MaxAmountB), which)
		}
		r.Hint = hint

	case "LiquidityPoolDepositResultCodeLiquidityPoolDepositUnderfunded":
		r.Summary = fmt.Sprintf(
			"the source cannot cover the deposit of %s and %s: the spendable balance of one asset (balance, minus the base reserve and any sell liabilities) is short",
			amount.String(o.MaxAmountA), amount.String(o.MaxAmountB))
		r.Hint = "Check both trustline balances; for XLM the spendable amount excludes the minimum reserve and open-offer liabilities."

	case "LiquidityPoolDepositResultCodeLiquidityPoolDepositPoolFull":
		r.Summary = fmt.Sprintf(
			"the deposit would push a reserve of pool %s, or the pool's total shares, past the protocol maximum (2^63−1); the pool has effectively reached its cap",
			poolIDLabel(o.LiquidityPoolId))
		r.Hint = "No smaller deposit fixes a pool at its cap; use a different pool, or withdraw first."

	case "LiquidityPoolDepositResultCodeLiquidityPoolDepositLineFull":
		r.Summary = fmt.Sprintf(
			"the deposit would mint pool shares for pool %s beyond the source's pool-share trustline limit",
			poolIDLabel(o.LiquidityPoolId))
		r.Hint = "Raise the trustline limit with change_trust (the maximum is INT64_MAX), or hold fewer pool shares."

	case "LiquidityPoolDepositResultCodeLiquidityPoolDepositNoTrust":
		r.Summary = fmt.Sprintf(
			"the source has no pool-share trustline for pool %s, so it cannot hold the shares this deposit mints",
			poolIDLabel(o.LiquidityPoolId))
		r.Hint = "Open the pool-share trustline with change_trust on that pool before depositing."

	case "LiquidityPoolDepositResultCodeLiquidityPoolDepositNotAuthorized":
		r.Summary = fmt.Sprintf(
			"one of the two asset trustlines behind pool %s is not authorised to hold its asset; the deposit cannot proceed until the issuer authorises it",
			poolIDLabel(o.LiquidityPoolId))
		r.Hint = "Ask the asset issuer to authorise the trustline (allow-trust or set-trustline-flags), then retry."

	case "LiquidityPoolDepositResultCodeLiquidityPoolDepositTrustlineFrozen":
		r.Summary = fmt.Sprintf(
			"one of the trustlines this deposit touches for pool %s is frozen; frozen balances cannot move into the pool",
			poolIDLabel(o.LiquidityPoolId))
		r.Hint = "Ask the asset issuer to unfreeze the trustline, then retry."
	}
	return r
}

// liquidityPoolDepositMalformedReason mirrors the doCheckValid rules of
// stellar-core's LiquidityPoolDepositOpFrame and names the first rule the
// operation breaks.
func liquidityPoolDepositMalformedReason(o xdr.LiquidityPoolDepositOp) (string, bool) {
	switch {
	case o.MaxAmountA <= 0:
		return fmt.Sprintf("maxAmountA is %s; a deposit must offer a positive amount of each asset",
			amount.String(o.MaxAmountA)), true
	case o.MaxAmountB <= 0:
		return fmt.Sprintf("maxAmountB is %s; a deposit must offer a positive amount of each asset",
			amount.String(o.MaxAmountB)), true
	case o.MinPrice.N <= 0 || o.MinPrice.D <= 0:
		return fmt.Sprintf("minPrice %s is not strictly positive on both sides",
			priceRatio(o.MinPrice)), true
	case o.MaxPrice.N <= 0 || o.MaxPrice.D <= 0:
		return fmt.Sprintf("maxPrice %s is not strictly positive on both sides",
			priceRatio(o.MaxPrice)), true
	case int64(o.MinPrice.N)*int64(o.MaxPrice.D) > int64(o.MinPrice.D)*int64(o.MaxPrice.N):
		return fmt.Sprintf("minPrice %s exceeds maxPrice %s, leaving an empty price window",
			priceRatio(o.MinPrice), priceRatio(o.MaxPrice)), true
	}
	return "", false
}

// depositAmountsVsWindow reports where the deposit's own maximum amounts sit
// relative to the accepted price window: -1 below it, +1 above, 0 inside.
// Exact integer arithmetic avoids the rounding that would make a boundary
// case lie.
func depositAmountsVsWindow(o xdr.LiquidityPoolDepositOp) int {
	a := new(big.Int).SetInt64(int64(o.MaxAmountA))
	b := new(big.Int).SetInt64(int64(o.MaxAmountB))
	minN, minD := big.NewInt(int64(o.MinPrice.N)), big.NewInt(int64(o.MinPrice.D))
	maxN, maxD := big.NewInt(int64(o.MaxPrice.N)), big.NewInt(int64(o.MaxPrice.D))
	// ratio is B per A: below the window when b/a < minN/minD, above it
	// when b/a > maxN/maxD.
	if b.Mul(b, minD).Cmp(a.Mul(a, minN)) < 0 {
		return -1
	}
	if b.Mul(b, maxD).Cmp(a.Mul(a, maxN)) > 0 {
		return 1
	}
	return 0
}

// deepenLiquidityPoolWithdrawFailure refines a failed
// liquidity-pool-withdraw explanation with the operation's own share amount
// and minimum-return bounds, following stellar-core's
// LiquidityPoolWithdrawOpFrame.
func deepenLiquidityPoolWithdrawFailure(o xdr.LiquidityPoolWithdrawOp, r Reason) Reason {
	switch r.Constant {
	case "LiquidityPoolWithdrawResultCodeLiquidityPoolWithdrawMalformed":
		switch {
		case o.Amount <= 0:
			r.Summary = fmt.Sprintf("the operation burns %s pool shares; the share amount must be positive",
				amount.String(o.Amount))
			r.Hint = "Set amount to the number of pool shares to burn, above zero."
		case o.MinAmountA < 0:
			r.Summary = fmt.Sprintf("minAmountA is %s; minimum returns cannot be negative",
				amount.String(o.MinAmountA))
			r.Hint = "Set each minimum return to zero or more."
		case o.MinAmountB < 0:
			r.Summary = fmt.Sprintf("minAmountB is %s; minimum returns cannot be negative",
				amount.String(o.MinAmountB))
			r.Hint = "Set each minimum return to zero or more."
		}

	case "LiquidityPoolWithdrawResultCodeLiquidityPoolWithdrawUnderfunded":
		r.Summary = fmt.Sprintf(
			"the account holds fewer than the %s of pool shares this withdrawal burns; open offers against the pool count as liabilities",
			amount.String(o.Amount))
		r.Hint = "Burn fewer shares (lower amount), or wait for offers that lock the shares to fill or be removed."

	case "LiquidityPoolWithdrawResultCodeLiquidityPoolWithdrawUnderMinimum":
		r.Summary = fmt.Sprintf(
			"burning %s pool shares would return less than the minimums demanded (at least %s and %s); the pool's ratio moved since the quote — slippage",
			amount.String(o.Amount), amount.String(o.MinAmountA), amount.String(o.MinAmountB))
		r.Hint = "Relax minAmountA/minAmountB to tolerate the pool's current ratio and retry; like a price limit, tight minimums fail whenever anyone trades the pool first."

	case "LiquidityPoolWithdrawResultCodeLiquidityPoolWithdrawLineFull":
		r.Summary = fmt.Sprintf(
			"the %s and %s this withdrawal returns would push one of the account's asset trustlines past its limit",
			amount.String(o.MinAmountA), amount.String(o.MinAmountB))
		r.Hint = "Lower the trustline limit or free up capacity on the asset the account already holds, then retry."

	case "LiquidityPoolWithdrawResultCodeLiquidityPoolWithdrawNoTrust":
		r.Summary = fmt.Sprintf(
			"the source has no pool-share trustline for pool %s, so it holds no shares to burn",
			poolIDLabel(o.LiquidityPoolId))
		r.Hint = "The account must hold pool shares before it can withdraw; check the pool id."

	case "LiquidityPoolWithdrawResultCodeLiquidityPoolWithdrawTrustlineFrozen":
		r.Summary = fmt.Sprintf(
			"one of the trustlines this withdrawal touches for pool %s is frozen; frozen balances cannot receive the returned assets",
			poolIDLabel(o.LiquidityPoolId))
		r.Hint = "Ask the asset issuer to unfreeze the trustline, then retry."
	}
	return r
}

// priceRatio renders an XDR price as the ratio developers quote it by.
func priceRatio(p xdr.Price) string {
	return fmt.Sprintf("%d/%d", p.N, p.D)
}

// poolIDLabel abbreviates a 32-byte pool id for inline prose, keeping enough
// of both ends to be recognisable.
func poolIDLabel(id xdr.PoolId) string {
	h := hex.EncodeToString(id[:])
	return h[:8] + "…" + h[len(h)-4:]
}

// resultCodeName returns the transaction-level result code constant name.
func resultCodeName(res *xdr.TransactionResult) (string, error) {
	name := safeString(res.Result.Code)
	if name == "" {
		return "", fmt.Errorf("%w: could not read transaction result code", ErrInvalidXDR)
	}
	return name, nil
}

// innerResultCodeName returns the inner transaction's result code for a
// fee-bump result, if this is one.
func innerResultCodeName(res *xdr.TransactionResult) (string, bool) {
	pair, ok := res.Result.GetInnerResultPair()
	if !ok {
		return "", false
	}
	name := safeString(pair.Result.Result.Code)
	if name == "" {
		return "", false
	}
	return name, true
}

// manageDataReservePerEntry is the base reserve each data entry locks on the
// public network. Lens is offline-first and never queries network parameters,
// so this is the well-known default rather than a live value.
const manageDataReservePerEntry = "0.5 XLM"

// claimableBalanceBaseReserve is the public-network base reserve in stroops,
// used for claimable-balance reserve arithmetic. Like the manage_data figure
// above it is a documented constant rather than a network lookup: lens is
// offline-first and never reads ledger parameters at runtime.
const claimableBalanceBaseReserve int64 = int64(DefaultBaseReserve)

// ClaimableBalanceReserve returns the native reserve, in stroops, that a
// claimable balance locks on its creator's account.
//
// The protocol charges two base reserves for the entry itself and one more for
// each claimant, so the requirement is baseReserve * (2 + claimantCount). With
// the mainnet default of 0.5 XLM per base reserve, a balance with two claimants
// therefore locks 2 XLM. A negative claimantCount is treated as zero.
func ClaimableBalanceReserve(claimantCount int) int64 {
	if claimantCount < 0 {
		claimantCount = 0
	}
	return int64(2+claimantCount) * claimableBalanceBaseReserve
}

// withOperationContext adds argument-specific detail to a result code when the
// envelope carries enough information to produce it.
//
// A result code says what went wrong but not necessarily which entry, asset or
// claimant it was about: those live only in the envelope. They are resolved
// here and attached to the Reason rather than folded into the static reason
// table, which has no access to the operation.
func withOperationContext(r Reason, op xdr.Operation, txSource string) Reason {
	switch op.Body.Type {
	case xdr.OperationTypeManageData:
		md, ok := op.Body.GetManageDataOp()
		if !ok {
			return r
		}
		return withManageDataContext(r, md)
	case xdr.OperationTypeCreateClaimableBalance:
		cb, ok := op.Body.GetCreateClaimableBalanceOp()
		if !ok {
			return r
		}
		return withCreateClaimableBalanceContext(r, cb, operationEffectiveSource(txSource, op))
	default:
		return r
	}
}

// withManageDataContext names the manage_data entry a failure was about.
func withManageDataContext(r Reason, md xdr.ManageDataOp) Reason {
	key := string(md.DataName)

	switch r.Constant {
	case "ManageDataResultCodeManageDataNameNotFound":
		r.Context = fmt.Sprintf(
			"the account has no data entry named %q, so there is nothing to delete; it may already have been removed.", key)
	case "ManageDataResultCodeManageDataLowReserve":
		r.Context = fmt.Sprintf(
			"data entry %q adds one subentry, and each subentry locks %s of reserve; the account cannot lock another %s.",
			key, manageDataReservePerEntry, manageDataReservePerEntry)
	case "ManageDataResultCodeManageDataInvalidName":
		r.Context = fmt.Sprintf(
			"the data entry name %q is not valid: names must be 1-64 bytes, and an empty name cannot be set or deleted.", key)
	}
	return r
}

// withCreateClaimableBalanceContext correlates a create_claimable_balance
// failure with the asset, amount, claimants and source account in the
// envelope, and states the trustline or reserve requirement that was missed.
//
// The static reason table deliberately stays free of argument detail so that
// result-only explanations keep working; everything operation-specific is
// added here.
func withCreateClaimableBalanceContext(r Reason, op xdr.CreateClaimableBalanceOp, source string) Reason {
	asset := assetLabel(op.Asset)
	amountText := amount.String(op.Amount)
	claimants := claimableBalanceClaimantList(op.Claimants, false)
	sourceLabel := "the source account"
	if source != "" {
		sourceLabel = source
	}

	switch r.Constant {
	case "CreateClaimableBalanceResultCodeCreateClaimableBalanceMalformed":
		r.Context = malformedClaimableBalanceContext(op, asset, amountText)

	case "CreateClaimableBalanceResultCodeCreateClaimableBalanceLowReserve":
		reserve := ClaimableBalanceReserve(len(op.Claimants))
		r.Context = fmt.Sprintf(
			"the claimable balance of %s %s has %d claimant(s) (%s), so it locks %s of native reserve: two base reserves for the entry plus one per claimant, at 0.5 XLM each ((2 + %d) × 0.5 XLM). %s must hold that on top of its existing reserve; add XLM or shorten the claimant list before retrying.",
			amountText, asset, len(op.Claimants), claimants, compactXLM(reserve), len(op.Claimants), sourceLabel)

	case "CreateClaimableBalanceResultCodeCreateClaimableBalanceNoTrust":
		r.Context = fmt.Sprintf(
			"the claimable balance would lock %s %s, but %s has no trustline for %s. Open a trustline with a change-trust operation (limit above zero) before retrying; claimants only need their own trustline when they claim the balance.",
			amountText, asset, sourceLabel, asset)

	case "CreateClaimableBalanceResultCodeCreateClaimableBalanceNotAuthorized":
		r.Context = fmt.Sprintf(
			"the claimable balance would lock %s %s, but %s is not authorised to hold %s. The asset issuer must authorise that trustline before the balance can be created; claimants also need an authorised trustline when they claim.",
			amountText, asset, sourceLabel, asset)

	case "CreateClaimableBalanceResultCodeCreateClaimableBalanceUnderfunded":
		if op.Asset.Type == xdr.AssetTypeAssetTypeNative {
			r.Context = fmt.Sprintf(
				"the claimable balance would lock %s XLM for %s, but %s does not hold enough spendable XLM: the amount must fit after selling liabilities and the account's minimum reserve. Claimants cannot contribute; fund the creator or lower the amount.",
				amountText, claimants, sourceLabel)
		} else {
			r.Context = fmt.Sprintf(
				"the claimable balance would lock %s %s for %s, but %s holds less than that on its %s trustline. Claimants cannot contribute; fund the creator or lower the amount.",
				amountText, asset, claimants, sourceLabel, asset)
		}
	}
	return r
}

// compactXLM renders a stroop count without the trailing decimal zeros that
// reserve arithmetic would otherwise show, e.g. "2 XLM" rather than
// "2.0000000 XLM".
func compactXLM(stroops int64) string {
	text := strings.TrimRight(amount.StringFromInt64(stroops), "0")
	text = strings.TrimSuffix(text, ".")
	if text == "" || text == "-" {
		text += "0"
	}
	return text + " XLM"
}

// malformedClaimableBalanceContext names the first structural problem found in
// a malformed create-claimable-balance operation. The checks mirror
// stellar-core's doCheckValid: a positive amount, 1-10 claimants, no duplicate
// destinations, and valid predicates.
func malformedClaimableBalanceContext(op xdr.CreateClaimableBalanceOp, asset, amountText string) string {
	prefix := fmt.Sprintf("the claimable balance of %s %s is malformed: ", amountText, asset)

	switch {
	case op.Amount <= 0:
		return prefix + "the amount must be positive; a zero or negative amount cannot be locked."
	case len(op.Claimants) == 0:
		return prefix + "it lists no claimants, so nobody could ever claim it; provide between 1 and 10 claimants."
	case len(op.Claimants) > 10:
		return fmt.Sprintf("%sit lists %d claimants, but the protocol allows at most 10.", prefix, len(op.Claimants))
	}

	if duplicates := duplicateClaimantDestinations(op.Claimants); len(duplicates) > 0 {
		return fmt.Sprintf("%sclaimant %s appears more than once; each destination may appear only once, even with different predicates.",
			prefix, strings.Join(duplicates, ", "))
	}
	for _, claimant := range op.Claimants {
		v0, ok := claimant.GetV0()
		if !ok {
			continue
		}
		if problem := claimPredicateProblem(v0.Predicate, 1); problem != "" {
			return fmt.Sprintf("%sclaimant %s has an invalid predicate: %s.",
				prefix, v0.Destination.Address(), problem)
		}
	}
	return prefix + "check that the amount is positive, there are 1-10 unique claimants, and every predicate is valid."
}

// claimableBalanceClaimantList renders claimants for inline prose, capping the
// list so a ten-claimant operation stays readable. Context uses full strkey
// addresses because a developer copying one into a change-trust operation
// needs the unambiguous form; the one-line operation detail abbreviates them.
func claimableBalanceClaimantList(claimants []xdr.Claimant, abbreviate bool) string {
	if len(claimants) == 0 {
		return "no claimants"
	}
	const maxListed = 3
	parts := make([]string, 0, maxListed+1)
	for i, claimant := range claimants {
		if i == maxListed {
			parts = append(parts, fmt.Sprintf("and %d more", len(claimants)-maxListed))
			break
		}
		parts = append(parts, claimableBalanceClaimantLabel(claimant, abbreviate))
	}
	return strings.Join(parts, ", ")
}

// claimableBalanceClaimantLabel names one claimant and its predicate.
func claimableBalanceClaimantLabel(claimant xdr.Claimant, abbreviate bool) string {
	v0, ok := claimant.GetV0()
	if !ok {
		return "an unsupported claimant type"
	}
	addr := v0.Destination.Address()
	if abbreviate {
		addr = shortAddr(addr)
	}
	return fmt.Sprintf("%s (%s)", addr, DescribeClaimPredicate(v0.Predicate))
}

// duplicateClaimantDestinations returns the destinations that appear more than
// once. The protocol rejects duplicate destinations regardless of predicate.
func duplicateClaimantDestinations(claimants []xdr.Claimant) []string {
	seen := make(map[string]bool, len(claimants))
	reported := make(map[string]bool)
	var duplicates []string
	for _, claimant := range claimants {
		v0, ok := claimant.GetV0()
		if !ok {
			continue
		}
		addr := v0.Destination.Address()
		if seen[addr] && !reported[addr] {
			reported[addr] = true
			duplicates = append(duplicates, addr)
			continue
		}
		seen[addr] = true
	}
	return duplicates
}

// claimPredicateProblem validates one predicate arm against the protocol's
// structural rules and returns a description of the first violation. depth is
// the nesting level, starting at 1; stellar-core rejects anything past four.
func claimPredicateProblem(p xdr.ClaimPredicate, depth int) string {
	if depth > 4 {
		return "predicates may nest at most 4 levels deep"
	}
	switch p.Type {
	case xdr.ClaimPredicateTypeClaimPredicateUnconditional:
		return ""
	case xdr.ClaimPredicateTypeClaimPredicateBeforeAbsoluteTime:
		if p.AbsBefore == nil {
			return "an absolute-time predicate has no time"
		}
		if *p.AbsBefore < 0 {
			return "an absolute-time predicate cannot be negative"
		}
		return ""
	case xdr.ClaimPredicateTypeClaimPredicateBeforeRelativeTime:
		if p.RelBefore == nil {
			return "a relative-time predicate has no duration"
		}
		if *p.RelBefore < 0 {
			return "a relative-time predicate cannot be negative"
		}
		return ""
	case xdr.ClaimPredicateTypeClaimPredicateNot:
		if p.NotPredicate == nil || *p.NotPredicate == nil {
			return "a not predicate has no child"
		}
		return claimPredicateProblem(**p.NotPredicate, depth+1)
	case xdr.ClaimPredicateTypeClaimPredicateAnd, xdr.ClaimPredicateTypeClaimPredicateOr:
		var children []xdr.ClaimPredicate
		if p.Type == xdr.ClaimPredicateTypeClaimPredicateAnd {
			if p.AndPredicates == nil {
				return "an and predicate has no children"
			}
			children = *p.AndPredicates
		} else {
			if p.OrPredicates == nil {
				return "an or predicate has no children"
			}
			children = *p.OrPredicates
		}
		if len(children) != 2 {
			return fmt.Sprintf("an %s predicate must have exactly 2 children, not %d", strings.ToLower(strings.TrimPrefix(p.Type.String(), "ClaimPredicateTypeClaimPredicate")), len(children))
		}
		for _, child := range children {
			if problem := claimPredicateProblem(child, depth+1); problem != "" {
				return problem
			}
		}
		return ""
	default:
		return "unknown predicate type"
	}
}

func envelopeHeadline(s *Summary) string {
	var b strings.Builder
	b.WriteString("Transaction from ")
	b.WriteString(shortAddr(s.Source))
	fmt.Fprintf(&b, " with %d operation", len(s.Operations))
	if len(s.Operations) != 1 {
		b.WriteString("s")
	}
	if len(s.Operations) > 0 {
		types := make([]string, 0, len(s.Operations))
		seen := map[string]bool{}
		for _, op := range s.Operations {
			if !seen[op.Type] {
				seen[op.Type] = true
				types = append(types, op.Type)
			}
		}
		b.WriteString(" (" + strings.Join(types, ", ") + ")")
	}
	if s.FeeBumpSource != "" {
		b.WriteString(", fee-bumped by " + shortAddr(s.FeeBumpSource))
	}
	b.WriteString(".")
	return b.String()
}

func resultHeadline(s *Summary, o *Outcome) string {
	if o.Success {
		if len(s.Operations) > 0 && s.Kind == "transaction" {
			return fmt.Sprintf("Transaction succeeded: all %d operation(s) applied.", len(s.Operations))
		}
		return "Transaction succeeded."
	}

	// Prefer naming the operation that actually failed, which is the thing
	// the user is looking for.
	if len(o.FailedOps) > 0 {
		idx := o.FailedOps[0]
		if idx < len(s.Operations) {
			op := s.Operations[idx]
			label := op.Type
			if label == "" || label == "unknown" {
				label = "operation"
			}
			detail := ""
			if op.Detail != "" {
				detail = " (" + op.Detail + ")"
			}
			reason := ""
			if op.Result != nil {
				reason = " " + op.Result.Summary
			}
			return fmt.Sprintf("Transaction failed at operation %d, %s%s:%s", idx, label, detail, reason)
		}
	}

	if o.InnerReason != nil && !o.InnerReason.Success {
		return "Transaction failed: " + o.InnerReason.Summary
	}
	return "Transaction failed: " + o.Reason.Summary
}

// describeOperation renders a one-line description of what an operation does.
//
// The common operations get a purpose-written line, since those are what
// developers are usually debugging. Anything else falls back to its type
// name, which is still accurate — just less specific.
func describeOperation(op xdr.Operation) string {
	body := op.Body
	switch body.Type {
	case xdr.OperationTypeCreateAccount:
		o, ok := body.GetCreateAccountOp()
		if !ok {
			break
		}
		return fmt.Sprintf("create account %s funded with %s XLM",
			shortAddr(o.Destination.Address()), amount.String(o.StartingBalance))

	case xdr.OperationTypePayment:
		o, ok := body.GetPaymentOp()
		if !ok {
			break
		}
		return fmt.Sprintf("pay %s %s to %s",
			amount.String(o.Amount), assetLabel(o.Asset), shortAddr(addressOf(o.Destination)))

	case xdr.OperationTypePathPaymentStrictReceive:
		o, ok := body.GetPathPaymentStrictReceiveOp()
		if !ok {
			break
		}
		desc := fmt.Sprintf("send at most %s %s to deliver exactly %s %s to %s",
			amount.String(o.SendMax), assetLabel(o.SendAsset),
			amount.String(o.DestAmount), assetLabel(o.DestAsset), shortAddr(addressOf(o.Destination)))
		if len(o.Path) > 0 {
			hops := make([]string, len(o.Path))
			for i, a := range o.Path {
				hops[i] = assetLabel(a)
			}
			desc += fmt.Sprintf(" via [%s]", strings.Join(hops, " -> "))
		}
		return desc

	case xdr.OperationTypePathPaymentStrictSend:
		o, ok := body.GetPathPaymentStrictSendOp()
		if !ok {
			break
		}
		desc := fmt.Sprintf("send exactly %s %s to deliver at least %s %s to %s",
			amount.String(o.SendAmount), assetLabel(o.SendAsset),
			amount.String(o.DestMin), assetLabel(o.DestAsset), shortAddr(addressOf(o.Destination)))
		if len(o.Path) > 0 {
			hops := make([]string, len(o.Path))
			for i, a := range o.Path {
				hops[i] = assetLabel(a)
			}
			desc += fmt.Sprintf(" via [%s]", strings.Join(hops, " -> "))
		}
		return desc

	case xdr.OperationTypeManageSellOffer:
		o, ok := body.GetManageSellOfferOp()
		if !ok {
			break
		}
		return fmt.Sprintf("sell %s %s for %s at %d/%d (offer id %d)",
			amount.String(o.Amount), assetLabel(o.Selling), assetLabel(o.Buying),
			o.Price.N, o.Price.D, o.OfferId)

	case xdr.OperationTypeManageBuyOffer:
		o, ok := body.GetManageBuyOfferOp()
		if !ok {
			break
		}
		return fmt.Sprintf("buy %s %s with %s at %d/%d (offer id %d)",
			amount.String(o.BuyAmount), assetLabel(o.Buying), assetLabel(o.Selling),
			o.Price.N, o.Price.D, o.OfferId)

	case xdr.OperationTypeCreatePassiveSellOffer:
		o, ok := body.GetCreatePassiveSellOfferOp()
		if !ok {
			break
		}
		return fmt.Sprintf("passively sell %s %s for %s at %d/%d",
			amount.String(o.Amount), assetLabel(o.Selling), assetLabel(o.Buying), o.Price.N, o.Price.D)

	case xdr.OperationTypeChangeTrust:
		o, ok := body.GetChangeTrustOp()
		if !ok {
			break
		}
		if o.Limit == 0 {
			return fmt.Sprintf("remove trustline for %s", changeTrustAssetLabel(o.Line))
		}
		return fmt.Sprintf("trust %s up to %s", changeTrustAssetLabel(o.Line), amount.String(o.Limit))

	case xdr.OperationTypeAccountMerge:
		// The SDK's GetDestination dereferences the arm without a nil check,
		// so guard before calling it on a hand-built body.
		if body.Destination == nil {
			break
		}
		dest, ok := body.GetDestination()
		if !ok {
			break
		}
		// Failure guidance for this operation lives in
		// deepenAccountMergeFailure, which fires when a result is paired.
		return "merge this account into " + shortAddr(addressOf(dest))

	case xdr.OperationTypeBumpSequence:
		o, ok := body.GetBumpSequenceOp()
		if !ok {
			break
		}
		return fmt.Sprintf("bump sequence number to %d", int64(o.BumpTo))

	case xdr.OperationTypeCreateClaimableBalance:
		o, ok := body.GetCreateClaimableBalanceOp()
		if !ok {
			break
		}
		return fmt.Sprintf("create claimable balance of %s %s claimable by %s",
			amount.String(o.Amount), assetLabel(o.Asset), claimableBalanceClaimantList(o.Claimants, true))

	case xdr.OperationTypeManageData:
		o, ok := body.GetManageDataOp()
		if !ok {
			break
		}
		dataName := string(o.DataName)
		if o.DataValue == nil {
			return fmt.Sprintf("delete data entry %q", dataName)
		}
		return fmt.Sprintf("set data entry %q (%d bytes)", dataName, len(*o.DataValue))

	case xdr.OperationTypeSetOptions:
		o, ok := body.GetSetOptionsOp()
		if !ok {
			break
		}
		return describeSetOptions(o)

	case xdr.OperationTypeInvokeHostFunction:
		o, ok := body.GetInvokeHostFunctionOp()
		if !ok {
			break
		}
		return describeHostFunction(o)

	case xdr.OperationTypeExtendFootprintTtl:
		o, ok := body.GetExtendFootprintTtlOp()
		if !ok {
			break
		}
		return fmt.Sprintf("extend ledger entry lifetimes to %d ledgers", o.ExtendTo)

	case xdr.OperationTypeRestoreFootprint:
		return "restore archived ledger entries"

	case xdr.OperationTypeLiquidityPoolDeposit:
		o, ok := body.GetLiquidityPoolDepositOp()
		if !ok {
			break
		}
		return fmt.Sprintf("deposit up to %s and %s into pool %s within price bounds %s..%s",
			amount.String(o.MaxAmountA), amount.String(o.MaxAmountB),
			poolIDLabel(o.LiquidityPoolId), priceRatio(o.MinPrice), priceRatio(o.MaxPrice))

	case xdr.OperationTypeLiquidityPoolWithdraw:
		o, ok := body.GetLiquidityPoolWithdrawOp()
		if !ok {
			break
		}
		return fmt.Sprintf("burn %s pool shares of pool %s for at least %s and %s",
			amount.String(o.Amount), poolIDLabel(o.LiquidityPoolId),
			amount.String(o.MinAmountA), amount.String(o.MinAmountB))

	case xdr.OperationTypeSetTrustLineFlags:
		o, ok := body.GetSetTrustLineFlagsOp()
		if !ok {
			break
		}
		return describeSetTrustLineFlags(o)
	}
	return operationTypeLabel(body.Type)
}

func describeSetOptions(o xdr.SetOptionsOp) string {
	var parts []string
	if o.InflationDest != nil {
		parts = append(parts, "set inflation destination")
	}
	if o.ClearFlags != nil {
		parts = append(parts, "clear flags "+strconv.FormatUint(uint64(*o.ClearFlags), 10))
	}
	if o.SetFlags != nil {
		parts = append(parts, "set flags "+strconv.FormatUint(uint64(*o.SetFlags), 10))
	}
	if o.MasterWeight != nil {
		parts = append(parts, "master weight "+strconv.FormatUint(uint64(*o.MasterWeight), 10))
	}
	if o.LowThreshold != nil {
		parts = append(parts, "low threshold "+strconv.FormatUint(uint64(*o.LowThreshold), 10))
	}
	if o.MedThreshold != nil {
		parts = append(parts, "medium threshold "+strconv.FormatUint(uint64(*o.MedThreshold), 10))
	}
	if o.HighThreshold != nil {
		parts = append(parts, "high threshold "+strconv.FormatUint(uint64(*o.HighThreshold), 10))
	}
	if o.HomeDomain != nil {
		parts = append(parts, "home domain "+string(*o.HomeDomain))
	}
	if o.Signer != nil {
		parts = append(parts, fmt.Sprintf("signer weight %d", o.Signer.Weight))
	}
	if len(parts) == 0 {
		return "set options (no changes)"
	}
	return strings.Join(parts, ", ")
}

// describeHostFunction names the Soroban contract and function being called,
// which is the part a developer recognises.
func describeHostFunction(o xdr.InvokeHostFunctionOp) string {
	switch o.HostFunction.Type {
	case xdr.HostFunctionTypeHostFunctionTypeInvokeContract:
		inv, ok := o.HostFunction.GetInvokeContract()
		if !ok {
			break
		}
		contract := ""
		if addr, err := inv.ContractAddress.String(); err == nil {
			contract = shortAddr(addr)
		}
		fn := string(inv.FunctionName)
		return fmt.Sprintf("call %s on contract %s with %d argument(s)", fn, contract, len(inv.Args))

	case xdr.HostFunctionTypeHostFunctionTypeCreateContract,
		xdr.HostFunctionTypeHostFunctionTypeCreateContractV2:
		return "create a contract instance"

	case xdr.HostFunctionTypeHostFunctionTypeUploadContractWasm:
		wasm, ok := o.HostFunction.GetWasm()
		if !ok {
			break
		}
		return fmt.Sprintf("upload contract wasm (%d bytes)", len(wasm))
	}
	return "invoke host function"
}

// describePreconditions renders the transaction's preconditions as readable
// lines, omitting the ones that are unset.
func describePreconditions(env *xdr.TransactionEnvelope) []string {
	var out []string
	if tb := env.TimeBounds(); tb != nil {
		switch {
		case tb.MinTime == 0 && tb.MaxTime == 0:
			// No effective bound; say nothing rather than print zeros.
		case tb.MaxTime == 0:
			out = append(out, "valid from "+formatTime(int64(tb.MinTime)))
		case tb.MinTime == 0:
			out = append(out, "valid until "+formatTime(int64(tb.MaxTime)))
		default:
			out = append(out, "valid from "+formatTime(int64(tb.MinTime))+" until "+formatTime(int64(tb.MaxTime)))
		}
	}
	if lb := env.LedgerBounds(); lb != nil && (lb.MinLedger != 0 || lb.MaxLedger != 0) {
		out = append(out, fmt.Sprintf("ledger bounds %d..%d", lb.MinLedger, lb.MaxLedger))
	}
	if ms := env.MinSeqNum(); ms != nil {
		out = append(out, fmt.Sprintf("minimum sequence number %d", *ms))
	}
	if age := env.MinSeqAge(); age != nil && *age != 0 {
		out = append(out, fmt.Sprintf("minimum sequence age %ds", uint64(*age)))
	}
	if gap := env.MinSeqLedgerGap(); gap != nil && *gap != 0 {
		out = append(out, fmt.Sprintf("minimum sequence ledger gap %d", uint32(*gap)))
	}
	if extra := env.ExtraSigners(); len(extra) > 0 {
		out = append(out, fmt.Sprintf("%d extra signer(s) required", len(extra)))
	}
	return out
}

func describeMemo(m xdr.Memo) string {
	switch m.Type {
	case xdr.MemoTypeMemoNone:
		return ""
	case xdr.MemoTypeMemoText:
		if t, ok := m.GetText(); ok {
			return "text: " + t
		}
	case xdr.MemoTypeMemoId:
		if id, ok := m.GetId(); ok {
			return "id: " + strconv.FormatUint(uint64(id), 10)
		}
	case xdr.MemoTypeMemoHash:
		if h, ok := m.GetHash(); ok {
			return "hash: " + h.HexString()
		}
	case xdr.MemoTypeMemoReturn:
		if h, ok := m.GetRetHash(); ok {
			return "return: " + h.HexString()
		}
	}
	return strings.TrimPrefix(m.Type.String(), "MemoTypeMemo")
}

// operationSource returns the operation-level source account override, or ""
// when the operation inherits the transaction's source.
func operationSource(op xdr.Operation) string {
	if op.SourceAccount == nil {
		return ""
	}
	return addressOf(*op.SourceAccount)
}

func assetLabel(a xdr.Asset) string {
	if a.Type == xdr.AssetTypeAssetTypeNative {
		return "XLM"
	}
	return a.StringCanonical()
}

func changeTrustAssetLabel(a xdr.ChangeTrustAsset) string {
	if a.Type == xdr.AssetTypeAssetTypePoolShare {
		return "liquidity pool shares"
	}
	return assetLabel(a.ToAsset())
}

// addressOf renders a muxed account as strkey, tolerating a malformed value.
func addressOf(m xdr.MuxedAccount) string {
	addr, err := (&m).GetAddress()
	if err != nil {
		return ""
	}
	return addr
}

// shortAddr abbreviates a strkey address for inline prose, keeping enough of
// both ends to be recognisable.
func shortAddr(addr string) string {
	if len(addr) <= 12 {
		return addr
	}
	return addr[:5] + "…" + addr[len(addr)-4:]
}

func formatTime(unix int64) string {
	if unix == 0 {
		return "0"
	}
	return time.Unix(unix, 0).UTC().Format(time.RFC3339)
}

func contextualizeChangeTrustFailure(op xdr.Operation, reason Reason) Reason {
	if op.Body.Type != xdr.OperationTypeChangeTrust {
		return reason
	}
	body, ok := op.Body.GetChangeTrustOp()
	if !ok {
		return reason
	}
	asset := changeTrustAssetLabel(body.Line)
	if asset == "" {
		asset = "this asset"
	}

	switch reason.Code {
	case "change_trust_malformed":
		reason.Summary = fmt.Sprintf("The ChangeTrust request for %s is malformed: the asset or limit is invalid.", asset)
		reason.Hint = fmt.Sprintf("Check the asset and limit for %s before resubmitting; a valid ChangeTrust operation must identify a valid asset and a non-negative limit.", asset)
	case "change_trust_no_issuer":
		issuer := changeTrustIssuerLabel(body.Line)
		if issuer == "" {
			reason.Summary = fmt.Sprintf("The trustline for %s cannot be created because the issuing account does not exist on-chain.", asset)
			reason.Hint = fmt.Sprintf("Create or fund the issuing account for %s on the target network, or switch to the correct network before retrying.", asset)
			return reason
		}
		reason.Summary = fmt.Sprintf("The trustline for %s cannot be created because the issuer account %s does not exist on-chain.", asset, issuer)
		reason.Hint = fmt.Sprintf("Create or fund the issuer account %s on the target network before retrying with %s.", issuer, asset)
	case "change_trust_invalid_limit":
		reason.Summary = fmt.Sprintf("The new limit for %s is below the current liabilities or balance already held, so the trustline would shrink beneath existing obligations.", asset)
		reason.Hint = "Increase the limit to at least the current liabilities or balance, or reduce the existing balance and liabilities before resubmitting."
	case "change_trust_low_reserve":
		reason.Summary = fmt.Sprintf("Opening or updating a trustline for %s would leave the account below the minimum XLM reserve required for another trustline.", asset)
		reason.Hint = "Fund the source account with enough XLM to cover the network reserve requirement for this additional trustline, then retry."
	}
	return reason
}

func changeTrustIssuerLabel(line xdr.ChangeTrustAsset) string {
	if line.Type == xdr.AssetTypeAssetTypeNative || line.Type == xdr.AssetTypeAssetTypePoolShare {
		return ""
	}
	asset := line.ToAsset()
	if asset.Type == xdr.AssetTypeAssetTypeNative {
		return ""
	}
	return asset.GetIssuer()
}

// deepenClawbackFailure refines a failed clawback explanation using the
// operation's asset, source, and target account. The protocol makes these
// checks deterministically from the body: clawback is only valid when the
// trustline was opened with the clawback-enabled flag, and the amount cannot
// exceed the target's balance.
func deepenClawbackFailure(source string, o xdr.ClawbackOp, r Reason) Reason {
	switch r.Constant {
	case "ClawbackResultCodeClawbackNotClawbackEnabled":
		r.Summary = fmt.Sprintf("the trustline for %s from %s was opened without the issuer's clawback-enabled flag, so this account is not allowed to be clawed back",
			assetLabel(o.Asset), shortAddr(source))
		r.Hint = "Enable the issuer's clawback flag before opening the trustline, or create a new trustline with trustline_clawback_enabled set and retry."
	case "ClawbackResultCodeClawbackNoTrust":
		r.Summary = fmt.Sprintf("the target account %s has no trustline for %s, so there is nothing to claw back",
			shortAddr(source), assetLabel(o.Asset))
		r.Hint = "Create the asset trustline for the target account before attempting clawback, or use the correct account and asset."
	case "ClawbackResultCodeClawbackUnderfunded":
		r.Summary = fmt.Sprintf("the clawback amount %s exceeds the current balance on the trustline for %s; the target account does not hold enough of that asset",
			amount.String(o.Amount), assetLabel(o.Asset))
		r.Hint = "Reduce the clawback amount to the target's current balance, or verify the trustline balance before submitting the operation."
	}
	return r
}

// deepenClawbackClaimableBalanceFailure refines a failed claimable-balance
// clawback explanation using the claimable balance id and the asset metadata it
// carries. The protocol only permits this when the claimable balance was
// created with clawback enabled.
func deepenClawbackClaimableBalanceFailure(o xdr.ClawbackClaimableBalanceOp, r Reason) Reason {
	switch r.Constant {
	case "ClawbackClaimableBalanceResultCodeClawbackClaimableBalanceNotClawbackEnabled":
		if balanceID, err := o.BalanceId.MarshalBinary(); err == nil {
			r.Summary = fmt.Sprintf("the claimable balance %s was created without clawback enabled, so it cannot be clawed back", hex.EncodeToString(balanceID)[:12])
		} else {
			r.Summary = "the claimable balance was created without clawback enabled, so it cannot be clawed back"
		}
		r.Hint = "Recreate the claimable balance with the clawback-enabled flag set, or use a balance that was issued under the clawback-enabled configuration."
	case "ClawbackClaimableBalanceResultCodeClawbackClaimableBalanceNotIssuer":
		r.Summary = "the sender is not the asset issuer for this claimable balance, so clawback is not authorised"
		r.Hint = "Only the issuing account can claw back a claimable balance for this asset."
	case "ClawbackClaimableBalanceResultCodeClawbackClaimableBalanceDoesNotExist":
		r.Summary = "the claimable balance id does not exist in the ledger, so there is nothing to claw back"
		r.Hint = "Verify the balance id and the correct network state before retrying."
	}
	return r
}

func describeSetTrustLineFlags(o xdr.SetTrustLineFlagsOp) string {
	var parts []string
	if o.SetFlags != 0 {
		setNames := trustLineFlagNames(o.SetFlags)
		if len(setNames) > 0 {
			parts = append(parts, "set "+strings.Join(setNames, "|"))
		} else {
			parts = append(parts, fmt.Sprintf("set flags %d", o.SetFlags))
		}
	}
	if o.ClearFlags != 0 {
		clearNames := trustLineFlagNames(o.ClearFlags)
		revoking := o.ClearFlags&xdr.Uint32(xdr.TrustLineFlagsAuthorizedFlag) != 0 ||
			o.ClearFlags&xdr.Uint32(xdr.TrustLineFlagsAuthorizedToMaintainLiabilitiesFlag) != 0
		if len(clearNames) > 0 {
			msg := "clear " + strings.Join(clearNames, "|")
			if revoking {
				msg += " (revoking authorization, requires issuer AUTH_REVOCABLE_FLAG)"
			}
			parts = append(parts, msg)
		} else {
			parts = append(parts, fmt.Sprintf("clear flags %d", o.ClearFlags))
		}
	}
	action := "modify trustline flags"
	if len(parts) > 0 {
		action = strings.Join(parts, ", ")
	}
	trustor := o.Trustor.Address()
	return fmt.Sprintf("%s for %s on %s", action, assetLabel(o.Asset), shortAddr(trustor))
}

// PathPaymentHop describes an intermediate conversion step in a path payment route.
type PathPaymentHop struct {
	Index   int    `json:"index"`
	From    string `json:"from"`
	To      string `json:"to"`
	Summary string `json:"summary"`
}

// PathPaymentSlippageDiagnosis provides contextual diagnostics breaking down
// intermediate hops and order book slippage in a path payment.
type PathPaymentSlippageDiagnosis struct {
	OperationIndex int              `json:"operationIndex"`
	PaymentType    string           `json:"paymentType"` // "strict_receive" or "strict_send"
	SendAsset      string           `json:"sendAsset"`
	DestAsset      string           `json:"destAsset"`
	SendMax        string           `json:"sendMax,omitempty"`
	DestMin        string           `json:"destMin,omitempty"`
	Amount         string           `json:"amount"`
	Destination    string           `json:"destination"`
	Hops           []PathPaymentHop `json:"hops,omitempty"`
	Summary        string           `json:"summary"`
	Explanation    string           `json:"explanation"`
	Hint           string           `json:"hint"`
	// StrictReceive carries the route and limits of a failed strict-receive payment.
	StrictReceive *PathPaymentDiagnosis `json:"strictReceive,omitempty"`
	// StrictSend carries the route and limits of a failed strict-send payment.
	StrictSend *PathPaymentStrictSendDiagnosis `json:"strictSend,omitempty"`
}

func (d *PathPaymentSlippageDiagnosis) String() string {
	if d == nil {
		return "<nil>"
	}
	return d.Summary
}

func (d *PathPaymentSlippageDiagnosis) DiagnosticReport() string {
	if d == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Path payment slippage failure:\n")
	fmt.Fprintf(&b, "  Operation:       %d (%s)\n", d.OperationIndex, d.PaymentType)
	fmt.Fprintf(&b, "  Route:           %s -> %s\n", d.SendAsset, d.DestAsset)
	if d.SendMax != "" {
		fmt.Fprintf(&b, "  Send limit:      sendMax %s %s\n", d.SendMax, d.SendAsset)
	}
	if d.DestMin != "" {
		fmt.Fprintf(&b, "  Dest limit:      destMin %s %s\n", d.DestMin, d.DestAsset)
	}
	if d.Destination != "" {
		fmt.Fprintf(&b, "  Destination:     %s\n", d.Destination)
	}
	if len(d.Hops) > 0 {
		fmt.Fprintf(&b, "  Intermediate hops (%d):\n", len(d.Hops))
		for _, hop := range d.Hops {
			fmt.Fprintf(&b, "    %d. %s\n", hop.Index+1, hop.Summary)
		}
	}
	fmt.Fprintf(&b, "  Explanation:     %s\n", d.Explanation)
	fmt.Fprintf(&b, "  Hint:            %s\n", d.Hint)
	return b.String()
}

func buildPathPaymentHops(sendAsset xdr.Asset, path []xdr.Asset, destAsset xdr.Asset) []PathPaymentHop {
	assets := make([]xdr.Asset, 0, len(path)+2)
	assets = append(assets, sendAsset)
	assets = append(assets, path...)
	assets = append(assets, destAsset)

	hops := make([]PathPaymentHop, 0, len(assets)-1)
	for i := 0; i < len(assets)-1; i++ {
		from := assetLabel(assets[i])
		to := assetLabel(assets[i+1])
		hops = append(hops, PathPaymentHop{
			Index:   i,
			From:    from,
			To:      to,
			Summary: fmt.Sprintf("%s -> %s via DEX order book", from, to),
		})
	}
	return hops
}

// DiagnosePathPaymentStrictReceive diagnoses a strict receive path payment failure,
// detailing intermediate conversion hops and slippage past the sendMax limit.
func DiagnosePathPaymentStrictReceive(op xdr.PathPaymentStrictReceiveOp, opIndex int, code string) *PathPaymentSlippageDiagnosis {
	hops := buildPathPaymentHops(op.SendAsset, op.Path, op.DestAsset)
	dest := shortAddr(addressOf(op.Destination))
	sendAssetStr := assetLabel(op.SendAsset)
	destAssetStr := assetLabel(op.DestAsset)
	sendMaxStr := amount.String(op.SendMax)
	destAmountStr := amount.String(op.DestAmount)

	hopCount := len(hops)
	var explanation, summary, hint string

	switch {
	case strings.Contains(code, "over_send_max") || strings.Contains(code, "over_sendmax") || strings.Contains(strings.ToLower(code), "oversendmax"):
		summary = fmt.Sprintf("Path payment failed: required send amount exceeds sendMax limit of %s %s",
			sendMaxStr, sendAssetStr)
		explanation = fmt.Sprintf("Order book liquidity shifted across %d intermediate trade hop(s) between path quotation and execution. Fluctuation in order book offer prices increased the cumulative conversion cost beyond the configured sendMax (%s %s).",
			hopCount, sendMaxStr, sendAssetStr)
		hint = "Market price moved against you on the DEX. Increase sendMax to allow for higher slippage tolerance, or re-quote path liquidity before resubmitting."
	case strings.Contains(code, "too_few_offers") || strings.Contains(code, "TooFewOffers"):
		summary = fmt.Sprintf("Path payment failed: insufficient order book depth across %d hop(s)", hopCount)
		explanation = fmt.Sprintf("One or more order books along the path (%s -> %s) lacked sufficient bid/ask offer depth to satisfy the requested conversion amount of %s %s.",
			sendAssetStr, destAssetStr, destAmountStr, destAssetStr)
		hint = "Liquidity was depleted between path computation and submission. Check liquidity on intermediate pairs or split the payment into smaller amounts."
	default:
		return nil
	}

	return &PathPaymentSlippageDiagnosis{
		OperationIndex: opIndex,
		PaymentType:    "strict_receive",
		SendAsset:      sendAssetStr,
		DestAsset:      destAssetStr,
		SendMax:        sendMaxStr,
		Amount:         destAmountStr,
		Destination:    dest,
		Hops:           hops,
		Summary:        summary,
		Explanation:    explanation,
		Hint:           hint,
	}
}

// DiagnosePathPaymentStrictSend diagnoses a strict send path payment failure,
// detailing intermediate conversion hops and slippage delivering below destMin.
func DiagnosePathPaymentStrictSend(op xdr.PathPaymentStrictSendOp, opIndex int, code string) *PathPaymentSlippageDiagnosis {
	hops := buildPathPaymentHops(op.SendAsset, op.Path, op.DestAsset)
	dest := shortAddr(addressOf(op.Destination))
	sendAssetStr := assetLabel(op.SendAsset)
	destAssetStr := assetLabel(op.DestAsset)
	destMinStr := amount.String(op.DestMin)
	sendAmountStr := amount.String(op.SendAmount)

	hopCount := len(hops)
	var explanation, summary, hint string

	switch {
	case strings.Contains(code, "under_dest_min") || strings.Contains(code, "under_destmin") || strings.Contains(strings.ToLower(code), "underdestmin"):
		summary = fmt.Sprintf("Path payment failed: delivered amount fell below destMin limit of %s %s",
			destMinStr, destAssetStr)
		explanation = fmt.Sprintf("Order book liquidity shifted across %d intermediate trade hop(s) between path quotation and execution. Fluctuation in order book offer prices degraded the cumulative conversion rate, delivering less than the required destMin (%s %s).",
			hopCount, destMinStr, destAssetStr)
		hint = "Market price moved against you on the DEX. Lower destMin to accept higher slippage, or re-quote path liquidity before resubmitting."
	case strings.Contains(code, "too_few_offers") || strings.Contains(code, "TooFewOffers"):
		summary = fmt.Sprintf("Path payment failed: insufficient order book depth across %d hop(s)", hopCount)
		explanation = fmt.Sprintf("One or more order books along the path (%s -> %s) lacked sufficient bid/ask offer depth to convert %s %s.",
			sendAssetStr, destAssetStr, sendAmountStr, sendAssetStr)
		hint = "Liquidity was depleted between path computation and submission. Check liquidity on intermediate pairs or split the payment into smaller amounts."
	default:
		return nil
	}

	return &PathPaymentSlippageDiagnosis{
		OperationIndex: opIndex,
		PaymentType:    "strict_send",
		SendAsset:      sendAssetStr,
		DestAsset:      destAssetStr,
		DestMin:        destMinStr,
		Amount:         sendAmountStr,
		Destination:    dest,
		Hops:           hops,
		Summary:        summary,
		Explanation:    explanation,
		Hint:           hint,
	}
}

// DiagnosePathPaymentSlippage extracts and diagnoses a path payment slippage failure
// from an operation and its corresponding operation result.
func DiagnosePathPaymentSlippage(op xdr.Operation, opIndex int, opRes xdr.OperationResult) *PathPaymentSlippageDiagnosis {
	reason := explainOperationResult(opRes)
	switch op.Body.Type {
	case xdr.OperationTypePathPaymentStrictReceive:
		if pOp, ok := op.Body.GetPathPaymentStrictReceiveOp(); ok {
			return DiagnosePathPaymentStrictReceive(pOp, opIndex, reason.Code)
		}
	case xdr.OperationTypePathPaymentStrictSend:
		if pOp, ok := op.Body.GetPathPaymentStrictSendOp(); ok {
			return DiagnosePathPaymentStrictSend(pOp, opIndex, reason.Code)
		}
	}
	return nil
}

// SetTrustLineFlagsDiagnosis provides explicit diagnosis for SetTrustLineFlags failures,
// specifically detailing revocation violations and AUTH_REVOCABLE_FLAG issuer requirements.
type SetTrustLineFlagsDiagnosis struct {
	OperationIndex int      `json:"operationIndex"`
	Asset          string   `json:"asset"`
	Trustor        string   `json:"trustor"`
	Action         string   `json:"action"` // e.g. "revoke_authorization", "set_flags", "clear_flags"
	SetFlags       []string `json:"setFlags,omitempty"`
	ClearFlags     []string `json:"clearFlags,omitempty"`
	RevokingAuth   bool     `json:"revokingAuth"`
	Summary        string   `json:"summary"`
	Explanation    string   `json:"explanation"`
	Hint           string   `json:"hint"`
}

func (d *SetTrustLineFlagsDiagnosis) String() string {
	if d == nil {
		return "<nil>"
	}
	return d.Summary
}

func (d *SetTrustLineFlagsDiagnosis) DiagnosticReport() string {
	if d == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "SetTrustLineFlags failure:\n")
	fmt.Fprintf(&b, "  Operation:       %d\n", d.OperationIndex)
	fmt.Fprintf(&b, "  Asset:           %s\n", d.Asset)
	fmt.Fprintf(&b, "  Trustor:         %s\n", d.Trustor)
	fmt.Fprintf(&b, "  Action:          %s\n", d.Action)
	if len(d.SetFlags) > 0 {
		fmt.Fprintf(&b, "  Flags to set:    %s\n", strings.Join(d.SetFlags, ", "))
	}
	if len(d.ClearFlags) > 0 {
		fmt.Fprintf(&b, "  Flags to clear:  %s\n", strings.Join(d.ClearFlags, ", "))
	}
	fmt.Fprintf(&b, "  Explanation:     %s\n", d.Explanation)
	fmt.Fprintf(&b, "  Hint:            %s\n", d.Hint)
	return b.String()
}

// DiagnoseSetTrustLineFlags diagnoses a SetTrustLineFlags operation failure,
// detailing revocation violations and issuer AUTH_REVOCABLE_FLAG requirements.
func DiagnoseSetTrustLineFlags(op xdr.SetTrustLineFlagsOp, opIndex int, code string) *SetTrustLineFlagsDiagnosis {
	assetStr := assetLabel(op.Asset)
	trustorStr := op.Trustor.Address()

	setNames := trustLineFlagNames(op.SetFlags)
	clearNames := trustLineFlagNames(op.ClearFlags)
	revokingAuth := op.ClearFlags&xdr.Uint32(xdr.TrustLineFlagsAuthorizedFlag) != 0 ||
		op.ClearFlags&xdr.Uint32(xdr.TrustLineFlagsAuthorizedToMaintainLiabilitiesFlag) != 0

	action := "modify_flags"
	if revokingAuth {
		action = "revoke_authorization"
	} else if len(setNames) > 0 && len(clearNames) == 0 {
		action = "set_flags"
	} else if len(clearNames) > 0 && len(setNames) == 0 {
		action = "clear_flags"
	}

	var summary, explanation, hint string

	switch {
	case strings.Contains(code, "success") || strings.Contains(code, "Success"):
		return nil
	case strings.Contains(code, "cant_revoke") || strings.Contains(code, "CantRevoke"):
		summary = fmt.Sprintf("Cannot revoke trustline authorization for %s on %s: issuer lacks AUTH_REVOCABLE_FLAG permission",
			shortAddr(trustorStr), assetStr)
		explanation = "The asset issuer attempted to clear the authorized flag on a trustline, but the issuer account does not have AUTH_REVOCABLE_FLAG (flag 0x2) enabled. On Stellar, trustline authorization is irrevocable by default; an issuer must enable AUTH_REVOCABLE_FLAG on their account prior to issuing the asset to retain the ability to revoke trustlines."
		hint = "To revoke authorization on trustlines, the asset issuer account must have AUTH_REVOCABLE_FLAG set via SetOptionsOp. If the issuer account does not have AUTH_REVOCABLE_FLAG, existing trustlines cannot be revoked."
	case strings.Contains(code, "no_trust_line") || strings.Contains(code, "NoTrustLine"):
		summary = fmt.Sprintf("Trustline does not exist for %s on asset %s", shortAddr(trustorStr), assetStr)
		explanation = fmt.Sprintf("The target account %s has not established a trustline for %s. Flags cannot be modified on a non-existent trustline.",
			shortAddr(trustorStr), assetStr)
		hint = "The trustor account must first submit a ChangeTrust operation creating the trustline before the issuer can configure trustline flags."
	case strings.Contains(code, "malformed") || strings.Contains(code, "Malformed"):
		summary = fmt.Sprintf("SetTrustLineFlags operation is malformed for asset %s", assetStr)
		explanation = "The SetTrustLineFlags operation was rejected as structurally invalid. Common causes include attempting to set flags on native XLM (which does not use trustlines), specifying an invalid asset, or submitting from an account that is not the asset's issuer."
		hint = "Verify that the asset is a valid issued credit asset (not XLM) and that the operation source matches the asset issuer account."
	case strings.Contains(code, "invalid_state") || strings.Contains(code, "InvalidState"):
		summary = "Invalid trustline flags state combination"
		explanation = "The requested set and clear flags produce an invalid or conflicting state (e.g. attempting to set and clear the same flag simultaneously)."
		hint = "Ensure setFlags and clearFlags bitmasks do not overlap and specify a valid flag transition."
	case strings.Contains(code, "low_reserve") || strings.Contains(code, "LowReserve"):
		summary = "Insufficient reserve for trustline flag change"
		explanation = "The operation failed because the account balance is insufficient to satisfy base reserve requirements for the updated trustline state."
		hint = "Ensure the account holds sufficient XLM to cover base reserves before modifying trustline flags."
	default:
		summary = fmt.Sprintf("SetTrustLineFlags failed with %s", code)
		explanation = fmt.Sprintf("Flag change (set: [%s], clear: [%s]) failed on trustline for %s.",
			strings.Join(setNames, ","), strings.Join(clearNames, ","), assetStr)
		hint = "Check trustline existence, issuer permissions, and flag parameters."
	}

	return &SetTrustLineFlagsDiagnosis{
		OperationIndex: opIndex,
		Asset:          assetStr,
		Trustor:        trustorStr,
		Action:         action,
		SetFlags:       setNames,
		ClearFlags:     clearNames,
		RevokingAuth:   revokingAuth,
		Summary:        summary,
		Explanation:    explanation,
		Hint:           hint,
	}
}

// DiagnoseSetTrustLineFlagsOperation extracts and diagnoses a SetTrustLineFlags failure
// from an operation and its corresponding operation result.
func DiagnoseSetTrustLineFlagsOperation(op xdr.Operation, opIndex int, opRes xdr.OperationResult) *SetTrustLineFlagsDiagnosis {
	if op.Body.Type != xdr.OperationTypeSetTrustLineFlags {
		return nil
	}
	sOp, ok := op.Body.GetSetTrustLineFlagsOp()
	if !ok {
		return nil
	}
	reason := explainOperationResult(opRes)
	return DiagnoseSetTrustLineFlags(sOp, opIndex, reason.Code)
}

// deepenAccountMergeFailure rewrites the summary and hint for the
// AccountMerge failures whose remedy can be stated precisely without any
// ledger access. Code and Constant are never touched.
//
// The guidance mirrors what stellar-core actually enforces in
// MergeOpFrame::doApply, in the order core checks it:
//
//   - account_merge_has_sub_entries: core rejects the merge whenever the
//     source account's subentry count differs from its signer count, so any
//     non-signer subentry blocks the merge. Signers themselves do not — core
//     removes them as part of a successful merge.
//   - account_merge_seqnum_too_far: core refuses to merge an account whose
//     sequence number has reached the closing ledger's starting sequence
//     (ledgerSeq << 32), because recreating the account later would let its
//     sequence number jump backwards.
//   - account_merge_dest_full: core adds the source's whole XLM balance to
//     the destination in one step, which fails when the destination's lumen
//     buying liabilities leave no room for it.
//   - account_merge_immutable_set: core refuses to merge an account with
//     AUTH_IMMUTABLE set, and that flag can never be cleared afterwards.
//
// The destination address is the only fact read from the operation body;
// everything else is protocol semantics. No network calls, deterministic
// output.
func deepenAccountMergeFailure(source string, op xdr.Operation, r Reason) Reason {
	if r.Success {
		return r
	}

	merger := "the source account"
	if source != "" {
		merger = shortAddr(source)
	}
	dest := "the destination"
	if op.Body.Destination != nil {
		if addr := addressOf(*op.Body.Destination); addr != "" {
			dest = shortAddr(addr)
		}
	}

	switch r.Code {
	case "account_merge_has_sub_entries":
		r.Summary = fmt.Sprintf(
			"The merging account %s still owns non-signer subentries, and the network deletes an account only when its subentry count matches its signer count.",
			merger)
		r.Hint = "Every active trustline (including liquidity-pool share trustlines), open DEX offer, data entry, and claimable balance the account created must be removed first. Close them in this order: cancel each open offer with ManageSellOffer or ManageBuyOffer at amount 0; send away or sell each asset balance, withdraw any liquidity pool positions, then remove the trustline by setting its ChangeTrust limit to 0; delete each data entry with a ManageData operation that carries no value. Signers do not block a merge — the network removes them automatically. Resubmit the merge once the account's remaining subentries are only signers."
	case "account_merge_seqnum_too_far":
		r.Summary = fmt.Sprintf(
			"The merging account %s holds a sequence number at or above the starting sequence of the closing ledger (ledger sequence << 32), so merging it now would let a recreated account jump backwards.",
			merger)
		r.Hint = "An account's sequence number can never be lowered. If a BumpSequence pushed it past the current ledger sequence times 2^32, the account can no longer be merged: move its funds out with payments instead. Otherwise, confirm the ledger you compared against belongs to the same network this transaction targets."
	case "account_merge_dest_full":
		r.Summary = fmt.Sprintf(
			"Destination %s cannot receive the merging account's full XLM balance: the transfer would push it past the maximum balance its lumen buying liabilities allow.",
			dest)
		r.Hint = "The merge moves the entire balance and cannot send a lesser amount. Cancel or shrink the destination's open offers that buy XLM until its lumen buying liabilities leave room for the incoming balance, then resubmit the merge — or merge into a different destination."
	case "account_merge_immutable_set":
		r.Summary = fmt.Sprintf(
			"The merging account %s has the AUTH_IMMUTABLE flag set, and the network refuses to merge an immutable account.",
			merger)
		r.Hint = "AUTH_IMMUTABLE is permanent: once set it can never be cleared, so this account can never be merged, and its flags, signers and thresholds can never change again. Move its funds out with ordinary payments instead."
	}
	return r
}
