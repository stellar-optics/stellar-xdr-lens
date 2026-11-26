package lens

import (
	"encoding/hex"
	"fmt"
	"reflect"
	"strconv"
	"time"

	"github.com/stellar/go-stellar-sdk/xdr"
)

func init() {
	// Opaque ledger keys carried by frozen/freeze-bypass config settings.
	// EncodedLedgerKey is defined as `opaque <>`, but the protocol guarantees
	// the bytes are a valid LedgerKey. Decoding them recovers the actual key
	// structure (account, trustline, offer, data, contract data, etc.) which
	// is far more useful than a 64-byte hex blob.
	enricherFor[reflect.TypeOf(xdr.EncodedLedgerKey{})] = enrichEncodedLedgerKey

	// Ledger-count TTL fields in StateArchivalSettings. The protocol stores
	// TTLs as a ledger count; at the default 5s target close time, one ledger
	// ≈ 5 seconds. We render an approximate wall-clock duration alongside the
	// raw count so developers can immediately see the practical meaning.
	// SamplePeriod is also ledger-counted; the same note applies.
	fieldNotes["MaxEntryTtl"] = ledgerCountNote
	fieldNotes["MinTemporaryTtl"] = ledgerCountNote
	fieldNotes["MinPersistentTtl"] = ledgerCountNote
	fieldNotes["LiveSorobanStateSizeWindowSamplePeriod"] = ledgerCountNote

	fieldNotes["ContractMaxSizeBytes"] = contractMaxSizeBytesNote
	fieldNotes["ContractDataKeySizeBytes"] = contractMaxSizeBytesNote
	fieldNotes["ContractDataEntrySizeBytes"] = contractMaxSizeBytesNote
	fieldNotes["TxMemoryLimit"] = contractMaxSizeBytesNote
	fieldNotes["LedgerMaxInstructions"] = cpuInstructionsNote
	fieldNotes["TxMaxInstructions"] = cpuInstructionsNote
	fieldNotes["LedgerMaxTxCount"] = executionLanesNote
	fieldNotes["LedgerMaxTxsSizeBytes"] = contractMaxSizeBytesNote
	fieldNotes["TxMaxSizeBytes"] = contractMaxSizeBytesNote
	fieldNotes["TxMaxContractEventsSizeBytes"] = contractMaxSizeBytesNote

	nodeHooks[reflect.TypeOf(xdr.ConfigSettingEntry{})] = decorateConfigSettingEntry
	enricherFor[reflect.TypeOf(xdr.ConfigSettingId(0))] = enrichConfigSettingId
}

func contractMaxSizeBytesNote(rv reflect.Value) (string, bool) {
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return "", false
		}
		rv = rv.Elem()
	}
	if v, ok := rv.Interface().(xdr.Uint32); ok {
		return formatByteSize(uint64(v)), true
	}
	return "", false
}

func cpuInstructionsNote(rv reflect.Value) (string, bool) {
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return "", false
		}
		rv = rv.Elem()
	}
	if v, ok := rv.Interface().(xdr.Int64); ok {
		return formatCPUInstructions(uint64(v)), true
	}
	return "", false
}

func executionLanesNote(rv reflect.Value) (string, bool) {
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return "", false
		}
		rv = rv.Elem()
	}
	if v, ok := rv.Interface().(xdr.Uint32); ok {
		return fmt.Sprintf("%d concurrent execution lanes", v), true
	}
	return "", false
}

// formatByteSize converts byte counts into KB / MB.
func formatByteSize(bytes uint64) string {
	switch {
	case bytes >= 1024*1024 && bytes%(1024*1024) == 0:
		return fmt.Sprintf("%d MB (%d bytes)", bytes/(1024*1024), bytes)
	case bytes >= 1024*1024:
		return fmt.Sprintf("%.2f MB (%d bytes)", float64(bytes)/float64(1024*1024), bytes)
	case bytes >= 1024 && bytes%1024 == 0:
		return fmt.Sprintf("%d KB (%d bytes)", bytes/1024, bytes)
	case bytes >= 1024:
		return fmt.Sprintf("%.2f KB (%d bytes)", float64(bytes)/1024.0, bytes)
	default:
		return fmt.Sprintf("%d bytes", bytes)
	}
}

