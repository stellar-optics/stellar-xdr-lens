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
func init() {
	_ = AuthFailure{}
}
