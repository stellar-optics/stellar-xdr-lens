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

All 208 protocol result codes have a written explanation. Use `--fail-on-error`
to exit non-zero when the transaction failed, which makes this usable as a CI gate.

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

Use `--exit-code` to exit non-zero when the values differ, like `diff(1)`.

### Piping and scripting

`lens` reads from an argument, a `--file`, or stdin, so it composes:

```sh
# Explain a live transaction straight from Horizon
curl -s "https://horizon.stellar.org/transactions/$HASH" \
  | jq -r '.envelope_xdr' \
  | lens explain

# Pull one field out with jq
lens decode --json "$XDR" | jq -r '.value.V1.Tx.Fee'

# Gate a deployment on the transaction having succeeded
lens explain "$ENV" --result "$RES" --fail-on-error || exit 1
```

The `--json` output has a documented, stable shape:

```json
{
  "type": "TransactionResult",
  "value": { "FeeCharged": 13355, "Result": { "Code": "TransactionResultCodeTxFeeBumpInnerFailed" } }
}
```

Absent union arms are omitted rather than emitted as `null`, so `jq` paths stay short.

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

See the [API reference](https://pkg.go.dev/github.com/stellar-optics/stellar-xdr-lens/pkg/lens)
and [docs/architecture.md](docs/architecture.md).

## A note on type detection

`lens decode` detects the XDR type by trying each known type and keeping the ones
that decode cleanly, relying on the SDK rejecting input it does not fully consume.
On transaction-sized payloads this is reliable — the test suite asserts that every
real fixture matches exactly one type.

Short values are genuinely ambiguous, though. An empty `Memo` and a native `Asset`
are both `AAAAAA==`, because they are the same four bytes. `lens` reports this
rather than guessing silently:

```console
$ lens decode --candidates "AAAAAA=="
* Memo
  Asset

2 types matched; the starred one is used by default. Use --type to choose.
```

Pass `--type` when you need certainty.

## Project status

**Early but real.** v0.1 does what this README says, with tests over real captured
mainnet XDR. The library API may still shift before v1.0 — it is not yet frozen.

- Supports 25 XDR types out of the ~400 in the protocol, chosen to cover what
  developers actually paste into a terminal. Adding more is a one-line change;
  see [docs/architecture.md](docs/architecture.md).
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
