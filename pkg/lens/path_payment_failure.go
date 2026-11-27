package lens

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/stellar/go-stellar-sdk/amount"
	"github.com/stellar/go-stellar-sdk/xdr"
)

// withPathPaymentFailureDetail folds the code-specific path payment diagnosis
// into a failed operation's explanation.
//
// It runs from attachResult rather than deepenOperationFailure because the
// asset behind a no-issuer failure is carried only by the operation result.
// slippage is the existing slippage diagnosis, which is nil for every code
// other than the slippage and liquidity failures; it is created here when
// absent so that any failed path payment reports a pathPayment object.
func withPathPaymentFailureDetail(slippage *PathPaymentSlippageDiagnosis, op xdr.Operation, opIndex int, opRes xdr.OperationResult, r *Reason) *PathPaymentSlippageDiagnosis {
	if r.Success || opRes.Tr == nil {
		return slippage
	}
	switch op.Body.Type {
	case xdr.OperationTypePathPaymentStrictReceive:
		d := DiagnosePathPaymentStrictReceiveFailure(op.Body.PathPaymentStrictReceiveOp, opRes.Tr.PathPaymentStrictReceiveResult)
		if d == nil {
			return slippage
		}
		if slippage == nil {
			slippage = &PathPaymentSlippageDiagnosis{
				OperationIndex: opIndex,
				PaymentType:    "strict_receive",
				SendAsset:      d.SendAsset,
				DestAsset:      d.DestAsset,
				SendMax:        d.SendMax,
				Amount:         d.DestAmount,
				Destination:    shortAddr(d.Destination),
				Hops:           d.Hops,
				Summary:        r.Summary,
				Explanation:    d.Context,
			}
		}
		slippage.StrictReceive = d
		applyPathPaymentFailure(slippage, d.Context, d.Hint, r)
	case xdr.OperationTypePathPaymentStrictSend:
		d := DiagnosePathPaymentStrictSendFailure(op.Body.PathPaymentStrictSendOp, opRes.Tr.PathPaymentStrictSendResult)
		if d == nil {
			return slippage
		}
		if slippage == nil {
			slippage = &PathPaymentSlippageDiagnosis{
				OperationIndex: opIndex,
				PaymentType:    "strict_send",
				SendAsset:      d.SendAsset,
				DestAsset:      d.DestAsset,
				DestMin:        d.DestMin,
				Amount:         d.SendAmount,
				Destination:    shortAddr(d.Destination),
				Hops:           d.Hops,
				Summary:        r.Summary,
				Explanation:    d.Context,
			}
		}
		slippage.StrictSend = d
		applyPathPaymentFailure(slippage, d.Context, d.Hint, r)
	}
	return slippage
}

// applyPathPaymentFailure records the diagnosis on the Reason. A diagnosis
// without its own hint leaves the slippage hint in place.
func applyPathPaymentFailure(slippage *PathPaymentSlippageDiagnosis, context, hint string, r *Reason) {
	r.Context = context
	if hint != "" {
		slippage.Hint = hint
	}
}

// pathPaymentRoute is the asset route shared by both path payment diagnoses.
type pathPaymentRoute struct {
	path []string
	hops []PathPaymentHop
	line string
}

// newPathPaymentRoute labels every asset on the route and renders it as one
// "A -> B -> C" line.
func newPathPaymentRoute(send xdr.Asset, path []xdr.Asset, dest xdr.Asset) pathPaymentRoute {
	route := pathPaymentRoute{hops: buildPathPaymentHops(send, path, dest)}
	labels := []string{assetLabel(send)}
	for _, a := range path {
		route.path = append(route.path, assetLabel(a))
		labels = append(labels, assetLabel(a))
	}
	route.line = strings.Join(append(labels, assetLabel(dest)), " -> ")
	return route
}

// books lists the order books the route trades through.
func (p pathPaymentRoute) books() string {
	books := make([]string, len(p.hops))
	for i, hop := range p.hops {
		books[i] = hop.From + " -> " + hop.To
	}
	return strings.Join(books, ", ")
}

// noIssuer resolves the asset a no-issuer result names to the first hop that
// touches it, and describes the failure. The label is empty when the result
// carries no asset.
func (p pathPaymentRoute) noIssuer(asset *xdr.Asset) (label string, hop *PathPaymentHop, sentence string) {
	if asset == nil {
		return "", nil, ""
	}
	label = assetLabel(*asset)
	for i := range p.hops {
		if p.hops[i].From == label || p.hops[i].To == label {
			found := p.hops[i]
			return label, &found, fmt.Sprintf(" The issuer of %s does not exist, which blocks hop %d (%s -> %s).",
				label, found.Index+1, found.From, found.To)
		}
	}
	return label, nil, fmt.Sprintf(" The issuer of %s does not exist.", label)
}

// pathPaymentRate renders numerator/denominator to stroop precision, or ""
// when either side is not positive and the rate would mean nothing.
func pathPaymentRate(numerator, denominator xdr.Int64) string {
	if numerator <= 0 || denominator <= 0 {
		return ""
	}
	return new(big.Rat).SetFrac64(int64(numerator), int64(denominator)).FloatString(7)
}

// nonPositiveAmount names a path payment amount that is not positive, which
// stellar-core rejects as malformed.
func nonPositiveAmount(name string, value xdr.Int64) string {
	if value > 0 {
		return ""
	}
	return fmt.Sprintf(" %s is %s, but it must be positive.", name, amount.String(value))
}
