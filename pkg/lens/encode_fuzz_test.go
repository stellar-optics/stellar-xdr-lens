package lens

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// RunStreamParserFuzz executes the stream parser against arbitrary byte input without panicking.
func RunStreamParserFuzz(data []byte) error {
	const maxRecSize = 1024 * 1024

	framings := []StreamFraming{
		FramingAuto,
		FramingRecordMarking,
		FramingLengthPrefixed,
		FramingBase64Lines,
	}

	var firstErr error
	for _, framing := range framings {
		p := NewStreamParser(
			bytes.NewReader(data),
			WithFraming(framing),
			WithMaxRecordSize(maxRecSize),
		)
		for {
			rec, err := p.Next()
			if err != nil {
				if !errors.Is(err, io.EOF) && firstErr == nil {
					firstErr = err
				}
				break
			}
			if uint32(len(rec)) > maxRecSize {
				panic("stream parser returned record exceeding maxRecordSize")
			}
		}
	}
	return firstErr
}

// RunHexInputFuzz exercises hex and base64 input normalization and decoding routines
// against arbitrary string inputs without panicking.
func RunHexInputFuzz(input string) {
	isHex := IsHexString(input)
	norm, normErr := NormalizeHex(input)
	if isHex && normErr != nil {
		panic("IsHexString returned true but NormalizeHex returned error")
	}
	if !isHex && normErr == nil {
		panic("IsHexString returned false but NormalizeHex succeeded")
	}

	raw, bytesErr := HexToBytes(input)
	if bytesErr == nil {
		if BytesToHex(raw) != norm {
			panic("HexToBytes -> BytesToHex did not round-trip to NormalizeHex")
		}
		if b64, err := HexToBase64(input); err != nil {
			panic("HexToBytes succeeded but HexToBase64 failed")
		} else if roundHex, err := Base64ToHex(b64); err != nil || roundHex != norm {
			panic("HexToBase64 -> Base64ToHex did not round-trip")
		}
	}

	_, _ = Base64ToHex(input)
	_, _ = DecodeHex(input)
	_, _ = Decode(input)
}

// RunCanonicalFuzz executes canonical validation and canonicalization against arbitrary byte input without panicking.
func RunCanonicalFuzz(data []byte) {
	defer func() {
		if r := recover(); r != nil {
			panic(r)
		}
	}()

	_ = ValidateCanonicalXDR(data)
	_ = IsCanonicalXDR(data)
	_, _ = CanonicalizeXDR(data)
}

// RunDiagnosticFuzz executes JSON error diagnostics against arbitrary byte input without panicking.
func RunDiagnosticFuzz(data []byte, path string) {
	defer func() {
		if r := recover(); r != nil {
			panic(r)
		}
	}()

	_, _ = FindJSONPathLocation(data, path)
	_ = FormatDiagnostic(data, errors.New("fuzz test error"))
	_ = WrapWithDiagnostics(data, errors.New("fuzz test error"))
	_, _ = ReadAndValidateJSON(bytes.NewReader(data))
}

// FuzzStreamParser fuzzes the stream parser with arbitrary byte sequences.
func FuzzStreamParser(f *testing.F) {
	// Seed with valid and invalid framing structures, base64, hex, and whitespace variations.
	f.Add([]byte{})
	f.Add([]byte{0x80, 0x00, 0x00, 0x04, 0x01, 0x02, 0x03, 0x04})
	f.Add([]byte{0x00, 0x00, 0x00, 0x04, 0x01, 0x02, 0x03, 0x04})
	f.Add([]byte{0x80, 0x00, 0x00, 0x00})             // empty final RFC 5531 fragment
	f.Add([]byte{0x00, 0x00, 0x00, 0x00})             // zero length prefix
	f.Add([]byte{0x80, 0x0F, 0xFF, 0xFF, 0x01, 0x02}) // truncated large RFC 5531 fragment
	f.Add([]byte{0x00, 0x0F, 0xFF, 0xFF, 0x01, 0x02}) // truncated large length-prefixed record
	f.Add([]byte("AAAAAA==\n"))
	f.Add([]byte("  \t AAAAAA== \r\n\n  AAAAAA==  \n"))
	f.Add([]byte("0x00000000\n"))
	f.Add([]byte("invalid base64!!!\n"))
	f.Add([]byte{0xFF, 0xFF, 0xFF, 0xFF})

	// Add testdata fixture seeds in RFC 5531, multi-fragment, length-prefixed, base64, and hex forms.
	if fixture, err := loadFuzzFixture("tx_success.env.txt"); err == nil {
		var rfcBuf bytes.Buffer
		_ = WriteRecordMarking(&rfcBuf, fixture, 0)
		f.Add(rfcBuf.Bytes())

		var fragBuf bytes.Buffer
		_ = WriteRecordMarking(&fragBuf, fixture, 32)
		f.Add(fragBuf.Bytes())

		var lenBuf bytes.Buffer
		_ = WriteLengthPrefixed(&lenBuf, fixture)
		f.Add(lenBuf.Bytes())

		var b64Buf bytes.Buffer
		_ = WriteBase64Line(&b64Buf, fixture)
		f.Add(b64Buf.Bytes())

		f.Add([]byte(hex.EncodeToString(fixture) + "\n"))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		_ = RunStreamParserFuzz(data)
	})
}

