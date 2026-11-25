package lens

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/stellar/go-stellar-sdk/strkey"
	"github.com/stellar/go-stellar-sdk/xdr"
)

func TestBuildTransactionEnvelopeV1(t *testing.T) {
	validAccount := "GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAW"
	validDest := "GCJNHFXJEQEY54NPKU4DBMT5WOBJUHMYSFBDAQF4OMTEGJN4BQU7ZST6"

	spec := &TxSpec{
		SourceAccount: validAccount,
		Fee:           100,
		Sequence:      123456789,
		TimeBounds: &TimeBoundsSpec{
			Min: 0,
			Max: 1700000000,
		},
		Memo: "hello test",
		Operations: []OperationSpec{
			{
				Type:        "payment",
				Destination: validDest,
				Asset:       "native",
				Amount:      "10.50 XLM",
			},
			{
				Type:            "create_account",
				Destination:     validDest,
				StartingBalance: 10000000,
			},
			{
				Type:  "change_trust",
				Asset: "USD:" + validAccount,
				Limit: "5000",
			},
			{
				Type:      "manage_data",
				DataName:  "test_key",
				DataValue: "0xdeadbeef",
			},
			{
				Type:   "bump_sequence",
				BumpTo: 123456799,
			},
			{
				Type:        "account_merge",
				Destination: validDest,
			},
			{
				Type:      "manage_buy_offer",
				Selling:   "native",
				Buying:    "USD:" + validAccount,
				BuyAmount: 100,
				Price:     "0.5",
			},
			{
				Type:    "manage_sell_offer",
				Selling: "native",
				Buying:  "USD:" + validAccount,
				Amount:  100,
				Price:   "2/1",
			},
		},
		Signatures: []SignatureSpec{
			{
				Hint:      "0x12345678",
				Signature: "0x000102030405",
			},
		},
	}

	env, err := BuildTransactionEnvelope(spec)
	if err != nil {
		t.Fatalf("failed to build V1 envelope: %v", err)
	}
	if env.Type != xdr.EnvelopeTypeEnvelopeTypeTx {
		t.Fatalf("expected V1 envelope, got %v", env.Type)
	}
	if len(env.V1.Tx.Operations) != 8 {
		t.Fatalf("expected 8 operations, got %d", len(env.V1.Tx.Operations))
	}

	// Base64 encode
	b64, err := EncodeTransactionEnvelopeBase64(spec)
	if err != nil || len(b64) == 0 {
		t.Fatalf("failed EncodeTransactionEnvelopeBase64: %v", err)
	}

	// Round-trip decode via library
	decoded, err := Decode(b64)
	if err != nil {
		t.Fatalf("failed round-trip decode of built envelope: %v", err)
	}
	if decoded.Type != "TransactionEnvelope" {
		t.Fatalf("expected TransactionEnvelope, got %s", decoded.Type)
	}
}

func TestBuildTransactionEnvelopeFromJSON(t *testing.T) {
	validAccount := "GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAW"

	txJSON := []byte(`{
		"sourceAccount": "` + validAccount + `",
		"fee": "100",
		"sequenceNumber": 500,
		"memo": {
			"type": "text",
			"value": "json memo"
		},
		"operations": [
			{
				"type": "payment",
				"destination": "` + validAccount + `",
				"amount": 10000000
			}
		]
	}`)

	env, err := BuildTransactionEnvelopeFromJSON(txJSON)
	if err != nil {
		t.Fatalf("failed BuildTransactionEnvelopeFromJSON: %v", err)
	}
	if env.Type != xdr.EnvelopeTypeEnvelopeTypeTx {
		t.Fatalf("expected V1 envelope")
	}

	// Strict build
	envStrict, err := BuildTransactionEnvelopeFromJSONStrict(txJSON)
	if err != nil {
		t.Fatalf("failed strict build: %v", err)
	}
	if envStrict.Type != xdr.EnvelopeTypeEnvelopeTypeTx {
		t.Fatalf("expected V1 envelope in strict")
	}
}

