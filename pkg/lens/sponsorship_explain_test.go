package lens_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

func TestDiagnoseSponsorshipSequence(t *testing.T) {
	t.Parallel()
	envelope := sponsorshipEnvelope(
		xdr.Operation{Body: xdr.OperationBody{Type: xdr.OperationTypeBeginSponsoringFutureReserves}},
		xdr.Operation{Body: xdr.OperationBody{Type: xdr.OperationTypeBeginSponsoringFutureReserves}},
	)
	result := &xdr.TransactionResult{Result: xdr.TransactionResultResult{Code: xdr.TransactionResultCodeTxBadSponsorship}}
	d, err := lens.DiagnoseSponsorship(envelope, result)
	if err != nil {
		t.Fatalf("DiagnoseSponsorship() error = %v", err)
	}
	if d.TransactionCode != "tx_bad_sponsorship" || len(d.Conflicts) != 2 || len(d.Operations) != 2 {
		t.Errorf("diagnosis = %+v, want nested and unmatched sponsorship conflicts", d)
	}
	if !strings.Contains(d.Summary, "before operation 0") {
		t.Errorf("Summary = %q, want nested-operation detail", d.Summary)
	}
}

func TestDiagnoseSponsorshipUnmatchedEndAndNonSponsorship(t *testing.T) {
	t.Parallel()
	endOnly := sponsorshipEnvelope(xdr.Operation{Body: xdr.OperationBody{Type: xdr.OperationTypeEndSponsoringFutureReserves}})
	failed := &xdr.TransactionResult{Result: xdr.TransactionResultResult{Code: xdr.TransactionResultCodeTxBadSponsorship}}
	d, err := lens.DiagnoseSponsorship(endOnly, failed)
	if err != nil || len(d.Conflicts) != 1 || !strings.Contains(d.Conflicts[0], "no preceding") {
		t.Errorf("unmatched end diagnosis = %+v, %v", d, err)
	}
	emptyOperations := []xdr.OperationResult{}
	nonSponsorship := &xdr.TransactionResult{Result: xdr.TransactionResultResult{Code: xdr.TransactionResultCodeTxFailed, Results: &emptyOperations}}
	if d, err := lens.DiagnoseSponsorship(endOnly, nonSponsorship); err != nil || d != nil {
		t.Errorf("non-sponsorship diagnosis = %+v, %v; want nil, nil", d, err)
	}
	if _, err := lens.DiagnoseSponsorship(nil, failed); !errors.Is(err, lens.ErrInvalidArgument) {
		t.Errorf("nil envelope error = %v, want ErrInvalidArgument", err)
	}
}

func TestDiagnoseSponsorshipOperationFailureAndBalancedSequence(t *testing.T) {
	t.Parallel()
	key := xdr.Uint256{}
	beginOp := xdr.Operation{Body: xdr.OperationBody{
		Type: xdr.OperationTypeBeginSponsoringFutureReserves,
		BeginSponsoringFutureReservesOp: &xdr.BeginSponsoringFutureReservesOp{
			SponsoredId: xdr.AccountId{Type: xdr.PublicKeyTypePublicKeyTypeEd25519, Ed25519: &key},
		},
	}}
	endOp := xdr.Operation{Body: xdr.OperationBody{Type: xdr.OperationTypeEndSponsoringFutureReserves}}
	revokeOp := xdr.Operation{Body: xdr.OperationBody{Type: xdr.OperationTypeRevokeSponsorship}}
	envelope := sponsorshipEnvelope(beginOp, endOp, revokeOp)
	beginResult := xdr.BeginSponsoringFutureReservesResult{Code: xdr.BeginSponsoringFutureReservesResultCodeBeginSponsoringFutureReservesAlreadySponsored}
	tr := xdr.OperationResultTr{Type: xdr.OperationTypeBeginSponsoringFutureReserves, BeginSponsoringFutureReservesResult: &beginResult}
	operations := []xdr.OperationResult{{Code: xdr.OperationResultCodeOpInner, Tr: &tr}}
	result := &xdr.TransactionResult{Result: xdr.TransactionResultResult{Code: xdr.TransactionResultCodeTxFailed, Results: &operations}}
	d, err := lens.DiagnoseSponsorship(envelope, result)
	if err != nil {
		t.Fatalf("DiagnoseSponsorship(operation result) error = %v", err)
	}
	if d.OperationIndex == nil || *d.OperationIndex != 0 || d.OperationCode != "begin_sponsoring_future_reserves_already_sponsored" || len(d.Conflicts) != 0 {
		t.Errorf("diagnosis = %+v, want failed begin and balanced sequence", d)
	}
	if len(d.Operations) != 3 || d.Operations[0].ResultCode != d.OperationCode {
		t.Errorf("sponsorship steps = %+v, want operation result attribution", d.Operations)
	}
}

