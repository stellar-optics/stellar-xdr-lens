package lens

import (
	"encoding/hex"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/stellar/go-stellar-sdk/amount"
	"github.com/stellar/go-stellar-sdk/strkey"
	"github.com/stellar/go-stellar-sdk/xdr"
)

// Ledger headers carry the protocol parameters and the totals that a validator
// agreed on for one ledger. Four of those fields are stroop amounts — the
// total supply, the fee pool and the two protocol parameters that set the
// price of ledger entries — and none of them is self-describing on the wire,
// which is what this file teaches the walker.
//
// Beyond the plain parameters, a header also carries:
//   - liquidity-pool control flags in the V1 extension, and
//   - upgrade votes: opaque blobs in the SCP value, each one an encoded
//     LedgerUpgrade proposing a protocol change.
//
// Both are invisible on the wire without decoding, so this file expands the
// flag bits into their canonical names and unmarshals each upgrade vote into
// the parameter it proposes. A ledger header is deliberately not collapsed
// into a single scalar by a type enricher: formatters and the differ consume
// its tree, and hiding the fields would throw away the structure that makes
// the tree useful. The human rendering is the sum of its per-field notes
// instead.
func init() {
	// TotalCoins and FeePool are Int64 stroop amounts. BaseFee and
	// BaseReserve are Uint32 protocol parameters, also denominated in
	// stroops: the base fee is the price of one operation, and the base
	// reserve is the minimum balance a ledger entry locks up.
	amountFields["TotalCoins"] = true
	amountFields["FeePool"] = true
	amountFields["BaseFee"] = true
	amountFields["BaseReserve"] = true

	// The version field is a bare Uint32; naming it as a protocol version is
	// the difference between "22" and knowing what "22" means. The same
	// field name appears in BucketMetadata, which gets the note for free.
	fieldNotes["LedgerVersion"] = protocolVersionNote

	// Liquidity-pool control flags live in the V1 header extension. The
	// containing type is keyed so other Flags fields stay untouched.
	registerFieldNote(reflect.TypeOf(xdr.LedgerHeaderExtensionV1{}), "Flags", ledgerHeaderFlagNote)

	// Upgrade votes arrive as opaque UpgradeType blobs inside the SCP value.
	// Each one is a serialized LedgerUpgrade; when it decodes, the note names
	// the proposed parameter change and the raw bytes stay machine-readable.
	// Register only the UpgradeType enricher so that decoding a raw
	// LedgerUpgrade (DecodeAs "LedgerUpgrade") still exposes the union
	// structure (Type plus arm) rather than collapsing it to a scalar note.
	registerEnricher(reflect.TypeOf(xdr.UpgradeType{}), enrichUpgradeType)

	// A nodeHook sets the summary Note on a decoded LedgerUpgrade while
	// leaving its children intact, so the tree is both human-readable at a
	// glance and still navigable field by field.
	nodeHooks[reflect.TypeOf(xdr.LedgerUpgrade{})] = decorateLedgerUpgrade
}

// protocolVersionNote renders a protocol version field as "protocol N", which
// is how the value is referred to everywhere outside the XDR itself. A zero
// version is not a released protocol, so it declines and stays a plain 0.
func protocolVersionNote(rv reflect.Value) (string, bool) {
	v, ok := rv.Interface().(xdr.Uint32)
	if !ok || v == 0 {
		return "", false
	}
	return "protocol " + strconv.FormatUint(uint64(v), 10), true
}

// ledgerHeaderFlagNames decodes the header flag bit mask into the canonical
// Horizon-style names. Unknown bits are ignored rather than guessed at.
func ledgerHeaderFlagNames(flags xdr.Uint32) []string {
	var names []string
	if flags&xdr.Uint32(xdr.LedgerHeaderFlagsDisableLiquidityPoolTradingFlag) != 0 {
		names = append(names, "disable_liquidity_pool_trading")
	}
	if flags&xdr.Uint32(xdr.LedgerHeaderFlagsDisableLiquidityPoolDepositFlag) != 0 {
		names = append(names, "disable_liquidity_pool_deposit")
	}
	if flags&xdr.Uint32(xdr.LedgerHeaderFlagsDisableLiquidityPoolWithdrawalFlag) != 0 {
		names = append(names, "disable_liquidity_pool_withdrawal")
	}
	return names
}

// ledgerHeaderFlagNote renders the set header flags for display.
func ledgerHeaderFlagNote(rv reflect.Value) (string, bool) {
	flags, ok := rv.Interface().(xdr.Uint32)
	if !ok {
		return "", false
	}
	names := ledgerHeaderFlagNames(flags)
	if len(names) == 0 {
		return "", false
	}
	return strings.Join(names, " | "), true
}

