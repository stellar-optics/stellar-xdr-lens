package format

import (
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

// MermaidFormatter renders transaction operations and relationships as a
// Mermaid flowchart diagram (graph TD).
type MermaidFormatter struct {
	direction string
}

// MermaidOption configures a MermaidFormatter.
type MermaidOption func(*MermaidFormatter)

// WithMermaidDirection sets the flowchart layout direction ("TD", "LR", etc.).
func WithMermaidDirection(direction string) MermaidOption {
	return func(m *MermaidFormatter) {
		m.direction = direction
	}
}

// NewMermaidFormatter creates a MermaidFormatter with default direction "TD".
func NewMermaidFormatter(opts ...MermaidOption) *MermaidFormatter {
	m := &MermaidFormatter{
		direction: "TD",
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Format renders the transaction operations or node structure to w as Mermaid flowchart syntax.
func (f *MermaidFormatter) Format(w io.Writer, v *lens.Value) error {
	if v == nil || v.Node == nil {
		return fmt.Errorf("format: nil value")
	}

	// Soroban values are a conversation: an authoriser, a contract, its
	// sub-invocations. A sequence diagram shows that ordering and direction;
	// the flowchart below cannot.
	if sequence, ok := sorobanSequence(v); ok {
		if _, err := io.WriteString(w, sequence); err != nil {
			return fmt.Errorf("format: write mermaid: %w", err)
		}
		return nil
	}

	direction := f.direction
	if direction == "" {
		direction = "TD"
	}

	b := &strings.Builder{}
	fmt.Fprintf(b, "graph %s\n", direction)

	// Attempt high-level transaction operation explanation first
	summary, err := lens.Explain(v)
	if err == nil && summary != nil && (summary.Source != "" || len(summary.Operations) > 0) {
		f.formatTransactionGraph(b, summary)
	} else {
		// Fallback for non-transaction XDR types: render top-level node hierarchy
		f.formatNodeGraph(b, v.Node)
	}

	_, writeErr := io.WriteString(w, b.String())
	return writeErr
}

var nonAlphanumeric = regexp.MustCompile(`[^a-zA-Z0-9_]`)

func sanitizeID(s string) string {
	res := nonAlphanumeric.ReplaceAllString(s, "_")
	if res == "" || (res[0] >= '0' && res[0] <= '9') {
		res = "node_" + res
	}
	return res
}

func sanitizeLabel(s string) string {
	// Escape quotes and ensure no bracket breaking
	clean := strings.ReplaceAll(s, `"`, `'`)
	clean = strings.ReplaceAll(clean, "\n", " ")
	return clean
}

func (f *MermaidFormatter) formatTransactionGraph(b *strings.Builder, s *lens.Summary) {
	sourceAcc := s.Source
	if sourceAcc == "" {
		sourceAcc = "TransactionSource"
	}
	sourceID := sanitizeID("acc_" + sourceAcc)
	fmt.Fprintf(b, "  %s[\"%s (Source)\"]\n", sourceID, sanitizeLabel(truncateAddr(sourceAcc)))

	if s.FeeBumpSource != "" && s.FeeBumpSource != s.Source {
		feeBumpID := sanitizeID("acc_" + s.FeeBumpSource)
		fmt.Fprintf(b, "  %s[\"%s (Fee-Bump)\"]\n", feeBumpID, sanitizeLabel(truncateAddr(s.FeeBumpSource)))
		fmt.Fprintf(b, "  %s -->|covers fee| %s\n", feeBumpID, sourceID)
	}

	for _, op := range s.Operations {
		opSource := op.Source
		currentSourceID := sourceID
		if opSource != "" && opSource != s.Source {
			currentSourceID = sanitizeID("acc_" + opSource)
			fmt.Fprintf(b, "  %s[\"%s (Op %d Source)\"]\n", currentSourceID, sanitizeLabel(truncateAddr(opSource)), op.Index)
		}

		opNodeID := fmt.Sprintf("op_%d", op.Index)
		opLabel := fmt.Sprintf("Op %d: %s", op.Index, op.Type)
		if op.Result != nil && !op.Result.Success {
			opLabel += fmt.Sprintf(" (FAILED: %s)", op.Result.Code)
		}
		fmt.Fprintf(b, "  %s[\"%s\"]\n", opNodeID, sanitizeLabel(opLabel))

		edgeLabel := op.Type
		if op.Detail != "" {
			edgeLabel = op.Detail
		}
		fmt.Fprintf(b, "  %s -->|\"%s\"| %s\n", currentSourceID, sanitizeLabel(edgeLabel), opNodeID)
	}
}

func (f *MermaidFormatter) formatNodeGraph(b *strings.Builder, root *lens.Node) {
	rootID := "root"
	rootName := root.TypeName
	if rootName == "" {
		rootName = root.Name
	}
	if rootName == "" {
		rootName = "Root"
	}
	if root.Kind == lens.KindScalar && root.Value != nil {
		rootName = fmt.Sprintf("%s: %v", rootName, root.Value)
	}
	fmt.Fprintf(b, "  %s[\"%s\"]\n", rootID, sanitizeLabel(rootName))

	for i, child := range root.Children {
		childID := fmt.Sprintf("child_%d", i)
		childName := child.Name
		if childName == "" && child.TypeName != "" {
			childName = child.TypeName
		}
		if childName == "" {
			childName = fmt.Sprintf("item_%d", i)
		}
		if child.Kind == lens.KindScalar && child.Value != nil {
			childName = fmt.Sprintf("%s: %v", childName, child.Value)
		}
		fmt.Fprintf(b, "  %s[\"%s\"]\n", childID, sanitizeLabel(childName))
		fmt.Fprintf(b, "  %s --> %s\n", rootID, childID)
	}
}

// sorobanSequence renders v as a Mermaid sequenceDiagram when it carries
// Soroban invocation data. The second return value reports whether a diagram
// was produced; the caller falls back to the flowchart when it was not.
func sorobanSequence(v *lens.Value) (string, bool) {
	var calls []hostCall
	switch raw := v.Raw.(type) {
	case *xdr.TransactionEnvelope:
		calls = envelopeHostCalls(raw)
	case xdr.TransactionEnvelope:
		calls = envelopeHostCalls(&raw)
	case *xdr.InvokeHostFunctionOp:
		root, auth := invokeOpCalls(*raw, "Transaction")
		calls = append(calls, root...)
		calls = append(calls, auth...)
	case *xdr.SorobanAuthorizationEntry:
		calls = authorizationCalls(*raw)
	case xdr.SorobanAuthorizationEntry:
		calls = authorizationCalls(raw)
	}
	if len(calls) == 0 {
		return "", false
	}
	return renderSequenceDiagram(calls), true
}

// envelopeHostCalls collects the root and authorised calls of every
// invoke-host-function operation in an envelope.
func envelopeHostCalls(env *xdr.TransactionEnvelope) []hostCall {
	if env == nil {
		return nil
	}
	source := envelopeSource(env)
	var calls []hostCall
	for _, op := range envelopeOperations(env) {
		inv, ok := op.Body.GetInvokeHostFunctionOp()
		if !ok {
			continue
		}
		root, auth := invokeOpCalls(inv, operationSource(op, source))
		calls = append(calls, root...)
		calls = append(calls, auth...)
	}
	return calls
}

// sequenceBuilder accumulates participants and messages so declarations can
// be emitted before any message, as Mermaid requires.
func renderSequenceDiagram(calls []hostCall) string {
	b := &strings.Builder{}
	b.WriteString("sequenceDiagram\n")
	b.WriteString("  autonumber\n")

	participants := &sequenceParticipants{ids: map[string]string{}}
	var messages strings.Builder
	for _, call := range calls {
		sender := participants.id(call.Caller)
		receiver := participants.id(call.Callee)
		arrow := "->>"
		label := call.label()
		if call.Authorized {
			arrow = "-->>"
			label = "authorize " + label
		}
		fmt.Fprintf(&messages, "  %s%s%s: %s\n", sender, arrow, receiver, sanitizeSequenceText(label))
	}

	for _, p := range participants.order {
		fmt.Fprintf(b, "  participant %s as %s\n", p.id, sanitizeSequenceText(p.name))
	}
	b.WriteString(messages.String())
	return b.String()
}

type sequenceParticipant struct {
	id   string
	name string
}

type sequenceParticipants struct {
	ids   map[string]string
	order []sequenceParticipant
}

// id returns a stable participant id for name, defaulting to a Transaction
// actor for root calls that do not name a caller.
func (p *sequenceParticipants) id(name string) string {
	if name == "" {
		name = "Transaction"
	}
	if id, ok := p.ids[name]; ok {
		return id
	}
	id := sanitizeID("p_" + name)
	for i := 2; ; i++ {
		if !p.idTaken(id) {
			break
		}
		id = sanitizeID("p_"+name) + "_" + strconv.Itoa(i)
	}
	p.ids[name] = id
	p.order = append(p.order, sequenceParticipant{id: id, name: name})
	return id
}

func (p *sequenceParticipants) idTaken(id string) bool {
	for _, existing := range p.order {
		if existing.id == id {
			return true
		}
	}
	return false
}

// sanitizeSequenceText keeps one message on one Mermaid statement: semicolons
// can terminate a statement, and raw newlines would break the diagram.
var sanitizeSequenceText = strings.NewReplacer(
	"\n", " ",
	"\r", " ",
	";", ",",
).Replace

func truncateAddr(addr string) string {
	if len(addr) > 12 {
		return addr[:4] + "..." + addr[len(addr)-4:]
	}
	return addr
}
