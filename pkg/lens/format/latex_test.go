package format_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens/format"
)

func TestLaTeXFormatterEscapesSpecialCharacters(t *testing.T) {
	t.Parallel()

	amount := int32(100)
	value := &lens.Value{
		Type: "Custom",
		Node: &lens.Node{
			Name:     "root",
			TypeName: "Custom",
			Kind:     lens.KindStruct,
			Children: []*lens.Node{
				{Name: "memo_text", Kind: lens.KindScalar, Value: "50% of $100 & #1_thing"},
				{Name: "path\\to", Kind: lens.KindScalar, Value: "a^b~c{1}"},
				{Name: "amount", Kind: lens.KindScalar, Value: amount},
			},
		},
	}

	var buf bytes.Buffer
	if err := format.NewLaTeXFormatter().Format(&buf, value); err != nil {
		t.Fatalf("Format: %v", err)
	}
	out := buf.String()

	for _, want := range []string{
		`memo\_text`,
		`50\% of \$100 \& \#1\_thing`,
		`path\textbackslash{}to`,
		`a\textasciicircum{}b\textasciitilde{}c\{1\}`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing escaped %q:\n%s", want, out)
		}
	}
	for _, raw := range []string{"memo_text", "50% of", "#1_thing", "path\\to"} {
		if strings.Contains(out, raw) {
			t.Errorf("output contains unescaped %q:\n%s", raw, out)
		}
	}
}

func TestLaTeXFormatterBooktabsStructure(t *testing.T) {
	t.Parallel()

	value := &lens.Value{
		Type: "TrustlineEntry",
		Node: &lens.Node{
			TypeName: "TrustlineEntry",
			Kind:     lens.KindStruct,
			Children: []*lens.Node{
				{Name: "limit", Kind: lens.KindScalar, Value: int64(1000)},
			},
		},
	}

	var buf bytes.Buffer
	if err := format.NewLaTeXFormatter(format.WithLaTeXCaption("A & B")).Format(&buf, value); err != nil {
		t.Fatalf("Format: %v", err)
	}
	out := buf.String()

	for _, want := range []string{
		`\begin{table}[htbp]`,
		`\centering`,
		`\caption{A \& B}`,
		`\begin{tabular}{ll}`,
		`\toprule`,
		`Field & Value \\`,
		`\midrule`,
		`\bottomrule`,
		`\end{tabular}`,
		`\end{table}`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}

	// booktabs requires this order: top rule, header, mid rule, body,
	// bottom rule.
	order := []string{`\toprule`, `Field & Value`, `\midrule`, `\bottomrule`}
	last := -1
	for _, marker := range order {
		idx := strings.Index(out, marker)
		if idx <= last {
			t.Fatalf("marker %q out of order in:\n%s", marker, out)
		}
		last = idx
	}
}

func TestLaTeXFormatterTransactionSummary(t *testing.T) {
	t.Parallel()

	env := loadValue(t, "tx_success.env")
	summary, err := lens.ExplainPair(env, loadValue(t, "tx_success.res"))
	if err != nil {
		t.Fatalf("ExplainPair: %v", err)
	}

	var buf bytes.Buffer
	if err := format.NewLaTeXFormatter().Format(&buf, env); err != nil {
		t.Fatalf("Format: %v", err)
	}
	out := buf.String()

	for _, want := range []string{
		`\caption{TransactionEnvelope}`,
		summary.Source,
		"Operation 0",
		`invoke\_host\_function`,
		`\texttt{`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestLaTeXFormatterRejectsNilValue(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := format.NewLaTeXFormatter().Format(&buf, nil); err == nil {
		t.Fatal("Format(nil) = nil error, want error")
	}
}
