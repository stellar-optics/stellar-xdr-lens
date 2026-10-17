package lens

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	// ErrInvalidYAML is returned when input cannot be parsed as YAML.
	ErrInvalidYAML = errors.New("input is not valid YAML")
	// ErrEmptyYAML is returned when YAML input is empty or whitespace only.
	ErrEmptyYAML = errors.New("YAML input is empty")
	// ErrMissingXDRData is returned when YAML cannot be converted to XDR.
	ErrMissingXDRData = errors.New("YAML document does not contain valid XDR data")
)

// YAMLDocument represents the standard YAML envelope for decoded XDR values.
type YAMLDocument struct {
	Type  string `yaml:"type"`
	Value any    `yaml:"value"`
	XDR   string `yaml:"xdr,omitempty"`
}

// YAMLToJSON converts arbitrary YAML bytes into formatted, deterministic JSON bytes.
func YAMLToJSON(yamlData []byte) ([]byte, error) {
	if len(bytes.TrimSpace(yamlData)) == 0 {
		return nil, ErrEmptyYAML
	}

	var root any
	if err := yaml.Unmarshal(yamlData, &root); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidYAML, err)
	}

	cleaned := cleanGenericValue(root)
	jsonBytes, err := json.MarshalIndent(cleaned, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encoding JSON: %w", err)
	}
	return jsonBytes, nil
}

