package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

func newLintCmd(g *globalFlags, stdout io.Writer, stdin io.Reader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lint [base64]",
		Short: "Statically check a transaction envelope for common mistakes",
		Long: `Run offline static checks against a transaction envelope.

lint catches conditions that Stellar Core rejects before the transaction
reaches a ledger, and antipatterns that usually mean the transaction was
built incorrectly:

  zero-fee-bid          the fee bid is zero
  fee-below-minimum     the fee bid is under operations × 100 stroops
  fee-bump-fee-too-low  a fee bump bids less than its inner transaction
  missing-signatures    the envelope (or inner fee-bump transaction) is unsigned
  inverted-timebounds   a time or ledger bound has max before min
  unreachable-operation an operation follows an ACCOUNT_MERGE

Each finding names the field path, its severity (ERROR or WARN), the affected
line, and a remediation hint. The exit code is 0 when the envelope is clean and
2 when any finding is reported, so lint works as a CI gate. Decoding problems
still exit with 3.

  lens lint "$ENVELOPE"
  lens lint --file tx.env
  cat tx.env | lens lint -`,
		Example: `  lens lint "$ENVELOPE"
  lens lint --quiet "$ENVELOPE"   # exit code only
  lens lint --file transaction.env`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload, err := resolveInput(firstArg(args), g.file, stdin)
			if err != nil {
				return err
			}

			value, err := lens.DecodeAs(payload, "TransactionEnvelope")
			if err != nil {
				return err
			}
			env, ok := value.Raw.(*xdr.TransactionEnvelope)
			if !ok || env == nil {
				return fmt.Errorf("%w: input is not a TransactionEnvelope", lens.ErrInvalidArgument)
			}

			issues := lens.LintEnvelope(env)
			if g.quiet {
				if len(issues) > 0 {
					return errLintIssues
				}
				return nil
			}

			if len(issues) == 0 {
				_, err := fmt.Fprintln(stdout, "no lint issues found")
				return err
			}

			if _, err := fmt.Fprintf(stdout, "%d issue(s) found in %s:\n\n", len(issues), value.Type); err != nil {
				return err
			}
			for i, issue := range issues {
				if _, err := fmt.Fprintf(stdout, "%d. [line %d] %s %s", i+1, issue.Line, issue.Severity, issue.Rule); err != nil {
					return err
				}
				if issue.Path != "" {
					if _, err := fmt.Fprintf(stdout, " (%s)", issue.Path); err != nil {
						return err
					}
				}
				if _, err := fmt.Fprintf(stdout, "\n   %s\n", issue.Message); err != nil {
					return err
				}
				if issue.Hint != "" {
					if _, err := fmt.Fprintf(stdout, "   hint: %s\n", issue.Hint); err != nil {
						return err
					}
				}
				if _, err := fmt.Fprintln(stdout); err != nil {
					return err
				}
			}
			return errLintIssues
		},
	}
	return cmd
}
