package lens

import (
	"fmt"
	"reflect"
	"strconv"

	"github.com/stellar/go-stellar-sdk/xdr"
)

// The Stellar Consensus Protocol exchanges three nested message types: a
// quorum set describing who may vote, a statement a validator makes about one
// ledger slot, and an envelope that carries a statement plus its signature.
// On the wire a validator is a raw 32-byte ed25519 key, and a statement type
// is a bare integer, so the two renderings that make SCP output readable are
// the strkey address and the pledge name.
//
// See docs/reference/consensus_scp.md for an authoritative reference on SCP
// message types (PREPARE, CONFIRM, EXTERNALIZE, NOMINATE), quorum sets, and
// diagnostic workflows.
func init() {
	enricherFor[reflect.TypeOf(xdr.PublicKey{})] = enrichPublicKey
	enricherFor[reflect.TypeOf(xdr.NodeId{})] = enrichNodeID
	enricherFor[reflect.TypeOf(xdr.ScpStatementType(0))] = enrichScpStatementType

	// A statement's slot index is the ledger sequence it votes on, not an
	// opaque counter; naming it saves a lookup.
	fieldNotes["SlotIndex"] = slotIndexNote

	// ScpBallot: label the Counter as the ballot round so the number's role
	// is clear at a glance.  Counter only appears in this struct, so the
	// field-name registry is sufficient; the type-scoped registry would add
	// no extra precision here.
	registerFieldNote(reflect.TypeOf(xdr.ScpBallot{}), "Counter", scpBallotCounterNote)
}

// enrichPublicKey renders a consensus public key as its strkey address, the
// same G... form accounts use. PublicKey has no Address helper of its own —
// AccountId is the typedef that does — so the value is converted rather than
// reimplementing strkey encoding.
func enrichPublicKey(rv reflect.Value) (string, any, bool) {
	pk, ok := rv.Interface().(xdr.PublicKey)
	if !ok {
		return "", nil, false
	}
	// GetAddress has a pointer receiver, so convert to an addressable local.
	accountID := xdr.AccountId(pk)
	addr, err := accountID.GetAddress()
	if err != nil || addr == "" {
		return "", nil, false
	}
	return "", addr, true
}

// enrichNodeID renders a consensus node id as its strkey address. NodeId is
// its own typedef over PublicKey, so it needs its own registration even
// though the two render identically.
func enrichNodeID(rv reflect.Value) (string, any, bool) {
	id, ok := rv.Interface().(xdr.NodeId)
	if !ok {
		return "", nil, false
	}
	addr, err := id.GetAddress()
	if err != nil || addr == "" {
		return "", nil, false
	}
	return "", addr, true
}

// enrichScpStatementType names the pledge a statement makes. The generated
// constant (ScpStatementTypeScpStPrepare) is accurate but long; the protocol
// documentation calls the pledges prepare, confirm, externalize and nominate.
// An unknown discriminant declines and keeps the generic enum rendering.
func enrichScpStatementType(rv reflect.Value) (string, any, bool) {
	t, ok := rv.Interface().(xdr.ScpStatementType)
	if !ok {
		return "", nil, false
	}
	switch t {
	case xdr.ScpStatementTypeScpStPrepare:
		return "", "prepare", true
	case xdr.ScpStatementTypeScpStConfirm:
		return "", "confirm", true
	case xdr.ScpStatementTypeScpStExternalize:
		return "", "externalize", true
	case xdr.ScpStatementTypeScpStNominate:
		return "", "nominate", true
	default:
		return "", nil, false
	}
}

// slotIndexNote names a statement's slot index for what it is: the ledger
// sequence the validators are agreeing on.
func slotIndexNote(rv reflect.Value) (string, bool) {
	v, ok := rv.Interface().(xdr.Uint64)
	if !ok {
		return "", false
	}
	return "ledger slot " + strconv.FormatUint(uint64(v), 10), true
}

// scpBallotCounterNote labels a ballot's Counter field as a ballot round
// number so the reader does not have to look up what the integer means.
func scpBallotCounterNote(rv reflect.Value) (string, bool) {
	switch rv.Kind() {
	case reflect.Uint32, reflect.Uint64, reflect.Uint:
		return fmt.Sprintf("ballot round %d", rv.Uint()), true
	}
	return "", false
}