func sponsorshipEnvelope(operations ...xdr.Operation) *xdr.TransactionEnvelope {
	return &xdr.TransactionEnvelope{
		Type: xdr.EnvelopeTypeEnvelopeTypeTx,
		V1:   &xdr.TransactionV1Envelope{Tx: xdr.Transaction{Operations: operations}},
	}
}

func revokeLedgerEntryOp(key xdr.LedgerKey) xdr.RevokeSponsorshipOp {
	return xdr.RevokeSponsorshipOp{Type: xdr.RevokeSponsorshipTypeRevokeSponsorshipLedgerEntry, LedgerKey: &key}
}

func revokeSignerOp(account xdr.AccountId, key xdr.SignerKey) xdr.RevokeSponsorshipOp {
	return xdr.RevokeSponsorshipOp{
		Type:   xdr.RevokeSponsorshipTypeRevokeSponsorshipSigner,
		Signer: &xdr.RevokeSponsorshipOpSigner{AccountId: account, SignerKey: key},
	}
}

func TestDiagnoseRevokeSponsorship(t *testing.T) {
	t.Parallel()

	owner := explainTestAccount(0x11)
	ownerAddr := owner.Address()
	short := shortAddress(ownerAddr)
	signerKey := xdr.SignerKey{Type: xdr.SignerKeyTypeSignerKeyTypeHashX, HashX: explainTestKey(0x22)}
	balanceID := xdr.Hash{0xAB, 0xCD}

	accountKey := xdr.LedgerKey{Type: xdr.LedgerEntryTypeAccount, Account: &xdr.LedgerKeyAccount{AccountId: owner}}
	dataKey := xdr.LedgerKey{Type: xdr.LedgerEntryTypeData, Data: &xdr.LedgerKeyData{AccountId: owner, DataName: "config"}}
	offerKey := xdr.LedgerKey{Type: xdr.LedgerEntryTypeOffer, Offer: &xdr.LedgerKeyOffer{SellerId: owner, OfferId: 7}}
	poolLineKey := xdr.LedgerKey{Type: xdr.LedgerEntryTypeTrustline, TrustLine: &xdr.LedgerKeyTrustLine{
		AccountId: owner,
		Asset:     xdr.TrustLineAsset{Type: xdr.AssetTypeAssetTypePoolShare, LiquidityPoolId: &xdr.PoolId{}},
	}}
	balanceKey := xdr.LedgerKey{Type: xdr.LedgerEntryTypeClaimableBalance, ClaimableBalance: &xdr.LedgerKeyClaimableBalance{
		BalanceId: xdr.ClaimableBalanceId{Type: xdr.ClaimableBalanceIdTypeClaimableBalanceIdTypeV0, V0: &balanceID},
	}}

	tests := []struct {
		name string
		op   xdr.RevokeSponsorshipOp
		code xdr.RevokeSponsorshipResultCode
		want []string
		deny []string
	}{
		{
			name: "low reserve on a data entry states one base reserve",
			op:   revokeLedgerEntryOp(dataKey),
			code: xdr.RevokeSponsorshipResultCodeRevokeSponsorshipLowReserve,
			want: []string{`ledger entry for data "config"`, "5000000 stroops (0.5 XLM)", "owner " + short, "available XLM"},
			deny: []string{"signer"},
		},
		{
			name: "low reserve on an account states two base reserves",
			op:   revokeLedgerEntryOp(accountKey),
			code: xdr.RevokeSponsorshipResultCodeRevokeSponsorshipLowReserve,
			want: []string{"ledger entry for account " + short, "10000000 stroops (1 XLM)"},
		},
		{
			name: "low reserve on a pool share trustline states two base reserves",
			op:   revokeLedgerEntryOp(poolLineKey),
			code: xdr.RevokeSponsorshipResultCodeRevokeSponsorshipLowReserve,
			want: []string{"liquidity pool shares trustline", "10000000 stroops (1 XLM)"},
		},
		{
			name: "low reserve on a claimable balance is per claimant",
			op:   revokeLedgerEntryOp(balanceKey),
			code: xdr.RevokeSponsorshipResultCodeRevokeSponsorshipLowReserve,
			want: []string{"claimable balance abcd0000…", "5000000 stroops (0.5 XLM) per claimant"},
		},
		{
			name: "low reserve on a signer names the signer and its account",
			op:   revokeSignerOp(owner, signerKey),
			code: xdr.RevokeSponsorshipResultCodeRevokeSponsorshipLowReserve,
			want: []string{"hash-x signer", "on account " + short, "5000000 stroops (0.5 XLM)"},
			deny: []string{"ledger entry"},
		},
		{
			name: "not sponsor on a ledger entry explains who may revoke",
			op:   revokeLedgerEntryOp(offerKey),
			code: xdr.RevokeSponsorshipResultCodeRevokeSponsorshipNotSponsor,
			want: []string{"ledger entry for offer 7", "current sponsor", "owner " + short},
		},
		{
			name: "not sponsor on a signer explains who may revoke",
			op:   revokeSignerOp(owner, signerKey),
			code: xdr.RevokeSponsorshipResultCodeRevokeSponsorshipNotSponsor,
			want: []string{"hash-x signer", "current sponsor", "owner " + short},
			deny: []string{"ledger entry"},
		},
		{
			name: "missing ledger entry",
			op:   revokeLedgerEntryOp(dataKey),
			code: xdr.RevokeSponsorshipResultCodeRevokeSponsorshipDoesNotExist,
			want: []string{"no such entry exists on the ledger"},
		},
		{
			name: "missing signer",
			op:   revokeSignerOp(owner, signerKey),
			code: xdr.RevokeSponsorshipResultCodeRevokeSponsorshipDoesNotExist,
			want: []string{"does not list that signer"},
		},
		{
			name: "claimable balance sponsorship is only transferable",
			op:   revokeLedgerEntryOp(balanceKey),
			code: xdr.RevokeSponsorshipResultCodeRevokeSponsorshipOnlyTransferable,
			want: []string{"must always have a sponsor", "begin_sponsoring_future_reserves"},
		},
		{
			name: "malformed offer id",
			op:   revokeLedgerEntryOp(xdr.LedgerKey{Type: xdr.LedgerEntryTypeOffer, Offer: &xdr.LedgerKeyOffer{SellerId: owner}}),
			code: xdr.RevokeSponsorshipResultCodeRevokeSponsorshipMalformed,
			want: []string{"an offer ID must be positive"},
		},
		{
			name: "malformed native trustline",
			op: revokeLedgerEntryOp(xdr.LedgerKey{Type: xdr.LedgerEntryTypeTrustline, TrustLine: &xdr.LedgerKeyTrustLine{
				AccountId: owner,
				Asset:     xdr.TrustLineAsset{Type: xdr.AssetTypeAssetTypeNative},
			}}),
			code: xdr.RevokeSponsorshipResultCodeRevokeSponsorshipMalformed,
			want: []string{"the native asset has no trustline"},
		},
		{
			name: "malformed self-issued trustline",
			op: revokeLedgerEntryOp(xdr.LedgerKey{Type: xdr.LedgerEntryTypeTrustline, TrustLine: &xdr.LedgerKeyTrustLine{
				AccountId: owner,
				Asset:     xdr.MustNewCreditAsset("USDC", ownerAddr).ToTrustLineAsset(),
			}}),
			code: xdr.RevokeSponsorshipResultCodeRevokeSponsorshipMalformed,
			want: []string{"an issuer holds no trustline to its own asset"},
		},
		{
			name: "malformed empty data name",
			op:   revokeLedgerEntryOp(xdr.LedgerKey{Type: xdr.LedgerEntryTypeData, Data: &xdr.LedgerKeyData{AccountId: owner}}),
			code: xdr.RevokeSponsorshipResultCodeRevokeSponsorshipMalformed,
			want: []string{"a data entry name cannot be empty"},
		},
		{
			name: "malformed unsupported entry type",
			op:   revokeLedgerEntryOp(xdr.LedgerKey{Type: xdr.LedgerEntryTypeTtl, Ttl: &xdr.LedgerKeyTtl{}}),
			code: xdr.RevokeSponsorshipResultCodeRevokeSponsorshipMalformed,
			want: []string{"only accounts, trustlines, offers, data entries and claimable balances can be sponsored"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := lens.DiagnoseRevokeSponsorship(&tc.op, tc.code)
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("DiagnoseRevokeSponsorship() = %q, want it to contain %q", got, want)
				}
			}
			for _, deny := range tc.deny {
				if strings.Contains(got, deny) {
					t.Errorf("DiagnoseRevokeSponsorship() = %q, must not contain %q", got, deny)
				}
			}
		})
	}
}

