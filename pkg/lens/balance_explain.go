package lens

import (
	"fmt"
	"strings"

	"github.com/stellar/go-stellar-sdk/amount"
	"github.com/stellar/go-stellar-sdk/xdr"
)

// DefaultBaseReserve is the network base reserve in stroops used when the
// caller does not supply one. It matches the mainnet value (0.5 XLM) and is
// only a default: protocol upgrades can change the reserve, so callers with a
// ledger header should pass its BaseReserve instead.
const DefaultBaseReserve uint32 = 5000000

// BalanceDiagnosis explains whether an account can spend a requested amount
// of native XLM after selling liabilities and the minimum reserve are set
// aside.
type BalanceDiagnosis struct {
	// Balance is the account's total native balance in stroops.
	Balance int64 `json:"balance"`
	// SellingLiabilities are stroops locked in open offers and cannot be spent.
	SellingLiabilities int64 `json:"sellingLiabilities"`
	// ReserveRequirement is the minimum balance the account must retain, in stroops.
	ReserveRequirement int64 `json:"reserveRequirement"`
	// AvailableBalance is Balance minus SellingLiabilities minus ReserveRequirement.
	AvailableBalance int64 `json:"availableBalance"`
	// AmountToSpend is the requested spend in stroops.
	AmountToSpend int64 `json:"amountToSpend"`
	// Shortfall is AmountToSpend minus AvailableBalance when insufficient, else 0.
	Shortfall int64 `json:"shortfall"`
	// Sufficient reports whether the spend fits inside the available balance.
	Sufficient bool `json:"sufficient"`
	// NumSubEntries is the account's subentry count used for the reserve.
	NumSubEntries uint32 `json:"numSubEntries"`
	// BaseReserve is the per-entry reserve used for the computation, in stroops.
	BaseReserve uint32 `json:"baseReserve"`
	// Summary is a one-paragraph plain-English account of the math.
	Summary string `json:"summary"`
	// Hint suggests what to do about a shortfall. Empty when sufficient.
	Hint string `json:"hint,omitempty"`
}

// AccountSellingLiabilities returns the selling liabilities locked on an
// account entry, or 0 when the entry carries no liabilities extension.
//
// It never panics: a nil entry yields 0 and unpopulated extensions are treated
// as zero liabilities.
func AccountSellingLiabilities(entry *xdr.AccountEntry) int64 {
	if entry == nil {
		return 0
	}
	// The liabilities live in Ext.V1; a V0 account predates liabilities.
	// Values are clamped at zero so a corrupt negative cannot invert the math.
	if entry.Ext.V == 1 && entry.Ext.V1 != nil {
		if v := int64(entry.Ext.V1.Liabilities.Selling); v > 0 {
			return v
		}
	}
	return 0
}

// AccountBuyingLiabilities returns the buying liabilities on an account entry,
// or 0 when the entry carries no liabilities extension.
func AccountBuyingLiabilities(entry *xdr.AccountEntry) int64 {
	if entry == nil {
		return 0
	}
	if entry.Ext.V == 1 && entry.Ext.V1 != nil {
		if v := int64(entry.Ext.V1.Liabilities.Buying); v > 0 {
			return v
		}
	}
	return 0
}

// AccountSponsorshipCounts returns the (sponsoring, sponsored) counters from
// the V2 extension, or zeros when the account has no sponsorship data.
func AccountSponsorshipCounts(entry *xdr.AccountEntry) (sponsoring, sponsored uint32) {
	if entry == nil {
		return 0, 0
	}
	if entry.Ext.V != 1 || entry.Ext.V1 == nil {
		return 0, 0
	}
	if entry.Ext.V1.Ext.V != 2 || entry.Ext.V1.Ext.V2 == nil {
		return 0, 0
	}
	return uint32(entry.Ext.V1.Ext.V2.NumSponsoring), uint32(entry.Ext.V1.Ext.V2.NumSponsored)
}

// ReserveRequirement computes the minimum native balance an account must
// retain: (2 + numSubEntries + numSponsoring - numSponsored) * baseReserve.
//
// Sponsored entries are paid for by another account, so they reduce the
// requirement; entries this account sponsors for others increase it. The result
// saturates at zero rather than underflowing when sponsored exceeds the rest.
func ReserveRequirement(numSubEntries, numSponsoring, numSponsored uint32, baseReserve uint32) int64 {
	entries := int64(2) + int64(numSubEntries) + int64(numSponsoring) - int64(numSponsored)
	if entries < 0 {
		entries = 0
	}
	return entries * int64(baseReserve)
}

