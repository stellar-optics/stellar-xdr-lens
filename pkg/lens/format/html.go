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
// Output is self-contained with embedded inline CSS and SVG icons: zero external scripts,
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
	f.renderHead(b, v.Type)
	f.renderHeader(b, v.Type)

	b.WriteString("<main>\n")
	if summary != nil {
		f.renderSummary(b, summary)
	}

	b.WriteString("<section class=\"tree-section\">\n")
	b.WriteString("<h2>Decoded Node Tree</h2>\n")
	b.WriteString("<div class=\"tree-container\">\n")
	f.renderNode(b, v.Node, 0)
	b.WriteString("</div>\n</section>\n")

	b.WriteString("</main>\n")
	f.renderFooter(b)

	_, err := io.WriteString(w, b.String())
	return err
}

// FormatAll renders multiple values into a single standalone HTML report.
func (f *HTMLFormatter) FormatAll(w io.Writer, values []*lens.Value) error {
	if len(values) == 0 {
		return fmt.Errorf("format: no values provided")
	}
	for i, v := range values {
		if v == nil || v.Node == nil {
			return fmt.Errorf("format: nil value at index %d", i)
		}
	}

	b := &strings.Builder{}
	typeDesc := fmt.Sprintf("%d Values", len(values))
	if len(values) == 1 {
		typeDesc = values[0].Type
	}

	f.renderHead(b, typeDesc)
	f.renderHeader(b, typeDesc)

	b.WriteString("<main>\n")
	for i, v := range values {
		fmt.Fprintf(b, "<div class=\"multi-value-header\"><h3>Value %d: <code>%s</code></h3></div>\n", i+1, html.EscapeString(v.Type))
		if s, err := lens.Explain(v); err == nil && s != nil {
			f.renderSummary(b, s)
		}
		b.WriteString("<section class=\"tree-section\">\n")
		b.WriteString("<h2>Decoded Node Tree</h2>\n")
		b.WriteString("<div class=\"tree-container\">\n")
		f.renderNode(b, v.Node, 0)
		b.WriteString("</div>\n</section>\n")
	}

	b.WriteString("</main>\n")
	f.renderFooter(b)

	_, err := io.WriteString(w, b.String())
	return err
}

// FormatSummary writes an HTML report from a precomputed lens.Summary.
func (f *HTMLFormatter) FormatSummary(w io.Writer, s *lens.Summary) error {
	if s == nil {
		return fmt.Errorf("format: nil summary")
	}

	b := &strings.Builder{}
	f.renderHead(b, s.Kind)
	f.renderHeader(b, s.Kind)

	b.WriteString("<main>\n")
	f.renderSummary(b, s)
	b.WriteString("</main>\n")
	f.renderFooter(b)

	_, err := io.WriteString(w, b.String())
	return err
}

func (f *HTMLFormatter) renderHead(b *strings.Builder, subtitle string) {
	b.WriteString("<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n")
	b.WriteString("<meta charset=\"utf-8\">\n")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	fmt.Fprintf(b, "<title>%s - %s</title>\n", html.EscapeString(f.title), html.EscapeString(subtitle))
	b.WriteString("<style>\n")
	b.WriteString(htmlStyles)
	b.WriteString("</style>\n</head>\n<body>\n")
}

func (f *HTMLFormatter) renderHeader(b *strings.Builder, badgeText string) {
	b.WriteString("<header class=\"report-header\">\n")
	b.WriteString("<div class=\"header-title\">\n")
	fmt.Fprintf(b, "<h1>%s</h1>\n", html.EscapeString(f.title))
	b.WriteString("</div>\n")
	b.WriteString("<div class=\"header-controls\">\n")
	fmt.Fprintf(b, "<span class=\"badge-type\">%s</span>\n", html.EscapeString(badgeText))
	b.WriteString("<label class=\"theme-switch\" title=\"Toggle Dark/Light Mode\">\n")
	b.WriteString("<input type=\"checkbox\" id=\"theme-toggle\" aria-label=\"Toggle dark mode\">\n")
	b.WriteString("<span class=\"theme-slider\">\n")
	fmt.Fprintf(b, "<span class=\"theme-icon icon-sun\">%s</span>\n", svgSunIcon)
	fmt.Fprintf(b, "<span class=\"theme-icon icon-moon\">%s</span>\n", svgMoonIcon)
	b.WriteString("</span>\n</label>\n")
	b.WriteString("</div>\n")
	b.WriteString("</header>\n\n")
}

