package lens_test

import (
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/strkey"
	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

func TestDecodeTrapReasonEmpty(t *testing.T) {
	t.Parallel()

	reason, err := lens.DecodeTrapReason(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reason.Trapped {
		t.Error("expected Trapped == false for empty events")
	}
	if !strings.Contains(reason.Summary, "No diagnostic events") {
		t.Errorf("unexpected summary: %s", reason.Summary)
	}
}

func TestDecodeTrapReasonWithPanicMessage(t *testing.T) {
	t.Parallel()

	contractHash := xdr.Hash{1, 2, 3, 4, 5}
	contractID := xdr.ContractId(contractHash)

	events := []xdr.DiagnosticEvent{
		{
			InSuccessfulContractCall: true,
			Event: xdr.ContractEvent{
				ContractId: &contractID,
				Type:       xdr.ContractEventTypeDiagnostic,
				Body: xdr.ContractEventBody{
					V: 0,
					V0: &xdr.ContractEventV0{
						Topics: []xdr.ScVal{
							{
								Type: xdr.ScValTypeScvSymbol,
								Sym:  (*xdr.ScSymbol)(ptrString("fn_call")),
							},
							{
								Type: xdr.ScValTypeScvSymbol,
								Sym:  (*xdr.ScSymbol)(ptrString("transfer")),
							},
						},
						Data: xdr.ScVal{
							Type: xdr.ScValTypeScvVoid,
						},
					},
				},
			},
		},
		{
			InSuccessfulContractCall: false,
			Event: xdr.ContractEvent{
				ContractId: &contractID,
				Type:       xdr.ContractEventTypeDiagnostic,
				Body: xdr.ContractEventBody{
					V: 0,
					V0: &xdr.ContractEventV0{
						Topics: []xdr.ScVal{
							{
								Type: xdr.ScValTypeScvSymbol,
								Sym:  (*xdr.ScSymbol)(ptrString("log")),
							},
						},
						Data: xdr.ScVal{
							Type: xdr.ScValTypeScvString,
							Str:  (*xdr.ScString)(ptrString("assertion failed: balance >= amount")),
						},
					},
				},
			},
		},
	}

	reason, err := lens.DecodeTrapReason(events)
	if err != nil {
		t.Fatalf("DecodeTrapReason failed: %v", err)
	}

	if !reason.Trapped {
		t.Error("expected Trapped == true")
	}
	if reason.PanicMessage != "assertion failed: balance >= amount" {
		t.Errorf("expected panic message %q, got %q", "assertion failed: balance >= amount", reason.PanicMessage)
	}
	if reason.FunctionName != "transfer" {
		t.Errorf("expected function name transfer, got %q", reason.FunctionName)
	}
	if !strings.Contains(reason.Summary, "assertion failed") {
		t.Errorf("expected summary to contain panic message: %s", reason.Summary)
	}
	if len(reason.DiagnosticTrace) != 2 {
		t.Errorf("expected 2 trace items, got %d", len(reason.DiagnosticTrace))
	}
}

func TestDecodeTrapReasonWithScError(t *testing.T) {
	t.Parallel()

	contractHash := xdr.Hash{0xaa, 0xbb}
	contractID := xdr.ContractId(contractHash)
	code := xdr.ScErrorCodeScecIndexBounds

	events := []xdr.DiagnosticEvent{
		{
			InSuccessfulContractCall: false,
			Event: xdr.ContractEvent{
				ContractId: &contractID,
				Type:       xdr.ContractEventTypeDiagnostic,
				Body: xdr.ContractEventBody{
					V: 0,
					V0: &xdr.ContractEventV0{
						Topics: []xdr.ScVal{
							{
								Type: xdr.ScValTypeScvSymbol,
								Sym:  (*xdr.ScSymbol)(ptrString("error")),
							},
						},
						Data: xdr.ScVal{
							Type: xdr.ScValTypeScvError,
							Error: &xdr.ScError{
								Type: xdr.ScErrorTypeSceStorage,
								Code: &code,
							},
						},
					},
				},
			},
		},
	}

	reason, err := lens.DecodeTrapReason(events)
	if err != nil {
		t.Fatalf("DecodeTrapReason failed: %v", err)
	}

	if !reason.Trapped {
		t.Error("expected Trapped == true")
	}
	if reason.ErrorCode != "scec_index_bounds" {
		t.Errorf("expected scec_index_bounds, got %q", reason.ErrorCode)
	}
	if !strings.Contains(reason.Summary, "storage") {
		t.Errorf("expected summary to mention storage: %s", reason.Summary)
	}
}

func TestFormatDiagnosticEvent(t *testing.T) {
	t.Parallel()

	event := xdr.DiagnosticEvent{
		InSuccessfulContractCall: false,
		Event: xdr.ContractEvent{
			Type: xdr.ContractEventTypeDiagnostic,
			Body: xdr.ContractEventBody{
				V: 0,
				V0: &xdr.ContractEventV0{
					Topics: []xdr.ScVal{
						{
							Type: xdr.ScValTypeScvSymbol,
							Sym:  (*xdr.ScSymbol)(ptrString("log")),
						},
					},
					Data: xdr.ScVal{
						Type: xdr.ScValTypeScvString,
						Str:  (*xdr.ScString)(ptrString("test panic")),
					},
				},
			},
		},
	}

	str := lens.FormatDiagnosticEvent(event)
	if !strings.Contains(str, "[failed]") || !strings.Contains(str, "test panic") {
		t.Errorf("unexpected formatted event: %s", str)
	}
}

func TestScValToStringTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		val  xdr.ScVal
		want string
	}{
		{"bool true", xdr.ScVal{Type: xdr.ScValTypeScvBool, B: ptrBool(true)}, "true"},
		{"void", xdr.ScVal{Type: xdr.ScValTypeScvVoid}, "void"},
		{"u32", xdr.ScVal{Type: xdr.ScValTypeScvU32, U32: (*xdr.Uint32)(ptrUint32(123))}, "123"},
		{"i32", xdr.ScVal{Type: xdr.ScValTypeScvI32, I32: (*xdr.Int32)(ptrInt32(-456))}, "-456"},
		{"u64", xdr.ScVal{Type: xdr.ScValTypeScvU64, U64: (*xdr.Uint64)(ptrUint64(789))}, "789"},
		{"i64", xdr.ScVal{Type: xdr.ScValTypeScvI64, I64: (*xdr.Int64)(ptrInt64(-999))}, "-999"},
		{"string", xdr.ScVal{Type: xdr.ScValTypeScvString, Str: (*xdr.ScString)(ptrString("hello"))}, "hello"},
		{"symbol", xdr.ScVal{Type: xdr.ScValTypeScvSymbol, Sym: (*xdr.ScSymbol)(ptrString("world"))}, "world"},
		{"bytes", xdr.ScVal{Type: xdr.ScValTypeScvBytes, Bytes: (*xdr.ScBytes)(ptrBytes([]byte("raw")))}, "raw"},
		{"timepoint", xdr.ScVal{Type: xdr.ScValTypeScvTimepoint, Timepoint: (*xdr.TimePoint)(ptrTimePoint(1000))}, "timepoint(1000)"},
		{"duration", xdr.ScVal{Type: xdr.ScValTypeScvDuration, Duration: (*xdr.Duration)(ptrDuration(60))}, "duration(60)"},
		{"u128", xdr.ScVal{Type: xdr.ScValTypeScvU128, U128: &xdr.UInt128Parts{Hi: 1, Lo: 2}}, "u128(1, 2)"},
		{"i128", xdr.ScVal{Type: xdr.ScValTypeScvI128, I128: &xdr.Int128Parts{Hi: 3, Lo: 4}}, "i128(3, 4)"},
		{"u256", xdr.ScVal{Type: xdr.ScValTypeScvU256}, "u256(...)"},
		{"i256", xdr.ScVal{Type: xdr.ScValTypeScvI256}, "i256(...)"},
		{"map", xdr.ScVal{Type: xdr.ScValTypeScvMap}, "{...}"},
	}

	for _, tt := range tests {
		got := lens.ScValToString(tt.val)
		if got != tt.want {
			t.Errorf("ScValToString(%s) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestExtractDiagnosticEventsAndMeta(t *testing.T) {
	t.Parallel()

	// nil meta
	events := lens.ExtractDiagnosticEvents(nil)
	if events != nil {
		t.Error("expected nil events for nil meta")
	}

	_, err := lens.DecodeTrapFromMeta(nil)
	if err == nil {
		t.Error("expected error for nil meta")
	}

	// Meta V3 with diagnostic events
	testEvents := []xdr.DiagnosticEvent{
		{
			InSuccessfulContractCall: false,
			Event: xdr.ContractEvent{
				Type: xdr.ContractEventTypeDiagnostic,
			},
		},
	}

	meta := &xdr.TransactionMeta{
		V: 3,
		V3: &xdr.TransactionMetaV3{
			SorobanMeta: &xdr.SorobanTransactionMeta{
				DiagnosticEvents: testEvents,
			},
		},
	}

	extracted := lens.ExtractDiagnosticEvents(meta)
	if len(extracted) != 1 {
		t.Fatalf("expected 1 extracted event, got %d", len(extracted))
	}

	trap, err := lens.DecodeTrapFromMeta(meta)
	if err != nil {
		t.Fatalf("DecodeTrapFromMeta failed: %v", err)
	}
	if !trap.Trapped {
		t.Error("expected Trapped == true")
	}
}

func TestScValToStringAddress(t *testing.T) {
	t.Parallel()

	accountHash := xdr.Uint256{1, 2, 3}
	accAddr := xdr.ScAddress{
		Type: xdr.ScAddressTypeScAddressTypeAccount,
		AccountId: &xdr.AccountId{
			Type:    xdr.PublicKeyTypePublicKeyTypeEd25519,
			Ed25519: &accountHash,
		},
	}
	accVal := xdr.ScVal{
		Type:    xdr.ScValTypeScvAddress,
		Address: &accAddr,
	}
	str := lens.ScValToString(accVal)
	if !strings.HasPrefix(str, "G") {
		t.Errorf("expected G... account strkey address, got %q", str)
	}

	contractHash := xdr.Hash{4, 5, 6}
	contractID := xdr.ContractId(contractHash)
	contractAddr := xdr.ScAddress{
		Type:       xdr.ScAddressTypeScAddressTypeContract,
		ContractId: &contractID,
	}
	contractVal := xdr.ScVal{
		Type:    xdr.ScValTypeScvAddress,
		Address: &contractAddr,
	}
	cStr := lens.ScValToString(contractVal)
	if !strings.HasPrefix(cStr, "C") {
		t.Errorf("expected C... contract strkey address, got %q", cStr)
	}
}

func TestCorrelateTrapWithMetaEvents(t *testing.T) {
	t.Parallel()

	contractHash := xdr.Hash{1, 2, 3, 4, 5}
	contractID := xdr.ContractId(contractHash)
	testEvents := []xdr.DiagnosticEvent{
		{
			InSuccessfulContractCall: false,
			Event: xdr.ContractEvent{
				ContractId: &contractID,
				Type:       xdr.ContractEventTypeSystem,
				Body: xdr.ContractEventBody{
					V: 0,
					V0: &xdr.ContractEventV0{
						Topics: []xdr.ScVal{
							{Type: xdr.ScValTypeScvSymbol, Sym: (*xdr.ScSymbol)(ptrString("panic"))},
						},
						Data: xdr.ScVal{
							Type: xdr.ScValTypeScvString,
							Str:  (*xdr.ScString)(ptrString("out of gas")),
						},
					},
				},
			},
		},
	}

	meta := &xdr.TransactionMeta{
		V: 3,
		V3: &xdr.TransactionMetaV3{
			SorobanMeta: &xdr.SorobanTransactionMeta{
				DiagnosticEvents: testEvents,
			},
		},
	}

	failedPayload := loadFixture(t, "tx_failed.res")
	decodedFailed, err := lens.Decode(failedPayload)
	if err != nil {
		t.Fatalf("decoding failed res: %v", err)
	}

	res := decodedFailed.Raw.(*xdr.TransactionResult)
	trap, err := lens.CorrelateTrap(res, meta)
	if err != nil {
		t.Fatalf("CorrelateTrap failed: %v", err)
	}
	if !trap.Trapped {
		t.Error("expected Trapped == true")
	}
	if trap.PanicMessage != "out of gas" {
		t.Errorf("expected panic message %q, got %q", "out of gas", trap.PanicMessage)
	}

	// Correlate nil checks
	if _, err := lens.CorrelateTrap(nil, nil); err == nil {
		t.Error("expected error for nil result")
	}
	if _, err := lens.CorrelateTrapFromValues(nil, nil); err == nil {
		t.Error("expected error for nil resVal")
	}
	metaVal := &lens.Value{Type: "TransactionMeta", Raw: meta}
	trapVal, err := lens.CorrelateTrapFromValues(decodedFailed, metaVal)
	if err != nil {
		t.Fatalf("CorrelateTrapFromValues failed: %v", err)
	}
	if !trapVal.Trapped || trapVal.PanicMessage != "out of gas" {
		t.Errorf("expected trapped out of gas, got %+v", trapVal)
	}
}

func ptrString(s string) *string {
	return &s
}

func ptrBool(b bool) *bool {
	return &b
}

func ptrInt32(i int32) *int32 {
	return &i
}

func ptrUint64(u uint64) *uint64 {
	return &u
}

func ptrInt64(i int64) *int64 {
	return &i
}

func ptrBytes(b []byte) *[]byte {
	return &b
}

func ptrTimePoint(t uint64) *uint64 {
	return &t
}

func ptrDuration(d uint64) *uint64 {
	return &d
}

// hostErrorTrace is what the host emits when a call fails with a host
// error: the fn_call into the callee, then an error event whose second topic
// is the ScError and whose data is the host's message.
func hostErrorTrace(errType xdr.ScErrorType, code xdr.ScErrorCode, msg string) []xdr.DiagnosticEvent {
	callee := xdr.ContractId{0x10, 0x20}
	calleeBytes := xdr.ScBytes(callee[:])
	sym := func(s string) xdr.ScVal {
		v := xdr.ScSymbol(s)
		return xdr.ScVal{Type: xdr.ScValTypeScvSymbol, Sym: &v}
	}
	text := xdr.ScString(msg)
	return []xdr.DiagnosticEvent{
		{Event: xdr.ContractEvent{Type: xdr.ContractEventTypeDiagnostic, Body: xdr.ContractEventBody{V0: &xdr.ContractEventV0{
			Topics: []xdr.ScVal{sym("fn_call"), {Type: xdr.ScValTypeScvBytes, Bytes: &calleeBytes}, sym("harvest")},
			Data:   xdr.ScVal{Type: xdr.ScValTypeScvVoid},
		}}}},
		{Event: xdr.ContractEvent{ContractId: &callee, Type: xdr.ContractEventTypeDiagnostic, Body: xdr.ContractEventBody{V0: &xdr.ContractEventV0{
			Topics: []xdr.ScVal{sym("error"), {Type: xdr.ScValTypeScvError, Error: &xdr.ScError{Type: errType, Code: &code}}},
			Data:   xdr.ScVal{Type: xdr.ScValTypeScvString, Str: &text},
		}}}},
	}
}

func TestDecodeTrapReasonClassifiesHostErrors(t *testing.T) {
	t.Parallel()

	callee := xdr.ContractId{0x10, 0x20}
	calleeID := strkey.MustEncode(strkey.VersionByteContract, callee[:])

	tests := []struct {
		name        string
		errType     xdr.ScErrorType
		code        xdr.ScErrorCode
		msg         string
		wantSummary []string
	}{
		{
			name: "entry point not found", errType: xdr.ScErrorTypeSceWasmVm, code: xdr.ScErrorCodeScecMissingValue,
			msg:         "trying to invoke non-existent contract function",
			wantSummary: []string{"Entry point not found", "contract " + calleeID, `does not export a function named "harvest"`, "Error(WasmVm, MissingValue)"},
		},
		{
			name: "auth verification failure", errType: xdr.ScErrorTypeSceAuth, code: xdr.ScErrorCodeScecInvalidAction,
			msg:         "failed account authentication",
			wantSummary: []string{"Authorization verification failed", calleeID + "::harvest", "Error(Auth, InvalidAction)"},
		},
		{
			name: "budget exceeded", errType: xdr.ScErrorTypeSceBudget, code: xdr.ScErrorCodeScecExceededLimit,
			msg:         "budget exceeded",
			wantSummary: []string{calleeID + "::harvest", "CPU instruction or memory budget", "Error(Budget, ExceededLimit)"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reason, err := lens.DecodeTrapReason(hostErrorTrace(tc.errType, tc.code, tc.msg))
			if err != nil {
				t.Fatalf("DecodeTrapReason() error = %v", err)
			}
			if reason.ContractID != calleeID || reason.FunctionName != "harvest" {
				t.Errorf("target = %s::%s, want %s::harvest", reason.ContractID, reason.FunctionName, calleeID)
			}
			for _, want := range tc.wantSummary {
				if !strings.Contains(reason.Summary, want) {
					t.Errorf("Summary %q does not contain %q", reason.Summary, want)
				}
			}
			if reason.Hint == "" {
				t.Error("Hint is empty")
			}
		})
	}
}

// explainWithInvokeResult pairs the real trapped fixture envelope, after
// edit has adjusted it, with its real result re-coded to code.
func explainWithInvokeResult(t *testing.T, code xdr.InvokeHostFunctionResultCode, edit func(*xdr.SorobanTransactionData)) *lens.Summary {
	t.Helper()
	envVal, err := lens.Decode(loadFixture(t, "tx_failed.env"))
	if err != nil {
		t.Fatal(err)
	}
	resVal, err := lens.Decode(loadFixture(t, "tx_failed.res"))
	if err != nil {
		t.Fatal(err)
	}
	env := *envVal.Raw.(*xdr.TransactionEnvelope)
	res := *resVal.Raw.(*xdr.TransactionResult)

	if edit != nil {
		data, ok := env.FeeBump.Tx.InnerTx.V1.Tx.Ext.GetSorobanData()
		if !ok {
			t.Fatal("fixture envelope carries no Soroban data")
		}
		edit(&data)
		env.FeeBump.Tx.InnerTx.V1.Tx.Ext.SorobanData = &data
	}
	inner := res.Result.InnerResultPair.Result.Result.Results
	(*inner)[0].Tr.InvokeHostFunctionResult.Code = code

	envB64, err := xdr.MarshalBase64(env)
	if err != nil {
		t.Fatal(err)
	}
	resB64, err := xdr.MarshalBase64(res)
	if err != nil {
		t.Fatal(err)
	}
	envOut, err := lens.Decode(envB64)
	if err != nil {
		t.Fatal(err)
	}
	resOut, err := lens.Decode(resB64)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := lens.ExplainPair(envOut, resOut)
	if err != nil {
		t.Fatalf("ExplainPair() error = %v", err)
	}
	return summary
}

func TestExplainInvokeHostFunctionFailureNamesTarget(t *testing.T) {
	t.Parallel()

	// The real fixture calls harvest on this contract with two arguments.
	const target = "contract CBGSBKYMYO6OMGHQXXNOBRGVUDFUDVC2XLC3SXON5R2SNXILR7XCKKY3 function harvest"

	tests := []struct {
		name        string
		code        xdr.InvokeHostFunctionResultCode
		edit        func(*xdr.SorobanTransactionData)
		wantSummary []string
		wantHint    string
	}{
		{
			name:        "trapped",
			code:        xdr.InvokeHostFunctionResultCodeInvokeHostFunctionTrapped,
			wantSummary: []string{target, "trapped"},
			wantHint:    `does not export a function named "harvest"`,
		},
		{
			name: "resource limit with declared values within the protocol limits",
			code: xdr.InvokeHostFunctionResultCodeInvokeHostFunctionResourceLimitExceeded,
			wantSummary: []string{target, "ran past its declared resources",
				"CPU instructions 2,367,776 of 400,000,000", "disk read bytes 116 of 200,000", "write bytes 116 of 132,096"},
			wantHint: "Re-run simulation",
		},
		{
			name: "resource limit with a declaration above the protocol limit",
			code: xdr.InvokeHostFunctionResultCodeInvokeHostFunctionResourceLimitExceeded,
			edit: func(d *xdr.SorobanTransactionData) { d.Resources.WriteBytes = 200_000 },
			wantSummary: []string{target, "declared more than the Protocol 27 per-transaction limit",
				"write bytes 200,000 of 132,096"},
			wantHint: "Lower the declared value",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			summary := explainWithInvokeResult(t, tc.code, tc.edit)
			r := summary.Operations[0].Result
			if r == nil {
				t.Fatal("operation 0 has no result")
			}
			for _, want := range tc.wantSummary {
				if !strings.Contains(r.Summary, want) {
					t.Errorf("Summary %q does not contain %q", r.Summary, want)
				}
			}
			if !strings.Contains(r.Hint, tc.wantHint) {
				t.Errorf("Hint %q does not contain %q", r.Hint, tc.wantHint)
			}
			if !strings.Contains(r.Context, "invoke_contract: contract CBGSBKYMYO6OMGHQXXNOBRGVUDFUDVC2XLC3SXON5R2SNXILR7XCKKY3, function harvest, 2 argument(s)") {
				t.Errorf("Context = %q, want the invocation context", r.Context)
			}
			if tc.edit == nil && strings.Contains(r.Summary, "declared more than") {
				t.Error("a within-limit declaration was reported as over the limit")
			}
		})
	}
}

func TestDescribeHostFunctionInvocation(t *testing.T) {
	t.Parallel()

	wasm := []byte("\x00asm\x01\x00\x00\x00")
	tests := []struct {
		name   string
		op     xdr.InvokeHostFunctionOp
		want   string
		target string
	}{
		{
			name: "upload",
			op: xdr.InvokeHostFunctionOp{HostFunction: xdr.HostFunction{
				Type: xdr.HostFunctionTypeHostFunctionTypeUploadContractWasm, Wasm: &wasm}},
			want: lens.HostFunctionUploadContractWasm, target: "contract upload (8 bytes of WASM)",
		},
		{
			name: "create",
			op: xdr.InvokeHostFunctionOp{HostFunction: xdr.HostFunction{
				Type: xdr.HostFunctionTypeHostFunctionTypeCreateContractV2, CreateContractV2: &xdr.CreateContractArgsV2{}}},
			want: lens.HostFunctionCreateContract, target: "contract creation",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			inv := lens.DescribeHostFunctionInvocation(tc.op)
			if inv.Kind != tc.want || inv.Target() != tc.target {
				t.Errorf("got kind %q target %q, want %q %q", inv.Kind, inv.Target(), tc.want, tc.target)
			}
		})
	}
}
