package lens

import (
	"fmt"
	"strings"

	"github.com/stellar/go-stellar-sdk/strkey"
	"github.com/stellar/go-stellar-sdk/xdr"
)

// nativeAssetName is the SEP-11 canonical spelling of the native asset.
const nativeAssetName = "native"

// Asset code lengths accepted by the protocol. A code of up to four characters
// must use the alphanum4 arm and a longer one the alphanum12 arm, which is what
// makes the canonical string form unambiguous.
const (
	minAlphanum4CodeLen  = 1
	maxAlphanum4CodeLen  = 4
	minAlphanum12CodeLen = 5
	maxAlphanum12CodeLen = 12
)

// ParseAsset converts a SEP-11 canonical asset string into an xdr.Asset. It
// accepts "native" and "CODE:ISSUER", where CODE is 1-12 ASCII letters or digits
// and ISSUER is a G... strkey account. The XDR arm follows from the code length.
//
// Parsing is strict, so a value that is accepted always renders back to the
// same string through FormatAsset: surrounding whitespace, other spellings of
// the native asset and muxed issuers are rejected. Every failure wraps
// ErrMalformedAsset. No network access is involved.
func ParseAsset(s string) (xdr.Asset, error) {
	if s == nativeAssetName {
		return xdr.Asset{Type: xdr.AssetTypeAssetTypeNative}, nil
	}

	code, issuer, found := strings.Cut(s, ":")
	if !found {
		return xdr.Asset{}, fmt.Errorf("%w: %q is neither %q nor CODE:ISSUER", ErrMalformedAsset, s, nativeAssetName)
	}
	if !validAssetCode(code) {
		return xdr.Asset{}, fmt.Errorf("%w: asset code %q must be 1-%d ASCII letters or digits", ErrMalformedAsset, code, maxAlphanum12CodeLen)
	}
	raw, err := strkey.Decode(strkey.VersionByteAccountID, issuer)
	if err != nil {
		return xdr.Asset{}, fmt.Errorf("%w: invalid issuer %q: %w", ErrMalformedAsset, issuer, err)
	}

	var key xdr.Uint256
	copy(key[:], raw)
	account := xdr.AccountId{Type: xdr.PublicKeyTypePublicKeyTypeEd25519, Ed25519: &key}

	if len(code) <= maxAlphanum4CodeLen {
		credit := xdr.AlphaNum4{Issuer: account}
		copy(credit.AssetCode[:], code)
		return xdr.Asset{Type: xdr.AssetTypeAssetTypeCreditAlphanum4, AlphaNum4: &credit}, nil
	}
	credit := xdr.AlphaNum12{Issuer: account}
	copy(credit.AssetCode[:], code)
	return xdr.Asset{Type: xdr.AssetTypeAssetTypeCreditAlphanum12, AlphaNum12: &credit}, nil
}

// FormatAsset renders an asset in its SEP-11 canonical form: "native" or
// "CODE:ISSUER". It is the inverse of ParseAsset.
//
// A value with no canonical form renders as the empty string instead of
// panicking, because decoded XDR is untrusted. That covers a missing union arm,
// an unknown asset type, an invalid code, and a code stored in the wrong arm.
func FormatAsset(a xdr.Asset) string {
	if a.Type == xdr.AssetTypeAssetTypeNative {
		return nativeAssetName
	}
	code, issuer, ok := creditAssetParts(a)
	if !ok {
		return ""
	}
	return code + ":" + issuer
}

// creditAssetParts returns the code and issuer strkey of a well-formed credit
// asset. It reports false for the native asset and for any malformed value.
func creditAssetParts(a xdr.Asset) (code, issuer string, ok bool) {
	var account xdr.AccountId
	switch a.Type {
	case xdr.AssetTypeAssetTypeCreditAlphanum4:
		if a.AlphaNum4 == nil {
			return "", "", false
		}
		code = strings.TrimRight(string(a.AlphaNum4.AssetCode[:]), "\x00")
		if len(code) < minAlphanum4CodeLen {
			return "", "", false
		}
		account = a.AlphaNum4.Issuer
	case xdr.AssetTypeAssetTypeCreditAlphanum12:
		if a.AlphaNum12 == nil {
			return "", "", false
		}
		code = strings.TrimRight(string(a.AlphaNum12.AssetCode[:]), "\x00")
		if len(code) < minAlphanum12CodeLen {
			return "", "", false
		}
		account = a.AlphaNum12.Issuer
	default:
		return "", "", false
	}

	if !validAssetCode(code) || account.Type != xdr.PublicKeyTypePublicKeyTypeEd25519 {
		return "", "", false
	}
	issuer = accountKeyAddress(account.Ed25519)
	return code, issuer, issuer != ""
}

// validAssetCode reports whether code is 1-12 ASCII letters or digits. An
// interior NUL fails here too, since only trailing padding is trimmed.
func validAssetCode(code string) bool {
	if len(code) < minAlphanum4CodeLen || len(code) > maxAlphanum12CodeLen {
		return false
	}
	for i := range len(code) {
		c := code[i]
		if (c < '0' || c > '9') && (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') {
			return false
		}
	}
	return true
}

// accountKeyAddress renders an ed25519 key as a G... strkey, or "" when the key
// is absent.
func accountKeyAddress(key *xdr.Uint256) string {
	if key == nil {
		return ""
	}
	addr, err := strkey.Encode(strkey.VersionByteAccountID, key[:])
	if err != nil {
		return ""
	}
	return addr
}
