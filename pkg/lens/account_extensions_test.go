package lens_test

import (
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

// TestAccountSignerThresholdsRendering pins the public decoding path for the
// threshold enricher: a decoded AccountEntry's Thresholds leaf carries the
// named triplet, the locked-master marker when applicable, and the
// signer-capacity analysis.
func TestAccountSignerThresholdsRendering(t *testing.T) {
	t.Parallel()

	aid := mustAccountID(t, testSource)
	var extraKey xdr.Uint256
	for i := range extraKey {
		extraKey[i] = 0xCD
	}
	extra, err := xdr.NewSignerKey(xdr.SignerKeyTypeSignerKeyTypeEd25519, extraKey)
	if err != nil {
		t.Fatalf("NewSignerKey() error = %v", err)
	}

	tests := []struct {
		name       string
		thresholds xdr.Thresholds
		signers    []xdr.Signer
		wants      []string
		unwanted   []string
	}{
		{
			name:       "single-sig",
			thresholds: xdr.Thresholds{3, 1, 2, 3},
			wants: []string{
				"low=1 med=2 high=3 master=3",
				"signers meet medium",
				"signers meet high",
			},
		},
		{
			name:       "2-of-3 multisig",
			thresholds: xdr.Thresholds{1, 1, 2, 3},
			signers: []xdr.Signer{
				{Key: extra, Weight: 1},
				{Key: extra, Weight: 1},
				{Key: extra, Weight: 1},
			},
			wants: []string{
				"low=1 med=2 high=3 master=1",
				"signers meet medium",
				"signers meet high",
			},
		},
		{
			name:       "locked master with two co-signers",
			thresholds: xdr.Thresholds{0, 1, 2, 3},
			signers: []xdr.Signer{
				{Key: extra, Weight: 1},
				{Key: extra, Weight: 1},
			},
			wants: []string{
				"master=0",
				"master locked",
				"signers meet medium",
				"signers cannot meet high (need 3, have 2)",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			entry := xdr.AccountEntry{
				AccountId:  aid,
				Balance:    10000000000,
				Thresholds: tc.thresholds,
				Signers:    tc.signers,
			}
			v, err := lens.DecodeAs(mustMarshal(t, entry), "AccountEntry")
			if err != nil {
				t.Fatalf("DecodeAs() error = %v", err)
			}
			thresholds := v.Node.Find("Thresholds")
			if thresholds == nil {
				t.Fatal("Thresholds node missing")
			}
			for _, want := range tc.wants {
				if !strings.Contains(thresholds.Note, want) {
					t.Errorf("Thresholds Note = %q, want it to contain %q", thresholds.Note, want)
				}
			}
			for _, bad := range tc.unwanted {
				if strings.Contains(thresholds.Note, bad) {
					t.Errorf("Thresholds Note = %q, must not contain %q", thresholds.Note, bad)
				}
			}
		})
	}
}

func TestDecodeAccountEntryExtV1(t *testing.T) {
	t.Parallel()

	v, err := lens.DecodeAs(loadFixture(t, "account_entry_ext_v1"), "AccountEntry")
	if err != nil {
		t.Fatalf("DecodeAs() error = %v", err)
	}
	raw, ok := v.Raw.(*xdr.AccountEntry)
	if !ok {
		t.Fatalf("Raw is %T, want *xdr.AccountEntry", v.Raw)
	}
	if raw.Ext.V != 1 || raw.Ext.V1 == nil {
		t.Fatalf("Ext = %+v, want the v1 extension", raw.Ext)
	}

	// Liabilities are stroop amounts just like a balance, and the amount
	// formatting is what makes them readable.
	buying := v.Node.Find("Ext.V1.Liabilities.Buying")
	if buying == nil || buying.Note != "150.0000000 (stroops: 1500000000)" {
		t.Errorf("Liabilities.Buying = %v, want an amount note", buying)
	}
	selling := v.Node.Find("Ext.V1.Liabilities.Selling")
	if selling == nil || selling.Note != "25.0000000 (stroops: 250000000)" {
		t.Errorf("Liabilities.Selling = %v, want an amount note", selling)
	}

	// The discriminant itself names the capability the extension adds.
	if extV := v.Node.Find("Ext.V"); extV == nil || extV.Note != "liabilities" {
		t.Errorf("Ext.V = %v, want a liabilities note", extV)
	}
}

func TestDecodeAccountEntryExtV2(t *testing.T) {
	t.Parallel()

	v, err := lens.DecodeAs(loadFixture(t, "account_entry_ext_v2"), "AccountEntry")
	if err != nil {
		t.Fatalf("DecodeAs() error = %v", err)
	}
	raw, ok := v.Raw.(*xdr.AccountEntry)
	if !ok {
		t.Fatalf("Raw is %T, want *xdr.AccountEntry", v.Raw)
	}
	if raw.Ext.V1 == nil || raw.Ext.V1.Ext.V != 2 || raw.Ext.V1.Ext.V2 == nil {
		t.Fatalf("Ext = %+v, want the v2 extension", raw.Ext)
	}

	sponsored := v.Node.Find("Ext.V1.Ext.V2.NumSponsored")
	if sponsored == nil || sponsored.Note != "1 ledger entry sponsored by another account" {
		t.Errorf("NumSponsored = %v, want a sponsored note", sponsored)
	}
	sponsoring := v.Node.Find("Ext.V1.Ext.V2.NumSponsoring")
	if sponsoring == nil || sponsoring.Note != "sponsoring 2 ledger entries" {
		t.Errorf("NumSponsoring = %v, want a sponsoring note", sponsoring)
	}

	// The sponsor list runs parallel to the signers: a nil entry means that
	// signer pays its own reserve, an address names the sponsor.
	ids := v.Node.Find("Ext.V1.Ext.V2.SignerSponsoringIDs")
	if ids == nil {
		t.Fatal("SignerSponsoringIDs node is missing")
	}
	if len(ids.Children) != 2 {
		t.Fatalf("SignerSponsoringIDs has %d entries, want 2", len(ids.Children))
	}
	if ids.Children[0].Value != nil {
		t.Errorf("SignerSponsoringIDs[0] = %v, want nil for an unsponsored signer", ids.Children[0].Value)
	}
	if ids.Children[1].Value != "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN" {
		t.Errorf("SignerSponsoringIDs[1] = %v, want the sponsor address", ids.Children[1].Value)
	}

	if extV := v.Node.Find("Ext.V1.Ext.V"); extV == nil || extV.Note != "sponsorship counters" {
		t.Errorf("Ext.V1.Ext.V = %v, want a sponsorship counters note", extV)
	}
}

func TestDecodeAccountEntryExtV3(t *testing.T) {
	t.Parallel()

	v, err := lens.DecodeAs(loadFixture(t, "account_entry_ext_v3"), "AccountEntry")
	if err != nil {
		t.Fatalf("DecodeAs() error = %v", err)
	}
	raw, ok := v.Raw.(*xdr.AccountEntry)
	if !ok {
		t.Fatalf("Raw is %T, want *xdr.AccountEntry", v.Raw)
	}
	v2 := raw.Ext.V1.Ext.V2
	if v2 == nil || v2.Ext.V != 3 || v2.Ext.V3 == nil {
		t.Fatalf("Ext = %+v, want the v3 extension", raw.Ext)
	}
	if v2.Ext.V3.SeqLedger != 55278432 || v2.Ext.V3.SeqTime != 1780000000 {
		t.Errorf("V3 = %+v, want ledger 55278432 at time 1780000000", v2.Ext.V3)
	}

	// A ledger number and a Unix timestamp are both bare integers on the
	// wire; the notes are what distinguish them.
	seq := v.Node.Find("Ext.V1.Ext.V2.Ext.V3.SeqLedger")
	if seq == nil || seq.Note != "seqNum set in ledger 55278432" {
		t.Errorf("SeqLedger = %v, want a seq-num ledger note", seq)
	}
	closeTime := v.Node.Find("Ext.V1.Ext.V2.Ext.V3.SeqTime")
	if closeTime == nil || closeTime.Note != "2026-05-28T20:26:40Z" {
		t.Errorf("SeqTime = %v, want an RFC 3339 note", closeTime)
	}

	if extV := v.Node.Find("Ext.V1.Ext.V2.Ext.V"); extV == nil || extV.Note != "sequence ledger and time" {
		t.Errorf("Ext.V2.Ext.V = %v, want a sequence note", extV)
	}

	// Zero sponsorship counters are the unremarkable case and stay bare.
	if ns := v.Node.Find("Ext.V1.Ext.V2.NumSponsored"); ns == nil || ns.Note != "" {
		t.Errorf("NumSponsored = %v, want no note for zero", ns)
	}
}

func TestAccountEntryExtensionsRejectTruncatedInput(t *testing.T) {
	t.Parallel()

	tests := []string{"account_entry_ext_v1", "account_entry_ext_v2", "account_entry_ext_v3"}
	for _, fixture := range tests {
		t.Run(fixture, func(t *testing.T) {
			t.Parallel()

			if _, err := lens.DecodeAs(loadFixtureRaw(fixture)[:32], "AccountEntry"); err == nil {
				t.Errorf("DecodeAs(%s truncated) error = nil, want an error", fixture)
			}
		})
	}
}
