package lens

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMultiStreamDecoderCanonicalFixtures(t *testing.T) {
	testdataDir := filepath.Join("..", "..", "testdata")

	fixtures := []string{
		"tx_success.env.txt",
		"tx_failed.env.txt",
		"tx_feebump.env.txt",
		"tx_soroban.env.txt",
	}

	var sb strings.Builder
	for i, f := range fixtures {
		content, err := os.ReadFile(filepath.Join(testdataDir, f))
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}
		trimmed := strings.TrimSpace(string(content))
		if i == 1 {
			// Write as hex to test mixed formats
			hexStr, err := Base64ToHex(trimmed)
			if err != nil {
				t.Fatalf("converting to hex: %v", err)
			}
			sb.WriteString("# Comment before hex record\n")
			sb.WriteString("0x" + hexStr + "\r\n\n")
		} else {
			sb.WriteString("// Comment before base64 record\n")
			sb.WriteString(trimmed + "\n\n")
		}
	}

	streamContent := sb.String()

	// Decode all using NewMultiStreamDecoder
	decoder := NewMultiStreamDecoder(strings.NewReader(streamContent),
		WithMultiStreamStopOnError(true),
		WithMultiStreamComments("#", "//"),
		WithMultiStreamHex(true),
		WithMultiStreamMaxBuffer(1024*1024),
	)

	records, err := decoder.DecodeAll()
	if err != nil {
		t.Fatalf("DecodeAll failed: %v", err)
	}

	if len(records) != len(fixtures) {
		t.Fatalf("expected %d records, got %d", len(fixtures), len(records))
	}

	for i, rec := range records {
		if rec.Index != i {
			t.Errorf("record %d index = %d, want %d", i, rec.Index, i)
		}
		if rec.Value == nil {
			t.Errorf("record %d has nil Value", i)
		}
		if rec.Value.Type != "TransactionEnvelope" {
			t.Errorf("record %d type = %s, want TransactionEnvelope", i, rec.Value.Type)
		}
	}

	// Test DecodeMultiStream convenience function
	values, err := DecodeMultiStreamString(streamContent, WithMultiStreamType("TransactionEnvelope"))
	if err != nil {
		t.Fatalf("DecodeMultiStreamString failed: %v", err)
	}
	if len(values) != len(fixtures) {
		t.Fatalf("expected %d values, got %d", len(fixtures), len(values))
	}

	// Test WriteMultiStream
	var outB64 bytes.Buffer
	if err := WriteMultiStream(&outB64, values); err != nil {
		t.Fatalf("WriteMultiStream failed: %v", err)
	}
	reDecoded, err := DecodeMultiStream(&outB64)
	if err != nil {
		t.Fatalf("DecodeMultiStream from WriteMultiStream output failed: %v", err)
	}
	if len(reDecoded) != len(values) {
		t.Fatalf("expected %d re-decoded values, got %d", len(values), len(reDecoded))
	}

	// Test WriteMultiStreamHex
	var outHex bytes.Buffer
	if err := WriteMultiStreamHex(&outHex, values); err != nil {
		t.Fatalf("WriteMultiStreamHex failed: %v", err)
	}
	reDecodedHex, err := DecodeMultiStream(&outHex)
	if err != nil {
		t.Fatalf("DecodeMultiStream from WriteMultiStreamHex output failed: %v", err)
	}
	if len(reDecodedHex) != len(values) {
		t.Fatalf("expected %d re-decoded hex values, got %d", len(values), len(reDecodedHex))
	}
}

func TestMultiStreamErrorsAndEdgeCases(t *testing.T) {
	// Empty stream
	emptyDecoder := NewMultiStreamDecoder(strings.NewReader("   \n\n  # only comments\n// another comment\n"))
	_, err := emptyDecoder.DecodeAll()
	if !errors.Is(err, ErrEmptyMultiStream) {
		t.Errorf("expected ErrEmptyMultiStream, got %v", err)
	}

	// Malformed stream with stopOnError=true
	malformedStream := "invalid_base64_or_hex_payload\n"
	strictDecoder := NewMultiStreamDecoder(strings.NewReader(malformedStream), WithMultiStreamStopOnError(true))
	_, err = strictDecoder.DecodeAll()
	if !errors.Is(err, ErrCorruptMultiStreamRecord) {
		t.Errorf("expected ErrCorruptMultiStreamRecord, got %v", err)
	}

	// Malformed stream with stopOnError=false
	lenientDecoder := NewMultiStreamDecoder(strings.NewReader(malformedStream), WithMultiStreamStopOnError(false))
	records, err := lenientDecoder.DecodeAll()
	if err != nil {
		t.Fatalf("unexpected error on lenient decode: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Err == nil {
		t.Errorf("expected record[0].Err to be non-nil")
	}

	// DecodeMultiStream when record has error
	_, err = DecodeMultiStream(strings.NewReader(malformedStream), WithMultiStreamStopOnError(false))
	if err == nil {
		t.Errorf("DecodeMultiStream expected error from corrupt record, got nil")
	}

	// WriteMultiStream with nil value
	if err := WriteMultiStream(&bytes.Buffer{}, []*Value{nil}); err == nil {
		t.Errorf("WriteMultiStream(nil) expected error, got nil")
	}
	if err := WriteMultiStreamHex(&bytes.Buffer{}, []*Value{nil}); err == nil {
		t.Errorf("WriteMultiStreamHex(nil) expected error, got nil")
	}

	// SafeMarshalBase64 panic recovery
	// xdr.MarshalBase64 will try to marshal or panic on unsupported unexported pointer/channels or mock
	_, _ = SafeMarshalBase64(make(chan int))

	// Decode line with explicit type name and hex input without 0x
	rawBytes := []byte{0, 0, 0, 0}
	hexVal := BytesToHex(rawBytes)
	dec := NewMultiStreamDecoder(strings.NewReader(hexVal+"\n"), WithMultiStreamType("Memo"), WithMultiStreamHex(true))
	rec, err := dec.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Value.Type != "Memo" {
		t.Errorf("expected Memo, got %s", rec.Value.Type)
	}
}
