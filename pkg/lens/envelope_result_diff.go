package lens

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/stellar/go-stellar-sdk/xdr"
)

// Operation status values reported by an envelope/result alignment.
const (
	// OperationStatusSuccess means the operation ran and succeeded.
	OperationStatusSuccess = "success"
	// OperationStatusFailed means the operation ran and failed.
	OperationStatusFailed = "failed"
	// OperationStatusNotRun means the transaction was rejected before any
	// operation executed, so no operation result exists to compare.
	OperationStatusNotRun = "not_run"
	// OperationStatusMissingResult means the result carried some operation
	// results but not one for this envelope operation.
	OperationStatusMissingResult = "missing_result"
	// OperationStatusUnpairedResult means the result carried an operation
	// result with no matching envelope operation.
	OperationStatusUnpairedResult = "unpaired_result"
)

// OperationResultDiff aligns one transaction operation with its corresponding
// operation result.
type OperationResultDiff struct {
	// Index is the zero-based operation position.
	Index int `json:"index"`
	// Type is the snake_case operation name, e.g. "invoke_host_function".
	Type string `json:"type"`
	// Source is the operation-level source override, when present.
	Source string `json:"source,omitempty"`
	// Detail describes what the operation was attempting.
	Detail string `json:"detail,omitempty"`
	// Status is one of the OperationStatus constants.
	Status string `json:"status"`
	// Success reports whether the operation succeeded.
	Success bool `json:"success"`
	// ResultCode is the canonical short result code, e.g. "payment_underfunded".
	ResultCode string `json:"resultCode,omitempty"`
	// ResultConstant is the generated XDR constant, when available.
	ResultConstant string `json:"resultConstant,omitempty"`
	// ResultSummary explains the result code in plain English.
	ResultSummary string `json:"resultSummary,omitempty"`
	// ResultHint suggests what to do about a failure.
	ResultHint string `json:"resultHint,omitempty"`
	// Mismatched reports that the result arm did not correspond to the
	// operation type, which usually means the two blobs are not a pair.
	Mismatched bool `json:"mismatched,omitempty"`
	// Changes lists the alignment differences for this operation.
	Changes []Change `json:"-"`
}

// EnvelopeResultDiff is an aligned, two-way comparison of a transaction
// envelope and its result: every operation is paired with the result entry at
// the same index, and mismatches, unpaired operations and unpaired results are
// reported rather than silently correlated.
type EnvelopeResultDiff struct {
	// LeftType and RightType are the compared XDR type names.
	LeftType  string `json:"leftType"`
	RightType string `json:"rightType"`
	// TransactionCode is the short transaction-level result code.
	TransactionCode string `json:"transactionCode"`
	// TransactionConstant is the generated transaction result constant.
	TransactionConstant string `json:"transactionConstant,omitempty"`
	// TransactionSummary explains the transaction-level code.
	TransactionSummary string `json:"transactionSummary"`
	// TransactionHint suggests what to do about a transaction-level failure.
	TransactionHint string `json:"transactionHint,omitempty"`
	// InnerTransactionCode is the inner result code for a fee bump.
	InnerTransactionCode string `json:"innerTransactionCode,omitempty"`
	// Success reports whether the transaction as a whole succeeded.
	Success bool `json:"success"`
	// OperationsRun reports whether the transaction reached operation
	// execution, i.e. whether operation results exist to compare.
	OperationsRun bool `json:"operationsRun"`
	// Operations is the aligned list, one entry per operation index.
	Operations []OperationResultDiff `json:"operations"`
	// UnpairedOperations lists operation indexes with no result entry.
	UnpairedOperations []int `json:"unpairedOperations,omitempty"`
	// UnpairedResults lists result indexes with no envelope operation.
	UnpairedResults []int `json:"unpairedResults,omitempty"`
	// FailedOperations lists operation indexes that ran and failed.
	FailedOperations []int `json:"failedOperations,omitempty"`
	// Changes aggregates every alignment difference as a structural diff.
	Changes []Change `json:"-"`

	envelope   *xdr.TransactionEnvelope
	result     *xdr.TransactionResult
	operations []xdr.Operation
	opResults  []xdr.OperationResult
	hasResults bool
}

