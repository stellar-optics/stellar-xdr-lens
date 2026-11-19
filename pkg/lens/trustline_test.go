package lens_test

import (
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/amount"
	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

func TestTrustLineEntrySimpleDecoding(t *testing.T) {
	t.Parallel()

	v, err := lens.DecodeAs(loadFixture(t, "trustline_entry_simple"), "TrustLineEntry")
	if err != nil {
		t.Fatalf("DecodeAs() error = %v", err)
	}
	if v.Type != "TrustLineEntry" {
		t.Errorf("Type = %q, want TrustLineEntry", v.Type)
	}

	accountID := v.Node.Find("AccountId")
	if accountID == nil {
		t.Fatal("AccountId not found")
	}
	if !strings.HasPrefix(accountID.Value.(string), "GCJN") {
		t.Errorf("AccountId = %q, want GCJN...", accountID.Value)
	}

	asset := v.Node.Find("Asset")
	if asset == nil {
		t.Fatal("Asset not found")
	}
	if !strings.Contains(asset.Value.(string), "USDC") {
		t.Errorf("Asset Value = %q, want to contain USDC", asset.Value)
	}

	balance := v.Node.Find("Balance")
	if balance == nil {
		t.Fatal("Balance not found")
	}
	if balance.Value != int64(1000*amount.One) {
		t.Errorf("Balance Value = %v, want %d", balance.Value, 1000*amount.One)
	}
	if !strings.Contains(balance.Note, "1000.0000000") {
		t.Errorf("Balance Note = %q, want formatted", balance.Note)
	}

	limit := v.Node.Find("Limit")
	if limit == nil {
		t.Fatal("Limit not found")
	}
	if limit.Value != int64(5000*amount.One) {
		t.Errorf("Limit Value = %v, want %d", limit.Value, 5000*amount.One)
	}

	flags := v.Node.Find("Flags")
	if flags == nil {
		t.Fatal("Flags not found")
	}
	if flags.Value != uint64(xdr.TrustLineFlagsAuthorizedFlag) {
		t.Errorf("Flags Value = %v, want %d", flags.Value, xdr.TrustLineFlagsAuthorizedFlag)
	}
	if flags.Note != "authorized" {
		t.Errorf("Flags Note = %q, want %q", flags.Note, "authorized")
	}

	ext := v.Node.Find("Ext")
	if ext == nil {
		t.Fatal("Ext not found")
	}
	vNode := ext.Child("V")
	if vNode == nil || vNode.Value != int64(0) {
		t.Errorf("Ext.V = %v, want 0", vNode.Value)
	}
}

func TestTrustLineEntryV1Decoding(t *testing.T) {
	t.Parallel()

	v, err := lens.DecodeAs(loadFixture(t, "trustline_entry_v1"), "TrustLineEntry")
	if err != nil {
		t.Fatalf("DecodeAs() error = %v", err)
	}
	if v.Type != "TrustLineEntry" {
		t.Errorf("Type = %q, want TrustLineEntry", v.Type)
	}

	ext := v.Node.Find("Ext")
	if ext == nil {
		t.Fatal("Ext not found")
	}
	vNode := ext.Child("V")
	if vNode == nil || vNode.Value != int64(1) {
		t.Errorf("Ext.V = %v, want 1", vNode.Value)
	}

	v1 := ext.Child("V1")
	if v1 == nil {
		t.Fatal("V1 not found")
	}

	liabilities := v1.Child("Liabilities")
	if liabilities == nil {
		t.Fatal("Liabilities not found")
	}

	buying := liabilities.Child("Buying")
	if buying == nil {
		t.Fatal("Buying not found")
	}
	if buying.Value != int64(100*amount.One) {
		t.Errorf("Buying Value = %v, want %d", buying.Value, 100*amount.One)
	}
	if !strings.Contains(buying.Note, "100.0000000") {
		t.Errorf("Buying Note = %q, want formatted", buying.Note)
	}

	selling := liabilities.Child("Selling")
	if selling == nil {
		t.Fatal("Selling not found")
	}
	if selling.Value != int64(200*amount.One) {
		t.Errorf("Selling Value = %v, want %d", selling.Value, 200*amount.One)
	}
	if !strings.Contains(selling.Note, "200.0000000") {
		t.Errorf("Selling Note = %q, want formatted", selling.Note)
	}

	v1Ext := v1.Child("Ext")
	if v1Ext == nil {
		t.Fatal("V1 Ext not found")
	}
	v1V := v1Ext.Child("V")
	if v1V == nil || v1V.Value != int64(0) {
		t.Errorf("V1 Ext.V = %v, want 0", v1V.Value)
	}
}

func TestTrustLineEntryV2Decoding(t *testing.T) {
	t.Parallel()

	v, err := lens.DecodeAs(loadFixture(t, "trustline_entry_v2"), "TrustLineEntry")
	if err != nil {
		t.Fatalf("DecodeAs() error = %v", err)
	}
	if v.Type != "TrustLineEntry" {
		t.Errorf("Type = %q, want TrustLineEntry", v.Type)
	}

	// Verify asset is PoolShare
	asset := v.Node.Find("Asset")
	if asset == nil {
		t.Fatal("Asset not found")
	}
	assetType := asset.Child("Type")
	if assetType == nil {
		t.Fatal("Asset.Type not found")
	}
	if assetType.Value != "AssetTypeAssetTypePoolShare" {
		t.Errorf("Asset.Type = %q, want AssetTypeAssetTypePoolShare", assetType.Value)
	}

	ext := v.Node.Find("Ext")
	if ext == nil {
		t.Fatal("Ext not found")
	}

	v1 := ext.Child("V1")
	if v1 == nil {
		t.Fatal("V1 not found")
	}

	v1Ext := v1.Child("Ext")
	if v1Ext == nil {
		t.Fatal("V1 Ext not found")
	}

	v2 := v1Ext.Child("V2")
	if v2 == nil {
		t.Fatal("V2 not found")
	}

	poolUseCount := v2.Child("LiquidityPoolUseCount")
	if poolUseCount == nil {
		t.Fatal("LiquidityPoolUseCount not found")
	}
	if poolUseCount.Value != int64(5) {
		t.Errorf("LiquidityPoolUseCount Value = %v, want 5", poolUseCount.Value)
	}
}

func TestTrustLineEntryDetection(t *testing.T) {
	t.Parallel()

	for _, fixture := range []string{"trustline_entry_simple", "trustline_entry_v1", "trustline_entry_v2"} {
		t.Run(fixture, func(t *testing.T) {
			t.Parallel()
			cands, err := lens.Detect(loadFixture(t, fixture))
			if err != nil {
				t.Fatalf("Detect() error = %v", err)
			}
			found := false
			for _, c := range cands {
				if c.Type == "TrustLineEntry" {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Detect() did not find TrustLineEntry among candidates: %v", cands)
			}
		})
	}
}

func TestTrustLineEntryAmountFieldsFormatted(t *testing.T) {
	t.Parallel()

	v, err := lens.DecodeAs(loadFixture(t, "trustline_entry_v1"), "TrustLineEntry")
	if err != nil {
		t.Fatalf("DecodeAs() error = %v", err)
	}

	amountFields := []string{"Balance", "Limit", "Buying", "Selling"}
	for _, field := range amountFields {
		var node *lens.Node
		if field == "Buying" || field == "Selling" {
			node = v.Node.Find("Ext.V1.Liabilities." + field)
		} else {
			node = v.Node.Find(field)
		}
		if node == nil {
			t.Errorf("Field %s not found", field)
			continue
		}
		if node.Note == "" {
			t.Errorf("Field %s has no formatted note", field)
		}
		if !strings.Contains(node.Note, "stroops") {
			t.Errorf("Field %s note %q does not contain stroops", field, node.Note)
		}
	}
}

func TestTrustLineFlagsRendering(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		flags    xdr.Uint32
		wantNote string
	}{
		{
			name:     "authorized",
			flags:    xdr.Uint32(xdr.TrustLineFlagsAuthorizedFlag),
			wantNote: "authorized",
		},
		{
			name:     "authorized to maintain liabilities",
			flags:    xdr.Uint32(xdr.TrustLineFlagsAuthorizedToMaintainLiabilitiesFlag),
			wantNote: "authorized_to_maintain_liabilities",
		},
		{
			name:     "trustline clawback enabled",
			flags:    xdr.Uint32(xdr.TrustLineFlagsTrustlineClawbackEnabledFlag),
			wantNote: "trustline_clawback_enabled",
		},
		{
			name: "all flags",
			flags: xdr.Uint32(xdr.TrustLineFlagsAuthorizedFlag |
				xdr.TrustLineFlagsAuthorizedToMaintainLiabilitiesFlag |
				xdr.TrustLineFlagsTrustlineClawbackEnabledFlag),
			wantNote: "authorized | authorized_to_maintain_liabilities | trustline_clawback_enabled",
		},
		{
			name:     "no flags",
			flags:    0,
			wantNote: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			entry := xdr.TrustLineEntry{
				AccountId: mustAccountID(t, testSource),
				Asset:     creditAsset(t, "USDC", testIssuer).ToTrustLineAsset(),
				Balance:   xdr.Int64(amount.One),
				Limit:     xdr.Int64(100 * amount.One),
				Flags:     tc.flags,
				Ext:       xdr.TrustLineEntryExt{V: 0},
			}
			v, err := lens.DecodeAs(mustMarshal(t, entry), "TrustLineEntry")
			if err != nil {
				t.Fatalf("DecodeAs() error = %v", err)
			}

			flags := v.Node.Find("Flags")
			if flags == nil {
				t.Fatal("Flags not found")
			}
			if flags.Value != uint64(tc.flags) {
				t.Errorf("Flags Value = %v, want %d", flags.Value, tc.flags)
			}
			if flags.Note != tc.wantNote {
				t.Errorf("Flags Note = %q, want %q", flags.Note, tc.wantNote)
			}
		})
	}
}

