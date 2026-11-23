package lens_test

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"

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

// TestBase64Diagnostics pins the byte offset and reason for each way base64
// decoding can fail, so a truncated paste points at the offending byte rather
// than producing an opaque "illegal base64 data" message.
func TestBase64Diagnostics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		payload      string
		wantReason   lens.DiagnosticReason
		wantOffset   int
		wantSentinel error
	}{
		{"illegal first byte", "!AAA", lens.ReasonIllegalCharacter, 0, lens.ErrInvalidBase64},
		{"illegal middle byte", "AAAA!AAA", lens.ReasonIllegalCharacter, 4, lens.ErrInvalidBase64},
		{"missing padding", "AAAAAA", lens.ReasonPadding, 4, lens.ErrInvalidBase64},
		{"extra padding", "AAAAAA===", lens.ReasonPadding, 8, lens.ErrInvalidBase64},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := lens.DecodeAs(tc.payload, "Memo")
			if err == nil {
				t.Fatalf("DecodeAs(%q) error = nil, want a diagnostic", tc.payload)
			}
			if !errors.Is(err, tc.wantSentinel) {
				t.Errorf("error = %v, want it to wrap %v", err, tc.wantSentinel)
			}

			var diag *lens.DecodeDiagnostic
			if !errors.As(err, &diag) {
				t.Fatalf("error = %T (%v), want *lens.DecodeDiagnostic", err, err)
			}
			if diag.Stage != lens.StageBase64 {
				t.Errorf("Stage = %q, want %q", diag.Stage, lens.StageBase64)
			}
			if diag.Reason != tc.wantReason {
				t.Errorf("Reason = %q, want %q", diag.Reason, tc.wantReason)
			}
			if diag.Offset != tc.wantOffset {
				t.Errorf("Offset = %d, want %d", diag.Offset, tc.wantOffset)
			}
			if diag.Total != len(tc.payload) {
				t.Errorf("Total = %d, want %d", diag.Total, len(tc.payload))
			}
			if !strings.Contains(err.Error(), "byte offset") {
				t.Errorf("message %q does not mention a byte offset", err)
			}
		})
	}
}

// truncatedEnvelope returns the transaction envelope fixture with its decoded
// bytes cut to n, re-encoded so the failure happens in XDR, not base64.
func truncatedEnvelope(t *testing.T, n int) string {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(loadFixture(t, "tx_success.env"))
	if err != nil {
		t.Fatalf("decoding fixture base64: %v", err)
	}
	if n >= len(raw) {
		t.Fatalf("n=%d must be smaller than the fixture (%d bytes)", n, len(raw))
	}
	return base64.StdEncoding.EncodeToString(raw[:n])
}

// TestTruncatedXDRDiagnostic checks that the offset is the end of the decoded
// payload, which is where a truncated structure runs out of bytes.
func TestTruncatedXDRDiagnostic(t *testing.T) {
	t.Parallel()

	const cut = 40
	payload := truncatedEnvelope(t, cut)

	_, err := lens.DecodeAs(payload, "TransactionEnvelope")
	if err == nil {
		t.Fatal("DecodeAs() error = nil, want a truncated-XDR diagnostic")
	}
	if !errors.Is(err, lens.ErrInvalidXDR) {
		t.Errorf("error = %v, want it to wrap ErrInvalidXDR", err)
	}

	var diag *lens.DecodeDiagnostic
	if !errors.As(err, &diag) {
		t.Fatalf("error = %T (%v), want *lens.DecodeDiagnostic", err, err)
	}
	if diag.Stage != lens.StageXDR {
		t.Errorf("Stage = %q, want %q", diag.Stage, lens.StageXDR)
	}
	if diag.Reason != lens.ReasonTruncated {
		t.Errorf("Reason = %q, want %q", diag.Reason, lens.ReasonTruncated)
	}
	if diag.Offset != cut {
		t.Errorf("Offset = %d, want %d (the end of the payload)", diag.Offset, cut)
	}
	if diag.Total != cut {
		t.Errorf("Total = %d, want %d", diag.Total, cut)
	}
	if diag.TypeName != "TransactionEnvelope" {
		t.Errorf("TypeName = %q, want TransactionEnvelope", diag.TypeName)
	}
}

