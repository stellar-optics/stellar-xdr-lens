package format

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

// SARIFFormatter renders transaction validation failures and diagnostics as
// SARIF 2.1.0 (Static Analysis Results Interchange Format), the JSON format
// GitHub Code Scanning and GitLab Security ingest to annotate a pull request
// with the findings they contain.
//
// Each failed operation becomes a result whose ruleId is the canonical result
// code, so a scanner tracks "invoke_host_function_trapped" as a stable rule
// across runs. Transaction-level rejection codes, fee-bump inner failures and
// non-fatal lens warnings are emitted as results too, at error or warning
// severity respectively.
//
// Output is deterministic: rules are sorted by id, results keep their
// transaction order, and nothing about the environment or the clock leaks
// into the document.
type SARIFFormatter struct {
	toolName string
	toolURI  string
}

// SARIFOption configures a SARIFFormatter.
type SARIFOption func(*SARIFFormatter)

// WithSARIFToolName sets the tool name reported in runs[].tool.driver.name.
// It defaults to "lens".
func WithSARIFToolName(name string) SARIFOption {
	return func(f *SARIFFormatter) { f.toolName = name }
}

// WithSARIFToolURI sets the tool information URI reported in
// runs[].tool.driver.informationUri.
func WithSARIFToolURI(uri string) SARIFOption {
	return func(f *SARIFFormatter) { f.toolURI = uri }
}

// NewSARIFFormatter returns a SARIFFormatter.
func NewSARIFFormatter(opts ...SARIFOption) *SARIFFormatter {
	f := &SARIFFormatter{
		toolName: "lens",
		toolURI:  "https://github.com/stellar-optics/stellar-xdr-lens",
	}
	for _, o := range opts {
		o(f)
	}
	return f
}

// SARIF severity levels, as defined by the SARIF 2.1.0 specification.
const (
	sarifLevelError   = "error"
	sarifLevelWarning = "warning"
)

// sarifLog is the top-level SARIF document.
type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	InformationURI string      `json:"informationUri,omitempty"`
	Rules          []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID                   string            `json:"id"`
	Name                 string            `json:"name,omitempty"`
	ShortDescription     sarifText         `json:"shortDescription"`
	FullDescription      *sarifText        `json:"fullDescription,omitempty"`
	Help                 *sarifText        `json:"help,omitempty"`
	DefaultConfiguration sarifRuleConfig   `json:"defaultConfiguration"`
	Properties           map[string]string `json:"properties,omitempty"`
}

type sarifRuleConfig struct {
	Level string `json:"level"`
}

type sarifText struct {
	Text string `json:"text"`
}

type sarifResult struct {
	RuleID     string          `json:"ruleId"`
	Level      string          `json:"level"`
	Message    sarifText       `json:"message"`
	Locations  []sarifLocation `json:"locations,omitempty"`
	RuleIndex  *int            `json:"ruleIndex,omitempty"`
	Properties map[string]any  `json:"properties,omitempty"`
}

type sarifLocation struct {
	LogicalLocations []sarifLogicalLocation `json:"logicalLocations"`
}

type sarifLogicalLocation struct {
	Name               string `json:"name"`
	Kind               string `json:"kind,omitempty"`
	FullyQualifiedName string `json:"fullyQualifiedName,omitempty"`
}

// Format writes a SARIF log for a single decoded value, satisfying the
// Formatter interface. TransactionEnvelope and TransactionResult values are
// summarised with lens.Explain; values Explain does not cover produce a valid
// log with no findings, because reaching a formatter already means the value
// decoded successfully.
func (f *SARIFFormatter) Format(w io.Writer, v *lens.Value) error {
	if v == nil || v.Node == nil {
		return fmt.Errorf("format: sarif: %w: nil value", lens.ErrInvalidArgument)
	}

	summary, err := lens.Explain(v)
	if err != nil {
		// Non-transaction XDR is not a finding: emit an empty, valid log.
		return f.writeLog(w, nil)
	}
	return f.FormatSummary(w, summary)
}

// FormatSummary writes a SARIF log for an already-built summary.
//
// This is the entry point explain uses, because it has the paired envelope
// and result in hand and must not decode or re-summarise them.
func (f *SARIFFormatter) FormatSummary(w io.Writer, s *lens.Summary) error {
	return f.writeLog(w, s)
}

// FormatAll writes one SARIF log covering every value. The values are treated
// as one analysis run, which is what a batch CI job wants: a single upload
// annotates every finding.
func (f *SARIFFormatter) FormatAll(w io.Writer, values []*lens.Value) error {
	summaries := make([]*lens.Summary, 0, len(values))
	for i, v := range values {
		if v == nil || v.Node == nil {
			return fmt.Errorf("format: sarif: %w: nil value at index %d", lens.ErrInvalidArgument, i)
		}
		s, err := lens.Explain(v)
		if err != nil {
			continue
		}
		summaries = append(summaries, s)
	}
	return f.writeLog(w, summaries...)
}

