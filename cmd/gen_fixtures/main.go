// +build ignore

package main

import (
	"fmt"
	"os"

	"github.com/stellar/go-stellar-sdk/xdr"
)

func marshalB64(v interface{}) string {
	s, err := xdr.MarshalBase64(v)
	if err != nil {
		panic(fmt.Sprintf("marshal: %v", err))
	}
	return s
}

func write(name, content string) {
	path := "testdata/" + name + ".txt"
	if err := os.WriteFile(path, []byte(content+"\n"), 0644); err != nil {
		panic(err)
	}
	fmt.Printf("wrote %s\n", path)
}

func main() {
	// OperationBody: a simple Payment operation body
	paymentDest := xdr.MustMuxedAddress("GAISEM2EKVTHPCEZVK54ZXPO74ABCIRTIRKWM54ITGVLXTG5537QAQZ5")
	opBody := xdr.OperationBody{
		Type: xdr.OperationTypePayment,
		PaymentOp: &xdr.PaymentOp{
			Destination: paymentDest,
			Asset:       xdr.Asset{Type: xdr.AssetTypeAssetTypeNative},
			Amount:      100_000_000, // 10 XLM
		},
	}
	write("operation_body", marshalB64(opBody))

	// LedgerEntryChange: a created account entry
	acctID := xdr.MustAddress("GAISEM2EKVTHPCEZVK54ZXPO74ABCIRTIRKWM54ITGVLXTG5537QAQZ5")
	seqNum := xdr.SequenceNumber(1)
	ledgerChange := xdr.LedgerEntryChange{
		Type: xdr.LedgerEntryChangeTypeLedgerEntryCreated,
		Created: &xdr.LedgerEntry{
			LastModifiedLedgerSeq: 100,
			Data: xdr.LedgerEntryData{
				Type: xdr.LedgerEntryTypeAccount,
				Account: &xdr.AccountEntry{
					AccountId:     acctID,
					Balance:       100_000_000,
					SeqNum:        seqNum,
					NumSubEntries: 0,
					Flags:         0,
				},
			},
		},
	}
	write("ledger_entry_change_created", marshalB64(ledgerChange))

	// LedgerEntryChange: updated
	ledgerChangeUpdated := xdr.LedgerEntryChange{
		Type: xdr.LedgerEntryChangeTypeLedgerEntryUpdated,
		Updated: &xdr.LedgerEntry{
			LastModifiedLedgerSeq: 101,
			Data: xdr.LedgerEntryData{
				Type: xdr.LedgerEntryTypeAccount,
				Account: &xdr.AccountEntry{
					AccountId:     acctID,
					Balance:       200_000_000,
					SeqNum:        seqNum,
					NumSubEntries: 0,
					Flags:         0,
				},
			},
		},
	}
	write("ledger_entry_change_updated", marshalB64(ledgerChangeUpdated))

	// ClaimOfferAtom
	sellerID := xdr.MustAddress("GAISEM2EKVTHPCEZVK54ZXPO74ABCIRTIRKWM54ITGVLXTG5537QAQZ5")
	issuerID := xdr.MustAddress("GAQSEIZEEUTCOKBJFIVSYLJOF4YDCMRTGQ2TMNZYHE5DWPB5HY7UAIOK")
	claimAtom := xdr.ClaimOfferAtom{
		SellerId: sellerID,
		OfferId:  99,
		AssetSold: xdr.Asset{
			Type: xdr.AssetTypeAssetTypeCreditAlphanum4,
			AlphaNum4: &xdr.AlphaNum4{
				AssetCode: [4]byte{'U', 'S', 'D', 'C'},
				Issuer:    issuerID,
			},
		},
		AmountSold: 50_000_000,
		AssetBought: xdr.Asset{Type: xdr.AssetTypeAssetTypeNative},
		AmountBought: 75_000_000,
	}
	write("claim_offer_atom", marshalB64(claimAtom))

	// ClaimOfferAtomV0
	claimAtomV0 := xdr.ClaimOfferAtomV0{
		SellerEd25519: xdr.Uint256{1, 2, 3},
		OfferId:       100,
		AssetSold:     xdr.Asset{Type: xdr.AssetTypeAssetTypeNative},
		AmountSold:    30_000_000,
		AssetBought: xdr.Asset{
			Type: xdr.AssetTypeAssetTypeCreditAlphanum4,
			AlphaNum4: &xdr.AlphaNum4{
				AssetCode: [4]byte{'U', 'S', 'D', 'C'},
				Issuer:    issuerID,
			},
		},
		AmountBought: 20_000_000,
	}
	write("claim_offer_atom_v0", marshalB64(claimAtomV0))

	// ConfigSettingId enum
	csID := xdr.ConfigSettingIdConfigSettingContractMaxSizeBytes
	write("config_setting_id", marshalB64(csID))
}