func (f *HTMLFormatter) renderFooter(b *strings.Builder) {
	b.WriteString("<footer>\n<p>Generated offline by <code>stellar-xdr-lens</code></p>\n</footer>\n")
	b.WriteString(htmlThemeScript)
	b.WriteString("\n</body>\n</html>\n")
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
		icon := svgCrossCircleIcon
		if s.Outcome.Success {
			statusClass = "status-success"
			statusText = "Succeeded"
			icon = svgCheckCircleIcon
		}
		b.WriteString("<div class=\"detail-card\">\n")
		b.WriteString("<span class=\"label\">Outcome</span>\n")
		fmt.Fprintf(b, "<span class=\"badge %s\">%s %s</span>\n", statusClass, icon, statusText)
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
		fmt.Fprintf(b, "<span class=\"value monospace syn-addr\">%s</span>\n", html.EscapeString(s.Source))
		b.WriteString("</div>\n")
	}

	if s.FeeBumpSource != "" {
		b.WriteString("<div class=\"detail-card full-width\">\n")
		b.WriteString("<span class=\"label\">Fee Bump Source</span>\n")
		fmt.Fprintf(b, "<span class=\"value monospace syn-addr\">%s</span>\n", html.EscapeString(s.FeeBumpSource))
		b.WriteString("</div>\n")
	}

	if s.Fee != 0 {
		b.WriteString("<div class=\"detail-card\">\n")
		b.WriteString("<span class=\"label\">Fee Bid</span>\n")
		fmt.Fprintf(b, "<span class=\"value syn-number\">%d stroops</span>\n", s.Fee)
		b.WriteString("</div>\n")
	}

	if s.FeeCharged != 0 {
		b.WriteString("<div class=\"detail-card\">\n")
		b.WriteString("<span class=\"label\">Fee Charged</span>\n")
		fmt.Fprintf(b, "<span class=\"value syn-number\">%d stroops</span>\n", s.FeeCharged)
		b.WriteString("</div>\n")
	}

	if s.SeqNum != 0 {
		b.WriteString("<div class=\"detail-card\">\n")
		b.WriteString("<span class=\"label\">Sequence Number</span>\n")
		fmt.Fprintf(b, "<span class=\"value syn-number\">%d</span>\n", s.SeqNum)
		b.WriteString("</div>\n")
	}

	if s.Memo != "" {
		b.WriteString("<div class=\"detail-card full-width\">\n")
		b.WriteString("<span class=\"label\">Memo</span>\n")
		fmt.Fprintf(b, "<span class=\"value syn-string\">%s</span>\n", html.EscapeString(s.Memo))
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
				fmt.Fprintf(b, "<td class=\"monospace syn-addr\">%s</td>\n", html.EscapeString(op.Source))
			} else {
				b.WriteString("<td><span class=\"dim\">(envelope)</span></td>\n")
			}
			fmt.Fprintf(b, "<td>%s</td>\n", html.EscapeString(op.Detail))
			if op.Result != nil {
				resClass := "op-fail"
				badge := svgCrossCircleIcon
				if op.Result.Success {
					resClass = "op-pass"
					badge = svgCheckCircleIcon
				}
				fmt.Fprintf(b, "<td class=\"%s\">%s <code>%s</code></td>\n",
					resClass, badge, html.EscapeString(op.Result.Code))
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

	if len(n.Children) == 0 && n.Kind == lens.KindScalar {
		b.WriteString("<div class=\"xdr-leaf\">\n")
		b.WriteString(svgLeafIcon)
		if n.Name != "" {
			fmt.Fprintf(b, "<span class=\"syn-key\">%s</span>: ", html.EscapeString(n.Name))
		}
		if n.TypeName != "" {
			fmt.Fprintf(b, "<span class=\"syn-type\">&lt;%s&gt;</span> ", html.EscapeString(n.TypeName))
		}
		b.WriteString(renderValueHTML(n.Value))
		if n.Note != "" {
			fmt.Fprintf(b, " <span class=\"syn-note\">/* %s */</span>", html.EscapeString(n.Note))
		}
		b.WriteString("\n</div>\n")
		return
	}

	// Collapsible pure CSS <details> / <summary> tree structure
	b.WriteString("<details class=\"xdr-node\" open>\n")
	b.WriteString("<summary class=\"xdr-summary\">\n")
	b.WriteString(svgChevronIcon)
	b.WriteString(svgFolderIcon)
	if n.Name != "" {
		fmt.Fprintf(b, "<span class=\"syn-key\">%s</span>", html.EscapeString(n.Name))
	} else if depth == 0 {
		b.WriteString("<span class=\"syn-key\">Root</span>")
	}
	if n.TypeName != "" {
		fmt.Fprintf(b, " <span class=\"syn-type\">&lt;%s&gt;</span>", html.EscapeString(n.TypeName))
	}
	if len(n.Children) > 0 {
		fmt.Fprintf(b, " <span class=\"node-badge\">%d items</span>", len(n.Children))
	}
	if n.Note != "" {
		fmt.Fprintf(b, " <span class=\"syn-note\">/* %s */</span>", html.EscapeString(n.Note))
	}
	b.WriteString("\n</summary>\n")

	b.WriteString("<div class=\"xdr-children\">\n")
	for _, child := range n.Children {
		f.renderNode(b, child, depth+1)
	}
	b.WriteString("</div>\n")
	b.WriteString("</details>\n")
}

