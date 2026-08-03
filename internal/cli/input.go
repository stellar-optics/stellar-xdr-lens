package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// errNoInput is returned when no payload could be resolved from any source.
var errNoInput = errors.New("no XDR input given: pass a base64 value, use --file, or pipe to stdin")

// stdinMarker is the conventional argument meaning "read from stdin".
const stdinMarker = "-"

// resolveInput returns an XDR payload from, in order of precedence:
//
//  1. the --file flag, when set;
//  2. the positional argument, unless it is "-";
//  3. standard input, when it is not a terminal or the argument was "-".
//
// Reading from stdin lets the tool sit in a pipeline, which is the point of
// having a CLI rather than a web form:
//
//	curl ... | jq -r .envelope_xdr | lens explain
func resolveInput(arg, file string, stdin io.Reader) (string, error) {
	if file != "" {
		b, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("reading %s: %w", file, err)
		}
		payload := strings.TrimSpace(string(b))
		if payload == "" {
			return "", fmt.Errorf("%s is empty", file)
		}
		return payload, nil
	}

	if arg != "" && arg != stdinMarker {
		return strings.TrimSpace(arg), nil
	}

	if arg == stdinMarker || !isTerminal(os.Stdin) {
		b, err := io.ReadAll(stdin)
		if err != nil {
			return "", fmt.Errorf("reading stdin: %w", err)
		}
		payload := strings.TrimSpace(string(b))
		if payload == "" {
			return "", errNoInput
		}
		return payload, nil
	}

	return "", errNoInput
}

// isTerminal reports whether f is attached to a character device, which is
// how we tell an interactive shell from a pipe without pulling in a
// dependency for it.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
