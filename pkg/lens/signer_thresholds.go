package lens

import (
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"

	"github.com/stellar/go-stellar-sdk/xdr"
)

// An AccountEntry records four signing weights: the master key's weight and
// the low, medium and high thresholds that operations require. Cross-
// referencing those numbers against the signer list is the difference between
// knowing an account exists and knowing whether a key can authorize a
// transaction — which is what this file teaches the walker.
//
// The Thresholds field itself is a bare [4]byte that the generic walker would
// render as hex. A type enricher names the triplet, and a node hook on the
// surrounding AccountEntry adds the analysis that needs the full picture:
// whether the master key is locked (weight 0) and whether the available
// signing weight can satisfy the medium and high thresholds. Both paths stay
// offline and deterministic.
func init() {
	registerEnricher(reflect.TypeOf(xdr.Thresholds{}), enrichThresholds)
	nodeHooks[reflect.TypeOf(xdr.AccountEntry{})] = annotateAccountSignerThresholds
}

// thresholdTripletNote renders the low/medium/high/master quartet the way
// developers refer to it outside the XDR.
func thresholdTripletNote(master, low, med, high byte) string {
	return fmt.Sprintf("low=%d med=%d high=%d master=%d", low, med, high, master)
}

// enrichThresholds renders the bare Thresholds array as its four named
// weights. It fires wherever a Thresholds value appears; the fuller analysis
// that also needs the signer list is attached by the AccountEntry node hook.
// The raw value stays the hex form the generic walker would have produced,
// so machine-readable output does not change shape.
func enrichThresholds(rv reflect.Value) (string, any, bool) {
	th, ok := rv.Interface().(xdr.Thresholds)
	if !ok {
		return "", nil, false
	}
	return thresholdTripletNote(th[0], th[1], th[2], th[3]), hex.EncodeToString(th[:]), true
}

// enrichSignerThresholds returns a note describing an account's threshold
// configuration, whether the master key is locked, and whether the signing
// weight available to the account can satisfy the medium and high thresholds.
//
// The reachable weight is the master weight (when non-zero) plus every
// configured signer's weight: that is the total an attacker or a wallet with
// all keys can accumulate. A locked master (weight 0) removes the master from
// that total, which is exactly the case where the co-signer weights matter
// most.
func enrichSignerThresholds(entry *xdr.AccountEntry) (string, bool) {
	if entry == nil {
		return "", false
	}
	master, low, med, high := AccountThresholds(entry)

	var coSigners uint32
	for _, s := range entry.Signers {
		coSigners += uint32(s.Weight)
	}
	reachable := coSigners
	if master > 0 {
		reachable += uint32(master)
	}

	var b strings.Builder
	b.WriteString(thresholdTripletNote(master, low, med, high))
	if master == 0 {
		b.WriteString("; master locked")
	}
	for _, level := range []struct {
		name string
		need uint8
	}{
		{ThresholdLevelMedium, med},
		{ThresholdLevelHigh, high},
	} {
		if reachable >= uint32(level.need) {
			fmt.Fprintf(&b, "; signers meet %s", level.name)
			continue
		}
		fmt.Fprintf(&b, "; signers cannot meet %s (need %d, have %d)",
			level.name, level.need, reachable)
	}
	return b.String(), true
}

// annotateAccountSignerThresholds attaches the full threshold analysis to the
// Thresholds leaf of a decoded AccountEntry. Notes only render on scalar
// leaves, so the analysis sits on Thresholds rather than on the account or
// the Signers list.
func annotateAccountSignerThresholds(n *Node, v any) {
	entry, ok := v.(xdr.AccountEntry)
	if !ok {
		return
	}
	note, ok := enrichSignerThresholds(&entry)
	if !ok {
		return
	}
	if t := n.Child("Thresholds"); t != nil {
		t.Note = note
	}
}

// DiagnoseSetOptions explains a set_options operation from its own parameters.
// A failed operation gets its deepened failure explanation plus any threshold
// or lockout warnings; a successful one gets only the warnings, since both
// problems are legal on-chain. The source account is not part of the operation,
// so use DiagnoseSetOptionsFromSource to detect an account signing for itself.
func DiagnoseSetOptions(op *xdr.SetOptionsOp, code xdr.SetOptionsResultCode) string {
	return DiagnoseSetOptionsFromSource("", op, code)
}

// DiagnoseSetOptionsFromSource is DiagnoseSetOptions for a known source account
// address. It returns an empty string for a nil operation, or for a successful
// operation with nothing to warn about.
func DiagnoseSetOptionsFromSource(source string, op *xdr.SetOptionsOp, code xdr.SetOptionsResultCode) string {
	if op == nil {
		return ""
	}
	r := ExplainCode(code.String())
	if r.Success {
		var warnings []string
		for _, w := range setOptionsWarnings(*op) {
			warnings = append(warnings, w.summary+" "+w.hint)
		}
		return strings.Join(warnings, " ")
	}
	return reasonDiagnosisText(deepenSetOptionsDiagnostics(source, *op, r))
}

