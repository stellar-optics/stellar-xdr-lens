package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

func newResultDiffCmd(stdout io.Writer, stdin io.Reader) *cobra.Command {
	var (
		asJSON    bool
		exitDiff  bool
		quietMode bool
	)

	cmd := &cobra.Command{
		Use:   "result-diff <envelope> <result>",
		Short: "Align a transaction envelope with its result, operation by operation",
		Long: `Lay a transaction envelope and its result out side by side and report
every place they disagree.

Each argument is either base64 XDR or a path to a file containing it; files are
detected by existence, so no flag is needed to mix the two. The first value
must decode as a TransactionEnvelope and the second as a TransactionResult.

Every operation is paired with the operation result at the same index. The
command reports mismatched operation and result types, operations with no
result of their own, results with no matching operation, and each failed
operation's result code in plain English. Everything is computed locally from
the two supplied blobs.`,
		Example: `  lens result-diff "$ENVELOPE" "$RESULT"
  lens result-diff envelope.xdr result.xdr
  lens result-diff --json envelope.xdr result.xdr
  lens result-diff --exit-code envelope.xdr result.xdr   # non-zero on mismatch`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			envelopePayload, err := readOperand(args[0], stdin)
			if err != nil {
				return fmt.Errorf("reading envelope: %w", err)
			}
			resultPayload, err := readOperand(args[1], stdin)
			if err != nil {
				return fmt.Errorf("reading result: %w", err)
			}

			envelopeValue, err := lens.Decode(envelopePayload)
			if err != nil {
				return fmt.Errorf("decoding envelope: %w", err)
			}
			resultValue, err := lens.Decode(resultPayload)
			if err != nil {
				return fmt.Errorf("decoding result: %w", err)
			}
			diff, err := lens.DiffEnvelopeResultValues(envelopeValue, resultValue)
			if err != nil {
				return err
			}

			switch {
			case quietMode:
				// Print nothing.
			case asJSON:
				enc := json.NewEncoder(stdout)
				enc.SetIndent("", "  ")
				if err := enc.Encode(diff); err != nil {
					return fmt.Errorf("encoding alignment: %w", err)
				}
			default:
				if _, err := io.WriteString(stdout, diff.String()); err != nil {
					return fmt.Errorf("writing alignment: %w", err)
				}
			}

			if (exitDiff || quietMode) && len(diff.Changes) > 0 {
				return errValuesDiffer
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "emit the alignment as JSON")
	cmd.Flags().BoolVar(&exitDiff, "exit-code", false,
		"exit with a non-zero status when the envelope and result disagree")
	cmd.Flags().BoolVarP(&quietMode, "quiet", "q", false,
		"print nothing; report the answer only through the exit code")

	return cmd
}