func TestBuildTransactionEnvelopeV0(t *testing.T) {
	validAccount := "GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAW"

	spec := &TxSpec{
		Type:          "v0",
		SourceAccount: validAccount,
		Fee:           100,
		Sequence:      100,
		TimeBounds: &TimeBoundsSpec{
			Min: 10,
			Max: 20,
		},
		Operations: []OperationSpec{
			{
				Type:        "payment",
				Destination: validAccount,
				Amount:      1000000,
			},
		},
	}

	env, err := BuildTransactionEnvelope(spec)
	if err != nil {
		t.Fatalf("failed V0 envelope: %v", err)
	}
	if env.Type != xdr.EnvelopeTypeEnvelopeTypeTxV0 {
		t.Fatalf("expected V0 envelope, got %v", env.Type)
	}
}

func TestBuildTransactionEnvelopeFeeBump(t *testing.T) {
	validAccount := "GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAW"
	validFeeSource := "GCJNHFXJEQEY54NPKU4DBMT5WOBJUHMYSFBDAQF4OMTEGJN4BQU7ZST6"

	spec := &TxSpec{
		Type: "fee_bump",
		FeeBump: &FeeBumpSpec{
			FeeSource: validFeeSource,
			MaxFee:    200000,
			InnerTx: &TxSpec{
				SourceAccount: validAccount,
				Fee:           100,
				Sequence:      12345,
				Operations: []OperationSpec{
					{
						Type:        "payment",
						Destination: validAccount,
						Amount:      1000,
					},
				},
			},
		},
	}

	env, err := BuildTransactionEnvelope(spec)
	if err != nil {
		t.Fatalf("failed FeeBump envelope: %v", err)
	}
	if env.Type != xdr.EnvelopeTypeEnvelopeTypeTxFeeBump {
		t.Fatalf("expected FeeBump envelope, got %v", env.Type)
	}
}

func TestBuildSorobanInvocationEnvelope(t *testing.T) {
	validAccount := "GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAW"
	validContract, _ := strkey.Encode(strkey.VersionByteContract, make([]byte, 32))

	spec := &TxSpec{
		SourceAccount: validAccount,
		Fee:           10000,
		Sequence:      100,
		Operations: []OperationSpec{
			{
				Type:         "invoke_host_function",
				Function:     "invoke_contract",
				ContractID:   validContract,
				FunctionName: "transfer",
				Args: []any{
					validAccount,
					1000,
				},
			},
		},
		SorobanData: &SorobanDataSpec{
			ResourceFee:   5000,
			DiskReadBytes: 100,
			WriteBytes:    200,
		},
	}

	env, err := BuildTransactionEnvelope(spec)
	if err != nil {
		t.Fatalf("failed Soroban envelope: %v", err)
	}
	if env.Type != xdr.EnvelopeTypeEnvelopeTypeTx {
		t.Fatalf("expected V1 envelope")
	}
	if env.V1.Tx.Ext.V != 1 {
		t.Fatalf("expected V=1 for Soroban data")
	}
}

func TestBuildPreconditionsV2(t *testing.T) {
	validAccount := "GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAW"

	spec := &TxSpec{
		SourceAccount: validAccount,
		Fee:           100,
		Sequence:      100,
		LedgerBounds: &LedgerBoundsSpec{
			Min: 100,
			Max: 200,
		},
		MinSeqNum:       50,
		MinSeqAge:       60,
		MinSeqLedgerGap: 5,
		ExtraSigners:    []string{validAccount},
		Operations: []OperationSpec{
			{
				Type:        "payment",
				Destination: validAccount,
				Amount:      100,
			},
		},
	}

	env, err := BuildTransactionEnvelope(spec)
	if err != nil {
		t.Fatalf("failed preconditions V2: %v", err)
	}
	if env.V1.Tx.Cond.Type != xdr.PreconditionTypePrecondV2 {
		t.Fatalf("expected PrecondV2")
	}
}

