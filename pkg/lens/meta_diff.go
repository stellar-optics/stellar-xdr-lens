package lens

import (
	"encoding/hex"
	"fmt"
	"math"
	"sort"

	"github.com/stellar/go-stellar-sdk/xdr"
)

// CalculateNetBalanceDeltas is a convenience wrapper around
// DiffTransactionMetaBalances that returns the slice of BalanceDelta values
// directly, omitting entries where the balance did not change.
func CalculateNetBalanceDeltas(meta *xdr.TransactionMeta) ([]BalanceDelta, error) {
	result, err := DiffTransactionMetaBalances(meta)
	if err != nil {
		return nil, err
	}
	return result.Changes, nil
}

// BatchDiffSummary summarises the aggregate differences between two
// LedgerCloseMetaBatch values.
type BatchDiffSummary struct {
	LeftStartSequence  uint32 `json:"leftStartSequence"`
	LeftEndSequence    uint32 `json:"leftEndSequence"`
	RightStartSequence uint32 `json:"rightStartSequence"`
	RightEndSequence   uint32 `json:"rightEndSequence"`
	// LeftLedgerCount is the number of LedgerCloseMeta records in the left batch.
	LeftLedgerCount int `json:"leftLedgerCount"`
	// RightLedgerCount is the number of LedgerCloseMeta records in the right batch.
	RightLedgerCount int `json:"rightLedgerCount"`
	// LedgerCountDelta is RightLedgerCount - LeftLedgerCount.
	LedgerCountDelta int `json:"ledgerCountDelta"`
	// LeftTxCount is the total transaction count across all ledgers in the left batch.
	LeftTxCount int `json:"leftTxCount"`
	// RightTxCount is the total transaction count across all ledgers in the right batch.
	RightTxCount int `json:"rightTxCount"`
	// TxCountDelta is RightTxCount - LeftTxCount.
	TxCountDelta int `json:"txCountDelta"`
	// LeftFailedTxCount is the number of failed transactions in the left batch.
	LeftFailedTxCount int `json:"leftFailedTxCount"`
	// RightFailedTxCount is the number of failed transactions in the right batch.
	RightFailedTxCount int `json:"rightFailedTxCount"`
	// LeftFeeCharged is the total fee charged across all transactions in the left batch (stroops).
	LeftFeeCharged int64 `json:"leftFeeCharged"`
	// RightFeeCharged is the total fee charged across all transactions in the right batch (stroops).
	RightFeeCharged int64 `json:"rightFeeCharged"`
	// FeeChargedDelta is RightFeeCharged - LeftFeeCharged.
	FeeChargedDelta int64 `json:"feeChargedDelta"`
}

// DiffLedgerBatch computes aggregate statistics across two
// LedgerCloseMetaBatch values and returns a comparison summary. Either
// argument may be nil, in which case its statistics are recorded as zero.
func DiffLedgerBatch(left, right *xdr.LedgerCloseMetaBatch) *BatchDiffSummary {
	s := &BatchDiffSummary{}
	if left != nil {
		s.LeftStartSequence = uint32(left.StartSequence)
		s.LeftEndSequence = uint32(left.EndSequence)
		s.LeftLedgerCount, s.LeftTxCount, s.LeftFailedTxCount, s.LeftFeeCharged = aggregateBatchStats(left)
	}
	if right != nil {
		s.RightStartSequence = uint32(right.StartSequence)
		s.RightEndSequence = uint32(right.EndSequence)
		s.RightLedgerCount, s.RightTxCount, s.RightFailedTxCount, s.RightFeeCharged = aggregateBatchStats(right)
	}
	s.LedgerCountDelta = s.RightLedgerCount - s.LeftLedgerCount
	s.TxCountDelta = s.RightTxCount - s.LeftTxCount
	s.FeeChargedDelta = s.RightFeeCharged - s.LeftFeeCharged
	return s
}

func aggregateBatchStats(batch *xdr.LedgerCloseMetaBatch) (ledgerCount, txCount, failedTxCount int, feeCharged int64) {
	if batch == nil {
		return
	}
	ledgerCount = len(batch.LedgerCloseMetas)
	for _, meta := range batch.LedgerCloseMetas {
		t, f, fee := ledgerCloseMetaTxStats(meta)
		txCount += t
		failedTxCount += f
		feeCharged += fee
	}
	return
}

