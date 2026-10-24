package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

// Shell autocompletion engine.
//
// Two halves live here: the completion sources (XDR type names from the live
// registry, plus the fixed choice lists for enum-like flags) and the wiring
// that attaches them to every command in the tree. Everything derives from
// the registry at completion time, so `lens completion bash` and
// `lens __complete decode --type ''` stay truthful when new types register —
// including types a feature file registered at init.
//
// All of it is local computation; completion never touches network or disk.

// xdrTypeSuggestions returns the registered XDR type names, sorted, deduped
// by the registry itself, and ready to feed a shell completion function. The
// list is computed from the live registry; the registry is populated at init
// and is read-only afterwards, so nothing else is needed for concurrency.
func xdrTypeSuggestions() []string {
	return lens.TypeNames()
}

// xdrTypeSuggestionsFor filters the registry names by the prefix the shell
// asked for, which is what the flag completion function needs for `--type`.
func xdrTypeSuggestionsFor(prefix string) []string {
	all := xdrTypeSuggestions()
	if prefix == "" {
		return all
	}
	filtered := make([]string, 0, len(all))
	for _, name := range all {
		if strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix)) {
			filtered = append(filtered, name)
		}
	}
	return filtered
}

// completeTypes is the flag completion function for every --type flag.
func completeTypes(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return xdrTypeSuggestionsFor(toComplete), cobra.ShellCompDirectiveNoFileComp
}

// choiceCompletions builds a completion function from a fixed list of
// accepted values, filtered by prefix. It is what --format, --color,
// --network and --redact complete from, and the values are the ones the
// corresponding commands actually validate.
func choiceCompletions(values ...string) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		filtered := make([]string, 0, len(values))
		for _, v := range values {
			if strings.HasPrefix(v, strings.ToLower(toComplete)) {
				filtered = append(filtered, v)
			}
		}
		return filtered, cobra.ShellCompDirectiveNoFileComp
	}
}

// staticChoices pairs each completion source with the commands whose flag
// takes that value. The command names match the real tree in root.go; a
// command listed here without the flag is skipped, so the table drifts
// safely, and a new command only needs one line. The persistent --color is
// completed once on the root command instead.
func staticChoices() map[string]map[string][]string {
	return map[string]map[string][]string{
		"decode": {
			"format":  {"tree", "json", "summary", "txrep", "csv", "html", "mermaid", "junit"},
			"network": networkFlagValues,
		},
		"asset": {
			"network": networkFlagValues,
		},
		"diff": {
			"format": {"text", "json", "json-patch", "unified", "side-by-side", "stats"},
			"redact": redactFlagValues,
		},
	}
}

// Values the flags accept. They mirror the validation in the commands; the
// completion list and the validator must agree, or the shell will offer
// something the CLI rejects.
var (
	colorFlagValues   = []string{"auto", "always", "never"}
	networkFlagValues = []string{"mainnet", "testnet", "futurenet"}
	redactFlagValues  = []string{"hashes", "signatures", "identifiers", "all"}
)

// typeFlaggedCommands lists the commands carrying an XDR-type flag, mapped to
// the exact flag names, so a new command only needs one line here.
func typeFlaggedCommands() map[string][]string {
	return map[string][]string{
		"decode":  {"type", "t"},
		"explore": {"type", "t"},
		"diff":    {"type", "t"},
		"encode":  {"type", "t"},
	}
}

// registerCompletions wires dynamic completion for every command in the
// tree. It is called once from newRootCmd, after the commands exist, and
// depends on nothing outside this package but the registry and cobra.
func registerCompletions(root *cobra.Command) {
	// XDR type names for every --type flag, drawn from the live registry.
	for name, flagNames := range typeFlaggedCommands() {
		cmd, _, err := root.Find([]string{name})
		if err != nil || cmd == nil || cmd == root {
			continue
		}
		for _, flag := range flagNames {
			if cmd.Flags().Lookup(flag) != nil {
				if err := cmd.RegisterFlagCompletionFunc(flag, completeTypes); err != nil {
					cmd.PrintErrf("warning: no completion for --%s: %v\n", flag, err)
				}
			}
		}
	}

	// Choice lists for the enum-like flags, including the persistent ones
	// that every command inherits (--color lives on the root command, so it
	// is completed from there).
	if err := root.RegisterFlagCompletionFunc("color", choiceCompletions(colorFlagValues...)); err != nil {
		root.PrintErrf("warning: no completion for --color: %v\n", err)
	}

	for name, flags := range staticChoices() {
		cmd, _, err := root.Find([]string{name})
		if err != nil || cmd == nil || cmd == root {
			continue
		}
		for flag, values := range flags {
			if cmd.Flags().Lookup(flag) == nil {
				continue
			}
			if err := cmd.RegisterFlagCompletionFunc(flag, choiceCompletions(values...)); err != nil {
				cmd.PrintErrf("warning: no completion for --%s: %v\n", flag, err)
			}
		}
	}
}

// newCompletionCmd emits the shell scripts for bash, zsh, fish and
// PowerShell. The scripts delegate back to `lens __complete`, where the
// dynamic sources above answer with the registry and the choice lists.
func newCompletionCmd(root *cobra.Command, stdout, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion",
		Short: "Generate shell completion scripts for bash, zsh, fish and PowerShell",
		Long: `Generate the shell completion script for your shell.

Each script wires a shell keypress to ` + "`lens __complete`" + `, which answers
with the registered XDR type names for every --type flag and the accepted
values for the fixed-choice flags. Everything is computed locally from the
registry, the same way decoding is.

Install per shell:

  bash        source <(lens completion bash)
  zsh         lens completion zsh > "${fpath[1]}/_lens"
  fish        lens completion fish | source
  powershell  lens completion powershell | Out-String | Invoke-Expression`,
		SilenceUsage: true,
	}

	for _, sh := range []struct {
		name string
		gen  func(io.Writer) error
	}{
		{"bash", func(w io.Writer) error { return root.GenBashCompletionV2(w, true) }},
		{"zsh", func(w io.Writer) error { return root.GenZshCompletion(w) }},
		{"fish", func(w io.Writer) error { return root.GenFishCompletion(w, true) }},
		{"powershell", func(w io.Writer) error { return root.GenPowerShellCompletionWithDesc(w) }},
	} {
		shell := sh
		sub := &cobra.Command{
			Use:                   shell.name,
			Short:                 fmt.Sprintf("Emit the %s completion script", shell.name),
			Args:                  cobra.NoArgs,
			DisableFlagsInUseLine: true,
			RunE: func(cmd *cobra.Command, args []string) error {
				return shell.gen(stdout)
			},
		}
		cmd.AddCommand(sub)
	}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	return cmd
}
