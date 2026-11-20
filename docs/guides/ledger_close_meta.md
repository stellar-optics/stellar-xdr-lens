# LedgerCloseMeta & Bucket List Storage Internals for Indexer Developers

Developers building indexers (such as custom ingestion pipelines, explorer backends, analytics engines, or compliance trackers) for the Stellar network consume `LedgerCloseMeta` streams emitted by `stellar-core`. These payloads encapsulate all information produced during ledger closure: transaction execution, fee processing, ledger entry changes, Soroban events, and state archival eviction.

This guide details how `stellar-xdr-lens` decomposes `LedgerCloseMeta`, explains ledger state transitions and bucket list storage internals, and provides complete Go code for indexer integrations.

---

## Table of Contents

- [The Anatomy of LedgerCloseMeta](#the-anatomy-of-ledgerclosemeta)
- [Protocol Evolution: v0, v1, and v2](#protocol-evolution-v0-v1-and-v2)
  - [LedgerCloseMetaV0 (Classic Stellar)](#ledgerclosemetav0-classic-stellar)
  - [LedgerCloseMetaV1 (Protocol 20 Soroban Genesis)](#ledgerclosemetav1-protocol-20-soroban-genesis)
  - [LedgerCloseMetaV2 (Protocol 20+ State Archival & Eviction)](#ledgerclosemetav2-protocol-20-state-archival--eviction)
- [Decomposing Ledger State Changes](#decomposing-ledger-state-changes)
  - [LedgerEntryChange Transitions](#ledgerentrychange-transitions)
  - [State Snapshot Sequence in Ingestion](#state-snapshot-sequence-in-ingestion)
- [Bucket List & State Archival Mechanics](#bucket-list--state-archival-mechanics)
  - [Bucket List Hash Tree & BucketEntry Types](#bucket-list-hash-tree--bucketentry-types)
  - [Hot Archive vs. Live State](#hot-archive-vs-live-state)
  - [State Archival Lifecycle: TTL, Eviction, & Rent](#state-archival-lifecycle-ttl-eviction--rent)
- [Using `pkg/lens` as a Library in Indexers](#using-pkglens-as-a-library-in-indexers)
  - [Why Use `pkg/lens` Over Raw SDK Reflection](#why-use-pkglens-over-raw-sdk-reflection)
  - [Code Example 1: Ingestion & Transaction Processing](#code-example-1-ingestion--transaction-processing)
  - [Code Example 2: Net Balance Delta Extraction](#code-example-2-net-balance-delta-extraction)
  - [Code Example 3: Parsing LedgerCloseMetaBatch Streams](#code-example-3-parsing-ledgerclosemetabatch-streams)
- [Summary Table](#summary-table)

---

## The Anatomy of LedgerCloseMeta

Whenever a Stellar ledger closes, consensus determines a valid transaction set and applies it to the preceding ledger state. The resulting output is an XDR union:

```text
union LedgerCloseMeta switch (int v)
{
case 0:
    LedgerCloseMetaV0 v0;
case 1:
    LedgerCloseMetaV1 v1;
case 2:
    LedgerCloseMetaV2 v2;
};
```

Regardless of version, each arm contains:
1. **Ledger Header:** Sequence number, closing timestamp, base fee, protocol version, total coins, and bucket list cryptographic root hash.
2. **Transaction Processing:** Pairs of envelopes, results, and fine-grained metadata (`FeeProcessing` and `TxApplyProcessing`).
3. **Upgrades & Eviction:** Protocol parameter changes, validator votes, and evicted ledger keys.

---

## Protocol Evolution: v0, v1, and v2

### LedgerCloseMetaV0 (Classic Stellar)

Pre-Protocol 20 ledgers emit `LedgerCloseMetaV0`.

- **Structure:**
  - `LedgerHeaderHistoryEntry`: Contains the historical header and hash.
  - `TxSet`: Classic transaction set (`TransactionSet`).
  - `TxProcessing`: Array of `TransactionResultMeta` pairing each transaction envelope with its result and `TransactionMeta` (v0 or v1).
  - `UpgradesProcessing`: Ledger entry changes produced by protocol upgrades (such as reserve or fee parameter changes).
  - `ScpInfo`: Consensus quorum and ballot messages.

### LedgerCloseMetaV1 (Protocol 20 Soroban Genesis)

Introduced with Soroban smart contracts in Protocol 20.

- **Key Innovations:**
  - **Generalized Transaction Set (`GeneralizedTransactionSet`):** Transactions are grouped into scheduling stages and components (phases) to accommodate concurrent Soroban execution.
  - **Soroban Metadata Integration:** Emits `TransactionMetaV3`, introducing Soroban return values, diagnostic events, contract events, and storage footprint changes.
  - **Contract Engine Metrics:** CPU instruction and RAM usage tracking per transaction.

### LedgerCloseMetaV2 (Protocol 20+ State Archival & Eviction)

Introduced to support scalable state archival and contract storage tiers.

- **Key Innovations:**
  - **`EvictedLedgerKeys` Array:** Explicit list of `LedgerKey` entries that expired during ledger close and were pruned from the live bucket list into the hot archive.
  - **Separate Upgrade Processing:** Dedicated structures for tracking protocol upgrades and rent fee distributions.
  - **Optimized Transaction Execution Metadata:** Includes `TransactionMetaV4` with enhanced operation metadata structures and state change ordering.

---

## Decomposing Ledger State Changes

When transactions execute, mutations to ledger state are recorded as `LedgerEntryChange` slices.

### LedgerEntryChange Transitions

An indexer must track the 5 fundamental state change types:

| Transition | Enum Value | Meaning | Contents |
|---|---|---|---|
| **CREATED** | `LedgerEntryCreated` | A new entry is inserted into ledger state for the first time. | Carries the complete newly created `LedgerEntry`. |
| **UPDATED** | `LedgerEntryUpdated` | An existing ledger entry was modified. | Carries the updated `LedgerEntry`. (Prior state is deduced from preceding `STATE` entry or previous ledger). |
| **DELETED** | `LedgerEntryRemoved` | An entry was removed from the ledger (e.g. merged account, closed trustline, burned balance). | Carries the `LedgerKey` of the deleted entry. |
| **STATE** | `LedgerEntryState` | The untouched snapshot of the entry before operation execution. | Carries the baseline `LedgerEntry`. |
| **RESTORED** | `LedgerEntryRestored` | An archived contract or storage key was restored to the live bucket list via `RestoreFootprintOp`. | Carries the newly revived `LedgerEntry`. |

### State Snapshot Sequence in Ingestion

In `TransactionMetaV3` and `TransactionMetaV4`, changes are segmented into:
1. `TxChangesBefore`: State entries snapshot before the transaction begins (typically `STATE` changes).
2. `Operations[i].Changes`: Incremental state mutations produced by operation `i`.
3. `TxChangesAfter`: Final updates following operation execution (e.g. final sequence increments or fee adjustments).

---

## Bucket List & State Archival Mechanics

Stellar Core stores live ledger entries in the **Bucket List**, an authenticated log-structured hash tree consisting of historical levels of exponentially increasing size.

### Bucket List Hash Tree & BucketEntry Types

The Bucket List is composed of `BucketEntry` records:

- `LIVEENTRY`: Active ledger state entry (account, trustline, offer, claimable balance, contract data, contract code).
- `INITENTRY`: Newly created entry pending merge down the bucket list levels.
- `DEADENTRY` (Tombstone): Records a deleted `LedgerKey`. When merged against older levels, it erases the older entry.
- `METAENTRY`: Metadata describing the bucket, including protocol ledger version.

### Hot Archive vs. Live State

Protocol 20+ separates state into:
- **Live Bucket List:** Fast, memory-mapped storage containing active accounts and contract entries within their active TTL.
- **Hot Archive Bucket List:** Persistent store containing expired/evicted contract entries. Entries in the archive cannot be read or invoked directly until restored.
  - `HOT_ARCHIVE_METAENTRY`: Archival metadata.
  - `HOT_ARCHIVE_ARCHIVED`: The preserved archived data entry.
  - `HOT_ARCHIVE_LIVE`: Restored key entry indicating the state has returned to the live list.

### State Archival Lifecycle: TTL, Eviction, & Rent

1. **`TTLEntry`:** Every Soroban contract data and code entry is paired with a `TTLEntry` recording `LiveUntilLedgerSeq`.
2. **Durability Tiers:**
   - **Instance & Persistent:** Long-term storage. Can be evicted to the hot archive upon TTL expiration and revived via `RestoreFootprintOp`.
   - **Temporary:** Short-term storage (e.g. price oracles, authorization nonces). Permanently deleted when TTL expires without revival.
3. **Rent Payments & Extension:** Developers call `ExtendFootprintTTLOp` to extend `LiveUntilLedgerSeq`, paying rent in XLM proportional to the byte size and ledger span.

---

## Using `pkg/lens` as a Library in Indexers

### Why Use `pkg/lens` Over Raw SDK Reflection

Writing raw indexer code against the Go Stellar SDK requires recursive reflection across dozens of nested structs, pointers, and union switches.

`pkg/lens` simplifies indexer development by providing:
- **Unified Decoders:** Safely parses base64, hex, or stream chunks into typed structs with automatic type detection.
- **Pre-computed Balance Deltas:** `DiffTransactionMetaBalances` automatically tracks and resolves net balance changes across complex operations.
- **Neutral Node Tree:** Inspect and extract arbitrary paths (`node.Child("Tx").Child("Fee")`) without type casting.

### Code Example 1: Ingestion & Transaction Processing

Here is a complete, runnable Go indexer routine processing `LedgerCloseMeta`:

```go
package main

import (
	"fmt"
	"log"

	"github.com/stellar/go-stellar-sdk/xdr"
	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

func ProcessLedgerPayload(base64Payload string) error {
	// Decode LedgerCloseMeta using lens
	value, err := lens.DecodeAs(base64Payload, "LedgerCloseMeta")
	if err != nil {
		return fmt.Errorf("decoding ledger close meta: %w", err)
	}

	meta, ok := value.Raw.(*xdr.LedgerCloseMeta)
	if !ok {
		return fmt.Errorf("unexpected payload type: %s", value.Type)
	}

	seq := meta.LedgerSequence()
	txCount := meta.CountTransactions()
	fmt.Printf("Ingesting Ledger %d (%d transactions)\n", seq, txCount)

	// Iterate across transactions
	for i := 0; i < txCount; i++ {
		txHash := meta.TransactionHash(i)
		envelope := meta.TransactionEnvelopes()[i]
		txMeta := meta.TxApplyProcessing(i)

		fmt.Printf("  Tx [%d] Hash: %x\n", i, txHash)
		fmt.Printf("    Envelope Type: %v\n", envelope.Type)

		// Check Soroban Contract Events if present in metadata
		if txMeta.V3 != nil {
			for _, event := range txMeta.V3.Events {
				fmt.Printf("    Contract Event: Type=%v Contract=%v\n", 
					event.Type, event.ContractId)
			}
		}
	}

	// Inspect evicted keys in Protocol 20+ v2 metadata
	if meta.V == 2 && meta.V2 != nil {
		if len(meta.V2.EvictedLedgerKeys) > 0 {
			fmt.Printf("  Evicted %d expired keys to hot archive\n", 
				len(meta.V2.EvictedLedgerKeys))
		}
	}

	return nil
}
```

### Code Example 2: Net Balance Delta Extraction

Indexers updating account and token balances can extract net balance deltas from metadata directly:

```go
func IndexBalanceUpdates(txMeta *xdr.TransactionMeta) error {
	// lens calculates net deltas across accounts, trustlines, and claimable balances
	diffResult, err := lens.DiffTransactionMetaBalances(txMeta)
	if err != nil {
		return fmt.Errorf("calculating balance diffs: %w", err)
	}

	for _, delta := range diffResult.Changes {
		fmt.Printf("Holder: %s | Asset: %s | Delta: %d stroops (Before: %d -> After: %d)\n",
			delta.Holder, delta.Asset, delta.Delta, delta.Before, delta.After)
	}
	return nil
}
```

### Code Example 3: Parsing LedgerCloseMetaBatch Streams

For historical backfills and bulk data pipelines:

```go
func IngestBatchFile(batchBase64 string) error {
	value, err := lens.DecodeAs(batchBase64, "LedgerCloseMetaBatch")
	if err != nil {
		return err
	}

	batch := value.Raw.(*xdr.LedgerCloseMetaBatch)
	fmt.Printf("Batch Range: Ledgers %d to %d\n", batch.StartSequence, batch.EndSequence)

	for _, ledgerMeta := range batch.LedgerCloseMetas {
		fmt.Printf("  -> Close time: %d\n", ledgerMeta.LedgerCloseTime())
	}
	return nil
}
```

---

## Summary Table

| Concept | Key Identifier | Indexer Responsibility |
|---|---|---|
| **Ledger Close Meta** | `LedgerCloseMeta` (v0, v1, v2) | Parse ledger header, extract transactions, and read closing ledger sequence. |
| **State Entry Added** | `LedgerEntryCreated` | Insert new database record for account, contract data, or trustline. |
| **State Entry Modified**| `LedgerEntryUpdated` | Update existing record with new balance, sequence, or storage fields. |
| **State Entry Deleted** | `LedgerEntryRemoved` | Delete or mark tombstoned using the supplied `LedgerKey`. |
| **State Archival** | `TTLEntry`, `EvictedLedgerKeys` | Track `LiveUntilLedgerSeq`; move expired records from live table to archive table. |
| **Balance Reconciliation** | `lens.DiffTransactionMetaBalances` | Update financial balances atomically without manual arithmetic. |