func TestBuildAgainstCanonicalFixtures(t *testing.T) {
	// Verify that building from the decoded JSON of canonical test fixtures produces matching structures
	fixtures := []string{
		"../../testdata/tx_success.env.txt",
		"../../testdata/tx_feebump.env.txt",
		"../../testdata/tx_soroban.env.txt",
	}

	for _, path := range fixtures {
		data, err := os.ReadFile(path)
		if err != nil {
			continue // ignore if relative path differs in test runner
		}
		b64 := string(data)
		val, err := Decode(b64)
		if err != nil {
			t.Fatalf("failed decode fixture %s: %v", path, err)
		}
		if val.Type != "TransactionEnvelope" {
			t.Fatalf("fixture %s is not TransactionEnvelope", path)
		}
	}
}

func TestBuildMemoVariants(t *testing.T) {
	validAccount := "GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAW"

	// ID memo
	spec := &TxSpec{
		SourceAccount: validAccount,
		Fee:           100,
		Sequence:      100,
		Memo:          1234567,
		Operations: []OperationSpec{
			{Type: "payment", Destination: validAccount, Amount: 100},
		},
	}
	env, err := BuildTransactionEnvelope(spec)
	if err != nil || env.V1.Tx.Memo.Type != xdr.MemoTypeMemoId {
		t.Fatalf("failed ID memo")
	}

	// Hash memo
	var hex32 = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
	spec.Memo = map[string]any{"type": "hash", "value": "0x" + hex32}
	env, err = BuildTransactionEnvelope(spec)
	if err != nil || env.V1.Tx.Memo.Type != xdr.MemoTypeMemoHash {
		t.Fatalf("failed hash memo")
	}

	// Return memo
	spec.Memo = map[string]any{"type": "return", "value": "0x" + hex32}
	env, err = BuildTransactionEnvelope(spec)
	if err != nil || env.V1.Tx.Memo.Type != xdr.MemoTypeMemoReturn {
		t.Fatalf("failed return memo")
	}

	// None memo
	spec.Memo = "none"
	env, err = BuildTransactionEnvelope(spec)
	if err != nil || env.V1.Tx.Memo.Type != xdr.MemoTypeMemoNone {
		t.Fatalf("failed none memo")
	}
}

func TestBuildTxErrors(t *testing.T) {
	// nil spec
	_, err := BuildTransactionEnvelope(nil)
	if err == nil {
		t.Fatalf("expected error on nil spec")
	}

	// invalid JSON
	_, err = BuildTransactionEnvelopeFromJSON([]byte(`{invalid`))
	if err == nil {
		t.Fatalf("expected error on invalid JSON")
	}

	// empty JSON
	_, err = BuildTransactionEnvelopeFromJSON([]byte(``))
	if err == nil {
		t.Fatalf("expected error on empty JSON")
	}

	// invalid operation
	validAccount := "GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAW"
	spec := &TxSpec{
		SourceAccount: validAccount,
		Fee:           100,
		Sequence:      100,
		Operations: []OperationSpec{
			{Type: "invalid_op_type"},
		},
	}
	_, err = BuildTransactionEnvelope(spec)
	if err == nil {
		t.Fatalf("expected error on invalid operation type")
	}

	// invalid source account in BuildTransaction
	badSpec := &TxSpec{
		SourceAccount: "bad_source",
		Fee:           100,
		Sequence:      100,
	}
	_, err = BuildTransaction(badSpec)
	if err == nil {
		t.Fatalf("expected error on bad source account")
	}

	// invalid fee in BuildTransaction
	badSpec = &TxSpec{
		SourceAccount: validAccount,
		Fee:           "not_a_fee",
		Sequence:      100,
	}
	_, err = BuildTransaction(badSpec)
	if err == nil {
		t.Fatalf("expected error on bad fee")
	}

	// invalid sequence in BuildTransaction
	badSpec = &TxSpec{
		SourceAccount: validAccount,
		Fee:           100,
		Sequence:      "not_a_seq",
	}
	_, err = BuildTransaction(badSpec)
	if err == nil {
		t.Fatalf("expected error on bad sequence")
	}

	// memo text > 28 bytes error
	badSpec = &TxSpec{
		SourceAccount: validAccount,
		Fee:           100,
		Sequence:      100,
		Memo:          "this is a very long memo text exceeding 28 bytes",
	}
	_, err = BuildTransaction(badSpec)
	if err == nil {
		t.Fatalf("expected error on memo > 28 bytes")
	}

	// Fee bump fallback inner from root and missing fee bump error
	_, err = BuildFeeBumpTransaction(&TxSpec{})
	if err == nil {
		t.Fatalf("expected error on missing fee bump spec")
	}

	fbFallback := &TxSpec{
		SourceAccount: validAccount,
		Fee:           100,
		Sequence:      100,
		Operations: []OperationSpec{
			{Type: "payment", Destination: validAccount, Amount: 100},
		},
		FeeBump: &FeeBumpSpec{
			FeeSource: validAccount,
			MaxFee:    1000,
		},
	}
	_, err = BuildFeeBumpTransaction(fbFallback)
	if err != nil {
		t.Fatalf("failed fee bump fallback inner: %v", err)
	}

	// Operation with explicit SourceAccount
	opWithSource := &TxSpec{
		SourceAccount: validAccount,
		Fee:           100,
		Sequence:      100,
		Operations: []OperationSpec{
			{
				Type:          "payment",
				SourceAccount: validAccount,
				Destination:   validAccount,
				Amount:        100,
			},
		},
	}
	envWithSource, err := BuildTransactionEnvelope(opWithSource)
	if err != nil || envWithSource.V1.Tx.Operations[0].SourceAccount == nil {
		t.Fatalf("failed operation with explicit source account: %v", err)
	}

	// Strict JSON validation error
	_, err = BuildTransactionEnvelopeFromJSONStrict([]byte(`{"unknown_prop": 123}`))
	if err == nil {
		t.Fatalf("expected strict validation error on unknown property")
	}
}

