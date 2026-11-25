package lens_test

import (
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

func TestDiagnosticEventDecoding(t *testing.T) {
	t.Parallel()

	v, err := lens.DecodeAs(loadFixture(t, "diagnostic_event"), "DiagnosticEvent")
	if err != nil {
		t.Fatalf("DecodeAs() error = %v", err)
	}
	if v.Type != "DiagnosticEvent" {
		t.Errorf("Type = %q, want DiagnosticEvent", v.Type)
	}

	// Verify InSuccessfulContractCall
	inSuccess := v.Node.Find("InSuccessfulContractCall")
	if inSuccess == nil {
		t.Fatal("InSuccessfulContractCall not found")
	}
	if inSuccess.Value != true {
		t.Errorf("InSuccessfulContractCall = %v, want true", inSuccess.Value)
	}

	// Verify Event
	event := v.Node.Find("Event")
	if event == nil {
		t.Fatal("Event not found")
	}

	contractID := event.Child("ContractId")
	if contractID == nil {
		t.Fatal("ContractId not found")
	}
	const wantContractID = "CBBUGQ2DINBUGQ2DINBUGQ2DINBUGQ2DINBUGQ2DINBUGQ2DINBUHSPU"
	if contractID.Value != wantContractID {
		t.Errorf("ContractId = %q, want %q", contractID.Value, wantContractID)
	}

	eventType := event.Child("Type")
	if eventType == nil {
		t.Fatal("Type not found")
	}
	if eventType.Value != "ContractEventTypeContract" {
		t.Errorf("Type = %q, want ContractEventTypeContract", eventType.Value)
	}

	// Verify Body
	body := event.Child("Body")
	if body == nil {
		t.Fatal("Body not found")
	}

	v0 := body.Child("V0")
	if v0 == nil {
		t.Fatal("V0 not found")
	}

	// Verify Topics
	topics := v0.Child("Topics")
	if topics == nil {
		t.Fatal("Topics not found")
	}
	if topics.Kind != lens.KindList {
		t.Errorf("Topics Kind = %v, want KindList", topics.Kind)
	}
	if len(topics.Children) != 2 {
		t.Errorf("Topics has %d children, want 2", len(topics.Children))
	}

	topic0 := topics.Children[0]
	if topic0 == nil {
		t.Fatal("Topic[0] is nil")
	}
	topic0Str := topic0.Child("Str")
	if topic0Str == nil {
		t.Fatal("Topic[0].Str not found")
	}
	if topic0Str.Value != "transfer" {
		t.Errorf("Topic[0].Str = %q, want 'transfer'", topic0Str.Value)
	}

	topic1 := topics.Children[1]
	if topic1 == nil {
		t.Fatal("Topic[1] is nil")
	}
	addr := topic1.Child("Address")
	if addr == nil {
		t.Fatal("Topic[1].Address not found")
	}
	if !strings.HasPrefix(addr.Value.(string), "GCJN") {
		t.Errorf("Topic[1].Address = %q, want GCJN...", addr.Value)
	}

	// Verify Data (Map)
	data := v0.Child("Data")
	if data == nil {
		t.Fatal("Data not found")
	}
	dataMap := data.Child("Map")
	if dataMap == nil {
		t.Fatal("Data.Map not found")
	}
	if len(dataMap.Children) != 3 {
		t.Errorf("Data.Map has %d children, want 3", len(dataMap.Children))
	}

	// Check map entries
	keys := []string{"from", "to", "amount"}
	for i, key := range keys {
		entry := dataMap.Children[i]
		if entry == nil {
			t.Fatalf("Map[%d] is nil", i)
		}
		k := entry.Child("Key")
		if k == nil {
			t.Fatalf("Map[%d].Key not found", i)
		}
		strNode := k.Child("Str")
		if strNode == nil {
			t.Fatalf("Map[%d].Key.Str not found", i)
		}
		if strNode.Value != key {
			t.Errorf("Map[%d].Key.Str = %q, want %q", i, strNode.Value, key)
		}
	}

	// Check amount value is I128
	amountEntry := dataMap.Children[2]
	amountVal := amountEntry.Child("Val")
	if amountVal == nil {
		t.Fatal("Map[2].Val not found")
	}
	if amountVal.Child("Type").Value != "ScValTypeScvI128" {
		t.Errorf("Amount Type = %q, want ScValTypeScvI128", amountVal.Child("Type").Value)
	}
	i128 := amountVal.Child("I128")
	if i128 == nil {
		t.Fatal("I128 not found")
	}
	lo := i128.Child("Lo")
	if lo == nil || lo.Value != uint64(1000000000) {
		t.Errorf("I128.Lo = %v, want 1000000000", lo.Value)
	}
}

func TestDiagnosticEventDetection(t *testing.T) {
	t.Parallel()

	cands, err := lens.Detect(loadFixture(t, "diagnostic_event"))
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	found := false
	for _, c := range cands {
		if c.Type == "DiagnosticEvent" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Detect() did not find DiagnosticEvent among candidates: %v", cands)
	}
}

func TestContractEventDecoding(t *testing.T) {
	t.Parallel()

	// Decode DiagnosticEvent and verify ContractEvent fields are accessible
	v, err := lens.DecodeAs(loadFixture(t, "diagnostic_event"), "DiagnosticEvent")
	if err != nil {
		t.Fatalf("DecodeAs() error = %v", err)
	}

	event := v.Node.Find("Event")
	if event == nil {
		t.Fatal("Event not found")
	}

	// Verify ContractEvent structure
	if event.TypeName != "ContractEvent" {
		t.Errorf("Event TypeName = %q, want ContractEvent", event.TypeName)
	}

	// Verify ContractEventBody
	body := event.Child("Body")
	if body == nil {
		t.Fatal("Body not found")
	}
	if body.TypeName != "ContractEventBody" {
		t.Errorf("Body TypeName = %q, want ContractEventBody", body.TypeName)
	}

	// Verify ContractEventV0
	v0 := body.Child("V0")
	if v0 == nil {
		t.Fatal("V0 not found")
	}
	if v0.TypeName != "ContractEventV0" {
		t.Errorf("V0 TypeName = %q, want ContractEventV0", v0.TypeName)
	}
}

func TestScValEnrichmentInDiagnosticEvent(t *testing.T) {
	t.Parallel()

	v, err := lens.DecodeAs(loadFixture(t, "diagnostic_event"), "DiagnosticEvent")
	if err != nil {
		t.Fatalf("DecodeAs() error = %v", err)
	}

	// Check that ScVal types are properly rendered
	topics := v.Node.Find("Event.Body.V0.Topics")
	if topics == nil {
		t.Fatal("Topics not found")
	}

	// Topic 0: String
	topic0 := topics.Children[0]
	if topic0 == nil {
		t.Fatal("Topic[0] is nil")
	}
	typeNode := topic0.Child("Type")
	if typeNode == nil {
		t.Fatal("Topic[0].Type not found")
	}
	if typeNode.Value != "ScValTypeScvString" {
		t.Errorf("Topic[0].Type = %q, want ScValTypeScvString", typeNode.Value)
	}
	strNode := topic0.Child("Str")
	if strNode == nil {
		t.Fatal("Topic[0].Str not found")
	}
	if strNode.Value != "transfer" {
		t.Errorf("Topic[0].Str = %q, want 'transfer'", strNode.Value)
	}

	// Topic 1: Address
	topic1 := topics.Children[1]
	if topic1 == nil {
		t.Fatal("Topic[1] is nil")
	}
	typeNode = topic1.Child("Type")
	if typeNode == nil {
		t.Fatal("Topic[1].Type not found")
	}
	if typeNode.Value != "ScValTypeScvAddress" {
		t.Errorf("Topic[1].Type = %q, want ScValTypeScvAddress", typeNode.Value)
	}
	addr := topic1.Child("Address")
	if addr == nil {
		t.Fatal("Topic[1].Address not found")
	}
	if addr.Kind != lens.KindScalar {
		t.Errorf("Topic[1].Address Kind = %v, want KindScalar", addr.Kind)
	}
	if !strings.HasPrefix(addr.Value.(string), "GCJN") {
		t.Errorf("Topic[1].Address = %q, want GCJN...", addr.Value)
	}
}

func TestContractScAddressEnrichment(t *testing.T) {
	t.Parallel()

	var contractID xdr.ContractId
	copy(contractID[:], strings.Repeat("C", 32))
	val := xdr.ScVal{
		Type: xdr.ScValTypeScvAddress,
		Address: &xdr.ScAddress{
			Type:       xdr.ScAddressTypeScAddressTypeContract,
			ContractId: &contractID,
		},
	}

	v, err := lens.DecodeAs(mustMarshal(t, val), "ScVal")
	if err != nil {
		t.Fatalf("DecodeAs() error = %v", err)
	}
	addr := v.Node.Child("Address")
	if addr == nil {
		t.Fatal("Address not found")
	}
	const wantContractID = "CBBUGQ2DINBUGQ2DINBUGQ2DINBUGQ2DINBUGQ2DINBUGQ2DINBUHSPU"
	if addr.Value != wantContractID {
		t.Errorf("Address = %q, want %q", addr.Value, wantContractID)
	}
}

func TestEnrichDiagnosticEventWithTimestamp(t *testing.T) {
	t.Parallel()

	const ledgerSeq = 50_000_000
	const closeTime = int64(1_700_000_000) // 2023-11-14T22:13:20Z

	event := xdr.DiagnosticEvent{InSuccessfulContractCall: true}
	ts := lens.EnrichDiagnosticEventWithTimestamp(event, ledgerSeq, closeTime)
	if ts == nil {
		t.Fatal("EnrichDiagnosticEventWithTimestamp() returned nil for non-zero closeTime")
	}
	if ts.LedgerSeq != ledgerSeq {
		t.Errorf("LedgerSeq = %d, want %d", ts.LedgerSeq, ledgerSeq)
	}
	if ts.CloseTime.Unix() != closeTime {
		t.Errorf("CloseTime.Unix() = %d, want %d", ts.CloseTime.Unix(), closeTime)
	}
	if ts.Note == "" {
		t.Error("Note must not be empty")
	}
	// Note must include both the RFC3339 timestamp and the ledger number.
	if !strings.Contains(ts.Note, "2023-11-14") {
		t.Errorf("Note %q should contain the date 2023-11-14", ts.Note)
	}
	if !strings.Contains(ts.Note, "50000000") {
		t.Errorf("Note %q should contain the ledger sequence 50000000", ts.Note)
	}
	// String() must equal Note.
	if s := ts.String(); s != ts.Note {
		t.Errorf("String() = %q, want %q", s, ts.Note)
	}
}

func TestEnrichDiagnosticEventWithTimestampZeroTime(t *testing.T) {
	t.Parallel()

	event := xdr.DiagnosticEvent{InSuccessfulContractCall: true}
	ts := lens.EnrichDiagnosticEventWithTimestamp(event, 1234, 0)
	if ts != nil {
		t.Errorf("EnrichDiagnosticEventWithTimestamp() = %v, want nil for zero closeTime", ts)
	}
}

func TestDiagnosticEventNodeHookSuccessful(t *testing.T) {
	t.Parallel()

	v, err := lens.DecodeAs(loadFixture(t, "diagnostic_event"), "DiagnosticEvent")
	if err != nil {
		t.Fatalf("DecodeAs() error = %v", err)
	}
	// The nodeHook should set a note reflecting the InSuccessfulContractCall flag.
	if v.Node.Note == "" {
		t.Error("root Node.Note must not be empty after nodeHook")
	}
	if !strings.Contains(v.Node.Note, "contract call") {
		t.Errorf("Node.Note %q should describe the call outcome", v.Node.Note)
	}
}

// TestFormatContractEvent verifies FormatContractEvent produces readable output
// with the event type, contract ID and topic/data values.
func TestFormatContractEvent(t *testing.T) {
	t.Parallel()

	sym := xdr.ScSymbol("transfer")
	eventType := xdr.ContractEventTypeContract
	event := xdr.ContractEvent{
		Type: eventType,
		Body: xdr.ContractEventBody{
			V: 0,
			V0: &xdr.ContractEventV0{
				Topics: []xdr.ScVal{
					{Type: xdr.ScValTypeScvSymbol, Sym: &sym},
				},
				Data: xdr.ScVal{Type: xdr.ScValTypeScvVoid},
			},
		},
	}

	got := lens.FormatContractEvent(&event)
	if !strings.Contains(got, "type=") {
		t.Errorf("FormatContractEvent() = %q, want 'type=' prefix", got)
	}
	if !strings.Contains(got, "transfer") {
		t.Errorf("FormatContractEvent() = %q, want symbol 'transfer' in topics", got)
	}
	if !strings.Contains(got, "void") {
		t.Errorf("FormatContractEvent() = %q, want 'void' data", got)
	}
}

// TestFormatContractEventNil verifies FormatContractEvent is safe with nil input.
func TestFormatContractEventNil(t *testing.T) {
	t.Parallel()

	got := lens.FormatContractEvent(nil)
	if got == "" {
		t.Error("FormatContractEvent(nil) returned empty string, want a descriptive message")
	}
}
