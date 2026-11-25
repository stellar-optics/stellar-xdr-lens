package lens

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Sentinel errors for JSON encoding diagnostics.
var (
	ErrMalformedJSON = errors.New("malformed JSON input")
	ErrPathNotFound  = errors.New("specified JSON path not found")
)

// FieldValidationError reports a schema violation at a specific JSON field path.
// It names the offending path and, where known, the expected and actual values so
// callers can point users at the exact field that failed validation.
type FieldValidationError struct {
	// Path is the dot/bracket JSON path of the offending field, e.g. "operations[0].amount".
	Path string
	// Expected describes the value the schema requires, e.g. "positive decimal string".
	Expected string
	// Actual is the offending value rendered for humans, e.g. "-5.0".
	Actual string
	// Message overrides the default "expected X, got Y" wording when set.
	Message string
	// Err is an optional underlying cause.
	Err error
}

// Error formats the violation as "at <path>: <reason>" so the invalid field is obvious.
func (e *FieldValidationError) Error() string {
	if e.Path == "" {
		return e.Detail()
	}
	return "at " + e.Path + ": " + e.Detail()
}

// Detail returns the reason without the path prefix.
func (e *FieldValidationError) Detail() string {
	if e.Message != "" {
		return e.Message
	}
	switch {
	case e.Expected != "" && e.Actual != "":
		return fmt.Sprintf("expected %s, got %s", e.Expected, e.Actual)
	case e.Expected != "":
		return "expected " + e.Expected
	case e.Actual != "":
		return "got " + e.Actual
	default:
		return "invalid value"
	}
}

// Unwrap exposes the underlying cause, defaulting to ErrValidationFailed.
func (e *FieldValidationError) Unwrap() error {
	if e.Err != nil {
		return e.Err
	}
	return ErrValidationFailed
}

// FormatActualValue renders a decoded JSON value for inclusion in a field error.
func FormatActualValue(val any) string {
	switch v := val.(type) {
	case nil:
		return "null"
	case string:
		return strconv.Quote(v)
	case json.Number:
		return v.String()
	case float32:
		return strconv.FormatFloat(float64(v), 'g', -1, 32)
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%v", v)
	case map[string]any:
		return "object"
	case []any:
		return "array"
	default:
		return fmt.Sprintf("%v", v)
	}
}

// JSONPosition records a location within a JSON source.
type JSONPosition struct {
	Line   int   // 1-based line number
	Column int   // 1-based column number
	Offset int64 // 0-based byte offset
}

// String returns a human-readable representation of the position.
func (p JSONPosition) String() string {
	if p.Line <= 0 && p.Column <= 0 {
		return fmt.Sprintf("offset %d", p.Offset)
	}
	return fmt.Sprintf("line %d, column %d", p.Line, p.Column)
}

// DiagnosticError represents a richly annotated JSON error with location and code snippet pointers.
type DiagnosticError struct {
	Pos        JSONPosition
	Path       string
	Snippet    string
	Message    string
	Underlying error
}

// Error implements the error interface.
func (e *DiagnosticError) Error() string {
	var b strings.Builder
	b.WriteString("JSON encode error")
	if e.Path != "" {
		b.WriteString(" at ")
		b.WriteString(e.Path)
	}
	if e.Pos.Line > 0 {
		b.WriteString(" (")
		b.WriteString(e.Pos.String())
		b.WriteString(")")
	}
	b.WriteString(": ")
	if e.Message != "" {
		b.WriteString(e.Message)
	} else if e.Underlying != nil {
		b.WriteString(e.Underlying.Error())
	}
	if e.Snippet != "" {
		b.WriteString("\n")
		b.WriteString(e.Snippet)
	}
	return b.String()
}

// Unwrap returns the underlying error for errors.Is and errors.As.
func (e *DiagnosticError) Unwrap() error {
	return e.Underlying
}

// Is reports whether e matches target.
func (e *DiagnosticError) Is(target error) bool {
	if errors.Is(target, ErrMalformedJSON) {
		return true
	}
	if e.Underlying != nil {
		return errors.Is(e.Underlying, target)
	}
	return false
}

// FindOffsetPosition calculates the 1-based Line and Column for a byte offset in data.
func FindOffsetPosition(data []byte, offset int64) JSONPosition {
	if offset < 0 {
		offset = 0
	}
	if offset > int64(len(data)) {
		offset = int64(len(data))
	}

	line := 1
	col := 1
	for i := int64(0); i < offset; i++ {
		if data[i] == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}

	return JSONPosition{
		Line:   line,
		Column: col,
		Offset: offset,
	}
}

