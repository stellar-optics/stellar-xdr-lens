package lens

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"

	"github.com/stellar/go-stellar-sdk/amount"
	"github.com/stellar/go-stellar-sdk/strkey"
	"github.com/stellar/go-stellar-sdk/xdr"
)

// ErrCoercionFailed is returned when a value cannot be coerced into the requested type.
var ErrCoercionFailed = errors.New("type coercion failed")

var (
	hexRegex = regexp.MustCompile(`^[0-9a-fA-F]+$`)
)

// cleanNumberString removes thousands separators, underscores, and extra whitespace.
func cleanNumberString(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "_", "")
	return s
}

// CoerceInt64 converts arbitrary numbers, stringified integers, or hex into an int64.
func CoerceInt64(v any) (int64, error) {
	if v == nil {
		return 0, fmt.Errorf("%w: cannot coerce nil to int64", ErrCoercionFailed)
	}

	switch val := v.(type) {
	case int:
		return int64(val), nil
	case int8:
		return int64(val), nil
	case int16:
		return int64(val), nil
	case int32:
		return int64(val), nil
	case int64:
		return val, nil
	case uint:
		if uint64(val) > math.MaxInt64 {
			return 0, fmt.Errorf("%w: uint exceeds max int64", ErrCoercionFailed)
		}
		return int64(val), nil
	case uint8:
		return int64(val), nil
	case uint16:
		return int64(val), nil
	case uint32:
		return int64(val), nil
	case uint64:
		if val > math.MaxInt64 {
			return 0, fmt.Errorf("%w: uint64 exceeds max int64", ErrCoercionFailed)
		}
		return int64(val), nil
	case float32:
		if float64(val) < math.MinInt64 || float64(val) > math.MaxInt64 || math.IsNaN(float64(val)) || math.IsInf(float64(val), 0) {
			return 0, fmt.Errorf("%w: float32 out of int64 range", ErrCoercionFailed)
		}
		return int64(val), nil
	case float64:
		if val < math.MinInt64 || val > math.MaxInt64 || math.IsNaN(val) || math.IsInf(val, 0) {
			return 0, fmt.Errorf("%w: float64 out of int64 range", ErrCoercionFailed)
		}
		return int64(val), nil
	case json.Number:
		if i, err := val.Int64(); err == nil {
			return i, nil
		}
		return CoerceInt64(string(val))
	case string:
		clean := cleanNumberString(val)
		if clean == "" {
			return 0, fmt.Errorf("%w: empty string for int64", ErrCoercionFailed)
		}
		// Handle hex prefix
		if strings.HasPrefix(clean, "0x") || strings.HasPrefix(clean, "0X") {
			n, err := strconv.ParseInt(clean[2:], 16, 64)
			if err != nil {
				return 0, fmt.Errorf("%w: invalid hex int64 %q: %w", ErrCoercionFailed, val, err)
			}
			return n, nil
		}
		// Try standard base 10
		n, err := strconv.ParseInt(clean, 10, 64)
		if err == nil {
			return n, nil
		}
		// Try scientific notation / float string that is whole
		if f, err := strconv.ParseFloat(clean, 64); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
			if f >= math.MinInt64 && f <= math.MaxInt64 && math.Floor(f) == f {
				return int64(f), nil
			}
		}
		return 0, fmt.Errorf("%w: cannot parse %q as int64", ErrCoercionFailed, val)
	default:
		return 0, fmt.Errorf("%w: unsupported type %T for int64", ErrCoercionFailed, v)
	}
}

