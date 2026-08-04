# Security Policy

## Supported versions

This project is pre-1.0. Security fixes are applied to the latest release and
to `main`; older tags are not patched.

| Version | Supported |
| --- | --- |
| 0.1.x | ✅ |
| < 0.1 | ❌ |

## Reporting a vulnerability

**Please do not open a public issue for a security vulnerability.**

Report it through
[GitHub's private vulnerability reporting](https://github.com/odusanya03/stellar-xdr-lens/security/advisories/new),
which opens a private channel with the maintainers.

Please include:

- what the issue is and why it matters;
- the XDR payload or input that triggers it — since everything here runs
  offline and deterministically, that is usually enough to reproduce exactly;
- the version or commit you tested;
- any suggested fix, if you have one.

### What to expect

- **Acknowledgement** within 5 working days.
- **An assessment** — whether we consider it a vulnerability, and how severe —
  within 10 working days.
- **A fix and advisory** for confirmed issues, coordinated with you on timing.

You will be credited in the advisory unless you prefer otherwise.

## Threat model

Understanding what this tool does makes it clearer what counts as a
vulnerability.

`lens` is an **offline parser of untrusted input**. It:

- makes no network calls, in either the library or the CLI;
- never handles secret keys, and has no signing or key-management code;
- never executes anything it decodes;
- reads from arguments, files and stdin, and writes to stdout and stderr.

The realistic attack surface is therefore **malformed or hostile XDR** — a
truncated blob, a hostile length prefix, or a deeply nested structure fed to a
tool running in someone's CI pipeline.

### In scope

- A crash, panic or unrecovered runtime error triggered by malformed XDR.
  The decoder and every enricher run behind a `recover` precisely because
  generated XDR accessors dereference absent union arms, but a panic that
  escapes is a bug — please report it.
- Unbounded memory or CPU consumption from a small input (a decompression- or
  allocation-bomb pattern). Both the SDK's decode limits and this project's own
  depth cap exist to prevent this.
- Any path that reads or writes a file the user did not name.
- Any network connection made by the library or by a core CLI command.
- Output that misrepresents a transaction — for example reporting a failed
  transaction as successful, or attributing a failure to the wrong operation.
  On a tool people use to decide whether a transaction worked, a confidently
  wrong answer is a security problem, not merely a bug.

### Out of scope

- Vulnerabilities in `github.com/stellar/go-stellar-sdk` itself. Report those
  to that project; we will pick up the fixed version.
- Denial of service that requires an input larger than the memory you chose to
  give the process.
- Anything requiring an attacker to already control the machine running `lens`.
- The absence of a hardening feature that would not prevent a concrete attack.

## For users

`lens` is safe to run on unsigned transaction envelopes: it does not transmit
them anywhere. That said, treat XDR the way you treat any data — an envelope
may reveal account addresses, amounts and memos, so think before pasting one
from a private system into a public issue.
