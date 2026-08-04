package lens_test

import (
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

// Well-formed mainnet addresses, used to build synthetic envelopes. Real
// strkeys are used rather than zero values because the enrichers decode them.
const (
	testSource = "GCJNHFXJEQEY54NPKU4DBMT5WOBJUHMYSFBDAQF4OMTEGJN4BQU7ZST6"
	testDest   = "GAPB7YMUQ6MPQ23PTFDNBZP7HGGTZPI2DT5JJURUOIXNPLGHW2O3WYXZ"
	testIssuer = "GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAW"
)

func mustAccountID(t *testing.T, addr string) xdr.AccountId {
	t.Helper()
	var aid xdr.AccountId
	if err := aid.SetAddress(addr); err != nil {
		t.Fatalf("parsing address %s: %v", addr, err)
	}
	return aid
}

func mustMuxed(t *testing.T, addr string) xdr.MuxedAccount {
	t.Helper()
	aid := mustAccountID(t, addr)
	return aid.ToMuxedAccount()
}

func creditAsset(t *testing.T, code, issuer string) xdr.Asset {
	t.Helper()
	var a xdr.Asset
	if err := a.SetCredit(code, mustAccountID(t, issuer)); err != nil {
		t.Fatalf("building asset %s: %v", code, err)
	}
	return a
}

// buildEnvelope wraps operations in a minimal, valid v1 transaction envelope
// and returns it as base64, so the test exercises the real decode path rather
// than reaching into internals.
func buildEnvelope(t *testing.T, memo xdr.Memo, cond xdr.Preconditions, ops ...xdr.Operation) string {
	t.Helper()

	env := xdr.TransactionEnvelope{
		Type: xdr.EnvelopeTypeEnvelopeTypeTx,
		V1: &xdr.TransactionV1Envelope{
			Tx: xdr.Transaction{
				SourceAccount: mustMuxed(t, testSource),
				Fee:           xdr.Uint32(100 * len(ops)),
				SeqNum:        xdr.SequenceNumber(12345),
				Cond:          cond,
				Memo:          memo,
				Operations:    ops,
			},
		},
	}
	return mustMarshal(t, env)
}

func noMemo() xdr.Memo { return xdr.Memo{Type: xdr.MemoTypeMemoNone} }

func noCond() xdr.Preconditions {
	return xdr.Preconditions{Type: xdr.PreconditionTypePrecondNone}
}

// TestDescribeOperationCoversClassicTypes asserts that each classic operation
// renders a specific, human-readable line rather than falling back to its bare
// type name. These strings are the product, so they are worth pinning.
func TestDescribeOperationCoversClassicTypes(t *testing.T) {
	t.Parallel()

	native := xdr.Asset{Type: xdr.AssetTypeAssetTypeNative}
	usdc := creditAsset(t, "USDC", testIssuer)

	tests := []struct {
		name        string
		op          xdr.Operation
		wantType    string
		wantContain []string
	}{
		{
			name: "create account",
			op: xdr.Operation{Body: xdr.OperationBody{
				Type: xdr.OperationTypeCreateAccount,
				CreateAccountOp: &xdr.CreateAccountOp{
					Destination:     mustAccountID(t, testDest),
					StartingBalance: 25_0000000,
				},
			}},
			wantType:    "create_account",
			wantContain: []string{"create account", "25.0000000"},
		},
		{
			name: "native payment",
			op: xdr.Operation{Body: xdr.OperationBody{
				Type: xdr.OperationTypePayment,
				PaymentOp: &xdr.PaymentOp{
					Destination: mustMuxed(t, testDest),
					Asset:       native,
					Amount:      100_0000000,
				},
			}},
			wantType:    "payment",
			wantContain: []string{"pay", "100.0000000", "XLM"},
		},
		{
			name: "credit payment",
			op: xdr.Operation{Body: xdr.OperationBody{
				Type: xdr.OperationTypePayment,
				PaymentOp: &xdr.PaymentOp{
					Destination: mustMuxed(t, testDest),
					Asset:       usdc,
					Amount:      50_0000000,
				},
			}},
			wantType:    "payment",
			wantContain: []string{"pay", "50.0000000", "USDC"},
		},
		{
			name: "path payment strict send",
			op: xdr.Operation{Body: xdr.OperationBody{
				Type: xdr.OperationTypePathPaymentStrictSend,
				PathPaymentStrictSendOp: &xdr.PathPaymentStrictSendOp{
					SendAsset:   native,
					SendAmount:  10_0000000,
					Destination: mustMuxed(t, testDest),
					DestAsset:   usdc,
					DestMin:     9_0000000,
				},
			}},
			wantType:    "path_payment_strict_send",
			wantContain: []string{"send exactly", "10.0000000", "at least", "USDC"},
		},
		{
			name: "path payment strict receive",
			op: xdr.Operation{Body: xdr.OperationBody{
				Type: xdr.OperationTypePathPaymentStrictReceive,
				PathPaymentStrictReceiveOp: &xdr.PathPaymentStrictReceiveOp{
					SendAsset:   native,
					SendMax:     12_0000000,
					Destination: mustMuxed(t, testDest),
					DestAsset:   usdc,
					DestAmount:  10_0000000,
				},
			}},
			wantType:    "path_payment_strict_receive",
			wantContain: []string{"at most", "12.0000000", "exactly", "10.0000000"},
		},
		{
			name: "manage sell offer",
			op: xdr.Operation{Body: xdr.OperationBody{
				Type: xdr.OperationTypeManageSellOffer,
				ManageSellOfferOp: &xdr.ManageSellOfferOp{
					Selling: native,
					Buying:  usdc,
					Amount:  5_0000000,
					Price:   xdr.Price{N: 1, D: 2},
					OfferId: 0,
				},
			}},
			wantType:    "manage_sell_offer",
			wantContain: []string{"sell", "5.0000000", "USDC", "1/2"},
		},
		{
			name: "manage buy offer",
			op: xdr.Operation{Body: xdr.OperationBody{
				Type: xdr.OperationTypeManageBuyOffer,
				ManageBuyOfferOp: &xdr.ManageBuyOfferOp{
					Selling:   native,
					Buying:    usdc,
					BuyAmount: 7_0000000,
					Price:     xdr.Price{N: 3, D: 4},
				},
			}},
			wantType:    "manage_buy_offer",
			wantContain: []string{"buy", "7.0000000", "3/4"},
		},
		{
			name: "change trust",
			op: xdr.Operation{Body: xdr.OperationBody{
				Type: xdr.OperationTypeChangeTrust,
				ChangeTrustOp: &xdr.ChangeTrustOp{
					Line:  usdc.ToChangeTrustAsset(),
					Limit: 1000_0000000,
				},
			}},
			wantType:    "change_trust",
			wantContain: []string{"trust", "USDC", "1000.0000000"},
		},
		{
			name: "change trust removal",
			op: xdr.Operation{Body: xdr.OperationBody{
				Type: xdr.OperationTypeChangeTrust,
				ChangeTrustOp: &xdr.ChangeTrustOp{
					Line:  usdc.ToChangeTrustAsset(),
					Limit: 0,
				},
			}},
			wantType:    "change_trust",
			wantContain: []string{"remove trustline", "USDC"},
		},
		{
			name: "account merge",
			op: xdr.Operation{Body: xdr.OperationBody{
				Type:        xdr.OperationTypeAccountMerge,
				Destination: ptr(mustMuxed(t, testDest)),
			}},
			wantType:    "account_merge",
			wantContain: []string{"merge this account into"},
		},
		{
			name: "bump sequence",
			op: xdr.Operation{Body: xdr.OperationBody{
				Type: xdr.OperationTypeBumpSequence,
				BumpSequenceOp: &xdr.BumpSequenceOp{
					BumpTo: xdr.SequenceNumber(99999),
				},
			}},
			wantType:    "bump_sequence",
			wantContain: []string{"bump sequence number to", "99999"},
		},
		{
			name: "manage data set",
			op: xdr.Operation{Body: xdr.OperationBody{
				Type: xdr.OperationTypeManageData,
				ManageDataOp: &xdr.ManageDataOp{
					DataName:  "config",
					DataValue: ptr(xdr.DataValue([]byte("value"))),
				},
			}},
			wantType:    "manage_data",
			wantContain: []string{"set data entry", "config", "5 bytes"},
		},
		{
			name: "manage data delete",
			op: xdr.Operation{Body: xdr.OperationBody{
				Type: xdr.OperationTypeManageData,
				ManageDataOp: &xdr.ManageDataOp{
					DataName: "obsolete",
				},
			}},
			wantType:    "manage_data",
			wantContain: []string{"delete data entry", "obsolete"},
		},
		{
			name: "set options",
			op: xdr.Operation{Body: xdr.OperationBody{
				Type: xdr.OperationTypeSetOptions,
				SetOptionsOp: &xdr.SetOptionsOp{
					MasterWeight:  ptr(xdr.Uint32(2)),
					HomeDomain:    ptr(xdr.String32("example.com")),
					HighThreshold: ptr(xdr.Uint32(3)),
				},
			}},
			wantType:    "set_options",
			wantContain: []string{"master weight 2", "home domain example.com", "high threshold 3"},
		},
		{
			name: "restore footprint",
			op: xdr.Operation{Body: xdr.OperationBody{
				Type:               xdr.OperationTypeRestoreFootprint,
				RestoreFootprintOp: &xdr.RestoreFootprintOp{},
			}},
			wantType:    "restore_footprint",
			wantContain: []string{"restore archived ledger entries"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			payload := buildEnvelope(t, noMemo(), noCond(), tc.op)
			v, err := lens.DecodeAs(payload, "TransactionEnvelope")
			if err != nil {
				t.Fatalf("DecodeAs() error = %v", err)
			}
			summary, err := lens.Explain(v)
			if err != nil {
				t.Fatalf("Explain() error = %v", err)
			}
			if len(summary.Operations) != 1 {
				t.Fatalf("got %d operations, want 1", len(summary.Operations))
			}

			op := summary.Operations[0]
			if op.Type != tc.wantType {
				t.Errorf("Type = %q, want %q", op.Type, tc.wantType)
			}
			for _, want := range tc.wantContain {
				if !strings.Contains(strings.ToLower(op.Detail), strings.ToLower(want)) {
					t.Errorf("Detail = %q, want it to contain %q", op.Detail, want)
				}
			}
			// A description that is only the type name means the switch fell
			// through, which is the bug this test exists to catch.
			if op.Detail == op.Type {
				t.Errorf("Detail is just the type name %q; no description was produced", op.Detail)
			}
		})
	}
}

func TestDescribeMemo(t *testing.T) {
	t.Parallel()

	hash := xdr.Hash{0x01, 0x02, 0x03}

	tests := []struct {
		name        string
		memo        xdr.Memo
		wantContain string
	}{
		{"none", xdr.Memo{Type: xdr.MemoTypeMemoNone}, ""},
		{"text", xdr.Memo{Type: xdr.MemoTypeMemoText, Text: ptr("hello world")}, "text: hello world"},
		{"id", xdr.Memo{Type: xdr.MemoTypeMemoId, Id: ptr(xdr.Uint64(42))}, "id: 42"},
		{"hash", xdr.Memo{Type: xdr.MemoTypeMemoHash, Hash: &hash}, "hash: 010203"},
		{"return", xdr.Memo{Type: xdr.MemoTypeMemoReturn, RetHash: &hash}, "return: 010203"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			op := xdr.Operation{Body: xdr.OperationBody{
				Type:           xdr.OperationTypeBumpSequence,
				BumpSequenceOp: &xdr.BumpSequenceOp{BumpTo: 1},
			}}
			payload := buildEnvelope(t, tc.memo, noCond(), op)

			v, err := lens.DecodeAs(payload, "TransactionEnvelope")
			if err != nil {
				t.Fatalf("DecodeAs() error = %v", err)
			}
			summary, err := lens.Explain(v)
			if err != nil {
				t.Fatalf("Explain() error = %v", err)
			}

			if tc.wantContain == "" {
				if summary.Memo != "" {
					t.Errorf("Memo = %q, want empty", summary.Memo)
				}
				return
			}
			if !strings.Contains(summary.Memo, tc.wantContain) {
				t.Errorf("Memo = %q, want it to contain %q", summary.Memo, tc.wantContain)
			}
		})
	}
}

