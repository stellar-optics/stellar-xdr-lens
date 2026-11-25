package lens

import (
	"fmt"
	"reflect"

	"github.com/stellar/go-stellar-sdk/xdr"
)

func init() {
	enricherFor[reflect.TypeOf(xdr.Price{})] = enrichPrice
	nodeHooks[reflect.TypeOf(xdr.ClaimOfferAtom{})] = func(n *Node, v any) { decorateClaimOfferAtom(n, v) }
	nodeHooks[reflect.TypeOf(xdr.ClaimOfferAtomV0{})] = func(n *Node, v any) { decorateClaimOfferAtomV0(n, v) }
}

// enrichPrice renders an order-book price as an "n/d" fraction together with
// its decimal value, e.g. "7/3 (= 2.3333333)". The rendering is the scalar
// value itself (like a strkey address), so there is no separate note.
// A price with a zero denominator is malformed, so it declines and lets the
// generic tree walker render the raw numerator and denominator instead of
// dividing by zero.
func enrichPrice(rv reflect.Value) (display string, raw any, ok bool) {
	p, valid := rv.Interface().(xdr.Price)
	if !valid || p.D == 0 {
		return "", nil, false
	}
	// The raw value is the human rendering: machine output keeps the
	// fraction, and the decimal is included for quick reading.
	return "", fmt.Sprintf("%s (= %.7f)", priceFraction(p), float64(p.N)/float64(p.D)), true
}

// priceFraction renders a price as "n/d". A zero denominator renders as
// "0/0" rather than panicking; callers should treat that as malformed.
func priceFraction(p xdr.Price) string {
	if p.D == 0 {
		return "0/0"
	}
	return fmt.Sprintf("%d/%d", p.N, p.D)
}

// decorateClaimOfferAtom sets a Note on a ClaimOfferAtom node summarising
// the trade: seller strkey, offer ID, and assets exchanged.
func decorateClaimOfferAtom(n *Node, v any) {
	atom, ok := v.(xdr.ClaimOfferAtom)
	if !ok {
		return
	}
	addr, err := atom.SellerId.GetAddress()
	if err != nil {
		return
	}
	n.Note = fmt.Sprintf("seller %s offer %d", addr, atom.OfferId)
}

// decorateClaimOfferAtomV0 sets a Note on a ClaimOfferAtomV0 node summarising
// the legacy trade record using its raw ed25519 seller key.
func decorateClaimOfferAtomV0(n *Node, v any) {
	atom, ok := v.(xdr.ClaimOfferAtomV0)
	if !ok {
		return
	}
	sellerHex := fmt.Sprintf("%x", atom.SellerEd25519[:8])
	n.Note = fmt.Sprintf("seller ed25519 %s… offer %d", sellerHex, atom.OfferId)
}