// enrichUpgradeType decodes one opaque upgrade vote into the LedgerUpgrade it
// proposes. Votes that are empty or do not parse as a known upgrade decline,
// leaving the generic hex rendering in place so a malformed proposal is still
// visible rather than silently dropped.
func enrichUpgradeType(rv reflect.Value) (string, any, bool) {
	raw, ok := rv.Interface().(xdr.UpgradeType)
	if !ok {
		return "", nil, false
	}
	if len(raw) == 0 {
		return "", nil, false
	}
	var upgrade xdr.LedgerUpgrade
	if err := xdr.SafeUnmarshal(raw, &upgrade); err != nil {
		return "", nil, false
	}
	// Use the succinct per-upgrade description for human-facing notes so that
	// decoded StellarValue Upgrades read as compact parameter descriptions
	// (e.g. "ledger flags 1"). Return the vote bytes as a hex string for
	// Node.Value so list elements render as hex; callers that need the
	// original blob can access the decoded container via the Value.Raw
	// (the Value.Raw pointer returned by DecodeAs).
	if note := describeLedgerUpgrade(&upgrade); note != "" {
		return note, hex.EncodeToString(raw), true
	}
	return "", nil, false
}

// enrichLedgerUpgrade renders an already-decoded LedgerUpgrade the same way
// as one unwrapped from an opaque vote, so the two paths never disagree.
func enrichLedgerUpgrade(rv reflect.Value) (string, any, bool) {
	upgrade, ok := rv.Interface().(xdr.LedgerUpgrade)
	if !ok {
		return "", nil, false
	}
	return describeLedgerUpgrade(&upgrade), upgrade, true
}

// decorateLedgerUpgrade is the nodeHook for xdr.LedgerUpgrade. It sets a
// human-readable Note on the root node so the upgrade is immediately
// legible while still exposing the full union structure to callers that
// want to navigate the arms.
func decorateLedgerUpgrade(n *Node, v any) {
	upgrade, ok := v.(xdr.LedgerUpgrade)
	if !ok || n.Note != "" {
		return
	}
	if note := describeLedgerUpgrade(&upgrade); note != "" {
		n.Note = note
	}
}

// describeLedgerUpgrade names the parameter a LedgerUpgrade proposes, with
// stroop amounts rendered as decimals for the two fee-like parameters. An
// arm whose payload is missing or a type that carries no short rendering
// falls back to the upgrade's snake-case name rather than guessing.
func describeLedgerUpgrade(u *xdr.LedgerUpgrade) string {
	switch u.Type {
	case xdr.LedgerUpgradeTypeLedgerUpgradeVersion:
		if v, ok := u.GetNewLedgerVersion(); ok {
			label := "Protocol " + strconv.FormatUint(uint64(v), 10)
			if name := protocolName(uint32(v)); name != "" {
				label += " (" + name + ")"
			}
			return label
		}
	case xdr.LedgerUpgradeTypeLedgerUpgradeBaseFee:
		if v, ok := u.GetNewBaseFee(); ok {
			return fmt.Sprintf("base fee %s XLM (%d stroops)",
				amount.String(xdr.Int64(int64(v))), v)
		}
	case xdr.LedgerUpgradeTypeLedgerUpgradeMaxTxSetSize:
		if v, ok := u.GetNewMaxTxSetSize(); ok {
			return "max tx set size " + strconv.FormatUint(uint64(v), 10)
		}
	case xdr.LedgerUpgradeTypeLedgerUpgradeBaseReserve:
		if v, ok := u.GetNewBaseReserve(); ok {
			return fmt.Sprintf("base reserve %s XLM (%d stroops)",
				amount.String(xdr.Int64(int64(v))), v)
		}
	case xdr.LedgerUpgradeTypeLedgerUpgradeFlags:
		if v, ok := u.GetNewFlags(); ok {
			if names := ledgerHeaderFlagNames(v); len(names) > 0 {
				return "flags " + strings.Join(names, " | ")
			}
			return "flags " + strconv.FormatUint(uint64(v), 10)
		}
	case xdr.LedgerUpgradeTypeLedgerUpgradeMaxSorobanTxSetSize:
		if v, ok := u.GetNewMaxSorobanTxSetSize(); ok {
			return "max soroban tx set size " + strconv.FormatUint(uint64(v), 10)
		}
	case xdr.LedgerUpgradeTypeLedgerUpgradeConfig:
		if k, ok := u.GetNewConfig(); ok {
			id, err := strkey.Encode(strkey.VersionByteContract, k.ContractId[:])
			if err != nil {
				id = hex.EncodeToString(k.ContractId[:])
			}
			// Match the phrasing used elsewhere: "config upgrade set: contract X,
			// content hash Y" so fixtures and human output stay consistent.
			return fmt.Sprintf("config upgrade set: contract %s, content hash %s",
				id, hex.EncodeToString(k.ContentHash[:]))
		}
	}
	name := strings.TrimPrefix(u.Type.String(), "LedgerUpgradeTypeLedgerUpgrade")
	if name == "" {
		return "upgrade type " + strconv.FormatInt(int64(u.Type), 10)
	}
	return "upgrade " + toSnake(name)
}
