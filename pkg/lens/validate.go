package lens

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"

	"github.com/stellar/go-stellar-sdk/strkey"
	"github.com/stellar/go-stellar-sdk/xdr"
)

// ErrValidationFailed indicates that input JSON does not conform to the expected schema.
var ErrValidationFailed = errors.New("validation failed")

// ValidationError records a schema violation at a specific path.
type ValidationError struct {
	Path    string
	Message string
	Err     error
}

func (e ValidationError) Error() string {
	if e.Path != "" {
		return fmt.Sprintf("validation error at %q: %s", e.Path, e.Message)
	}
	return fmt.Sprintf("validation error: %s", e.Message)
}

func (e ValidationError) Unwrap() error {
	if e.Err != nil {
		return e.Err
	}
	return ErrValidationFailed
}

// ValidationErrors is a collection of schema violations.
type ValidationErrors []ValidationError

func (ve ValidationErrors) Error() string {
	if len(ve) == 0 {
		return "no validation errors"
	}
	if len(ve) == 1 {
		return ve[0].Error()
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d validation errors:\n", len(ve))
	for _, e := range ve {
		sb.WriteString("  - ")
		sb.WriteString(e.Error())
		sb.WriteByte('\n')
	}
	return strings.TrimRight(sb.String(), "\n")
}

func (ve ValidationErrors) Is(target error) bool {
	return errors.Is(target, ErrValidationFailed)
}

// SchemaRule defines validation constraints on a JSON property or element.
type SchemaRule struct {
	// Type specifies the expected primitive type: "string", "number", "integer", "boolean", "object", "array", "any".
	Type string
	// Required indicates if the property must be present and non-nil.
	Required bool
	// DisallowUnknown when set on an "object" rule flags any undeclared keys as errors.
	DisallowUnknown bool
	// Properties defines rules for child fields when Type is "object".
	Properties map[string]*SchemaRule
	// Aliases maps alternate field names (e.g. "sourceAccount" -> "source_account").
	Aliases map[string]string
	// Items defines rules for array elements when Type is "array".
	Items *SchemaRule
	// Enum specifies allowed discrete values.
	Enum []any
	// Pattern validates string values against a regular expression.
	Pattern *regexp.Regexp
	// Custom allows domain-specific validations (e.g. strkey checksums).
	Custom func(val any, path string) error
}

// ValidateJSON parses raw JSON and validates it against the rule.
func ValidateJSON(data []byte, schema *SchemaRule) error {
	if len(data) == 0 {
		return ValidationError{Message: "empty JSON payload", Err: ErrValidationFailed}
	}
	var root any
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.UseNumber()
	if err := dec.Decode(&root); err != nil {
		return ValidationError{Message: fmt.Sprintf("invalid JSON syntax: %v", err), Err: ErrValidationFailed}
	}
	return ValidateValue(root, schema, "")
}

// ValidateValue checks a decoded value against the schema rule.
func ValidateValue(val any, rule *SchemaRule, path string) error {
	var errs ValidationErrors

	if rule == nil {
		return nil
	}

	if val == nil {
		if rule.Required {
			return ValidationError{Path: path, Message: "required field is missing or null", Err: ErrValidationFailed}
		}
		return nil
	}

	// Type checking
	switch rule.Type {
	case "string":
		strVal, ok := val.(string)
		if !ok {
			errs = append(errs, ValidationError{Path: path, Message: fmt.Sprintf("expected string, got %T", val), Err: ErrValidationFailed})
			return errs
		}
		if rule.Pattern != nil && !rule.Pattern.MatchString(strVal) {
			errs = append(errs, ValidationError{Path: path, Message: fmt.Sprintf("string %q does not match pattern %s", strVal, rule.Pattern.String()), Err: ErrValidationFailed})
		}
	case "integer":
		if _, err := CoerceInt64(val); err != nil {
			errs = append(errs, ValidationError{Path: path, Message: fmt.Sprintf("expected integer: %v", err), Err: ErrValidationFailed})
			return errs
		}
	case "number":
		switch v := val.(type) {
		case float32, float64, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, json.Number:
			// ok
		case string:
			if _, err := CoerceInt64(v); err != nil {
				errs = append(errs, ValidationError{Path: path, Message: fmt.Sprintf("expected number: %v", err), Err: ErrValidationFailed})
				return errs
			}
		default:
			errs = append(errs, ValidationError{Path: path, Message: fmt.Sprintf("expected number, got %T", val), Err: ErrValidationFailed})
			return errs
		}
	case "boolean":
		if _, ok := val.(bool); !ok {
			if _, err := CoerceBool(val); err != nil {
				errs = append(errs, ValidationError{Path: path, Message: fmt.Sprintf("expected boolean, got %T", val), Err: ErrValidationFailed})
				return errs
			}
		}
	case "array":
		slice, ok := val.([]any)
		if !ok {
			errs = append(errs, ValidationError{Path: path, Message: fmt.Sprintf("expected array, got %T", val), Err: ErrValidationFailed})
			return errs
		}
		if rule.Items != nil {
			for i, item := range slice {
				elemPath := fmt.Sprintf("%s[%d]", path, i)
				if err := ValidateValue(item, rule.Items, elemPath); err != nil {
					var subErrs ValidationErrors
					var ve ValidationError
					if errors.As(err, &subErrs) {
						errs = append(errs, subErrs...)
					} else if errors.As(err, &ve) {
						errs = append(errs, ve)
					}
				}
			}
		}
	case "object":
		m, ok := val.(map[string]any)
		if !ok {
			errs = append(errs, ValidationError{Path: path, Message: fmt.Sprintf("expected object, got %T", val), Err: ErrValidationFailed})
			return errs
		}

		canonical := make(map[string]any)
		for k, v := range m {
			targetKey := k
			if rule.Aliases != nil {
				if mapped, ok := rule.Aliases[k]; ok {
					targetKey = mapped
				}
			}
			canonical[targetKey] = v
		}

		// Check for unknown fields if strict
		if rule.DisallowUnknown && rule.Properties != nil {
			for k := range m {
				origKey := k
				targetKey := k
				if rule.Aliases != nil {
					if mapped, ok := rule.Aliases[k]; ok {
						targetKey = mapped
					}
				}
				if _, allowed := rule.Properties[targetKey]; !allowed {
					childPath := joinPath(path, origKey)
					errs = append(errs, ValidationError{Path: childPath, Message: fmt.Sprintf("unexpected field %q", origKey), Err: ErrValidationFailed})
				}
			}
		}

		// Validate declared properties
		if rule.Properties != nil {
			for propKey, propRule := range rule.Properties {
				childPath := joinPath(path, propKey)
				propVal, exists := canonical[propKey]
				if !exists || propVal == nil {
					if propRule.Required {
						errs = append(errs, ValidationError{Path: childPath, Message: fmt.Sprintf("missing required field %q", propKey), Err: ErrValidationFailed})
					}
					continue
				}
				if err := ValidateValue(propVal, propRule, childPath); err != nil {
					var subErrs ValidationErrors
					var ve ValidationError
					if errors.As(err, &subErrs) {
						errs = append(errs, subErrs...)
					} else if errors.As(err, &ve) {
						errs = append(errs, ve)
					}
				}
			}
		}
	}

	// Enum checking
	if len(rule.Enum) > 0 {
		matched := false
		for _, e := range rule.Enum {
			if fmt.Sprintf("%v", e) == fmt.Sprintf("%v", val) {
				matched = true
				break
			}
		}
		if !matched {
			errs = append(errs, ValidationError{Path: path, Message: fmt.Sprintf("value %v is not one of allowed enum values %v", val, rule.Enum), Err: ErrValidationFailed})
		}
	}

	// Custom validation hook
	if rule.Custom != nil {
		if err := rule.Custom(val, path); err != nil {
			errs = append(errs, ValidationError{Path: path, Message: err.Error(), Err: ErrValidationFailed})
		}
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}

func joinPath(base, child string) string {
	if base == "" {
		return child
	}
	return base + "." + child
}

// ValidateAccountAddress verifies that a string is a valid Ed25519 account ID (G...) or Muxed account (M...).
func ValidateAccountAddress(v any, path string) error {
	s, ok := v.(string)
	if !ok {
		return fmt.Errorf("account address must be string, got %T", v)
	}
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "G") && len(s) == 56 {
		if _, err := strkey.Decode(strkey.VersionByteAccountID, s); err != nil {
			return fmt.Errorf("invalid Stellar account ID: %w", err)
		}
		return nil
	}
	if strings.HasPrefix(s, "M") {
		if _, err := strkey.DecodeMuxedAccount(s); err != nil {
			return fmt.Errorf("invalid Stellar muxed account: %w", err)
		}
		return nil
	}
	return fmt.Errorf("invalid address %q: must start with 'G' (56 chars) or 'M'", s)
}

// ValidateContractAddress verifies that a string is a valid Soroban contract address (C...).
func ValidateContractAddress(v any, path string) error {
	s, ok := v.(string)
	if !ok {
		return fmt.Errorf("contract address must be string, got %T", v)
	}
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "C") && len(s) == 56 {
		if _, err := strkey.Decode(strkey.VersionByteContract, s); err != nil {
			return fmt.Errorf("invalid Soroban contract address: %w", err)
		}
		return nil
	}
	return fmt.Errorf("invalid contract address %q: must start with 'C' (56 chars)", s)
}

