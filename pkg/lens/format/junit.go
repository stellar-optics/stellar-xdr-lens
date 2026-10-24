package format

import (
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

// JUnitFormatter renders decoded XDR as a JUnit XML test suite, the format
// enterprise CI systems (Jenkins, GitLab CI, CircleCI) parse to display test
// suites and failure breakdowns.
//
// Each value becomes one <testcase>, named after its XDR type. A test case
// fails when lens.Explain reports a failed transaction outcome; the failure
// carries the canonical result code and a message a developer can act on.
// Values that Explain does not cover (anything that is not a TransactionEnvelope
// or TransactionResult) pass, because the very fact that they reached a
// formatter means they decoded successfully.
//
// Output is deterministic: the bytes depend only on the values and the suite
// name, never on the clock, environment, or network.
type JUnitFormatter struct {
	suiteName string
}

// JUnitOption configures a JUnitFormatter.
type JUnitOption func(*JUnitFormatter)

// WithSuiteName sets the testsuite name (default "stellar-xdr-lens").
func WithSuiteName(name string) JUnitOption {
	return func(f *JUnitFormatter) { f.suiteName = name }
}

// NewJUnitFormatter returns a JUnitFormatter.
func NewJUnitFormatter(opts ...JUnitOption) *JUnitFormatter {
	f := &JUnitFormatter{suiteName: "stellar-xdr-lens"}
	for _, o := range opts {
		o(f)
	}
	return f
}

// Format writes a single-testcase JUnit report for v, satisfying the Formatter
// interface.
func (f *JUnitFormatter) Format(w io.Writer, v *lens.Value) error {
	return f.FormatAll(w, []*lens.Value{v})
}

// FormatAll writes a JUnit report with one testcase per value.
//
// The whole document is built before anything is written, so a failure part-way
// through leaves w untouched rather than holding truncated XML.
func (f *JUnitFormatter) FormatAll(w io.Writer, values []*lens.Value) error {
	suite, err := f.buildSuite(values)
	if err != nil {
		return err
	}

	body, err := xml.MarshalIndent(suite, "", "  ")
	if err != nil {
		return fmt.Errorf("format: marshal junit: %w", err)
	}

	out := make([]byte, 0, len(xml.Header)+len(body)+1)
	out = append(out, xml.Header...)
	out = append(out, body...)
	out = append(out, '\n')

	if _, err := w.Write(out); err != nil {
		return fmt.Errorf("format: write junit: %w", err)
	}
	return nil
}

// junitTestSuite mirrors the subset of the JUnit <testsuite> element the
// formatter emits. Attribute values are escaped by encoding/xml, so a memo or
// failure message containing XML metacharacters stays well-formed.
type junitTestSuite struct {
	XMLName  xml.Name        `xml:"testsuite"`
	Name     string          `xml:"name,attr"`
	Tests    int             `xml:"tests,attr"`
	Failures int             `xml:"failures,attr"`
	Errors   int             `xml:"errors,attr"`
	Skipped  int             `xml:"skipped,attr"`
	Time     string          `xml:"time,attr"`
	Cases    []junitTestCase `xml:"testcase"`
}

type junitTestCase struct {
	Name      string        `xml:"name,attr"`
	ClassName string        `xml:"classname,attr"`
	Time      string        `xml:"time,attr"`
	Failure   *junitFailure `xml:"failure,omitempty"`
	SystemOut string        `xml:"system-out,omitempty"`
}

type junitFailure struct {
	Type    string `xml:"type,attr"`
	Message string `xml:"message,attr"`
	Body    string `xml:",chardata"`
}

// buildSuite converts values into the suite model, rejecting values that carry
// no decoded tree rather than emitting misleading test cases for them.
func (f *JUnitFormatter) buildSuite(values []*lens.Value) (*junitTestSuite, error) {
	suite := &junitTestSuite{
		Name:  junitName(f.suiteName),
		Tests: len(values),
		// Durations are not measured: a wall-clock reading would make the
		// output non-deterministic, which this formatter promises it is not.
		Time: "0.000",
	}

	for i, v := range values {
		if v == nil || v.Node == nil {
			return nil, fmt.Errorf("format: junit: %w: nil value at index %d", lens.ErrInvalidArgument, i)
		}

		tc := junitTestCase{
			Name:      junitName(v.Type),
			ClassName: suite.Name,
			Time:      "0.000",
		}

		summary, err := lens.Explain(v)
		switch {
		case err != nil || summary == nil:
			// A decoded value that Explain does not cover is still a
			// successful decode; report it as a passing test case.
			tc.SystemOut = "decoded " + junitName(v.Type)
		case summary.Outcome != nil && !summary.Outcome.Success:
			tc.Failure = failureFromSummary(summary)
			suite.Failures++
		default:
			tc.SystemOut = summary.Headline
		}

		suite.Cases = append(suite.Cases, tc)
	}

	return suite, nil
}

// junitName substitutes a stable placeholder for an empty name, since JUnit
// consumers key their reports on non-empty test and suite names.
func junitName(name string) string {
	if name == "" {
		return "unknown"
	}
	return name
}

// failureFromSummary builds the <failure> element for a failed transaction.
//
// The type is the most specific failing result code available: the inner
// transaction's code for fee bumps, otherwise the transaction's own code. The
// body lists each failing operation with its reason and hint, which is what a
// CI log needs to explain the failure without re-running the tool.
func failureFromSummary(s *lens.Summary) *junitFailure {
	failure := &junitFailure{Message: s.Headline}

	reason := &s.Outcome.Reason
	if s.Outcome.InnerReason != nil && !s.Outcome.InnerReason.Success {
		reason = s.Outcome.InnerReason
	}
	failure.Type = reason.Code
	if failure.Type == "" {
		failure.Type = "transaction_failed"
	}
	if failure.Message == "" {
		failure.Message = reason.Summary
	}

	failure.Body = failureBody(s)
	return failure
}

// failureBody renders the human-readable detail attached to a <failure>
// element. It names the transaction-level reason and then every failed
// operation, so the failure breakdown survives into CI logs and web views.
func failureBody(s *lens.Summary) string {
	var lines []string
	appendReason := func(label string, r *lens.Reason) {
		if r == nil {
			return
		}
		if r.Summary != "" {
			lines = append(lines, label+": "+r.Summary)
		}
		if r.Context != "" {
			lines = append(lines, "  Context: "+r.Context)
		}
		if r.Hint != "" {
			lines = append(lines, "  Hint: "+r.Hint)
		}
	}

	appendReason("Reason", &s.Outcome.Reason)
	if inner := s.Outcome.InnerReason; inner != nil && inner.Code != s.Outcome.Reason.Code {
		appendReason("Inner reason", inner)
	}

	for _, idx := range s.Outcome.FailedOps {
		label := "Operation " + strconv.Itoa(idx)
		op := operationSummary(s, idx)
		if op == nil {
			lines = append(lines, label+" failed")
			continue
		}
		if op.Type != "" && op.Type != "unknown" {
			label += " (" + op.Type + ")"
		}
		lines = append(lines, label+" failed")
		appendReason("  Reason", op.Result)
	}

	return strings.Join(lines, "\n")
}

// operationSummary finds the operation with the given index, if the summary
// carries one.
func operationSummary(s *lens.Summary, index int) *lens.OpSummary {
	for i := range s.Operations {
		if s.Operations[i].Index == index {
			return &s.Operations[i]
		}
	}
	return nil
}
