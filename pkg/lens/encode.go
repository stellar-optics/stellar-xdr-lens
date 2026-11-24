package lens

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"reflect"
	"strconv"
	"strings"
	"sync"

	"github.com/stellar/go-stellar-sdk/strkey"
	"github.com/stellar/go-stellar-sdk/xdr"
)

// Encode, the inverse of Decode.
//
// Decode turns XDR into a neutral tree and a stable JSON document. Encode
// closes the loop: it turns a decoded value, or the stable JSON that Decode
// emits, back into base64 XDR. Everything here is pure and offline.
//
// The JSON encoder walks the registered Go type rather than the tree, because
// the generated XDR types are the authority on field names, unions and enum
// values. It accepts the structural JSON produced by `lens decode --json`, so
// the two commands are symmetric: decode then encode reproduces the canonical
// XDR, and encode then decode reproduces the tree.
//
// Some enrichers deliberately collapse a structure into a human sentence (a
// claim predicate, for example). Those renderings are not reversible, and the
// encoder reports ErrUnsupportedType rather than inventing a value. The
// value-level Encode path is unaffected: it re-serializes the concrete XDR
// value and works for every registered type.

// ErrInvalidJSON is returned when encoder input is not valid JSON.
var ErrInvalidJSON = errors.New("invalid JSON input")

// Encode serializes a decoded value back to base64 XDR. It is the inverse of
// Decode and DecodeAs: the result decodes to the same type and, for canonical
// input, to the same bytes.
func Encode(v *Value) (string, error) {
	if v == nil || v.Raw == nil {
		return "", fmt.Errorf("%w: encode requires a decoded value", ErrInvalidArgument)
	}
	encoded, err := SafeMarshalBase64(v.Raw)
	if err != nil {
		return "", fmt.Errorf("%w: xdr encode %s: %w", ErrInvalidXDR, v.Type, err)
	}
	return encoded, nil
}

// EncodeJSON decodes a stable JSON document of the form
//
//	{"type": "TransactionResult", "value": {...}}
//
// and returns base64 XDR. The "type" field is optional for the two simplified
// inputs the CLI already understands: transaction templates and ScVal
// literals, which are detected from their shape. A document carrying an
// "xdr", "base64" or "payload" field is passed through, which makes YAML-style
// documents round-trip as well.
func EncodeJSON(data []byte) (string, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return "", fmt.Errorf("%w: empty input", ErrInvalidJSON)
	}

	var doc map[string]any
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidJSON, err)
	}
	if dec.More() {
		return "", fmt.Errorf("%w: trailing data after the JSON document", ErrInvalidJSON)
	}

	for _, key := range []string{"xdr", "base64", "payload", "raw"} {
		if raw, ok := doc[key].(string); ok && strings.TrimSpace(raw) != "" {
			return strings.TrimSpace(raw), nil
		}
	}

	typeName := ""
	if name, ok := doc["type"].(string); ok {
		typeName = name
	}
	value, hasValue := doc["value"]
	if hasValue {
		if typeName == "" {
			return "", fmt.Errorf("%w: document has a value but no type", ErrInvalidArgument)
		}
		return EncodeJSONValue(typeName, value)
	}

	// No document envelope: fall back to the simplified shapes the CLI
	// supports, using the same shape detection.
	if typeName == "" {
		typeName = detectJSONEncodeType(doc)
	}
	return EncodeJSONAs(typeName, trimmed)
}

// EncodeJSONAs encodes a raw JSON value as typeName. It first tries the
// structural encoder that matches the stable JSON shape. If that fails for the
// two simplified input types, it falls back to the existing transaction and
// ScVal builders so both JSON dialects keep working.
func EncodeJSONAs(typeName string, data []byte) (string, error) {
	if strings.TrimSpace(typeName) == "" {
		return "", fmt.Errorf("%w: type name is required", ErrInvalidArgument)
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return "", fmt.Errorf("%w: empty input", ErrInvalidJSON)
	}

	var value any
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	if err := dec.Decode(&value); err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidJSON, err)
	}

	// Accept a full {"type": ..., "value": ...} document as well as a bare
	// value. EncodeJSON unwraps the envelope before dispatching, but this
	// typed entry point is also called directly with a whole document, so it
	// mirrors that behavior: an embedded xdr/blob passes through and a nested
	// "value" is unwrapped.
	if object, ok := value.(map[string]any); ok {
		for _, key := range []string{"xdr", "base64", "payload", "raw"} {
			if raw, ok := object[key].(string); ok && strings.TrimSpace(raw) != "" {
				return strings.TrimSpace(raw), nil
			}
		}
		if inner, ok := object["value"]; ok {
			value = inner
		}
	}

	encoded, structuralErr := EncodeJSONValue(typeName, value)
	if structuralErr == nil {
		return encoded, nil
	}

	switch strings.ToLower(strings.TrimSpace(typeName)) {
	case "scval":
		scVal, err := ParseScValJSON(trimmed)
		if err == nil {
			bin, marshalErr := scVal.MarshalBinary()
			if marshalErr != nil {
				return "", fmt.Errorf("%w: scval encode binary: %w", ErrInvalidXDR, marshalErr)
			}
			return base64.StdEncoding.EncodeToString(bin), nil
		}
	case "transactionenvelope", "tx", "envelope":
		env, err := BuildTransactionEnvelopeFromJSON(trimmed)
		if err == nil {
			return SafeMarshalBase64(env)
		}
	}

	return "", structuralErr
}

