package lens_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureDir is the shared corpus of real captured mainnet XDR. Tests read
// from it rather than embedding base64 in source, so a fixture can be traced
// back to a ledger via testdata/manifest.json.
const fixtureDir = "../../testdata"

// loadFixture reads a fixture by name, e.g. "tx_failed.env".
func loadFixture(t *testing.T, name string) string {
	t.Helper()
	payload, err := readFixture(name)
	if err != nil {
		t.Fatalf("%v", err)
	}
	return payload
}

// readFixture reads a fixture without a *testing.T, for use in table literals
// that are evaluated before any subtest starts.
func readFixture(name string) (string, error) {
	path := filepath.Join(fixtureDir, name+".txt")
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading fixture %s: %w", path, err)
	}
	payload := strings.TrimSpace(string(b))
	if payload == "" {
		return "", fmt.Errorf("fixture %s is empty", path)
	}
	return payload, nil
}