// FindJSONPathLocation searches the JSON byte slice for the token at the given dot/bracket path.
// Examples of path: "operations[0].amount", "memo.value", "fee".
func FindJSONPathLocation(data []byte, path string) (JSONPosition, error) {
	segments := parsePathSegments(path)
	if len(segments) == 0 {
		return JSONPosition{Line: 1, Column: 1, Offset: 0}, nil
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	pos, found := searchPath(dec, data, segments, 0)
	if !found {
		return JSONPosition{}, fmt.Errorf("%w: %q", ErrPathNotFound, path)
	}
	return pos, nil
}

func parsePathSegments(path string) []string {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	var segments []string
	clean := strings.ReplaceAll(path, "[", ".")
	clean = strings.ReplaceAll(clean, "]", "")
	parts := strings.Split(clean, ".")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			segments = append(segments, p)
		}
	}
	return segments
}

func searchPath(dec *json.Decoder, data []byte, segments []string, segIdx int) (JSONPosition, bool) {
	if segIdx >= len(segments) {
		pos := FindOffsetPosition(data, dec.InputOffset())
		return pos, true
	}

	targetSegment := segments[segIdx]
	tok, err := dec.Token()
	if err != nil {
		return JSONPosition{}, false
	}

	switch delim := tok.(type) {
	case json.Delim:
		switch delim {
		case '{':
			return searchObject(dec, data, segments, segIdx)
		case '[':
			targetIdx, err := strconv.Atoi(targetSegment)
			if err != nil {
				return JSONPosition{}, false
			}
			return searchArray(dec, data, segments, segIdx, targetIdx)
		}
	}

	return JSONPosition{}, false
}

func searchObject(dec *json.Decoder, data []byte, segments []string, segIdx int) (JSONPosition, bool) {
	targetKey := segments[segIdx]
	for dec.More() {
		keyOffset := dec.InputOffset()
		keyTok, err := dec.Token()
		if err != nil {
			return JSONPosition{}, false
		}
		keyStr, ok := keyTok.(string)
		if !ok {
			return JSONPosition{}, false
		}

		if keyStr == targetKey {
			if segIdx == len(segments)-1 {
				// Reached target key: point to the key or value offset
				keyPos := findKeyInSource(data, keyOffset, keyStr)
				return keyPos, true
			}
			return searchPath(dec, data, segments, segIdx+1)
		}

		// Skip unmatched value
		if err := skipJSONValue(dec); err != nil {
			return JSONPosition{}, false
		}
	}

	// Consume closing delimiter '}'
	if _, err := dec.Token(); err != nil {
		return JSONPosition{}, false
	}
	return JSONPosition{}, false
}

func searchArray(dec *json.Decoder, data []byte, segments []string, segIdx int, targetIdx int) (JSONPosition, bool) {
	curIdx := 0
	for dec.More() {
		elemOffset := dec.InputOffset()
		if curIdx == targetIdx {
			if segIdx == len(segments)-1 {
				pos := FindOffsetPosition(data, elemOffset)
				return pos, true
			}
			return searchPath(dec, data, segments, segIdx+1)
		}
		if err := skipJSONValue(dec); err != nil {
			return JSONPosition{}, false
		}
		curIdx++
	}

	// Consume closing delimiter ']'
	if _, err := dec.Token(); err != nil {
		return JSONPosition{}, false
	}
	return JSONPosition{}, false
}

func findKeyInSource(data []byte, searchFrom int64, key string) JSONPosition {
	quoted := `"` + key + `"`
	idx := bytes.Index(data[searchFrom:], []byte(quoted))
	if idx != -1 {
		return FindOffsetPosition(data, searchFrom+int64(idx)+1)
	}
	return FindOffsetPosition(data, searchFrom)
}

func skipJSONValue(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if _, ok := tok.(json.Delim); ok {
		depth := 1
		for depth > 0 {
			t, err := dec.Token()
			if err != nil {
				return err
			}
			if d, ok := t.(json.Delim); ok {
				switch d {
				case '{', '[':
					depth++
				case '}', ']':
					depth--
				}
			}
		}
	}
	return nil
}

