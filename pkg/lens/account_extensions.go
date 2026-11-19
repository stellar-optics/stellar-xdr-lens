package lens

import (
	"fmt"
	"reflect"

	"github.com/stellar/go-stellar-sdk/xdr"
)

// An AccountEntry has grown three levels of extensions, each one adding a
// piece of bookkeeping that the protocol could not express before:
//
//   - v1 records liabilities: the reserves an account has locked up in open
//     offers and trustlines that cannot be spent.
//   - v2 records sponsorship counters and, per signer, which sponsor pays the
//     reserve for that signer.
//   - v3 records when the account's sequence number last changed, as a ledger
//     sequence and a close time.
//
// None of those fields is self-describing on the wire — twice-stroop
// distinctions, bare counters and a ledger number all render as opaque
// integers — so this file attaches the missing context as field notes. The
// extension unions themselves are also left intact and annotated with the
// capability each discriminant selects, rather than collapsed, so the tree
// still shows exactly what was encoded.
func init() {
	// Liabilities are stroop amounts like every other balance, and Buying
	// and Selling are only Int64 there. The identically named OfferEntry
	// fields are Assets, which the amount enricher's type switch declines.
	amountFields["Buying"] = true
	amountFields["Selling"] = true

	fieldNotes["NumSponsoring"] = numSponsoringNote
	fieldNotes["NumSponsored"] = numSponsoredNote
	fieldNotes["SeqLedger"] = seqLedgerNote

	// Extension unions are struct nodes, and both formatters display notes
	// on scalar leaves only, so the capability note goes on the
	// discriminant child rather than on the union itself.
	nodeHooks[reflect.TypeOf(xdr.AccountEntryExt{})] = annotateAccountExtension
	nodeHooks[reflect.TypeOf(xdr.AccountEntryExtensionV1Ext{})] = annotateAccountExtension
	nodeHooks[reflect.TypeOf(xdr.AccountEntryExtensionV2Ext{})] = annotateAccountExtension
}

// numSponsoringNote renders the count of entries whose reserve this account
// pays for. Zero means no sponsorships, which is the default state and not
// worth annotating.
func numSponsoringNote(rv reflect.Value) (string, bool) {
	v, ok := rv.Interface().(xdr.Uint32)
	if !ok || v == 0 {
		return "", false
	}
	return fmt.Sprintf("sponsoring %d ledger entr%s", v, pluralSuffix(uint64(v))), true
}

// numSponsoredNote renders the count of this account's entries whose reserve
// another account pays. As with numSponsoring, zero is the unremarkable case.
func numSponsoredNote(rv reflect.Value) (string, bool) {
	v, ok := rv.Interface().(xdr.Uint32)
	if !ok || v == 0 {
		return "", false
	}
	return fmt.Sprintf("%d ledger entr%s sponsored by another account", v, pluralSuffix(uint64(v))), true
}

// seqLedgerNote names a v3 ledger number for what it is: the ledger at which
// the account's present sequence number was set.
func seqLedgerNote(rv reflect.Value) (string, bool) {
	v, ok := rv.Interface().(xdr.Uint32)
	if !ok {
		return "", false
	}
	return fmt.Sprintf("seqNum set in ledger %d", uint64(v)), true
}

// annotateAccountExtension attaches the capability note produced by
// accountExtensionNote to the union's discriminant child.
func annotateAccountExtension(n *Node, v any) {
	note, ok := accountExtensionNote(reflect.ValueOf(v))
	if !ok {
		return
	}
	if child := n.Child("V"); child != nil {
		child.Note = note
	}
}

// accountExtensionNote names the capability each account-extension
// discriminant selects. It only fires for the three account extension unions;
// every other Ext field in the protocol declines and keeps the generic
// rendering, which is why registering it under the shared field name is safe.
// The zero arms are deliberately unannotated: an account with no extensions is
// the common case, and "no further extensions" on every account would be
// noise. V1, V2 and V3 each never select an arm below their own version.
func accountExtensionNote(rv reflect.Value) (string, bool) {
	switch u := rv.Interface().(type) {
	case xdr.AccountEntryExt:
		if u.V == 1 {
			return "liabilities", true
		}
	case xdr.AccountEntryExtensionV1Ext:
		if u.V == 2 {
			return "sponsorship counters", true
		}
	case xdr.AccountEntryExtensionV2Ext:
		if u.V == 3 {
			return "sequence ledger and time", true
		}
	}
	return "", false
}

// pluralSuffix returns "ies" for anything but one, so counters read naturally:
// "1 ledger entry", "2 ledger entries".
func pluralSuffix(n uint64) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}

// DiagnoseBumpSequence explains a bump_sequence failure by comparing the target
// with the envelope's sequence number, which is the source account's sequence
// when the transaction applies. It returns "" for any other code, and when the
// target is above txSeq, since the operation then gives no cause to report.
//
// stellar-core rejects only negative targets with this code and treats any
// other target at or below the current sequence as a successful no-op, so the
// non-negative case also asks the reader to check the pairing.
func DiagnoseBumpSequence(txSeq int64, bumpTo xdr.SequenceNumber, code xdr.BumpSequenceResultCode) string {
	if code != xdr.BumpSequenceResultCodeBumpSequenceBadSeq {
		return ""
	}
	target := int64(bumpTo)
	const rule = "BumpSequence cannot rewind or stay on the current sequence number"
	switch {
	case target < 0:
		return fmt.Sprintf(
			"bumpTo %d is negative, so it is not greater than the envelope sequence number %d; %s, and stellar-core rejects every negative target. Set bumpTo > %d.",
			target, txSeq, rule, txSeq)
	case target > txSeq:
		return ""
	}
	relation := "is below"
	if target == txSeq {
		relation = "equals"
	}
	return fmt.Sprintf(
		"bumpTo %d %s the envelope sequence number %d; %s. Set bumpTo > %d. stellar-core normally applies such a target as a no-op and reserves this code for negative targets, so confirm this result belongs to this envelope.",
		target, relation, txSeq, rule, txSeq)
}

// deepenBumpSequenceFailure attaches DiagnoseBumpSequence's finding to a failed
// bump_sequence. An operation-level source override is left alone: the envelope
// sequence number belongs to the transaction source, not to that account.
func deepenBumpSequenceFailure(source string, txSeq int64, op xdr.Operation, r Reason) Reason {
	body, ok := op.Body.GetBumpSequenceOp()
	if !ok || operationEffectiveSource(source, op) != source {
		return r
	}
	if r.Constant != xdr.BumpSequenceResultCodeBumpSequenceBadSeq.String() {
		return r
	}
	if diagnosis := DiagnoseBumpSequence(txSeq, body.BumpTo, xdr.BumpSequenceResultCodeBumpSequenceBadSeq); diagnosis != "" {
		r.Context = diagnosis
		r.Hint = "Set bumpTo to a non-negative value greater than the source account's current sequence number."
	}
	return r
}