// AccountReserveRequirement computes the reserve for a concrete account entry
// using its subentry and sponsorship counters.
func AccountReserveRequirement(entry *xdr.AccountEntry, baseReserve uint32) int64 {
	if entry == nil {
		return int64(2) * int64(baseReserve)
	}
	sponsoring, sponsored := AccountSponsorshipCounts(entry)
	return ReserveRequirement(uint32(entry.NumSubEntries), sponsoring, sponsored, baseReserve)
}

// AvailableBalance computes the spendable native balance: the total minus what
// is locked in selling liabilities minus what must stay as reserve.
func AvailableBalance(balance, sellingLiabilities, reserveRequirement int64) int64 {
	return balance - sellingLiabilities - reserveRequirement
}

// DiagnoseBalance explains whether the account described by entry can spend
// amountToSpend stroops of native XLM given baseReserve.
//
// A baseReserve of 0 selects DefaultBaseReserve. A negative amountToSpend is
// rejected with ErrInvalidArgument. The computation is purely local: no
// network, no clock, no environment.
func DiagnoseBalance(entry *xdr.AccountEntry, baseReserve uint32, amountToSpend int64) (*BalanceDiagnosis, error) {
	if entry == nil {
		return nil, fmt.Errorf("%w: cannot diagnose balance of nil AccountEntry", ErrInvalidArgument)
	}
	if amountToSpend < 0 {
		return nil, fmt.Errorf("%w: amount to spend must not be negative: %d", ErrInvalidArgument, amountToSpend)
	}
	if baseReserve == 0 {
		baseReserve = DefaultBaseReserve
	}
	balance := int64(entry.Balance)
	selling := AccountSellingLiabilities(entry)
	reserve := AccountReserveRequirement(entry, baseReserve)
	available := AvailableBalance(balance, selling, reserve)

	d := &BalanceDiagnosis{
		Balance:            balance,
		SellingLiabilities: selling,
		ReserveRequirement: reserve,
		AvailableBalance:   available,
		AmountToSpend:      amountToSpend,
		NumSubEntries:      uint32(entry.NumSubEntries),
		BaseReserve:        baseReserve,
		Sufficient:         available >= amountToSpend,
	}
	if !d.Sufficient {
		d.Shortfall = amountToSpend - available
	}
	d.Summary = renderBalanceSummary(d)
	if !d.Sufficient {
		d.Hint = renderBalanceHint(d)
	}
	return d, nil
}

// DiagnoseBalanceFromValue diagnoses the spendable balance of a decoded
// AccountEntry Value. The Value must wrap *xdr.AccountEntry (as produced by
// DecodeAs with type "AccountEntry" or by decoding a ledger entry fixture).
func DiagnoseBalanceFromValue(v *Value, baseReserve uint32, amountToSpend int64) (*BalanceDiagnosis, error) {
	if v == nil {
		return nil, fmt.Errorf("%w: balance diagnosis requires a Value", ErrInvalidArgument)
	}
	entry, ok := v.Raw.(*xdr.AccountEntry)
	if !ok {
		return nil, fmt.Errorf("%w: balance diagnosis needs *xdr.AccountEntry, got %T", ErrInvalidArgument, v.Raw)
	}
	return DiagnoseBalance(entry, baseReserve, amountToSpend)
}

