package lens

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/stellar/go-stellar-sdk/strkey"
	"github.com/stellar/go-stellar-sdk/xdr"
)

// DiagnosticDetail represents a parsed diagnostic event for diagnostic reports.
type DiagnosticDetail struct {
	// InSuccessfulCall indicates whether this event occurred in a successful call branch.
	InSuccessfulCall bool `json:"inSuccessfulCall"`
	// ContractID is the strkey (C...) address of the emitting contract, if present.
	ContractID string `json:"contractId,omitempty"`
	// Type is the event type name (system, contract, diagnostic).
	Type string `json:"type"`
	// Topics is the human-readable string representation of event topics.
	Topics []string `json:"topics,omitempty"`
	// Data is the human-readable string representation of event data.
	Data string `json:"data,omitempty"`
}

// TrappedReason encapsulates the plain-English diagnosis of a trapped host function
// invocation correlated with Soroban diagnostic events.
type TrappedReason struct {
	// Trapped reports true if a contract trap or panic was identified.
	Trapped bool `json:"trapped"`
	// PanicMessage is the human-readable panic text extracted from diagnostic events.
	PanicMessage string `json:"panicMessage,omitempty"`
	// ErrorType is the Soroban error category (e.g. "Contract execution error", "WebAssembly VM runtime error").
	ErrorType string `json:"errorType,omitempty"`
	// ErrorCode is the canonical error code string or custom numeric code.
	ErrorCode string `json:"errorCode,omitempty"`
	// ContractID is the C... strkey address or hex identifier of the trapped contract.
	ContractID string `json:"contractId,omitempty"`
	// FunctionName is the contract function being executed during the trap, if known.
	FunctionName string `json:"functionName,omitempty"`
	// DiagnosticTrace contains chronological details of diagnostic events leading to the trap.
	DiagnosticTrace []DiagnosticDetail `json:"diagnosticTrace,omitempty"`
	// Summary explains what caused the trap in plain English.
	Summary string `json:"summary"`
	// Hint suggests remediation steps for the developer.
	Hint string `json:"hint,omitempty"`
}

// DecodeTrapReason inspects a slice of Soroban DiagnosticEvents to extract the
// root cause, panic message, contract ID, and diagnostic trace of a trapped contract.
func DecodeTrapReason(events []xdr.DiagnosticEvent) (*TrappedReason, error) {
	if len(events) == 0 {
		return &TrappedReason{
			Trapped: false,
			Summary: "No diagnostic events provided; unable to extract trap reason.",
		}, nil
	}

	reason := &TrappedReason{
		Trapped: true,
	}

	trace := make([]DiagnosticDetail, 0, len(events))
	var lastPanicMsg string
	var lastContractID string
	var lastFnName string
	var scErr *xdr.ScError

	for _, diag := range events {
		detail := parseDiagnosticDetail(diag)
		trace = append(trace, detail)

		// Focus especially on events in failed execution branches
		if detail.ContractID != "" {
			lastContractID = detail.ContractID
		}

		topics := detail.Topics
		if len(topics) > 0 {
			switch topics[0] {
			case "fn_call":
				if len(topics) >= 3 {
					lastFnName = topics[2]
				} else if len(topics) >= 2 {
					lastFnName = topics[1]
				}
			case "log", "panic", "error":
				if detail.Data != "" {
					lastPanicMsg = detail.Data
				}
			}
		}

		// Check if event data contains an ScError or panic message
		if diag.Event.Body.V == 0 && diag.Event.Body.V0 != nil {
			v0 := diag.Event.Body.V0
			if v0.Data.Type == xdr.ScValTypeScvError {
				errVal := v0.Data.MustError()
				scErr = &errVal
			}
			// If data is a string or symbol with panic content
			if lastPanicMsg == "" {
				switch v0.Data.Type {
				case xdr.ScValTypeScvString:
					if str, ok := v0.Data.GetStr(); ok && len(str) > 0 {
						lastPanicMsg = string(str)
					}
				case xdr.ScValTypeScvSymbol:
					if sym, ok := v0.Data.GetSym(); ok && len(sym) > 0 {
						symStr := string(sym)
						if strings.Contains(symStr, "panic") || strings.Contains(symStr, "error") {
							lastPanicMsg = symStr
						}
					}
				}
			}
		}
	}

	reason.DiagnosticTrace = trace
	reason.ContractID = lastContractID
	reason.FunctionName = lastFnName
	reason.PanicMessage = lastPanicMsg

	if scErr != nil {
		explainedErr := ExplainScError(*scErr)
		reason.ErrorCode = explainedErr.Code
		reason.ErrorType = ExplainHostErrorType(scErr.Type)
		if reason.PanicMessage != "" {
			reason.Summary = fmt.Sprintf("Contract execution trapped in %s: %s (panic: %q)",
				formatTarget(reason.ContractID, reason.FunctionName),
				explainedErr.Summary,
				reason.PanicMessage)
		} else {
			reason.Summary = fmt.Sprintf("Contract execution trapped in %s: %s",
				formatTarget(reason.ContractID, reason.FunctionName),
				explainedErr.Summary)
		}
		reason.Hint = explainedErr.Hint
		if reason.Hint == "" {
			reason.Hint = "Inspect diagnostic events and simulation logs to adjust contract arguments or storage prerequisites."
		}
	} else if reason.PanicMessage != "" {
		reason.Summary = fmt.Sprintf("Contract panicked in %s: %s",
			formatTarget(reason.ContractID, reason.FunctionName),
			reason.PanicMessage)
		reason.Hint = "Check contract preconditions, assertions, and boundary checks."
	} else {
		reason.Summary = fmt.Sprintf("Contract execution trapped in %s: no explicit panic message found in diagnostic events.",
			formatTarget(reason.ContractID, reason.FunctionName))
		reason.Hint = "Re-run transaction simulation with verbose logging enabled to inspect host diagnostic events."
	}

	return reason, nil
}

