package lens

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/stellar/go-stellar-sdk/xdr"
)

// A contract spec can be exported as JSON with
//
//	stellar contract info interface --wasm contract.wasm --output json
//
// which prints the ScSpecEntry stream in the stellar-xdr serde form: an array
// of single-key objects such as {"function_v0": {...}}, with snake_case field
// names, primitive types as strings ("u32", "address") and composite types as
// single-key objects ({"vec": {"element_type": "u32"}}). This file converts
// that form back into the XDR entries the rest of the package works with.

// isSpecJSON reports whether data looks like a JSON spec export. A raw XDR
// ScSpecEntry stream starts with a zero byte and a WASM module with "\0asm",
// so neither is mistaken for JSON.
func isSpecJSON(data []byte) bool {
	trimmed := bytes.TrimSpace(data)
	return len(trimmed) > 0 && (trimmed[0] == '[' || trimmed[0] == '{')
}

// parseSpecJSON decodes a JSON spec export. A single entry object is accepted
// as well as the array the CLI prints.
func parseSpecJSON(data []byte) ([]xdr.ScSpecEntry, error) {
	trimmed := bytes.TrimSpace(data)
	var raws []json.RawMessage
	if trimmed[0] == '{' {
		raws = []json.RawMessage{trimmed}
	} else if err := json.Unmarshal(trimmed, &raws); err != nil {
		return nil, fmt.Errorf("%w: JSON spec: %w", ErrInvalidContractSpec, err)
	}

	entries := make([]xdr.ScSpecEntry, 0, len(raws))
	for i, raw := range raws {
		entry, err := specEntryFromJSON(raw)
		if err != nil {
			return nil, fmt.Errorf("%w: JSON spec entry %d: %w", ErrInvalidContractSpec, i, err)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

type jsonSpecInput struct {
	Doc  string          `json:"doc"`
	Name string          `json:"name"`
	Type json.RawMessage `json:"type_"`
}

type jsonSpecFunction struct {
	Doc     string            `json:"doc"`
	Name    string            `json:"name"`
	Inputs  []jsonSpecInput   `json:"inputs"`
	Outputs []json.RawMessage `json:"outputs"`
}

type jsonSpecStruct struct {
	Doc    string          `json:"doc"`
	Lib    string          `json:"lib"`
	Name   string          `json:"name"`
	Fields []jsonSpecInput `json:"fields"`
}

type jsonSpecUnionCase struct {
	Doc   string            `json:"doc"`
	Name  string            `json:"name"`
	Types []json.RawMessage `json:"type_"`
}

type jsonSpecUnion struct {
	Doc   string                       `json:"doc"`
	Lib   string                       `json:"lib"`
	Name  string                       `json:"name"`
	Cases []map[string]json.RawMessage `json:"cases"`
}

type jsonSpecEnumCase struct {
	Doc   string `json:"doc"`
	Name  string `json:"name"`
	Value uint32 `json:"value"`
}

type jsonSpecEnum struct {
	Doc   string             `json:"doc"`
	Lib   string             `json:"lib"`
	Name  string             `json:"name"`
	Cases []jsonSpecEnumCase `json:"cases"`
}

type jsonSpecEventParam struct {
	Doc      string          `json:"doc"`
	Name     string          `json:"name"`
	Type     json.RawMessage `json:"type_"`
	Location string          `json:"location"`
}

type jsonSpecEvent struct {
	Doc          string               `json:"doc"`
	Lib          string               `json:"lib"`
	Name         string               `json:"name"`
	PrefixTopics []string             `json:"prefix_topics"`
	Params       []jsonSpecEventParam `json:"params"`
	DataFormat   string               `json:"data_format"`
}

// singleKey unpacks a serde externally tagged enum: an object with exactly
// one key naming the variant.
func singleKey(raw json.RawMessage) (string, json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return "", nil, err
	}
	if len(obj) != 1 {
		return "", nil, fmt.Errorf("want an object with one key, got %d key(s)", len(obj))
	}
	for k, v := range obj {
		return k, v, nil
	}
	return "", nil, nil
}

func specEntryFromJSON(raw json.RawMessage) (xdr.ScSpecEntry, error) {
	kind, body, err := singleKey(raw)
	if err != nil {
		return xdr.ScSpecEntry{}, err
	}
	switch kind {
	case "function_v0":
		var fn jsonSpecFunction
		if err := json.Unmarshal(body, &fn); err != nil {
			return xdr.ScSpecEntry{}, err
		}
		out := xdr.ScSpecFunctionV0{Doc: fn.Doc, Name: xdr.ScSymbol(fn.Name)}
		for _, in := range fn.Inputs {
			td, err := specTypeFromJSON(in.Type)
			if err != nil {
				return xdr.ScSpecEntry{}, fmt.Errorf("function %s input %s: %w", fn.Name, in.Name, err)
			}
			out.Inputs = append(out.Inputs, xdr.ScSpecFunctionInputV0{Doc: in.Doc, Name: in.Name, Type: td})
		}
		for _, o := range fn.Outputs {
			td, err := specTypeFromJSON(o)
			if err != nil {
				return xdr.ScSpecEntry{}, fmt.Errorf("function %s output: %w", fn.Name, err)
			}
			out.Outputs = append(out.Outputs, td)
		}
		return xdr.ScSpecEntry{Kind: xdr.ScSpecEntryKindScSpecEntryFunctionV0, FunctionV0: &out}, nil

	case "udt_struct_v0":
		var st jsonSpecStruct
		if err := json.Unmarshal(body, &st); err != nil {
			return xdr.ScSpecEntry{}, err
		}
		out := xdr.ScSpecUdtStructV0{Doc: st.Doc, Lib: st.Lib, Name: st.Name}
		for _, f := range st.Fields {
			td, err := specTypeFromJSON(f.Type)
			if err != nil {
				return xdr.ScSpecEntry{}, fmt.Errorf("struct %s field %s: %w", st.Name, f.Name, err)
			}
			out.Fields = append(out.Fields, xdr.ScSpecUdtStructFieldV0{Doc: f.Doc, Name: f.Name, Type: td})
		}
		return xdr.ScSpecEntry{Kind: xdr.ScSpecEntryKindScSpecEntryUdtStructV0, UdtStructV0: &out}, nil

	case "udt_union_v0":
		var un jsonSpecUnion
		if err := json.Unmarshal(body, &un); err != nil {
			return xdr.ScSpecEntry{}, err
		}
		out := xdr.ScSpecUdtUnionV0{Doc: un.Doc, Lib: un.Lib, Name: un.Name}
		for _, rawCase := range un.Cases {
			c, err := unionCaseFromJSON(rawCase)
			if err != nil {
				return xdr.ScSpecEntry{}, fmt.Errorf("union %s: %w", un.Name, err)
			}
			out.Cases = append(out.Cases, c)
		}
		return xdr.ScSpecEntry{Kind: xdr.ScSpecEntryKindScSpecEntryUdtUnionV0, UdtUnionV0: &out}, nil

	case "udt_enum_v0":
		var en jsonSpecEnum
		if err := json.Unmarshal(body, &en); err != nil {
			return xdr.ScSpecEntry{}, err
		}
		out := xdr.ScSpecUdtEnumV0{Doc: en.Doc, Lib: en.Lib, Name: en.Name}
		for _, c := range en.Cases {
			out.Cases = append(out.Cases, xdr.ScSpecUdtEnumCaseV0{Doc: c.Doc, Name: c.Name, Value: xdr.Uint32(c.Value)})
		}
		return xdr.ScSpecEntry{Kind: xdr.ScSpecEntryKindScSpecEntryUdtEnumV0, UdtEnumV0: &out}, nil

	case "udt_error_enum_v0":
		var en jsonSpecEnum
		if err := json.Unmarshal(body, &en); err != nil {
			return xdr.ScSpecEntry{}, err
		}
		out := xdr.ScSpecUdtErrorEnumV0{Doc: en.Doc, Lib: en.Lib, Name: en.Name}
		for _, c := range en.Cases {
			out.Cases = append(out.Cases, xdr.ScSpecUdtErrorEnumCaseV0{Doc: c.Doc, Name: c.Name, Value: xdr.Uint32(c.Value)})
		}
		return xdr.ScSpecEntry{Kind: xdr.ScSpecEntryKindScSpecEntryUdtErrorEnumV0, UdtErrorEnumV0: &out}, nil

	case "event_v0":
		var ev jsonSpecEvent
		if err := json.Unmarshal(body, &ev); err != nil {
			return xdr.ScSpecEntry{}, err
		}
		out := xdr.ScSpecEventV0{Doc: ev.Doc, Lib: ev.Lib, Name: xdr.ScSymbol(ev.Name)}
		for _, topic := range ev.PrefixTopics {
			out.PrefixTopics = append(out.PrefixTopics, xdr.ScSymbol(topic))
		}
		for _, p := range ev.Params {
			td, err := specTypeFromJSON(p.Type)
			if err != nil {
				return xdr.ScSpecEntry{}, fmt.Errorf("event %s param %s: %w", ev.Name, p.Name, err)
			}
			param := xdr.ScSpecEventParamV0{Doc: p.Doc, Name: p.Name, Type: td}
			switch p.Location {
			case "data", "":
				param.Location = xdr.ScSpecEventParamLocationV0ScSpecEventParamLocationData
			case "topic_list":
				param.Location = xdr.ScSpecEventParamLocationV0ScSpecEventParamLocationTopicList
			default:
				return xdr.ScSpecEntry{}, fmt.Errorf("event %s param %s: unknown location %q", ev.Name, p.Name, p.Location)
			}
			out.Params = append(out.Params, param)
		}
		switch ev.DataFormat {
		case "single_value", "":
			out.DataFormat = xdr.ScSpecEventDataFormatScSpecEventDataFormatSingleValue
		case "vec":
			out.DataFormat = xdr.ScSpecEventDataFormatScSpecEventDataFormatVec
		case "map":
			out.DataFormat = xdr.ScSpecEventDataFormatScSpecEventDataFormatMap
		default:
			return xdr.ScSpecEntry{}, fmt.Errorf("event %s: unknown data format %q", ev.Name, ev.DataFormat)
		}
		return xdr.ScSpecEntry{Kind: xdr.ScSpecEntryKindScSpecEntryEventV0, EventV0: &out}, nil
	}
	return xdr.ScSpecEntry{}, fmt.Errorf("unknown spec entry kind %q", kind)
}

func unionCaseFromJSON(raw map[string]json.RawMessage) (xdr.ScSpecUdtUnionCaseV0, error) {
	if len(raw) != 1 {
		return xdr.ScSpecUdtUnionCaseV0{}, fmt.Errorf("union case: want an object with one key, got %d key(s)", len(raw))
	}
	for kind, body := range raw {
		var c jsonSpecUnionCase
		if err := json.Unmarshal(body, &c); err != nil {
			return xdr.ScSpecUdtUnionCaseV0{}, err
		}
		switch kind {
		case "void_v0":
			return xdr.ScSpecUdtUnionCaseV0{
				Kind:     xdr.ScSpecUdtUnionCaseV0KindScSpecUdtUnionCaseVoidV0,
				VoidCase: &xdr.ScSpecUdtUnionCaseVoidV0{Doc: c.Doc, Name: c.Name},
			}, nil
		case "tuple_v0":
			tuple := xdr.ScSpecUdtUnionCaseTupleV0{Doc: c.Doc, Name: c.Name}
			for _, t := range c.Types {
				td, err := specTypeFromJSON(t)
				if err != nil {
					return xdr.ScSpecUdtUnionCaseV0{}, fmt.Errorf("case %s: %w", c.Name, err)
				}
				tuple.Type = append(tuple.Type, td)
			}
			return xdr.ScSpecUdtUnionCaseV0{
				Kind:      xdr.ScSpecUdtUnionCaseV0KindScSpecUdtUnionCaseTupleV0,
				TupleCase: &tuple,
			}, nil
		default:
			return xdr.ScSpecUdtUnionCaseV0{}, fmt.Errorf("unknown union case kind %q", kind)
		}
	}
	return xdr.ScSpecUdtUnionCaseV0{}, nil
}

// jsonSpecPrimitives maps the serde names of the payload-free ScSpecType
// variants to their XDR discriminants.
var jsonSpecPrimitives = map[string]xdr.ScSpecType{
	"val":           xdr.ScSpecTypeScSpecTypeVal,
	"bool":          xdr.ScSpecTypeScSpecTypeBool,
	"void":          xdr.ScSpecTypeScSpecTypeVoid,
	"error":         xdr.ScSpecTypeScSpecTypeError,
	"u32":           xdr.ScSpecTypeScSpecTypeU32,
	"i32":           xdr.ScSpecTypeScSpecTypeI32,
	"u64":           xdr.ScSpecTypeScSpecTypeU64,
	"i64":           xdr.ScSpecTypeScSpecTypeI64,
	"timepoint":     xdr.ScSpecTypeScSpecTypeTimepoint,
	"duration":      xdr.ScSpecTypeScSpecTypeDuration,
	"u128":          xdr.ScSpecTypeScSpecTypeU128,
	"i128":          xdr.ScSpecTypeScSpecTypeI128,
	"u256":          xdr.ScSpecTypeScSpecTypeU256,
	"i256":          xdr.ScSpecTypeScSpecTypeI256,
	"bytes":         xdr.ScSpecTypeScSpecTypeBytes,
	"string":        xdr.ScSpecTypeScSpecTypeString,
	"symbol":        xdr.ScSpecTypeScSpecTypeSymbol,
	"address":       xdr.ScSpecTypeScSpecTypeAddress,
	"muxed_address": xdr.ScSpecTypeScSpecTypeMuxedAddress,
}

type jsonSpecComposite struct {
	ValueType   json.RawMessage   `json:"value_type"`
	OkType      json.RawMessage   `json:"ok_type"`
	ErrorType   json.RawMessage   `json:"error_type"`
	ElementType json.RawMessage   `json:"element_type"`
	KeyType     json.RawMessage   `json:"key_type"`
	ValueTypes  []json.RawMessage `json:"value_types"`
	N           uint32            `json:"n"`
	Name        string            `json:"name"`
}

func specTypeFromJSON(raw json.RawMessage) (xdr.ScSpecTypeDef, error) {
	if len(raw) == 0 {
		return xdr.ScSpecTypeDef{}, fmt.Errorf("missing type")
	}
	var name string
	if err := json.Unmarshal(raw, &name); err == nil {
		t, ok := jsonSpecPrimitives[name]
		if !ok {
			return xdr.ScSpecTypeDef{}, fmt.Errorf("unknown type %q", name)
		}
		return xdr.ScSpecTypeDef{Type: t}, nil
	}

	kind, body, err := singleKey(raw)
	if err != nil {
		return xdr.ScSpecTypeDef{}, fmt.Errorf("type: %w", err)
	}
	var c jsonSpecComposite
	if err := json.Unmarshal(body, &c); err != nil {
		return xdr.ScSpecTypeDef{}, fmt.Errorf("type %s: %w", kind, err)
	}
	sub := func(r json.RawMessage) (xdr.ScSpecTypeDef, error) {
		td, err := specTypeFromJSON(r)
		if err != nil {
			return xdr.ScSpecTypeDef{}, fmt.Errorf("%s: %w", kind, err)
		}
		return td, nil
	}

	switch kind {
	case "option":
		v, err := sub(c.ValueType)
		if err != nil {
			return xdr.ScSpecTypeDef{}, err
		}
		return xdr.ScSpecTypeDef{Type: xdr.ScSpecTypeScSpecTypeOption, Option: &xdr.ScSpecTypeOption{ValueType: v}}, nil
	case "result":
		ok, err := sub(c.OkType)
		if err != nil {
			return xdr.ScSpecTypeDef{}, err
		}
		e, err := sub(c.ErrorType)
		if err != nil {
			return xdr.ScSpecTypeDef{}, err
		}
		return xdr.ScSpecTypeDef{Type: xdr.ScSpecTypeScSpecTypeResult, Result: &xdr.ScSpecTypeResult{OkType: ok, ErrorType: e}}, nil
	case "vec":
		el, err := sub(c.ElementType)
		if err != nil {
			return xdr.ScSpecTypeDef{}, err
		}
		return xdr.ScSpecTypeDef{Type: xdr.ScSpecTypeScSpecTypeVec, Vec: &xdr.ScSpecTypeVec{ElementType: el}}, nil
	case "map":
		k, err := sub(c.KeyType)
		if err != nil {
			return xdr.ScSpecTypeDef{}, err
		}
		v, err := sub(c.ValueType)
		if err != nil {
			return xdr.ScSpecTypeDef{}, err
		}
		return xdr.ScSpecTypeDef{Type: xdr.ScSpecTypeScSpecTypeMap, Map: &xdr.ScSpecTypeMap{KeyType: k, ValueType: v}}, nil
	case "tuple":
		tuple := &xdr.ScSpecTypeTuple{}
		for _, r := range c.ValueTypes {
			td, err := sub(r)
			if err != nil {
				return xdr.ScSpecTypeDef{}, err
			}
			tuple.ValueTypes = append(tuple.ValueTypes, td)
		}
		return xdr.ScSpecTypeDef{Type: xdr.ScSpecTypeScSpecTypeTuple, Tuple: tuple}, nil
	case "bytes_n":
		return xdr.ScSpecTypeDef{Type: xdr.ScSpecTypeScSpecTypeBytesN, BytesN: &xdr.ScSpecTypeBytesN{N: xdr.Uint32(c.N)}}, nil
	case "udt":
		if c.Name == "" {
			return xdr.ScSpecTypeDef{}, fmt.Errorf("udt type without a name")
		}
		return xdr.ScSpecTypeDef{Type: xdr.ScSpecTypeScSpecTypeUdt, Udt: &xdr.ScSpecTypeUdt{Name: c.Name}}, nil
	}
	return xdr.ScSpecTypeDef{}, fmt.Errorf("unknown type %q", kind)
}
