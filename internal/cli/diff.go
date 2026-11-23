package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens/format"
)

func newDiffCmd(g *globalFlags, stdout io.Writer, stdin io.Reader) *cobra.Command {
	var (
		typeName         string
		formatName       string
		asJSON           bool
		jsonPatch        bool
		unified          bool
		sideBySide       bool
		exitDiff         bool
		alignLists       bool
		semantic         bool
		unorderedSigners bool
		statsOnly        bool
		summaryOnly      bool
		footprintMode    bool
		includePaths     []string
		excludePaths     []string
		maskPaths        []string
		redact           []string
		redactFields     []string
		context          int
		sideWidth        int
		diffThreshold    float64
	)

	cmd := &cobra.Command{
		Use:   "diff <a> <b>",
		Short: "Structurally compare two XDR values",
		Long: `Compare two XDR values and report every path that differs.

Each argument is either base64 XDR or a path to a file containing it; files
are detected by existence, so no flag is needed to mix the two.

Unlike a byte comparison, this names the fields that changed — so a one-line
answer to "why does my rebuilt transaction not match" is the field, not a
mismatched hash.

Three modes change what is compared rather than how it is printed:

  --unordered-signers  treat signature and signer collections as unordered
                        multisets, so reordering them is not a difference;
  --footprint          compare the Soroban ledger footprints of two
                        envelopes (or SorobanTransactionData) as sets;
  --stats              print only the statistical summary of the diff.

--include-paths and --exclude-paths narrow which changed paths are reported.
A pattern is a dot-separated field path; * and ? are glob metacharacters within
a segment, ** spans segments, and a pattern also matches everything beneath it.
Excludes win over includes. For example, ignoring the noise of a rebuild:

  lens diff --exclude-paths Signatures,SeqNum before.xdr after.xdr

--mask-paths excludes matching paths from the diff evaluation entirely.
Unlike --exclude-paths which only hides output, masked paths are never
compared, so differences in masked paths do not affect the Equal() result.
This is useful for ignoring dynamic fields like sequence numbers or signatures
when checking if two transactions are semantically equivalent.

  lens diff --mask-paths Signatures,SeqNum before.xdr after.xdr

--redact normalizes the dynamic cryptographic values that a rebuild cannot
reproduce — hashes, signatures and derived identifiers — before the comparison,
so two values that differ only in those fields compare equal. It accepts the
categories hashes, signatures, identifiers, or all, and --redact-fields
additionally redacts named fields (globs allowed). Redacted paths are reported
so the normalization is never silent.

Three output formats render the difference for other tools:

  --json-patch   an RFC 6902 JSON Patch (add/remove/replace operations);
  --unified      a standard unified diff for code review comments;
  --side-by-side a colourised two-column terminal view.

These are mutually exclusive with each other and with --json/--stats/--summary.`,
		Example: `  lens diff "$XDR_A" "$XDR_B"
  lens diff before.xdr after.xdr
  lens diff --type TransactionEnvelope a.xdr b.xdr
  lens diff --align before.xdr after.xdr
  lens diff --semantic before.xdr after.xdr
  lens diff --unordered-signers a.xdr b.xdr
  lens diff --include-paths Result.Code a.xdr b.xdr
  lens diff --exclude-paths Signatures,SeqNum a.xdr b.xdr
  lens diff --mask-paths Signatures,SeqNum a.xdr b.xdr
  lens diff --redact hashes,signatures a.xdr b.xdr
  lens diff --redact all --redact-fields PrevHash a.xdr b.xdr
  lens diff --footprint before.env.xdr after.env.xdr
  lens diff --json-patch a.xdr b.xdr
  lens diff --unified a.xdr b.xdr
  lens diff --side-by-side a.xdr b.xdr
  lens diff --stats before.xdr after.xdr
  lens diff --summary before.xdr after.xdr
  lens diff --diff-threshold 5 before.xdr after.xdr
  lens diff --exit-code a.xdr b.xdr   # non-zero when they differ`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if diffThreshold < 0 {
				return fmt.Errorf("%w: --diff-threshold must be non-negative", lens.ErrInvalidArgument)
			}
			if formatName != "" {
				switch strings.ToLower(formatName) {
				case "text":
					// standard text diff
				case "json":
					asJSON = true
				case "json-patch":
					jsonPatch = true
				case "unified":
					unified = true
				case "side-by-side":
					sideBySide = true
				case "stats":
					statsOnly = true
				case "summary":
					summaryOnly = true
				default:
					return fmt.Errorf("%w: unknown format %q (allowed: text, json, json-patch, unified, side-by-side, stats, summary)",
						lens.ErrInvalidArgument, formatName)
				}
			}
			if err := validateDiffModes(asJSON, statsOnly, summaryOnly, jsonPatch, unified, sideBySide, footprintMode); err != nil {
				return err
			}
			if footprintMode && (len(includePaths) > 0 || len(excludePaths) > 0) {
				return fmt.Errorf("%w: --include-paths and --exclude-paths do not apply to --footprint",
					lens.ErrInvalidArgument)
			}
			if footprintMode && (len(redact) > 0 || len(redactFields) > 0) {
				return fmt.Errorf("%w: --redact does not apply to --footprint", lens.ErrInvalidArgument)
			}
			if footprintMode && len(maskPaths) > 0 {
				return fmt.Errorf("%w: --mask-paths does not apply to --footprint", lens.ErrInvalidArgument)
			}

			left, err := readOperand(args[0], stdin)
			if err != nil {
				return fmt.Errorf("reading first value: %w", err)
			}
			right, err := readOperand(args[1], stdin)
			if err != nil {
				return fmt.Errorf("reading second value: %w", err)
			}

			if footprintMode {
				return runFootprintDiff(stdout, left, right, asJSON, g.quiet, exitDiff)
			}

			filter, err := lens.NewPathFilter(includePaths, excludePaths)
			if err != nil {
				return err
			}

			maskFilter, err := lens.NewPathFilter(maskPaths, nil)
			if err != nil {
				return err
			}

			redaction, err := lens.RedactionPolicyFromNames(redact)
			if err != nil {
				return err
			}
			if len(redactFields) > 0 {
				for _, fields := range redactFields {
					for _, field := range strings.Split(fields, ",") {
						if field = strings.TrimSpace(field); field != "" {
							redaction.Fields = append(redaction.Fields, field)
						}
					}
				}
			}

			result, err := lens.DiffBase64WithOptions(left, right, typeName, lens.DiffOptions{
				AlignLists:       alignLists,
				Semantic:         semantic,
				UnorderedSigners: unorderedSigners,
				Filter:           filter,
				Mask:             maskFilter,
				Threshold:        diffThreshold,
				Redaction:        redaction,
			})
			if err != nil {
				return err
			}

			switch {
			case g.quiet:
				// Print nothing; the exit code carries the answer.
			case statsOnly:
				stats := lens.SummarizeDiff(result)
				if asJSON {
					enc := json.NewEncoder(stdout)
					enc.SetIndent("", "  ")
					if err := enc.Encode(stats); err != nil {
						return fmt.Errorf("encoding diff stats: %w", err)
					}
				} else {
					fmt.Fprintln(stdout, stats.String())
				}
			case summaryOnly:
				palette, err := paletteForOutput(stdout, g.color)
				if err != nil {
					return err
				}
				f := format.NewDiffSummaryFormatter(
					format.WithDiffSummaryPalette(palette),
				)
				if err := f.Format(stdout, result); err != nil {
					return err
				}
			case jsonPatch:
				leftValue, err := decodeValue(left, typeName)
				if err != nil {
					return fmt.Errorf("decoding first value: %w", err)
				}
				rightValue, err := decodeValue(right, typeName)
				if err != nil {
					return fmt.Errorf("decoding second value: %w", err)
				}
				f := format.NewJSONPatchFormatter(
					format.WithJSONPatchSources(leftValue, rightValue),
				)
				if err := f.Format(stdout, result); err != nil {
					return err
				}
			case unified:
				leftValue, err := decodeValue(left, typeName)
				if err != nil {
					return fmt.Errorf("decoding first value: %w", err)
				}
				rightValue, err := decodeValue(right, typeName)
				if err != nil {
					return fmt.Errorf("decoding second value: %w", err)
				}
				f := format.NewUnifiedFormatter(
					format.WithUnifiedContext(context),
					format.WithUnifiedFilter(filter),
				)
				if err := f.Format(stdout, leftValue, rightValue); err != nil {
					return err
				}
			case sideBySide:
				leftValue, err := decodeValue(left, typeName)
				if err != nil {
					return fmt.Errorf("decoding first value: %w", err)
				}
				rightValue, err := decodeValue(right, typeName)
				if err != nil {
					return fmt.Errorf("decoding second value: %w", err)
				}
				palette, err := paletteForOutput(stdout, g.color)
				if err != nil {
					return err
				}
				f := format.NewSideBySideFormatter(
					format.WithSideBySidePalette(palette),
					format.WithSideBySideContext(context),
					format.WithSideBySideWidth(sideWidth),
					format.WithSideBySideFilter(filter),
				)
				if err := f.Format(stdout, leftValue, rightValue); err != nil {
					return err
				}
			case asJSON:
				f := format.NewDiffJSONFormatter()
				if err := f.Format(stdout, result); err != nil {
					return err
				}
			default:
				palette, err := paletteForOutput(stdout, g.color)
				if err != nil {
					return err
				}
				f := format.NewDiffFormatter(
					format.WithDiffPalette(palette),
				)
				if err := f.Format(stdout, result); err != nil {
					return err
				}
				if len(result.Redacted) > 0 {
					fmt.Fprintf(stdout, "\n%d path(s) redacted: %s\n",
						len(result.Redacted), strings.Join(result.Redacted, ", "))
				}
			}

			if (exitDiff || g.quiet) && !result.Equal() {
				return errValuesDiffer
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&typeName, "type", "t", "",
		"decode both values as this XDR type instead of detecting each")
	cmd.Flags().StringVar(&formatName, "format", "", "output format: text, json, json-patch, unified, side-by-side, stats, summary")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit the diff as JSON")
	cmd.Flags().BoolVar(&jsonPatch, "json-patch", false, "emit an RFC 6902 JSON Patch of the difference")
	cmd.Flags().BoolVar(&unified, "unified", false, "emit a standard unified diff")
	cmd.Flags().BoolVar(&sideBySide, "side-by-side", false, "emit a colourised two-column diff")
	cmd.Flags().StringArrayVar(&includePaths, "include-paths", nil,
		"only report changes matching these path patterns (comma-separated, repeatable)")
	cmd.Flags().StringArrayVar(&includePaths, "include-path", nil,
		"alias for --include-paths")
	cmd.Flags().StringArrayVar(&excludePaths, "exclude-paths", nil,
		"ignore changes matching these path patterns (comma-separated, repeatable)")
	cmd.Flags().StringArrayVar(&excludePaths, "exclude-path", nil,
		"alias for --exclude-paths")
	cmd.Flags().StringArrayVar(&excludePaths, "ignore-paths", nil,
		"alias for --exclude-paths")
	cmd.Flags().StringArrayVar(&excludePaths, "ignore-path", nil,
		"alias for --exclude-paths")
	cmd.Flags().StringArrayVar(&maskPaths, "mask-paths", nil,
		"exclude matching paths from diff evaluation entirely (comma-separated, repeatable)")
	cmd.Flags().StringArrayVar(&maskPaths, "mask-path", nil,
		"alias for --mask-paths")
	cmd.Flags().StringArrayVar(&redact, "redact", nil,
		"normalize dynamic cryptographic values before diffing: hashes, signatures, identifiers or all")
	cmd.Flags().StringArrayVar(&redactFields, "redact-fields", nil,
		"field names (or globs) whose values are normalized before diffing (comma-separated, repeatable)")
	cmd.Flags().IntVar(&context, "context", 3, "context lines for --unified and --side-by-side")
	cmd.Flags().IntVar(&sideWidth, "side-by-side-width", 160, "total column width for --side-by-side")
	cmd.Flags().BoolVar(&alignLists, "align", false,
		"align list elements with LCS to reduce insertion and removal noise")
	cmd.Flags().BoolVar(&semantic, "semantic", false,
		"compare supported values semantically, including exact ScVal wide integers")
	cmd.Flags().BoolVar(&unorderedSigners, "unordered-signers", false,
		"treat signature and signer collections as unordered multisets")
	cmd.Flags().BoolVar(&footprintMode, "footprint", false,
		"compare the Soroban ledger footprints of the two values as sets")
	cmd.Flags().BoolVar(&statsOnly, "stats", false,
		"print only the statistical summary of the diff")
	cmd.Flags().Float64Var(&diffThreshold, "diff-threshold", 0,
		"ignore scalar numeric differences below the given delta")
	cmd.Flags().BoolVar(&exitDiff, "exit-code", false,
		"exit with a non-zero status when the values differ")

	return cmd
}

// validateDiffModes rejects combinations of output modes that would otherwise
// silently pick one. --stats --json is a single mode (the JSON statistics), so
// it is allowed.
func validateDiffModes(asJSON, statsOnly, summaryOnly, jsonPatch, unified, sideBySide, footprint bool) error {
	modes := 0
	if asJSON || statsOnly || summaryOnly {
		modes++
	}
	for _, on := range []bool{jsonPatch, unified, sideBySide} {
		if on {
			modes++
		}
	}
	if modes > 1 {
		return fmt.Errorf("%w: choose one of --json/--stats/--summary, --json-patch, --unified or --side-by-side", lens.ErrInvalidArgument)
	}
	if footprint && (jsonPatch || unified || sideBySide) {
		return fmt.Errorf("%w: --footprint cannot be combined with --json-patch, --unified or --side-by-side", lens.ErrInvalidArgument)
	}
	return nil
}

// runFootprintDiff decodes both operands and reports the set difference
// between the Soroban ledger footprints they declare.
func runFootprintDiff(stdout io.Writer, left, right string, asJSON, quietMode, exitDiff bool) error {
	leftValue, err := lens.Decode(left)
	if err != nil {
		return fmt.Errorf("decoding first value: %w", err)
	}
	rightValue, err := lens.Decode(right)
	if err != nil {
		return fmt.Errorf("decoding second value: %w", err)
	}
	leftFootprint, ok := lens.FootprintFromValue(leftValue)
	if !ok {
		return fmt.Errorf("first value (%s) declares no Soroban footprint", leftValue.Type)
	}
	rightFootprint, ok := lens.FootprintFromValue(rightValue)
	if !ok {
		return fmt.Errorf("second value (%s) declares no Soroban footprint", rightValue.Type)
	}
	result, err := lens.DiffFootprints(leftFootprint, rightFootprint)
	if err != nil {
		return err
	}

	switch {
	case quietMode:
		// Print nothing.
	case asJSON:
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			return fmt.Errorf("encoding footprint diff: %w", err)
		}
	default:
		if err := writeFootprintDiff(stdout, result); err != nil {
			return err
		}
	}

	if (exitDiff || quietMode) && !result.Equal() {
		return errValuesDiffer
	}
	return nil
}

