package lens

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/stellar/go-stellar-sdk/amount"
	"github.com/stellar/go-stellar-sdk/xdr"
)

// Enricher turns a decoded XDR value into a human-friendly rendering.
//
// It receives the reflected value and returns the display string plus the
// underlying raw value to retain in machine-readable output. Returning
// ok=false leaves the value to the generic tree walker.
//
// Enrichers are the primary extension point of this package. To teach the
// tool to render a new XDR type nicely, write an Enricher and register it
// with registerEnricher — nothing else needs to change, and every output
// format picks it up at once. See docs/architecture.md.
type Enricher func(rv reflect.Value) (display string, raw any, ok bool)

// enricherFor maps a concrete XDR type to its Enricher. It is a
// declaration-time literal so every core entry exists before any init runs,
// regardless of file order; feature files register their own enrichers from
// an init function. It is read-only after package initialization, so it needs
// no synchronisation.
var enricherFor = map[reflect.Type]Enricher{
	reflect.TypeOf(xdr.AccountId{}):        enrichAccountID,
	reflect.TypeOf(xdr.MuxedAccount{}):     enrichMuxedAccount,
	reflect.TypeOf(xdr.Asset{}):            enrichAsset,
	reflect.TypeOf(xdr.ChangeTrustAsset{}): enrichChangeTrustAsset,
	reflect.TypeOf(xdr.TrustLineAsset{}):   enrichTrustLineAsset,
	reflect.TypeOf(xdr.TimePoint(0)):       enrichTimePoint,
	reflect.TypeOf(xdr.Duration(0)):        enrichDuration,
	reflect.TypeOf(xdr.Hash{}):             enrichHash,
	reflect.TypeOf(xdr.PoolId{}):           enrichPoolID,
	reflect.TypeOf(xdr.Signature{}):        enrichSignature,
}

func registerEnricher(t reflect.Type, fn Enricher) {
	enricherFor[t] = fn
}

// enrich looks up and applies the Enricher registered for rv's type.
//
// Generated XDR accessors dereference union arms without checking the
// discriminant, so they panic on a zero or malformed value. Since this tool
// is expected to decode untrusted, possibly truncated blobs, every enricher
// runs behind a recover: a value we cannot prettify falls back to the generic
// rendering instead of taking the process down.
func enrich(rv reflect.Value) (display string, raw any, ok bool) {
	fn, found := enricherFor[rv.Type()]
	if !found {
		return "", nil, false
	}
	defer func() {
		if r := recover(); r != nil {
			display, raw, ok = "", nil, false
		}
	}()
	return fn(rv)
}

func enrichAccountID(rv reflect.Value) (string, any, bool) {
	aid, valid := rv.Interface().(xdr.AccountId)
	if !valid {
		return "", nil, false
	}
	addr, err := aid.GetAddress()
	if err != nil || addr == "" {
		return "", nil, false
	}
	return "", addr, true
}

func enrichMuxedAccount(rv reflect.Value) (string, any, bool) {
	ma, valid := rv.Interface().(xdr.MuxedAccount)
	if !valid {
		return "", nil, false
	}
	// Address has a pointer receiver and a reflected struct field is not
	// addressable, so operate on a copy we own.
	addr, err := (&ma).GetAddress()
	if err != nil || addr == "" {
		return "", nil, false
	}
	// A multiplexed account also carries a subaccount id worth surfacing.
	if ma.Type == xdr.CryptoKeyTypeKeyTypeMuxedEd25519 {
		if id, idErr := (&ma).GetId(); idErr == nil {
			return "muxed id " + strconv.FormatUint(id, 10), addr, true
		}
	}
	return "", addr, true
}

func enrichAsset(rv reflect.Value) (string, any, bool) {
	a, valid := rv.Interface().(xdr.Asset)
	if !valid {
		return "", nil, false
	}
	return "", a.StringCanonical(), true
}

func enrichChangeTrustAsset(rv reflect.Value) (string, any, bool) {
	a, valid := rv.Interface().(xdr.ChangeTrustAsset)
	if !valid {
		return "", nil, false
	}
	// Liquidity pool shares are a distinct arm with no canonical string.
	if a.Type == xdr.AssetTypeAssetTypePoolShare {
		return "", nil, false
	}
	return "", a.ToAsset().StringCanonical(), true
}

func enrichTrustLineAsset(rv reflect.Value) (string, any, bool) {
	a, valid := rv.Interface().(xdr.TrustLineAsset)
	if !valid {
		return "", nil, false
	}
	if a.Type == xdr.AssetTypeAssetTypePoolShare {
		return "", nil, false
	}
	return "", a.ToAsset().StringCanonical(), true
}