func renderValueHTML(val any) string {
	if val == nil {
		return `<span class="syn-null">null</span>`
	}
	s := fmt.Sprintf("%v", val)
	escaped := html.EscapeString(s)

	if isStellarAddress(s) {
		return fmt.Sprintf(`<span class="syn-addr monospace" title="Stellar Address">%s</span>`, escaped)
	}
	if isHexString(s) {
		return fmt.Sprintf(`<span class="syn-hex monospace" title="Hex Data">%s</span>`, escaped)
	}
	switch val.(type) {
	case bool:
		return fmt.Sprintf(`<span class="syn-bool">%s</span>`, escaped)
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return fmt.Sprintf(`<span class="syn-number">%s</span>`, escaped)
	}
	if s == "true" || s == "false" {
		return fmt.Sprintf(`<span class="syn-bool">%s</span>`, escaped)
	}
	if isNumeric(s) {
		return fmt.Sprintf(`<span class="syn-number">%s</span>`, escaped)
	}
	return fmt.Sprintf(`<span class="syn-string">%s</span>`, escaped)
}

func isStellarAddress(s string) bool {
	if (len(s) == 56 && (s[0] == 'G' || s[0] == 'C' || s[0] == 'B')) ||
		(len(s) == 69 && s[0] == 'M') {
		for i := 0; i < len(s); i++ {
			c := s[i]
			if !((c >= 'A' && c <= 'Z') || (c >= '2' && c <= '7')) {
				return false
			}
		}
		return true
	}
	return false
}

func isHexString(s string) bool {
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		hexPart := s[2:]
		if len(hexPart) == 0 {
			return false
		}
		for i := 0; i < len(hexPart); i++ {
			c := hexPart[i]
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				return false
			}
		}
		return true
	}
	if len(s) >= 8 && len(s)%2 == 0 {
		for i := 0; i < len(s); i++ {
			c := s[i]
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				return false
			}
		}
		return true
	}
	return false
}