func TestDescribePreconditions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		cond        xdr.Preconditions
		wantContain string
		wantEmpty   bool
	}{
		{
			name:      "none",
			cond:      noCond(),
			wantEmpty: true,
		},
		{
			name: "zero time bounds say nothing",
			cond: xdr.Preconditions{
				Type:       xdr.PreconditionTypePrecondTime,
				TimeBounds: &xdr.TimeBounds{MinTime: 0, MaxTime: 0},
			},
			wantEmpty: true,
		},
		{
			name: "max time only",
			cond: xdr.Preconditions{
				Type:       xdr.PreconditionTypePrecondTime,
				TimeBounds: &xdr.TimeBounds{MinTime: 0, MaxTime: 1700000000},
			},
			wantContain: "valid until",
		},
		{
			name: "min time only",
			cond: xdr.Preconditions{
				Type:       xdr.PreconditionTypePrecondTime,
				TimeBounds: &xdr.TimeBounds{MinTime: 1700000000, MaxTime: 0},
			},
			wantContain: "valid from",
		},
		{
			name: "both bounds",
			cond: xdr.Preconditions{
				Type:       xdr.PreconditionTypePrecondTime,
				TimeBounds: &xdr.TimeBounds{MinTime: 1700000000, MaxTime: 1700003600},
			},
			wantContain: "until",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			op := xdr.Operation{Body: xdr.OperationBody{
				Type:           xdr.OperationTypeBumpSequence,
				BumpSequenceOp: &xdr.BumpSequenceOp{BumpTo: 1},
			}}
			payload := buildEnvelope(t, noMemo(), tc.cond, op)

			v, err := lens.DecodeAs(payload, "TransactionEnvelope")
			if err != nil {
				t.Fatalf("DecodeAs() error = %v", err)
			}
			summary, err := lens.Explain(v)
			if err != nil {
				t.Fatalf("Explain() error = %v", err)
			}

			joined := strings.Join(summary.Preconds, "; ")
			if tc.wantEmpty {
				if joined != "" {
					t.Errorf("Preconds = %q, want none", joined)
				}
				return
			}
			if !strings.Contains(joined, tc.wantContain) {
				t.Errorf("Preconds = %q, want it to contain %q", joined, tc.wantContain)
			}
		})
	}
}

