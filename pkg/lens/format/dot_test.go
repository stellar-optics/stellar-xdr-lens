package format_test

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens/format"
)

var (
	dotNodeLine = regexp.MustCompile(`^\s*"([^"]+)"\s+\[`)
	dotEdgeLine = regexp.MustCompile(`^\s*"([^"]+)"\s*->\s*"([^"]+)"`)
)

// validateDOT checks the properties that make a digraph parseable: one
// balanced block, quoted node ids, semicolon-terminated statements, and every
// edge endpoint declared as a node.
func validateDOT(t *testing.T, out string) (nodes map[string]bool, edges int) {
	t.Helper()

	trimmed := strings.TrimSpace(out)
	if !strings.HasPrefix(trimmed, "digraph ") {
		t.Fatalf("output does not start with a digraph: %s", out)
	}
	if !strings.HasSuffix(trimmed, "}") {
		t.Fatalf("output does not close the digraph: %s", out)
	}
	if strings.Count(out, "{") != strings.Count(out, "}") {
		t.Fatalf("unbalanced braces:\n%s", out)
	}

	nodes = map[string]bool{}
	type endpoint struct{ from, to string }
	var endpoints []endpoint

	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "digraph "), line == "}",
			strings.HasPrefix(line, "rankdir="), strings.HasPrefix(line, "node ["),
			strings.HasPrefix(line, "edge ["):
			continue
		}
		if !strings.HasSuffix(line, ";") {
			t.Errorf("statement is not semicolon-terminated: %q", line)
		}
		if m := dotNodeLine.FindStringSubmatch(line); m != nil && !strings.Contains(line, "->") {
			nodes[m[1]] = true
			continue
		}
		if m := dotEdgeLine.FindStringSubmatch(line); m != nil {
			endpoints = append(endpoints, endpoint{m[1], m[2]})
			continue
		}
		t.Errorf("unrecognised DOT statement: %q", line)
	}

	for _, e := range endpoints {
		if !nodes[e.from] {
			t.Errorf("edge source %q is not declared", e.from)
		}
		if !nodes[e.to] {
			t.Errorf("edge target %q is not declared", e.to)
		}
	}
	return nodes, len(endpoints)
}

func TestDOTFormatterSorobanTransaction(t *testing.T) {
	t.Parallel()

	env := loadValue(t, "tx_soroban.env")
	summary, err := lens.Explain(env)
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}

	var buf bytes.Buffer
	if err := format.NewDOTFormatter().Format(&buf, env); err != nil {
		t.Fatalf("Format: %v", err)
	}
	out := buf.String()
	nodes, edges := validateDOT(t, out)

	if !strings.Contains(out, `rankdir="LR"`) {
		t.Errorf("missing rankdir:\n%s", out)
	}
	if !strings.Contains(out, summary.Source) {
		t.Errorf("missing source account node %s:\n%s", summary.Source, out)
	}
	if !strings.Contains(out, "op_0") {
		t.Errorf("missing operation node:\n%s", out)
	}
	if !strings.Contains(out, "Contract CBGSBKYMYO6OMGHQXXNOBRGVUDFUDVC2XLC3SXON5R2SNXILR7XCKKY3") {
		t.Errorf("missing contract node:\n%s", out)
	}
	if !strings.Contains(out, `label="harvest(`) {
		t.Errorf("missing invocation edge label:\n%s", out)
	}
	if edges < 2 {
		t.Errorf("got %d edges, want at least a source->op and op->contract", edges)
	}
	for id := range nodes {
		if strings.ContainsAny(id, " \n\"") {
			t.Errorf("node id %q contains unsafe characters", id)
		}
	}

}

func TestDOTFormatterPaymentFlow(t *testing.T) {
	t.Parallel()

	source := xdr.MustAddress("GAPB7YMUQ6MPQ23PTFDNBZP7HGGTZPI2DT5JJURUOIXNPLGHW2O3WYXZ")
	dest := xdr.MustAddress("GCJNHFXJEQEY54NPKU4DBMT5WOBJUHMYSFBDAQF4OMTEGJN4BQU7ZST6")
	env := &xdr.TransactionEnvelope{
		Type: xdr.EnvelopeTypeEnvelopeTypeTx,
		V1: &xdr.TransactionV1Envelope{
			Tx: xdr.Transaction{
				SourceAccount: xdr.MustMuxedAddress(source.Address()),
				Fee:           100,
				SeqNum:        1,
				Operations: []xdr.Operation{{
					Body: xdr.OperationBody{
						Type: xdr.OperationTypePayment,
						PaymentOp: &xdr.PaymentOp{
							Destination: xdr.MustMuxedAddress(dest.Address()),
							Asset:       xdr.MustNewNativeAsset(),
							Amount:      100,
						},
					},
				}},
			},
		},
	}
	value := &lens.Value{Type: "TransactionEnvelope", Raw: env, Node: &lens.Node{TypeName: "TransactionEnvelope", Kind: lens.KindStruct}}

	var buf bytes.Buffer
	if err := format.NewDOTFormatter().Format(&buf, value); err != nil {
		t.Fatalf("Format: %v", err)
	}
	out := buf.String()
	validateDOT(t, out)

	if !strings.Contains(out, dest.Address()) {
		t.Errorf("missing destination node %s:\n%s", dest.Address(), out)
	}
	if !strings.Contains(out, `label="pay `) {
		t.Errorf("missing payment edge label:\n%s", out)
	}
}

func TestDOTFormatterNodeTreeFallback(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := format.NewDOTFormatter(format.WithDOTName("trustline")).Format(&buf, loadValue(t, "trustline_entry_simple")); err != nil {
		t.Fatalf("Format: %v", err)
	}
	out := buf.String()
	validateDOT(t, out)

	if !strings.Contains(out, `digraph "trustline"`) {
		t.Errorf("missing custom graph name:\n%s", out)
	}
	if !strings.Contains(out, "root") {
		t.Errorf("missing root node:\n%s", out)
	}
}

func TestDOTFormatterEscapesLabels(t *testing.T) {
	t.Parallel()

	value := &lens.Value{
		Type: "Custom",
		Node: &lens.Node{
			TypeName: "Custom",
			Kind:     lens.KindStruct,
			Children: []*lens.Node{
				{Name: `say "hi"`, Kind: lens.KindScalar, Value: "back\\slash\nline"},
			},
		},
	}

	var buf bytes.Buffer
	if err := format.NewDOTFormatter().Format(&buf, value); err != nil {
		t.Fatalf("Format: %v", err)
	}
	out := buf.String()
	validateDOT(t, out)

	if strings.Contains(out, "back\\slash\nline") {
		t.Errorf("raw newline/backslash leaked into label:\n%s", out)
	}
	if !strings.Contains(out, `say \"hi\"`) {
		t.Errorf("quotes were not escaped:\n%s", out)
	}
	if !strings.Contains(out, `back\\slash\nline`) {
		t.Errorf("backslash and newline were not escaped:\n%s", out)
	}
}

func TestDOTFormatterRejectsNilValue(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := format.NewDOTFormatter().Format(&buf, nil); err == nil {
		t.Fatal("Format(nil) = nil error, want error")
	}
}
