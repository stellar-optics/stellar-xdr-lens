package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

// outputSink routes command output according to the global --quiet and
// --output flags. It is lazy: the destination is chosen on the first write,
// which is after Cobra has parsed the flags, and an output file is written
// through a temporary file in the destination directory and renamed into
// place only when the command succeeds.
type outputSink struct {
	base   io.Writer
	flags  *globalFlags
	writer io.Writer
	// buffer holds output while --redact-addresses is active. Addresses can
	// straddle two formatter writes, so redaction is applied to the complete
	// stream in Close rather than chunk by chunk.
	buffer *bytes.Buffer

	file      *os.File
	tempPath  string
	finalPath string
	err       error
}

func newOutputSink(base io.Writer, flags *globalFlags) *outputSink {
	return &outputSink{base: base, flags: flags}
}

// Write sends p to the selected destination. A failed destination setup is
// remembered so a command sees the error instead of silently losing output.
func (s *outputSink) Write(p []byte) (int, error) {
	if s.err != nil {
		return 0, s.err
	}
	if s.flags.redactAddresses {
		if s.buffer == nil {
			s.buffer = &bytes.Buffer{}
		}
		return s.buffer.Write(p)
	}
	if s.writer == nil {
		if err := s.open(); err != nil {
			s.err = err
			return 0, err
		}
	}
	return s.writer.Write(p)
}

// colorTarget reports the writer whose terminal status decides --color auto.
// Commands render colour before writing, so they need the real stdout rather
// than the sink (which is not an *os.File and would always look like a pipe).
func (s *outputSink) colorTarget() io.Writer {
	switch {
	case s.flags.quiet:
		return io.Discard
	case s.flags.output == "" || s.flags.output == stdinMarker:
		return s.base
	default:
		return nil
	}
}

func (s *outputSink) open() error {
	switch {
	case s.flags.quiet:
		s.writer = io.Discard
	case s.flags.output == "" || s.flags.output == stdinMarker:
		s.writer = s.base
	default:
		dir := filepath.Dir(s.flags.output)
		file, err := os.CreateTemp(dir, "."+filepath.Base(s.flags.output)+".tmp-*")
		if err != nil {
			return fmt.Errorf("creating output file for %s: %w", s.flags.output, err)
		}
		s.file = file
		s.tempPath = file.Name()
		s.finalPath = s.flags.output
		s.writer = file
	}
	return nil
}

// Close finishes the sink. On success an output file is flushed and atomically
// renamed into place; on failure it is removed so a partial report never
// replaces a previous good file.
func (s *outputSink) Close(success bool) error {
	if s.err != nil {
		s.discard()
		return s.err
	}
	if s.buffer != nil {
		data := []byte(lens.RedactAddresses(s.buffer.String()))
		s.buffer = nil
		if err := s.flushBuffered(data, success); err != nil {
			s.err = err
			s.discard()
			return err
		}
	}
	if s.file == nil {
		return nil
	}
	if !success {
		s.discard()
		return nil
	}
	if err := s.file.Sync(); err != nil {
		s.discard()
		return fmt.Errorf("writing %s: %w", s.finalPath, err)
	}
	if err := s.file.Close(); err != nil {
		s.file = nil
		s.discard()
		return fmt.Errorf("writing %s: %w", s.finalPath, err)
	}
	s.file = nil
	if err := os.Chmod(s.tempPath, 0o644); err != nil {
		s.discard()
		return fmt.Errorf("setting permissions on %s: %w", s.finalPath, err)
	}
	if err := os.Rename(s.tempPath, s.finalPath); err != nil {
		s.discard()
		return fmt.Errorf("replacing %s: %w", s.finalPath, err)
	}
	s.tempPath = ""
	return nil
}

// flushBuffered writes redacted output collected by Write. A successful
// command writes its configured destination; a failed one still writes to
// stdout (the unbuffered path has already emitted partial output there) but
// discards an output file so a partial report never replaces a good one.
func (s *outputSink) flushBuffered(data []byte, success bool) error {
	if s.flags.quiet {
		return nil
	}
	isFile := s.flags.output != "" && s.flags.output != stdinMarker
	if isFile && !success {
		return nil
	}
	if s.writer == nil {
		if err := s.open(); err != nil {
			return err
		}
	}
	_, err := s.writer.Write(data)
	return err
}

func (s *outputSink) discard() {
	if s.file != nil {
		_ = s.file.Close()
		s.file = nil
	}
	if s.tempPath != "" {
		_ = os.Remove(s.tempPath)
		s.tempPath = ""
	}
}
