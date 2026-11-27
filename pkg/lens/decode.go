package lens

import (
	"encoding/base64"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/stellar/go-stellar-sdk/xdr"
)

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

// detectExcluded lists types excluded from Detect() auto-detection because
// their on-wire encoding is structurally isomorphic with other registered
// types and would produce false positives. They are still fully supported by
// DecodeAs and the --type flag.
var detectExcluded = map[string]bool{
	// ScpBallot {Uint32, opaque<>} is isomorphic with Error {Uint32, string<>}.
	"ScpBallot": true,
	// ScpNomination {Hash, opaque<>[], opaque<>[]} is isomorphic with other
	// variable-length aggregate types at short lengths.
	"ScpNomination": true,
	// TimeBounds {Uint64, Uint64} = exactly 16 bytes; matches IPv4 PeerAddress,
	// EvictionIterator and other 16-byte structures.
	"TimeBounds": true,
	// LedgerBounds {Uint32, Uint32} = exactly 8 bytes; matches many small structs.
	"LedgerBounds": true,
}

// registryMu guards registry for concurrent access.
var registryMu sync.RWMutex

// registry lists every type that Detect will attempt, in registration order.
//
// To support a new XDR type, add an entry here or use RegisterType. Detection,
// decoding, the --type flag and all output formats pick it up automatically; no
// other file needs to change. See docs/architecture.md.
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
	{"LedgerCloseMetaBatch", func() any { return new(xdr.LedgerCloseMetaBatch) }, 54},
	{"LedgerHeader", func() any { return new(xdr.LedgerHeader) }, 50},
	{"LedgerHeaderHistoryEntry", func() any { return new(xdr.LedgerHeaderHistoryEntry) }, 45},
	{"TransactionHistoryEntry", func() any { return new(xdr.TransactionHistoryEntry) }, 42},
	{"TransactionHistoryResultEntry", func() any { return new(xdr.TransactionHistoryResultEntry) }, 41},

	// Bucket-list and consensus types: the ledger-state archive and the
	// protocol messages that agree on it.
	{"BucketEntry", func() any { return new(xdr.BucketEntry) }, 49},
	{"BucketMetadata", func() any { return new(xdr.BucketMetadata) }, 48},
	{"HotArchiveBucketEntry", func() any { return new(xdr.HotArchiveBucketEntry) }, 47},
	{"StellarValue", func() any { return new(xdr.StellarValue) }, 47},
	{"ScpEnvelope", func() any { return new(xdr.ScpEnvelope) }, 46},
	{"ScpStatement", func() any { return new(xdr.ScpStatement) }, 44},
	{"ScpQuorumSet", func() any { return new(xdr.ScpQuorumSet) }, 43},
	{"ScpBallot", func() any { return new(xdr.ScpBallot) }, 16},
	{"ScpNomination", func() any { return new(xdr.ScpNomination) }, 16},

	// Generalized transaction sets: the phase-based form introduced in
	// protocol 19, and the legacy v0 set it replaced.
	{"GeneralizedTransactionSet", func() any { return new(xdr.GeneralizedTransactionSet) }, 30},
	{"TransactionSetV1", func() any { return new(xdr.TransactionSetV1) }, 29},
	{"TransactionSet", func() any { return new(xdr.TransactionSet) }, 28},

	// Overlay (peer-to-peer) types: the messages Stellar Core nodes exchange
	// to find each other and share data. Hello and StellarMessage are
	// message-sized; Auth is a bare 4-byte flag word, so it ranks very low.
	{"StellarMessage", func() any { return new(xdr.StellarMessage) }, 36},
	{"Hello", func() any { return new(xdr.Hello) }, 35},
	{"PeerAddress", func() any { return new(xdr.PeerAddress) }, 27},
	{"SignedTimeSlicedSurveyRequestMessage", func() any { return new(xdr.SignedTimeSlicedSurveyRequestMessage) }, 26},
	{"SignedTimeSlicedSurveyResponseMessage", func() any { return new(xdr.SignedTimeSlicedSurveyResponseMessage) }, 25},
	{"SignedTimeSlicedSurveyStartCollectingMessage", func() any { return new(xdr.SignedTimeSlicedSurveyStartCollectingMessage) }, 24},
	// Start and stop collect messages have identical field lists, so their
	// encodings are identical; stop ranks lower and needs --type to win.
	{"SignedTimeSlicedSurveyStopCollectingMessage", func() any { return new(xdr.SignedTimeSlicedSurveyStopCollectingMessage) }, 21},
	{"DontHave", func() any { return new(xdr.DontHave) }, 23},
	{"AuthCert", func() any { return new(xdr.AuthCert) }, 22},
	{"Error", func() any { return new(xdr.Error) }, 3},
	{"Auth", func() any { return new(xdr.Auth) }, 2},

	// Soroban types.
	{"ScVal", func() any { return new(xdr.ScVal) }, 40},
	{"ScSpecEntry", func() any { return new(xdr.ScSpecEntry) }, 35},
	{"ContractEvent", func() any { return new(xdr.ContractEvent) }, 34},
	{"SorobanTransactionData", func() any { return new(xdr.SorobanTransactionData) }, 33},
	{"SorobanAuthorizationEntry", func() any { return new(xdr.SorobanAuthorizationEntry) }, 32},
	{"DiagnosticEvent", func() any { return new(xdr.DiagnosticEvent) }, 31},

	// State-archival entries: TTL records, the keys that identify them, and
	// the eviction cursor validators walk when expiring them.
	{"TtlEntry", func() any { return new(xdr.TtlEntry) }, 39},
	{"LedgerKeyTtl", func() any { return new(xdr.LedgerKeyTtl) }, 38},
	{"EvictionIterator", func() any { return new(xdr.EvictionIterator) }, 37},

	// Smaller building blocks. These decode from very short payloads and are
	// frequently ambiguous with one another, hence the low priority.
	{"TimeBounds", func() any { return new(xdr.TimeBounds) }, 15},
	{"LedgerBounds", func() any { return new(xdr.LedgerBounds) }, 14},
	{"Operation", func() any { return new(xdr.Operation) }, 20},
	{"OperationResult", func() any { return new(xdr.OperationResult) }, 19},
	{"Memo", func() any { return new(xdr.Memo) }, 10},
	// Ledger upgrades are commonly ambiguous with StellarMessage and Price.
	{"LedgerUpgrade", func() any { return new(xdr.LedgerUpgrade) }, 38},
	{"Asset", func() any { return new(xdr.Asset) }, 9},
	{"Price", func() any { return new(xdr.Price) }, 8},
	{"SignerKey", func() any { return new(xdr.SignerKey) }, 7},
	{"Signer", func() any { return new(xdr.Signer) }, 6},
	{"ClaimableBalanceId", func() any { return new(xdr.ClaimableBalanceId) }, 6},
	{"ClaimableBalanceEntry", func() any { return new(xdr.ClaimableBalanceEntry) }, 5},
	{"Claimant", func() any { return new(xdr.Claimant) }, 5},
	{"ClaimPredicate", func() any { return new(xdr.ClaimPredicate) }, 5},
	{"AccountEntry", func() any { return new(xdr.AccountEntry) }, 5},
	{"LedgerEntryExtensionV1", func() any { return new(xdr.LedgerEntryExtensionV1) }, 5},
	{"TrustLineEntry", func() any { return new(xdr.TrustLineEntry) }, 5},
	{"TrustLineEntryExtensionV2", func() any { return new(xdr.TrustLineEntryExtensionV2) }, 5},
	{"OfferEntry", func() any { return new(xdr.OfferEntry) }, 5},
	{"DataEntry", func() any { return new(xdr.DataEntry) }, 5},
	{"LiquidityPoolEntry", func() any { return new(xdr.LiquidityPoolEntry) }, 5},
	{"LiquidityPoolConstantProductParameters", func() any { return new(xdr.LiquidityPoolConstantProductParameters) }, 5},
	{"ContractCodeEntry", func() any { return new(xdr.ContractCodeEntry) }, 5},
	{"ContractDataEntry", func() any { return new(xdr.ContractDataEntry) }, 5},
	{"ConfigSettingEntry", func() any { return new(xdr.ConfigSettingEntry) }, 5},

	// DEX trade execution records. ClaimOfferAtom appears inside operation
	// results when offers cross; V0 is the legacy form with a raw ed25519 key.
	{"ClaimOfferAtom", func() any { return new(xdr.ClaimOfferAtom) }, 16},
	{"ClaimOfferAtomV0", func() any { return new(xdr.ClaimOfferAtomV0) }, 15},

	// A single ledger-state transition emitted in transaction meta.
	{"LedgerEntryChange", func() any { return new(xdr.LedgerEntryChange) }, 17},

	// Enum and union building blocks. Both decode from exactly 4 bytes,
	// so they must rank below Auth (priority 2) to keep pipe detection stable.
	{"ConfigSettingId", func() any { return new(xdr.ConfigSettingId) }, 1},
	{"OperationBody", func() any { return new(xdr.OperationBody) }, 1},
}

