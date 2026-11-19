package lens

import (
	"fmt"

	"github.com/stellar/go-stellar-sdk/xdr"
)

// DiagnoseCreateAccount explains the CreateAccount failures whose remedy can
// be stated from the operation alone. It returns "" when op or res is missing
// and for every other result code.
//
// The minimum uses DefaultBaseReserve, since lens never reads live network
// parameters: two base reserves, or 1 XLM on Mainnet.
func DiagnoseCreateAccount(op *xdr.CreateAccountOp, res *xdr.CreateAccountResult) string {
	if op == nil || res == nil {
		return ""
	}
	switch res.Code {
	case xdr.CreateAccountResultCodeCreateAccountAlreadyExist:
		return "Account already exists on the ledger. Use a Payment operation to fund existing accounts rather than CreateAccount."
	case xdr.CreateAccountResultCodeCreateAccountLowReserve:
		minimum := ReserveRequirement(0, 0, 0, DefaultBaseReserve)
		starting := int64(op.StartingBalance)
		if starting >= minimum {
			return fmt.Sprintf(
				"Starting balance of %s (%d stroops) meets the 1.0 XLM minimum at the default base reserve of 0.5 XLM, so this network must require a higher base reserve.",
				stroopsToXLM(starting), starting)
		}
		shortfall := minimum - starting
		return fmt.Sprintf(
			"Starting balance of %s (%d stroops) is below the 1.0 XLM minimum (2 x 0.5 XLM base reserve = %d stroops), a shortfall of %d stroops (%s).",
			stroopsToXLM(starting), starting, minimum, shortfall, stroopsToXLM(shortfall))
	}
	return ""
}

// deepenCreateAccountFailure attaches the CreateAccount diagnosis to a failed
// operation's explanation.
func deepenCreateAccountFailure(op xdr.Operation, r Reason) Reason {
	res := xdr.CreateAccountResult{}
	switch r.Constant {
	case "CreateAccountResultCodeCreateAccountAlreadyExist":
		res.Code = xdr.CreateAccountResultCodeCreateAccountAlreadyExist
	case "CreateAccountResultCodeCreateAccountLowReserve":
		res.Code = xdr.CreateAccountResultCodeCreateAccountLowReserve
		r.Hint = "Fund the new account with at least two base reserves (1.0 XLM on Mainnet), or have another account sponsor its reserves."
	default:
		return r
	}
	r.Context = DiagnoseCreateAccount(op.Body.CreateAccountOp, &res)
	return r
}
