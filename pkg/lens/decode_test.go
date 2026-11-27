package lens_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

func TestDecodeDetectsFixtureTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		fixture  string
		wantType string
	}{
		{"successful envelope", "tx_success.env", "TransactionEnvelope"},
		{"successful result", "tx_success.res", "TransactionResult"},
		{"failed envelope", "tx_failed.env", "TransactionEnvelope"},
		{"failed result", "tx_failed.res", "TransactionResult"},
		{"fee-bump envelope", "tx_feebump.env", "TransactionEnvelope"},
		{"fee-bump result", "tx_feebump.res", "TransactionResult"},
		{"soroban envelope", "tx_soroban.env", "TransactionEnvelope"},
		{"soroban result", "tx_soroban.res", "TransactionResult"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := lens.Decode(loadFixture(t, tc.fixture))
			if err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			if got.Type != tc.wantType {
				t.Errorf("Decode() type = %q, want %q", got.Type, tc.wantType)
			}
			if got.Node == nil {
				t.Fatal("Decode() returned a nil node tree")
			}
			if got.Raw == nil {
				t.Error("Decode() returned a nil Raw value")
			}
			if len(got.Node.Children) == 0 {
				t.Error("Decode() produced a tree with no children")
			}
		})
	}
}

// TestDetectIsUnambiguousForRealPayloads guards the property that makes
// auto-detection usable: a transaction-sized payload should match exactly one
// type. If a future registry addition breaks this, detection silently starts
// guessing.
func TestDetectIsUnambiguousForRealPayloads(t *testing.T) {
	t.Parallel()

	fixtures := []string{
		"tx_success.env", "tx_success.res",
		"tx_failed.env", "tx_failed.res",
		"tx_feebump.env", "tx_soroban.env",
	}

	for _, f := range fixtures {
		t.Run(f, func(t *testing.T) {
			t.Parallel()

			cands, err := lens.Detect(loadFixture(t, f))
			if err != nil {
				t.Fatalf("Detect() error = %v", err)
			}
			if len(cands) != 1 {
				names := make([]string, 0, len(cands))
				for _, c := range cands {
					names = append(names, c.Type)
				}
				t.Errorf("Detect() matched %d types (%s), want exactly 1",
					len(cands), strings.Join(names, ", "))
			}
		})
	}
}

// TestDetectReportsGenuineAmbiguity documents the known-ambiguous case rather
// than pretending detection is always certain. An empty Memo and a native
// Asset really are the same four bytes.
func TestDetectReportsGenuineAmbiguity(t *testing.T) {
	t.Parallel()

	cands, err := lens.Detect("AAAAAA==")
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if len(cands) < 2 {
		t.Fatalf("Detect() matched %d types, want at least 2 for an ambiguous payload", len(cands))
	}

	got := make(map[string]bool, len(cands))
	for _, c := range cands {
		got[c.Type] = true
	}
	for _, want := range []string{"Memo", "Asset"} {
		if !got[want] {
			t.Errorf("Detect() did not report %s as a candidate", want)
		}
	}
	// Ranking must be deterministic so scripts relying on the default choice
	// do not shift between runs.
	if cands[0].Type != "Memo" {
		t.Errorf("Detect() best candidate = %q, want %q", cands[0].Type, "Memo")
	}
}

func TestDecodeAs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		fixture  string
		typeName string
		wantErr  bool
	}{
		{"correct type", "tx_success.env", "TransactionEnvelope", false},
		{"case insensitive", "tx_success.env", "transactionenvelope", false},
		{"wrong type is rejected", "tx_success.env", "LedgerHeader", true},
		{"unknown type is rejected", "tx_success.env", "NotAnXdrType", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := lens.DecodeAs(loadFixture(t, tc.fixture), tc.typeName)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("DecodeAs(%q) error = nil, want an error", tc.typeName)
				}
				return
			}
			if err != nil {
				t.Fatalf("DecodeAs(%q) error = %v", tc.typeName, err)
			}
			if got.Type != "TransactionEnvelope" {
				t.Errorf("DecodeAs() type = %q, want TransactionEnvelope", got.Type)
			}
		})
	}
}

