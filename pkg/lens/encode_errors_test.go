package lens

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestFieldValidationErrorFormatting(t *testing.T) {
	fieldErr := &FieldValidationError{
		Path:     "operations[0].amount",
		Expected: "positive decimal string",
		Actual:   "-5.0",
	}
	if got, want := fieldErr.Error(), "at operations[0].amount: expected positive decimal string, got -5.0"; got != want {
		t.Errorf("unexpected error string: got %q want %q", got, want)
	}
	if got, want := fieldErr.Detail(), "expected positive decimal string, got -5.0"; got != want {
		t.Errorf("unexpected detail: got %q want %q", got, want)
	}
	if !errors.Is(fieldErr, ErrValidationFailed) {
		t.Error("expected FieldValidationError to match ErrValidationFailed")
	}

	// Message overrides the expected/actual wording.
	withMessage := &FieldValidationError{Path: "fee", Message: "required field is missing or null"}
	if got, want := withMessage.Error(), "at fee: required field is missing or null"; got != want {
		t.Errorf("unexpected message error: got %q want %q", got, want)
	}

	// Partial and pathless errors still render sensibly.
	if got := (&FieldValidationError{Expected: "string"}).Error(); got != "expected string" {
		t.Errorf("unexpected expected-only error: %q", got)
	}
	if got := (&FieldValidationError{Actual: "object"}).Error(); got != "got object" {
		t.Errorf("unexpected actual-only error: %q", got)
	}
	if got := (&FieldValidationError{}).Error(); got != "invalid value" {
		t.Errorf("unexpected empty error: %q", got)
	}

	// A wrapped cause is exposed through errors.As/Is.
	cause := errors.New("cause")
	wrapped := &FieldValidationError{Path: "x", Err: cause}
	if !errors.Is(wrapped, cause) {
		t.Error("expected underlying cause to be discoverable")
	}
}

func TestFormatActualValue(t *testing.T) {
	cases := []struct {
		val  any
		want string
	}{
		{nil, "null"},
		{"abc", `"abc"`},
		{json.Number("-5.0"), "-5.0"},
		{float64(-5), "-5"},
		{float32(1.5), "1.5"},
		{true, "true"},
		{int64(42), "42"},
		{map[string]any{}, "object"},
		{[]any{}, "array"},
	}
	for _, tc := range cases {
		if got := FormatActualValue(tc.val); got != tc.want {
			t.Errorf("FormatActualValue(%#v) = %q, want %q", tc.val, got, tc.want)
		}
	}
}

func TestValidateJSONReportsFieldPaths(t *testing.T) {
	validAccount := "GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAW"

	badAmount := []byte(`{
		"source_account": "` + validAccount + `",
		"fee": 100,
		"sequence": 1,
		"operations": [
			{"type": "payment", "destination": "` + validAccount + `", "amount": -5.0}
		]
	}`)
	err := ValidateJSON(badAmount, StrictTxSpecSchema())
	if err == nil {
		t.Fatal("expected negative payment amount to fail validation")
	}
	if !errors.Is(err, ErrValidationFailed) {
		t.Fatalf("expected ErrValidationFailed, got %v", err)
	}
	var fieldErr *FieldValidationError
	if !errors.As(err, &fieldErr) {
		t.Fatalf("expected errors.As to expose FieldValidationError, got %v", err)
	}
	if fieldErr.Path != "operations[0].amount" {
		t.Fatalf("expected path operations[0].amount, got %q", fieldErr.Path)
	}
	want := "at operations[0].amount: expected positive decimal string, got -5.0"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("expected %q in error %q", want, err.Error())
	}

	// Type mismatches name the schema type and the offending value.
	badFee := []byte(`{
		"source_account": "` + validAccount + `",
		"fee": "abc",
		"sequence": 1,
		"operations": []
	}`)
	err = ValidateJSON(badFee, StrictTxSpecSchema())
	if err == nil {
		t.Fatal("expected non-numeric fee to fail validation")
	}
	var feeErr *FieldValidationError
	if !errors.As(err, &feeErr) {
		t.Fatalf("expected errors.As to expose FieldValidationError, got %v", err)
	}
	if feeErr.Path != "fee" {
		t.Fatalf("expected path fee, got %q", feeErr.Path)
	}
	if !strings.Contains(err.Error(), `at fee: expected number, got "abc"`) {
		t.Fatalf("unexpected fee error: %q", err.Error())
	}

	// Nested array paths are reported for element type failures.
	badOps := []byte(`{
		"source_account": "` + validAccount + `",
		"fee": 100,
		"sequence": 1,
		"operations": ["not-an-object"]
	}`)
	err = ValidateJSON(badOps, StrictTxSpecSchema())
	if err == nil {
		t.Fatal("expected scalar operation to fail validation")
	}
	if !strings.Contains(err.Error(), "at operations[0]: expected object, got \"not-an-object\"") {
		t.Fatalf("unexpected operations error: %q", err.Error())
	}

	// Missing fields report their exact path.
	missing := []byte(`{
		"source_account": "` + validAccount + `",
		"sequence": 1,
		"operations": []
	}`)
	err = ValidateJSON(missing, StrictTxSpecSchema())
	if err == nil {
		t.Fatal("expected missing fee to fail validation")
	}
	if !strings.Contains(err.Error(), "at fee: missing required field \"fee\"") {
		t.Fatalf("unexpected missing field error: %q", err.Error())
	}
}