// formatCPUInstructions converts CPU instruction counts into K / M.
func formatCPUInstructions(insns uint64) string {
	switch {
	case insns >= 1_000_000 && insns%1_000_000 == 0:
		return fmt.Sprintf("%dM CPU instructions (%d)", insns/1_000_000, insns)
	case insns >= 1_000_000:
		return fmt.Sprintf("%.2fM CPU instructions (%d)", float64(insns)/1_000_000.0, insns)
	case insns >= 1_000 && insns%1_000 == 0:
		return fmt.Sprintf("%dK CPU instructions (%d)", insns/1_000, insns)
	case insns >= 1_000:
		return fmt.Sprintf("%.2fK CPU instructions (%d)", float64(insns)/1_000.0, insns)
	default:
		return fmt.Sprintf("%d CPU instructions", insns)
	}
}

// decorateConfigSettingEntry decorates ConfigSettingEntry nodes and their children
// with human-readable units for Protocol 20/21 settings.
func decorateConfigSettingEntry(n *Node, raw any) {
	entry, ok := raw.(xdr.ConfigSettingEntry)
	if !ok {
		return
	}

	if idNode := n.Child("ConfigSettingId"); idNode != nil {
		idNode.Note = configSettingShortName(entry.ConfigSettingId)
	}

	switch entry.ConfigSettingId {
	case xdr.ConfigSettingIdConfigSettingContractMaxSizeBytes:
		if entry.ContractMaxSizeBytes != nil {
			bytes := uint64(*entry.ContractMaxSizeBytes)
			n.Note = fmt.Sprintf("max contract size: %s", formatByteSize(bytes))
			if child := n.Child("ContractMaxSizeBytes"); child != nil {
				child.Note = formatByteSize(bytes)
			}
		}

	case xdr.ConfigSettingIdConfigSettingContractComputeV0:
		if entry.ContractCompute != nil {
			c := entry.ContractCompute
			n.Note = fmt.Sprintf("CPU: %s ledger / %s tx, Memory: %s",
				formatCPUInstructions(uint64(c.LedgerMaxInstructions)),
				formatCPUInstructions(uint64(c.TxMaxInstructions)),
				formatByteSize(uint64(c.TxMemoryLimit)),
			)
			if child := n.Child("ContractCompute"); child != nil {
				if m := child.Child("LedgerMaxInstructions"); m != nil {
					m.Note = formatCPUInstructions(uint64(c.LedgerMaxInstructions))
				}
				if m := child.Child("TxMaxInstructions"); m != nil {
					m.Note = formatCPUInstructions(uint64(c.TxMaxInstructions))
				}
				if m := child.Child("TxMemoryLimit"); m != nil {
					m.Note = formatByteSize(uint64(c.TxMemoryLimit))
				}
			}
		}

	case xdr.ConfigSettingIdConfigSettingContractLedgerCostV0:
		if entry.ContractLedgerCost != nil {
			c := entry.ContractLedgerCost
			readFeePerByte := float64(c.FeeDiskRead1Kb) / 1024.0
			writeFeePerByteLow := float64(c.RentFee1KbSorobanStateSizeLow) / 1024.0
			writeFeePerByteHigh := float64(c.RentFee1KbSorobanStateSizeHigh) / 1024.0
			n.Note = fmt.Sprintf("read fee: %.4f stroops/byte, write fee: %.4f..%.4f stroops/byte",
				readFeePerByte, writeFeePerByteLow, writeFeePerByteHigh)
			if child := n.Child("ContractLedgerCost"); child != nil {
				child.Note = fmt.Sprintf("read: %.4f stroops/byte, write: %.4f..%.4f stroops/byte",
					readFeePerByte, writeFeePerByteLow, writeFeePerByteHigh)
				if m := child.Child("FeeDiskRead1Kb"); m != nil {
					m.Note += fmt.Sprintf(" (%.4f stroops/byte)", readFeePerByte)
				}
				if m := child.Child("RentFee1KbSorobanStateSizeLow"); m != nil {
					m.Note += fmt.Sprintf(" (%.4f stroops/byte)", writeFeePerByteLow)
				}
				if m := child.Child("RentFee1KbSorobanStateSizeHigh"); m != nil {
					m.Note += fmt.Sprintf(" (%.4f stroops/byte)", writeFeePerByteHigh)
				}
				if m := child.Child("LedgerMaxDiskReadBytes"); m != nil {
					m.Note = formatByteSize(uint64(c.LedgerMaxDiskReadBytes))
				}
				if m := child.Child("LedgerMaxWriteBytes"); m != nil {
					m.Note = formatByteSize(uint64(c.LedgerMaxWriteBytes))
				}
				if m := child.Child("TxMaxDiskReadBytes"); m != nil {
					m.Note = formatByteSize(uint64(c.TxMaxDiskReadBytes))
				}
				if m := child.Child("TxMaxWriteBytes"); m != nil {
					m.Note = formatByteSize(uint64(c.TxMaxWriteBytes))
				}
			}
		}

	case xdr.ConfigSettingIdConfigSettingStateArchival:
		if entry.StateArchivalSettings != nil {
			s := entry.StateArchivalSettings
			minDays := float64(s.MinTemporaryTtl*5) / (24 * 3600)
			maxDays := float64(s.MaxEntryTtl*5) / (24 * 3600)
			n.Note = fmt.Sprintf("TTL: min %.1f days (%d ledgers), max %.1f days (%d ledgers)",
				minDays, s.MinTemporaryTtl, maxDays, s.MaxEntryTtl)
			if child := n.Child("StateArchivalSettings"); child != nil {
				child.Note = fmt.Sprintf("min TTL: %.1f days (%d ledgers), max TTL: %.1f days (%d ledgers)",
					minDays, s.MinTemporaryTtl, maxDays, s.MaxEntryTtl)
			}
		}

	case xdr.ConfigSettingIdConfigSettingContractExecutionLanes:
		if entry.ContractExecutionLanes != nil {
			lanes := entry.ContractExecutionLanes.LedgerMaxTxCount
			n.Note = fmt.Sprintf("%d concurrent execution lanes", lanes)
			if child := n.Child("ContractExecutionLanes"); child != nil {
				if m := child.Child("LedgerMaxTxCount"); m != nil {
					m.Note = fmt.Sprintf("%d concurrent execution lanes", lanes)
				}
			}
		}

	case xdr.ConfigSettingIdConfigSettingContractDataKeySizeBytes:
		if entry.ContractDataKeySizeBytes != nil {
			bytes := uint64(*entry.ContractDataKeySizeBytes)
			n.Note = fmt.Sprintf("max contract data key size: %s", formatByteSize(bytes))
			if child := n.Child("ContractDataKeySizeBytes"); child != nil {
				child.Note = formatByteSize(bytes)
			}
		}

	case xdr.ConfigSettingIdConfigSettingContractDataEntrySizeBytes:
		if entry.ContractDataEntrySizeBytes != nil {
			bytes := uint64(*entry.ContractDataEntrySizeBytes)
			n.Note = fmt.Sprintf("max contract data entry size: %s", formatByteSize(bytes))
			if child := n.Child("ContractDataEntrySizeBytes"); child != nil {
				child.Note = formatByteSize(bytes)
			}
		}

	case xdr.ConfigSettingIdConfigSettingContractBandwidthV0:
		if entry.ContractBandwidth != nil {
			b := entry.ContractBandwidth
			n.Note = fmt.Sprintf("bandwidth: %s ledger / %s tx",
				formatByteSize(uint64(b.LedgerMaxTxsSizeBytes)),
				formatByteSize(uint64(b.TxMaxSizeBytes)))
			if child := n.Child("ContractBandwidth"); child != nil {
				if m := child.Child("LedgerMaxTxsSizeBytes"); m != nil {
					m.Note = formatByteSize(uint64(b.LedgerMaxTxsSizeBytes))
				}
				if m := child.Child("TxMaxSizeBytes"); m != nil {
					m.Note = formatByteSize(uint64(b.TxMaxSizeBytes))
				}
			}
		}

	case xdr.ConfigSettingIdConfigSettingContractEventsV0:
		if entry.ContractEvents != nil {
			e := entry.ContractEvents
			n.Note = fmt.Sprintf("max events size: %s", formatByteSize(uint64(e.TxMaxContractEventsSizeBytes)))
			if child := n.Child("ContractEvents"); child != nil {
				if m := child.Child("TxMaxContractEventsSizeBytes"); m != nil {
					m.Note = formatByteSize(uint64(e.TxMaxContractEventsSizeBytes))
				}
			}
		}
	}
}

