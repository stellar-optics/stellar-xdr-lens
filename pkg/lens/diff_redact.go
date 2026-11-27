package lens

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

// Deterministic hash and signature redaction for structural diffs.
//
// A rebuilt transaction legitimately differs from its predecessor in the
// values it cannot reproduce: transaction hashes, ledger hashes, signatures
// and the signature hints that point at them. A positional diff reports every
// one of those as a change, which buries the fields a reviewer actually wants
// to see. Redaction normalizes those dynamic cryptographic values before the
// walk so that two values which differ only in such fields compare equal.
//
// Redaction never mutates its input and never weakens the comparison of the
// values that remain: it replaces a matched scalar with a fixed placeholder,
// so two different hashes become the same placeholder and therefore compare
// equal, while every other field is compared exactly as before.
//
// The feature is opt-in. A zero RedactionPolicy is disabled, so existing
// Diff, DiffWithOptions and CLI behavior are unchanged unless a caller asks
// for redaction.

// Redaction placeholders. They are stable strings so that a redacted diff is
// deterministic across runs and platforms.
const (
	// RedactedHash replaces a hash-shaped scalar.
	RedactedHash = "<redacted hash>"
	// RedactedSignature replaces a signature-shaped scalar.
	RedactedSignature = "<redacted signature>"
	// RedactedIdentifier replaces a derived identifier such as a contract or
	// pool id.
	RedactedIdentifier = "<redacted identifier>"
	// RedactedField replaces a scalar named by RedactionPolicy.Fields.
	RedactedField = "<redacted>"
)

// RedactionPolicy selects which dynamic cryptographic values are normalized
// before a structural comparison. The zero value redacts nothing.
//
// Hash, signature and identifier matching is by the decoded XDR type, which is
// what makes it reliable: a Hash field is recognized wherever it appears, not
// only at a known path. Fields adds name-based matching for the cases where a
// value is not carried by a self-describing type, such as an anonymous byte
// array named Hint.
type RedactionPolicy struct {
	// Hashes redacts xdr.Hash values and fields typed as a 32-byte hash.
	Hashes bool
	// Signatures redacts xdr.Signature values and signature hint components.
	Signatures bool
	// Identifiers redacts derived identifiers: contract ids, pool ids and
	// claimable balance ids.
	Identifiers bool
	// Fields lists additional field names to redact. A name may be an exact
	// field name or a shell glob (as understood by path.Match) matched against
	// the field name alone.
	Fields []string
}

// Enabled reports whether the policy redacts anything.
func (p RedactionPolicy) Enabled() bool {
	return p.Hashes || p.Signatures || p.Identifiers || len(p.Fields) > 0
}

// RedactHashes returns a policy that redacts hash-shaped values only.
func RedactHashes() RedactionPolicy { return RedactionPolicy{Hashes: true} }

// RedactSignatures returns a policy that redacts signature-shaped values only.
func RedactSignatures() RedactionPolicy { return RedactionPolicy{Signatures: true} }

// RedactHashesAndSignatures returns a policy that redacts both hash and
// signature values. It is the policy the CLI selects by default for
// `lens diff --redact hashes,signatures`.
func RedactHashesAndSignatures() RedactionPolicy {
	return RedactionPolicy{Hashes: true, Signatures: true}
}

// RedactIdentifiers returns a policy that redacts derived identifiers.
func RedactIdentifiers() RedactionPolicy { return RedactionPolicy{Identifiers: true} }

// placeholderFor classifies a node and returns the placeholder to substitute
// for it. The second result is false when the node is not redacted.
func (p RedactionPolicy) placeholderFor(n *Node) (string, bool) {
	if n == nil || !p.Enabled() {
		return "", false
	}
	if p.matchesField(n.Name) {
		return RedactedField, true
	}
	if n.Kind != KindScalar {
		return "", false
	}
	switch n.TypeName {
	case "Hash", "Uint256", "HashX", "PreAuthTx":
		if p.Hashes {
			return RedactedHash, true
		}
	case "Signature", "SignatureHint":
		if p.Signatures {
			return RedactedSignature, true
		}
	case "ContractId", "PoolId", "ClaimableBalanceId", "LiquidityPoolId":
		if p.Identifiers {
			return RedactedIdentifier, true
		}
	}
	if p.Signatures {
		switch n.Name {
		case "Hint", "Ed25519", "HashX", "PreAuthTx", "Signature":
			return RedactedSignature, true
		}
	}
	return "", false
}