// JSONToYAML converts arbitrary JSON bytes into formatted YAML bytes.
func JSONToYAML(jsonData []byte) ([]byte, error) {
	if len(bytes.TrimSpace(jsonData)) == 0 {
		return nil, fmt.Errorf("JSON input is empty")
	}

	var root any
	if err := json.Unmarshal(jsonData, &root); err != nil {
		return nil, fmt.Errorf("parsing JSON: %w", err)
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(root); err != nil {
		return nil, fmt.Errorf("encoding YAML: %w", err)
	}
	_ = enc.Close()

	return buf.Bytes(), nil
}

// NodeToGeneric converts a Node tree into generic Go maps, slices, and scalars.
func NodeToGeneric(n *Node, annotate bool) any {
	if n == nil {
		return nil
	}
	switch n.Kind {
	case KindList:
		out := make([]any, 0, len(n.Children))
		for _, c := range n.Children {
			out = append(out, NodeToGeneric(c, annotate))
		}
		return out

	case KindStruct:
		out := make(map[string]any, len(n.Children))
		for _, c := range n.Children {
			out[c.Name] = NodeToGeneric(c, annotate)
		}
		return out

	case KindScalar:
		if annotate && n.Note != "" {
			return map[string]any{
				"value":   n.Value,
				"display": n.Note,
			}
		}
		return n.Value

	default:
		return nil
	}
}

// NodeToYAML serializes a Node tree into formatted YAML bytes.
func NodeToYAML(n *Node, annotate bool) ([]byte, error) {
	if n == nil {
		return nil, fmt.Errorf("node is nil")
	}
	data := NodeToGeneric(n, annotate)

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(data); err != nil {
		return nil, fmt.Errorf("encoding YAML: %w", err)
	}
	_ = enc.Close()

	return buf.Bytes(), nil
}

// ValueToYAML formats a decoded Value into a clean YAML document with type and tree value.
func ValueToYAML(v *Value, annotate bool) ([]byte, error) {
	if v == nil || v.Node == nil {
		return nil, fmt.Errorf("value or node is nil")
	}

	var xdrStr string
	if v.Raw != nil {
		if b64, err := SafeMarshalBase64(v.Raw); err == nil {
			xdrStr = b64
		}
	}

	doc := YAMLDocument{
		Type:  v.Type,
		Value: NodeToGeneric(v.Node, annotate),
		XDR:   xdrStr,
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, fmt.Errorf("encoding YAML document: %w", err)
	}
	_ = enc.Close()

	return buf.Bytes(), nil
}

// XDRToYAML decodes a base64 or hexadecimal XDR payload and emits formatted YAML.
func XDRToYAML(payload string, typeName string, annotate bool) ([]byte, error) {
	var val *Value
	var err error

	if IsHexString(payload) {
		if typeName != "" {
			val, err = DecodeHexAs(payload, typeName)
		} else {
			val, err = DecodeHex(payload)
		}
	} else {
		if typeName != "" {
			val, err = DecodeAs(payload, typeName)
		} else {
			val, err = Decode(payload)
		}
	}

	if err != nil {
		return nil, err
	}

	return ValueToYAML(val, annotate)
}

// YAMLToXDR parses a YAML document and returns raw binary XDR bytes and base64 string.
func YAMLToXDR(yamlData []byte, typeName string) ([]byte, string, error) {
	if len(bytes.TrimSpace(yamlData)) == 0 {
		return nil, "", ErrEmptyYAML
	}

	var parsed any
	if err := yaml.Unmarshal(yamlData, &parsed); err != nil {
		return nil, "", fmt.Errorf("%w: %w", ErrInvalidYAML, err)
	}

	// 1. Check if parsed is a simple string payload (base64 or hex)
	if s, ok := parsed.(string); ok {
		return resolvePayloadToXDR(strings.TrimSpace(s), typeName)
	}

	// 2. Check if parsed is a map containing an xdr, base64, or payload field
	if m, ok := parsed.(map[string]any); ok {
		resolvedType := typeName
		if resolvedType == "" {
			if t, ok := m["type"].(string); ok {
				resolvedType = t
			}
		}

		for _, key := range []string{"xdr", "base64", "payload", "raw"} {
			if val, exists := m[key]; exists {
				if s, ok := val.(string); ok && strings.TrimSpace(s) != "" {
					return resolvePayloadToXDR(strings.TrimSpace(s), resolvedType)
				}
			}
		}
	}

	// 3. Fallback: Check if generic map contains any of the above
	cleaned := cleanGenericValue(parsed)
	if m, ok := cleaned.(map[string]any); ok {
		resolvedType := typeName
		if resolvedType == "" {
			if t, ok := m["type"].(string); ok {
				resolvedType = t
			}
		}

		for _, key := range []string{"xdr", "base64", "payload", "raw"} {
			if val, exists := m[key]; exists {
				if s, ok := val.(string); ok && strings.TrimSpace(s) != "" {
					return resolvePayloadToXDR(strings.TrimSpace(s), resolvedType)
				}
			}
		}
	}

	return nil, "", fmt.Errorf("%w: expected string payload or object with 'xdr'/'base64' field", ErrMissingXDRData)
}

func resolvePayloadToXDR(payload, typeName string) ([]byte, string, error) {
	var val *Value
	var err error

	if IsHexString(payload) {
		if typeName != "" {
			val, err = DecodeHexAs(payload, typeName)
		} else {
			val, err = DecodeHex(payload)
		}
	} else {
		if typeName != "" {
			val, err = DecodeAs(payload, typeName)
		} else {
			val, err = Decode(payload)
		}
	}

	if err != nil {
		return nil, "", err
	}

	b64, err := SafeMarshalBase64(val.Raw)
	if err != nil {
		return nil, "", err
	}

	rawBytes, err := HexToBytes(mustBase64ToHex(b64))
	if err != nil {
		return nil, "", err
	}

	return rawBytes, b64, nil
}

func mustBase64ToHex(b64 string) string {
	h, err := Base64ToHex(b64)
	if err != nil {
		return ""
	}
	return h
}

// cleanGenericValue recursively ensures all maps have string keys so json.Marshal succeeds.
func cleanGenericValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		res := make(map[string]any, len(val))
		for k, item := range val {
			res[k] = cleanGenericValue(item)
		}
		return res
	case map[any]any:
		res := make(map[string]any, len(val))
		for k, item := range val {
			res[fmt.Sprintf("%v", k)] = cleanGenericValue(item)
		}
		return res
	case []any:
		res := make([]any, len(val))
		for i, item := range val {
			res[i] = cleanGenericValue(item)
		}
		return res
	default:
		return v
	}
}

// SortMapKeys is a helper returning sorted map keys for deterministic representation.
func SortMapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
