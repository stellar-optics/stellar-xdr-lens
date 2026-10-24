# Architecture

This document explains how `stellar-xdr-lens` fits together and, more usefully,
where to make the three changes contributors most often want to make:

- [Adding a new XDR type](#adding-a-new-xdr-type)
- [Improving how a type is rendered](#adding-an-enricher)
- [Adding a new output format](#adding-a-formatter)

## The pipeline

Decoding and explaining flows one way:

```
base64 ──► detect ──► xdr decode ──► Node tree ──► enrich ──► Formatter ──► output
              │            │             │                        │
        registry      stellar SDK    neutral IR            tree │ json │ text │ diff │ summary │ txrep │ csv
```

Encoding simplified JSON into XDR flows deterministically:

```
JSON spec ──► schema validate (strict) ──► type coerce ──► xdr build ──► base64 XDR
                      │                         │               │
                 validate.go                coerce.go      tx_builder.go / scval_encode.go
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
  encode.go            encode command for JSON -> base64 XDR
  explain.go           explain command, exit-code plumbing
  diff.go              diff command
  meta_diff.go         transaction metadata balance-diff command
  asset.go             asset command; network resolution for contract ids
  input.go             argument / --file / stdin / --batch resolution
  output.go            --quiet and atomic --output destination selection
  exitcodes.go         installed exit-code policy (0 / 1 / 2 / 3)
  completion.go        shell completion for bash/zsh/fish/powershell
  tui_tree.go          interactive terminal tree explorer (TUI)
pkg/lens/              the library; this is the reusable surface
  errors.go            stable error kinds and errors.Is-compatible sentinels
  decode.go            type registry, Detect, Decode, DecodeAs
  registry_index.go    introspection: RegistryIndex, categories, min sizes
  node.go              the neutral Node tree — the only file using reflection
  enrich.go            per-type human rendering (strkey, amounts, timestamps)
  contract_data.go     contract data storage: durability and special storage keys
  ledger_header.go     ledger-header field notes, LP flags and upgrade votes
  bucket_entry.go      bucket list, tombstone and hot archive terminology
  scp.go               consensus node identity and pledge names
  state_archival.go    TTL expiry and eviction rendering
  trustline.go           trustline flag names and liability amount fields
  account_extensions.go liabilities, sponsorship counters and sequence-ledger notes
  signer_thresholds.go  account threshold triplet, locked master, signer capacity
  data_entry.go        account data values: printable text vs hex
  survey.go            time-sliced survey command names and encrypted bodies
  contract_code.go     WASM section parsing: contract spec, env meta and meta
  contract_executable.go contract executables: wasm hash, built-in token, external refs
  overlay.go           peer addresses, certificate keys, overlay message names
  tx_history.go        ledger-sequence field notes for history sets and batches
  asset_types.go       asset details and Stellar Asset Contract id derivation
  validate.go          strict schema validation for JSON templates
  coerce.go            permissive type coercion & number parsing (stringified numbers, hex, assets)
  scval_encode.go      ScVal constructor from intuitive JSON literals
  tx_builder.go        TransactionEnvelope builder from JSON spec
  explain.go           Summary, Outcome, per-operation descriptions and failure deepening
  reason.go            all 208 result codes → plain English
  reason_lookup.go     O(1) short code lookup table (ReasonByCode)
  protocol21_reasons.go Protocol 21 & 22 error additions and Soroban host error decoder
  op_isolator.go       Multi-operation failure isolator (pre-op vs execution pinpointing)
  trap_reasons.go      Soroban trapped host function diagnostic reason decoder
  balance_explain.go   Insufficient-balance math (liabilities, reserve, shortfall)
  signature_explain.go Signature-threshold diagnosis (hint matching, missing weight)
  simulation_explain.go Simulation-failure root-cause decoder (footprint, CPU, fee)
  feebump_explain.go   Fee-bump inner-vs-outer failure separation
  claimable_explain.go  recursive claim-predicate evaluation with explicit ledger times
  resource_explain.go   Soroban resource rejection decoder from transaction results
  resource_breakdown.go declared Soroban resources, fees and protocol-limit comparison
  sponsorship_explain.go sponsorship result and begin/end sequence diagnosis
  upgrade_explain.go   protocol-version boundary correlation for failed results
  diff.go              structural path diff and bounded opt-in LCS alignment
  diff_filter.go       include/exclude path filtering for structural diffs
  diff_redact.go       opt-in hash/signature/identifier redaction for diffs
  encode.go            symmetric JSON/value -> base64 XDR encoder (inverse of decode)
  roundtrip.go         decode -> encode -> decode property verification engine
  scval_diff.go        recursive ScVal comparison with canonical map-key matching
  semantic_diff.go     exact wide-integer and type-aware semantic comparison
  diff_stats.go        statistical summary of a diff: counts, sections, depth
  signer_diff.go       unordered signer/signature multiset comparison
  footprint_diff.go    Soroban footprint set-theoretic (Venn) diff
  envelope_result_diff.go  aligned envelope/result operation pairing
  meta_diff.go         transaction metadata balance deltas (offline)
  canonical.go         deterministic canonical XDR validation (RFC 4506) and serialization
  encode_errors.go     malformed input diagnostics, line/column tracking and error pointers
  stream_parser.go     stream-oriented XDR reader and chunk parser (RFC 5531, length-prefixed, base64)
  stdin_pipe.go        stdin framing probe: text (base64/hex) vs binary streams
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
    html.go            self-contained offline HTML reports
    mermaid.go         Mermaid flowchart (graph TD) transaction visualization
    junit.go           JUnit XML test reports for CI gatekeeping
    linediff.go        shared line alignment for unified and side-by-side diffs
    unified.go         standard unified diff for CI review comments
    json_patch.go      RFC 6902 JSON Patch output
    side_by_side.go    colourised two-column terminal diff
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

### Binary normalization (Base64 vs Hex)

Comparing XDR structures often involves equivalent binary payloads (such as transaction hashes, signatures, contract IDs, or raw byte buffers) that are formatted as Base64 in one tree and Hex in another, or encoded as byte arrays or byte lists. `pkg/lens/diff.go` normalizes binary representations prior to computing diffs so that identical byte sequences are evaluated as equal regardless of string encoding, avoiding spurious false-positive diff lines.

### Numeric delta threshold filter (`--diff-threshold`)

When diffing ledger headers, transactions, or state trees, minor differences in timestamps, base fees, or sequence counters can clutter output when only major state changes are of interest. The `--diff-threshold <value>` flag (and `DiffOptions.Threshold` / `DiffOptions.DiffThreshold`) instructs the differ to suppress scalar numeric differences where the absolute difference $|left - right| \le \text{threshold}$.

### Operation permutation detection

When comparing transactions where operations were reordered (e.g. operations 0 and 1 swapped), naive positional diffs report complete body replacements, while standard alignment reports delete/insert pairs. `pkg/lens/diff.go` performs permutation matching across the `Operations` list, identifying operations whose bodies match structurally but appear at shifted indices. These are reported with `ChangeOp` `ChangeReordered` (e.g., `~ Tx.Operations[0]: reordered from index 1 to 0`), preserving document order and keeping true modifications isolated.

### Soroban ContractEvent log comparison

Simulation diagnostic events and contract events (`ContractEvent` / `DiagnosticEvent`) emitted during contract invocations are compared using dedicated event-matching in `pkg/lens/diff.go` (and through `DiffContractEvents`). Events are paired across executions by Contract ID and topic signatures. Changes within matched pairs pinpoint altered data values (`Body.V0.Data`) and topic mutations (`Body.V0.Topics`), avoiding spurious cascade diffs caused by event reordering or insertions.

### Account signer configuration and quorum drift

Security auditing requires comparing account entries before and after key rotation to ensure multisig quorums have not been weakened. `pkg/lens/diff.go` computes effective signing power changes (combining master key weight and active cosigner weights) via `AnalyzeAccountSigners` / `DiffAccountSigners` and records effective power deltas (`Thresholds.EffectiveSigningPower`). If the post-rotation signing power drops below the medium or high threshold required by the account, explicit warnings are emitted in both terminal reports and the structured diff result (`DiffResult.Warnings`).

`DiffTransactionMetaBalances` accepts decoded `TransactionMeta` versions 0
through 4 and returns deterministic deltas for account XLM balances, trustline
balances, and claimable-balance amounts. A `STATE` entry supplies the before
snapshot; create/remove changes use zero for the absent side. Metadata that
does not include a baseline sets `BeforeKnown` false rather than inventing a
balance. The CLI command `lens meta-diff` renders these records as text or
JSON. It only inspects the supplied metadata and makes no network calls.

## Statistical diff summaries

`diff_stats.go` turns a `DiffResult` into a `DiffStats`: the total number of
changes, a breakdown into modified, added, removed and type-changed paths, the
changes grouped by their top-level path section, and the deepest changed path.
`SummarizeDiff` accepts a `*DiffResult` and `SummarizeChanges` accepts a raw
`[]Change`. Both produce deterministic output; sections are emitted in
first-appearance order and the deepest path breaks ties by first occurrence.

`DiffStats` is additive: it derives everything from an existing diff and runs
no new decoding, so it adds no network path. The CLI exposes it as
`lens diff --stats`, which prints `DiffStats.String()` or, with `--json`, the
tagged JSON form.

## Path filters for structural diffs

Not every field is worth reporting. A rebuilt transaction changes its sequence
number and signatures but should otherwise be identical, and those two paths
drown out the change a reviewer actually wants to see.

`diff_filter.go` narrows a diff to the paths a caller cares about.
`NewPathFilter(include, exclude)` builds a validated `PathFilter` from
dot-separated field patterns. Within a segment the shell glob metacharacters
`*`, `?` and `[...]` apply, a segment of `**` spans any number of segments, and
a pattern also matches everything beneath it, so `V1.Tx` selects the whole
transaction subtree. An empty include set selects everything, and excludes
always win over includes. Malformed patterns wrap `ErrInvalidArgument`.

The filter is applied to the finished change list, so it never changes how the
values are compared — it only narrows the output. `DiffOptions.Filter`
integrates it with `DiffWithOptions`; `FilterChanges` and `DiffResult.Filter`
are the standalone entry points. Because filtering happens last, a diff whose
only changes are excluded reports as equal, which is what makes
`lens diff --exclude-paths ... --exit-code` a usable CI gate. The CLI exposes
it as `--include-paths` and `--exclude-paths` (comma-separated and
repeatable), and the format-side `WithUnifiedFilter`/`WithSideBySideFilter`
options apply the same selection to the line-based output.

## Hash and signature redaction for structural diffs

A rebuilt transaction or envelope is usually identical except for values that
cannot be reproduced deterministically: transaction hashes, signatures and
hints, and identifiers derived from them. `diff_redact.go` normalizes those
values before the walk so `DiffWithOptions` reports only the differences a
reviewer can act on.

Redaction is opt-in through `DiffOptions.Redaction`, a `RedactionPolicy`:

```go
result, err := lens.DiffWithOptions(left, right, lens.DiffOptions{
    Redaction: lens.RedactionPolicy{
        Hashes:     true,
        Signatures: true,
        Fields:     []string{"PrevHash", "LedgerHash*"},
    },
})
```

`Hashes`, `Signatures` and `Identifiers` select categories by the decoded XDR
type, not by a fixed path list. A `Hash` is recognized wherever it appears, a
`Signature`/`SignatureHint` is recognized as a signature, and contract, pool
and claimable-balance ids are recognized as derived identifiers. `Fields`
adds name-based matching for values that are not self-describing, such as an
anonymous byte array named `Hint`; a field pattern may be exact or use
`path.Match` glob syntax. `RedactHashesAndSignatures()` is the convenience
policy exposed by `lens diff --redact hashes,signatures`.

The prepass deep-copies and normalizes the trees; the caller's `Value`s are
never mutated. Each selected scalar is replaced with a stable placeholder
(`<redacted hash>`, `<redacted signature>`, `<redacted identifier>` or
`<redacted>`), and `DiffResult.Redacted` lists the normalized paths in
first-seen order. Nothing is redacted silently: text output prints a redaction
summary, and JSON output carries the same list in its `redacted` field. The CLI
accepts repeatable `--redact` categories (`hashes`, `signatures`,
`identifiers`, `all`) and repeatable `--redact-fields`; both are rejected with
`--footprint`, which compares sets rather than hydrated values.

## Symmetric JSON encoder and round-trip verification

`encode.go` closes the loop opened by `decode`: it turns a decoded `Value`, or
the stable JSON document `lens decode --json` emits, back into base64 XDR. The
structural encoder walks the registered Go type rather than the JSON tree, so
field names, union discriminants and enum values come from the generated XDR
definitions rather than a hand-maintained schema.

Two entry points cover the common cases:

- `Encode(v)` re-serializes `v.Raw`, the concrete XDR value behind a decoded
  tree. It is exact for every registered type and is the inverse of `Decode`.
- `EncodeJSON(data)` / `EncodeJSONAs(typeName, data)` accept the stable
  `{"type": ..., "value": ...}` document, a bare value with an explicit type,
  or the simplified transaction-envelope and ScVal dialects. Documents with an
  `xdr`, `base64`, `payload` or `raw` field pass through unchanged.

The structural encoder understands the scalar forms the enrichers produce:
StrKey accounts and contracts, canonical asset strings, `n/d` prices,
`host:port` peer addresses, `A / B @ fee%` pool parameters, claimable-balance
ids, named storage keys, wasm and external contract executables, hex or base64
byte strings, and enum names or enriched labels. Derived annotations that the
decoder appends (WASM sections, `Spec`, `EnvMeta`, `Meta`) are accepted and
ignored because they are not XDR fields. Deliberately lossy renderings, notably
`ClaimPredicate` expressions, return `ErrUnsupportedType` instead of inventing
a value; callers that hold the decoded `Value` should use `Encode` for those.

`roundtrip.go` turns the encoder contract into a property test that callers can
run on their own payloads:

```go
report, err := lens.RoundTripBase64(payload, "TransactionEnvelope")
if err != nil {
    // The input could not be decoded or encoded at all.
}
if !report.OK() {
    // report.Changes, report.BytesEqual and report.TreeEqual explain why.
}
```

`RoundTripBinary` checks that re-serializing the decoded value reproduces the
original bytes and tree. `RoundTripJSON` goes through the stable JSON document
and `EncodeJSON` instead. `RunRoundTripSuite` runs a named set of cases, and
`RoundTripRegistry` synthesizes a minimal value for every registered type and
verifies it on the chosen route. Routes report `Skipped` with a reason for
renderings that are intentionally lossy (claim predicates, and fields such as
`PeerAddress.NumFailures` that the JSON contract does not carry). The engine is
deterministic, offline and read-only.



## Unified, JSON Patch and side-by-side diff output

Three formatters turn a structural difference into a shape another tool or a
reviewer can consume. All three are deterministic and make no network calls.

- `format/unified.go` emits a standard unified diff (`---`/`+++` headers and
  `@@` hunks with context). It is what a pull-request comment bot or a
  `patch`-style consumer expects, so a structural change can be attached to a
  review verbatim. Identical values produce no output, matching diff(1).
- `format/json_patch.go` emits an RFC 6902 JSON Patch: an array of `add`,
  `remove` and `replace` operations addressed by RFC 6901 JSON Pointers
  relative to the decoded value. When the two decoded values are supplied with
  `WithJSONPatchSources`, each operation carries the full JSON subtree from the
  tree, so the patch is directly applicable; without them the scalar recorded
  on the change is used.
- `format/side_by_side.go` emits a colourised two-column view, pairing each
  removed line with the added line that replaced it. Columns are padded before
  colour is applied, so ANSI escapes never disturb the layout, and the palette
  comes from the same environment-selected theme as the other terminal output.

`format/linediff.go` provides the shared engine: it flattens a decoded value
into a deterministic one-line-per-node stream and aligns two streams with an
LCS pass whose dynamic-programming table is bounded to $2^{20}$ cells, falling
back to a wholesale replacement above that. The same bounded-alignment
discipline as the structural differ applies, so untrusted XDR cannot make the
terminal formatters allocate without limit. Hunks are built by the standard
context-merging algorithm. The CLI exposes the formats as `diff --unified`,
`diff --json-patch` and `diff --side-by-side`; the three are mutually exclusive
with each other and with `--json`/`--stats`.

### Structured JSON diff output (`--format json`)

For automated CI tools, bots, and machine consumption, `pkg/lens/format/diff.go` provides `DiffJSONFormatter` which formats diff results into a stable JSON schema:
- `left_type` and `right_type`: the XDR types compared;
- `equal`: boolean indicator of structural identity;
- `summary`: human-readable one-line change tally;
- `changes`: array of changes, each specifying `path`, `op` (`modified`, `added`, `removed`, `type_changed`, `reordered`), `before`, `after`, and enriched string notes;
- `redacted`: list of normalized cryptographic paths (if redaction was applied);
- `warnings`: list of non-fatal security warnings, such as quorum degradation below thresholds.

The CLI exposes this format via `lens diff --format json` (as well as `--json`).

## Unordered signer and signature diffs

Signatures and signers are sets, not sequences: a transaction signed by the
same keys is the same transaction whatever order the signatures appear in.
`signer_diff.go` compares these collections as multisets, matching duplicates
by count, through `DiffSignerKeys`, `DiffSigners` and `DiffDecoratedSignatures`.
Identity uses each element's canonical XDR encoding, so a reordered set is
equal, a weight change is one removal plus one addition, and a genuine
insertion or removal is reported as exactly that.

The structural differ integrates the same behaviour through
`DiffOptions.UnorderedSigners`: with it set, list fields named `Signatures`,
`Signers` or `ExtraSigners` are compared as multisets and every other list
stays positional. The CLI enables it with `lens diff --unordered-signers`.

## Soroban footprint set diffs

`footprint_diff.go` computes the read-only and read-write set differences
between two Soroban ledger footprints. Keys are identified by their canonical
XDR encoding and rendered with `ledgerKeySummary`, so the output names accounts,
trustlines and contract data keys rather than opaque bytes. `DiffFootprintKeys`
works on raw `[]LedgerKey`; `DiffFootprints` and `DiffSorobanFootprints` handle
whole footprints and `SorobanTransactionData`; `FootprintFromEnvelope` and
`FootprintFromValue` extract a footprint from a V1 or fee-bump envelope or a
standalone data value. The CLI enables it with `lens diff --footprint`, which
also accepts `--json`.

## Envelope/result alignment

`envelope_result_diff.go` is the pairwise counterpart to `ExplainPair`. Given a
`TransactionEnvelope` and its `TransactionResult` it pairs every operation with
the operation result at the same index and reports mismatched operation and
result types, operations with no result of their own, results with no matching
operation, and each failed operation's plain-English result code.
`DiffEnvelopeResult`, `DiffEnvelopeResultValues` and
`DiffEnvelopeResultBase64` cover the concrete, `Value` and base64 entry points.

The result carries the aligned entries plus a `DiffResult` view
(`DiffResult()`), a `DiffStats` view (`Summarize()`), a readable report
(`String()`) and an aligned two-way `Node` tree (`Tree()`) that places each
operation's subtree beside its result's. The CLI command `lens result-diff`
renders the report, or the JSON form with `--json`.

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

### Liquidity pools and share tokens

`pkg/lens/liquidity_pool.go` covers the Protocol 18+ liquidity pool family:

- `xdr.PoolId` renders as its 64-character hex value with a
  `constant product pool id` note. Protocol 18 defines a single pool formula,
  so the note is always accurate.
- `xdr.LiquidityPoolConstantProductParameters` collapses into the asset pair
  and fee, for example `native / USDC:GA... @ 0.3%`; the fee is stored in
  basis points and rendered as a percentage.
- A `LiquidityPoolEntry` node hook annotates the `LiquidityPoolId` leaf with
  the pair and fee from `Body.ConstantProduct`, so the id line answers "which
  market is this?" without descending into the body.
- `TotalPoolShares` renders as decimal pool share tokens next to its stroop
  count (`200000.0000000 pool share tokens (stroops: 2000000000000)`) rather
  than as lumens, and `PoolSharesTrustLineCount` renders as
  `held by 150 trustlines`. Both notes are registered per containing type, so
  no other `Int64` field picks them up.

Every rendering is a pure function of the decoded value: no clock, no
environment and no network access.

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
      └─ Executable: deadbeef…0c0d  wasm:deadbeef…0c0d
```

`ContractDataEntry` is registered (priority 5), so `lens decode --type
ContractDataEntry` decodes a standalone blob, and an entry embedded in a
`LedgerEntry` decodes the same way at `Data.ContractData`.

### Ledger headers, flags and upgrade votes

`pkg/lens/ledger_header.go` teaches the tool about the parts of a ledger
header that are opaque integers on the wire:

- **BaseReserve and BaseFee** are Uint32 protocol parameters denominated in
  stroops. They join the shared `amountFields` list, so `BaseReserve: 5000000`
  renders as `0.5000000 (stroops: 5000000)` — the same decimal form used for
  balances and fees everywhere else.
- **Liquidity-pool control flags** live in `LedgerHeaderExtensionV1.Flags`.
  The three `DISABLE_LIQUIDITY_POOL_*` bits expand to their Horizon-style
  names (`disable_liquidity_pool_trading`, `disable_liquidity_pool_deposit`,
  `disable_liquidity_pool_withdrawal`), joined with ` | ` when several are
  set. The note is keyed on the extension type, so other `Flags` fields in the
  protocol are untouched.
- **Upgrade votes** in `StellarValue.Upgrades` are opaque `UpgradeType` blobs.
  Each blob is a serialized `LedgerUpgrade`; the registered enricher
  unmarshals it and names the proposed change — `protocol version 23`,
  `base fee 0.0000200 XLM (200 stroops)`, `max tx set size 1200`, and so on.
  Blobs that do not parse decline and keep the generic hex rendering, so a
  malformed proposal is still visible.

A ledger header is intentionally not collapsed into a single scalar: the tree
keeps every field, and the human reading is the sum of its per-field notes.

### Field-name enrichment

Some renderings depend on the field name rather than the type. XDR models
amounts, sequence numbers and offer ids with the same integer types, so the
only signal that a value is an amount is the field it sits in. `amountFields`
in `enrich.go` lists the known amount fields — both plain `Int64` fields and
the `Uint32` union arms of a `LedgerUpgrade`, which arrive pointer-wrapped.
Anything not listed stays a plain integer, because rendering a sequence number
as `12345.0000000` would be worse than showing it raw.

## Ledger upgrades and protocol votes

Validators vote for ledger upgrades — protocol version bumps, base fee changes,
base reserve changes — and the protocol carries those votes as opaque bytes:
`StellarValue.upgrades` is "a vector of encoded `LedgerUpgrade` so that nodes
can drop unknown steps during consensus". A decoder therefore has to try
harder than usual. `enrichUpgradeType` in
[`ledger_header.go`](../pkg/lens/ledger_header.go) unmarshals each `UpgradeType` as an
`xdr.LedgerUpgrade` and requires the input to be consumed exactly; a vote that
does not fit — including an upgrade newer than this build — declines and keeps
its generic hex rendering.

```
$ lens decode --type StellarValue "$(cat testdata/stellar_value_votes.txt)"
StellarValue
├─ TxSetHash: [26 202 157 122 123 201 199 246 109 239 44 246 156 233 24 163 241 22 124 52 125 219 2 144 174 151 255 29 205 25 24 144]  1aca9d7a7bc9c7f66def2cf69ce918a3f1167c347ddb0290ae97ff1dcd191890
├─ CloseTime: 1790000000  2026-09-21T14:13:20Z
├─ Upgrades  [3]
│  ├─ [0]: 0000000100000015  protocol version 21
│  ├─ [1]: 0000000400989680  base reserve 1.0000000 XLM (10000000 stroops)
│  └─ [2]: deadbeef
└─ Ext
   └─ V: StellarValueTypeStellarValueBasic
```

The hex payload stays the machine value and the decoded vote becomes the note,
so `--json` output remains byte-exact while the tree names the vote.

`LedgerUpgrade` is registered as a type in its own right, so a single vote can
be decoded directly — which matters because the common arms are only eight
bytes:

```
$ lens decode --type LedgerUpgrade "$(cat testdata/ledger_upgrade_base_reserve.txt)"
LedgerUpgrade
├─ Type: LedgerUpgradeTypeLedgerUpgradeBaseReserve
└─ NewBaseReserve: 10000000  1.0000000 (stroops: 10000000)
```

That eight-byte shape hides one inherent ambiguity worth stating: the wire
encoding of the base reserve arm is byte-identical to an `ScVal` i32, and any
eight bytes are also a valid `Price`. Detection ranks `ScVal` first for such
payloads; `--type LedgerUpgrade` pins the interpretation. Votes nested in a
`StellarValue` or `LedgerHeader` are unambiguous.

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
[`NO_COLOR`](https://no-color.org) convention (suppressing color whenever `NO_COLOR != ""`),
disables ANSI styling when `TERM=dumb`, and checks whether the writer is a
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

### Deepening failures with the operation body

[`reason.go`](../pkg/lens/reason.go) explains every result code on its own.
Some failures deserve more. When an envelope is paired with its result,
`attachResult` hands each failed operation's reason to
`deepenOperationFailure`, which rewrites only the summary and hint for codes
whose cause is provable from the operation's own parameters:

- **`set_options`**: which threshold weight exceeded 255, which flag bit is
  both set and cleared, which unrecognised bits were submitted, whether the
  signer is the source account itself (resolved through any operation-level
  source override), and whether a new signer subentry outran the account's
  reserve. Every condition mirrors stellar-core's `doCheckValid`, which checks
  them from the operation body alone.
- **`liquidity_pool_deposit` / `liquidity_pool_withdraw`**: slippage against
  the accepted price window (flagging an exact-ratio window and amounts that
  already sit outside it), which deposit parameter is malformed, and how the
  requested amounts map to `underfunded`, `pool_full`, `line_full` and
  `under_minimum`.
- **`clawback` / `clawback_claimable_balance`**: when the trustline or claimable balance was created without the clawback-enabled bit, when the target account has no trustline, and when the requested amount exceeds the balance held, the explanation points directly at the issuer flag, trustline state, or insufficient balance rather than only naming the generic protocol code.
- **`account_merge`**: which non-signer subentries block the merge (active
  trustlines including pool-share trustlines, open DEX offers, data entries
  and claimable balances) and the operations that close them; the
  ledger-sequence bound behind `seqnum_too_far` (`ledgerSeq << 32`, which no
  transaction can lower); the destination's lumen buying liabilities behind
  `dest_full`, named with the destination address from the operation body;
  and the permanence of `auth_immutable` behind `immutable_set`. Each mirrors
  the corresponding check in stellar-core's `MergeOpFrame::doApply`, in the
  same order core applies them.

It never touches `Code` or `Constant`, never invents ledger state, and a code
it cannot pin down keeps the generic per-code text — a result explained
without its envelope produces the same explanations as before. All analysis is
pure arithmetic on supplied XDR (exact `big.Int` for price comparisons): no
network calls, deterministic output.

### Create claimable balance

A `create_claimable_balance` failure names the asset, amount, source account
and claimants the operation was about. `deepenOperationFailure` resolves those
arguments through `withOperationContext` and states the trustline or reserve
requirement that was missed; the detail is attached to `Reason.Context` rather
than folded into the static reason table. The checks mirror stellar-core's
`doCheckValid` and `doApply`:

| Result code | Context resolved from the envelope |
| --- | --- |
| `create_claimable_balance_malformed` | The offending rule: non-positive amount, empty claimant list, duplicate destination, or the claimant whose predicate is invalid |
| `create_claimable_balance_low_reserve` | The claimants and the exact native reserve the balance locks: **(2 + claimant count) × 0.5 XLM** |
| `create_claimable_balance_no_trust` | The asset and the source account that must open a trustline with `change-trust` |
| `create_claimable_balance_not_authorized` | The asset and the source account whose trustline the issuer must authorise |
| `create_claimable_balance_underfunded` | The amount, asset and claimants, and whether the shortfall is spendable XLM or a trustline balance |

Context is additive. `ExplainCode` still returns the same static `Reason` from
the table in `reason.go`, so result-only explanations, `ReasonByCode` and every
existing consumer keep working unchanged; `--json` simply gains an optional
`context` field, and the text and Markdown formatters print it beside the
operation result.

`ClaimableBalanceReserve(claimantCount)` exposes the same formula as stroops,
for callers that want the figure without formatting a `Summary`. Claimants do
not need a trustline to be named on the balance; they need one when they claim
it, so the context separates the creator's trustline requirement from the
claimant's.

The reserve figures are documented constants, not network lookups: lens is
offline-first and never queries ledger parameters, so 0.5 XLM (the public
network default) is stated explicitly rather than derived at runtime.

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

## Claimable-balance predicate failures

`claim_claimable_balance_cannot_claim` means at least one claimant's
`ClaimPredicate` evaluated false at the ledger close time. Predicates may nest
absolute and relative time checks under `and`, `or` and `not`.

[`claimable_explain.go`](../pkg/lens/claimable_explain.go) evaluates that tree
locally through `DiagnoseClaimablePredicate`. The caller supplies both the
current ledger close time and the ledger close time when the balance was
created; the entry itself does not carry the latter. Relative predicates use
`created close time + relBefore`, with the protocol's strict less-than
comparison. Malformed unions and excessively deep trees return classified
errors instead of panicking.

Tests evaluate the predicate from the canonical `claimable_balance_entry`
fixture and construct nested predicates for boolean and time-boundary cases.

## Soroban resource-limit rejection

[`resource_explain.go`](../pkg/lens/resource_explain.go) inspects a
`TransactionResult` with `DiagnoseResourceFailure`, distinguishing invalid
resource declarations (`tx_soroban_invalid`), host resource-limit exhaustion,
and insufficient refundable fees. CPU and memory counters are not present in
those XDR result codes; when simulation provides used/limit values,
`DiagnoseSimulationResponse` remains the API that reports the exact binding
resource. The result explainer reads only supplied XDR and returns no diagnosis
for unrelated result codes.

## Sponsorship authorization and revocation

[`sponsorship_explain.go`](../pkg/lens/sponsorship_explain.go) combines
sponsorship operation result codes with the transaction's begin/end/revoke
operations. `DiagnoseSponsorship` identifies a failing operation and flags
unmatched or overlapping begin/end instructions, while retaining operation
indexes and sources for correlation. It does not infer ledger sponsorship
ownership without ledger-entry input; revoke ownership and reserve errors are
reported from their exact XDR result codes.

## Protocol-upgrade ledger boundaries

[`upgrade_explain.go`](../pkg/lens/upgrade_explain.go) compares adjacent
`LedgerHeader` values and a transaction result. `DiagnoseProtocolUpgrade`
returns no diagnosis unless the protocol version changes and the transaction
fails. Unsupported-feature result codes are marked as likely related; other
codes are reported as coincident with, but not specifically implicating, the
upgrade. This is a deterministic correlation, not proof of causation, and
requires no ledger or RPC lookup.

These four context-dependent diagnosers are library APIs. The existing
`lens explain` flags and output contracts are unchanged; use `lens explain`
for the standard result-code report, and call the library helpers when the
relevant ledger context or claim-balance creation time is already available.

## Path payment slippage diagnostics

[`explain.go`](../pkg/lens/explain.go) provides contextual diagnostics for `PathPaymentStrictReceive` and `PathPaymentStrictSend` failures, particularly:
- `path_payment_strict_receive_over_send_max`: The send amount required to produce the requested destination amount exceeded `sendMax`.
- `path_payment_strict_send_under_dest_min`: The destination amount delivered by the fixed send amount fell below `destMin`.
- `path_payment_strict_receive_too_few_offers` and `path_payment_strict_send_too_few_offers`: Order books along the conversion route lacked sufficient liquidity depth to fulfill the payment.

`DiagnosePathPaymentSlippage`, `DiagnosePathPaymentStrictReceive`, and `DiagnosePathPaymentStrictSend` inspect the operation and operation result, breakdown intermediate hops (`PathPaymentHop`) along the payment path, and explain how order book liquidity shifts between quote and submission cause slippage past limit parameters. When paired with `ExplainPair`, these diagnostics are attached directly to each operation's summary.

## SetTrustLineFlags and revocation violations

[`explain.go`](../pkg/lens/explain.go) provides explicit diagnostics for `SetTrustLineFlags` failures:
- `set_trust_line_flags_cant_revoke`: In Stellar protocol, trustline authorization is irrevocable by default. An asset issuer must set `AUTH_REVOCABLE_FLAG` (flag `0x2`) on their account via `SetOptionsOp` before issuing the asset if they wish to clear `AUTHORIZED_FLAG` or `AUTHORIZED_TO_MAINTAIN_LIABILITIES_FLAG`. If the issuer lacks `AUTH_REVOCABLE_FLAG`, revocation fails with `cant_revoke`.
- `set_trust_line_flags_no_trust_line`: The target trustor account has not yet established a trustline for the asset via `ChangeTrustOp`.
- `set_trust_line_flags_malformed`: The operation is structurally invalid (such as attempting to modify trustline flags on native XLM, which does not use trustlines, or submitting from an account other than the asset issuer).
- `set_trust_line_flags_invalid_state` and `set_trust_line_flags_low_reserve`: The operation requested conflicting flag transitions or the account lacks XLM reserves to support the updated trustline state.

`DiagnoseSetTrustLineFlags` and `DiagnoseSetTrustLineFlagsOperation` output structured diagnostic reports with actionable remediation advice and highlight `AUTH_REVOCABLE_FLAG` requirements.

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

Structural-diff benchmarks live in
[`pkg/lens/diff_bench_test.go`](../pkg/lens/diff_bench_test.go). They cover
deeply nested trees, the worst-case single change at the deepest leaf, wide
lists with and without the opt-in LCS aligner, real ledger-close envelopes, and
the redaction prepass:

```sh
go test ./pkg/lens -run '^$' -bench 'BenchmarkDiff' -benchmem
```

The same package verifies the encoder contract as a property: `roundtrip_test.go`
runs every checked-in fixture through both `RoundTripBinary` and
`RoundTripJSON`, and `RoundTripRegistry` synthesizes a minimal value for every
registered type. Cases that cannot be represented by the stable JSON contract
are asserted as skipped with a reason rather than silently passed. Run the
property suite with the regular tests:

```sh
go test ./pkg/lens -run 'RoundTrip' -v
```

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

## HTML standalone reports (`pkg/lens/format/html.go`)

`HTMLFormatter` generates a self-contained, CSS-styled HTML page displaying the decoded Node tree and explain outcomes.
- Embeds all styles inline with zero external network assets or CDN dependencies, adhering to offline-first principles.
- Formats both transaction summaries (headline, outcome badge, operations table, preconditions) and complete hierarchical node trees.
- Reachable from the CLI as `lens decode --format html`.

## Mermaid flowchart formatter (`pkg/lens/format/mermaid.go`)

`MermaidFormatter` converts transaction operations and participant accounts into Mermaid graph definitions (`graph TD`).
- Renders source accounts, fee-bump wrappers, and directed arrows indicating operation details, asset transfers, or Soroban contract calls.
- Provides fallback rendering of node hierarchies for general XDR types.
- Reachable from the CLI as `lens decode --format mermaid`.

## JUnit XML test reports (`pkg/lens/format/junit.go`)

`JUnitFormatter` renders decoded values as a JUnit XML `<testsuite>` that
Jenkins, GitLab CI, CircleCI and other CI systems already know how to display.
- Each value becomes one `<testcase>`, named after its XDR type, so an
  envelope and its result can be reported side by side in one suite.
- A test case fails when `lens.Explain` resolves a failed transaction outcome.
  The `<failure>` element carries the most specific result code as its `type`
  and the explain headline as its `message`, with the transaction and
  per-operation reasons, context and hints in the element body.
- Decoded values that `lens.Explain` does not cover (everything except
  `TransactionEnvelope` and `TransactionResult`) pass: reaching a formatter
  means the value decoded successfully. Nil values are rejected instead of
  being reported as test cases.
- Output is written through `encoding/xml`, so quotes, angle brackets,
  ampersands and newlines in memos or failure messages are escaped and the
  document always parses. `tests`, `failures` and `errors` counts are derived
  from the values themselves.
- Output is deterministic: durations are reported as `0.000` rather than read
  from a clock, and nothing depends on the environment or the network.
- Reachable from the CLI as `lens decode --format junit`.

## Precondition Expiry Verification (`pkg/lens/timebounds_explain.go`)

`VerifyPreconditionExpiry` and `CheckPreconditionExpiry` verify transaction preconditions (`TimeBounds` and `LedgerBounds`) against a reference timestamp.
- Evaluates whether the transaction window is currently valid, not yet valid (`tx_too_early`), or expired (`tx_too_late`).
- Supports an optional reference time (RFC 3339 or Unix timestamp) defaulting to UTC now.
- Prominently warns when bounds have elapsed or remain inactive.
- Reachable from the CLI as `lens explain --check-expiry [--reference-time <timestamp>]`.

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

## Exit code policy (`internal/cli/exitcodes.go`)

Every error the CLI can produce resolves to exactly one of four pinned exit
codes, and scripts are entitled to rely on that table:

| Code | Constant | Meaning |
|------|----------|---------|
| 0 | `ExitSuccess` | clean run |
| 1 | `ExitError` | generic error: usage mistakes, missing files, unreadable environments — anything not caused by the payload |
| 2 | `ExitDiffFound` | the "checked condition is false" slot: `diff --exit-code`/`--quiet` finding a difference, `explain --fail-on-error` seeing a failed transaction, `result-diff --exit-code` seeing a disagreement |
| 3 | `ExitInvalidXDR` | the payload itself could not be decoded |

Classification in `ExitCode(err)` (which returns the code plus whether the
message is printable) is stable across wrapping:

1. errors carrying an explicit code — `errTransactionFailed` and
   `errValuesDiffer` are defined once here and shared by explain and diff —
   resolve to their own code and are not printed (the command already
   summarized its condition);
2. decode failures are matched with `errors.Is` against the library sentinel
   family — `ErrInvalidXDR`, `ErrInvalidBase64`, `ErrNoMatch`, `ErrNonCanonical`,
   `ErrPrematureEOF`, `ErrInvalidFraming`, `ErrRecordTooLarge`,
   `ErrCorruptMultiStreamRecord` — and exit 3;
3. everything else, including `ErrUnknownType` and `ErrEmptyInput`, exits 1.

The distinction that matters operationally: a CI gate that sees exit 2 knows
its pipeline worked and the artifact really is different/failed; exit 3 means
"the bytes you fed in are garbage", which is a caller bug, and exit 1 means the
tooling itself was used wrongly. `IsInvalidXDRError` is re-exported for callers
who need only the classification.

## Stdin framing probe (`pkg/lens/stdin_pipe.go`)

Standard input carries no metadata, so `ReadStdinPayload` (exposed as
`NewStdinPipe` for direct use) probes the first bytes before committing to an
interpretation:

- **Text framing** — if the probe sees only base64 or hex alphabet characters,
  whitespace or `#` comment markers, the payload is handled by
  `MultiStreamDecoder`: newline-separated base64 records, comment lines and
  blank lines all decode, matching `decode --multi` behaviour.
- **Binary framing** — anything else goes to `StreamParser` with
  `FramingAuto`, which recognizes RFC 5531 record marking and
  length-prefixed framing and yields the first record.
- `Framing()` reports what was detected (`StdinFramingLines`,
  `StdinFramingBinary` or `StdinFramingUnknown`), `ReadPayload()` returns the
  first record, and `Decode()`/`DecodeEach()` bind it to the registry.

Two robustness details the probe guarantees: reads are chunked through a
buffer that tolerates readers delivering fewer bytes than requested (the probe
never misreads a partial first chunk), and the default lookahead size is
configurable with `WithStdinPipeBuffer` so binary framing can be pinned before
the data arrives. `internal/cli/input.go` routes every stdin invocation through
this probe — there is exactly one stdin policy for all commands. Empty input
surfaces as `lens.ErrEmptyInput`, which the CLI maps to a usage error rather
than the invalid-XDR code.

## Shell completion (`internal/cli/completion.go`)

`lens` disables cobra's default completion command and installs its own:

```
lens completion bash|zsh|fish|powershell
```

`registerCompletions(root)` runs **after** the command tree is fully built in
`newRootCmd` — running it earlier sees only `rootCmd` and silently completes
nothing — and walks every command:

- **Dynamic flag values**: `--type`/`-t` on `decode`, `explore`, `diff` and
  `encode` complete from `lens.TypeNames()`, the same registry the `types`
  command prints, so nothing can drift between them.
- **Choice lists**: `--format` (tree/json/compact/summary/txrep/csv/junit),
  `--color`, `--network`, and the repeatable `--redact` prefix set are static
  candidates.
- **Shell scripts**: all four render through cobra's `Gen*Completion`, with the
  whole feature covered by driving cobra's hidden `__complete` entry point from
  tests (`completion_test.go`) — the same channel real shells use.

Completion output is cached in the shell session; there is no network access
in any of it.

## Testdata corpus synchronizer (`pkg/lens/corpus_sync_test.go`)

The captured fixtures in `testdata/` are the regression suite's ground truth,
and `corpus_sync_test.go` is the tool that keeps that directory honest: every
file must either appear in `manifest.json` (captured section by envelope file,
synthetic section by key plus `.txt`) or the manifest check fails, and every
captured record must decode and carry its transaction hash.

The synchronizer reads the most recent transactions from public Testnet, but
**only** when asked. The gated run behind `LENS_CORPUS_SYNC` implements
three levels:

| Value | Behaviour |
|-------|-----------|
| unset | no network, no writes: only the offline invariants run |
| `1` | dry run: fetches the page, plans it, verifies kept envelopes decode, writes nothing |
| `write` | additionally captures new `testnet_<hash16>.env.txt`/`.res.txt` fixtures and merges entries into `manifest.json` |

Selection (`planCorpus`) is deterministic and network-free by construction:
every candidate is decoded *before* selection (fixtures that fail to decode
would be dead weight), records whose transaction hash is already in the
manifest are dropped, and duplicates — the same envelope bytes under different
hashes — collapse to the lexicographically smallest hash, so the plan depends
only on the page content and the current manifest. Unit tests exercise the
whole selection, naming and manifest-merge logic against an in-memory stub
client (`stubCorpusClient`), which is why the default `go test` run never
opens a socket. `internal/cli` is never involved; no fetch path exists in
`pkg/lens` non-test code.

```
Horizon page ──► planCorpus (decode, dedupe, hash-name) ──► corpusPlan
                     │                                        │
        ┌────────────┴───────────┐            manifest.json ◄── writeCorpusPlan (write mode only)
        │ stub (unit tests)      │                (merge, never overwrite)
        │ HTTP (LENS_CORPUS_SYNC)│
```

`writeCorpusPlan` is deliberately conservative: fixture files are only created
when absent (existing files are never overwritten) and manifest entries are
merged — pre-existing provenance survives an updated capture.

