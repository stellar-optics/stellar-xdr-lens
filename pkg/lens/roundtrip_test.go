package lens_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

// roundTripFixtures lists every canonical fixture with the type it was captured
// as. Keeping the list explicit (rather than guessing from the filename) makes
// an ambiguous short payload unambiguous and keeps the suite deterministic.
func roundTripFixtures() []lens.RoundTripCase {
	fixtures := []struct {
		name     string
		fixture  string
		typeName string
	}{
		{"account extension v1", "account_entry_ext_v1", "AccountEntry"},
		{"account extension v2", "account_entry_ext_v2", "AccountEntry"},
		{"account extension v3", "account_entry_ext_v3", "AccountEntry"},
		{"asset native", "asset_native", "Asset"},
		{"asset usdc", "asset_usdc", "Asset"},
		{"bucket entry dead", "bucket_entry_dead", "BucketEntry"},
		{"bucket entry live", "bucket_entry_live", "BucketEntry"},
		{"bucket entry meta", "bucket_entry_meta", "BucketEntry"},
		{"bucket metadata", "bucket_metadata", "BucketMetadata"},
		{"claimable balance", "claimable_balance_entry", "ClaimableBalanceEntry"},
		{"claimable balance simple", "claimable_balance_entry_simple", "ClaimableBalanceEntry"},
		{"config setting 0", "config_setting_0", "ConfigSettingEntry"},
		{"config setting 1", "config_setting_1", "ConfigSettingEntry"},
		{"config setting 2", "config_setting_2", "ConfigSettingEntry"},
		{"config setting 3", "config_setting_3", "ConfigSettingEntry"},
		{"config setting 4", "config_setting_4", "ConfigSettingEntry"},
		{"config setting 5", "config_setting_5", "ConfigSettingEntry"},
		{"config setting 6", "config_setting_6", "ConfigSettingEntry"},
		{"config setting 7", "config_setting_7", "ConfigSettingEntry"},
		{"config setting 8", "config_setting_8", "ConfigSettingEntry"},
		{"config setting 9", "config_setting_9", "ConfigSettingEntry"},
		{"config setting 10", "config_setting_10", "ConfigSettingEntry"},
		{"config setting 11", "config_setting_11", "ConfigSettingEntry"},
		{"config setting 12", "config_setting_12", "ConfigSettingEntry"},
		{"config setting 13", "config_setting_13", "ConfigSettingEntry"},
		{"config setting 14", "config_setting_14", "ConfigSettingEntry"},
		{"config setting 15", "config_setting_15", "ConfigSettingEntry"},
		{"config setting 16", "config_setting_16", "ConfigSettingEntry"},
		{"config setting 17", "config_setting_17", "ConfigSettingEntry"},
		{"config setting 19", "config_setting_19", "ConfigSettingEntry"},
		{"contract code", "contract_code", "ContractCodeEntry"},
		{"contract code v1", "contract_code_v1", "ContractCodeEntry"},
		{"contract data instance", "contract_data_instance", "ContractDataEntry"},
		{"contract data persistent", "contract_data_persistent", "ContractDataEntry"},
		{"contract data temporary", "contract_data_temporary", "ContractDataEntry"},
		{"data entry binary", "data_entry_binary", "DataEntry"},
		{"data entry text", "data_entry_text", "DataEntry"},
		{"diagnostic event", "diagnostic_event", "DiagnosticEvent"},
		{"eviction iterator", "eviction_iterator", "EvictionIterator"},
		{"generalized tx set", "generalized_tx_set", "GeneralizedTransactionSet"},
		{"hot archive bucket entry", "hot_archive_bucket_entry", "HotArchiveBucketEntry"},
		{"ledger close meta batch", "ledger_close_meta_batch", "LedgerCloseMetaBatch"},
		{"ledger entry contract data", "ledger_entry_contract_data", "LedgerEntry"},
		{"ledger entry offer", "ledger_entry_offer", "LedgerEntry"},
		{"ledger entry ttl", "ledger_entry_ttl", "LedgerEntry"},
		{"ledger header", "ledger_header", "LedgerHeader"},
		{"ledger header history", "ledger_header_history", "LedgerHeaderHistoryEntry"},
		{"ledger key ttl", "ledger_key_ttl", "LedgerKeyTtl"},
		{"ledger upgrade base fee", "ledger_upgrade_base_fee", "LedgerUpgrade"},
		{"ledger upgrade base reserve", "ledger_upgrade_base_reserve", "LedgerUpgrade"},
		{"ledger upgrade version", "ledger_upgrade_version", "LedgerUpgrade"},
		{"liquidity pool", "liquidity_pool_entry", "LiquidityPoolEntry"},
		{"offer entry", "offer_entry", "OfferEntry"},
		{"overlay auth", "overlay_auth", "Auth"},
		{"overlay auth cert", "overlay_auth_cert", "AuthCert"},
		{"overlay dont have", "overlay_dont_have", "DontHave"},
		{"overlay error", "overlay_error", "Error"},
		{"overlay hello", "overlay_hello", "Hello"},
		{"overlay peer address", "overlay_peer_address", "PeerAddress"},
		{"overlay peer address v6", "overlay_peer_address_v6", "PeerAddress"},
		{"overlay peers", "overlay_peers", "StellarMessage"},
		{"overlay stellar message", "overlay_stellar_message", "StellarMessage"},
		{"price", "price", "Price"},
		{"price zero", "price_zero", "Price"},
		{"scp envelope", "scp_envelope", "ScpEnvelope"},
		{"scp quorum set", "scp_quorum_set", "ScpQuorumSet"},
		{"scp statement", "scp_statement", "ScpStatement"},
		{"stellar value votes", "stellar_value_votes", "StellarValue"},
		{"survey request", "survey_request", "SignedTimeSlicedSurveyRequestMessage"},
		{"survey response", "survey_response", "SignedTimeSlicedSurveyResponseMessage"},
		{"survey start", "survey_start", "SignedTimeSlicedSurveyStartCollectingMessage"},
		{"survey stop", "survey_stop", "SignedTimeSlicedSurveyStopCollectingMessage"},
		{"trustline simple", "trustline_entry_simple", "TrustLineEntry"},
		{"trustline v1", "trustline_entry_v1", "TrustLineEntry"},
		{"trustline v2", "trustline_entry_v2", "TrustLineEntry"},
		{"ttl entry", "ttl_entry", "TtlEntry"},
		{"tx failed envelope", "tx_failed.env", "TransactionEnvelope"},
		{"tx failed result", "tx_failed.res", "TransactionResult"},
		{"tx feebump envelope", "tx_feebump.env", "TransactionEnvelope"},
		{"tx feebump result", "tx_feebump.res", "TransactionResult"},
		{"tx history entry v0", "tx_history_entry_v0", "TransactionHistoryEntry"},
		{"tx history entry v1", "tx_history_entry_v1", "TransactionHistoryEntry"},
		{"tx history result entry", "tx_history_result_entry", "TransactionHistoryResultEntry"},
		{"tx soroban envelope", "tx_soroban.env", "TransactionEnvelope"},
		{"tx soroban result", "tx_soroban.res", "TransactionResult"},
		{"tx success envelope", "tx_success.env", "TransactionEnvelope"},
		{"tx success result", "tx_success.res", "TransactionResult"},
	}

	cases := make([]lens.RoundTripCase, 0, len(fixtures))
	for _, f := range fixtures {
		payload, err := readFixture(f.fixture)
		if err != nil {
			// The suite is built during the test; a missing fixture is a
			// hard failure surfaced by the test body, but we keep the case so
			// its name appears in the report.
			payload = ""
		}
		cases = append(cases, lens.RoundTripCase{
			Name:    f.name,
			Type:    f.typeName,
			Payload: payload,
		})
	}
	return cases
}

