package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
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

// DirectoryOutputSink writes each decoded element to its own file inside a
// target directory, named <index>_<type>.<format>. The directory is created
// automatically if it does not yet exist.
type DirectoryOutputSink struct {
	dir     string
	format  string
	created bool
}

// NewDirectoryOutputSink constructs a DirectoryOutputSink targeting dir with
// the given format file extension (defaulting to "txt" when empty).
func NewDirectoryOutputSink(dir, format string) *DirectoryOutputSink {
	ext := sanitizeFileToken(format)
	if ext == "" {
		ext = "txt"
	}
	return &DirectoryOutputSink{
		dir:    dir,
		format: ext,
	}
}

// EnsureDir creates the target directory and any missing parents if they do
// not already exist.
func (d *DirectoryOutputSink) EnsureDir() error {
	if d.created {
		return nil
	}
	if err := os.MkdirAll(d.dir, 0o755); err != nil {
		return fmt.Errorf("creating output directory %s: %w", d.dir, err)
	}
	d.created = true
	return nil
}

// WriteElement atomically writes content for the element at index with the
// given XDR type name to <dir>/<index>_<type>.<format>, returning the path.
func (d *DirectoryOutputSink) WriteElement(index int, typeName string, content []byte) (string, error) {
	if err := d.EnsureDir(); err != nil {
		return "", err
	}
	safeType := sanitizeFileToken(typeName)
	if safeType == "" {
		safeType = "unknown"
	}
	filename := fmt.Sprintf("%d_%s.%s", index, safeType, d.format)
	finalPath := filepath.Join(d.dir, filename)

	tmp, err := os.CreateTemp(d.dir, "."+filename+".tmp-*")
	if err != nil {
		return "", fmt.Errorf("creating output file %s: %w", finalPath, err)
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}

	if _, err := tmp.Write(content); err != nil {
		cleanup()
		return "", fmt.Errorf("writing %s: %w", finalPath, err)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return "", fmt.Errorf("writing %s: %w", finalPath, err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("writing %s: %w", finalPath, err)
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("setting permissions on %s: %w", finalPath, err)
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("replacing %s: %w", finalPath, err)
	}
	return finalPath, nil
}

func sanitizeFileToken(s string) string {
	if s == "" {
		return ""
	}
	buf := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		isAlphaNum := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
		if isAlphaNum || c == '-' || c == '_' {
			buf = append(buf, c)
		} else {
			buf = append(buf, '_')
		}
	}
	return string(buf)
}
