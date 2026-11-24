package lens_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

func accountLedgerEntry(balance int64) *xdr.LedgerEntry {
	return accountLedgerEntryWithID(balance, 17)
}

func accountLedgerEntryWithID(balance int64, id byte) *xdr.LedgerEntry {
	key := xdr.Uint256{}
	key[0] = id
	accountID := xdr.AccountId{Type: xdr.PublicKeyTypePublicKeyTypeEd25519, Ed25519: &key}
	account := xdr.AccountEntry{AccountId: accountID, Balance: xdr.Int64(balance)}
	return &xdr.LedgerEntry{Data: xdr.LedgerEntryData{
		Type: xdr.LedgerEntryTypeAccount, Account: &account,
	}}
}

func trustlineLedgerEntry(t *testing.T, balance int64) *xdr.LedgerEntry {
	t.Helper()
	accountKey := xdr.Uint256{}
	accountKey[0] = 21
	accountID := xdr.AccountId{Type: xdr.PublicKeyTypePublicKeyTypeEd25519, Ed25519: &accountKey}
	issuerKey := xdr.Uint256{}
	issuerKey[0] = 22
	issuerID := xdr.AccountId{Type: xdr.PublicKeyTypePublicKeyTypeEd25519, Ed25519: &issuerKey}
	issuer, err := issuerID.GetAddress()
	if err != nil {
		t.Fatalf("issuer GetAddress() error = %v", err)
	}
	asset, err := xdr.NewCreditAsset("USD", issuer)
	if err != nil {
		t.Fatalf("NewCreditAsset() error = %v", err)
	}
	trustline := xdr.TrustLineEntry{
		AccountId: accountID,
		Asset:     asset.ToTrustLineAsset(),
		Balance:   xdr.Int64(balance),
	}
	return &xdr.LedgerEntry{Data: xdr.LedgerEntryData{
		Type: xdr.LedgerEntryTypeTrustline, TrustLine: &trustline,
	}}
}

func claimableBalanceLedgerEntry(amount int64) *xdr.LedgerEntry {
	hash := xdr.Hash{}
	hash[0] = 31
	entry := xdr.ClaimableBalanceEntry{
		BalanceId: xdr.ClaimableBalanceId{
			Type: xdr.ClaimableBalanceIdTypeClaimableBalanceIdTypeV0,
			V0:   &hash,
		},
		Asset:  xdr.MustNewNativeAsset(),
		Amount: xdr.Int64(amount),
	}
	return &xdr.LedgerEntry{Data: xdr.LedgerEntryData{
		Type: xdr.LedgerEntryTypeClaimableBalance, ClaimableBalance: &entry,
	}}
}

func ledgerChange(changeType xdr.LedgerEntryChangeType, entry *xdr.LedgerEntry) xdr.LedgerEntryChange {
	change := xdr.LedgerEntryChange{Type: changeType}
	switch changeType {
	case xdr.LedgerEntryChangeTypeLedgerEntryState:
		change.State = entry
	case xdr.LedgerEntryChangeTypeLedgerEntryCreated:
		change.Created = entry
	case xdr.LedgerEntryChangeTypeLedgerEntryUpdated:
		change.Updated = entry
	case xdr.LedgerEntryChangeTypeLedgerEntryRestored:
		change.Restored = entry
	}
	return change
}

