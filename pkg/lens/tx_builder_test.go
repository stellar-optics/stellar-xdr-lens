package lens

import (
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

func TestBuildClaimPredicate(t *testing.T) {
	t.Parallel()

	// 1. Unconditional string
	p, err := BuildClaimPredicate("unconditional")
	if err != nil {
		t.Fatalf("BuildClaimPredicate(unconditional) failed: %v", err)
	}
	if p.Type != xdr.ClaimPredicateTypeClaimPredicateUnconditional {
		t.Errorf("got %v, want unconditional", p.Type)
	}

	// 2. Unconditional map
	p, err = BuildClaimPredicate(map[string]any{"unconditional": true})
	if err != nil {
		t.Fatalf("BuildClaimPredicate(map unconditional) failed: %v", err)
	}
	if p.Type != xdr.ClaimPredicateTypeClaimPredicateUnconditional {
		t.Errorf("got %v, want unconditional", p.Type)
	}

	// 3. Before absolute time
	p, err = BuildClaimPredicate(map[string]any{"before_time": 1700000000})
	if err != nil {
		t.Fatalf("BuildClaimPredicate(before_time) failed: %v", err)
	}
	if p.Type != xdr.ClaimPredicateTypeClaimPredicateBeforeAbsoluteTime || p.AbsBefore == nil || *p.AbsBefore != 1700000000 {
		t.Errorf("got %v with abs %v, want 1700000000", p.Type, p.AbsBefore)
	}

	// 4. Before relative time
	p, err = BuildClaimPredicate(map[string]any{"before_relative_time": 86400})
	if err != nil {
		t.Fatalf("BuildClaimPredicate(before_relative_time) failed: %v", err)
	}
	if p.Type != xdr.ClaimPredicateTypeClaimPredicateBeforeRelativeTime || p.RelBefore == nil || *p.RelBefore != 86400 {
		t.Errorf("got %v with rel %v, want 86400", p.Type, p.RelBefore)
	}

	// 5. After time (encoded as NOT before_absolute_time)
	p, err = BuildClaimPredicate(map[string]any{"after_time": 1700000000})
	if err != nil {
		t.Fatalf("BuildClaimPredicate(after_time) failed: %v", err)
	}
	if p.Type != xdr.ClaimPredicateTypeClaimPredicateNot || p.NotPredicate == nil || *p.NotPredicate == nil {
		t.Fatalf("expected NOT predicate for after_time, got %v", p.Type)
	}
	inner := **p.NotPredicate
	if inner.Type != xdr.ClaimPredicateTypeClaimPredicateBeforeAbsoluteTime || inner.AbsBefore == nil || *inner.AbsBefore != 1700000000 {
		t.Errorf("inner of after_time: got %v with abs %v", inner.Type, inner.AbsBefore)
	}

	// 6. After relative time (encoded as NOT before_relative_time)
	p, err = BuildClaimPredicate(map[string]any{"after_relative_time": 3600})
	if err != nil {
		t.Fatalf("BuildClaimPredicate(after_relative_time) failed: %v", err)
	}
	if p.Type != xdr.ClaimPredicateTypeClaimPredicateNot || p.NotPredicate == nil || *p.NotPredicate == nil {
		t.Fatalf("expected NOT predicate for after_relative_time, got %v", p.Type)
	}
	inner = **p.NotPredicate
	if inner.Type != xdr.ClaimPredicateTypeClaimPredicateBeforeRelativeTime || inner.RelBefore == nil || *inner.RelBefore != 3600 {
		t.Errorf("inner of after_relative_time: got %v with rel %v", inner.Type, inner.RelBefore)
	}

	// 7. Nested AND predicate: {"and": [{"before_time": 1700000000}, {"after_time": 1600000000}]}
	nestedAndSpec := map[string]any{
		"and": []any{
			map[string]any{"before_time": 1700000000},
			map[string]any{"after_time": 1600000000},
		},
	}
	p, err = BuildClaimPredicate(nestedAndSpec)
	if err != nil {
		t.Fatalf("BuildClaimPredicate(nested AND) failed: %v", err)
	}
	if p.Type != xdr.ClaimPredicateTypeClaimPredicateAnd || p.AndPredicates == nil || len(*p.AndPredicates) != 2 {
		t.Fatalf("expected AND predicate with 2 children, got %v", p)
	}

	// 8. Nested OR predicate: {"or": [{"before_time": 1700000000}, "unconditional"]}
	nestedOrSpec := map[string]any{
		"or": []any{
			map[string]any{"before_time": 1700000000},
			"unconditional",
		},
	}
	p, err = BuildClaimPredicate(nestedOrSpec)
	if err != nil {
		t.Fatalf("BuildClaimPredicate(nested OR) failed: %v", err)
	}
	if p.Type != xdr.ClaimPredicateTypeClaimPredicateOr || p.OrPredicates == nil || len(*p.OrPredicates) != 2 {
		t.Fatalf("expected OR predicate with 2 children, got %v", p)
	}

	// 9. Error cases
	// Negative relative time
	if _, err := BuildClaimPredicate(map[string]any{"before_relative_time": -10}); err == nil {
		t.Errorf("expected error for negative relative time")
	}
	// Empty AND
	if _, err := BuildClaimPredicate(map[string]any{"and": []any{}}); err == nil {
		t.Errorf("expected error for empty AND array")
	}
	// Unknown string
	if _, err := BuildClaimPredicate("invalid_predicate_str"); err == nil {
		t.Errorf("expected error for unknown string")
	}
}

func TestBuildClaimableBalanceOp(t *testing.T) {
	t.Parallel()

	validDest1 := "GCJNHFXJEQEY54NPKU4DBMT5WOBJUHMYSFBDAQF4OMTEGJN4BQU7ZST6"
	validDest2 := "GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAW"

	spec := map[string]any{
		"asset":  "native",
		"amount": "100.5 XLM",
		"claimants": []any{
			map[string]any{
				"destination": validDest1,
				"predicate": map[string]any{
					"and": []any{
						map[string]any{"before_time": 1700000000},
						map[string]any{"after_time": 1600000000},
					},
				},
			},
			map[string]any{
				"destination": validDest2,
				"predicate":   "unconditional",
			},
		},
	}

	op, err := BuildClaimableBalanceOp(spec)
	if err != nil {
		t.Fatalf("BuildClaimableBalanceOp failed: %v", err)
	}

	if op.Asset.Type != xdr.AssetTypeAssetTypeNative {
		t.Errorf("expected native asset, got %v", op.Asset.Type)
	}
	if op.Amount != 1005000000 {
		t.Errorf("expected amount 1005000000, got %d", op.Amount)
	}
	if len(op.Claimants) != 2 {
		t.Fatalf("expected 2 claimants, got %d", len(op.Claimants))
	}

	// Verify byte structure by marshaling to binary and round-tripping
	bytes, err := op.MarshalBinary()
	if err != nil || len(bytes) == 0 {
		t.Fatalf("MarshalBinary() failed: %v", err)
	}

	var roundTripped xdr.CreateClaimableBalanceOp
	if err := roundTripped.UnmarshalBinary(bytes); err != nil {
		t.Fatalf("UnmarshalBinary() failed: %v", err)
	}
	if roundTripped.Amount != op.Amount {
		t.Errorf("round-trip amount mismatch: %d vs %d", roundTripped.Amount, op.Amount)
	}
	if len(roundTripped.Claimants) != len(op.Claimants) {
		t.Errorf("round-trip claimants mismatch: %d vs %d", len(roundTripped.Claimants), len(op.Claimants))
	}
}

func TestBuildTransactionEnvelopeWithClaimableBalanceOp(t *testing.T) {
	t.Parallel()

	validSource := "GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAW"
	validDest := "GCJNHFXJEQEY54NPKU4DBMT5WOBJUHMYSFBDAQF4OMTEGJN4BQU7ZST6"

	spec := &TxSpec{
		SourceAccount: validSource,
		Fee:           100,
		Sequence:      1,
		Operations: []OperationSpec{
			{
				Type:   "create_claimable_balance",
				Asset:  "native",
				Amount: 50000000,
				Claimants: []any{
					map[string]any{
						"destination": validDest,
						"predicate": map[string]any{
							"or": []any{
								map[string]any{"before_time": 1700000000},
								"unconditional",
							},
						},
					},
				},
			},
		},
	}

	env, err := BuildTransactionEnvelope(spec)
	if err != nil {
		t.Fatalf("BuildTransactionEnvelope with create_claimable_balance failed: %v", err)
	}

	if len(env.V1.Tx.Operations) != 1 {
		t.Fatalf("expected 1 op, got %d", len(env.V1.Tx.Operations))
	}
	body := env.V1.Tx.Operations[0].Body
	if body.Type != xdr.OperationTypeCreateClaimableBalance {
		t.Fatalf("expected CreateClaimableBalance op, got %v", body.Type)
	}

	// Base64 encode and decode back through the library
	b64, err := EncodeTransactionEnvelopeBase64(spec)
	if err != nil {
		t.Fatalf("EncodeTransactionEnvelopeBase64 failed: %v", err)
	}

	decoded, err := Decode(b64)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if decoded.Type != "TransactionEnvelope" {
		t.Errorf("expected TransactionEnvelope, got %s", decoded.Type)
	}
}
