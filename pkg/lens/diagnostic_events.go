package lens

import (
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

// FormatContractEvent formats a ContractEvent for human consumption. The
// contract ID is rendered as a C... strkey; each topic ScVal is formatted using
// string heuristics (symbol, string, address, integer); and the data value is
// rendered the same way.
func FormatContractEvent(event *xdr.ContractEvent) string {
	if event == nil {
		return "<nil ContractEvent>"
	}
	var sb strings.Builder

	// Event type.
	sb.WriteString(fmt.Sprintf("type=%s", event.Type.String()))

	// Contract ID, if present.
	if event.ContractId != nil {
		str, err := strkey.Encode(strkey.VersionByteContract, (*event.ContractId)[:])
		if err == nil && str != "" {
			sb.WriteString(fmt.Sprintf(" contract=%s", str))
		}
	}

	// Topics and data live inside the body.
	if event.Body.V0 != nil {
		topics := event.Body.V0.Topics
		if len(topics) > 0 {
			parts := make([]string, 0, len(topics))
			for _, t := range topics {
				parts = append(parts, formatScVal(t))
			}
			sb.WriteString(fmt.Sprintf(" topics=[%s]", strings.Join(parts, ", ")))
		}
		sb.WriteString(fmt.Sprintf(" data=%s", formatScVal(event.Body.V0.Data)))
	}

	return sb.String()
}

// formatScVal formats a ScVal using simple string heuristics: symbols and
// strings are shown as their text, addresses as strkeys, integers as decimal
// numbers, and everything else as its XDR type name.
func formatScVal(v xdr.ScVal) string {
	switch v.Type {
	case xdr.ScValTypeScvSymbol:
		if v.Sym != nil {
			return string(*v.Sym)
		}
	case xdr.ScValTypeScvString:
		if v.Str != nil {
			return fmt.Sprintf("%q", string(*v.Str))
		}
	case xdr.ScValTypeScvAddress:
		if v.Address != nil {
			if s, err := v.Address.String(); err == nil {
				return s
			}
		}
	case xdr.ScValTypeScvI32:
		if v.I32 != nil {
			return fmt.Sprintf("%d", *v.I32)
		}
	case xdr.ScValTypeScvU32:
		if v.U32 != nil {
			return fmt.Sprintf("%d", *v.U32)
		}
	case xdr.ScValTypeScvI64:
		if v.I64 != nil {
			return fmt.Sprintf("%d", *v.I64)
		}
	case xdr.ScValTypeScvU64:
		if v.U64 != nil {
			return fmt.Sprintf("%d", *v.U64)
		}
	case xdr.ScValTypeScvBool:
		if v.B != nil {
			return fmt.Sprintf("%v", *v.B)
		}
	case xdr.ScValTypeScvVoid:
		return "void"
	}
	return v.Type.String()
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
