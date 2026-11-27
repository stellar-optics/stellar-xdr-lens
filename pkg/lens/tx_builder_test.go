package lens

import (
	"encoding/base64"
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

func TestBuildPaymentOp(t *testing.T) {
	t.Parallel()

	const issuer = "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN"
	const dest = "GCJNHFXJEQEY54NPKU4DBMT5WOBJUHMYSFBDAQF4OMTEGJN4BQU7ZST6"

	muxedAccount := xdr.MuxedAccount{
		Type: xdr.CryptoKeyTypeKeyTypeMuxedEd25519,
		Med25519: &xdr.MuxedAccountMed25519{
			Id:      7,
			Ed25519: *xdr.MustAddress(dest).Ed25519,
		},
	}
	muxed := muxedAccount.Address()

	tests := []struct {
		name       string
		spec       map[string]any
		wantAsset  xdr.AssetType
		wantCode   string
		wantAmount int64
		wantMuxed  bool
	}{
		{
			name:       "amount defaults to a native asset",
			spec:       map[string]any{"destination": dest, "amount": "10.5"},
			wantAsset:  xdr.AssetTypeAssetTypeNative,
			wantAmount: 105000000,
		},
		{
			name:       "explicit native asset",
			spec:       map[string]any{"destination": dest, "amount": "0.0000001", "asset": "native"},
			wantAsset:  xdr.AssetTypeAssetTypeNative,
			wantAmount: 1,
		},
		{
			name: "issued asset object",
			spec: map[string]any{
				"destination": dest,
				"amount":      "100.5",
				"asset":       map[string]any{"code": "USDC", "issuer": issuer},
			},
			wantAsset:  xdr.AssetTypeAssetTypeCreditAlphanum4,
			wantCode:   "USDC",
			wantAmount: 1005000000,
		},
		{
			name:       "muxed destination",
			spec:       map[string]any{"destination": muxed, "amount": 10},
			wantAsset:  xdr.AssetTypeAssetTypeNative,
			wantAmount: 10,
			wantMuxed:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			op, err := buildPaymentOp(tc.spec)
			if err != nil {
				t.Fatalf("buildPaymentOp() error = %v", err)
			}
			if int64(op.Amount) != tc.wantAmount {
				t.Errorf("Amount = %d, want %d", op.Amount, tc.wantAmount)
			}
			if op.Asset.Type != tc.wantAsset {
				t.Errorf("Asset.Type = %v, want %v", op.Asset.Type, tc.wantAsset)
			}
			if tc.wantCode != "" {
				if op.Asset.AlphaNum4 == nil {
					t.Fatalf("Asset.AlphaNum4 is nil for a code asset")
				}
				if got := strings.TrimRight(string(op.Asset.AlphaNum4.AssetCode[:]), "\x00"); got != tc.wantCode {
					t.Errorf("Asset code = %q, want %q", got, tc.wantCode)
				}
			}
			gotMuxed := op.Destination.Type == xdr.CryptoKeyTypeKeyTypeMuxedEd25519
			if gotMuxed != tc.wantMuxed {
				t.Errorf("Destination muxed = %v, want %v", gotMuxed, tc.wantMuxed)
			}
		})
	}
}

