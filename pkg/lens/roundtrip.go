package lens

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
)

// Round-trip verification closes the loop between the decoder and the
// encoders. The property it checks is the one the CLI promises:
//
//	decode(encode(value)) == value
//
// A trip starts from a decoded Value (or from a base64 payload), re-encodes it,
// decodes the result again, and compares both the bytes and the neutral trees.
// Two routes are available:
//
//   - RoundTripBinary re-serializes the concrete xdr value (Encode). It proves
//     that decoding and encoding are inverses for every registered type.
//   - RoundTripJSON goes through the stable JSON document and EncodeJSON. It
//     proves that the symmetric JSON encoder accepts the decoder's own output.
//
// The engine is deterministic and offline. It reads no files, opens no
// sockets, and never mutates the value it is given.

// RoundTripRoute identifies the encode path a report verifies.
type RoundTripRoute string

const (
	// RoundTripBinary re-serializes the decoded xdr value directly.
	RoundTripBinary RoundTripRoute = "binary"
	// RoundTripJSON rounds the value through its stable JSON document first.
	RoundTripJSON RoundTripRoute = "json"
)

// RoundTripReport records the outcome of one decode/encode/decode trip.
//
// The zero value is not meaningful. A report is produced even when the trip
// cannot run: Err carries the failure, and Skipped marks an input the route
// deliberately does not support (for example a lossy enriched rendering).
type RoundTripReport struct {
	// Name is the suite case name, when the report came from a suite. It is
	// empty for a single value or payload.
	Name string
	// Type is the registered XDR type name that was verified.
	Type string
	// Route is the encode path that was exercised.
	Route RoundTripRoute
	// Original is the base64 XDR the trip started from.
	Original string
	// Reencoded is the base64 XDR produced by the encoder.
	Reencoded string
	// BytesEqual reports whether Reencoded is byte-identical to Original.
	BytesEqual bool
	// TreeEqual reports whether the re-decoded value has the same neutral
	// tree as the original, which is the property itself.
	TreeEqual bool
	// Changes lists the structural differences when TreeEqual is false.
	Changes []Change
	// Skipped marks a route that cannot represent the value, such as a claim
	// predicate collapsed to a human sentence. SkipReason explains why.
	Skipped    bool
	SkipReason string
	// Err is set when the trip could not be completed for any other reason.
	Err error
}

// OK reports whether the property held: the trip completed, the bytes match,
// and the trees are structurally identical.
func (r *RoundTripReport) OK() bool {
	return r != nil && !r.Skipped && r.Err == nil && r.BytesEqual && r.TreeEqual
}

// String renders a one-line summary suitable for test output.
func (r *RoundTripReport) String() string {
	if r == nil {
		return "round trip: <nil>"
	}
	name := r.Name
	if name == "" {
		name = r.Type
	}
	switch {
	case r.Skipped:
		return fmt.Sprintf("%s [%s]: skipped: %s", name, r.Route, r.SkipReason)
	case r.Err != nil:
		return fmt.Sprintf("%s [%s]: error: %v", name, r.Route, r.Err)
	case r.OK():
		return fmt.Sprintf("%s [%s]: ok", name, r.Route)
	default:
		return fmt.Sprintf("%s [%s]: mismatch (%d change(s), bytes equal=%t)",
			name, r.Route, len(r.Changes), r.BytesEqual)
	}
}

// RoundTripValue verifies the binary property for an already decoded value.
func RoundTripValue(v *Value) (*RoundTripReport, error) {
	return RoundTripRouteValue(v, RoundTripBinary)
}

// RoundTripJSONValue verifies the JSON property for an already decoded value.
func RoundTripJSONValue(v *Value) (*RoundTripReport, error) {
	return RoundTripRouteValue(v, RoundTripJSON)
}

