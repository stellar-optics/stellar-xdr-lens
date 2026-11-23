# Security Guide: Safe Untrusted XDR Parsing

`stellar-xdr-lens` is frequently deployed in microservices, HTTP ingestion endpoints, public RPC proxies, and CI/CD pipelines where it receives untrusted XDR payloads from external callers. 

While the underlying Stellar Go SDK (`github.com/stellar/go-stellar-sdk/xdr`) bounds payload lengths for elementary XDR types, nested structures, adversarial byte vectors, and maliciously crafted payloads must be handled defensively to prevent denial-of-service (CPU and memory exhaustion).

This guide documents the built-in guardrails within `lens` and outlines hardening best practices for embedding the library into untrusted environments.

---

## 1. Built-in Resource Limits & Guardrails

`stellar-xdr-lens` implements multiple defensive layers to isolate untrusted input parsing.

### 1.1 Maximum Recursion Depth (`maxDepth`)

XDR structures (such as nested Soroban `ScVal` tuples, vector trees, or deeply nested claim predicates) can be maliciously crafted to induce stack exhaustion or exponential traversal times.

- **Tree Walker Depth Cap:** `pkg/lens/node.go` enforces a strict recursion depth limit:
  ```go
  const maxDepth = 200
  ```
- When a nested structure exceeds depth 200, recursion terminates immediately, returning a leaf node labeled `<max depth exceeded>`.
- Formatters (e.g. `TreeFormatter`) also provide opt-in output depth bounds via `WithMaxDepth(limit)` or CLI flag `--depth <n>` to constrain terminal and buffer rendering costs.

### 1.2 Maximum Payload Size & Pre-Sized Buffering

- **Streaming Input Limits:** Piped stdin and HTTP stream inputs are processed via `lens.ReadStdinPayload` and `lens.NewStdinPipe`. These use pre-sized read buffers rather than unbounded doubling growth, mitigating memory allocation spikes.
- **Batch Processing Bounds:** When batch decoding files (`--batch`), `lens` resolves matching files deterministically and verifies file boundaries before queuing decode jobs.

### 1.3 Panic Recovery Across All Enrichers

Generated Go code from XDR schemas contains accessor functions (e.g., `MustWasmHash()`, `GetAddress()`) that panic if an unexpected or unpopulated union discriminant is accessed.
- Every `Enricher`, `fieldNote`, and `nodeHook` in `stellar-xdr-lens` runs inside a deferred `recover()` block.
- A malformed or truncated payload will never panic or crash the host process; rather, enrichment gracefully falls back to the generic XDR structural representation.

---

## 2. Hardening Best Practices for Integrators

If you are consuming `pkg/lens` inside a web server, gRPC service, or worker daemon, follow these production recommendations:

### 2.1 Enforce Context Deadlines & Timeouts

XDR decoding is synchronous and CPU-bound. When accepting payloads over HTTP or RPC, always enforce timeouts using Go's `context.Context`:

```go
func SafeDecodeWithTimeout(ctx context.Context, payload, typeName string) (*lens.Value, error) {
    type result struct {
        val *lens.Value
        err error
    }

    ch := make(chan result, 1)
    go func() {
        val, err := lens.DecodeAs(payload, typeName)
        ch <- result{val: val, err: err}
    }()

    select {
    case <-ctx.Done():
        return nil, ctx.Err() // Timeout or client disconnect
    case res := <-ch:
        return res.val, res.err
    }
}
```

### 2.2 Limit Inbound Request Body Sizes

Before passing raw text or base64 data to `lens.Decode`, enforce an absolute size cap on incoming HTTP requests using `http.MaxBytesReader` or `io.LimitReader`:

```go
// Bound incoming request bodies to 10 MB maximum
r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
```

For individual transaction envelopes on Stellar, valid wire envelopes rarely exceed 1 MB (Soroban transactions have an SDK maximum budget, typically under 128 KB for transaction envelopes and up to a few MBs for contract code uploads).

### 2.3 Sanitizing Sensitive Data (Redaction)

XDR payloads often contain sensitive account identities, private network hashes, or proprietary contract state keys. When sharing diffs or logging XDR trees, sanitize them using `lens` redaction tools:

- **CLI Redaction:**
  ```bash
  # Redact hashes and signatures
  lens diff --redact hashes,signatures envA.xdr envB.xdr

  # Redact all dynamic identifiers
  lens diff --redact all --redact-fields PrevHash,SourceAccount envA.xdr envB.xdr
  ```
- **Programmatic Redaction:**
  Use `lens.RedactValue(val, lens.RedactHashesAndSignatures())` to strip cryptographic material and clear the underlying raw byte fields before logging or transmitting.

### 2.4 Offline Guarantee

`lens` never initiates network requests, does not perform DNS lookups, and does not touch system keys or cryptographic keyrings. Maintain this guarantee by running worker containers in sandboxes without outbound network access.

---

## 3. Vulnerability Disclosure Policy

If you discover a denial-of-service vector, unhandled panic, or memory exhaustion exploit in `stellar-xdr-lens`:

1. **Do not open a public issue.**
2. Report the vulnerability privately via [GitHub Security Advisories](https://github.com/stellar-optics/stellar-xdr-lens/security/advisories/new).
3. Include the offending base64 XDR string and reproduction steps.
4. For details on timelines, review periods, and response SLAs, consult [SECURITY.md](../../SECURITY.md).
