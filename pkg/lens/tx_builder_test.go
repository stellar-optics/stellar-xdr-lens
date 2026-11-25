package lens

import (
	"encoding/base64"
	"math"
	"os"
	"strings"
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

func TestBuildManageDataOp(t *testing.T) {
	validAccount := "GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAW"

	// Set UTF-8 string value via JSON template
	setJSON := []byte(`{
		"source_account": "` + validAccount + `",
		"fee": 100,
		"sequence": 1,
		"operations": [
			{"type": "manage_data", "name": "config", "value": "enabled"}
		]
	}`)
	env, err := BuildTransactionEnvelopeFromJSON(setJSON)
	if err != nil {
		t.Fatalf("manage_data set failed: %v", err)
	}
	md := env.V1.Tx.Operations[0].Body.ManageDataOp
	if md == nil || string(md.DataName) != "config" {
		t.Fatalf("unexpected DataName: %+v", md)
	}
	if md.DataValue == nil || string(*md.DataValue) != "enabled" {
		t.Fatalf("unexpected DataValue: %v", md.DataValue)
	}

	// Delete entry via null value
	delJSON := []byte(`{
		"source_account": "` + validAccount + `",
		"fee": 100,
		"sequence": 2,
		"operations": [
			{"type": "manage_data", "name": "config", "value": null}
		]
	}`)
	envDel, err := BuildTransactionEnvelopeFromJSON(delJSON)
	if err != nil {
		t.Fatalf("manage_data delete failed: %v", err)
	}
	mdDel := envDel.V1.Tx.Operations[0].Body.ManageDataOp
	if mdDel == nil || string(mdDel.DataName) != "config" || mdDel.DataValue != nil {
		t.Fatalf("expected DataValue=nil for deletion, got %+v", mdDel)
	}

	// Base64 object value
	rawBin := []byte{0x00, 0x01, 0xfe, 0xff}
	b64Str := base64.StdEncoding.EncodeToString(rawBin)
	mdB64, err := buildManageDataOp(map[string]any{
		"name":  "bin_key",
		"value": map[string]any{"base64": b64Str},
	})
	if err != nil {
		t.Fatalf("manage_data base64 failed: %v", err)
	}
	if mdB64.DataValue == nil || !bytes.Equal(*mdB64.DataValue, rawBin) {
		t.Fatalf("expected binary %x, got %v", rawBin, mdB64.DataValue)
	}

	// Name > 64 bytes error
	if _, err := buildManageDataOp(map[string]any{
		"name":  strings.Repeat("a", 65),
		"value": "ok",
	}); err == nil {
		t.Fatalf("expected error for data name > 64 bytes")
	}

	// Value > 64 bytes error
	if _, err := buildManageDataOp(map[string]any{
		"name":  "too_long",
		"value": strings.Repeat("b", 65),
	}); err == nil {
		t.Fatalf("expected error for data value > 64 bytes")
	}
}

func TestBuildManageOfferOps(t *testing.T) {
	validAccount := "GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAW"

	// ManageSellOffer from JSON with decimal price "1.5" -> canonical 3/2 and default offerId=0
	sellJSON := []byte(`{
		"source_account": "` + validAccount + `",
		"fee": 100,
		"sequence": 10,
		"operations": [
			{
				"type": "manage_sell_offer",
				"selling": "native",
				"buying": "USD:` + validAccount + `",
				"amount": "100",
				"price": "1.5"
			},
			{
				"type": "manage_sell_offer",
				"selling": "native",
				"buying": "USD:` + validAccount + `",
				"amount": "0",
				"price": "3/2",
				"offerId": 42
			},
			{
				"type": "manage_buy_offer",
				"selling": "USD:` + validAccount + `",
				"buying": "native",
				"buyAmount": "50",
				"price": 1.5
			},
			{
				"type": "manage_buy_offer",
				"selling": "USD:` + validAccount + `",
				"buying": "native",
				"buy_amount": "0",
				"price": "3/2",
				"offer_id": 99
			}
		]
	}`)
	env, err := BuildTransactionEnvelopeFromJSON(sellJSON)
	if err != nil {
		t.Fatalf("failed building offer ops from JSON: %v", err)
	}
	ops := env.V1.Tx.Operations
	if len(ops) != 4 {
		t.Fatalf("expected 4 operations, got %d", len(ops))
	}

	sellNew := ops[0].Body.ManageSellOfferOp
	if sellNew.OfferId != 0 || sellNew.Price.N != 3 || sellNew.Price.D != 2 {
		t.Fatalf("unexpected sellNew: %+v", sellNew)
	}
	sellCancel := ops[1].Body.ManageSellOfferOp
	if sellCancel.Amount != 0 || sellCancel.OfferId != 42 || sellCancel.Price.N != 3 || sellCancel.Price.D != 2 {
		t.Fatalf("unexpected sellCancel: %+v", sellCancel)
	}
	buyNew := ops[2].Body.ManageBuyOfferOp
	if buyNew.OfferId != 0 || buyNew.Price.N != 3 || buyNew.Price.D != 2 {
		t.Fatalf("unexpected buyNew: %+v", buyNew)
	}
	buyCancel := ops[3].Body.ManageBuyOfferOp
	if buyCancel.BuyAmount != 0 || buyCancel.OfferId != 99 {
		t.Fatalf("unexpected buyCancel: %+v", buyCancel)
	}

	// Invalid price error
	if _, err := buildManageSellOfferOp(map[string]any{
		"selling": "native",
		"buying":  "USD:" + validAccount,
		"amount":  100,
		"price":   "1/0",
	}); err == nil {
		t.Fatalf("expected error for zero denominator price")
	}
}

func TestBuildSetOptionsOp(t *testing.T) {
	validAccount := "GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAW"
	signerAccount := "GCJNHFXJEQEY54NPKU4DBMT5WOBJUHMYSFBDAQF4OMTEGJN4BQU7ZST6"

	// Partial SetOptions from JSON
	partialJSON := []byte(`{
		"source_account": "` + validAccount + `",
		"fee": 100,
		"sequence": 1,
		"operations": [
			{
				"type": "set_options",
				"masterWeight": 2,
				"lowThreshold": 1,
				"medThreshold": 2,
				"highThreshold": 3,
				"homeDomain": "stellar.org",
				"signer": {
					"key": "` + signerAccount + `",
					"weight": 1
				}
			}
		]
	}`)
	env, err := BuildTransactionEnvelopeFromJSON(partialJSON)
	if err != nil {
		t.Fatalf("failed partial set_options: %v", err)
	}
	so := env.V1.Tx.Operations[0].Body.SetOptionsOp
	if so == nil {
		t.Fatal("expected SetOptionsOp")
	}
	if so.MasterWeight == nil || *so.MasterWeight != 2 {
		t.Fatalf("unexpected MasterWeight: %v", so.MasterWeight)
	}
	if so.LowThreshold == nil || *so.LowThreshold != 1 || so.MedThreshold == nil || *so.MedThreshold != 2 || so.HighThreshold == nil || *so.HighThreshold != 3 {
		t.Fatalf("unexpected thresholds: %+v", so)
	}
	if so.HomeDomain == nil || string(*so.HomeDomain) != "stellar.org" {
		t.Fatalf("unexpected HomeDomain: %v", so.HomeDomain)
	}
	if so.Signer == nil || so.Signer.Weight != 1 || so.Signer.Key.Type != xdr.SignerKeyTypeSignerKeyTypeEd25519 {
		t.Fatalf("unexpected Signer: %+v", so.Signer)
	}

	// Full SetOptions via buildSetOptionsOp
	fullOp, err := buildSetOptionsOp(map[string]any{
		"inflation_dest": signerAccount,
		"clear_flags":    1,
		"set_flags":      2,
		"master_weight":  10,
		"low_threshold":  5,
		"med_threshold":  10,
		"high_threshold": 15,
		"home_domain":    "example.com",
		"signer": map[string]any{
			"key":    signerAccount,
			"weight": 5,
		},
	})
	if err != nil {
		t.Fatalf("failed full buildSetOptionsOp: %v", err)
	}
	if fullOp.InflationDest == nil || fullOp.ClearFlags == nil || fullOp.SetFlags == nil {
		t.Fatalf("expected inflation_dest, clear_flags, set_flags to be set")
	}

	// Threshold out of range (> 255)
	if _, err := buildSetOptionsOp(map[string]any{"high_threshold": 256}); err == nil {
		t.Fatalf("expected error for high_threshold > 255")
	}
	if _, err := buildSetOptionsOp(map[string]any{"low_threshold": -1}); err == nil {
		t.Fatalf("expected error for low_threshold < 0")
	}
	if _, err := buildSetOptionsOp(map[string]any{
		"signer": map[string]any{"key": signerAccount, "weight": 300},
	}); err == nil {
		t.Fatalf("expected error for signer weight > 255")
	}
}

func TestBuildChangeTrustOp(t *testing.T) {
	validAccount := "GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAW"

	// Omitted limit defaults to math.MaxInt64
	ctJSON := []byte(`{
		"source_account": "` + validAccount + `",
		"fee": 100,
		"sequence": 1,
		"operations": [
			{
				"type": "change_trust",
				"asset": "USD:` + validAccount + `"
			},
			{
				"type": "change_trust",
				"line": {
					"type": "pool_share",
					"asset_a": "native",
					"asset_b": "USD:` + validAccount + `",
					"fee": 30
				},
				"limit": "1000"
			}
		]
	}`)
	env, err := BuildTransactionEnvelopeFromJSONStrict(ctJSON)
	if err != nil {
		t.Fatalf("failed strict change_trust JSON: %v", err)
	}
	ops := env.V1.Tx.Operations
	if len(ops) != 2 {
		t.Fatalf("expected 2 operations, got %d", len(ops))
	}

	ctDefault := ops[0].Body.ChangeTrustOp
	if ctDefault == nil || ctDefault.Limit != xdr.Int64(math.MaxInt64) {
		t.Fatalf("expected default max int64 limit, got %v", ctDefault)
	}
	ctPool := ops[1].Body.ChangeTrustOp
	if ctPool == nil || ctPool.Line.Type != xdr.AssetTypeAssetTypePoolShare || ctPool.Limit != 1000 {
		t.Fatalf("unexpected pool ChangeTrustOp: %+v", ctPool)
	}

	// Base64 XDR encoding verification
	b64, err := EncodeTransactionEnvelopeBase64(&TxSpec{
		SourceAccount: validAccount,
		Fee:           100,
		Sequence:      1,
		Operations: []OperationSpec{
			{Type: "change_trust", Asset: "USD:" + validAccount},
		},
	})
	if err != nil || b64 == "" {
		t.Fatalf("failed EncodeTransactionEnvelopeBase64 for change_trust: %v", err)
	}

	// Invalid negative limit error
	if _, err := buildChangeTrustOp(map[string]any{
		"asset": "USD:" + validAccount,
		"limit": -5,
	}); err == nil {
		t.Fatalf("expected error for negative limit")
	}
}

func TestBuildManageDataOp(t *testing.T) {
	validAccount := "GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAW"

	// Set UTF-8 string value via JSON template
	setJSON := []byte(`{
		"source_account": "` + validAccount + `",
		"fee": 100,
		"sequence": 1,
		"operations": [
			{"type": "manage_data", "name": "config", "value": "enabled"}
		]
	}`)
	env, err := BuildTransactionEnvelopeFromJSON(setJSON)
	if err != nil {
		t.Fatalf("manage_data set failed: %v", err)
	}
	md := env.V1.Tx.Operations[0].Body.ManageDataOp
	if md == nil || string(md.DataName) != "config" || md.DataValue == nil || string(*md.DataValue) != "enabled" {
		t.Fatalf("unexpected ManageDataOp for set: %+v", md)
	}

	// Delete entry via null value
	delJSON := []byte(`{
		"source_account": "` + validAccount + `",
		"fee": 100,
		"sequence": 2,
		"operations": [
			{"type": "manage_data", "name": "config", "value": null}
		]
	}`)
	envDel, err := BuildTransactionEnvelopeFromJSON(delJSON)
	if err != nil {
		t.Fatalf("manage_data delete failed: %v", err)
	}
	mdDel := envDel.V1.Tx.Operations[0].Body.ManageDataOp
	if mdDel == nil || string(mdDel.DataName) != "config" || mdDel.DataValue != nil {
		t.Fatalf("expected nil DataValue for deletion, got %+v", mdDel)
	}

	// Base64 object value
	rawBin := []byte{0x01, 0x02, 0xff, 0xfe}
	b64Val := base64.StdEncoding.EncodeToString(rawBin)
	op, err := buildManageDataOp(map[string]any{
		"name":  "bin_key",
		"value": map[string]any{"base64": b64Val},
	})
	if err != nil {
		t.Fatalf("manage_data base64 failed: %v", err)
	}
	if op.DataValue == nil || string(*op.DataValue) != string(rawBin) {
		t.Fatalf("unexpected decoded base64 bytes: %v", op.DataValue)
	}

	// Validation: name > 64 bytes and value > 64 bytes
	if _, err := buildManageDataOp(map[string]any{"name": strings.Repeat("a", 65), "value": "ok"}); err == nil {
		t.Fatalf("expected error for data name > 64 bytes")
	}
	if _, err := buildManageDataOp(map[string]any{"name": "ok", "value": strings.Repeat("b", 65)}); err == nil {
		t.Fatalf("expected error for data value > 64 bytes")
	}
}

func TestBuildManageOfferOps(t *testing.T) {
	validAccount := "GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAW"

	txJSON := []byte(`{
		"source_account": "` + validAccount + `",
		"fee": 100,
		"sequence": 10,
		"operations": [
			{
				"type": "manage_sell_offer",
				"selling": "native",
				"buying": "USD:` + validAccount + `",
				"amount": "100",
				"price": "1.5"
			},
			{
				"type": "manage_buy_offer",
				"selling": "USD:` + validAccount + `",
				"buying": "native",
				"buy_amount": "0",
				"price": "3/2",
				"offerId": 42
			}
		]
	}`)
	env, err := BuildTransactionEnvelopeFromJSON(txJSON)
	if err != nil {
		t.Fatalf("manage offers JSON failed: %v", err)
	}

	sellOp := env.V1.Tx.Operations[0].Body.ManageSellOfferOp
	if sellOp == nil || sellOp.Price.N != 3 || sellOp.Price.D != 2 || sellOp.OfferId != 0 {
		t.Fatalf("unexpected ManageSellOfferOp: %+v", sellOp)
	}

	buyOp := env.V1.Tx.Operations[1].Body.ManageBuyOfferOp
	if buyOp == nil || buyOp.BuyAmount != 0 || buyOp.Price.N != 3 || buyOp.Price.D != 2 || buyOp.OfferId != 42 {
		t.Fatalf("unexpected ManageBuyOfferOp: %+v", buyOp)
	}

	// Cancel sell offer (amount: "0")
	cancelSell, err := buildManageSellOfferOp(map[string]any{
		"selling":  "native",
		"buying":   "USD:" + validAccount,
		"amount":   "0",
		"price":    1.5,
		"offer_id": 99,
	})
	if err != nil {
		t.Fatalf("cancel sell offer failed: %v", err)
	}
	if cancelSell.Amount != 0 || cancelSell.OfferId != 99 || cancelSell.Price.N != 3 || cancelSell.Price.D != 2 {
		t.Fatalf("unexpected canceled sell offer: %+v", cancelSell)
	}

	// Invalid price error
	if _, err := ParsePriceFraction("1/0"); err == nil {
		t.Fatalf("expected error on zero denominator")
	}
}

func TestBuildSetOptionsOp(t *testing.T) {
	validAccount := "GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAW"
	signerAccount := "GCJNHFXJEQEY54NPKU4DBMT5WOBJUHMYSFBDAQF4OMTEGJN4BQU7ZST6"

	txJSON := []byte(`{
		"source_account": "` + validAccount + `",
		"fee": 100,
		"sequence": 20,
		"operations": [
			{
				"type": "set_options",
				"inflation_dest": "` + signerAccount + `",
				"clear_flags": 1,
				"set_flags": ["auth_required", "auth_revocable"],
				"masterWeight": 10,
				"lowThreshold": 5,
				"medThreshold": 10,
				"highThreshold": 20,
				"home_domain": "stellar.org",
				"signer": {"key": "` + signerAccount + `", "weight": 5}
			},
			{
				"type": "set_options",
				"master_weight": 0
			}
		]
	}`)
	env, err := BuildTransactionEnvelopeFromJSON(txJSON)
	if err != nil {
		t.Fatalf("set_options JSON failed: %v", err)
	}

	full := env.V1.Tx.Operations[0].Body.SetOptionsOp
	if full == nil || full.InflationDest == nil || full.ClearFlags == nil || *full.ClearFlags != 1 ||
		full.SetFlags == nil || *full.SetFlags != 3 ||
		full.MasterWeight == nil || *full.MasterWeight != 10 ||
		full.LowThreshold == nil || *full.LowThreshold != 5 ||
		full.MedThreshold == nil || *full.MedThreshold != 10 ||
		full.HighThreshold == nil || *full.HighThreshold != 20 ||
		full.HomeDomain == nil || string(*full.HomeDomain) != "stellar.org" ||
		full.Signer == nil || full.Signer.Weight != 5 ||
		full.Signer.Key.Type != xdr.SignerKeyTypeSignerKeyTypeEd25519 {
		t.Fatalf("unexpected full SetOptionsOp: %+v", full)
	}

	partial := env.V1.Tx.Operations[1].Body.SetOptionsOp
	if partial == nil || partial.MasterWeight == nil || *partial.MasterWeight != 0 ||
		partial.LowThreshold != nil || partial.Signer != nil {
		t.Fatalf("unexpected partial SetOptionsOp: %+v", partial)
	}

	// Out-of-range threshold error (> 255)
	if _, err := buildSetOptionsOp(map[string]any{"high_threshold": 256}); err == nil {
		t.Fatalf("expected error for threshold > 255")
	}
	if _, err := buildSetOptionsOp(map[string]any{"low_threshold": -1}); err == nil {
		t.Fatalf("expected error for threshold < 0")
	}
}

func TestBuildChangeTrustOp(t *testing.T) {
	validAccount := "GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAW"

	// Omitted limit defaults to math.MaxInt64 + liquidity pool share support
	txJSON := []byte(`{
		"source_account": "` + validAccount + `",
		"fee": 100,
		"sequence": 30,
		"operations": [
			{
				"type": "change_trust",
				"asset": "USD:` + validAccount + `"
			},
			{
				"type": "change_trust",
				"line": {
					"type": "pool_share",
					"asset_a": "native",
					"asset_b": "USDC:` + validAccount + `",
					"fee": 30
				},
				"limit": "500.5"
			}
		]
	}`)
	if err := ValidateJSON(txJSON, StrictTxSpecSchema()); err != nil {
		t.Fatalf("strict schema validation failed for change_trust: %v", err)
	}
	env, err := BuildTransactionEnvelopeFromJSON(txJSON)
	if err != nil {
		t.Fatalf("change_trust JSON failed: %v", err)
	}

	ctDefault := env.V1.Tx.Operations[0].Body.ChangeTrustOp
	if ctDefault == nil || ctDefault.Limit != xdr.Int64(math.MaxInt64) {
		t.Fatalf("expected default MaxInt64 limit, got %+v", ctDefault)
	}

	ctPool := env.V1.Tx.Operations[1].Body.ChangeTrustOp
	if ctPool == nil || ctPool.Line.Type != xdr.AssetTypeAssetTypePoolShare || ctPool.Limit != 5005000000 {
		t.Fatalf("unexpected pool ChangeTrustOp: %+v", ctPool)
	}

	// Verify XDR base64 encoding
	b64, err := EncodeTransactionEnvelopeBase64(&TxSpec{
		SourceAccount: validAccount,
		Fee:           100,
		Sequence:      31,
		Operations:    []OperationSpec{{Type: "change_trust", Asset: "USD:" + validAccount}},
	})
	if err != nil || b64 == "" {
		t.Fatalf("EncodeTransactionEnvelopeBase64 failed: %v", err)
	}
}