func enrichTimePoint(rv reflect.Value) (string, any, bool) {
	tp, valid := rv.Interface().(xdr.TimePoint)
	if !valid {
		return "", nil, false
	}
	if tp == 0 {
		return "", uint64(0), true
	}
	return time.Unix(int64(tp), 0).UTC().Format(time.RFC3339), uint64(tp), true
}

func enrichDuration(rv reflect.Value) (string, any, bool) {
	d, valid := rv.Interface().(xdr.Duration)
	if !valid {
		return "", nil, false
	}
	if d == 0 {
		return "", uint64(0), true
	}
	return (time.Duration(d) * time.Second).String(), uint64(d), true
}

// amountFields are the field names whose payload is a stroop amount. XDR
// models amounts, sequence numbers and offer ids with the same integer types,
// so the field name is the only available signal. Anything not listed here is
// left as a plain integer, which is the safe default: showing a sequence
// number as "12345.0000000" would be worse than showing it raw. Feature files
// may register additional fields during init.
var amountFields = map[string]bool{
	"Amount":                          true,
	"StartingBalance":                 true,
	"Limit":                           true,
	"SendAmount":                      true,
	"SendMax":                         true,
	"DestAmount":                      true,
	"DestMin":                         true,
	"BuyAmount":                       true,
	"MaxAmountA":                      true,
	"MaxAmountB":                      true,
	"MinAmountA":                      true,
	"MinAmountB":                      true,
	"Balance":                         true,
	"FeeCharged":                      true,
	"FeeRatePerInstructionsIncrement": true,
	"FeeDiskReadLedgerEntry":          true,
	"FeeWriteLedgerEntry":             true,
	"FeeDiskRead1Kb":                  true,
	"RentFee1KbSorobanStateSizeLow":   true,
	"RentFee1KbSorobanStateSizeHigh":  true,
	"FeeHistorical1Kb":                true,
	"FeeContractEvents1Kb":            true,
	"FeeTxSize1Kb":                    true,
	"FeeWrite1Kb":                     true,
	"ClaimableBalance":                true,
	"ReserveA":                        true,
	"ReserveB":                        true,
	"TotalPoolShares":                 true,
	"Buying":                          true,
	"Selling":                         true,
	"NewBaseFee":                      true,
	"NewBaseReserve":                  true,
}

// enrichAmountField applies stroop formatting when a field is known to carry
// an amount. It is applied by the walker after the type-based registry, since
// it depends on the field name rather than the type alone.
//
// Amounts reach the walker as Int64 on ledger entries and as Uint32 on the
// protocol parameters that denominate them (the base fee and base reserve),
// and optional union arms arrive pointer-wrapped, so both shapes are accepted.
func enrichAmountField(fieldName string, rv reflect.Value) (string, bool) {
	if !amountFields[fieldName] {
		return "", false
	}
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return "", false
		}
		rv = rv.Elem()
	}
	var stroops int64
	switch v := rv.Interface().(type) {
	case xdr.Int64:
		stroops = int64(v)
	case xdr.Uint32:
		stroops = int64(v)
	default:
		return "", false
	}
	return amount.String(xdr.Int64(stroops)) + " (stroops: " + strconv.FormatInt(stroops, 10) + ")", true
}

// fieldNotes maps a field name to a function that produces an additional
// human-friendly rendering for it. It is the field-name counterpart of the
// type-based enricher registry, for cases where the type alone is not enough:
// a protocol version and a ledger count are both plain Uint32, and only the
// field they sit in says which is which. Feature files register their notes
// during init; the map is read-only after package initialization.
var fieldNotes = map[string]func(reflect.Value) (string, bool){}

// enrichField applies the field-name-based renderings: stroop amounts first,
// then any note registered in fieldNotes. It returns ok=false when the field
// has no special rendering, leaving the generic walker output untouched.
// A note function that panics on a malformed value degrades to no note rather
// than taking the process down.
func enrichField(fieldName string, rv reflect.Value) (note string, ok bool) {
	if note, ok := enrichAmountField(fieldName, rv); ok {
		return note, true
	}
	fn, found := fieldNotes[fieldName]
	if !found {
		return "", false
	}
	defer func() {
		if r := recover(); r != nil {
			note, ok = "", false
		}
	}()
	return fn(rv)
}

// fieldNoteFn renders an extra Note for a struct field. Returning ok=false
// leaves the field untouched.
type fieldNoteFn func(rv reflect.Value) (note string, ok bool)

// fieldNotesByType maps a containing struct type to per-field note renderers. Like
// amountFields it exists because XDR encodes semantically distinct concepts
// with the same scalar type: TrustLineEntry.Flags and OfferEntry.Flags are
// both xdr.Uint32 but only the former is a trust line flag set, so the
// containing type is needed as well as the field name.
var fieldNotesByType = map[reflect.Type]map[string]fieldNoteFn{}