// DiffEnvelopeResult aligns a transaction envelope with its result. It is the
// pairwise counterpart to ExplainPair: where ExplainPair answers "why did this
// fail", DiffEnvelopeResult lays the operations and results out side by side
// and reports every place they disagree.
func DiffEnvelopeResult(envelope *xdr.TransactionEnvelope, result *xdr.TransactionResult) (*EnvelopeResultDiff, error) {
	if envelope == nil || result == nil {
		return nil, fmt.Errorf("%w: both a TransactionEnvelope and a TransactionResult are required", ErrInvalidArgument)
	}

	code, err := resultCodeName(result)
	if err != nil {
		return nil, err
	}
	reason := ExplainCode(code)

	diff := &EnvelopeResultDiff{
		LeftType:            "TransactionEnvelope",
		RightType:           "TransactionResult",
		TransactionCode:     reason.Code,
		TransactionConstant: reason.Constant,
		TransactionSummary:  reason.Summary,
		TransactionHint:     reason.Hint,
		Success:             result.Successful(),
		envelope:            envelope,
		result:              result,
		operations:          envelope.Operations(),
	}
	if inner, ok := innerResultCodeName(result); ok {
		diff.InnerTransactionCode = ExplainCode(inner).Code
	}

	// A fee bump reports the inner transaction's operation results; a classic
	// result reports its own. OperationResults normalises both.
	diff.opResults, diff.hasResults = result.OperationResults()
	diff.OperationsRun = diff.hasResults

	total := len(diff.operations)
	if diff.hasResults && len(diff.opResults) > total {
		total = len(diff.opResults)
	}
	for i := 0; i < total; i++ {
		pair := diff.alignOperation(i)
		diff.Operations = append(diff.Operations, pair)
		diff.Changes = append(diff.Changes, pair.Changes...)
		switch pair.Status {
		case OperationStatusFailed:
			diff.FailedOperations = append(diff.FailedOperations, i)
		case OperationStatusMissingResult:
			diff.UnpairedOperations = append(diff.UnpairedOperations, i)
		case OperationStatusUnpairedResult:
			diff.UnpairedResults = append(diff.UnpairedResults, i)
		}
	}
	return diff, nil
}

// alignOperation builds the paired entry for one index.
func (d *EnvelopeResultDiff) alignOperation(index int) OperationResultDiff {
	pair := OperationResultDiff{Index: index}
	path := "Operations[" + strconv.Itoa(index) + "]"

	hasOperation := index < len(d.operations)
	if hasOperation {
		op := d.operations[index]
		pair.Type = operationTypeLabel(op.Body.Type)
		pair.Source = operationSource(op)
		pair.Detail = describeOperation(op)
	}

	hasResult := d.hasResults && index < len(d.opResults)
	if !hasResult {
		if hasOperation {
			if d.hasResults {
				// The transaction produced results, but not for this operation.
				pair.Status = OperationStatusMissingResult
				pair.Changes = append(pair.Changes, Change{Path: path, Op: ChangeRemoved, Before: pair.Type})
			} else {
				// The transaction never reached execution.
				pair.Status = OperationStatusNotRun
			}
		} else {
			pair.Status = OperationStatusUnpairedResult
		}
		return pair
	}

	reason := explainOperationResult(d.opResults[index])
	pair.ResultCode = reason.Code
	pair.ResultConstant = reason.Constant
	pair.ResultSummary = reason.Summary
	pair.ResultHint = reason.Hint
	pair.Success = reason.Success

	if !hasOperation {
		pair.Type = "unknown"
		pair.Status = OperationStatusUnpairedResult
		pair.Changes = append(pair.Changes, Change{Path: path, Op: ChangeAdded, After: reason.Code})
		return pair
	}

	arm := operationResultArmType(d.opResults[index])
	if arm != "" && arm != pair.Type {
		pair.Mismatched = true
		pair.Changes = append(pair.Changes, Change{
			Path:   path,
			Op:     ChangeTypeChanged,
			Before: pair.Type,
			After:  arm,
		})
	}

	if reason.Success {
		pair.Status = OperationStatusSuccess
	} else {
		pair.Status = OperationStatusFailed
		pair.Changes = append(pair.Changes, Change{
			Path:   path + ".Result",
			Op:     ChangeModified,
			Before: pair.Type,
			After:  reason.Code,
		})
	}
	return pair
}