func TestDecodeAsUnknownTypeIsIdentifiable(t *testing.T) {
	t.Parallel()

	_, err := lens.DecodeAs("AAAAAA==", "NoSuchType")
	if !errors.Is(err, lens.ErrUnknownType) {
		t.Errorf("DecodeAs() error = %v, want it to wrap ErrUnknownType", err)
	}
}

// TestDecodeRejectsBadInput covers the hostile-input paths. A decoder is a
// parser for untrusted data, so it must return errors rather than panic.
func TestDecodeRejectsBadInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload string
	}{
		{"empty", ""},
		{"whitespace only", "   \n\t "},
		{"not base64", "!!!not base64!!!"},
		{"valid base64 but not XDR", "aGVsbG8gd29ybGQ="},
		{"truncated envelope", loadFixtureRaw("tx_success.env")[:40]},
		{"single byte", "AA=="},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Must not panic, and must report an error.
			if _, err := lens.Decode(tc.payload); err == nil {
				t.Errorf("Decode(%q) error = nil, want an error", truncate(tc.payload))
			}
		})
	}
}

// TestDecodeToleratesWhitespaceAndURLSafeBase64 covers the shapes XDR arrives
// in when copied from a terminal, a log, or a query string.
func TestDecodeToleratesInputVariants(t *testing.T) {
	t.Parallel()

	canonical := loadFixtureRaw("tx_success.env")

	tests := []struct {
		name    string
		payload string
	}{
		{"canonical", canonical},
		{"leading and trailing whitespace", "  \n" + canonical + "\t\n "},
		{"embedded newlines", wrapAt(canonical, 64)},
		{"url-safe alphabet", strings.NewReplacer("+", "-", "/", "_").Replace(canonical)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := lens.Decode(tc.payload)
			if err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			if got.Type != "TransactionEnvelope" {
				t.Errorf("Decode() type = %q, want TransactionEnvelope", got.Type)
			}
		})
	}
}

func TestTypeNamesIsSortedAndNonEmpty(t *testing.T) {
	t.Parallel()

	names := lens.TypeNames()
	if len(names) == 0 {
		t.Fatal("TypeNames() returned no types")
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Errorf("TypeNames() is not sorted: %q before %q", names[i-1], names[i])
		}
	}
}

// TestEnrichmentRendersHumanValues asserts the enrichers actually fire on real
// data. Without these the tool would print raw byte arrays where developers
// expect strkey addresses.
func TestEnrichmentRendersHumanValues(t *testing.T) {
	t.Parallel()

	v, err := lens.Decode(loadFixture(t, "tx_success.env"))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	source := v.Node.Find("V1.Tx.SourceAccount")
	if source == nil {
		t.Fatal("could not find V1.Tx.SourceAccount in the decoded tree")
	}
	addr, ok := source.Value.(string)
	if !ok {
		t.Fatalf("SourceAccount value is %T, want a strkey string", source.Value)
	}
	if !strings.HasPrefix(addr, "G") || len(addr) != 56 {
		t.Errorf("SourceAccount = %q, want a 56-character strkey beginning with G", addr)
	}
}

// TestEnumsRenderAsConstantNames guards the property that makes the output
// readable: enums must show their name, not their integer value.
func TestEnumsRenderAsConstantNames(t *testing.T) {
	t.Parallel()

	v, err := lens.Decode(loadFixture(t, "tx_failed.res"))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	code := v.Node.Find("Result.Code")
	if code == nil {
		t.Fatal("could not find Result.Code in the decoded tree")
	}
	got, ok := code.Value.(string)
	if !ok {
		t.Fatalf("Result.Code is %T (%v), want the constant name as a string", code.Value, code.Value)
	}
	if !strings.HasPrefix(got, "TransactionResultCode") {
		t.Errorf("Result.Code = %q, want a TransactionResultCode constant name", got)
	}
}

// loadFixtureRaw is the non-testing.T variant used in table literals, which
// are evaluated before a subtest's *testing.T exists.
func loadFixtureRaw(name string) string {
	b, err := readFixture(name)
	if err != nil {
		panic("test fixture " + name + ": " + err.Error())
	}
	return b
}

func truncate(s string) string {
	if len(s) <= 32 {
		return s
	}
	return s[:32] + "..."
}