// writeLog builds and emits the SARIF document for the given summaries.
func (f *SARIFFormatter) writeLog(w io.Writer, summaries ...*lens.Summary) error {
	rules := map[string]*sarifRule{}
	levelFor := map[string]string{}
	// SARIF requires results to be an array, so start non-nil: an
	// analysis that found nothing must still emit "results": [].
	results := make([]sarifResult, 0)

	addResult := func(ruleID, level, message string, loc sarifLogicalLocation, props map[string]any) {
		if ruleID == "" {
			if level == sarifLevelWarning {
				ruleID = "lens_warning"
			} else {
				ruleID = "transaction_failed"
			}
		}
		if message == "" {
			message = "Transaction validation failed."
		}
		if prev, ok := levelFor[ruleID]; !ok || severityRank(level) > severityRank(prev) {
			levelFor[ruleID] = level
		}
		results = append(results, sarifResult{
			RuleID:     ruleID,
			Level:      level,
			Message:    sarifText{Text: message},
			Locations:  []sarifLocation{{LogicalLocations: []sarifLogicalLocation{loc}}},
			Properties: props,
		})
	}

	for _, s := range summaries {
		if s == nil {
			continue
		}
		if s.Outcome != nil && !s.Outcome.Success {
			txLoc := sarifLogicalLocation{Name: "transaction", Kind: "transaction", FullyQualifiedName: s.Kind}
			reason := &s.Outcome.Reason
			addResult(reason.Code, sarifLevelError, reasonMessage(reason), txLoc, reasonProperties(reason))

			if inner := s.Outcome.InnerReason; inner != nil && !inner.Success && inner.Code != reason.Code {
				addResult(inner.Code, sarifLevelError, reasonMessage(inner), txLoc, reasonProperties(inner))
			}

			for _, idx := range s.Outcome.FailedOps {
				op := operationSummary(s, idx)
				loc := sarifLogicalLocation{
					Name:               "operation[" + strconv.Itoa(idx) + "]",
					Kind:               "operation",
					FullyQualifiedName: "transaction.operations[" + strconv.Itoa(idx) + "]",
				}
				if op == nil || op.Result == nil {
					addResult("operation_failed", sarifLevelError,
						"Operation "+strconv.Itoa(idx)+" failed.", loc,
						map[string]any{"operationIndex": idx})
					continue
				}
				msg := reasonMessage(op.Result)
				if op.Type != "" && op.Type != "unknown" {
					msg = op.Type + ": " + msg
				}
				props := reasonProperties(op.Result)
				props["operationIndex"] = idx
				if op.Type != "" {
					props["operationType"] = op.Type
				}
				if op.Detail != "" {
					props["operationDetail"] = op.Detail
				}
				addResult(op.Result.Code, sarifLevelError, msg, loc, props)
			}
		}

		for _, warning := range s.Warnings {
			addResult("lens_warning", sarifLevelWarning, warning,
				sarifLogicalLocation{Name: "transaction", Kind: "transaction"}, nil)
		}
	}

	// Rules are emitted in id order so two runs that find the same problems
	// produce byte-identical output even if the findings arrived in a
	// different order.
	ids := make([]string, 0, len(levelFor))
	for id := range levelFor {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	ruleIndex := make(map[string]int, len(ids))
	for i, id := range ids {
		ruleIndex[id] = i
		rules[id] = &sarifRule{
			ID:                   id,
			Name:                 sarifRuleName(id),
			ShortDescription:     sarifText{Text: ruleDescription(id)},
			DefaultConfiguration: sarifRuleConfig{Level: levelFor[id]},
			Properties:           map[string]string{"tags": "stellar,transaction,validation"},
		}
	}

	for i := range results {
		idx := ruleIndex[results[i].RuleID]
		results[i].RuleIndex = &idx
	}

	ruleList := make([]sarifRule, 0, len(ids))
	for _, id := range ids {
		ruleList = append(ruleList, *rules[id])
	}

	doc := sarifLog{
		Schema:  "https://json.schemastore.org/sarif-2.1.0.json",
		Version: "2.1.0",
		Runs: []sarifRun{{
			Tool: sarifTool{Driver: sarifDriver{
				Name:           f.toolName,
				InformationURI: f.toolURI,
				Rules:          ruleList,
			}},
			Results: results,
		}},
	}

	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("format: marshal sarif: %w", err)
	}
	body = append(body, '\n')
	if _, err := w.Write(body); err != nil {
		return fmt.Errorf("format: write sarif: %w", err)
	}
	return nil
}

// reasonMessage combines a reason's summary and hint into the result message.
func reasonMessage(r *lens.Reason) string {
	if r == nil {
		return ""
	}
	msg := r.Summary
	if r.Hint != "" {
		if msg != "" {
			msg += " "
		}
		msg += "Hint: " + r.Hint
	}
	return msg
}

// reasonProperties records the raw protocol code alongside the short code, so
// a reviewer can cross-reference the XDR definition from the SARIF viewer.
func reasonProperties(r *lens.Reason) map[string]any {
	props := map[string]any{"resultCode": r.Code}
	if r.Constant != "" {
		props["resultConstant"] = r.Constant
	}
	return props
}

// severityRank orders SARIF levels so a rule shared by an error and a warning
// result reports its most severe level.
func severityRank(level string) int {
	switch level {
	case sarifLevelError:
		return 3
	case sarifLevelWarning:
		return 2
	case "note":
		return 1
	default:
		return 0
	}
}

// ruleDescription names the rule in the GitHub Code Scanning rule list. The
// short code is already the canonical, stable identifier, so it only needs a
// human-readable prefix.
func ruleDescription(id string) string {
	switch id {
	case "lens_warning":
		return "Non-fatal diagnostic warning reported by lens"
	case "transaction_failed":
		return "Transaction validation failed"
	case "operation_failed":
		return "Operation failed"
	default:
		return "Stellar transaction result: " + strings.ReplaceAll(id, "_", " ")
	}
}

// sarifRuleName turns a rule id into the PascalCase name SARIF encourages.
func sarifRuleName(id string) string {
	parts := strings.FieldsFunc(id, func(r rune) bool { return r == '_' || r == '-' })
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		b.WriteString(strings.ToUpper(p[:1]))
		b.WriteString(p[1:])
	}
	if b.Len() == 0 {
		return id
	}
	return b.String()
}
