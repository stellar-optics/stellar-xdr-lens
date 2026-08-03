package lens

import (
	"encoding/base64"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/stellar/go-stellar-sdk/xdr"
)

// ErrNoMatch is returned when a payload does not decode as any known XDR type.
var ErrNoMatch = errors.New("input did not decode as any known XDR type")

// ErrUnknownType is returned when a caller names a type that is not registered.
var ErrUnknownType = errors.New("unknown XDR type")

// Value is a successfully decoded XDR payload.
type Value struct {
	// Type is the registered XDR type name, e.g. "TransactionEnvelope".
	Type string
	// Node is the neutral tree used by formatters and the differ.
	Node *Node
	// Raw is the concrete decoded xdr.* value, for callers that want to
	// work with the SDK types directly. It is always a pointer.
	Raw any
}

// Candidate is a type that a payload successfully decoded into, together with
// a confidence score used to rank ambiguous inputs.
type Candidate struct {
	// Type is the registered XDR type name.
	Type string
	// Score ranks candidates; higher is more likely. See scoreCandidate.
	Score int
	// Value is the decoded payload for this candidate.
	Value *Value
}

// xdrType describes one decodable XDR type in the registry.
type xdrType struct {
	name string
	// new allocates a fresh pointer to the underlying type.
	new func() any
	// priority breaks ties between types that decode the same bytes. Types
	// developers actually paste into a terminal rank above obscure ones.
	priority int
}

// registry lists every type that Detect will attempt, in registration order.
//
// To support a new XDR type, add an entry here. Detection, decoding, the
// --type flag and all output formats pick it up automatically; no other file
// needs to change. See docs/architecture.md.
var registry = []xdrType{
	// Transaction-level types: what a developer is most often holding.
	{"TransactionEnvelope", func() any { return new(xdr.TransactionEnvelope) }, 100},
	{"TransactionResult", func() any { return new(xdr.TransactionResult) }, 95},
	{"TransactionMeta", func() any { return new(xdr.TransactionMeta) }, 90},
	{"TransactionResultPair", func() any { return new(xdr.TransactionResultPair) }, 70},
	{"Transaction", func() any { return new(xdr.Transaction) }, 65},
	{"FeeBumpTransaction", func() any { return new(xdr.FeeBumpTransaction) }, 60},
	{"TransactionV0", func() any { return new(xdr.TransactionV0) }, 55},

	// Ledger and history types.
	{"LedgerCloseMeta", func() any { return new(xdr.LedgerCloseMeta) }, 85},
	{"LedgerEntry", func() any { return new(xdr.LedgerEntry) }, 80},
	{"LedgerKey", func() any { return new(xdr.LedgerKey) }, 75},
	{"LedgerHeader", func() any { return new(xdr.LedgerHeader) }, 50},
	{"LedgerHeaderHistoryEntry", func() any { return new(xdr.LedgerHeaderHistoryEntry) }, 45},

	// Soroban types.
	{"ScVal", func() any { return new(xdr.ScVal) }, 40},
	{"ScSpecEntry", func() any { return new(xdr.ScSpecEntry) }, 35},
	{"ContractEvent", func() any { return new(xdr.ContractEvent) }, 34},
	{"SorobanTransactionData", func() any { return new(xdr.SorobanTransactionData) }, 33},
	{"SorobanAuthorizationEntry", func() any { return new(xdr.SorobanAuthorizationEntry) }, 32},
	{"DiagnosticEvent", func() any { return new(xdr.DiagnosticEvent) }, 31},

	// Smaller building blocks. These decode from very short payloads and are
	// frequently ambiguous with one another, hence the low priority.
	{"Operation", func() any { return new(xdr.Operation) }, 20},
	{"OperationResult", func() any { return new(xdr.OperationResult) }, 19},
	{"Memo", func() any { return new(xdr.Memo) }, 10},
	{"Asset", func() any { return new(xdr.Asset) }, 9},
	{"Price", func() any { return new(xdr.Price) }, 8},
	{"SignerKey", func() any { return new(xdr.SignerKey) }, 7},
	{"ClaimableBalanceId", func() any { return new(xdr.ClaimableBalanceId) }, 6},
}

// TypeNames returns every registered XDR type name, sorted, for use in help
// text, shell completion and the --type flag.
func TypeNames() []string {
	names := make([]string, 0, len(registry))
	for _, t := range registry {
		names = append(names, t.name)
	}
	sort.Strings(names)
	return names
}