// enrichEncodedLedgerKey decodes the opaque EncodedLedgerKey bytes as a
// LedgerKey and produces a concise one-line summary. The raw hex is kept as
// the machine value; the decoded summary becomes the note. Anything that does
// not decode cleanly declines, falling back to the generic hex rendering.
func enrichEncodedLedgerKey(rv reflect.Value) (string, any, bool) {
	ek, valid := rv.Interface().(xdr.EncodedLedgerKey)
	if !valid || len(ek) == 0 {
		return "", nil, false
	}
	var key xdr.LedgerKey
	if err := xdr.SafeUnmarshal(ek, &key); err != nil {
		return "", nil, false
	}
	summary, ok := ledgerKeySummary(key)
	if !ok {
		return "", nil, false
	}
	return summary, hex.EncodeToString(ek), true
}

// ledgerKeySummary renders a LedgerKey as a single human-readable line.
// It covers all current LedgerEntryType arms. For keys that contain
// addresses or hashes, the identifying value is included.
func ledgerKeySummary(key xdr.LedgerKey) (string, bool) {
	switch key.Type {
	case xdr.LedgerEntryTypeAccount:
		if key.Account == nil {
			return "", false
		}
		addr, err := key.Account.AccountId.GetAddress()
		if err != nil {
			return "", false
		}
		return "account key: " + addr, true

	case xdr.LedgerEntryTypeTrustline:
		if key.TrustLine == nil {
			return "", false
		}
		addr, err := key.TrustLine.AccountId.GetAddress()
		if err != nil {
			return "", false
		}
		assetStr := key.TrustLine.Asset.ToAsset().StringCanonical()
		return "trustline key: " + addr + " for " + assetStr, true

	case xdr.LedgerEntryTypeOffer:
		if key.Offer == nil {
			return "", false
		}
		addr, err := key.Offer.SellerId.GetAddress()
		if err != nil {
			return "", false
		}
		return fmt.Sprintf("offer key: #%d by %s", key.Offer.OfferId, addr), true

	case xdr.LedgerEntryTypeData:
		if key.Data == nil {
			return "", false
		}
		addr, err := key.Data.AccountId.GetAddress()
		if err != nil {
			return "", false
		}
		// DataName is a string; use %q to show special characters safely.
		return fmt.Sprintf("data entry key: %q for %s", key.Data.DataName, addr), true

	case xdr.LedgerEntryTypeClaimableBalance:
		if key.ClaimableBalance == nil {
			return "", false
		}
		// BalanceId is a union; v0 carries a Hash.
		var h xdr.Hash
		if key.ClaimableBalance.BalanceId.Type == xdr.ClaimableBalanceIdTypeClaimableBalanceIdTypeV0 &&
			key.ClaimableBalance.BalanceId.V0 != nil {
			h = *key.ClaimableBalance.BalanceId.V0
		} else {
			return "", false
		}
		return "claimable balance key: " + hex.EncodeToString(h[:]), true

	case xdr.LedgerEntryTypeLiquidityPool:
		if key.LiquidityPool == nil {
			return "", false
		}
		// PoolId is a Hash; render as hex prefix (full 64 chars is verbose).
		pid := hex.EncodeToString(key.LiquidityPool.LiquidityPoolId[:])
		return "liquidity pool key: " + pid[:16] + "…", true

	case xdr.LedgerEntryTypeContractData:
		if key.ContractData == nil {
			return "", false
		}
		contractStr, err := key.ContractData.Contract.String()
		if err != nil {
			return "", false
		}
		// The ScVal key is the distinguishing part of a contract data key.
		// Render a compact form: kind + short key representation.
		keyStr := scvalShortKey(key.ContractData.Key)
		return "contract data key: " + contractStr + " " + keyStr, true

	case xdr.LedgerEntryTypeContractCode:
		if key.ContractCode == nil {
			return "", false
		}
		hash := hex.EncodeToString(key.ContractCode.Hash[:])
		return "contract code key: " + hash[:16] + "…", true

	case xdr.LedgerEntryTypeConfigSetting:
		if key.ConfigSetting == nil {
			return "", false
		}
		id := key.ConfigSetting.ConfigSettingId
		return "config setting key: " + configSettingShortName(id), true

	case xdr.LedgerEntryTypeTtl:
		if key.Ttl == nil {
			return "", false
		}
		kh := hex.EncodeToString(key.Ttl.KeyHash[:])
		return "ttl key: " + kh[:16] + "…", true

	default:
		return "", false
	}
}

