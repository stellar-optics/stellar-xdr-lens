package lens

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"
)

func TestEnrichHash(t *testing.T) {
	hash := xdr.Hash{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1A, 0x1B, 0x1C, 0x1D, 0x1E, 0x1F, 0x20}

	display, raw, ok := enrichHash(reflect.ValueOf(hash))
	if !ok {
		t.Fatal("enrichHash failed")
	}

	expected := "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"
	if display != expected {
		t.Errorf("got %q, want %q", display, expected)
	}
	if raw != hash {
		t.Errorf("raw value altered")
	}
}

func TestEnrichPoolID(t *testing.T) {
	poolID := xdr.PoolId{0x0A, 0x0B, 0x0C} // Rest are zero

	display, raw, ok := enrichPoolID(reflect.ValueOf(poolID))
	if !ok {
		t.Fatal("enrichPoolID failed")
	}

	expected := "0a0b0c0000000000000000000000000000000000000000000000000000000000"
	if display != expected {
		t.Errorf("got %q, want %q", display, expected)
	}
	if raw != poolID {
		t.Errorf("raw value altered")
	}
}

func TestEnrichSignature(t *testing.T) {
	sig := xdr.Signature{0xFF, 0xEE, 0xDD}

	display, raw, ok := enrichSignature(reflect.ValueOf(sig))
	if !ok {
		t.Fatal("enrichSignature failed")
	}

	expected := "ffeedd"
	if display != expected {
		t.Errorf("got %q, want %q", display, expected)
	}
	// bytes slice equality
	rawSig := raw.(xdr.Signature)
	if len(rawSig) != len(sig) || rawSig[0] != sig[0] || rawSig[1] != sig[1] || rawSig[2] != sig[2] {
		t.Errorf("raw value altered")
	}
}

// The domain enrichers are registered against exact types, so routing one a
// different value means a bug in registration. They must decline cleanly
// rather than panic or invent a rendering.
func TestDomainEnrichersRejectWrongType(t *testing.T) {
	t.Parallel()

	wrong := reflect.ValueOf("not an xdr value")
	enrichers := map[string]Enricher{
		"enrichPoolParameters":     enrichPoolParameters,
		"enrichClaimPredicate":     enrichClaimPredicate,
		"enrichClaimableBalanceID": enrichClaimableBalanceID,
		"enrichContractID":         enrichContractID,
		"enrichScAddress":          enrichScAddress,
	}
	for name, fn := range enrichers {
		if _, _, ok := fn(wrong); ok {
			t.Errorf("%s accepted a non-XDR value", name)
		}
	}
}

func TestClaimableBalanceIDRejectsUnknownType(t *testing.T) {
	t.Parallel()

	id := xdr.ClaimableBalanceId{Type: xdr.ClaimableBalanceIdType(99)}
	if _, _, ok := enrichClaimableBalanceID(reflect.ValueOf(id)); ok {
		t.Error("enrichClaimableBalanceID accepted an unknown id type")
	}
}

func TestScAddressRejectsUnknownType(t *testing.T) {
	t.Parallel()

	addr := xdr.ScAddress{Type: xdr.ScAddressType(99)}
	if _, _, ok := enrichScAddress(reflect.ValueOf(addr)); ok {
		t.Error("enrichScAddress accepted an unknown address type")
	}
}

func TestTrustLineFlagNote(t *testing.T) {
	t.Parallel()

	if _, ok := trustLineFlagNote(reflect.ValueOf(xdr.Uint32(0))); ok {
		t.Error("trustLineFlagNote produced a note for an empty flag set")
	}
	if _, ok := trustLineFlagNote(reflect.ValueOf("x")); ok {
		t.Error("trustLineFlagNote accepted a non-Uint32 value")
	}
}

func TestClaimableBalanceFlagNote(t *testing.T) {
	t.Parallel()

	clawback := xdr.Uint32(xdr.ClaimableBalanceFlagsClaimableBalanceClawbackEnabledFlag)
	if note, ok := claimableBalanceFlagNote(reflect.ValueOf(clawback)); !ok || note != "clawback enabled" {
		t.Errorf("clawback note = %q, %v; want 'clawback enabled', true", note, ok)
	}
	if _, ok := claimableBalanceFlagNote(reflect.ValueOf(xdr.Uint32(0))); ok {
		t.Error("claimableBalanceFlagNote produced a note for an empty flag set")
	}
	if _, ok := claimableBalanceFlagNote(reflect.ValueOf("x")); ok {
		t.Error("claimableBalanceFlagNote accepted a non-Uint32 value")
	}
}

