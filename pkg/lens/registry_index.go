package lens

import (
	"reflect"
	"sort"
)

// Categories group the registry by what a type is for. They are stable
// strings suitable for filtering the index.
const (
	// CategoryTransaction covers envelopes, results and their inner pieces.
	CategoryTransaction = "transaction"
	// CategoryLedger covers ledger-level and bucket-list types.
	CategoryLedger = "ledger"
	// CategoryHistory covers the history-archive transaction and result sets.
	CategoryHistory = "history"
	// CategoryOverlay covers peer-to-peer messages exchanged by Core nodes.
	CategoryOverlay = "overlay"
	// CategoryConsensus covers SCP quorum sets, statements and envelopes.
	CategoryConsensus = "consensus"
	// CategorySoroban covers Soroban host, contract and authorization types.
	CategorySoroban = "soroban"
	// CategoryStateArchival covers TTL and eviction types.
	CategoryStateArchival = "state-archival"
	// CategoryLedgerEntry covers the individual live ledger-entry types.
	CategoryLedgerEntry = "ledger-entry"
	// CategoryPrimitive covers small building blocks that decode from very
	// few bytes and are frequently ambiguous.
	CategoryPrimitive = "primitive"
)

// TypeInfo describes one registered XDR type for introspection.
type TypeInfo struct {
	// Name is the registered type name, as accepted by DecodeAs and --type.
	Name string `json:"name"`
	// Category groups the type by purpose; one of the Category* constants.
	Category string `json:"category"`
	// Description is a one-line, curated explanation of what the type is.
	Description string `json:"description"`
	// Priority is the detection ranking described in decode.go. Higher wins.
	Priority int `json:"priority"`
	// MinWireSize is the smallest number of bytes any valid value of the
	// type can encode to, computed structurally. For unions it accounts for
	// void arms, so a type with an empty arm reports its discriminant size.
	// It is a lower bound, not the size of a particular value.
	MinWireSize int `json:"minWireSize"`
}

// RegistryIndex returns an introspection entry for every registered XDR type,
// sorted by name. The index is derived from the live registry, so it can never
// list a type that DecodeAs rejects, and TestRegistryIndexCoversRegistry keeps
// the descriptions exhaustive.
//
// The size estimate is computed from the Go types rather than from a value,
// which keeps the index usable without decoding anything and keeps the
// offline-first property: no fixtures or network access are involved.
func RegistryIndex() []TypeInfo {
	registryMu.RLock()
	defer registryMu.RUnlock()
	infos := make([]TypeInfo, 0, len(registry))
	for _, t := range registry {
		doc := typeDocs[t.name]
		infos = append(infos, TypeInfo{
			Name:        t.name,
			Category:    doc.category,
			Description: doc.description,
			Priority:    t.priority,
			MinWireSize: minWireSizeOf(t),
		})
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })
	return infos
}

// TypesByCategory returns all registered types in the given category, sorted by
// name. An empty category returns an empty slice (not all types). Category
// strings are the Category* constants defined in this file.
func TypesByCategory(category string) []TypeInfo {
	all := RegistryIndex()
	out := make([]TypeInfo, 0, len(all))
	for _, info := range all {
		if info.Category == category {
			out = append(out, info)
		}
	}
	return out
}

// typeDoc is the curated documentation attached to a registered type.
type typeDoc struct {
	category    string
	description string
}