// jsonLossyFixtures are canonical fixtures whose enriched JSON rendering
// deliberately drops information, so the JSON route either skips them or
// produces a different value. They are still exercised: the test asserts the
// documented behavior rather than silently excluding them.
var jsonLossyFixtures = map[string]string{
	"claimable balance":        "claim predicate renders as an expression",
	"claimable balance simple": "claim predicate renders as an expression",
	"overlay peer address v6":  "peer failure counters are carried as notes",
	"overlay peers":            "peer failure counters are carried as notes",
}

func TestRoundTripFixturesBinary(t *testing.T) {
	t.Parallel()

	suite := lens.RunRoundTripSuite(lens.RoundTripBinary, roundTripFixtures())
	t.Log(suite.Summary())
	for _, report := range suite.Reports {
		if !report.OK() {
			t.Errorf("%s", report)
			for _, change := range report.Changes {
				t.Errorf("  %s", change)
			}
		}
	}
}

func TestRoundTripFixturesJSON(t *testing.T) {
	t.Parallel()

	cases := roundTripFixtures()
	suite := lens.RunRoundTripSuite(lens.RoundTripJSON, cases)
	t.Log(suite.Summary())

	byName := make(map[string]*lens.RoundTripReport, len(suite.Reports))
	for _, report := range suite.Reports {
		byName[report.Name] = report
	}

	for _, report := range suite.Reports {
		reason, lossy := jsonLossyFixtures[report.Name]
		switch {
		case !lossy && !report.OK():
			t.Errorf("%s", report)
			for _, change := range report.Changes {
				t.Errorf("  %s", change)
			}
		case lossy && report.OK():
			t.Errorf("%s: expected the documented loss (%s) but the trip passed", report, reason)
		}
	}

	// The claim predicate is rejected as unsupported instead of inventing an
	// expression. Check the classification explicitly.
	if report := byName["claimable balance"]; report == nil || !report.Skipped {
		t.Errorf("claimable balance JSON trip = %v, want a skipped (unsupported) report", report)
	} else if !strings.Contains(report.SkipReason, "ClaimPredicate") &&
		!strings.Contains(report.SkipReason, "claim predicate") {
		t.Errorf("skip reason = %q, want it to name the claim predicate", report.SkipReason)
	}
}

