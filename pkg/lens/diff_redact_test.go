package lens_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

// redactionEnvelope builds a v1 envelope with one bump-sequence operation and
// the supplied signatures, so tests can vary only the dynamic cryptographic
// fields and leave everything else identical.
func redactionEnvelope(t *testing.T, signatures ...xdr.DecoratedSignature) string {
	t.Helper()
	op := xdr.Operation{Body: xdr.OperationBody{
		Type:           xdr.OperationTypeBumpSequence,
		BumpSequenceOp: &xdr.BumpSequenceOp{BumpTo: xdr.SequenceNumber(7)},
	}}
	env := xdr.TransactionEnvelope{
		Type: xdr.EnvelopeTypeEnvelopeTypeTx,
		V1: &xdr.TransactionV1Envelope{
			Tx: xdr.Transaction{
				SourceAccount: xdr.MustMuxedAddress("GAPB7YMUQ6MPQ23PTFDNBZP7HGGTZPI2DT5JJURUOIXNPLGHW2O3WYXZ"),
				Fee:           100,
				SeqNum:        xdr.SequenceNumber(1),
				Cond:          xdr.Preconditions{Type: xdr.PreconditionTypePrecondNone},
				Memo:          xdr.Memo{Type: xdr.MemoTypeMemoNone},
				Operations:    []xdr.Operation{op},
				Ext:           xdr.TransactionExt{V: 0},
			},
			Signatures: signatures,
		},
	}
	return mustMarshal(t, env)
}

func redactionSignature(hint, payload byte) xdr.DecoratedSignature {
	return xdr.DecoratedSignature{
		Hint:      xdr.SignatureHint{hint, hint, hint, hint},
		Signature: []byte{payload, payload, payload},
	}
}

func TestRedactionPolicyFromNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		names   []string
		want    lens.RedactionPolicy
		wantErr bool
	}{
		{name: "empty", names: nil},
		{name: "hashes", names: []string{"hashes"}, want: lens.RedactionPolicy{Hashes: true}},
		{name: "signatures", names: []string{"SIGNATURES"}, want: lens.RedactionPolicy{Signatures: true}},
		{name: "identifiers", names: []string{"ids"}, want: lens.RedactionPolicy{Identifiers: true}},
		{name: "all", names: []string{"all"}, want: lens.RedactionPolicy{Hashes: true, Signatures: true, Identifiers: true}},
		{name: "comma separated", names: []string{"hashes,signatures"}, want: lens.RedactionPolicy{Hashes: true, Signatures: true}},
		{name: "repeated", names: []string{"hashes", "signatures"}, want: lens.RedactionPolicy{Hashes: true, Signatures: true}},
		{name: "unknown", names: []string{"secrets"}, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := lens.RedactionPolicyFromNames(tc.names)
			if tc.wantErr {
				if err == nil {
					t.Fatal("RedactionPolicyFromNames() error = nil, want an error")
				}
				if !errors.Is(err, lens.ErrInvalidArgument) {
					t.Errorf("error = %v, want ErrInvalidArgument", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("RedactionPolicyFromNames() error = %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("policy = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestRedactionIgnoresReorderedSignatures(t *testing.T) {
	t.Parallel()

	policy := lens.RedactSignatures()
	left := redactionEnvelope(t, redactionSignature(0xaa, 0x01))
	right := redactionEnvelope(t, redactionSignature(0xbb, 0x02))

	plain, err := lens.DiffBase64(left, right, "")
	if err != nil {
		t.Fatalf("DiffBase64() error = %v", err)
	}
	if plain.Equal() {
		t.Fatal("unredacted diff reported equality for different signatures")
	}

	redacted, err := lens.DiffBase64WithOptions(left, right, "", lens.DiffOptions{Redaction: policy})
	if err != nil {
		t.Fatalf("DiffBase64WithOptions() error = %v", err)
	}
	if !redacted.Equal() {
		t.Errorf("redacted diff reported %d change(s): %v", len(redacted.Changes), redacted.Changes)
	}
	if len(redacted.Redacted) == 0 {
		t.Error("redacted diff recorded no redacted paths")
	}
	for _, p := range redacted.Redacted {
		if !strings.Contains(p, "Signatures") {
			t.Errorf("redacted path %q does not locate the signature subtree", p)
		}
	}
}

func TestRedactionIgnoresDifferingHashes(t *testing.T) {
	t.Parallel()

	left := mustMarshal(t, xdr.LedgerHeaderHistoryEntry{Hash: xdr.Hash{1, 2, 3}})
	right := mustMarshal(t, xdr.LedgerHeaderHistoryEntry{Hash: xdr.Hash{9, 8, 7}})

	plain, err := lens.DiffBase64(left, right, "LedgerHeaderHistoryEntry")
	if err != nil {
		t.Fatalf("DiffBase64() error = %v", err)
	}
	if plain.Equal() {
		t.Fatal("unredacted diff reported equality for different hashes")
	}

	redacted, err := lens.DiffBase64WithOptions(left, right, "LedgerHeaderHistoryEntry",
		lens.DiffOptions{Redaction: lens.RedactHashes()})
	if err != nil {
		t.Fatalf("DiffBase64WithOptions() error = %v", err)
	}
	if !redacted.Equal() {
		t.Errorf("redacted diff reported %d change(s): %v", len(redacted.Changes), redacted.Changes)
	}
}

func TestRedactionSeparatesCategories(t *testing.T) {
	t.Parallel()

	// Different signatures must survive a hash-only policy, and different
	// hashes must survive a signature-only policy: the categories are
	// independent, not aliases of one another.
	left := mustMarshal(t, xdr.LedgerHeaderHistoryEntry{
		Hash: xdr.Hash{1},
	})
	right := mustMarshal(t, xdr.LedgerHeaderHistoryEntry{
		Hash: xdr.Hash{2},
	})

	hashOnly, err := lens.DiffBase64WithOptions(left, right, "LedgerHeaderHistoryEntry",
		lens.DiffOptions{Redaction: lens.RedactHashes()})
	if err != nil {
		t.Fatalf("hash-only diff error = %v", err)
	}
	if !hashOnly.Equal() {
		t.Errorf("hash-only policy still reported %d change(s): %v", len(hashOnly.Changes), hashOnly.Changes)
	}

	// The Hash field here is a top-level union arm; a signature-only policy
	// must leave the differing hash visible.
	sigOnly, err := lens.DiffBase64WithOptions(left, right, "LedgerHeaderHistoryEntry",
		lens.DiffOptions{Redaction: lens.RedactSignatures()})
	if err != nil {
		t.Fatalf("signature-only diff error = %v", err)
	}
	if sigOnly.Equal() {
		t.Error("signature-only policy hid a differing hash")
	}
}

func TestRedactionCustomFields(t *testing.T) {
	t.Parallel()

	// A field-name policy reaches anonymous values that have no
	// self-describing XDR type.
	leftPayload := mustMarshal(t, xdr.TransactionSet{PreviousLedgerHash: xdr.Hash{1}})
	rightPayload := mustMarshal(t, xdr.TransactionSet{PreviousLedgerHash: xdr.Hash{2}})

	plain, err := lens.DiffBase64(leftPayload, rightPayload, "TransactionSet")
	if err != nil {
		t.Fatalf("DiffBase64() error = %v", err)
	}
	if plain.Equal() {
		t.Fatal("expected the differing PreviousLedgerHash to be reported")
	}

	redacted, err := lens.DiffBase64WithOptions(leftPayload, rightPayload, "TransactionSet",
		lens.DiffOptions{Redaction: lens.RedactionPolicy{Fields: []string{"PreviousLedgerHash"}}})
	if err != nil {
		t.Fatalf("DiffBase64WithOptions() error = %v", err)
	}
	if !redacted.Equal() {
		t.Errorf("field-named policy reported %d change(s): %v", len(redacted.Changes), redacted.Changes)
	}
}

func TestRedactionGlobFieldPattern(t *testing.T) {
	t.Parallel()

	left := mustMarshal(t, xdr.TransactionSet{PreviousLedgerHash: xdr.Hash{1}})
	right := mustMarshal(t, xdr.TransactionSet{PreviousLedgerHash: xdr.Hash{2}})

	redacted, err := lens.DiffBase64WithOptions(left, right, "TransactionSet",
		lens.DiffOptions{Redaction: lens.RedactionPolicy{Fields: []string{"*Hash"}}})
	if err != nil {
		t.Fatalf("DiffBase64WithOptions() error = %v", err)
	}
	if !redacted.Equal() {
		t.Errorf("glob policy reported %d change(s): %v", len(redacted.Changes), redacted.Changes)
	}
}

func TestRedactValueDoesNotMutateInput(t *testing.T) {
	t.Parallel()

	payload := redactionEnvelope(t, redactionSignature(0xaa, 0x01))
	v, err := lens.DecodeAs(payload, "TransactionEnvelope")
	if err != nil {
		t.Fatalf("DecodeAs() error = %v", err)
	}
	original := v.Node.Find("V1.Signatures[0].Signature")
	if original == nil {
		t.Fatal("failed to locate the signature node")
	}
	before := original.Value

	redacted, paths, err := lens.RedactValue(v, lens.RedactHashesAndSignatures())
	if err != nil {
		t.Fatalf("RedactValue() error = %v", err)
	}
	if redacted == v {
		t.Fatal("RedactValue() returned the input value, want a copy")
	}
	if redacted.Raw != nil {
		t.Error("RedactValue() kept Raw, which would leak the unredacted XDR")
	}
	if len(paths) == 0 {
		t.Error("RedactValue() recorded no redacted paths")
	}
	after := v.Node.Find("V1.Signatures[0].Signature")
	if after == nil {
		t.Fatal("input tree lost the signature node")
	}
	if !reflect.DeepEqual(after.Value, before) {
		t.Errorf("RedactValue() mutated the input: %v -> %v", before, after.Value)
	}
}

func TestRedactValueRequiresValue(t *testing.T) {
	t.Parallel()

	_, _, err := lens.RedactValue(nil, lens.RedactHashes())
	if err == nil {
		t.Fatal("RedactValue(nil) error = nil, want an error")
	}
	if !errors.Is(err, lens.ErrInvalidArgument) {
		t.Errorf("error = %v, want ErrInvalidArgument", err)
	}
}

func TestRedactAddress(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "short", in: "abc", want: "***"},
		{name: "exactly eight", in: "abcdefgh", want: "********"},
		{name: "nine characters", in: "abcdefghi", want: "abcd...fghi"},
		{
			name: "account",
			in:   "GA3FJ5QJQ6MPQ23PTFDNBZP7HGGTZPI2DT5JJURUOIXNPLGHW2O3WXYZ",
			want: "GA3F...WXYZ",
		},
		{
			name: "contract",
			in:   "CBGSBKYMYO6OMGHQXXNOBRGVUDFUDVC2XLC3SXON5R2SNXILR7XCKKY3",
			want: "CBGS...KKY3",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := lens.RedactAddress(tc.in); got != tc.want {
				t.Errorf("RedactAddress(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestRedactAddresses(t *testing.T) {
	t.Parallel()

	account := "GAPB7YMUQ6MPQ23PTFDNBZP7HGGTZPI2DT5JJURUOIXNPLGHW2O3WYXZ"
	contract := "CBGSBKYMYO6OMGHQXXNOBRGVUDFUDVC2XLC3SXON5R2SNXILR7XCKKY3"
	muxed := "M" + strings.Repeat("A", 68)
	hexHash := "9e9dfcffc2a451db2125c09ea8fa17e88af14fd044dee19f0453106ee5aab2df"

	input := strings.Join([]string{
		"source=" + account,
		"contract=" + contract,
		"muxed=" + muxed,
		"hash=" + hexHash,
		"short=GABC",
		"json=\"" + account + "\"",
	}, "\n")
	got := lens.RedactAddresses(input)

	for _, full := range []string{account, contract, muxed} {
		if strings.Contains(got, full) {
			t.Errorf("redacted output still contains %q:\n%s", full, got)
		}
	}
	for _, want := range []string{
		"GAPB...WYXZ",
		"CBGS...KKY3",
		"MAAA...AAAA",
		hexHash,
		"short=GABC",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("redacted output does not contain %q:\n%s", want, got)
		}
	}
}

func TestRedactionDisabledByDefault(t *testing.T) {
	t.Parallel()

	left := redactionEnvelope(t, redactionSignature(0xaa, 0x01))
	right := redactionEnvelope(t, redactionSignature(0xbb, 0x02))

	result, err := lens.DiffBase64(left, right, "")
	if err != nil {
		t.Fatalf("DiffBase64() error = %v", err)
	}
	if result.Equal() {
		t.Fatal("default diff reported equality for different signatures")
	}
	if result.Redacted != nil {
		t.Errorf("default diff recorded redacted paths: %v", result.Redacted)
	}
}
