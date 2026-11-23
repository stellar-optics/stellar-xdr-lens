package format

import (
	"fmt"
	"io"
	"strings"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

// DOTFormatter renders a decoded value as a Graphviz DOT digraph.
//
// Transactions get the domain graph: the source and fee-bump accounts,
// one node per operation, payment destinations, and the contracts an
// invoke-host-function operation calls. Every relationship is a directed,
// labelled edge, so `lens decode --format dot | dot -Tsvg` shows the flow of
// value and calls without hand-drawing it.
//
// XDR types that are not transactions fall back to their decoded node tree:
// a node per field, an edge per parent/child relationship. Output is
// deterministic, escapes labels, and declares every edge endpoint.
type DOTFormatter struct {
	name    string
	rankdir string
}

// DOTOption configures a DOTFormatter.
type DOTOption func(*DOTFormatter)

// WithDOTName sets the graph name. The default is the decoded type.
func WithDOTName(name string) DOTOption {
	return func(f *DOTFormatter) { f.name = name }
}

// WithDOTRankDir sets the Graphviz rank direction ("LR" or "TB").
func WithDOTRankDir(dir string) DOTOption {
	return func(f *DOTFormatter) { f.rankdir = dir }
}

// NewDOTFormatter returns a DOTFormatter.
func NewDOTFormatter(opts ...DOTOption) *DOTFormatter {
	f := &DOTFormatter{rankdir: "LR"}
	for _, o := range opts {
		o(f)
	}
	return f
}

// Format writes v as a DOT digraph, satisfying the Formatter interface.
func (f *DOTFormatter) Format(w io.Writer, v *lens.Value) error {
	if v == nil || v.Node == nil {
		return fmt.Errorf("format: dot: %w: nil value", lens.ErrInvalidArgument)
	}

	name := f.name
	if name == "" {
		name = v.Type
	}
	if name == "" {
		name = "xdr"
	}

	g := &dotGraph{}
	g.writeHeader(name, f.rankdir)

	if summary, err := lens.Explain(v); err == nil && summary != nil &&
		(summary.Source != "" || len(summary.Operations) > 0) {
		var ops []xdr.Operation
		if env, ok := v.Raw.(*xdr.TransactionEnvelope); ok {
			ops = envelopeOperations(env)
		}
		f.formatTransactionGraph(g, summary, ops)
	} else {
		f.formatNodeGraph(g, v.Node)
	}

	g.writeFooter()

	if _, err := io.WriteString(w, g.b.String()); err != nil {
		return fmt.Errorf("format: write dot: %w", err)
	}
	return nil
}

// dotGraph accumulates declarations so each node is emitted at most once and
// every edge endpoint is guaranteed to exist.
type dotGraph struct {
	b        strings.Builder
	declared map[string]bool
	extra    int
}

func (g *dotGraph) writeHeader(name, rankdir string) {
	if rankdir == "" {
		rankdir = "LR"
	}
	fmt.Fprintf(&g.b, "digraph %q {\n", name)
	fmt.Fprintf(&g.b, "  rankdir=%q;\n", rankdir)
	g.b.WriteString("  node [fontname=\"Helvetica\", style=\"rounded,filled\", fillcolor=\"#eef3ff\"];\n")
	g.b.WriteString("  edge [fontname=\"Helvetica\"];\n")
}

func (g *dotGraph) writeFooter() {
	g.b.WriteString("}\n")
}

// node declares id if it has not been declared yet.
func (g *dotGraph) node(id, label, shape string) {
	if g.declared == nil {
		g.declared = map[string]bool{}
	}
	if g.declared[id] {
		return
	}
	g.declared[id] = true
	if shape != "" {
		fmt.Fprintf(&g.b, "  %q [label=\"%s\", shape=%q];\n", id, dotEscape(label), shape)
		return
	}
	fmt.Fprintf(&g.b, "  %q [label=\"%s\"];\n", id, dotEscape(label))
}

// edge declares both endpoints, so the DOT is valid even when the caller
// forgets to add a node first.
func (g *dotGraph) edge(from, to, label, style string) {
	g.node(from, from, "")
	g.node(to, to, "")
	if style != "" {
		fmt.Fprintf(&g.b, "  %q -> %q [label=\"%s\", style=%q];\n", from, to, dotEscape(label), style)
		return
	}
	fmt.Fprintf(&g.b, "  %q -> %q [label=\"%s\"];\n", from, to, dotEscape(label))
}

// formatTransactionGraph draws accounts, operations, payments and contract
// calls as one directed graph.
func (f *DOTFormatter) formatTransactionGraph(g *dotGraph, s *lens.Summary, ops []xdr.Operation) {
	source := s.Source
	if source == "" {
		source = "transaction"
	}
	sourceID := "acct_" + sanitizeID(source)
	g.node(sourceID, source+" (source)", "ellipse")

	if s.FeeBumpSource != "" && s.FeeBumpSource != s.Source {
		feeID := "acct_" + sanitizeID(s.FeeBumpSource)
		g.node(feeID, s.FeeBumpSource+" (fee bump)", "ellipse")
		g.edge(feeID, sourceID, "covers fee", "")
	}

	for _, op := range s.Operations {
		opSource := op.Source
		if opSource == "" {
			opSource = s.Source
		}
		opSourceID := sourceID
		if opSource != "" && opSource != s.Source {
			opSourceID = "acct_" + sanitizeID(opSource)
			g.node(opSourceID, opSource+" (operation source)", "ellipse")
		}

		opID := fmt.Sprintf("op_%d", op.Index)
		label := fmt.Sprintf("Op %d: %s", op.Index, op.Type)
		if op.Result != nil && !op.Result.Success {
			label += " (FAILED: " + op.Result.Code + ")"
		}
		g.node(opID, label, "box")
		label = op.Type
		if op.Detail != "" {
			label = op.Detail
		}
		g.edge(opSourceID, opID, label, "")

		if op.Index < 0 || op.Index >= len(ops) {
			continue
		}
		raw := ops[op.Index]

		if dest := operationDestination(raw); dest != "" {
			destID := "acct_" + sanitizeID(dest)
			g.node(destID, dest, "ellipse")
			edgeLabel := op.Detail
			if edgeLabel == "" {
				edgeLabel = "payment"
			}
			g.edge(opID, destID, edgeLabel, "")
		}

		if inv, ok := raw.Body.GetInvokeHostFunctionOp(); ok {
			root, auth := invokeOpCalls(inv, opSource)
			for _, call := range root {
				f.addCallEdge(g, opID, call, "")
			}
			for _, call := range auth {
				f.addCallEdge(g, opID, call, "dashed")
			}
		}
	}
}

// addCallEdge draws op -> contract for a Soroban invocation.
func (f *DOTFormatter) addCallEdge(g *dotGraph, opID string, call hostCall, style string) {
	callee := call.Callee
	if callee == "" {
		callee = "new contract"
	}
	id := "contract_" + sanitizeID(callee)
	if callee == "new contract" {
		// An anonymous created contract has no address to key on, so key
		// it on the operation that creates it.
		id = "contract_new_" + sanitizeID(opID)
	}
	g.node(id, "Contract "+callee, "hexagon")
	g.edge(opID, id, call.label(), style)
}

// formatNodeGraph draws a non-transaction value as its decoded field tree.
func (f *DOTFormatter) formatNodeGraph(g *dotGraph, root *lens.Node) {
	f.walkNode(g, root, "root")
}

func (f *DOTFormatter) walkNode(g *dotGraph, n *lens.Node, id string) {
	if n == nil {
		return
	}
	label := n.Name
	if label == "" {
		label = n.TypeName
	}
	if label == "" {
		label = id
	}
	if n.Kind == lens.KindScalar || len(n.Children) == 0 {
		if n.Value != nil {
			label += " = " + fmt.Sprint(n.Value)
		} else if n.Note != "" {
			label += " = " + n.Note
		}
		g.node(id, label, "ellipse")
		return
	}

	g.node(id, label, "box")
	for i, child := range n.Children {
		childID := fmt.Sprintf("%s_%d", id, i)
		f.walkNode(g, child, childID)
		edgeLabel := child.Name
		if edgeLabel == "" {
			edgeLabel = child.TypeName
		}
		g.edge(id, childID, edgeLabel, "")
	}
}

// operationDestination extracts the destination account of an operation whose
// body moves value or creates an account. The summary's Detail truncates
// addresses for humans; a graph node needs the full strkey.
func operationDestination(op xdr.Operation) string {
	switch op.Body.Type {
	case xdr.OperationTypePayment:
		if op.Body.PaymentOp != nil {
			return op.Body.PaymentOp.Destination.Address()
		}
	case xdr.OperationTypePathPaymentStrictReceive:
		if op.Body.PathPaymentStrictReceiveOp != nil {
			return op.Body.PathPaymentStrictReceiveOp.Destination.Address()
		}
	case xdr.OperationTypePathPaymentStrictSend:
		if op.Body.PathPaymentStrictSendOp != nil {
			return op.Body.PathPaymentStrictSendOp.Destination.Address()
		}
	case xdr.OperationTypeAccountMerge:
		if op.Body.Destination != nil {
			return op.Body.Destination.Address()
		}
	case xdr.OperationTypeCreateAccount:
		if op.Body.CreateAccountOp != nil {
			return op.Body.CreateAccountOp.Destination.Address()
		}
	}
	return ""
}

// dotEscape makes a label safe inside a double-quoted DOT string.
func dotEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", "")
	return s
}