// operationResultArmType returns the snake_case operation name of the arm a
// result selected, or "" when the operation did not run.
func operationResultArmType(opRes xdr.OperationResult) string {
	if opRes.Code != xdr.OperationResultCodeOpInner || opRes.Tr == nil {
		return ""
	}
	return operationTypeLabel(opRes.Tr.Type)
}

// DiffEnvelopeResultValues is the Value-based wrapper around
// DiffEnvelopeResult: envelopeVal must wrap *xdr.TransactionEnvelope and
// resultVal must wrap *xdr.TransactionResult.
func DiffEnvelopeResultValues(envelopeVal, resultVal *Value) (*EnvelopeResultDiff, error) {
	if envelopeVal == nil || resultVal == nil {
		return nil, fmt.Errorf("%w: envelope and result values are required", ErrInvalidArgument)
	}
	env, ok := envelopeVal.Raw.(*xdr.TransactionEnvelope)
	if !ok {
		return nil, fmt.Errorf("%w: first value is %s, want TransactionEnvelope", ErrInvalidArgument, envelopeVal.Type)
	}
	result, ok := resultVal.Raw.(*xdr.TransactionResult)
	if !ok {
		return nil, fmt.Errorf("%w: second value is %s, want TransactionResult", ErrInvalidArgument, resultVal.Type)
	}
	return DiffEnvelopeResult(env, result)
}

// DiffEnvelopeResultBase64 decodes both payloads and aligns them. Each side is
// decoded independently, so a type mismatch is reported as an error rather
// than a misleading alignment.
func DiffEnvelopeResultBase64(envelopePayload, resultPayload string) (*EnvelopeResultDiff, error) {
	envelopeVal, err := Decode(envelopePayload)
	if err != nil {
		return nil, fmt.Errorf("decoding envelope: %w", err)
	}
	resultVal, err := Decode(resultPayload)
	if err != nil {
		return nil, fmt.Errorf("decoding result: %w", err)
	}
	return DiffEnvelopeResultValues(envelopeVal, resultVal)
}

// DiffResult renders the alignment as a path-level DiffResult, so the standard
// diff formatter, statistics and equality helpers apply.
func (d *EnvelopeResultDiff) DiffResult() *DiffResult {
	if d == nil {
		return nil
	}
	changes := make([]Change, len(d.Changes))
	copy(changes, d.Changes)
	return &DiffResult{LeftType: d.LeftType, RightType: d.RightType, Changes: changes}
}

// Summarize returns the statistical summary of the alignment differences.
func (d *EnvelopeResultDiff) Summarize() DiffStats {
	return SummarizeDiff(d.DiffResult())
}

// Summary renders a one-line account of the alignment.
func (d *EnvelopeResultDiff) Summary() string {
	if d == nil {
		return ""
	}
	if d.Success {
		return fmt.Sprintf("Transaction succeeded: all %d operation(s) matched their results.", len(d.Operations))
	}
	if len(d.FailedOperations) > 0 {
		index := d.FailedOperations[0]
		if index < len(d.Operations) {
			pair := d.Operations[index]
			detail := ""
			if pair.Detail != "" {
				detail = " (" + pair.Detail + ")"
			}
			return fmt.Sprintf("Transaction failed at operation %d, %s%s: %s",
				index, pair.Type, detail, pair.ResultSummary)
		}
	}
	if len(d.Operations) > 0 && !d.OperationsRun {
		return "Transaction failed before any operation ran: " + d.TransactionSummary
	}
	return "Transaction failed: " + d.TransactionSummary
}