func TestRoundtripAll(t *testing.T) {
	t.Parallel()

	cases := manifestRoundTripFixtures(t)
	suite := lens.RunRoundTripSuite(lens.RoundTripJSON, cases)
	t.Log(suite.Summary())

	knownLossy := map[string]string{
		"claimable_balance_entry":        "claim predicate renders as an expression",
		"claimable_balance_entry_simple": "claim predicate renders as an expression",
		"overlay_peer_address_v6":        "peer failure counters are carried as notes",
		"overlay_peers":                  "peer failure counters are carried as notes",
	}
	for _, report := range suite.Reports {
		if reason, lossy := knownLossy[report.Name]; lossy {
			if report.OK() {
				t.Errorf("%s: expected the documented JSON loss (%s) but the trip passed", report, reason)
			}
			if report.Err != nil && !report.Skipped {
				t.Errorf("%s: unexpected round-trip error for documented JSON loss", report)
			}
			continue
		}
		if !report.OK() {
			t.Errorf("%s", report)
			for _, change := range report.Changes {
				t.Errorf("  %s", change)
			}
		}
	}
}

func manifestRoundTripFixtures(t *testing.T) []lens.RoundTripCase {
	t.Helper()

	type fixtureInfo struct {
		Type         string `json:"type"`
		ResultType   string `json:"result_type"`
		EnvelopeFile string `json:"envelope_file"`
		ResultFile   string `json:"result_file"`
	}
	var manifest struct {
		Fixtures map[string]fixtureInfo `json:"fixtures"`
		SyntheticFixtures struct {
			Fixtures map[string]struct {
				Type string `json:"type"`
			} `json:"fixtures"`
		} `json:"synthetic_fixtures"`
	}
	data, err := os.ReadFile(filepath.Join(fixtureDir, "manifest.json"))
	if err != nil {
		t.Fatalf("reading fixture manifest: %v", err)
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("decoding fixture manifest: %v", err)
	}

	byFile := make(map[string]lens.RoundTripCase)
	add := func(file, typeName string) {
		t.Helper()
		if file == "" {
			return
		}
		if filepath.Base(file) != file || filepath.Ext(file) != ".txt" {
			t.Fatalf("manifest fixture path %q must be a .txt file in testdata", file)
		}
		if typeName == "" {
			t.Fatalf("manifest fixture %q has no registered XDR type", file)
		}
		if previous, exists := byFile[file]; exists {
			if previous.Type != typeName {
				t.Fatalf("manifest fixture %q has conflicting types %q and %q", file, previous.Type, typeName)
			}
			return
		}
		name := strings.TrimSuffix(file, ".txt")
		payload, err := readFixture(name)
		if err != nil {
			t.Fatalf("loading manifest fixture %q: %v", file, err)
		}
		byFile[file] = lens.RoundTripCase{Name: name, Type: typeName, Payload: payload}
	}

	for name, fixture := range manifest.SyntheticFixtures.Fixtures {
		add(name+".txt", fixture.Type)
	}
	fixtureNames := make([]string, 0, len(manifest.Fixtures))
	for name := range manifest.Fixtures {
		fixtureNames = append(fixtureNames, name)
	}
	sort.Strings(fixtureNames)
	for _, name := range fixtureNames {
		fixture := manifest.Fixtures[name]
		add(fixture.EnvelopeFile, fixture.Type)
		add(fixture.ResultFile, fixture.ResultType)
	}

	entries, err := os.ReadDir(fixtureDir)
	if err != nil {
		t.Fatalf("reading testdata directory: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".txt" {
			continue
		}
		if _, ok := byFile[entry.Name()]; !ok {
			t.Errorf("testdata fixture %q is missing from manifest type registration", entry.Name())
		}
	}

	files := make([]string, 0, len(byFile))
	for file := range byFile {
		files = append(files, file)
	}
	sort.Strings(files)
	cases := make([]lens.RoundTripCase, 0, len(files))
	for _, file := range files {
		cases = append(cases, byFile[file])
	}
	return cases
}