func formatTarget(contractID, fnName string) string {
	if contractID != "" && fnName != "" {
		return fmt.Sprintf("%s::%s", shortContractID(contractID), fnName)
	}
	if contractID != "" {
		return fmt.Sprintf("contract %s", shortContractID(contractID))
	}
	if fnName != "" {
		return fmt.Sprintf("function %s", fnName)
	}
	return "host function"
}

func shortContractID(id string) string {
	if len(id) > 12 {
		return id[:6] + "..." + id[len(id)-4:]
	}
	return id
}

// parseDiagnosticDetail parses a raw DiagnosticEvent into a clean DiagnosticDetail.
func parseDiagnosticDetail(diag xdr.DiagnosticEvent) DiagnosticDetail {
	detail := DiagnosticDetail{
		InSuccessfulCall: diag.InSuccessfulContractCall,
		Type:             eventTypeName(diag.Event.Type),
	}

	if diag.Event.ContractId != nil {
		hash := [32]byte(*diag.Event.ContractId)
		if addr, err := strkey.Encode(strkey.VersionByteContract, hash[:]); err == nil {
			detail.ContractID = addr
		} else {
			detail.ContractID = hex.EncodeToString(hash[:])
		}
	}

	if diag.Event.Body.V == 0 && diag.Event.Body.V0 != nil {
		v0 := diag.Event.Body.V0
		for _, topic := range v0.Topics {
			detail.Topics = append(detail.Topics, ScValToString(topic))
		}
		detail.Data = ScValToString(v0.Data)
	}

	return detail
}

func eventTypeName(t xdr.ContractEventType) string {
	switch t {
	case xdr.ContractEventTypeSystem:
		return "system"
	case xdr.ContractEventTypeContract:
		return "contract"
	case xdr.ContractEventTypeDiagnostic:
		return "diagnostic"
	default:
		return "unknown"
	}
}

// ScValToString converts an ScVal into a concise, human-readable string representation.
func ScValToString(v xdr.ScVal) string {
	switch v.Type {
	case xdr.ScValTypeScvBool:
		if b, ok := v.GetB(); ok {
			return fmt.Sprintf("%t", b)
		}
	case xdr.ScValTypeScvVoid:
		return "void"
	case xdr.ScValTypeScvU32:
		if u, ok := v.GetU32(); ok {
			return fmt.Sprintf("%d", u)
		}
	case xdr.ScValTypeScvI32:
		if i, ok := v.GetI32(); ok {
			return fmt.Sprintf("%d", i)
		}
	case xdr.ScValTypeScvU64:
		if u, ok := v.GetU64(); ok {
			return fmt.Sprintf("%d", u)
		}
	case xdr.ScValTypeScvI64:
		if i, ok := v.GetI64(); ok {
			return fmt.Sprintf("%d", i)
		}
	case xdr.ScValTypeScvTimepoint:
		if t, ok := v.GetTimepoint(); ok {
			return fmt.Sprintf("timepoint(%d)", t)
		}
	case xdr.ScValTypeScvDuration:
		if d, ok := v.GetDuration(); ok {
			return fmt.Sprintf("duration(%d)", d)
		}
	case xdr.ScValTypeScvU128:
		if u, ok := v.GetU128(); ok {
			return fmt.Sprintf("u128(%d, %d)", u.Hi, u.Lo)
		}
	case xdr.ScValTypeScvI128:
		if i, ok := v.GetI128(); ok {
			return fmt.Sprintf("i128(%d, %d)", i.Hi, i.Lo)
		}
	case xdr.ScValTypeScvU256:
		return "u256(...)"
	case xdr.ScValTypeScvI256:
		return "i256(...)"
	case xdr.ScValTypeScvBytes:
		if b, ok := v.GetBytes(); ok {
			return string(b)
		}
	case xdr.ScValTypeScvString:
		if s, ok := v.GetStr(); ok {
			return string(s)
		}
	case xdr.ScValTypeScvSymbol:
		if s, ok := v.GetSym(); ok {
			return string(s)
		}
	case xdr.ScValTypeScvVec:
		if vec, ok := v.GetVec(); ok && vec != nil {
			var parts []string
			for _, item := range *vec {
				parts = append(parts, ScValToString(item))
			}
			return strings.Join(parts, " ")
		}
	case xdr.ScValTypeScvMap:
		return "{...}"
	case xdr.ScValTypeScvAddress:
		if addr, ok := v.GetAddress(); ok {
			return formatScAddress(addr)
		}
	case xdr.ScValTypeScvError:
		if err, ok := v.GetError(); ok {
			return ExplainScError(err).Summary
		}
	}
	return ""
}

