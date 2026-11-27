package lens_test

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

func TestRegistryIndexCoversTypeNames(t *testing.T) {
	t.Parallel()

	index := lens.RegistryIndex()
	names := lens.TypeNames()
	if len(index) != len(names) {
		t.Fatalf("RegistryIndex() has %d entries, TypeNames() has %d", len(index), len(names))
	}

	categories := map[string]bool{
		lens.CategoryTransaction:   true,
		lens.CategoryLedger:        true,
		lens.CategoryHistory:       true,
		lens.CategoryOverlay:       true,
		lens.CategoryConsensus:     true,
		lens.CategorySoroban:       true,
		lens.CategoryStateArchival: true,
		lens.CategoryLedgerEntry:   true,
		lens.CategoryPrimitive:     true,
	}

	for i, info := range index {
		// Both lists are sorted by name, so aligned positions must match.
		if info.Name != names[i] {
			t.Errorf("entry %d is %q, want %q", i, info.Name, names[i])
		}
		if info.Description == "" {
			t.Errorf("%s: empty description", info.Name)
		}
		if !categories[info.Category] {
			t.Errorf("%s: unknown category %q", info.Name, info.Category)
		}
		if info.Priority <= 0 {
			t.Errorf("%s: priority %d, want positive", info.Name, info.Priority)
		}
		if info.MinWireSize <= 0 {
			t.Errorf("%s: min wire size %d, want positive", info.Name, info.MinWireSize)
		}
	}
}

// TestRegistryIndexMinWireSizeIsLowerBound checks the estimate against real
// encodings: whatever the true size of a fixture is, it cannot be smaller
// than the published minimum.
func TestRegistryIndexMinWireSizeIsLowerBound(t *testing.T) {
	t.Parallel()

	byName := map[string]int{}
	for _, info := range lens.RegistryIndex() {
		byName[info.Name] = info.MinWireSize
	}

	tests := []struct {
		fixture  string
		typeName string
	}{
		{"overlay_hello", "Hello"},
		{"overlay_stellar_message", "StellarMessage"},
		{"overlay_peer_address", "PeerAddress"},
		{"overlay_auth_cert", "AuthCert"},
		{"tx_history_entry_v0", "TransactionHistoryEntry"},
		{"tx_history_result_entry", "TransactionHistoryResultEntry"},
		{"generalized_tx_set", "GeneralizedTransactionSet"},
		{"ledger_close_meta_batch", "LedgerCloseMetaBatch"},
		{"ledger_header", "LedgerHeader"},
		{"ttl_entry", "TtlEntry"},
		{"scp_envelope", "ScpEnvelope"},
	}

	for _, tc := range tests {
		t.Run(tc.fixture, func(t *testing.T) {
			t.Parallel()

			min, ok := byName[tc.typeName]
			if !ok {
				t.Fatalf("type %s missing from the index", tc.typeName)
			}
			raw, err := base64.StdEncoding.DecodeString(loadFixture(t, tc.fixture))
			if err != nil {
				t.Fatalf("decoding fixture base64: %v", err)
			}
			if len(raw) < min {
				t.Errorf("%s encodes to %d bytes, below the published minimum %d", tc.typeName, len(raw), min)
			}
		})
	}
}

// TestRegistryIndexJSONShape guards the contract of `lens types --json`:
// camelCase keys and nothing else.
func TestRegistryIndexJSONShape(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(lens.RegistryIndex())
	if err != nil {
		t.Fatalf("marshal error = %v", err)
	}
	var decoded []map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal error = %v", err)
	}
	if len(decoded) != len(lens.TypeNames()) {
		t.Fatalf("decoded %d entries, want %d", len(decoded), len(lens.TypeNames()))
	}

	wantKeys := map[string]bool{
		"name": true, "category": true, "description": true,
		"priority": true, "minWireSize": true,
	}
	for _, entry := range decoded {
		for key := range entry {
			if !wantKeys[key] {
				t.Errorf("unexpected JSON key %q", key)
			}
		}
		for key := range wantKeys {
			if _, ok := entry[key]; !ok {
				t.Errorf("entry %v is missing key %q", entry["name"], key)
			}
		}
	}
}

func TestTypesByCategoryFilters(t *testing.T) {
	t.Parallel()

	soroban := lens.TypesByCategory(lens.CategorySoroban)
	if len(soroban) == 0 {
		t.Fatal("TypesByCategory(soroban) returned no types")
	}
	for _, info := range soroban {
		if info.Category != lens.CategorySoroban {
			t.Errorf("TypesByCategory(soroban)[%s].Category = %q, want %q",
				info.Name, info.Category, lens.CategorySoroban)
		}
	}

	// An unknown category returns an empty slice, not an error.
	unknown := lens.TypesByCategory("not-a-category")
	if len(unknown) != 0 {
		t.Errorf("TypesByCategory(unknown) = %v, want empty", unknown)
	}

	// Every type appears in exactly one category.
	all := lens.RegistryIndex()
	seen := make(map[string]bool, len(all))
	for _, info := range all {
		types := lens.TypesByCategory(info.Category)
		found := false
		for _, t2 := range types {
			if t2.Name == info.Name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("TypesByCategory(%q) does not contain %q", info.Category, info.Name)
		}
		seen[info.Name] = true
	}
	if len(seen) != len(all) {
		t.Errorf("coverage mismatch: %d seen vs %d total", len(seen), len(all))
	}
}

func TestTypesByCategoryIsSorted(t *testing.T) {
	t.Parallel()

	transaction := lens.TypesByCategory(lens.CategoryTransaction)
	for i := 1; i < len(transaction); i++ {
		if transaction[i-1].Name > transaction[i].Name {
			t.Errorf("TypesByCategory(transaction) not sorted: %q > %q",
				transaction[i-1].Name, transaction[i].Name)
		}
	}
}

func TestSignerTypeIsRegistered(t *testing.T) {
	t.Parallel()

	names := lens.TypeNames()
	found := false
	for _, n := range names {
		if n == "Signer" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("Signer is not in the registry")
	}

	// The Signer type should be in the primitive category.
	primitives := lens.TypesByCategory(lens.CategoryPrimitive)
	for _, info := range primitives {
		if info.Name == "Signer" {
			return
		}
	}
	t.Fatal("Signer is not in the primitive category")
}