// registerFieldNote registers fn for one field of one struct type. Domain
// files call it from init; see registerEnricher for why this is ordering-safe.
func registerFieldNote(parent reflect.Type, field string, fn fieldNoteFn) {
	if fieldNotesByType[parent] == nil {
		fieldNotesByType[parent] = map[string]fieldNoteFn{}
	}
	fieldNotesByType[parent][field] = fn
}

// noteForField applies a registered field note, if any. A missing entry is
// not an error: most fields have no note.
func noteForField(parent reflect.Type, field string, rv reflect.Value) (string, bool) {
	fn, found := fieldNotesByType[parent][field]
	if !found {
		return "", false
	}
	return fn(rv)
}

// isEnumType reports whether t is a generated XDR enum. Enums carry a
// ValidEnum method; plain numeric aliases such as Int64 and Uint32 do not,
// which keeps ordinary numbers from being rendered as constant names.
func isEnumType(t reflect.Type) bool {
	_, ok := t.MethodByName("ValidEnum")
	return ok
}

// operationTypeLabel converts an XDR operation type constant into the
// snake_case name used by Horizon and the Stellar docs, which is what users
// recognise. Falls back to the raw constant name for unknown types.
func operationTypeLabel(t xdr.OperationType) string {
	name := t.String()
	name = strings.TrimPrefix(name, "OperationType")
	return toSnake(name)
}

func toSnake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(r + ('a' - 'A'))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func enrichHash(rv reflect.Value) (string, any, bool) {
	h, valid := rv.Interface().(xdr.Hash)
	if !valid {
		return "", nil, false
	}
	return h.HexString(), h, true
}

func enrichPoolID(rv reflect.Value) (string, any, bool) {
	p, valid := rv.Interface().(xdr.PoolId)
	if !valid {
		return "", nil, false
	}
	return hex.EncodeToString(p[:]), p, true
}

func enrichSignature(rv reflect.Value) (string, any, bool) {
	s, valid := rv.Interface().(xdr.Signature)
	if !valid {
		return "", nil, false
	}
	return hex.EncodeToString(s), s, true
}

// A FeeBumpTransaction is somebody else's transaction with a second fee payer
// bolted on: the fee source is a different account from the inner
// transaction's source, the outer envelope carries its own signatures over the
// wrapper, and the inner transaction carries its own signatures over the
// inner hash. None of the three facts below is an XDR field, and all three are
// what a developer holding a fee-bump envelope actually asks for:
//
//   - who paid — the fee source key's 4-byte signature hint, derived from
//     the same ed25519 key the strkey encodes. Signature hints are the index
//     stellar-core matches DecoratedSignatures by, so putting the key's hint
//     next to its strkey makes a mismatch visible at a glance;
//   - which outer signatures belong to that key — each envelope signature
//     hint checked against the fee source key hint. A signature that does not
//     match is another signer of the fee source account, not a wrong
//     signature, so the note says which rather than judging;
//   - the inner transaction hash — the inner transaction is signed
//     separately from the wrapper and its digest appears nowhere in the
//     envelope.
//
// Both hooks append derived children and leave every wire field exactly as
// decoded, so machine-readable output stays byte-faithful. Everything is
// computed from the decoded value alone: no network, no passphrase, no clock.

func init() {
	nodeHooks[reflect.TypeOf(xdr.FeeBumpTransaction{})] = decorateFeeBumpTransaction
	nodeHooks[reflect.TypeOf(xdr.FeeBumpTransactionEnvelope{})] = decorateFeeBumpEnvelope
}

// muxedAccountHint returns the 4-byte signature hint behind a muxed account:
// the last four bytes of its ed25519 key, whichever arm carries it. ok is
// false when the account holds no key (an unset or unknown union arm), in
// which case there is no hint to match signatures against.
//
// The GetEd25519 accessor cannot be used here: KEY_TYPE_ED25519 is the union's
// zero discriminant, so a zero MuxedAccount claims the ed25519 arm with a nil
// pointer and the accessor dereferences it. Reading the arms directly keeps a
// malformed value on the declining path.
func muxedAccountHint(ma xdr.MuxedAccount) (hint xdr.SignatureHint, ok bool) {
	switch {
	case ma.Type == xdr.CryptoKeyTypeKeyTypeEd25519 && ma.Ed25519 != nil:
		return hintFromKey(*ma.Ed25519), true
	case ma.Type == xdr.CryptoKeyTypeKeyTypeMuxedEd25519 && ma.Med25519 != nil:
		return hintFromKey(ma.Med25519.Ed25519), true
	default:
		return xdr.SignatureHint{}, false
	}
}

