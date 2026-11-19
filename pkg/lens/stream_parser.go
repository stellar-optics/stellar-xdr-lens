package lens

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/stellar/go-stellar-sdk/xdr"
)

// Sentinel errors for stream parsing.
var (
	ErrRecordTooLarge = errors.New("XDR stream record exceeds maximum allowed size")
	ErrPrematureEOF   = errors.New("premature end of XDR stream while reading record")
	ErrInvalidFraming = errors.New("invalid or corrupted XDR stream framing")
	ErrStreamClosed   = errors.New("stream parser is closed")
)

// Default limits for streaming parser.
const (
	DefaultMaxRecordSize  = 64 * 1024 * 1024 // 64 MB maximum record size
	DefaultBufferSize     = 64 * 1024        // 64 KB read buffer
	RFC5531LastFragMask   = 0x80000000       // Highest bit indicates final fragment
	RFC5531LengthMask     = 0x7FFFFFFF       // Lower 31 bits encode fragment length
	maxFragmentsPerRecord = 65536            // Maximum RFC 5531 fragments per single record
)

// StreamFraming defines the framing protocol used by the stream.
type StreamFraming int

const (
	// FramingAuto automatically detects RFC 5531, length-prefixed, or base64 lines.
	FramingAuto StreamFraming = iota
	// FramingRecordMarking uses RFC 5531 / RFC 4506 4-byte fragment headers.
	FramingRecordMarking
	// FramingLengthPrefixed uses simple 4-byte big-endian uint32 length prefixes.
	FramingLengthPrefixed
	// FramingBase64Lines parses newline-delimited base64 XDR strings.
	FramingBase64Lines
)

// String returns the name of the framing mode.
func (f StreamFraming) String() string {
	switch f {
	case FramingAuto:
		return "auto"
	case FramingRecordMarking:
		return "record-marking (RFC 5531)"
	case FramingLengthPrefixed:
		return "length-prefixed"
	case FramingBase64Lines:
		return "base64-lines"
	default:
		return "unknown"
	}
}

// StreamOption configures a StreamParser.
type StreamOption func(*StreamParser)

// WithFraming configures the framing protocol.
func WithFraming(f StreamFraming) StreamOption {
	return func(p *StreamParser) {
		p.framing = f
	}
}

// WithMaxRecordSize sets the maximum allowed size in bytes for a single XDR record.
func WithMaxRecordSize(maxSize uint32) StreamOption {
	return func(p *StreamParser) {
		if maxSize > 0 {
			p.maxRecordSize = maxSize
		}
	}
}

// WithBufferSize sets the size of the internal buffered reader.
func WithBufferSize(size int) StreamOption {
	return func(p *StreamParser) {
		if size > 0 {
			p.bufSize = size
		}
	}
}

// StreamParser parses framed XDR streams item by item without buffering entire files in memory.
type StreamParser struct {
	r             io.Reader
	reader        *bufio.Reader
	framing       StreamFraming
	detected      StreamFraming
	maxRecordSize uint32
	bufSize       int
	chunkIndex    int64
	totalBytes    int64
	closed        bool
}

// NewStreamParser constructs a new streaming XDR parser.
func NewStreamParser(r io.Reader, opts ...StreamOption) *StreamParser {
	p := &StreamParser{
		r:             r,
		framing:       FramingAuto,
		maxRecordSize: DefaultMaxRecordSize,
		bufSize:       DefaultBufferSize,
	}

	for _, opt := range opts {
		opt(p)
	}

	p.reader = bufio.NewReaderSize(r, p.bufSize)
	return p
}

// Framing returns the detected or configured framing mechanism.
func (p *StreamParser) Framing() StreamFraming {
	if p.framing != FramingAuto {
		return p.framing
	}
	return p.detected
}

// ChunkIndex returns the index of the most recently read record (0-based).
func (p *StreamParser) ChunkIndex() int64 {
	return p.chunkIndex
}

// BytesRead returns the total number of bytes read from the underlying stream.
func (p *StreamParser) BytesRead() int64 {
	return p.totalBytes
}

// Next reads and returns the next complete XDR record bytes from the stream.
// Returns io.EOF when the end of the stream is reached cleanly.
func (p *StreamParser) Next() ([]byte, error) {
	if p.closed {
		return nil, ErrStreamClosed
	}

	if p.framing == FramingAuto && p.detected == FramingAuto {
		detected, err := p.detectFraming()
		if err != nil {
			return nil, err
		}
		p.detected = detected
	}

	effectiveFraming := p.framing
	if effectiveFraming == FramingAuto {
		effectiveFraming = p.detected
	}

	var record []byte
	var err error

	switch effectiveFraming {
	case FramingRecordMarking:
		record, err = p.readRecordMarking()
	case FramingLengthPrefixed:
		record, err = p.readLengthPrefixed()
	case FramingBase64Lines:
		record, err = p.readBase64Line()
	default:
		return nil, fmt.Errorf("%w: unrecognized framing type", ErrInvalidFraming)
	}

	if err != nil {
		if errors.Is(err, io.EOF) {
			p.closed = true
		}
		return nil, err
	}

	p.chunkIndex++
	return record, nil
}