// writeFootprintDiff renders a footprint set difference as a deterministic
// two-section report.
func writeFootprintDiff(w io.Writer, diff *lens.FootprintDiff) error {
	var b strings.Builder
	b.WriteString("read-only: " + diff.ReadOnly.Summary() + "\n")
	writeKeySet(&b, diff.ReadOnly)
	b.WriteString("read-write: " + diff.ReadWrite.Summary() + "\n")
	writeKeySet(&b, diff.ReadWrite)
	if _, err := io.WriteString(w, b.String()); err != nil {
		return fmt.Errorf("writing footprint diff: %w", err)
	}
	return nil
}

func writeKeySet(b *strings.Builder, set lens.FootprintKeySetDiff) {
	for _, key := range set.Removed {
		fmt.Fprintf(b, "  - %s\n", key)
	}
	for _, key := range set.Added {
		fmt.Fprintf(b, "  + %s\n", key)
	}
}

// errValuesDiffer now lives in exitcodes.go with the strict exit-code policy:
// a detected difference is a "diff found" condition, not a generic error, so
// it exits with ExitDiffFound (2).

// readOperand resolves a diff operand, which may be a file path, base64 XDR,
// or "-" for stdin.
func readOperand(arg string, stdin io.Reader) (string, error) {
	if arg == stdinMarker {
		b, err := io.ReadAll(stdin)
		if err != nil {
			return "", fmt.Errorf("reading stdin: %w", err)
		}
		return strings.TrimSpace(string(b)), nil
	}
	// Prefer an existing file, so a path is never mistaken for base64.
	if info, err := os.Stat(arg); err == nil && !info.IsDir() {
		b, err := os.ReadFile(arg)
		if err != nil {
			return "", fmt.Errorf("reading %s: %w", arg, err)
		}
		return strings.TrimSpace(string(b)), nil
	}
	return strings.TrimSpace(arg), nil
}