func formatScAddress(addr xdr.ScAddress) string {
	switch addr.Type {
	case xdr.ScAddressTypeScAddressTypeAccount:
		if acc, ok := addr.GetAccountId(); ok {
			if id, ok := acc.GetEd25519(); ok {
				if s, err := strkey.Encode(strkey.VersionByteAccountID, id[:]); err == nil {
					return s
				}
			}
		}
	case xdr.ScAddressTypeScAddressTypeContract:
		if c, ok := addr.GetContractId(); ok {
			if s, err := strkey.Encode(strkey.VersionByteContract, c[:]); err == nil {
				return s
			}
		}
	}
	return "address"
}

// ExtractDiagnosticEvents retrieves all DiagnosticEvents embedded within a TransactionMeta.
func ExtractDiagnosticEvents(meta *xdr.TransactionMeta) []xdr.DiagnosticEvent {
	if meta == nil {
		return nil
	}

	switch meta.V {
	case 3:
		if meta.V3 != nil && meta.V3.SorobanMeta != nil {
			return meta.V3.SorobanMeta.DiagnosticEvents
		}
	case 4:
		// Protocol 23 moved diagnostic events out of SorobanMeta to the top
		// level of TransactionMetaV4.
		if meta.V4 != nil {
			return meta.V4.DiagnosticEvents
		}
	}
	return nil
}

// DiagnosticEventsFromValue returns the diagnostic events of a decoded
// TransactionMeta, or nil for any other value.
func DiagnosticEventsFromValue(meta *Value) []xdr.DiagnosticEvent {
	if meta == nil {
		return nil
	}
	m, ok := meta.Raw.(*xdr.TransactionMeta)
	if !ok {
		return nil
	}
	return ExtractDiagnosticEvents(m)
}

// DecodeTrapFromMeta extracts diagnostic events from a TransactionMeta and decodes
// the trapped host function reason.
func DecodeTrapFromMeta(meta *xdr.TransactionMeta) (*TrappedReason, error) {
	if meta == nil {
		return nil, fmt.Errorf("%w: cannot decode trap from nil TransactionMeta", ErrInvalidArgument)
	}
	events := ExtractDiagnosticEvents(meta)
	return DecodeTrapReason(events)
}

// CorrelateTrap inspects a TransactionResult and TransactionMeta to verify whether
// a host function trapped, and if so, correlates the failure with diagnostic events.
func CorrelateTrap(res *xdr.TransactionResult, meta *xdr.TransactionMeta) (*TrappedReason, error) {
	if res == nil {
		return nil, fmt.Errorf("%w: cannot correlate trap from nil TransactionResult", ErrInvalidArgument)
	}

	// Verify if transaction resulted in a trapped host function
	var trapped bool
	opResults, ok := res.OperationResults()
	if ok {
		for _, op := range opResults {
			if explainOperationResult(op).Code == "invoke_host_function_trapped" {
				trapped = true
				break
			}
		}
	}

	// Check fee-bump inner pair
	if !trapped && res.Result.Code == xdr.TransactionResultCodeTxFeeBumpInnerFailed {
		if pair, ok := res.Result.GetInnerResultPair(); ok {
			if pair.Result.Result.Results != nil {
				for _, op := range *pair.Result.Result.Results {
					if explainOperationResult(op).Code == "invoke_host_function_trapped" {
						trapped = true
						break
					}
				}
			}
		}
	}

	if !trapped {
		return &TrappedReason{
			Trapped: false,
			Summary: "Transaction did not fail with invoke_host_function_trapped.",
		}, nil
	}

	var events []xdr.DiagnosticEvent
	if meta != nil {
		events = ExtractDiagnosticEvents(meta)
	}

	if len(events) == 0 {
		return &TrappedReason{
			Trapped: true,
			Summary: "Contract invocation trapped, but no diagnostic events were provided to extract the panic message.",
			Hint:    "Run transaction simulation or capture diagnostic events in TransactionMeta to view detailed panic messages.",
		}, nil
	}

	return DecodeTrapReason(events)
}

// CorrelateTrapFromValues correlates a decoded TransactionResult Value with a
// decoded TransactionMeta Value.
func CorrelateTrapFromValues(resVal *Value, metaVal *Value) (*TrappedReason, error) {
	if resVal == nil {
		return nil, fmt.Errorf("%w: result Value is required", ErrInvalidArgument)
	}
	res, ok := resVal.Raw.(*xdr.TransactionResult)
	if !ok {
		return nil, fmt.Errorf("%w: expected *xdr.TransactionResult, got %T", ErrInvalidArgument, resVal.Raw)
	}

	var meta *xdr.TransactionMeta
	if metaVal != nil {
		if m, ok := metaVal.Raw.(*xdr.TransactionMeta); ok {
			meta = m
		}
	}

	return CorrelateTrap(res, meta)
}
