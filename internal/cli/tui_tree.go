package cli

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens/format"
)

// treeRow represents a single visible line in the interactive tree explorer.
type treeRow struct {
	node        *lens.Node
	parent      *lens.Node
	path        string
	depth       int
	isLast      bool
	hasChildren bool
	expanded    bool
}

// TUITree is an interactive expandable terminal tree explorer for XDR structures.
//
// It supports keyboard navigation, expansion and an incremental search bar that
// filters the tree to matching nodes and their ancestors.
type TUITree struct {
	valueType     string
	root          *lens.Node
	expandedState map[*lens.Node]bool
	rows          []*treeRow
	cursor        int
	scrollOffset  int
	height        int
	width         int
	filter        string
	matches       []*lens.Node
	matchSet      map[*lens.Node]bool
	visible       map[*lens.Node]bool
	matchIdx      int
	searchMode    bool
	savedExpanded map[*lens.Node]bool
	savedCursor   *lens.Node
	palette       format.Palette
	in            io.Reader
	out           io.Writer
	quit          bool
	statusMsg     string
	valueType        string
	root             *lens.Node
	expandedState    map[*lens.Node]bool
	rows             []*treeRow
	cursor           int
	scrollOffset     int
	height           int
	width            int
	filter           string
	searchMode       bool
	searchBuffer     string
	searchQuery      string
	searchMatches    []int
	searchIndex      int
	showHelp         bool
	helpVisible      bool
	showInspector    bool
	inspectorVisible bool
	palette          format.Palette
	in               io.Reader
	out              io.Writer
	quit             bool
	statusMsg        string
}

// NewTUITree initializes an interactive tree explorer for value.
func NewTUITree(v *lens.Value, in io.Reader, out io.Writer, p format.Palette) *TUITree {
	t := &TUITree{
		valueType:     v.Type,
		root:          v.Node,
		expandedState: make(map[*lens.Node]bool),
		height:        24,
		width:         80,
		palette:       p,
		in:            in,
		out:           out,
	}

	// Expand root and immediate children by default for initial context
	if t.root != nil {
		t.expandedState[t.root] = true
		for _, c := range t.root.Children {
			if len(c.Children) > 0 {
				t.expandedState[c] = true
			}
		}
	}

	t.rebuildRows()
	return t
}

// SetDimensions sets the viewport dimensions for scrolling.
func (t *TUITree) SetDimensions(width, height int) {
	if width > 0 {
		t.width = width
	}
	if height > 0 {
		t.height = height
	}
}

// rebuildRows flattens visible nodes based on expansion state and the active filter.
// HandleResize updates dimensions on terminal resize.
func (t *TUITree) HandleResize(width, height int) {
	t.SetDimensions(width, height)
}

// rebuildRows flattens visible nodes based on expansion state and filters.
func (t *TUITree) rebuildRows() {
	if t.root == nil {
		t.rows = nil
		return
	}

	var rows []*treeRow
	t.walk(t.root, nil, "", 0, true, &rows)

	if t.filter != "" {
		filtered := rows[:0]
		for _, r := range rows {
			if t.visible[r.node] {
				filtered = append(filtered, r)
			}
		}
		rows = filtered
	}

	t.rows = rows
	if t.searchQuery != "" {
		t.searchMatches = t.searchMatches[:0]
		for i := range t.rows {
			t.searchMatches = append(t.searchMatches, i)
		}
		if len(t.searchMatches) == 0 {
			t.searchIndex = 0
		} else if t.searchIndex >= len(t.searchMatches) {
			t.searchIndex = 0
		}
	}
	if t.cursor >= len(t.rows) {
		t.cursor = len(t.rows) - 1
	}
	if t.cursor < 0 {
		t.cursor = 0
	}
}