// EncodeJSONValue encodes an already-parsed JSON value (maps, slices and
// scalars as produced by encoding/json with UseNumber) as typeName.
func EncodeJSONValue(typeName string, value any) (encoded string, err error) {
	t, ok := registryType(typeName)
	if !ok {
		return "", fmt.Errorf("%w: %q (see `lens types` for the supported list)", ErrUnknownType, typeName)
	}
	defer func() {
		if r := recover(); r != nil {
			encoded = ""
			err = fmt.Errorf("%w: encoder panicked on %s input: %v", ErrInvalidArgument, t.name, r)
		}
	}()

	dest := t.new()
	rv := reflect.ValueOf(dest)
	if rv.Kind() != reflect.Pointer {
		return "", fmt.Errorf("%w: registry entry %s did not allocate a pointer", ErrInvalidArgument, t.name)
	}
	if err := encodeJSONInto(rv.Elem(), value, ""); err != nil {
		return "", err
	}
	encoded, err = SafeMarshalBase64(dest)
	if err != nil {
		return "", fmt.Errorf("%w: xdr encode %s: %w", ErrInvalidXDR, t.name, err)
	}
	return encoded, nil
}

// registryType resolves a type name case-insensitively.
func registryType(typeName string) (xdrType, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	for _, t := range registry {
		if strings.EqualFold(t.name, typeName) {
			return t, true
		}
	}
	return xdrType{}, false
}

// detectJSONEncodeType mirrors the CLI's shape detection for documents that
// have no explicit type field.
func detectJSONEncodeType(doc map[string]any) string {
	for _, key := range []string{"source_account", "sourceAccount", "operations", "fee_bump"} {
		if _, ok := doc[key]; ok {
			return "TransactionEnvelope"
		}
	}
	return "ScVal"
}

// encodeJSONInto sets rv from a generic JSON value. path is a dotted location
// used in error messages so a caller can find the offending field.
func encodeJSONInto(rv reflect.Value, input any, path string) error {
	if !rv.IsValid() {
		return fmt.Errorf("%w: cannot encode into an invalid value at %s", ErrInvalidArgument, displayPath(path))
	}

	// Interfaces are rare in generated XDR, but decode them when present.
	if rv.Kind() == reflect.Interface {
		if input == nil {
			return nil
		}
		candidate := reflect.ValueOf(input)
		if candidate.Type().AssignableTo(rv.Type()) {
			rv.Set(candidate)
			return nil
		}
		return fmt.Errorf("%w: %s: cannot assign %T to %s", ErrInvalidArgument, displayPath(path), input, rv.Type())
	}

	if rv.Kind() == reflect.Pointer {
		if input == nil {
			return nil
		}
		ptr := reflect.New(rv.Type().Elem())
		if err := encodeJSONInto(ptr.Elem(), input, path); err != nil {
			return err
		}
		rv.Set(ptr)
		return nil
	}

	// A missing optional field stays at its zero value. The protocol uses
	// pointers for opt-in arms, so this is the correct representation of
	// "absent" for the structural JSON.
	if input == nil {
		return nil
	}

	if handled, err := encodeSpecialJSON(rv, input, path); handled {
		return err
	}

	if isEnumType(rv.Type()) {
		return encodeEnum(rv, input, path)
	}

	switch rv.Kind() {
	case reflect.Struct:
		return encodeStruct(rv, input, path)
	case reflect.Slice:
		if rv.Type().Elem().Kind() == reflect.Uint8 {
			return encodeByteSlice(rv, input, path, false)
		}
		return encodeSlice(rv, input, path)
	case reflect.Array:
		if rv.Type().Elem().Kind() == reflect.Uint8 {
			return encodeByteArray(rv, input, path)
		}
		return encodeArray(rv, input, path)
	case reflect.Bool:
		value, ok := input.(bool)
		if !ok {
			return fmt.Errorf("%w: %s: expected a boolean, got %T", ErrInvalidArgument, displayPath(path), input)
		}
		rv.SetBool(value)
	case reflect.String:
		value, ok := input.(string)
		if !ok {
			return fmt.Errorf("%w: %s: expected a string, got %T", ErrInvalidArgument, displayPath(path), input)
		}
		rv.SetString(value)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value, ok := inputInt64(input)
		if !ok {
			return fmt.Errorf("%w: %s: expected an integer, got %T", ErrInvalidArgument, displayPath(path), input)
		}
		if rv.OverflowInt(value) {
			return fmt.Errorf("%w: %s: %d overflows %s", ErrInvalidArgument, displayPath(path), value, rv.Type())
		}
		rv.SetInt(value)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		value, ok := inputUint64(input)
		if !ok {
			return fmt.Errorf("%w: %s: expected an unsigned integer, got %T", ErrInvalidArgument, displayPath(path), input)
		}
		if rv.OverflowUint(value) {
			return fmt.Errorf("%w: %s: %d overflows %s", ErrInvalidArgument, displayPath(path), value, rv.Type())
		}
		rv.SetUint(value)
	case reflect.Float32, reflect.Float64:
		value, ok := inputFloat64(input)
		if !ok {
			return fmt.Errorf("%w: %s: expected a number, got %T", ErrInvalidArgument, displayPath(path), input)
		}
		if rv.OverflowFloat(value) {
			return fmt.Errorf("%w: %s: %v overflows %s", ErrInvalidArgument, displayPath(path), value, rv.Type())
		}
		rv.SetFloat(value)
	default:
		return fmt.Errorf("%w: %s: cannot encode kind %s", ErrUnsupportedType, displayPath(path), rv.Kind())
	}
	return nil
}