func TestJSONPositionString(t *testing.T) {
	p1 := JSONPosition{Line: 5, Column: 12, Offset: 80}
	if p1.String() != "line 5, column 12" {
		t.Errorf("unexpected string: %q", p1.String())
	}

	p2 := JSONPosition{Line: 0, Column: 0, Offset: 42}
	if p2.String() != "offset 42" {
		t.Errorf("unexpected string: %q", p2.String())
	}
}

func TestDiagnosticErrorMethods(t *testing.T) {
	baseErr := errors.New("base failure")
	diag := &DiagnosticError{
		Pos:        JSONPosition{Line: 2, Column: 4, Offset: 10},
		Path:       "operations[0].amount",
		Message:    "invalid amount",
		Snippet:    "  2 | test",
		Underlying: baseErr,
	}

	errStr := diag.Error()
	if !strings.Contains(errStr, "operations[0].amount") || !strings.Contains(errStr, "line 2, column 4") {
		t.Errorf("error string missing expected elements: %s", errStr)
	}

	// Unwrap
	if !errors.Is(diag, baseErr) {
		t.Errorf("expected errors.Is to match baseErr")
	}
	if !errors.Is(diag, ErrMalformedJSON) {
		t.Errorf("expected errors.Is to match ErrMalformedJSON")
	}

	// Nil underlying error fallback
	diagNil := &DiagnosticError{
		Pos:     JSONPosition{Line: 1, Column: 1},
		Message: "only message",
	}
	if !strings.Contains(diagNil.Error(), "only message") {
		t.Errorf("expected message in error string: %s", diagNil.Error())
	}
}

func TestFindOffsetPosition(t *testing.T) {
	input := []byte("line1\nline2\nline3")

	posStart := FindOffsetPosition(input, 0)
	if posStart.Line != 1 || posStart.Column != 1 {
		t.Errorf("expected line 1, col 1, got %+v", posStart)
	}

	posLine2 := FindOffsetPosition(input, 6) // 'l' in line2
	if posLine2.Line != 2 || posLine2.Column != 1 {
		t.Errorf("expected line 2, col 1, got %+v", posLine2)
	}

	posNeg := FindOffsetPosition(input, -5)
	if posNeg.Offset != 0 {
		t.Errorf("expected clamped offset 0, got %+v", posNeg)
	}

	posPast := FindOffsetPosition(input, 999)
	if posPast.Offset != int64(len(input)) {
		t.Errorf("expected clamped offset %d, got %+v", len(input), posPast)
	}
}

