package lens

import (
	"fmt"
	"strings"
	"testing"
)

func dumpNode(n *Node, sb *strings.Builder) {
	fmt.Fprintf(sb, "%s|%v|%d;", n.Name, n.Value, len(n.Children))
	for _, c := range n.Children {
		dumpNode(c, sb)
	}
}

func TestBuildNodeMapKeyOrderDeterministic(t *testing.T) {
	m := map[any]string{1: "int one", "1": "string one"}
	for i := 2; i <= 32; i++ {
		m[i] = fmt.Sprintf("int %d", i)
		m[fmt.Sprintf("k%d", i)] = fmt.Sprintf("str %d", i)
	}
	render := func() string {
		var sb strings.Builder
		dumpNode(nodeFor("m", m), &sb)
		return sb.String()
	}
	want := render()
	for i := 1; i < 100; i++ {
		if got := render(); got != want {
			t.Fatalf("node tree differs on iteration %d", i)
		}
	}
}