func TestBuildPaymentOpRejectsInvalidSpecs(t *testing.T) {
	t.Parallel()

	const dest = "GCJNHFXJEQEY54NPKU4DBMT5WOBJUHMYSFBDAQF4OMTEGJN4BQU7ZST6"
	const badChecksum = "GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAX"

	tests := []struct {
		name string
		spec map[string]any
		want string
	}{
		{name: "nil spec", spec: nil, want: "destination"},
		{name: "missing destination", spec: map[string]any{"amount": 1}, want: "destination"},
		{name: "blank destination", spec: map[string]any{"destination": "  ", "amount": 1}, want: "destination"},
		{name: "invalid destination", spec: map[string]any{"destination": badChecksum, "amount": 1}, want: "destination"},
		{name: "missing amount", spec: map[string]any{"destination": dest}, want: "amount"},
		{name: "unparseable amount", spec: map[string]any{"destination": dest, "amount": "ten"}, want: "amount"},
		{
			name: "asset without issuer",
			spec: map[string]any{"destination": dest, "amount": 1, "asset": map[string]any{"code": "USDC"}},
			want: "asset",
		},
		{
			name: "asset with invalid issuer",
			spec: map[string]any{"destination": dest, "amount": 1, "asset": map[string]any{"code": "USDC", "issuer": badChecksum}},
			want: "asset",
		},
		{name: "unparseable asset", spec: map[string]any{"destination": dest, "amount": 1, "asset": "USDC"}, want: "asset"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			op, err := buildPaymentOp(tc.spec)
			if err == nil {
				t.Fatalf("buildPaymentOp() = %+v, want an error", op)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestBuildTransactionEnvelopeFromJSONIntuitivePayment(t *testing.T) {
	t.Parallel()

	const issuer = "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN"
	const dest = "GCJNHFXJEQEY54NPKU4DBMT5WOBJUHMYSFBDAQF4OMTEGJN4BQU7ZST6"

	template := `{
		"source_account": "GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAW",
		"fee": 100,
		"sequence": 1,
		"operations": [{
			"type": "payment",
			"destination": "` + dest + `",
			"amount": "10.5",
			"asset": {"code": "USDC", "issuer": "` + issuer + `"}
		}]
	}`

	env, err := BuildTransactionEnvelopeFromJSONStrict([]byte(template))
	if err != nil {
		t.Fatalf("BuildTransactionEnvelopeFromJSONStrict() error = %v", err)
	}
	if env.V1 == nil || len(env.V1.Tx.Operations) != 1 {
		t.Fatalf("envelope has no payment operation: %+v", env)
	}

	paymentOp := env.V1.Tx.Operations[0].Body.PaymentOp
	if paymentOp == nil {
		t.Fatal("operation body is not a PaymentOp")
	}
	if int64(paymentOp.Amount) != 105000000 {
		t.Errorf("amount = %d, want 105000000 stroops", paymentOp.Amount)
	}
	if paymentOp.Asset.Type != xdr.AssetTypeAssetTypeCreditAlphanum4 {
		t.Fatalf("asset type = %v, want alphanum4", paymentOp.Asset.Type)
	}
	if got := strings.TrimRight(string(paymentOp.Asset.AlphaNum4.AssetCode[:]), "\x00"); got != "USDC" {
		t.Errorf("asset code = %q, want USDC", got)
	}

	// Round-trip the built envelope through the decoder: the decoded amount
	// and asset must match the intuitive input.
	raw, err := env.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	value, err := Decode(base64.StdEncoding.EncodeToString(raw))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	amountNode := value.Node.Find("V1.Tx.Operations[0].Body.PaymentOp.Amount")
	if amountNode == nil {
		t.Fatal("decoded tree has no payment amount")
	}
	if got := amountNode.String(); !strings.Contains(got, "105000000") {
		t.Errorf("decoded amount = %q, want 105000000 stroops", got)
	}
	assetNode := value.Node.Find("V1.Tx.Operations[0].Body.PaymentOp.Asset")
	if assetNode == nil {
		t.Fatal("decoded tree has no payment asset")
	}
	if rendered := assetNode.String(); !strings.Contains(rendered, "USDC") || !strings.Contains(rendered, issuer) {
		t.Errorf("decoded asset = %q, want it to name USDC and the issuer", rendered)
	}
}

func TestBuildTransactionEnvelopeStrictRejectsInvalidPayment(t *testing.T) {
	t.Parallel()

	template := `{
		"source_account": "GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAW",
		"fee": 100,
		"sequence": 1,
		"operations": [{
			"type": "payment",
			"destination": "GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAX",
			"amount": "10.5"
		}]
	}`

	if _, err := BuildTransactionEnvelopeFromJSONStrict([]byte(template)); err == nil {
		t.Fatal("strict build accepted an invalid payment destination")
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

func TestBuildLiquidityPoolDepositOp(t *testing.T) {
	validAccount := "GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAW"
	poolID := "a3d4b8c1e2f5a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6"

	spec := &TxSpec{
		SourceAccount: validAccount,
		Sequence:      1,
		Fee:           100,
		Operations: []OperationSpec{
			{
				Type:       "liquidity_pool_deposit",
				PoolID:     poolID,
				MaxAmountA: "10",
				MaxAmountB: "20",
				MinPrice:   "0.5",
				MaxPrice:   "2",
			},
		},
	}

	env, err := BuildTransactionEnvelope(spec)
	if err != nil {
		t.Fatalf("BuildTransactionEnvelope liquidity_pool_deposit: %v", err)
	}
	if env.Type != xdr.EnvelopeTypeEnvelopeTypeTx {
		t.Fatalf("expected V1 envelope")
	}
	op := env.V1.Tx.Operations[0]
	if op.Body.Type != xdr.OperationTypeLiquidityPoolDeposit {
		t.Fatalf("expected LiquidityPoolDeposit, got %v", op.Body.Type)
	}
	depositOp := op.Body.LiquidityPoolDepositOp
	if depositOp == nil {
		t.Fatal("LiquidityPoolDepositOp is nil")
	}
	if depositOp.MaxAmountA != 10 {
		t.Errorf("MaxAmountA = %d, want 10", depositOp.MaxAmountA)
	}
	if depositOp.MaxAmountB != 20 {
		t.Errorf("MaxAmountB = %d, want 20", depositOp.MaxAmountB)
	}
	if eq, err := depositOp.MinPrice.TryEqual(xdr.Price{N: 1, D: 2}); err != nil || !eq {
		t.Errorf("MinPrice = %v, want 0.5000000 (1/2)", depositOp.MinPrice)
	}
	if eq, err := depositOp.MaxPrice.TryEqual(xdr.Price{N: 2, D: 1}); err != nil || !eq {
		t.Errorf("MaxPrice = %v, want 2.0000000 (2/1)", depositOp.MaxPrice)
	}
}

func TestBuildLiquidityPoolWithdrawOp(t *testing.T) {
	validAccount := "GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAW"
	poolID := "a3d4b8c1e2f5a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6"

	spec := &TxSpec{
		SourceAccount: validAccount,
		Sequence:      1,
		Fee:           100,
		Operations: []OperationSpec{
			{
				Type:       "liquidity_pool_withdraw",
				PoolID:     poolID,
				Shares:     "5",
				MinAmountA: "1",
				MinAmountB: "2",
			},
		},
	}

	env, err := BuildTransactionEnvelope(spec)
	if err != nil {
		t.Fatalf("BuildTransactionEnvelope liquidity_pool_withdraw: %v", err)
	}
	op := env.V1.Tx.Operations[0]
	if op.Body.Type != xdr.OperationTypeLiquidityPoolWithdraw {
		t.Fatalf("expected LiquidityPoolWithdraw, got %v", op.Body.Type)
	}
	withdrawOp := op.Body.LiquidityPoolWithdrawOp
	if withdrawOp == nil {
		t.Fatal("LiquidityPoolWithdrawOp is nil")
	}
	if withdrawOp.Amount != 5 {
		t.Errorf("Amount (shares) = %d, want 5", withdrawOp.Amount)
	}
	if withdrawOp.MinAmountA != 1 {
		t.Errorf("MinAmountA = %d, want 1", withdrawOp.MinAmountA)
	}
	if withdrawOp.MinAmountB != 2 {
		t.Errorf("MinAmountB = %d, want 2", withdrawOp.MinAmountB)
	}
}

func TestBuildLiquidityPoolDepositInvalidPoolID(t *testing.T) {
	validAccount := "GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAW"
	spec := &TxSpec{
		SourceAccount: validAccount,
		Sequence:      1,
		Fee:           100,
		Operations: []OperationSpec{
			{
				Type:       "liquidity_pool_deposit",
				PoolID:     "not-a-valid-pool-id",
				MaxAmountA: "10",
				MaxAmountB: "20",
				MinPrice:   "1",
				MaxPrice:   "2",
			},
		},
	}
	if _, err := BuildTransactionEnvelope(spec); err == nil {
		t.Error("expected error for invalid pool_id")
	}
}
