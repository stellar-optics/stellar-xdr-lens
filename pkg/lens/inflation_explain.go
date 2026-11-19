package lens

import "github.com/stellar/go-stellar-sdk/xdr"

// inflationRetirementNotice is the banner carried by every inflation operation.
const inflationRetirementNotice = "Deprecated: CAP-0026 permanently disabled the inflation mechanism in Protocol 12, so this operation can never succeed."

// operationDeprecation returns the banner for an operation the protocol has
// permanently retired, or "" for any other operation.
func operationDeprecation(op xdr.Operation) string {
	if op.Body.Type == xdr.OperationTypeInflation {
		return inflationRetirementNotice
	}
	return ""
}

// deepenInflationFailure explains the operation-level rejection current
// networks return. Since Protocol 12 stellar-core reports inflation as
// unsupported rather than running it, and the generic opNOT_SUPPORTED reason
// does not say the retirement is permanent. inflation_not_time is already
// covered by the static reason table.
func deepenInflationFailure(r Reason) Reason {
	if r.Constant != xdr.OperationResultCodeOpNotSupported.String() {
		return r
	}
	r.Summary = "The network no longer supports inflation: CAP-0026 permanently disabled the mechanism in Protocol 12."
	r.Hint = "Remove this operation; no retry, fee or account change can make it succeed."
	return r
}
