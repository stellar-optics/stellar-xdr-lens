package lens

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"
)

func TestEnrichThresholds(t *testing.T) {
	t.Parallel()

	note, raw, ok := enrichThresholds(reflect.ValueOf(xdr.Thresholds{2, 1, 3, 5}))
	if !ok {
		t.Fatal("enrichThresholds(ok) = false, want true")
	}
	if note != "low=1 med=3 high=5 master=2" {
		t.Errorf("note = %q, want the named triplet", note)
	}
	if th, okRaw := raw.(string); !okRaw || th != "02010305" {
		t.Errorf("raw = %v, want hex of the original thresholds", raw)
	}
	if _, _, ok := enrichThresholds(reflect.ValueOf("not thresholds")); ok {
		t.Error("enrichThresholds(string) ok = true, want false")
	}
}

func TestEnrichSignerThresholdsNil(t *testing.T) {
	t.Parallel()

	if note, ok := enrichSignerThresholds(nil); ok {
		t.Errorf("enrichSignerThresholds(nil) = (%q, true), want ok=false", note)
	}
}

// TestEnrichSignerThresholdsSingleSig is the baseline: a normal account whose
// master key alone clears every threshold.
func TestEnrichSignerThresholdsSingleSig(t *testing.T) {
	t.Parallel()

	entry := &xdr.AccountEntry{
		Thresholds: xdr.Thresholds{3, 1, 2, 3}, // master, low, med, high
	}
	note, ok := enrichSignerThresholds(entry)
	if !ok {
		t.Fatal("enrichSignerThresholds(single-sig) ok = false")
	}
	for _, want := range []string{
		"low=1 med=2 high=3 master=3",
		"signers meet medium",
		"signers meet high",
	} {
		if !strings.Contains(note, want) {
			t.Errorf("note = %q, want it to contain %q", note, want)
		}
	}
	if strings.Contains(note, "master locked") {
		t.Errorf("note = %q, must not report a locked master", note)
	}
}

// TestEnrichSignerThresholdsMultisig2of3 is the classic three-signer setup
// where any two weights reach medium and all three reach high, while the
// master key is deliberately weak.
func TestEnrichSignerThresholdsMultisig2of3(t *testing.T) {
	t.Parallel()

	signers := make([]xdr.Signer, 3)
	for i := range signers {
		var key xdr.Uint256
		for j := range key {
			key[j] = byte(0xA0 + i)
		}
		k, err := xdr.NewSignerKey(xdr.SignerKeyTypeSignerKeyTypeEd25519, key)
		if err != nil {
			t.Fatalf("NewSignerKey() error = %v", err)
		}
		signers[i] = xdr.Signer{Key: k, Weight: 1}
	}
	entry := &xdr.AccountEntry{
		Thresholds: xdr.Thresholds{1, 1, 2, 3}, // master, low, med, high
		Signers:    signers,
	}
	note, ok := enrichSignerThresholds(entry)
	if !ok {
		t.Fatal("enrichSignerThresholds(2-of-3) ok = false")
	}
	for _, want := range []string{
		"low=1 med=2 high=3 master=1",
		"signers meet medium",
		"signers meet high",
	} {
		if !strings.Contains(note, want) {
			t.Errorf("note = %q, want it to contain %q", note, want)
		}
	}
}

// TestEnrichSignerThresholdsLockedMaster covers the case the issue calls out
// explicitly: the master key is disabled (weight 0) and only the co-signers
// can authorize anything. With two weight-1 signers against a medium
// threshold of 2 and a high threshold of 3, medium is reachable and high is
// not.
func TestEnrichSignerThresholdsLockedMaster(t *testing.T) {
	t.Parallel()

	signers := make([]xdr.Signer, 2)
	for i := range signers {
		var key xdr.Uint256
		for j := range key {
			key[j] = byte(0xB0 + i)
		}
		k, err := xdr.NewSignerKey(xdr.SignerKeyTypeSignerKeyTypeEd25519, key)
		if err != nil {
			t.Fatalf("NewSignerKey() error = %v", err)
		}
		signers[i] = xdr.Signer{Key: k, Weight: 1}
	}
	entry := &xdr.AccountEntry{
		Thresholds: xdr.Thresholds{0, 1, 2, 3}, // master locked
		Signers:    signers,
	}
	note, ok := enrichSignerThresholds(entry)
	if !ok {
		t.Fatal("enrichSignerThresholds(locked) ok = false")
	}
	for _, want := range []string{
		"low=1 med=2 high=3 master=0",
		"master locked",
		"signers meet medium",
		"signers cannot meet high (need 3, have 2)",
	} {
		if !strings.Contains(note, want) {
			t.Errorf("note = %q, want it to contain %q", note, want)
		}
	}
}

// TestEnrichSignerThresholdsLockedWithoutEnoughSigners is the empty co-signer
// set on a locked account: nothing can meet any non-zero threshold.
func TestEnrichSignerThresholdsLockedWithoutEnoughSigners(t *testing.T) {
	t.Parallel()

	entry := &xdr.AccountEntry{
		Thresholds: xdr.Thresholds{0, 1, 1, 1},
	}
	note, ok := enrichSignerThresholds(entry)
	if !ok {
		t.Fatal("enrichSignerThresholds(locked, no signers) ok = false")
	}
	for _, want := range []string{
		"master locked",
		"signers cannot meet medium (need 1, have 0)",
		"signers cannot meet high (need 1, have 0)",
	} {
		if !strings.Contains(note, want) {
			t.Errorf("note = %q, want it to contain %q", note, want)
		}
	}
}

func TestAnnotateAccountSignerThresholds(t *testing.T) {
	t.Parallel()

	entry := xdr.AccountEntry{
		Thresholds: xdr.Thresholds{3, 1, 2, 3},
	}
	node := buildNode("AccountEntry", reflect.ValueOf(entry), 0)
	if node == nil {
		t.Fatal("buildNode(AccountEntry) returned nil")
	}
	thresholds := node.Child("Thresholds")
	if thresholds == nil {
		t.Fatal("Thresholds child missing")
	}
	if th, ok := thresholds.Value.(string); !ok || th != "03010203" {
		t.Errorf("Thresholds Value = %v, want the hex form", thresholds.Value)
	}
	if !strings.Contains(thresholds.Note, "low=1 med=2 high=3 master=3") {
		t.Errorf("Thresholds Note = %q, want the named triplet", thresholds.Note)
	}
	if !strings.Contains(thresholds.Note, "signers meet medium") {
		t.Errorf("Thresholds Note = %q, want the signer-capacity analysis", thresholds.Note)
	}

	// The hook declines foreign values rather than panicking.
	annotateAccountSignerThresholds(&Node{}, "not an account")
}

func TestEnrichThresholdsRegistered(t *testing.T) {
	t.Parallel()

	fn, found := enricherFor[reflect.TypeOf(xdr.Thresholds{})]
	if !found {
		t.Fatal("xdr.Thresholds enricher is not registered")
	}
	note, _, ok := fn(reflect.ValueOf(xdr.Thresholds{0, 1, 2, 3}))
	if !ok || note != "low=1 med=2 high=3 master=0" {
		t.Errorf("registered enricher = (%q, %v), want the triplet", note, ok)
	}
}
