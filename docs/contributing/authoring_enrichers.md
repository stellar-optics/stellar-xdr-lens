# Contributor Guide: Authoring Enrichers & Output Formatters

The core architectural principle of `stellar-xdr-lens` is a strictly decoupled, one-way pipeline:

```text
raw XDR (base64/hex) ──► decode ──► neutral Node tree ──► enrich ──► Formatter ──► output
```

This decoupled design ensures:
1. **Zero Coupling to XDR in Formatters:** Formatters consume a neutral `Node` tree rather than generated SDK structs. Adding a formatter immediately works for all ~60 registered XDR types.
2. **Non-Destructive Enrichment:** Human annotations (notes) are attached beside values without modifying underlying scalar values or types.
3. **Plug-and-Play Extensibility:** New enrichers and formatters can be added in standalone files without modifying core decoding routines.

This guide provides a comprehensive tutorial for writing, registering, and testing new enrichers and formatters.

---

## Table of Contents

- [The Enricher Lifecycle](#the-enricher-lifecycle)
  - [Tree Traversal & Node Attachment](#tree-traversal--node-attachment)
  - [The Note Mechanism](#the-note-mechanism)
  - [Registration Architecture](#registration-architecture)
  - [Handling Union Switches & Defensive Parsing](#handling-union-switches--defensive-parsing)
- [Step-by-Step Tutorial: Authoring an Enricher](#step-by-step-tutorial-authoring-an-enricher)
- [The Formatter Interface](#the-formatter-interface)
  - [The `Formatter` Contract](#the-formatter-contract)
  - [Walking the Node Tree](#walking-the-node-tree)
  - [Color Themes & Terminal Styling](#color-themes--terminal-styling)
  - [Survey of Existing Formatters](#survey-of-existing-formatters)
- [Step-by-Step Tutorial: Authoring a Formatter](#step-by-step-tutorial-authoring-a-formatter)
  - [Tutorial: Graphviz DOT Formatter](#tutorial-graphviz-dot-formatter)
- [Testing Standards](#testing-standards)
  - [Canonical Test Fixtures in `testdata/`](#canonical-test-fixtures-in-testdata)
  - [Table-Driven Unit Tests](#table-driven-unit-tests)
  - [Testing Without Color & Edge Cases](#testing-without-color--edge-cases)

---

## The Enricher Lifecycle

### Tree Traversal & Node Attachment

When `lens.Decode` parses an XDR blob:
1. The XDR unmarshaler creates a concrete SDK struct (e.g. `xdr.TransactionEnvelope`).
2. `pkg/lens/node.go` reflects over the struct to produce an intermediate `*lens.Node` tree:
   - Each `Node` has a `Name`, `Kind` (Scalar, Struct, Slice, Map, Union), `Value`, and optional `Note`.
3. `pkg/lens/enrich.go` traverses the tree post-decode, consulting registered enrichers to populate `Node.Note`.

### The Note Mechanism

An enricher transforms opaque protocol bytes into human-friendly strings:
- **Stroops to Stellar Units:** Converting `10000000` stroops to `"1.0000000 XLM"`.
- **Public Keys to Strkey:** Converting 32 raw Ed25519 bytes into `"GAAZI4T57VYYM7TKGTOUFFM4UK2PNLHZ74PUGL4WKKAE3DKUAAOGTDQS"`.
- **Unix Timestamps:** Converting `1700000000` to `"2023-11-14 22:13:20 UTC"`.
- **Asset Identifiers:** Converting an `xdr.Asset` union to `"USDC:GA5Z...VN"`.

> [!NOTE]
> The original value in `Node.Value` remains untouched (e.g., an integer remains an `int64`). Formatters like `--json` or `--raw` can output either the exact value, the enriched note, or both.

### Registration Architecture

Enrichers are registered during package initialization in `init()` functions. The global registry maps `reflect.Type` to an `Enricher` function:

```go
type Enricher func(rv reflect.Value) (display string, raw any, ok bool)
```

- `display`: Short formatted string (e.g. `"100.0000000 XLM"`).
- `raw`: Optional simplified representation used by JSON formatters.
- `ok`: Return `true` if enrichment succeeded, or `false` to decline.

Because registration occurs in `init()`, the registry is immutable at runtime and requires zero synchronization locks.

### Handling Union Switches & Defensive Parsing

Because `lens` handles untrusted and potentially malformed XDR, all enrichers are executed behind a panic recovery barrier in `enrich.go`. However, enrichers must proactively validate discriminants:
- Return `ok=false` for unknown or unhandled union arms.
- Never panic on unexpected enum integers.
- Decline gracefully when zero-value placeholders are encountered.

---

## Step-by-Step Tutorial: Authoring an Enricher

Suppose Protocol 23 introduces an `EscrowStatus` enum that we want to enrich.

### 1. Create the Feature File
Create `pkg/lens/escrow.go`:

```go
package lens

import (
	"reflect"

	"github.com/stellar/go-stellar-sdk/xdr"
)

func init() {
	enricherFor[reflect.TypeOf(xdr.EscrowStatus(0))] = enrichEscrowStatus
}

// enrichEscrowStatus renders escrow states into plain English.
func enrichEscrowStatus(rv reflect.Value) (string, any, bool) {
	status, ok := rv.Interface().(xdr.EscrowStatus)
	if !ok {
		return "", nil, false
	}

	switch status {
	case xdr.EscrowStatusPending:
		return "", "pending deposit", true
	case xdr.EscrowStatusLocked:
		return "", "locked until timebound", true
	case xdr.EscrowStatusReleased:
		return "", "released to beneficiary", true
	case xdr.EscrowStatusReverted:
		return "", "refunded to creator", true
	default:
		// Decline unknown or future protocol enum values
		return "", nil, false
	}
}
```

### 2. Write Unit Tests
Create `pkg/lens/escrow_test.go`:

```go
package lens

import (
	"reflect"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"
)

func TestEnrichEscrowStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status xdr.EscrowStatus
		want   string
		wantOK bool
	}{
		{"pending", xdr.EscrowStatusPending, "pending deposit", true},
		{"locked", xdr.EscrowStatusLocked, "locked until timebound", true},
		{"released", xdr.EscrowStatusReleased, "released to beneficiary", true},
		{"reverted", xdr.EscrowStatusReverted, "refunded to creator", true},
		{"unknown declines", xdr.EscrowStatus(99), "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, raw, ok := enrichEscrowStatus(reflect.ValueOf(tc.status))
			if ok != tc.wantOK || (tc.wantOK && raw != tc.want) {
				t.Errorf("enrichEscrowStatus(%v) = (_, %v, %v), want (_, %q, %v)",
					tc.status, raw, ok, tc.want, tc.wantOK)
			}
		})
	}
}
```

---

## The Formatter Interface

### The `Formatter` Contract

Every output format implements the `format.Formatter` interface in `pkg/lens/format/format.go`:

```go
package format

import (
	"io"
	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

// Formatter renders a decoded XDR value to a writer.
type Formatter interface {
	Format(w io.Writer, v *lens.Value) error
}
```

### Walking the Node Tree

The `*lens.Value` struct exposes the root `*lens.Node`:

```go
type Node struct {
	Name     string
	Kind     Kind           // KindScalar, KindStruct, KindSlice, KindMap, KindUnion
	Type     string         // Concrete XDR type name
	Value    any            // Underlying value
	Note     string         // Human-readable note from enricher
	Children []*Node        // Nested child nodes
}
```

A formatter traverses `Children` recursively, outputting node keys, values, and notes according to its format rules.

### Color Themes & Terminal Styling

`lens` supports terminal colors via `format.Palette`:
- Standard roles: `Key`, `Type`, `Str`, `Num`, `Note`, `Dim`, `Add`, `Remove`, `Change`, `Reset`.
- Automatically respects `NO_COLOR`, `TERM=dumb`, and non-TTY file redirects.

Always use palette variables rather than hardcoding ANSI escape sequences.

### Survey of Existing Formatters

| Formatter | File | Output Style | Use Case |
|---|---|---|---|
| `tree` | `pkg/lens/format/tree.go` | Indented ASCII/Unicode tree with ANSI colors. | Interactive terminal exploration (`lens decode`). |
| `json` | `pkg/lens/format/json.go` | Deterministic, sorted JSON representation. | Scripting and `jq` pipelines (`--json`). |
| `text` | `pkg/lens/format/text.go` | Plain English natural language sentences. | Human failure analysis (`lens explain`). |
| `mermaid`| `pkg/lens/format/mermaid.go`| Mermaid markdown sequence and flowcharts. | Architecture documentation & PR explanations. |
| `summary`| `pkg/lens/format/summary.go`| Single-line dense summary. | Grep, logs, and batch audits (`--format summary`). |
| `txrep` | `pkg/lens/format/txrep.go` | SEP-0011 TxRep human-readable key-value lines. | Stellar core debugging & multisig coordination. |
| `csv` | `pkg/lens/format/csv.go` | Flat tabular CSV row. | Data warehouse ingestion and spreadsheets. |
| `html` | `pkg/lens/format/html.go` | Standalone HTML document with dark theme. | Browser reports & audit attachments. |
| `junit` | `pkg/lens/format/junit.go` | Standard XML test report format. | CI test reporter integration. |

---

## Step-by-Step Tutorial: Authoring a Formatter

### Tutorial: Graphviz DOT Formatter

Let's build a Graphviz DOT formatter that turns any XDR node tree into a directed graph for visual rendering with `dot -Tpng`.

#### 1. Implement `format.DotFormatter`
Create `pkg/lens/format/dot.go`:

```go
package format

import (
	"fmt"
	"io"
	"strconv"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

type DotFormatter struct{}

func NewDotFormatter() *DotFormatter {
	return &DotFormatter{}
}

func (f *DotFormatter) Format(w io.Writer, v *lens.Value) error {
	if v == nil || v.Node == nil {
		return fmt.Errorf("format: nil value or root node")
	}

	if _, err := fmt.Fprintln(w, "digraph XDR {"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "  node [shape=box, fontname=\"Courier\"];"); err != nil {
		return err
	}

	counter := 0
	if err := f.walk(w, v.Node, &counter, 0); err != nil {
		return err
	}

	_, err := fmt.Fprintln(w, "}")
	return err
}

func (f *DotFormatter) walk(w io.Writer, node *lens.Node, counter *int, parentID int) error {
	*counter++
	currentID := *counter

	label := node.Name
	if node.Note != "" {
		label += fmt.Sprintf(" (%s)", node.Note)
	} else if node.Value != nil {
		label += fmt.Sprintf(": %v", node.Value)
	}

	// Output node declaration
	if _, err := fmt.Fprintf(w, "  node%d [label=%s];\n", 
		currentID, strconv.Quote(label)); err != nil {
		return err
	}

	// Output edge from parent
	if parentID > 0 {
		if _, err := fmt.Fprintf(w, "  node%d -> node%d;\n", parentID, currentID); err != nil {
			return err
		}
	}

	// Walk children
	for _, child := range node.Children {
		if err := f.walk(w, child, counter, currentID); err != nil {
			return err
		}
	}
	return nil
}
```

#### 2. Wire into CLI
In `internal/cli/decode.go`, add `"dot"` to the `--format` flag handler:

```go
case "dot":
    f := format.NewDotFormatter()
    return f.Format(stdout, value)
```

---

## Testing Standards

### Canonical Test Fixtures in `testdata/`

All decoder and enricher tests should leverage canonical fixtures in `testdata/`:
- `testdata/manifest.json`: Records ledger sequence, network, and source hash for each fixture.
- When introducing a new XDR type or feature, add a `.xdr` base64 file to `testdata/` and document its provenance in `testdata/manifest.json`.
- **Never add tests that make network calls.** All unit tests must execute 100% offline.

### Table-Driven Unit Tests

Follow the project convention of table-driven testing with `t.Parallel()`:

```go
func TestFormatterOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		node    *lens.Node
		want    string
		wantErr bool
	}{
		// Test cases
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			f := format.NewDotFormatter()
			err := f.Format(&buf, &lens.Value{Node: tc.node})
			if (err != nil) != tc.wantErr {
				t.Fatalf("unexpected error status: %v", err)
			}
			if !tc.wantErr && !strings.Contains(buf.String(), tc.want) {
				t.Errorf("got %q, want substring %q", buf.String(), tc.want)
			}
		})
	}
}
```

### Testing Without Color & Edge Cases

When testing formatters:
1. Always test with `format.NoColor` to ensure stable string comparisons in CI.
2. Verify behavior when `Node.Note` is empty vs populated.
3. Test edge cases: empty structs, empty slices, nil pointers, and deep recursion.
