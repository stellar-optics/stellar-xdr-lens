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
)

// Version is the build version, overridden at release time via ldflags:
//
//	go build -ldflags "-X github.com/odusanya03/stellar-xdr-lens/internal/cli.Version=v0.1.0"
var Version = "dev"

// globalFlags holds options shared by every subcommand.
type globalFlags struct {
	// color is one of "auto", "always" or "never".
	color string
	// file reads the payload from a path instead of an argument.
	file string
}

// Execute builds and runs the root command. It returns an error rather than
// exiting so that main stays in control of the process exit code.
func Execute(args []string, stdout, stderr io.Writer, stdin io.Reader) error {
	root := newRootCmd(stdout, stderr, stdin)
	root.SetArgs(args)
	return root.Execute()
}

func newRootCmd(stdout, stderr io.Writer, stdin io.Reader) *cobra.Command {
	g := &globalFlags{}

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
  lens diff a.xdr b.xdr`,
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
		`when to colourise output: "auto", "always" or "never"`)
	root.PersistentFlags().StringVarP(&g.file, "file", "f", "",
		"read the XDR payload from a file instead of an argument")

	root.AddCommand(
		newDecodeCmd(g, stdout, stdin),
		newExplainCmd(g, stdout, stdin),
		newDiffCmd(g, stdout, stdin),
		newTypesCmd(stdout),
		newVersionCmd(stdout),
	)
	return root
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