// footprintContractAddress builds the contract arm of an ScAddress with a
// recognisable payload, so strkey output in the tests is predictable.
func footprintContractAddress(id byte) xdr.ScAddress {
	return xdr.ScAddress{
		Type:       xdr.ScAddressTypeScAddressTypeContract,
		ContractId: &xdr.ContractId{id},
	}
}

func footprintSymbolKey(symbol string) xdr.ScVal {
	sym := xdr.ScSymbol(symbol)
	return xdr.ScVal{Type: xdr.ScValTypeScvSymbol, Sym: &sym}
}

func footprintContractDataKey(id byte, durability xdr.ContractDataDurability, key xdr.ScVal) xdr.LedgerKey {
	return xdr.LedgerKey{
		Type: xdr.LedgerEntryTypeContractData,
		ContractData: &xdr.LedgerKeyContractData{
			Contract:   footprintContractAddress(id),
			Key:        key,
			Durability: durability,
		},
	}
}

func assertFootprintGroup(t *testing.T, node *Node, name string, want []string) {
	t.Helper()
	group := node.Child(name)
	if group == nil {
		t.Fatalf("missing %s group in %v", name, childNames(node))
	}
	if group.Kind != KindList {
		t.Errorf("%s group kind = %v, want list", name, group.Kind)
	}
	got := make([]string, 0, len(group.Children))
	for _, child := range group.Children {
		value, ok := child.Value.(string)
		if !ok {
			t.Fatalf("%s group entry value has type %T, want string", name, child.Value)
		}
		got = append(got, value)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s group entries = %v, want %v", name, got, want)
	}
}

func childNames(node *Node) []string {
	names := make([]string, 0, len(node.Children))
	for _, child := range node.Children {
		names = append(names, child.Name)
	}
	return names
}

func TestDecorateLedgerFootprintGroupsByStorage(t *testing.T) {
	t.Parallel()

	instanceKey := xdr.ScVal{Type: xdr.ScValTypeScvLedgerKeyContractInstance}
	persistent := footprintContractDataKey(0x11, xdr.ContractDataDurabilityPersistent, footprintSymbolKey("COUNTER"))
	instance := footprintContractDataKey(0x22, xdr.ContractDataDurabilityPersistent, instanceKey)
	temporary := footprintContractDataKey(0x33, xdr.ContractDataDurabilityTemporary, footprintSymbolKey("SEEN"))
	otherPersistent := footprintContractDataKey(0x44, xdr.ContractDataDurabilityPersistent, footprintSymbolKey("TOTAL"))
	codeHash := xdr.Hash{0xAA, 0xBB}
	code := xdr.LedgerKey{
		Type:         xdr.LedgerEntryTypeContractCode,
		ContractCode: &xdr.LedgerKeyContractCode{Hash: codeHash},
	}
	config := xdr.LedgerKey{
		Type:          xdr.LedgerEntryTypeConfigSetting,
		ConfigSetting: &xdr.LedgerKeyConfigSetting{ConfigSettingId: xdr.ConfigSettingIdConfigSettingContractMaxSizeBytes},
	}
	fp := xdr.LedgerFootprint{
		ReadOnly:  []xdr.LedgerKey{instance, persistent, temporary, code},
		ReadWrite: []xdr.LedgerKey{temporary, config, otherPersistent},
	}

	node := nodeFor("Footprint", fp)
	if node == nil {
		t.Fatal("nodeFor returned nil for a footprint")
	}

	// The declared lists stay exactly as decoded; grouping is derived.
	if got := len(node.Child("ReadOnly").Children); got != 4 {
		t.Errorf("ReadOnly has %d entries, want 4", got)
	}
	if got := len(node.Child("ReadWrite").Children); got != 3 {
		t.Errorf("ReadWrite has %d entries, want 3", got)
	}

	assertFootprintGroup(t, node, "Persistent", []string{"readOnly[1]", "readWrite[2]"})
	assertFootprintGroup(t, node, "Temporary", []string{"readOnly[2]", "readWrite[0]"})
	assertFootprintGroup(t, node, "Instance", []string{"readOnly[0]"})
	assertFootprintGroup(t, node, "Other", []string{"readOnly[3]", "readWrite[1]"})

	wantOrder := []string{"ReadOnly", "ReadWrite", "Persistent", "Temporary", "Instance", "Other"}
	if got := childNames(node); !reflect.DeepEqual(got, wantOrder) {
		t.Errorf("footprint children = %v, want %v", got, wantOrder)
	}
}