// derivedNodeFields records node children that enrichment hooks append to a
// decoded node but that are not XDR fields, such as the parsed WASM sections on
// a ContractCodeEntry. The structural encoder reads the Go type, which has no
// such fields, so it must accept and ignore these names to let decode output
// round-trip; anything else that is not a field stays an error.
var derivedNodeFields = map[reflect.Type]map[string]bool{}

// registerDerivedNodeFields marks names as encoder-transparent annotations for
// type t. It is called from the init of the file that attaches them.
func registerDerivedNodeFields(t reflect.Type, names ...string) {
	set := derivedNodeFields[t]
	if set == nil {
		set = make(map[string]bool, len(names))
		derivedNodeFields[t] = set
	}
	for _, name := range names {
		set[name] = true
	}
}

// isDerivedNodeField reports whether name is an annotation rather than a field.
func isDerivedNodeField(t reflect.Type, name string) bool {
	set, ok := derivedNodeFields[t]
	return ok && set[name]
}

func encodeStruct(rv reflect.Value, input any, path string) error {
	object, ok := input.(map[string]any)
	if !ok {
		return fmt.Errorf("%w: %s: expected an object for %s, got %T",
			ErrInvalidArgument, displayPath(path), rv.Type().Name(), input)
	}
	rt := rv.Type()
	for key, value := range object {
		field, found := rt.FieldByName(key)
		if !found || field.PkgPath != "" {
			if isDerivedNodeField(rt, key) {
				continue
			}
			return fmt.Errorf("%w: %s: %s has no field %q",
				ErrInvalidArgument, displayPath(path), rt.Name(), key)
		}
		if err := encodeJSONInto(rv.FieldByIndex(field.Index), value, join(path, key)); err != nil {
			return err
		}
	}
	return nil
}

func encodeSlice(rv reflect.Value, input any, path string) error {
	items, ok := input.([]any)
	if !ok {
		return fmt.Errorf("%w: %s: expected an array, got %T", ErrInvalidArgument, displayPath(path), input)
	}
	slice := reflect.MakeSlice(rv.Type(), len(items), len(items))
	for i, item := range items {
		if err := encodeJSONInto(slice.Index(i), item, path+"["+strconv.Itoa(i)+"]"); err != nil {
			return err
		}
	}
	rv.Set(slice)
	return nil
}

func encodeArray(rv reflect.Value, input any, path string) error {
	items, ok := input.([]any)
	if !ok {
		return fmt.Errorf("%w: %s: expected an array, got %T", ErrInvalidArgument, displayPath(path), input)
	}
	if len(items) != rv.Len() {
		return fmt.Errorf("%w: %s: expected %d elements, got %d",
			ErrInvalidArgument, displayPath(path), rv.Len(), len(items))
	}
	for i, item := range items {
		if err := encodeJSONInto(rv.Index(i), item, path+"["+strconv.Itoa(i)+"]"); err != nil {
			return err
		}
	}
	return nil
}

// encodeByteSlice handles the two shapes byte slices take in the stable JSON:
// an array of byte values (an enriched hash) and a string. genericHex selects
// which string encoding is expected; base64 strings are used by enrichers that
// return a raw []byte, while the generic walker emits hex.
func encodeByteSlice(rv reflect.Value, input any, path string, base64String bool) error {
	bytesValue, err := decodeJSONBytes(input, path, base64String)
	if err != nil {
		return err
	}
	rv.SetBytes(bytesValue)
	return nil
}

func encodeByteArray(rv reflect.Value, input any, path string) error {
	bytesValue, err := decodeJSONBytes(input, path, false)
	if err != nil {
		return err
	}
	if len(bytesValue) != rv.Len() {
		return fmt.Errorf("%w: %s: expected %d bytes, got %d",
			ErrInvalidArgument, displayPath(path), rv.Len(), len(bytesValue))
	}
	for i, b := range bytesValue {
		rv.Index(i).SetUint(uint64(b))
	}
	return nil
}

