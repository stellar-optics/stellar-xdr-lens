package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens/format"
)

func loadFixtureValue(t *testing.T, filename string) *lens.Value {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", filename)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", filename, err)
	}
	val, err := lens.Decode(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("decoding %s: %v", filename, err)
	}
	return val
}

func TestTUITreeCanonicalFixtures(t *testing.T) {
	fixtures := []string{
		"tx_success.env.txt",
		"tx_failed.env.txt",
		"tx_feebump.env.txt",
		"tx_soroban.env.txt",
		"tx_success.res.txt",
	}

	for _, f := range fixtures {
		t.Run(f, func(t *testing.T) {
			val := loadFixtureValue(t, f)
			in := strings.NewReader("q\n")
			var out bytes.Buffer

			tree := NewTUITree(val, in, &out, format.NoColor)
			tree.SetDimensions(80, 24)

			if err := tree.Run(); err != nil {
				t.Fatalf("tree.Run() error = %v", err)
			}

			rendered := out.String()
			if !strings.Contains(rendered, val.Type) {
				t.Errorf("expected rendered TUI to contain type %s", val.Type)
			}
			if !strings.Contains(rendered, "Explorer") {
				t.Errorf("expected header in TUI output")
			}
		})
	}
}

func TestTUITreeNavigationAndExpand(t *testing.T) {
	val := loadFixtureValue(t, "tx_success.env.txt")
	var out bytes.Buffer
	tree := NewTUITree(val, strings.NewReader(""), &out, format.NoColor)
	tree.SetDimensions(80, 24)

	initialCount := len(tree.rows)
	if initialCount == 0 {
		t.Fatal("expected non-empty initial rows")
	}

	// Move Down
	tree.HandleKey("down")
	if tree.cursor != 1 {
		t.Errorf("expected cursor at 1, got %d", tree.cursor)
	}

	// Move Up
	tree.HandleKey("up")
	if tree.cursor != 0 {
		t.Errorf("expected cursor at 0, got %d", tree.cursor)
	}

	// Move Bottom and Top
	tree.HandleKey("G")
	if tree.cursor != len(tree.rows)-1 {
		t.Errorf("expected cursor at end, got %d", tree.cursor)
	}
	tree.HandleKey("g")
	if tree.cursor != 0 {
		t.Errorf("expected cursor at top, got %d", tree.cursor)
	}

	// Expand All
	tree.HandleKey("e")
	expandedCount := len(tree.rows)
	if expandedCount < initialCount {
		t.Errorf("expected expanded rows >= initial rows, got %d < %d", expandedCount, initialCount)
	}

	// Collapse All
	tree.HandleKey("c")
	collapsedCount := len(tree.rows)
	if collapsedCount > expandedCount {
		t.Errorf("expected collapsed rows <= expanded rows, got %d > %d", collapsedCount, expandedCount)
	}

	// Toggle selected
	tree.HandleKey("enter")
	tree.HandleKey("space")

	// Collapse selected / move to parent
	tree.HandleKey("left")
	tree.HandleKey("h")
}

func TestTUITreeFilter(t *testing.T) {
	val := loadFixtureValue(t, "tx_success.env.txt")
	var out bytes.Buffer
	tree := NewTUITree(val, strings.NewReader(""), &out, format.NoColor)

	tree.HandleKey("/Fee")
	if len(tree.rows) == 0 {
		t.Error("expected matches for filter /Fee")
	}

	// Clear filter with esc
	tree.HandleKey("esc")
	if tree.filter != "" {
		t.Errorf("expected empty filter after esc, got %q", tree.filter)
	}
}

func TestTUITreeKeySequencesAndRun(t *testing.T) {
	val := loadFixtureValue(t, "tx_success.env.txt")

	// Simulated ANSI arrow key sequences: down, enter, up, ctrl-c
	input := "\x1b[B\r\x1b[A\x1b[H\x1b[F\x03"
	var out bytes.Buffer
	tree := NewTUITree(val, strings.NewReader(input), &out, format.ColorPalette)

	if err := tree.Run(); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	rendered := out.String()
	if !strings.Contains(rendered, "\x1b[") {
		t.Errorf("expected ANSI color codes with ColorPalette")
	}
}

// findNode returns the first node with the given name anywhere in the tree.
func findNode(n *lens.Node, name string) *lens.Node {
	if n == nil {
		return nil
	}
	if n.Name == name {
		return n
	}
	for _, c := range n.Children {
		if found := findNode(c, name); found != nil {
			return found
		}
	}
	return nil
}