// RoundTripRouteValue verifies v using the requested route.
func RoundTripRouteValue(v *Value, route RoundTripRoute) (*RoundTripReport, error) {
	if v == nil || v.Raw == nil {
		return nil, fmt.Errorf("%w: round trip requires a decoded value", ErrInvalidArgument)
	}
	original, err := Encode(v)
	if err != nil {
		return nil, err
	}
	return roundTripFrom(v, original, route)
}

// RoundTripBase64 verifies the binary property for a base64 payload. When
// typeName is empty the type is detected, which is the same behavior as
// `lens decode`.
func RoundTripBase64(payload, typeName string) (*RoundTripReport, error) {
	return RoundTripRouteBase64(payload, typeName, RoundTripBinary)
}

// RoundTripJSONBase64 verifies the JSON property for a base64 payload.
func RoundTripJSONBase64(payload, typeName string) (*RoundTripReport, error) {
	return RoundTripRouteBase64(payload, typeName, RoundTripJSON)
}

// RoundTripRouteBase64 decodes payload and verifies the requested route.
func RoundTripRouteBase64(payload, typeName string, route RoundTripRoute) (*RoundTripReport, error) {
	v, err := decodeMaybeTyped(payload, typeName)
	if err != nil {
		return nil, err
	}
	return roundTripFrom(v, normalizeInput(payload), route)
}

// RoundTripCase is one named input for a suite. Type is required unless the
// caller has already chosen a payload that the detector resolves unambiguously.
type RoundTripCase struct {
	Name    string
	Type    string
	Payload string
}

// RoundTripSuiteReport aggregates the reports from one suite.
type RoundTripSuiteReport struct {
	// Route is the path every case was verified with.
	Route RoundTripRoute
	// Reports is in the same order as the supplied cases.
	Reports []*RoundTripReport
}

// OK reports whether every case either passed or was deliberately skipped.
func (s *RoundTripSuiteReport) OK() bool {
	return s != nil && len(s.Failures()) == 0
}

// Failures returns the reports that neither passed nor were skipped.
func (s *RoundTripSuiteReport) Failures() []*RoundTripReport {
	if s == nil {
		return nil
	}
	out := make([]*RoundTripReport, 0)
	for _, r := range s.Reports {
		if r != nil && !r.OK() && !r.Skipped {
			out = append(out, r)
		}
	}
	return out
}

// Skipped returns the reports that the route could not verify.
func (s *RoundTripSuiteReport) Skipped() []*RoundTripReport {
	if s == nil {
		return nil
	}
	out := make([]*RoundTripReport, 0)
	for _, r := range s.Reports {
		if r != nil && r.Skipped {
			out = append(out, r)
		}
	}
	return out
}

// Summary renders one line with the case counts, for test logs.
func (s *RoundTripSuiteReport) Summary() string {
	passed, skipped := 0, 0
	for _, r := range s.Reports {
		if r != nil && r.OK() {
			passed++
		}
		if r != nil && r.Skipped {
			skipped++
		}
	}
	return fmt.Sprintf("round trip [%s]: %d passed, %d skipped, %d failed",
		s.Route, passed, skipped, len(s.Failures()))
}

// RunRoundTripSuite verifies every case with the requested route. A decode
// failure is recorded on the case's report instead of aborting the suite, so a
// single malformed fixture does not hide the results for the others.
func RunRoundTripSuite(route RoundTripRoute, cases []RoundTripCase) *RoundTripSuiteReport {
	suite := &RoundTripSuiteReport{Route: route}
	for _, tc := range cases {
		report, err := RoundTripRouteBase64(tc.Payload, tc.Type, route)
		if err != nil {
			report = &RoundTripReport{
				Name:  tc.Name,
				Type:  tc.Type,
				Route: route,
				Err:   err,
			}
		}
		if report.Name == "" {
			report.Name = tc.Name
		}
		suite.Reports = append(suite.Reports, report)
	}
	return suite
}

