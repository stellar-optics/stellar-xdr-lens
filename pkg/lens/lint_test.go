package lens_test

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

func buildTestEnvelope(t *testing.T, ops ...xdr.Operation) *xdr.TransactionEnvelope {
	t.Helper()

	var key xdr.Uint256
	key[0] = 0x01
	src := xdr.MuxedAccount{
		Type:    xdr.CryptoKeyTypeKeyTypeEd25519,
		Ed25519: &key,
	}

	return &xdr.TransactionEnvelope{
		Type: xdr.EnvelopeTypeEnvelopeTypeTx,
		V1: &xdr.TransactionV1Envelope{
			Tx: xdr.Transaction{
				SourceAccount: src,
				Fee:           100,
				SeqNum:        1,
				Operations:    ops,
			},
		},
	}
}

func makeCreditAsset(code string, issuerByte byte) xdr.Asset {
	var issuerKey xdr.Uint256
	issuerKey[0] = issuerByte
	issuer := xdr.AccountId{
		Type:    xdr.PublicKeyTypePublicKeyTypeEd25519,
		Ed25519: &issuerKey,
	}
	var c [4]byte
	copy(c[:], code)
	return xdr.Asset{
		Type: xdr.AssetTypeAssetTypeCreditAlphanum4,
		AlphaNum4: &xdr.AlphaNum4{
			AssetCode: c,
			Issuer:    issuer,
		},
	}
}

func makePaymentOp(destByte byte, amount xdr.Int64) xdr.Operation {
	var key xdr.Uint256
	key[0] = destByte
	return xdr.Operation{
		Body: xdr.OperationBody{
			Type: xdr.OperationTypePayment,
			PaymentOp: &xdr.PaymentOp{
				Destination: xdr.MuxedAccount{
					Type:    xdr.CryptoKeyTypeKeyTypeEd25519,
					Ed25519: &key,
				},
				Asset:  xdr.Asset{Type: xdr.AssetTypeAssetTypeNative},
				Amount: amount,
			},
		},
	}
}

func makeChangeTrustOp(asset xdr.Asset, limit xdr.Int64) xdr.Operation {
	return xdr.Operation{
		Body: xdr.OperationBody{
			Type: xdr.OperationTypeChangeTrust,
			ChangeTrustOp: &xdr.ChangeTrustOp{
				Line:  asset.ToChangeTrustAsset(),
				Limit: limit,
			},
		},
	}
}

func makePathPaymentStrictReceiveOp(sendAsset, destAsset xdr.Asset, path []xdr.Asset) xdr.Operation {
	var key xdr.Uint256
	key[0] = 0x02
	return xdr.Operation{
		Body: xdr.OperationBody{
			Type: xdr.OperationTypePathPaymentStrictReceive,
			PathPaymentStrictReceiveOp: &xdr.PathPaymentStrictReceiveOp{
				SendAsset: sendAsset,
				SendMax:   1000,
				Destination: xdr.MuxedAccount{
					Type:    xdr.CryptoKeyTypeKeyTypeEd25519,
					Ed25519: &key,
				},
				DestAsset:  destAsset,
				DestAmount: 500,
				Path:       path,
			},
		},
	}
}

func makePathPaymentStrictSendOp(sendAsset, destAsset xdr.Asset, path []xdr.Asset) xdr.Operation {
	var key xdr.Uint256
	key[0] = 0x02
	return xdr.Operation{
		Body: xdr.OperationBody{
			Type: xdr.OperationTypePathPaymentStrictSend,
			PathPaymentStrictSendOp: &xdr.PathPaymentStrictSendOp{
				SendAsset:  sendAsset,
				SendAmount: 1000,
				Destination: xdr.MuxedAccount{
					Type:    xdr.CryptoKeyTypeKeyTypeEd25519,
					Ed25519: &key,
				},
				DestAsset: destAsset,
				DestMin:   500,
				Path:      path,
			},
		},
	}
}