// scvalShortKey produces a compact one-line representation of a contract
// data key ScVal. It handles the common cases: ledger-key-contract-instance,
// symbol, string, bytes, and address. Other types fall back to the SDK's
// String() which is already concise for primitives.
func scvalShortKey(v xdr.ScVal) string {
	switch v.Type {
	case xdr.ScValTypeScvLedgerKeyContractInstance:
		return "(instance)"
	case xdr.ScValTypeScvSymbol:
		return "symbol:" + string(*v.Sym)
	case xdr.ScValTypeScvString:
		// Contract data keys are often symbols; strings may contain arbitrary
		// bytes. Quote and truncate if long.
		s := string(*v.Str)
		if len(s) > 20 {
			s = s[:20] + "…"
		}
		return "string:" + strconv.Quote(s)
	case xdr.ScValTypeScvBytes:
		b := *v.Bytes
		if len(b) > 12 {
			return "bytes:" + hex.EncodeToString(b[:12]) + "…"
		}
		return "bytes:" + hex.EncodeToString(b)
	case xdr.ScValTypeScvAddress:
		// ScAddress contains a type and optional hash. Check validity.
		if v.Address.Type == xdr.ScAddressTypeScAddressTypeContract &&
			v.Address.ContractId != nil {
			addr, err := v.Address.String()
			if err != nil {
				return "address:<error>"
			}
			return "address:" + addr
		}
		return "address:<invalid>"
	default:
		// For U32, I32, U64, I64, U128, I128, etc., the SDK String() is fine.
		return v.String()
	}
}

