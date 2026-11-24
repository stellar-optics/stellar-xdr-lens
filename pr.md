# Pull Request: Comprehensive Documentation, Semantic Diff Masking, LedgerCloseMeta Ingestion, and Contributor Guides

### Closes / Resolves Issues
- Closes #344: `docs: create comprehensive CLI user guide with workflow recipes and shell aliases`
- Closes #345: `docs: author guide on semantic diffing and masking volatile fields in CI pipelines`
- Closes #346: `docs: document LedgerCloseMeta and bucket list storage internals for indexer developers`
- Closes #347: `docs: write contributor guide for implementing new XDR enrichers and formatters`

---

## Overview & Motivation

`stellar-xdr-lens` is a critical offline CLI utility and Go library used by protocol developers, smart contract engineers, security auditors, and indexer authors. While the core engine provides high-performance decoding, structural comparison, and failure diagnosis, the project lacked dedicated, end-to-end documentation for common real-world workflows and internal extension points. Furthermore, CI workflows comparing deterministic transactions suffered from false-positive diff alerts caused by volatile transaction fields (sequence numbers, signatures, timestamps).

This PR addresses four core areas:
1. **CLI User Guide & Workflow Recipes (#344):** Authors `docs/cli/user_guide.md`, providing an exhaustive walkthrough of all subcommands (`decode`, `diff`, `meta-diff`, `explain`, `explore`, `types`, `encode`, `asset`, `completion`, `version`), practical pipeline recipes with `curl` and `jq`, and shell aliases for terminal productivity.
2. **Semantic Diffing & CI Masking Guide & CLI Support (#345):** Authors `docs/guides/semantic_diffing.md`, explaining masking versus excluding, path globbing syntax (`*`, `?`, `**`, `[index]`), numerical tolerance (`--diff-threshold`), and exit code semantics (0, 1, 2, 3). Implements `IgnorePaths` in `pkg/lens.DiffOptions` as well as CLI flag aliases (`--mask-path`, `--ignore-path`, `--ignore-paths`) to prevent CI false positives.
3. **LedgerCloseMeta & Bucket List Guide & Enrichment (#346):** Authors `docs/guides/ledger_close_meta.md` covering the protocol evolution of `LedgerCloseMeta` (v0, v1, v2), state change classification (`CREATED`, `UPDATED`, `DELETED`, `STATE`, `RESTORED`), bucket list hash trees, and Soroban state archival (TTL, rent, eviction). Implements the `enrichLedgerEntryChangeType` enricher in `pkg/lens/bucket_entry.go` and provides complete, runnable Go code for indexers.
4. **Authoring Enrichers & Formatters Contributor Guide (#347):** Authors `docs/contributing/authoring_enrichers.md`, detailing the decoupled one-way pipeline (`raw XDR -> Node tree -> enricher -> Formatter`), explaining the non-destructive `Note` mechanism and panic-safe `init()` registration, surveying all existing formatters, providing a step-by-step tutorial for authoring a custom enricher and custom Graphviz DOT formatter, and detailing testing standards with canonical fixtures in `testdata/`.

---

## Detailed Summary of Changes

### 1. CLI User Guide & Workflow Recipes (`#344`)
- Created `docs/cli/user_guide.md` covering:
  - Command walkthroughs with flags and real-world inputs: `lens decode`, `lens diff`, `lens meta-diff`, `lens explain`, `lens explore` (TUI navigation & keybindings), and `lens types`.
  - Secondary command reference: `lens encode`, `lens asset`, `lens completion`, and `lens version`.
  - Pipeline recipes:
    - Querying failed transactions from Horizon and piping to `lens explain`.
    - Streaming recent Soroban contract events from RPC and parsing with `jq`.
    - Batch auditing ledger close fixture streams.
    - Automated CI pull request diff evaluations.
  - Shell aliases and bash/zsh helper functions (`xdr-decode`, `xdr-explain`, `xdr-diff`, `xdr-fetch-tx`, `xdr-scval`).
- Updated `README.md` with links to the user guide under Usage.

### 2. Semantic Diffing, Volatile Field Masking, & CI Templates (`#345`)
- Created `docs/guides/semantic_diffing.md` covering:
  - The volatile field challenge in continuous integration (`SeqNum`, `Signatures`, `TimeBounds`, nonces, fees).
  - The fundamental distinction between masking (`--mask-path` / `DiffOptions.MaskPaths` evaluating to equal) and excluding (`--exclude-paths` / `--ignore-path` filtering presentation only).
  - Path syntax and wildcard specifications (`*`, `?`, `**`, `[index]`).
  - Numerical tolerance configuration via `--diff-threshold`.
  - Exit code contract: `0` (clean/equal), `1` (CLI error), `2` (diff detected), `3` (invalid XDR).
  - Production-ready CI workflows: complete GitHub Actions workflow and GitLab CI job templates.
- Updated `pkg/lens/diff.go`:
  - Added `IgnorePaths []string` to `DiffOptions` as a first-class alias for `MaskPaths`.
  - Combined `MaskPaths` and `IgnorePaths` in `DiffWithOptions`.
  - Added godoc documentation explaining masking versus filtering.
- Updated `internal/cli/diff.go`:
  - Added flag aliases `--mask-path`, `--ignore-path`, `--ignore-paths`, `--include-path`, and `--exclude-path` to support singular and plural forms interchangeably.
- Added comprehensive unit tests in `pkg/lens/diff_test.go` and `internal/cli/diff_modes_test.go`.

### 3. LedgerCloseMeta & Bucket List Storage Internals (`#346`)
- Created `docs/guides/ledger_close_meta.md` covering:
  - Detailed architecture of `LedgerCloseMeta` across protocol versions: `v0` (Classic), `v1` (Protocol 20 Soroban debut with generalized transaction sets), and `v2` (Protocol 20+ state archival, eviction, and fee metrics).
  - State change decomposition across all 5 transition types: `CREATED`, `UPDATED`, `DELETED` (tombstone with `LedgerKey`), `STATE` (pre-tx snapshot), and `RESTORED` (archived entry resurrected).
  - State archival mechanics: Live Bucket List vs Hot Archive, `BucketEntry` variants, `TTLEntry`, `liveUntilLedgerSeq`, durability tiers (Persistent, Instance, Temporary), rent models, and eviction.
  - Using `pkg/lens` as an indexer library: Complete, runnable Go examples demonstrating transaction extraction, contract event handling, net balance delta computation via `lens.DiffTransactionMetaBalances`, and stream batch processing via `LedgerCloseMetaBatch`.
- Updated `pkg/lens/bucket_entry.go`:
  - Added `enrichLedgerEntryChangeType` enricher for `xdr.LedgerEntryChangeType` (`created`, `updated`, `deleted (removed)`, `state (snapshot)`, `restored from archive`).
  - Registered `xdr.LedgerEntryChangeType(0)` in package `init()`.
- Added unit tests in `pkg/lens/bucket_entry_internal_test.go`.

### 4. Authoring Enrichers & Output Formatters Contributor Guide (`#347`)
- Created `docs/contributing/authoring_enrichers.md` covering:
  - The decoupled pipeline: `base64 -> decode -> Node tree -> enrich -> Formatter -> output`.
  - Enricher lifecycle: tree traversal, `Note` attachment without mutating underlying values or types, lock-free registration in package `init()`, and defensive handling with panic recovery.
  - Formatter interface: contract specification (`Format(w io.Writer, v *lens.Value) error`), recursive `Node` traversal, and theme/color palette integration (`format.Palette`, `NO_COLOR`, `TERM`).
  - Review of all existing formatters: `tree`, `json`, `text`, `mermaid`, `summary`, `txrep`, `csv`, `html`, and `junit`.
  - Step-by-step tutorial:
    - Authoring a custom Enricher (`enrichEscrowStatus`) with unit tests.
    - Authoring a custom Formatter (`format.DotFormatter` for Graphviz directed graphs).
  - Testing standards: canonical fixtures in `testdata/`, table-driven tests, and offline execution requirements.
- Updated `CONTRIBUTING.md` and `docs/architecture.md` with links and cross-references.

---

## Verification & Testing Evidence

All tests pass cleanly without errors:

```console
$ gofmt -l .
(empty output - properly formatted)

$ go vet ./...
(exit code 0 - clean)

$ go test ./...
ok   github.com/stellar-optics/stellar-xdr-lens/cmd/lens     0.284s
ok   github.com/stellar-optics/stellar-xdr-lens/internal/cli 1.156s
ok   github.com/stellar-optics/stellar-xdr-lens/pkg/lens     2.342s
ok   github.com/stellar-optics/stellar-xdr-lens/pkg/lens/format 0.312s
```

All 4 acceptance criteria checklists across issues #344, #345, #346, and #347 are 100% fulfilled.
