package lens

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/stellar/go-stellar-sdk/strkey"
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
		if _, ok := m["line"]; !ok {
			if _, ok := m["asset"]; !ok {
				return fmt.Errorf("change_trust operation requires 'line' or 'asset'")
			}
		}
	case "manage_data":
		if _, ok := m["data_name"]; !ok {
			if _, ok := m["dataName"]; !ok {
				if _, ok := m["name"]; !ok {
					return fmt.Errorf("manage_data operation requires 'data_name'")
				}
			}
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
		if _, ok := m["buy_amount"]; !ok && m["buyAmount"] == nil {
			return fmt.Errorf("manage_buy_offer operation requires 'buy_amount'")
		}
		if _, ok := m["price"]; !ok {
			return fmt.Errorf("manage_buy_offer operation requires 'price'")
		}
	case "manage_sell_offer":
		if _, ok := m["selling"]; !ok {
			return fmt.Errorf("manage_sell_offer operation requires 'selling'")
		}
		if _, ok := m["buying"]; !ok {
			return fmt.Errorf("manage_sell_offer operation requires 'buying'")
		}
		if _, ok := m["amount"]; !ok {
			return fmt.Errorf("manage_sell_offer operation requires 'amount'")
		}
		if _, ok := m["price"]; !ok {
			return fmt.Errorf("manage_sell_offer operation requires 'price'")
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
		if _, ok := m["price"]; !ok {
			return fmt.Errorf("create_passive_sell_offer operation requires 'price'")
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
		// All fields are optional in set_options
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