func TestLintRedundantOperations(t *testing.T) {
	t.Parallel()

	xlm := xdr.Asset{Type: xdr.AssetTypeAssetTypeNative}
	usdc := makeCreditAsset("USDC", 0x10)
	eurc := makeCreditAsset("EURC", 0x20)

	tests := []struct {
		name         string
		ops          []xdr.Operation
		wantCount    int
		wantSeverity lens.LintSeverity
		wantOpIndex  int
		wantSubstr   string
	}{
		{
			name:      "clean single payment",
			ops:       []xdr.Operation{makePaymentOp(0x02, 100)},
			wantCount: 0,
		},
		{
			name:         "path payment strict receive same native asset empty path",
			ops:          []xdr.Operation{makePathPaymentStrictReceiveOp(xlm, xlm, nil)},
			wantCount:    1,
			wantSeverity: lens.LintSeverityWarning,
			wantOpIndex:  0,
			wantSubstr:   "PathPaymentStrictReceive",
		},
		{
			name:         "path payment strict send same credit asset empty path",
			ops:          []xdr.Operation{makePathPaymentStrictSendOp(usdc, usdc, nil)},
			wantCount:    1,
			wantSeverity: lens.LintSeverityWarning,
			wantOpIndex:  0,
			wantSubstr:   "PathPaymentStrictSend",
		},
		{
			name:      "path payment same asset with intermediate hop is allowed",
			ops:       []xdr.Operation{makePathPaymentStrictSendOp(usdc, usdc, []xdr.Asset{xlm})},
			wantCount: 0,
		},
		{
			name:      "path payment different assets empty path is allowed",
			ops:       []xdr.Operation{makePathPaymentStrictReceiveOp(xlm, usdc, nil)},
			wantCount: 0,
		},
		{
			name: "consecutive identical payments",
			ops: []xdr.Operation{
				makePaymentOp(0x02, 500),
				makePaymentOp(0x02, 500),
			},
			wantCount:    1,
			wantSeverity: lens.LintSeverityWarning,
			wantOpIndex:  1,
			wantSubstr:   "identical to the immediately preceding operation 0",
		},
		{
			name: "distinct consecutive payments are allowed",
			ops: []xdr.Operation{
				makePaymentOp(0x02, 500),
				makePaymentOp(0x02, 600),
			},
			wantCount: 0,
		},
		{
			name: "redundant change trust limit on same asset",
			ops: []xdr.Operation{
				makeChangeTrustOp(usdc, 1000000),
				makePaymentOp(0x02, 100),
				makeChangeTrustOp(usdc, 1000000),
			},
			wantCount:    1,
			wantSeverity: lens.LintSeverityWarning,
			wantOpIndex:  2,
			wantSubstr:   "repeats a ChangeTrust limit",
		},
		{
			name: "change trust updating to different limit is allowed",
			ops: []xdr.Operation{
				makeChangeTrustOp(usdc, 1000000),
				makeChangeTrustOp(usdc, 2000000),
			},
			wantCount: 0,
		},
		{
			name: "change trust on different assets with same limit is allowed",
			ops: []xdr.Operation{
				makeChangeTrustOp(usdc, 1000000),
				makeChangeTrustOp(eurc, 1000000),
			},
			wantCount: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			env := buildTestEnvelope(t, tc.ops...)
			raw, err := env.MarshalBinary()
			if err != nil {
				t.Fatalf("MarshalBinary: %v", err)
			}
			val, err := lens.Decode(base64.StdEncoding.EncodeToString(raw))
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}

			findings, err := lens.LintValue(val)
			if err != nil {
				t.Fatalf("LintValue: %v", err)
			}
			if len(findings) != tc.wantCount {
				t.Fatalf("got %d findings (%+v), want %d", len(findings), findings, tc.wantCount)
			}
			if tc.wantCount == 0 {
				return
			}

			f := findings[0]
			if f.Rule != "redundant-operations" {
				t.Errorf("Rule = %q, want %q", f.Rule, "redundant-operations")
			}
			if f.Severity != tc.wantSeverity {
				t.Errorf("Severity = %q, want %q", f.Severity, tc.wantSeverity)
			}
			if f.OpIndex != tc.wantOpIndex {
				t.Errorf("OpIndex = %d, want %d", f.OpIndex, tc.wantOpIndex)
			}
			if tc.wantSubstr != "" && !strings.Contains(f.Message, tc.wantSubstr) {
				t.Errorf("Message = %q, want substring %q", f.Message, tc.wantSubstr)
			}
		})
	}
}

func TestLintValueErrors(t *testing.T) {
	t.Parallel()

	if _, err := lens.LintValue(nil); !errors.Is(err, lens.ErrInvalidArgument) {
		t.Errorf("LintValue(nil) error = %v, want ErrInvalidArgument", err)
	}

	// Non-TransactionEnvelope value should return ErrInvalidArgument.
	val, err := lens.DecodeAs("AAAAAA==", "Asset")
	if err != nil {
		t.Fatalf("DecodeAs: %v", err)
	}
	if _, err := lens.LintValue(val); !errors.Is(err, lens.ErrInvalidArgument) {
		t.Errorf("LintValue(Asset) error = %v, want ErrInvalidArgument", err)
	}

	if got := lens.LintTransaction(nil); len(got) != 0 {
		t.Errorf("LintTransaction(nil) = %+v, want empty", got)
	}
}