func isNumeric(s string) bool {
	if len(s) == 0 {
		return false
	}
	start := 0
	if s[0] == '-' || s[0] == '+' {
		start = 1
	}
	if start >= len(s) {
		return false
	}
	hasDot := false
	for i := start; i < len(s); i++ {
		if s[i] == '.' {
			if hasDot {
				return false
			}
			hasDot = true
			continue
		}
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// Inline SVGs for self-contained, zero-external-network offline rendering.
const (
	svgChevronIcon = `<svg class="svg-chevron" viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="9 18 15 12 9 6"></polyline></svg>`

	svgFolderIcon = `<svg class="svg-icon icon-folder" viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"></path></svg>`

	svgLeafIcon = `<svg class="svg-icon icon-leaf" viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="4" y1="9" x2="20" y2="9"></line><line x1="4" y1="15" x2="20" y2="15"></line><line x1="10" y1="3" x2="8" y2="21"></line><line x1="16" y1="3" x2="14" y2="21"></line></svg>`

	svgSunIcon = `<svg class="svg-icon" viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="5"></circle><line x1="12" y1="1" x2="12" y2="3"></line><line x1="12" y1="21" x2="12" y2="23"></line><line x1="4.22" y1="4.22" x2="5.64" y2="5.64"></line><line x1="18.36" y1="18.36" x2="19.78" y2="19.78"></line><line x1="1" y1="12" x2="3" y2="12"></line><line x1="21" y1="12" x2="23" y2="12"></line><line x1="4.22" y1="19.78" x2="5.64" y2="18.36"></line><line x1="18.36" y1="5.64" x2="19.78" y2="4.22"></line></svg>`

	svgMoonIcon = `<svg class="svg-icon" viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"></path></svg>`

	svgCheckCircleIcon = `<svg class="svg-icon icon-pass" viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"></path><polyline points="22 4 12 14.01 9 11.01"></polyline></svg>`

	svgCrossCircleIcon = `<svg class="svg-icon icon-fail" viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"></circle><line x1="15" y1="9" x2="9" y2="15"></line><line x1="9" y1="9" x2="15" y2="15"></line></svg>`
)

const htmlStyles = `
:root {
	--bg-body: #f6f8fa;
	--bg-card: #ffffff;
	--bg-subtle: #f8f9fa;
	--text-primary: #1f2328;
	--text-secondary: #656d76;
	--border-color: #d0d7de;
	--border-subtle: #eaecef;
	--accent-color: #0969da;
	--syn-key: #0550ae;
	--syn-type: #8250df;
	--syn-val: #1a7f37;
	--syn-addr: #0969da;
	--syn-hex: #953800;
	--syn-bool: #cf222e;
	--syn-number: #0550ae;
	--syn-string: #0a3069;
	--syn-null: #8c959f;
	--syn-note: #6e7781;
	--badge-bg: #24292e;
	--badge-text: #ffffff;
	--status-pass-bg: #dafbe1;
	--status-pass-text: #1a7f37;
	--status-fail-bg: #ffebe9;
	--status-fail-text: #cf222e;
	--card-shadow: 0 1px 3px rgba(0,0,0,0.06);
	--toggle-bg: #d0d7de;
}

[data-theme="dark"], body:has(#theme-toggle:checked) {
	--bg-body: #0d1117;
	--bg-card: #161b22;
	--bg-subtle: #21262d;
	--text-primary: #e6edf3;
	--text-secondary: #8b949e;
	--border-color: #30363d;
	--border-subtle: #21262d;
	--accent-color: #2f81f7;
	--syn-key: #79c0ff;
	--syn-type: #d2a8ff;
	--syn-val: #7ee787;
	--syn-addr: #58a6ff;
	--syn-hex: #ffa657;
	--syn-bool: #ff7b72;
	--syn-number: #79c0ff;
	--syn-string: #a5d6ff;
	--syn-null: #6e7681;
	--syn-note: #8b949e;
	--badge-bg: #30363d;
	--badge-text: #e6edf3;
	--status-pass-bg: #1f4728;
	--status-pass-text: #3fb950;
	--status-fail-bg: #4e191d;
	--status-fail-text: #f85149;
	--card-shadow: 0 1px 3px rgba(0,0,0,0.3);
	--toggle-bg: #30363d;
}

@media (prefers-color-scheme: dark) {
	:root:not([data-theme="light"]) {
		--bg-body: #0d1117;
		--bg-card: #161b22;
		--bg-subtle: #21262d;
		--text-primary: #e6edf3;
		--text-secondary: #8b949e;
		--border-color: #30363d;
		--border-subtle: #21262d;
		--accent-color: #2f81f7;
		--syn-key: #79c0ff;
		--syn-type: #d2a8ff;
		--syn-val: #7ee787;
		--syn-addr: #58a6ff;
		--syn-hex: #ffa657;
		--syn-bool: #ff7b72;
		--syn-number: #79c0ff;
		--syn-string: #a5d6ff;
		--syn-null: #6e7681;
		--syn-note: #8b949e;
		--badge-bg: #30363d;
		--badge-text: #e6edf3;
		--status-pass-bg: #1f4728;
		--status-pass-text: #3fb950;
		--status-fail-bg: #4e191d;
		--status-fail-text: #f85149;
		--card-shadow: 0 1px 3px rgba(0,0,0,0.3);
		--toggle-bg: #30363d;
	}
}

body {
	font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
	line-height: 1.6;
	color: var(--text-primary);
	background-color: var(--bg-body);
	margin: 0;
	padding: 24px;
	transition: background-color 0.2s, color 0.2s;
}

main {
	max-width: 1100px;
	margin: 0 auto;
}

header.report-header {
	max-width: 1100px;
	margin: 0 auto 24px auto;
	padding-bottom: 16px;
	border-bottom: 2px solid var(--border-color);
	display: flex;
	align-items: center;
	justify-content: space-between;
}

header h1 {
	margin: 0;
	font-size: 1.8rem;
	color: var(--accent-color);
}

.header-controls {
	display: flex;
	align-items: center;
	gap: 16px;
}

.badge-type {
	background-color: var(--badge-bg);
	color: var(--badge-text);
	font-family: ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace;
	font-size: 0.9rem;
	padding: 4px 10px;
	border-radius: 6px;
	font-weight: 600;
}

.theme-switch {
	position: relative;
	display: inline-flex;
	align-items: center;
	cursor: pointer;
}

.theme-switch input {
	position: absolute;
	opacity: 0;
	width: 0;
	height: 0;
}

.theme-slider {
	display: flex;
	align-items: center;
	justify-content: space-between;
	width: 48px;
	height: 24px;
	background-color: var(--toggle-bg);
	border-radius: 12px;
	padding: 2px 4px;
	box-sizing: border-box;
	transition: background-color 0.2s;
}

.theme-icon {
	display: inline-flex;
	align-items: center;
	justify-content: center;
	color: var(--text-secondary);
}

section {
	background: var(--bg-card);
	border-radius: 8px;
	border: 1px solid var(--border-color);
	padding: 24px;
	margin-bottom: 24px;
	box-shadow: var(--card-shadow);
}

h2 {
	margin-top: 0;
	margin-bottom: 16px;
	font-size: 1.4rem;
	border-bottom: 1px solid var(--border-subtle);
	padding-bottom: 8px;
}

h3 {
	margin-top: 20px;
	margin-bottom: 12px;
	font-size: 1.1rem;
}

.multi-value-header {
	margin-top: 24px;
	margin-bottom: 8px;
}

.headline-card {
	background: var(--bg-subtle);
	border-left: 4px solid var(--accent-color);
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
	background: var(--bg-subtle);
	border: 1px solid var(--border-subtle);
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
	color: var(--text-secondary);
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
	display: inline-flex;
	align-items: center;
	gap: 4px;
	padding: 2px 8px;
	border-radius: 4px;
	font-weight: 600;
	font-size: 0.85rem;
	width: fit-content;
}

.status-success {
	background-color: var(--status-pass-bg);
	color: var(--status-pass-text);
}

.status-failed {
	background-color: var(--status-fail-bg);
	color: var(--status-fail-text);
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
	border: 1px solid var(--border-color);
	padding: 8px 12px;
	text-align: left;
}

table.ops-table th {
	background-color: var(--bg-subtle);
	font-weight: 600;
}

.op-pass {
	color: var(--status-pass-text);
	font-weight: 600;
}

.op-fail {
	color: var(--status-fail-text);
	font-weight: 600;
}

.dim {
	color: var(--text-secondary);
}

.preconds-card {
	background: var(--bg-subtle);
	border: 1px solid var(--border-color);
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
	background: var(--bg-subtle);
	border: 1px solid var(--border-color);
	border-radius: 6px;
	padding: 16px;
	overflow-x: auto;
}

details.xdr-node {
	margin: 2px 0;
}

summary.xdr-summary {
	display: flex;
	align-items: center;
	gap: 6px;
	padding: 4px 6px;
	border-radius: 4px;
	cursor: pointer;
	user-select: none;
	list-style: none;
}

summary.xdr-summary::-webkit-details-marker {
	display: none;
}

summary.xdr-summary:hover {
	background-color: var(--border-subtle);
}

.svg-chevron {
	transition: transform 0.15s ease-in-out;
	flex-shrink: 0;
	color: var(--text-secondary);
}

details[open] > summary.xdr-summary > .svg-chevron {
	transform: rotate(90deg);
}

.svg-icon {
	flex-shrink: 0;
}

.icon-folder {
	color: var(--syn-key);
}

.icon-leaf {
	color: var(--text-secondary);
}

.xdr-children {
	margin-left: 18px;
	padding-left: 8px;
	border-left: 1px dashed var(--border-color);
}

.xdr-leaf {
	display: flex;
	align-items: center;
	gap: 6px;
	padding: 3px 6px;
	margin-left: 18px;
	border-radius: 4px;
}

.xdr-leaf:hover {
	background-color: var(--border-subtle);
}

.node-badge {
	background-color: var(--border-subtle);
	color: var(--text-secondary);
	font-size: 0.75rem;
	padding: 1px 6px;
	border-radius: 10px;
	font-weight: 500;
}

.syn-key {
	color: var(--syn-key);
	font-weight: 600;
}

.syn-type {
	color: var(--syn-type);
}

.syn-addr {
	color: var(--syn-addr);
	font-weight: 500;
}

.syn-hex {
	color: var(--syn-hex);
	font-weight: 500;
}

.syn-number {
	color: var(--syn-number);
}

.syn-string {
	color: var(--syn-string);
}

.syn-bool {
	color: var(--syn-bool);
	font-weight: 600;
}

.syn-null {
	color: var(--syn-null);
	font-style: italic;
}

.syn-note {
	color: var(--syn-note);
	font-style: italic;
}

footer {
	text-align: center;
	color: var(--text-secondary);
	font-size: 0.85rem;
	margin-top: 32px;
}
`

const htmlThemeScript = `<script>
(function(){
	try {
		var t = document.getElementById('theme-toggle');
		if (!t) return;
		var saved = localStorage.getItem('stellar-xdr-lens-theme');
		if (saved === 'dark' || (!saved && window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches)) {
			t.checked = true;
			document.documentElement.setAttribute('data-theme', 'dark');
		}
		t.addEventListener('change', function() {
			var mode = t.checked ? 'dark' : 'light';
			document.documentElement.setAttribute('data-theme', mode);
			localStorage.setItem('stellar-xdr-lens-theme', mode);
		});
	} catch(e) {}
})();
</script>`