// ValidateNonNegativeNumber verifies a number is >= 0.
func ValidateNonNegativeNumber(v any, path string) error {
	n, err := CoerceInt64(v)
	if err != nil {
		return err
	}
	if n < 0 {
		return fmt.Errorf("value %d must be greater than or equal to 0", n)
	}
	return nil
}

// ValidatePositiveAmount verifies an amount is > 0.
func ValidatePositiveAmount(v any, path string) error {
	n, err := CoerceAmount(v)
	if err != nil {
		return err
	}
	if n <= 0 {
		return fmt.Errorf("amount %d must be positive (> 0)", n)
	}
	return nil
}

// ValidateNonNegativeAmount verifies an amount is >= 0.
func ValidateNonNegativeAmount(v any, path string) error {
	n, err := CoerceAmount(v)
	if err != nil {
		return err
	}
	if n < 0 {
		return fmt.Errorf("amount %d must be greater than or equal to 0", n)
	}
	return nil
}

// StrictTxSpecSchema returns a strict validation schema for TransactionEnvelope JSON templates.
func StrictTxSpecSchema() *SchemaRule {
	return &SchemaRule{
		Type:            "object",
		DisallowUnknown: true,
		Aliases: map[string]string{
			"sourceAccount":  "source_account",
			"source":         "source_account",
			"seqNum":         "sequence",
			"sequenceNumber": "sequence",
			"timeBounds":     "time_bounds",
			"ledgerBounds":   "ledger_bounds",
			"sorobanData":    "soroban_data",
			"extraSigners":   "extra_signers",
			"feeBump":        "fee_bump",
		},
		Properties: map[string]*SchemaRule{
			"source_account": {
				Type:     "string",
				Required: true,
				Custom:   ValidateAccountAddress,
			},
			"fee": {
				Type:     "number",
				Required: true,
				Custom:   ValidateNonNegativeNumber,
			},
			"sequence": {
				Type:     "integer",
				Required: true,
				Custom:   ValidateNonNegativeNumber,
			},
			"memo": {
				Type: "any",
			},
			"time_bounds": {
				Type:            "object",
				DisallowUnknown: true,
				Aliases: map[string]string{
					"minTime": "min",
					"maxTime": "max",
				},
				Properties: map[string]*SchemaRule{
					"min": {Type: "integer", Custom: ValidateNonNegativeNumber},
					"max": {Type: "integer", Custom: ValidateNonNegativeNumber},
				},
			},
			"ledger_bounds": {
				Type:            "object",
				DisallowUnknown: true,
				Properties: map[string]*SchemaRule{
					"min": {Type: "integer", Custom: ValidateNonNegativeNumber},
					"max": {Type: "integer", Custom: ValidateNonNegativeNumber},
				},
			},
			"min_seq_num": {
				Type:   "integer",
				Custom: ValidateNonNegativeNumber,
			},
			"min_seq_age": {
				Type:   "integer",
				Custom: ValidateNonNegativeNumber,
			},
			"min_seq_ledger_gap": {
				Type:   "integer",
				Custom: ValidateNonNegativeNumber,
			},
			"extra_signers": {
				Type:  "array",
				Items: &SchemaRule{Type: "string"},
			},
			"operations": {
				Type:     "array",
				Required: true,
				Items:    OperationSpecSchema(),
			},
			"signatures": {
				Type: "array",
				Items: &SchemaRule{
					Type:            "object",
					DisallowUnknown: true,
					Properties: map[string]*SchemaRule{
						"hint":      {Type: "string"},
						"signature": {Type: "string", Required: true},
					},
				},
			},
			"soroban_data": {
				Type: "object",
			},
			"type": {
				Type: "string",
				Enum: []any{"v1", "v0", "fee_bump"},
			},
			"fee_bump": {
				Type:            "object",
				DisallowUnknown: true,
				Aliases: map[string]string{
					"feeSource": "fee_source",
					"maxFee":    "max_fee",
				},
				Properties: map[string]*SchemaRule{
					"fee_source": {Type: "string", Required: true, Custom: ValidateAccountAddress},
					"max_fee":    {Type: "number", Required: true, Custom: ValidateNonNegativeNumber},
					"inner_tx":   {Type: "any"},
				},
			},
		},
	}
}

