package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

func mustRegisterFlagCompletionFunc(cmd *cobra.Command, flag string, fn func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective)) {
	if err := cmd.RegisterFlagCompletionFunc(flag, fn); err != nil {
		panic(err)
	}
}

func installCompletionHandlers(cmd *cobra.Command) {
	cmd.InitDefaultCompletionCmd()

	mustRegisterFlagCompletionFunc(cmd, "color", func(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return completionValues([]string{"auto", "always", "never"}, toComplete), cobra.ShellCompDirectiveNoFileComp
	})

	mustRegisterFlagCompletionFunc(cmd, "file", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return nil, cobra.ShellCompDirectiveDefault
	})
}

func typeNameCompletion(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return completionValues(lens.TypeNames(), toComplete), cobra.ShellCompDirectiveNoFileComp
}

func completionValues(options []string, prefix string) []string {
	if len(options) == 0 {
		return nil
	}

	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return append([]string(nil), options...)
	}

	needle := strings.ToLower(prefix)
	matches := make([]string, 0, len(options))
	for _, option := range options {
		if strings.HasPrefix(strings.ToLower(option), needle) {
			matches = append(matches, option)
		}
	}
	return matches
}