// typeDocs maps every registered type to its category and description. One
// entry per registry line; tests fail when the two drift apart.
var typeDocs = map[string]typeDoc{
	"AccountEntry": {CategoryLedgerEntry,
		"A funded account: balance, sequence number, signers, thresholds and home domain."},
	"Asset": {CategoryPrimitive,
		"A classic asset: native lumens, or a credit asset identified by code and issuer."},
	"Auth": {CategoryOverlay,
		"A peer's response to a connection request, carrying its authentication flags."},
	"AuthCert": {CategoryOverlay,
		"A short-lived certificate proving a peer holds the key behind its node id."},
	"BucketEntry": {CategoryLedger,
		"One record in a bucket: a live ledger entry, a dead tombstone, or bucket metadata."},
	"BucketMetadata": {CategoryLedger,
		"Metadata describing a bucket's ledger range and protocol version."},
	"ClaimableBalanceEntry": {CategoryLedgerEntry,
		"A balance claimable by named claimants, with its predicate and asset."},
	"ClaimableBalanceId": {CategoryPrimitive,
		"The identifier of a claimable balance, one variant per claim predicate shape."},
	"Claimant": {CategoryLedgerEntry,
		"A party eligible to claim a balance when its predicate is satisfied."},
	"ClaimPredicate": {CategoryLedgerEntry,
		"A recursive boolean and time condition that controls when a claimable balance can be claimed."},
	"ContractCodeEntry": {CategoryLedgerEntry,
		"The WASM code of a deployed Soroban contract, with the hash it is keyed by."},
	"ContractDataEntry": {CategoryLedgerEntry,
		"One Soroban contract data entry: its key, value and storage durability."},
	"ConfigSettingEntry": {CategoryLedgerEntry,
		"A Soroban config setting entry: its ID, fee/rent parameters, and storage durability."},
	"ContractEvent": {CategorySoroban,
		"An event emitted by a Soroban contract."},
	"DataEntry": {CategoryLedgerEntry,
		"A key/value data record attached to an account."},
	"DiagnosticEvent": {CategorySoroban,
		"A diagnostic event emitted during Soroban host execution."},
	"DontHave": {CategoryOverlay,
		"A peer's reply that it does not hold a requested transaction set or quorum set."},
	"Error": {CategoryOverlay,
		"An overlay error message: a code and a human-readable description."},
	"EvictionIterator": {CategoryStateArchival,
		"The cursor validators use to walk the bucket list for expired entries."},
	"FeeBumpTransaction": {CategoryTransaction,
		"A transaction that pays the fee for an inner transaction."},
	"GeneralizedTransactionSet": {CategoryHistory,
		"The versioned transaction set introduced in protocol 19; v1 splits transactions into phases."},
	"Hello": {CategoryOverlay,
		"A peer's introduction: version, network id, listening port, identity and certificate."},
	"HotArchiveBucketEntry": {CategoryLedger,
		"A record in the hot archive bucket: an archived entry or its live successor."},
	"LedgerCloseMeta": {CategoryLedger,
		"Everything that happened in one ledger close: header, transactions, results and meta."},
	"LedgerCloseMetaBatch": {CategoryHistory,
		"Close metadata for a contiguous run of ledgers, as exported for analytics."},
	"LedgerUpgrade": {CategoryConsensus,
		"A protocol upgrade vote carrying a new ledger version, fee, reserve, or configuration."},
	"LedgerEntry": {CategoryLedger,
		"One entry in the live ledger state, wrapping the type-specific entry payload."},
	"LedgerHeader": {CategoryLedger,
		"The consensus header of a ledger: sequence, hashes, totals and protocol parameters."},
	"LedgerHeaderHistoryEntry": {CategoryLedger,
		"A ledger header plus the hash and metadata that link it to its predecessor."},
	"LedgerBounds": {CategoryPrimitive,
		"Ledger sequence bounds for a transaction precondition: min and max ledger numbers."},
	"LedgerKey": {CategoryLedger,
		"The key identifying a ledger entry, one variant per entry type."},
	"LedgerKeyTtl": {CategoryStateArchival,
		"The ledger key identifying a Soroban TTL record."},
	"LiquidityPoolConstantProductParameters": {CategoryLedgerEntry,
		"The asset pair and trading fee configuration of a constant product liquidity pool."},
	"LiquidityPoolEntry": {CategoryLedgerEntry,
		"An automated market maker liquidity pool and its reserves."},
	"Memo": {CategoryPrimitive,
		"The memo attached to a transaction: text, id, hash or nothing."},
	"OfferEntry": {CategoryLedgerEntry,
		"An open offer on the order book."},
	"Operation": {CategoryTransaction,
		"A single operation with its source account and type-specific body."},
	"OperationResult": {CategoryTransaction,
		"The result of one operation, including its type-specific result code."},
	"PeerAddress": {CategoryOverlay,
		"One peer address advertised in a Peers message, with its failure count."},
	"Price": {CategoryPrimitive,
		"A rational price as a numerator and denominator."},
	"ScpBallot": {CategoryConsensus,
		"An SCP ballot: a round counter and the opaque value the validator is voting for."},
	"ScpEnvelope": {CategoryConsensus,
		"An SCP statement together with the signature that authenticates it."},
	"ScpNomination": {CategoryConsensus,
		"An SCP nomination: the quorum-set hash and the lists of voted and accepted values."},
	"ScpQuorumSet": {CategoryConsensus,
		"The validators and nested sets a validator trusts, with their threshold."},
	"ScpStatement": {CategoryConsensus,
		"A validator's prepare, confirm, externalize or nominate statement for one slot."},
	"ScSpecEntry": {CategorySoroban,
		"One entry of a contract's embedded specification: function, struct, union, enum, error or event."},
	"ScVal": {CategorySoroban,
		"A Soroban contract value in its wire form."},
	"SignedTimeSlicedSurveyRequestMessage": {CategoryOverlay,
		"A surveyor's signed request for a window of a peer's network topology."},
	"SignedTimeSlicedSurveyResponseMessage": {CategoryOverlay,
		"A signed survey response carrying the peer's encrypted topology report."},
	"SignedTimeSlicedSurveyStartCollectingMessage": {CategoryOverlay,
		"A surveyor's signed command to start collecting time-sliced statistics."},
	"SignedTimeSlicedSurveyStopCollectingMessage": {CategoryOverlay,
		"A surveyor's signed command to stop collecting time-sliced statistics."},
	"Signer": {CategoryPrimitive,
		"An account co-signer: a key (ed25519, pre-auth or hash-x) paired with its signing weight."},
	"SignerKey": {CategoryPrimitive,
		"A signer: an ed25519, pre-auth transaction, hash-x or signed-payload key."},
	"SorobanAuthorizationEntry": {CategorySoroban,
		"A Soroban authorization tree entry proving a contract call was authorized."},
	"SorobanTransactionData": {CategorySoroban,
		"The Soroban transaction extension: resources, ledger footprint and refundable fee."},
	"StellarMessage": {CategoryOverlay,
		"The outer envelope of every peer-to-peer message exchanged by Core nodes."},
	"StellarValue": {CategoryConsensus,
		"A validator's consensus value for a ledger close, including protocol upgrade votes."},
	"Transaction": {CategoryTransaction,
		"A v1 transaction: source, sequence, fee, preconditions, memo and operations."},
	"TransactionEnvelope": {CategoryTransaction,
		"A signed transaction in one of its wire forms: legacy v0, v1, or fee-bump."},
	"TransactionHistoryEntry": {CategoryHistory,
		"One ledger's transaction set as stored in the history archive, including the generalized form."},
	"TransactionHistoryResultEntry": {CategoryHistory,
		"One ledger's transaction results as stored in the history archive."},
	"TransactionMeta": {CategoryTransaction,
		"The ledger-entry changes produced by a transaction."},
	"TransactionResult": {CategoryTransaction,
		"The result of applying a transaction, including each operation's result."},
	"TransactionResultPair": {CategoryTransaction,
		"A transaction hash paired with its result, as stored in a result set."},
	"TransactionSet": {CategoryHistory,
		"The legacy v0 transaction set: the previous ledger hash and a flat list of envelopes."},
	"TransactionSetV1": {CategoryHistory,
		"A generalized transaction set: the previous ledger hash and one or more transaction phases."},
	"TransactionV0": {CategoryTransaction,
		"The pre-protocol-13 transaction form, with a raw ed25519 source and time bounds."},
	"TrustLineEntry": {CategoryLedgerEntry,
		"A trustline: an account's balance and limits for one asset."},
	"TrustLineEntryExtensionV2": {CategoryLedgerEntry,
		"The V2 trustline extension, including its liquidity pool use count."},
	"TimeBounds": {CategoryPrimitive,
		"Unix-timestamp bounds for a transaction precondition: min and max time as seconds since epoch."},
	"TtlEntry": {CategoryStateArchival,
		"The time-to-live record for a Soroban ledger entry."},
	"ClaimOfferAtom": {CategoryTransaction,
		"A DEX trade execution record: the offer filled, seller, assets exchanged, and amounts."},
	"ClaimOfferAtomV0": {CategoryTransaction,
		"The legacy v0 form of a DEX trade execution record, using a raw ed25519 seller key."},
	"ConfigSettingId": {CategoryPrimitive,
		"A Soroban network configuration parameter identifier, naming one of the protocol's tunable settings."},
	"LedgerEntryChange": {CategoryLedger,
		"A single ledger-state transition emitted in transaction meta: created, updated, removed or restored."},
	"LedgerEntryExtensionV1": {CategoryLedger,
		"The protocol-14 ledger entry extension carrying the sponsor's account ID."},
	"OperationBody": {CategoryTransaction,
		"The type-discriminated body of an operation, without its source account wrapper."},
}

