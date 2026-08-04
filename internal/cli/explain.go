package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Odusanya03/stellar-xdr-lens/pkg/lens"
	"github.com/Odusanya03/stellar-xdr-lens/pkg/lens/format"
)

func newExplainCmd(g *globalFlags, stdout io.Writer, stdin io.Reader) *cobra.Command {
	var (
		resultArg  string
		resultFile string
		asJSON     bool
		verbose    bool
		failOnErr  bool
	)

	cmd := &cobra.Command{
		Use:   "explain [base64]",
		Short: "Explain a transaction in plain English",
		Long: `Explain what a transaction does, and why it failed.

Given an envelope, explain describes the source account, preconditions and
each operation. Given a result, it explains the outcome. Given both — via
--result — it attributes each result code to the operation that produced it,
which is what actually answers "why did this fail".`,
		Example: `  # Envelope alone: what would this transaction do?
  lens explain "$ENVELOPE"

  # Result alone: what happened?
  lens explain "$RESULT"

  # Both: which operation failed, and why
  lens explain "$ENVELOPE" --result "$RESULT"

  # In CI: exit non-zero when the transaction failed
  lens explain "$ENVELOPE" --result "$RESULT" --fail-on-error`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload, err := resolveInput(firstArg(args), g.file, stdin)
			if err != nil {
				return err
			}

			primary, err := lens.Decode(payload)
			if err != nil {
				return err
			}

			summary, err := buildSummary(primary, resultArg, resultFile)
			if err != nil {
				return err
			}

			if asJSON {
				enc := json.NewEncoder(stdout)
				enc.SetIndent("", "  ")
				if err := enc.Encode(summary); err != nil {
					return fmt.Errorf("encoding summary: %w", err)
				}
			} else {
				f := format.NewSummaryFormatter(
					format.WithSummaryPalette(format.PaletteFor(stdout, g.color)),
					format.WithVerbose(verbose),
				)
				if err := f.Format(stdout, summary); err != nil {
					return err
				}
			}

			// Reporting failure through the exit code is what makes this
			// usable as a CI gate.
			if failOnErr && summary.Outcome != nil && !summary.Outcome.Success {
				return errTransactionFailed
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&resultArg, "result", "",
		"the base64 TransactionResult that accompanies the envelope")
	cmd.Flags().StringVar(&resultFile, "result-file", "",
		"read the TransactionResult from a file")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit the summary as JSON")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false,
		"include the underlying XDR result-code constants")
	cmd.Flags().BoolVar(&failOnErr, "fail-on-error", false,
		"exit with a non-zero status when the transaction failed")

	return cmd
}

// errTransactionFailed signals --fail-on-error. It is handled in main so that
// it maps to a distinct exit code rather than being printed as an error.
var errTransactionFailed = &exitError{code: 2, msg: "transaction failed"}

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
		return 0, false
	}
	var ee *exitError
	if errors.As(err, &ee) {
		return ee.code, false
	}
	return 1, true
}

// buildSummary decides whether to explain one value or a pair, based on what
// the user supplied and what the primary payload turned out to be.
func buildSummary(primary *lens.Value, resultArg, resultFile string) (*lens.Summary, error) {
	resultPayload, err := readOptional(resultArg, resultFile)
	if err != nil {
		return nil, err
	}
	if resultPayload == "" {
		summary, err := lens.Explain(primary)
		if err != nil {
			return nil, err
		}
		return summary, nil
	}

	result, err := lens.Decode(resultPayload)
	if err != nil {
		return nil, fmt.Errorf("decoding --result: %w", err)
	}
	summary, err := lens.ExplainPair(primary, result)
	if err != nil {
		return nil, err
	}
	return summary, nil
}

// readOptional resolves an optional payload from an inline value or a file.
func readOptional(inline, file string) (string, error) {
	if file != "" {
		b, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("reading %s: %w", file, err)
		}
		return strings.TrimSpace(string(b)), nil
	}
	return strings.TrimSpace(inline), nil
}