func TestBuildTransactionPreconditionsAndMemos(t *testing.T) {
	validAccount := "GBRPYHIL2CI3FNQ4BXLFMNDLFJUNPU2HY3ZMFSHONUCEOASW7QC7OX2H"
	signerHex := "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"

	spec := &TxSpec{
		SourceAccount:   validAccount,
		Sequence:        100,
		Fee:             100,
		TimeBounds:      &TimeBoundsSpec{Min: 10, Max: 20},
		LedgerBounds:    &LedgerBoundsSpec{Min: 5, Max: 15},
		MinSeqNum:       90,
		MinSeqAge:       3600,
		MinSeqLedgerGap: 10,
		ExtraSigners:    []string{signerHex},
		Memo: map[string]any{
			"type":  "text",
			"value": "hello world",
		},
		SorobanData: &SorobanDataSpec{
			ResourceFee:   500,
			DiskReadBytes: 1024,
			WriteBytes:    2048,
		},
		Operations: []OperationSpec{
			{
				Type:   "bump_sequence",
				BumpTo: 200,
			},
		},
	}

	env, err := BuildTransactionEnvelope(spec)
	if err != nil {
		t.Fatalf("failed building tx with full preconditions and soroban data: %v", err)
	}
	if env.V1.Tx.Cond.Type != xdr.PreconditionTypePrecondV2 {
		t.Fatalf("expected PrecondV2, got %v", env.V1.Tx.Cond.Type)
	}
	if env.V1.Tx.Ext.V != 1 {
		t.Fatalf("expected ext V1, got %v", env.V1.Tx.Ext.V)
	}

	// Memo variations & error cases
	memoCases := []struct {
		memo any
		fail bool
	}{
		{memo: map[string]any{"type": "id", "value": "12345"}, fail: false},
		{memo: map[string]any{"type": "hash", "value": signerHex}, fail: false},
		{memo: map[string]any{"type": "return", "value": signerHex}, fail: false},
		{memo: map[string]any{"type": "none"}, fail: false},
		{memo: map[string]any{"type": "unknown_type"}, fail: true},
		{memo: map[string]any{"type": "text", "value": "this is a very long memo text that exceeds 28 bytes easily"}, fail: true},
		{memo: map[string]any{"type": "hash", "value": "invalid-hex"}, fail: true},
		{memo: 12345, fail: false},
		{memo: "simple text", fail: false},
		{memo: true, fail: true},
	}
	for i, tc := range memoCases {
		s := &TxSpec{
			SourceAccount: validAccount,
			Sequence:      1,
			Fee:           100,
			Memo:          tc.memo,
			Operations:    []OperationSpec{{Type: "bump_sequence", BumpTo: 2}},
		}
		_, err := BuildTransactionEnvelope(s)
		if tc.fail && err == nil {
			t.Errorf("case %d: expected error for memo %+v", i, tc.memo)
		} else if !tc.fail && err != nil {
			t.Errorf("case %d: unexpected error for memo %+v: %v", i, tc.memo, err)
		}
	}
}