func ledgerCloseMetaTxStats(meta xdr.LedgerCloseMeta) (txCount, failedTxCount int, feeCharged int64) {
	switch meta.V {
	case 0:
		if meta.V0 == nil {
			return
		}
		for _, tp := range meta.V0.TxProcessing {
			txCount++
			feeCharged += int64(tp.Result.Result.FeeCharged)
			if tp.Result.Result.Result.Code != xdr.TransactionResultCodeTxSuccess {
				failedTxCount++
			}
		}
	case 1:
		if meta.V1 == nil {
			return
		}
		for _, tp := range meta.V1.TxProcessing {
			txCount++
			feeCharged += int64(tp.Result.Result.FeeCharged)
			if tp.Result.Result.Result.Code != xdr.TransactionResultCodeTxSuccess {
				failedTxCount++
			}
		}
	case 2:
		if meta.V2 == nil {
			return
		}
		for _, tp := range meta.V2.TxProcessing {
			txCount++
			feeCharged += int64(tp.Result.Result.FeeCharged)
			if tp.Result.Result.Result.Code != xdr.TransactionResultCodeTxSuccess {
				failedTxCount++
			}
		}
	}
	return
}

// BalanceDelta describes the balance change for one ledger entry. Amounts are
// stored in stroops for XLM and in the asset's smallest ledger unit otherwise.
type BalanceDelta struct {
	EntryKey    string `json:"entryKey"`
	Holder      string `json:"holder"`
	Asset       string `json:"asset"`
	Before      int64  `json:"before"`
	After       int64  `json:"after"`
	Delta       int64  `json:"delta"`
	BeforeKnown bool   `json:"beforeKnown"`
	AfterKnown  bool   `json:"afterKnown"`
}

// MetaBalanceDiffResult contains deterministic balance changes extracted from
// transaction metadata.
type MetaBalanceDiffResult struct {
	Changes []BalanceDelta `json:"changes"`
}

// DiffTransactionMetaBalances derives account, trustline and claimable-balance
// deltas from a decoded TransactionMeta. No ledger or network lookups occur.
func DiffTransactionMetaBalances(meta *xdr.TransactionMeta) (*MetaBalanceDiffResult, error) {
	if meta == nil {
		return nil, fmt.Errorf("%w: nil transaction metadata", ErrInvalidArgument)
	}

	builders := make(map[string]*balanceDeltaBuilder)
	process := func(changes xdr.LedgerEntryChanges) error {
		for index := range changes {
			if err := applyLedgerEntryChange(builders, &changes[index]); err != nil {
				return fmt.Errorf("ledger change %d: %w", index, err)
			}
		}
		return nil
	}
	processOperations := func(operations []xdr.OperationMeta) error {
		for index := range operations {
			if err := process(operations[index].Changes); err != nil {
				return fmt.Errorf("operation metadata %d: %w", index, err)
			}
		}
		return nil
	}
	processOperationsV2 := func(operations []xdr.OperationMetaV2) error {
		for index := range operations {
			if err := process(operations[index].Changes); err != nil {
				return fmt.Errorf("operation metadata %d: %w", index, err)
			}
		}
		return nil
	}

	var err error
	switch meta.V {
	case 0:
		if meta.Operations == nil {
			return nil, fmt.Errorf("%w: transaction metadata v0 operations are missing", ErrInvalidArgument)
		}
		err = processOperations(*meta.Operations)
	case 1:
		if meta.V1 == nil {
			return nil, fmt.Errorf("%w: transaction metadata v1 arm is missing", ErrInvalidArgument)
		}
		if err = processOperations(meta.V1.Operations); err == nil {
			err = process(meta.V1.TxChanges)
		}
	case 2:
		if meta.V2 == nil {
			return nil, fmt.Errorf("%w: transaction metadata v2 arm is missing", ErrInvalidArgument)
		}
		if err = process(meta.V2.TxChangesBefore); err == nil {
			err = processOperations(meta.V2.Operations)
		}
		if err == nil {
			err = process(meta.V2.TxChangesAfter)
		}
	case 3:
		if meta.V3 == nil {
			return nil, fmt.Errorf("%w: transaction metadata v3 arm is missing", ErrInvalidArgument)
		}
		if err = process(meta.V3.TxChangesBefore); err == nil {
			err = processOperations(meta.V3.Operations)
		}
		if err == nil {
			err = process(meta.V3.TxChangesAfter)
		}
	case 4:
		if meta.V4 == nil {
			return nil, fmt.Errorf("%w: transaction metadata v4 arm is missing", ErrInvalidArgument)
		}
		if err = process(meta.V4.TxChangesBefore); err == nil {
			err = processOperationsV2(meta.V4.Operations)
		}
		if err == nil {
			err = process(meta.V4.TxChangesAfter)
		}
	default:
		return nil, fmt.Errorf("%w: TransactionMeta version %d", ErrUnsupportedType, meta.V)
	}
	if err != nil {
		return nil, err
	}

	result := &MetaBalanceDiffResult{Changes: make([]BalanceDelta, 0, len(builders))}
	for _, builder := range builders {
		if !builder.afterKnown {
			continue
		}
		delta := BalanceDelta{
			EntryKey:    builder.entryKey,
			Holder:      builder.holder,
			Asset:       builder.asset,
			Before:      builder.before,
			After:       builder.after,
			BeforeKnown: builder.beforeKnown,
			AfterKnown:  true,
		}
		if delta.BeforeKnown {
			if (delta.Before < 0 && delta.After > math.MaxInt64+delta.Before) ||
				(delta.Before > 0 && delta.After < math.MinInt64+delta.Before) {
				return nil, fmt.Errorf("%w: balance delta overflows int64 for %s", ErrInvalidArgument, builder.entryKey)
			}
			delta.Delta = delta.After - delta.Before
		}
		if delta.BeforeKnown && delta.Delta == 0 {
			continue
		}
		result.Changes = append(result.Changes, delta)
	}
	sort.Slice(result.Changes, func(i, j int) bool {
		a, b := result.Changes[i], result.Changes[j]
		if a.Holder != b.Holder {
			return a.Holder < b.Holder
		}
		if a.Asset != b.Asset {
			return a.Asset < b.Asset
		}
		return a.EntryKey < b.EntryKey
	})
	return result, nil
}