func TestRoundTripBase64ReproducesFixture(t *testing.T) {
	t.Parallel()

	payload := loadFixture(t, "tx_success.env")
	report, err := lens.RoundTripBase64(payload, "TransactionEnvelope")
	if err != nil {
		t.Fatalf("RoundTripBase64() error = %v", err)
	}
	if !report.OK() {
		t.Fatalf("report = %s", report)
	}
	if report.Original != report.Reencoded {
		t.Error("re-encoded XDR differs from the fixture")
	}
	if report.Type != "TransactionEnvelope" {
		t.Errorf("Type = %q, want TransactionEnvelope", report.Type)
	}
}

func TestRoundTripBase64DetectsType(t *testing.T) {
	t.Parallel()

	payload := loadFixture(t, "ledger_header")
	report, err := lens.RoundTripBase64(payload, "")
	if err != nil {
		t.Fatalf("RoundTripBase64() error = %v", err)
	}
	if !report.OK() {
		t.Fatalf("report = %s", report)
	}
	if report.Type != "LedgerHeader" {
		t.Errorf("detected Type = %q, want LedgerHeader", report.Type)
	}
}

func TestRoundTripRouteRejectsUnknown(t *testing.T) {
	t.Parallel()

	payload := loadFixture(t, "price")
	_, err := lens.RoundTripRouteBase64(payload, "Price", lens.RoundTripRoute("nonsense"))
	if err == nil {
		t.Fatal("RoundTripRouteBase64(unknown route) error = nil, want an error")
	}
	if !errors.Is(err, lens.ErrInvalidArgument) {
		t.Errorf("error = %v, want ErrInvalidArgument", err)
	}
}

func TestRoundTripRequiresValue(t *testing.T) {
	t.Parallel()

	if _, err := lens.RoundTripValue(nil); !errors.Is(err, lens.ErrInvalidArgument) {
		t.Errorf("RoundTripValue(nil) error = %v, want ErrInvalidArgument", err)
	}
	if _, err := lens.RoundTripValue(&lens.Value{Type: "Price"}); !errors.Is(err, lens.ErrInvalidArgument) {
		t.Errorf("RoundTripValue(no raw) error = %v, want ErrInvalidArgument", err)
	}
	if _, err := lens.RoundTripJSONValue(nil); !errors.Is(err, lens.ErrInvalidArgument) {
		t.Errorf("RoundTripJSONValue(nil) error = %v, want ErrInvalidArgument", err)
	}
}

func TestRoundTripValueUsesBinaryRoute(t *testing.T) {
	t.Parallel()

	payload := loadFixture(t, "price")
	v, err := lens.DecodeAs(payload, "Price")
	if err != nil {
		t.Fatalf("DecodeAs() error = %v", err)
	}
	report, err := lens.RoundTripValue(v)
	if err != nil {
		t.Fatalf("RoundTripValue() error = %v", err)
	}
	if !report.OK() {
		t.Errorf("report = %s", report)
	}
	if report.Route != lens.RoundTripBinary {
		t.Errorf("Route = %q, want %q", report.Route, lens.RoundTripBinary)
	}
}

func TestRoundTripJSONValueClassifiesLossyValue(t *testing.T) {
	t.Parallel()

	payload := loadFixture(t, "claimable_balance_entry_simple")
	v, err := lens.DecodeAs(payload, "ClaimableBalanceEntry")
	if err != nil {
		t.Fatalf("DecodeAs() error = %v", err)
	}
	report, err := lens.RoundTripJSONValue(v)
	if err != nil {
		t.Fatalf("RoundTripJSONValue() error = %v", err)
	}
	if !report.Skipped {
		t.Fatalf("report = %s, want a skip", report)
	}
	if report.OK() {
		t.Error("a skipped report must not report OK")
	}
}