// minWireSizeOf estimates the smallest encoded size of the type behind a
// registry entry by inspecting the Go type recursively.
func minWireSizeOf(t xdrType) (size int) {
	defer func() {
		// A structural edge case (an anonymous union without the generated
		// helpers, say) must not take RegistryIndex down; an unknown size is
		// a better answer than a panic.
		if r := recover(); r != nil {
			size = 0
		}
	}()
	v := t.new()
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return 0
	}
	return minWireSize(rv.Type().Elem())
}

// maxWireSizeDepth bounds the structural walk. XDR types can nest by value
// through their arms (ScSpecTypeDef contains itself, for example), so a depth
// cap is required; values below the cap are still correct lower bounds.
const maxWireSizeDepth = 24

// minWireSize returns the smallest number of bytes a valid value of t can
// encode to. It is a conservative lower bound: empty strings and slices cost
// their length prefix, optional pointers cost only their presence flag, and a
// union pays for its discriminant plus its smallest arm (zero for a void arm).
func minWireSize(t reflect.Type) int {
	return minWireSizeAt(t, 0)
}

func minWireSizeAt(t reflect.Type, depth int) int {
	if t == nil || depth > maxWireSizeDepth {
		return 0
	}
	if isEnumType(t) {
		return 4
	}
	switch t.Kind() {
	case reflect.Bool,
		reflect.Int8, reflect.Int16, reflect.Int32,
		reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return 4
	case reflect.Int64, reflect.Uint64:
		return 8
	case reflect.String, reflect.Slice, reflect.Interface:
		return 4
	case reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 {
			// Fixed opaque: exactly one byte per element, not one word.
			return t.Len()
		}
		return t.Len() * minWireSizeAt(t.Elem(), depth+1)
	case reflect.Pointer:
		// Optional value: a presence flag plus the value when present.
		return 4 + minWireSizeAt(t.Elem(), depth+1)
	case reflect.Struct:
		if hasUnionMethods(t) {
			return 4 + minUnionArmSize(t, depth+1)
		}
		total := 0
		for i := range t.NumField() {
			f := t.Field(i)
			if f.PkgPath != "" {
				continue
			}
			total += minWireSizeAt(f.Type, depth+1)
		}
		return total
	default:
		return 4
	}
}