// TestTrailingBytesDiagnostic covers the strict-consumption rule that makes
// auto-detection reliable: extra bytes are an error, not a silent prefix.
func TestTrailingBytesDiagnostic(t *testing.T) {
	t.Parallel()

	raw, err := base64.StdEncoding.DecodeString(loadFixture(t, "tx_success.env"))
	if err != nil {
		t.Fatalf("decoding fixture base64: %v", err)
	}
	payload := base64.StdEncoding.EncodeToString(append(raw, 0x00))

	_, err = lens.DecodeAs(payload, "TransactionEnvelope")
	if err == nil {
		t.Fatal("DecodeAs() error = nil, want a trailing-bytes diagnostic")
	}

	var diag *lens.DecodeDiagnostic
	if !errors.As(err, &diag) {
		t.Fatalf("error = %T (%v), want *lens.DecodeDiagnostic", err, err)
	}
	if diag.Reason != lens.ReasonTrailingBytes {
		t.Errorf("Reason = %q, want %q", diag.Reason, lens.ReasonTrailingBytes)
	}
	if diag.Offset != len(raw) {
		t.Errorf("Offset = %d, want %d", diag.Offset, len(raw))
	}
}

// TestDetectCarriesClosestDiagnostic keeps ErrNoMatch intact while still
// explaining where the most plausible type stopped decoding.
func TestDetectCarriesClosestDiagnostic(t *testing.T) {
	t.Parallel()

	_, err := lens.Decode(truncatedEnvelope(t, 40))
	if err == nil {
		t.Fatal("Decode() error = nil, want an error")
	}
	if !errors.Is(err, lens.ErrNoMatch) {
		t.Errorf("error = %v, want it to wrap ErrNoMatch", err)
	}

	var diag *lens.DecodeDiagnostic
	if !errors.As(err, &diag) {
		t.Fatalf("error = %T (%v), want it to carry a *lens.DecodeDiagnostic", err, err)
	}
	if diag.Offset <= 0 {
		t.Errorf("Offset = %d, want a positive failure offset", diag.Offset)
	}

	if _, err := lens.Detect("!!!not base64!!!"); !errors.Is(err, lens.ErrInvalidBase64) {
		t.Errorf("Detect(invalid base64) error = %v, want ErrInvalidBase64", err)
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()

	t.Run("valid payload detects its type", func(t *testing.T) {
		t.Parallel()

		got, err := lens.Validate(loadFixture(t, "tx_success.env"), "")
		if err != nil {
			t.Fatalf("Validate() error = %v", err)
		}
		if got.Type != "TransactionEnvelope" {
			t.Errorf("Type = %q, want TransactionEnvelope", got.Type)
		}
		if got.Bytes == 0 {
			t.Error("Bytes = 0, want the decoded payload size")
		}
	})

	t.Run("explicit type", func(t *testing.T) {
		t.Parallel()

		got, err := lens.Validate(loadFixture(t, "tx_success.res"), "TransactionResult")
		if err != nil {
			t.Fatalf("Validate() error = %v", err)
		}
		if got.Type != "TransactionResult" {
			t.Errorf("Type = %q, want TransactionResult", got.Type)
		}
	})

	t.Run("truncated payload reports an offset", func(t *testing.T) {
		t.Parallel()

		_, err := lens.Validate(truncatedEnvelope(t, 40), "TransactionEnvelope")
		var diag *lens.DecodeDiagnostic
		if !errors.As(err, &diag) {
			t.Fatalf("error = %T (%v), want *lens.DecodeDiagnostic", err, err)
		}
		if diag.Reason != lens.ReasonTruncated || diag.Offset != 40 {
			t.Errorf("diagnostic = %+v, want truncated at offset 40", diag)
		}
	})

	t.Run("invalid base64", func(t *testing.T) {
		t.Parallel()

		_, err := lens.Validate("!!!not base64!!!", "")
		if !errors.Is(err, lens.ErrInvalidBase64) {
			t.Errorf("error = %v, want ErrInvalidBase64", err)
		}
	})

	t.Run("unknown type", func(t *testing.T) {
		t.Parallel()

		_, err := lens.Validate("AAAAAA==", "NoSuchType")
		if !errors.Is(err, lens.ErrUnknownType) {
			t.Errorf("error = %v, want ErrUnknownType", err)
		}
	})

	t.Run("empty input", func(t *testing.T) {
		t.Parallel()

		_, err := lens.Validate("   ", "")
		if !errors.Is(err, lens.ErrEmptyInput) {
			t.Errorf("error = %v, want ErrEmptyInput", err)
		}
	})
}

// BenchmarkDecode and BenchmarkValidate quantify the fast path used by
// `lens decode --validate-only`, which deliberately skips the Node tree.
func BenchmarkDecode(b *testing.B) {
	payload := loadFixtureRaw("tx_success.env")
	b.ReportAllocs()
	for b.Loop() {
		if _, err := lens.Decode(payload); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkValidate(b *testing.B) {
	payload := loadFixtureRaw("tx_success.env")
	b.ReportAllocs()
	for b.Loop() {
		if _, err := lens.Validate(payload, "TransactionEnvelope"); err != nil {
			b.Fatal(err)
		}
	}
}