// NextNode reads the next record and converts it to a neutral Node tree using type auto-detection.
func (p *StreamParser) NextNode() (*Node, error) {
	record, err := p.Next()
	if err != nil {
		return nil, err
	}

	b64 := base64.StdEncoding.EncodeToString(record)
	val, err := Decode(b64)
	if err != nil {
		return nil, fmt.Errorf("chunk [%d]: %w", p.chunkIndex-1, err)
	}

	return val.Node, nil
}

// NextInto reads the next record and unmarshals it into the destination struct.
func (p *StreamParser) NextInto(dest any) error {
	record, err := p.Next()
	if err != nil {
		return err
	}

	readBytes, err := xdr.Unmarshal(bytes.NewReader(record), dest)
	if err != nil {
		return fmt.Errorf("chunk [%d] unmarshal error: %w", p.chunkIndex-1, err)
	}
	if readBytes != len(record) {
		return fmt.Errorf("chunk [%d]: %w (consumed %d, expected %d)", p.chunkIndex-1, ErrTrailingGarbage, readBytes, len(record))
	}

	return nil
}

// Drain reads all remaining records in the stream, returning the total count of records.
func (p *StreamParser) Drain() (int64, error) {
	count := int64(0)
	for {
		_, err := p.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return count, nil
			}
			return count, err
		}
		count++
	}
}

func (p *StreamParser) detectFraming() (StreamFraming, error) {
	peekBytes, err := p.reader.Peek(4)
	if err != nil {
		if errors.Is(err, io.EOF) && len(peekBytes) == 0 {
			return FramingAuto, io.EOF
		}
		return FramingAuto, fmt.Errorf("%w: failed reading initial bytes: %w", ErrPrematureEOF, err)
	}

	// Check if printable ASCII characters indicating base64 text
	isASCII := true
	for _, b := range peekBytes {
		if (b < '0' || b > '9') && (b < 'a' || b > 'z') && (b < 'A' || b > 'Z') && b != '+' && b != '/' && b != '=' && b != '\n' && b != '\r' {
			isASCII = false
			break
		}
	}
	if isASCII {
		return FramingBase64Lines, nil
	}

	header := binary.BigEndian.Uint32(peekBytes)
	// In RFC 5531, bit 31 is the last-fragment bit. If set, header & RFC5531LastFragMask != 0.
	if (header & RFC5531LastFragMask) != 0 {
		return FramingRecordMarking, nil
	}

	return FramingLengthPrefixed, nil
}

func (p *StreamParser) readRecordMarking() ([]byte, error) {
	var record bytes.Buffer
	var totalSize uint32
	fragments := 0

	for {
		var headerBuf [4]byte
		n, err := io.ReadFull(p.reader, headerBuf[:])
		p.totalBytes += int64(n)
		if err != nil {
			if errors.Is(err, io.EOF) && record.Len() == 0 && fragments == 0 {
				return nil, io.EOF
			}
			return nil, fmt.Errorf("%w: record marking fragment header: %w", ErrPrematureEOF, err)
		}

		fragments++
		if fragments > maxFragmentsPerRecord {
			return nil, fmt.Errorf("%w: record exceeds maximum fragment count %d", ErrInvalidFraming, maxFragmentsPerRecord)
		}

		header := binary.BigEndian.Uint32(headerBuf[:])
		isLast := (header & RFC5531LastFragMask) != 0
		fragLen := header & RFC5531LengthMask

		if fragLen == 0 && isLast {
			break
		}

		if fragLen > p.maxRecordSize || totalSize > p.maxRecordSize-fragLen {
			return nil, fmt.Errorf("%w: size exceeds limit %d", ErrRecordTooLarge, p.maxRecordSize)
		}
		totalSize += fragLen

		// Stream fragment bytes incrementally so a forged header cannot force
		// an upfront multi-megabyte allocation before bytes actually arrive.
		written, err := io.CopyN(&record, p.reader, int64(fragLen))
		p.totalBytes += written
		if err != nil {
			return nil, fmt.Errorf("%w: record marking fragment data: %w", ErrPrematureEOF, err)
		}

		if isLast {
			break
		}
	}

	return record.Bytes(), nil
}

