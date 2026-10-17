package lens

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/stellar/go-stellar-sdk/xdr"
)

var (
	// ErrEmptyMultiStream is returned when a stream has no XDR records.
	ErrEmptyMultiStream = errors.New("stream contains no XDR records")
	// ErrCorruptMultiStreamRecord is returned when a stream record fails to decode.
	ErrCorruptMultiStreamRecord = errors.New("corrupt record in multi-stream")
)

// DefaultMaxRecordBuffer is the default maximum line size for multi-stream payloads (16 MiB).
const DefaultMaxRecordBuffer = 16 * 1024 * 1024

// MultiStreamRecord represents a single record decoded from a multi-stream source.
type MultiStreamRecord struct {
	// Index is the 0-indexed count of records yielded from this stream.
	Index int
	// LineNumber is the 1-indexed source line number where this record appeared.
	LineNumber int
	// RawLine is the trimmed line string from the stream.
	RawLine string
	// Value is the decoded XDR Value, if decoding succeeded.
	Value *Value
	// Err is the decoding error, if decoding failed and StopOnError is false.
	Err error
}

// MultiStreamDecoder decodes concatenated newline-separated XDR payloads from an io.Reader.
type MultiStreamDecoder struct {
	scanner      *bufio.Scanner
	typeName     string
	allowHex     bool
	stopOnError  bool
	commentChars []string
	lineNum      int
	recordCount  int
}

// MultiStreamOption configures a MultiStreamDecoder.
type MultiStreamOption func(*MultiStreamDecoder)

// WithMultiStreamType enforces decoding as a specific registered XDR type name.
func WithMultiStreamType(typeName string) MultiStreamOption {
	return func(d *MultiStreamDecoder) {
		d.typeName = typeName
	}
}

// WithMultiStreamHex enables automatic hexadecimal decoding for lines in the stream.
func WithMultiStreamHex(allowHex bool) MultiStreamOption {
	return func(d *MultiStreamDecoder) {
		d.allowHex = allowHex
	}
}

// WithMultiStreamStopOnError dictates whether the decoder stops immediately on the
// first malformed record or yields a record with Err set.
func WithMultiStreamStopOnError(stop bool) MultiStreamOption {
	return func(d *MultiStreamDecoder) {
		d.stopOnError = stop
	}
}

// WithMultiStreamComments adds comment prefixes that will be ignored during stream scanning.
func WithMultiStreamComments(prefixes ...string) MultiStreamOption {
	return func(d *MultiStreamDecoder) {
		d.commentChars = append(d.commentChars, prefixes...)
	}
}

// WithMultiStreamMaxBuffer sets the maximum scan buffer capacity for oversized lines.
func WithMultiStreamMaxBuffer(maxBytes int) MultiStreamOption {
	return func(d *MultiStreamDecoder) {
		buf := make([]byte, 64*1024)
		d.scanner.Buffer(buf, maxBytes)
	}
}

// NewMultiStreamDecoder creates a new decoder reading from r.
func NewMultiStreamDecoder(r io.Reader, opts ...MultiStreamOption) *MultiStreamDecoder {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, DefaultMaxRecordBuffer)

	d := &MultiStreamDecoder{
		scanner:      scanner,
		allowHex:     true,
		stopOnError:  true,
		commentChars: []string{"#", "//"},
	}

	for _, opt := range opts {
		opt(d)
	}

	return d
}

// Next reads and decodes the next valid record from the stream.
// It returns io.EOF when the end of the stream is reached.
func (d *MultiStreamDecoder) Next() (*MultiStreamRecord, error) {
	for d.scanner.Scan() {
		d.lineNum++
		line := strings.TrimSpace(d.scanner.Text())
		if line == "" {
			continue
		}

		if d.isComment(line) {
			continue
		}

		rec := &MultiStreamRecord{
			Index:      d.recordCount,
			LineNumber: d.lineNum,
			RawLine:    line,
		}
		d.recordCount++

		val, err := d.decodeLine(line)
		if err != nil {
			if d.stopOnError {
				return nil, fmt.Errorf("%w at line %d: %w", ErrCorruptMultiStreamRecord, d.lineNum, err)
			}
			rec.Err = err
			return rec, nil
		}

		rec.Value = val
		return rec, nil
	}

	if err := d.scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading multi-stream: %w", err)
	}

	return nil, io.EOF
}

