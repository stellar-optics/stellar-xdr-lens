package lens

import (
	"encoding/hex"
	"fmt"
	"math/big"
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
	// ExpiredLedger is the ledger at which the authorization signature expired.
	ExpiredLedger uint32
	// CurrentLedger is the ledger sequence against which expiration was verified.
	CurrentLedger uint32
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

// NewAuthExpirationFailure creates an AuthFailure for an expired signature expiration ledger.
func NewAuthExpirationFailure(operationIndex int, expiredLedger, currentLedger uint32) *AuthFailure {
	return &AuthFailure{
		FailureKind:    "signature_expired",
		OperationIndex: operationIndex,
		NonceExpiry:    fmt.Sprintf("expired at ledger %d (current ledger: %d)", expiredLedger, currentLedger),
		Summary:        fmt.Sprintf("Authorization expired at ledger %d (current ledger: %d)", expiredLedger, currentLedger),
		Hint:           fmt.Sprintf("Authorization expired at ledger %d (current ledger: %d). Refresh signature nonce and expiration before resubmitting.", expiredLedger, currentLedger),
		ExpiredLedger:  expiredLedger,
		CurrentLedger:  currentLedger,
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

// getAddressCredentials extracts the SorobanAddressCredentials from any of the address-based credentials arms.
func getAddressCredentials(creds xdr.SorobanCredentials) *xdr.SorobanAddressCredentials {
	switch creds.Type {
	case xdr.SorobanCredentialsTypeSorobanCredentialsAddress:
		return creds.Address
	case xdr.SorobanCredentialsTypeSorobanCredentialsAddressV2:
		return creds.AddressV2
	case xdr.SorobanCredentialsTypeSorobanCredentialsAddressWithDelegates:
		if creds.AddressWithDelegates != nil {
			return &creds.AddressWithDelegates.AddressCredentials
		}
	}
	return nil
}

// DiagnoseAuthExpiration evaluates Soroban authorization credentials against the current ledger sequence.
// If any entry's signatureExpirationLedger is less than currentLedger, it flags the authorization as expired.
func DiagnoseAuthExpiration(auth []xdr.SorobanAuthorizationEntry, currentLedger uint32, operationIndex int) *AuthFailure {
	if currentLedger == 0 {
		return nil
	}
	for _, entry := range auth {
		addr := getAddressCredentials(entry.Credentials)
		if addr == nil {
			continue
		}
		exp := uint32(addr.SignatureExpirationLedger)
		if exp < currentLedger {
			return NewAuthExpirationFailure(operationIndex, exp, currentLedger)
		}
	}
	return nil
}

// DiagnoseAuthExpirationFromBounds evaluates authorization entries against transaction precondition ledger bounds.
func DiagnoseAuthExpirationFromBounds(auth []xdr.SorobanAuthorizationEntry, lb *xdr.LedgerBounds, operationIndex int) *AuthFailure {
	if lb == nil {
		return nil
	}
	currentLedger := uint32(lb.MinLedger)
	if currentLedger == 0 {
		currentLedger = uint32(lb.MaxLedger)
	}
	return DiagnoseAuthExpiration(auth, currentLedger, operationIndex)
}

// DiagnoseAuthExpirationFromHeader evaluates authorization entries against a ledger header sequence.
func DiagnoseAuthExpirationFromHeader(auth []xdr.SorobanAuthorizationEntry, header *xdr.LedgerHeader, operationIndex int) *AuthFailure {
	if header == nil {
		return nil
	}
	return DiagnoseAuthExpiration(auth, uint32(header.LedgerSeq), operationIndex)
}

// DiagnoseInvokeHostFunctionAuth checks an InvokeHostFunctionOp for expired authorization signatures.
func DiagnoseInvokeHostFunctionAuth(op xdr.InvokeHostFunctionOp, currentLedger uint32, operationIndex int) *AuthFailure {
	return DiagnoseAuthExpiration(op.Auth, currentLedger, operationIndex)
}

// DiagnoseAuthExpirationFromEnvelope checks all InvokeHostFunction operations in an envelope
// against its precondition ledger bounds (or explicit currentLedger if provided).
func DiagnoseAuthExpirationFromEnvelope(env *xdr.TransactionEnvelope, currentLedger uint32) []*AuthFailure {
	if env == nil {
		return nil
	}
	if currentLedger == 0 {
		if lb := env.LedgerBounds(); lb != nil {
			currentLedger = uint32(lb.MinLedger)
			if currentLedger == 0 {
				currentLedger = uint32(lb.MaxLedger)
			}
		}
	}
	if currentLedger == 0 {
		return nil
	}

	var failures []*AuthFailure
	for i, op := range env.Operations() {
		if op.Body.Type == xdr.OperationTypeInvokeHostFunction {
			if hostOp, ok := op.Body.GetInvokeHostFunctionOp(); ok {
				if fail := DiagnoseInvokeHostFunctionAuth(hostOp, currentLedger, i); fail != nil {
					failures = append(failures, fail)
				}
			}
		}
	}
	return failures
}

// init registers the diagnosis function with the lens framework.

// FormatAuthTree renders a SorobanAuthorizedInvocation hierarchy as an indented ASCII tree.
func FormatAuthTree(inv xdr.SorobanAuthorizedInvocation) string {
	var b strings.Builder
	b.WriteString("└── Authorized Call: " + FormatContractCall(inv.Function) + "\n")
	renderSubInvocations(&b, inv.SubInvocations, "    ")
	return strings.TrimRight(b.String(), "\n")
}

// FormatAuthorizationEntry renders a single SorobanAuthorizationEntry's root invocation as an indented ASCII tree.
func FormatAuthorizationEntry(entry xdr.SorobanAuthorizationEntry) string {
	return FormatAuthTree(entry.RootInvocation)
}

// FormatAuthorizationEntries renders multiple SorobanAuthorizationEntry root invocations as indented ASCII trees.
func FormatAuthorizationEntries(entries []xdr.SorobanAuthorizationEntry) string {
	if len(entries) == 0 {
		return ""
	}
	var b strings.Builder
	for i, entry := range entries {
		branch := "├── "
		prefix := "│   "
		if i == len(entries)-1 {
			branch = "└── "
			prefix = "    "
		}
		fmt.Fprintf(&b, "%sAuthorized Call: %s\n", branch, FormatContractCall(entry.RootInvocation.Function))
		renderSubInvocations(&b, entry.RootInvocation.SubInvocations, prefix)
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderSubInvocations(b *strings.Builder, subs []xdr.SorobanAuthorizedInvocation, prefix string) {
	for i, sub := range subs {
		isLast := (i == len(subs)-1)
		branch := "├── "
		nextPrefix := prefix + "│   "
		if isLast {
			branch = "└── "
			nextPrefix = prefix + "    "
		}
		fmt.Fprintf(b, "%s%sSub-Invocation: %s\n", prefix, branch, FormatContractCall(sub.Function))
		if len(sub.SubInvocations) > 0 {
			renderSubInvocations(b, sub.SubInvocations, nextPrefix)
		}
	}
}

// FormatContractCall formats a SorobanAuthorizedFunction into a readable call signature Contract.fn(args).
func FormatContractCall(fn xdr.SorobanAuthorizedFunction) string {
	switch fn.Type {
	case xdr.SorobanAuthorizedFunctionTypeSorobanAuthorizedFunctionTypeContractFn:
		cFn := fn.ContractFn
		if cFn == nil {
			return "unknown.unknown()"
		}
		contractName := formatContractAddress(cFn.ContractAddress)
		fnName := string(cFn.FunctionName)
		var argStrs []string
		for _, arg := range cFn.Args {
			argStrs = append(argStrs, FormatScValShort(arg))
		}
		return fmt.Sprintf("%s.%s(%s)", contractName, fnName, strings.Join(argStrs, ", "))

	case xdr.SorobanAuthorizedFunctionTypeSorobanAuthorizedFunctionTypeCreateContractHostFn:
		return "create_contract(...)"

	case xdr.SorobanAuthorizedFunctionTypeSorobanAuthorizedFunctionTypeCreateContractV2HostFn:
		return "create_contract_v2(...)"

	default:
		return "unknown_function()"
	}
}

func formatContractAddress(addr xdr.ScAddress) string {
	switch addr.Type {
	case xdr.ScAddressTypeScAddressTypeContract:
		if addr.ContractId != nil {
			raw := addr.ContractId[:]
			name := strings.TrimRight(string(raw), "\x00")
			if isCleanIdentifier(name) {
				return name
			}
			if s, err := addr.String(); err == nil && s != "" {
				return s
			}
			return fmt.Sprintf("contract:%x", raw[:4])
		}
	case xdr.ScAddressTypeScAddressTypeAccount:
		if addr.AccountId != nil {
			if s, err := addr.String(); err == nil && s != "" {
				return s
			}
		}
	}
	return "unknown_contract"
}

func isCleanIdentifier(s string) bool {
	if len(s) == 0 || len(s) > 32 {
		return false
	}
	for i, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (i > 0 && r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return false
	}
	return true
}

// FormatScValShort formats an ScVal into a concise human-readable argument string.
func FormatScValShort(v xdr.ScVal) string {
	switch v.Type {
	case xdr.ScValTypeScvBool:
		if v.B != nil {
			return fmt.Sprintf("%t", *v.B)
		}
		return "false"
	case xdr.ScValTypeScvVoid:
		return "()"
	case xdr.ScValTypeScvU32:
		if v.U32 != nil {
			return fmt.Sprintf("%d", *v.U32)
		}
		return "0"
	case xdr.ScValTypeScvI32:
		if v.I32 != nil {
			return fmt.Sprintf("%d", *v.I32)
		}
		return "0"
	case xdr.ScValTypeScvU64:
		if v.U64 != nil {
			return fmt.Sprintf("%d", *v.U64)
		}
		return "0"
	case xdr.ScValTypeScvI64:
		if v.I64 != nil {
			return fmt.Sprintf("%d", *v.I64)
		}
		return "0"
	case xdr.ScValTypeScvTimepoint:
		if v.Timepoint != nil {
			return fmt.Sprintf("%d", *v.Timepoint)
		}
		return "0"
	case xdr.ScValTypeScvDuration:
		if v.Duration != nil {
			return fmt.Sprintf("%d", *v.Duration)
		}
		return "0"
	case xdr.ScValTypeScvU128:
		if v.U128 != nil {
			hi := new(big.Int).SetUint64(uint64(v.U128.Hi))
			lo := new(big.Int).SetUint64(uint64(v.U128.Lo))
			val := new(big.Int).Lsh(hi, 64)
			val.Or(val, lo)
			return val.String()
		}
		return "0"
	case xdr.ScValTypeScvI128:
		if v.I128 != nil {
			hi := new(big.Int).SetInt64(int64(v.I128.Hi))
			lo := new(big.Int).SetUint64(uint64(v.I128.Lo))
			val := new(big.Int).Lsh(hi, 64)
			val.Or(val, lo)
			return val.String()
		}
		return "0"
	case xdr.ScValTypeScvSymbol:
		if v.Sym != nil {
			return string(*v.Sym)
		}
		return "symbol"
	case xdr.ScValTypeScvString:
		if v.Str != nil {
			return string(*v.Str)
		}
		return `""`
	case xdr.ScValTypeScvBytes:
		if v.Bytes != nil {
			b := *v.Bytes
			if len(b) > 8 {
				return hex.EncodeToString(b[:8]) + "…"
			}
			return hex.EncodeToString(b)
		}
		return "0x"
	case xdr.ScValTypeScvAddress:
		if v.Address != nil {
			return formatContractAddress(*v.Address)
		}
		return "address"
	case xdr.ScValTypeScvVec:
		if v.Vec != nil && *v.Vec != nil {
			elems := **v.Vec
			if len(elems) == 0 {
				return "[]"
			}
			parts := make([]string, 0, len(elems))
			for _, e := range elems {
				parts = append(parts, FormatScValShort(e))
			}
			return "[" + strings.Join(parts, ", ") + "]"
		}
		return "[]"
	case xdr.ScValTypeScvMap:
		return "{...}"
	case xdr.ScValTypeScvContractInstance:
		return "(instance)"
	case xdr.ScValTypeScvLedgerKeyContractInstance:
		return "(instance)"
	case xdr.ScValTypeScvLedgerKeyNonce:
		return "(nonce)"
	default:
		return "..."
	}
}

// init registers the diagnosis function with the lens framework.
func init() {
	_ = AuthFailure{}
}
