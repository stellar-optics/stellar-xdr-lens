package format

import (
	"fmt"
	"io"
	"strings"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

// TreeFormatter renders a value as an indented, optionally colourised tree
// using box-drawing characters.
type TreeFormatter struct {
	palette  Palette
	maxDepth int
	showType bool
	explorer string
}

// TreeOption configures a TreeFormatter.
type TreeOption func(*TreeFormatter)

// WithPalette sets the colour palette. Use NoColor to disable colour.
func WithPalette(p Palette) TreeOption {
	return func(t *TreeFormatter) { t.palette = p }
}

// WithMaxDepth limits how deep the tree is rendered. Nodes below the limit
// are summarised. A value of 0 or less means unlimited.
func WithMaxDepth(d int) TreeOption {
	return func(t *TreeFormatter) { t.maxDepth = d }
}

// WithTypeNames shows the originating XDR type alongside each struct node.
func WithTypeNames(show bool) TreeOption {
	return func(t *TreeFormatter) { t.showType = show }
}

// WithHyperlinks makes strkey addresses clickable in OSC 8 terminals, linking
// them to the Stellar Expert network identified by base (see
// ExplorerBaseURL). Links are emitted only when the palette is coloured, so
// `--color never` and non-TTY output stay free of escape sequences.
func WithHyperlinks(base string) TreeOption {
	return func(t *TreeFormatter) { t.explorer = base }
}

// NewTreeFormatter returns a TreeFormatter. By default it renders without
// colour and without depth limit; pass options to change that.
func NewTreeFormatter(opts ...TreeOption) *TreeFormatter {
	t := &TreeFormatter{palette: NoColor}
	for _, o := range opts {
		o(t)
	}
	return t
}

// Format implements Formatter.
func (t *TreeFormatter) Format(w io.Writer, v *lens.Value) error {
	if v == nil || v.Node == nil {
		return fmt.Errorf("format: nil value")
	}
	p := t.palette
	if _, err := fmt.Fprintf(w, "%s%s%s\n", p.Type, v.Type, p.Reset); err != nil {
		return fmt.Errorf("format: write header: %w", err)
	}
	return t.writeChildren(w, v.Node, "", 1)
}

func (t *TreeFormatter) writeChildren(w io.Writer, n *lens.Node, prefix string, depth int) error {
	for i, c := range n.Children {
		last := i == len(n.Children)-1
		if err := t.writeNode(w, c, prefix, last, depth, i); err != nil {
			return err
		}
	}
	return nil
}

func (t *TreeFormatter) writeNode(w io.Writer, n *lens.Node, prefix string, last bool, depth, index int) error {
	p := t.palette

	branch, childPrefix := "├─ ", prefix+"│  "
	if last {
		branch, childPrefix = "└─ ", prefix+"   "
	}

	label := n.Name
	if label == "" {
		label = fmt.Sprintf("[%d]", index)
	}

	var line strings.Builder
	line.WriteString(prefix)
	line.WriteString(p.Dim)
	line.WriteString(branch)
	line.WriteString(p.Reset)
	line.WriteString(p.Key)
	line.WriteString(label)
	line.WriteString(p.Reset)

	switch n.Kind {
	case lens.KindScalar:
		line.WriteString(": ")
		line.WriteString(t.scalar(n))
	case lens.KindBytes:
		line.WriteString(": ")
		line.WriteString(t.scalar(n))
	case lens.KindList:
		line.WriteString(p.Dim)
		fmt.Fprintf(&line, "  [%d]", len(n.Children))
		line.WriteString(p.Reset)
	default:
		if t.showType && n.TypeName != "" {
			line.WriteString(p.Dim)
			line.WriteString("  " + n.TypeName)
			line.WriteString(p.Reset)
		}
	}

	if _, err := fmt.Fprintln(w, line.String()); err != nil {
		return fmt.Errorf("format: write node: %w", err)
	}

	if n.Kind == lens.KindScalar || n.Kind == lens.KindBytes || len(n.Children) == 0 {
		return nil
	}
	if t.maxDepth > 0 && depth >= t.maxDepth {
		if _, err := fmt.Fprintf(w, "%s%s... %d more level(s) hidden%s\n",
			childPrefix, p.Dim, 1, p.Reset); err != nil {
			return fmt.Errorf("format: write elision: %w", err)
		}
		return nil
	}
	return t.writeChildren(w, n, childPrefix, depth+1)
}

// scalar renders a leaf value with type-appropriate colour and appends any
// enricher note, which is where strkey addresses and formatted amounts show up.
func (t *TreeFormatter) scalar(n *lens.Node) string {
	p := t.palette
	var b strings.Builder

	switch val := n.Value.(type) {
	case nil:
		b.WriteString(p.Dim + "(none)" + p.Reset)
	case string:
		b.WriteString(p.Str + hyperlinkAddress(p, t.explorer, val) + p.Reset)
	case bool:
		b.WriteString(p.Num + fmt.Sprintf("%t", val) + p.Reset)
	default:
		b.WriteString(p.Num + fmt.Sprintf("%v", val) + p.Reset)
	}

	if n.Note != "" {
		b.WriteString(p.Note + "  " + n.Note + p.Reset)
	}
	return b.String()
}

// FormatAuthInvocation formats an authorized invocation hierarchy to w.
func (t *TreeFormatter) FormatAuthInvocation(w io.Writer, inv xdr.SorobanAuthorizedInvocation) error {
	rendered := lens.FormatAuthTree(inv)
	_, err := fmt.Fprintln(w, rendered)
	return err
}

// FormatAuthEntries formats a slice of SorobanAuthorizationEntry trees to w.
func (t *TreeFormatter) FormatAuthEntries(w io.Writer, entries []xdr.SorobanAuthorizationEntry) error {
	rendered := lens.FormatAuthorizationEntries(entries)
	_, err := fmt.Fprintln(w, rendered)
	return err
}

// RenderAuthHierarchy renders an authorized invocation hierarchy as an indented ASCII tree.
func RenderAuthHierarchy(inv xdr.SorobanAuthorizedInvocation) string {
	return lens.FormatAuthTree(inv)
}

// RenderAuthTree renders an authorized invocation hierarchy as an indented ASCII tree.
func RenderAuthTree(inv xdr.SorobanAuthorizedInvocation) string {
	return lens.FormatAuthTree(inv)
}