// TestOperationSourceOverrideIsReported covers the case where an operation
// runs as a different account from the transaction, which changes who must
// sign and is easy to get wrong.
func TestOperationSourceOverrideIsReported(t *testing.T) {
	t.Parallel()

	override := mustMuxed(t, testDest)
	op := xdr.Operation{
		SourceAccount: &override,
		Body: xdr.OperationBody{
			Type:           xdr.OperationTypeBumpSequence,
			BumpSequenceOp: &xdr.BumpSequenceOp{BumpTo: 7},
		},
	}

	v, err := lens.DecodeAs(buildEnvelope(t, noMemo(), noCond(), op), "TransactionEnvelope")
	if err != nil {
		t.Fatalf("DecodeAs() error = %v", err)
	}
	summary, err := lens.Explain(v)
	if err != nil {
		t.Fatalf("Explain() error = %v", err)
	}

	if got := summary.Operations[0].Source; got != testDest {
		t.Errorf("Operations[0].Source = %q, want %q", got, testDest)
	}
}

func TestMultipleOperationsAreAllDescribed(t *testing.T) {
	t.Parallel()

	ops := []xdr.Operation{
		{Body: xdr.OperationBody{
			Type:           xdr.OperationTypeBumpSequence,
			BumpSequenceOp: &xdr.BumpSequenceOp{BumpTo: 1},
		}},
		{Body: xdr.OperationBody{
			Type: xdr.OperationTypePayment,
			PaymentOp: &xdr.PaymentOp{
				Destination: mustMuxed(t, testDest),
				Asset:       xdr.Asset{Type: xdr.AssetTypeAssetTypeNative},
				Amount:      1_0000000,
			},
		}},
	}

	v, err := lens.DecodeAs(buildEnvelope(t, noMemo(), noCond(), ops...), "TransactionEnvelope")
	if err != nil {
		t.Fatalf("DecodeAs() error = %v", err)
	}
	summary, err := lens.Explain(v)
	if err != nil {
		t.Fatalf("Explain() error = %v", err)
	}

	if len(summary.Operations) != 2 {
		t.Fatalf("got %d operations, want 2", len(summary.Operations))
	}
	for i, op := range summary.Operations {
		if op.Index != i {
			t.Errorf("Operations[%d].Index = %d, want %d", i, op.Index, i)
		}
		if op.Detail == "" {
			t.Errorf("Operations[%d].Detail is empty", i)
		}
	}
	// The headline should mention both operation types.
	for _, want := range []string{"bump_sequence", "payment"} {
		if !strings.Contains(summary.Headline, want) {
			t.Errorf("Headline = %q, want it to mention %q", summary.Headline, want)
		}
	}
}

// ptr returns a pointer to v, for building the many optional XDR fields.
func ptr[T any](v T) *T { return &v }
