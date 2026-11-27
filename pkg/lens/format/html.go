package format

import (
	"fmt"
	"html"
	"io"
	"strings"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

// HTMLFormatter renders decoded XDR values as a standalone, styled HTML document.
//
// Output is self-contained with embedded inline CSS: zero external scripts,
// fonts or stylesheets are referenced, maintaining strict offline-first guarantees.
type HTMLFormatter struct {
	title string
}

// HTMLOption configures an HTMLFormatter.
type HTMLOption func(*HTMLFormatter)

// WithHTMLTitle sets the document title and top-level header.
func WithHTMLTitle(title string) HTMLOption {
	return func(h *HTMLFormatter) {
		h.title = title
	}
}

// NewHTMLFormatter returns an HTMLFormatter with sensible defaults.
func NewHTMLFormatter(opts ...HTMLOption) *HTMLFormatter {
	h := &HTMLFormatter{
		title: "Stellar XDR Lens Report",
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Format renders v as a standalone HTML page written to w.
func (f *HTMLFormatter) Format(w io.Writer, v *lens.Value) error {
	if v == nil || v.Node == nil {
		return fmt.Errorf("format: nil value")
	}

	var summary *lens.Summary
	if s, err := lens.Explain(v); err == nil {
		summary = s
	}

	b := &strings.Builder{}
	b.WriteString("<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n")
	b.WriteString("<meta charset=\"utf-8\">\n")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	fmt.Fprintf(b, "<title>%s - %s</title>\n", html.EscapeString(f.title), html.EscapeString(v.Type))
	b.WriteString("<style>\n")
	b.WriteString(htmlStyles)
	b.WriteString("</style>\n</head>\n<body>\n")

	b.WriteString("<header class=\"report-header\">\n")
	fmt.Fprintf(b, "<h1>%s</h1>\n", html.EscapeString(f.title))
	fmt.Fprintf(b, "<div class=\"badge-type\">%s</div>\n", html.EscapeString(v.Type))
	b.WriteString("</header>\n\n<main>\n")

	if summary != nil {
		f.renderSummary(b, summary)
	}

	b.WriteString("<section class=\"tree-section\">\n")
	b.WriteString("<h2>Decoded Node Tree</h2>\n")
	b.WriteString("<div class=\"tree-container\">\n")
	f.renderNode(b, v.Node, 0)
	b.WriteString("</div>\n</section>\n")

	b.WriteString("</main>\n")
	b.WriteString("<footer>\n<p>Generated offline by <code>stellar-xdr-lens</code></p>\n</footer>\n")
	b.WriteString("</body>\n</html>\n")

	_, err := io.WriteString(w, b.String())
	return err
}

// FormatSummary writes an HTML report from a precomputed lens.Summary.
func (f *HTMLFormatter) FormatSummary(w io.Writer, s *lens.Summary) error {
	if s == nil {
		return fmt.Errorf("format: nil summary")
	}

	b := &strings.Builder{}
	b.WriteString("<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n")
	b.WriteString("<meta charset=\"utf-8\">\n")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	fmt.Fprintf(b, "<title>%s - %s</title>\n", html.EscapeString(f.title), html.EscapeString(s.Kind))
	b.WriteString("<style>\n")
	b.WriteString(htmlStyles)
	b.WriteString("</style>\n</head>\n<body>\n")

	b.WriteString("<header class=\"report-header\">\n")
	fmt.Fprintf(b, "<h1>%s</h1>\n", html.EscapeString(f.title))
	fmt.Fprintf(b, "<div class=\"badge-type\">%s</div>\n", html.EscapeString(s.Kind))
	b.WriteString("</header>\n\n<main>\n")

	f.renderSummary(b, s)

	b.WriteString("</main>\n")
	b.WriteString("<footer>\n<p>Generated offline by <code>stellar-xdr-lens</code></p>\n</footer>\n")
	b.WriteString("</body>\n</html>\n")

	_, err := io.WriteString(w, b.String())
	return err
}

func (f *HTMLFormatter) renderSummary(b *strings.Builder, s *lens.Summary) {
	b.WriteString("<section class=\"summary-section\">\n")
	b.WriteString("<h2>Transaction Overview</h2>\n")

	if s.Headline != "" {
		b.WriteString("<div class=\"headline-card\">\n")
		fmt.Fprintf(b, "<p class=\"headline\">%s</p>\n", html.EscapeString(s.Headline))
		b.WriteString("</div>\n")
	}

	b.WriteString("<div class=\"grid-details\">\n")
	if s.Outcome != nil {
		statusClass := "status-failed"
		statusText := "Failed"
		if s.Outcome.Success {
			statusClass = "status-success"
			statusText = "Succeeded"
		}
		b.WriteString("<div class=\"detail-card\">\n")
		b.WriteString("<span class=\"label\">Outcome</span>\n")
		fmt.Fprintf(b, "<span class=\"badge %s\">%s</span>\n", statusClass, statusText)
		b.WriteString("</div>\n")

		b.WriteString("<div class=\"detail-card full-width\">\n")
		b.WriteString("<span class=\"label\">Result Reason</span>\n")
		fmt.Fprintf(b, "<span class=\"value\"><code>%s</code>: %s</span>\n",
			html.EscapeString(s.Outcome.Reason.Code),
			html.EscapeString(s.Outcome.Reason.Summary),
		)
		b.WriteString("</div>\n")
	}

	if s.Source != "" {
		b.WriteString("<div class=\"detail-card full-width\">\n")
		b.WriteString("<span class=\"label\">Source Account</span>\n")
		fmt.Fprintf(b, "<span class=\"value monospace\">%s</span>\n", html.EscapeString(s.Source))
		b.WriteString("</div>\n")
	}

	if s.FeeBumpSource != "" {
		b.WriteString("<div class=\"detail-card full-width\">\n")
		b.WriteString("<span class=\"label\">Fee Bump Source</span>\n")
		fmt.Fprintf(b, "<span class=\"value monospace\">%s</span>\n", html.EscapeString(s.FeeBumpSource))
		b.WriteString("</div>\n")
	}

	if s.Fee != 0 {
		b.WriteString("<div class=\"detail-card\">\n")
		b.WriteString("<span class=\"label\">Fee Bid</span>\n")
		fmt.Fprintf(b, "<span class=\"value\">%d stroops</span>\n", s.Fee)
		b.WriteString("</div>\n")
	}

	if s.FeeCharged != 0 {
		b.WriteString("<div class=\"detail-card\">\n")
		b.WriteString("<span class=\"label\">Fee Charged</span>\n")
		fmt.Fprintf(b, "<span class=\"value\">%d stroops</span>\n", s.FeeCharged)
		b.WriteString("</div>\n")
	}

	if s.SeqNum != 0 {
		b.WriteString("<div class=\"detail-card\">\n")
		b.WriteString("<span class=\"label\">Sequence Number</span>\n")
		fmt.Fprintf(b, "<span class=\"value\">%d</span>\n", s.SeqNum)
		b.WriteString("</div>\n")
	}

	if s.Memo != "" {
		b.WriteString("<div class=\"detail-card full-width\">\n")
		b.WriteString("<span class=\"label\">Memo</span>\n")
		fmt.Fprintf(b, "<span class=\"value\">%s</span>\n", html.EscapeString(s.Memo))
		b.WriteString("</div>\n")
	}

	b.WriteString("</div>\n") // grid-details

	if len(s.Preconds) > 0 {
		b.WriteString("<div class=\"preconds-card\">\n")
		b.WriteString("<h3>Preconditions</h3>\n<ul>\n")
		for _, pc := range s.Preconds {
			fmt.Fprintf(b, "<li>%s</li>\n", html.EscapeString(pc))
		}
		b.WriteString("</ul>\n</div>\n")
	}

	if len(s.Operations) > 0 {
		b.WriteString("<h3>Operations</h3>\n")
		b.WriteString("<div class=\"table-responsive\">\n")
		b.WriteString("<table class=\"ops-table\">\n")
		b.WriteString("<thead><tr><th>#</th><th>Type</th><th>Source</th><th>Details</th><th>Result</th></tr></thead>\n<tbody>\n")
		for _, op := range s.Operations {
			b.WriteString("<tr>\n")
			fmt.Fprintf(b, "<td>%d</td>\n", op.Index)
			fmt.Fprintf(b, "<td><code>%s</code></td>\n", html.EscapeString(op.Type))
			if op.Source != "" {
				fmt.Fprintf(b, "<td class=\"monospace\">%s</td>\n", html.EscapeString(op.Source))
			} else {
				b.WriteString("<td><span class=\"dim\">(envelope)</span></td>\n")
			}
			fmt.Fprintf(b, "<td>%s</td>\n", html.EscapeString(op.Detail))
			if op.Result != nil {
				resClass := "op-fail"
				resBadge := "✗"
				if op.Result.Success {
					resClass = "op-pass"
					resBadge = "✓"
				}
				fmt.Fprintf(b, "<td class=\"%s\">%s <code>%s</code></td>\n",
					resClass, resBadge, html.EscapeString(op.Result.Code))
			} else {
				b.WriteString("<td><span class=\"dim\">-</span></td>\n")
			}
			b.WriteString("</tr>\n")
		}
		b.WriteString("</tbody>\n</table>\n</div>\n")
	}

	b.WriteString("</section>\n")
}

func (f *HTMLFormatter) renderNode(b *strings.Builder, n *lens.Node, depth int) {
	if n == nil {
		return
	}

	b.WriteString("<div class=\"node-row\">\n")
	if n.Name != "" {
		fmt.Fprintf(b, "<span class=\"node-key\">%s</span>: ", html.EscapeString(n.Name))
	}
	if n.TypeName != "" {
		fmt.Fprintf(b, "<span class=\"node-type\">&lt;%s&gt;</span> ", html.EscapeString(n.TypeName))
	}

	if n.Kind == lens.KindScalar || n.Kind == lens.KindBytes {
		valStr := fmt.Sprintf("%v", n.Value)
		fmt.Fprintf(b, "<span class=\"node-val\">%s</span>", html.EscapeString(valStr))
		if n.Note != "" {
			fmt.Fprintf(b, " <span class=\"node-note\">/* %s */</span>", html.EscapeString(n.Note))
		}
		b.WriteString("\n</div>\n")
		return
	}

	b.WriteString("\n")
	if len(n.Children) > 0 {
		b.WriteString("<div class=\"node-children\">\n")
		for _, child := range n.Children {
			f.renderNode(b, child, depth+1)
		}
		b.WriteString("</div>\n")
	}
	b.WriteString("</div>\n")
}

const htmlStyles = `
body {
	font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
	line-height: 1.6;
	color: #24292e;
	background-color: #f6f8fa;
	margin: 0;
	padding: 24px;
}
main {
	max-width: 1100px;
	margin: 0 auto;
}
header.report-header {
	max-width: 1100px;
	margin: 0 auto 24px auto;
	padding-bottom: 16px;
	border-bottom: 2px solid #e1e4e8;
	display: flex;
	align-items: center;
	justify-content: space-between;
}
header h1 {
	margin: 0;
	font-size: 1.8rem;
	color: #0366d6;
}
.badge-type {
	background-color: #24292e;
	color: #ffffff;
	font-family: ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace;
	font-size: 0.9rem;
	padding: 4px 10px;
	border-radius: 6px;
	font-weight: 600;
}
section {
	background: #ffffff;
	border-radius: 8px;
	border: 1px solid #e1e4e8;
	padding: 24px;
	margin-bottom: 24px;
	box-shadow: 0 1px 3px rgba(0,0,0,0.05);
}
h2 {
	margin-top: 0;
	margin-bottom: 16px;
	font-size: 1.4rem;
	border-bottom: 1px solid #eaecef;
	padding-bottom: 8px;
}
h3 {
	margin-top: 20px;
	margin-bottom: 12px;
	font-size: 1.1rem;
}
.headline-card {
	background: #e1f5fe;
	border-left: 4px solid #0288d1;
	padding: 12px 16px;
	border-radius: 4px;
	margin-bottom: 16px;
}
p.headline {
	margin: 0;
	font-weight: 500;
}
.grid-details {
	display: grid;
	grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
	gap: 12px;
	margin-bottom: 16px;
}
.detail-card {
	background: #f8f9fa;
	border: 1px solid #e9ecef;
	padding: 10px 14px;
	border-radius: 6px;
	display: flex;
	flex-direction: column;
}
.detail-card.full-width {
	grid-column: 1 / -1;
}
.detail-card .label {
	font-size: 0.75rem;
	text-transform: uppercase;
	color: #586069;
	font-weight: 600;
	margin-bottom: 4px;
}
.detail-card .value {
	font-size: 0.95rem;
	word-break: break-all;
}
.monospace {
	font-family: ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace;
}
.badge {
	display: inline-block;
	padding: 2px 8px;
	border-radius: 4px;
	font-weight: 600;
	font-size: 0.85rem;
	width: fit-content;
}
.status-success {
	background-color: #dcffe4;
	color: #155724;
}
.status-failed {
	background-color: #ffdce0;
	color: #cb2431;
}
.table-responsive {
	overflow-x: auto;
}
table.ops-table {
	width: 100%;
	border-collapse: collapse;
	margin-top: 8px;
	font-size: 0.9rem;
}
table.ops-table th, table.ops-table td {
	border: 1px solid #e1e4e8;
	padding: 8px 12px;
	text-align: left;
}
table.ops-table th {
	background-color: #f6f8fa;
	font-weight: 600;
}
.op-pass {
	color: #22863a;
	font-weight: 600;
}
.op-fail {
	color: #cb2431;
	font-weight: 600;
}
.dim {
	color: #6a737d;
}
.preconds-card {
	background: #fafbfc;
	border: 1px solid #e1e4e8;
	border-radius: 6px;
	padding: 12px 16px;
	margin-bottom: 16px;
}
.preconds-card ul {
	margin: 0;
	padding-left: 20px;
}
.tree-container {
	font-family: ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace;
	font-size: 0.85rem;
	background: #fafbfc;
	border: 1px solid #e1e4e8;
	border-radius: 6px;
	padding: 16px;
	overflow-x: auto;
}
.node-row {
	margin-left: 8px;
	padding: 2px 0;
}
.node-children {
	margin-left: 20px;
	border-left: 1px dashed #d1d5da;
	padding-left: 8px;
}
.node-key {
	color: #005cc5;
	font-weight: 600;
}
.node-type {
	color: #6f42c1;
}
.node-val {
	color: #22863a;
}
.node-note {
	color: #6a737d;
	font-style: italic;
}
footer {
	text-align: center;
	color: #6a737d;
	font-size: 0.85rem;
	margin-top: 32px;
}
`
