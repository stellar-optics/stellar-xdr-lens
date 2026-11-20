package lens

import (
	"fmt"

	"github.com/stellar/go-stellar-sdk/xdr"
)

// PathInefficiencyDiagnosis summarizes inefficiencies detected in a path payment route,
// including redundant zero-hop paths and circular swap loops.
type PathInefficiencyDiagnosis struct {
	AssetSequence  []string `json:"assetSequence"`
	IsZeroHop      bool     `json:"isZeroHop"`
	IsCircular     bool     `json:"isCircular"`
	CircularAssets []string `json:"circularAssets,omitempty"`
	Warnings       []string `json:"warnings,omitempty"`
}

// DiagnosePathPaymentInefficiency inspects the complete conversion path [SendAsset, Path..., DestAsset]
// for zero-hop redundancy and circular asset loops.
func DiagnosePathPaymentInefficiency(sendAsset xdr.Asset, path []xdr.Asset, destAsset xdr.Asset) *PathInefficiencyDiagnosis {
	chain := make([]xdr.Asset, 0, len(path)+2)
	chain = append(chain, sendAsset)
	chain = append(chain, path...)
	chain = append(chain, destAsset)

	seq := make([]string, len(chain))
	for i, a := range chain {
		seq[i] = assetLabel(a)
	}

	diag := &PathInefficiencyDiagnosis{
		AssetSequence: seq,
	}

	// 1. Zero-hop redundancy: empty path when SendAsset == DestAsset
	if len(path) == 0 && assetsEqual(sendAsset, destAsset) {
		diag.IsZeroHop = true
		diag.Warnings = append(diag.Warnings,
			fmt.Sprintf("Inefficient path detected: send and destination assets are identical (%s) with empty path; regular Payment operation should be used instead.", seq[0]))
	}

	// 2. Circular / redundant path analysis: any asset appearing multiple times in the path chain
	seen := make(map[string]bool, len(chain))
	reported := make(map[string]bool)

	for _, a := range chain {
		label := assetLabel(a)
		if seen[label] && !reported[label] {
			reported[label] = true
			diag.IsCircular = true
			diag.CircularAssets = append(diag.CircularAssets, label)
			diag.Warnings = append(diag.Warnings,
				fmt.Sprintf("Inefficient path detected: asset %s appears multiple times, creating a circular swap loop.", label))
		}
		seen[label] = true
	}

	return diag
}

// DiagnosePathPaymentOperation checks an operation for path inefficiencies if it is
// PathPaymentStrictSend or PathPaymentStrictReceive.
func DiagnosePathPaymentOperation(op xdr.Operation) *PathInefficiencyDiagnosis {
	switch op.Body.Type {
	case xdr.OperationTypePathPaymentStrictSend:
		if pOp, ok := op.Body.GetPathPaymentStrictSendOp(); ok {
			return DiagnosePathPaymentInefficiency(pOp.SendAsset, pOp.Path, pOp.DestAsset)
		}
	case xdr.OperationTypePathPaymentStrictReceive:
		if pOp, ok := op.Body.GetPathPaymentStrictReceiveOp(); ok {
			return DiagnosePathPaymentInefficiency(pOp.SendAsset, pOp.Path, pOp.DestAsset)
		}
	}
	return nil
}

func assetsEqual(a, b xdr.Asset) bool {
	if a.Type != b.Type {
		return false
	}
	return a.StringCanonical() == b.StringCanonical()
}
