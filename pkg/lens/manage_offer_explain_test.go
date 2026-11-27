package lens_test

import (
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

func TestDexExplainDiagnoseManageOffer(t *testing.T) {
	t.Parallel()

	native := xdr.Asset{Type: xdr.AssetTypeAssetTypeNative}
	usdc := creditAsset(t, "USDC", testIssuer)
	usdcLabel := "USDC:" + testIssuer
	good := xdr.Price{N: 1, D: 2}

	tests := []struct {
		name    string
		selling xdr.Asset
		buying  xdr.Asset
		price   xdr.Price
		amount  xdr.Int64
		offerID xdr.Int64
		code    string
		want    []string
	}{
		{name: "identical assets", selling: usdc, buying: usdc, price: good, amount: 1, code: "manage_sell_offer_malformed", want: []string{"same asset (" + usdcLabel + ")"}},
		{name: "zero numerator", selling: native, buying: usdc, price: xdr.Price{N: 0, D: 1}, amount: 1, code: "manage_sell_offer_malformed", want: []string{"price 0/1 is non-positive"}},
		{name: "negative denominator", selling: native, buying: usdc, price: xdr.Price{N: 1, D: -1}, amount: 1, code: "manage_buy_offer_malformed", want: []string{"price 1/-1 is non-positive"}},
		{name: "negative amount", selling: native, buying: usdc, price: good, amount: -1, code: "manage_sell_offer_malformed", want: []string{"amount -0.0000001 is negative"}},
		{name: "negative offer id", selling: native, buying: usdc, price: good, amount: 1, offerID: -4, code: "manage_sell_offer_malformed", want: []string{"offer id -4 is negative"}},
		{name: "delete without an id", selling: native, buying: usdc, price: good, code: "manage_sell_offer_malformed", want: []string{"offer id 0 does not identify one"}},
		{name: "every problem is reported", selling: usdc, buying: usdc, price: xdr.Price{}, amount: 1, code: "manage_sell_offer_malformed", want: []string{"same asset", "non-positive"}},
		{name: "constant form is accepted", selling: usdc, buying: usdc, price: good, amount: 1, code: "ManageSellOfferResultCodeManageSellOfferMalformed", want: []string{"same asset"}},
		{name: "delete of a missing offer", selling: native, buying: usdc, price: good, offerID: 77, code: "manage_sell_offer_not_found", want: []string{"Offer 77 cannot be deleted", "does not exist on the source account"}},
		{name: "update of a missing offer", selling: native, buying: usdc, price: good, amount: 1, offerID: 77, code: "manage_buy_offer_not_found", want: []string{"Offer 77 cannot be updated"}},
		{name: "cross self", selling: native, buying: usdc, price: good, amount: 1, code: "manage_sell_offer_cross_self", want: []string{"price 1/2", "same account", "XLM/" + usdcLabel}},
		{name: "sell no trust", selling: usdc, buying: native, price: good, amount: 1, code: "manage_sell_offer_sell_no_trust", want: []string{"no trustline for " + usdcLabel + ", the asset it is selling"}},
		{name: "buy no trust", selling: native, buying: usdc, price: good, amount: 1, code: "manage_buy_offer_buy_no_trust", want: []string{"no trustline for " + usdcLabel + ", the asset it is buying"}},
		{name: "sell not authorized", selling: usdc, buying: native, price: good, amount: 1, code: "manage_sell_offer_sell_not_authorized", want: []string{"authorised this account to sell"}},
		{name: "buy not authorized", selling: native, buying: usdc, price: good, amount: 1, code: "manage_sell_offer_buy_not_authorized", want: []string{"authorised this account to buy"}},
		{name: "sell no issuer", selling: usdc, buying: native, price: good, amount: 1, code: "manage_sell_offer_sell_no_issuer", want: []string{"the asset being sold"}},
		{name: "buy no issuer", selling: native, buying: usdc, price: good, amount: 1, code: "manage_sell_offer_buy_no_issuer", want: []string{"the asset being bought"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := lens.DiagnoseManageOffer(tc.selling, tc.buying, tc.price, tc.amount, tc.offerID, tc.code)
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("DiagnoseManageOffer() = %q, want it to contain %q", got, want)
				}
			}
		})
	}
}

