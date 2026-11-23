package lens_test

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
	"github.com/stellar/go-stellar-sdk/xdr"
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

// TestContractIdPreimageRegistered verifies that ContractIdPreimage can be detected
// and decoded automatically and that it appears in the registry with the correct category.
func TestContractIdPreimageRegistered(t *testing.T) {
	t.Parallel()

	// Verify it appears in the index with the right category.
	found := false
	for _, info := range lens.RegistryIndex() {
		if info.Name == "ContractIdPreimage" {
			found = true
			if info.Category != lens.CategorySoroban {
				t.Errorf("ContractIdPreimage category = %q, want %q", info.Category, lens.CategorySoroban)
			}
			if info.Priority <= 0 {
				t.Errorf("ContractIdPreimage priority = %d, want positive", info.Priority)
			}
			if info.MinWireSize <= 0 {
				t.Errorf("ContractIdPreimage MinWireSize = %d, want positive", info.MinWireSize)
			}
		}
	}
	if !found {
		t.Fatal("ContractIdPreimage not found in RegistryIndex")
	}

	// Build a FROM_ASSET (native) preimage — it marshals cleanly without
	// needing a real account keypair.
	nativeAsset := xdr.Asset{Type: xdr.AssetTypeAssetTypeNative}
	preimage := xdr.ContractIdPreimage{
		Type:      xdr.ContractIdPreimageTypeContractIdPreimageFromAsset,
		FromAsset: &nativeAsset,
	}
	raw, err := preimage.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	encoded := base64.StdEncoding.EncodeToString(raw)
	v, err := lens.DecodeAs(encoded, "ContractIdPreimage")
	if err != nil {
		t.Fatalf("DecodeAs(ContractIdPreimage): %v", err)
	}
	if v.Type != "ContractIdPreimage" {
		t.Errorf("Type = %q, want ContractIdPreimage", v.Type)
	}
}
