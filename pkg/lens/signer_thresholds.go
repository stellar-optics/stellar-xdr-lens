package lens

import (
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"

	"github.com/stellar/go-stellar-sdk/strkey"
	"github.com/stellar/go-stellar-sdk/xdr"
)

// An AccountEntry records four signing weights: the master key's weight and
// the low, medium and high thresholds that operations require. Cross-
// referencing those numbers against the signer list is the difference between
// knowing an account exists and knowing whether a key can authorize a
// transaction — which is what this file teaches the walker.
//
// The Thresholds field itself is a bare [4]byte that the generic walker would
// render as hex. A type enricher names the triplet, and a node hook on the
// surrounding AccountEntry adds the analysis that needs the full picture:
// whether the master key is locked (weight 0) and whether the available
// signing weight can satisfy the medium and high thresholds. Both paths stay
// offline and deterministic.
func init() {
	registerEnricher(reflect.TypeOf(xdr.Thresholds{}), enrichThresholds)
	nodeHooks[reflect.TypeOf(xdr.AccountEntry{})] = annotateAccountSignerThresholds
}

// thresholdTripletNote renders the low/medium/high/master quartet the way
// developers refer to it outside the XDR.
func thresholdTripletNote(master, low, med, high byte) string {
	return fmt.Sprintf("low=%d med=%d high=%d master=%d", low, med, high, master)
}

// enrichThresholds renders the bare Thresholds array as its four named
// weights. It fires wherever a Thresholds value appears; the fuller analysis
// that also needs the signer list is attached by the AccountEntry node hook.
// The raw value stays the hex form the generic walker would have produced,
// so machine-readable output does not change shape.
func enrichThresholds(rv reflect.Value) (string, any, bool) {
	th, ok := rv.Interface().(xdr.Thresholds)
	if !ok {
		return "", nil, false
	}
	return thresholdTripletNote(th[0], th[1], th[2], th[3]), hex.EncodeToString(th[:]), true
}

// enrichSignerThresholds returns a note describing an account's threshold
// configuration, whether the master key is locked, and whether the signing
// weight available to the account can satisfy the medium and high thresholds.
//
// The reachable weight is the master weight (when non-zero) plus every
// configured signer's weight: that is the total an attacker or a wallet with
// all keys can accumulate. A locked master (weight 0) removes the master from
// that total, which is exactly the case where the co-signer weights matter
// most.
func enrichSignerThresholds(entry *xdr.AccountEntry) (string, bool) {
	if entry == nil {
		return "", false
	}
	master, low, med, high := AccountThresholds(entry)

	var coSigners uint32
	for _, s := range entry.Signers {
		coSigners += uint32(s.Weight)
	}
	reachable := coSigners
	if master > 0 {
		reachable += uint32(master)
	}

	var b strings.Builder
	b.WriteString(thresholdTripletNote(master, low, med, high))
	if master == 0 {
		b.WriteString("; master locked")
	}
	for _, level := range []struct {
		name string
		need uint8
	}{
		{ThresholdLevelMedium, med},
		{ThresholdLevelHigh, high},
	} {
		if reachable >= uint32(level.need) {
			fmt.Fprintf(&b, "; signers meet %s", level.name)
			continue
		}
		fmt.Fprintf(&b, "; signers cannot meet %s (need %d, have %d)",
			level.name, level.need, reachable)
	}
	return b.String(), true
}

// annotateAccountSignerThresholds attaches the full threshold analysis to the
// Thresholds leaf of a decoded AccountEntry. Notes only render on scalar
// leaves, so the analysis sits on Thresholds rather than on the account or
// the Signers list.
func annotateAccountSignerThresholds(n *Node, v any) {
	entry, ok := v.(xdr.AccountEntry)
	if !ok {
		return
	}
	note, ok := enrichSignerThresholds(&entry)
	if !ok {
		return
	}
	if t := n.Child("Thresholds"); t != nil {
		t.Note = note
	}
}

// ValidateThresholdWeight validates that a threshold or signer weight fits in [0, 255].
func ValidateThresholdWeight(v any, field string) (xdr.Uint32, error) {
	n, err := CoerceInt64(v)
	if err != nil {
		return 0, fmt.Errorf("%w: %s: %w", ErrInvalidTxSpec, field, err)
	}
	if n < 0 || n > 255 {
		return 0, fmt.Errorf("%w: %s %d out of valid range [0, 255]", ErrInvalidTxSpec, field, n)
	}
	return xdr.Uint32(n), nil
}

// ValidateThresholdRange is a SchemaRule.Custom validator for 0-255 thresholds and weights.
func ValidateThresholdRange(v any, path string) error {
	_, err := ValidateThresholdWeight(v, path)
	return err
}

