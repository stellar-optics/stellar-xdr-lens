# Pull Request: XDR Type Detection Architecture, Soroban Spec Decoding, Failure Diagnosis, and JSON Schema Specification

### Closes / Resolves Issues
- Closes #340: `docs: comprehensive architecture guide for XDR type detection heuristics and scoring algorithm`
- Closes #341: `docs: author tutorial on decoding and verifying Soroban contract specifications`
- Closes #342: `docs: author guide on debugging failed transactions with lens explain and failure isolator`
- Closes #343: `docs: document JSON-to-XDR schema format and encoding specification`

---

## Overview & Motivation

`stellar-xdr-lens` provides powerful offline utilities for decoding, explaining, and encoding Stellar and Soroban XDR. However, four vital system components lacked dedicated documentation, schema references, and developer tutorials:
1. **Detection Heuristics Engine (#340):** The scoring model, byte consumption rules, wire-size filters, and ambiguity resolution algorithms in `pkg/lens/decode.go` and `pkg/lens/registry_index.go` were undocumented for contributors registering new protocol types.
2. **Soroban Contract Specifications (#341):** Developers inspecting opaque `ScVal` contract calls, events, and error codes lacked a tutorial on using `lens decode --spec` to extract embedded `contractspecv0` sections from WASM binaries.
3. **Debugging Failed Transactions (#342):** While `lens explain` decodes all 208 result codes, developers needed an authoritative reference on error hierarchies, failure isolation (`--isolate`), reserve mathematical shortfalls, signer threshold weighting, and Soroban diagnostic event correlation.
4. **JSON-to-XDR Schema Specification (#343):** `lens encode` parses and coerces JSON templates into canonical XDR binary, but lacked a formal schema specification across all supported operation types (`Payment`, `ManageBuyOffer`, `ManageData`, `InvokeHostFunction`, etc.) and validation error models.

This PR provides comprehensive documentation, guides, schema references, and code enhancements resolving all four issues.

---

## Detailed Summary of Changes

### 1. XDR Type Detection Heuristics & Scoring Guide (`#340`)
- Authored `docs/architecture/detection.md` detailing:
  - The multi-stage candidate unmarshaling and detection pipeline with a Mermaid architectural diagram.
  - The scoring model: base priority weighting (`t.priority * 1000`), structural node count term (`countNodes`), strict byte consumption requirements, and small payload damping (`rawLen <= 8`).
  - Ambiguity resolution for wire collisions (4-byte collisions like `Memo` vs `Asset` vs `Auth` vs void `ScVal`; 32-byte hashes vs public keys; bucket entries vs archival entries).
  - Step-by-step contributor walkthrough for adding new XDR types to `decode.go` and `registry_index.go`.
- Cross-referenced in `docs/architecture.md` and `README.md`.

### 2. Decoding & Verifying Soroban Contract Specifications Tutorial (`#341`)
- Authored `docs/guides/soroban_specs.md` detailing:
  - Extraction of the custom `contractspecv0` WASM section from compiled Soroban contracts.
  - Using `lens decode --spec contract.wasm` and passing pre-extracted `.spec.xdr` files.
  - Side-by-side terminal output comparisons demonstrating raw untyped `ScVal` versus named, typed JSON structures.
  - Contract user event schema decoding and custom error enum resolution (`ErrorEnums`).
  - Complete AMM liquidity pool contract walkthrough.
- Cross-referenced in `README.md`.

### 3. Debugging Failed Transactions with `lens explain` Guide (`#342`)
- Authored `docs/guides/debugging_failures.md` detailing:
  - The four-layer hierarchy of Stellar transaction errors (Pre-Operation Checks -> Fee-Bump Wrappers -> Operation Execution -> Soroban Host Function Diagnostics).
  - Deep diagnostic engine features:
    - Operation isolation via `op_isolator.go` (`--isolate`) to pinpoint exact failure indices in bundles.
    - Mathematical balance and reserve shortfall calculations via `balance_explain.go`.
    - Signature weight threshold verification across Low, Medium, and High operation levels via `signature_explain.go`.
    - Host trap correlation with Soroban diagnostic events via `trap_reasons.go`.
    - Precondition timebound expiration verification (`--check-expiry`).
  - Practical cookbook covering common scenarios: `op_underfunded`, `op_bad_auth`, `tx_soroban_invalid`, and `tx_fee_bump_inner_failed`.
- Cross-referenced in `docs/architecture.md` and `README.md`.

### 4. JSON-to-XDR Schema Format & Encoding Specification (`#343`)
- Authored `docs/encode/schema_reference.md` detailing:
  - Formal JSON schema reference for top-level `TransactionEnvelope` and all supported operations: `payment`, `path_payment_strict_receive`, `path_payment_strict_send`, `create_account`, `account_merge`, `manage_buy_offer`, `manage_sell_offer`, `create_passive_sell_offer`, `set_options`, `change_trust`, `set_trust_line_flags`, `manage_data`, and `invoke_host_function`.
  - Type coercion rules in `coerce.go` (asset strings `"native"`, `"CODE:ISSUER"`, strkey addresses, amount-to-stroop math, price fractions, and hex/base64 binary).
  - Validation error models and strict mode enforcement (`--strict`).
- Enhanced `pkg/lens/validate.go`:
  - Expanded `validateOperationDetails` to validate required fields for `manage_buy_offer`, `manage_sell_offer`, `create_passive_sell_offer`, `path_payment_strict_receive`, `path_payment_strict_send`, and `set_trust_line_flags`.
- Added unit tests in `pkg/lens/validate_test.go`.
- Cross-referenced in `README.md`.

---

## Verification & Testing Evidence

All tests pass cleanly without errors:

```console
$ gofmt -l .
(empty output - properly formatted)

$ go vet ./...
(exit code 0 - clean)

$ go test ./...
ok   github.com/stellar-optics/stellar-xdr-lens/cmd/lens     0.312s
ok   github.com/stellar-optics/stellar-xdr-lens/internal/cli 1.045s
ok   github.com/stellar-optics/stellar-xdr-lens/pkg/lens     2.289s
ok   github.com/stellar-optics/stellar-xdr-lens/pkg/lens/format 0.298s
```

All 4 issues (#340, #341, #342, #343) are completely fulfilled and verified.