func TestDiffTransactionMetaBalancesAcrossVersions(t *testing.T) {
	t.Parallel()

	before, after := accountLedgerEntry(10000000), accountLedgerEntry(12500000)
	changes := xdr.LedgerEntryChanges{
		ledgerChange(xdr.LedgerEntryChangeTypeLedgerEntryState, before),
		ledgerChange(xdr.LedgerEntryChangeTypeLedgerEntryUpdated, after),
	}
	versions := []struct {
		name string
		meta xdr.TransactionMeta
	}{
		{
			name: "v0",
			meta: xdr.TransactionMeta{V: 0, Operations: &[]xdr.OperationMeta{{Changes: changes}}},
		},
		{
			name: "v1",
			meta: xdr.TransactionMeta{V: 1, V1: &xdr.TransactionMetaV1{Operations: []xdr.OperationMeta{{Changes: changes}}}},
		},
		{
			name: "v2",
			meta: xdr.TransactionMeta{V: 2, V2: &xdr.TransactionMetaV2{Operations: []xdr.OperationMeta{{Changes: changes}}}},
		},
		{
			name: "v3",
			meta: xdr.TransactionMeta{V: 3, V3: &xdr.TransactionMetaV3{Operations: []xdr.OperationMeta{{Changes: changes}}}},
		},
		{
			name: "v4",
			meta: xdr.TransactionMeta{V: 4, V4: &xdr.TransactionMetaV4{Operations: []xdr.OperationMetaV2{{Changes: changes}}}},
		},
	}

	for _, tc := range versions {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := lens.DiffTransactionMetaBalances(&tc.meta)
			if err != nil {
				t.Fatalf("DiffTransactionMetaBalances() error = %v", err)
			}
			if len(got.Changes) != 1 {
				t.Fatalf("got %d delta(s), want 1: %+v", len(got.Changes), got.Changes)
			}
			delta := got.Changes[0]
			if delta.Before != 10000000 || delta.After != 12500000 || delta.Delta != 2500000 {
				t.Errorf("delta = %+v, want 10000000 -> 12500000 (+2500000)", delta)
			}
			if delta.Asset != "XLM" || delta.Holder == "" || delta.EntryKey == "" || !delta.BeforeKnown || !delta.AfterKnown {
				t.Errorf("delta identity or known flags = %+v", delta)
			}
		})
	}
}

func TestDiffTransactionMetaBalancesCreateAndRemove(t *testing.T) {
	t.Parallel()

	created := accountLedgerEntryWithID(700, 17)
	removed := accountLedgerEntryWithID(900, 18)
	removedKey, err := removed.LedgerKey()
	if err != nil {
		t.Fatalf("LedgerKey() error = %v", err)
	}
	meta := xdr.TransactionMeta{
		V: 4,
		V4: &xdr.TransactionMetaV4{
			Operations: []xdr.OperationMetaV2{{Changes: xdr.LedgerEntryChanges{
				ledgerChange(xdr.LedgerEntryChangeTypeLedgerEntryCreated, created),
				ledgerChange(xdr.LedgerEntryChangeTypeLedgerEntryState, removed),
				{Type: xdr.LedgerEntryChangeTypeLedgerEntryRemoved, Removed: &removedKey},
			}}},
		},
	}
	got, err := lens.DiffTransactionMetaBalances(&meta)
	if err != nil {
		t.Fatalf("DiffTransactionMetaBalances() error = %v", err)
	}
	if len(got.Changes) != 2 {
		t.Fatalf("got %d delta(s), want create and remove: %+v", len(got.Changes), got.Changes)
	}
	var sawCreated, sawRemoved bool
	for _, delta := range got.Changes {
		switch {
		case delta.Before == 0 && delta.After == 700 && delta.Delta == 700:
			sawCreated = true
		case delta.Before == 900 && delta.After == 0 && delta.Delta == -900:
			sawRemoved = true
		}
	}
	if !sawCreated || !sawRemoved {
		t.Errorf("create/remove deltas = %+v", got.Changes)
	}
}