func TestDiagnoseChangeTrustFailure(t *testing.T) {
	t.Parallel()

	const issuer = "GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAW"
	usdc := xdr.MustNewCreditAsset("USDC", issuer).ToChangeTrustAsset()
	pool := xdr.ChangeTrustAsset{Type: xdr.AssetTypeAssetTypePoolShare}
	label := "USDC:" + issuer

	tests := []struct {
		name string
		op   *xdr.ChangeTrustOp
		code xdr.ChangeTrustResultCode
		want string
	}{
		{
			name: "removal with liabilities names offers and the remaining balance",
			op:   &xdr.ChangeTrustOp{Line: usdc, Limit: 0},
			code: xdr.ChangeTrustResultCodeChangeTrustInvalidLimit,
			want: "Setting the " + label + " limit to 0 asks to remove the trustline, which is only possible once its balance and buying liabilities are zero; a trustline that does not exist cannot be removed either. " +
				"Cancel open DEX offers for " + label + " to release their liabilities and send any remaining balance away (for example back to the issuer), then retry with limit 0. Claimable balances already created hold no liabilities on this trustline, so they do not block removal.",
		},
		{
			name: "reduced limit states the minimum",
			op:   &xdr.ChangeTrustOp{Line: usdc, Limit: 250000000},
			code: xdr.ChangeTrustResultCodeChangeTrustInvalidLimit,
			want: "The new " + label + " limit of 25.0000000 is below the trustline's minimum: its current balance plus the buying liabilities of open DEX offers. " +
				"Set the limit to at least the balance plus buying liabilities, or cancel open buy offers and reduce the balance before lowering it.",
		},
		{
			name: "low reserve computes one base reserve",
			op:   &xdr.ChangeTrustOp{Line: usdc, Limit: 250000000},
			code: xdr.ChangeTrustResultCodeChangeTrustLowReserve,
			want: "Creating the " + label + " trustline adds one subentry, raising the account's minimum balance by 5000000 stroops (0.5 XLM) at the 0.5 XLM base reserve, and its available XLM cannot cover that. " +
				"Make at least 5000000 stroops (0.5 XLM) available above the current minimum balance, free reserve by removing an unused offer, trustline, data entry or signer, or have another account sponsor the trustline.",
		},
		{
			name: "low reserve counts a pool share trustline twice",
			op:   &xdr.ChangeTrustOp{Line: pool, Limit: 250000000},
			code: xdr.ChangeTrustResultCodeChangeTrustLowReserve,
			want: "Creating the liquidity pool shares trustline adds two subentries, raising the account's minimum balance by 10000000 stroops (1 XLM) at the 0.5 XLM base reserve, and its available XLM cannot cover that. " +
				"Make at least 10000000 stroops (1 XLM) available above the current minimum balance, free reserve by removing an unused offer, trustline, data entry or signer, or have another account sponsor the trustline.",
		},
		{
			name: "other failures keep the generic explanation",
			op:   &xdr.ChangeTrustOp{Line: usdc, Limit: 1},
			code: xdr.ChangeTrustResultCodeChangeTrustSelfNotAllowed,
			want: "An account cannot open a trustline to an asset it issues itself.",
		},
		{
			name: "success has nothing to diagnose",
			op:   &xdr.ChangeTrustOp{Line: usdc, Limit: 1},
			code: xdr.ChangeTrustResultCodeChangeTrustSuccess,
			want: "",
		},
		{
			name: "nil operation has nothing to diagnose",
			code: xdr.ChangeTrustResultCodeChangeTrustInvalidLimit,
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := lens.DiagnoseChangeTrustFailure(tc.op, tc.code); got != tc.want {
				t.Errorf("DiagnoseChangeTrustFailure() =\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}

// TestExplainPairDeepensChangeTrustFailures pins that the diagnosis survives
// the whole explain path rather than being replaced by a later rewrite.
func TestExplainPairDeepensChangeTrustFailures(t *testing.T) {
	t.Parallel()

	line := xdr.MustNewCreditAsset("USDC", "GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAW").ToChangeTrustAsset()

	tests := []struct {
		name  string
		limit xdr.Int64
		code  xdr.ChangeTrustResultCode
		want  []string
	}{
		{"removal", 0, xdr.ChangeTrustResultCodeChangeTrustInvalidLimit, []string{"limit to 0", "buying liabilities"}},
		{"reduction", 10000000, xdr.ChangeTrustResultCodeChangeTrustInvalidLimit, []string{"limit of 1.0000000", "buying liabilities"}},
		{"low reserve", 10000000, xdr.ChangeTrustResultCodeChangeTrustLowReserve, []string{"5000000 stroops (0.5 XLM)"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := explainSingleOperation(t,
				xdr.OperationBody{Type: xdr.OperationTypeChangeTrust, ChangeTrustOp: &xdr.ChangeTrustOp{Line: line, Limit: tc.limit}},
				xdr.OperationResultTr{Type: xdr.OperationTypeChangeTrust, ChangeTrustResult: &xdr.ChangeTrustResult{Code: tc.code}})
			assertReasonContains(t, got, false, tc.want...)
			if got.Hint == "" {
				t.Error("Hint is empty, want a remediation")
			}
		})
	}
}