// applyFilter recomputes the matching nodes for the current filter, expands the
// ancestors needed to reveal them, and records which rows stay visible.
func (t *TUITree) applyFilter() {
	t.matches = nil
	t.matchSet = nil
	t.visible = nil
	if t.root == nil || t.filter == "" {
		return
	}

	query := strings.ToLower(t.filter)
	t.matchSet = make(map[*lens.Node]bool)
	t.visible = make(map[*lens.Node]bool)

	var mark func(n *lens.Node) bool
	mark = func(n *lens.Node) bool {
		if n == nil {
			return false
		}
		selfMatch := nodeMatches(n, query)
		if selfMatch {
			t.matches = append(t.matches, n)
			t.matchSet[n] = true
		}
		childMatch := false
		for _, c := range n.Children {
			if mark(c) {
				childMatch = true
			}
		}
		if !selfMatch && !childMatch {
			return false
		}
		t.visible[n] = true
		if len(n.Children) > 0 {
			t.expandedState[n] = true
		}
		return true
	}
	mark(t.root)
}

// nodeMatches reports whether a node's label, value, note or type contains query.
func nodeMatches(n *lens.Node, lowerQuery string) bool {
	text := n.Name + " " + fmt.Sprintf("%v", n.Value) + " " + n.Note + " " + n.TypeName
	return strings.Contains(strings.ToLower(text), lowerQuery)
}

func (t *TUITree) walk(n *lens.Node, parent *lens.Node, path string, depth int, isLast bool, rows *[]*treeRow) {
	if n == nil {
		return
	}

	nodePath := n.Name
	if path != "" {
		if n.Name != "" {
			nodePath = path + "." + n.Name
		} else {
			nodePath = path
		}
	}

	hasKids := len(n.Children) > 0
	expanded := t.expandedState[n]

	*rows = append(*rows, &treeRow{
		node:        n,
		parent:      parent,
		path:        nodePath,
		depth:       depth,
		isLast:      isLast,
		hasChildren: hasKids,
		expanded:    expanded,
	})

	if hasKids && expanded {
		for i, c := range n.Children {
			last := i == len(n.Children)-1
			cPath := nodePath
			if c.Name == "" {
				cPath = fmt.Sprintf("%s[%d]", nodePath, i)
			}
			t.walk(c, n, cPath, depth+1, last, rows)
		}
	}
}

// MoveUp moves cursor one row up.
func (t *TUITree) MoveUp() {
	if t.cursor > 0 {
		t.cursor--
		if t.cursor < t.scrollOffset {
			t.scrollOffset = t.cursor
		}
	}
}

// viewportRows is the number of tree rows that fit on screen, leaving room for
// the header, the status line and the footer or search bar.
func (t *TUITree) viewportRows() int {
	visibleRows := t.height - 3
	if visibleRows < 1 {
		visibleRows = 1
	}
	return visibleRows
}

// ensureCursorVisible scrolls the viewport so the cursor row is on screen.
func (t *TUITree) ensureCursorVisible() {
	visibleRows := t.viewportRows()
	if t.cursor < t.scrollOffset {
		t.scrollOffset = t.cursor
	}
	if t.cursor >= t.scrollOffset+visibleRows {
		t.scrollOffset = t.cursor - visibleRows + 1
	}
	if t.scrollOffset < 0 {
		t.scrollOffset = 0
	}
}

// MoveDown moves cursor one row down.
func (t *TUITree) MoveDown() {
	if t.cursor < len(t.rows)-1 {
		t.cursor++
		t.ensureCursorVisible()
	}
}

// MoveTop moves cursor to first row.
func (t *TUITree) MoveTop() {
	t.cursor = 0
	t.scrollOffset = 0
}

// MoveBottom moves cursor to last row.
func (t *TUITree) MoveBottom() {
	if len(t.rows) > 0 {
		t.cursor = len(t.rows) - 1
		t.ensureCursorVisible()
	}
}

// ToggleSelected toggles expansion of the selected node.
func (t *TUITree) ToggleSelected() {
	if t.cursor < 0 || t.cursor >= len(t.rows) {
		return
	}
	r := t.rows[t.cursor]
	if !r.hasChildren {
		return
	}
	t.expandedState[r.node] = !t.expandedState[r.node]
	selectedNode := r.node
	t.rebuildRows()
	// Restore cursor to the same node
	for i, row := range t.rows {
		if row.node == selectedNode {
			t.cursor = i
			break
		}
	}
}