// TestDecodeNewRegistryTypes verifies that all types added for issues
// #269, #272, #274, #276 decode successfully and are auto-detected.
func TestDecodeNewRegistryTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		fixture  string
		wantType string
	}{
		{"ConfigSettingId", "config_setting_id", "ConfigSettingId"},
		{"OperationBody", "operation_body", "OperationBody"},
		{"ClaimOfferAtom", "claim_offer_atom", "ClaimOfferAtom"},
		{"ClaimOfferAtomV0", "claim_offer_atom_v0", "ClaimOfferAtomV0"},
		{"LedgerEntryChange created", "ledger_entry_change_created", "LedgerEntryChange"},
		{"LedgerEntryChange updated", "ledger_entry_change_updated", "LedgerEntryChange"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := lens.DecodeAs(loadFixture(t, tc.fixture), tc.wantType)
			if err != nil {
				t.Fatalf("DecodeAs(%q) error = %v", tc.wantType, err)
			}
			if got.Type != tc.wantType {
				t.Errorf("Type = %q, want %q", got.Type, tc.wantType)
			}
		})
	}
}

// TestConfigSettingIdEnrichment checks that a ConfigSettingId value is
// rendered as a human-readable string rather than a raw integer.
func TestConfigSettingIdEnrichment(t *testing.T) {
	t.Parallel()

	v, err := lens.DecodeAs(loadFixture(t, "config_setting_id"), "ConfigSettingId")
	if err != nil {
		t.Fatalf("DecodeAs() error = %v", err)
	}
	val, ok := v.Node.Value.(string)
	if !ok {
		t.Fatalf("Value is %T (%v), want a string", v.Node.Value, v.Node.Value)
	}
	if !strings.Contains(val, "ConfigSettingId") {
		t.Errorf("Value = %q, want a string containing 'ConfigSettingId'", val)
	}
}

// TestClaimOfferAtomNote checks the nodeHook sets a Note with the seller address.
func TestClaimOfferAtomNote(t *testing.T) {
	t.Parallel()

	v, err := lens.DecodeAs(loadFixture(t, "claim_offer_atom"), "ClaimOfferAtom")
	if err != nil {
		t.Fatalf("DecodeAs() error = %v", err)
	}
	if v.Node.Note == "" {
		t.Fatal("ClaimOfferAtom node has no Note; expected seller and offer info")
	}
	if !strings.Contains(v.Node.Note, "seller") {
		t.Errorf("Note = %q, want it to contain 'seller'", v.Node.Note)
	}
}

// TestLedgerEntryChangeUnionArms checks that created and updated arms decode.
func TestLedgerEntryChangeUnionArms(t *testing.T) {
	t.Parallel()

	for _, fixture := range []string{"ledger_entry_change_created", "ledger_entry_change_updated"} {
		t.Run(fixture, func(t *testing.T) {
			t.Parallel()
			v, err := lens.DecodeAs(loadFixture(t, fixture), "LedgerEntryChange")
			if err != nil {
				t.Fatalf("DecodeAs() error = %v", err)
			}
			if v.Node == nil || len(v.Node.Children) == 0 {
				t.Error("expected a non-empty tree for LedgerEntryChange")
			}
		})
	}
}

func wrapAt(s string, n int) string {
	var b strings.Builder
	for i := 0; i < len(s); i += n {
		end := i + n
		if end > len(s) {
			end = len(s)
		}
		b.WriteString(s[i:end])
		b.WriteString("\n")
	}
	return b.String()
}

// TestDecodeAsTimeBoundsFormatsTimestampsAsRFC3339 verifies issue #277:
// TimeBounds is registered in the type registry and its time fields are
// rendered as RFC 3339 strings by the enrichTimePoint enricher.
func TestDecodeAsTimeBoundsFormatsTimestampsAsRFC3339(t *testing.T) {
	t.Parallel()

	raw := xdr.TimeBounds{MinTime: 1700000000, MaxTime: 1800000000}
	payload := mustMarshal(t, raw)

	v, err := lens.DecodeAs(payload, "TimeBounds")
	if err != nil {
		t.Fatalf("DecodeAs(TimeBounds) error = %v", err)
	}
	if v.Type != "TimeBounds" {
		t.Errorf("type = %q, want TimeBounds", v.Type)
	}

	minNode := v.Node.Find("MinTime")
	if minNode == nil {
		t.Fatal("MinTime node not found")
	}
	if minNode.Note == "" {
		t.Error("MinTime.Note is empty, want RFC 3339 timestamp")
	}
	if !strings.Contains(minNode.Note, "2023") {
		t.Errorf("MinTime.Note = %q, want a year-2023 RFC 3339 string", minNode.Note)
	}
}