// hasUnionMethods reports whether t was generated as an XDR union. The
// generator gives every union a SwitchFieldName method, which is the only
// reliable marker available through reflection.
func hasUnionMethods(t reflect.Type) bool {
	if _, ok := t.MethodByName("SwitchFieldName"); ok {
		return true
	}
	_, ok := reflect.PointerTo(t).MethodByName("SwitchFieldName")
	return ok
}

// maxUnionProbe bounds the search for valid discriminants. Stellar's enums are
// small and contiguous enough that this covers every union arm in practice; a
// discriminant beyond it merely makes the size estimate less tight.
const maxUnionProbe = 256

// minUnionArmSize returns the size of the smallest arm, or zero when the union
// has a void arm. Arms are encoded without a presence flag — the discriminant
// already says which one is present — so a pointer arm contributes only the
// size of its pointee, and a void arm contributes nothing.
func minUnionArmSize(t reflect.Type, depth int) int {
	method := reflect.ValueOf(reflect.Zero(t).Interface()).MethodByName("ArmForSwitch")
	if !method.IsValid() {
		return 0
	}
	mt := method.Type()
	if mt.NumIn() != 1 || mt.NumOut() != 2 {
		return 0
	}
	best := -1
	for sw := 0; sw < maxUnionProbe; sw++ {
		out := method.Call([]reflect.Value{reflect.ValueOf(int32(sw)).Convert(mt.In(0))})
		arm, armOK := out[0].Interface().(string)
		valid, validOK := out[1].Interface().(bool)
		if !armOK || !validOK || !valid {
			continue
		}
		if arm == "" {
			return 0
		}
		field, ok := t.FieldByName(arm)
		if !ok {
			continue
		}
		armType := field.Type
		if armType.Kind() == reflect.Pointer {
			armType = armType.Elem()
		}
		size := minWireSizeAt(armType, depth+1)
		if best < 0 || size < best {
			best = size
		}
	}
	if best < 0 {
		return 0
	}
	return best
}