// CoerceUint64 converts arbitrary numbers, stringified integers, or hex into a uint64.
func CoerceUint64(v any) (uint64, error) {
	if v == nil {
		return 0, fmt.Errorf("%w: cannot coerce nil to uint64", ErrCoercionFailed)
	}

	switch val := v.(type) {
	case uint:
		return uint64(val), nil
	case uint8:
		return uint64(val), nil
	case uint16:
		return uint64(val), nil
	case uint32:
		return uint64(val), nil
	case uint64:
		return val, nil
	case int:
		if val < 0 {
			return 0, fmt.Errorf("%w: negative int for uint64", ErrCoercionFailed)
		}
		return uint64(val), nil
	case int8:
		if val < 0 {
			return 0, fmt.Errorf("%w: negative int8 for uint64", ErrCoercionFailed)
		}
		return uint64(val), nil
	case int16:
		if val < 0 {
			return 0, fmt.Errorf("%w: negative int16 for uint64", ErrCoercionFailed)
		}
		return uint64(val), nil
	case int32:
		if val < 0 {
			return 0, fmt.Errorf("%w: negative int32 for uint64", ErrCoercionFailed)
		}
		return uint64(val), nil
	case int64:
		if val < 0 {
			return 0, fmt.Errorf("%w: negative int64 for uint64", ErrCoercionFailed)
		}
		return uint64(val), nil
	case float32:
		if val < 0 || float64(val) > math.MaxUint64 || math.IsNaN(float64(val)) || math.IsInf(float64(val), 0) {
			return 0, fmt.Errorf("%w: float32 out of uint64 range", ErrCoercionFailed)
		}
		return uint64(val), nil
	case float64:
		if val < 0 || val > math.MaxUint64 || math.IsNaN(val) || math.IsInf(val, 0) {
			return 0, fmt.Errorf("%w: float64 out of uint64 range", ErrCoercionFailed)
		}
		return uint64(val), nil
	case json.Number:
		return CoerceUint64(string(val))
	case string:
		clean := cleanNumberString(val)
		if clean == "" {
			return 0, fmt.Errorf("%w: empty string for uint64", ErrCoercionFailed)
		}
		if strings.HasPrefix(clean, "0x") || strings.HasPrefix(clean, "0X") {
			n, err := strconv.ParseUint(clean[2:], 16, 64)
			if err != nil {
				return 0, fmt.Errorf("%w: invalid hex uint64 %q: %w", ErrCoercionFailed, val, err)
			}
			return n, nil
		}
		n, err := strconv.ParseUint(clean, 10, 64)
		if err == nil {
			return n, nil
		}
		if f, err := strconv.ParseFloat(clean, 64); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
			if f >= 0 && f <= math.MaxUint64 && math.Floor(f) == f {
				return uint64(f), nil
			}
		}
		return 0, fmt.Errorf("%w: cannot parse %q as uint64", ErrCoercionFailed, val)
	default:
		return 0, fmt.Errorf("%w: unsupported type %T for uint64", ErrCoercionFailed, v)
	}
}

// CoerceInt32 converts arbitrary numbers or stringified values into an int32.
func CoerceInt32(v any) (int32, error) {
	n, err := CoerceInt64(v)
	if err != nil {
		return 0, err
	}
	if n < math.MinInt32 || n > math.MaxInt32 {
		return 0, fmt.Errorf("%w: value %d out of int32 range", ErrCoercionFailed, n)
	}
	return int32(n), nil
}

// CoerceUint32 converts arbitrary numbers or stringified values into a uint32.
func CoerceUint32(v any) (uint32, error) {
	n, err := CoerceUint64(v)
	if err != nil {
		return 0, err
	}
	if n > math.MaxUint32 {
		return 0, fmt.Errorf("%w: value %d out of uint32 range", ErrCoercionFailed, n)
	}
	return uint32(n), nil
}

