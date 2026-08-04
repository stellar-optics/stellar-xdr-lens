# Contributing

Thanks for considering a contribution. This project is small and deliberately
easy to extend — the three most common contributions each touch a single file.

## Quick start

```sh
git clone https://github.com/stellar-optics/stellar-xdr-lens
cd stellar-xdr-lens
go build ./...
go test ./...
```

You need **Go 1.25 or later**, which is the floor required by
`github.com/stellar/go-stellar-sdk`.

There are no other dependencies to install and no code generation step. The
test suite makes no network calls, so it works offline and gives the same result
every run.

## Running the checks CI runs

Before opening a pull request, run what CI will run:

```sh
gofmt -l .                  # must print nothing
go vet ./...
go test -race ./...
golangci-lint run ./...     # see below to install
```

Install the linter if you do not have it:

```sh
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
```

For coverage:

```sh
go test -race -covermode=atomic -coverprofile=coverage.out \
  -coverpkg=./pkg/...,./internal/... ./...
go tool cover -html=coverage.out
```

## Good first contributions

These are designed to be one-file changes. See
[docs/architecture.md](docs/architecture.md) for the details.

### Add an XDR type

One line in `registry` in `pkg/lens/decode.go`. Detection, `--type`,
`lens types`, every output format and the differ pick it up automatically.

### Improve how a type renders

Add an `Enricher` in `pkg/lens/enrich.go` so a type displays the way developers
recognise it, the way `AccountId` already renders as a strkey address.

### Explain a result code better

`pkg/lens/reason.go` maps all 208 protocol result codes to plain English. If an
explanation is vague, wrong, or missing the advice you needed when you hit it,
improving it is a genuinely valuable change.

Two rules, both enforced by tests:

- Do not restate the constant. "tx_bad_seq" is not an explanation.
- Every code must have an entry. `TestEveryResultCodeIsExplained` will tell you
  if a new protocol version has added one.

## Project conventions

### Go only

The entire project is Go — library, CLI, tests and tooling. Please do not
introduce another language or a JS build step. Shell is fine for trivial CI
glue.

Beyond that, ordinary idiomatic Go: small interfaces, errors wrapped with `%w`,
`context.Context` where it belongs, no reflection-heavy magic, no DI framework.

Reflection is confined to `pkg/lens/node.go` by design. If you find yourself
needing `reflect` elsewhere, that is a strong signal the change belongs in the
node walker or an enricher instead.

### Tests

New behaviour needs a test. Table-driven is the house style:

```go
tests := []struct {
	name string
	// inputs and expectations
}{
	{name: "...", /* ... */},
}

for _, tc := range tests {
	t.Run(tc.name, func(t *testing.T) {
		t.Parallel()
		// ...
	})
}
```

Assert on behaviour that would break the tool, not on line coverage. Prefer
real captured XDR in `testdata/` where you can; where you cannot, build values
with the SDK encoder and round-trip them through the real decode path rather
than reaching into internals.

If you add a fixture, record its provenance in `testdata/manifest.json` so it
can be traced back to a ledger.

**Never add a test that makes a network call.** Offline and deterministic is a
property worth protecting.

### Commit messages

[Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<scope>): <short summary in the imperative mood>

<body explaining why, not what — the diff already says what>
```

Types in use: `feat`, `fix`, `docs`, `test`, `refactor`, `perf`, `chore`, `ci`.
Scopes in use: `lens`, `format`, `cli`, `fixtures`, `docs`.

Examples:

```
feat(lens): add ClaimableBalanceEntry to the type registry
fix(format): stop emitting escape codes when stdout is a pipe
docs(architecture): explain how enrichers are registered
```

Commit granularly. One logical change per commit is much easier to review and
to revert than a single large drop.

### Branch naming

```
<type>/<short-description>
```

For example `feat/ledger-entry-types`, `fix/nil-memo-panic`,
`docs/roadmap-update`.

## Pull requests

Before you open one:

- [ ] `go build ./...` passes
- [ ] `go test -race ./...` passes
- [ ] `golangci-lint run ./...` reports no issues
- [ ] `gofmt -l .` prints nothing
- [ ] New behaviour has tests
- [ ] Public API changes have doc comments
- [ ] Commits follow Conventional Commits

In the PR description, include the linked issue, what changed and why, and
**evidence it works** — the actual command output, not a claim that you ran it.
The template will prompt you for these.

Please open an issue before starting anything large, so we can agree on the
approach before you spend the time.

## Reporting bugs

Open an issue with the XDR that reproduces it. Because everything runs offline
and deterministically, a base64 payload plus the command you ran is almost
always enough to reproduce a bug exactly.

Do not paste XDR containing secrets. Envelopes are not secret, but if a payload
came from a private system, check before sharing it.

## Security

Do not report security issues in a public issue. See [SECURITY.md](SECURITY.md).

## Code of conduct

This project follows the [Contributor Covenant](CODE_OF_CONDUCT.md).

## Licence

By contributing, you agree that your contributions are licensed under
Apache-2.0, the same licence as the project.
