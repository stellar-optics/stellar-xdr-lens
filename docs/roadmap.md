# Roadmap

An honest account of where this is going. Dates are intentions, not commitments.

## Where things stand (v0.1)

Working and tested:

- `decode` with automatic type detection, tree and stable JSON output
- `explain` for envelopes, results, and the two paired together
- `diff` with path-level structural comparison, opt-in LCS list alignment,
  recursive Soroban map/vector comparison, exact ScVal wide-integer values,
  include/exclude path filters, and unified, RFC 6902 JSON Patch and
  side-by-side output modes
- `meta-diff` with offline account, trustline, and claimable-balance deltas
- `asset`, deriving Stellar Asset Contract ids offline for any network
- `types` with a machine-readable `--json` index of every registered type
- 63 registered XDR types, including ledger headers, bucket entries, contract
  data (with its storage durability), SCP messages, overlay messages
  (time-sliced surveys included), history-archive sets, account extensions,
  data entries and the state-archival types; all 208 protocol result codes
  explained
- Input from arguments, files or stdin; exit codes suitable for CI
- No network access anywhere in the library or tests

Known limits, stated plainly:

- **63 types, not ~400.** The registry covers what developers actually paste
  into a terminal. Anything else needs a one-line addition.
- **Soroban values render structurally.** `ScVal` diffs recurse through maps
  and vectors and normalize wide integers, but without a contract spec `lens`
  cannot name a struct's fields or an enum's variants — it shows the wire shape.
- **Detection is ambiguous for short values.** This is inherent to XDR, not a
  bug. `--candidates` and `--type` exist for it.
- **The library API is not frozen.** Expect breaking changes to `pkg/lens`
  before v1.0.

## Near term

### Broaden type coverage

Ledger headers, bucket entries, SCP messages, state-archival types, overlay
messages (including time-sliced surveys), the history-archive sets, account
extensions, data entries and contract code — with the interface spec embedded
in its WASM — are now covered, on top of the transaction, Soroban and
ledger-entry families. What remains is the long tail — individual result and
effect types, and the operation-specific helpers. Each is a registry line, a
`typeDocs` entry and a test. Good first contributions.

### Soroban diagnostic events

When a contract traps, the actionable detail is in the diagnostic events, not
the result code. `lens` currently tells you the call trapped and points you at
simulation; decoding and rendering `DiagnosticEvent` payloads directly would
close that loop. This is the single highest-value addition.

### `lens encode`

The inverse of `decode`: JSON in, base64 XDR out. Useful for building test
fixtures and for round-trip verification in CI. Straightforward given the
existing registry, but needs care so the JSON contract stays symmetric.

## Later, and less certain

### Contract-spec-aware rendering

Given a contract's `ScSpecEntry` definitions, render `ScVal` payloads with real
field and variant names instead of the wire shape. This is what would make
Soroban debugging genuinely pleasant. It needs a way to supply the spec — a
local WASM file or a `--spec` flag — and is a substantial piece of work.

### Optional network lookups

`lens explain <tx-hash>` fetching from Horizon or an RPC endpoint would be
convenient. It is deliberately deferred: offline-and-deterministic is a
property worth protecting, so any network access must be strictly opt-in,
confined to the CLI (never the library), and never on a default path.

### txrep (SEP-0011)

Human-readable txrep is a defined standard and [`xdrpp/stc`](https://github.com/xdrpp/stc)
already implements it well in Go. Worth adding as an output format only if
users ask; duplicating a working tool is not a goal.

## Explicitly not planned

- **A GUI or web interface.** [Stellar Lab](https://lab.stellar.org) exists and
  is good. This is a terminal and CI tool.
- **Transaction building or signing.** That is
  [`txnbuild`](https://pkg.go.dev/github.com/stellar/go-stellar-sdk/txnbuild)'s
  job. `lens` reads; it does not construct or sign.
- **Key management of any kind.** `lens` never handles secret keys, and adding
  that would make an offline inspection tool a security-sensitive one.
- **Reimplementing `stellar xdr`.** Where the official CLI already does the job
  well, this tool should defer to it rather than compete. See the comparison
  table in the [README](../README.md).

## Contributing to the roadmap

Adding types, enrichers and result-code explanations is designed to be a
one-file change — see [architecture.md](architecture.md). If you want something
here sooner, open an issue saying what you are debugging and why the current
output falls short; concrete cases are more useful than feature requests.
