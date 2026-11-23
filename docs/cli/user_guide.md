# CLI User Guide & Workflow Recipes

`stellar-xdr-lens` (`lens`) is an offline command-line inspection tool and library for Stellar and Soroban XDR. It converts opaque base64 and binary XDR streams into human-readable English explanations, tree visualizations, stable JSON, and structural diffs.

This guide provides an end-to-end walkthrough of every command, practical Unix pipeline recipes, and recommended shell aliases.

---

## Table of Contents

- [Core Principles](#core-principles)
- [Command Reference](#command-reference)
  - [`lens decode`](#lens-decode)
  - [`lens diff`](#lens-diff)
  - [`lens meta-diff`](#lens-meta-diff)
  - [`lens explain`](#lens-explain)
  - [`lens explore` (TUI)](#lens-explore-tui)
  - [`lens types`](#lens-types)
  - [Secondary Subcommands](#secondary-subcommands)
    - [`lens encode`](#lens-encode)
    - [`lens asset`](#lens-asset)
    - [`lens completion`](#lens-completion)
    - [`lens version`](#lens-version)
- [Pipeline Recipes](#pipeline-recipes)
  - [Recipe 1: Fetch and Explain Failed Transactions from Horizon](#recipe-1-fetch-and-explain-failed-transactions-from-horizon)
  - [Recipe 2: Stream and Filter Soroban RPC Events with `jq`](#recipe-2-stream-and-filter-soroban-rpc-events-with-jq)
  - [Recipe 3: Batch Verify and Audit Ledger Close Batches](#recipe-3-batch-verify-and-audit-ledger-close-batches)
  - [Recipe 4: Automated CI Pull Request Diff Summaries](#recipe-4-automated-ci-pull-request-diff-summaries)
- [Productivity Shell Aliases & Functions](#productivity-shell-aliases--functions)

---

## Core Principles

1. **100% Offline & Deterministic:** `lens` never connects to the network. It requires no RPC or Horizon node to decode, diff, or explain payloads.
2. **Safe for Unsigned Data:** Safe to run on untrusted or unpublished pre-signed transactions, keys, and private contract states.
3. **Unix Pipeline Friendly:** Accepts inputs from arguments, flags (`-f/--file`), or standard input (`stdin`). Exits with strict, pinned status codes.

---

## Command Reference

### `lens decode`

Decodes arbitrary base64 or hexadecimal XDR into structured representations.

```console
$ lens decode [base64-payload] [flags]
```

#### Common Flags

| Flag | Short | Description |
|---|---|---|
| `--json` | | Emit stable, machine-readable JSON representation. |
| `--compact` | | Emit single-line compact JSON (with `--json`). |
| `--yaml` | | Emit formatted YAML. |
| `--format` | | Format name: `tree`, `json`, `summary`, `txrep`, `csv`, `html`, `mermaid`, `junit`. |
| `--type` | `-t` | Explicit XDR type (skips type guessing). See `lens types`. |
| `--depth` | | Limit tree recursion depth (0 = unlimited). |
| `--show-types` | | Annotate each node with its concrete XDR type. |
| `--select`, `--query`| | Query and extract subtrees by dotted path (e.g. `Tx.Fee`, `Signatures[0]`). |
| `--raw` | | Output raw bytes or base64 of the selected node (used with `--select`). |
| `--raw-format` | | Format for raw output: `auto`, `base64`, `hex`, `raw`. |
| `--hex` | | Decode input from hex instead of base64. |
| `--multi` | | Decode newline-delimited stream of XDR payloads. |
| `--stream` | | Stream RFC 5531 or length-prefixed chunked frames. |
| `--spec` | | Path to contract WASM or `ScSpecEntry` file for spec-aware contract call decoding. |
| `--wasm` | | Extract and inspect embedded contract WebAssembly bytecode. |
| `--account` | | Filter tree nodes involving an account strkey (`G...`). |
| `--op-type` | | Filter operations by type (e.g. `payment`, `invoke_host_function`). |
| `--canonical` | | Enforce strict canonical XDR encoding standards. |
| `--validate-only` | | Check syntax only: skip tree building, enrichment, and formatting. |

#### Walkthrough Examples

**1. Hierarchical Tree Inspection:**
```console
$ lens decode "AAAAAgAAAADKZ..."
```
Displays indented tree with colorized keys, values, and human notes (e.g., stroop conversions and strkey account addresses).

**2. Limiting Tree Depth:**
```console
$ lens decode --depth 2 --file tx.xdr
```

**3. Extracting Specific Nested Fields with `--select`:**
```console
$ lens decode --select "Tx.Fee" "$ENVELOPE_XDR"
100

$ lens decode --select "Tx.Operations[0].Body.PaymentOp.Amount" --raw "$ENVELOPE_XDR"
10000000
```

**4. Emitting SEP-0011 TxRep Format:**
```console
$ lens decode --txrep --file tx.xdr
```

**5. Decoding Contract Calls with WASM Spec:**
```console
$ lens decode --spec ./target/contract.wasm "$INVOKE_HOST_FUNCTION_XDR"
```

**6. Validating Ingestion Payloads Without Building a Tree:**
```console
$ lens decode --validate-only "$XDR"
valid XDR: TransactionEnvelope (1548 bytes)

# Silent gate for shell pipelines and CI: no stdout, exit code only.
$ lens decode --validate-only -q "$XDR" || exit 1
```
`--validate-only` skips the `Node` tree, enrichers, and formatters. Malformed
input reports the failing stage and byte offset, for example
`invalid XDR (TransactionEnvelope) at byte offset 40 of 40: truncated payload`.

---

### `lens diff`

Structurally compares two XDR structures, isolating exact modified paths, additions, removals, and semantic deltas.

```console
$ lens diff <operand-a> <operand-b> [flags]
```

Operands can be base64 strings, file paths, or `-` for `stdin`.

#### Common Flags

| Flag | Description |
|---|---|
| `--format` | Output format: `text`, `json`, `json-patch`, `unified`, `side-by-side`, `stats`, `summary`. |
| `--unified` | Unified diff format (like `diff -u`). |
| `--side-by-side`| Two-column terminal diff. |
| `--stats` | Numeric breakdown of changes (additions, deletions, modifications). |
| `--summary` | Compact single-line summary of differences. |
| `--mask-paths`, `--mask-path` | Exclude paths from diff comparison entirely (prevents false positive exits). |
| `--exclude-paths`, `--ignore-path` | Suppress changes from output display. |
| `--diff-threshold` | Numerical tolerance: suppress numeric differences below delta. |
| `--align` | Longest-common-subsequence list alignment to prevent offset shifting. |
| `--semantic` | Semantic equality for ScVal integers and map keys. |
| `--unordered-signers` | Multiset comparison for signatures/signers (order agnostic). |
| `--footprint` | Set comparison of Soroban ledger footprints. |
| `--exit-code` | Exit code 2 if differences exist, 0 if identical. |

#### Walkthrough Examples

**1. Standard Structural Diff:**
```console
$ lens diff original_tx.xdr updated_tx.xdr
~ V1.Tx.Fee: 100 -> 200
+ V1.Tx.Operations[1] = payment to G...
```

**2. Side-by-Side Visual Comparison:**
```console
$ lens diff --side-by-side original_tx.xdr updated_tx.xdr
```

**3. Masking Volatile Fields in Automated Checks:**
```console
$ lens diff --mask-path "*.SeqNum,*.Signatures" candidate.xdr expected.xdr --exit-code
```

---

### `lens meta-diff`

Summarizes net account and trustline balance deltas computed across transaction execution metadata (`TransactionMeta` versions 0 through 4).

```console
$ lens meta-diff <meta-base64-or-path> [flags]
```

#### Common Flags

| Flag | Description |
|---|---|
| `--json` | Emit balance deltas as machine-readable JSON array. |

#### Walkthrough Examples

**1. Inspecting Balance Changes:**
```console
$ lens meta-diff meta.xdr
HOLDER                                                  ASSET   BEFORE          AFTER           DELTA
GBBD...54NM                                             native  10.0000000      9.9999900       -0.0000100
GCS2...LK29                                             native  0.0000000       5.0000000       +5.0000000
```

**2. Machine-Readable Balance Output for Audits:**
```console
$ lens meta-diff --json meta.xdr | jq '.changes[] | select(.asset == "native")'
```

---

### `lens explain`

Explains transactions, result codes, and execution failures in plain, actionable English.

```console
$ lens explain <envelope> [flags]
```

#### Common Flags

| Flag | Description |
|---|---|
| `--result`, `--result-file` | Accompanied `TransactionResult` payload to identify exact failures. |
| `--meta`, `--meta-file` | Accompanied `TransactionMeta` payload for diagnostic event correlation. |
| `--isolate` | Pinpoint and output only the single failing operation index. |
| `--resources` | Soroban resource analysis (CPU instructions, memory, read/write bytes vs limits). |
| `--check-expiry` | Verify `TimeBounds` and `LedgerBounds` validity against current time or reference. |
| `--reference-time` | Reference timestamp (RFC 3339 or Unix seconds) for expiration verification. |
| `--fail-on-error` | Exit with code 2 if the transaction failed. |
| `--markdown` | Emit GitHub-flavored Markdown tables. |
| `--json` | Emit diagnostic report as structured JSON. |
| `--verbose`, `-v` | Include protocol constants alongside explanations. |

#### Walkthrough Examples

**1. Explaining a Transaction Failure:**
```console
$ lens explain --file envelope.xdr --result-file result.xdr
Transaction failed at operation 0, payment: The destination account does not exist.
Hint: To send an asset to an account that has not been created on the ledger, use CreateAccount instead of Payment.
```

**2. Isolating Failing Operation Index for Automated CI Alerts:**
```console
$ lens explain "$ENV_XDR" --result "$RES_XDR" --isolate
Operation 2 failed: op_no_trust
```

**3. Markdown Report for PR Reviews or Audits:**
```console
$ lens explain "$ENV_XDR" --result "$RES_XDR" --markdown > failure_report.md
```

---

### `lens explore` (TUI)

Navigates complex, nested XDR payloads interactively in a fullscreen terminal tree viewer.

```console
$ lens explore <base64-or-path> [flags]
$ lens tui <base64-or-path>
```

#### Keybindings

| Key | Action |
|---|---|
| `↑` / `k` | Move cursor up. |
| `↓` / `j` | Move cursor down. |
| `Enter` / `Space` | Toggle expansion of selected node. |
| `e` | Expand all nodes. |
| `c` | Collapse all nodes. |
| `1`–`9` | Switch to a numbered bundle view. |
| `Tab` / `Shift-Tab` | Cycle forward / backward through bundle views. |
| `?` | Toggle the keybinding help modal. |
| `q` / `Ctrl+C` | Quit viewer. |

When a payload bundles an envelope, result, and metadata (for example a
`TransactionResultPair`), the viewer opens numbered tabs
(`[ 1: Envelope ] [ 2: Result ] [ 3: Meta ]`). Each tab keeps its own scroll
position, cursor, and expansion state, so switching views does not lose place.
Payloads that are a single structure keep the plain tree layout with no tab bar.

---

### `lens types`

Lists all registered XDR types, category classifications, and canonical wire byte sizes.

```console
$ lens types [flags]
```

#### Output Example

```console
$ lens types
TYPE                          CATEGORY         MIN SIZE (BYTES)
TransactionEnvelope           Transaction      44
TransactionResult             Transaction      28
LedgerCloseMeta               Ledger           64
LedgerEntry                   State            36
ScVal                         Soroban          4
```

---

### Secondary Subcommands

#### `lens encode`
Constructs canonical XDR base64 from simplified JSON documents.
```console
# Encode ScVal integer
$ echo '{"type":"i32","value":42}' | lens encode --scval

# Encode TransactionEnvelope with schema validation
$ lens encode --tx --strict -f tx.json
```

#### `lens convert`
Converts payloads between base64, hex, and raw binary without unmarshalling the
XDR, so it accepts arbitrary bytes as well as valid XDR.
```console
$ lens convert "$XDR"                     # base64 -> hex (default)
$ lens convert --from hex --to base64 "$HEX"
$ lens convert --hex "$XDR"                # shortcut for --to hex
$ lens convert --base64 "$HEX"             # shortcut for --to base64
$ cat payload.bin | lens convert --from raw --hex
```
Input comes from the arguments, `--file`, or stdin. Whitespace and a `0x`
prefix in hex input are tolerated. Malformed base64 or hex is reported with its
line, column, and byte offset, and exits with the invalid-XDR status code (3).

#### `lens asset`
Derives Soroban SAC (Stellar Asset Contract) contract IDs from classic asset identifiers across networks.
```console
# Derive USDC SAC contract address on Mainnet
$ lens asset "USDC:GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN"

# Derive for Testnet
$ lens asset --network testnet "native"
```

#### `lens completion`
Generates shell autocompletion scripts for `bash`, `zsh`, `fish`, or `powershell`.
```console
# Bash
$ source <(lens completion bash)

# Zsh
$ source <(lens completion zsh)
```

#### `lens version`
Displays semantic version and Git build metadata.
```console
$ lens version
```

---

## Pipeline Recipes

### Recipe 1: Fetch and Explain Failed Transactions from Horizon

When debugging failed production transactions on Mainnet or Testnet:

```bash
TX_HASH="c4b4..."
HORIZON="https://horizon.stellar.org"

# Fetch transaction envelope and result, then explain
RESPONSE=$(curl -sS "${HORIZON}/transactions/${TX_HASH}")
ENV=$(echo "$RESPONSE" | jq -r .envelope_xdr)
RES=$(echo "$RESPONSE" | jq -r .result_xdr)

lens explain "$ENV" --result "$RES" --verbose
```

### Recipe 2: Stream and Filter Soroban RPC Events with `jq`

Filter and decode specific Soroban contract events from Soroban RPC:

```bash
RPC_URL="https://mainnet.sorobanrpc.org"

# Query events from recent ledger
curl -sS -X POST "$RPC_URL" \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "getEvents",
    "params": {
      "startLedger": 52000000,
      "filters": [{
        "type": "contract"
      }]
    }
  }' \
  | jq -r '.result.events[]?.value' \
  | while read -r event_xdr; do
      [ -n "$event_xdr" ] && lens decode --type ScVal "$event_xdr" --json --compact
    done
```

### Recipe 3: Batch Verify and Audit Ledger Close Batches

Inspect all ledgers in a captured test directory:

```bash
# Decode every fixture and report summary
lens decode --batch "./testdata/*.xdr" --format summary
```

### Recipe 4: Automated CI Pull Request Diff Summaries

Verify deterministic envelope generation in a continuous integration workflow:

```bash
lens diff expected.xdr actual.xdr \
  --mask-path "V1.Tx.SeqNum,V1.Signatures**" \
  --format summary \
  --exit-code
```

---

## Productivity Shell Aliases & Functions

Add these to your `~/.bashrc` or `~/.zshrc`:

```bash
# Quick aliases
alias xdr-decode="lens decode"
alias xdr-json="lens decode --json"
alias xdr-explain="lens explain"
alias xdr-diff="lens diff"
alias xdr-meta="lens meta-diff"
alias xdr-tui="lens explore"

# Quick Horizon Transaction Explainer
xdr-fetch-tx() {
  local hash="$1"
  local network="${2:-public}"
  local url="https://horizon.stellar.org"
  if [ "$network" = "testnet" ]; then
    url="https://horizon-testnet.stellar.org"
  fi

  local resp
  resp=$(curl -sS "${url}/transactions/${hash}")
  local env
  env=$(echo "$resp" | jq -r .envelope_xdr)
  local res
  res=$(echo "$resp" | jq -r .result_xdr)

  if [ "$env" = "null" ] || [ -z "$env" ]; then
    echo "Transaction not found on Horizon (${network})." >&2
    return 1
  fi

  lens explain "$env" --result "$res"
}

# Quick ScVal Decoder
xdr-scval() {
  lens decode --type ScVal "$1"
}
```
