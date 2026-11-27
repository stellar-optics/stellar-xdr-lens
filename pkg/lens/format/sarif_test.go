package format_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens/format"
)

// validateSARIF performs a structural validation of a SARIF 2.1.0 document
// against the requirements GitHub Code Scanning enforces: the version and
// schema markers, a run with a named driver, well-formed rules, and results
// whose rule ids resolve to those rules. It intentionally avoids a network
// fetch of the published JSON schema so the test stays offline.
func validateSARIF(t *testing.T, body []byte) map[string]any {
	t.Helper()

	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, body)
	}

	if got, want := doc["version"], "2.1.0"; got != want {
		t.Errorf("version = %v, want %q", got, want)
	}
	schema, _ := doc["$schema"].(string)
	if !strings.Contains(schema, "sarif") {
		t.Errorf("$schema = %q, want a SARIF schema URL", schema)
	}

	runs, ok := doc["runs"].([]any)
	if !ok || len(runs) == 0 {
		t.Fatalf("runs missing or empty: %v", doc["runs"])
	}
	run := runs[0].(map[string]any)

	tool := run["tool"].(map[string]any)
	driver := tool["driver"].(map[string]any)
	if name, _ := driver["name"].(string); name == "" {
		t.Error("tool.driver.name is required")
	}

	ruleIDs := map[string]bool{}
	ruleIndexes := map[float64]bool{}
	if rawRules, ok := driver["rules"].([]any); ok {
		for i, raw := range rawRules {
			rule := raw.(map[string]any)
			id, _ := rule["id"].(string)
			if id == "" {
				t.Errorf("rule %d has no id", i)
				continue
			}
			ruleIDs[id] = true
			ruleIndexes[float64(i)] = true

			short, _ := rule["shortDescription"].(map[string]any)
			if text, _ := short["text"].(string); text == "" {
				t.Errorf("rule %q has no shortDescription.text", id)
			}
			conf, _ := rule["defaultConfiguration"].(map[string]any)
			if level, _ := conf["level"].(string); !validSARIFLevel(level) {
				t.Errorf("rule %q has invalid defaultConfiguration.level %q", id, level)
			}
		}
	}

	results, _ := run["results"].([]any)
	for i, raw := range results {
		result := raw.(map[string]any)
		id, _ := result["ruleId"].(string)
		if id == "" {
			t.Errorf("result %d has no ruleId", i)
		} else if !ruleIDs[id] {
			t.Errorf("result %d references unknown rule %q", i, id)
		}
		if level, _ := result["level"].(string); !validSARIFLevel(level) {
			t.Errorf("result %d has invalid level %q", i, level)
		}
		msg, _ := result["message"].(map[string]any)
		if text, _ := msg["text"].(string); text == "" {
			t.Errorf("result %d has no message.text", i)
		}
		for _, rawLoc := range asSlice(result["locations"]) {
			loc := rawLoc.(map[string]any)
			locs := asSlice(loc["logicalLocations"])
			if len(locs) == 0 {
				t.Errorf("result %d location has no logicalLocations", i)
			}
			for _, rawLL := range locs {
				ll := rawLL.(map[string]any)
				if name, _ := ll["name"].(string); name == "" {
					t.Errorf("result %d logicalLocation has no name", i)
				}
			}
		}
		if rawIdx, ok := result["ruleIndex"]; ok {
			idx := rawIdx.(float64)
			if !ruleIndexes[idx] {
				t.Errorf("result %d references out-of-range rule index %v", i, idx)
			}
		}
	}

	return doc
}

func asSlice(v any) []any {
	s, _ := v.([]any)
	return s
}

func validSARIFLevel(level string) bool {
	switch level {
	case "error", "warning", "note", "none":
		return true
	}
	return false
}

func sarifResultRules(t *testing.T, doc map[string]any) []string {
	t.Helper()
	runs := doc["runs"].([]any)
	results := runs[0].(map[string]any)["results"].([]any)
	ids := make([]string, 0, len(results))
	for _, raw := range results {
		ids = append(ids, raw.(map[string]any)["ruleId"].(string))
	}
	return ids
}

