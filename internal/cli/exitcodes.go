package cli

import (
	"errors"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

// The CLI exit code policy is strict and pinned:
//
//	0  clean — the command did what it was asked and the answer is "no".
//	1  generic error — bad flag combinations, missing files, unreadable
//	   environments, malformed arguments; culprits other than the XDR itself.
//	2  diff found — the command completed but the condition being checked
//	   came out false: a structural `diff --exit-code`/`--quiet` difference,
//	   a failed transaction reported by `explain --fail-on-error`, or an
//	   envelope/result pair that disagrees. It is "completed successfully;
//	   what you asked me to check is false", not an error.
//	3  invalid XDR — the payload itself could not be decoded: bad base64,
//	   a malformed structure, no registered type matches, non-canonical
//	   encoding or a corrupted stream record.
//
// Scripts can rely on exactly four outcomes: success (0), a check that
// returned false (2), XDR they cannot read (3), and everything else (1).
const (
	// ExitSuccess is the exit code for a clean run.
	ExitSuccess = 0
	// ExitError is the exit code for a generic error that is not caused by
	// the XDR payload: usage mistakes, missing files, theme errors and the
	// like. Every error not classified below exits with this code.
	ExitError = 1
	// ExitDiffFound is the exit code for the "diff found" condition slot: a
	// structural difference (--diff --exit-code/--quiet), a failed
	// transaction (--fail-on-error) or a disagreeing envelope/result pair.
	// The run succeeded; the checked condition is false.
	ExitDiffFound = 2
	// ExitInvalidXDR is the exit code when the supplied payload cannot be
	// decoded as XDR at all. It lets callers distinguish "my pipeline broke"
	// (ExitDiffFound) from "the bytes are garbage" (ExitInvalidXDR).
	ExitInvalidXDR = 3
)

// exitError carries an explicit process exit code.
type exitError struct {
	code int
	msg  string
}

func (e *exitError) Error() string { return e.msg }

// errTransactionFailed signals --fail-on-error. It exits with the diff-found
// code so a CI gate reports "checked, and the answer is false" distinctly
// from an actual failure of the tool itself.
var errTransactionFailed = &exitError{code: ExitDiffFound, msg: "transaction failed"}

// errValuesDiffer signals --exit-code and --quiet, mirroring diff(1)'s
// convention of a distinct non-zero status when the inputs differ.
var errValuesDiffer = &exitError{code: ExitDiffFound, msg: "values differ"}

// ExitCode returns the process exit code for err, and whether err should be
// printed as an error message.
//
// Classification is stable: the printed exitError entries carry their own
// code, XDR decode failures exit with ExitInvalidXDR, and everything else is
// a generic error. Callers can match with errors.Is on the library sentinels
// mirrored here or use this function directly.
func ExitCode(err error) (code int, printable bool) {
	if err == nil {
		return ExitSuccess, false
	}
	var ee *exitError
	if errors.As(err, &ee) {
		return ee.code, false
	}
	if isInvalidXDRError(err) {
		return ExitInvalidXDR, true
	}
	return ExitError, true
}

// isInvalidXDRError reports whether err means "the payload could not be
// decoded as XDR". The sentinels are matched with errors.Is, so wrapping
// preserves them: every decode error produced by the library maps to exit
// code 3.
func isInvalidXDRError(err error) bool {
	return errors.Is(err, lens.ErrInvalidXDR) ||
		errors.Is(err, lens.ErrInvalidBase64) ||
		errors.Is(err, lens.ErrInvalidHex) ||
		errors.Is(err, lens.ErrOddLengthHex) ||
		errors.Is(err, lens.ErrEmptyHex) ||
		errors.Is(err, lens.ErrNoMatch) ||
		errors.Is(err, lens.ErrNonCanonical) ||
		errors.Is(err, lens.ErrPrematureEOF) ||
		errors.Is(err, lens.ErrInvalidFraming) ||
		errors.Is(err, lens.ErrRecordTooLarge) ||
		errors.Is(err, lens.ErrCorruptMultiStreamRecord)
}

// IsInvalidXDRError reports whether err is a payload that could not be
// decoded as XDR, re-exposed for callers that need the classification without
// computing an exit code.
func IsInvalidXDRError(err error) bool {
	return isInvalidXDRError(err)
}
