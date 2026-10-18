package lens

import (
	"fmt"
	"reflect"
	"strconv"

	"github.com/stellar/go-stellar-sdk/amount"
	"github.com/stellar/go-stellar-sdk/xdr"
)

// Liquidity pool ledger entries: LiquidityPoolEntry and the constant product
// parameters that describe what a pool trades and what it charges.
//
// The generic walker decodes every field already. What it cannot know is that
// Fee is a protocol fee in basis points and that AssetA/AssetB form a trading
// pair, so the enricher registered here renders both in one line. The share
// fields are stroop-denominated like any other amount but carry pool share
// tokens rather than a currency, so they get their own notes instead of the
// generic XLM-style amount rendering.

func init() {
	registerEnricher(reflect.TypeOf(xdr.LiquidityPoolConstantProductParameters{}), enrichPoolParameters)
	registerFieldNote(reflect.TypeOf(xdr.LiquidityPoolEntryConstantProduct{}), "TotalPoolShares", poolShareBalanceNote)
	registerFieldNote(reflect.TypeOf(xdr.LiquidityPoolEntryConstantProduct{}), "PoolSharesTrustLineCount", poolSharesTrustLineCountNote)
	nodeHooks[reflect.TypeOf(xdr.LiquidityPoolEntry{})] = decorateLiquidityPoolEntry
}

// describePoolParameters renders a constant product pool's configuration as
// one line: the asset pair and the trading fee, for example
// "native / USDC:GA... @ 0.3%".
func describePoolParameters(p xdr.LiquidityPoolConstantProductParameters) string {
	return fmt.Sprintf("%s / %s @ %s", p.AssetA.StringCanonical(), p.AssetB.StringCanonical(), poolFeePercent(p.Fee))
}

// poolFeePercent converts the protocol fee, which XDR stores in basis points
// (30 means 0.30%), into a percentage. %g keeps whole and one-decimal fees
// free of trailing zeros while still rendering the smallest 1 bp fee as
// "0.01%".
func poolFeePercent(fee xdr.Int32) string {
	return fmt.Sprintf("%g%%", float64(fee)/100)
}

// enrichPoolParameters collapses the parameters subtree into the pair and fee
// line. Everything else in the entry — reserves, share count — stays
// structured and amount-formatted by the generic walker.
func enrichPoolParameters(rv reflect.Value) (string, any, bool) {
	p, valid := rv.Interface().(xdr.LiquidityPoolConstantProductParameters)
	if !valid {
		return "", nil, false
	}
	return "", describePoolParameters(p), true
}

// poolShareBalanceNote renders total pool shares in their two useful forms:
// the decimal token amount a wallet or DEX UI shows, and the raw stroop count
// the ledger stores. Pool shares use the same 7-decimal scale as other
// Stellar amounts, so amount.String applies; the wording names the token
// because saying "XLM" would be wrong.
func poolShareBalanceNote(rv reflect.Value) (string, bool) {
	v, ok := rv.Interface().(xdr.Int64)
	if !ok || v < 0 {
		return "", false
	}
	return amount.String(v) + " pool share tokens (stroops: " + strconv.FormatInt(int64(v), 10) + ")", true
}

// poolSharesTrustLineCountNote explains the trustline counter that sits beside
// the share supply: it is the number of accounts holding shares in the pool,
// which is what makes a pool's distribution readable at a glance.
func poolSharesTrustLineCountNote(rv reflect.Value) (string, bool) {
	v, ok := rv.Interface().(xdr.Int64)
	if !ok || v < 0 {
		return "", false
	}
	noun := "trustlines"
	if v == 1 {
		noun = "trustline"
	}
	return "held by " + strconv.FormatInt(int64(v), 10) + " " + noun, true
}

// decorateLiquidityPoolEntry annotates the pool id leaf with the asset pair
// and fee from the entry body. The id's hex is already its value; the note is
// the one place a reader can learn which market the constant product pool is.
// A malformed entry with no constant product arm is left untouched.
func decorateLiquidityPoolEntry(n *Node, v any) {
	entry, ok := v.(xdr.LiquidityPoolEntry)
	if !ok {
		return
	}
	idNode := n.Child("LiquidityPoolId")
	cp := entry.Body.ConstantProduct
	if idNode == nil || cp == nil {
		return
	}
	idNode.Note = "constant product pool: " + describePoolParameters(cp.Params)
}