func decodeJSONBytes(input any, path string, base64String bool) ([]byte, error) {
	switch value := input.(type) {
	case string:
		trimmed := strings.TrimSpace(value)
		trimmed = strings.TrimPrefix(strings.TrimPrefix(trimmed, "0x"), "0X")
		if base64String {
			decoded, err := base64.StdEncoding.DecodeString(trimmed)
			if err == nil {
				return decoded, nil
			}
			// Some callers hand-build hex; accept it as a fallback rather
			// than rejecting a value the type can represent.
			decoded, hexErr := hex.DecodeString(trimmed)
			if hexErr == nil {
				return decoded, nil
			}
			return nil, fmt.Errorf("%w: %s: neither base64 nor hex: %w",
				ErrInvalidArgument, displayPath(path), err)
		}
		decoded, err := hex.DecodeString(trimmed)
		if err != nil {
			return nil, fmt.Errorf("%w: %s: expected a hex string: %w",
				ErrInvalidArgument, displayPath(path), err)
		}
		return decoded, nil
	case []any:
		out := make([]byte, len(value))
		for i, item := range value {
			n, ok := inputUint64(item)
			if !ok || n > math.MaxUint8 {
				return nil, fmt.Errorf("%w: %s[%d]: expected a byte, got %v",
					ErrInvalidArgument, displayPath(path), i, item)
			}
			out[i] = byte(n)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("%w: %s: expected a hex string or byte array, got %T",
			ErrInvalidArgument, displayPath(path), input)
	}
}

// decodeUpgradeTypeInput recovers an opaque upgrade vote. An enricher that
// recognizes the vote keeps the raw []byte, which the JSON formatter renders as
// base64; a vote that does not parse falls through to the generic hex
// rendering. Both spellings appear in decode output, so the decoder prefers
// whichever interpretation yields a valid LedgerUpgrade and otherwise trusts
// hex. A base64 blob that happens to parse as an upgrade is always the enriched
// form, so preferring it is exact rather than heuristic.
func decodeUpgradeTypeInput(input any, path string) ([]byte, error) {
	switch value := input.(type) {
	case string:
		trimmed := strings.TrimSpace(value)
		if decoded, err := base64.StdEncoding.DecodeString(trimmed); err == nil && isLedgerUpgradeBytes(decoded) {
			return decoded, nil
		}
		hexText := strings.TrimPrefix(strings.TrimPrefix(trimmed, "0x"), "0X")
		if decoded, err := hex.DecodeString(hexText); err == nil {
			return decoded, nil
		}
		if decoded, err := base64.StdEncoding.DecodeString(trimmed); err == nil {
			return decoded, nil
		}
		return nil, fmt.Errorf("%w: %s: upgrade vote is neither base64 nor hex",
			ErrInvalidArgument, displayPath(path))
	case []any:
		return decodeJSONBytes(value, path, false)
	default:
		return nil, fmt.Errorf("%w: %s: expected a base64 or hex upgrade vote, got %T",
			ErrInvalidArgument, displayPath(path), input)
	}
}

// isLedgerUpgradeBytes reports whether b decodes as a complete LedgerUpgrade.
func isLedgerUpgradeBytes(b []byte) bool {
	var upgrade xdr.LedgerUpgrade
	return upgrade.UnmarshalBinary(b) == nil
}

// encodeSpecialJSON handles the types whose enrichers render them as
// something other than a struct, such as a strkey address or a canonical
// asset string. It reports handled=false when the input is not in the
// enriched shape, which lets a union fall through to the structural encoder.
func encodeSpecialJSON(rv reflect.Value, input any, path string) (bool, error) {
	t := rv.Type()
	switch t {
	case reflect.TypeOf(xdr.AccountId{}):
		value, err := CoerceAccountID(input)
		if err != nil {
			return true, wrapAt(err, path)
		}
		rv.Set(reflect.ValueOf(value))
		return true, nil

	case reflect.TypeOf(xdr.MuxedAccount{}):
		value, err := CoerceMuxedAccount(input)
		if err != nil {
			return true, wrapAt(err, path)
		}
		rv.Set(reflect.ValueOf(value))
		return true, nil

	case reflect.TypeOf(xdr.Asset{}):
		value, err := CoerceAsset(input)
		if err != nil {
			return true, wrapAt(err, path)
		}
		rv.Set(reflect.ValueOf(value))
		return true, nil

	case reflect.TypeOf(xdr.ChangeTrustAsset{}):
		value, err := CoerceChangeTrustAsset(input)
		if err != nil {
			return true, wrapAt(err, path)
		}
		rv.Set(reflect.ValueOf(value))
		return true, nil

	case reflect.TypeOf(xdr.TrustLineAsset{}):
		if _, isObject := input.(map[string]any); isObject {
			return false, nil
		}
		asset, err := CoerceAsset(input)
		if err != nil {
			return true, wrapAt(err, path)
		}
		rv.Set(reflect.ValueOf(asset.ToTrustLineAsset()))
		return true, nil

	case reflect.TypeOf(xdr.Price{}):
		if object, isObject := input.(map[string]any); isObject {
			// The stable JSON falls back to the struct fields when an
			// enricher declines (a zero denominator, say). Let the
			// structural encoder reproduce that shape verbatim.
			if _, hasN := object["N"]; hasN {
				return false, nil
			}
		}
		value, err := parsePriceInput(input)
		if err != nil {
			return true, wrapAt(err, path)
		}
		rv.Set(reflect.ValueOf(value))
		return true, nil

	case reflect.TypeOf(xdr.ScAddress{}):
		if _, isObject := input.(map[string]any); isObject {
			return false, nil
		}
		value, err := parseScAddressInput(input)
		if err != nil {
			return true, wrapAt(err, path)
		}
		rv.Set(reflect.ValueOf(value))
		return true, nil

	case reflect.TypeOf(xdr.ContractId{}):
		value, err := parseContractIDInput(input)
		if err != nil {
			return true, wrapAt(err, path)
		}
		rv.Set(reflect.ValueOf(value))
		return true, nil

	case reflect.TypeOf(xdr.ClaimableBalanceId{}):
		value, err := parseClaimableBalanceIDInput(input)
		if err != nil {
			return true, wrapAt(err, path)
		}
		rv.Set(reflect.ValueOf(value))
		return true, nil

	case reflect.TypeOf(xdr.PublicKey{}):
		value, err := parsePublicKeyInput(input)
		if err != nil {
			return true, wrapAt(err, path)
		}
		rv.Set(reflect.ValueOf(value))
		return true, nil

	case reflect.TypeOf(xdr.NodeId{}):
		value, err := parseNodeIDInput(input)
		if err != nil {
			return true, wrapAt(err, path)
		}
		rv.Set(reflect.ValueOf(value))
		return true, nil

	case reflect.TypeOf(xdr.Signature{}):
		return true, encodeByteSlice(rv, input, path, true)

	case reflect.TypeOf(xdr.UpgradeType{}):
		value, err := decodeUpgradeTypeInput(input, path)
		if err != nil {
			return true, err
		}
		rv.SetBytes(value)
		return true, nil

	case reflect.TypeOf(xdr.DataValue{}):
		value, err := parseDataValueInput(input)
		if err != nil {
			return true, wrapAt(err, path)
		}
		rv.Set(reflect.ValueOf(value))
		return true, nil

	case reflect.TypeOf(xdr.PeerAddress{}):
		value, err := parsePeerAddressInput(input)
		if err != nil {
			return true, wrapAt(err, path)
		}
		rv.Set(reflect.ValueOf(value))
		return true, nil

	case reflect.TypeOf(xdr.LiquidityPoolConstantProductParameters{}):
		value, err := parsePoolParametersInput(input)
		if err != nil {
			return true, wrapAt(err, path)
		}
		rv.Set(reflect.ValueOf(value))
		return true, nil

	case reflect.TypeOf(xdr.ContractExecutable{}):
		if _, isObject := input.(map[string]any); isObject {
			return false, nil
		}
		value, err := parseContractExecutableInput(input)
		if err != nil {
			return true, wrapAt(err, path)
		}
		rv.Set(reflect.ValueOf(value))
		return true, nil

	case reflect.TypeOf(xdr.ScVal{}):
		if _, isObject := input.(map[string]any); isObject {
			return false, nil
		}
		value, err := parseStorageKeyScVal(input)
		if err != nil {
			return true, wrapAt(err, path)
		}
		rv.Set(reflect.ValueOf(value))
		return true, nil

	case reflect.TypeOf(xdr.ClaimPredicate{}):
		return true, fmt.Errorf("%w: %s: claim predicates render as an expression and cannot be encoded from JSON; encode the decoded value instead",
			ErrUnsupportedType, displayPath(path))
	}
	return false, nil
}

func wrapAt(err error, path string) error {
	return fmt.Errorf("%w: %s: %w", ErrInvalidArgument, displayPath(path), err)
}

func displayPath(path string) string {
	if path == "" {
		return "<root>"
	}
	return path
}

func parsePriceInput(input any) (xdr.Price, error) {
	if text, ok := input.(string); ok {
		fraction := strings.TrimSpace(text)
		if index := strings.Index(fraction, " ("); index >= 0 {
			fraction = fraction[:index]
		}
		parts := strings.SplitN(fraction, "/", 2)
		if len(parts) != 2 {
			return xdr.Price{}, fmt.Errorf("price %q is not an n/d fraction", text)
		}
		n, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 32)
		if err != nil {
			return xdr.Price{}, fmt.Errorf("price numerator: %w", err)
		}
		d, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 32)
		if err != nil {
			return xdr.Price{}, fmt.Errorf("price denominator: %w", err)
		}
		return xdr.Price{N: xdr.Int32(n), D: xdr.Int32(d)}, nil
	}
	return CoercePrice(input)
}