func TestFindJSONPathLocation(t *testing.T) {
	jsonDoc := []byte(`{
  "source": "GBRPYHIL2CI3FNQ4BXLFMNDLFJUNPU2HY3ZMFSHONUCEOASW7QC7OX2H",
  "fee": 100,
  "memo": {
    "type": "text",
    "value": "hello"
  },
  "operations": [
    {
      "type": "payment",
      "amount": "100"
    }
  ]
}`)

	// Root / empty path
	pRoot, err := FindJSONPathLocation(jsonDoc, "")
	if err != nil || pRoot.Line != 1 {
		t.Errorf("failed empty path: %+v, err=%v", pRoot, err)
	}

	// Simple key
	pFee, err := FindJSONPathLocation(jsonDoc, "fee")
	if err != nil || pFee.Line != 3 {
		t.Errorf("failed simple path 'fee': %+v, err=%v", pFee, err)
	}

	// Nested object key
	pMemoVal, err := FindJSONPathLocation(jsonDoc, "memo.value")
	if err != nil || pMemoVal.Line != 6 {
		t.Errorf("failed nested path 'memo.value': %+v, err=%v", pMemoVal, err)
	}

	// Array element field
	pOpAmt, err := FindJSONPathLocation(jsonDoc, "operations[0].amount")
	if err != nil || pOpAmt.Line != 11 {
		t.Errorf("failed array path 'operations[0].amount': %+v, err=%v", pOpAmt, err)
	}

	// Non-existent path
	_, err = FindJSONPathLocation(jsonDoc, "nonexistent.field")
	if err == nil || !errors.Is(err, ErrPathNotFound) {
		t.Errorf("expected ErrPathNotFound, got: %v", err)
	}

	// Malformed path array index
	_, err = FindJSONPathLocation(jsonDoc, "operations[not_a_number].amount")
	if err == nil {
		t.Errorf("expected error on non-integer array index")
	}
}

func TestGenerateSnippet(t *testing.T) {
	code := []byte("first line\nsecond line\nthird line\nfourth line\nfifth line")

	// Empty data
	if s := GenerateSnippet(nil, JSONPosition{}, 1); s != "" {
		t.Errorf("expected empty string for empty data")
	}

	// Line 3 with length 4 pointer
	snip := GenerateSnippet(code, JSONPosition{Line: 3, Column: 7}, 4)
	if !strings.Contains(snip, "3 | third line") || !strings.Contains(snip, "^~~~") {
		t.Errorf("unexpected snippet format:\n%s", snip)
	}

	// Clamping tests
	snipTop := GenerateSnippet(code, JSONPosition{Line: -1, Column: -1}, 0)
	if !strings.Contains(snipTop, "1 | first line") {
		t.Errorf("unexpected clamped snippet:\n%s", snipTop)
	}

	snipBottom := GenerateSnippet(code, JSONPosition{Line: 99, Column: 99}, 1)
	if !strings.Contains(snipBottom, "5 | fifth line") {
		t.Errorf("unexpected clamped bottom snippet:\n%s", snipBottom)
	}
}

func TestFormatDiagnostic(t *testing.T) {
	if FormatDiagnostic(nil, nil) != "" {
		t.Errorf("expected empty output for nil error")
	}

	badJSON := []byte("{\n  \"key\": invalid\n}")
	var dummy any
	err := json.Unmarshal(badJSON, &dummy)
	if err == nil {
		t.Fatal("expected syntax error on bad JSON")
	}

	diag := FormatDiagnostic(badJSON, err)
	if !strings.Contains(diag, "line 2") || !strings.Contains(diag, "^") {
		t.Errorf("expected formatted diagnostic with line number, got:\n%s", diag)
	}

	// UnmarshalTypeError
	type target struct {
		Number int `json:"number"`
	}
	typeMismatchJSON := []byte("{\n  \"number\": \"not_an_int\"\n}")
	var tgt target
	unmarshalErr := json.Unmarshal(typeMismatchJSON, &tgt)
	if unmarshalErr == nil {
		t.Fatal("expected unmarshal type error")
	}
	diagType := FormatDiagnostic(typeMismatchJSON, unmarshalErr)
	if !strings.Contains(diagType, "cannot unmarshal") || !strings.Contains(diagType, "line 2") {
		t.Errorf("expected type mismatch diagnostic, got:\n%s", diagType)
	}

	// General error with path
	pathErr := errors.New("validation failed at operations[0].amount: bad amount")
	validJSON := []byte("{\n  \"operations\": [\n    {\"amount\": \"bad\"}\n  ]\n}")
	diagPath := FormatDiagnostic(validJSON, pathErr)
	if !strings.Contains(diagPath, "operations[0].amount") || !strings.Contains(diagPath, "line 3") {
		t.Errorf("expected path diagnostic, got:\n%s", diagPath)
	}

	// General error without path
	simpleErr := errors.New("plain error without path")
	diagSimple := FormatDiagnostic(validJSON, simpleErr)
	if diagSimple != "plain error without path" {
		t.Errorf("expected plain error string, got: %s", diagSimple)
	}
}

