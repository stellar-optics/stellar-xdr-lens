# stellar-xdr-lens

A Go CLI and library that explains Stellar and Soroban XDR in plain English, and diffs it structurally.

[![CI](https://github.com/stellar-optics/stellar-xdr-lens/actions/workflows/ci.yml/badge.svg)](https://github.com/stellar-optics/stellar-xdr-lens/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/stellar-optics/stellar-xdr-lens.svg)](https://pkg.go.dev/github.com/stellar-optics/stellar-xdr-lens)
[![Go Report Card](https://goreportcard.com/badge/github.com/stellar-optics/stellar-xdr-lens)](https://goreportcard.com/report/github.com/stellar-optics/stellar-xdr-lens)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

## Why this exists

When a Stellar transaction fails, you get a result code like `tx_failed` wrapped
around `opINNER` wrapped around `InvokeHostFunctionResultCodeInvokeHostFunctionTrapped`.
Working out *which operation* failed, *what it was trying to do*, and *what to
do about it* means decoding two separate blobs and correlating them by hand.

`lens` does that correlation for you: 

```console
$ lens explain "$ENVELOPE" --result "$RESULT"
Transaction failed at operation 0, invoke_host_function (call harvest on contract CBGSB…KKY3 with 2 argument(s)): The contract trapped: it panicked, or explicitly returned an error. 
```

### Honest scope: what already exists

The official [`stellar` CLI](https://developers.stellar.org/docs/tools/cli/stellar-cli)
already ships `stellar xdr decode`, `guess`, `compare` and `encode`. **If you
just need to turn a blob into JSON, use that** — it is maintained by the SDF,
covers every XDR type in the protocol, and is probably already installed.

`lens` exists for the three things that tool does not do:

| | `stellar xdr` | `lens` |
|---|---|---|
| Decode to JSON | ✅ | ✅ |
| Guess the type | ✅ | ✅ |
| **Explain a failure in plain English** | ❌ | ✅ |
| **Pair an envelope with its result** | ❌ | ✅ |
| **Structural, path-level diff** | ordering only (`-1`/`0`/`1`) | ✅ full path diff |
| **Reusable Go library** | ❌ (Rust) | ✅ `pkg/lens` |

That last row matters: Horizon, ingestion pipelines and most Stellar backend
infrastructure are written in Go, and there was no Go library for any of this.

## Install

```sh
go install github.com/stellar-optics/stellar-xdr-lens/cmd/lens@latest
```

Requires Go 1.25 or later. Or build from source:

```sh
git clone https://github.com/stellar-optics/stellar-xdr-lens
cd stellar-xdr-lens
go build ./cmd/lens
```

Everything runs offline. `lens` makes no network calls, which makes it safe on
unsigned transactions and deterministic in CI.

#### Terminal colors

Human-readable terminal output uses the dark color theme by default. Set
`LENS_THEME=light` for a palette suited to light terminal backgrounds; set it
to `dark` to select the default palette explicitly. Customize individual ANSI
styles with `LENS_THEME_DARK_KEY`, `LENS_THEME_LIGHT_ADD`, and the equivalent
`KEY`, `TYPE`, `STR`, `NUM`, `NOTE`, `DIM`, `ADD`, `REMOVE`, and `CHANGE`
variables. Values are SGR parameters without escape characters, for example:

```sh
LENS_THEME=dark LENS_THEME_DARK_KEY='38;5;39' lens decode "$XDR"
```

Set a style variable to an empty string to disable that style. `--color auto`
continues to color only terminal output, while `--color always` and
`--color never` explicitly enable or disable colors. Invalid theme values or
style parameters produce an error. `NO_COLOR` remains honored in automatic
mode.

## Contributor validation

The CLI is exercised by subprocess tests in `cmd/lens/e2e_test.go` against the
canonical fixtures in `testdata/`. These checks run the real command binary, not
library call wrappers, and intentionally scrub proxy environment variables to
prove the core execution path stays offline and deterministic in CI.

```sh
go test ./cmd/lens
```

## Usage

### `lens explain` — why did this fail?

Pair an envelope with its result and `lens` names the failing operation, what it
was attempting, and what to do next:

```console
$ lens explain --file envelope.xdr --result-file result.xdr
Transaction failed at operation 0, invoke_host_function (call harvest on contract CBGSB…KKY3 with 2 argument(s)): The contract trapped: it panicked, or explicitly returned an error.

  Source           GAPB7YMUQ6MPQ23PTFDNBZP7HGGTZPI2DT5JJURUOIXNPLGHW2O3WYXZ
  Fee-bump source  GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAW
  Sequence         264125967419700139
  Fee bid          0.0024167 XLM (24167 stroops)
  Fee charged      0.0013355 XLM (13355 stroops)
  Signatures       1
  Precondition     valid until 2026-08-03T13:33:39Z

Operations (1)
  ✗ [0] invoke_host_function — call harvest on contract CBGSB…KKY3 with 2 argument(s)
      invoke_host_function_trapped: The contract trapped: it panicked, or explicitly returned an error.
      → This is a failure inside the contract itself, not a protocol-level rejection. Run the call through simulation and read the diagnostic events to find the contract-level cause.

Outcome
  tx_fee_bump_inner_failed: The fee-bump wrapper was valid, but the inner transaction it paid for failed.
    → The fee was still charged to the fee-bump source. Inspect the inner result for the real cause.
  inner transaction:
  tx_failed: The transaction was well-formed and authorised, but at least one of its operations failed.
    → Check the per-operation results: the transaction itself is fine, the failure is inside an operation.
```

All 208 protocol result codes have a written explanation. When an envelope is
paired with its result, `set_options`, liquidity-pool and `account_merge`
failures are also diagnosed from the operation's own parameters: which
threshold weight exceeded 255, which flag bit is contradictory or
unrecognised, whether the signer is the source account itself, whether the
pool's price bounds left any room for slippage, and which deposit or
withdrawal bound is malformed. An `account_merge` failure names the subentry
classes that still block the merge — active trustlines, open DEX offers, data
entries and claimable balances — and the operations that close them, plus the
ledger-sequence bound behind `seqnum_too_far`, the destination's lumen buying
liabilities behind `dest_full`, and the fact that `auth_immutable` can never
be cleared. Use `--fail-on-error` to exit with 2 when the transaction failed,
which makes this usable as a CI gate.

For `manage_data` failures, the per-operation result also carries a context
line: the entry key pulled from the envelope, and for `manage_data_low_reserve`
the reserve the new subentry would lock.

```console
$ lens explain --file envelope.xdr --result-file result.xdr
...
  ✗ [0] manage_data — set data entry "profile" (7 bytes)
      manage_data_low_reserve: The account cannot meet the reserve required for another data entry.
      data entry "profile" adds one subentry, and each subentry locks 0.5 XLM of reserve; the account cannot lock another 0.5 XLM.
      → Each data entry is a subentry and locks 0.5 XLM of base reserve. Free up reserve or remove another entry, then retry.
```

For `create_claimable_balance` failures the same context line names the asset,
the amount, the source account and the claimants, and for
`create_claimable_balance_low_reserve` it computes the native reserve the
balance locks: two base reserves for the entry plus one per claimant, at
0.5 XLM each. This distinguishes a missing creator trustline from a claimant
who cannot receive the asset, and it is computed entirely from the envelope.

The same detail appears as `context` in `--json` output and in the Markdown
result cell, and it is resolved entirely from the decoded envelope — no network
call is made.

For a GitHub-flavored Markdown report with transaction details and one row per
operation, add `--markdown`:

```console
$ lens explain --file envelope.xdr --result-file result.xdr --markdown
```

The output contains a transaction-details table and an operations table with
each operation's description, result, status, and hint. It is deterministic and
offline, and can be redirected into an issue, pull request, or other Markdown
document.

#### Soroban resource and fee breakdown

`lens explain --resources` adds a Soroban resource table to the summary. It
compares the transaction's declared instructions, I/O, entry counts and
footprint with the Mainnet Protocol 27 per-transaction limits, and marks any
value over its limit as `EXCEEDED`:

```console
$ lens explain "$SOROBAN_ENVELOPE" --resources
Soroban resource breakdown (per-transaction limits)
RESOURCE                 USED       LIMIT           USAGE  STATUS
Instructions (CPU)       6,688,817  400,000,000     1.7%   ok
Memory                   n/a        41,943,040      -      not declared
Disk read bytes          116        200,000         0.1%   ok
Write bytes              116        132,096         0.1%   ok
Disk read entries        10         200             5.0%   ok
Write entries            6          200             3.0%   ok
Footprint entries        16         400             4.0%   ok
...
```

The table is derived from the envelope alone; add `--meta` (or `--meta-file`)
to include the charged non-refundable, refundable and rent fees from
transaction metadata. Soroban XDR does not declare memory usage, so that row is
shown as `n/a` instead of pretending it is zero. In `--json` mode, `--resources`
adds a top-level `resources` object next to the summary fields.

### `lens decode` — read the structure

```console
$ lens decode --depth 3 --file envelope.xdr
TransactionEnvelope
├─ Type: EnvelopeTypeEnvelopeTypeTx
└─ V1
   ├─ Tx
   │  ├─ SourceAccount: GCJNHFXJEQEY54NPKU4DBMT5WOBJUHMYSFBDAQF4OMTEGJN4BQU7ZST6
   │  ├─ Fee: 58002
   │  ├─ SeqNum: 259021488982594615
   │  ├─ Cond
   │  │  ... 1 more level(s) hidden
   │  ├─ Memo
   │  │  ... 1 more level(s) hidden
   │  ├─ Operations  [1]
   │  │  ... 1 more level(s) hidden
   │  └─ Ext
   │     ... 1 more level(s) hidden
   └─ Signatures  [1]
      └─ [0]
         ... 1 more level(s) hidden
```

Accounts render as strkey, amounts as XLM, timestamps as RFC 3339, and enums as
their constant names — not raw byte arrays and integers.

Claimable balances render their predicate trees as boolean expressions instead
of nested union arms, and the balance id carries its claimant count and asset:

```console
$ lens decode --type ClaimableBalanceEntry "$BALANCE"
ClaimableBalanceEntry
├─ BalanceId: BAAEEQSCIJBEEQSCIJBEEQSCIJBEEQSCIJBEEQSCIJBEEQSCIJBEEQQSBM  3 claimants; asset USDC:GA2J…FHVAW
├─ Claimants  [3]
│  ├─ [0]
│  │  └─ V0
│  │     ├─ Destination: GAPB7YMUQ6MPQ23PTFDNBZP7HGGTZPI2DT5JJURUOIXNPLGHW2O3WYXZ
│  │     └─ Predicate: before 2025-01-01T00:00:00Z
│  └─ [2]
│     └─ V0
│        └─ Predicate: (before 2026-01-01T00:00:00Z or unconditional)
└─ Asset: USDC:GA2J…FHVAW
```

`lens decode --type ContractDataEntry` decodes Soroban contract data and names
its storage level. `persistent` and `temporary` come from the wire durability;
instance storage keeps persistent durability on the wire and is recognised by
its special key:

```console
$ lens decode --type ContractDataEntry "$TEMP"
ContractDataEntry
├─ Ext
│  └─ V: 0
├─ Contract: CDFP5OV6AAAQEAYEAUDAOCAJBIFQYDIOB4IBCEQTCQKRMFYYDENBWGZ3
├─ Key
│  ├─ Type: ScValTypeScvSymbol
│  └─ Sym: SEEN
├─ Durability: temporary
└─ Val
   ├─ Type: ScValTypeScvBool
   └─ B: true
```

For an instance entry the same fields render as `Key: instance storage key`
with `Durability: persistent`, which is what separates instance storage from an
ordinary persistent entry. A contract instance's `Executable` also names its
implementation — `wasm:<hex>` for a WASM module, `stellar asset contract
(built-in token)` for the built-in token, and the owner plus tag for Protocol
23 external references — instead of stopping at a union discriminant.

`lens decode --type LiquidityPoolEntry` names the market a constant product
pool trades and renders its share supply as pool share tokens, not lumens:

```console
$ lens decode --type LiquidityPoolEntry "$POOL"
LiquidityPoolEntry
├─ LiquidityPoolId: 41…41  constant product pool: USDC:GA2J… / native @ 0.3%
└─ Body
   └─ ConstantProduct
      ├─ Params: USDC:GA2J… / native @ 0.3%
      ├─ ReserveA: 1000000000000  100000.0000000 (stroops: 1000000000000)
      ├─ ReserveB: 5000000000000  500000.0000000 (stroops: 5000000000000)
      ├─ TotalPoolShares: 2000000000000  200000.0000000 pool share tokens (stroops: 2000000000000)
      └─ PoolSharesTrustLineCount: 150  held by 150 trustlines
```

A fee-bump envelope cannot answer "who paid, and did they actually sign this?"
from the wire alone: signature hints and the inner transaction's hash are
derived. `lens decode` appends them next to the untouched `Tx` and `Signatures`
fields, putting the fee payer's 4-byte key hint beside its strkey and checking
each outer signature hint against it:

```console
$ lens decode --file fee-bump-envelope.xdr  # excerpt
├─ FeeSource: GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAW
├─ Fee: 45206
├─ InnerTx
│  └─ ...
├─ FeeSourceHint: 01d84053  4-byte key hint of fee source GA2JR…HVAW
├─ InnerTxHash: ce6d04d8e3d2235664fefee5e6bb8270920f7c24df286346697c050f8f7c9970  sha256 of 00000002 (ENVELOPE_TYPE_TX) followed by the inner tx XDR; the signed inner hash prefixes the 32-byte network id
├─ Signatures  [1]
│  └─ [0]
│     ├─ Hint: 01d84053
│     └─ Signature: ...  d35ae3bec7e70078...
└─ SignatureHints  [1]
   └─ [0]: 01d84053  matches GA2JR…HVAW key hint
```

`InnerTxHash` fingerprints the inner transaction without the network id: the
signature preimage prefixes it with the raw 32-byte network id, which an
offline decode has no passphrase for, so use the digest to identify the inner
transaction rather than as the explorer's transaction hash. An outer signature
whose hint does not match is another signer of the fee source account, not a
wrong signature, and the note says so.
A transaction's `LedgerFootprint` is flat on the wire, so `lens decode` also
groups its keys by storage level after the untouched `ReadOnly` and `ReadWrite`
lists. Each grouped entry points at its wire position and names the contract it
belongs to (contract code keys show their WASM hash instead):

```console
$ lens decode --file soroban-envelope.xdr  # footprint excerpt
Footprint
├─ ReadOnly  [10]
│  └─ ...
├─ ReadWrite  [6]
│  └─ ...
├─ Instance  [3]
│  ├─ [0]: readOnly[0]  contract data for CBGSBKYMYO6OMGHQXXNOBRGVUDFUDVC2XLC3SXON5R2SNXILR7XCKKY3
│  └─ ...
├─ Temporary  [10]
│  └─ ...
└─ Other  [3]
   ├─ [0]: readOnly[8]  contract code 70fe44694c9fe6b0abc69a6da4858fc2aaba04fa10492a466a1d426d04ca8560
   └─ ...
```

`Instance` holds persistent keys that use the contract instance key; `Other`
holds keys with no Soroban durability, such as trust lines and contract code,
so no entry is dropped.

#### Alternative Formats and Interactive Explorer

```console
# Terse single-line summary suitable for grep and shell pipelines
$ lens decode --summary --file envelope.xdr
# or: lens decode --format summary --file envelope.xdr

# SEP-0011 TxRep human-readable format
$ lens decode --txrep --file envelope.xdr
# or: lens decode --format txrep --file envelope.xdr

# Flat CSV row of the high-level transaction fields
$ lens decode --csv --file envelope.xdr
# or: lens decode --format csv --file envelope.xdr

# JUnit XML test report for CI gatekeeping
$ lens decode --format junit --file result.xdr > junit.xml

# Interactive terminal tree explorer (TUI)
$ lens decode -i --file envelope.xdr
# or use the dedicated command:
$ lens explore --file envelope.xdr

# Contract spec-aware ScVal decoding from contract WASM or spec XDR
$ lens decode --spec contract.wasm --file invocation.xdr
```

#### Batch decoding

`lens decode --batch` walks a directory or a shell-style glob and decodes every
regular file it finds. Files are processed in sorted order, each one gets a
`=== <path> ===` header, and one bad file does not stop the rest:

```console
$ lens decode --batch 'captures/*.env.txt' --json
=== captures/002.env.txt ===
{ "type": "TransactionEnvelope", ... }

=== captures/003.bad.txt ===
error: decoding base64: illegal base64 data at input byte 0

Batch summary: 2 file(s) processed, 1 succeeded, 1 failed
```

The command exits non-zero when any file fails: `1` for read/format failures and
`3` when every failure was invalid XDR, so pipelines can still distinguish
garbage bytes from a broken path. `--batch` cannot be combined with `--stream`,
`--multi`, `--interactive`, `--file` or a positional payload.

#### CSV export

`lens decode --csv` prints one fixed-schema row per value, which is what a
batch export over thousands of transactions wants:

```csv
type,variant,source,fee_bump_source,seq_num,fee,memo,signatures,operation_count,operation_types,fee_charged,success,result_code,inner_result_code,failed_operations,detail
TransactionEnvelope,fee_bump,GAPB7YMUQ6MPQ23PTFDNBZP7HGGTZPI2DT5JJURUOIXNPLGHW2O3WYXZ,GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAW,264125967419700139,24167,,1,1,invoke_host_function,,,,,,
```

Every row carries the same number of fields, so rows from different
invocations concatenate and still parse: a column that does not apply — the
fee bid of a bare `TransactionResult`, say — is an empty field rather than a
missing one. `Type` is the XDR type name, `variant` is the envelope shape
(`v0`, `v1`, `fee_bump`), and non-transaction types set `detail` instead of
the transaction columns. Output is RFC 4180, so memos containing commas,
quotes or newlines round-trip through any CSV reader.

Each `lens decode --csv` call emits the header once. To export a batch under a
single header, use the library, which is also how the pairing works:

```go
summaries := make([]*lens.Summary, 0, len(rows))
for _, row := range rows {
	s, err := lens.ExplainPair(row.envelope, row.result)
	if err != nil {
		return err
	}
	summaries = append(summaries, s)
}

f := format.NewCSVFormatter()
if err := f.FormatSummaries(os.Stdout, summaries); err != nil {
	return err
}
```

`WithCSVHeader(false)` drops the header for appends, and `WithCSVDelimiter`
switches the field delimiter — the column order, and the names scripts address
by position, are pinned by tests and are a breaking change to alter.

#### Streaming and Canonical Validation

```console
# Stream framed XDR records (RFC 5531 or length-prefixed) without buffering the whole file
$ lens decode --stream --file ledger_archive.xdr

# Verify that XDR input adheres strictly to canonical RFC 4506 encoding (no padding leaks)
$ lens decode --canonical "$XDR"
```

### `lens diff` — what changed?

```console
$ lens diff before.xdr after.xdr
TransactionResult

~ FeeCharged
    - 35438 0.0035438 (stroops: 35438)
    + 13355 0.0013355 (stroops: 13355)
~ Result.Code
    - TransactionResultCodeTxFeeBumpInnerSuccess
    + TransactionResultCodeTxFeeBumpInnerFailed
~ Result.InnerResultPair.Result.Result.Results[0].Tr.InvokeHostFunctionResult.Code
    - InvokeHostFunctionResultCodeInvokeHostFunctionSuccess
    + InvokeHostFunctionResultCodeInvokeHostFunctionTrapped
- Result.InnerResultPair.Result.Result.Results[0].Tr.InvokeHostFunctionResult.Success = ea29da78…

5 changed, 1 removed
```

Use `--exit-code` to exit with 2 when the values differ, mirroring `diff(1)`.
The default compares ordered lists by index. Use `--align` to opt into LCS
matching for unchanged list elements when insertions or removals would cause a
cascade of positional changes. `--semantic` compares ScVal wide integers as
exact decimal values and matches recursive map entries by their canonical
XDR keys. These options can be combined:

```console
$ lens diff --align --semantic before.xdr after.xdr
```

Three modes compare the values differently rather than printing them
differently:

```console
# Signatures and signers are sets, not sequences: reordering them is not a diff
$ lens diff --unordered-signers before.xdr after.xdr

# Set-theoretic (Venn) difference of two Soroban ledger footprints
$ lens diff --footprint before.env.xdr after.env.xdr

# Ignore minor numeric noise (e.g. sequence numbers, timestamps, base fees) within a delta
$ lens diff --diff-threshold 10 before.xdr after.xdr

# Just the statistics: how many changes, of what kind, in which section
$ lens diff --stats before.xdr after.xdr
TransactionResult
6 change(s): 5 modified, 1 removed
  FeeCharged: 1
  Result: 5
```

`lens diff` also automatically normalizes binary data (such as Base64 vs Hex hashes and signatures), eliminating spurious diff lines when representations differ only by string encoding.

`--stats` derives everything from an existing diff, so it stays deterministic
and offline. It has a stable JSON form (`--stats --json`) with the total, the
per-kind counts, the per-section breakdown and the deepest changed path.

`--include-paths` and `--exclude-paths` narrow which changed paths are reported,
which is how a rebuilt envelope's sequence number and signatures stop drowning
out the change you care about. A pattern is a dot-separated field path; `*` and
`?` are glob metacharacters within a segment, `**` spans segments, and a pattern
also matches everything beneath it. Excludes win over includes.

```console
# Ignore the noise of a rebuild and report only what really changed
$ lens diff --exclude-paths Signatures,SeqNum before.xdr after.xdr

# Report only the top-level result code and the fee charged
$ lens diff --include-paths Result.Code,FeeCharged before.res after.res
```

`--redact` normalizes the dynamic cryptographic values that a rebuild cannot
reproduce — hashes, signatures and derived identifiers — before comparing, so
two values that differ only in those fields compare equal. `--redact` accepts
`hashes`, `signatures`, `identifiers` or `all` and is repeatable;
`--redact-fields` adds named fields and accepts globs. The paths that were
normalized are always reported, both in text and in the JSON `redacted` field.
Redaction is rejected with `--footprint`, which compares sets rather than
hydrated values.

```console
# Treat a rebuilt envelope's hashes and signatures as equal
$ lens diff --redact hashes,signatures before.env.xdr after.env.xdr

# Add a specific field to the redaction set (globs allowed)
$ lens diff --redact all --redact-fields PrevHash,LedgerHash* a.xdr b.xdr
```

Three formats render the difference for other tools. Each is mutually exclusive
with the others and with `--json`/`--stats`.

```console
# An RFC 6902 JSON Patch of the difference, ready for any JSON Patch consumer
$ lens diff --json-patch before.xdr after.xdr
[
  {"op": "replace", "path": "/Result/Code", "value": "TransactionResultCodeTxFailed"},
  {"op": "remove", "path": "/Result/InnerResultPair/Result/Result/Results/0/Tr/InvokeHostFunctionResult/Success"}
]

# A standard unified diff, suitable for posting as a pull-request review comment
$ lens diff --unified before.xdr after.xdr
--- a/TransactionResult
+++ b/TransactionResult
@@ -1,6 +1,6 @@
 . = TransactionResult
-FeeCharged = 35438 (0.0035438 (stroops: 35438))
+FeeCharged = 13355 (0.0013355 (stroops: 13355))

# A colourised two-column terminal view, removed on the left, added on the right
$ lens diff --side-by-side before.xdr after.xdr
```

Use `--context N` to bound the context lines for `--unified` and `--side-by-side`
(default 3), and `--side-by-side-width N` to set the total column width.

### `lens result-diff` — align an envelope with its result

`lens diff` compares like with like. `result-diff` compares a transaction with
the result it produced, pairing each operation with the operation result at the
same index and naming every place they disagree:

```console
$ lens result-diff envelope.xdr result.xdr
tx_fee_bump_inner_failed
  The fee-bump wrapper was valid, but the inner transaction it paid for failed.
  inner: tx_failed
Operations (1)
  ✗ [0] invoke_host_function — call harvest on contract CBGSB…KKY3 with 2 argument(s)
      invoke_host_function_trapped: The contract trapped: it panicked, or explicitly returned an error.
      → This is a failure inside the contract itself, not a protocol-level rejection. Run the call through simulation and read the diagnostic events to find the contract-level cause.
```

It reports mismatched operation and result types, operations that have no result
of their own, results with no matching operation, and each failed operation's
result code in plain English. `--json` emits the alignment as camelCase JSON and
`--exit-code` exits with 2 when the two disagree. Everything is computed from
the two supplied blobs; no network call is made.

### `lens meta-diff` — how did ledger balances change?

Transaction metadata carries ledger-entry state and changes. `meta-diff`
pairs those snapshots locally and reports account XLM, trustline, and
claimable-balance changes without contacting a ledger or RPC endpoint:

```console
$ lens meta-diff transaction-meta.xdr
G... XLM: 10.0000000 XLM -> 12.5000000 XLM (+2.5000000 XLM)
```

Use `--json` for machine-readable output. Balances are represented as signed
64-bit ledger units (`stroops` for XLM); `beforeKnown` is false when metadata
contains an after-value without its prior state. Created and removed entries
use zero on the absent side when the corresponding metadata record makes that
baseline known.

### `lens asset` — derive a Stellar Asset Contract id

Classic assets have deterministic Stellar Asset Contract ids: the network
passphrase and the asset are hashed into a `C…` address. `lens` computes it
offline, so you can go from an asset to its contract without an RPC call:

```console
$ lens asset "AAAAAVVTREMAAAAAO5kROA7+mIugqJAOsc/kTzZvfb6Ua+0HckD39iTfFcU="
Network      mainnet
Type         credit_alphanum4
Code         USDC
Issuer       GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN
Canonical    USDC:GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN
Contract ID  CCW67TSZV3SSS2HXMBQ5JFGCKJNXKZM7UQUWUZPUTHXSTZLEO7SJMI75
```

`--network` accepts `mainnet`, `testnet`, `futurenet`, or a raw passphrase —
the same asset has a different contract address on each network. `--json`
emits the details with camelCase keys.

### `lens types` — what can it decode?

`lens types` lists the registered type names. `--json` adds the category, a
one-line description, the detection priority and the minimum encoded size:

```console
$ lens types --json | jq -r '.[] | select(.category == "overlay") | .name'
Auth
AuthCert
DontHave
Error
Hello
PeerAddress
StellarMessage
```

### `lens encode` — build XDR from JSON

Encode simplified JSON specifications into base64-encoded XDR completely offline:

```console
# Round-trip a decode document straight back to XDR
$ lens decode --json "$XDR" | lens encode

# Encode a bare value as any registered type
$ lens encode --type ContractCodeEntry --file value.json

# Encode a transaction envelope from a JSON template
$ lens encode --tx '{
  "source_account": "GBRPYHIL2CI3FNQ4BXLFMNDLFJUNPU2HY3ZMFSHONUCEOASW7QC7OX2H",
  "fee": "100",
  "sequence": "1",
  "operations": [
    {
      "type": "payment",
      "destination": "GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAW",
      "amount": "10000000"
    }
  ]
}'

# Encode Soroban ScVal literals directly
$ lens encode --scval '{"symbol": "transfer"}'
$ lens encode --scval '{"vec": [1, 2, 3]}'

# Enforce strict schema validation (rejects unknown properties or invalid fields)
$ lens encode --strict --file tx_template.json
```

`lens encode` is symmetric with `lens decode`: it accepts the stable
`{"type": ..., "value": ...}` document or a raw value with `--type`, and
reproduces the canonical XDR. The `TransactionEnvelope` and `ScVal` simplified
dialects remain supported. Enrichments that intentionally collapse structure
into prose (notably claim predicates) return an error rather than inventing a
value; pass the original decoded value to the library's `lens.Encode` instead.

### `lens completion` — shell autocompletion

`lens` ships its own completion engine for bash, zsh, fish and PowerShell.
Subcommand names and persistent flags are completed on every level; dynamic
flags complete real values: `--type`/`-t` suggests every registered type name
(the same list `lens types` prints), and `--format`, `--color`, `--network`
and `--redact` offer their valid choices.

```console
# Load completions for the current shell session
$ source <(lens completion bash)      # bash
$ source <(lens completion zsh)       # zsh
$ lens completion fish | source       # fish
```

Install per-user by writing the script to your completion directory, e.g. for
zsh:

```sh
lens completion zsh > "${fpath[1]}/_lens"
```

`lens completion` replaces the generic cobra completion command, and the
hidden `__complete` entry point is exercised by `internal/cli/completion_test.go`.
Registrations run after the command tree is built, so every subcommand is
visible to `registerCompletions(root)`, which is called from `newRootCmd`.

### Piping and scripting

`lens` reads from an argument, a `--file`, or stdin, so it composes:

```sh
# If you already have a Horizon response, pipe its XDR field into lens
curl -s "https://horizon.stellar.org/transactions/$HASH" \
  | jq -r '.envelope_xdr' \
  | lens explain

# Pull one field out with jq
lens decode --json "$XDR" | jq -r '.value.V1.Tx.Fee'

# Gate a deployment on the transaction having succeeded
# (exit 2 = the transaction failed, exit 3 = undecodable XDR)
lens explain "$ENV" --result "$RES" --fail-on-error

# Hexadecimal input support
lens decode --hex "0x00000000..."

# Decode concatenated newline-separated streams (NDJSON or multi-document YAML)
lens decode --multi -f transactions.txt --json --compact

# Extract and disassemble embedded WebAssembly from Soroban transactions
lens decode --wasm -f contract_upload.xdr

# Decode into clean YAML
lens decode --yaml "$XDR"

# Encode YAML specification into XDR
lens encode -f spec.yaml
```

#### Global output controls

`--quiet` / `-q` suppresses standard output without changing the exit status.
It composes with the diff commands, so `lens diff --quiet a.xdr b.xdr` is a
silent "do these differ?" check that exits `2` when they do.

`--output` / `-o` writes the command's standard output to a file instead.
The file is written through a temporary file in the same directory and renamed
only after the command succeeds; a failed run leaves an existing file untouched.
Use `-o -` as an explicit synonym for standard output, and `--quiet` takes
precedence when both flags are supplied.

`lens` itself only processes the XDR supplied to it and makes no network calls.
The optional `curl` pipeline above is a caller-managed input step; use `--file`
or standard input when working entirely with local data.

The `--json` output has a documented, stable shape:

```json
{
  "type": "TransactionResult",
  "value": { "FeeCharged": 13355, "Result": { "Code": "TransactionResultCodeTxFeeBumpInnerFailed" } }
}
```

Absent union arms are omitted rather than emitted as `null`, so `jq` paths stay short.

`explain --json` has its own documented shape, in camelCase to match Horizon
and Soroban RPC:

```json
{
  "kind": "transaction",
  "headline": "Transaction failed at operation 0, invoke_host_function …",
  "fee": 24167,
  "feeCharged": 13355,
  "operations": [
    { "index": 0, "type": "invoke_host_function",
      "result": { "code": "invoke_host_function_trapped", "summary": "…", "hint": "…" } }
  ],
  "outcome": {
    "success": false,
    "reason": { "code": "tx_fee_bump_inner_failed", "…": "…" },
    "innerReason": { "code": "tx_failed", "…": "…" },
    "failedOperations": [0]
  }
}
```

These field names are part of the public interface — sibling tools and scripts
address them with `jq`, so renaming one is a breaking change. The CSV column
names in [`CSVColumns`](#csv-export) are part of the same contract.

### Exit codes

The CLI has a strict, pinned exit-code policy (`internal/cli/exitcodes.go`):

| Code | Meaning |
|------|---------|
| `0` | clean — the command did what was asked and the answer is "no" |
| `1` | generic error — bad flags, missing files, unreadable environments; anything not caused by the XDR itself |
| `2` | diff found — the command ran, but the checked condition is false: `diff --exit-code`/`--quiet` finding a difference, `explain --fail-on-error` seeing a failed transaction, or `result-diff --exit-code` seeing a disagreement |
| `3` | invalid XDR — the payload could not be decoded: bad base64, a malformed structure, no registered type matches, non-canonical encoding or a corrupted stream record |

Scripts therefore see exactly four outcomes: success, a check that returned
false, XDR they cannot read, and everything else. The classification is
`errors.Is`-based, so wrapped decode errors still resolve to code 3.
A `--batch` run with mixed failures exits `1`; it exits `3` only when every
failed file was invalid XDR.

> **Migration note.** The "diff found" condition now exits with **2**, and
> undecodable XDR exits with **3**. Earlier releases collapsed both into the
> generic **1** (and documented `diff(1)` semantics for it); CI that matched
> "any non-zero" keeps working, but scripts that compared against 1 must now
> expect 2 for a differing pair and 3 for garbage input.

### Stdin framing

Standard input is probed before it is parsed (`pkg/lens/stdin_pipe.go`, the
`lens.ReadStdinPayload` entry point): if the first bytes look like base64 or
hex text, the input is split on lines and comments (`#`) and decoded through
the multi-stream decoder; otherwise the bytes are treated as a binary framed
stream (RFC 5531 record marking or length-prefixed, auto-detected). Piping a
Horizon `envelope_xdr` field and piping a raw binary record to the same
commands both work without extra flags, and payloads larger than the default
`bufio` reader are handled correctly.

## Library use

The CLI is a thin shell over `pkg/lens`:

```go
package main

import (
	"fmt"
	"log"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

func main() {
	env, err := lens.Decode(envelopeB64)
	if err != nil {
		log.Fatal(err)
	}
	res, err := lens.Decode(resultB64)
	if err != nil {
		log.Fatal(err)
	}

	summary, err := lens.ExplainPair(env, res)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(summary.Headline)
	for _, op := range summary.Operations {
		if op.Result != nil && !op.Result.Success {
			fmt.Printf("op %d (%s) failed: %s\n", op.Index, op.Type, op.Result.Summary)
		}
	}
}
```

The diff helpers compose with the same decoded values. `lens.SummarizeDiff`
turns a `*lens.DiffResult` into counts and per-section statistics;
`lens.DiffSigners` and `lens.DiffDecoratedSignatures` compare signer and
signature collections as unordered multisets; `lens.DiffFootprints` computes
the read-only and read-write set differences of two Soroban footprints; and
`lens.DiffEnvelopeResult` aligns an envelope with its result, operation by
operation. None of them fetches or decodes anything beyond the values passed in.

Diff redaction is a library feature as well as a CLI flag. Set
`DiffOptions.Redaction` to a `lens.RedactionPolicy` (categories `Hashes`,
`Signatures`, `Identifiers`, plus name globs in `Fields`) and the trees are
deep-copied and normalized before the walk; the inputs are never mutated, and
`DiffResult.Redacted` lists the normalized paths.

The encoder and its round-trip verifier are public too. `lens.Encode` reverses
`lens.Decode`, while `lens.EncodeJSON` and `lens.EncodeJSONAs` accept the
stable decode document and return canonical base64 XDR. `lens.RoundTripBase64`
and `lens.RoundTripJSONBase64` verify the `decode -> encode -> decode` property
for a payload, `lens.RunRoundTripSuite` runs a named case list, and
`lens.RoundTripRegistry` exercises every registered type. Reports distinguish a
byte mismatch, a tree mismatch, and a route that is intentionally lossy.

The output formatters are reusable too. `format.NewJSONPatchFormatter` and
`format.NewUnifiedFormatter` turn a diff into an RFC 6902 patch or a unified
diff, and `format.NewSideBySideFormatter` produces the two-column view. A
`lens.NewPathFilter(include, exclude)` narrows any of them to the paths you care
about, either through `DiffOptions.Filter` before the diff or the formatter's
filter option afterwards.

See the [API reference](https://pkg.go.dev/github.com/stellar-optics/stellar-xdr-lens/pkg/lens)
and [docs/architecture.md](docs/architecture.md).

### Failure diagnostics library

Beyond `ExplainPair`, `pkg/lens` ships offline diagnostics for common failure
classes. They take decoded XDR, captured RPC text, and any required ledger
context from the caller; they make no network calls. Existing `lens explain`
flags and output formats are unchanged, and these context-dependent
helpers are available through the Go library.

```go
// 1. Insufficient balance: available = balance - selling liabilities - reserve.
bal, _ := lens.DiagnoseBalance(accountEntry, lens.DefaultBaseReserve, spendStroops)
fmt.Println(bal.Summary) // names the exact shortfall in stroops and XLM.

// 2. Missing signatures: which signer weights are still needed.
sig, _ := lens.DiagnoseTransactionSignatures(envelope, accountEntry)
fmt.Println(sig.Summary) // "supply weight 1 but threshold 2 requires 1 more..."

// 3. Simulation root cause: footprint, CPU/memory, fee, archived, auth, trap.
sim, _ := lens.DiagnoseSimulationResponse(&lens.SimulationResponse{Error: rpcErr})
fmt.Println(sim.Kind, sim.Hint) // e.g. missing_footprint + "use the footprint it returns..."

// 4. Fee-bump: did the outer wrapper or the inner transaction fail?
fb, _ := lens.DiagnoseFeeBump(envelope, result)
fmt.Println(fb.Stage, fb.Summary) // inner vs outer, with fee-charged accounting.

// 5. Claimable balance: evaluate nested predicates at explicit ledger times.
claim, _ := lens.DiagnoseClaimablePredicate(predicate, lens.ClaimablePredicateContext{
  LedgerCloseTime: ledgerCloseTime, BalanceCreatedCloseTime: createdCloseTime,
})
fmt.Println(claim.Satisfied, claim.FailedConditions)

// 6. Soroban resources: classify tx_soroban_invalid / host resource failures.
resource, _ := lens.DiagnoseResourceFailure(transactionResult)
if resource != nil {
  fmt.Println(resource.Kind, resource.Hint)
}

// 7. Sponsorship: correlate operation errors with begin/end/revoke operations.
sponsor, _ := lens.DiagnoseSponsorship(envelope, transactionResult)
if sponsor != nil {
  fmt.Println(sponsor.Summary, sponsor.Conflicts)
}

// 8. Protocol upgrade: correlate adjacent headers with a failed result.
upgrade, _ := lens.DiagnoseProtocolUpgrade(previousHeader, currentHeader, transactionResult)
if upgrade != nil {
  fmt.Println(upgrade.LikelyUpgradeRelated, upgrade.Summary)
}
```

Claim-predicate evaluation requires the creation-ledger close time because
that value is not stored in the claimable-balance entry. Protocol-upgrade
analysis requires adjacent headers and reports correlation rather than proof
that an upgrade caused a rejection. Resource result codes do not include CPU or
memory counters; use `DiagnoseSimulationResponse` when simulation reports
those values. These APIs complement the CLI's standard result-code explanation
without adding flags or changing its output contract.

### Error handling

Library errors support stable classification with `errors.Is`; callers can
also use `errors.As` to read the shared `lens.ErrorKind`. For example:

```go
value, err := lens.Decode(payload)
if errors.Is(err, lens.ErrInvalidBase64) {
  return fmt.Errorf("decode payload: %w", err)
}
if err != nil {
  var kind lens.ErrorKind
  if errors.As(err, &kind) {
    log.Printf("lens error kind: %s", kind)
  }
  return err
}
_ = value
```

The CLI keeps its human-readable error messages on stderr and returns a
non-zero exit status on failure. Error sentinels and kinds are library APIs;
existing command flags and output formats are unchanged.

Runnable Go examples for decoding, explaining, diffing, asset derivation, and
other public library workflows are included in
[`pkg/lens/examples_test.go`](pkg/lens/examples_test.go). They run offline
against the repository fixtures; see
[`docs/architecture.md`](docs/architecture.md#runnable-go-examples).

### Decoder performance benchmarks

The Go CLI does not add a benchmark flag; decoding benchmarks are run against
the library's checked-in XDR fixtures. From the repository root:

```sh
go test ./pkg/lens -run '^$' -bench 'Benchmark(DecodeCanonicalFixtures|DetectCanonicalFixtures|DecodeByProtocolVersion)$' -benchmem
```

The output includes nanoseconds per operation and allocation statistics.
Benchmarks run fully offline. Protocol-version cases vary the `LedgerVersion`
field of the canonical Protocol 22 header fixture (Protocols 20–23), so those
results compare version values with an otherwise identical XDR structure.
See [docs/architecture.md](docs/architecture.md#testing-approach) for fixture
selection and benchmark details.

Structural-diff benchmarks cover deep trees, wide lists (positional and
aligned), real ledger-close envelopes, and the redaction prepass:

```sh
go test ./pkg/lens -run '^$' -bench 'BenchmarkDiff' -benchmem
```

The encoder's contract is checked as a property suite: every fixture is run
through both the binary and JSON routes, and a synthesized value exercises every
registered type. Known-lossy renderings are reported as skipped with a reason.

```sh
go test ./pkg/lens -run 'RoundTrip' -v
```

## A note on type detection

`lens decode` detects the XDR type by trying each known type and keeping the ones
that decode cleanly, relying on the SDK rejecting input it does not fully consume.
On transaction-sized payloads this is reliable — the test suite asserts that every
real fixture matches exactly one type.

Short values are genuinely ambiguous, though. An empty `Memo` and a native `Asset`
are both `AAAAAA==`, because they are the same four bytes. The overlay `Auth`
flags and a void `ScVal` collide the same way, and short messages like `DontHave`
also decode as a `LedgerKey` or TTL entry. Some longer aliases are inherent to the
wire format too: `HotArchiveBucketEntry` is byte-identical to the matching
`BucketEntry` arm, and `BucketMetadata` collides with some `ScVal` encodings.
`lens` reports these rather than guessing silently:

```console
$ lens decode --candidates "AAAAAA=="
* Memo
  Asset
  Auth

3 types matched; the starred one is used by default. Use --type to choose.
```

Pass `--type` when you need certainty.

## Project status

**Early but real.** v0.1 does what this README says, with tests over real captured
mainnet XDR. The library API may still shift before v1.0 — it is not yet frozen.

- Supports 58 XDR types out of the ~400 in the protocol, chosen to cover what
  developers actually paste into a terminal: transactions, ledger entries, SCP,
  overlay messages (including time-sliced surveys), history-archive sets and
  Soroban — including the interface spec embedded in a contract's WASM.
  Adding more is a one-line change; see [docs/architecture.md](docs/architecture.md).
- All 208 protocol result codes are explained.
- No network access anywhere in the library or the tests.

See [docs/roadmap.md](docs/roadmap.md) for what is planned and what is deliberately
out of scope.

## Contributing

Contributions are welcome — especially new XDR type handlers and better failure
explanations, both of which are designed to be one-file changes.
See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

Apache-2.0. See [LICENSE](LICENSE).