// CollapseSelected collapses current node or jumps to parent.
func (t *TUITree) CollapseSelected() {
	if t.cursor < 0 || t.cursor >= len(t.rows) {
		return
	}
	r := t.rows[t.cursor]
	if r.hasChildren && t.expandedState[r.node] {
		t.expandedState[r.node] = false
		t.rebuildRows()
		return
	}
	// Move to parent
	if r.parent != nil {
		for i, row := range t.rows {
			if row.node == r.parent {
				t.cursor = i
				if t.cursor < t.scrollOffset {
					t.scrollOffset = t.cursor
				}
				break
			}
		}
	}
}

// ExpandAll expands all nodes recursively.
func (t *TUITree) ExpandAll() {
	var markAll func(n *lens.Node)
	markAll = func(n *lens.Node) {
		if n == nil {
			return
		}
		if len(n.Children) > 0 {
			t.expandedState[n] = true
			for _, c := range n.Children {
				markAll(c)
			}
		}
	}
	markAll(t.root)
	t.rebuildRows()
	t.statusMsg = "All nodes expanded"
}

// CollapseAll collapses all nodes except root.
func (t *TUITree) CollapseAll() {
	t.expandedState = make(map[*lens.Node]bool)
	if t.root != nil {
		t.expandedState[t.root] = true
	}
	t.cursor = 0
	t.scrollOffset = 0
	t.rebuildRows()
	t.statusMsg = "All nodes collapsed"
}

// SetFilter filters visible nodes by a search string. Matching nodes and the
// ancestors that lead to them stay visible; non-matching branches collapse.
// Clearing the query restores the expansion state and cursor from before the
// search started.
func (t *TUITree) SetFilter(q string) {
	if q != "" && t.filter == "" {
		t.savedExpanded = cloneExpandedState(t.expandedState)
		t.savedCursor = t.selectedNode()
	}

	t.filter = q
	t.matchIdx = -1
	t.searchQuery = q
	t.searchIndex = 0
	t.searchMatches = nil
	t.cursor = 0
	t.scrollOffset = 0

	if q == "" {
		t.matches = nil
		t.matchSet = nil
		t.visible = nil
		t.restoreExpandedState()
		t.rebuildRows()
		if t.savedCursor != nil {
			t.moveCursorTo(t.savedCursor)
			t.savedCursor = nil
		}
		t.statusMsg = "Search cleared"
		return
	}

	t.applyFilter()
	t.rebuildRows()

	switch len(t.matches) {
	case 0:
		t.statusMsg = fmt.Sprintf("Search %q: no matches", q)
	case 1:
		t.statusMsg = fmt.Sprintf("Search %q: 1 match — Enter: jump, Esc: clear", q)
	default:
		t.statusMsg = fmt.Sprintf("Search %q: %d matches — Enter: first, n/N: next/prev", q, len(t.matches))
	}
}

// BeginSearch opens the interactive search input bar.
func (t *TUITree) BeginSearch() {
	t.searchMode = true
	t.statusMsg = "Type to search; Enter: first match; Esc: clear"
}

// CommitSearch closes the search bar and jumps to the first match.
func (t *TUITree) CommitSearch() {
	t.searchMode = false
	if t.filter == "" {
		t.statusMsg = "Search cleared"
		return
	}
	if len(t.matches) == 0 {
		t.statusMsg = fmt.Sprintf("No matches for %q", t.filter)
		return
	}
	t.jumpToMatch(0)
}

// NextMatch moves the cursor to the next search match, wrapping at the end.
func (t *TUITree) NextMatch() {
	t.stepMatch(1)
}

// PrevMatch moves the cursor to the previous search match, wrapping at the start.
func (t *TUITree) PrevMatch() {
	t.stepMatch(-1)
}