func TestFootprintGroupsAnnotateContractIDs(t *testing.T) {
	t.Parallel()

	fp := xdr.LedgerFootprint{ReadOnly: []xdr.LedgerKey{
		footprintContractDataKey(0x11, xdr.ContractDataDurabilityPersistent, footprintSymbolKey("COUNTER")),
	}}
	node := nodeFor("Footprint", fp)
	if node == nil {
		t.Fatal("nodeFor returned nil for a footprint")
	}

	wantID, err := footprintContractAddress(0x11).String()
	if err != nil {
		t.Fatalf("encoding contract address: %v", err)
	}
	entry := node.Child("Persistent").Children[0]
	wantNote := "contract data for " + wantID
	if entry.Note != wantNote {
		t.Errorf("group entry note = %q, want %q", entry.Note, wantNote)
	}
	if !strings.HasPrefix(wantID, "C") {
		t.Errorf("contract id %q does not start with C", wantID)
	}
}

func TestFootprintGroupsOmitEmptyBuckets(t *testing.T) {
	t.Parallel()

	config := xdr.LedgerKey{
		Type:          xdr.LedgerEntryTypeConfigSetting,
		ConfigSetting: &xdr.LedgerKeyConfigSetting{ConfigSettingId: xdr.ConfigSettingIdConfigSettingContractMaxSizeBytes},
	}
	node := nodeFor("Footprint", xdr.LedgerFootprint{ReadOnly: []xdr.LedgerKey{config}})
	if node == nil {
		t.Fatal("nodeFor returned nil for a footprint")
	}
	if got, want := childNames(node), []string{"ReadOnly", "Other"}; !reflect.DeepEqual(got, want) {
		t.Errorf("footprint children = %v, want %v", got, want)
	}

	empty := nodeFor("Footprint", xdr.LedgerFootprint{})
	if empty == nil {
		t.Fatal("nodeFor returned nil for an empty footprint")
	}
	if len(empty.Children) != 0 {
		t.Errorf("empty footprint gained %d derived children", len(empty.Children))
	}
}

// TestLedgerFootprintGroupsFromFixture exercises the grouping through the same
// decode path the CLI uses, against the canonical Soroban transaction fixture.
func TestLedgerFootprintGroupsFromFixture(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "tx_soroban.env.txt"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	value, err := Decode(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("decoding fixture: %v", err)
	}
	footprint := value.Node.Find("FeeBump.Tx.InnerTx.V1.Tx.Ext.SorobanData.Resources.Footprint")
	if footprint == nil {
		t.Fatal("footprint not found in decoded fixture")
	}

	if got := len(footprint.Child("ReadOnly").Children); got != 10 {
		t.Errorf("ReadOnly has %d entries, want 10", got)
	}
	if got := len(footprint.Child("ReadWrite").Children); got != 6 {
		t.Errorf("ReadWrite has %d entries, want 6", got)
	}
	if footprint.Child("Persistent") != nil {
		t.Error("fixture has no plain persistent entries; group should be omitted")
	}
	assertFootprintGroup(t, footprint, "Temporary", []string{
		"readOnly[2]", "readOnly[3]", "readOnly[4]", "readOnly[5]", "readOnly[6]",
		"readWrite[1]", "readWrite[2]", "readWrite[3]", "readWrite[4]", "readWrite[5]",
	})
	assertFootprintGroup(t, footprint, "Instance", []string{"readOnly[0]", "readOnly[1]", "readOnly[7]"})
	assertFootprintGroup(t, footprint, "Other", []string{"readOnly[8]", "readOnly[9]", "readWrite[0]"})

	instance := footprint.Child("Instance").Children[0]
	if !strings.HasPrefix(instance.Note, "contract data for C") {
		t.Errorf("instance entry note = %q, want a contract id", instance.Note)
	}
	code := footprint.Child("Other").Children[0]
	if !strings.HasPrefix(code.Note, "contract code ") {
		t.Errorf("contract code note = %q, want the WASM hash", code.Note)
	}
}

func TestDecorateLedgerFootprintIgnoresOtherValues(t *testing.T) {
	t.Parallel()

	node := &Node{Name: "Footprint", Kind: KindStruct}
	decorateLedgerFootprint(node, "not a footprint")
	if len(node.Children) != 0 {
		t.Errorf("decorateLedgerFootprint accepted a non-footprint value and added %d children", len(node.Children))
	}
}

