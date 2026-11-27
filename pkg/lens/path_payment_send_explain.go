package lens

import (
	"fmt"

	"github.com/stellar/go-stellar-sdk/amount"
	"github.com/stellar/go-stellar-sdk/xdr"
)

// PathPaymentStrictSendDiagnosis breaks a failed PathPaymentStrictSend down
// into its route and limits. Everything in it comes from the operation and its
// result.
type PathPaymentStrictSendDiagnosis struct {
	// Code is the short result code, e.g. "path_payment_strict_send_under_destmin".
	Code string `json:"code"`
	// SendAsset and SendAmount are the asset and exact amount spent.
	SendAsset  string `json:"sendAsset"`
	SendAmount string `json:"sendAmount"`
	// SendAmountStroops is SendAmount in stroops.
	SendAmountStroops int64 `json:"sendAmountStroops"`
	// DestAsset and DestMin are the asset delivered and the least acceptable.
	DestAsset string `json:"destAsset"`
	DestMin   string `json:"destMin"`
	// DestMinStroops is DestMin in stroops.
	DestMinStroops int64 `json:"destMinStroops"`
	// Destination is the receiving account.
	Destination string `json:"destination"`
	// Path lists the intermediate assets in route order.
	Path []string `json:"path,omitempty"`
	// Hops lists each conversion the route needs.
	Hops []PathPaymentHop `json:"hops"`
	// MinDestPerSend is the worst rate destMin accepts: units of DestAsset per
	// unit of SendAsset. It is empty when either amount is not positive.
	MinDestPerSend string `json:"minDestPerSend,omitempty"`
	// NoIssuerAsset is the asset whose issuer is missing, for no-issuer failures.
	NoIssuerAsset string `json:"noIssuerAsset,omitempty"`
	// NoIssuerHop is the first hop that touches NoIssuerAsset.
	NoIssuerHop *PathPaymentHop `json:"noIssuerHop,omitempty"`
	// Context is the plain-English account of the failure.
	Context string `json:"context"`
	// Hint suggests a fix when the failure calls for specific advice.
	Hint string `json:"hint,omitempty"`
}

// DiagnosePathPaymentStrictSendFailure explains a failed strict-send path
// payment from the operation and its typed result. It returns nil when either
// is missing or the operation succeeded.
//
// A failed result does not report what the route would have delivered, so the
// shortfall against destMin needs a quote from the caller: see DestMinShortfall.
func DiagnosePathPaymentStrictSendFailure(op *xdr.PathPaymentStrictSendOp, res *xdr.PathPaymentStrictSendResult) *PathPaymentStrictSendDiagnosis {
	if op == nil || res == nil || res.Code == xdr.PathPaymentStrictSendResultCodePathPaymentStrictSendSuccess {
		return nil
	}

	route := newPathPaymentRoute(op.SendAsset, op.Path, op.DestAsset)
	d := &PathPaymentStrictSendDiagnosis{
		Code:              ExplainCode(res.Code.String()).Code,
		SendAsset:         assetLabel(op.SendAsset),
		SendAmount:        amount.String(op.SendAmount),
		SendAmountStroops: int64(op.SendAmount),
		DestAsset:         assetLabel(op.DestAsset),
		DestMin:           amount.String(op.DestMin),
		DestMinStroops:    int64(op.DestMin),
		Destination:       addressOf(op.Destination),
		Path:              route.path,
		Hops:              route.hops,
		MinDestPerSend:    pathPaymentRate(op.DestMin, op.SendAmount),
	}

	d.Context = fmt.Sprintf("Route %s (%d hop(s)): send exactly %s %s to deliver at least %s %s to %s.",
		route.line, len(route.hops), d.SendAmount, d.SendAsset, d.DestMin, d.DestAsset, shortAddr(d.Destination))
	if d.MinDestPerSend != "" {
		d.Context += fmt.Sprintf(" destMin requires at least %s %s per 1 %s.", d.MinDestPerSend, d.DestAsset, d.SendAsset)
	}

	switch res.Code {
	case xdr.PathPaymentStrictSendResultCodePathPaymentStrictSendUnderDestmin:
		d.Context += fmt.Sprintf(" Converting %s %s along this route would have delivered less than the destMin of %s %s.",
			d.SendAmount, d.SendAsset, d.DestMin, d.DestAsset)
		d.Hint = fmt.Sprintf("Relax destMin below %s %s if the current rate is acceptable, increase sendAmount above %s %s so the route can reach destMin, or inspect liquidity on these books: %s.",
			d.DestMin, d.DestAsset, d.SendAmount, d.SendAsset, route.books())
	case xdr.PathPaymentStrictSendResultCodePathPaymentStrictSendTooFewOffers:
		d.Context += " At least one of these books lacked the depth to fill the payment: " + route.books() + "."
	case xdr.PathPaymentStrictSendResultCodePathPaymentStrictSendNoIssuer:
		var sentence string
		d.NoIssuerAsset, d.NoIssuerHop, sentence = route.noIssuer(res.NoIssuer)
		d.Context += sentence
		if d.NoIssuerAsset != "" {
			d.Hint = fmt.Sprintf("Check that %s is issued on this network, or choose a route that avoids it.", d.NoIssuerAsset)
		}
	case xdr.PathPaymentStrictSendResultCodePathPaymentStrictSendMalformed:
		d.Context += nonPositiveAmount("sendAmount", op.SendAmount) + nonPositiveAmount("destMin", op.DestMin)
	}
	return d
}

// DestMinShortfall returns how many stroops a quoted delivery falls short of
// destMin, or 0 when the quote meets it.
func (d *PathPaymentStrictSendDiagnosis) DestMinShortfall(quoted xdr.Int64) int64 {
	if d == nil || int64(quoted) >= d.DestMinStroops {
		return 0
	}
	return d.DestMinStroops - int64(quoted)
}