func TestDiagnoseRevokeSponsorshipNothingToDiagnose(t *testing.T) {
	t.Parallel()

	op := revokeSignerOp(explainTestAccount(0x11), xdr.SignerKey{Type: xdr.SignerKeyTypeSignerKeyTypeHashX, HashX: explainTestKey(0x22)})
	if got := lens.DiagnoseRevokeSponsorship(nil, xdr.RevokeSponsorshipResultCodeRevokeSponsorshipLowReserve); got != "" {
		t.Errorf("nil operation = %q, want empty", got)
	}
	if got := lens.DiagnoseRevokeSponsorship(&op, xdr.RevokeSponsorshipResultCodeRevokeSponsorshipSuccess); got != "" {
		t.Errorf("success = %q, want empty", got)
	}
}

// TestDiagnoseRevokeSponsorshipToleratesNilArms pins that a union with its
// selected arm missing still yields an explanation instead of a panic.
func TestDiagnoseRevokeSponsorshipToleratesNilArms(t *testing.T) {
	t.Parallel()

	for _, op := range []xdr.RevokeSponsorshipOp{
		{Type: xdr.RevokeSponsorshipTypeRevokeSponsorshipLedgerEntry},
		{Type: xdr.RevokeSponsorshipTypeRevokeSponsorshipSigner},
	} {
		if got := lens.DiagnoseRevokeSponsorship(&op, xdr.RevokeSponsorshipResultCodeRevokeSponsorshipNotSponsor); got == "" {
			t.Errorf("DiagnoseRevokeSponsorship(%v) is empty, want an explanation", op.Type)
		}
	}
}