func parseScAddressInput(input any) (xdr.ScAddress, error) {
	text, ok := input.(string)
	if !ok {
		return xdr.ScAddress{}, fmt.Errorf("expected a strkey address, got %T", input)
	}
	if strings.HasPrefix(text, "G") {
		account, err := CoerceAccountID(text)
		if err != nil {
			return xdr.ScAddress{}, err
		}
		return xdr.ScAddress{
			Type:      xdr.ScAddressTypeScAddressTypeAccount,
			AccountId: &account,
		}, nil
	}
	if strings.HasPrefix(text, "C") {
		raw, err := strkey.Decode(strkey.VersionByteContract, text)
		if err != nil {
			return xdr.ScAddress{}, fmt.Errorf("contract address: %w", err)
		}
		if len(raw) != 32 {
			return xdr.ScAddress{}, fmt.Errorf("contract address decoded to %d bytes, want 32", len(raw))
		}
		var id xdr.ContractId
		copy(id[:], raw)
		return xdr.ScAddress{
			Type:       xdr.ScAddressTypeScAddressTypeContract,
			ContractId: &id,
		}, nil
	}
	return xdr.ScAddress{}, fmt.Errorf("address %q is neither an account (G...) nor a contract (C...) strkey", text)
}

func parseContractIDInput(input any) (xdr.ContractId, error) {
	text, ok := input.(string)
	if !ok {
		return xdr.ContractId{}, fmt.Errorf("expected a C... strkey, got %T", input)
	}
	raw, err := strkey.Decode(strkey.VersionByteContract, text)
	if err != nil {
		return xdr.ContractId{}, fmt.Errorf("contract address: %w", err)
	}
	if len(raw) != 32 {
		return xdr.ContractId{}, fmt.Errorf("contract address decoded to %d bytes, want 32", len(raw))
	}
	var id xdr.ContractId
	copy(id[:], raw)
	return id, nil
}

