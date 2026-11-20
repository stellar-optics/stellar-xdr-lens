package lens

import (
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"

	"github.com/stellar/go-stellar-sdk/strkey"
	"github.com/stellar/go-stellar-sdk/xdr"
)

// Diagnostic and contract events emitted during Soroban execution.
//
// ContractEvent.ContractId and the contract arm of ScAddress are raw 32-byte
// hashes in XDR. Wallets and explorers show them as C... strkeys, so the
// enrichers registered here render them the same way. ScAddress covers the
// account and contract arms at once, which makes event topics readable
// without collapsing the rest of the event: topics and data stay structured
// ScVal trees.

func init() {
	registerEnricher(reflect.TypeOf(xdr.ContractId{}), enrichContractID)
	registerEnricher(reflect.TypeOf(xdr.ScAddress{}), enrichScAddress)
}

// enrichContractID renders a contract hash as its C... strkey.
func enrichContractID(rv reflect.Value) (string, any, bool) {
	id, valid := rv.Interface().(xdr.ContractId)
	if !valid {
		return "", nil, false
	}
	str, err := strkey.Encode(strkey.VersionByteContract, id[:])
	if err != nil || str == "" {
		return "", nil, false
	}
	return "", str, true
}

// enrichScAddress renders any Soroban address arm — account or contract — as
// its strkey form, which is what the SDK's own String method produces.
func enrichScAddress(rv reflect.Value) (string, any, bool) {
	addr, valid := rv.Interface().(xdr.ScAddress)
	if !valid {
		return "", nil, false
	}
	str, err := addr.String()
	if err != nil || str == "" {
		return "", nil, false
	}
	return "", str, true
}

// Soroban core diagnostic topics. The host emits fn_call and fn_return around
// every contract invocation, error when a host or contract error is raised,
// log for contract log!/panic! output, and core_metrics for budget counters.
const (
	DiagnosticKindFnCall      = "fn_call"
	DiagnosticKindFnReturn    = "fn_return"
	DiagnosticKindError       = "error"
	DiagnosticKindLog         = "log"
	DiagnosticKindCoreMetrics = "core_metrics"
	// DiagnosticKindContract and DiagnosticKindSystem label events that are
	// not one of the host's diagnostic topics: a contract's own events, and
	// system events.
	DiagnosticKindContract = "contract"
	DiagnosticKindSystem   = "system"
)

// DiagnosticEventSummary is a decoded DiagnosticEvent: the core topic it
// carries, the contract and function it concerns, and the error it reports,
// all rendered as text.
type DiagnosticEventSummary struct {
	// Index is the event's position in the diagnostic event list.
	Index int `json:"index"`
	// Kind is the core topic (fn_call, fn_return, error, log, core_metrics),
	// or contract/system for other events.
	Kind string `json:"kind"`
	// Failed reports that the event was emitted inside a contract call that
	// did not succeed: the call trace that led to the failure.
	Failed bool `json:"failed"`
	// ContractID is the C... strkey of the contract the event concerns: the
	// callee for fn_call, the emitting contract otherwise.
	ContractID string `json:"contractId,omitempty"`
	// Function is the contract function called or returning.
	Function string `json:"function,omitempty"`
	// Error is the ScError in Soroban notation, such as
	// "Error(WasmVm, MissingValue)" or "Error(Contract, #3)".
	Error string `json:"error,omitempty"`
	// Topics and Data are the event's topics and payload as text.
	Topics []string `json:"topics,omitempty"`
	Data   string   `json:"data,omitempty"`
	// Text is the one-line rendering produced by FormatDiagnosticEvent.
	Text string `json:"text"`
}

// DecodeDiagnosticEvents decodes each diagnostic event into a summary,
// keeping the order the host emitted them in.
func DecodeDiagnosticEvents(events []xdr.DiagnosticEvent) []DiagnosticEventSummary {
	out := make([]DiagnosticEventSummary, 0, len(events))
	for i, event := range events {
		summary := decodeDiagnosticEvent(event)
		summary.Index = i
		out = append(out, summary)
	}
	return out
}

func decodeDiagnosticEvent(event xdr.DiagnosticEvent) DiagnosticEventSummary {
	s := DiagnosticEventSummary{
		Kind:   DiagnosticKindContract,
		Failed: !event.InSuccessfulContractCall,
	}
	if event.Event.Type == xdr.ContractEventTypeSystem {
		s.Kind = DiagnosticKindSystem
	}
	if event.Event.ContractId != nil {
		id := [32]byte(*event.Event.ContractId)
		s.ContractID = contractStrkey(id[:])
	}

	var topics []xdr.ScVal
	var data *xdr.ScVal
	if body, ok := event.Event.Body.GetV0(); ok {
		topics = body.Topics
		data = &body.Data
	}
	for _, t := range topics {
		s.Topics = append(s.Topics, diagnosticValueString(t))
	}
	if data != nil {
		s.Data = diagnosticValueString(*data)
	}

	if event.Event.Type == xdr.ContractEventTypeDiagnostic && len(topics) > 0 {
		if sym, ok := topics[0].GetSym(); ok {
			switch string(sym) {
			case DiagnosticKindFnCall:
				s.Kind = DiagnosticKindFnCall
				// fn_call topics: [fn_call, callee contract id bytes, function symbol]
				if len(topics) > 1 {
					if b, ok := topics[1].GetBytes(); ok && len(b) == 32 {
						s.ContractID = contractStrkey(b)
					}
				}
				if len(topics) > 2 {
					if fn, ok := topics[2].GetSym(); ok {
						s.Function = string(fn)
					}
				}
			case DiagnosticKindFnReturn:
				s.Kind = DiagnosticKindFnReturn
				if len(topics) > 1 {
					if fn, ok := topics[1].GetSym(); ok {
						s.Function = string(fn)
					}
				}
			case DiagnosticKindError:
				s.Kind = DiagnosticKindError
				if len(topics) > 1 {
					if scErr, ok := topics[1].GetError(); ok {
						s.Error = FormatScError(scErr)
					}
				}
			case DiagnosticKindLog:
				s.Kind = DiagnosticKindLog
			case DiagnosticKindCoreMetrics:
				s.Kind = DiagnosticKindCoreMetrics
			}
		}
	}
	if s.Error == "" && data != nil {
		if scErr, ok := data.GetError(); ok {
			s.Error = FormatScError(scErr)
		}
	}
	s.Text = formatDiagnosticSummary(s)
	return s
}