// deepenSetOptionsDiagnostics extends the per-code set_options explanation with
// the signer and threshold problems visible in the operation body.
func deepenSetOptionsDiagnostics(source string, o xdr.SetOptionsOp, r Reason) Reason {
	r = deepenSetOptionsFailure(source, o, r)
	if r.Constant == "SetOptionsResultCodeSetOptionsBadSigner" && hasEmptySignedPayload(o) {
		r.Summary = "the signed-payload signer carries an empty payload; a signed-payload signer must commit to at least one byte"
		r.Hint = "Set a non-empty payload of up to 64 bytes on the signer key."
	}
	for _, w := range setOptionsWarnings(o) {
		r.Summary = strings.TrimSpace(r.Summary + " " + w.summary)
		r.Hint = strings.TrimSpace(r.Hint + " " + w.hint)
	}
	return r
}

// hasEmptySignedPayload reports whether the operation's signer is otherwise
// in range but is a signed-payload key without a payload.
func hasEmptySignedPayload(o xdr.SetOptionsOp) bool {
	if o.Signer == nil || o.Signer.Weight > 255 {
		return false
	}
	key := o.Signer.Key
	return key.Type == xdr.SignerKeyTypeSignerKeyTypeEd25519SignedPayload &&
		key.Ed25519SignedPayload != nil && len(key.Ed25519SignedPayload.Payload) == 0
}

// setOptionsWarning is a hazard in a set_options operation that the protocol
// does not reject.
type setOptionsWarning struct {
	summary string
	hint    string
}

// setOptionsWarnings collects the threshold ordering and master key lockout
// hazards in an operation.
func setOptionsWarnings(o xdr.SetOptionsOp) []setOptionsWarning {
	var warnings []setOptionsWarning
	if violations := thresholdOrderViolations(o); len(violations) > 0 {
		warnings = append(warnings, setOptionsWarning{
			summary: fmt.Sprintf(
				"Threshold hierarchy violation: %s. The protocol accepts this ordering, but a higher tier then needs less signing weight than a lower one.",
				strings.Join(violations, ", ")),
			hint: "Keep low <= med <= high.",
		})
	}
	if w, ok := masterLockoutWarning(o); ok {
		warnings = append(warnings, w)
	}
	return warnings
}

// thresholdOrderViolations names each pair of thresholds set by the operation
// that breaks low <= med <= high. Low and high are only compared directly when
// the operation leaves med unset, because med otherwise reports the break.
func thresholdOrderViolations(o xdr.SetOptionsOp) []string {
	var violations []string
	exceeds := func(aName string, a *xdr.Uint32, bName string, b *xdr.Uint32) {
		if a != nil && b != nil && *a > *b {
			violations = append(violations, fmt.Sprintf("%s=%d > %s=%d", aName, *a, bName, *b))
		}
	}
	exceeds("low", o.LowThreshold, "med", o.MedThreshold)
	exceeds("med", o.MedThreshold, "high", o.HighThreshold)
	if o.MedThreshold == nil {
		exceeds("low", o.LowThreshold, "high", o.HighThreshold)
	}
	return violations
}

// masterLockoutWarning detects an operation that disables the master key
// without leaving enough signing weight behind. Changing signers needs the high
// threshold, so an account that cannot reach it can never be repaired. Signers
// already on the account are not visible in the operation, so the warning says
// how much weight they would have to supply.
func masterLockoutWarning(o xdr.SetOptionsOp) (setOptionsWarning, bool) {
	if o.MasterWeight == nil || *o.MasterWeight != 0 {
		return setOptionsWarning{}, false
	}
	var added uint32
	if o.Signer != nil && o.Signer.Weight <= 255 {
		added = uint32(o.Signer.Weight)
	}
	hint := "Add signers that reach the high threshold before setting the master weight to 0."
	if o.HighThreshold == nil {
		if added > 0 {
			return setOptionsWarning{}, false
		}
		return setOptionsWarning{
			summary: "Master key lockout risk: master=0 disables the master key and this operation adds no signer weight, so the account is locked unless its existing signers meet the high threshold.",
			hint:    hint,
		}, true
	}
	// Even a zero threshold needs one signature from a key with weight.
	required := max(uint32(*o.HighThreshold), 1)
	if added >= required {
		return setOptionsWarning{}, false
	}
	return setOptionsWarning{
		summary: fmt.Sprintf(
			"Master key lockout risk: master=0 disables the master key and this operation provides signer weight %d against high=%d, so the account is locked unless its existing signers supply the remaining %d.",
			added, *o.HighThreshold, required-added),
		hint: hint,
	}, true
}