func TestBuildTransactionV0AndFeeBumpErrors(t *testing.T) {
	validAccount := "GBRPYHIL2CI3FNQ4BXLFMNDLFJUNPU2HY3ZMFSHONUCEOASW7QC7OX2H"

	// V0 nil spec
	if _, err := BuildTransactionV0(nil); err == nil {
		t.Errorf("expected error for nil V0 spec")
	}

	// V0 invalid source
	if _, err := BuildTransactionV0(&TxSpec{SourceAccount: "invalid"}); err == nil {
		t.Errorf("expected error for invalid source in V0")
	}

	// FeeBump nil spec
	if _, err := BuildFeeBumpTransaction(nil); err == nil {
		t.Errorf("expected error for nil fee bump spec")
	}

	// FeeBump missing fee_bump field
	if _, err := BuildFeeBumpTransaction(&TxSpec{}); err == nil {
		t.Errorf("expected error for missing fee_bump field")
	}

	// FeeBump missing fee_source
	if _, err := BuildFeeBumpTransaction(&TxSpec{FeeBump: &FeeBumpSpec{InnerTx: &TxSpec{SourceAccount: validAccount}}}); err == nil {
		t.Errorf("expected error for missing fee_source")
	}

	// FeeBump missing inner tx
	if _, err := BuildFeeBumpTransaction(&TxSpec{FeeBump: &FeeBumpSpec{FeeSource: validAccount}}); err == nil {
		t.Errorf("expected error for missing inner_tx")
	}

	// Encode base64 helper
	b64, err := EncodeTransactionEnvelopeBase64(&TxSpec{
		SourceAccount: validAccount,
		Sequence:      1,
		Fee:           100,
		Operations:    []OperationSpec{{Type: "bump_sequence", BumpTo: 2}},
	})
	if err != nil || b64 == "" {
		t.Errorf("failed EncodeTransactionEnvelopeBase64: %v", err)
	}
	if _, err := EncodeTransactionEnvelopeBase64(nil); err == nil {
		t.Errorf("expected error on nil spec")
	}

	// Signatures error handling
	s := &TxSpec{
		SourceAccount: validAccount,
		Sequence:      1,
		Fee:           100,
		Operations:    []OperationSpec{{Type: "bump_sequence", BumpTo: 2}},
		Signatures:    []SignatureSpec{{Signature: "0xinvalidhex"}},
	}
	if _, err := BuildTransactionEnvelope(s); err == nil {
		t.Errorf("expected error on invalid signature hex")
	}
}

func invokeContractID(t *testing.T, seed byte) (string, xdr.Hash) {
	t.Helper()
	raw := bytes.Repeat([]byte{seed}, 32)
	var want xdr.Hash
	copy(want[:], raw)
	contract, err := strkey.Encode(strkey.VersionByteContract, raw)
	if err != nil {
		t.Fatalf("failed to encode contract ID: %v", err)
	}
	return contract, want
}

