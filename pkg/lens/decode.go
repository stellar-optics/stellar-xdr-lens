package lens

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"

	"github.com/stellar/go-stellar-sdk/xdr"
	xdr3 "github.com/stellar/go-xdr/xdr3"
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
	{"Operation", func() any { return new(xdr.Operation) }, 20},
	{"OperationResult", func() any { return new(xdr.OperationResult) }, 19},
	{"Memo", func() any { return new(xdr.Memo) }, 10},
	// Ledger upgrades are commonly ambiguous with StellarMessage and Price.
	{"LedgerUpgrade", func() any { return new(xdr.LedgerUpgrade) }, 38},
	{"Asset", func() any { return new(xdr.Asset) }, 9},
	{"Price", func() any { return new(xdr.Price) }, 8},
	{"SignerKey", func() any { return new(xdr.SignerKey) }, 7},
	{"ClaimableBalanceId", func() any { return new(xdr.ClaimableBalanceId) }, 6},
	{"ClaimableBalanceEntry", func() any { return new(xdr.ClaimableBalanceEntry) }, 5},
	{"Claimant", func() any { return new(xdr.Claimant) }, 5},
	{"ClaimPredicate", func() any { return new(xdr.ClaimPredicate) }, 5},
	{"AccountEntry", func() any { return new(xdr.AccountEntry) }, 5},
	{"TrustLineEntry", func() any { return new(xdr.TrustLineEntry) }, 5},
	{"TrustLineEntryExtensionV2", func() any { return new(xdr.TrustLineEntryExtensionV2) }, 5},
	{"OfferEntry", func() any { return new(xdr.OfferEntry) }, 5},
	{"DataEntry", func() any { return new(xdr.DataEntry) }, 5},
	{"LiquidityPoolEntry", func() any { return new(xdr.LiquidityPoolEntry) }, 5},
	{"LiquidityPoolConstantProductParameters", func() any { return new(xdr.LiquidityPoolConstantProductParameters) }, 5},
	{"ContractCodeEntry", func() any { return new(xdr.ContractCodeEntry) }, 5},
	{"ContractDataEntry", func() any { return new(xdr.ContractDataEntry) }, 5},
	{"ConfigSettingEntry", func() any { return new(xdr.ConfigSettingEntry) }, 5},
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

// DiagnosticStage identifies the decoding stage that failed.
type DiagnosticStage string

const (
	// StageBase64 means the payload failed while being base64-decoded.
	StageBase64 DiagnosticStage = "base64"
	// StageXDR means the payload was valid base64 but failed XDR decoding.
	StageXDR DiagnosticStage = "xdr"
)

// DiagnosticReason classifies a decode failure so callers can branch on it
// without parsing the message.
type DiagnosticReason string

const (
	// ReasonIllegalCharacter is a byte outside the base64 alphabet.
	ReasonIllegalCharacter DiagnosticReason = "illegal character"
	// ReasonPadding is malformed or missing base64 padding.
	ReasonPadding DiagnosticReason = "invalid padding"
	// ReasonTruncated means the input ended before the structure did.
	ReasonTruncated DiagnosticReason = "truncated payload"
	// ReasonTrailingBytes means decoding succeeded but bytes were left over.
	ReasonTrailingBytes DiagnosticReason = "trailing bytes"
	// ReasonMalformed covers structural XDR errors such as bad union tags.
	ReasonMalformed DiagnosticReason = "malformed structure"
)

// DecodeDiagnostic reports exactly where and why a payload failed to decode.
//
// Offset is a zero-based byte offset. For StageBase64 it indexes the
// normalised base64 text; for StageXDR it indexes the decoded binary payload.
// Total is the length of that stage's input, so Offset == Total means the
// input ran out at the end, which is the signature of a truncated payload.
//
// The type implements error and unwraps to both the stage sentinel
// (ErrInvalidBase64 or ErrInvalidXDR) and the underlying decoder error, so
// errors.Is and errors.As work as usual.
type DecodeDiagnostic struct {
	Stage    DiagnosticStage
	Reason   DiagnosticReason
	Offset   int
	Total    int
	TypeName string
	Err      error
}