// TypeNames returns every registered XDR type name, sorted, for use in help
// text, shell completion and the --type flag.
func TypeNames() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	names := make([]string, 0, len(registry))
	for _, t := range registry {
		names = append(names, t.name)
	}
	sort.Strings(names)
	return names
}

// RegisterType dynamically registers a new XDR type in a thread-safe manner.
// It returns an error if name is empty or already registered.
func RegisterType(name string, ctor func() any, priority int) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("%w: type name must not be empty", ErrInvalidArgument)
	}
	if ctor == nil {
		return fmt.Errorf("%w: constructor must not be nil", ErrInvalidArgument)
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	for _, t := range registry {
		if t.name == name {
			return fmt.Errorf("%w: type %q is already registered", ErrInvalidArgument, name)
		}
	}
	registry = append(registry, xdrType{name: name, new: ctor, priority: priority})
	return nil
}

// registryEntries returns a snapshot of the current registry.
func registryEntries() []xdrType {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]xdrType, len(registry))
	copy(out, registry)
	return out
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
		return nil, fmt.Errorf("%w: %s produced an empty tree", ErrInvalidXDR, t.name)
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
			err = fmt.Errorf("%w: decoder panicked on malformed input: %v", ErrInvalidXDR, r)
		}
	}()
	if err := xdr.SafeUnmarshalBase64(payload, dest); err != nil {
		return fmt.Errorf("%w: xdr decode: %w", ErrInvalidXDR, err)
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
		return nil, ErrEmptyInput
	}
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidBase64, err)
	}

	var out []Candidate
	for _, t := range registryEntries() {
		if detectExcluded[t.name] {
			continue
		}
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
//     explains many bytes is a better fit than one that explains few;
//   - for exactly 32-byte (256-bit) payloads, an additional disambiguation
//     pass boosts AccountID when the first scalar looks like a G-address and
//     boosts Hash when the value renders as 64 lowercase hex characters,
//     reducing false positives when users paste raw keys or hashes.
func scoreCandidate(t xdrType, v *Value, rawLen int) int {
	score := t.priority * 1000
	score += countNodes(v.Node)
	// Very small payloads carry little evidence; damp the structural term so
	// priority decides, which keeps ambiguous 4-byte inputs stable.
	if rawLen <= 8 {
		score = t.priority * 1000
	}
	// 32-byte disambiguation: a G-address first scalar favours AccountID/
	// SignerKey; a 64-char lowercase hex scalar favours Hash types.
	if rawLen == 32 {
		if firstScalar := firstScalarValue(v.Node); firstScalar != "" {
			switch {
			case strings.HasPrefix(firstScalar, "G") && len(firstScalar) == 56:
				// Looks like a Stellar account address.
				if t.name == "SignerKey" {
					score += 500
				}
			case isLowercaseHex64(firstScalar):
				// Looks like a raw hash rendered as hex.
				if t.name == "Hash" {
					score += 500
				}
			}
		}
	}
	return score
}

// firstScalarValue returns the string representation of the first scalar node
// in the tree, or "" when there is none.
func firstScalarValue(n *Node) string {
	if n == nil {
		return ""
	}
	if n.Kind == KindScalar {
		return fmt.Sprintf("%v", n.Value)
	}
	for _, child := range n.Children {
		if s := firstScalarValue(child); s != "" {
			return s
		}
	}
	return ""
}

// isLowercaseHex64 reports whether s is exactly 64 lowercase hexadecimal
// characters, which is the canonical rendering of a 32-byte hash.
func isLowercaseHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
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
		return nil, ErrEmptyInput
	}
	for _, t := range registryEntries() {
		if strings.EqualFold(t.name, typeName) {
			if _, err := base64.StdEncoding.DecodeString(payload); err != nil {
				return nil, fmt.Errorf("%w: %w", ErrInvalidBase64, err)
			}
			v, err := decodeInto(t, payload)
			if err != nil {
				return nil, fmt.Errorf("input is not a valid %s: %w", t.name, err)
			}
			return v, nil
		}
	}
	return nil, fmt.Errorf("%w: %q (see `lens types` for the supported list)", ErrUnknownType, typeName)
}