// OperationSpecSchema returns a schema that validates individual operations.
func OperationSpecSchema() *SchemaRule {
	return &SchemaRule{
		Type:            "object",
		DisallowUnknown: false,
		Aliases: map[string]string{
			"sourceAccount":   "source_account",
			"source":          "source_account",
			"startingBalance": "starting_balance",
			"destAsset":       "dest_asset",
			"sendAsset":       "send_asset",
			"destMin":         "dest_min",
			"destAmount":      "dest_amount",
			"sendMax":         "send_max",
			"sendAmount":      "send_amount",
			"buyAmount":       "buy_amount",
			"offerId":         "offer_id",
			"dataName":        "data_name",
			"dataValue":       "data_value",
			"name":            "data_name",
			"value":           "data_value",
			"inflationDest":   "inflation_dest",
			"setFlags":        "set_flags",
			"clearFlags":      "clear_flags",
			"masterWeight":    "master_weight",
			"lowThreshold":    "low_threshold",
			"medThreshold":    "med_threshold",
			"highThreshold":   "high_threshold",
			"homeDomain":      "home_domain",
			"functionName":    "function_name",
			"contractId":      "contract_id",
		},
		Properties: map[string]*SchemaRule{
			"type": {
				Type:     "string",
				Required: true,
			},
			"source_account": {
				Type:   "string",
				Custom: ValidateAccountAddress,
			},
		},
		Custom: validateOperationDetails,
	}
}