func parseClaimableBalanceIDInput(input any) (xdr.ClaimableBalanceId, error) {
	text, ok := input.(string)
	if !ok {
		return xdr.ClaimableBalanceId{}, fmt.Errorf("expected a B... strkey, got %T", input)
	}
	var id xdr.ClaimableBalanceId
	if err := id.DecodeFromStrkey(text); err != nil {
		return xdr.ClaimableBalanceId{}, fmt.Errorf("claimable balance id: %w", err)
	}
	return id, nil
}

func parsePublicKeyInput(input any) (xdr.PublicKey, error) {
	account, err := CoerceAccountID(input)
	if err != nil {
		return xdr.PublicKey{}, err
	}
	return xdr.PublicKey(account), nil
}

func parseNodeIDInput(input any) (xdr.NodeId, error) {
	publicKey, err := parsePublicKeyInput(input)
	if err != nil {
		return xdr.NodeId{}, err
	}
	return xdr.NodeId(publicKey), nil
}

// parseDataValueInput recovers an account data payload. The decoder renders
// printable values as text and everything else as hex; a value that is valid
// hex is treated as hex (the binary case), and anything else as text.
func parseDataValueInput(input any) (xdr.DataValue, error) {
	switch value := input.(type) {
	case string:
		if value == "" {
			return xdr.DataValue{}, nil
		}
		if len(value)%2 == 0 {
			if decoded, err := hex.DecodeString(value); err == nil {
				return xdr.DataValue(decoded), nil
			}
		}
		return xdr.DataValue(value), nil
	case []any:
		bytesValue, err := decodeJSONBytes(value, "", false)
		if err != nil {
			return nil, err
		}
		return xdr.DataValue(bytesValue), nil
	default:
		return nil, fmt.Errorf("expected text or hex, got %T", input)
	}
}

func parsePeerAddressInput(input any) (xdr.PeerAddress, error) {
	text, ok := input.(string)
	if !ok {
		return xdr.PeerAddress{}, fmt.Errorf("expected host:port, got %T", input)
	}
	host, portText, err := net.SplitHostPort(strings.TrimSpace(text))
	if err != nil {
		return xdr.PeerAddress{}, fmt.Errorf("peer address %q: %w", text, err)
	}
	port, err := strconv.ParseUint(portText, 10, 32)
	if err != nil {
		return xdr.PeerAddress{}, fmt.Errorf("peer port: %w", err)
	}
	ip, err := parsePeerIP(host)
	if err != nil {
		return xdr.PeerAddress{}, err
	}
	return xdr.PeerAddress{Ip: ip, Port: xdr.Uint32(port)}, nil
}

func parsePeerIP(host string) (xdr.PeerAddressIp, error) {
	parsed := net.ParseIP(host)
	if parsed == nil {
		return xdr.PeerAddressIp{}, fmt.Errorf("peer host %q is not an IP address", host)
	}
	if ipv4 := parsed.To4(); ipv4 != nil {
		var address [4]byte
		copy(address[:], ipv4)
		return xdr.PeerAddressIp{Type: xdr.IpAddrTypeIPv4, Ipv4: &address}, nil
	}
	var address [16]byte
	copy(address[:], parsed.To16())
	return xdr.PeerAddressIp{Type: xdr.IpAddrTypeIPv6, Ipv6: &address}, nil
}

// parsePoolParametersInput accepts the "A / B @ 0.3%" rendering produced by
// the liquidity-pool enricher.
func parsePoolParametersInput(input any) (xdr.LiquidityPoolConstantProductParameters, error) {
	text, ok := input.(string)
	if !ok {
		return xdr.LiquidityPoolConstantProductParameters{}, fmt.Errorf("expected pool parameters text, got %T", input)
	}
	at := strings.LastIndex(text, " @ ")
	if at < 0 {
		return xdr.LiquidityPoolConstantProductParameters{}, fmt.Errorf("pool parameters %q have no fee", text)
	}
	pair := text[:at]
	feeText := strings.TrimSuffix(strings.TrimSpace(text[at+3:]), "%")
	pairParts := strings.SplitN(pair, " / ", 2)
	if len(pairParts) != 2 {
		return xdr.LiquidityPoolConstantProductParameters{}, fmt.Errorf("pool parameters %q have no asset pair", text)
	}
	assetA, err := CoerceAsset(strings.TrimSpace(pairParts[0]))
	if err != nil {
		return xdr.LiquidityPoolConstantProductParameters{}, fmt.Errorf("asset A: %w", err)
	}
	assetB, err := CoerceAsset(strings.TrimSpace(pairParts[1]))
	if err != nil {
		return xdr.LiquidityPoolConstantProductParameters{}, fmt.Errorf("asset B: %w", err)
	}
	feePercent, err := strconv.ParseFloat(feeText, 64)
	if err != nil {
		return xdr.LiquidityPoolConstantProductParameters{}, fmt.Errorf("fee: %w", err)
	}
	return xdr.LiquidityPoolConstantProductParameters{
		AssetA: assetA,
		AssetB: assetB,
		Fee:    xdr.Int32(math.Round(feePercent * 100)),
	}, nil
}

