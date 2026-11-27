package format

import (
	"strings"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

// hostCall is one contract invocation edge in a Soroban call graph. Caller may
// be empty when the call is a transaction's root invocation.
type hostCall struct {
	Caller     string
	Callee     string
	Function   string
	Args       []string
	Authorized bool
}

// label renders the call as "function(arg, arg)".
func (c hostCall) label() string {
	if c.Function == "" {
		return "invoke"
	}
	if len(c.Args) == 0 {
		return c.Function + "()"
	}
	return c.Function + "(" + strings.Join(c.Args, ", ") + ")"
}

// envelopeOperations returns the operations of an envelope, unwrapping the fee
// bump to the inner transaction. It returns nil for shapes it does not know.
func envelopeOperations(env *xdr.TransactionEnvelope) []xdr.Operation {
	if env == nil {
		return nil
	}
	if v1, ok := env.GetV1(); ok {
		return v1.Tx.Operations
	}
	if v0, ok := env.GetV0(); ok {
		return v0.Tx.Operations
	}
	if fb, ok := env.GetFeeBump(); ok {
		if inner, ok := fb.Tx.InnerTx.GetV1(); ok {
			return inner.Tx.Operations
		}
	}
	return nil
}

// envelopeSource returns the strkey source account of an envelope.
func envelopeSource(env *xdr.TransactionEnvelope) string {
	if env == nil {
		return ""
	}
	if v1, ok := env.GetV1(); ok {
		return v1.Tx.SourceAccount.Address()
	}
	if v0, ok := env.GetV0(); ok {
		account := xdr.AccountId{
			Type:    xdr.PublicKeyTypePublicKeyTypeEd25519,
			Ed25519: &v0.Tx.SourceAccountEd25519,
		}
		if addr, err := account.GetAddress(); err == nil {
			return addr
		}
	}
	if fb, ok := env.GetFeeBump(); ok {
		return fb.Tx.FeeSource.Address()
	}
	return ""
}

// operationSource returns the operation's own source account, or fallback when
// the operation inherits the transaction source.
func operationSource(op xdr.Operation, fallback string) string {
	if op.SourceAccount == nil {
		return fallback
	}
	if addr := op.SourceAccount.Address(); addr != "" {
		return addr
	}
	return fallback
}

// scAddressText renders a Soroban address as its strkey form, falling back to
// a stable placeholder so a graph never contains an empty identifier.
func scAddressText(addr xdr.ScAddress) string {
	if s, err := addr.String(); err == nil && s != "" {
		return s
	}
	return "unknown"
}

// hostFunctionCalls turns a transaction's host function into root call edges.
func hostFunctionCalls(fn xdr.HostFunction) []hostCall {
	if inv, ok := fn.GetInvokeContract(); ok {
		return []hostCall{contractCall(inv, "")}
	}
	if _, ok := fn.GetCreateContract(); ok {
		return []hostCall{{Function: "create_contract"}}
	}
	if _, ok := fn.GetCreateContractV2(); ok {
		return []hostCall{{Function: "create_contract_v2"}}
	}
	if _, ok := fn.GetWasm(); ok {
		return []hostCall{{Function: "upload_wasm"}}
	}
	return nil
}

// contractCall converts InvokeContractArgs into a call edge.
func contractCall(inv xdr.InvokeContractArgs, caller string) hostCall {
	args := make([]string, 0, len(inv.Args))
	for _, arg := range inv.Args {
		args = append(args, lens.FormatScValShort(arg))
	}
	return hostCall{
		Caller:   caller,
		Callee:   scAddressText(inv.ContractAddress),
		Function: string(inv.FunctionName),
		Args:     args,
	}
}

// invocationCalls flattens a SorobanAuthorizedInvocation tree into call edges,
// using the parent contract as the caller of its sub-invocations.
func invocationCalls(inv xdr.SorobanAuthorizedInvocation, caller string) []hostCall {
	var call hostCall
	switch inv.Function.Type {
	case xdr.SorobanAuthorizedFunctionTypeSorobanAuthorizedFunctionTypeContractFn:
		if inv.Function.ContractFn != nil {
			call = contractCall(*inv.Function.ContractFn, caller)
		}
	case xdr.SorobanAuthorizedFunctionTypeSorobanAuthorizedFunctionTypeCreateContractHostFn:
		call = hostCall{Caller: caller, Function: "create_contract"}
	case xdr.SorobanAuthorizedFunctionTypeSorobanAuthorizedFunctionTypeCreateContractV2HostFn:
		call = hostCall{Caller: caller, Function: "create_contract_v2"}
	}
	if call.Function == "" {
		return nil
	}

	calls := []hostCall{call}
	nextCaller := call.Callee
	if nextCaller == "" {
		nextCaller = caller
	}
	for _, sub := range inv.SubInvocations {
		calls = append(calls, invocationCalls(sub, nextCaller)...)
	}
	return calls
}

// authorizationCalls returns the calls an authorization entry authorises,
// attributed to the account or contract that signed the entry.
func authorizationCalls(entry xdr.SorobanAuthorizationEntry) []hostCall {
	authorizer := ""
	if creds, ok := entry.Credentials.GetAddress(); ok {
		authorizer = scAddressText(creds.Address)
	} else if creds, ok := entry.Credentials.GetAddressV2(); ok {
		authorizer = scAddressText(creds.Address)
	} else if creds, ok := entry.Credentials.GetAddressWithDelegates(); ok {
		authorizer = scAddressText(creds.AddressCredentials.Address)
	}
	calls := invocationCalls(entry.RootInvocation, authorizer)
	for i := range calls {
		calls[i].Authorized = true
	}
	return calls
}

// invokeOpCalls returns the root and authorised calls of one
// invoke-host-function operation, attributing the root call to opSource.
func invokeOpCalls(op xdr.InvokeHostFunctionOp, opSource string) (root, auth []hostCall) {
	for _, call := range hostFunctionCalls(op.HostFunction) {
		call.Caller = opSource
		root = append(root, call)
	}
	for _, entry := range op.Auth {
		auth = append(auth, authorizationCalls(entry)...)
	}
	return root, auth
}