func (t *TUITree) stepMatch(delta int) {
	if t.filter == "" {
		t.statusMsg = "No active search — press / to search"
		return
	}
	if len(t.matches) == 0 {
		t.statusMsg = fmt.Sprintf("No matches for %q", t.filter)
		return
	}

	next := 0
	if t.matchIdx >= 0 {
		next = (t.matchIdx + delta + len(t.matches)) % len(t.matches)
	} else if delta < 0 {
		next = len(t.matches) - 1
	}
	t.jumpToMatch(next)
}

func (t *TUITree) jumpToMatch(idx int) {
	if idx < 0 || idx >= len(t.matches) {
		return
	}
	target := t.matches[idx]
	t.revealNode(target)
	t.rebuildRows()
	t.matchIdx = idx
	if i := t.rowIndex(target); i >= 0 {
		t.cursor = i
		t.statusMsg = fmt.Sprintf("Match %d/%d: %s", idx+1, len(t.matches), t.rows[i].path)
		t.ensureCursorVisible()
	}
}

// revealNode expands every ancestor so that target is present in the rows.
func (t *TUITree) revealNode(target *lens.Node) {
	for _, n := range t.nodePath(target) {
		if len(n.Children) > 0 {
			t.expandedState[n] = true
		}
	}
}

// nodePath returns the chain of nodes from the root to target, inclusive.
func (t *TUITree) nodePath(target *lens.Node) []*lens.Node {
	var stack []*lens.Node
	var find func(n *lens.Node) bool
	find = func(n *lens.Node) bool {
		if n == nil {
			return false
		}
		stack = append(stack, n)
		if n == target {
			return true
		}
		for _, c := range n.Children {
			if find(c) {
				return true
			}
		}
		stack = stack[:len(stack)-1]
		return false
	}
	if !find(t.root) {
		return nil
	}
	return stack
}

func (t *TUITree) rowIndex(node *lens.Node) int {
	for i, r := range t.rows {
		if r.node == node {
			return i
		}
	}
	return -1
}

func (t *TUITree) moveCursorTo(node *lens.Node) {
	if i := t.rowIndex(node); i >= 0 {
		t.cursor = i
		t.ensureCursorVisible()
	}
}

func (t *TUITree) selectedNode() *lens.Node {
	if t.cursor >= 0 && t.cursor < len(t.rows) {
		return t.rows[t.cursor].node
	}
	return nil
}

func cloneExpandedState(state map[*lens.Node]bool) map[*lens.Node]bool {
	clone := make(map[*lens.Node]bool, len(state))
	for n, expanded := range state {
		clone[n] = expanded
	}
	return clone
}

func (t *TUITree) restoreExpandedState() {
	if t.savedExpanded != nil {
		t.expandedState = t.savedExpanded
		t.savedExpanded = nil
	}
}

// handleSearchKey processes input while the search bar is open.
func (t *TUITree) handleSearchKey(key string) {
	switch key {
	case "enter":
		t.CommitSearch()
	case "esc":
		t.searchMode = false
		t.SetFilter("")
	case "backspace":
		t.searchBackspace()
	case "space":
		t.SetFilter(t.filter + " ")
	case "ctrl+c":
		t.quit = true
	default:
		if isPrintableKey(key) {
			t.SetFilter(t.filter + key)
		}
	}
}

func (t *TUITree) searchBackspace() {
	if t.filter == "" {
		return
	}
	_, size := utf8.DecodeLastRuneInString(t.filter)
	t.SetFilter(t.filter[:len(t.filter)-size])
}

// isPrintableKey reports whether key is a single printable character, so that
// named keys such as "down" or "tab" are not typed into the search query.
func isPrintableKey(key string) bool {
	r, size := utf8.DecodeRuneInString(key)
	if size == 0 || size != len(key) || r == utf8.RuneError {
		return false
	}
	return r >= 0x20 && r != 0x7f
}

