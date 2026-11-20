# Semantic Diffing & Masking Volatile Fields in CI

In continuous integration and protocol regression testing, developers compare transaction envelopes, metadata, and smart contract ledger footprints against expected fixtures. However, naive byte-by-byte or unmasked diffs frequently fail due to **volatile fields**: sequence numbers, dynamic transaction fees, timebounds, nonces, and cryptographic signatures.

`stellar-xdr-lens` provides structural and semantic diffing via `lens diff`, allowing pipelines to mask dynamic fields, apply numeric tolerances, and evaluate semantic equivalence with precise exit code semantics.

---

## Table of Contents

- [The Volatile Field Problem](#the-volatile-field-problem)
- [Masking vs. Excluding Paths](#masking-vs-excluding-paths)
- [Path Syntax & Globbing Rules](#path-syntax--globbing-rules)
  - [Exact Paths](#exact-paths)
  - [Glob & Wildcard Syntax](#glob--wildcard-syntax)
  - [Common Masking Recipes](#common-masking-recipes)
- [Numerical Tolerance with `--diff-threshold`](#numerical-tolerance-with---diff-threshold)
- [Semantic & Multiset Comparisons](#semantic--multiset-comparisons)
- [Process Exit Code Semantics](#process-exit-code-semantics)
- [CI Pipeline Integration Recipes](#ci-pipeline-integration-recipes)
  - [GitHub Actions Workflow Template](#github-actions-workflow-template)
  - [GitLab CI/CD Pipeline Template](#gitlab-cicd-pipeline-template)
- [Best Practices in CI](#best-practices-in-ci)

---

## The Volatile Field Problem

A transaction envelope built during automated tests rarely matches a recorded fixture bit-for-bit:

1. **Sequence Numbers (`SeqNum`):** Source account sequence increments with every transaction.
2. **Cryptographic Signatures (`Signatures`):** Ed25519 signatures or ECDSA signatures change when envelope contents differ, or can contain non-deterministic k-values.
3. **Preconditions / Timebounds (`TimeBounds`, `LedgerBounds`):** Validity windows change between test executions.
4. **Soroban Auth Nonces (`Nonce`):** Contract authorization invocations embed monotonically increasing nonces.
5. **Dynamic Base Fees (`Fee`):** Network surge pricing fluctuates fees.

Without semantic masking, regression tests must either mock every volatile field or suffer from intermittent false positives.

---

## Masking vs. Excluding Paths

`lens diff` makes a fundamental architectural distinction between **masking** and **excluding**:

| Feature | Flag | Purpose | Impact on Exit Code |
|---|---|---|---|
| **Mask Paths** | `--mask-path`, `--mask-paths` | Excludes fields from diff evaluation entirely. | **Does NOT trigger exit code 2.** If only masked fields differ, the diff returns 0 (Equal). |
| **Exclude Paths** | `--exclude-paths`, `--ignore-path`, `--ignore-paths` | Suppresses matching paths from formatted output. | **Still triggers exit code 2.** The comparison detects a difference, but hides it from output display. |

> [!IMPORTANT]
> In automated CI gates where `diff --exit-code` decides if a test passed, **always use `--mask-path`**. Using `--ignore-path` will suppress the lines in your terminal but the process will still exit with status 2!

---

## Path Syntax & Globbing Rules

A path locates an element within the decoded XDR node hierarchy. Segments are separated by dots (`.`), and array/slice indices are enclosed in brackets (`[index]`).

### Exact Paths

- `V1.Tx.SeqNum`: The sequence number of a V1 transaction envelope.
- `V1.Tx.Fee`: The fee in stroops.
- `V1.Tx.Signatures[0].Signature`: The 64-byte signature payload of the first signer.
- `V1.Tx.Operations[0].Body.PaymentOp.Amount`: The payment amount of the first operation.

### Glob & Wildcard Syntax

`lens diff` supports shell-style globbing:

- `*`: Matches any characters within a single path segment.
  - `V1.Tx.*`: Matches all direct children of `V1.Tx` (`Fee`, `SeqNum`, `SourceAccount`, etc.).
  - `*.*_time`: Matches `min_time` and `max_time`.
- `?`: Matches exactly one character.
- `**`: Recursive wildcard matching zero or more path segments.
  - `**.Signatures`: Matches signatures at any depth.
  - `V1.Signatures**`: Matches all signatures and inner fields inside `V1`.
- `[n]`: Matches specific index in lists.
- `[*]`: Matches every element in a list.
  - `V1.Tx.Operations[*].SourceAccount`: Matches source accounts of all operations.

### Common Masking Recipes

```bash
# Ignore sequence numbers across any envelope version
--mask-path "*.SeqNum"

# Ignore all signatures and hints
--mask-path "*.Signatures[*],*.Signatures"

# Ignore fees and timebounds
--mask-path "*.Fee,*.*Time*,*.TimeBounds**"

# Comprehensive recipe for deterministic transaction verification
--mask-path "*.SeqNum,*.Signatures**,*.TimeBounds**,*.LedgerBounds**"
```

Multiple paths can be passed as comma-separated values or by repeating the flag:

```console
$ lens diff a.xdr b.xdr \
    --mask-path "*.SeqNum" \
    --mask-path "*.Signatures**" \
    --diff-threshold 50
```

---

## Numerical Tolerance with `--diff-threshold`

Dynamic fee fluctuations or floating-point conversions (such as DEX offer fractions or gas estimations) often require a numerical tolerance margin.

Pass `--diff-threshold <delta>` to ignore scalar differences where `|after - before| <= delta`:

```console
# Ignore fee variations up to 500 stroops
$ lens diff candidate.xdr fixture.xdr --diff-threshold 500 --exit-code
```

- When the difference between the two scalar values is less than or equal to `500`, `lens diff` records no difference for that field.
- Non-numeric fields (e.g. string addresses, public keys) are unaffected by `--diff-threshold` and are evaluated strictly.

---

## Semantic & Multiset Comparisons

Beyond path masking, `lens diff` supports two high-level semantic modes:

1. **`--semantic`:**
   - Evaluates Soroban `ScVal` integers (such as `U128`, `I128`, `U256`, `I256`) as exact mathematical numbers rather than comparing raw slice byte offsets.
   - Pairs `ScVal` map keys canonically rather than positionally.
2. **`--unordered-signers`:**
   - Treats transaction signers and signatures as unordered multisets. If two transactions have identical signatures in different order, `--unordered-signers` reports zero differences.
3. **`--align`:**
   - Uses the Longest Common Subsequence (LCS) algorithm to align array elements. If an operation is inserted at index 0, `--align` reports a single operation addition rather than reporting every subsequent operation as modified.

---

## Process Exit Code Semantics

`lens` guarantees stable exit codes suitable for shell scripting and CI gates:

| Code | Status | Meaning |
|---|---|---|
| `0` | Clean | The command succeeded and the values are identical (or equal after masking/thresholds). |
| `1` | Invocation Error | Generic CLI failure: missing arguments, invalid flags, or unreadable files. |
| `2` | Diff Detected | Comparison succeeded, but structural differences were found. |
| `3` | Invalid XDR | One or both payloads could not be decoded as valid XDR. |

When used with `--exit-code` or `--quiet`:
```bash
lens diff a.xdr b.xdr --mask-path "*.SeqNum" --exit-code
STATUS=$?

if [ $STATUS -eq 0 ]; then
  echo "Envelopes match semantically."
elif [ $STATUS -eq 2 ]; then
  echo "Envelopes differ structurally!"
elif [ $STATUS -eq 3 ]; then
  echo "Corrupted XDR payload!"
fi
```

---

## CI Pipeline Integration Recipes

### GitHub Actions Workflow Template

Create `.github/workflows/xdr-diff.yml`:

```yaml
name: XDR Semantic Verification

on:
  pull_request:
    branches: [main]

jobs:
  verify-xdr:
    name: Verify Transaction Determinism
    runs-on: ubuntu-latest
    steps:
      - name: Checkout Repository
        uses: actions/checkout@v4

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.25'

      - name: Install lens
        run: go install github.com/stellar-optics/stellar-xdr-lens/cmd/lens@latest

      - name: Generate Candidate XDR
        run: |
          # Generate your transaction in test suite
          go test -run TestGenerateContractEnvelope -v ./...
          # Assume candidate written to ./build/generated.xdr

      - name: Perform Semantic Diff
        id: diff
        continue-on-error: true
        run: |
          lens diff fixtures/expected.xdr build/generated.xdr \
            --mask-path "*.SeqNum,*.Signatures**,*.TimeBounds**" \
            --diff-threshold 100 \
            --format summary \
            --exit-code > diff_summary.txt
          echo "EXIT_CODE=$?" >> $GITHUB_OUTPUT

      - name: Post PR Review Comment on Failure
        if: steps.diff.outputs.EXIT_CODE == '2'
        uses: actions/github-script@v7
        with:
          script: |
            const fs = require('fs');
            const summary = fs.readFileSync('diff_summary.txt', 'utf8');
            github.rest.issues.createComment({
              issue_number: context.issue.number,
              owner: context.repo.owner,
              repo: context.repo.repo,
              body: `### ❌ XDR Regression Detected\n\n\`\`\`\n${summary}\n\`\`\`\n\nPlease check generated envelope deterministic outputs.`
            });

      - name: Enforce Diff Exit Code
        run: |
          if [ "${{ steps.diff.outputs.EXIT_CODE }}" != "0" ]; then
            echo "Semantic diff failed with exit code ${{ steps.diff.outputs.EXIT_CODE }}"
            exit 1
          fi
```

### GitLab CI/CD Pipeline Template

Create `.gitlab-ci.yml`:

```yaml
stages:
  - test
  - verify

xdr_semantic_diff:
  stage: verify
  image: golang:1.25
  before_script:
    - go install github.com/stellar-optics/stellar-xdr-lens/cmd/lens@latest
  script:
    - |
      set +e
      lens diff ./fixtures/expected.xdr ./build/generated.xdr \
        --mask-path "*.SeqNum,*.Signatures**" \
        --diff-threshold 50 \
        --unified \
        --exit-code > diff.patch
      EXIT_CODE=$?
      set -e

      if [ $EXIT_CODE -eq 0 ]; then
        echo "✅ Transactions match expectations."
        exit 0
      elif [ $EXIT_CODE -eq 2 ]; then
        echo "❌ XDR Difference detected:"
        cat diff.patch
        exit 2
      else
        echo "🚨 CLI or XDR parsing error (code $EXIT_CODE)"
        exit 1
      fi
  artifacts:
    when: on_failure
    paths:
      - diff.patch
    expire_in: 1 week
```

---

## Best Practices in CI

1. **Always mask `*.SeqNum` and `*.Signatures**`:** Sequence numbers and signatures are non-deterministic across environments; masking them prevents spurious pipeline breaks.
2. **Use `--format summary` or `--unified`:** Summary mode provides compact CI log output, while unified mode produces clean patch artifacts for download.
3. **Capture Exit Code `3`:** Distinguish corrupt payloads (exit code 3) from structural regressions (exit code 2) in your pipeline notification alerts.
4. **Combine with `--semantic` for Smart Contracts:** Soroban contract testing benefits heavily from comparing big integers by numerical value rather than XDR byte representation.
