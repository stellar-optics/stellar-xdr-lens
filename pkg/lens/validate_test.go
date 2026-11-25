package lens

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/strkey"
	"github.com/stellar/go-stellar-sdk/xdr"
)

func TestValidateJSONBasicTypes(t *testing.T) {
	// String with regex pattern
	rule := &SchemaRule{
		Type:    "string",
		Pattern: regexp.MustCompile(`^[a-z]+$`),
	}
	if err := ValidateJSON([]byte(`"hello"`), rule); err != nil {
		t.Fatalf("expected valid string, got %v", err)
	}
	if err := ValidateJSON([]byte(`"Hello123"`), rule); err == nil {
		t.Fatalf("expected pattern mismatch error")
	}
	if err := ValidateJSON([]byte(`123`), rule); err == nil {
		t.Fatalf("expected type mismatch error")
	}

	// Integer & Number
	intRule := &SchemaRule{Type: "integer"}
	if err := ValidateJSON([]byte(`42`), intRule); err != nil {
		t.Fatalf("expected valid integer: %v", err)
	}
	if err := ValidateJSON([]byte(`"invalid"`), intRule); err == nil {
		t.Fatalf("expected invalid integer error")
	}

	numRule := &SchemaRule{Type: "number"}
	if err := ValidateJSON([]byte(`42.5`), numRule); err != nil {
		t.Fatalf("expected valid number: %v", err)
	}
	if err := ValidateJSON([]byte(`"42"`), numRule); err != nil {
		t.Fatalf("expected valid stringified number: %v", err)
	}
	if err := ValidateJSON([]byte(`[1, 2]`), numRule); err == nil {
		t.Fatalf("expected invalid number error for array")
	}

	// Boolean
	boolRule := &SchemaRule{Type: "boolean"}
	if err := ValidateJSON([]byte(`true`), boolRule); err != nil {
		t.Fatalf("expected valid boolean: %v", err)
	}
	if err := ValidateJSON([]byte(`"invalid"`), boolRule); err == nil {
		t.Fatalf("expected invalid boolean error")
	}

	// Array with Items
	arrRule := &SchemaRule{
		Type:  "array",
		Items: &SchemaRule{Type: "integer"},
	}
	if err := ValidateJSON([]byte(`[1, 2, 3]`), arrRule); err != nil {
		t.Fatalf("expected valid array: %v", err)
	}
	if err := ValidateJSON([]byte(`[1, "notanint"]`), arrRule); err == nil {
		t.Fatalf("expected array item error")
	}
	if err := ValidateJSON([]byte(`{"a": 1}`), arrRule); err == nil {
		t.Fatalf("expected array type mismatch")
	}

	// Empty JSON & Syntax Error
	if err := ValidateJSON([]byte(``), rule); err == nil {
		t.Fatalf("expected empty JSON error")
	}
	if err := ValidateJSON([]byte(`{invalid`), rule); err == nil {
		t.Fatalf("expected syntax error")
	}
}

func TestValidateStrictTxSpec(t *testing.T) {
	schema := StrictTxSpecSchema()
	validAccount := "GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAW"

	validTxJSON := []byte(`{
		"source_account": "` + validAccount + `",
		"fee": 100,
		"sequence": 123456,
		"operations": [
			{
				"type": "payment",
				"destination": "` + validAccount + `",
				"asset": "native",
				"amount": 10000000
			}
		]
	}`)

	if err := ValidateJSON(validTxJSON, schema); err != nil {
		t.Fatalf("valid tx JSON failed validation: %v", err)
	}

	// Unknown field (strict check)
	unknownFieldJSON := []byte(`{
		"source_account": "` + validAccount + `",
		"fee": 100,
		"sequence": 123456,
		"unknown_field": "disallowed",
		"operations": []
	}`)
	err := ValidateJSON(unknownFieldJSON, schema)
	if err == nil {
		t.Fatalf("expected strict validation to reject unknown field")
	}
	if !errors.Is(err, ErrValidationFailed) {
		t.Fatalf("expected errors.Is(err, ErrValidationFailed)")
	}

	// Missing required field
	missingFeeJSON := []byte(`{
		"source_account": "` + validAccount + `",
		"sequence": 123456,
		"operations": []
	}`)
	if err := ValidateJSON(missingFeeJSON, schema); err == nil {
		t.Fatalf("expected error for missing fee")
	}

	// Negative fee
	negativeFeeJSON := []byte(`{
		"source_account": "` + validAccount + `",
		"fee": -10,
		"sequence": 123456,
		"operations": []
	}`)
	if err := ValidateJSON(negativeFeeJSON, schema); err == nil {
		t.Fatalf("expected error for negative fee")
	}

	// Invalid account
	badAccountJSON := []byte(`{
		"source_account": "invalid_address",
		"fee": 100,
		"sequence": 123456,
		"operations": []
	}`)
	if err := ValidateJSON(badAccountJSON, schema); err == nil {
		t.Fatalf("expected error for invalid address")
	}
}

