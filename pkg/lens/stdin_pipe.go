package lens

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// ReadPipe reads a piped or file-backed XDR stream and returns the first
// non-empty payload after trimming whitespace. It is intentionally tolerant of
// newline- or whitespace-delimited inputs used in shell pipelines.
func ReadPipe(r io.Reader) (string, error) {
	records, err := ReadPipeRecords(r)
	if err != nil {
		return "", err
	}
	if len(records) == 0 {
		return "", ErrEmptyInput
	}
	return records[0], nil
}

// ReadPipeRecords reads all input from r and splits it into XDR payloads.
// Empty lines and whitespace are ignored; each remaining segment is normalized so
// that URL-safe base64 variants and trailing newlines are accepted.
func ReadPipeRecords(r io.Reader) ([]string, error) {
	if r == nil {
		return nil, ErrEmptyInput
	}

	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading stdin: %w", err)
	}

	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, ErrEmptyInput
	}
	if json.Valid([]byte(trimmed)) {
		return []string{trimmed}, nil
	}

	raw := strings.FieldsFunc(trimmed, func(ch rune) bool {
		switch ch {
		case '\n', '\r', '\t', ' ', '\f', '\v':
			return true
		default:
			return false
		}
	})
	if len(raw) == 0 {
		return nil, ErrEmptyInput
	}

	out := make([]string, 0, len(raw))
	for _, entry := range raw {
		normalized := normalizeInput(entry)
		if normalized == "" {
			continue
		}
		out = append(out, normalized)
	}
	if len(out) == 0 {
		return nil, ErrEmptyInput
	}
	return out, nil
}

// ReadStdinPipe reads stdin-like input and returns the first normalized XDR
// payload. The helper is intentionally a thin wrapper around ReadPipe so CLI and
// library code share one deterministic implementation of shell-pipe input.
func ReadStdinPipe(r io.Reader) (string, error) {
	return ReadPipe(r)
}