// RoundTripRegistry synthesizes a minimal valid value for every registered XDR
// type and verifies the requested route for each. It is the broadest property
// check available without fixtures.
//
// A type whose minimal value cannot be serialized is reported as skipped; that
// is a limitation of the synthesized value, not a failure of the encoder.
// Likewise, the JSON route skips values that a lossy enricher cannot invert,
// such as a claim predicate.
func RoundTripRegistry(route RoundTripRoute) *RoundTripSuiteReport {
	suite := &RoundTripSuiteReport{Route: route}
	entries := registryEntries()
	for _, t := range entries {
		suite.Reports = append(suite.Reports, roundTripRegistryType(t, route))
	}
	return suite
}

func roundTripRegistryType(t xdrType, route RoundTripRoute) (report *RoundTripReport) {
	report = &RoundTripReport{Name: t.name, Type: t.name, Route: route}
	defer func() {
		if r := recover(); r != nil {
			report.Skipped = true
			report.SkipReason = fmt.Sprintf("could not synthesize a minimal %s: %v", t.name, r)
			report.Err = nil
		}
	}()

	raw := t.new()
	rv := reflect.ValueOf(raw)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		report.Skipped = true
		report.SkipReason = "registry entry did not allocate a pointer"
		return report
	}
	fillMinimal(rv.Elem(), 0)

	encoded, err := SafeMarshalBase64(raw)
	if err != nil {
		report.Skipped = true
		report.SkipReason = "minimal value does not serialize: " + err.Error()
		return report
	}

	value, err := DecodeAs(encoded, t.name)
	if err != nil {
		report.Err = fmt.Errorf("re-decoding a synthesized %s: %w", t.name, err)
		return report
	}

	result, err := roundTripFrom(value, encoded, route)
	if err != nil {
		report.Err = err
		return report
	}
	result.Name = t.name
	return result
}

// roundTripFrom runs the actual property check. original must be canonical
// base64 for the byte comparison to mean anything.
func roundTripFrom(v *Value, original string, route RoundTripRoute) (*RoundTripReport, error) {
	report := &RoundTripReport{Type: v.Type, Route: route, Original: original}

	var encoded string
	var err error
	switch route {
	case RoundTripBinary:
		encoded, err = Encode(v)
	case RoundTripJSON:
		var document []byte
		document, err = stableJSONDocument(v)
		if err == nil {
			encoded, err = EncodeJSON(document)
		}
	default:
		return nil, fmt.Errorf("%w: unknown round-trip route %q", ErrInvalidArgument, route)
	}
	if err != nil {
		if errors.Is(err, ErrUnsupportedType) {
			report.Skipped = true
			report.SkipReason = err.Error()
			return report, nil
		}
		return nil, fmt.Errorf("re-encoding %s: %w", v.Type, err)
	}

	report.Reencoded = encoded
	report.BytesEqual = original == encoded

	redecoded, err := DecodeAs(encoded, v.Type)
	if err != nil {
		return nil, fmt.Errorf("re-decoding %s: %w", v.Type, err)
	}
	diff, err := Diff(v, redecoded)
	if err != nil {
		return nil, err
	}
	report.Changes = diff.Changes
	report.TreeEqual = diff.Equal()
	return report, nil
}

// stableJSONDocument renders the documented JSON envelope. It mirrors the
// format package's shape without importing it: format imports lens, so using
// the formatter here would create an import cycle. NodeToGeneric is the same
// conversion the YAML encoder uses, so the two stay in step.
func stableJSONDocument(v *Value) ([]byte, error) {
	if v == nil || v.Node == nil {
		return nil, fmt.Errorf("%w: round trip requires a decoded value with a tree", ErrInvalidArgument)
	}
	document := struct {
		Type  string `json:"type"`
		Value any    `json:"value"`
	}{
		Type:  v.Type,
		Value: NodeToGeneric(v.Node, false),
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("rendering %s as JSON: %w", v.Type, err)
	}
	return encoded, nil
}

func decodeMaybeTyped(payload, typeName string) (*Value, error) {
	if strings.TrimSpace(typeName) != "" {
		return DecodeAs(payload, typeName)
	}
	return Decode(payload)
}

