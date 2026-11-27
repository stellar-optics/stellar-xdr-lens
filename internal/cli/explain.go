package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens/format"
)

func newExplainCmd(g *globalFlags, stdout io.Writer, stdin io.Reader) *cobra.Command {
	var (
		resultArg   string
		resultFile  string
		metaArg     string
		metaFile    string
		asJSON      bool
		asMarkdown  bool
		verbose     bool
		failOnErr   bool
		isolate     bool
		resources   bool
		checkExpiry bool
		refTimeStr  string
		formatName  string
		network     string
	)

	cmd := &cobra.Command{
		Use:   "explain [base64]",
		Short: "Explain a transaction in plain English",
		Long: `Explain what a transaction does, and why it failed.

Given an envelope, explain describes the source account, preconditions and
each operation. Given a result, it explains the outcome. Given both — via
--result — it attributes each result code to the operation that produced it,
which is what actually answers "why did this fail".

Use --isolate to pinpoint the exact failing operation and distinguish
pre-operation check failures from operation execution failures.
Use --meta / --meta-file to correlate trapped Soroban host invocations with
diagnostic events and display contract panic messages.
Use --resources to add a Soroban resource and fee breakdown, including
protocol-limit comparison and an explicit EXCEEDED marker.

Paired results also deepen account_merge failures: the guidance names the
subentry classes that still block the merge and the operations that close
them, the ledger-sequence bound behind seqnum_too_far, and the destination's
lumen buying liabilities behind dest_full.`,
		Example: `  # Envelope alone: what would this transaction do?
  lens explain "$ENVELOPE"

  # Result alone: what happened?
  lens explain "$RESULT"

  # Both: which operation failed, and why
  lens explain "$ENVELOPE" --result "$RESULT"

  # GitHub-flavored Markdown tables for a report or pull request
  lens explain "$ENVELOPE" --result "$RESULT" --markdown

  # Pinpoint failing operation index and pre-op vs execution failure
  lens explain "$ENVELOPE" --result "$RESULT" --isolate

  # Decode Soroban contract panic message from diagnostic events
  lens explain "$ENVELOPE" --result "$RESULT" --meta "$META"

  # In CI: exit non-zero when the transaction failed
  lens explain "$ENVELOPE" --result "$RESULT" --fail-on-error

  # SARIF for GitHub Code Scanning / GitLab Security
  lens explain "$ENVELOPE" --result "$RESULT" --format sarif > lens.sarif

  # Soroban resource and fee breakdown against protocol limits
  lens explain "$ENVELOPE" --resources
  lens explain "$ENVELOPE" --meta "$META" --resources`,
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

			summary, resultVal, err := buildSummary(primary, resultArg, resultFile)
			if err != nil {
				return err
			}

			// Handle optional metadata correlation for Soroban trapped host calls
			metaPayload, err := readOptional(metaArg, metaFile)
			if err != nil {
				return err
			}
			var metaVal *lens.Value
			if metaPayload != "" {
				metaVal, err = lens.Decode(metaPayload)
				if err != nil {
					return fmt.Errorf("decoding --meta: %w", err)
				}
			}

			var breakdown *lens.ResourceBreakdown
			if resources {
				breakdown, err = lens.ExplainResourcesWithMeta(primary, metaVal, lens.DefaultResourceLimits())
				if err != nil {
					return err
				}
			}

			if checkExpiry {
				var refTime time.Time
				if refTimeStr != "" {
					if t, err := time.Parse(time.RFC3339, refTimeStr); err == nil {
						refTime = t
					} else if t, err := time.Parse("2006-01-02T15:04:05", refTimeStr); err == nil {
						refTime = t
					} else if t, err := time.Parse("2006-01-02", refTimeStr); err == nil {
						refTime = t
					} else if sec, err := strconv.ParseInt(refTimeStr, 10, 64); err == nil {
						refTime = time.Unix(sec, 0)
					} else {
						return fmt.Errorf("parsing --reference-time: invalid timestamp %q", refTimeStr)
					}
				} else {
					refTime = time.Now().UTC()
				}

				var envVal *lens.Value
				if primary.Type == "TransactionEnvelope" {
					envVal = primary
				}
				if envVal != nil {
					if expiry, err := lens.CheckPreconditionExpiry(envVal, refTime); err == nil && expiry != nil {
						summary.Expiry = expiry
					}
				}
			}

			if formatName != "" && formatName != "sarif" {
				return fmt.Errorf("%w: unsupported --format %q; explain supports \"sarif\"", lens.ErrInvalidArgument, formatName)
			}

			if formatName == "sarif" {
				if err := format.NewSARIFFormatter().FormatSummary(stdout, summary); err != nil {
					return err
				}
				if failOnErr && summary.Outcome != nil && !summary.Outcome.Success {
					return errTransactionFailed
				}
				return nil
			}

			if asMarkdown {
				if err := format.NewMarkdownFormatter().FormatSummary(stdout, summary); err != nil {
					return err
				}
				if breakdown != nil {
					if _, err := fmt.Fprintf(stdout, "\n## Soroban Resources\n\n```\n%s```\n", breakdown.String()); err != nil {
						return fmt.Errorf("writing resource breakdown: %w", err)
					}
				}
				if checkExpiry && summary.Expiry != nil {
					fmt.Fprintf(stdout, "\n## Preconditions Expiry\n\n")
					if summary.Expiry.Warning != "" {
						fmt.Fprintf(stdout, "> **WARNING:** %s\n\n", summary.Expiry.Warning)
					} else {
						fmt.Fprintf(stdout, "> **Preconditions Valid** as of %s\n\n", summary.Expiry.ReferenceTime.Format(time.RFC3339))
					}
					for _, d := range summary.Expiry.Details {
						fmt.Fprintf(stdout, "- %s\n", d)
					}
				}
				return nil
			}

			if asJSON {
				doc := any(summary)
				if breakdown != nil {
					doc = struct {
						*lens.Summary
						Resources *lens.ResourceBreakdown `json:"resources,omitempty"`
					}{Summary: summary, Resources: breakdown}
				}
				enc := json.NewEncoder(stdout)
				enc.SetIndent("", "  ")
				if err := enc.Encode(doc); err != nil {
					return fmt.Errorf("encoding summary: %w", err)
				}
			} else {
				palette, err := paletteForOutput(stdout, g.color)
				if err != nil {
					return err
				}
				f := format.NewSummaryFormatter(
					format.WithSummaryPalette(palette),
					format.WithVerbose(verbose),
					format.WithSummaryHyperlinks(explorerBaseURL(network)),
				)
				if err := f.Format(stdout, summary); err != nil {
					return err
				}

				if breakdown != nil {
					if _, err := fmt.Fprintf(stdout, "\n%s", breakdown.String()); err != nil {
						return fmt.Errorf("writing resource breakdown: %w", err)
					}
				}

				if checkExpiry && summary.Expiry != nil {
					fmt.Fprintln(stdout)
					if summary.Expiry.Warning != "" {
						fmt.Fprintf(stdout, "WARNING: %s\n", summary.Expiry.Warning)
						for _, d := range summary.Expiry.Details {
							fmt.Fprintf(stdout, "  • %s\n", d)
						}
					} else {
						fmt.Fprintf(stdout, "Preconditions verified: valid as of %s\n", summary.Expiry.ReferenceTime.Format(time.RFC3339))
						for _, d := range summary.Expiry.Details {
							fmt.Fprintf(stdout, "  • %s\n", d)
						}
					}
				}

				// If failure isolation requested, print the report
				if isolate && summary.Outcome != nil {
					var resVal *lens.Value
					var envVal *lens.Value
					if primary.Type == "TransactionResult" {
						resVal = primary
					} else {
						envVal = primary
						resVal = resultVal
					}
					if resVal != nil {
						if isolated, err := lens.IsolateFailureFromValue(resVal, envVal); err == nil {
							fmt.Fprintln(stdout)
							fmt.Fprint(stdout, isolated.DiagnosticReport())
						}
					}
				}

				// If meta is supplied and transaction had trapped operations, display trap reason
				if metaVal != nil && summary.Outcome != nil {
					var resVal *lens.Value
					if primary.Type == "TransactionResult" {
						resVal = primary
					} else {
						resVal = resultVal
					}
					if resVal != nil {
						if trap, err := lens.CorrelateTrapFromValues(resVal, metaVal); err == nil && trap.Trapped {
							fmt.Fprintln(stdout)
							fmt.Fprintf(stdout, "Trapped Reason: %s\n", trap.Summary)
							if trap.PanicMessage != "" {
								fmt.Fprintf(stdout, "Panic Message:  %s\n", trap.PanicMessage)
							}
							if trap.Hint != "" {
								fmt.Fprintf(stdout, "Hint:           %s\n", trap.Hint)
							}
						}
					}
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
	cmd.Flags().StringVar(&metaArg, "meta", "",
		"the base64 TransactionMeta to correlate diagnostic events")
	cmd.Flags().StringVar(&metaFile, "meta-file", "",
		"read the TransactionMeta from a file")
	cmd.Flags().BoolVar(&isolate, "isolate", false,
		"isolate and pinpoint the exact failing operation index")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit the summary as JSON")
	cmd.Flags().BoolVar(&asMarkdown, "markdown", false,
		"emit GitHub-flavored Markdown tables for transaction details and operations")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false,
		"include the underlying XDR result-code constants")
	cmd.Flags().BoolVar(&failOnErr, "fail-on-error", false,
		"exit with a non-zero status when the transaction failed")
	cmd.Flags().BoolVar(&resources, "resources", false,
		"add a Soroban resource and fee breakdown compared with protocol limits")
	cmd.Flags().BoolVar(&checkExpiry, "check-expiry", false,
		"verify transaction precondition expiration (TimeBounds and LedgerBounds)")
	cmd.Flags().StringVar(&refTimeStr, "reference-time", "",
		"optional reference timestamp (RFC 3339 or Unix seconds) for --check-expiry")
	cmd.Flags().StringVar(&formatName, "format", "",
		`output format: "sarif" emits SARIF 2.1.0 for GitHub Code Scanning and GitLab Security`)
	cmd.Flags().StringVar(&network, "network", "public",
		`Stellar Expert network for clickable address links: "public" or "testnet" (empty disables)`)

	return cmd
}

// errTransactionFailed lives in exitcodes.go along with the strict exit-code
// policy; --fail-on-error and the diff-found slot share exit code 2.

// buildSummary decides whether to explain one value or a pair, based on what
// the user supplied and what the primary payload turned out to be.
func buildSummary(primary *lens.Value, resultArg, resultFile string) (*lens.Summary, *lens.Value, error) {
	resultPayload, err := readOptional(resultArg, resultFile)
	if err != nil {
		return nil, nil, err
	}
	if resultPayload == "" {
		summary, err := lens.Explain(primary)
		if err != nil {
			return nil, nil, err
		}
		return summary, nil, nil
	}

	result, err := lens.Decode(resultPayload)
	if err != nil {
		return nil, nil, fmt.Errorf("decoding --result: %w", err)
	}
	summary, err := lens.ExplainPair(primary, result)
	if err != nil {
		return nil, nil, err
	}
	return summary, result, nil
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