// CoerceBigInt parses big integers from numbers or strings, supporting negative, hex, or base-10 strings.
func CoerceBigInt(v any) (*big.Int, error) {
	if v == nil {
		return nil, fmt.Errorf("%w: cannot coerce nil to *big.Int", ErrCoercionFailed)
	}

	switch val := v.(type) {
	case *big.Int:
		if val == nil {
			return nil, fmt.Errorf("%w: nil *big.Int", ErrCoercionFailed)
		}
		return new(big.Int).Set(val), nil
	case int, int8, int16, int32, int64:
		n, err := CoerceInt64(val)
		if err != nil {
			return nil, err
		}
		return big.NewInt(n), nil
	case uint, uint8, uint16, uint32, uint64:
		n, err := CoerceUint64(val)
		if err != nil {
			return nil, err
		}
		return new(big.Int).SetUint64(n), nil
	case json.Number:
		return CoerceBigInt(string(val))
	case string:
		clean := cleanNumberString(val)
		if clean == "" {
			return nil, fmt.Errorf("%w: empty string for *big.Int", ErrCoercionFailed)
		}
		bi := new(big.Int)
		base := 10
		if strings.HasPrefix(clean, "0x") || strings.HasPrefix(clean, "0X") {
			clean = clean[2:]
			base = 16
		} else if strings.HasPrefix(clean, "-0x") || strings.HasPrefix(clean, "-0X") {
			clean = "-" + clean[3:]
			base = 16
		}
		if _, ok := bi.SetString(clean, base); !ok {
			return nil, fmt.Errorf("%w: invalid big integer string %q", ErrCoercionFailed, val)
		}
		return bi, nil
	default:
		return nil, fmt.Errorf("%w: unsupported type %T for *big.Int", ErrCoercionFailed, v)
	}
}

// CoerceBool converts booleans, strings ("true"/"false", "1"/"0", "yes"/"no"), and numbers (1/0) into bool.
func CoerceBool(v any) (bool, error) {
	if v == nil {
		return false, fmt.Errorf("%w: cannot coerce nil to bool", ErrCoercionFailed)
	}

	switch val := v.(type) {
	case bool:
		return val, nil
	case int, int8, int16, int32, int64:
		n, err := CoerceInt64(val)
		if err != nil {
			return false, err
		}
		if n == 1 {
			return true, nil
		}
		if n == 0 {
			return false, nil
		}
		return false, fmt.Errorf("%w: integer %d not a valid boolean (expected 0 or 1)", ErrCoercionFailed, n)
	case uint, uint8, uint16, uint32, uint64:
		n, err := CoerceUint64(val)
		if err != nil {
			return false, err
		}
		if n == 1 {
			return true, nil
		}
		if n == 0 {
			return false, nil
		}
		return false, fmt.Errorf("%w: uint %d not a valid boolean (expected 0 or 1)", ErrCoercionFailed, n)
	case string:
		s := strings.TrimSpace(strings.ToLower(val))
		switch s {
		case "true", "t", "1", "yes", "y":
			return true, nil
		case "false", "f", "0", "no", "n":
			return false, nil
		default:
			return false, fmt.Errorf("%w: invalid boolean string %q", ErrCoercionFailed, val)
		}
	default:
		return false, fmt.Errorf("%w: unsupported type %T for bool", ErrCoercionFailed, v)
	}
}

// CoerceString converts string, []byte, or stringer to a string.
func CoerceString(v any) (string, error) {
	if v == nil {
		return "", fmt.Errorf("%w: cannot coerce nil to string", ErrCoercionFailed)
	}
	switch val := v.(type) {
	case string:
		return val, nil
	case []byte:
		return string(val), nil
	case json.Number:
		return string(val), nil
	case fmt.Stringer:
		return val.String(), nil
	default:
		return fmt.Sprintf("%v", val), nil
	}
}