func TestValidateOperations(t *testing.T) {
	opSchema := OperationSpecSchema()
	validAccount := "GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAW"

	// Payment without destination
	badPayment := []byte(`{
		"type": "payment",
		"amount": 100
	}`)
	if err := ValidateJSON(badPayment, opSchema); err == nil {
		t.Fatalf("expected error for payment without destination")
	}

	// Create account without starting balance
	badCreate := []byte(`{
		"type": "create_account",
		"destination": "` + validAccount + `"
	}`)
	if err := ValidateJSON(badCreate, opSchema); err == nil {
		t.Fatalf("expected error for create_account without starting_balance")
	}

	// Change trust without line
	badChangeTrust := []byte(`{
		"type": "change_trust"
	}`)
	if err := ValidateJSON(badChangeTrust, opSchema); err == nil {
		t.Fatalf("expected error for change_trust without line")
	}

	// Manage data without data_name
	badManageData := []byte(`{
		"type": "manage_data"
	}`)
	if err := ValidateJSON(badManageData, opSchema); err == nil {
		t.Fatalf("expected error for manage_data without data_name")
	}

	// Bump sequence without bump_to
	badBump := []byte(`{
		"type": "bump_sequence"
	}`)
	if err := ValidateJSON(badBump, opSchema); err == nil {
		t.Fatalf("expected error for bump_sequence without bump_to")
	}

	// Account merge without destination
	badMerge := []byte(`{
		"type": "account_merge"
	}`)
	if err := ValidateJSON(badMerge, opSchema); err == nil {
		t.Fatalf("expected error for account_merge without destination")
	}

	// Manage buy offer without buying/selling
	badBuyOffer := []byte(`{
		"type": "manage_buy_offer",
		"selling": "native"
	}`)
	if err := ValidateJSON(badBuyOffer, opSchema); err == nil {
		t.Fatalf("expected error for manage_buy_offer missing fields")
	}

	// Path payment strict receive without destination
	badPathReceive := []byte(`{
		"type": "path_payment_strict_receive",
		"send_max": 100,
		"dest_amount": 90
	}`)
	if err := ValidateJSON(badPathReceive, opSchema); err == nil {
		t.Fatalf("expected error for path_payment_strict_receive missing destination")
	}

	// Set trust line flags without trustor
	badTrustFlags := []byte(`{
		"type": "set_trust_line_flags",
		"asset": "USDC:GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAW"
	}`)
	if err := ValidateJSON(badTrustFlags, opSchema); err == nil {
		t.Fatalf("expected error for set_trust_line_flags missing trustor")
	}
}

func TestValidateScValSpec(t *testing.T) {
	schema := StrictScValSpecSchema()

	// Valid tagged ScVal
	validScVal := []byte(`{"type": "u32", "value": 100}`)
	if err := ValidateJSON(validScVal, schema); err != nil {
		t.Fatalf("valid ScVal failed: %v", err)
	}

	// Valid primitive literal
	if err := ValidateJSON([]byte(`100`), schema); err != nil {
		t.Fatalf("primitive ScVal failed: %v", err)
	}

	// Invalid ScVal type
	invalidScVal := []byte(`{"type": "unsupported_type", "value": 100}`)
	if err := ValidateJSON(invalidScVal, schema); err == nil {
		t.Fatalf("expected error on unsupported ScVal type")
	}

	// Non-string type discriminant
	badType := []byte(`{"type": 123}`)
	if err := ValidateJSON(badType, schema); err == nil {
		t.Fatalf("expected error on non-string type discriminant")
	}
}