type balanceDeltaBuilder struct {
	entryKey    string
	holder      string
	asset       string
	before      int64
	after       int64
	beforeKnown bool
	afterKnown  bool
}

type ledgerBalance struct {
	entryKey string
	holder   string
	asset    string
	amount   int64
}

func applyLedgerEntryChange(builders map[string]*balanceDeltaBuilder, change *xdr.LedgerEntryChange) error {
	if change == nil {
		return fmt.Errorf("%w: nil ledger entry change", ErrInvalidArgument)
	}
	var entry *xdr.LedgerEntry
	var removedKey *xdr.LedgerKey
	switch change.Type {
	case xdr.LedgerEntryChangeTypeLedgerEntryState:
		entry = change.State
	case xdr.LedgerEntryChangeTypeLedgerEntryCreated:
		entry = change.Created
	case xdr.LedgerEntryChangeTypeLedgerEntryUpdated:
		entry = change.Updated
	case xdr.LedgerEntryChangeTypeLedgerEntryRestored:
		entry = change.Restored
	case xdr.LedgerEntryChangeTypeLedgerEntryRemoved:
		removedKey = change.Removed
	default:
		return fmt.Errorf("%w: unknown ledger entry change type %d", ErrInvalidArgument, change.Type)
	}

	var keyID string
	var balance *ledgerBalance
	if entry != nil {
		point, tracked, err := balanceFromEntry(entry)
		if err != nil {
			return err
		}
		keyID = point.entryKey
		if tracked {
			balance = &point
		}
	} else if removedKey != nil {
		keyBytes, err := removedKey.MarshalBinary()
		if err != nil {
			return fmt.Errorf("encoding removed ledger key: %w", err)
		}
		keyID = hex.EncodeToString(keyBytes)
	} else {
		return fmt.Errorf("%w: change arm %s is missing", ErrInvalidArgument, change.Type)
	}

	builder := builders[keyID]
	if balance == nil && builder == nil {
		return nil
	}
	if balance != nil && builder == nil {
		builder = &balanceDeltaBuilder{entryKey: keyID, holder: balance.holder, asset: balance.asset}
		builders[keyID] = builder
	}
	if balance != nil && (builder.holder != balance.holder || builder.asset != balance.asset) {
		return fmt.Errorf("%w: ledger key changed balance identity", ErrInvalidArgument)
	}

	switch change.Type {
	case xdr.LedgerEntryChangeTypeLedgerEntryState:
		if balance == nil {
			return nil
		}
		if !builder.beforeKnown {
			builder.before = balance.amount
			builder.beforeKnown = true
		}
	case xdr.LedgerEntryChangeTypeLedgerEntryCreated:
		if balance == nil {
			return nil
		}
		if !builder.beforeKnown {
			builder.before = 0
			builder.beforeKnown = true
		}
		builder.after = balance.amount
		builder.afterKnown = true
	case xdr.LedgerEntryChangeTypeLedgerEntryUpdated, xdr.LedgerEntryChangeTypeLedgerEntryRestored:
		if balance == nil {
			return nil
		}
		builder.after = balance.amount
		builder.afterKnown = true
	case xdr.LedgerEntryChangeTypeLedgerEntryRemoved:
		builder.after = 0
		builder.afterKnown = true
	}
	return nil
}