func TestFootprintStorageLevel(t *testing.T) {
	t.Parallel()

	instanceKey := xdr.ScVal{Type: xdr.ScValTypeScvLedgerKeyContractInstance}
	tests := []struct {
		name string
		key  xdr.LedgerKey
		want string
	}{
		{
			name: "persistent contract data",
			key:  footprintContractDataKey(0x01, xdr.ContractDataDurabilityPersistent, footprintSymbolKey("COUNTER")),
			want: footprintPersistent,
		},
		{
			name: "temporary contract data",
			key:  footprintContractDataKey(0x01, xdr.ContractDataDurabilityTemporary, footprintSymbolKey("COUNTER")),
			want: footprintTemporary,
		},
		{
			name: "instance storage",
			key:  footprintContractDataKey(0x01, xdr.ContractDataDurabilityPersistent, instanceKey),
			want: footprintInstance,
		},
		{
			name: "unknown durability",
			key:  footprintContractDataKey(0x01, xdr.ContractDataDurability(7), footprintSymbolKey("COUNTER")),
			want: footprintOther,
		},
		{
			name: "missing contract data payload",
			key:  xdr.LedgerKey{Type: xdr.LedgerEntryTypeContractData},
			want: footprintOther,
		},
		{
			name: "contract code",
			key:  xdr.LedgerKey{Type: xdr.LedgerEntryTypeContractCode},
			want: footprintOther,
		},
		{
			name: "unknown entry type",
			key:  xdr.LedgerKey{Type: xdr.LedgerEntryType(200)},
			want: footprintOther,
		},
	}
	for _, tc := range tests {
		if got := footprintStorageLevel(tc.key); got != tc.want {
			t.Errorf("%s: footprintStorageLevel = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestFootprintContractID(t *testing.T) {
	t.Parallel()

	want, err := footprintContractAddress(0x01).String()
	if err != nil {
		t.Fatalf("encoding contract address: %v", err)
	}
	tests := []struct {
		name string
		key  xdr.LedgerKey
		want string
	}{
		{
			name: "contract arm",
			key:  footprintContractDataKey(0x01, xdr.ContractDataDurabilityPersistent, footprintSymbolKey("COUNTER")),
			want: want,
		},
		{
			name: "account arm",
			key: xdr.LedgerKey{
				Type: xdr.LedgerEntryTypeContractData,
				ContractData: &xdr.LedgerKeyContractData{
					Contract: xdr.ScAddress{Type: xdr.ScAddressTypeScAddressTypeAccount},
				},
			},
			want: "",
		},
		{
			name: "unknown address type",
			key: xdr.LedgerKey{
				Type: xdr.LedgerEntryTypeContractData,
				ContractData: &xdr.LedgerKeyContractData{
					Contract: xdr.ScAddress{Type: xdr.ScAddressType(99)},
				},
			},
			want: "",
		},
		{
			name: "malformed contract arm",
			key: xdr.LedgerKey{
				Type: xdr.LedgerEntryTypeContractData,
				ContractData: &xdr.LedgerKeyContractData{
					Contract: xdr.ScAddress{Type: xdr.ScAddressTypeScAddressTypeContract},
				},
			},
			want: "",
		},
		{
			name: "missing contract data payload",
			key:  xdr.LedgerKey{Type: xdr.LedgerEntryTypeContractData},
			want: "",
		},
		{
			name: "contract code",
			key:  xdr.LedgerKey{Type: xdr.LedgerEntryTypeContractCode},
			want: "",
		},
	}
	for _, tc := range tests {
		if got := footprintContractID(tc.key); got != tc.want {
			t.Errorf("%s: footprintContractID = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestFootprintKeyNote(t *testing.T) {
	t.Parallel()

	hash := xdr.Hash{0xAA, 0xBB}
	tests := []struct {
		name string
		key  xdr.LedgerKey
		want string
	}{
		{
			name: "contract code hash",
			key:  xdr.LedgerKey{Type: xdr.LedgerEntryTypeContractCode, ContractCode: &xdr.LedgerKeyContractCode{Hash: hash}},
			want: "contract code " + hash.HexString(),
		},
		{
			name: "contract code without payload",
			key:  xdr.LedgerKey{Type: xdr.LedgerEntryTypeContractCode},
			want: "contract code",
		},
		{
			name: "ttl key",
			key:  xdr.LedgerKey{Type: xdr.LedgerEntryTypeTtl},
			want: "ttl",
		},
		{
			name: "claimable balance key",
			key:  xdr.LedgerKey{Type: xdr.LedgerEntryTypeClaimableBalance},
			want: "claimable balance",
		},
		{
			name: "unknown entry type",
			key:  xdr.LedgerKey{Type: xdr.LedgerEntryType(200)},
			want: "ledger entry type 200",
		},
	}
	for _, tc := range tests {
		if got := footprintKeyNote(tc.key); got != tc.want {
			t.Errorf("%s: footprintKeyNote = %q, want %q", tc.name, got, tc.want)
		}
	}
}