func TestExplainPairDeepensRevokeSponsorshipFailures(t *testing.T) {
	t.Parallel()

	owner := explainTestAccount(0x11)
	source := explainTestAccount(0xA1)
	signerKey := xdr.SignerKey{Type: xdr.SignerKeyTypeSignerKeyTypeHashX, HashX: explainTestKey(0x22)}
	dataKey := xdr.LedgerKey{Type: xdr.LedgerEntryTypeData, Data: &xdr.LedgerKeyData{AccountId: owner, DataName: "config"}}

	tests := []struct {
		name string
		op   xdr.RevokeSponsorshipOp
		want []string
	}{
		{"ledger entry", revokeLedgerEntryOp(dataKey), []string{"Source account " + shortAddress(source.Address()), "ledger entry for data"}},
		{"signer", revokeSignerOp(owner, signerKey), []string{"Source account " + shortAddress(source.Address()), "hash-x signer"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := explainSingleOperation(t,
				xdr.OperationBody{Type: xdr.OperationTypeRevokeSponsorship, RevokeSponsorshipOp: &tc.op},
				xdr.OperationResultTr{
					Type:                    xdr.OperationTypeRevokeSponsorship,
					RevokeSponsorshipResult: &xdr.RevokeSponsorshipResult{Code: xdr.RevokeSponsorshipResultCodeRevokeSponsorshipNotSponsor},
				})
			assertReasonContains(t, got, false, tc.want...)
		})
	}
}
