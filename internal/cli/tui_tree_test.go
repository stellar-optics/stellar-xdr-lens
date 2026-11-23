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

// bundledValue builds a synthetic payload with the three sections the TUI
// splits into tabs.
func bundledValue() *lens.Value {
	section := func(name string, fee int64) *lens.Node {
		return &lens.Node{
			Name: name,
			Kind: lens.KindStruct,
			Children: []*lens.Node{
				{Name: "Fee", Kind: lens.KindScalar, Value: fee},
				{Name: "Seq", Kind: lens.KindScalar, Value: fee + 1},
			},
		}
	}
	return &lens.Value{
		Type: "TransactionBundle",
		Node: &lens.Node{
			Kind: lens.KindStruct,
			Children: []*lens.Node{
				section("Envelope", 100),
				section("Result", 200),
				section("Meta", 300),
			},
		},
	}
}

func TestTUITreeTabs(t *testing.T) {
	var out bytes.Buffer
	tree := NewTUITree(bundledValue(), strings.NewReader(""), &out, format.NoColor)
	tree.SetDimensions(80, 12)

	if len(tree.tabs) != 3 {
		t.Fatalf("tabs = %d, want 3", len(tree.tabs))
	}
	for i, want := range []string{"Envelope", "Result", "Meta"} {
		if tree.tabs[i].label != want {
			t.Errorf("tab %d label = %q, want %q", i, tree.tabs[i].label, want)
		}
	}

	rendered := tree.Render()
	for _, want := range []string{"1: Envelope", "2: Result", "3: Meta", "switch view"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered tabs do not contain %q:\n%s", want, rendered)
		}
	}

	// Cursor position is independent per tab.
	tree.HandleKey("down")
	tree.HandleKey("down")
	if tree.cursor != 2 {
		t.Fatalf("cursor in tab 0 = %d, want 2", tree.cursor)
	}
	tree.SwitchTab(1)
	if tree.cursor != 0 {
		t.Errorf("cursor in tab 1 = %d, want 0 (independent state)", tree.cursor)
	}
	tree.SwitchTab(0)
	if tree.cursor != 2 {
		t.Errorf("cursor after returning to tab 0 = %d, want 2", tree.cursor)
	}

	// Collapse state is independent per tab too.
	tree.HandleKey("g")
	tree.HandleKey("enter") // collapse the section root
	collapsed := len(tree.rows)
	tree.SwitchTab(1)
	if len(tree.rows) <= collapsed {
		t.Errorf("tab 1 rows = %d, want more than collapsed tab 0 (%d)", len(tree.rows), collapsed)
	}
	tree.SwitchTab(0)
	if len(tree.rows) != collapsed {
		t.Errorf("tab 0 rows = %d after switch, want %d", len(tree.rows), collapsed)
	}

	// Number keys and Tab/Shift-Tab navigate.
	tree.HandleKey("2")
	if tree.activeTab != 1 {
		t.Errorf("active tab after '2' = %d, want 1", tree.activeTab)
	}
	tree.HandleKey("tab")
	if tree.activeTab != 2 {
		t.Errorf("active tab after Tab = %d, want 2", tree.activeTab)
	}
	tree.HandleKey("shift+tab")
	if tree.activeTab != 1 {
		t.Errorf("active tab after Shift-Tab = %d, want 1", tree.activeTab)
	}

	// Help modal documents tab switching.
	tree.HandleKey("?")
	if help := tree.Render(); !strings.Contains(help, "Keybindings") || !strings.Contains(help, "Tab/Shift-Tab") {
		t.Errorf("help modal does not document tab switching:\n%s", help)
	}
	tree.HandleKey("?")
	if tree.showHelp {
		t.Error("help modal did not close")
	}
}

func TestTUITreeTabsScrollStateIsIndependent(t *testing.T) {
	var out bytes.Buffer
	tree := NewTUITree(bundledValue(), strings.NewReader(""), &out, format.NoColor)
	tree.SetDimensions(80, 5)
	tree.HandleKey("e") // expand everything so the viewport scrolls
	tree.HandleKey("G")

	if tree.scrollOffset == 0 {
		t.Fatalf("expected tab 0 to be scrolled, got offset 0 (rows=%d)", len(tree.rows))
	}
	savedOffset := tree.scrollOffset

	tree.SwitchTab(1)
	if tree.scrollOffset != 0 {
		t.Errorf("new tab scroll offset = %d, want 0", tree.scrollOffset)
	}
	tree.SwitchTab(0)
	if tree.scrollOffset != savedOffset {
		t.Errorf("tab 0 scroll offset = %d after returning, want %d", tree.scrollOffset, savedOffset)
	}
}

// TestTUITreeTabsForTransactionResultPair covers the aliased field names: the
// pair calls its envelope "Transaction", not "Envelope".
func TestTUITreeTabsForTransactionResultPair(t *testing.T) {
	scalar := func(name string) *lens.Node {
		return &lens.Node{Name: name, Kind: lens.KindScalar, Value: int64(1)}
	}
	val := &lens.Value{
		Type: "TransactionResultPair",
		Node: &lens.Node{Kind: lens.KindStruct, Children: []*lens.Node{
			{Name: "Transaction", Kind: lens.KindStruct, Children: []*lens.Node{scalar("Fee")}},
			{Name: "Result", Kind: lens.KindStruct, Children: []*lens.Node{scalar("Code")}},
		}},
	}

	tree := NewTUITree(val, strings.NewReader(""), &bytes.Buffer{}, format.NoColor)
	if len(tree.tabs) != 2 {
		t.Fatalf("tabs = %d, want 2", len(tree.tabs))
	}
	if tree.tabs[0].label != "Envelope" || tree.tabs[1].label != "Result" {
		t.Errorf("tab labels = %q, %q; want Envelope, Result", tree.tabs[0].label, tree.tabs[1].label)
	}
}

func TestTUITreeHasNoTabsForSingleStructure(t *testing.T) {
	val := loadFixtureValue(t, "tx_success.env.txt")
	var out bytes.Buffer
	tree := NewTUITree(val, strings.NewReader("q\n"), &out, format.NoColor)

	if tree.hasTabs() {
		t.Errorf("envelope should not render tabs, got %d", len(tree.tabs))
	}
	if rendered := tree.Render(); strings.Contains(rendered, "1: Envelope") {
		t.Errorf("single-structure render contains a tab bar:\n%s", rendered)
	}
	// Tab keys are harmless without tabs.
	tree.HandleKey("tab")
	tree.HandleKey("2")
	if tree.activeTab != -1 {
		t.Errorf("activeTab = %d, want -1 without tabs", tree.activeTab)
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