// mergedPaths returns the union of two path lists, deduplicated and ordered by
// first appearance (left then right).
func mergedPaths(left, right []string) []string {
	merged := make([]string, 0, len(left)+len(right))
	for _, p := range left {
		merged = appendPath(merged, p)
	}
	for _, p := range right {
		merged = appendPath(merged, p)
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}

// matchesField reports whether name is selected by RedactionPolicy.Fields.
func (p RedactionPolicy) matchesField(name string) bool {
	if name == "" {
		return false
	}
	for _, pattern := range p.Fields {
		if pattern == "" {
			continue
		}
		if pattern == name {
			return true
		}
		if matched, err := path.Match(pattern, name); err == nil && matched {
			return true
		}
	}
	return false
}

// redactNode returns a deep copy of n in which every redacted scalar is
// replaced by its placeholder. The input tree is never modified. It also
// records the path of each redacted scalar, deduplicated in first-seen order.
func redactNode(n *Node, p RedactionPolicy, prefix string, paths *[]string) *Node {
	if n == nil {
		return nil
	}
	current := join(prefix, n.Name)
	if placeholder, ok := p.placeholderFor(n); ok {
		*paths = appendPath(*paths, current)
		return &Node{
			Name:     n.Name,
			Kind:     KindScalar,
			TypeName: n.TypeName,
			Value:    placeholder,
		}
	}
	clone := *n
	if len(n.Children) > 0 {
		clone.Children = make([]*Node, len(n.Children))
		for i, child := range n.Children {
			clone.Children[i] = redactNode(child, p, current, paths)
		}
	}
	return &clone
}

// appendPath appends path when it is not already present, preserving the
// order in which paths were first seen.
func appendPath(paths []string, p string) []string {
	for _, existing := range paths {
		if existing == p {
			return paths
		}
	}
	return append(paths, p)
}

// RedactValue returns a copy of v whose dynamic cryptographic scalars are
// replaced by fixed placeholders, together with the deduplicated paths that
// were redacted (in document order). The returned value's Raw field is cleared
// so that the unredacted XDR cannot leak out through it.
//
// The input value is not modified. RedactValue is the value-level counterpart
// of DiffOptions.Redaction and is useful for sharing a tree safely.
func RedactValue(v *Value, p RedactionPolicy) (*Value, []string, error) {
	if v == nil || v.Node == nil {
		return nil, nil, fmt.Errorf("%w: redaction requires a decoded value", ErrInvalidArgument)
	}
	if !p.Enabled() {
		clone := *v
		return &clone, nil, nil
	}
	var paths []string
	node := redactNode(v.Node, p, "", &paths)
	return &Value{Type: v.Type, Node: node}, paths, nil
}

// RedactAddress masks the interior of a Stellar strkey address while keeping
// the first and last four characters, so an address remains recognizable to
// its owner in a shared log without being usable or searchable in full:
//
//	GA3FJ5...WXYZ  ->  GA3F...WXYZ
//
// Strings of eight characters or fewer are masked entirely, since preserving
// both ends would reveal the whole value. RedactAddress is a pure string
// transform: callers that want to redact a stream of formatted output should
// use RedactAddresses, which recognizes address-shaped tokens.
func RedactAddress(addr string) string {
	runes := []rune(addr)
	if len(runes) == 0 {
		return ""
	}
	if len(runes) <= 8 {
		return strings.Repeat("*", len(runes))
	}
	return string(runes[:4]) + RedactedAddressMarker + string(runes[len(runes)-4:])
}

// RedactedAddressMarker separates the visible ends of a redacted address.
const RedactedAddressMarker = "..."

// strkeyAddressPattern finds runs of base32 characters that start with a
// Stellar strkey version byte. Candidate length is validated separately, so an
// unrelated uppercase identifier is left alone.
var strkeyAddressPattern = regexp.MustCompile(`[GCMSTLXB][A-Z2-7]{20,}`)

// RedactAddresses replaces every Stellar strkey address in text with its
// masked form. It is the stream-level counterpart of RedactAddress and is what
// the CLI's --redact-addresses flag applies to formatted output, so text, JSON
// and tree formatters all get the same treatment.
func RedactAddresses(text string) string {
	return strkeyAddressPattern.ReplaceAllStringFunc(text, func(token string) string {
		if !isStrkeyAddress(token) {
			return token
		}
		return RedactAddress(token)
	})
}

// isStrkeyAddress reports whether token has the exact length of a Stellar
// strkey for its version byte. Checking the length avoids masking arbitrary
// long uppercase strings that merely resemble an address.
func isStrkeyAddress(token string) bool {
	if token == "" {
		return false
	}
	switch token[0] {
	case 'G', 'C', 'S', 'T', 'X', 'L':
		return len(token) == 56
	case 'M':
		return len(token) == 69
	case 'B':
		return len(token) == 72
	default:
		return false
	}
}

// RedactionPolicyFromNames builds a policy from category names, which is how
// the CLI parses its --redact flag. Recognized names are "hashes",
// "signatures", "identifiers" and "all"; matching is case-insensitive and
// surrounding whitespace is ignored. An empty list enables nothing, and an
// unknown name wraps ErrInvalidArgument.
func RedactionPolicyFromNames(names []string) (RedactionPolicy, error) {
	var policy RedactionPolicy
	for _, raw := range names {
		for _, name := range strings.Split(raw, ",") {
			name = strings.ToLower(strings.TrimSpace(name))
			switch name {
			case "":
				continue
			case "all":
				policy.Hashes = true
				policy.Signatures = true
				policy.Identifiers = true
			case "hashes", "hash":
				policy.Hashes = true
			case "signatures", "signature", "sigs":
				policy.Signatures = true
			case "identifiers", "identifier", "ids":
				policy.Identifiers = true
			default:
				return RedactionPolicy{}, fmt.Errorf("%w: unknown redaction category %q (want hashes, signatures, identifiers or all)", ErrInvalidArgument, name)
			}
		}
	}
	return policy, nil
}