// hintFromKey takes the last four bytes of a 32-byte key, exactly what
// SignerHint does for signer-key unions and what stellar-core puts in a
// DecoratedSignature.Hint.
func hintFromKey(raw xdr.Uint256) xdr.SignatureHint {
	return xdr.SignatureHint{raw[28], raw[29], raw[30], raw[31]}
}

// feeSourceHintNote labels the derived fee-source hint with the strkey it
// came from, which is what puts the 4-byte hint "alongside the public key
// strkey" rather than floating unattached in the tree.
func feeSourceHintNote(ma xdr.MuxedAccount) string {
	if addr := addressOf(ma); addr != "" {
		return "4-byte key hint of fee source " + shortAddr(addr)
	}
	return "4-byte key hint of the fee source key"
}

// innerTransactionDigest hashes the inner transaction's signature preimage
// without the network id: the four bytes of ENVELOPE_TYPE_TX followed by the
// inner Transaction's XDR.
//
// The hash the inner signatures are actually computed over prefixes that
// preimage with the raw 32-byte network id, and the offline decode path has
// no passphrase to derive it from. Rather than invent one, the node's note
// states what the digest is built from, so it can be used as a byte-exact
// fingerprint of the inner transaction without being mistaken for the
// explorer's transaction hash. ok is false when the union arm is absent or
// not a v1 envelope, so a malformed value simply gains no child.
func innerTransactionDigest(inner xdr.FeeBumpTransactionInnerTx) (digest string, ok bool) {
	// The generated GetV1 accessor dereferences the arm without checking it,
	// so test the pointer rather than reaching for the accessor.
	if inner.Type != xdr.EnvelopeTypeEnvelopeTypeTx || inner.V1 == nil {
		return "", false
	}
	txBytes, err := inner.V1.Tx.MarshalBinary()
	if err != nil {
		return "", false
	}
	preimage := make([]byte, 4+len(txBytes))
	binary.BigEndian.PutUint32(preimage, uint32(xdr.EnvelopeTypeEnvelopeTypeTx))
	copy(preimage[4:], txBytes)
	sum := sha256.Sum256(preimage)
	return hex.EncodeToString(sum[:]), true
}

// decorateFeeBumpTransaction appends the fee-source key hint and the inner
// transaction digest to a fee-bump transaction node. It runs for a
// FeeBumpTransaction decoded on its own (--type FeeBumpTransaction) as well
// as one nested in an envelope, so neither case loses the derived facts.
func decorateFeeBumpTransaction(n *Node, v any) {
	fb, ok := v.(xdr.FeeBumpTransaction)
	if !ok {
		return
	}
	if hint, found := muxedAccountHint(fb.FeeSource); found {
		n.Children = append(n.Children, &Node{
			Name:     "FeeSourceHint",
			Kind:     KindScalar,
			TypeName: "SignatureHint",
			Value:    hex.EncodeToString(hint[:]),
			Note:     feeSourceHintNote(fb.FeeSource),
		})
	}
	if digest, found := innerTransactionDigest(fb.InnerTx); found {
		n.Children = append(n.Children, &Node{
			Name:     "InnerTxHash",
			Kind:     KindScalar,
			TypeName: "Hash",
			Value:    digest,
			Note:     "sha256 of 00000002 (ENVELOPE_TYPE_TX) followed by the inner tx XDR; the signed inner hash prefixes the 32-byte network id",
		})
	}
}

// decorateFeeBumpEnvelope appends the outer fee-bump signatures' hints as a
// derived group, each one checked against the fee source key hint. The
// envelope's wire fields — Tx and Signatures — are left untouched, so the
// group mirrors rather than replaces them.
func decorateFeeBumpEnvelope(n *Node, v any) {
	env, ok := v.(xdr.FeeBumpTransactionEnvelope)
	if !ok || len(env.Signatures) == 0 {
		return
	}
	// Turning the fee source into a strkey dereferences the union arm, and a
	// malformed value can claim a key it does not carry, so only derive the
	// label once the arm has been confirmed present.
	feeHint, haveFeeHint := muxedAccountHint(env.Tx.FeeSource)
	feeSource := "fee source"
	if haveFeeHint {
		if addr := addressOf(env.Tx.FeeSource); addr != "" {
			feeSource = shortAddr(addr)
		}
	}
	list := &Node{Name: "SignatureHints", Kind: KindList, TypeName: "DecoratedSignature"}
	for _, sig := range env.Signatures {
		note := ""
		switch {
		case !haveFeeHint:
			note = "fee source carries no key; hint not matched"
		case sig.Hint == feeHint:
			note = "matches " + feeSource + " key hint"
		default:
			note = "does not match " + feeSource + " key hint; another fee-source signer"
		}
		list.Children = append(list.Children, &Node{
			Kind:  KindScalar,
			Value: hex.EncodeToString(sig.Hint[:]),
			Note:  note,
		})
	}
	n.Children = append(n.Children, list)
}
