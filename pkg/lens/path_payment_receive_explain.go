package lens

import (
	"fmt"
	"math/big"

	"github.com/stellar/go-stellar-sdk/amount"
	"github.com/stellar/go-stellar-sdk/xdr"
)

// PathPaymentDiagnosis breaks a failed PathPaymentStrictReceive down into its
// route and limits. Everything in it comes from the operation and its result.
type PathPaymentDiagnosis struct {
	// Code is the short result code, e.g. "path_payment_strict_receive_no_issuer".
	Code string `json:"code"`
	// SendAsset and SendMax are the asset spent and the most that may be spent.
	SendAsset string `json:"sendAsset"`
	SendMax   string `json:"sendMax"`
	// SendMaxStroops is SendMax in stroops.
	SendMaxStroops int64 `json:"sendMaxStroops"`
	// DestAsset and DestAmount are the asset and exact amount to deliver.
	DestAsset  string `json:"destAsset"`
	DestAmount string `json:"destAmount"`
	// DestAmountStroops is DestAmount in stroops.
	DestAmountStroops int64 `json:"destAmountStroops"`
	// Destination is the receiving account.
	Destination string `json:"destination"`
	// Path lists the intermediate assets in route order.
	Path []string `json:"path,omitempty"`
	// Hops lists each conversion the route needs.
	Hops []PathPaymentHop `json:"hops"`
	// MaxSendPerDest is the worst rate SendMax accepts: units of SendAsset per
	// unit of DestAsset. It is empty when either amount is not positive.
	MaxSendPerDest string `json:"maxSendPerDest,omitempty"`
	// NoIssuerAsset is the asset whose issuer is missing, for no-issuer failures.
	NoIssuerAsset string `json:"noIssuerAsset,omitempty"`
	// NoIssuerHop is the first hop that touches NoIssuerAsset.
	NoIssuerHop *PathPaymentHop `json:"noIssuerHop,omitempty"`
	// Context is the plain-English account of the failure.
	Context string `json:"context"`
	// Hint suggests a fix when the failure calls for specific advice.
	Hint string `json:"hint,omitempty"`
}

// DiagnosePathPaymentStrictReceiveFailure explains a failed strict-receive
// path payment from the operation and its typed result. It returns nil when
// either is missing or the operation succeeded.
//
// A failed result does not report the amount the route would have cost or, for
// liquidity failures, which book fell short, so the diagnosis lists every hop
// to check rather than guessing at one.
func DiagnosePathPaymentStrictReceiveFailure(op *xdr.PathPaymentStrictReceiveOp, res *xdr.PathPaymentStrictReceiveResult) *PathPaymentDiagnosis {
	if op == nil || res == nil || res.Code == xdr.PathPaymentStrictReceiveResultCodePathPaymentStrictReceiveSuccess {
		return nil
	}

	route := newPathPaymentRoute(op.SendAsset, op.Path, op.DestAsset)
	d := &PathPaymentDiagnosis{
		Code:              ExplainCode(res.Code.String()).Code,
		SendAsset:         assetLabel(op.SendAsset),
		SendMax:           amount.String(op.SendMax),
		SendMaxStroops:    int64(op.SendMax),
		DestAsset:         assetLabel(op.DestAsset),
		DestAmount:        amount.String(op.DestAmount),
		DestAmountStroops: int64(op.DestAmount),
		Destination:       addressOf(op.Destination),
		Path:              route.path,
		Hops:              route.hops,
		MaxSendPerDest:    pathPaymentRate(op.SendMax, op.DestAmount),
	}

	d.Context = fmt.Sprintf("Route %s (%d hop(s)): send at most %s %s to deliver exactly %s %s to %s.",
		route.line, len(route.hops), d.SendMax, d.SendAsset, d.DestAmount, d.DestAsset, shortAddr(d.Destination))
	if d.MaxSendPerDest != "" {
		d.Context += fmt.Sprintf(" sendMax accepts at most %s %s per 1 %s.", d.MaxSendPerDest, d.SendAsset, d.DestAsset)
	}

	switch res.Code {
	case xdr.PathPaymentStrictReceiveResultCodePathPaymentStrictReceiveOverSendmax:
		d.Context += fmt.Sprintf(" Delivering %s %s would have cost more than the sendMax of %s %s across these books: %s.",
			d.DestAmount, d.DestAsset, d.SendMax, d.SendAsset, route.books())
	case xdr.PathPaymentStrictReceiveResultCodePathPaymentStrictReceiveTooFewOffers:
		d.Context += " At least one of these books lacked the depth to fill the payment: " + route.books() + "."
	case xdr.PathPaymentStrictReceiveResultCodePathPaymentStrictReceiveNoIssuer:
		var sentence string
		d.NoIssuerAsset, d.NoIssuerHop, sentence = route.noIssuer(res.NoIssuer)
		d.Context += sentence
		if d.NoIssuerAsset != "" {
			d.Hint = fmt.Sprintf("Check that %s is issued on this network, or choose a route that avoids it.", d.NoIssuerAsset)
		}
	case xdr.PathPaymentStrictReceiveResultCodePathPaymentStrictReceiveMalformed:
		d.Context += nonPositiveAmount("sendMax", op.SendMax) + nonPositiveAmount("destAmount", op.DestAmount)
	}
	return d
}

// SlippageTolerance reports how far above a quoted price, in percent, sendMax
// still allows the route to execute. quoted is the send asset per destination
// asset price the path was quoted at. It reports false when the diagnosis has
// no usable rate or the quote is not positive; a negative tolerance means
// sendMax was already below the quote.
func (d *PathPaymentDiagnosis) SlippageTolerance(quoted xdr.Price) (string, bool) {
	if d == nil || d.SendMaxStroops <= 0 || d.DestAmountStroops <= 0 || quoted.N <= 0 || quoted.D <= 0 {
		return "", false
	}
	limit := new(big.Rat).SetFrac64(d.SendMaxStroops, d.DestAmountStroops)
	price := new(big.Rat).SetFrac64(int64(quoted.N), int64(quoted.D))
	ratio := new(big.Rat).Quo(limit, price)
	percent := ratio.Sub(ratio, big.NewRat(1, 1)).Mul(ratio, big.NewRat(100, 1))
	return percent.FloatString(2) + "%", true
}