// Error renders a compact, byte-accurate description of the failure.
func (d *DecodeDiagnostic) Error() string {
	var b strings.Builder
	if d.Stage == StageBase64 {
		b.WriteString("invalid base64")
	} else {
		b.WriteString("invalid XDR")
	}
	if d.TypeName != "" {
		b.WriteString(" (")
		b.WriteString(d.TypeName)
		b.WriteString(")")
	}
	fmt.Fprintf(&b, " at byte offset %d of %d: %s", d.Offset, d.Total, d.Reason)
	if d.Err != nil {
		b.WriteString(": ")
		b.WriteString(d.Err.Error())
	}
	return b.String()
}

// Unwrap exposes both the stage sentinel and the decoder error to errors.Is
// and errors.As.
func (d *DecodeDiagnostic) Unwrap() []error {
	sentinel := ErrInvalidXDR
	if d.Stage == StageBase64 {
		sentinel = ErrInvalidBase64
	}
	if d.Err == nil {
		return []error{sentinel}
	}
	return []error{sentinel, d.Err}
}

// decodeBase64 decodes payload, attaching a byte-accurate diagnostic to any
// failure. It distinguishes illegal characters from padding problems by
// inspecting the byte that broke the decoder.
func decodeBase64(payload string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err == nil {
		return raw, nil
	}

	diag := &DecodeDiagnostic{
		Stage:  StageBase64,
		Reason: ReasonPadding,
		Offset: 0,
		Total:  len(payload),
		Err:    err,
	}

	var corrupt base64.CorruptInputError
	if errors.As(err, &corrupt) {
		diag.Offset = int(corrupt)
		if diag.Offset < 0 {
			diag.Offset = 0
		}
		badByte := diag.Offset < len(payload) && !isBase64Byte(payload[diag.Offset])
		if badByte {
			diag.Reason = ReasonIllegalCharacter
		}
		// Otherwise the characters are legal but the group is incomplete or
		// mis-padded, which is what a truncated paste looks like.
	} else {
		diag.Reason = ReasonMalformed
	}
	return nil, diag
}

// isBase64Byte reports whether c belongs to the standard base64 alphabet or
// is padding. It distinguishes an illegal character from malformed padding.
func isBase64Byte(c byte) bool {
	switch {
	case c >= 'A' && c <= 'Z':
		return true
	case c >= 'a' && c <= 'z':
		return true
	case c >= '0' && c <= '9':
		return true
	case c == '+', c == '/', c == '=':
		return true
	default:
		return false
	}
}

// countingReader remembers where each Read started, which turns a decoder
// error into the byte offset of the primitive that failed. The XDR decoder
// reads one primitive at a time with io.ReadFull, so the start of the last
// read is the failure site; for a truncated payload it is the end of input.
type countingReader struct {
	r             io.Reader
	offset        int
	lastReadStart int
}

func (c *countingReader) Read(p []byte) (int, error) {
	c.lastReadStart = c.offset
	n, err := c.r.Read(p)
	c.offset += n
	return n, err
}

// unmarshalXDR decodes raw into dest, rejecting trailing bytes to preserve the
// strictness type detection depends on. Decoding hostile input is this tool's
// job, so a panic from a malformed nested length also becomes a diagnostic.
func unmarshalXDR(raw []byte, dest any, typeName string) (err error) {
	cr := &countingReader{r: bytes.NewReader(raw)}
	// The SDK hands MaxDepth straight to generated DecodeFrom methods, so it
	// must be explicit; zero is not defaulted on that path.
	opts := xdr.DecodeOptions{MaxInputLen: len(raw), MaxDepth: xdr3.DecodeDefaultMaxDepth}
	defer func() {
		if r := recover(); r != nil {
			err = &DecodeDiagnostic{
				Stage:    StageXDR,
				Reason:   ReasonMalformed,
				Offset:   cr.lastReadStart,
				Total:    len(raw),
				TypeName: typeName,
				Err:      fmt.Errorf("decoder panicked on malformed input: %v", r),
			}
		}
	}()

	_, decErr := xdr.UnmarshalWithOptions(cr, dest, opts)
	if decErr != nil {
		reason := ReasonMalformed
		offset := cr.lastReadStart
		if errors.Is(decErr, io.EOF) || errors.Is(decErr, io.ErrUnexpectedEOF) {
			reason = ReasonTruncated
			offset = len(raw)
		}
		return &DecodeDiagnostic{
			Stage:    StageXDR,
			Reason:   reason,
			Offset:   offset,
			Total:    len(raw),
			TypeName: typeName,
			Err:      decErr,
		}
	}
	if cr.offset != len(raw) {
		return &DecodeDiagnostic{
			Stage:    StageXDR,
			Reason:   ReasonTrailingBytes,
			Offset:   cr.offset,
			Total:    len(raw),
			TypeName: typeName,
			Err:      fmt.Errorf("input not fully consumed: expected %d bytes, decoded %d", len(raw), cr.offset),
		}
	}
	return nil
}

