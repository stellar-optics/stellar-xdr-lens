# Architecture

This document explains how `stellar-xdr-lens` fits together and, more usefully,
where to make the three changes contributors most often want to make:

- [Adding a new XDR type](#adding-a-new-xdr-type)
- [Improving how a type is rendered](#adding-an-enricher)
- [Adding a new output format](#adding-a-formatter)

## The pipeline

Everything flows one way:

```
base64 ──► detect ──► xdr decode ──► Node tree ──► enrich ──► Formatter ──► output
              │            │             │                        │
        registry      stellar SDK    neutral IR            tree │ json │ text │ diff
```

The important property is that **only one stage knows about XDR**. Once a value
becomes a `Node` tree, nothing downstream imports `xdr` or uses reflection.
That is what lets a new output format work for all 25 registered types at once,
and lets the differ work without knowing what it is comparing.

## Package layout

```
cmd/lens/              main; wires os.Args to internal/cli and maps errors to exit codes
internal/cli/          cobra commands — deliberately thin
  root.go              command tree, global flags
  decode.go            decode + types commands
  explain.go           explain command, exit-code plumbing
  diff.go              diff command
  input.go             argument / --file / stdin resolution
pkg/lens/              the library; this is the reusable surface
  decode.go            type registry, Detect, Decode, DecodeAs
  node.go              the neutral Node tree — the only file using reflection
  enrich.go            per-type human rendering (strkey, amounts, timestamps)
  explain.go           Summary, Outcome, per-operation descriptions
  reason.go            all 208 result codes → plain English
  diff.go              structural path diff
  format/              output formats
    format.go          Formatter interface, colour palette, TTY detection
    tree.go            colourised tree
    json.go            stable JSON
    text.go            explain summaries
    diff.go            diff rendering
testdata/              real captured mainnet XDR + manifest.json recording provenance
```

`pkg/lens` is the public API and is what external projects import. `internal/cli`
cannot be imported by anyone else, which is deliberate: the CLI is an
implementation detail, and keeping it internal means its shape is free to change
without breaking anyone.

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
That is the whole change. Detection, `--type`, `lens types`, the tree, the JSON
output and the differ all pick it up automatically.

### Choosing a priority

Priority breaks ties when several types decode the same bytes. Higher wins.
Roughly:

| Range | Use for |
|---|---|
| 90–100 | What developers paste most: envelopes, results, meta |
| 60–89 | Ledger entries, keys, inner transaction types |
| 30–59 | Soroban types |
| 1–29 | Small building blocks that decode from very few bytes |

Small types deserve low priority because short payloads are ambiguous — a
4-byte value may decode as half a dozen types, and the ranking decides which
one `lens decode` shows by default.

## Adding an enricher

Enrichers turn an XDR value into something a human recognises. They live in
[`enrich.go`](../pkg/lens/enrich.go) and are registered by concrete type:

```go
func init() {
	enricherFor = map[reflect.Type]Enricher{
		reflect.TypeOf(xdr.AccountId{}): enrichAccountID,
		// add yours here
	}
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

The registry is populated once in `init` and read-only afterwards, so it needs
no locking.

### Field-name enrichment

Some renderings depend on the field name rather than the type. XDR models
amounts, sequence numbers and offer ids all as `Int64`, so the only signal that
a value is an amount is the field it sits in. `amountFields` in `enrich.go`
lists the known amount fields; anything not listed stays a plain integer,
because rendering a sequence number as `12345.0000000` would be worse than
showing it raw.

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

Colour is handled centrally: call `format.PaletteFor(w, mode)`, which honours
`--color`, the [`NO_COLOR`](https://no-color.org) convention, and whether the
writer is a terminal. Never write escape sequences directly.

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

## Testing approach

Fixtures in `testdata/` are **real captured mainnet transactions**, not
synthetic ones, with `manifest.json` recording each transaction hash so a
fixture can be traced back to the ledger. Tests never touch the network.

Where a case cannot be captured — every classic operation type, memo variant and
precondition shape — tests construct XDR with the SDK encoder and round-trip it
through the real decode path, rather than reaching into internals.

The suite deliberately targets properties that would break the tool if violated:

- detection stays unambiguous on real payloads (`TestDetectIsUnambiguousForRealPayloads`);
- ambiguity is reported, not hidden (`TestDetectReportsGenuineAmbiguity`);
- enums render as names, not integers (`TestEnumsRenderAsConstantNames`);
- a failure is attributed to the operation that caused it
  (`TestExplainPairAttributesFailureToOperation`);
- JSON output is deterministic and omits nulls;
- every result code has a real explanation.