// CoerceBytes converts bytes, hex strings ("0x...", or raw hex), base64 strings, or number slices into []byte.
func CoerceBytes(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}

	switch val := v.(type) {
	case []byte:
		cp := make([]byte, len(val))
		copy(cp, val)
		return cp, nil
	case string:
		s := strings.TrimSpace(val)
		if s == "" {
			return []byte{}, nil
		}
		// Explicit hex prefix
		if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
			hexStr := s[2:]
			if len(hexStr)%2 != 0 {
				hexStr = "0" + hexStr
			}
			b, err := hex.DecodeString(hexStr)
			if err != nil {
				return nil, fmt.Errorf("%w: invalid hex byte string: %w", ErrCoercionFailed, err)
			}
			return b, nil
		}

		// Try hex if all characters are hex and even length
		if len(s)%2 == 0 && hexRegex.MatchString(s) {
			if b, err := hex.DecodeString(s); err == nil {
				return b, nil
			}
		}

		// Try standard base64
		if b, err := base64.StdEncoding.DecodeString(s); err == nil {
			return b, nil
		}
		// Try raw URL base64
		if b, err := base64.RawURLEncoding.DecodeString(s); err == nil {
			return b, nil
		}

		// Default to raw string bytes
		return []byte(s), nil
	case []any:
		res := make([]byte, len(val))
		for i, elem := range val {
			b, err := CoerceUint32(elem)
			if err != nil || b > 255 {
				return nil, fmt.Errorf("%w: element %d in byte array out of range [0, 255]", ErrCoercionFailed, i)
			}
			res[i] = byte(b)
		}
		return res, nil
	default:
		return nil, fmt.Errorf("%w: unsupported type %T for []byte", ErrCoercionFailed, v)
	}
}

// CoerceHash coerces a 32-byte hash from hex, base64, or byte slice.
func CoerceHash(v any) ([32]byte, error) {
	var hash [32]byte
	b, err := CoerceBytes(v)
	if err != nil {
		return hash, err
	}
	if len(b) != 32 {
		return hash, fmt.Errorf("%w: expected 32 bytes for hash, got %d", ErrCoercionFailed, len(b))
	}
	copy(hash[:], b)
	return hash, nil
}

// CoerceAmount parses an amount into stroops (int64).
// Accepts integer stroops (e.g. 10000000), stringified stroops ("10000000"),
// or decimal amounts ("1.5", "1.5 XLM").
func CoerceAmount(v any) (int64, error) {
	if v == nil {
		return 0, fmt.Errorf("%w: cannot coerce nil to amount", ErrCoercionFailed)
	}

	switch val := v.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return CoerceInt64(val)
	case float32, float64:
		// Float amount, format as decimal string and parse through amount.ParseInt64
		s := fmt.Sprintf("%.7f", val)
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
		stroops, err := amount.ParseInt64(s)
		if err != nil {
			return 0, fmt.Errorf("%w: invalid float amount %v: %w", ErrCoercionFailed, v, err)
		}
		return stroops, nil
	case json.Number:
		return CoerceAmount(string(val))
	case string:
		s := cleanNumberString(val)
		// Strip currency suffix like " XLM" or " stroops"
		parts := strings.Fields(s)
		if len(parts) > 1 {
			unit := strings.ToLower(parts[1])
			if unit == "stroop" || unit == "stroops" {
				return CoerceInt64(parts[0])
			}
			s = parts[0]
		}
		// If it contains a decimal point, parse as fractional amount
		if strings.Contains(s, ".") {
			stroops, err := amount.ParseInt64(s)
			if err != nil {
				return 0, fmt.Errorf("%w: invalid decimal amount %q: %w", ErrCoercionFailed, val, err)
			}
			return stroops, nil
		}
		// Otherwise parse as integer stroops
		return CoerceInt64(s)
	default:
		return 0, fmt.Errorf("%w: unsupported type %T for amount", ErrCoercionFailed, v)
	}
}