func TestDiffTransactionMetaBalancesTrustlineAndClaimableBalance(t *testing.T) {
	t.Parallel()

	changes := xdr.LedgerEntryChanges{
		ledgerChange(xdr.LedgerEntryChangeTypeLedgerEntryState, trustlineLedgerEntry(t, 100)),
		ledgerChange(xdr.LedgerEntryChangeTypeLedgerEntryUpdated, trustlineLedgerEntry(t, 250)),
		ledgerChange(xdr.LedgerEntryChangeTypeLedgerEntryState, claimableBalanceLedgerEntry(500)),
		ledgerChange(xdr.LedgerEntryChangeTypeLedgerEntryUpdated, claimableBalanceLedgerEntry(300)),
	}
	meta := xdr.TransactionMeta{V: 2, V2: &xdr.TransactionMetaV2{
		TxChangesBefore: changes[:1:1],
		Operations:      []xdr.OperationMeta{{Changes: changes[1:]}},
	}}
	got, err := lens.DiffTransactionMetaBalances(&meta)
	if err != nil {
		t.Fatalf("DiffTransactionMetaBalances() error = %v", err)
	}
	if len(got.Changes) != 2 {
		t.Fatalf("got %d balance changes, want trustline and claimable balance: %+v", len(got.Changes), got.Changes)
	}
	var sawTrustline, sawClaimable bool
	for _, delta := range got.Changes {
		switch {
		case delta.Asset == "USD:"+mustIssuerAddress(t) && delta.Before == 100 && delta.After == 250:
			sawTrustline = true
		case delta.Asset == "XLM" && delta.Before == 500 && delta.After == 300 && strings.HasPrefix(delta.Holder, "claimable-balance:"):
			sawClaimable = true
		}
	}
	if !sawTrustline || !sawClaimable {
		t.Errorf("trustline/claimable deltas not found: %+v", got.Changes)
	}
}

func mustIssuerAddress(t *testing.T) string {
	t.Helper()
	key := xdr.Uint256{}
	key[0] = 22
	accountID := xdr.AccountId{Type: xdr.PublicKeyTypePublicKeyTypeEd25519, Ed25519: &key}
	address, err := accountID.GetAddress()
	if err != nil {
		t.Fatalf("GetAddress() error = %v", err)
	}
	return address
}

func TestDiffTransactionMetaBalancesUnknownBaselineAndNoOp(t *testing.T) {
	t.Parallel()

	updated := accountLedgerEntry(42)
	unknown := xdr.TransactionMeta{V: 1, V1: &xdr.TransactionMetaV1{
		Operations: []xdr.OperationMeta{{Changes: xdr.LedgerEntryChanges{
			ledgerChange(xdr.LedgerEntryChangeTypeLedgerEntryUpdated, updated),
		}}},
	}}
	got, err := lens.DiffTransactionMetaBalances(&unknown)
	if err != nil {
		t.Fatalf("DiffTransactionMetaBalances(unknown baseline) error = %v", err)
	}
	if len(got.Changes) != 1 || got.Changes[0].BeforeKnown || !got.Changes[0].AfterKnown || got.Changes[0].After != 42 {
		t.Errorf("unknown-baseline result = %+v", got.Changes)
	}

	stateOnly := xdr.TransactionMeta{V: 2, V2: &xdr.TransactionMetaV2{
		TxChangesBefore: xdr.LedgerEntryChanges{ledgerChange(xdr.LedgerEntryChangeTypeLedgerEntryState, updated)},
	}}
	noOp, err := lens.DiffTransactionMetaBalances(&stateOnly)
	if err != nil {
		t.Fatalf("DiffTransactionMetaBalances(state only) error = %v", err)
	}
	if len(noOp.Changes) != 0 {
		t.Errorf("state-only metadata produced changes: %+v", noOp.Changes)
	}
}

func TestDiffTransactionMetaBalancesErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		meta *xdr.TransactionMeta
		want error
	}{
		{"nil", nil, lens.ErrInvalidArgument},
		{"missing v0 operations", &xdr.TransactionMeta{V: 0}, lens.ErrInvalidArgument},
		{"missing v1 arm", &xdr.TransactionMeta{V: 1}, lens.ErrInvalidArgument},
		{"missing v2 arm", &xdr.TransactionMeta{V: 2}, lens.ErrInvalidArgument},
		{"missing v3 arm", &xdr.TransactionMeta{V: 3}, lens.ErrInvalidArgument},
		{"missing v4 arm", &xdr.TransactionMeta{V: 4}, lens.ErrInvalidArgument},
		{"unknown version", &xdr.TransactionMeta{V: 5}, lens.ErrUnsupportedType},
		{"missing change arm", &xdr.TransactionMeta{V: 0, Operations: &[]xdr.OperationMeta{{Changes: xdr.LedgerEntryChanges{{Type: xdr.LedgerEntryChangeTypeLedgerEntryUpdated}}}}}, lens.ErrInvalidArgument},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := lens.DiffTransactionMetaBalances(tc.meta)
			if !errors.Is(err, tc.want) {
				t.Errorf("DiffTransactionMetaBalances() error = %v, want errors.Is(_, %v)", err, tc.want)
			}
		})
	}
}

func TestDiffTransactionMetaBalancesCanonicalEntryFixture(t *testing.T) {
	t.Parallel()

	value, err := lens.DecodeAs(loadFixture(t, "bucket_entry_live"), "BucketEntry")
	if err != nil {
		t.Fatalf("DecodeAs(canonical live-entry fixture) error = %v", err)
	}
	bucket, ok := value.Raw.(*xdr.BucketEntry)
	if !ok || bucket.LiveEntry == nil || bucket.LiveEntry.Data.Account == nil {
		t.Fatalf("fixture raw value = %T, want a live account ledger entry", value.Raw)
	}
	before := bucket.LiveEntry
	after := *before
	account := *before.Data.Account
	account.Balance += 12345
	after.Data.Account = &account
	meta := xdr.TransactionMeta{V: 2, V2: &xdr.TransactionMetaV2{
		TxChangesBefore: xdr.LedgerEntryChanges{ledgerChange(xdr.LedgerEntryChangeTypeLedgerEntryState, before)},
		TxChangesAfter:  xdr.LedgerEntryChanges{ledgerChange(xdr.LedgerEntryChangeTypeLedgerEntryUpdated, &after)},
	}}
	got, err := lens.DiffTransactionMetaBalances(&meta)
	if err != nil {
		t.Fatalf("DiffTransactionMetaBalances(canonical fixture) error = %v", err)
	}
	if len(got.Changes) != 1 || got.Changes[0].Before != int64(before.Data.Account.Balance) || got.Changes[0].After != int64(account.Balance) || got.Changes[0].Delta != 12345 {
		t.Errorf("canonical fixture delta = %+v", got.Changes)
	}
}

// --- Issue #283: CalculateNetBalanceDeltas multi-account ---

func TestCalculateNetBalanceDeltasMultiAccount(t *testing.T) {
	t.Parallel()

	// Use the same accountLedgerEntryWithID helper used by the rest of this file.
	// ID byte 0x01 and 0x02 produce distinct key hashes → two separate accounts.
	before1 := accountLedgerEntryWithID(1_000_000_000, 0x01)
	after1 := accountLedgerEntryWithID(999_900_000, 0x01)
	before2 := accountLedgerEntryWithID(500_000_000, 0x02)
	after2 := accountLedgerEntryWithID(500_100_000, 0x02)

	meta := xdr.TransactionMeta{V: 2, V2: &xdr.TransactionMetaV2{
		TxChangesBefore: xdr.LedgerEntryChanges{
			ledgerChange(xdr.LedgerEntryChangeTypeLedgerEntryState, before1),
			ledgerChange(xdr.LedgerEntryChangeTypeLedgerEntryState, before2),
		},
		TxChangesAfter: xdr.LedgerEntryChanges{
			ledgerChange(xdr.LedgerEntryChangeTypeLedgerEntryUpdated, after1),
			ledgerChange(xdr.LedgerEntryChangeTypeLedgerEntryUpdated, after2),
		},
	}}

	deltas, err := lens.CalculateNetBalanceDeltas(&meta)
	if err != nil {
		t.Fatalf("CalculateNetBalanceDeltas() error = %v", err)
	}
	if len(deltas) != 2 {
		t.Fatalf("expected 2 deltas, got %d: %+v", len(deltas), deltas)
	}
	// Accumulate deltas; the two accounts should have opposite signs.
	var totalDelta int64
	for _, d := range deltas {
		totalDelta += d.Delta
	}
	// Net should cancel out: −100 000 + 100 000 = 0.
	if totalDelta != 0 {
		t.Errorf("net delta across two accounts = %d; want 0", totalDelta)
	}
	// Individual deltas must be non-zero and opposite.
	if deltas[0].Delta == 0 || deltas[1].Delta == 0 {
		t.Errorf("unexpected zero delta: %+v", deltas)
	}
	if deltas[0].Delta+deltas[1].Delta != 0 {
		t.Errorf("deltas should be equal and opposite: %d, %d", deltas[0].Delta, deltas[1].Delta)
	}
}

