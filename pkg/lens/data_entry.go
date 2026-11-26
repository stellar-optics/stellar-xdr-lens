package lens

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/stellar/go-stellar-sdk/xdr"
)

// A DataEntry is arbitrary key/value storage attached to an account: the name
// is a short UTF-8 label (DataName) and the payload is up to 64 opaque bytes
// (DataValue). Nothing on the wire says whether those bytes are text, so a
// decoder that always prints hex buries the common case — configuration
// strings, small JSON documents, flags — behind an encoding step. The value is
// therefore inspected and rendered as text when it is valid printable UTF-8,
// and as hex otherwise. Both forms are deterministic and require no network
// access.
func init() {
	enricherFor[reflect.TypeOf(xdr.DataValue{})] = enrichDataValue
}

// DescribeDataValue returns the display form of an account data value and
// whether that form is UTF-8 text.
//
// A value is considered text when it is non-empty, valid UTF-8 and consists
// solely of printable runes. Control characters — including newlines and tabs —
// disqualify it, because rendering them inside a one-line tree node would
// corrupt the surrounding output. Anything else, including invalid UTF-8 and
// embedded NUL bytes, is rendered as lower-case hex.
func DescribeDataValue(b []byte) (display string, textual bool) {
	if len(b) > 0 && utf8.Valid(b) {
		text := string(b)
		textual = true
		for _, r := range text {
			if !unicode.IsPrint(r) {
				textual = false
				break
			}
		}
		if textual {
			return text, true
		}
	}
	return hex.EncodeToString(b), false
}

// enrichDataValue renders the payload of a DataEntry. The raw value is the
// text itself when the bytes decode as text and the hex form otherwise, so
// machine-readable output carries whichever representation the detector chose;
// the note states which that was and how many bytes it came from.
func enrichDataValue(rv reflect.Value) (string, any, bool) {
	v, ok := rv.Interface().(xdr.DataValue)
	if !ok {
		return "", nil, false
	}
	switch display, textual := DescribeDataValue(v); {
	case len(v) == 0:
		return "empty", "", true
	case textual:
		return fmt.Sprintf("UTF-8 text, %d byte(s)", len(v)), display, true
	default:
		return fmt.Sprintf("binary, %d byte(s)", len(v)), display, true
	}
}

// ValidateDataName validates that a ManageData key name is non-empty and at most 64 bytes.
func ValidateDataName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: manage_data name is required", ErrInvalidTxSpec)
	}
	if len(name) > 64 {
		return fmt.Errorf("%w: manage_data name exceeds 64 bytes (%d)", ErrInvalidTxSpec, len(name))
	}
	return nil
}

// EncodeDataValue encodes a ManageData value into *xdr.DataValue.
// A nil value returns nil (entry deletion). Plain strings (except 0x-prefixed hex)
// are validated as UTF-8 and encoded directly; objects with a "base64" field are
// base64-decoded. All encoded values must be at most 64 bytes.
func EncodeDataValue(v any) (*xdr.DataValue, error) {
	if v == nil {
		return nil, nil
	}

	var raw []byte
	switch val := v.(type) {
	case xdr.DataValue:
		raw = append([]byte(nil), val...)
	case *xdr.DataValue:
		if val == nil {
			return nil, nil
		}
		raw = append([]byte(nil), (*val)...)
	case []byte:
		raw = append([]byte(nil), val...)
	case string:
		if strings.HasPrefix(val, "0x") || strings.HasPrefix(val, "0X") {
			b, err := CoerceBytes(val)
			if err != nil {
				return nil, fmt.Errorf("%w: manage_data hex value: %w", ErrInvalidTxSpec, err)
			}
			raw = b
		} else {
			if !utf8.ValidString(val) {
				return nil, fmt.Errorf("%w: manage_data string value is not valid UTF-8", ErrInvalidTxSpec)
			}
			raw = []byte(val)
		}
	case map[string]any:
		switch {
		case val["base64"] != nil:
			s, ok := val["base64"].(string)
			if !ok {
				return nil, fmt.Errorf("%w: manage_data 'base64' value must be a string, got %T", ErrInvalidTxSpec, val["base64"])
			}
			b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
			if err != nil {
				return nil, fmt.Errorf("%w: invalid base64 manage_data value: %w", ErrInvalidTxSpec, err)
			}
			raw = b
		case val["hex"] != nil:
			s, ok := val["hex"].(string)
			if !ok {
				return nil, fmt.Errorf("%w: manage_data 'hex' value must be a string, got %T", ErrInvalidTxSpec, val["hex"])
			}
			trimmed := strings.TrimSpace(s)
			if !strings.HasPrefix(trimmed, "0x") && !strings.HasPrefix(trimmed, "0X") {
				trimmed = "0x" + trimmed
			}
			b, err := CoerceBytes(trimmed)
			if err != nil {
				return nil, fmt.Errorf("%w: invalid hex manage_data value: %w", ErrInvalidTxSpec, err)
			}
			raw = b
		case val["text"] != nil || val["utf8"] != nil:
			txtVal := val["text"]
			if txtVal == nil {
				txtVal = val["utf8"]
			}
			s, ok := txtVal.(string)
			if !ok {
				return nil, fmt.Errorf("%w: manage_data text value must be a string, got %T", ErrInvalidTxSpec, txtVal)
			}
			if !utf8.ValidString(s) {
				return nil, fmt.Errorf("%w: manage_data text value is not valid UTF-8", ErrInvalidTxSpec)
			}
			raw = []byte(s)
		default:
			return nil, fmt.Errorf("%w: manage_data object value requires 'base64', 'hex', or 'text' field", ErrInvalidTxSpec)
		}
	case []any:
		b, err := CoerceBytes(val)
		if err != nil {
			return nil, fmt.Errorf("%w: manage_data byte array: %w", ErrInvalidTxSpec, err)
		}
		raw = b
	default:
		return nil, fmt.Errorf("%w: unsupported manage_data value type %T", ErrInvalidTxSpec, v)
	}

	if len(raw) > 64 {
		return nil, fmt.Errorf("%w: manage_data value exceeds 64 bytes (%d)", ErrInvalidTxSpec, len(raw))
	}
	dv := xdr.DataValue(raw)
	return &dv, nil
}
