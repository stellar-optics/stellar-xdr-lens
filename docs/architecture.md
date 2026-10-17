# Architecture

This document explains how `stellar-xdr-lens` fits together and, more usefully,
where to make the three changes contributors most often want to make:

- [Adding a new XDR type](#adding-a-new-xdr-type)
- [Improving how a type is rendered](#adding-an-enricher)
- [Adding a new output format](#adding-a-formatter)

## The pipeline

Everything flows one way:

```
base64 ──► detect ──► xdr decode ──► Node tree ──► enrich ──► Formatter ──► output
              │            │             │                        │
        registry      stellar SDK    neutral IR            tree │ json │ text │ diff │ summary │ txrep │ csv
```

The important property is that **only one stage knows about XDR**. Once a value
becomes a `Node` tree, nothing downstream imports `xdr` or uses reflection.
That is what lets a new output format work for all 63 registered types at once,
and lets the differ work without knowing what it is comparing.

## End-to-end CLI coverage

The `cmd/lens` package includes end-to-end subprocess tests that execute the
built `lens` binary against the canonical `testdata/` fixtures. They cover the
real command surface (`decode`, `explain`, `diff`, and `meta-diff`), use the
same input resolution path as users do, and assert the observable CLI output. The suite is
explicitly offline-first: it runs with the standard proxy variables cleared to
ensure no network pathway is introduced by the command wrapper or default
runtime environment.

## Package layout

```
cmd/lens/              main; wires os.Args to internal/cli and maps errors to exit codes
internal/cli/          cobra commands — deliberately thin
  root.go              command tree, global flags, explore (TUI) command
  decode.go            decode + types commands, formatting and spec decoding flags
  explain.go           explain command, exit-code plumbing
  diff.go              diff command
  meta_diff.go         transaction metadata balance-diff command
  asset.go             asset command; network resolution for contract ids
  input.go             argument / --file / stdin resolution
  tui_tree.go          interactive terminal tree explorer (TUI)
pkg/lens/              the library; this is the reusable surface
  errors.go            stable error kinds and errors.Is-compatible sentinels
  decode.go            type registry, Detect, Decode, DecodeAs
  registry_index.go    introspection: RegistryIndex, categories, min sizes
  node.go              the neutral Node tree — the only file using reflection
  enrich.go            per-type human rendering (strkey, amounts, timestamps)
  contract_data.go     contract data storage: durability and special storage keys
  ledger_header.go     ledger-header field notes and protocol parameters
  bucket_entry.go      bucket list, tombstone and hot archive terminology
  scp.go               consensus node identity and pledge names
  state_archival.go    TTL expiry and eviction rendering
  account_extensions.go liabilities, sponsorship counters and sequence-ledger notes
  data_entry.go        account data values: printable text vs hex
  survey.go            time-sliced survey command names and encrypted bodies
  contract_code.go     WASM section parsing: contract spec, env meta and meta
  overlay.go           peer addresses, certificate keys, overlay message names
  tx_history.go        ledger-sequence field notes for history sets and batches
  asset_types.go       asset details and Stellar Asset Contract id derivation
  explain.go           Summary, Outcome, per-operation descriptions
  reason.go            all 208 result codes → plain English
  reason_lookup.go     O(1) short code lookup table (ReasonByCode)
  protocol21_reasons.go Protocol 21 & 22 error additions and Soroban host error decoder
  op_isolator.go       Multi-operation failure isolator (pre-op vs execution pinpointing)
  trap_reasons.go      Soroban trapped host function diagnostic reason decoder
  balance_explain.go   Insufficient-balance math (liabilities, reserve, shortfall)
  signature_explain.go Signature-threshold diagnosis (hint matching, missing weight)
  simulation_explain.go Simulation-failure root-cause decoder (footprint, CPU, fee)
  feebump_explain.go   Fee-bump inner-vs-outer failure separation
  diff.go              structural path diff and bounded opt-in LCS alignment
  scval_diff.go        recursive ScVal comparison with canonical map-key matching
  semantic_diff.go     exact wide-integer and type-aware semantic comparison
  meta_diff.go         transaction metadata balance deltas (offline)
  canonical.go         deterministic canonical XDR validation (RFC 4506) and serialization
  encode_errors.go     malformed input diagnostics, line/column tracking and error pointers
  stream_parser.go     stream-oriented XDR reader and chunk parser (RFC 5531, length-prefixed, base64)
  spec_decode.go       Contract spec aware ScVal decoding (ScSpecEntry / WASM custom section)
  format/              output formats
    format.go          Formatter interface, colour palette, TTY detection
    theme.go           environment-selected light/dark ANSI palettes
    tree.go            colourised tree
    json.go            stable JSON
    text.go            explain summaries
    diff.go            diff rendering
    summary.go         compact single-line summary (grep/pipeline friendly)
    txrep.go           SEP-0011 human-readable TxRep format
    csv.go             flat CSV export for batch ledger envelopes
    markdown.go        GitHub-flavored Markdown transaction reports
testdata/              real captured mainnet XDR + manifest.json recording provenance
```

`pkg/lens` is the public API and is what external projects import. `internal/cli`
cannot be imported by anyone else, which is deliberate: the CLI is an
implementation detail, and keeping it internal means its shape is free to change
without breaking anyone.

## Library errors

Errors from `pkg/lens` use exported sentinels for stable classification. Use
`errors.Is` to match a specific condition, such as `lens.ErrInvalidBase64`, or
`errors.As` to inspect the shared `lens.ErrorKind` value. Contextual wrapping
preserves both forms of inspection:

```go
if errors.Is(err, lens.ErrInvalidBase64) {
  // Reject or report the malformed payload.
}

var kind lens.ErrorKind
if errors.As(err, &kind) {
  log.Printf("lens error kind: %s", kind)
}
```

Existing exported sentinels such as `ErrNoMatch`, `ErrUnknownType`,
`ErrNotAsset`, and the WASM/spec errors remain available. Error classification
is entirely local and does not add network access to library operations.

## Structural, semantic, and metadata diffs

`Diff(left, right)` keeps its original positional behavior for ordered lists.
Call `DiffWithOptions` with `DiffOptions{AlignLists: true}` to use an LCS pass
that reports unchanged insertions and removals without shifting later items.
The aligner bounds its dynamic-programming table to $2^{20}$ cells and falls
back to positional comparison above that limit.

`DiffScVal(left, right)` recursively compares Soroban vectors and maps. Vector
order remains significant; map entries are matched by the canonical XDR bytes
of each key, so inserting a map entry does not cascade through later values.
`DiffSemantic` (or `DiffOptions{Semantic: true}`) additionally represents
ScVal `u128`, `i128`, `u256`, and `i256` values as exact decimal integers. The
existing asset and address enrichers retain canonical asset identifiers and
StrKeys. These behaviors are exposed by `lens diff --align` and
`lens diff --semantic`, which can be combined.

`DiffTransactionMetaBalances` accepts decoded `TransactionMeta` versions 0
through 4 and returns deterministic deltas for account XLM balances, trustline
balances, and claimable-balance amounts. A `STATE` entry supplies the before
snapshot; create/remove changes use zero for the absent side. Metadata that
does not include a baseline sets `BeforeKnown` false rather than inventing a
balance. The CLI command `lens meta-diff` renders these records as text or
JSON. It only inspects the supplied metadata and makes no network calls.

## Runnable Go examples

[`pkg/lens/examples_test.go`](../pkg/lens/examples_test.go) contains executable
`godoc` examples for the main public workflows: decoding and detecting XDR,
structural diffs, transaction explanations, asset contract-id derivation,
reason lookup, registry introspection, contract-spec parsing, and error
classification. The examples use the checked-in fixtures in `testdata/` and
deterministic local values; they make no network calls.

Run them together with the library tests from the repository root:

```sh
go test -v -race ./pkg/...
```

Examples are ordinary Go examples, so they are also shown on
[pkg.go.dev](https://pkg.go.dev/github.com/stellar-optics/stellar-xdr-lens/pkg/lens).

## The Node tree

`Node` is a small, format-independent tree:

```go
type Node struct {
	Name     string   // field name in the parent
	Kind     Kind     // KindScalar | KindStruct | KindList
	TypeName string   // originating XDR type, best-effort
	Value    any      // scalar payload: string, bool, int64, uint64, float64
	Note     string   // human rendering from an enricher
	Children []*Node
}
```

`Value` is restricted to those five types so that **formatters never need
reflection**. `Note` carries the enriched rendering — a strkey address, a
formatted amount — alongside the raw value, so machine-readable output can keep
the raw form while human output shows the friendly one.

### Why reflection, and why only here

`buildNode` in [`node.go`](../pkg/lens/node.go) is the only reflective code in
the project. The alternative — hand-writing a renderer for each of the ~400
generated XDR types — is not maintainable, and the other obvious option,
`encoding/json`, loses information we need: marshalling a result code yields
`"Code": -13` rather than `TransactionResultCodeTxFeeBumpInnerFailed`, because
Go marshals the underlying integer and not the `String()` method.

So the walker does three things generically:

1. **Prunes nil pointers.** XDR unions carry one populated arm and up to 27 nil
   ones. Emitting them as `null` is what makes raw XDR JSON unreadable.
2. **Resolves enum names.** Generated enums carry a `ValidEnum` method that
   plain numeric aliases like `Int64` and `Uint32` do not, so that method is
   used to tell an enum from an ordinary number before calling `String()`.
3. **Renders byte sequences as hex.** A 32-byte hash is a hex string, not a
   list of 32 integers.

### Robustness

Generated XDR accessors dereference union arms without checking the
discriminant, so they panic on a zero or malformed value — `AccountId.Address()`
on a zero value is a nil dereference. Since this tool's entire job is parsing
untrusted, possibly truncated blobs, both the decoder and every enricher run
behind a `recover`. A value we cannot prettify degrades to its generic
rendering; it never takes the process down.

## Adding a new XDR type

Add one line to `registry` in [`decode.go`](../pkg/lens/decode.go):

```go
var registry = []xdrType{
	// ...
	{"ClaimableBalanceEntry", func() any { return new(xdr.ClaimableBalanceEntry) }, 30},
}
```

The three fields are the display name, an allocator, and a detection priority.
Then add the matching entry to `typeDocs` in
[`registry_index.go`](../pkg/lens/registry_index.go) so `lens types --json`
can describe it:

```go
"ClaimableBalanceEntry": {CategoryLedgerEntry,
	"A balance claimable by named claimants, with its predicate and asset."},
```

That is the whole change. Detection, `--type`, `lens types` (including
`--json`), the tree, the JSON output and the differ all pick it up
automatically. `TestTypeDocsCoversRegistry` fails if the two maps drift apart,
and `TestRegistryIndexMinWireSizeIsLowerBound` checks the published minimum
size against the actual fixtures.

### Choosing a priority

Priority breaks ties when several types decode the same bytes. Higher wins.
Roughly:

| Range | Use for |
|---|---|
| 90–100 | What developers paste most: envelopes, results, meta |
| 60–89 | Ledger entries, keys, inner transaction types |
| 40–59 | Ledger-level types, Soroban, history-archive sets and batches |
| 30–39 | Overlay messages and events, which are large but rarely pasted |
| 1–29 | Small building blocks that decode from very few bytes |

Small types deserve low priority because short payloads are ambiguous — a
4-byte value may decode as half a dozen types, and the ranking decides which
one `lens decode` shows by default. The overlay `Auth` message is the extreme
case: four bytes that also parse as a void `ScVal`, so it ranks at 2 and is
decoded with `--type` in practice.

## Adding an enricher

Enrichers turn an XDR value into something a human recognises. `enricherFor` in
[`enrich.go`](../pkg/lens/enrich.go) maps a concrete XDR type to its enricher.
The core entries are a declaration-time literal; a type added by a feature file
registers itself without touching `enrich.go` at all:

```go
func init() {
	enricherFor[reflect.TypeOf(xdr.MyType{})] = enrichMyType
}

func enrichMyType(rv reflect.Value) (display string, raw any, ok bool) {
	v, valid := rv.Interface().(xdr.MyType)
	if !valid {
		return "", nil, false
	}
	return "human form", v.RawForm(), true
}
```

Return `ok=false` to fall back to the generic rendering — do this for union arms
you cannot sensibly render, such as liquidity-pool-share assets, rather than
returning a misleading string.

The registry is populated once before any decoding happens and is read-only
afterwards, so it needs no locking.

### Offer entries and price fractions

`pkg/lens/offer_entry.go` teaches the tool about DEX order book offers:

- `xdr.Price` renders as an `n/d` fraction with its decimal value, e.g.
  `Price: 7/3 (= 2.3333333)`. A zero denominator is malformed, so the enricher
  declines and the generic walker renders the raw numerator and denominator
  instead of dividing by zero.
- `xdr.OfferEntry` is already registered (priority 5) so `lens decode --type
  OfferEntry` decodes a ledger offer and renders its seller as a strkey
  address, its assets in `CODE:ISSUER` form (`native` for XLM), its amount in
  stroops, its price as a fraction, and its flags. An offer embedded in a
  `LedgerEntry` decodes the same way at `Data.Offer`.

### Contract data and storage levels

`pkg/lens/contract_data.go` decodes Soroban contract data and makes the three
storage levels distinguishable in the output:

- `xdr.ContractDataDurability` renders as `persistent` or `temporary` — the
  durability recorded on the wire — instead of the
  `ContractDataDurabilityPersistent` constant name.
- `xdr.ScAddress` renders as its strkey form (`C...` for a contract address,
  `G...` for an account address) instead of a discriminant plus a raw 32-byte
  hash. That enricher lives in `diagnostic_events.go`, so contract addresses
  are readable wherever a tree carries one, not only inside contract data.
- The special `ScVal` ledger keys render by name: the contract instance key
  becomes `instance storage key`, and the nonce key becomes `nonce key: 5`,
  keeping the nonce that callers need. Every other `ScVal` keeps the structural
  rendering that stays machine-readable without a contract spec.

Instance storage is recorded with persistent durability and can only be
recognised by its key, so the named instance key is what separates it from an
ordinary persistent entry:

| Storage level | What the output shows |
|---|---|
| instance | `Key: instance storage key` (durability is `persistent` on the wire) |
| persistent | `Durability: persistent` with an ordinary `ScVal` key |
| temporary | `Durability: temporary` |

```console
$ lens decode --type ContractDataEntry --depth 3 "$INSTANCE"
ContractDataEntry
├─ Ext
│  └─ V: 0
├─ Contract: CDFP5OV6AAAQEAYEAUDAOCAJBIFQYDIOB4IBCEQTCQKRMFYYDENBWGZ3
├─ Key: instance storage key
├─ Durability: persistent
└─ Val
   ├─ Type: ScValTypeScvContractInstance
   └─ Instance
      └─ Executable
         ... 1 more level(s) hidden
```

`ContractDataEntry` is registered (priority 5), so `lens decode --type
ContractDataEntry` decodes a standalone blob, and an entry embedded in a
`LedgerEntry` decodes the same way at `Data.ContractData`.

### State archival and TTL entries

`pkg/lens/state_archival.go` renders protocol 20's rent bookkeeping. A TTL
record's two fields are both opaque on the wire — a hash and a ledger number:

- `KeyHash` renders as its hex string (the walker's default for a `Hash` is a
  byte-array dump) with a `state archival key hash` note. The hash is the
  SHA-256 of the tracked entry's `LedgerKey`. A TTL entry tracks contract code
  or contract data; which of the two — and whether the data is persistent or
  temporary — is not recoverable from the record, so the note names the set
  instead of guessing.
- `LiveUntilLedgerSeq: 55281000` becomes
  `live through ledger 55281000 (~2025-04-03T14:33:02Z)`: the ledger through
  which the entry stays live, plus an estimated close date. The estimate uses
  the protocol's target close interval (`LedgerCloseInterval`, 5 seconds) and
  a recorded Mainnet reference close: ledger 64,990,097 at
  `2026-10-17T11:24:27Z`.
- A zero TTL renders `already expired (evict at next close)`.

The estimate is deterministic and offline. `TTLExpiryAt` computes the date
from any reference ledger/close-time pair, and `EstimateTTLExpiry` applies the
package's recorded Mainnet reference. Real close intervals average slightly
above the target and have changed across protocol upgrades, so the date is
labelled `~` and should be read as approximate.

### Field-name enrichment

Some renderings depend on the field name rather than the type. XDR models
amounts, sequence numbers and offer ids all as `Int64`, so the only signal that
a value is an amount is the field it sits in. `amountFields` in `enrich.go`
lists the known amount fields; anything not listed stays a plain integer,
because rendering a sequence number as `12345.0000000` would be worse than
showing it raw.

`fieldNotes` is the same idea for anything that is not an amount — a hint
attached to a field rendered alongside it. `LedgerVersion: 22` becomes
`protocol 22`, and `LiveUntilLedgerSeq` becomes `live through ledger N` plus
an estimated close date (see the state archival section). Feature files
register notes from `init`:

```go
func init() {
	fieldNotes["SlotIndex"] = slotIndexNote
}
```

A note is only rendered on a scalar leaf. The tree and JSON formatters ignore
`Node.Note` on struct and list nodes, so a note that should be visible has to
sit on a child that renders as a scalar — `account_extensions.go` attaches the
extension capability note to the union's discriminant child for exactly this
reason.

### Node hooks

Some useful content is not an XDR field at all: a Soroban contract's interface
lives in a WASM custom section inside the opaque `Code` byte array of a
`ContractCodeEntry`. `nodeHooks` in [`node.go`](../pkg/lens/node.go) maps a
concrete XDR type to a function that runs after the declared fields have been
walked and may append derived children or annotate existing ones:

```go
func init() {
	nodeHooks[reflect.TypeOf(xdr.ContractCodeEntry{})] = decorateContractCode
}

func decorateContractCode(n *Node, v any) {
	entry := v.(xdr.ContractCodeEntry)
	parsed, err := ParseContractCode(entry.Code)
	// annotate the Code leaf and append Spec, EnvMeta and Meta children
}
```

Hooks receive the concrete decoded value rather than a `reflect.Value`, so
feature files stay free of reflection, and one that panics is treated as absent
rather than being allowed to take the process down. Use a hook when the
rendering depends on content buried inside a field; use the enricher registries
when the type or field name is enough.

## Adding a formatter

Implement `Formatter` in [`format/format.go`](../pkg/lens/format/format.go):

```go
type Formatter interface {
	Format(w io.Writer, v *lens.Value) error
}
```

Because you receive a `*lens.Value` and walk its `Node` tree, a new format works
for every registered type immediately and needs no XDR knowledge. Follow the
existing files' functional-options pattern (`WithPalette`, `WithIndent`) for
configuration.

Colour is handled centrally: the CLI uses `format.ThemeFromEnv` and
`format.PaletteForTheme(w, mode, palette)`. `LENS_THEME=dark|light` selects a
scheme (dark is the default), and per-role values such as
`LENS_THEME_DARK_KEY=38;5;39` customize its ANSI SGR colours. Supported roles
are `KEY`, `TYPE`, `STR`, `NUM`, `NOTE`, `DIM`, `ADD`, `REMOVE`, and `CHANGE`;
set a role variable to an empty value to disable that colour. Values contain
only semicolon-separated SGR integers from 0 to 255, without escape prefixes.
The palette helper honours `--color`, the
[`NO_COLOR`](https://no-color.org) convention, and whether the writer is a
terminal. Invalid theme configuration is reported as an error. Never write
escape sequences directly.

## Adding a result code explanation

[`reason.go`](../pkg/lens/reason.go) maps every protocol result code to plain
English. Keys are the exact strings returned by the constants' `String()`
method:

```go
"PaymentResultCodePaymentUnderfunded": bad(
	"The source account does not hold enough of the asset to send this amount.",
	"Remember that XLM balances must stay above the minimum reserve, so the spendable balance is lower than the total."),
```

Use `ok(...)` for success codes and `bad(summary, hint)` otherwise; the hint may
be empty when no general advice applies.

Two rules, both enforced by tests:

- **Do not restate the constant.** "tx_bad_seq" is not an explanation; "the
  sequence number did not match the source account's next expected sequence
  number" is.
- **Cover everything.** `TestEveryResultCodeIsExplained` enumerates all 208
  codes across the 28 protocol enums by reflection and fails if any lacks an
  entry. When a new protocol version adds a code, that test tells you its name.

## Introspection and contract ids

Two small APIs round out the library for tooling use:

- [`RegistryIndex()`](../pkg/lens/registry_index.go) returns a `TypeInfo` for
  every registered type — name, category, description, priority and minimum
  encoded size — which is what `lens types --json` emits. The index is derived
  from the live registry, so it can never list a type `DecodeAs` rejects. Sizes
  are computed structurally from the Go types by reflection, with a depth cap
  for self-referential unions and a conservative fallback, so the number is a
  lower bound that holds for every valid value; no fixture or network access is
  involved.
- [`DescribeAsset`](../pkg/lens/asset_types.go) decodes a classic asset and
  derives its Stellar Asset Contract id. The id is a hash of the network id and
  the asset (`Asset.ContractID`), rendered as a strkey `C…` address, so it is
  deterministic, offline, and different on every network. `DescribeAssetValue`
  does the same for an already-decoded `Value` and reports `ErrNotAsset` when
  the value is not an asset.

`ContractId` and `ScAddress` values anywhere in a decoded tree render as strkey
addresses too, via enrichers registered in `asset_types.go` — a contract id
buried in a Soroban footprint is as readable as one at the top level.

## Explain, and why it reads the Node tree

`explainOperationResult` needs the type-specific result code buried in an
`OperationResult`'s `Tr` union. Doing that with the SDK directly means a switch
over all 27 arms.

Instead it reuses the `Node` tree, which has already selected the populated arm
and rendered its code as a constant name. Finding the one child that is not the
discriminant and reading its `Code` field costs a few lines and, more
importantly, keeps working when the protocol adds a 28th operation type.

The rest of `explain.go` uses the concrete SDK types, because
`TransactionEnvelope` exposes helpers (`SourceAccount`, `Operations`,
`Preconditions`, `IsFeeBump`) that already normalise across v0, v1 and fee-bump
envelopes. Using them is both clearer and more correct than re-deriving that
logic from the tree.

## Reason lookup table (ReasonByCode)

Downstream consumers (such as `focus` and automated diagnostic linters) frequently
require O(1) random-access lookups for short canonical result codes without
performing linear scans or importing raw XDR definitions.

[`reason_lookup.go`](../pkg/lens/reason_lookup.go) provides:

- `lens.ReasonByCode(short string) (Reason, bool)`: thread-safe, case-insensitive
  O(1) resolution of canonical short codes (e.g. `payment_underfunded`, `tx_bad_seq`).
- `lens.AllReasons() []Reason`: sorted slice of all registered reasons for enumeration.
- `lens.ReasonLookupTable() map[string]Reason`: snapshot copy of the full lookup map.

The lookup table is precomputed at initialization and automatically updated
whenever protocol extension packages register additional result codes.

## Protocol 21+ Error Codes and Explanations

Stellar Protocol 21 and 22 introduce state archival, TTL management, key freezing,
and expanded Soroban host error semantics. [`protocol21_reasons.go`](../pkg/lens/protocol21_reasons.go)
provides plain-English diagnostic explanations and actionable remediation hints for:

- **State Archival & Key Freezing**: `tx_frozen_key_accessed` (CAP-0056) when an
  operation attempts to access an expired, frozen ledger entry.
- **Soroban State and Fee Codes**: `invoke_host_function_entry_archived`,
  `invoke_host_function_insufficient_refundable_fee`,
  `extend_footprint_ttl_insufficient_refundable_fee`, and
  `restore_footprint_insufficient_refundable_fee`.
- **Soroban Host Errors**: Full decoders for `xdr.ScError`, mapping error types
  (`ScErrorTypeSceStorage`, `ScErrorTypeSceBudget`, `ScErrorTypeSceWasmVm`, etc.)
  and error codes (`ScErrorCodeScecIndexBounds`, `ScErrorCodeScecExceededLimit`, etc.)
  into descriptive developer-facing errors.

## Multi-Operation Transaction Failure Isolator

Stellar transactions execute atomically. When a transaction contains multiple
operations, developers need to isolate:

1. **Pre-operation Check Failures**: Transaction-level failures (e.g. `tx_bad_seq`,
   `tx_bad_auth`, `tx_too_early`, `tx_insufficient_balance`) reject the transaction
   during header and precondition validation before any operation runs.
2. **Operation Execution Failures**: The transaction passed pre-operation checks and
   was applied up to the failing operation index.

[`op_isolator.go`](../pkg/lens/op_isolator.go) inspects envelopes and results to pinpoint:

- The exact zero-based operation index where execution halted (`FailingOpIndex`).
- The operation type name (`FailingOpType`, e.g. `payment`, `manage_buy_offer`).
- Whether the failure was a `pre_operation` check failure or an `operation` execution failure.
- A comprehensive multi-line `DiagnosticReport()` for debugging.

The CLI exposes this via `lens explain --isolate`.

## Soroban Trapped Host Function Diagnostic Decoder

When a Soroban contract call traps, the transaction result code is simply
`invoke_host_function_trapped` without internal error details. The actionable panic
string or host abort reason is stored in `DiagnosticEvent` payloads within transaction
metadata (`TransactionMeta.V3.SorobanMeta.DiagnosticEvents`).

[`trap_reasons.go`](../pkg/lens/trap_reasons.go) correlates trapped invocations with
diagnostic events to extract:

- **Panic Messages**: Human-readable strings from `log` and `panic` topics/data.
- **Host & Contract Errors**: Decoded `ScError` values including custom contract error codes.
- **Contract & Function Identity**: Identifies the contract ID (rendered as strkey `C...`)
  and function name in the failing execution frame.
- **Diagnostic Trace**: Chronological trace of diagnostic events leading to the abort.

The CLI exposes this via `lens explain --meta <path-or-base64>`.

## Insufficient-balance and liabilities math

`tx_insufficient_balance`, `payment_underfunded` and their siblings mean
"the math did not fit", not "the account is empty": selling liabilities lock
part of the balance and the base reserve must stay untouched.

[`balance_explain.go`](../pkg/lens/balance_explain.go) computes the exact
shortfall offline from a ledger entry plus a base reserve:

- `AccountSellingLiabilities` / `AccountBuyingLiabilities` read the V1
  liabilities extension (zero when absent).
- `ReserveRequirement` implements
  `(2 + numSubEntries + numSponsoring - numSponsored) * baseReserve`, with
  `AccountReserveRequirement` pulling the counters from the V2 extension.
- `DiagnoseBalance(entry, baseReserve, amountToSpend)` returns the available
  balance, the shortfall in stroops and XLM, and a funding hint; a
  `baseReserve` of 0 selects `DefaultBaseReserve` (0.5 XLM).
- `DiagnoseTrustlineSpend` covers non-native balances, where only selling
  liabilities reduce the spendable amount.

Verified against the `account_entry_ext_v1/v2` and `trustline_entry_v1`
fixtures; runnable examples live in `pkg/lens/examples_test.go` style tests
alongside the unit suite.

## Signature-threshold diagnosis

`tx_bad_auth` and `op_bad_auth` mean "not enough signer weight", but the
result code never says which signer is missing.

[`signature_explain.go`](../pkg/lens/signature_explain.go) compares the
envelope's `DecoratedSignature` hints against the account's signer set:

- `OperationThresholdLevel` maps each operation to `low` (allow trust, set
  trust-line flags, bump sequence, claim claimable balance), `high` (account
  merge, set options) or `medium` (everything else).
- `SignerHint` derives the 4-byte hint stellar-core uses (last four bytes of
  the key), so matching needs no transaction hash and no network.
- `DiagnoseSignatures(entry, sigs, threshold)` sums the weight of matched
  signers, counts unknown signatures separately, and names the missing
  signers; `DiagnoseTransactionSignatures` picks the threshold from the first
  operation automatically.

## Simulation-failure root cause

`simulateTransaction` failures arrive as RPC error strings, not XDR result
codes: "missing footprint key", "CPU budget exceeded", "insufficient
refundable fee" and friends.

[`simulation_explain.go`](../pkg/lens/simulation_explain.go) classifies those
strings offline into a stable `SimulationKind` (`missing_footprint`,
`cpu_exceeded`, `memory_exceeded`, `insufficient_fee`, `archived_entry`,
`auth_missing`, `contract_trapped`, `invalid_input`, `unknown`):

- `DiagnoseSimulationError(msg)` does case-insensitive substring matching and
  never fails on unrecognised text (it yields `unknown`).
- `DiagnoseSimulationResponse(resp)` prefers structured fields when present:
  explicit `MissingKeys`, CPU/memory used-vs-limit counters, and the
  recommended `MinResourceFee` are folded into the summary and hint.
- `DiagnoseMissingFootprint` and `DiagnoseResourceExhaustion` cover callers
  that already extracted keys or budgets numerically.

`SimulationResponse` mirrors the RPC JSON names so callers can unmarshal a
captured response directly; lens never fetches it.

## Fee-bump inner-vs-outer separation

A fee-bump result carries two verdicts: the outer wrapper's and the inner
transaction's. Fixing the wrong half wastes a rebuild.

[`feebump_explain.go`](../pkg/lens/feebump_explain.go) separates them:

- `DiagnoseFeeBump(env, res)` reports `Stage` as `inner` (wrapper valid, inner
  failed — fee still charged to the fee-bump source), `outer` (wrapper
  rejected before the inner ran — inner codes are not the cause), `none`
  (both applied) or `not_fee_bump` for classic transactions.
- The diagnosis records both short codes, both plain-English reasons, the fee
  source and inner source addresses, the fee charged, and the failing inner
  operation indexes, plus a `DiagnosticReport()` for CLI-style output.
- `DiagnoseFeeBumpFromValues` accepts decoded `*lens.Value`s, matching the
  `ExplainPair` calling convention.

Verified against the canonical `tx_failed` (inner failure), `tx_soroban`
(inner success) and `tx_success` (non-fee-bump) fixtures.

## Testing approach

Fixtures in `testdata/` are **real captured mainnet transactions**, with
`manifest.json` recording each transaction hash so a fixture can be traced back
to the ledger. Tests never touch the network.

For types that never appear as standalone Horizon payloads — SCP envelopes,
bucket entries, ledger headers, TTL entries, overlay messages, history-archive
sets, classic assets, account extensions, data entries, contract code entries
and survey messages — `manifest.json` also lists synthetic fixtures:
deterministic payloads built with `xdr.MarshalBase64` from fixed hashes and
sequence numbers. Each one is declared as such in the manifest, so provenance
is never in doubt. Asset fixtures double as known-answer tests: the native and
USDC assets carry well-known mainnet SAC contract ids that the tests assert
exactly.

Where a case cannot be captured — every classic operation type, memo variant and
precondition shape — tests construct XDR with the SDK encoder and round-trip it
through the real decode path, rather than reaching into internals.

Decoder benchmarks live in
[`pkg/lens/benchmarks_test.go`](../pkg/lens/benchmarks_test.go) and use only
checked-in `testdata/` fixtures. Run them from the repository root with:

```sh
go test ./pkg/lens -run '^$' -bench 'Benchmark(DecodeCanonicalFixtures|DetectCanonicalFixtures|DecodeByProtocolVersion)$' -benchmem
```

The fixture benchmarks report decode and type-detection latency, allocations,
and input throughput for representative transactions, ledger data and overlay
messages. The protocol-version benchmark starts with the canonical Protocol 22
ledger-header fixture and re-encodes its `LedgerVersion` as Protocols 20–23.
Those cases deliberately keep the header's XDR shape and all other fields
constant: they compare decoder cost across the version values, not historical
wire-schema changes. No benchmark or library decode path makes network calls.

The suite deliberately targets properties that would break the tool if violated:

- detection stays unambiguous on real payloads (`TestDetectIsUnambiguousForRealPayloads`);
- ambiguity is reported, not hidden (`TestDetectReportsGenuineAmbiguity`);
- the introspection index tracks the registry exactly
  (`TestTypeDocsCoversRegistry`) and its size estimates hold as lower bounds
  (`TestRegistryIndexMinWireSizeIsLowerBound`);
- enums render as names, not integers (`TestEnumsRenderAsConstantNames`);
- a failure is attributed to the operation that caused it
  (`TestExplainPairAttributesFailureToOperation`);
- JSON output is deterministic and omits nulls;
- every result code has a real explanation.

## Compact single-line summary (`pkg/lens/format/summary.go`)

`CompactFormatter` serialises any decoded XDR `*lens.Value` into a terse, single-line format ideal for shell pipelines, `grep`, and quick log examination.
- Envelopes and Transactions output source accounts, sequence numbers, operation counts, and fees.
- Operation results and transaction results highlight execution codes and failure/success status.
- Nested structures are flattened into concise key-value tokens (`type=TransactionEnvelope v1 source=G... ops=1 fee=100 stroops`).
- Like all formatters, it is strictly offline and adheres to the `Formatter` interface without calling network APIs.

## SEP-0011 TxRep formatter (`pkg/lens/format/txrep.go`)

`TxRepFormatter` implements the human-readable [SEP-0011 TxRep](https://github.com/stellar/stellar-protocol/blob/master/ecosystem/sep-0011.md) standard format for Stellar transaction envelopes.
- Emits canonical TxRep colon-delimited lines (e.g. `tx.sourceAccount: G...`, `tx.fee: 100`, `tx.operations[0].body.paymentOp...`).
- Handles `TransactionEnvelope` variants: V0, V1, and FeeBump envelopes.
- Formats signatures with key hints and base64 payloads (`tx.signatures[0].signature: ...`).
- Generates valid TxRep output for debugging and multisig coordination offline.

## Flat CSV export (`pkg/lens/format/csv.go`)

`CSVFormatter` extracts the high-level fields of many transactions into one flat,
machine-readable table, which is the shape a batch export — "who sent it, what it
cost, what it did, and whether it succeeded" — needs.

- The schema is fixed and exported: [`CSVColumns`](../pkg/lens/format/csv.go)
  lists the sixteen columns in emit order, and the column constants and the
  header row are pinned against it by `TestCSVColumnIndexesMatchSchema` and
  `TestCSVColumnsAreStable`. Renaming or reordering a column is a breaking
  change for scripts that address them positionally.
- `FormatAll` and `FormatSummaries` are the batch entry points: one header,
  then one row per value, so thousands of transactions concatenate under a
  single header. `WithCSVHeader(false)` drops the header for appends.
- Rows are fixed-width. A field that does not apply to a row — the fee bid of a
  bare `TransactionResult`, the variant of a bucket entry — is emitted as an
  empty field, never omitted, so every row parses with the same reader.
- Transaction values go through `lens.Explain`, which is what turns the raw
  tree into short result codes, short operation names and the outcome. The
  paired form, `FormatSummary` / `FormatSummaries` on a `lens.Summary` from
  `ExplainPair`, is the one that fills `success`, `result_code`,
  `inner_result_code` and `failed_operations` from a single transaction.
- Any other registered type still exports: `type` carries the XDR type name and
  `detail` carries the tree flattened to `key=value` pairs. A transaction that
  cannot be explained is an error instead, because a half-filled transaction row
  would be a lie.
- Output is written through `encoding/csv`, so a memo containing the delimiter,
  a quote or a newline is quoted per RFC 4180 and round-trips through any
  standard reader. Nothing but the value and the writer influences the bytes:
  no network, no clock, no environment, which is what makes the export
  deterministic in CI.
- Rows are built before the writer is touched, so a failed batch leaves the
  writer untouched rather than truncated mid-export.

Reachable from the CLI as `lens decode --csv` (or `--format csv`); see the
[README](../README.md#csv-export) for the column list.

## Markdown transaction reports (`pkg/lens/format/markdown.go`)

`MarkdownFormatter` renders a `lens.Summary` as GitHub-flavored Markdown: a
transaction details table followed by an operation table containing each
operation's description, result, status, and hint. `Format` accepts a decoded
transaction envelope or result; `FormatSummary` accepts a summary, including
the paired summary from `lens.ExplainPair`.

Table cells escape Markdown pipes and convert embedded line breaks to `<br>`,
so memos, operation descriptions, and protocol hints do not break the table.
The formatter only reads the summary and writes to the supplied writer; it
makes no network calls and does not use the environment or clock.

The CLI exposes paired transaction reports as `lens explain --markdown`; see
the [README](../README.md#lens-explain--why-did-this-fail) for usage.

## Interactive Terminal Tree Explorer (`internal/cli/tui_tree.go`)

`TUITree` provides a curses-like interactive navigation experience within standard ANSI terminals for exploring deeply nested XDR structures.
- Launchable with `lens decode -i`, `lens decode --interactive`, or `lens explore` / `lens tui`.
- Keybindings:
  - `j` / `Down`: Move selection down
  - `k` / `Up`: Move selection up
  - `Enter` / `Space` / `Right`: Expand or toggle node
  - `Left` / `h`: Collapse node or jump to parent
  - `e`: Expand all nodes
  - `c`: Collapse all nodes
  - `/`: Search / filter node paths
  - `q` / `Ctrl+C`: Quit
- Pure offline terminal handling using raw mode and ANSI escape codes with fallback to static rendering in non-TTY environments.

## Contract spec-aware ScVal decoding (`pkg/lens/spec_decode.go`)

Soroban smart contracts encode arguments, return types, storage entries, and events as opaque `xdr.ScVal` structures. `pkg/lens/spec_decode.go` resolves these generic values against contract specifications:
- Accepts contract specs as raw XDR byte slices/streams or extracted from WebAssembly (`.wasm`) binaries via the `contractspecv0` custom section.
- Resolves:
  - UDT Structs (`ScSpecUdtStructV0`): Maps tuple/map fields to human-readable struct member names.
  - UDT Enums (`ScSpecUdtEnumV0`): Maps numeric discriminant values to enum variant identifiers.
  - UDT Unions (`ScSpecUdtUnionV0`): Labels case names and associated values.
  - Function Invocations (`ScSpecFunctionV0`): Resolves function names and parameter lists in `InvokeHostFunctionOp`.
- Usable via `--spec <spec.xdr|contract.wasm>` in `lens decode`.

## Streaming XDR Architecture (`stream_parser.go`)

Parsing large ledger histories or bulk transaction archives requires stream processing without buffering entire archives in memory:

```
Framed Stream ──► StreamParser (bufio.Reader) ──► Record Chunk ──► Decode / Unmarshal
                     │
         Auto-Detect Framing
         ├── RFC 5531 Record Marking (4-byte header, bit 31 final-frag flag)
         ├── Length-Prefixed Framing (4-byte big-endian uint32 prefix)
         └── Base64 Lines Framing (newline-delimited)
```

- **Chunked Reassembly**: Multi-fragment RFC 5531 records are streamed fragment-by-fragment and assembled directly into target records.
- **Strict Bounded Memory**: `WithMaxRecordSize` protects against unbounded memory allocations and OOM crashes from malicious or corrupt streams.
- **Zero Allocations Drain**: `Drain()` calculates stream metrics without storing historical chunks.

## Canonical XDR Serialization (`canonical.go`)

Adheres strictly to RFC 4506 deterministic serialization:

- **4-Byte Word Alignment**: All records and variable-length segments must align to 4-byte boundaries.
- **Zero Padding Enforcement**: Trailing padding bytes $(4 - (N \pmod 4)) \pmod 4$ in opaque arrays and strings must strictly be `0x00`. Non-zero padding bytes are detected and rejected as non-canonical leaks (`ErrNonCanonicalPadding`).
- **Canonical Re-Encoding Verification**: Decodes input as registered XDR types, re-marshals canonically with SDK encoders, and asserts bit-for-bit equivalence, flagging trailing junk (`ErrTrailingGarbage`).

## Malformed Input Diagnostics & Error Pointers (`encode_errors.go`)

When JSON inputs fail syntax validation or schema parsing, `lens` computes exact line and column pointers:

- **Path Tracking**: Resolves dot-and-bracket paths (e.g. `operations[0].amount`) to token byte offsets.
- **Visual Caret Indicators**: Generates source code snippets with surrounding line context and aligned caret indicators (`^~~~`).
- **Error Chaining**: Diagnostic errors wrap base causes with full compatibility for `errors.Is` and `errors.As`.