// ChangeTrustOpSchema returns the validation schema for ChangeTrust operations.
func ChangeTrustOpSchema() *SchemaRule {
	return &SchemaRule{
		Type: "object",
		Aliases: map[string]string{
			"sourceAccount": "source_account",
			"source":        "source_account",
		},
		Properties: map[string]*SchemaRule{
			"type": {
				Type:     "string",
				Required: true,
				Enum:     []any{"change_trust"},
			},
			"source_account": {
				Type:   "string",
				Custom: ValidateAccountAddress,
			},
			"line": {
				Type:   "any",
				Custom: ValidateChangeTrustLine,
			},
			"asset": {
				Type:   "any",
				Custom: ValidateChangeTrustLine,
			},
			"limit": {
				Type:   "any",
				Custom: ValidateNonNegativeAmount,
			},
		},
		Custom: func(val any, path string) error {
			m, ok := val.(map[string]any)
			if !ok {
				return nil
			}
			if m["line"] == nil && m["asset"] == nil {
				return fmt.Errorf("change_trust operation requires 'line' or 'asset'")
			}
			return nil
		},
	}
}

// ValidateChangeTrustLine validates an asset or liquidity pool share specification for ChangeTrust.
func ValidateChangeTrustLine(v any, path string) error {
	if v == nil {
		return fmt.Errorf("change_trust line/asset cannot be nil")
	}
	if _, err := CoerceChangeTrustAsset(v); err != nil {
		return err
	}
	return nil
}

