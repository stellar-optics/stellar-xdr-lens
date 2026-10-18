package cli

import (
	"errors"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

const (
	ExitCodeOK           = 0
	ExitCodeGenericError = 1
	ExitCodeDiffFound    = 2
	ExitCodeInvalidXDR   = 3
)

// exitError carries an explicit process exit code.
type exitError struct {
	code int
	msg  string
}

func (e *exitError) Error() string { return e.msg }

// ExitCode returns the process exit code for err, and whether err should be
// printed as an error message.
func ExitCode(err error) (code int, printable bool) {
	if err == nil {
		return ExitCodeOK, false
	}

	var ee *exitError
	if errors.As(err, &ee) {
		return ee.code, false
	}

	if errors.Is(err, lens.ErrInvalidXDR) ||
		errors.Is(err, lens.ErrInvalidBase64) ||
		errors.Is(err, lens.ErrNoMatch) ||
		errors.Is(err, lens.ErrUnknownType) {
		return ExitCodeInvalidXDR, true
	}

	return ExitCodeGenericError, true
}