// CoerceAccountID parses a Stellar strkey (G...), hex string, or raw 32 bytes into an xdr.AccountId.
func CoerceAccountID(v any) (xdr.AccountId, error) {
	var acc xdr.AccountId
	if v == nil {
		return acc, fmt.Errorf("%w: cannot coerce nil to AccountId", ErrCoercionFailed)
	}

	switch val := v.(type) {
	case xdr.AccountId:
		return val, nil
	case *xdr.AccountId:
		if val == nil {
			return acc, fmt.Errorf("%w: nil *xdr.AccountId", ErrCoercionFailed)
		}
		return *val, nil
	case string:
		s := strings.TrimSpace(val)
		if strings.HasPrefix(s, "G") && len(s) == 56 {
			raw, err := strkey.Decode(strkey.VersionByteAccountID, s)
			if err != nil {
				return acc, fmt.Errorf("%w: invalid strkey account %q: %w", ErrCoercionFailed, s, err)
			}
			var ed xdr.Uint256
			copy(ed[:], raw)
			return xdr.AccountId{
				Type:    xdr.PublicKeyTypePublicKeyTypeEd25519,
				Ed25519: &ed,
			}, nil
		}
		// Try hex 32-byte key
		b, err := CoerceBytes(s)
		if err == nil && len(b) == 32 {
			var ed xdr.Uint256
			copy(ed[:], b)
			return xdr.AccountId{
				Type:    xdr.PublicKeyTypePublicKeyTypeEd25519,
				Ed25519: &ed,
			}, nil
		}
		return acc, fmt.Errorf("%w: invalid account ID representation %q", ErrCoercionFailed, val)
	case [32]byte:
		var ed xdr.Uint256
		copy(ed[:], val[:])
		return xdr.AccountId{
			Type:    xdr.PublicKeyTypePublicKeyTypeEd25519,
			Ed25519: &ed,
		}, nil
	case []byte:
		if len(val) != 32 {
			return acc, fmt.Errorf("%w: expected 32 bytes for account ID, got %d", ErrCoercionFailed, len(val))
		}
		var ed xdr.Uint256
		copy(ed[:], val)
		return xdr.AccountId{
			Type:    xdr.PublicKeyTypePublicKeyTypeEd25519,
			Ed25519: &ed,
		}, nil
	default:
		return acc, fmt.Errorf("%w: unsupported type %T for AccountId", ErrCoercionFailed, v)
	}
}

// CoerceMuxedAccount parses G... (ed25519) or M... (med25519) or composite structures into an xdr.MuxedAccount.
func CoerceMuxedAccount(v any) (xdr.MuxedAccount, error) {
	var mux xdr.MuxedAccount
	if v == nil {
		return mux, fmt.Errorf("%w: cannot coerce nil to MuxedAccount", ErrCoercionFailed)
	}

	switch val := v.(type) {
	case xdr.MuxedAccount:
		return val, nil
	case *xdr.MuxedAccount:
		if val == nil {
			return mux, fmt.Errorf("%w: nil *xdr.MuxedAccount", ErrCoercionFailed)
		}
		return *val, nil
	case string:
		s := strings.TrimSpace(val)
		if strings.HasPrefix(s, "M") {
			decoded, err := strkey.DecodeMuxedAccount(s)
			if err != nil {
				return mux, fmt.Errorf("%w: invalid M... muxed account: %w", ErrCoercionFailed, err)
			}
			ed := decoded.Ed25519()
			return xdr.MuxedAccount{
				Type: xdr.CryptoKeyTypeKeyTypeMuxedEd25519,
				Med25519: &xdr.MuxedAccountMed25519{
					Id:      xdr.Uint64(decoded.ID()),
					Ed25519: xdr.Uint256(ed),
				},
			}, nil
		}
		// Regular account ID (G...)
		acc, err := CoerceAccountID(s)
		if err != nil {
			return mux, err
		}
		return xdr.MuxedAccount{
			Type:    xdr.CryptoKeyTypeKeyTypeEd25519,
			Ed25519: acc.Ed25519,
		}, nil
	case map[string]any:
		accVal, ok := val["account"]
		if !ok {
			accVal = val["address"]
		}
		if accVal == nil {
			return mux, fmt.Errorf("%w: missing account or address in muxed account map", ErrCoercionFailed)
		}
		acc, err := CoerceAccountID(accVal)
		if err != nil {
			return mux, err
		}
		if idVal, ok := val["id"]; ok && idVal != nil {
			id, err := CoerceUint64(idVal)
			if err != nil {
				return mux, err
			}
			return xdr.MuxedAccount{
				Type: xdr.CryptoKeyTypeKeyTypeMuxedEd25519,
				Med25519: &xdr.MuxedAccountMed25519{
					Id:      xdr.Uint64(id),
					Ed25519: *acc.Ed25519,
				},
			}, nil
		}
		return xdr.MuxedAccount{
			Type:    xdr.CryptoKeyTypeKeyTypeEd25519,
			Ed25519: acc.Ed25519,
		}, nil
	default:
		return mux, fmt.Errorf("%w: unsupported type %T for MuxedAccount", ErrCoercionFailed, v)
	}
}

