package lens

import (
	"fmt"
	"strings"

	"github.com/stellar/go-stellar-sdk/amount"
	"github.com/stellar/go-stellar-sdk/xdr"
)

// DiagnoseManageOffer explains a failed ManageSellOffer or ManageBuyOffer from
// the operation's own fields. code is the result code in short or constant
// form. It returns "" for a success and for codes the fields cannot explain.
//
// Structural problems are reported first and together, following the checks in
// stellar-core's doCheckValid, because any one of them makes the offer invalid.
func DiagnoseManageOffer(selling, buying xdr.Asset, price xdr.Price, offerAmount xdr.Int64, offerID xdr.Int64, code string) string {
	normalized := strings.ToLower(strings.ReplaceAll(code, "_", ""))
	if strings.Contains(normalized, "success") {
		return ""
	}
	if problems := manageOfferProblems(selling, buying, price, offerAmount, offerID); len(problems) > 0 {
		return "The offer is malformed: " + strings.Join(problems, "; ") + "."
	}

	sellLabel, buyLabel := assetLabel(selling), assetLabel(buying)
	switch {
	case strings.Contains(normalized, "sellnotrust"):
		return fmt.Sprintf("The account has no trustline for %s, the asset it is selling. Open one with ChangeTrust first.", sellLabel)
	case strings.Contains(normalized, "buynotrust"):
		return fmt.Sprintf("The account has no trustline for %s, the asset it is buying. Open one with ChangeTrust first.", buyLabel)
	case strings.Contains(normalized, "sellnotauthorized"):
		return fmt.Sprintf("The issuer of %s has not authorised this account to sell it.", sellLabel)
	case strings.Contains(normalized, "buynotauthorized"):
		return fmt.Sprintf("The issuer of %s has not authorised this account to buy it.", buyLabel)
	case strings.Contains(normalized, "sellnoissuer"):
		return fmt.Sprintf("The issuer of %s, the asset being sold, does not exist on this network.", sellLabel)
	case strings.Contains(normalized, "buynoissuer"):
		return fmt.Sprintf("The issuer of %s, the asset being bought, does not exist on this network.", buyLabel)
	case strings.Contains(normalized, "crossself"):
		return fmt.Sprintf("At price %s this offer would trade against another offer from the same account on the %s/%s book. Cancel that offer or change the price so the two no longer cross.",
			priceRatio(price), sellLabel, buyLabel)
	case strings.Contains(normalized, "notfound"):
		if offerAmount == 0 {
			return fmt.Sprintf("Offer %d cannot be deleted because it does not exist on the source account. It may already have been filled or removed.", offerID)
		}
		return fmt.Sprintf("Offer %d cannot be updated because it does not exist on the source account. Use offer id 0 to create a new offer.", offerID)
	}
	return ""
}

// manageOfferProblems lists every structural rule the offer breaks.
func manageOfferProblems(selling, buying xdr.Asset, price xdr.Price, offerAmount xdr.Int64, offerID xdr.Int64) []string {
	var problems []string
	if selling.Equals(buying) {
		problems = append(problems, fmt.Sprintf("selling and buying are the same asset (%s), but an offer must trade two different assets", assetLabel(selling)))
	}
	if price.N <= 0 || price.D <= 0 {
		problems = append(problems, fmt.Sprintf("price %s is non-positive, but its numerator and denominator must both be greater than zero", priceRatio(price)))
	}
	if offerAmount < 0 {
		problems = append(problems, fmt.Sprintf("amount %s is negative", amount.String(offerAmount)))
	}
	if offerID < 0 {
		problems = append(problems, fmt.Sprintf("offer id %d is negative", offerID))
	}
	if offerAmount == 0 && offerID == 0 {
		problems = append(problems, "amount 0 deletes an offer, but offer id 0 does not identify one")
	}
	return problems
}

// deepenManageOfferFailure attaches the offer diagnosis to a failed
// ManageSellOffer or ManageBuyOffer explanation.
func deepenManageOfferFailure(op xdr.Operation, r Reason) Reason {
	var context string
	switch {
	case op.Body.ManageSellOfferOp != nil:
		o := op.Body.ManageSellOfferOp
		context = DiagnoseManageOffer(o.Selling, o.Buying, o.Price, o.Amount, o.OfferId, r.Code)
	case op.Body.ManageBuyOfferOp != nil:
		o := op.Body.ManageBuyOfferOp
		context = DiagnoseManageOffer(o.Selling, o.Buying, o.Price, o.BuyAmount, o.OfferId, r.Code)
	}
	if context != "" {
		r.Context = context
	}
	return r
}
