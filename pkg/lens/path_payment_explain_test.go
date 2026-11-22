package lens_test

import (
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

func makeTestAsset(code string) xdr.Asset {
	if code == "XLM" || code == "native" {
		return xdr.Asset{Type: xdr.AssetTypeAssetTypeNative}
	}
	var aCode [4]byte
	copy(aCode[:], []byte(code))
	issuer := xdr.MustAddress("GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN")
	return xdr.Asset{
		Type: xdr.AssetTypeAssetTypeCreditAlphanum4,
		AlphaNum4: &xdr.AlphaNum4{
			AssetCode: aCode,
			Issuer:    issuer,
		},
	}
}

func TestDiagnosePathPaymentInefficiency(t *testing.T) {
	t.Parallel()

	xlm := makeTestAsset("XLM")
	usdc := makeTestAsset("USDC")
	eur := makeTestAsset("EUR")

	t.Run("clean linear path has no inefficiencies", func(t *testing.T) {
		diag := lens.DiagnosePathPaymentInefficiency(xlm, []xdr.Asset{usdc}, eur)
		if diag.IsCircular || diag.IsZeroHop || len(diag.Warnings) > 0 {
			t.Errorf("expected clean path, got %+v", diag)
		}
	})

	t.Run("zero-hop identical assets", func(t *testing.T) {
		diag := lens.DiagnosePathPaymentInefficiency(xlm, nil, xlm)
		if !diag.IsZeroHop {
			t.Errorf("expected IsZeroHop to be true, got false")
		}
		if len(diag.Warnings) == 0 || !strings.Contains(diag.Warnings[0], "identical") {
			t.Errorf("expected warning about identical assets with empty path, got %+v", diag.Warnings)
		}
	})

	t.Run("circular loop in path XLM -> USDC -> XLM -> EUR", func(t *testing.T) {
		diag := lens.DiagnosePathPaymentInefficiency(xlm, []xdr.Asset{usdc, xlm}, eur)
		if !diag.IsCircular {
			t.Errorf("expected IsCircular to be true, got false")
		}
		if len(diag.CircularAssets) != 1 || diag.CircularAssets[0] != "XLM" {
			t.Errorf("expected circular asset XLM, got %v", diag.CircularAssets)
		}
		expectedMsg := "Inefficient path detected: asset XLM appears multiple times, creating a circular swap loop."
		found := false
		for _, w := range diag.Warnings {
			if w == expectedMsg {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected warning %q, got %v", expectedMsg, diag.Warnings)
		}
	})

	t.Run("arbitrage path XLM -> USDC -> XLM", func(t *testing.T) {
		diag := lens.DiagnosePathPaymentInefficiency(xlm, []xdr.Asset{usdc}, xlm)
		if !diag.IsCircular {
			t.Errorf("expected circular detection on arbitrage path, got false")
		}
	})

	t.Run("multiple circular assets", func(t *testing.T) {
		// XLM -> USDC -> EUR -> USDC -> XLM
		diag := lens.DiagnosePathPaymentInefficiency(xlm, []xdr.Asset{usdc, eur, usdc}, xlm)
		if !diag.IsCircular {
			t.Errorf("expected circular detection, got false")
		}
		if len(diag.CircularAssets) != 2 {
			t.Errorf("expected 2 circular assets (USDC, XLM), got %v", diag.CircularAssets)
		}
	})
}