// SetSearch applies an interactive search query: it expands ancestors of
// matching nodes and moves the cursor to the first match.
func (t *TUITree) SetSearch(q string) {
	t.searchMode = false
	t.searchBuffer = q
	t.SetFilter(q)
	if q != "" && len(t.searchMatches) > 0 {
		t.searchIndex = 0
		t.cursor = t.searchMatches[0]
		t.visibleScrollToCursor()
	}
	if q != "" {
		t.statusMsg = fmt.Sprintf("Search: %q (%d matches)", q, len(t.searchMatches))
	} else {
		t.statusMsg = "Search cleared"
	}
}

// Search is an alias for SetSearch.
func (t *TUITree) Search(q string) { t.SetSearch(q) }

// StartSearch enters interactive search input mode.
func (t *TUITree) StartSearch() {
	t.searchMode = true
	t.searchBuffer = t.searchQuery
	t.statusMsg = "Search: " + t.searchBuffer
}

// EnterSearch is an alias for StartSearch.
func (t *TUITree) EnterSearch() { t.StartSearch() }

// BeginSearch is an alias for StartSearch.
func (t *TUITree) BeginSearch() { t.StartSearch() }

// CancelSearch exits interactive search input mode without changing results.
func (t *TUITree) CancelSearch() {
	t.searchMode = false
	t.statusMsg = "Search cancelled"
}

// ExitSearch is an alias for CancelSearch.
func (t *TUITree) ExitSearch() { t.CancelSearch() }

// EndSearch is an alias for CancelSearch.
func (t *TUITree) EndSearch() { t.CancelSearch() }

// ClearSearch clears the active search query and filter.
func (t *TUITree) ClearSearch() {
	t.searchMode = false
	t.searchBuffer = ""
	t.SetFilter("")
}

// NextMatch moves the cursor to the next search match (wrapping).
func (t *TUITree) NextMatch() {
	if len(t.searchMatches) == 0 {
		if t.searchQuery != "" {
			t.rebuildRows()
		}
		if len(t.searchMatches) == 0 {
			return
		}
	}
	t.searchIndex++
	if t.searchIndex >= len(t.searchMatches) {
		t.searchIndex = 0
	}
	t.cursor = t.searchMatches[t.searchIndex]
	t.visibleScrollToCursor()
	if t.searchQuery != "" {
		t.statusMsg = fmt.Sprintf("Search: %q (%d/%d)", t.searchQuery, t.searchIndex+1, len(t.searchMatches))
	}
}

// NextSearchMatch is an alias for NextMatch.
func (t *TUITree) NextSearchMatch() { t.NextMatch() }

// NextResult is an alias for NextMatch.
func (t *TUITree) NextResult() { t.NextMatch() }

// PrevMatch moves the cursor to the previous search match (wrapping).
func (t *TUITree) PrevMatch() {
	if len(t.searchMatches) == 0 {
		if t.searchQuery != "" {
			t.rebuildRows()
		}
		if len(t.searchMatches) == 0 {
			return
		}
	}
	t.searchIndex--
	if t.searchIndex < 0 {
		t.searchIndex = len(t.searchMatches) - 1
	}
	t.cursor = t.searchMatches[t.searchIndex]
	t.visibleScrollToCursor()
	if t.searchQuery != "" {
		t.statusMsg = fmt.Sprintf("Search: %q (%d/%d)", t.searchQuery, t.searchIndex+1, len(t.searchMatches))
	}
}

// PrevSearchMatch is an alias for PrevMatch.
func (t *TUITree) PrevSearchMatch() { t.PrevMatch() }

// PreviousMatch is an alias for PrevMatch.
func (t *TUITree) PreviousMatch() { t.PrevMatch() }

// PreviousResult is an alias for PrevMatch.
func (t *TUITree) PreviousResult() { t.PrevMatch() }

// SearchMatches returns the row indices of all current search matches.
func (t *TUITree) SearchMatches() []int {
	out := make([]int, len(t.searchMatches))
	copy(out, t.searchMatches)
	return out
}

// MatchedRows is an alias for SearchMatches.
func (t *TUITree) MatchedRows() []int { return t.SearchMatches() }

// MatchCount returns the number of current search matches.
func (t *TUITree) MatchCount() int { return len(t.searchMatches) }

