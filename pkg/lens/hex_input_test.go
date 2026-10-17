package lens

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHexInputValidation(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantNorm  string
		wantValid bool
		wantErr   error
	}{
		{
			name:      "empty",
			input:     "",
			wantValid: false,
			wantErr:   ErrEmptyHex,
		},
		{
			name:      "whitespace only",
			input:     "   \n\t  ",
			wantValid: false,
			wantErr:   ErrEmptyHex,
		},
		{
			name:      "odd length",
			input:     "0xabc",
			wantValid: false,
			wantErr:   ErrOddLengthHex,
		},
		{
			name:      "invalid chars",
			input:     "0x12zz",
			wantValid: false,
			wantErr:   ErrInvalidHex,
		},
		{
			name:      "valid simple",
			input:     "deadbeef",
			wantNorm:  "deadbeef",
			wantValid: true,
			wantErr:   nil,
		},
		{
			name:      "valid with 0x prefix",
			input:     "0xDEADBEEF",
			wantNorm:  "deadbeef",
			wantValid: true,
			wantErr:   nil,
		},
		{
			name:      "valid with 0X prefix and spaces",
			input:     "  0X1234 \n 5678  ",
			wantNorm:  "12345678",
			wantValid: true,
			wantErr:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isValid := IsHexString(tt.input)
			if isValid != tt.wantValid {
				t.Errorf("IsHexString(%q) = %v, want %v", tt.input, isValid, tt.wantValid)
			}

			norm, err := NormalizeHex(tt.input)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("NormalizeHex(%q) expected error, got nil", tt.input)
				}
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("NormalizeHex(%q) err = %v, want %v", tt.input, err, tt.wantErr)
				}
			} else {
				if err != nil {
					t.Fatalf("NormalizeHex(%q) unexpected error: %v", tt.input, err)
				}
				if norm != tt.wantNorm {
					t.Errorf("NormalizeHex(%q) = %q, want %q", tt.input, norm, tt.wantNorm)
				}
			}
		})
	}
}

func TestHexRoundtripAndConversions(t *testing.T) {
	rawBytes := []byte{0x00, 0x01, 0x02, 0xfe, 0xff}
	hexStr := BytesToHex(rawBytes)
	if hexStr != "000102feff" {
		t.Fatalf("BytesToHex failed, got %q", hexStr)
	}

	decodedBytes, err := HexToBytes("0x" + strings.ToUpper(hexStr))
	if err != nil {
		t.Fatalf("HexToBytes failed: %v", err)
	}
	if string(decodedBytes) != string(rawBytes) {
		t.Fatalf("HexToBytes mismatch: got %v, want %v", decodedBytes, rawBytes)
	}

	// Hex <-> Base64
	b64, err := HexToBase64(hexStr)
	if err != nil {
		t.Fatalf("HexToBase64 failed: %v", err)
	}
	hexBack, err := Base64ToHex(b64)
	if err != nil {
		t.Fatalf("Base64ToHex failed: %v", err)
	}
	if hexBack != hexStr {
		t.Fatalf("Base64ToHex mismatch: got %q, want %q", hexBack, hexStr)
	}

	// Empty Base64 to Hex
	if _, err := Base64ToHex(""); err == nil {
		t.Errorf("Base64ToHex(\"\") expected error, got nil")
	}
	// Invalid Base64 to Hex
	if _, err := Base64ToHex("!!!invalid!!!"); err == nil {
		t.Errorf("Base64ToHex(invalid) expected error, got nil")
	}
	// Invalid Hex to Bytes
	if _, err := HexToBytes("odd"); err == nil {
		t.Errorf("HexToBytes(odd) expected error, got nil")
	}
}

func TestDecodeHexCanonicalFixtures(t *testing.T) {
	fixtures := []string{
		"tx_success.env.txt",
		"tx_failed.env.txt",
		"tx_feebump.env.txt",
		"tx_soroban.env.txt",
		"tx_success.res.txt",
	}

	testdataDir := filepath.Join("..", "..", "testdata")

	for _, fix := range fixtures {
		t.Run(fix, func(t *testing.T) {
			b64Bytes, err := os.ReadFile(filepath.Join(testdataDir, fix))
			if err != nil {
				t.Fatalf("reading fixture %s: %v", fix, err)
			}
			b64 := strings.TrimSpace(string(b64Bytes))

			// Decode base64 value directly
			valB64, err := Decode(b64)
			if err != nil {
				t.Fatalf("Decode(b64) failed: %v", err)
			}

			// Convert base64 to hex
			hexStr, err := Base64ToHex(b64)
			if err != nil {
				t.Fatalf("Base64ToHex failed: %v", err)
			}

			// Decode from hex
			valHex, err := DecodeHex("0x" + hexStr)
			if err != nil {
				t.Fatalf("DecodeHex failed: %v", err)
			}

			if valHex.Type != valB64.Type {
				t.Errorf("Type mismatch: hex decoded %s, base64 decoded %s", valHex.Type, valB64.Type)
			}

			// Decode from hex with explicit type
			valHexAs, err := DecodeHexAs(hexStr, valB64.Type)
			if err != nil {
				t.Fatalf("DecodeHexAs failed: %v", err)
			}
			if valHexAs.Type != valB64.Type {
				t.Errorf("Type mismatch for DecodeHexAs: %s vs %s", valHexAs.Type, valB64.Type)
			}

			// Detect from hex
			cands, err := DetectHex(hexStr)
			if err != nil {
				t.Fatalf("DetectHex failed: %v", err)
			}
			if len(cands) == 0 || cands[0].Type != valB64.Type {
				t.Errorf("DetectHex best candidate %v, want %s", cands, valB64.Type)
			}
		})
	}
}

func TestDecodeHexErrors(t *testing.T) {
	if _, err := DecodeHex("invalid_hex"); err == nil {
		t.Errorf("DecodeHex(invalid) expected error, got nil")
	}
	if _, err := DecodeHexAs("invalid_hex", "TransactionEnvelope"); err == nil {
		t.Errorf("DecodeHexAs(invalid) expected error, got nil")
	}
	if _, err := DetectHex("invalid_hex"); err == nil {
		t.Errorf("DetectHex(invalid) expected error, got nil")
	}
	if _, err := HexToBase64("invalid_hex"); err == nil {
		t.Errorf("HexToBase64(invalid) expected error, got nil")
	}
}