// ParsePriceFraction parses a fraction string ("3/2"), decimal string ("1.5"),
// float, integer, or {n, d} object into a canonical xdr.Price.
func ParsePriceFraction(v any) (xdr.Price, error) {
	var zero xdr.Price
	if v == nil {
		return zero, fmt.Errorf("%w: cannot coerce nil to Price", ErrCoercionFailed)
	}

	switch val := v.(type) {
	case xdr.Price:
		if val.N <= 0 || val.D <= 0 {
			return zero, fmt.Errorf("%w: price numerator and denominator must be positive, got %d/%d", ErrCoercionFailed, val.N, val.D)
		}
		return canonicalPrice(int64(val.N), int64(val.D))
	case *xdr.Price:
		if val == nil {
			return zero, fmt.Errorf("%w: nil *xdr.Price", ErrCoercionFailed)
		}
		return ParsePriceFraction(*val)
	case json.Number:
		return ParsePriceFraction(string(val))
	case string:
		s := cleanNumberString(val)
		if s == "" {
			return zero, fmt.Errorf("%w: empty price string", ErrCoercionFailed)
		}
		if strings.Contains(s, "/") {
			parts := strings.Split(s, "/")
			if len(parts) != 2 {
				return zero, fmt.Errorf("%w: invalid price fraction %q", ErrCoercionFailed, val)
			}
			n, err1 := CoerceInt64(parts[0])
			d, err2 := CoerceInt64(parts[1])
			if err1 != nil || err2 != nil {
				return zero, fmt.Errorf("%w: invalid price fraction %q", ErrCoercionFailed, val)
			}
			if d == 0 {
				return zero, fmt.Errorf("%w: division by zero in price", ErrCoercionFailed)
			}
			return canonicalPrice(n, d)
		}
		if p, ok, err := decimalStringToPrice(s); ok {
			return p, err
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return zero, fmt.Errorf("%w: invalid price string %q: %w", ErrCoercionFailed, val, err)
		}
		return floatToCanonicalPrice(f)
	case float32:
		return floatToCanonicalPrice(float64(val))
	case float64:
		return floatToCanonicalPrice(val)
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		n, err := CoerceInt64(val)
		if err != nil {
			return zero, err
		}
		return canonicalPrice(n, 1)
	case map[string]any:
		nVal, ok1 := val["n"]
		dVal, ok2 := val["d"]
		if !ok1 || !ok2 {
			nVal, ok1 = val["numerator"]
			dVal, ok2 = val["denominator"]
		}
		if !ok1 || !ok2 {
			return zero, fmt.Errorf("%w: missing numerator/n or denominator/d in price map", ErrCoercionFailed)
		}
		n, err1 := CoerceInt64(nVal)
		d, err2 := CoerceInt64(dVal)
		if err1 != nil || err2 != nil {
			return zero, fmt.Errorf("%w: invalid numerator or denominator in price", ErrCoercionFailed)
		}
		if d == 0 {
			return zero, fmt.Errorf("%w: denominator cannot be zero", ErrCoercionFailed)
		}
		return canonicalPrice(n, d)
	default:
		return zero, fmt.Errorf("%w: unsupported type %T for Price", ErrCoercionFailed, v)
	}
}