func TestValidationErrorsFormatting(t *testing.T) {
	var errs ValidationErrors
	if errs.Error() != "no validation errors" {
		t.Errorf("expected 'no validation errors', got %s", errs.Error())
	}
	errs = append(errs, ValidationError{Path: "field1", Message: "is bad"})
	if errs.Error() != `validation error at "field1": is bad` {
		t.Errorf("unexpected error string: %s", errs.Error())
	}
	errs = append(errs, ValidationError{Path: "field2", Message: "is missing"})
	if len(errs) != 2 {
		t.Errorf("expected 2 errors")
	}
	_ = errs.Error()

	ve := ValidationError{Message: "root error"}
	if ve.Error() != "validation error: root error" {
		t.Errorf("unexpected error without path: %s", ve.Error())
	}
	if !errors.Is(ve.Unwrap(), ErrValidationFailed) {
		t.Errorf("expected Unwrap to yield ErrValidationFailed")
	}

	veWrapped := ValidationError{Message: "wrapped error", Err: errors.New("underlying")}
	if veWrapped.Unwrap().Error() != "underlying" {
		t.Errorf("expected wrapped error")
	}
}

func TestValidateContractAddressAndPositiveAmount(t *testing.T) {
	// Contract address
	validContract, err := strkey.Encode(strkey.VersionByteContract, make([]byte, 32))
	if err != nil {
		t.Fatalf("failed to encode contract address: %v", err)
	}
	if err := ValidateContractAddress(validContract, "contract"); err != nil {
		t.Errorf("expected valid contract address: %v", err)
	}
	if err := ValidateContractAddress("invalid", "contract"); err == nil {
		t.Errorf("expected error on invalid contract address")
	}
	if err := ValidateContractAddress(12345, "contract"); err == nil {
		t.Errorf("expected error on non-string contract address")
	}

	// Account address non-string and Muxed
	if err := ValidateAccountAddress(12345, "account"); err == nil {
		t.Errorf("expected error on non-string account address")
	}
	validAccount := "GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAW"
	muxExpected := xdr.MuxedAccount{
		Type: xdr.CryptoKeyTypeKeyTypeMuxedEd25519,
		Med25519: &xdr.MuxedAccountMed25519{
			Id: 100,
		},
	}
	acc, _ := CoerceAccountID(validAccount)
	muxExpected.Med25519.Ed25519 = *acc.Ed25519
	validMux := muxExpected.Address()
	if err := ValidateAccountAddress(validMux, "account"); err != nil {
		t.Errorf("expected valid muxed account: %v", err)
	}

	// Positive Amount
	if err := ValidatePositiveAmount("100", "amt"); err != nil {
		t.Errorf("expected valid positive amount: %v", err)
	}
	if err := ValidatePositiveAmount(0, "amt"); err == nil {
		t.Errorf("expected error on zero amount")
	}
	if err := ValidatePositiveAmount(-5, "amt"); err == nil {
		t.Errorf("expected error on negative amount")
	}
	if err := ValidatePositiveAmount("invalid", "amt"); err == nil {
		t.Errorf("expected error on invalid amount")
	}

	// Enum Rule and Non-Required Nil
	enumRule := &SchemaRule{
		Type: "string",
		Enum: []any{"apple", "banana"},
	}
	if err := ValidateValue("apple", enumRule, "fruit"); err != nil {
		t.Errorf("expected valid enum value: %v", err)
	}
	if err := ValidateValue("orange", enumRule, "fruit"); err == nil {
		t.Errorf("expected error on unknown enum value")
	}
	if err := ValidateValue(nil, enumRule, "fruit"); err != nil {
		t.Errorf("expected nil without required to pass: %v", err)
	}

	// Object type mismatch
	objRule := &SchemaRule{Type: "object"}
	if err := ValidateValue("not_an_object", objRule, "obj"); err == nil {
		t.Errorf("expected error on object type mismatch")
	}

	// NonNegativeNumber error
	if err := ValidateNonNegativeNumber("invalid", "num"); err == nil {
		t.Errorf("expected error on non-numeric value")
	}
}