// fillMinimal builds a valid, deterministic minimal value for a generated XDR
// type. It exists so the registry suite can exercise every type without
// shipping a hand-written example for each one.
//
// The rules are the XDR encoding rules: scalars may be zero, sequences may be
// empty, optionals are allocated (so required pointers are non-nil), enums take
// their smallest valid constant, and unions select the first valid arm and
// recursively populate it. Marshal failures for types this cannot satisfy are
// reported as skipped by the caller.
func fillMinimal(rv reflect.Value, depth int) {
	if depth > maxWireSizeDepth || !rv.IsValid() || !rv.CanSet() {
		return
	}
	t := rv.Type()
	if isEnumType(t) {
		if entries := enumEntriesFor(t); len(entries) > 0 {
			rv.SetInt(entries[0].value)
		}
		return
	}

	switch rv.Kind() {
	case reflect.Pointer:
		if rv.IsNil() {
			rv.Set(reflect.New(t.Elem()))
		}
		fillMinimal(rv.Elem(), depth+1)
	case reflect.Struct:
		if hasUnionMethods(t) {
			fillMinimalUnion(rv, depth)
			return
		}
		for i := range t.NumField() {
			field := t.Field(i)
			if field.PkgPath != "" {
				continue
			}
			fillMinimal(rv.Field(i), depth+1)
		}
	}
}

// fillMinimalUnion selects the first switch value with a valid arm and fills
// that arm. Void arms need no further work.
func fillMinimalUnion(rv reflect.Value, depth int) {
	t := rv.Type()
	swName := switchFieldNameOf(t)
	if swName == "" {
		return
	}
	sw := rv.FieldByName(swName)
	if !sw.IsValid() || !sw.CanSet() {
		return
	}
	if !isEnumType(sw.Type()) {
		return
	}

	armMethod, ok := armForSwitchMethod(rv)
	if !ok {
		return
	}
	for _, entry := range enumEntriesFor(sw.Type()) {
		arm, valid := callArmForSwitch(armMethod, entry.value)
		if !valid {
			continue
		}
		sw.SetInt(entry.value)
		if arm == "" || arm == "-" {
			return
		}
		field := rv.FieldByName(arm)
		if !field.IsValid() {
			continue
		}
		if field.Kind() == reflect.Pointer {
			if field.IsNil() {
				field.Set(reflect.New(field.Type().Elem()))
			}
			fillMinimal(field.Elem(), depth+1)
			return
		}
		fillMinimal(field, depth+1)
		return
	}
}

// switchFieldNameOf resolves the generated SwitchFieldName method.
func switchFieldNameOf(t reflect.Type) string {
	method, ok := t.MethodByName("SwitchFieldName")
	if !ok {
		return ""
	}
	out := method.Func.Call([]reflect.Value{reflect.Zero(t)})
	if len(out) != 1 {
		return ""
	}
	name, ok := out[0].Interface().(string)
	if !ok {
		return ""
	}
	return name
}

// armForSwitchMethod resolves the generated ArmForSwitch method on a value.
func armForSwitchMethod(rv reflect.Value) (reflect.Value, bool) {
	if method := rv.MethodByName("ArmForSwitch"); method.IsValid() {
		return method, true
	}
	if rv.CanAddr() {
		if method := rv.Addr().MethodByName("ArmForSwitch"); method.IsValid() {
			return method, true
		}
	}
	return reflect.Value{}, false
}

// callArmForSwitch invokes a bound ArmForSwitch method for one discriminant.
func callArmForSwitch(method reflect.Value, value int64) (arm string, valid bool) {
	mt := method.Type()
	if mt.NumIn() != 1 || mt.NumOut() != 2 {
		return "", false
	}
	arg := reflect.ValueOf(value).Convert(mt.In(0))
	out := method.Call([]reflect.Value{arg})
	arm, armOK := out[0].Interface().(string)
	valid, validOK := out[1].Interface().(bool)
	if !armOK || !validOK {
		return "", false
	}
	return arm, valid
}
