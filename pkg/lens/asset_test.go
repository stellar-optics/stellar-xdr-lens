package lens_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/strkey"
	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

const assetTestIssuer = "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN"

func TestParseAssetRoundTrips(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		wantType xdr.AssetType
		wantCode string
	}{
		{name: "native", input: "native", wantType: xdr.AssetTypeAssetTypeNative},
		{name: "one character code", input: "X:" + assetTestIssuer, wantType: xdr.AssetTypeAssetTypeCreditAlphanum4, wantCode: "X"},
		{name: "four character code", input: "USDC:" + assetTestIssuer, wantType: xdr.AssetTypeAssetTypeCreditAlphanum4, wantCode: "USDC"},
		{name: "five character code", input: "USDCX:" + assetTestIssuer, wantType: xdr.AssetTypeAssetTypeCreditAlphanum12, wantCode: "USDCX"},
		{name: "twelve character code", input: "ABCDEFGHIJ12:" + assetTestIssuer, wantType: xdr.AssetTypeAssetTypeCreditAlphanum12, wantCode: "ABCDEFGHIJ12"},
		{name: "code case is preserved", input: "uSdC:" + assetTestIssuer, wantType: xdr.AssetTypeAssetTypeCreditAlphanum4, wantCode: "uSdC"},
		{name: "code spelled native", input: "native:" + assetTestIssuer, wantType: xdr.AssetTypeAssetTypeCreditAlphanum12, wantCode: "native"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			asset, err := lens.ParseAsset(tc.input)
			if err != nil {
				t.Fatalf("ParseAsset(%q) error = %v", tc.input, err)
			}
			if asset.Type != tc.wantType {
				t.Errorf("Type = %v, want %v", asset.Type, tc.wantType)
			}
			if got := lens.FormatAsset(asset); got != tc.input {
				t.Errorf("FormatAsset() = %q, want %q", got, tc.input)
			}
			if got := asset.StringCanonical(); got != tc.input {
				t.Errorf("StringCanonical() = %q, want %q", got, tc.input)
			}
			// GetCode keeps the XDR padding of a code shorter than its arm.
			if got := strings.TrimRight(asset.GetCode(), "\x00"); tc.wantCode != "" && got != tc.wantCode {
				t.Errorf("GetCode() = %q, want %q", got, tc.wantCode)
			}
		})
	}
}

// TestParseAssetMatchesCanonicalFixtures checks the helpers against the
// checked-in asset XDR, so they agree with real encoded values.
func TestParseAssetMatchesCanonicalFixtures(t *testing.T) {
	t.Parallel()

	for _, fixture := range []string{"asset_native", "asset_usdc"} {
		t.Run(fixture, func(t *testing.T) {
			t.Parallel()

			payload := loadFixture(t, fixture)
			v, err := lens.DecodeAs(payload, "Asset")
			if err != nil {
				t.Fatalf("DecodeAs() error = %v", err)
			}
			decoded, ok := v.Raw.(*xdr.Asset)
			if !ok {
				t.Fatalf("Raw = %T, want *xdr.Asset", v.Raw)
			}

			canonical := lens.FormatAsset(*decoded)
			parsed, err := lens.ParseAsset(canonical)
			if err != nil {
				t.Fatalf("ParseAsset(%q) error = %v", canonical, err)
			}
			encoded, err := xdr.MarshalBase64(parsed)
			if err != nil {
				t.Fatalf("MarshalBase64() error = %v", err)
			}
			if encoded != payload {
				t.Errorf("re-encoded asset = %q, want fixture %q", encoded, payload)
			}
		})
	}
}