// decodeInto attempts to decode raw as the given registry entry. It returns a
// Value on success.
func decodeInto(t xdrType, raw []byte) (*Value, error) {
	dest := t.new()
	if err := unmarshalXDR(raw, dest, t.name); err != nil {
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

// noMatchError preserves ErrNoMatch while carrying the closest diagnostic so
// callers still learn where the payload went wrong.
type noMatchError struct {
	diagnostic *DecodeDiagnostic
}

func (e *noMatchError) Error() string {
	if e.diagnostic == nil {
		return ErrNoMatch.Error()
	}
	return fmt.Sprintf("%s: closest match %s", ErrNoMatch, e.diagnostic)
}

func (e *noMatchError) Unwrap() []error {
	if e.diagnostic == nil {
		return []error{ErrNoMatch}
	}
	return []error{ErrNoMatch, e.diagnostic}
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
	raw, err := decodeBase64(payload)
	if err != nil {
		return nil, err
	}

	var out []Candidate
	var closest *DecodeDiagnostic
	for _, t := range registry {
		v, decErr := decodeInto(t, raw)
		if decErr != nil {
			var diag *DecodeDiagnostic
			if errors.As(decErr, &diag) && (closest == nil || diag.Offset > closest.Offset) {
				closest = diag
			}
			continue
		}
		out = append(out, Candidate{
			Type:  t.name,
			Score: scoreCandidate(t, v, len(raw)),
			Value: v,
		})
	}
	if len(out) == 0 {
		return nil, &noMatchError{diagnostic: closest}
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
		return nil, ErrEmptyInput
	}
	raw, err := decodeBase64(payload)
	if err != nil {
		return nil, err
	}
	for _, t := range registry {
		if strings.EqualFold(t.name, typeName) {
			v, decErr := decodeInto(t, raw)
			if decErr != nil {
				return nil, decErr
			}
			return v, nil
		}
	}
	return nil, fmt.Errorf("%w: %q (see `lens types` for the supported list)", ErrUnknownType, typeName)
}

// ValidationResult describes a successful Validate call.
type ValidationResult struct {
	// Type is the XDR type the payload validated as.
	Type string
	// Bytes is the size of the decoded binary payload.
	Bytes int
}

// Validate checks that payload is syntactically valid XDR without building a
// Node tree, applying enrichers or formatting anything. It is the fast path
// behind `lens decode --validate-only`.
//
// When typeName is empty the payload is matched against the registry without
// materialising trees. The returned error is a *DecodeDiagnostic whenever the
// failure has a byte offset, so callers can report exactly where decoding
// stopped.
func Validate(payload, typeName string) (*ValidationResult, error) {
	payload = normalizeInput(payload)
	if payload == "" {
		return nil, ErrEmptyInput
	}
	raw, err := decodeBase64(payload)
	if err != nil {
		return nil, err
	}

	if typeName != "" {
		for _, t := range registry {
			if strings.EqualFold(t.name, typeName) {
				if err := unmarshalXDR(raw, t.new(), t.name); err != nil {
					return nil, err
				}
				return &ValidationResult{Type: t.name, Bytes: len(raw)}, nil
			}
		}
		return nil, fmt.Errorf("%w: %q (see `lens types` for the supported list)", ErrUnknownType, typeName)
	}

	var closest *DecodeDiagnostic
	for _, t := range registry {
		if err := unmarshalXDR(raw, t.new(), t.name); err != nil {
			var diag *DecodeDiagnostic
			if errors.As(err, &diag) && (closest == nil || diag.Offset > closest.Offset) {
				closest = diag
			}
			continue
		}
		return &ValidationResult{Type: t.name, Bytes: len(raw)}, nil
	}
	return nil, &noMatchError{diagnostic: closest}
}