// FuzzHexInput fuzzes hex and base64 input normalization and decoder entry points.
func FuzzHexInput(f *testing.F) {
	f.Add("")
	f.Add("   \t\r\n  ")
	f.Add("0x")
	f.Add("0X00000000")
	f.Add("00 00 00 00")
	f.Add("deadBEEF")
	f.Add("0xdead\n\tbeef")
	f.Add("abc")      // odd length
	f.Add("0xzzzz")   // invalid hex
	f.Add("AAAAAA==") // valid base64
	f.Add("  AAAAAA==\r\n")
	f.Add("not!valid@base64#")

	if fixture, err := loadFuzzFixture("tx_success.env.txt"); err == nil {
		f.Add(hex.EncodeToString(fixture))
		f.Add("0x" + strings.ToUpper(hex.EncodeToString(fixture)))
		f.Add(base64.StdEncoding.EncodeToString(fixture))
	}

	f.Fuzz(func(t *testing.T, input string) {
		RunHexInputFuzz(input)
	})
}

// FuzzCanonicalValidation fuzzes the canonical XDR validation engine.
func FuzzCanonicalValidation(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0x00, 0x00, 0x00, 0x01, 'A', 0x00, 0x00, 0x00})
	f.Add([]byte{0x00, 0x00, 0x00, 0x01, 'A', 0x00, 0xFF, 0x00})
	f.Add([]byte{0x01, 0x02, 0x03}) // misaligned

	if fixture, err := loadFuzzFixture("tx_success.env.txt"); err == nil {
		f.Add(fixture)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		RunCanonicalFuzz(data)
	})
}

// FuzzDiagnosticLocator fuzzes the JSON diagnostic engine and path locator.
func FuzzDiagnosticLocator(f *testing.F) {
	f.Add([]byte(`{"key": "value"}`), "key")
	f.Add([]byte(`{"arr": [{"sub": 10}]}`), "arr[0].sub")
	f.Add([]byte(`{malformed json`), "path")
	f.Add([]byte(``), "")

	f.Fuzz(func(t *testing.T, data []byte, path string) {
		RunDiagnosticFuzz(data, path)
	})
}

func loadFuzzFixture(filename string) ([]byte, error) {
	paths := []string{
		filepath.Join("../../testdata", filename),
		filepath.Join("../testdata", filename),
		filepath.Join("testdata", filename),
	}
	for _, p := range paths {
		if b, err := os.ReadFile(p); err == nil {
			trimmed := strings.TrimSpace(string(b))
			return base64.StdEncoding.DecodeString(trimmed)
		}
	}
	return nil, os.ErrNotExist
}

// TestFuzzHarnessNeverPanics tests all fuzz targets with representative malformed inputs
// to verify safe degradation and high test coverage.
func TestFuzzHarnessNeverPanics(t *testing.T) {
	// 1. Stream parser inputs
	streamCorpus := [][]byte{
		{},
		{0x80, 0x00},
		{0x80, 0x00, 0x00, 0x05, 'A', 'B', 'C'}, // truncated
		{0x00, 0x00, 0x00, 0x00},
		[]byte("AAAA\nBBBB\n"),
		[]byte("not-base-64\n"),
		make([]byte, 1024),
	}
	for i, input := range streamCorpus {
		err := RunStreamParserFuzz(input)
		t.Logf("stream corpus [%d]: err=%v", i, err)
	}

	// 2. Canonical validation inputs
	canonicalCorpus := [][]byte{
		{},
		{0x01},
		{0x01, 0x02, 0x03, 0x04},
		{0x00, 0x00, 0x00, 0x01, 'A', 0x00, 0x00, 0x00},
		{0x00, 0x00, 0x00, 0x01, 'A', 0xFF, 0x00, 0x00},
		make([]byte, 256),
	}
	for _, input := range canonicalCorpus {
		RunCanonicalFuzz(input)
	}

	// 3. Diagnostic inputs
	diagCorpus := []struct {
		json []byte
		path string
	}{
		{[]byte(`{}`), ""},
		{[]byte(`{"a": 1}`), "a"},
		{[]byte(`{"a": [1, 2]}`), "a[1]"},
		{[]byte(`{"unclosed": `), "unclosed"},
		{[]byte(`invalid`), "missing"},
		{nil, "empty"},
	}
	for _, tc := range diagCorpus {
		RunDiagnosticFuzz(tc.json, tc.path)
	}

	// 4. Hex and base64 normalization inputs
	hexCorpus := []string{
		"",
		"   ",
		"0x",
		"0x00000000",
		"0XDEADBEEF",
		"abc",
		"zzzz",
		"AAAAAA==",
		"  AAAAAA== \n",
		"invalid!base64",
	}
	for _, input := range hexCorpus {
		RunHexInputFuzz(input)
	}

	// 5. Test with canonical fixture
	if fixture, err := loadFuzzFixture("tx_success.env.txt"); err == nil {
		RunCanonicalFuzz(fixture)
		RunHexInputFuzz(hex.EncodeToString(fixture))
		RunHexInputFuzz(base64.StdEncoding.EncodeToString(fixture))
		var buf bytes.Buffer
		_ = WriteRecordMarking(&buf, fixture, 256)
		_ = RunStreamParserFuzz(buf.Bytes())
	}
}