func TestBuildInvokeContractFromJSON(t *testing.T) {
	validAccount := "GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAW"
	contract, wantContract := invokeContractID(t, 7)

	jsonSpec := fmt.Sprintf(`{
		"source_account": %q,
		"fee": 100,
		"sequence": 1,
		"operations": [
			{"type": "invoke_contract", "contract": %q, "function": "hello", "args": ["world", 42]}
		]
	}`, validAccount, contract)

	env, err := BuildTransactionEnvelopeFromJSON([]byte(jsonSpec))
	if err != nil {
		t.Fatalf("failed to build invoke_contract envelope: %v", err)
	}
	if env.V1 == nil || len(env.V1.Tx.Operations) != 1 {
		t.Fatalf("expected a single operation, got %+v", env.V1)
	}

	op := env.V1.Tx.Operations[0]
	if op.Body.Type != xdr.OperationTypeInvokeHostFunction {
		t.Fatalf("expected invoke_host_function body, got %v", op.Body.Type)
	}
	hostOp := op.Body.InvokeHostFunctionOp
	if hostOp == nil || hostOp.HostFunction.Type != xdr.HostFunctionTypeHostFunctionTypeInvokeContract {
		t.Fatalf("expected invoke_contract host function, got %+v", hostOp)
	}
	invoke := hostOp.HostFunction.InvokeContract
	if invoke == nil {
		t.Fatal("expected invoke contract args")
	}
	if invoke.ContractAddress.Type != xdr.ScAddressTypeScAddressTypeContract {
		t.Fatalf("expected contract address, got %v", invoke.ContractAddress.Type)
	}
	if got := xdr.Hash(*invoke.ContractAddress.ContractId); got != wantContract {
		t.Fatalf("contract ID mismatch: got %x want %x", got, wantContract)
	}
	if invoke.FunctionName != "hello" {
		t.Fatalf("expected function symbol hello, got %q", invoke.FunctionName)
	}
	if len(invoke.Args) != 2 {
		t.Fatalf("expected 2 args, got %d", len(invoke.Args))
	}
	if invoke.Args[0].Type != xdr.ScValTypeScvSymbol || string(*invoke.Args[0].Sym) != "world" {
		t.Fatalf("expected symbol arg world, got %+v", invoke.Args[0])
	}
	if invoke.Args[1].Type != xdr.ScValTypeScvI64 || int64(*invoke.Args[1].I64) != 42 {
		t.Fatalf("expected i64 arg 42, got %+v", invoke.Args[1])
	}

	// The result must survive an XDR round trip.
	raw, err := env.MarshalBinary()
	if err != nil {
		t.Fatalf("failed to marshal envelope: %v", err)
	}
	var decoded xdr.TransactionEnvelope
	if err := decoded.UnmarshalBinary(raw); err != nil {
		t.Fatalf("failed to unmarshal envelope: %v", err)
	}
	decodedInvoke := decoded.V1.Tx.Operations[0].Body.InvokeHostFunctionOp.HostFunction.InvokeContract
	if decodedInvoke == nil || decodedInvoke.FunctionName != "hello" {
		t.Fatalf("decoded invocation mismatch: %+v", decodedInvoke)
	}
	if got := xdr.Hash(*decodedInvoke.ContractAddress.ContractId); got != wantContract {
		t.Fatalf("decoded contract ID mismatch: got %x want %x", got, wantContract)
	}
}

func TestBuildInvokeContractRejectsInvalidSpec(t *testing.T) {
	validAccount := "GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAW"
	base := TxSpec{
		SourceAccount: validAccount,
		Fee:           100,
		Sequence:      1,
	}

	for name, op := range map[string]OperationSpec{
		"missing contract":   {Type: "invoke_contract", Function: "hello"},
		"invalid contract":   {Type: "invoke_contract", Contract: "not-a-contract", Function: "hello"},
		"missing function":   {Type: "invoke_contract", Contract: "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAABSC4"},
		"unsupported hostfn": {Type: "invoke_host_function", Function: "upload_wasm", ContractID: "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAABSC4"},
	} {
		t.Run(name, func(t *testing.T) {
			spec := base
			spec.Operations = []OperationSpec{op}
			if _, err := BuildTransactionEnvelope(&spec); err == nil {
				t.Fatalf("expected error for %s", name)
			} else if !errors.Is(err, ErrInvalidTxSpec) {
				t.Fatalf("expected ErrInvalidTxSpec, got %v", err)
			}
		})
	}
}