// DiagnoseTrustlineSpend explains whether a trustline balance covers a spend
// of amountToSpend units after selling liabilities are excluded. Trustlines
// carry no base reserve, so only liabilities reduce the balance.
func DiagnoseTrustlineSpend(entry *xdr.TrustLineEntry, amountToSpend int64) (*BalanceDiagnosis, error) {
	if entry == nil {
		return nil, fmt.Errorf("%w: cannot diagnose balance of nil TrustLineEntry", ErrInvalidArgument)
	}
	if amountToSpend < 0 {
		return nil, fmt.Errorf("%w: amount to spend must not be negative: %d", ErrInvalidArgument, amountToSpend)
	}
	var selling int64
	if entry.Ext.V == 1 && entry.Ext.V1 != nil {
		if v := int64(entry.Ext.V1.Liabilities.Selling); v > 0 {
			selling = v
		}
	}
	balance := int64(entry.Balance)
	available := balance - selling
	d := &BalanceDiagnosis{
		Balance:            balance,
		SellingLiabilities: selling,
		ReserveRequirement: 0,
		AvailableBalance:   available,
		AmountToSpend:      amountToSpend,
		Sufficient:         available >= amountToSpend,
	}
	if !d.Sufficient {
		d.Shortfall = amountToSpend - available
	}
	if d.Sufficient {
		d.Summary = fmt.Sprintf("Trustline holds %s with %s locked in selling liabilities, leaving %s available; spending %s fits.",
			stroopsToXLM(balance), stroopsToXLM(selling), stroopsToXLM(available), stroopsToXLM(amountToSpend))
	} else {
		d.Summary = fmt.Sprintf("Trustline holds %s with %s locked in selling liabilities, leaving %s available; spending %s falls short by %s.",
			stroopsToXLM(balance), stroopsToXLM(selling), stroopsToXLM(available), stroopsToXLM(amountToSpend), stroopsToXLM(d.Shortfall))
		d.Hint = "Reduce the amount, or cancel open offers backed by this trustline to release selling liabilities."
	}
	return d, nil
}

func renderBalanceSummary(d *BalanceDiagnosis) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Account holds %s (%d stroops)", stroopsToXLM(d.Balance), d.Balance)
	if d.SellingLiabilities > 0 {
		fmt.Fprintf(&b, " with %s (%d stroops) locked in selling liabilities",
			stroopsToXLM(d.SellingLiabilities), d.SellingLiabilities)
	}
	fmt.Fprintf(&b, "; reserve requires %s (%d stroops) for %d subentr%s, leaving %s (%d stroops) available.",
		stroopsToXLM(d.ReserveRequirement), d.ReserveRequirement, d.NumSubEntries, pluralEnding(d.NumSubEntries),
		stroopsToXLM(d.AvailableBalance), d.AvailableBalance)
	if d.Sufficient {
		fmt.Fprintf(&b, " Spending %s (%d stroops) fits inside the available balance.",
			stroopsToXLM(d.AmountToSpend), d.AmountToSpend)
	} else {
		fmt.Fprintf(&b, " Spending %s (%d stroops) falls short by %s (%d stroops).",
			stroopsToXLM(d.AmountToSpend), d.AmountToSpend, stroopsToXLM(d.Shortfall), d.Shortfall)
	}
	return b.String()
}

func renderBalanceHint(d *BalanceDiagnosis) string {
	parts := []string{
		fmt.Sprintf("Fund the account with at least %s (%d stroops),", stroopsToXLM(d.Shortfall), d.Shortfall),
		"lower the amount,",
	}
	if d.SellingLiabilities > 0 {
		parts = append(parts, "cancel open sell offers to release selling liabilities, or")
	}
	parts = append(parts, "remove unneeded trustlines, offers or data entries to lower the reserve.")
	return strings.Join(parts, " ")
}

func stroopsToXLM(stroops int64) string {
	return amount.StringFromInt64(stroops) + " XLM"
}

// NetBalanceMathNote returns a one-line summary of a BalanceDelta, e.g.
// "GA… XLM: 10.0000000 XLM → 9.9999000 XLM (−100 stroops)".
// When before is unknown it replaces the before value with "?".
func NetBalanceMathNote(d BalanceDelta) string {
	sign := "+"
	if d.Delta < 0 {
		sign = ""
	}
	before := "?"
	if d.BeforeKnown {
		before = formatNetBalance(d.Asset, d.Before)
	}
	after := formatNetBalance(d.Asset, d.After)
	switch d.Asset {
	case "XLM":
		return fmt.Sprintf("%s %s: %s → %s (%s%d stroops)",
			d.Holder, d.Asset, before, after, sign, d.Delta)
	default:
		return fmt.Sprintf("%s %s: %s → %s (%s%d)",
			d.Holder, d.Asset, before, after, sign, d.Delta)
	}
}

func formatNetBalance(asset string, v int64) string {
	if asset == "XLM" {
		return stroopsToXLM(v)
	}
	return fmt.Sprintf("%d", v)
}

func pluralEnding(n uint32) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}