// CoerceAsset parses native, "CODE:ISSUER", or object specifications into an xdr.Asset.
func CoerceAsset(v any) (xdr.Asset, error) {
	var asset xdr.Asset
	if v == nil {
		return asset, fmt.Errorf("%w: cannot coerce nil to Asset", ErrCoercionFailed)
	}

	switch val := v.(type) {
	case xdr.Asset:
		return val, nil
	case *xdr.Asset:
		if val == nil {
			return asset, fmt.Errorf("%w: nil *xdr.Asset", ErrCoercionFailed)
		}
		return *val, nil
	case string:
		s := strings.TrimSpace(val)
		if strings.EqualFold(s, "native") || strings.EqualFold(s, "xlm") {
			return xdr.Asset{
				Type: xdr.AssetTypeAssetTypeNative,
			}, nil
		}
		parts := strings.Split(s, ":")
		if len(parts) == 2 {
			code := strings.TrimSpace(parts[0])
			issuerStr := strings.TrimSpace(parts[1])
			issuer, err := CoerceAccountID(issuerStr)
			if err != nil {
				return asset, fmt.Errorf("%w: invalid asset issuer: %w", ErrCoercionFailed, err)
			}
			return buildAlphaNumAsset(code, issuer)
		}
		return asset, fmt.Errorf("%w: invalid asset string format %q (expected 'native' or 'CODE:ISSUER')", ErrCoercionFailed, val)
	case map[string]any:
		if typ, ok := val["type"].(string); ok && strings.EqualFold(typ, "native") {
			return xdr.Asset{Type: xdr.AssetTypeAssetTypeNative}, nil
		}
		codeVal, ok := val["code"]
		if !ok {
			return asset, fmt.Errorf("%w: missing asset code", ErrCoercionFailed)
		}
		code, err := CoerceString(codeVal)
		if err != nil {
			return asset, err
		}
		if strings.EqualFold(code, "native") || strings.EqualFold(code, "xlm") {
			return xdr.Asset{Type: xdr.AssetTypeAssetTypeNative}, nil
		}
		issuerVal, ok := val["issuer"]
		if !ok {
			return asset, fmt.Errorf("%w: missing asset issuer", ErrCoercionFailed)
		}
		issuer, err := CoerceAccountID(issuerVal)
		if err != nil {
			return asset, err
		}
		return buildAlphaNumAsset(code, issuer)
	default:
		return asset, fmt.Errorf("%w: unsupported type %T for Asset", ErrCoercionFailed, v)
	}
}

func buildAlphaNumAsset(code string, issuer xdr.AccountId) (xdr.Asset, error) {
	var asset xdr.Asset
	codeLen := len(code)
	if codeLen >= 1 && codeLen <= 4 {
		var c [4]byte
		copy(c[:], code)
		asset = xdr.Asset{
			Type: xdr.AssetTypeAssetTypeCreditAlphanum4,
			AlphaNum4: &xdr.AlphaNum4{
				AssetCode: xdr.AssetCode4(c),
				Issuer:    issuer,
			},
		}
		return asset, nil
	} else if codeLen >= 5 && codeLen <= 12 {
		var c [12]byte
		copy(c[:], code)
		asset = xdr.Asset{
			Type: xdr.AssetTypeAssetTypeCreditAlphanum12,
			AlphaNum12: &xdr.AlphaNum12{
				AssetCode: xdr.AssetCode12(c),
				Issuer:    issuer,
			},
		}
		return asset, nil
	}
	return asset, fmt.Errorf("%w: asset code %q must be between 1 and 12 characters", ErrCoercionFailed, code)
}

