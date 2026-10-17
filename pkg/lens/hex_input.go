package lens

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

var (
	// ErrInvalidHex is returned when an input contains non-hexadecimal characters.
	ErrInvalidHex = errors.New("input is not valid hexadecimal")
	// ErrOddLengthHex is returned when an input contains an odd number of hex digits.
	ErrOddLengthHex = errors.New("hexadecimal string must have an even length")
	// ErrEmptyHex is returned when the input hex string is empty.
	ErrEmptyHex = errors.New("hexadecimal input is empty")
)

func isHexChar(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// IsHexString reports whether s is a valid hexadecimal string, ignoring whitespace
// and an optional "0x" or "0X" prefix. It returns false for empty strings or odd lengths.
func IsHexString(s string) bool {
	s = cleanHexString(s)
	if s == "" || len(s)%2 != 0 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isHexChar(s[i]) {
			return false
		}
	}
	return true
}

// cleanHexString trims whitespace and removes optional 0x/0X prefix.
func cleanHexString(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', '\t', ' ':
			return -1
		}
		return r
	}, s)
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		s = s[2:]
	}
	return s
}

// NormalizeHex cleans whitespace, removes optional 0x/0X prefix, validates hex digits,
// and returns the lowercase normalized hex string.
func NormalizeHex(s string) (string, error) {
	s = cleanHexString(s)
	if s == "" {
		return "", ErrEmptyHex
	}
	if len(s)%2 != 0 {
		return "", fmt.Errorf("%w: length %d is odd", ErrOddLengthHex, len(s))
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !isHexChar(c) {
			return "", fmt.Errorf("%w: invalid character %q at index %d", ErrInvalidHex, c, i)
		}
	}
	return strings.ToLower(s), nil
}

// HexToBytes decodes a hexadecimal string into raw bytes.
func HexToBytes(s string) ([]byte, error) {
	norm, err := NormalizeHex(s)
	if err != nil {
		return nil, err
	}
	b, err := hex.DecodeString(norm)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidHex, err)
	}
	return b, nil
}

// BytesToHex encodes raw bytes into a lowercase hexadecimal string.
func BytesToHex(b []byte) string {
	return hex.EncodeToString(b)
}

// HexToBase64 converts a hexadecimal XDR string into a standard base64 string.
func HexToBase64(hexStr string) (string, error) {
	b, err := HexToBytes(hexStr)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

// Base64ToHex converts a standard or URL-safe base64 XDR string into a lowercase hexadecimal string.
func Base64ToHex(b64 string) (string, error) {
	norm := normalizeInput(b64)
	if norm == "" {
		return "", fmt.Errorf("empty input")
	}
	b, err := base64.StdEncoding.DecodeString(norm)
	if err != nil {
		return "", fmt.Errorf("invalid base64: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// DetectHex decodes a hexadecimal payload against every registered XDR type and returns
// the candidates that succeeded, best first.
func DetectHex(hexStr string) ([]Candidate, error) {
	b64, err := HexToBase64(hexStr)
	if err != nil {
		return nil, err
	}
	return Detect(b64)
}

// DecodeHex decodes a hexadecimal XDR payload using the best-scoring detected type.
func DecodeHex(hexStr string) (*Value, error) {
	b64, err := HexToBase64(hexStr)
	if err != nil {
		return nil, err
	}
	return Decode(b64)
}

// DecodeHexAs decodes a hexadecimal XDR payload as the named XDR type.
func DecodeHexAs(hexStr, typeName string) (*Value, error) {
	b64, err := HexToBase64(hexStr)
	if err != nil {
		return nil, err
	}
	return DecodeAs(b64, typeName)
}