func TestSARIFFormatterFailedTransaction(t *testing.T) {
	t.Parallel()

	env := loadValue(t, "tx_failed.env")
	res := loadValue(t, "tx_failed.res")
	summary, err := lens.ExplainPair(env, res)
	if err != nil {
		t.Fatalf("ExplainPair: %v", err)
	}

	var buf bytes.Buffer
	if err := format.NewSARIFFormatter().FormatSummary(&buf, summary); err != nil {
		t.Fatalf("FormatSummary: %v", err)
	}
	doc := validateSARIF(t, buf.Bytes())

	ids := sarifResultRules(t, doc)
	for _, want := range []string{"tx_fee_bump_inner_failed", "tx_failed", "invoke_host_function_trapped"} {
		if !contains(ids, want) {
			t.Errorf("missing SARIF result for %q; got %v", want, ids)
		}
	}

	runs := doc["runs"].([]any)
	results := runs[0].(map[string]any)["results"].([]any)
	for _, raw := range results {
		result := raw.(map[string]any)
		if level := result["level"]; level != "error" {
			t.Errorf("result %v has level %v, want error", result["ruleId"], level)
		}
	}
}

func TestSARIFFormatterSuccessfulTransactionHasNoFindings(t *testing.T) {
	t.Parallel()

	v := loadValue(t, "tx_success.env")
	var buf bytes.Buffer
	if err := format.NewSARIFFormatter().Format(&buf, v); err != nil {
		t.Fatalf("Format: %v", err)
	}
	doc := validateSARIF(t, buf.Bytes())

	if ids := sarifResultRules(t, doc); len(ids) != 0 {
		t.Errorf("successful transaction produced findings: %v", ids)
	}
}

func TestSARIFFormatterWarningsUseWarningSeverity(t *testing.T) {
	t.Parallel()

	summary := &lens.Summary{
		Kind:     "transaction",
		Headline: "ok",
		Outcome:  &lens.Outcome{Success: true},
		Warnings: []string{"trustline is frozen", "memo is unusually long"},
	}

	var buf bytes.Buffer
	if err := format.NewSARIFFormatter().FormatSummary(&buf, summary); err != nil {
		t.Fatalf("FormatSummary: %v", err)
	}
	doc := validateSARIF(t, buf.Bytes())

	runs := doc["runs"].([]any)
	results := runs[0].(map[string]any)["results"].([]any)
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	for _, raw := range results {
		result := raw.(map[string]any)
		if result["ruleId"] != "lens_warning" || result["level"] != "warning" {
			t.Errorf("warning result = %v, want lens_warning at warning", result)
		}
	}
}

func TestSARIFFormatterNonTransactionValueIsValid(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := format.NewSARIFFormatter().Format(&buf, loadValue(t, "trustline_entry_simple")); err != nil {
		t.Fatalf("Format: %v", err)
	}
	doc := validateSARIF(t, buf.Bytes())
	if ids := sarifResultRules(t, doc); len(ids) != 0 {
		t.Errorf("non-transaction value produced findings: %v", ids)
	}
}

func TestSARIFFormatterIsDeterministic(t *testing.T) {
	t.Parallel()

	summary, err := lens.ExplainPair(loadValue(t, "tx_failed.env"), loadValue(t, "tx_failed.res"))
	if err != nil {
		t.Fatalf("ExplainPair: %v", err)
	}

	var first, second bytes.Buffer
	f := format.NewSARIFFormatter()
	if err := f.FormatSummary(&first, summary); err != nil {
		t.Fatalf("first FormatSummary: %v", err)
	}
	if err := f.FormatSummary(&second, summary); err != nil {
		t.Fatalf("second FormatSummary: %v", err)
	}
	if first.String() != second.String() {
		t.Error("SARIF output differs between runs")
	}
}

func TestSARIFFormatterRejectsNilValue(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := format.NewSARIFFormatter().Format(&buf, nil); err == nil {
		t.Fatal("Format(nil) = nil error, want error")
	}
}

func TestSARIFFormatterToolOptions(t *testing.T) {
	t.Parallel()

	f := format.NewSARIFFormatter(
		format.WithSARIFToolName("xdr-lens"),
		format.WithSARIFToolURI("https://example.test/lens"),
	)
	var buf bytes.Buffer
	if err := f.Format(&buf, loadValue(t, "tx_success.env")); err != nil {
		t.Fatalf("Format: %v", err)
	}

	doc := validateSARIF(t, buf.Bytes())
	driver := doc["runs"].([]any)[0].(map[string]any)["tool"].(map[string]any)["driver"].(map[string]any)
	if driver["name"] != "xdr-lens" {
		t.Errorf("tool name = %v, want xdr-lens", driver["name"])
	}
	if driver["informationUri"] != "https://example.test/lens" {
		t.Errorf("tool uri = %v", driver["informationUri"])
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