// ParseSignerKey parses a Stellar strkey (G..., T..., X..., P...), 32-byte hex string,
// or typed map into an xdr.SignerKey.
func ParseSignerKey(v any) (xdr.SignerKey, error) {
	var zero xdr.SignerKey
	if v == nil {
		return zero, fmt.Errorf("%w: signer key cannot be nil", ErrInvalidTxSpec)
	}

	switch val := v.(type) {
	case xdr.SignerKey:
		return val, nil
	case *xdr.SignerKey:
		if val == nil {
			return zero, fmt.Errorf("%w: signer key cannot be nil", ErrInvalidTxSpec)
		}
		return *val, nil
	case string:
		s := strings.TrimSpace(val)
		if s == "" {
			return zero, fmt.Errorf("%w: empty signer key", ErrInvalidTxSpec)
		}
		switch {
		case strings.HasPrefix(s, "G") && len(s) == 56:
			raw, err := strkey.Decode(strkey.VersionByteAccountID, s)
			if err != nil || len(raw) != 32 {
				return zero, fmt.Errorf("%w: invalid ed25519 signer key %q: %v", ErrInvalidTxSpec, s, err)
			}
			var k xdr.Uint256
			copy(k[:], raw)
			return xdr.NewSignerKey(xdr.SignerKeyTypeSignerKeyTypeEd25519, k)
		case strings.HasPrefix(s, "T") && len(s) == 56:
			raw, err := strkey.Decode(strkey.VersionByteHashTx, s)
			if err != nil || len(raw) != 32 {
				return zero, fmt.Errorf("%w: invalid pre-auth tx signer key %q: %v", ErrInvalidTxSpec, s, err)
			}
			var k xdr.Uint256
			copy(k[:], raw)
			return xdr.NewSignerKey(xdr.SignerKeyTypeSignerKeyTypePreAuthTx, k)
		case strings.HasPrefix(s, "X") && len(s) == 56:
			raw, err := strkey.Decode(strkey.VersionByteHashX, s)
			if err != nil || len(raw) != 32 {
				return zero, fmt.Errorf("%w: invalid hash-x signer key %q: %v", ErrInvalidTxSpec, s, err)
			}
			var k xdr.Uint256
			copy(k[:], raw)
			return xdr.NewSignerKey(xdr.SignerKeyTypeSignerKeyTypeHashX, k)
		case strings.HasPrefix(s, "P"):
			raw, err := strkey.Decode(strkey.VersionByteSignedPayload, s)
			if err != nil {
				return zero, fmt.Errorf("%w: invalid signed-payload signer key %q: %w", ErrInvalidTxSpec, s, err)
			}
			var sp xdr.SignerKeyEd25519SignedPayload
			if err := sp.UnmarshalBinary(raw); err != nil {
				return zero, fmt.Errorf("%w: invalid signed-payload signer body: %w", ErrInvalidTxSpec, err)
			}
			return xdr.NewSignerKey(xdr.SignerKeyTypeSignerKeyTypeEd25519SignedPayload, sp)
		default:
			h, err := CoerceHash(s)
			if err != nil {
				return zero, fmt.Errorf("%w: invalid signer key %q: %w", ErrInvalidTxSpec, s, err)
			}
			return xdr.NewSignerKey(xdr.SignerKeyTypeSignerKeyTypeEd25519, xdr.Uint256(h))
		}
	case map[string]any:
		typRaw, _ := val["type"].(string)
		valRaw := val["value"]
		if valRaw == nil {
			valRaw = val["key"]
		}
		if typRaw == "" && valRaw != nil {
			return ParseSignerKey(valRaw)
		}
		switch strings.ToLower(strings.TrimSpace(typRaw)) {
		case "ed25519":
			h, err := CoerceHash(valRaw)
			if err != nil {
				return ParseSignerKey(valRaw)
			}
			return xdr.NewSignerKey(xdr.SignerKeyTypeSignerKeyTypeEd25519, xdr.Uint256(h))
		case "pre_auth_tx", "preauthtx":
			h, err := CoerceHash(valRaw)
			if err != nil {
				return zero, fmt.Errorf("%w: pre_auth_tx signer key: %w", ErrInvalidTxSpec, err)
			}
			return xdr.NewSignerKey(xdr.SignerKeyTypeSignerKeyTypePreAuthTx, xdr.Uint256(h))
		case "hash_x", "hashx", "sha256_hash":
			h, err := CoerceHash(valRaw)
			if err != nil {
				return zero, fmt.Errorf("%w: hash_x signer key: %w", ErrInvalidTxSpec, err)
			}
			return xdr.NewSignerKey(xdr.SignerKeyTypeSignerKeyTypeHashX, xdr.Uint256(h))
		case "signed_payload", "ed25519_signed_payload":
			edKey := val["ed25519"]
			if edKey == nil {
				edKey = valRaw
			}
			acc, err := CoerceAccountID(edKey)
			if err != nil || acc.Ed25519 == nil {
				return zero, fmt.Errorf("%w: signed_payload ed25519 key: %w", ErrInvalidTxSpec, err)
			}
			payload, err := CoerceBytes(val["payload"])
			if err != nil {
				return zero, fmt.Errorf("%w: signed_payload payload: %w", ErrInvalidTxSpec, err)
			}
			return xdr.NewSignerKey(xdr.SignerKeyTypeSignerKeyTypeEd25519SignedPayload, xdr.SignerKeyEd25519SignedPayload{
				Ed25519: *acc.Ed25519,
				Payload: payload,
			})
		default:
			return zero, fmt.Errorf("%w: unsupported signer key type %q", ErrInvalidTxSpec, typRaw)
		}
	default:
		return zero, fmt.Errorf("%w: unsupported signer key type %T", ErrInvalidTxSpec, v)
	}
}