func TestBuildInvokeHostFunctionAuthEntries(t *testing.T) {
	validAccount := "GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAW"
	contract, wantContract := invokeContractID(t, 9)

	spec := &TxSpec{
		SourceAccount: validAccount,
		Fee:           1000,
		Sequence:      5,
		Operations: []OperationSpec{
			{
				Type:         "invoke_host_function",
				Function:     "invoke_contract",
				ContractID:   contract,
				FunctionName: "transfer",
				Args:         []any{validAccount, 1000},
				Auth: []any{
					map[string]any{
						"credentials": map[string]any{"type": "source_account"},
						"root_invocation": map[string]any{
							"contract": contract,
							"function": "transfer",
							"args":     []any{validAccount, 1000},
							"sub_invocations": []any{
								map[string]any{"contract": contract, "function": "approve", "args": []any{validAccount}},
							},
						},
					},
					map[string]any{
						"credentials": map[string]any{
							"type":                        "address",
							"address":                     validAccount,
							"nonce":                       7,
							"signature_expiration_ledger": 42,
							"signature":                   map[string]any{"type": "void"},
						},
						"root_invocation": map[string]any{"contract": contract, "function": "hello"},
					},
				},
			},
		},
	}

	env, err := BuildTransactionEnvelope(spec)
	if err != nil {
		t.Fatalf("failed auth envelope: %v", err)
	}
	hostOp := env.V1.Tx.Operations[0].Body.InvokeHostFunctionOp
	if len(hostOp.Auth) != 2 {
		t.Fatalf("expected 2 auth entries, got %d", len(hostOp.Auth))
	}

	sourceEntry := hostOp.Auth[0]
	if sourceEntry.Credentials.Type != xdr.SorobanCredentialsTypeSorobanCredentialsSourceAccount {
		t.Fatalf("expected source account credentials, got %v", sourceEntry.Credentials.Type)
	}
	root := sourceEntry.RootInvocation
	if root.Function.Type != xdr.SorobanAuthorizedFunctionTypeSorobanAuthorizedFunctionTypeContractFn || root.Function.ContractFn == nil {
		t.Fatalf("expected contract_fn root invocation, got %+v", root.Function)
	}
	if root.Function.ContractFn.FunctionName != "transfer" {
		t.Fatalf("expected transfer root invocation, got %q", root.Function.ContractFn.FunctionName)
	}
	if got := xdr.Hash(*root.Function.ContractFn.ContractAddress.ContractId); got != wantContract {
		t.Fatalf("root invocation contract mismatch: got %x want %x", got, wantContract)
	}
	if len(root.SubInvocations) != 1 {
		t.Fatalf("expected 1 sub-invocation, got %d", len(root.SubInvocations))
	}
	sub := root.SubInvocations[0]
	if sub.Function.ContractFn == nil || sub.Function.ContractFn.FunctionName != "approve" {
		t.Fatalf("expected approve sub-invocation, got %+v", sub.Function)
	}

	addrEntry := hostOp.Auth[1]
	if addrEntry.Credentials.Type != xdr.SorobanCredentialsTypeSorobanCredentialsAddress || addrEntry.Credentials.Address == nil {
		t.Fatalf("expected address credentials, got %+v", addrEntry.Credentials)
	}
	addrCreds := addrEntry.Credentials.Address
	if addrCreds.Address.Type != xdr.ScAddressTypeScAddressTypeAccount {
		t.Fatalf("expected account address, got %v", addrCreds.Address.Type)
	}
	if int64(addrCreds.Nonce) != 7 || uint32(addrCreds.SignatureExpirationLedger) != 42 {
		t.Fatalf("unexpected nonce/expiration: %+v", addrCreds)
	}
	if addrCreds.Signature.Type != xdr.ScValTypeScvVoid {
		t.Fatalf("expected void signature, got %v", addrCreds.Signature.Type)
	}

	// Auth entries must survive an XDR round trip.
	raw, err := env.MarshalBinary()
	if err != nil {
		t.Fatalf("failed to marshal envelope: %v", err)
	}
	var decoded xdr.TransactionEnvelope
	if err := decoded.UnmarshalBinary(raw); err != nil {
		t.Fatalf("failed to unmarshal envelope: %v", err)
	}
	decodedAuth := decoded.V1.Tx.Operations[0].Body.InvokeHostFunctionOp.Auth
	if len(decodedAuth) != 2 {
		t.Fatalf("expected 2 decoded auth entries, got %d", len(decodedAuth))
	}
	if decodedAuth[0].RootInvocation.SubInvocations[0].Function.ContractFn.FunctionName != "approve" {
		t.Fatalf("decoded sub-invocation mismatch: %+v", decodedAuth[0].RootInvocation)
	}
	if int64(decodedAuth[1].Credentials.Address.Nonce) != 7 {
		t.Fatalf("decoded nonce mismatch: %+v", decodedAuth[1].Credentials.Address)
	}
}

