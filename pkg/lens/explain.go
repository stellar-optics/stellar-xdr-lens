package lens

import (
	"fmt"
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
// it returns an error, since a general-purpose "explanation" of an arbitrary
// XDR struct would be no better than the decoded tree.
func Explain(v *Value) (*Summary, error) {
	if v == nil {
		return nil, fmt.Errorf("nil value")
	}
	switch raw := v.Raw.(type) {
	case *xdr.TransactionEnvelope:
		return explainEnvelope(raw, nil)
	case *xdr.TransactionResult:
		return explainResult(raw)
	default:
		return nil, fmt.Errorf("cannot explain %s; explain supports TransactionEnvelope and TransactionResult", v.Type)
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
		return nil, fmt.Errorf("both an envelope and a result are required")
	}
	env, ok := envelope.Raw.(*xdr.TransactionEnvelope)
	if !ok {
		return nil, fmt.Errorf("first value must be a TransactionEnvelope, got %s", envelope.Type)
	}
	res, ok := result.Raw.(*xdr.TransactionResult)
	if !ok {
		return nil, fmt.Errorf("second value must be a TransactionResult, got %s", result.Type)
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

	for i, op := range env.Operations() {
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
	if err := attachResult(s, res); err != nil {
		return nil, err
	}
	return s, nil
}

// explainResult builds a Summary from a result alone. Without the envelope
// there is nothing to say about what each operation was attempting, so only
// the outcome is populated.
func explainResult(res *xdr.TransactionResult) (*Summary, error) {
	s := &Summary{Kind: "transaction_result"}
	if err := attachResult(s, res); err != nil {
		return nil, err
	}
	return s, nil
}

// attachResult resolves a TransactionResult into an Outcome and pairs each
// operation result with its operation.
func attachResult(s *Summary, res *xdr.TransactionResult) error {
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
			if !reason.Success {
				outcome.FailedOps = append(outcome.FailedOps, i)
			}
			// Attach to the matching operation when the envelope was given.
			if i < len(s.Operations) {
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

// resultCodeName returns the transaction-level result code constant name.
func resultCodeName(res *xdr.TransactionResult) (string, error) {
	name := safeString(res.Result.Code)
	if name == "" {
		return "", fmt.Errorf("could not read transaction result code")
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
		return fmt.Sprintf("send at most %s %s to deliver exactly %s %s to %s",
			amount.String(o.SendMax), assetLabel(o.SendAsset),
			amount.String(o.DestAmount), assetLabel(o.DestAsset), shortAddr(addressOf(o.Destination)))

	case xdr.OperationTypePathPaymentStrictSend:
		o, ok := body.GetPathPaymentStrictSendOp()
		if !ok {
			break
		}
		return fmt.Sprintf("send exactly %s %s to deliver at least %s %s to %s",
			amount.String(o.SendAmount), assetLabel(o.SendAsset),
			amount.String(o.DestMin), assetLabel(o.DestAsset), shortAddr(addressOf(o.Destination)))

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
		dest, ok := body.GetDestination()
		if !ok {
			break
		}
		return "merge this account into " + shortAddr(addressOf(dest))

	case xdr.OperationTypeBumpSequence:
		o, ok := body.GetBumpSequenceOp()
		if !ok {
			break
		}
		return fmt.Sprintf("bump sequence number to %d", int64(o.BumpTo))

	case xdr.OperationTypeManageData:
		o, ok := body.GetManageDataOp()
		if !ok {
			break
		}
		if o.DataValue == nil {
			return fmt.Sprintf("delete data entry %q", string(o.DataName))
		}
		return fmt.Sprintf("set data entry %q (%d bytes)", string(o.DataName), len(*o.DataValue))

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