func decimalStringToPrice(s string) (xdr.Price, bool, error) {
	if strings.ContainsAny(s, "eE") {
		return xdr.Price{}, false, nil
	}
	parts := strings.Split(s, ".")
	if len(parts) > 2 {
		return xdr.Price{}, true, fmt.Errorf("%w: invalid decimal price %q", ErrCoercionFailed, s)
	}
	if len(parts) == 1 {
		n, err := CoerceInt64(parts[0])
		if err != nil {
			return xdr.Price{}, false, nil
		}
		p, err := canonicalPrice(n, 1)
		return p, true, err
	}
	intPart := parts[0]
	fracPart := strings.TrimRight(parts[1], "0")
	if intPart == "" {
		intPart = "0"
	}
	if strings.HasPrefix(intPart, "-") {
		return xdr.Price{}, true, fmt.Errorf("%w: price must be positive, got %q", ErrCoercionFailed, s)
	}
	intPart = strings.TrimPrefix(intPart, "+")
	if fracPart == "" {
		n, err := CoerceInt64(intPart)
		if err != nil {
			return xdr.Price{}, false, nil
		}
		p, err := canonicalPrice(n, 1)
		return p, true, err
	}
	digits := intPart + fracPart
	numBig, ok := new(big.Int).SetString(digits, 10)
	if !ok {
		return xdr.Price{}, false, nil
	}
	if numBig.Sign() <= 0 {
		return xdr.Price{}, true, fmt.Errorf("%w: price must be positive, got %q", ErrCoercionFailed, s)
	}
	denBig := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(len(fracPart))), nil)
	g := new(big.Int).GCD(nil, nil, numBig, denBig)
	numBig.Div(numBig, g)
	denBig.Div(denBig, g)
	max32 := big.NewInt(math.MaxInt32)
	if numBig.Cmp(max32) <= 0 && denBig.Cmp(max32) <= 0 {
		return xdr.Price{N: xdr.Int32(numBig.Int64()), D: xdr.Int32(denBig.Int64())}, true, nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return xdr.Price{}, true, fmt.Errorf("%w: invalid decimal price %q: %w", ErrCoercionFailed, s, err)
	}
	p, err := floatToCanonicalPrice(f)
	return p, true, err
}

func canonicalPrice(n, d int64) (xdr.Price, error) {
	if d == 0 {
		return xdr.Price{}, fmt.Errorf("%w: division by zero in price", ErrCoercionFailed)
	}
	if d < 0 {
		n = -n
		d = -d
	}
	if n <= 0 {
		return xdr.Price{}, fmt.Errorf("%w: price must be positive, got %d/%d", ErrCoercionFailed, n, d)
	}
	g := gcdInt64(n, d)
	n /= g
	d /= g
	if n > math.MaxInt32 || d > math.MaxInt32 {
		return xdr.Price{}, fmt.Errorf("%w: price %d/%d exceeds int32 bounds", ErrCoercionFailed, n, d)
	}
	return xdr.Price{N: xdr.Int32(n), D: xdr.Int32(d)}, nil
}

func floatToCanonicalPrice(f float64) (xdr.Price, error) {
	if f <= 0 || math.IsNaN(f) || math.IsInf(f, 0) {
		return xdr.Price{}, fmt.Errorf("%w: price must be a positive finite number, got %v", ErrCoercionFailed, f)
	}
	d := int64(1)
	for d <= 1000000 {
		n := math.Round(f * float64(d))
		if n > 0 && math.Abs(n/float64(d)-f) < 1e-9 && n <= math.MaxInt32 {
			return canonicalPrice(int64(n), d)
		}
		d *= 10
	}
	n := int64(math.Round(f * 1000000))
	if n <= 0 {
		return xdr.Price{}, fmt.Errorf("%w: price underflows int32 representation: %v", ErrCoercionFailed, f)
	}
	return canonicalPrice(n, 1000000)
}

func gcdInt64(a, b int64) int64 {
	if a < 0 {
		a = -a
	}
	if b < 0 {
		b = -b
	}
	for b != 0 {
		a, b = b, a%b
	}
	if a == 0 {
		return 1
	}
	return a
}