func TestRoundTripSuiteRecordsDecodeFailures(t *testing.T) {
	t.Parallel()

	suite := lens.RunRoundTripSuite(lens.RoundTripBinary, []lens.RoundTripCase{
		{Name: "broken", Type: "Price", Payload: "not base64"},
		{Name: "good", Type: "Price", Payload: loadFixture(t, "price")},
	})
	if suite.OK() {
		t.Fatal("suite.OK() = true, want false")
	}
	if len(suite.Failures()) != 1 {
		t.Fatalf("Failures() = %d, want 1", len(suite.Failures()))
	}
	if suite.Failures()[0].Name != "broken" {
		t.Errorf("failure name = %q, want broken", suite.Failures()[0].Name)
	}
	if !strings.Contains(suite.Summary(), "1 failed") {
		t.Errorf("Summary() = %q, want it to count the failure", suite.Summary())
	}
}

func TestRoundTripRegistryBinary(t *testing.T) {
	t.Parallel()

	suite := lens.RoundTripRegistry(lens.RoundTripBinary)
	t.Log(suite.Summary())

	for _, report := range suite.Reports {
		if !report.OK() && !report.Skipped {
			t.Errorf("%s", report)
			for _, change := range report.Changes {
				t.Errorf("  %s", change)
			}
		}
	}

	// The synthesized minimal value should be encodable for the large
	// majority of the registry; a regression that stops filling union arms
	// would show up as a wave of skips.
	passed := 0
	for _, report := range suite.Reports {
		if report.OK() {
			passed++
		}
	}
	if passed < len(suite.Reports)*3/4 {
		t.Errorf("only %d of %d registered types round-tripped", passed, len(suite.Reports))
	}
	if len(suite.Reports) != len(lens.TypeNames()) {
		t.Errorf("suite covered %d types, registry has %d", len(suite.Reports), len(lens.TypeNames()))
	}
}

func TestRoundTripRegistryJSON(t *testing.T) {
	t.Parallel()

	suite := lens.RoundTripRegistry(lens.RoundTripJSON)
	t.Log(suite.Summary())

	for _, report := range suite.Reports {
		if !report.OK() && !report.Skipped {
			t.Errorf("%s", report)
			for _, change := range report.Changes {
				t.Errorf("  %s", change)
			}
		}
	}

	// Lossy enrichments are expected to be skipped, but they must be a small
	// minority: most registered types have a reversible JSON form.
	if skipped := len(suite.Skipped()); skipped > len(suite.Reports)/4 {
		t.Errorf("%d of %d types were skipped; want the lossy set to stay small",
			skipped, len(suite.Reports))
	}
}

func TestRoundTripReportString(t *testing.T) {
	t.Parallel()

	var nilReport *lens.RoundTripReport
	if got := nilReport.String(); !strings.Contains(got, "<nil>") {
		t.Errorf("nil report = %q", got)
	}

	ok := &lens.RoundTripReport{Name: "price", Type: "Price", Route: lens.RoundTripBinary,
		Original: "AAAAAA==", Reencoded: "AAAAAA==", BytesEqual: true, TreeEqual: true}
	if got := ok.String(); !strings.Contains(got, "ok") {
		t.Errorf("ok report = %q", got)
	}
	if !ok.OK() {
		t.Error("OK() = false for a passing report")
	}

	skipped := &lens.RoundTripReport{Name: "claim", Type: "ClaimPredicate", Route: lens.RoundTripJSON,
		Skipped: true, SkipReason: "lossy"}
	if got := skipped.String(); !strings.Contains(got, "skipped") || !strings.Contains(got, "lossy") {
		t.Errorf("skipped report = %q", got)
	}
	if skipped.OK() {
		t.Error("OK() = true for a skipped report")
	}

	failed := &lens.RoundTripReport{Name: "bad", Type: "Price", Route: lens.RoundTripJSON,
		Err: errors.New("boom")}
	if got := failed.String(); !strings.Contains(got, "boom") {
		t.Errorf("failed report = %q", got)
	}

	mismatch := &lens.RoundTripReport{Name: "diff", Type: "Price", Route: lens.RoundTripJSON,
		BytesEqual: false, TreeEqual: false}
	if got := mismatch.String(); !strings.Contains(got, "mismatch") {
		t.Errorf("mismatch report = %q", got)
	}
}