func TestCalculateNetBalanceDeltasNilMeta(t *testing.T) {
	t.Parallel()
	_, err := lens.CalculateNetBalanceDeltas(nil)
	if err == nil {
		t.Fatal("expected error for nil meta")
	}
	if !errors.Is(err, lens.ErrInvalidArgument) {
		t.Errorf("error = %v; want ErrInvalidArgument", err)
	}
}

// --- Issue #287: DiffLedgerBatch aggregate stats ---

func makeBatch(startSeq, endSeq uint32) *xdr.LedgerCloseMetaBatch {
	var metas []xdr.LedgerCloseMeta
	for seq := startSeq; seq <= endSeq; seq++ {
		header := xdr.LedgerHeader{LedgerSeq: xdr.Uint32(seq)}
		metas = append(metas, xdr.LedgerCloseMeta{
			V: 0,
			V0: &xdr.LedgerCloseMetaV0{
				LedgerHeader: xdr.LedgerHeaderHistoryEntry{Header: header},
			},
		})
	}
	return &xdr.LedgerCloseMetaBatch{
		StartSequence:    xdr.Uint32(startSeq),
		EndSequence:      xdr.Uint32(endSeq),
		LedgerCloseMetas: metas,
	}
}

func TestDiffLedgerBatchLedgerCount(t *testing.T) {
	t.Parallel()

	left := makeBatch(100, 109) // 10 ledgers
	right := makeBatch(200, 214) // 15 ledgers

	summary := lens.DiffLedgerBatch(left, right)
	if summary == nil {
		t.Fatal("DiffLedgerBatch returned nil")
	}
	if summary.LeftLedgerCount != 10 {
		t.Errorf("LeftLedgerCount = %d; want 10", summary.LeftLedgerCount)
	}
	if summary.RightLedgerCount != 15 {
		t.Errorf("RightLedgerCount = %d; want 15", summary.RightLedgerCount)
	}
	if summary.LedgerCountDelta != 5 {
		t.Errorf("LedgerCountDelta = %d; want 5", summary.LedgerCountDelta)
	}
	if uint32(summary.LeftStartSequence) != 100 {
		t.Errorf("LeftStartSequence = %d; want 100", summary.LeftStartSequence)
	}
	if uint32(summary.RightEndSequence) != 214 {
		t.Errorf("RightEndSequence = %d; want 214", summary.RightEndSequence)
	}
}

func TestDiffLedgerBatchNilBatches(t *testing.T) {
	t.Parallel()
	summary := lens.DiffLedgerBatch(nil, nil)
	if summary == nil {
		t.Fatal("DiffLedgerBatch(nil,nil) returned nil")
	}
	if summary.LeftLedgerCount != 0 || summary.RightLedgerCount != 0 {
		t.Errorf("unexpected non-zero counts for nil batches: %+v", summary)
	}
}