func TestDexExplainDiagnoseManageOfferNoDiagnosis(t *testing.T) {
	t.Parallel()

	native := xdr.Asset{Type: xdr.AssetTypeAssetTypeNative}
	usdc := creditAsset(t, "USDC", testIssuer)

	for _, code := range []string{"manage_sell_offer_success", "manage_sell_offer_underfunded", "manage_buy_offer_low_reserve"} {
		if got := lens.DiagnoseManageOffer(native, usdc, xdr.Price{N: 1, D: 1}, 1, 0, code); got != "" {
			t.Errorf("DiagnoseManageOffer(%s) = %q, want empty", code, got)
		}
	}
}

// TestDexExplainManageOfferPair checks both operation types on the public
// explain path.
func TestDexExplainManageOfferPair(t *testing.T) {
	t.Parallel()

	usdc := creditAsset(t, "USDC", testIssuer)
	native := xdr.Asset{Type: xdr.AssetTypeAssetTypeNative}

	tests := []struct {
		name string
		op   xdr.OperationBody
		tr   xdr.OperationResultTr
		want string
	}{
		{
			name: "sell offer with a non-positive price",
			op: xdr.OperationBody{
				Type:              xdr.OperationTypeManageSellOffer,
				ManageSellOfferOp: &xdr.ManageSellOfferOp{Selling: native, Buying: usdc, Amount: 1, Price: xdr.Price{N: 0, D: 1}},
			},
			tr: xdr.OperationResultTr{
				Type:                  xdr.OperationTypeManageSellOffer,
				ManageSellOfferResult: &xdr.ManageSellOfferResult{Code: xdr.ManageSellOfferResultCodeManageSellOfferMalformed},
			},
			want: "price 0/1 is non-positive",
		},
		{
			name: "buy offer deleting a missing offer",
			op: xdr.OperationBody{
				Type:             xdr.OperationTypeManageBuyOffer,
				ManageBuyOfferOp: &xdr.ManageBuyOfferOp{Selling: native, Buying: usdc, Price: xdr.Price{N: 1, D: 1}, OfferId: 9},
			},
			tr: xdr.OperationResultTr{
				Type:                 xdr.OperationTypeManageBuyOffer,
				ManageBuyOfferResult: &xdr.ManageBuyOfferResult{Code: xdr.ManageBuyOfferResultCodeManageBuyOfferNotFound},
			},
			want: "Offer 9 cannot be deleted",
		},
		{
			name: "buy offer with identical assets",
			op: xdr.OperationBody{
				Type:             xdr.OperationTypeManageBuyOffer,
				ManageBuyOfferOp: &xdr.ManageBuyOfferOp{Selling: usdc, Buying: usdc, BuyAmount: 1, Price: xdr.Price{N: 1, D: 1}},
			},
			tr: xdr.OperationResultTr{
				Type:                 xdr.OperationTypeManageBuyOffer,
				ManageBuyOfferResult: &xdr.ManageBuyOfferResult{Code: xdr.ManageBuyOfferResultCodeManageBuyOfferMalformed},
			},
			want: "same asset",
		},
		{
			name: "underfunded keeps the generic explanation",
			op: xdr.OperationBody{
				Type:              xdr.OperationTypeManageSellOffer,
				ManageSellOfferOp: &xdr.ManageSellOfferOp{Selling: native, Buying: usdc, Amount: 1, Price: xdr.Price{N: 1, D: 1}},
			},
			tr: xdr.OperationResultTr{
				Type:                  xdr.OperationTypeManageSellOffer,
				ManageSellOfferResult: &xdr.ManageSellOfferResult{Code: xdr.ManageSellOfferResultCodeManageSellOfferUnderfunded},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			env, err := lens.DecodeAs(buildEnvelope(t, noMemo(), noCond(), xdr.Operation{Body: tc.op}), "TransactionEnvelope")
			if err != nil {
				t.Fatalf("DecodeAs(env): %v", err)
			}
			tr := tc.tr
			txRes := xdr.TransactionResult{
				FeeCharged: 100,
				Result: xdr.TransactionResultResult{
					Code:    xdr.TransactionResultCodeTxFailed,
					Results: &[]xdr.OperationResult{{Code: xdr.OperationResultCodeOpInner, Tr: &tr}},
				},
			}
			res, err := lens.DecodeAs(mustMarshal(t, txRes), "TransactionResult")
			if err != nil {
				t.Fatalf("DecodeAs(res): %v", err)
			}
			summary, err := lens.ExplainPair(env, res)
			if err != nil {
				t.Fatalf("ExplainPair: %v", err)
			}
			got := summary.Operations[0].Result.Context
			if tc.want == "" && got != "" {
				t.Errorf("Context = %q, want empty", got)
			}
			if !strings.Contains(got, tc.want) {
				t.Errorf("Context = %q, want it to contain %q", got, tc.want)
			}
		})
	}
}
