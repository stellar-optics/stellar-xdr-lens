package lens_test

import (
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/strkey"
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

// contractErrorTrace builds the diagnostic events the Soroban host emits when
// a contract call fails with a contract error: the fn_call into the contract,
// the contract's log line, the host's error event escalating the contract
// error, and the core metrics. Topic and data shapes follow soroban-env-host.
func contractErrorTrace() []xdr.DiagnosticEvent {
	callee := xdr.ContractId{0xaa, 0xbb, 0xcc}
	calleeBytes := xdr.ScBytes(callee[:])
	caller := xdr.MustAddress("GCJNHFXJEQEY54NPKU4DBMT5WOBJUHMYSFBDAQF4OMTEGJN4BQU7ZST6")
	sym := func(s string) xdr.ScVal {
		v := xdr.ScSymbol(s)
		return xdr.ScVal{Type: xdr.ScValTypeScvSymbol, Sym: &v}
	}
	str := func(s string) xdr.ScVal {
		v := xdr.ScString(s)
		return xdr.ScVal{Type: xdr.ScValTypeScvString, Str: &v}
	}
	code := xdr.Uint32(1)
	insns := xdr.Uint64(1_234_567)
	event := func(contract *xdr.ContractId, inSuccess bool, data xdr.ScVal, topics ...xdr.ScVal) xdr.DiagnosticEvent {
		return xdr.DiagnosticEvent{
			InSuccessfulContractCall: inSuccess,
			Event: xdr.ContractEvent{
				ContractId: contract,
				Type:       xdr.ContractEventTypeDiagnostic,
				Body:       xdr.ContractEventBody{V: 0, V0: &xdr.ContractEventV0{Topics: topics, Data: data}},
			},
		}
	}
	return []xdr.DiagnosticEvent{
		event(nil, false,
			xdr.ScVal{Type: xdr.ScValTypeScvAddress, Address: &xdr.ScAddress{Type: xdr.ScAddressTypeScAddressTypeAccount, AccountId: &caller}},
			sym("fn_call"), xdr.ScVal{Type: xdr.ScValTypeScvBytes, Bytes: &calleeBytes}, sym("increment")),
		event(&callee, false, str("counter is not initialized"), sym("log")),
		event(&callee, false, str("escalating Ok(ScErrorType::Contract) frame-exit to Err"),
			sym("error"), xdr.ScVal{Type: xdr.ScValTypeScvError, Error: &xdr.ScError{Type: xdr.ScErrorTypeSceContract, ContractCode: &code}}),
		event(nil, true, xdr.ScVal{Type: xdr.ScValTypeScvU64, U64: &insns}, sym("core_metrics"), sym("cpu_insn")),
	}
}

func TestDecodeDiagnosticEventsFromMeta(t *testing.T) {
	t.Parallel()

	// Round-trip the trace through a Protocol 23 TransactionMetaV4 so the
	// events come out of the real decode path.
	meta := xdr.TransactionMeta{V: 4, V4: &xdr.TransactionMetaV4{DiagnosticEvents: contractErrorTrace()}}
	val, err := lens.DecodeAs(mustMarshal(t, meta), "TransactionMeta")
	if err != nil {
		t.Fatalf("DecodeAs(TransactionMeta) error = %v", err)
	}
	events := lens.DiagnosticEventsFromValue(val)
	if len(events) != 4 {
		t.Fatalf("DiagnosticEventsFromValue() = %d events, want 4", len(events))
	}

	// The fn_call topic carries the callee as raw bytes and the later
	// events carry it as the emitting contract; both must render as the
	// same strkey.
	callee := strkey.MustEncode(strkey.VersionByteContract, []byte{0xaa, 0xbb, 0xcc, 31: 0})
	tests := []struct {
		kind     string
		failed   bool
		contract string
		function string
		scError  string
		text     string
	}{
		{kind: "fn_call", failed: true, contract: callee, function: "increment",
			text: "[failed] fn_call " + callee + "::increment(GCJNHFXJEQEY54NPKU4DBMT5WOBJUHMYSFBDAQF4OMTEGJN4BQU7ZST6)"},
		{kind: "log", failed: true, contract: callee,
			text: "[failed] log from " + callee + ` topics=[log] data="counter is not initialized"`},
		{kind: "error", failed: true, contract: callee, scError: "Error(Contract, #1)",
			text: "[failed] error in contract " + callee + ` Error(Contract, #1): "escalating Ok(ScErrorType::Contract) frame-exit to Err"`},
		{kind: "core_metrics", text: "[success] core_metrics cpu_insn = 1234567"},
	}
	got := lens.DecodeDiagnosticEvents(events)
	for i, tc := range tests {
		t.Run(tc.kind, func(t *testing.T) {
			t.Parallel()
			e := got[i]
			if e.Index != i || e.Kind != tc.kind || e.Failed != tc.failed {
				t.Errorf("event %d = {Index:%d Kind:%q Failed:%v}, want {%d %q %v}", i, e.Index, e.Kind, e.Failed, i, tc.kind, tc.failed)
			}
			if e.ContractID != tc.contract || e.Function != tc.function || e.Error != tc.scError {
				t.Errorf("event %d = {ContractID:%q Function:%q Error:%q}, want {%q %q %q}",
					i, e.ContractID, e.Function, e.Error, tc.contract, tc.function, tc.scError)
			}
			if e.Text != tc.text {
				t.Errorf("Text = %q\nwant   %q", e.Text, tc.text)
			}
		})
	}
}

func TestFormatScError(t *testing.T) {
	t.Parallel()

	code := xdr.Uint32(42)
	missing := xdr.ScErrorCodeScecMissingValue
	invalid := xdr.ScErrorCodeScecInvalidAction
	tests := []struct {
		name string
		err  xdr.ScError
		want string
	}{
		{name: "contract code", err: xdr.ScError{Type: xdr.ScErrorTypeSceContract, ContractCode: &code}, want: "Error(Contract, #42)"},
		{name: "missing export", err: xdr.ScError{Type: xdr.ScErrorTypeSceWasmVm, Code: &missing}, want: "Error(WasmVm, MissingValue)"},
		{name: "auth", err: xdr.ScError{Type: xdr.ScErrorTypeSceAuth, Code: &invalid}, want: "Error(Auth, InvalidAction)"},
		{name: "no code", err: xdr.ScError{Type: xdr.ScErrorTypeSceBudget}, want: "Error(Budget)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := lens.FormatScError(tc.err); got != tc.want {
				t.Errorf("FormatScError() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAttachDiagnosticEventsOnlyOnFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		env     string
		res     string
		wantLen int
	}{
		{name: "failed transaction", env: "tx_failed.env", res: "tx_failed.res", wantLen: 4},
		{name: "successful transaction", env: "tx_soroban.env", res: "tx_soroban.res", wantLen: 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env, err := lens.Decode(loadFixture(t, tc.env))
			if err != nil {
				t.Fatal(err)
			}
			res, err := lens.Decode(loadFixture(t, tc.res))
			if err != nil {
				t.Fatal(err)
			}
			summary, err := lens.ExplainPair(env, res)
			if err != nil {
				t.Fatalf("ExplainPair() error = %v", err)
			}
			lens.AttachDiagnosticEvents(summary, contractErrorTrace())
			if got := len(summary.Outcome.Diagnostics); got != tc.wantLen {
				t.Errorf("Outcome.Diagnostics has %d events, want %d", got, tc.wantLen)
			}
		})
	}
}
