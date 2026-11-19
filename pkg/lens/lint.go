package lens

import (
	"bytes"
	"encoding/hex"
	"fmt"

	"github.com/stellar/go-stellar-sdk/xdr"
)

// LintSeverity classifies how serious a static analysis finding is.
type LintSeverity string

const (
	// LintSeverityError indicates a defect that will cause the transaction to
	// fail or behave unexpectedly on-chain.
	LintSeverityError LintSeverity = "error"
	// LintSeverityWarning indicates a suspicious, wasteful, or unsafe pattern.
	LintSeverityWarning LintSeverity = "warning"
	// LintSeverityInfo indicates an informational best-practice suggestion.
	LintSeverityInfo LintSeverity = "info"
)

// LintFinding describes a single issue reported by a static analysis rule.
type LintFinding struct {
	// Rule is the stable identifier of the lint rule that produced this finding.
	Rule string `json:"rule"`
	// Severity is the severity level (error, warning, info).
	Severity LintSeverity `json:"severity"`
	// OpIndex is the 0-based operation index associated with the finding, or -1
	// when the finding applies to the transaction as a whole.
	OpIndex int `json:"op_index"`
	// Message explains what was detected.
	Message string `json:"message"`
	// Suggestion offers actionable remediation guidance.
	Suggestion string `json:"suggestion,omitempty"`
}

// LintTransaction runs all static analysis rules against a transaction envelope.
func LintTransaction(env *xdr.TransactionEnvelope) []LintFinding {
	if env == nil {
		return nil
	}
	var findings []LintFinding
	findings = append(findings, lintRedundantOperations(env)...)
	return findings
}

// LintValue runs transaction lint rules against a decoded Value.
// The value must hold a TransactionEnvelope.
func LintValue(v *Value) ([]LintFinding, error) {
	if v == nil {
		return nil, fmt.Errorf("%w: nil value", ErrInvalidArgument)
	}
	env, ok := v.Raw.(*xdr.TransactionEnvelope)
	if !ok {
		return nil, fmt.Errorf("%w: value is %s, want TransactionEnvelope", ErrInvalidArgument, v.Type)
	}
	return LintTransaction(env), nil
}

type trustlineKey struct {
	source string
	line   string
}

// lintRedundantOperations flags no-op or duplicate operations within a transaction:
//   - PathPaymentStrictReceive / PathPaymentStrictSend where send and destination
//     assets are identical and no intermediate conversion path is specified.
//   - ChangeTrust operations that set the same trustline to the same limit as an
//     earlier ChangeTrust in the same transaction.
//   - Consecutive identical operations.
func lintRedundantOperations(env *xdr.TransactionEnvelope) []LintFinding {
	ops := env.Operations()
	if len(ops) == 0 {
		return nil
	}

	var findings []LintFinding
	lastTrustLimit := make(map[trustlineKey]xdr.Int64)
	var prevOpBytes []byte

	for i, op := range ops {
		flaggedTrustline := false

		switch op.Body.Type {
		case xdr.OperationTypePathPaymentStrictReceive:
			pp := op.Body.MustPathPaymentStrictReceiveOp()
			if pp.SendAsset.Equals(pp.DestAsset) && len(pp.Path) == 0 {
				findings = append(findings, LintFinding{
					Rule:       "redundant-operations",
					Severity:   LintSeverityWarning,
					OpIndex:    i,
					Message:    fmt.Sprintf("operation %d is a PathPaymentStrictReceive sending and receiving the same asset (%s) with an empty conversion path", i, formatAssetLabel(pp.SendAsset)),
					Suggestion: "Use a direct Payment operation instead of a circular PathPayment when no asset conversion occurs.",
				})
			}

		case xdr.OperationTypePathPaymentStrictSend:
			pp := op.Body.MustPathPaymentStrictSendOp()
			if pp.SendAsset.Equals(pp.DestAsset) && len(pp.Path) == 0 {
				findings = append(findings, LintFinding{
					Rule:       "redundant-operations",
					Severity:   LintSeverityWarning,
					OpIndex:    i,
					Message:    fmt.Sprintf("operation %d is a PathPaymentStrictSend sending and receiving the same asset (%s) with an empty conversion path", i, formatAssetLabel(pp.SendAsset)),
					Suggestion: "Use a direct Payment operation instead of a circular PathPayment when no asset conversion occurs.",
				})
			}

		case xdr.OperationTypeChangeTrust:
			ct := op.Body.MustChangeTrustOp()
			if lineBytes, err := ct.Line.MarshalBinary(); err == nil {
				key := trustlineKey{
					source: opSourceKey(op.SourceAccount),
					line:   hex.EncodeToString(lineBytes),
				}
				if prevLimit, seen := lastTrustLimit[key]; seen && prevLimit == ct.Limit {
					flaggedTrustline = true
					findings = append(findings, LintFinding{
						Rule:       "redundant-operations",
						Severity:   LintSeverityWarning,
						OpIndex:    i,
						Message:    fmt.Sprintf("operation %d repeats a ChangeTrust limit (%d) already set earlier in this transaction", i, ct.Limit),
						Suggestion: "Remove the redundant ChangeTrust operation.",
					})
				}
				lastTrustLimit[key] = ct.Limit
			}
		}

		opBytes, err := op.MarshalBinary()
		if err == nil {
			if i > 0 && !flaggedTrustline && bytes.Equal(prevOpBytes, opBytes) {
				findings = append(findings, LintFinding{
					Rule:       "redundant-operations",
					Severity:   LintSeverityWarning,
					OpIndex:    i,
					Message:    fmt.Sprintf("operation %d (%s) is identical to the immediately preceding operation %d", i, op.Body.Type.String(), i-1),
					Suggestion: "Verify whether the duplicate operation is intentional or can be removed.",
				})
			}
			prevOpBytes = opBytes
		} else {
			prevOpBytes = nil
		}
	}

	return findings
}

func opSourceKey(src *xdr.MuxedAccount) string {
	if src == nil {
		return ""
	}
	b, err := src.MarshalBinary()
	if err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}

func formatAssetLabel(a xdr.Asset) string {
	if a.Type == xdr.AssetTypeAssetTypeNative {
		return "XLM"
	}
	var assetType, code, issuer string
	if err := a.Extract(&assetType, &code, &issuer); err == nil && code != "" {
		return code + ":" + issuer
	}
	return a.String()
}