// normalizeInput trims whitespace and tolerates the URL-safe base64 alphabet,
// which is what turns up when XDR is copied out of a query string.
func normalizeInput(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', '\t', ' ':
			return -1
		}
		return r
	}, s)
	if strings.ContainsAny(s, "-_") && !strings.ContainsAny(s, "+/") {
		s = strings.NewReplacer("-", "+", "_", "/").Replace(s)
	}
	return s
}

// decodeInto attempts to decode payload as the given registry entry. It
// returns a Value on success.
//
// The SDK's SafeUnmarshalBase64 requires the input to be fully consumed, so a
// payload with trailing bytes is rejected rather than silently truncated.
// That strictness is what makes type detection viable at all.
func decodeInto(t xdrType, payload string) (*Value, error) {
	dest := t.new()
	if err := safeUnmarshal(payload, dest); err != nil {
		return nil, err
	}
	rv := reflect.ValueOf(dest).Elem()
	node := buildNode("", rv, 0)
	if node == nil {
		return nil, fmt.Errorf("%s produced an empty tree", t.name)
	}
	node.TypeName = t.name
	return &Value{Type: t.name, Node: node, Raw: dest}, nil
}

// safeUnmarshal wraps the SDK decoder with a recover. Decoding hostile input
// is this tool's entire job, and a malformed length prefix deep inside a
// nested union should surface as an error, not a crash.
func safeUnmarshal(payload string, dest any) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("decoder panicked on malformed input: %v", r)
		}
	}()
	if err := xdr.SafeUnmarshalBase64(payload, dest); err != nil {
		return fmt.Errorf("xdr decode: %w", err)
	}
	return nil
}

// Detect decodes payload against every registered XDR type and returns the
// candidates that succeeded, best first.
//
// More than one candidate is normal and not a bug: short XDR values are
// genuinely ambiguous because distinct types can share an encoding. An empty
// Memo and a native Asset, for example, are both the four zero bytes
// "AAAAAA==". Callers that need certainty should use DecodeAs.
func Detect(payload string) ([]Candidate, error) {
	payload = normalizeInput(payload)
	if payload == "" {
		return nil, fmt.Errorf("empty input")
	}
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil, fmt.Errorf("input is not valid base64: %w", err)
	}

	var out []Candidate
	for _, t := range registry {
		v, decErr := decodeInto(t, payload)
		if decErr != nil {
			continue
		}
		out = append(out, Candidate{
			Type:  t.name,
			Score: scoreCandidate(t, v, len(raw)),
			Value: v,
		})
	}
	if len(out) == 0 {
		return nil, ErrNoMatch
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out, nil
}

// scoreCandidate ranks a successful decode. The heuristic is deliberately
// simple and documented so its behaviour is predictable:
//
//   - the registry priority dominates, so common types win ties;
//   - trees with more decoded content score higher, since a type that
//     explains many bytes is a better fit than one that explains few.
func scoreCandidate(t xdrType, v *Value, rawLen int) int {
	score := t.priority * 1000
	score += countNodes(v.Node)
	// Very small payloads carry little evidence; damp the structural term so
	// priority decides, which keeps ambiguous 4-byte inputs stable.
	if rawLen <= 8 {
		score = t.priority * 1000
	}
	return score
}

func countNodes(n *Node) int {
	if n == nil {
		return 0
	}
	total := 1
	for _, c := range n.Children {
		total += countNodes(c)
	}
	return total
}

// Decode decodes payload using the best-scoring detected type.
//
// Use Detect when the caller needs to know that the input was ambiguous, or
// DecodeAs when the type is already known.
func Decode(payload string) (*Value, error) {
	cands, err := Detect(payload)
	if err != nil {
		return nil, err
	}
	return cands[0].Value, nil
}

// DecodeAs decodes payload as the named XDR type. The name is matched
// case-insensitively against TypeNames.
func DecodeAs(payload, typeName string) (*Value, error) {
	payload = normalizeInput(payload)
	if payload == "" {
		return nil, fmt.Errorf("empty input")
	}
	for _, t := range registry {
		if strings.EqualFold(t.name, typeName) {
			v, err := decodeInto(t, payload)
			if err != nil {
				return nil, fmt.Errorf("input is not a valid %s: %w", t.name, err)
			}
			return v, nil
		}
	}
	return nil, fmt.Errorf("%w: %q (see `lens types` for the supported list)", ErrUnknownType, typeName)
}
