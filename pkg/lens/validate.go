package lens

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/stellar/go-stellar-sdk/strkey"
	"github.com/stellar/go-stellar-sdk/xdr"
)

// ErrValidationFailed indicates that input JSON does not conform to the expected schema.
var ErrValidationFailed = errors.New("validation failed")

// ErrInvalidPrecondition indicates transaction preconditions that can never be satisfied.
var ErrInvalidPrecondition = errors.New("invalid precondition")

// ValidationError records a schema violation at a specific path.
type ValidationError struct {
	Path    string
	Message string
	Err     error
}

func (e ValidationError) Error() string {
	var fieldErr *FieldValidationError
	if errors.As(e.Err, &fieldErr) {
		return fieldErr.Error()
	}
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

// Unwrap exposes each violation so errors.As can reach the underlying FieldValidationError.
func (ve ValidationErrors) Unwrap() []error {
	errs := make([]error, len(ve))
	for i, e := range ve {
		errs[i] = e
	}
	return errs
}

// newValidationFieldError builds a ValidationError that carries a FieldValidationError
// naming the offending path, expected schema type, and actual value.
func newValidationFieldError(path, expected string, actual any, message string) ValidationError {
	fieldErr := &FieldValidationError{
		Path:     path,
		Expected: expected,
		Message:  message,
	}
	if actual != nil {
		fieldErr.Actual = FormatActualValue(actual)
	}
	return ValidationError{Path: path, Message: fieldErr.Detail(), Err: fieldErr}
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
			return newValidationFieldError(path, "", nil, "required field is missing or null")
		}
		return nil
	}

	// Type checking
	switch rule.Type {
	case "string":
		strVal, ok := val.(string)
		if !ok {
			errs = append(errs, newValidationFieldError(path, "string", val, ""))
			return errs
		}
		if rule.Pattern != nil && !rule.Pattern.MatchString(strVal) {
			errs = append(errs, newValidationFieldError(path, "string matching "+rule.Pattern.String(), val,
				fmt.Sprintf("string %q does not match pattern %s", strVal, rule.Pattern.String())))
		}
	case "integer":
		if _, err := CoerceInt64(val); err != nil {
			errs = append(errs, newValidationFieldError(path, "integer", val, ""))
			return errs
		}
	case "number":
		switch v := val.(type) {
		case float32, float64, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, json.Number:
			// ok
		case string:
			if _, err := CoerceInt64(v); err != nil {
				errs = append(errs, newValidationFieldError(path, "number", val, ""))
				return errs
			}
		default:
			errs = append(errs, newValidationFieldError(path, "number", val, ""))
			return errs
		}
	case "boolean":
		if _, ok := val.(bool); !ok {
			if _, err := CoerceBool(val); err != nil {
				errs = append(errs, newValidationFieldError(path, "boolean", val, ""))
				return errs
			}
		}
	case "array":
		slice, ok := val.([]any)
		if !ok {
			errs = append(errs, newValidationFieldError(path, "array", val, ""))
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
			errs = append(errs, newValidationFieldError(path, "object", val, ""))
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
					errs = append(errs, newValidationFieldError(childPath, "", nil, fmt.Sprintf("unexpected field %q", origKey)))
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
						errs = append(errs, newValidationFieldError(childPath, "", nil, fmt.Sprintf("missing required field %q", propKey)))
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
			errs = append(errs, newValidationFieldError(path, "", nil, fmt.Sprintf("value %v is not one of allowed enum values %v", val, rule.Enum)))
		}
	}

	// Custom validation hook
	if rule.Custom != nil {
		if err := rule.Custom(val, path); err != nil {
			var fieldErr *FieldValidationError
			if errors.As(err, &fieldErr) {
				errs = append(errs, ValidationError{Path: fieldErr.Path, Message: fieldErr.Detail(), Err: fieldErr})
			} else {
				errs = append(errs, newValidationFieldError(path, "", nil, err.Error()))
			}
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

// ValidatePreconditions checks that transaction preconditions are internally consistent
// and can be satisfied. It rejects reversed time or ledger bounds, where the minimum
// lies beyond an explicit maximum, with an error wrapping ErrInvalidPrecondition.
func ValidatePreconditions(preconds *xdr.Preconditions) error {
	if preconds == nil {
		return nil
	}

	switch preconds.Type {
	case xdr.PreconditionTypePrecondNone:
		return nil
	case xdr.PreconditionTypePrecondTime:
		if preconds.TimeBounds == nil {
			return fmt.Errorf("%w: PRECOND_TIME requires time bounds", ErrInvalidPrecondition)
		}
		return ValidateTimeBounds(preconds.TimeBounds)
	case xdr.PreconditionTypePrecondV2:
		if preconds.V2 == nil {
			return fmt.Errorf("%w: PRECOND_V2 requires a v2 precondition body", ErrInvalidPrecondition)
		}
		if err := ValidateTimeBounds(preconds.V2.TimeBounds); err != nil {
			return err
		}
		return ValidateLedgerBounds(preconds.V2.LedgerBounds)
	default:
		return fmt.Errorf("%w: unrecognized precondition type %d", ErrInvalidPrecondition, int32(preconds.Type))
	}
}

// ValidateTimeBounds checks that min_time does not exceed an explicit max_time.
// A max_time of 0 means the bounds have no upper limit.
func ValidateTimeBounds(tb *xdr.TimeBounds) error {
	if tb == nil {
		return nil
	}
	if tb.MaxTime > 0 && tb.MinTime > tb.MaxTime {
		return fmt.Errorf("%w: time bounds min_time %d exceeds max_time %d", ErrInvalidPrecondition, int64(tb.MinTime), int64(tb.MaxTime))
	}
	return nil
}

// ValidateLedgerBounds checks that min_ledger does not exceed an explicit max_ledger.
// A max_ledger of 0 means the bounds have no upper limit.
func ValidateLedgerBounds(lb *xdr.LedgerBounds) error {
	if lb == nil {
		return nil
	}
	if lb.MaxLedger > 0 && lb.MinLedger > lb.MaxLedger {
		return fmt.Errorf("%w: ledger bounds min_ledger %d exceeds max_ledger %d", ErrInvalidPrecondition, uint32(lb.MinLedger), uint32(lb.MaxLedger))
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

	requireField := func(field string, aliases ...string) error {
		if _, ok := m[field]; ok {
			return nil
		}
		for _, alias := range aliases {
			if _, ok := m[alias]; ok {
				return nil
			}
		}
		return newValidationFieldError(joinPath(path, field), "", nil, "required field is missing or null")
	}

	switch typ {
	case "payment":
		if err := requireField("destination"); err != nil {
			return err
		}
		if err := requireField("amount"); err != nil {
			return err
		}
		amtVal := m["amount"]
		if n, err := CoerceAmount(amtVal); err != nil || n <= 0 {
			return newValidationFieldError(joinPath(path, "amount"), "positive decimal string", amtVal, "")
		}
	case "create_account":
		if err := requireField("destination"); err != nil {
			return err
		}
		if err := requireField("starting_balance", "startingBalance"); err != nil {
			return err
		}
	case "change_trust":
		if err := requireField("line", "asset"); err != nil {
			return err
		}
	case "manage_data":
		if err := requireField("data_name", "dataName", "name"); err != nil {
			return err
		}
	case "bump_sequence":
		if err := requireField("bump_to", "bumpTo"); err != nil {
			return err
		}
	case "account_merge":
		if err := requireField("destination"); err != nil {
			return err
		}
	case "manage_buy_offer":
		if err := requireField("selling"); err != nil {
			return err
		}
		if err := requireField("buying"); err != nil {
			return err
		}
		if err := requireField("buy_amount", "buyAmount"); err != nil {
			return err
		}
		if err := requireField("price"); err != nil {
			return err
		}
	case "manage_sell_offer":
		if err := requireField("selling"); err != nil {
			return err
		}
		if err := requireField("buying"); err != nil {
			return err
		}
		if err := requireField("amount"); err != nil {
			return err
		}
		if err := requireField("price"); err != nil {
			return err
		}
	case "create_passive_sell_offer":
		if err := requireField("selling"); err != nil {
			return err
		}
		if err := requireField("buying"); err != nil {
			return err
		}
		if err := requireField("amount"); err != nil {
			return err
		}
		if err := requireField("price"); err != nil {
			return err
		}
	case "path_payment_strict_receive":
		if err := requireField("destination"); err != nil {
			return err
		}
		if err := requireField("send_max", "sendMax"); err != nil {
			return err
		}
		if err := requireField("dest_amount", "destAmount"); err != nil {
			return err
		}
	case "path_payment_strict_send":
		if err := requireField("destination"); err != nil {
			return err
		}
		if err := requireField("send_amount", "sendAmount"); err != nil {
			return err
		}
		if err := requireField("dest_min", "destMin"); err != nil {
			return err
		}
	case "set_options":
		// All fields are optional in set_options
	case "set_trust_line_flags":
		if err := requireField("trustor"); err != nil {
			return err
		}
		if err := requireField("asset"); err != nil {
			return err
		}
	case "invoke_host_function":
		// Can have invoke_contract, create_contract, or upload_wasm
	case "invoke_contract":
		if err := requireField("contract", "contract_id"); err != nil {
			return err
		}
		if err := requireField("function", "function_name"); err != nil {
			return err
		}
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
