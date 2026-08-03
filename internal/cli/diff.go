package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/odusanya03/stellar-xdr-lens/pkg/lens"
	"github.com/odusanya03/stellar-xdr-lens/pkg/lens/format"
)

func newDiffCmd(g *globalFlags, stdout io.Writer, stdin io.Reader) *cobra.Command {
	var (
		typeName  string
		asJSON    bool
		exitDiff  bool
		quietMode bool
	)

	cmd := &cobra.Command{
		Use:   "diff <a> <b>",
		Short: "Structurally compare two XDR values",
		Long: `Compare two XDR values and report every path that differs.

Each argument is either base64 XDR or a path to a file containing it; files
are detected by existence, so no flag is needed to mix the two.

Unlike a byte comparison, this names the fields that changed — so a one-line
answer to "why does my rebuilt transaction not match" is the field, not a
mismatched hash.`,
		Example: `  lens diff "$XDR_A" "$XDR_B"
  lens diff before.xdr after.xdr
  lens diff --type TransactionEnvelope a.xdr b.xdr
  lens diff --exit-code a.xdr b.xdr   # non-zero when they differ`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			left, err := readOperand(args[0], stdin)
			if err != nil {
				return fmt.Errorf("reading first value: %w", err)
			}
			right, err := readOperand(args[1], stdin)
			if err != nil {
				return fmt.Errorf("reading second value: %w", err)
			}

			result, err := lens.DiffBase64(left, right, typeName)
			if err != nil {
				return err
			}

			switch {
			case quietMode:
				// Print nothing; the exit code carries the answer.
			case asJSON:
				enc := json.NewEncoder(stdout)
				enc.SetIndent("", "  ")
				if err := enc.Encode(diffDocument(result)); err != nil {
					return fmt.Errorf("encoding diff: %w", err)
				}
			default:
				f := format.NewDiffFormatter(
					format.WithDiffPalette(format.PaletteFor(stdout, g.color)),
				)
				if err := f.Format(stdout, result); err != nil {
					return err
				}
			}

			if (exitDiff || quietMode) && !result.Equal() {
				return errValuesDiffer
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&typeName, "type", "t", "",
		"decode both values as this XDR type instead of detecting each")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit the diff as JSON")
	cmd.Flags().BoolVar(&exitDiff, "exit-code", false,
		"exit with a non-zero status when the values differ")
	cmd.Flags().BoolVarP(&quietMode, "quiet", "q", false,
		"print nothing; report the answer only through the exit code")

	return cmd
}

// errValuesDiffer signals --exit-code, mirroring diff(1)'s convention of
// exiting 1 when the inputs differ.
var errValuesDiffer = &exitError{code: 1, msg: "values differ"}

// diffChange is the stable JSON shape for one change.
type diffChange struct {
	Path   string `json:"path"`
	Op     string `json:"op"`
	Before any    `json:"before,omitempty"`
	After  any    `json:"after,omitempty"`
}

// diffDoc is the stable JSON shape for a whole diff.
type diffDoc struct {
	LeftType  string       `json:"left_type"`
	RightType string       `json:"right_type"`
	Equal     bool         `json:"equal"`
	Summary   string       `json:"summary"`
	Changes   []diffChange `json:"changes"`
}

func diffDocument(d *lens.DiffResult) diffDoc {
	changes := make([]diffChange, 0, len(d.Changes))
	for _, c := range d.Changes {
		changes = append(changes, diffChange{
			Path:   c.Path,
			Op:     c.Op.String(),
			Before: c.Before,
			After:  c.After,
		})
	}
	return diffDoc{
		LeftType:  d.LeftType,
		RightType: d.RightType,
		Equal:     d.Equal(),
		Summary:   d.Summary(),
		Changes:   changes,
	}
}

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
