# Decoding & Verifying Soroban Contract Specifications

When developing, auditing, or debugging Soroban smart contracts on the Stellar network, raw XDR representations of contract invocations, return values, and contract events are expressed as generic `ScVal` data trees (such as untyped vectors, maps, symbols, and integers). Without the contract's interface specification, deciphering function arguments and event topics requires manual reconstruction.

`stellar-xdr-lens` supports spec-aware decoding via `lens decode --spec`, which extracts the embedded `contractspecv0` section from contract WASM files (or reads pre-exported `ScSpecEntry` streams) to annotate invocations, return values, user events, and error codes with full parameter names and typed structures.

---

## Table of Contents

- [The Need for Contract Specifications](#the-need-for-contract-specifications)
- [How Spec Extraction Works (`contractspecv0`)](#how-spec-extraction-works-contractspecv0)
- [Using `lens decode --spec`](#using-lens-decode---spec)
  - [With Contract WASM Binary](#with-contract-wasm-binary)
  - [With Pre-Extracted Specification Data](#with-pre-extracted-specification-data)
- [Side-by-Side Comparison: Raw vs. Spec-Decoded](#side-by-side-comparison-raw-vs-spec-decoded)
  - [Example: Soroban Token Transfer](#example-soroban-token-transfer)
- [Decoding Contract Events & User Errors](#decoding-contract-events--user-errors)
  - [Contract Events](#contract-events)
  - [Custom Error Enums](#custom-error-enums)
- [AMM Liquidity Pool Walkthrough](#amm-liquidity-pool-walkthrough)
- [Summary of Spec Flags](#summary-of-spec-flags)

---

## The Need for Contract Specifications

In Soroban, all contract calls are transmitted via `InvokeHostFunctionOp` carrying:
1. `ContractAddress`: The target contract's strkey (e.g. `CA3D...`).
2. `FunctionName`: An `ScSymbol` (e.g. `transfer`, `swap`).
3. `Args`: A generic slice of `xdr.ScVal`.

Without a specification, a call to a DEX contract looks like:
```text
args: [
  ScVal{Type: Vec, Value: [...]},
  ScVal{Type: U128, Value: 1000000000},
  ScVal{Type: U32, Value: 300}
]
```
With the contract specification loaded, `lens` parses argument names, applies custom structs and enums, and renders:
```json
{
  "function": "swap",
  "args": {
    "path": ["USDC:GA...", "XLM:native"],
    "amount_in": "100.0000000",
    "min_amount_out": "298.5000000"
  }
}
```

---

## How Spec Extraction Works (`contractspecv0`)

When a Soroban Rust contract is compiled with the `soroban-sdk`, the compiler embeds a WebAssembly custom section named `contractspecv0`.

In `pkg/lens/spec_decode.go`, `lens` inspects the WASM bytecode:
1. Validates the 4-byte WASM magic header (`\x00asm`) and version.
2. Iterates across custom sections until section `contractspecv0` is identified.
3. Unmarshals the payload into a stream of `xdr.ScSpecEntry` objects:
   - Functions (`ScSpecFunctionV0`): parameter names, types, and docstrings.
   - User-Defined Types (`ScSpecUdtStructV0`, `ScSpecUdtUnionV0`, `ScSpecUdtEnumV0`).
   - Custom Errors (`ScSpecUdtErrorEnumV0`): numeric codes mapped to names.
   - Events (`ScSpecEventV0`): topic and data schemas.

---

## Using `lens decode --spec`

### With Contract WASM Binary

Pass the path to your compiled contract `.wasm` binary:

```console
$ lens decode --spec ./target/wasm32-unknown-unknown/release/token.wasm "$INVOKE_HOST_FUNCTION_XDR"
```

### With Pre-Extracted Specification Data

If you have pre-extracted the specification entries (e.g. from Horizon, Soroban RPC, or `stellar contract inspect`):

```console
$ lens decode --spec token.spec.xdr "$INVOKE_HOST_FUNCTION_XDR"
```

`lens` automatically detects whether `--spec` points to a binary WASM file or raw XDR spec bytes.

---

## Side-by-Side Comparison: Raw vs. Spec-Decoded

### Example: Soroban Token Transfer

Consider a standard token transfer on Mainnet.

#### 1. Raw XDR (`lens decode "$XDR"`)

```console
$ lens decode "$ENVELOPE_XDR" --select "Tx.Operations[0].Body.InvokeHostFunctionOp"
InvokeHostFunctionOp
  HostFunction: InvokeContract
    ContractAddress: CBGSB7XNPHNRO6CY3C5X5SMR7X57I4O6Q7EAY2N3Z336V45Q7Y6J3B2
    FunctionName: transfer
    Args (3)
      [0] Address: GBY77V7Z7...
      [1] Address: GDF77A8Z1...
      [2] I128: 500000000 (lo: 500000000, hi: 0)
```

#### 2. Spec-Aware Decoded (`lens decode --spec token.wasm "$XDR"`)

```console
$ lens decode --spec token.wasm "$ENVELOPE_XDR" --json
{
  "contract": "CBGSB7XNPHNRO6CY3C5X5SMR7X57I4O6Q7EAY2N3Z336V45Q7Y6J3B2",
  "function": "transfer",
  "doc": "Transfer tokens from sender to recipient with balance checks.",
  "parameters": {
    "from": "GBY77V7Z7...",
    "to": "GDF77A8Z1...",
    "amount": 500000000
  }
}
```

Notice that generic `[0]`, `[1]`, `[2]` list items are mapped directly to their schema names: `from`, `to`, and `amount`.

---

## Decoding Contract Events & User Errors

### Contract Events

When transactions execute, contracts emit diagnostic and contract events into `TransactionMeta`.

In raw XDR, event topics and data are unstructured arrays of `ScVal`. Using `--spec`, `lens` resolves event names and field attributes:

```console
$ lens decode --spec amm.wasm "$META_XDR" --select "Events[0]"
ContractEvent
  Contract: CC3X...
  Type: Contract
  Name: "swap"
  Topics:
    user: GAIZ...
    pool: CP77...
  Data:
    amount_in: 10000000
    amount_out: 9950000
```

### Custom Error Enums

When a Soroban contract traps with a custom error code (e.g. `Error(Contract, #1)`), `lens` references `ErrorEnums` in the spec:

```console
$ lens explain "$ENVELOPE" --result "$RESULT" --spec token.wasm
Transaction failed at operation 0, invoke_host_function:
Contract trapped with error 1: InsufficientAllowance
Hint: The spender attempted to transfer more tokens than currently authorized by the owner.
```

---

## AMM Liquidity Pool Walkthrough

Suppose you are auditing an Automated Market Maker (AMM) contract invocation:

```console
$ lens decode --spec amm.wasm "$SWAP_XDR"
```

Output:
```text
InvokeContract
  Contract: CDEX... (ConstantProductAMM)
  Function: deposit_liquidity
    desired_a: 500.0000000 USDC
    min_a:     495.0000000 USDC
    desired_b: 2500.0000000 XLM
    min_b:     2480.0000000 XLM
```

---

## Summary of Spec Flags

| Flag | Purpose | Example |
|---|---|---|
| `--spec <file>` | Path to `.wasm` contract or `.spec.xdr` specification file. | `lens decode --spec token.wasm "$XDR"` |
| `--wasm` | Extracts embedded WASM bytecode from `ContractCodeEntry`. | `lens decode --wasm code_entry.xdr` |
| `--json` | Emits spec-decoded arguments in structured JSON. | `lens decode --spec token.wasm --json "$XDR"` |
| `--interactive` | Navigates spec-decoded fields interactively in terminal TUI. | `lens explore --spec token.wasm "$XDR"` |