func (p *StreamParser) readLengthPrefixed() ([]byte, error) {
	var headerBuf [4]byte
	n, err := io.ReadFull(p.reader, headerBuf[:])
	p.totalBytes += int64(n)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, io.EOF
		}
		return nil, fmt.Errorf("%w: length prefix: %w", ErrPrematureEOF, err)
	}

	length := binary.BigEndian.Uint32(headerBuf[:])
	if length > p.maxRecordSize {
		return nil, fmt.Errorf("%w: length %d exceeds limit %d", ErrRecordTooLarge, length, p.maxRecordSize)
	}

	if length == 0 {
		return []byte{}, nil
	}

	// Copy incrementally instead of pre-allocating make([]byte, length) so a
	// short truncated payload with a large declared length cannot cause OOM.
	var data bytes.Buffer
	written, err := io.CopyN(&data, p.reader, int64(length))
	p.totalBytes += written
	if err != nil {
		return nil, fmt.Errorf("%w: record data: %w", ErrPrematureEOF, err)
	}

	return data.Bytes(), nil
}

func (p *StreamParser) readBase64Line() ([]byte, error) {
	// Base64 expands 3 bytes to 4 characters; allow modest overhead for padding/whitespace.
	maxLineBytes := (int64(p.maxRecordSize)/3+1)*4 + 1024

	for {
		line, err := p.readBoundedLine(maxLineBytes)
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		trimmed := strings.TrimSpace(line)

		if trimmed != "" {
			data, decErr := base64.StdEncoding.DecodeString(trimmed)
			if decErr != nil {
				return nil, fmt.Errorf("%w: malformed base64 line: %w", ErrInvalidFraming, decErr)
			}
			if uint32(len(data)) > p.maxRecordSize {
				return nil, fmt.Errorf("%w: size %d exceeds limit %d", ErrRecordTooLarge, len(data), p.maxRecordSize)
			}
			return data, nil
		}

		if errors.Is(err, io.EOF) {
			return nil, io.EOF
		}
	}
}

func (p *StreamParser) readBoundedLine(maxBytes int64) (string, error) {
	var buf bytes.Buffer
	for {
		frag, err := p.reader.ReadSlice('\n')
		p.totalBytes += int64(len(frag))
		if int64(buf.Len())+int64(len(frag)) > maxBytes {
			return "", fmt.Errorf("%w: base64 line exceeds maximum length %d", ErrRecordTooLarge, maxBytes)
		}
		buf.Write(frag)
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return buf.String(), io.EOF
			}
			return "", fmt.Errorf("reading base64 stream: %w", err)
		}
		return buf.String(), nil
	}
}

// WriteRecordMarking writes a record into w using RFC 5531 / RFC 4506 Record Marking framing.
// If maxFragmentSize is greater than 0 and smaller than len(record), the record is split across multiple fragments.
func WriteRecordMarking(w io.Writer, record []byte, maxFragmentSize uint32) error {
	if len(record) == 0 {
		var header [4]byte
		binary.BigEndian.PutUint32(header[:], RFC5531LastFragMask)
		_, err := w.Write(header[:])
		return err
	}

	if maxFragmentSize == 0 || maxFragmentSize >= uint32(len(record)) {
		var header [4]byte
		headerVal := RFC5531LastFragMask | uint32(len(record))
		binary.BigEndian.PutUint32(header[:], headerVal)
		if _, err := w.Write(header[:]); err != nil {
			return err
		}
		_, err := w.Write(record)
		return err
	}

	// Split into fragments
	offset := uint32(0)
	total := uint32(len(record))

	for offset < total {
		remaining := total - offset
		chunkSize := maxFragmentSize
		isLast := false
		if remaining <= maxFragmentSize {
			chunkSize = remaining
			isLast = true
		}

		headerVal := chunkSize
		if isLast {
			headerVal |= RFC5531LastFragMask
		}

		var header [4]byte
		binary.BigEndian.PutUint32(header[:], headerVal)
		if _, err := w.Write(header[:]); err != nil {
			return err
		}
		if _, err := w.Write(record[offset : offset+chunkSize]); err != nil {
			return err
		}

		offset += chunkSize
	}

	return nil
}

// WriteLengthPrefixed writes a record into w using 4-byte big-endian uint32 length prefix.
func WriteLengthPrefixed(w io.Writer, record []byte) error {
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(record)))
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	_, err := w.Write(record)
	return err
}

// WriteBase64Line writes a record into w as a base64 encoded line.
func WriteBase64Line(w io.Writer, record []byte) error {
	b64 := base64.StdEncoding.EncodeToString(record)
	_, err := fmt.Fprintln(w, b64)
	return err
}