func balanceFromEntry(entry *xdr.LedgerEntry) (ledgerBalance, bool, error) {
	if entry == nil {
		return ledgerBalance{}, false, fmt.Errorf("%w: nil ledger entry", ErrInvalidArgument)
	}
	key, err := safeLedgerKey(entry)
	if err != nil {
		return ledgerBalance{}, false, err
	}
	keyBytes, err := key.MarshalBinary()
	if err != nil {
		return ledgerBalance{}, false, fmt.Errorf("encoding ledger key: %w", err)
	}
	point := ledgerBalance{entryKey: hex.EncodeToString(keyBytes)}
	switch entry.Data.Type {
	case xdr.LedgerEntryTypeAccount:
		if entry.Data.Account == nil {
			return ledgerBalance{}, false, fmt.Errorf("%w: account ledger entry arm is missing", ErrInvalidArgument)
		}
		address, err := entry.Data.Account.AccountId.GetAddress()
		if err != nil {
			return ledgerBalance{}, false, fmt.Errorf("formatting account id: %w", err)
		}
		point.holder = address
		point.asset = "XLM"
		point.amount = int64(entry.Data.Account.Balance)
		return point, true, nil
	case xdr.LedgerEntryTypeTrustline:
		if entry.Data.TrustLine == nil {
			return ledgerBalance{}, false, fmt.Errorf("%w: trustline ledger entry arm is missing", ErrInvalidArgument)
		}
		address, err := entry.Data.TrustLine.AccountId.GetAddress()
		if err != nil {
			return ledgerBalance{}, false, fmt.Errorf("formatting trustline account id: %w", err)
		}
		asset, err := trustlineAssetName(entry.Data.TrustLine.Asset)
		if err != nil {
			return ledgerBalance{}, false, err
		}
		point.holder = address
		point.asset = asset
		point.amount = int64(entry.Data.TrustLine.Balance)
		return point, true, nil
	case xdr.LedgerEntryTypeClaimableBalance:
		if entry.Data.ClaimableBalance == nil {
			return ledgerBalance{}, false, fmt.Errorf("%w: claimable-balance ledger entry arm is missing", ErrInvalidArgument)
		}
		asset, err := classicAssetName(entry.Data.ClaimableBalance.Asset)
		if err != nil {
			return ledgerBalance{}, false, err
		}
		point.holder = "claimable-balance:" + point.entryKey
		point.asset = asset
		point.amount = int64(entry.Data.ClaimableBalance.Amount)
		return point, true, nil
	default:
		return point, false, nil
	}
}

func safeLedgerKey(entry *xdr.LedgerEntry) (key xdr.LedgerKey, err error) {
	defer func() {
		if recover() != nil {
			key = xdr.LedgerKey{}
			err = fmt.Errorf("%w: malformed ledger entry key", ErrInvalidArgument)
		}
	}()
	key, err = entry.LedgerKey()
	if err != nil {
		return xdr.LedgerKey{}, fmt.Errorf("deriving ledger key: %w", err)
	}
	return key, nil
}

func trustlineAssetName(asset xdr.TrustLineAsset) (string, error) {
	switch asset.Type {
	case xdr.AssetTypeAssetTypeNative:
		return "XLM", nil
	case xdr.AssetTypeAssetTypeCreditAlphanum4:
		if asset.AlphaNum4 == nil {
			break
		}
		return asset.ToAsset().StringCanonical(), nil
	case xdr.AssetTypeAssetTypeCreditAlphanum12:
		if asset.AlphaNum12 == nil {
			break
		}
		return asset.ToAsset().StringCanonical(), nil
	case xdr.AssetTypeAssetTypePoolShare:
		if asset.LiquidityPoolId == nil {
			break
		}
		return "pool:" + hex.EncodeToString(asset.LiquidityPoolId[:]), nil
	}
	return "", fmt.Errorf("%w: malformed trustline asset", ErrInvalidArgument)
}

func classicAssetName(asset xdr.Asset) (string, error) {
	switch asset.Type {
	case xdr.AssetTypeAssetTypeNative:
		return "XLM", nil
	case xdr.AssetTypeAssetTypeCreditAlphanum4:
		if asset.AlphaNum4 == nil {
			return "", fmt.Errorf("%w: malformed classic asset", ErrInvalidArgument)
		}
		return asset.StringCanonical(), nil
	case xdr.AssetTypeAssetTypeCreditAlphanum12:
		if asset.AlphaNum12 == nil {
			return "", fmt.Errorf("%w: malformed classic asset", ErrInvalidArgument)
		}
		return asset.StringCanonical(), nil
	default:
		return "", fmt.Errorf("%w: malformed classic asset", ErrInvalidArgument)
	}
}
