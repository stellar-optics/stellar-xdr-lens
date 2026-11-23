// Package cli implements the lens command-line interface.
//
// Commands are deliberately thin: they resolve input, call pkg/lens, and hand
// the result to a formatter. Any behaviour worth testing lives in the library
// so that it is reusable and testable without a process.
package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens/format"
)

// Version is the build version, overridden at release time via ldflags:
//
//	go build -ldflags "-X github.com/stellar-optics/stellar-xdr-lens/internal/cli.Version=v0.1.0"
var Version = "dev"

// globalFlags holds options shared by every subcommand.
type globalFlags struct {
	// color is one of "auto", "always" or "never".
	color string
	// file reads the payload from a path instead of an argument.
	file string
	// quiet suppresses standard output without changing the exit status.
	quiet bool
	// output writes standard output to a file instead (an empty value means
	// stdout, and "-" is accepted as an explicit synonym for stdout).
	output string
}

// Execute builds and runs the root command. It returns an error rather than
// exiting so that main stays in control of the process exit code.
func Execute(args []string, stdout, stderr io.Writer, stdin io.Reader) error {
	g := &globalFlags{}
	out := newOutputSink(stdout, g)
	root := newRootCmdWithFlags(g, out, stderr, stdin)
	root.SetArgs(args)
	err := root.Execute()
	if closeErr := out.Close(err == nil); err == nil && closeErr != nil {
		err = closeErr
	}
	return err
}

func newRootCmdWithFlags(g *globalFlags, stdout, stderr io.Writer, stdin io.Reader) *cobra.Command {
	root := &cobra.Command{
		Use:   "lens",
		Short: "Decode, explain and diff Stellar and Soroban XDR",
		Long: `lens inspects Stellar and Soroban XDR offline.

It decodes base64 XDR into a readable tree or stable JSON, explains what a
transaction did and why it failed in plain English, and structurally diffs
two XDR values by path.

Everything runs locally with no network access, so it is safe to use on
unsigned transactions and works in CI.`,
		Example: `  # Explain why a transaction failed, pairing envelope with result
  lens explain "$ENVELOPE" --result "$RESULT"

  # Decode from a pipeline
  curl -s "$HORIZON/transactions/$HASH" | jq -r .envelope_xdr | lens decode

  # Machine-readable output for scripting
  lens decode --json "$XDR" | jq '.value.V1.Tx.Fee'

  # See what changed between two envelopes
	lens diff a.xdr b.xdr

	# Summarize balance changes recorded in transaction metadata
	lens meta-diff transaction-meta.xdr`,
		SilenceUsage:  true,
		SilenceErrors: true,
		// Without args, print help rather than an error: a bare `lens` is a
		// request for orientation, not a mistake.
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetIn(stdin)

	root.PersistentFlags().StringVar(&g.color, "color", "auto",
		`when to colourise output: "auto", "always" or "never" (LENS_THEME selects light or dark colours)`)
	root.PersistentFlags().StringVarP(&g.file, "file", "f", "",
		"read the XDR payload from a file instead of an argument")
	root.PersistentFlags().BoolVarP(&g.quiet, "quiet", "q", false,
		"suppress standard output; the exit code still reports the result")
	root.PersistentFlags().StringVarP(&g.output, "output", "o", "",
		"write command output to a file instead of standard output")

	// Replace cobra's generic completion command with ours, so `lens
	// completion bash` and friends are documented behaviour of this tool.
	root.CompletionOptions.DisableDefaultCmd = true

	root.AddCommand(
		newDecodeCmd(g, stdout, stdin),
		newEncodeCmd(g, stdout, stdin),
		newExploreCmd(g, stdout, stdin),
		newExplainCmd(g, stdout, stdin),
		newDiffCmd(g, stdout, stdin),
		newResultDiffCmd(g, stdout, stdin),
		newMetaDiffCmd(g, stdout, stdin),
		newAssetCmd(g, stdout, stdin),
		newTypesCmd(stdout),
		newVersionCmd(stdout),
		newCompletionCmd(root, stdout, stderr),
	)

	// The completion engine attaches after the tree exists, so it can bind
	// to every command above.
	registerCompletions(root)
	return root
}

func paletteForOutput(w io.Writer, mode string) (format.Palette, error) {
	palette, err := format.ThemeFromEnv()
	if err != nil {
		return format.Palette{}, err
	}
	colorTarget := w
	if sink, ok := w.(*outputSink); ok {
		colorTarget = sink.colorTarget()
	}
	return format.PaletteForTheme(colorTarget, mode, palette), nil
}

// explorerBaseURL turns a --network value into the Stellar Expert base used for
// OSC 8 hyperlinks. A value that is not an explorer network name is a network
// passphrase (as --network is for txrep) or a typo, and disables links rather
// than guessing a network the addresses do not belong to.
func explorerBaseURL(network string) string {
	base, ok := format.ExplorerBaseURL(network)
	if !ok {
		return ""
	}
	return base
}

func newVersionCmd(stdout io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the lens version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintln(stdout, Version)
			return err
		},
	}
}
