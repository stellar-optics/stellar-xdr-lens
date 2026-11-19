package lens

import (
	"fmt"
	"math/big"
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

// DiagnosePoolDeposit explains a failed liquidity pool deposit from the
// operation's own amounts and price bounds. The protocol prices a deposit as
// asset A per asset B, so the ratio implied by the offered amounts is
// maxAmountA/maxAmountB. It returns "" for a nil operation and for codes the
// operation's parameters cannot shed any light on.
func DiagnosePoolDeposit(op *xdr.LiquidityPoolDepositOp, code xdr.LiquidityPoolDepositResultCode) string {
	if op == nil {
		return ""
	}
	switch code {
	case xdr.LiquidityPoolDepositResultCodeLiquidityPoolDepositMalformed:
		if reason, malformed := liquidityPoolDepositMalformedReason(*op); malformed {
			return "the deposit violates a boundary rule: " + reason + "."
		}
	case xdr.LiquidityPoolDepositResultCodeLiquidityPoolDepositBadPrice:
		return diagnosePoolDepositPrice(*op)
	case xdr.LiquidityPoolDepositResultCodeLiquidityPoolDepositPoolFull:
		return fmt.Sprintf(
			"depositing up to %s of asset A and %s of asset B would take a pool reserve or the total pool shares past the protocol maximum of 2^63-1 stroops.",
			amount.String(op.MaxAmountA), amount.String(op.MaxAmountB))
	}
	return ""
}

// diagnosePoolDepositPrice compares the deposit's implied price with its
// accepted window. Bounds that cannot form a window yield "": those are
// malformed, not a price drift.
func diagnosePoolDepositPrice(o xdr.LiquidityPoolDepositOp) string {
	if _, malformed := liquidityPoolDepositMalformedReason(o); malformed {
		return ""
	}
	implied := fmt.Sprintf("the implied deposit price A/B is %s (maxAmountA %s / maxAmountB %s)",
		poolPriceText(int64(o.MaxAmountA), int64(o.MaxAmountB)),
		amount.String(o.MaxAmountA), amount.String(o.MaxAmountB))
	minPrice := poolPriceText(int64(o.MinPrice.N), int64(o.MinPrice.D))
	maxPrice := poolPriceText(int64(o.MaxPrice.N), int64(o.MaxPrice.D))

	const outsideWindow = "price ratio drift outside the accepted window caused the rejection. An empty pool takes these amounts as offered; a funded pool takes them at its reserve ratio, which must also sit inside the window."
	switch depositAmountsVsWindow(o) {
	case -1:
		return fmt.Sprintf("%s, below minPrice %s (maxPrice is %s); %s", implied, minPrice, maxPrice, outsideWindow)
	case 1:
		return fmt.Sprintf("%s, above maxPrice %s (minPrice is %s); %s", implied, maxPrice, minPrice, outsideWindow)
	}
	return fmt.Sprintf(
		"%s, inside the window [%s, %s], so the offered amounts are not at fault: the pool's reserve ratio drifted outside the window between composing the transaction and consensus.",
		implied, minPrice, maxPrice)
}

// poolPriceText renders a positive ratio as a reduced fraction with its
// decimal value. The fraction is exact; the decimal is rounded for reading.
func poolPriceText(n, d int64) string {
	r := big.NewRat(n, d)
	return fmt.Sprintf("%s/%s (≈%s)", r.Num(), r.Denom(), r.FloatString(7))
}

// withPoolDepositDiagnosis attaches DiagnosePoolDeposit's finding to a failed
// deposit's Reason.
func withPoolDepositDiagnosis(o xdr.LiquidityPoolDepositOp, r Reason) Reason {
	for _, code := range []xdr.LiquidityPoolDepositResultCode{
		xdr.LiquidityPoolDepositResultCodeLiquidityPoolDepositMalformed,
		xdr.LiquidityPoolDepositResultCodeLiquidityPoolDepositBadPrice,
		xdr.LiquidityPoolDepositResultCodeLiquidityPoolDepositPoolFull,
	} {
		if code.String() == r.Constant {
			r.Context = DiagnosePoolDeposit(&o, code)
			break
		}
	}
	return r
}

// DiagnosePoolWithdraw explains a failed liquidity pool withdrawal from the
// pool shares the operation burns and the minimum returns it demands. It
// returns "" for a nil operation and for codes the operation's parameters
// cannot shed any light on.
func DiagnosePoolWithdraw(op *xdr.LiquidityPoolWithdrawOp, code xdr.LiquidityPoolWithdrawResultCode) string {
	if op == nil {
		return ""
	}
	switch code {
	case xdr.LiquidityPoolWithdrawResultCodeLiquidityPoolWithdrawMalformed:
		if op.Amount <= 0 || op.MinAmountA < 0 || op.MinAmountB < 0 {
			return fmt.Sprintf(
				"the withdrawal burns %s pool shares with minimums of %s for asset A and %s for asset B; the share amount must be positive and neither minimum may be negative.",
				amount.String(op.Amount), amount.String(op.MinAmountA), amount.String(op.MinAmountB))
		}
	case xdr.LiquidityPoolWithdrawResultCodeLiquidityPoolWithdrawUnderfunded:
		return fmt.Sprintf(
			"the withdrawal burns %s pool shares, more than the source account can spend from its pool-share trustline. Check that balance and lower amount to what is available.",
			amount.String(op.Amount))
	case xdr.LiquidityPoolWithdrawResultCodeLiquidityPoolWithdrawUnderMinimum:
		return fmt.Sprintf(
			"burning %s pool shares had to return at least %s of asset A (minAmountA) and %s of asset B (minAmountB), but the pool's reserves at apply time redeem less than one of those minimums. Adjust the slippage tolerance on minAmountA/minAmountB or check the pool balance before retrying.",
			amount.String(op.Amount), amount.String(op.MinAmountA), amount.String(op.MinAmountB))
	}
	return ""
}

// withPoolWithdrawDiagnosis attaches DiagnosePoolWithdraw's finding to a
// failed withdrawal's Reason.
func withPoolWithdrawDiagnosis(o xdr.LiquidityPoolWithdrawOp, r Reason) Reason {
	for _, code := range []xdr.LiquidityPoolWithdrawResultCode{
		xdr.LiquidityPoolWithdrawResultCodeLiquidityPoolWithdrawMalformed,
		xdr.LiquidityPoolWithdrawResultCodeLiquidityPoolWithdrawUnderfunded,
		xdr.LiquidityPoolWithdrawResultCodeLiquidityPoolWithdrawUnderMinimum,
	} {
		if code.String() == r.Constant {
			r.Context = DiagnosePoolWithdraw(&o, code)
			break
		}
	}
	return r
}