// String renders a readable, deterministic report of the alignment.
func (d *EnvelopeResultDiff) String() string {
	if d == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", d.TransactionCode)
	fmt.Fprintf(&b, "  %s\n", d.TransactionSummary)
	if d.InnerTransactionCode != "" {
		fmt.Fprintf(&b, "  inner: %s\n", d.InnerTransactionCode)
	}
	fmt.Fprintf(&b, "Operations (%d)\n", len(d.Operations))
	for _, pair := range d.Operations {
		marker := "·"
		switch pair.Status {
		case OperationStatusSuccess:
			marker = "✓"
		case OperationStatusFailed:
			marker = "✗"
		}
		line := fmt.Sprintf("  %s [%d] %s", marker, pair.Index, pair.Type)
		if pair.Detail != "" {
			line += " — " + pair.Detail
		}
		b.WriteString(line + "\n")
		switch pair.Status {
		case OperationStatusFailed:
			fmt.Fprintf(&b, "      %s: %s\n", pair.ResultCode, pair.ResultSummary)
			if pair.ResultHint != "" {
				fmt.Fprintf(&b, "      → %s\n", pair.ResultHint)
			}
		case OperationStatusMissingResult:
			b.WriteString("      no result entry for this operation\n")
		case OperationStatusUnpairedResult:
			fmt.Fprintf(&b, "      result with no matching operation: %s\n", pair.ResultCode)
		case OperationStatusNotRun:
			b.WriteString("      did not run: the transaction was rejected before execution\n")
		}
	}
	return b.String()
}

// Tree renders the alignment as an aligned two-way Node tree: a root carrying
// the transaction verdict plus the operation list, where each element holds
// the operation's own subtree next to its result's subtree.
func (d *EnvelopeResultDiff) Tree() *Node {
	if d == nil {
		return nil
	}
	root := &Node{Kind: KindStruct, TypeName: "EnvelopeResultDiff"}
	root.Children = append(root.Children,
		&Node{Name: "TransactionCode", Kind: KindScalar, Value: d.TransactionCode, Note: d.TransactionSummary},
		&Node{Name: "Success", Kind: KindScalar, Value: d.Success},
	)
	operations := &Node{Name: "Operations", Kind: KindList}
	for i, pair := range d.Operations {
		entry := &Node{Kind: KindStruct, TypeName: "OperationResultDiff"}
		entry.Children = append(entry.Children,
			&Node{Name: "Index", Kind: KindScalar, Value: pair.Index},
			&Node{Name: "Type", Kind: KindScalar, Value: pair.Type},
			&Node{Name: "Status", Kind: KindScalar, Value: pair.Status},
		)
		if pair.Detail != "" {
			entry.Children = append(entry.Children, &Node{Name: "Detail", Kind: KindScalar, Value: pair.Detail})
		}
		if operation := d.operationNode(i); operation != nil {
			entry.Children = append(entry.Children, operation)
		}
		if result := d.resultNode(i); result != nil {
			entry.Children = append(entry.Children, result)
		}
		operations.Children = append(operations.Children, entry)
	}
	root.Children = append(root.Children, operations)
	return root
}

func (d *EnvelopeResultDiff) operationNode(index int) *Node {
	if index < 0 || index >= len(d.operations) {
		return nil
	}
	return nodeFor("Operation", d.operations[index])
}

func (d *EnvelopeResultDiff) resultNode(index int) *Node {
	if !d.hasResults || index < 0 || index >= len(d.opResults) {
		return nil
	}
	return nodeFor("Result", d.opResults[index])
}