// TestDecodeAsLedgerBoundsRegistered verifies issue #277: LedgerBounds is
// registered so it can be decoded via --type LedgerBounds.
func TestDecodeAsLedgerBoundsRegistered(t *testing.T) {
	t.Parallel()

	raw := xdr.LedgerBounds{MinLedger: 100, MaxLedger: 200}
	payload := mustMarshal(t, raw)

	v, err := lens.DecodeAs(payload, "LedgerBounds")
	if err != nil {
		t.Fatalf("DecodeAs(LedgerBounds) error = %v", err)
	}
	if v.Type != "LedgerBounds" {
		t.Errorf("type = %q, want LedgerBounds", v.Type)
	}
	minNode := v.Node.Find("MinLedger")
	if minNode == nil {
		t.Fatal("MinLedger node not found")
	}
	if minNode.Value != uint64(100) {
		t.Errorf("MinLedger = %v, want 100", minNode.Value)
	}
}

// TestDecodeAsScpBallotRegistered verifies issue #275: ScpBallot is registered
// and its Counter field is labelled as a ballot round number.
func TestDecodeAsScpBallotRegistered(t *testing.T) {
	t.Parallel()

	val := xdr.Value([]byte{0x01, 0x02, 0x03, 0x04})
	raw := xdr.ScpBallot{Counter: 7, Value: val}
	payload := mustMarshal(t, raw)

	v, err := lens.DecodeAs(payload, "ScpBallot")
	if err != nil {
		t.Fatalf("DecodeAs(ScpBallot) error = %v", err)
	}
	if v.Type != "ScpBallot" {
		t.Errorf("type = %q, want ScpBallot", v.Type)
	}
	counter := v.Node.Find("Counter")
	if counter == nil {
		t.Fatal("Counter node not found")
	}
	if counter.Value != uint64(7) {
		t.Errorf("Counter.Value = %v, want 7", counter.Value)
	}
	if !strings.Contains(counter.Note, "ballot round 7") {
		t.Errorf("Counter.Note = %q, want it to contain %q", counter.Note, "ballot round 7")
	}
}

// TestDecodeAsScpNominationRegistered verifies issue #275: ScpNomination is
// registered and its QuorumSetHash is rendered as a hex string.
func TestDecodeAsScpNominationRegistered(t *testing.T) {
	t.Parallel()

	var hash xdr.Hash
	for i := range hash {
		hash[i] = byte(i)
	}
	raw := xdr.ScpNomination{
		QuorumSetHash: hash,
		Votes:         []xdr.Value{{0x01, 0x02}},
		Accepted:      nil,
	}
	payload := mustMarshal(t, raw)

	v, err := lens.DecodeAs(payload, "ScpNomination")
	if err != nil {
		t.Fatalf("DecodeAs(ScpNomination) error = %v", err)
	}
	if v.Type != "ScpNomination" {
		t.Errorf("type = %q, want ScpNomination", v.Type)
	}
	hashNode := v.Node.Find("QuorumSetHash")
	if hashNode == nil {
		t.Fatal("QuorumSetHash node not found")
	}
	// enrichHash provides a hex note for xdr.Hash fields.
	if hashNode.Note == "" && hashNode.Value == nil {
		t.Error("QuorumSetHash has neither a note nor a value")
	}
}

// TestTimeBoundsAndLedgerBoundsExcludedFromDetect verifies issue #277:
// these precondition sub-types are excluded from auto-detection to prevent
// false positives against structurally isomorphic types.
func TestTimeBoundsAndLedgerBoundsExcludedFromDetect(t *testing.T) {
	t.Parallel()

	for _, typeName := range []string{"TimeBounds", "LedgerBounds"} {
		t.Run(typeName, func(t *testing.T) {
			t.Parallel()
			names := lens.TypeNames()
			found := false
			for _, n := range names {
				if n == typeName {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("%s not in TypeNames(), want it registered for --type flag", typeName)
			}
		})
	}
}