// CurrentMatchIndex returns the index within searchMatches of the current match.
func (t *TUITree) CurrentMatchIndex() int { return t.searchIndex }

// IsSearchMode reports whether the TUI is accepting search input.
func (t *TUITree) IsSearchMode() bool { return t.searchMode }

// ToggleHelp toggles the keybindings help overlay.
func (t *TUITree) ToggleHelp() {
	t.showHelp = !t.showHelp
	t.helpVisible = t.showHelp
	if t.showHelp {
		t.statusMsg = "Help overlay"
	} else {
		t.statusMsg = "Help hidden"
	}
}

// ShowHelp shows the help overlay.
func (t *TUITree) ShowHelp() { t.showHelp = true; t.helpVisible = true }

// HideHelp hides the help overlay.
func (t *TUITree) HideHelp() { t.showHelp = false; t.helpVisible = false }

// HelpVisible reports whether help overlay is visible.
func (t *TUITree) HelpVisible() bool { return t.showHelp }

// IsHelpVisible is an alias.
func (t *TUITree) IsHelpVisible() bool { return t.showHelp }

// ToggleInspector toggles the detail inspector sidebar.
func (t *TUITree) ToggleInspector() {
	t.showInspector = !t.showInspector
	t.inspectorVisible = t.showInspector
	if t.showInspector {
		t.statusMsg = "Inspector"
	} else {
		t.statusMsg = "Inspector hidden"
	}
}

// ShowInspector shows the inspector sidebar.
func (t *TUITree) ShowInspector() { t.showInspector = true; t.inspectorVisible = true }

// HideInspector hides the inspector sidebar.
func (t *TUITree) HideInspector() { t.showInspector = false; t.inspectorVisible = false }

// InspectorVisible reports whether inspector sidebar is visible.
func (t *TUITree) InspectorVisible() bool { return t.showInspector }

// IsInspectorVisible is an alias.
func (t *TUITree) IsInspectorVisible() bool { return t.showInspector }

// SearchBuffer returns the current search input buffer.
func (t *TUITree) SearchBuffer() string { return t.searchBuffer }

// SearchQuery returns the active search query.
func (t *TUITree) SearchQuery() string { return t.searchQuery }

func (t *TUITree) visibleScrollToCursor() {
	visibleRows := t.height - 3
	if visibleRows < 1 {
		visibleRows = 1
	}
	if t.cursor < t.scrollOffset {
		t.scrollOffset = t.cursor
	}
	if t.cursor >= t.scrollOffset+visibleRows {
		t.scrollOffset = t.cursor - visibleRows + 1
	}
}

// HandleKey processes a single user key stroke.
func (t *TUITree) HandleKey(key string) {
	// Interactive search input mode.
	if t.searchMode {
		t.statusMsg = ""
		switch key {
		case "enter":
			t.SetSearch(t.searchBuffer)
			return
		case "esc":
			t.CancelSearch()
			t.ClearSearch()
			return
		case "backspace":
			if len(t.searchBuffer) > 0 {
				t.searchBuffer = t.searchBuffer[:len(t.searchBuffer)-1]
			}
			t.searchQuery = t.searchBuffer
			t.filter = t.searchBuffer
			t.searchIndex = 0
			t.rebuildRows()
			if len(t.searchMatches) > 0 {
				t.jumpToFirstMatch()
			}
			t.statusMsg = "Search: " + t.searchBuffer
			return
		default:
			if len(key) == 1 {
				t.searchBuffer += key
				t.searchQuery = t.searchBuffer
				t.filter = t.searchBuffer
				t.searchIndex = 0
				t.rebuildRows()
				if len(t.searchMatches) > 0 {
					t.jumpToFirstMatch()
				}
				t.statusMsg = fmt.Sprintf("Search: %s (%d matches)", t.searchBuffer, len(t.searchMatches))
				return
			}
		}
		return
	}

	t.statusMsg = ""

	if t.searchMode {
		t.handleSearchKey(key)
		return
	}

	switch key {
	case "up", "k":
		t.MoveUp()
	case "down", "j":
		t.MoveDown()
	case "right", "l", "enter", "space":
		t.ToggleSelected()
	case "left", "h":
		t.CollapseSelected()
	case "e":
		t.ExpandAll()
	case "c":
		t.CollapseAll()
	case "g", "home":
		t.MoveTop()
	case "G", "end":
		t.MoveBottom()
	case "n":
		t.NextMatch()
	case "N":
		t.PrevMatch()
	case "?":
		t.ToggleHelp()
	case "i", "v":
		t.ToggleInspector()
	case "/":
		t.StartSearch()
	case "q", "ctrl+c":
		t.quit = true
	case "esc":
		if t.filter != "" || t.searchQuery != "" {
			t.ClearSearch()
		}
	case "/":
		t.BeginSearch()
	default:
		if strings.HasPrefix(key, "/") {
			t.BeginSearch()
			t.SetFilter(strings.TrimPrefix(key, "/"))
			t.SetSearch(strings.TrimPrefix(key, "/"))
		}
	}
}