func TestAnnotateAndWrapJSONError(t *testing.T) {
	jsonDoc := []byte("{\n  \"fee\": 0\n}")

	if AnnotateJSONError(jsonDoc, "fee", nil) != nil {
		t.Errorf("expected nil for nil error")
	}
	if WrapWithDiagnostics(jsonDoc, nil) != nil {
		t.Errorf("expected nil for nil error")
	}

	baseErr := errors.New("fee cannot be zero")
	annotated := AnnotateJSONError(jsonDoc, "fee", baseErr)
	if annotated == nil || !strings.Contains(annotated.Error(), "fee (line 2") {
		t.Errorf("unexpected annotated error: %v", annotated)
	}

	// WrapWithDiagnostics
	bad := []byte("{")
	var m map[string]any
	if err := json.Unmarshal(bad, &m); err != nil {
		wrapped := WrapWithDiagnostics(bad, err)
		if !strings.Contains(wrapped.Error(), "^") {
			t.Errorf("expected wrapped error to include pointer, got: %v", wrapped)
		}
	} else {
		t.Fatal("expected syntax error")
	}
}

func TestReadAndValidateJSON(t *testing.T) {
	valid := []byte(`{"hello": "world"}`)
	out, err := ReadAndValidateJSON(bytes.NewReader(valid))
	if err != nil || string(out) != string(valid) {
		t.Errorf("failed valid JSON read: %v, out: %s", err, out)
	}

	invalid := []byte(`{hello: world}`)
	_, err = ReadAndValidateJSON(bytes.NewReader(invalid))
	if err == nil || !errors.Is(err, ErrMalformedJSON) {
		t.Errorf("expected ErrMalformedJSON on invalid JSON, got: %v", err)
	}

	jsonDoc := []byte("{\n  \"memo\": {\"value\": 10}\n}")
	baseErr := errors.New("sample error")

	// DiagnosticError already wrapped
	diagErrObj := &DiagnosticError{
		Pos:     JSONPosition{Line: 1, Column: 1},
		Message: "already diagnostic",
	}
	if FormatDiagnostic(jsonDoc, diagErrObj) != diagErrObj.Error() {
		t.Errorf("expected DiagnosticError to return its own Error()")
	}

	// DiagnosticError.Is with unrelated error
	unrelatedErr := errors.New("unrelated error")
	if diagErrObj.Is(unrelatedErr) {
		t.Errorf("expected false for unrelated error in Is()")
	}

	// Path with "field <path>:"
	fieldErr := errors.New("invalid field memo.value: unexpected type")
	diagField := FormatDiagnostic(jsonDoc, fieldErr)
	if !strings.Contains(diagField, "memo.value") {
		t.Errorf("expected field pattern to extract path, got: %s", diagField)
	}

	// Annotate with non-existent path
	annotatedNotFound := AnnotateJSONError(jsonDoc, "nonexistent", baseErr)
	if annotatedNotFound == nil || !strings.Contains(annotatedNotFound.Error(), "line 1") {
		t.Errorf("expected fallback position for non-existent path, got: %v", annotatedNotFound)
	}

	// Search path that requires skipping nested objects and arrays
	complexDoc := []byte(`{
  "skipped_obj": {"nested": [1, 2, {"a": "b"}]},
  "skipped_arr": [10, 20, {"deep": true}],
  "target": "found_me"
}`)
	pTarget, err := FindJSONPathLocation(complexDoc, "target")
	if err != nil || pTarget.Line != 4 {
		t.Errorf("failed searching after skipping complex structures: %+v, err=%v", pTarget, err)
	}

	// Search array element after skipping previous elements
	arrDoc := []byte(`{
  "list": [
    {"name": "first", "data": {"x": 1}},
    {"name": "second"}
  ]
}`)
	pSecond, err := FindJSONPathLocation(arrDoc, "list[1].name")
	if err != nil || pSecond.Line != 4 {
		t.Errorf("failed searching array element after skip: %+v, err=%v", pSecond, err)
	}

	// ReadAndValidateJSON io error
	errReader := &faultyReader{err: errors.New("io read failure")}
	if _, err := ReadAndValidateJSON(errReader); err == nil {
		t.Errorf("expected io error from ReadAndValidateJSON")
	}
}

type faultyReader struct {
	err error
}

func (r *faultyReader) Read(p []byte) (n int, err error) {
	return 0, r.err
}