// FormatDiagnosticEvent formats a single DiagnosticEvent into a readable
// line: "[failed] error Error(WasmVm, MissingValue): ..." for an error,
// "fn_call CABC...::transfer(...)" for a call, and so on.
func FormatDiagnosticEvent(event xdr.DiagnosticEvent) string {
	return decodeDiagnosticEvent(event).Text
}

func formatDiagnosticSummary(s DiagnosticEventSummary) string {
	var b strings.Builder
	status := "success"
	if s.Failed {
		status = "failed"
	}
	fmt.Fprintf(&b, "[%s] %s", status, s.Kind)

	switch s.Kind {
	case DiagnosticKindFnCall:
		fmt.Fprintf(&b, " %s(%s)", eventTarget(s), strings.TrimSuffix(strings.TrimPrefix(s.Data, "["), "]"))
		return b.String()
	case DiagnosticKindFnReturn:
		fmt.Fprintf(&b, " %s -> %s", eventTarget(s), s.Data)
		return b.String()
	case DiagnosticKindError:
		if s.ContractID != "" {
			fmt.Fprintf(&b, " in contract %s", s.ContractID)
		}
		if s.Error != "" {
			fmt.Fprintf(&b, " %s", s.Error)
		}
		if s.Data != "" {
			fmt.Fprintf(&b, ": %s", s.Data)
		}
		return b.String()
	case DiagnosticKindCoreMetrics:
		if len(s.Topics) > 1 {
			fmt.Fprintf(&b, " %s = %s", s.Topics[1], s.Data)
			return b.String()
		}
	}

	if s.ContractID != "" {
		fmt.Fprintf(&b, " from %s", s.ContractID)
	}
	if len(s.Topics) > 0 {
		fmt.Fprintf(&b, " topics=[%s]", strings.Join(s.Topics, ", "))
	}
	if s.Data != "" {
		fmt.Fprintf(&b, " data=%s", s.Data)
	}
	return b.String()
}

// eventTarget names the contract function an event concerns, with the full
// contract strkey so it can be searched for.
func eventTarget(s DiagnosticEventSummary) string {
	switch {
	case s.ContractID != "" && s.Function != "":
		return s.ContractID + "::" + s.Function
	case s.Function != "":
		return s.Function
	case s.ContractID != "":
		return s.ContractID
	}
	return "<unknown>"
}

// FormatScError renders an ScError the way the Soroban host and SDK print
// it: Error(Contract, #3) for a contract error code, Error(Auth,
// InvalidAction) for a host error.
func FormatScError(e xdr.ScError) string {
	typ := strings.TrimPrefix(e.Type.String(), "ScErrorTypeSce")
	if typ == "" {
		typ = fmt.Sprintf("%d", int32(e.Type))
	}
	if e.Type == xdr.ScErrorTypeSceContract {
		if e.ContractCode != nil {
			return fmt.Sprintf("Error(%s, #%d)", typ, uint32(*e.ContractCode))
		}
		return fmt.Sprintf("Error(%s)", typ)
	}
	if e.Code == nil {
		return fmt.Sprintf("Error(%s)", typ)
	}
	code := strings.TrimPrefix(e.Code.String(), "ScErrorCodeScec")
	if code == "" {
		code = fmt.Sprintf("%d", int32(*e.Code))
	}
	return fmt.Sprintf("Error(%s, %s)", typ, code)
}

// diagnosticValueString renders an event topic or payload. Strings are
// quoted so a message reads as one, errors use Soroban notation, and vectors
// and maps are spelled out instead of collapsed.
func diagnosticValueString(v xdr.ScVal) string {
	switch v.Type {
	case xdr.ScValTypeScvString:
		if str, ok := v.GetStr(); ok {
			return fmt.Sprintf("%q", string(str))
		}
	case xdr.ScValTypeScvBytes:
		if b, ok := v.GetBytes(); ok {
			return "0x" + hex.EncodeToString(b)
		}
	case xdr.ScValTypeScvError:
		if scErr, ok := v.GetError(); ok {
			return FormatScError(scErr)
		}
	case xdr.ScValTypeScvVec:
		if vec, ok := v.GetVec(); ok && vec != nil {
			parts := make([]string, 0, len(*vec))
			for _, item := range *vec {
				parts = append(parts, diagnosticValueString(item))
			}
			return "[" + strings.Join(parts, ", ") + "]"
		}
		return "[]"
	case xdr.ScValTypeScvMap:
		if m, ok := v.GetMap(); ok && m != nil {
			parts := make([]string, 0, len(*m))
			for _, entry := range *m {
				parts = append(parts, diagnosticValueString(entry.Key)+": "+diagnosticValueString(entry.Val))
			}
			return "{" + strings.Join(parts, ", ") + "}"
		}
		return "{}"
	}
	return ScValToString(v)
}

func contractStrkey(id []byte) string {
	if s, err := strkey.Encode(strkey.VersionByteContract, id); err == nil {
		return s
	}
	return hex.EncodeToString(id)
}