func (t *TUITree) jumpToFirstMatch() {
	if len(t.searchMatches) == 0 {
		return
	}
	t.searchIndex = 0
	t.cursor = t.searchMatches[0]
	t.visibleScrollToCursor()
}

// Render returns the current screen display.
func (t *TUITree) Render() string {
	p := t.palette
	var b strings.Builder

	// Header line
	b.WriteString(p.Type + "=== " + t.valueType + " Explorer ===" + p.Reset + "\n")

	visibleRows := t.viewportRows()

	endIdx := t.scrollOffset + visibleRows
	if endIdx > len(t.rows) {
		endIdx = len(t.rows)
	}

	startIdx := t.scrollOffset
	if startIdx >= len(t.rows) {
		startIdx = 0
	}

	for i := startIdx; i < endIdx; i++ {
		row := t.rows[i]
		isSelected := i == t.cursor

		// Selection cursor indicator
		cursorMarker := "  "
		if isSelected {
			cursorMarker = "> "
		}

		indent := strings.Repeat("  ", row.depth)
		expandMarker := "  "
		if row.hasChildren {
			if row.expanded {
				expandMarker = "▼ "
			} else {
				expandMarker = "▶ "
			}
		}

		label := row.node.Name
		if label == "" {
			label = "(item)"
		}

		var line strings.Builder
		line.WriteString(cursorMarker)
		line.WriteString(indent)
		line.WriteString(p.Dim + expandMarker + p.Reset)

		switch {
		case isSelected:
			line.WriteString(p.Change + label + p.Reset)
		case t.matchSet[row.node]:
			line.WriteString(p.Add + label + p.Reset)
		default:
			line.WriteString(p.Key + label + p.Reset)
		}

		switch row.node.Kind {
		case lens.KindScalar:
			line.WriteString(": ")
			valStr := fmt.Sprintf("%v", row.node.Value)
			line.WriteString(p.Str + valStr + p.Reset)
			if row.node.Note != "" {
				line.WriteString(" " + p.Note + "(" + row.node.Note + ")" + p.Reset)
			}
		case lens.KindList:
			fmt.Fprintf(&line, " %s[%d]%s", p.Dim, len(row.node.Children), p.Reset)
		default:
			if row.node.TypeName != "" {
				line.WriteString(" " + p.Dim + row.node.TypeName + p.Reset)
			}
		}

		b.WriteString(line.String() + "\n")
	}

	// Status line
	status := t.statusMsg
	if status == "" {
		if t.cursor >= 0 && t.cursor < len(t.rows) {
			cur := t.rows[t.cursor]
			status = cur.path
			if cur.node.Note != "" {
				status += " (" + cur.node.Note + ")"
			}
		} else {
			status = "No nodes"
		}
	}
	b.WriteString(p.Dim + "--- " + status + " ---" + p.Reset + "\n")

	// Search input bar or the keyboard help line, always on the last row.
	if t.searchMode {
		b.WriteString(p.Type + "/" + p.Reset + t.filter + p.Change + "█" + p.Reset)
		b.WriteString(p.Dim + "  [Enter: first match | Esc: clear]" + p.Reset + "\n")
	} else if t.filter != "" {
		position := "-"
		if t.matchIdx >= 0 {
			position = fmt.Sprintf("%d", t.matchIdx+1)
		}
		fmt.Fprintf(&b, "%s[n/N: next/prev match (%s/%d) | Esc: clear search | ↑/↓/j/k: nav | q: quit]%s\n",
			p.Dim, position, len(t.matches), p.Reset)
	} else {
		b.WriteString(p.Dim + "[↑/↓/j/k: nav | Enter/Space: toggle | e: expand all | c: collapse all | /: search | q: quit]" + p.Reset + "\n")
	b.WriteString(p.Dim + "[↑/↓/j/k: nav | Enter/Space: toggle | e: expand all | c: collapse all | /: search | n/N: matches | ?: help | i/v: inspector | q: quit]" + p.Reset + "\n")

	if t.showInspector {
		b.WriteString(p.Dim + "--- Inspector ---" + p.Reset + "\n")
		if t.cursor >= 0 && t.cursor < len(t.rows) {
			cur := t.rows[t.cursor]
			fmt.Fprintf(&b, "Path: %s\n", cur.path)
			fmt.Fprintf(&b, "Type: %s\n", cur.node.TypeName)
			fmt.Fprintf(&b, "Kind: %v\n", cur.node.Kind)
			valStr := fmt.Sprintf("%v", cur.node.Value)
			fmt.Fprintf(&b, "Value: %s (len=%d)\n", valStr, len(valStr))
			if cur.node.Note != "" {
				fmt.Fprintf(&b, "Note: %s\n", cur.node.Note)
			}
		} else {
			b.WriteString("No node selected\n")
		}
	}

	if t.showHelp {
		b.WriteString(p.Dim + "--- Help Overlay ---" + p.Reset + "\n")
		b.WriteString("j/k or arrows: navigate\n")
		b.WriteString("Enter/Space/Right/l: toggle node\n")
		b.WriteString("Left/h: collapse or parent\n")
		b.WriteString("e: expand all | c: collapse all\n")
		b.WriteString("/: search | n/N: next/prev match\n")
		b.WriteString("?: toggle help | i/v: inspector\n")
		b.WriteString("q/Ctrl+C: quit\n")
	}

	return b.String()
}

// Run executes the interactive loop reading key commands from in until quit.
func (t *TUITree) Run() error {
	reader := bufio.NewReader(t.in)

	for !t.quit {
		screen := t.Render()
		if _, err := fmt.Fprint(t.out, screen); err != nil {
			return fmt.Errorf("tui render: %w", err)
		}

		key, err := readKey(reader)
		if err != nil {
			if errorsIsEOF(err) {
				break
			}
			return fmt.Errorf("reading input key: %w", err)
		}

		t.HandleKey(key)
	}

	return nil
}

// readKey reads a single key or escape sequence from reader.
func readKey(r *bufio.Reader) (string, error) {
	b, err := r.ReadByte()
	if err != nil {
		return "", err
	}

	switch b {
	case '\r', '\n':
		return "enter", nil
	case ' ':
		return "space", nil
	case '\t':
		return "tab", nil
	case 8, 127: // Ctrl+H and DEL: backspace in the search bar
		return "backspace", nil
	case 3: // Ctrl+C
		return "ctrl+c", nil
	case 27: // Escape or ANSI escape sequence
		if r.Buffered() == 0 {
			return "esc", nil
		}
		next, err := r.ReadByte()
		if err != nil {
			return "esc", err
		}
		if next == '[' {
			code, err := r.ReadByte()
			if err != nil {
				return "esc", err
			}
			switch code {
			case 'A':
				return "up", nil
			case 'B':
				return "down", nil
			case 'C':
				return "right", nil
			case 'D':
				return "left", nil
			case 'H':
				return "home", nil
			case 'F':
				return "end", nil
			}
		}
		return "esc", nil
	default:
		return string(b), nil
	}
}

func errorsIsEOF(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "EOF"))
}