// parseContractExecutableInput recovers a contract executable from the scalar
// forms the enricher emits: a wasm hash, the built-in token name, or an
// external reference description.
func parseContractExecutableInput(input any) (xdr.ContractExecutable, error) {
	text, ok := input.(string)
	if !ok {
		return xdr.ContractExecutable{}, fmt.Errorf("expected an executable description, got %T", input)
	}
	if text == "stellar_asset" || strings.HasPrefix(text, "stellar asset contract") {
		return xdr.ContractExecutable{Type: xdr.ContractExecutableTypeContractExecutableStellarAsset}, nil
	}
	if hashText, found := strings.CutPrefix(text, "wasm:"); found {
		hash, err := CoerceHash(hashText)
		if err != nil {
			return xdr.ContractExecutable{}, err
		}
		return xdr.ContractExecutable{
			Type:     xdr.ContractExecutableTypeContractExecutableWasm,
			WasmHash: (*xdr.Hash)(&hash),
		}, nil
	}
	// The stable JSON carries the raw hash without the "wasm:" display
	// prefix, so a bare 32-byte hash is also the wasm arm. A strkey owner is
	// not valid hex, which keeps the two spellings apart.
	if hash, err := CoerceHash(text); err == nil {
		return xdr.ContractExecutable{
			Type:     xdr.ContractExecutableTypeContractExecutableWasm,
			WasmHash: (*xdr.Hash)(&hash),
		}, nil
	}
	// External references render as "<owner>" or "<owner> tag: <tag>".
	ownerText := text
	var tag string
	if index := strings.Index(text, " tag: "); index >= 0 {
		ownerText = text[:index]
		tag = text[index+len(" tag: "):]
	}
	owner, err := parseScAddressInput(ownerText)
	if err != nil {
		return xdr.ContractExecutable{}, err
	}
	ref := &xdr.ContractExecutableExternalRef{ExecutableOwner: owner}
	if tag != "" {
		ref.Tag = xdr.ScString(tag)
	}
	return xdr.ContractExecutable{
		Type:        xdr.ContractExecutableTypeContractExecutableExternalRef,
		ExternalRef: ref,
	}, nil
}

// parseStorageKeyScVal recovers the two special ScVal ledger keys that the
// contract-data enricher names instead of rendering structurally.
func parseStorageKeyScVal(input any) (xdr.ScVal, error) {
	text, ok := input.(string)
	if !ok {
		return xdr.ScVal{}, fmt.Errorf("expected a named storage key, got %T", input)
	}
	switch {
	case text == "instance storage key":
		return xdr.ScVal{Type: xdr.ScValTypeScvLedgerKeyContractInstance}, nil
	case strings.HasPrefix(text, "nonce key: "):
		nonce, err := strconv.ParseInt(strings.TrimPrefix(text, "nonce key: "), 10, 64)
		if err != nil {
			return xdr.ScVal{}, fmt.Errorf("nonce key: %w", err)
		}
		nonceKey := &xdr.ScNonceKey{Nonce: xdr.Int64(nonce)}
		return xdr.ScVal{
			Type:     xdr.ScValTypeScvLedgerKeyNonce,
			NonceKey: nonceKey,
		}, nil
	default:
		return xdr.ScVal{}, fmt.Errorf("ScVal %q is not a named storage key; encode the structural union object instead", text)
	}
}

func encodeEnum(rv reflect.Value, input any, path string) error {
	if value, ok := inputInt64(input); ok {
		if rv.OverflowInt(value) {
			return fmt.Errorf("%w: %s: enum value %d overflows %s",
				ErrInvalidArgument, displayPath(path), value, rv.Type())
		}
		rv.SetInt(value)
		return nil
	}
	name, ok := input.(string)
	if !ok {
		return fmt.Errorf("%w: %s: expected an enum name or number, got %T",
			ErrInvalidArgument, displayPath(path), input)
	}
	value, found := enumValueForName(rv.Type(), name)
	if !found {
		return fmt.Errorf("%w: %s: %q is not a valid %s value",
			ErrInvalidArgument, displayPath(path), name, rv.Type().Name())
	}
	rv.SetInt(value)
	return nil
}

// enumEntry is one named enum constant.
type enumEntry struct {
	name  string
	label string
	value int64
}

var (
	enumEntriesCache sync.Map // map[reflect.Type][]enumEntry
	enumNamesCache   sync.Map // map[reflect.Type]map[string]int64
)

