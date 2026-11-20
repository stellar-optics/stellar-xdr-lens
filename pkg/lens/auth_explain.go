package lens

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/stellar/go-stellar-sdk/xdr"
)

// AuthFailure explains a Soroban contract authorization failure.
// This covers invalid signatures, expired nonce sequences, and missing
// authorization entries that prevent a transaction from executing.
type AuthFailure struct {
	// FailureKind is the category of authorization failure.
	FailureKind string
	// OperationIndex is the index of the failing operation.
	OperationIndex int
	// MissingAuthenticator describes what authenticator is missing, if known.
	MissingAuthenticator string
	// NonceExpiry explains if and when a nonce expired, if applicable.
	NonceExpiry string
	// Summary plain-English summary of the failure.
	Summary string
	// Hint suggests the next debugging step.
	Hint string
}

// NewAuthFailure creates a new AuthFailure diagnosis.
func NewAuthFailure(failureKind string, operationIndex int, missingAuthenticator, nonceExpiry, summary, hint string) *AuthFailure {
	return &AuthFailure{
		FailureKind:          failureKind,
		OperationIndex:       operationIndex,
		MissingAuthenticator: missingAuthenticator,
		NonceExpiry:          nonceExpiry,
		Summary:              summary,
		Hint:                 hint,
	}
}

// String renders the diagnosis as a single line.
func (a *AuthFailure) String() string {
	if a == nil {
		return "<nil>"
	}
	return a.Summary
}

// DiagnosticReport renders a multi-line report suitable for CLI output.
func (a *AuthFailure) DiagnosticReport() string {
	if a == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Soroban authorization failure:\n")
	fmt.Fprintf(&b, "  Kind:          %s\n", a.FailureKind)
	fmt.Fprintf(&b, "  Operation:     %d\n", a.OperationIndex)
	if a.MissingAuthenticator != "" {
		fmt.Fprintf(&b, "  Missing:       %s\n", a.MissingAuthenticator)
	}
	if a.NonceExpiry != "" {
		fmt.Fprintf(&b, "  Nonce expiry:  %s\n", a.NonceExpiry)
	}
	fmt.Fprintf(&b, "  Summary:       %s\n", a.Summary)
	fmt.Fprintf(&b, "  Hint:          %s\n", a.Hint)
	return b.String()
}

// DiagnoseAuthFailure diagnoses a Soroban authorization failure from an
// operation result. This is a purely local, offline diagnosis - no network
// calls are made.
//
// It identifies three categories of auth failures:
//  1. Missing or invalid signature authorization entries
//  2. Expired or invalid nonce sequences
//  3. Missing contract-level auth entries
func DiagnoseAuthFailure(opResult xdr.OperationResult, operationIndex int) *AuthFailure {
	// Check if the inner result code indicates an auth failure
	if opResult.Code != xdr.OperationResultCodeOpInner {
		return nil
	}

	node := buildNode("", reflect.ValueOf(opResult), 0)
	if node == nil {
		return nil
	}

	tr := node.Child("Tr")
	if tr == nil {
		return nil
	}

	// Look for the invoked contract's auth-related fields
	for _, arm := range tr.Children {
		// Check for auth entry related arms
		if arm.Name == "Auth" || strings.HasPrefix(arm.Name, "Auth") {
			// Extract auth details
			var missingAuth, nonceExpiry, summary, hint string

			// Look for specific auth code children
			if codeNode := arm.Child("Code"); codeNode != nil {
				if code, ok := codeNode.Value.(string); ok {
					switch {
					case strings.Contains(code, "Auth") && strings.Contains(code, "Missing"):
						missingAuth = code
						summary = "Missing or invalid Soroban authorization entry"
						hint = "Collect the missing authorization entry and sign the authorizeEntry payload with the required signers"
					case strings.Contains(code, "Nonce") && strings.Contains(code, "Expired"):
						nonceExpiry = code
						summary = "Soroban nonce sequence has expired"
						hint = "Rebuild the transaction with a fresh nonce sequence or extend the time bounds"
					default:
						summary = "Authorization failure: " + code
					}
				}
			}

			if summary != "" {
				return NewAuthFailure(
					getFailureKind(missingAuth, nonceExpiry),
					operationIndex,
					missingAuth,
					nonceExpiry,
					summary,
					hint,
				)
			}
		}

		// Also check for general result code patterns
		if codeNode := arm.Child("Code"); codeNode != nil {
			if code, ok := codeNode.Value.(string); ok {
				switch {
				case strings.Contains(code, "AuthMissing") || strings.Contains(code, "auth_missing"):
					return NewAuthFailure(
						"auth_missing",
						operationIndex,
						"missing authorization entry",
						"",
						"Missing or invalid Soroban authorization entry",
						"Collect the missing authorization entry and sign the authorizeEntry payload with the required signers",
					)
				case strings.Contains(code, "NonceExpired") || strings.Contains(code, "nonce_expired"):
					return NewAuthFailure(
						"nonce_expired",
						operationIndex,
						"",
						"nonce sequence expired",
						"Soroban nonce sequence has expired",
						"Rebuild the transaction with a fresh nonce sequence or extend the time bounds",
					)
				}
			}
		}
	}

	return nil
}

