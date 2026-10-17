package lens

import (
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

func init() {
	nodeHooks[reflect.TypeOf(xdr.LedgerFootprint{})] = decorateLedgerFootprint
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

// A Soroban transaction declares the ledger entries it will touch in a
// LedgerFootprint: two flat lists of LedgerKey unions, ReadOnly and ReadWrite.
// The wire shape says nothing about storage level, and the only place a key's
// durability appears is deep inside its ContractData arm, so answering "what
// does this transaction read persistently?" means walking every key. The hook
// registered above appends derived children that group the same keys by
// storage level and name the contract each key belongs to; the wire fields are
// left exactly as decoded so machine output stays byte-faithful.

// Storage levels used by the derived footprint groups. "other" collects ledger
// keys that carry no Soroban durability at all — accounts, trust lines, offers,
// contract code, config settings and TTL records — so no footprint entry is
// dropped from the grouping.
const (
	footprintPersistent = "persistent"
	footprintTemporary  = "temporary"
	footprintInstance   = "instance"
	footprintOther      = "other"
)

// footprintStorageOrder fixes the order the groups are rendered in. It is part
// of the output contract: stable ordering keeps JSON output and diffs
// deterministic.
var footprintStorageOrder = []struct {
	level string
	label string
}{
	{level: footprintPersistent, label: "Persistent"},
	{level: footprintTemporary, label: "Temporary"},
	{level: footprintInstance, label: "Instance"},
	{level: footprintOther, label: "Other"},
}

// footprintEntry is one key placed in a storage group. Access and Index locate
// it in the undecorated ReadOnly/ReadWrite lists, which is why the derived
// groups reference entries instead of duplicating their (potentially large)
// key trees.
type footprintEntry struct {
	access string
	index  int
	key    xdr.LedgerKey
}

// footprintGroup is one storage bucket of a footprint. Groups with no entries
// are omitted, so a group node is never rendered empty.
type footprintGroup struct {
	name    string
	entries []footprintEntry
}

// decorateLedgerFootprint appends the storage groups to a footprint node. It
// is a node hook, so it only ever sees the concrete decoded value and never
// fails the surrounding decode.
func decorateLedgerFootprint(n *Node, v any) {
	fp, ok := v.(xdr.LedgerFootprint)
	if !ok {
		return
	}
	for _, group := range groupLedgerFootprint(fp) {
		list := &Node{Name: group.name, Kind: KindList, TypeName: "LedgerKey"}
		for _, entry := range group.entries {
			list.Children = append(list.Children, &Node{
				Kind:  KindScalar,
				Value: entry.access + "[" + strconv.Itoa(entry.index) + "]",
				Note:  footprintKeyNote(entry.key),
			})
		}
		n.Children = append(n.Children, list)
	}
}

// groupLedgerFootprint buckets every key by storage level, preserving the
// order the keys appeared in within each bucket. ReadOnly entries are visited
// before ReadWrite entries, which mirrors the wire order.
func groupLedgerFootprint(fp xdr.LedgerFootprint) []footprintGroup {
	buckets := map[string][]footprintEntry{}
	add := func(access string, keys []xdr.LedgerKey) {
		for i, key := range keys {
			level := footprintStorageLevel(key)
			buckets[level] = append(buckets[level], footprintEntry{access: access, index: i, key: key})
		}
	}
	add("readOnly", fp.ReadOnly)
	add("readWrite", fp.ReadWrite)

	groups := make([]footprintGroup, 0, len(footprintStorageOrder))
	for _, bucket := range footprintStorageOrder {
		entries := buckets[bucket.level]
		if len(entries) == 0 {
			continue
		}
		groups = append(groups, footprintGroup{name: bucket.label, entries: entries})
	}
	return groups
}

// footprintStorageLevel classifies one key. Contract data carries the
// durability explicitly, but instance storage is recorded as persistent and is
// only recognisable by its special ledger key, so that key splits instance
// entries out of the persistent bucket.
func footprintStorageLevel(key xdr.LedgerKey) string {
	if key.Type != xdr.LedgerEntryTypeContractData || key.ContractData == nil {
		return footprintOther
	}
	switch key.ContractData.Durability {
	case xdr.ContractDataDurabilityPersistent:
		if key.ContractData.Key.Type == xdr.ScValTypeScvLedgerKeyContractInstance {
			return footprintInstance
		}
		return footprintPersistent
	case xdr.ContractDataDurabilityTemporary:
		return footprintTemporary
	default:
		return footprintOther
	}
}

// footprintContractID returns the C... address a contract data key belongs to,
// or "" when the key carries no contract address. "Where available" is literal:
// account, trust line, code, config and TTL keys have no contract id, and a
// contract data key that names an account rather than a contract declines.
func footprintContractID(key xdr.LedgerKey) (id string) {
	if key.Type != xdr.LedgerEntryTypeContractData || key.ContractData == nil {
		return ""
	}
	contract := key.ContractData.Contract
	if contract.Type != xdr.ScAddressTypeScAddressTypeContract {
		return ""
	}
	// ScAddress.String dereferences its union arm, so a malformed key can
	// panic inside the SDK. The derived note is optional: losing it is better
	// than failing the decode.
	defer func() {
		if r := recover(); r != nil {
			id = ""
		}
	}()
	addr, err := contract.String()
	if err != nil {
		return ""
	}
	return addr
}

// footprintKeyNote labels one grouped entry with its key kind and, where the
// key carries one, the contract id it belongs to. Contract code keys have no
// contract id but are identified by their WASM hash, so that hash is shown
// instead of leaving the reference line anonymous.
func footprintKeyNote(key xdr.LedgerKey) string {
	kind := ledgerKeyKindName(key)
	if id := footprintContractID(key); id != "" {
		return kind + " for " + id
	}
	if key.Type == xdr.LedgerEntryTypeContractCode && key.ContractCode != nil {
		return kind + " " + key.ContractCode.Hash.HexString()
	}
	return kind
}

// ledgerKeyKindName renders a LedgerKey discriminant as lower-case words, the
// form used elsewhere in the tree ("contract data", "liquidity pool").
// Unknown discriminants, which the generated String renders as empty, fall back
// to their numeric value rather than producing an empty label.
func ledgerKeyKindName(key xdr.LedgerKey) string {
	name := strings.TrimPrefix(key.Type.String(), "LedgerEntryType")
	if name == "" {
		return "ledger entry type " + strconv.FormatInt(int64(key.Type), 10)
	}
	return strings.ReplaceAll(toSnake(name), "_", " ")
}
