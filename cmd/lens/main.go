// Command lens decodes, explains and diffs Stellar and Soroban XDR.
//
// It runs entirely offline: no network access is required or performed, which
// makes it safe to use on unsigned transactions and deterministic in CI.
//
// Usage:
//
//	lens decode  <base64>            decode XDR as a tree or JSON
//	lens explain <base64> [--result] explain a transaction in plain English
//	lens diff    <a> <b>             structurally compare two XDR values
//	lens types                       list decodable XDR types
//
// See https://github.com/stellar-optics/stellar-xdr-lens for full documentation.
package main

import (
	"fmt"
	"os"

	"github.com/stellar-optics/stellar-xdr-lens/internal/cli"
)

func main() {
	err := cli.Execute(os.Args[1:], os.Stdout, os.Stderr, os.Stdin)
	code, printable := cli.ExitCode(err)
	if printable {
		fmt.Fprintln(os.Stderr, "lens:", err)
	}
	os.Exit(code)
}