func validateOperationDetails(val any, path string) error {
	m, ok := val.(map[string]any)
	if !ok {
		return nil
	}
	typ, ok := m["type"].(string)
	if !ok {
		return nil
	}
	typ = strings.ToLower(strings.TrimSpace(typ))

	switch typ {
	case "payment":
		destination, ok := m["destination"]
		if !ok {
			return fmt.Errorf("payment operation requires 'destination'")
		}
		if err := ValidateAccountAddress(destination, path+".destination"); err != nil {
			return err
		}
		amount, ok := m["amount"]
		if !ok {
			return fmt.Errorf("payment operation requires 'amount'")
		}
		if _, err := CoerceAmount(amount); err != nil {
			return fmt.Errorf("payment amount: %w", err)
		}
		if asset, ok := m["asset"]; ok && asset != nil {
			if _, err := CoerceAsset(asset); err != nil {
				return fmt.Errorf("payment asset: %w", err)
			}
		}
	case "create_account":
		if _, ok := m["destination"]; !ok {
			return fmt.Errorf("create_account operation requires 'destination'")
		}
		if _, ok := m["starting_balance"]; !ok {
			if _, ok := m["startingBalance"]; !ok {
				return fmt.Errorf("create_account operation requires 'starting_balance'")
			}
		}
	case "change_trust":
		lineVal := m["line"]
		if lineVal == nil {
			lineVal = m["asset"]
		}
		if lineVal == nil {
			return fmt.Errorf("change_trust operation requires 'line' or 'asset'")
		}
		if err := ValidateChangeTrustLine(lineVal, joinPath(path, "line")); err != nil {
			return err
		}
		if lim, exists := m["limit"]; exists && lim != nil {
			if err := ValidateNonNegativeAmount(lim, joinPath(path, "limit")); err != nil {
				return err
			}
		}
	case "manage_data":
		nameVal := m["data_name"]
		if nameVal == nil {
			nameVal = m["dataName"]
		}
		if nameVal == nil {
			nameVal = m["name"]
		}
		if nameVal == nil {
			return fmt.Errorf("manage_data operation requires 'data_name'")
		}
		nameStr, ok := nameVal.(string)
		if !ok {
			return fmt.Errorf("manage_data 'name' must be a string, got %T", nameVal)
		}
		if err := ValidateDataName(nameStr); err != nil {
			return err
		}
	case "bump_sequence":
		if _, ok := m["bump_to"]; !ok {
			if _, ok := m["bumpTo"]; !ok {
				return fmt.Errorf("bump_sequence operation requires 'bump_to'")
			}
		}
	case "account_merge":
		if _, ok := m["destination"]; !ok {
			return fmt.Errorf("account_merge operation requires 'destination'")
		}
	case "manage_buy_offer":
		if _, ok := m["selling"]; !ok {
			return fmt.Errorf("manage_buy_offer operation requires 'selling'")
		}
		if _, ok := m["buying"]; !ok {
			return fmt.Errorf("manage_buy_offer operation requires 'buying'")
		}
		buyAmt := m["buy_amount"]
		if buyAmt == nil {
			buyAmt = m["buyAmount"]
		}
		if buyAmt == nil {
			buyAmt = m["amount"]
		}
		if buyAmt == nil {
			return fmt.Errorf("manage_buy_offer operation requires 'buy_amount'")
		}
		if err := ValidateNonNegativeAmount(buyAmt, joinPath(path, "buy_amount")); err != nil {
			return err
		}
		priceVal, ok := m["price"]
		if !ok || priceVal == nil {
			return fmt.Errorf("manage_buy_offer operation requires 'price'")
		}
		if _, err := ParsePriceFraction(priceVal); err != nil {
			return err
		}
	case "manage_sell_offer":
		if _, ok := m["selling"]; !ok {
			return fmt.Errorf("manage_sell_offer operation requires 'selling'")
		}
		if _, ok := m["buying"]; !ok {
			return fmt.Errorf("manage_sell_offer operation requires 'buying'")
		}
		amtVal, ok := m["amount"]
		if !ok || amtVal == nil {
			return fmt.Errorf("manage_sell_offer operation requires 'amount'")
		}
		if err := ValidateNonNegativeAmount(amtVal, joinPath(path, "amount")); err != nil {
			return err
		}
		priceVal, ok := m["price"]
		if !ok || priceVal == nil {
			return fmt.Errorf("manage_sell_offer operation requires 'price'")
		}
		if _, err := ParsePriceFraction(priceVal); err != nil {
			return err
		}
	case "create_passive_sell_offer":
		if _, ok := m["selling"]; !ok {
			return fmt.Errorf("create_passive_sell_offer operation requires 'selling'")
		}
		if _, ok := m["buying"]; !ok {
			return fmt.Errorf("create_passive_sell_offer operation requires 'buying'")
		}
		if _, ok := m["amount"]; !ok {
			return fmt.Errorf("create_passive_sell_offer operation requires 'amount'")
		}
		priceVal, ok := m["price"]
		if !ok || priceVal == nil {
			return fmt.Errorf("create_passive_sell_offer operation requires 'price'")
		}
		if _, err := ParsePriceFraction(priceVal); err != nil {
			return err
		}
	case "path_payment_strict_receive":
		if _, ok := m["destination"]; !ok {
			return fmt.Errorf("path_payment_strict_receive operation requires 'destination'")
		}
		if _, ok := m["send_max"]; !ok && m["sendMax"] == nil {
			return fmt.Errorf("path_payment_strict_receive operation requires 'send_max'")
		}
		if _, ok := m["dest_amount"]; !ok && m["destAmount"] == nil {
			return fmt.Errorf("path_payment_strict_receive operation requires 'dest_amount'")
		}
	case "path_payment_strict_send":
		if _, ok := m["destination"]; !ok {
			return fmt.Errorf("path_payment_strict_send operation requires 'destination'")
		}
		if _, ok := m["send_amount"]; !ok && m["sendAmount"] == nil {
			return fmt.Errorf("path_payment_strict_send operation requires 'send_amount'")
		}
		if _, ok := m["dest_min"]; !ok && m["destMin"] == nil {
			return fmt.Errorf("path_payment_strict_send operation requires 'dest_min'")
		}
	case "set_options":
		if inf := firstPresent(m, "inflation_dest", "inflationDest"); inf != nil {
			if err := ValidateAccountAddress(inf, joinPath(path, "inflation_dest")); err != nil {
				return err
			}
		}
		for _, pair := range [][2]string{
			{"master_weight", "masterWeight"},
			{"low_threshold", "lowThreshold"},
			{"med_threshold", "medThreshold"},
			{"high_threshold", "highThreshold"},
		} {
			if v := firstPresent(m, pair[0], pair[1]); v != nil {
				if err := ValidateThresholdRange(v, joinPath(path, pair[0])); err != nil {
					return err
				}
			}
		}
		if hd := firstPresent(m, "home_domain", "homeDomain"); hd != nil {
			s, ok := hd.(string)
			if !ok {
				return fmt.Errorf("home_domain must be string, got %T", hd)
			}
			if len(s) > 32 {
				return fmt.Errorf("home_domain exceeds 32 bytes (%d)", len(s))
			}
		}
		if sig, ok := m["signer"]; ok && sig != nil {
			if _, err := ParseSigner(sig); err != nil {
				return err
			}
		}
	case "set_trust_line_flags":
		if _, ok := m["trustor"]; !ok {
			return fmt.Errorf("set_trust_line_flags operation requires 'trustor'")
		}
		if _, ok := m["asset"]; !ok {
			return fmt.Errorf("set_trust_line_flags operation requires 'asset'")
		}
	case "invoke_host_function":
		// Can have invoke_contract, create_contract, or upload_wasm
	}
	return nil
}

// StrictScValSpecSchema returns a validation rule for ScVal JSON literals.
func StrictScValSpecSchema() *SchemaRule {
	return &SchemaRule{
		Type: "any",
		Custom: func(val any, path string) error {
			if val == nil {
				return nil
			}
			m, ok := val.(map[string]any)
			if !ok {
				// Primitive literal (bool, number, string, array)
				return nil
			}
			// Tagged object
			if typVal, ok := m["type"]; ok {
				typStr, ok := typVal.(string)
				if !ok {
					return fmt.Errorf("expected string for 'type' discriminant in ScVal, got %T", typVal)
				}
				typLower := strings.ToLower(typStr)
				switch typLower {
				case "bool", "void", "u32", "i32", "u64", "i64", "timepoint", "duration",
					"u128", "i128", "u256", "i256", "bytes", "string", "symbol", "vec", "map", "address", "contract_instance", "error":
					// valid
				default:
					return fmt.Errorf("unsupported ScVal type %q", typStr)
				}
			}
			return nil
		},
	}
}