func TestParseAssetRejectsMalformedInput(t *testing.T) {
	t.Parallel()

	muxed, err := strkey.Encode(strkey.VersionByteMuxedAccount, make([]byte, 40))
	if err != nil {
		t.Fatalf("encoding muxed address: %v", err)
	}
	contract, err := strkey.Encode(strkey.VersionByteContract, make([]byte, 32))
	if err != nil {
		t.Fatalf("encoding contract address: %v", err)
	}
	corrupted := assetTestIssuer[:len(assetTestIssuer)-1] + "A"

	tests := []struct {
		name  string
		input string
	}{
		{name: "empty", input: ""},
		{name: "upper case native", input: "NATIVE"},
		{name: "xlm alias", input: "XLM"},
		{name: "leading space", input: " native"},
		{name: "trailing newline", input: "native\n"},
		{name: "code without issuer", input: "USDC"},
		{name: "empty issuer", input: "USDC:"},
		{name: "empty code", input: ":" + assetTestIssuer},
		{name: "code too long", input: "ABCDEFGHIJKLM:" + assetTestIssuer},
		{name: "code with punctuation", input: "US-C:" + assetTestIssuer},
		{name: "code with non-ascii", input: "USDÇ:" + assetTestIssuer},
		{name: "code with interior nul", input: "US\x00C:" + assetTestIssuer},
		{name: "extra separator", input: "USDC:" + assetTestIssuer + ":extra"},
		{name: "corrupted issuer checksum", input: "USDC:" + corrupted},
		{name: "lower case issuer", input: "USDC:" + strings.ToLower(assetTestIssuer)},
		{name: "muxed issuer", input: "USDC:" + muxed},
		{name: "contract issuer", input: "USDC:" + contract},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if _, err := lens.ParseAsset(tc.input); !errors.Is(err, lens.ErrMalformedAsset) {
				t.Errorf("ParseAsset(%q) error = %v, want ErrMalformedAsset", tc.input, err)
			}
		})
	}
}

// TestFormatAssetDegradesOnMalformedValues covers values decoded XDR can carry
// but that have no canonical string. None of them may panic.
func TestFormatAssetDegradesOnMalformedValues(t *testing.T) {
	t.Parallel()

	valid, err := lens.ParseAsset("USDC:" + assetTestIssuer)
	if err != nil {
		t.Fatalf("ParseAsset() error = %v", err)
	}
	issuer := valid.AlphaNum4.Issuer

	tests := []struct {
		name  string
		asset xdr.Asset
	}{
		{name: "alphanum4 without arm", asset: xdr.Asset{Type: xdr.AssetTypeAssetTypeCreditAlphanum4}},
		{name: "alphanum12 without arm", asset: xdr.Asset{Type: xdr.AssetTypeAssetTypeCreditAlphanum12}},
		{name: "pool share type", asset: xdr.Asset{Type: xdr.AssetTypeAssetTypePoolShare}},
		{name: "unknown type", asset: xdr.Asset{Type: xdr.AssetType(99)}},
		{name: "empty alphanum4 code", asset: xdr.Asset{
			Type:      xdr.AssetTypeAssetTypeCreditAlphanum4,
			AlphaNum4: &xdr.AlphaNum4{Issuer: issuer},
		}},
		{name: "short code in alphanum12 arm", asset: xdr.Asset{
			Type:       xdr.AssetTypeAssetTypeCreditAlphanum12,
			AlphaNum12: &xdr.AlphaNum12{AssetCode: xdr.AssetCode12{'U', 'S', 'D', 'C'}, Issuer: issuer},
		}},
		{name: "interior nul", asset: xdr.Asset{
			Type:      xdr.AssetTypeAssetTypeCreditAlphanum4,
			AlphaNum4: &xdr.AlphaNum4{AssetCode: xdr.AssetCode4{'U', 0, 'D', 'C'}, Issuer: issuer},
		}},
		{name: "punctuation in code", asset: xdr.Asset{
			Type:      xdr.AssetTypeAssetTypeCreditAlphanum4,
			AlphaNum4: &xdr.AlphaNum4{AssetCode: xdr.AssetCode4{'U', '-', 'D', 'C'}, Issuer: issuer},
		}},
		{name: "issuer without key", asset: xdr.Asset{
			Type:      xdr.AssetTypeAssetTypeCreditAlphanum4,
			AlphaNum4: &xdr.AlphaNum4{AssetCode: xdr.AssetCode4{'U', 'S', 'D', 'C'}},
		}},
		{name: "issuer with unknown key type", asset: xdr.Asset{
			Type: xdr.AssetTypeAssetTypeCreditAlphanum4,
			AlphaNum4: &xdr.AlphaNum4{
				AssetCode: xdr.AssetCode4{'U', 'S', 'D', 'C'},
				Issuer:    xdr.AccountId{Type: xdr.PublicKeyType(7), Ed25519: issuer.Ed25519},
			},
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := lens.FormatAsset(tc.asset); got != "" {
				t.Errorf("FormatAsset() = %q, want empty string", got)
			}
		})
	}
}