// typeQuery opens the search bar and types each rune of query as a key press.
func typeQuery(t *testing.T, tree *TUITree, query string) {
	t.Helper()
	tree.HandleKey("/")
	for _, r := range query {
		tree.HandleKey(string(r))
	}
}

func TestTUITreeSearchModeFiltersAsYouType(t *testing.T) {
	val := loadFixtureValue(t, "tx_success.env.txt")
	var out bytes.Buffer
	tree := NewTUITree(val, strings.NewReader(""), &out, format.NoColor)
	tree.SetDimensions(80, 24)

	tree.HandleKey("e") // expand all, so filtering has non-matching rows to hide
	unfilteredRows := len(tree.rows)

	tree.HandleKey("/")
	if !tree.searchMode {
		t.Fatal("expected / to open the search bar")
	}
	if tree.filter != "" {
		t.Fatalf("expected empty query on opening search, got %q", tree.filter)
	}

	tree.HandleKey("F")
	if tree.filter != "F" {
		t.Errorf("expected query %q after typing F, got %q", "F", tree.filter)
	}
	tree.HandleKey("e")
	tree.HandleKey("e")
	if tree.filter != "Fee" {
		t.Fatalf("expected query %q after typing, got %q", "Fee", tree.filter)
	}
	if !tree.searchMode {
		t.Error("expected search bar to stay open while typing")
	}

	if len(tree.matches) != 2 {
		t.Fatalf("expected 2 matches for %q, got %d", tree.filter, len(tree.matches))
	}
	if len(tree.rows) >= unfilteredRows {
		t.Errorf("expected non-matching branches to collapse, rows = %d, unfiltered = %d", len(tree.rows), unfilteredRows)
	}

	for _, m := range tree.matches {
		if tree.rowIndex(m) < 0 {
			t.Errorf("expected match %q to be visible", m.Name)
		}
	}
	if tb := findNode(tree.root, "Tx"); tb == nil || tree.rowIndex(tb) < 0 {
		t.Error("expected ancestor of a match to stay visible")
	}
	if sigs := findNode(tree.root, "Signatures"); sigs == nil {
		t.Fatal("expected Signatures node in fixture")
	} else if tree.rowIndex(sigs) >= 0 {
		t.Error("expected non-matching branch Signatures to be hidden")
	}

	// Movement keys are plain text while the search bar is open.
	cursorBefore := tree.cursor
	tree.HandleKey("down")
	if tree.cursor != cursorBefore {
		t.Error("expected down arrow to be ignored while typing a query")
	}

	rendered := tree.Render()
	if !strings.Contains(rendered, "/Fee") {
		t.Errorf("expected search bar to show the query, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "[Enter: first match") {
		t.Error("expected search bar hint in footer")
	}
}

func TestTUITreeSearchEnterJumpsToFirstMatch(t *testing.T) {
	val := loadFixtureValue(t, "tx_success.env.txt")
	var out bytes.Buffer
	tree := NewTUITree(val, strings.NewReader(""), &out, format.NoColor)
	tree.SetDimensions(80, 24)

	typeQuery(t, tree, "Fee")
	tree.HandleKey("enter")

	if tree.searchMode {
		t.Error("expected Enter to close the search bar")
	}
	if tree.matchIdx != 0 {
		t.Errorf("expected match index 0 after Enter, got %d", tree.matchIdx)
	}
	if got := tree.rows[tree.cursor].node; got != tree.matches[0] {
		t.Errorf("expected cursor on first match %q, got %q", tree.matches[0].Name, got.Name)
	}
	if !strings.Contains(tree.statusMsg, "Match 1/2") {
		t.Errorf("expected match position in status, got %q", tree.statusMsg)
	}
}

func TestTUITreeSearchNextPrevWrapAround(t *testing.T) {
	val := loadFixtureValue(t, "tx_success.env.txt")
	var out bytes.Buffer
	tree := NewTUITree(val, strings.NewReader(""), &out, format.NoColor)
	tree.SetDimensions(80, 24)

	typeQuery(t, tree, "Fee")
	tree.HandleKey("enter")
	if tree.matchIdx != 0 {
		t.Fatalf("expected first match selected, got index %d", tree.matchIdx)
	}

	tree.HandleKey("n")
	if tree.matchIdx != 1 {
		t.Fatalf("expected n to select match 2, got index %d", tree.matchIdx)
	}
	if got := tree.rows[tree.cursor].node; got != tree.matches[1] {
		t.Errorf("expected cursor on second match, got %q", got.Name)
	}

	tree.HandleKey("n")
	if tree.matchIdx != 0 {
		t.Errorf("expected n to wrap to first match, got index %d", tree.matchIdx)
	}

	tree.HandleKey("N")
	if tree.matchIdx != 1 {
		t.Errorf("expected N to wrap to last match, got index %d", tree.matchIdx)
	}
	tree.HandleKey("N")
	if tree.matchIdx != 0 {
		t.Errorf("expected N to select previous match, got index %d", tree.matchIdx)
	}

	if !strings.Contains(tree.Render(), "(1/2)") {
		t.Error("expected match counter in footer")
	}
}

func TestTUITreeSearchEscRestoresTree(t *testing.T) {
	val := loadFixtureValue(t, "tx_success.env.txt")
	var out bytes.Buffer
	tree := NewTUITree(val, strings.NewReader(""), &out, format.NoColor)
	tree.SetDimensions(80, 24)

	tree.HandleKey("e") // expand all so the restored state is non-trivial
	tree.HandleKey("j")
	tree.HandleKey("j")

	rowsBefore := len(tree.rows)
	cursorBefore := tree.selectedNode()

	typeQuery(t, tree, "Fee")
	tree.HandleKey("enter")
	tree.HandleKey("n")
	if tree.selectedNode() == cursorBefore {
		t.Fatal("expected search navigation to move the cursor")
	}

	tree.HandleKey("esc")
	if tree.filter != "" {
		t.Errorf("expected empty query after Esc, got %q", tree.filter)
	}
	if tree.searchMode {
		t.Error("expected Esc to close the search bar")
	}
	if len(tree.matches) != 0 || len(tree.matchSet) != 0 || len(tree.visible) != 0 {
		t.Error("expected match state to be cleared after Esc")
	}
	if len(tree.rows) != rowsBefore {
		t.Errorf("expected %d rows restored, got %d", rowsBefore, len(tree.rows))
	}
	if tree.selectedNode() != cursorBefore {
		t.Errorf("expected cursor restored to %q, got %q", cursorBefore.Name, tree.selectedNode().Name)
	}
	if strings.Contains(tree.Render(), "next/prev match") {
		t.Error("expected search footer to disappear after Esc")
	}
}

func TestTUITreeSearchBackspace(t *testing.T) {
	val := loadFixtureValue(t, "tx_success.env.txt")
	var out bytes.Buffer
	tree := NewTUITree(val, strings.NewReader(""), &out, format.NoColor)
	tree.SetDimensions(80, 24)

	typeQuery(t, tree, "Fee")
	tree.HandleKey("backspace")
	if tree.filter != "Fe" {
		t.Fatalf("expected query %q after backspace, got %q", "Fe", tree.filter)
	}
	tree.HandleKey("backspace")
	tree.HandleKey("backspace")
	if tree.filter != "" {
		t.Fatalf("expected empty query after deleting it, got %q", tree.filter)
	}
	if !tree.searchMode {
		t.Error("expected search bar to remain open after deleting the query")
	}

	for _, r := range "Seq" {
		tree.HandleKey(string(r))
	}
	if tree.filter != "Seq" {
		t.Fatalf("expected query %q, got %q", "Seq", tree.filter)
	}
	if findNode(tree.root, "SeqNum") == nil || len(tree.matches) == 0 {
		t.Fatal("expected matches for Seq")
	}
	if tree.rowIndex(tree.matches[0]) < 0 {
		t.Error("expected backspaced query to still filter visible rows")
	}
}

func TestTUITreeSearchNoMatches(t *testing.T) {
	val := loadFixtureValue(t, "tx_success.env.txt")
	var out bytes.Buffer
	tree := NewTUITree(val, strings.NewReader(""), &out, format.NoColor)
	tree.SetDimensions(80, 24)

	tree.HandleKey("e")
	rowsBefore := len(tree.rows)

	typeQuery(t, tree, "zzzzz")
	if len(tree.matches) != 0 {
		t.Fatalf("expected no matches, got %d", len(tree.matches))
	}
	if len(tree.rows) != 0 {
		t.Errorf("expected all rows hidden without matches, got %d", len(tree.rows))
	}
	if !strings.Contains(tree.statusMsg, "no matches") {
		t.Errorf("expected no-match status, got %q", tree.statusMsg)
	}

	tree.HandleKey("enter")
	if !strings.Contains(tree.statusMsg, "No matches") {
		t.Errorf("expected no-match status after Enter, got %q", tree.statusMsg)
	}
	tree.HandleKey("n")
	if !strings.Contains(tree.statusMsg, "No matches") {
		t.Errorf("expected no-match status after n, got %q", tree.statusMsg)
	}

	tree.HandleKey("esc")
	if len(tree.rows) != rowsBefore {
		t.Errorf("expected %d rows restored after clearing a no-match search, got %d", rowsBefore, len(tree.rows))
	}
}

func TestTUITreeSearchIsCaseInsensitive(t *testing.T) {
	val := loadFixtureValue(t, "tx_success.env.txt")
	var out bytes.Buffer

	var lowerQuery, upperQuery []string
	for _, q := range []string{"fee", "FEE", "Fee"} {
		tree := NewTUITree(val, strings.NewReader(""), &out, format.NoColor)
		typeQuery(t, tree, q)
		for _, m := range tree.matches {
			if q == "fee" {
				lowerQuery = append(lowerQuery, m.Name)
			} else {
				upperQuery = append(upperQuery, m.Name)
			}
		}
	}

	if len(lowerQuery) != 2 {
		t.Fatalf("expected 2 matches for lowercase query, got %d (%v)", len(lowerQuery), lowerQuery)
	}
	if len(upperQuery) != 4 {
		t.Errorf("expected 2 matches for each upper/mixed query, got %d (%v)", len(upperQuery), upperQuery)
	}
}

func TestTUITreeSearchHighlightsMatches(t *testing.T) {
	val := loadFixtureValue(t, "tx_success.env.txt")
	var out bytes.Buffer
	tree := NewTUITree(val, strings.NewReader(""), &out, format.ColorPalette)
	tree.SetDimensions(100, 200)

	typeQuery(t, tree, "Fee")
	tree.HandleKey("enter")
	tree.HandleKey("n") // move off the first match so it is not selection-highlighted

	rendered := tree.Render()
	if !strings.Contains(rendered, format.ColorPalette.Add+"Fee") {
		t.Errorf("expected unselected match to use the match highlight, got:\n%q", rendered)
	}
}

func TestTUITreeSearchRunSession(t *testing.T) {
	val := loadFixtureValue(t, "tx_success.env.txt")
	var out bytes.Buffer

	// Open search, type Fee, Enter to jump, n to advance, q to quit.
	tree := NewTUITree(val, strings.NewReader("/Fee\rnq"), &out, format.NoColor)
	tree.SetDimensions(80, 24)

	if err := tree.Run(); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if tree.filter != "Fee" {
		t.Errorf("expected query %q after session, got %q", "Fee", tree.filter)
	}
	if tree.searchMode {
		t.Error("expected search bar closed after Enter")
	}
	if tree.matchIdx != 1 {
		t.Errorf("expected n to leave match index 1, got %d", tree.matchIdx)
	}

	rendered := out.String()
	if !strings.Contains(rendered, "/Fee") {
		t.Error("expected rendered output to show the typed query")
	}
	if !strings.Contains(rendered, "Match 2/2") {
		t.Error("expected rendered status to show the second match")
	}
}

func TestTUITreeSearchTypingQDoesNotQuit(t *testing.T) {
	val := loadFixtureValue(t, "tx_success.env.txt")
	var out bytes.Buffer
	tree := NewTUITree(val, strings.NewReader(""), &out, format.NoColor)

	typeQuery(t, tree, "q")
	if tree.quit {
		t.Fatal("expected q to be typed into the search bar, not to quit")
	}
	if tree.filter != "q" {
		t.Errorf("expected query %q, got %q", "q", tree.filter)
	}
}

func TestTUITreeEdgeCases(t *testing.T) {
	emptyVal := &lens.Value{
		Type: "Empty",
		Node: nil,
	}
	var out bytes.Buffer
	tree := NewTUITree(emptyVal, strings.NewReader("q\n"), &out, format.NoColor)
	tree.SetDimensions(0, 0) // Should keep default dimensions

	if err := tree.Run(); err != nil {
		t.Fatalf("empty tree run: %v", err)
	}

	tree.MoveUp()
	tree.MoveDown()
	tree.MoveTop()
	tree.MoveBottom()
	tree.ToggleSelected()
	tree.CollapseSelected()

	// Test readKey branches: space, tab, standalone esc, arrow keys
	keysInput := " \t\x1b\x1b[C\x1b[D"
	keyTree := NewTUITree(emptyVal, strings.NewReader(keysInput), &out, format.NoColor)
	_ = keyTree.Run()
}