// enumEntriesFor enumerates the constants of a generated enum type by probing
// valid values. Generated XDR enums carry a String method and a ValidEnum
// method; protocol enums are small and contiguous enough that probing a
// bounded range is both cheap and deterministic. The result is cached.
func enumEntriesFor(t reflect.Type) []enumEntry {
	if cached, ok := enumEntriesCache.Load(t); ok {
		if entries, ok := cached.([]enumEntry); ok {
			return entries
		}
	}
	entries := make([]enumEntry, 0, 32)
	validEnum, hasValid := t.MethodByName("ValidEnum")
	appendValue := func(value int64) {
		if value < math.MinInt32 || value > math.MaxInt32 {
			return
		}
		rv := reflect.New(t).Elem()
		rv.SetInt(value)
		if hasValid {
			results := validEnum.Func.Call([]reflect.Value{rv, reflect.ValueOf(int32(value))})
			if len(results) != 1 || !results[0].Bool() {
				return
			}
		}
		stringer, ok := rv.Interface().(fmt.Stringer)
		if !ok {
			return
		}
		name := safeString(stringer)
		if name == "" {
			return
		}
		entries = append(entries, enumEntry{name: name, label: normalizeEnumLabel(name), value: value})
	}
	// Current protocol enums occupy [-18, 2000]. Probe a margin around that
	// range first, then widen once so a newer protocol with larger values
	// still resolves without making the common path expensive.
	for value := int64(-64); value <= 4096; value++ {
		appendValue(value)
	}
	names := make(map[string]int64, len(entries))
	for _, entry := range entries {
		names[entry.name] = entry.value
	}
	enumNamesCache.Store(t, names)
	enumEntriesCache.Store(t, entries)
	return entries
}

// enumValueForName resolves a constant name or a human label. Labels are
// matched against the constant suffix after normalizing case, punctuation and
// parenthetical qualifiers, which covers the enrichments that name a value in
// protocol-documentation terms ("dead entry (tombstone)"). An enricher may also
// replace the value with a short label ("metadata entry", "tx_set"); those are
// resolved by replaying the enricher over every valid constant, which keeps the
// label vocabulary and the encoder in step automatically.
func enumValueForName(t reflect.Type, name string) (int64, bool) {
	entries := enumEntriesFor(t)
	if cached, ok := enumNamesCache.Load(t); ok {
		if names, ok := cached.(map[string]int64); ok {
			if value, found := names[name]; found {
				return value, true
			}
		}
	}
	wanted := normalizeEnumLabel(name)
	typeName := normalizeEnumLabel(t.Name())

	// Exact matches win, in two passes, so a short label like "tx_set" cannot
	// be swallowed by a longer constant that merely ends with it.
	for _, entry := range entries {
		if entry.label == wanted {
			return entry.value, true
		}
		if suffix := strings.TrimPrefix(entry.label, typeName); suffix != "" && suffix == wanted {
			return entry.value, true
		}
	}
	if value, found := enumValueForEnrichedLabel(t, wanted); found {
		return value, true
	}
	// Finally accept a partial suffix, which is how a caller may abbreviate a
	// long generated constant.
	for _, entry := range entries {
		if suffix := strings.TrimPrefix(entry.label, typeName); suffix != "" && strings.HasSuffix(suffix, wanted) {
			return entry.value, true
		}
	}
	return 0, false
}

var enumLabelCache sync.Map // map[reflect.Type]map[string]int64

// enumValueForEnrichedLabel resolves the short label an enum enricher produces
// back to its constant value. The label map is built once per type by replaying
// the registered enricher over every valid constant.
func enumValueForEnrichedLabel(t reflect.Type, wanted string) (int64, bool) {
	if cached, ok := enumLabelCache.Load(t); ok {
		if labels, ok := cached.(map[string]int64); ok {
			value, found := labels[wanted]
			return value, found
		}
	}
	labels := make(map[string]int64)
	for _, entry := range enumEntriesFor(t) {
		rv := reflect.New(t).Elem()
		rv.SetInt(entry.value)
		_, raw, ok := enrich(rv)
		if !ok {
			continue
		}
		label, ok := raw.(string)
		if !ok || label == "" {
			continue
		}
		labels[normalizeEnumLabel(label)] = entry.value
	}
	enumLabelCache.Store(t, labels)
	value, found := labels[wanted]
	return value, found
}

// normalizeEnumLabel lower-cases a name, drops parenthetical qualifiers and
// removes everything that is not a letter or digit, so "dead entry
// (tombstone)" and "Deadentry" both become "deadentry".
func normalizeEnumLabel(name string) string {
	if index := strings.IndexAny(name, "(["); index >= 0 {
		name = name[:index]
	}
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func inputInt64(input any) (int64, bool) {
	switch value := input.(type) {
	case json.Number:
		parsed, err := value.Int64()
		return parsed, err == nil
	case float64:
		if value != math.Trunc(value) {
			return 0, false
		}
		return int64(value), true
	case int:
		return int64(value), true
	case int64:
		return value, true
	case uint64:
		if value > math.MaxInt64 {
			return 0, false
		}
		return int64(value), true
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func inputUint64(input any) (uint64, bool) {
	switch value := input.(type) {
	case json.Number:
		if parsed, err := value.Int64(); err == nil {
			return uint64(parsed), parsed >= 0
		}
		parsed, err := strconv.ParseUint(value.String(), 10, 64)
		return parsed, err == nil
	case float64:
		if value < 0 || value != math.Trunc(value) {
			return 0, false
		}
		return uint64(value), true
	case int:
		if value < 0 {
			return 0, false
		}
		return uint64(value), true
	case int64:
		if value < 0 {
			return 0, false
		}
		return uint64(value), true
	case uint64:
		return value, true
	case string:
		parsed, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func inputFloat64(input any) (float64, bool) {
	switch value := input.(type) {
	case json.Number:
		parsed, err := value.Float64()
		return parsed, err == nil
	case float64:
		return value, true
	case int64:
		return float64(value), true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}