// configSettingShortName maps a ConfigSettingId to a concise human name.
// The generated String() includes the full "ConfigSettingIdConfigSetting..."
// prefix; this map strips that noise.
func configSettingShortName(id xdr.ConfigSettingId) string {
	switch id {
	case xdr.ConfigSettingIdConfigSettingContractMaxSizeBytes:
		return "contract max size bytes"
	case xdr.ConfigSettingIdConfigSettingContractComputeV0:
		return "contract compute V0"
	case xdr.ConfigSettingIdConfigSettingContractLedgerCostV0:
		return "contract ledger cost V0"
	case xdr.ConfigSettingIdConfigSettingContractHistoricalDataV0:
		return "contract historical data V0"
	case xdr.ConfigSettingIdConfigSettingContractEventsV0:
		return "contract events V0"
	case xdr.ConfigSettingIdConfigSettingContractBandwidthV0:
		return "contract bandwidth V0"
	case xdr.ConfigSettingIdConfigSettingContractCostParamsCpuInstructions:
		return "contract cost params CPU instructions"
	case xdr.ConfigSettingIdConfigSettingContractCostParamsMemoryBytes:
		return "contract cost params memory bytes"
	case xdr.ConfigSettingIdConfigSettingContractDataKeySizeBytes:
		return "contract data key size bytes"
	case xdr.ConfigSettingIdConfigSettingContractDataEntrySizeBytes:
		return "contract data entry size bytes"
	case xdr.ConfigSettingIdConfigSettingStateArchival:
		return "state archival"
	case xdr.ConfigSettingIdConfigSettingContractExecutionLanes:
		return "contract execution lanes"
	case xdr.ConfigSettingIdConfigSettingLiveSorobanStateSizeWindow:
		return "live soroban state size window"
	case xdr.ConfigSettingIdConfigSettingEvictionIterator:
		return "eviction iterator"
	case xdr.ConfigSettingIdConfigSettingContractParallelComputeV0:
		return "contract parallel compute V0"
	case xdr.ConfigSettingIdConfigSettingContractLedgerCostExtV0:
		return "contract ledger cost ext V0"
	case xdr.ConfigSettingIdConfigSettingScpTiming:
		return "SCP timing"
	case xdr.ConfigSettingIdConfigSettingFrozenLedgerKeys:
		return "frozen ledger keys"
	case xdr.ConfigSettingIdConfigSettingFrozenLedgerKeysDelta:
		return "frozen ledger keys delta"
	case xdr.ConfigSettingIdConfigSettingFreezeBypassTxs:
		return "freeze bypass txs"
	case xdr.ConfigSettingIdConfigSettingFreezeBypassTxsDelta:
		return "freeze bypass txs delta"
	default:
		return fmt.Sprintf("unknown(%d)", id)
	}
}

// enrichConfigSettingId renders a ConfigSettingId enum as its constant name
// plus a short human-friendly label, e.g.
// "ConfigSettingIdConfigSettingContractMaxSizeBytes (contract max size bytes)".
func enrichConfigSettingId(rv reflect.Value) (string, any, bool) {
	id, ok := rv.Interface().(xdr.ConfigSettingId)
	if !ok {
		return "", nil, false
	}
	return "", fmt.Sprintf("%s (%s)", id.String(), configSettingShortName(id)), true
}

// ledgerCountNote renders a ledger-count field (Uint32) as an approximate
// wall-clock duration at the protocol's default 5-second target close time.
// The note uses "≈" and states the assumption explicitly.
func ledgerCountNote(rv reflect.Value) (string, bool) {
	v, ok := rv.Interface().(xdr.Uint32)
	if !ok {
		return "", false
	}
	ledgers := uint64(v)
	seconds := ledgers * 5 // 5s per ledger (default target close time)
	d := time.Duration(seconds) * time.Second

	var human string
	if d >= 24*time.Hour {
		days := float64(d) / (24 * float64(time.Hour))
		human = fmt.Sprintf("≈%.1f days", days)
	} else {
		human = "≈" + d.String()
	}
	return human + " (at 5s per ledger)", true
}