// ParseSigner parses a signer object {"key": ..., "weight": ...} into *xdr.Signer.
func ParseSigner(v any) (*xdr.Signer, error) {
	if v == nil {
		return nil, nil
	}
	switch val := v.(type) {
	case *xdr.Signer:
		return val, nil
	case xdr.Signer:
		cp := val
		return &cp, nil
	case map[string]any:
		keyVal, ok := val["key"]
		if !ok {
			keyVal = val["Key"]
		}
		if keyVal == nil {
			return nil, fmt.Errorf("%w: signer requires 'key'", ErrInvalidTxSpec)
		}
		key, err := ParseSignerKey(keyVal)
		if err != nil {
			return nil, err
		}
		weightVal, ok := val["weight"]
		if !ok {
			weightVal = val["Weight"]
		}
		if weightVal == nil {
			return nil, fmt.Errorf("%w: signer requires 'weight'", ErrInvalidTxSpec)
		}
		weight, err := ValidateThresholdWeight(weightVal, "signer.weight")
		if err != nil {
			return nil, err
		}
		return &xdr.Signer{
			Key:    key,
			Weight: weight,
		}, nil
	default:
		return nil, fmt.Errorf("%w: signer must be an object with 'key' and 'weight', got %T", ErrInvalidTxSpec, v)
	}
}

// ParseAccountFlags parses account flag bitmasks from integers or flag name strings/slices.
func ParseAccountFlags(v any) (xdr.Uint32, error) {
	if v == nil {
		return 0, fmt.Errorf("%w: account flags cannot be nil", ErrInvalidTxSpec)
	}
	switch val := v.(type) {
	case string:
		return parseSingleAccountFlag(val)
	case []string:
		var mask uint32
		for _, item := range val {
			f, err := parseSingleAccountFlag(item)
			if err != nil {
				return 0, err
			}
			mask |= uint32(f)
		}
		return xdr.Uint32(mask), nil
	case []any:
		var mask uint32
		for _, item := range val {
			f, err := ParseAccountFlags(item)
			if err != nil {
				return 0, err
			}
			mask |= uint32(f)
		}
		return xdr.Uint32(mask), nil
	default:
		u, err := CoerceUint32(v)
		if err != nil {
			return 0, fmt.Errorf("%w: account flags: %w", ErrInvalidTxSpec, err)
		}
		return xdr.Uint32(u), nil
	}
}

func parseSingleAccountFlag(s string) (xdr.Uint32, error) {
	trimmed := strings.ToLower(strings.TrimSpace(s))
	switch trimmed {
	case "auth_required", "auth_required_flag":
		return xdr.Uint32(xdr.AccountFlagsAuthRequiredFlag), nil
	case "auth_revocable", "auth_revocable_flag":
		return xdr.Uint32(xdr.AccountFlagsAuthRevocableFlag), nil
	case "auth_immutable", "auth_immutable_flag":
		return xdr.Uint32(xdr.AccountFlagsAuthImmutableFlag), nil
	case "auth_clawback_enabled", "auth_clawback_enabled_flag":
		return xdr.Uint32(xdr.AccountFlagsAuthClawbackEnabledFlag), nil
	default:
		u, err := CoerceUint32(trimmed)
		if err != nil {
			return 0, fmt.Errorf("%w: unknown account flag %q", ErrInvalidTxSpec, s)
		}
		return xdr.Uint32(u), nil
	}
}