func TestValidatePreconditions(t *testing.T) {
	if err := ValidatePreconditions(nil); err != nil {
		t.Fatalf("nil preconditions should be valid: %v", err)
	}
	if err := ValidatePreconditions(&xdr.Preconditions{Type: xdr.PreconditionTypePrecondNone}); err != nil {
		t.Fatalf("PRECOND_NONE should be valid: %v", err)
	}

	// Valid time bounds, including an unbounded max_time of 0.
	for _, tb := range []*xdr.TimeBounds{
		{MinTime: 100, MaxTime: 200},
		{MinTime: 100, MaxTime: 0},
		{MinTime: 0, MaxTime: 0},
	} {
		if err := ValidatePreconditions(&xdr.Preconditions{Type: xdr.PreconditionTypePrecondTime, TimeBounds: tb}); err != nil {
			t.Fatalf("expected valid time bounds %+v: %v", tb, err)
		}
	}

	// Reversed time bounds.
	err := ValidatePreconditions(&xdr.Preconditions{
		Type:       xdr.PreconditionTypePrecondTime,
		TimeBounds: &xdr.TimeBounds{MinTime: 200, MaxTime: 100},
	})
	if err == nil {
		t.Fatal("expected reversed time bounds to fail")
	}
	if !errors.Is(err, ErrInvalidPrecondition) {
		t.Fatalf("expected ErrInvalidPrecondition, got %v", err)
	}
	if !strings.Contains(err.Error(), "min_time 200") || !strings.Contains(err.Error(), "max_time 100") {
		t.Fatalf("expected descriptive time bounds error, got %q", err.Error())
	}

	// Reversed ledger bounds inside a V2 precondition.
	err = ValidatePreconditions(&xdr.Preconditions{
		Type: xdr.PreconditionTypePrecondV2,
		V2: &xdr.PreconditionsV2{
			LedgerBounds: &xdr.LedgerBounds{MinLedger: 50, MaxLedger: 10},
		},
	})
	if err == nil || !errors.Is(err, ErrInvalidPrecondition) {
		t.Fatalf("expected ErrInvalidPrecondition for reversed ledger bounds, got %v", err)
	}
	if !strings.Contains(err.Error(), "min_ledger 50") || !strings.Contains(err.Error(), "max_ledger 10") {
		t.Fatalf("expected descriptive ledger bounds error, got %q", err.Error())
	}

	// Valid V2 combined bounds.
	if err := ValidatePreconditions(&xdr.Preconditions{
		Type: xdr.PreconditionTypePrecondV2,
		V2: &xdr.PreconditionsV2{
			TimeBounds:   &xdr.TimeBounds{MinTime: 1, MaxTime: 2},
			LedgerBounds: &xdr.LedgerBounds{MinLedger: 10, MaxLedger: 20},
		},
	}); err != nil {
		t.Fatalf("valid V2 preconditions failed: %v", err)
	}

	// Malformed unions and unknown discriminants.
	if err := ValidatePreconditions(&xdr.Preconditions{Type: xdr.PreconditionTypePrecondTime}); err == nil {
		t.Error("expected PRECOND_TIME without time bounds to fail")
	}
	if err := ValidatePreconditions(&xdr.Preconditions{Type: xdr.PreconditionTypePrecondV2}); err == nil {
		t.Error("expected PRECOND_V2 without a body to fail")
	}
	if err := ValidatePreconditions(&xdr.Preconditions{Type: xdr.PreconditionType(99)}); err == nil {
		t.Error("expected unknown precondition type to fail")
	}
}

func TestBuildEnvelopeRejectsReversedBounds(t *testing.T) {
	validAccount := "GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAW"
	ops := []OperationSpec{{Type: "payment", Destination: validAccount, Amount: 100}}

	_, err := BuildTransactionEnvelope(&TxSpec{
		SourceAccount: validAccount,
		Fee:           100,
		Sequence:      1,
		TimeBounds:    &TimeBoundsSpec{Min: 200, Max: 100},
		Operations:    ops,
	})
	if err == nil || !errors.Is(err, ErrInvalidPrecondition) {
		t.Fatalf("expected ErrInvalidPrecondition for reversed time bounds, got %v", err)
	}

	_, err = BuildTransactionEnvelope(&TxSpec{
		SourceAccount: validAccount,
		Fee:           100,
		Sequence:      1,
		LedgerBounds:  &LedgerBoundsSpec{Min: 50, Max: 10},
		Operations:    ops,
	})
	if err == nil || !errors.Is(err, ErrInvalidPrecondition) {
		t.Fatalf("expected ErrInvalidPrecondition for reversed ledger bounds, got %v", err)
	}

	// Unbounded max values are accepted.
	if _, err := BuildTransactionEnvelope(&TxSpec{
		SourceAccount: validAccount,
		Fee:           100,
		Sequence:      1,
		TimeBounds:    &TimeBoundsSpec{Min: 200, Max: 0},
		Operations:    ops,
	}); err != nil {
		t.Fatalf("expected unbounded time bounds to encode: %v", err)
	}
}