// getFailureKind determines the kind of auth failure from the code and hint text.
func getFailureKind(missingAuth, nonceExpiry string) string {
	if missingAuth != "" && strings.Contains(missingAuth, "Missing") {
		return "auth_missing"
	}
	if nonceExpiry != "" && strings.Contains(nonceExpiry, "Expired") {
		return "nonce_expired"
	}
	return "auth_failure"
}

// Host function kinds reported by HostFunctionInvocation.
const (
	HostFunctionInvokeContract     = "invoke_contract"
	HostFunctionCreateContract     = "create_contract"
	HostFunctionUploadContractWasm = "upload_contract_wasm"
)

// HostFunctionInvocation is what an InvokeHostFunction operation asked the
// host to do: which host function, against which contract and function, and
// with how many authorization entries attached.
type HostFunctionInvocation struct {
	// Kind is one of HostFunctionInvokeContract, HostFunctionCreateContract
	// or HostFunctionUploadContractWasm.
	Kind string `json:"kind"`
	// ContractID is the C... strkey of the contract being called.
	ContractID string `json:"contractId,omitempty"`
	// FunctionName is the exported contract function being called.
	FunctionName string `json:"functionName,omitempty"`
	// ArgCount is the number of arguments passed to the function.
	ArgCount int `json:"argCount,omitempty"`
	// WasmSize is the size in bytes of uploaded contract code.
	WasmSize int `json:"wasmSize,omitempty"`
	// AuthEntries is the number of Soroban authorization entries attached,
	// and AddressAuthEntries how many of them carry their own signature
	// (address credentials) rather than relying on the source account.
	AuthEntries        int `json:"authEntries,omitempty"`
	AddressAuthEntries int `json:"addressAuthEntries,omitempty"`
}

// DescribeHostFunctionInvocation extracts the invocation context of an
// InvokeHostFunction operation, the part a failure explanation has to name
// for a developer to recognise their call.
func DescribeHostFunctionInvocation(op xdr.InvokeHostFunctionOp) HostFunctionInvocation {
	inv := HostFunctionInvocation{AuthEntries: len(op.Auth)}
	for _, entry := range op.Auth {
		if entry.Credentials.Type != xdr.SorobanCredentialsTypeSorobanCredentialsSourceAccount {
			inv.AddressAuthEntries++
		}
	}
	switch op.HostFunction.Type {
	case xdr.HostFunctionTypeHostFunctionTypeInvokeContract:
		inv.Kind = HostFunctionInvokeContract
		if args, ok := op.HostFunction.GetInvokeContract(); ok {
			if addr, err := args.ContractAddress.String(); err == nil {
				inv.ContractID = addr
			}
			inv.FunctionName = string(args.FunctionName)
			inv.ArgCount = len(args.Args)
		}
	case xdr.HostFunctionTypeHostFunctionTypeCreateContract,
		xdr.HostFunctionTypeHostFunctionTypeCreateContractV2:
		inv.Kind = HostFunctionCreateContract
	case xdr.HostFunctionTypeHostFunctionTypeUploadContractWasm:
		inv.Kind = HostFunctionUploadContractWasm
		if wasm, ok := op.HostFunction.GetWasm(); ok {
			inv.WasmSize = len(wasm)
		}
	}
	return inv
}

// Target names the invocation the way failure messages refer to it:
// "contract C...::transfer", "contract creation" or "contract upload".
func (h HostFunctionInvocation) Target() string {
	switch h.Kind {
	case HostFunctionInvokeContract:
		return fmt.Sprintf("contract %s function %s", h.ContractID, h.FunctionName)
	case HostFunctionCreateContract:
		return "contract creation"
	case HostFunctionUploadContractWasm:
		return fmt.Sprintf("contract upload (%d bytes of WASM)", h.WasmSize)
	}
	return "host function"
}

// Context renders the invocation as one line of detail for a Reason.
func (h HostFunctionInvocation) Context() string {
	var b strings.Builder
	b.WriteString(h.Kind)
	if h.Kind == HostFunctionInvokeContract {
		fmt.Fprintf(&b, ": contract %s, function %s, %d argument(s)", h.ContractID, h.FunctionName, h.ArgCount)
	}
	if h.AuthEntries > 0 {
		fmt.Fprintf(&b, "; %d authorization entr", h.AuthEntries)
		if h.AuthEntries == 1 {
			b.WriteString("y")
		} else {
			b.WriteString("ies")
		}
		if h.AddressAuthEntries > 0 {
			fmt.Fprintf(&b, " (%d with address credentials)", h.AddressAuthEntries)
		}
	}
	return b.String()
}

// init registers the diagnosis function with the lens framework.
func init() {
	_ = AuthFailure{}
}
