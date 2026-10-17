package lens

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestYAMLJSONRoundTrip(t *testing.T) {
	yamlInput := `
name: Stellar XDR Lens
version: 1
settings:
  offline: true
  formats:
    - tree
    - json
    - yaml
metadata:
  nested:
    count: 42
    ratio: 3.14
    null_val: null
`

	jsonBytes, err := YAMLToJSON([]byte(yamlInput))
	if err != nil {
		t.Fatalf("YAMLToJSON failed: %v", err)
	}

	var jsonMap map[string]any
	if err := json.Unmarshal(jsonBytes, &jsonMap); err != nil {
		t.Fatalf("Unmarshal JSON failed: %v", err)
	}

	if jsonMap["name"] != "Stellar XDR Lens" {
		t.Errorf("name mismatch: %v", jsonMap["name"])
	}

	yamlBack, err := JSONToYAML(jsonBytes)
	if err != nil {
		t.Fatalf("JSONToYAML failed: %v", err)
	}

	jsonBack, err := YAMLToJSON(yamlBack)
	if err != nil {
		t.Fatalf("YAMLToJSON on converted yaml failed: %v", err)
	}

	var jsonMapBack map[string]any
	if err := json.Unmarshal(jsonBack, &jsonMapBack); err != nil {
		t.Fatalf("Unmarshal second JSON failed: %v", err)
	}

	if jsonMapBack["name"] != "Stellar XDR Lens" {
		t.Errorf("roundtrip mismatch: %v", jsonMapBack["name"])
	}
}

func TestYAMLCanonicalXDRFixtures(t *testing.T) {
	testdataDir := filepath.Join("..", "..", "testdata")

	fixtures := []string{
		"tx_success.env.txt",
		"tx_failed.env.txt",
		"tx_feebump.env.txt",
		"tx_soroban.env.txt",
	}

	for _, fix := range fixtures {
		t.Run(fix, func(t *testing.T) {
			b64Bytes, err := os.ReadFile(filepath.Join(testdataDir, fix))
			if err != nil {
				t.Fatalf("reading fixture %s: %v", fix, err)
			}
			b64 := strings.TrimSpace(string(b64Bytes))

			// Decode fixture to Value
			val, err := Decode(b64)
			if err != nil {
				t.Fatalf("Decode failed: %v", err)
			}

			// Value to YAML
			yamlBytes, err := ValueToYAML(val, true)
			if err != nil {
				t.Fatalf("ValueToYAML failed: %v", err)
			}

			if !strings.Contains(string(yamlBytes), "type: "+val.Type) {
				t.Errorf("YAML missing type %s", val.Type)
			}
			if !strings.Contains(string(yamlBytes), "xdr: "+b64) {
				t.Errorf("YAML missing embedded xdr payload")
			}

			// Convert YAML back to XDR
			rawBytes, extractedB64, err := YAMLToXDR(yamlBytes, val.Type)
			if err != nil {
				t.Fatalf("YAMLToXDR failed: %v", err)
			}

			if extractedB64 != b64 {
				t.Errorf("extracted base64 mismatch: got %s, want %s", extractedB64, b64)
			}
			if len(rawBytes) == 0 {
				t.Errorf("expected non-empty raw bytes")
			}

			// Test XDRToYAML with base64 and hex
			yamlFromXDR, err := XDRToYAML(b64, val.Type, false)
			if err != nil {
				t.Fatalf("XDRToYAML failed: %v", err)
			}
			if len(yamlFromXDR) == 0 {
				t.Errorf("empty YAML from XDR")
			}

			hexStr, err := Base64ToHex(b64)
			if err != nil {
				t.Fatalf("Base64ToHex failed: %v", err)
			}
			yamlFromHex, err := XDRToYAML("0x"+hexStr, val.Type, false)
			if err != nil {
				t.Fatalf("XDRToYAML hex failed: %v", err)
			}
			if !bytes.Equal(yamlFromXDR, yamlFromHex) {
				t.Errorf("YAML from base64 and hex mismatch")
			}

			// Test NodeToYAML directly
			nodeYAML, err := NodeToYAML(val.Node, false)
			if err != nil {
				t.Fatalf("NodeToYAML failed: %v", err)
			}
			if len(nodeYAML) == 0 {
				t.Errorf("empty YAML from NodeToYAML")
			}
		})
	}
}

func TestYAMLErrorsAndEdgeCases(t *testing.T) {
	// Empty YAML
	if _, err := YAMLToJSON([]byte("   \n\t  ")); !errors.Is(err, ErrEmptyYAML) {
		t.Errorf("expected ErrEmptyYAML, got %v", err)
	}
	if _, _, err := YAMLToXDR([]byte("   \n\t  "), ""); !errors.Is(err, ErrEmptyYAML) {
		t.Errorf("expected ErrEmptyYAML from YAMLToXDR, got %v", err)
	}

	// Invalid YAML
	if _, err := YAMLToJSON([]byte("foo: [unclosed list")); !errors.Is(err, ErrInvalidYAML) {
		t.Errorf("expected ErrInvalidYAML, got %v", err)
	}
	if _, _, err := YAMLToXDR([]byte("foo: [unclosed list"), ""); !errors.Is(err, ErrInvalidYAML) {
		t.Errorf("expected ErrInvalidYAML, got %v", err)
	}

	// Missing XDR in YAML
	missingXDRDoc := `
some_field: 123
other_field: hello
`
	if _, _, err := YAMLToXDR([]byte(missingXDRDoc), ""); !errors.Is(err, ErrMissingXDRData) {
		t.Errorf("expected ErrMissingXDRData, got %v", err)
	}

	// Empty JSON
	if _, err := JSONToYAML([]byte("   ")); err == nil {
		t.Errorf("JSONToYAML(empty) expected error, got nil")
	}
	// Invalid JSON
	if _, err := JSONToYAML([]byte("not valid json")); err == nil {
		t.Errorf("JSONToYAML(invalid) expected error, got nil")
	}

	// Nil Node or Value
	if _, err := NodeToYAML(nil, false); err == nil {
		t.Errorf("NodeToYAML(nil) expected error, got nil")
	}
	if _, err := ValueToYAML(nil, false); err == nil {
		t.Errorf("ValueToYAML(nil) expected error, got nil")
	}

	// SortMapKeys
	keys := SortMapKeys(map[string]any{"z": 1, "a": 2, "m": 3})
	if len(keys) != 3 || keys[0] != "a" || keys[1] != "m" || keys[2] != "z" {
		t.Errorf("SortMapKeys result unexpected: %v", keys)
	}

	// YAML as bare string
	bareYAML := []byte(`"AAAAAA=="`)
	_, b64, err := YAMLToXDR(bareYAML, "Memo")
	if err != nil || b64 != "AAAAAA==" {
		t.Errorf("YAMLToXDR(bare string) failed: %v, b64=%s", err, b64)
	}

	// YAML with base64 field
	docYAML := []byte("base64: AAAAAA==\ntype: Memo\n")
	_, b64, err = YAMLToXDR(docYAML, "")
	if err != nil || b64 != "AAAAAA==" {
		t.Errorf("YAMLToXDR(base64 doc) failed: %v, b64=%s", err, b64)
	}

	// XDRToYAML without type name
	yamlAuto, err := XDRToYAML("AAAAAA==", "", false)
	if err != nil || len(yamlAuto) == 0 {
		t.Errorf("XDRToYAML(auto) failed: %v", err)
	}
}
