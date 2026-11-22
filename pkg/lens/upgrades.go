package lens

import (
	"encoding/hex"
	"fmt"
	"reflect"

	"github.com/stellar/go-stellar-sdk/amount"
	"github.com/stellar/go-stellar-sdk/xdr"
)

// protocolName maps well-known Stellar protocol versions to their milestone
// names. Unknown versions return an empty string.
func protocolName(version uint32) string {
	names := map[uint32]string{
		12: "Inflation Removal",
		15: "Claimable Balances",
		16: "Sponsorship",
		19: "Pre-Soroban",
		20: "Soroban Launch",
	}
	return names[version]
}

// ledgerUpgradeSummary renders a LedgerUpgrade as one self-describing line,
// since a vote's type is as important as its value: "Protocol 21" reads
// without the surrounding union. Unknown types and absent arms decline so
// callers can fall back rather than print something misleading.
func ledgerUpgradeSummary(upgrade xdr.LedgerUpgrade) (string, bool) {
	switch upgrade.Type {
	case xdr.LedgerUpgradeTypeLedgerUpgradeVersion:
		if upgrade.NewLedgerVersion == nil {
			return "", false
		}
		label := fmt.Sprintf("Protocol %d", *upgrade.NewLedgerVersion)
		if name := protocolName(uint32(*upgrade.NewLedgerVersion)); name != "" {
			label += " (" + name + ")"
		}
		return label, true

	case xdr.LedgerUpgradeTypeLedgerUpgradeBaseFee:
		if upgrade.NewBaseFee == nil {
			return "", false
		}
		return fmt.Sprintf("base fee %d stroops (%s XLM)",
			*upgrade.NewBaseFee, amount.String(xdr.Int64(*upgrade.NewBaseFee))), true

	case xdr.LedgerUpgradeTypeLedgerUpgradeMaxTxSetSize:
		if upgrade.NewMaxTxSetSize == nil {
			return "", false
		}
		return fmt.Sprintf("max tx set size %d", *upgrade.NewMaxTxSetSize), true

	case xdr.LedgerUpgradeTypeLedgerUpgradeBaseReserve:
		if upgrade.NewBaseReserve == nil {
			return "", false
		}
		return fmt.Sprintf("base reserve %d stroops (%s XLM)",
			*upgrade.NewBaseReserve, amount.String(xdr.Int64(*upgrade.NewBaseReserve))), true

	case xdr.LedgerUpgradeTypeLedgerUpgradeFlags:
		if upgrade.NewFlags == nil {
			return "", false
		}
		return fmt.Sprintf("ledger flags %d", *upgrade.NewFlags), true

	case xdr.LedgerUpgradeTypeLedgerUpgradeConfig:
		if upgrade.NewConfig == nil {
			return "", false
		}
		contract, err := contractIDString(upgrade.NewConfig.ContractId)
		if err != nil {
			return "", false
		}
		return fmt.Sprintf("config upgrade set: contract %s, content hash %s",
			contract, upgrade.NewConfig.ContentHash.HexString()), true

	case xdr.LedgerUpgradeTypeLedgerUpgradeMaxSorobanTxSetSize:
		if upgrade.NewMaxSorobanTxSetSize == nil {
			return "", false
		}
		return fmt.Sprintf("max Soroban tx set size %d", *upgrade.NewMaxSorobanTxSetSize), true

	default:
		return "", false
	}
}

// contractIDString renders a contract id as its C... strkey through the SDK's
// ScAddress helper, the same one `lens explain` uses for contract addresses,
// so the two renderings cannot drift apart.
func contractIDString(id xdr.ContractId) (string, error) {
	address := xdr.ScAddress{
		Type:       xdr.ScAddressTypeScAddressTypeContract,
		ContractId: &id,
	}
	return address.String()
}

var _ = reflect.TypeOf(xdr.UpgradeType{})
var _ = hex.EncodeToString