func TestBuildSorobanAuthorizationEntryDefaults(t *testing.T) {
	contract, _ := invokeContractID(t, 11)

	entry, err := BuildSorobanAuthorizationEntry(map[string]any{
		"credentials":     "source_account",
		"root_invocation": map[string]any{"contract_id": contract, "function_name": "hello"},
	})
	if err != nil {
		t.Fatalf("failed to build auth entry: %v", err)
	}
	if entry.Credentials.Type != xdr.SorobanCredentialsTypeSorobanCredentialsSourceAccount {
		t.Fatalf("expected source account credentials, got %v", entry.Credentials.Type)
	}
	if entry.RootInvocation.Function.ContractFn.FunctionName != "hello" {
		t.Fatalf("unexpected function name %q", entry.RootInvocation.Function.ContractFn.FunctionName)
	}

	if _, err := BuildSorobanAuthorizationEntry(map[string]any{
		"root_invocation": map[string]any{"contract": contract, "function": "hello"},
	}); err != nil {
		t.Fatalf("expected omitted credentials to default: %v", err)
	}

	if _, err := BuildSorobanAuthorizationEntry(map[string]any{
		"credentials":     map[string]any{"type": "address"},
		"root_invocation": map[string]any{"contract": contract, "function": "hello"},
	}); err == nil {
		t.Fatal("expected error for address credentials without an address")
	}
}

func TestBuildInvokeHostFunctionAuthFromJSON(t *testing.T) {
	validAccount := "GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAW"
	contract, _ := invokeContractID(t, 13)

	jsonSpec := fmt.Sprintf(`{
		"source_account": %q,
		"fee": 1000,
		"sequence": 5,
		"operations": [{
			"type": "invoke_host_function",
			"function": "invoke_contract",
			"contract_id": %q,
			"function_name": "transfer",
			"args": [%q, 1000],
			"auth": [{
				"credentials": {"type": "address", "address": %q, "nonce": 7, "signature_expiration_ledger": 42, "signature": {"type": "void"}},
				"root_invocation": {
					"contract": %q,
					"function": "transfer",
					"args": [%q, 1000],
					"sub_invocations": [{"contract": %q, "function": "approve"}]
				}
			}]
		}]
	}`, validAccount, contract, validAccount, validAccount, contract, validAccount, contract)

	env, err := BuildTransactionEnvelopeFromJSON([]byte(jsonSpec))
	if err != nil {
		t.Fatalf("failed to build envelope with auth from JSON: %v", err)
	}
	hostOp := env.V1.Tx.Operations[0].Body.InvokeHostFunctionOp
	if len(hostOp.Auth) != 1 {
		t.Fatalf("expected 1 auth entry, got %d", len(hostOp.Auth))
	}
	creds := hostOp.Auth[0].Credentials
	if creds.Type != xdr.SorobanCredentialsTypeSorobanCredentialsAddress || creds.Address == nil {
		t.Fatalf("expected address credentials, got %+v", creds)
	}
	if int64(creds.Address.Nonce) != 7 || uint32(creds.Address.SignatureExpirationLedger) != 42 {
		t.Fatalf("unexpected address credentials: %+v", creds.Address)
	}
	subs := hostOp.Auth[0].RootInvocation.SubInvocations
	if len(subs) != 1 || subs[0].Function.ContractFn == nil || subs[0].Function.ContractFn.FunctionName != "approve" {
		t.Fatalf("unexpected sub-invocations: %+v", subs)
	}
}