// CoerceChangeTrustAsset parses assets for ChangeTrust operations.
func CoerceChangeTrustAsset(v any) (xdr.ChangeTrustAsset, error) {
	var cta xdr.ChangeTrustAsset
	if v == nil {
		return cta, fmt.Errorf("%w: cannot coerce nil to ChangeTrustAsset", ErrCoercionFailed)
	}

	// Try standard asset first
	asset, err := CoerceAsset(v)
	if err == nil {
		switch asset.Type {
		case xdr.AssetTypeAssetTypeNative:
			return xdr.ChangeTrustAsset{Type: xdr.AssetTypeAssetTypeNative}, nil
		case xdr.AssetTypeAssetTypeCreditAlphanum4:
			return xdr.ChangeTrustAsset{
				Type:      xdr.AssetTypeAssetTypeCreditAlphanum4,
				AlphaNum4: asset.AlphaNum4,
			}, nil
		case xdr.AssetTypeAssetTypeCreditAlphanum12:
			return xdr.ChangeTrustAsset{
				Type:       xdr.AssetTypeAssetTypeCreditAlphanum12,
				AlphaNum12: asset.AlphaNum12,
			}, nil
		default:
			return cta, fmt.Errorf("%w: unsupported asset type for ChangeTrust", ErrCoercionFailed)
		}
	}

	// Check if liquidity pool
	if m, ok := v.(map[string]any); ok {
		typ, _ := m["type"].(string)
		typLower := strings.ToLower(strings.TrimSpace(typ))
		isPool := typLower == "pool" || typLower == "pool_share" || typLower == "liquidity_pool" || typLower == "liquidity_pool_share"
		if isPool || m["asset_a"] != nil || m["assetA"] != nil || m["pool_id"] != nil || m["poolId"] != nil {
			poolIDVal, ok := m["pool_id"]
			if !ok {
				poolIDVal = m["poolId"]
			}
			assetAVal := m["asset_a"]
			if assetAVal == nil {
				assetAVal = m["assetA"]
			}
			assetBVal := m["asset_b"]
			if assetBVal == nil {
				assetBVal = m["assetB"]
			}
			if poolIDVal != nil {
				if _, err := CoerceHash(poolIDVal); err != nil {
					return cta, err
				}
				if assetAVal == nil && assetBVal == nil {
					return xdr.ChangeTrustAsset{
						Type: xdr.AssetTypeAssetTypePoolShare,
						LiquidityPool: &xdr.LiquidityPoolParameters{
							Type:            xdr.LiquidityPoolTypeLiquidityPoolConstantProduct,
							ConstantProduct: &xdr.LiquidityPoolConstantProductParameters{},
						},
					}, nil
				}
			}
			assetA, errA := CoerceAsset(assetAVal)
			if errA != nil {
				return cta, errA
			}
			assetB, errB := CoerceAsset(assetBVal)
			if errB != nil {
				return cta, errB
			}
			fee := int32(xdr.LiquidityPoolFeeV18)
			if fVal, ok := m["fee"]; ok && fVal != nil {
				feeNum, errF := CoerceInt32(fVal)
				if errF != nil {
					return cta, errF
				}
				fee = feeNum
			}
			return xdr.ChangeTrustAsset{
				Type: xdr.AssetTypeAssetTypePoolShare,
				LiquidityPool: &xdr.LiquidityPoolParameters{
					Type: xdr.LiquidityPoolTypeLiquidityPoolConstantProduct,
					ConstantProduct: &xdr.LiquidityPoolConstantProductParameters{
						AssetA: assetA,
						AssetB: assetB,
						Fee:    xdr.Int32(fee),
					},
				},
			}, nil
		}
	}

	return cta, err
}

// CoercePrice converts "n/d", float, or map into a canonical xdr.Price.
func CoercePrice(v any) (xdr.Price, error) {
	return ParsePriceFraction(v)
}
