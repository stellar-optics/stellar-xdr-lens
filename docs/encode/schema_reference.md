# JSON-to-XDR Schema Reference & Encoding Specification

`stellar-xdr-lens` (`lens encode`) allows developers to construct binary Stellar and Soroban XDR payloads from intuitive, human-readable JSON documents. Rather than requiring low-level manual binary packing or verbose SDK union hierarchies, `lens encode` parses high-level JSON templates, validates them against strict schema definitions, performs type coercion, and emits canonical XDR base64.

This document serves as the formal specification for the JSON schema, supported operations, type coercion rules, and validation error models.

---

## Table of Contents

- [Overview & CLI Usage](#overview--cli-usage)
- [Top-Level Transaction Envelope Schema](#top-level-transaction-envelope-schema)
- [Operation Schemas](#operation-schemas)
  - [1. `payment`](#1-payment)
  - [2. `path_payment_strict_receive`](#2-path_payment_strict_receive)
  - [3. `path_payment_strict_send`](#3-path_payment_strict_send)
  - [4. `create_account`](#4-create_account)
  - [5. `account_merge`](#5-account_merge)
  - [6. `manage_buy_offer`](#6-manage_buy_offer)
  - [7. `manage_sell_offer`](#7-manage_sell_offer)
  - [8. `create_passive_sell_offer`](#8-create_passive_sell_offer)
  - [9. `set_options`](#9-set_options)
  - [10. `change_trust`](#10-change_trust)
  - [11. `set_trust_line_flags`](#11-set_trust_line_flags)
  - [12. `manage_data`](#12-manage_data)
  - [13. `invoke_host_function`](#13-invoke_host_function)
- [Type Coercion Engine (`coerce.go`)](#type-coercion-engine-coercego)
  - [Asset Literals](#asset-literals)
  - [Account & Muxed Addresses](#account--muxed-addresses)
  - [Amounts & Stroop Conversions](#amounts--stroop-conversions)
  - [Prices & Fractions](#prices--fractions)
  - [Binary Data & Hex Strings](#binary-data--hex-strings)
  - [Soroban `ScVal` Types](#soroban-scval-types)
- [Validation Errors & Strict Mode (`validate.go`)](#validation-errors--strict-mode-validatego)
  - [Strict Mode (`--strict`)](#strict-mode---strict)
  - [Error Format](#error-format)

---

## Overview & CLI Usage

Encode a JSON transaction template to canonical base64 XDR:

```console
$ lens encode --tx -f tx.json
AAAAAgAAAADKZ...
```

Encode an isolated `ScVal` literal:
```console
$ echo '{"type":"u32","value":100}' | lens encode --scval
AAAAAwAAAAZkdW5jAA==
```

Flags:
- `--tx`: Shortcut to encode the root JSON as `TransactionEnvelope`.
- `--scval`: Shortcut to encode the root JSON as `ScVal`.
- `--type <type>`: Target specific registered XDR type.
- `--strict`: Enable strict schema enforcement (disallows unknown properties and validates addresses).

---

## Top-Level Transaction Envelope Schema

```json
{
  "source_account": "GAPB7YMUQ6MPQ23PTFDNBZP7HGGTZPI2DT5JJURUOIXNPLGHW2O3WYXZ",
  "fee": 100,
  "sequence_number": 264125967419700139,
  "time_bounds": {
    "min": 1700000000,
    "max": 1700003600
  },
  "ledger_bounds": {
    "min": 50000000,
    "max": 50001000
  },
  "memo": {
    "type": "text",
    "value": "Invoice #4920"
  },
  "operations": [
    {
      "type": "payment",
      "destination": "GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAW",
      "asset": "native",
      "amount": "100.0000000"
    }
  ],
  "signatures": [
    {
      "hint": "4a7b9c12",
      "signature": "d3f2...64bytesHexOrBase64"
    }
  ]
}
```

---

## Operation Schemas

### 1. `payment`
Sends native XLM or a credit asset to a destination account.

```json
{
  "type": "payment",
  "source_account": "G... (optional override)",
  "destination": "GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAW",
  "asset": "USDC:GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN",
  "amount": "50.0000000"
}
```

### 2. `path_payment_strict_receive`
Sends an asset to a destination where the destination receives an exact amount and the sender commits up to a maximum.

```json
{
  "type": "path_payment_strict_receive",
  "destination": "GB...",
  "send_asset": "native",
  "send_max": "10.0000000",
  "dest_asset": "EURT:GC...",
  "dest_amount": "8.5000000",
  "path": ["USDC:GA..."]
}
```

### 3. `path_payment_strict_send`
Sends an exact amount of an asset, receiving at least a minimum of the destination asset.

```json
{
  "type": "path_payment_strict_send",
  "destination": "GB...",
  "send_asset": "native",
  "send_amount": "100.0000000",
  "dest_asset": "USDC:GA...",
  "dest_min": "99.0000000",
  "path": []
}
```

### 4. `create_account`
Funds and creates a new Stellar account.

```json
{
  "type": "create_account",
  "destination": "GC...",
  "starting_balance": "2.5000000"
}
```

### 5. `account_merge`
Merges an account into another, transferring remaining native balance and deleting the source account.

```json
{
  "type": "account_merge",
  "destination": "GC..."
}
```

### 6. `manage_buy_offer`
Creates, updates, or deletes an offer to buy a specific asset amount.

```json
{
  "type": "manage_buy_offer",
  "selling": "native",
  "buying": "USDC:GA...",
  "buy_amount": "100.0000000",
  "price": "0.125",
  "offer_id": 0
}
```

### 7. `manage_sell_offer`
Creates, updates, or deletes an offer to sell a specific asset amount.

```json
{
  "type": "manage_sell_offer",
  "selling": "USDC:GA...",
  "buying": "native",
  "amount": "500.0000000",
  "price": "8.0",
  "offer_id": 149201
}
```

### 8. `create_passive_sell_offer`
Places an offer that does not consume matching offers at the same price immediately.

```json
{
  "type": "create_passive_sell_offer",
  "selling": "USDC:GA...",
  "buying": "native",
  "amount": "100.0000000",
  "price": "7.5"
}
```

### 9. `set_options`
Configures account thresholds, flags, inflation destination, home domain, and signers.

```json
{
  "type": "set_options",
  "master_weight": 10,
  "low_threshold": 10,
  "med_threshold": 20,
  "high_threshold": 30,
  "home_domain": "stellar.org",
  "signer": {
    "key": "G...",
    "weight": 10
  }
}
```

### 10. `change_trust`
Creates, updates, or closes a trustline.

```json
{
  "type": "change_trust",
  "line": "USDC:GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN",
  "limit": "1000000.0000000"
}
```

### 11. `set_trust_line_flags`
Sets or clears flags on an asset trustline.

```json
{
  "type": "set_trust_line_flags",
  "trustor": "GB...",
  "asset": "USDC:GA...",
  "clear_flags": 1,
  "set_flags": 2
}
```

### 12. `manage_data`
Sets or clears a key-value data entry on an account (up to 64 bytes). Strings and base64 payloads are automatically encoded.

```json
{
  "type": "manage_data",
  "data_name": "config_endpoint",
  "data_value": "https://api.example.com/v1"
}
```

To delete a data entry, set `"data_value": null`.

### 13. `invoke_host_function`
Invokes a Soroban smart contract function.

```json
{
  "type": "invoke_host_function",
  "function": "invoke_contract",
  "contract_id": "CBGSB7XNPHNRO6CY3C5X5SMR7X57I4O6Q7EAY2N3Z336V45Q7Y6J3B2",
  "function_name": "swap",
  "args": [
    {"type": "address", "value": "GA2J..."},
    {"type": "i128", "value": 100000000}
  ]
}
```

---

## Type Coercion Engine (`coerce.go`)

`lens encode` uses permissive, developer-friendly type coercion:

### Asset Literals
- `"native"` or `"XLM"`: Decodes to `xdr.AssetTypeAssetTypeNative`.
- `"CODE:ISSUER"` (e.g. `"USDC:GA5Z..."`): Auto-detects 4-character vs 12-character asset alphanumeric codes and validates issuer public key checksums.

### Account & Muxed Addresses
- `G...` standard Ed25519 public keys.
- `M...` SEP-0023 Med25519 multiplexed addresses (extracts inner ID and account ID).
- `C...` contract strkey addresses.

### Amounts & Stroop Conversions
- String amounts with decimals (e.g. `"10.5000000"`) are multiplied by $10^7$ into integer stroops (`105000000`).
- Raw integer stroop counts are accepted directly (e.g. `105000000`).
- Underscores and thousands separators are stripped (`"1_000.50"`).

### Prices & Fractions
- Floating-point strings: `"2.5"` -> fraction $5/2$.
- Fraction strings: `"7/3"` -> fraction $7/3$.
- Structured objects: `{"n": 7, "d": 3}`.

### Binary Data & Hex Strings
- If a string matches `^[0-9a-fA-F]+$`, it is decoded as hex bytes.
- If base64-encoded, it is decoded as standard base64 bytes.
- Plain text strings are converted to UTF-8 byte slices.

### Soroban `ScVal` Types
Primitives can be passed either as tagged objects (`{"type": "u64", "value": 100}`) or inferred directly from JSON primitives (booleans, strings, integers, arrays).

---

## Validation Errors & Strict Mode (`validate.go`)

### Strict Mode (`--strict`)
In strict mode, `pkg/lens/validate.go` enforces:
1. `DisallowUnknown`: Unknown JSON properties trigger a failure.
2. Address verification: Strkey checksums are calculated; invalid checksums fail.
3. Positive amounts: Operations requiring positive values (`amount > 0`) are validated before binary encoding begins.

### Error Format
Validation errors report the exact JSON path and failure reason:

```text
lens: validation error at "operations[0].destination": invalid strkey address checksum: checksum failed
```
Multiple errors are aggregated into `ValidationErrors` for batch correction.