// isComment checks if a line starts with any registered comment prefix.
func (d *MultiStreamDecoder) isComment(line string) bool {
	for _, p := range d.commentChars {
		if strings.HasPrefix(line, p) {
			return true
		}
	}
	return false
}

// decodeLine decodes an individual line payload as base64 or hexadecimal XDR.
func (d *MultiStreamDecoder) decodeLine(line string) (*Value, error) {
	isHex := strings.HasPrefix(line, "0x") || strings.HasPrefix(line, "0X") || (d.allowHex && IsHexString(line))

	if isHex {
		if d.typeName != "" {
			return DecodeHexAs(line, d.typeName)
		}
		return DecodeHex(line)
	}

	if d.typeName != "" {
		v, err := DecodeAs(line, d.typeName)
		if err == nil {
			return v, nil
		}
		if d.allowHex && IsHexString(line) {
			if hv, herr := DecodeHexAs(line, d.typeName); herr == nil {
				return hv, nil
			}
		}
		return nil, err
	}

	v, err := Decode(line)
	if err == nil {
		return v, nil
	}
	if d.allowHex && IsHexString(line) {
		if hv, herr := DecodeHex(line); herr == nil {
			return hv, nil
		}
	}
	return nil, err
}

// DecodeAll reads and decodes every record in the stream until EOF.
func (d *MultiStreamDecoder) DecodeAll() ([]*MultiStreamRecord, error) {
	var records []*MultiStreamRecord
	for {
		rec, err := d.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		records = append(records, rec)
	}

	if len(records) == 0 {
		return nil, ErrEmptyMultiStream
	}

	return records, nil
}

// DecodeMultiStream decodes all records from an io.Reader and returns a slice of Values.
func DecodeMultiStream(r io.Reader, opts ...MultiStreamOption) ([]*Value, error) {
	d := NewMultiStreamDecoder(r, opts...)
	records, err := d.DecodeAll()
	if err != nil {
		return nil, err
	}
	values := make([]*Value, 0, len(records))
	for _, rec := range records {
		if rec.Err != nil {
			return nil, rec.Err
		}
		values = append(values, rec.Value)
	}
	return values, nil
}

// DecodeMultiStreamString decodes all records from a string of concatenated lines.
func DecodeMultiStreamString(s string, opts ...MultiStreamOption) ([]*Value, error) {
	return DecodeMultiStream(strings.NewReader(s), opts...)
}

// WriteMultiStream writes a series of decoded XDR values as newline-delimited base64 lines.
func WriteMultiStream(w io.Writer, values []*Value) error {
	for _, v := range values {
		if v == nil || v.Raw == nil {
			return errors.New("cannot write nil value to multi-stream")
		}
		b64, err := SafeMarshalBase64(v.Raw)
		if err != nil {
			return fmt.Errorf("marshalling value: %w", err)
		}
		if _, err := fmt.Fprintf(w, "%s\n", b64); err != nil {
			return fmt.Errorf("writing multi-stream: %w", err)
		}
	}
	return nil
}

// WriteMultiStreamHex writes a series of decoded XDR values as newline-delimited hex lines.
func WriteMultiStreamHex(w io.Writer, values []*Value) error {
	for _, v := range values {
		if v == nil || v.Raw == nil {
			return errors.New("cannot write nil value to multi-stream")
		}
		b64, err := SafeMarshalBase64(v.Raw)
		if err != nil {
			return fmt.Errorf("marshalling value: %w", err)
		}
		hexStr, err := Base64ToHex(b64)
		if err != nil {
			return fmt.Errorf("converting to hex: %w", err)
		}
		if _, err := fmt.Fprintf(w, "%s\n", hexStr); err != nil {
			return fmt.Errorf("writing multi-stream: %w", err)
		}
	}
	return nil
}

// SafeMarshalBase64 wraps xdr.MarshalBase64 with panic recovery.
func SafeMarshalBase64(v any) (s string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("encoder panicked: %v", r)
		}
	}()
	return xdr.MarshalBase64(v)
}
