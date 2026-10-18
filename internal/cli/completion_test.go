package cli

import (
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestInstallCompletionHandlers(t *testing.T) {
	root := newRootCmd(io.Discard, io.Discard, strings.NewReader(""))
	if root == nil {
		t.Fatal("newRootCmd returned nil")
	}

	var found bool
	for _, cmd := range root.Commands() {
		if cmd.Name() == "completion" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("completion command not registered")
	}
}

func TestTypeNameCompletion(t *testing.T) {
	values, directive := typeNameCompletion(nil, nil, "Trans")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("directive = %v, want %v", directive, cobra.ShellCompDirectiveNoFileComp)
	}
	if len(values) == 0 {
		t.Fatal("type completion returned no values")
	}

	contains := false
	for _, value := range values {
		if value == "TransactionEnvelope" {
			contains = true
			break
		}
	}
	if !contains {
		t.Fatalf("type completion values should include TransactionEnvelope, got %v", values[:min(5, len(values))])
	}
}

func TestCompletionValues(t *testing.T) {
	values := completionValues([]string{"tree", "json", "summary"}, "js")
	if len(values) != 1 || values[0] != "json" {
		t.Fatalf("completionValues returned %#v, want [json]", values)
	}

	all := completionValues([]string{"auto", "always", "never"}, "")
	if len(all) != 3 {
		t.Fatalf("completionValues empty prefix returned %d values, want 3", len(all))
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
