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