// GenerateSnippet produces a source-code style diagnostic box with line numbers and a caret pointer.
func GenerateSnippet(data []byte, pos JSONPosition, pointerLen int) string {
	if len(data) == 0 {
		return ""
	}
	if pointerLen <= 0 {
		pointerLen = 1
	}

	lines := strings.Split(string(data), "\n")
	targetLine := pos.Line
	if targetLine <= 0 {
		targetLine = 1
	}
	if targetLine > len(lines) {
		targetLine = len(lines)
	}

	start := targetLine - 2
	if start < 1 {
		start = 1
	}
	end := targetLine + 2
	if end > len(lines) {
		end = len(lines)
	}

	var sb strings.Builder
	for i := start; i <= end; i++ {
		lineContent := lines[i-1]
		fmt.Fprintf(&sb, "%4d | %s\n", i, lineContent)
		if i == targetLine {
			indent := pos.Column - 1
			if indent < 0 {
				indent = 0
			}
			if indent > len(lineContent) {
				indent = len(lineContent)
			}
			pointer := "^"
			if pointerLen > 1 {
				pointer += strings.Repeat("~", pointerLen-1)
			}
			fmt.Fprintf(&sb, "     | %s%s\n", strings.Repeat(" ", indent), pointer)
		}
	}

	return strings.TrimRight(sb.String(), "\n")
}

// FormatDiagnostic turns any JSON-related error into a comprehensive line-and-column diagnostic string.
func FormatDiagnostic(data []byte, err error) string {
	if err == nil {
		return ""
	}

	var diagErr *DiagnosticError
	if errors.As(err, &diagErr) {
		return diagErr.Error()
	}

	var synErr *json.SyntaxError
	if errors.As(err, &synErr) {
		pos := FindOffsetPosition(data, synErr.Offset)
		snippet := GenerateSnippet(data, pos, 1)
		diag := &DiagnosticError{
			Pos:        pos,
			Message:    synErr.Error(),
			Snippet:    snippet,
			Underlying: err,
		}
		return diag.Error()
	}

	var unmarshalErr *json.UnmarshalTypeError
	if errors.As(err, &unmarshalErr) {
		pos := FindOffsetPosition(data, unmarshalErr.Offset)
		snippet := GenerateSnippet(data, pos, 1)
		msg := fmt.Sprintf("cannot unmarshal %s into Go value of type %s", unmarshalErr.Value, unmarshalErr.Type)
		diag := &DiagnosticError{
			Pos:        pos,
			Path:       unmarshalErr.Field,
			Message:    msg,
			Snippet:    snippet,
			Underlying: err,
		}
		return diag.Error()
	}

	// General error: check if it carries a path
	path := extractPathFromError(err)
	if path != "" {
		pos, findErr := FindJSONPathLocation(data, path)
		if findErr == nil {
			snippet := GenerateSnippet(data, pos, 1)
			diag := &DiagnosticError{
				Pos:        pos,
				Path:       path,
				Message:    err.Error(),
				Snippet:    snippet,
				Underlying: err,
			}
			return diag.Error()
		}
	}

	return err.Error()
}

func extractPathFromError(err error) string {
	msg := err.Error()
	// Check for pattern "at <path>:" or "field <path>:"
	if idx := strings.Index(msg, "at "); idx != -1 {
		rest := msg[idx+3:]
		if endIdx := strings.IndexAny(rest, " :,\n"); endIdx != -1 {
			return rest[:endIdx]
		}
		return rest
	}
	if idx := strings.Index(msg, "field "); idx != -1 {
		rest := msg[idx+6:]
		if endIdx := strings.IndexAny(rest, " :,\n"); endIdx != -1 {
			return rest[:endIdx]
		}
		return rest
	}
	return ""
}

// AnnotateJSONError inspects err and returns a DiagnosticError with source coordinates if possible.
func AnnotateJSONError(data []byte, path string, err error) error {
	if err == nil {
		return nil
	}

	pos, locErr := FindJSONPathLocation(data, path)
	if locErr != nil {
		pos = JSONPosition{Line: 1, Column: 1, Offset: 0}
	}

	snippet := GenerateSnippet(data, pos, 1)
	return &DiagnosticError{
		Pos:        pos,
		Path:       path,
		Message:    err.Error(),
		Snippet:    snippet,
		Underlying: err,
	}
}

// WrapWithDiagnostics wraps an error with detailed source diagnostics if applicable.
func WrapWithDiagnostics(data []byte, err error) error {
	if err == nil {
		return nil
	}
	formatted := FormatDiagnostic(data, err)
	if formatted == err.Error() {
		return err
	}
	return errors.New(formatted)
}

// ReadAndValidateJSON validates that input bytes form a well-formed JSON object or value,
// providing an annotated diagnostic on syntax failure.
func ReadAndValidateJSON(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading JSON input: %w", err)
	}

	var js json.RawMessage
	if err := json.Unmarshal(data, &js); err != nil {
		return nil, fmt.Errorf("%w:\n%s", ErrMalformedJSON, FormatDiagnostic(data, err))
	}

	return data, nil
}
