package lens_test

import (
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

func TestDiagnoseSetOptionsThresholdOrdering(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		op   xdr.SetOptionsOp
		want []string
		deny []string
	}{
		{
			name: "low above med",
			op:   xdr.SetOptionsOp{LowThreshold: optUint32(20), MedThreshold: optUint32(10)},
			want: []string{"low=20 > med=10", "Keep low <= med <= high"},
		},
		{
			name: "med above high",
			op:   xdr.SetOptionsOp{MedThreshold: optUint32(30), HighThreshold: optUint32(5)},
			want: []string{"med=30 > high=5"},
		},
		{
			name: "both tiers inverted",
			op:   xdr.SetOptionsOp{LowThreshold: optUint32(30), MedThreshold: optUint32(20), HighThreshold: optUint32(10)},
			want: []string{"low=30 > med=20, med=20 > high=10"},
			deny: []string{"low=30 > high=10"},
		},
		{
			name: "low above high with med unset",
			op:   xdr.SetOptionsOp{LowThreshold: optUint32(9), HighThreshold: optUint32(3)},
			want: []string{"low=9 > high=3"},
		},
		{
			name: "ascending thresholds are not flagged",
			op:   xdr.SetOptionsOp{LowThreshold: optUint32(1), MedThreshold: optUint32(2), HighThreshold: optUint32(3)},
			deny: []string{"hierarchy violation"},
		},
		{
			name: "equal thresholds are not flagged",
			op:   xdr.SetOptionsOp{LowThreshold: optUint32(2), MedThreshold: optUint32(2), HighThreshold: optUint32(2)},
			deny: []string{"hierarchy violation"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := lens.DiagnoseSetOptions(&tc.op, xdr.SetOptionsResultCodeSetOptionsThresholdOutOfRange)
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("DiagnoseSetOptions() = %q, want it to contain %q", got, want)
				}
			}
			for _, deny := range tc.deny {
				if strings.Contains(got, deny) {
					t.Errorf("DiagnoseSetOptions() = %q, must not contain %q", got, deny)
				}
			}
		})
	}
}

func TestDiagnoseSetOptionsMasterKeyLockout(t *testing.T) {
	t.Parallel()

	signer := func(weight xdr.Uint32) *xdr.Signer {
		return &xdr.Signer{
			Key:    xdr.SignerKey{Type: xdr.SignerKeyTypeSignerKeyTypeEd25519, Ed25519: explainTestKey(0x2B)},
			Weight: weight,
		}
	}

	tests := []struct {
		name string
		op   xdr.SetOptionsOp
		want string
	}{
		{
			name: "no signer against a raised high threshold",
			op:   xdr.SetOptionsOp{MasterWeight: optUint32(0), HighThreshold: optUint32(5)},
			want: "signer weight 0 against high=5, so the account is locked unless its existing signers supply the remaining 5",
		},
		{
			name: "signer too weak for the high threshold",
			op:   xdr.SetOptionsOp{MasterWeight: optUint32(0), HighThreshold: optUint32(5), Signer: signer(2)},
			want: "signer weight 2 against high=5, so the account is locked unless its existing signers supply the remaining 3",
		},
		{
			name: "zero high threshold still needs one signing key",
			op:   xdr.SetOptionsOp{MasterWeight: optUint32(0), HighThreshold: optUint32(0)},
			want: "signer weight 0 against high=0, so the account is locked unless its existing signers supply the remaining 1",
		},
		{
			name: "oversized signer weight contributes nothing",
			op:   xdr.SetOptionsOp{MasterWeight: optUint32(0), HighThreshold: optUint32(5), Signer: signer(300)},
			want: "signer weight 0 against high=5",
		},
		{
			name: "high threshold unknown and no signer added",
			op:   xdr.SetOptionsOp{MasterWeight: optUint32(0)},
			want: "adds no signer weight, so the account is locked unless its existing signers meet the high threshold",
		},
		{
			name: "signer meets the high threshold",
			op:   xdr.SetOptionsOp{MasterWeight: optUint32(0), HighThreshold: optUint32(5), Signer: signer(5)},
		},
		{
			name: "signer added while the high threshold is unknown cannot be judged",
			op:   xdr.SetOptionsOp{MasterWeight: optUint32(0), Signer: signer(1)},
		},
		{
			name: "master key left enabled",
			op:   xdr.SetOptionsOp{MasterWeight: optUint32(1), HighThreshold: optUint32(5)},
		},
		{
			name: "master weight untouched",
			op:   xdr.SetOptionsOp{HighThreshold: optUint32(5)},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// The lockout succeeds on-chain, so a successful code must warn too.
			got := lens.DiagnoseSetOptions(&tc.op, xdr.SetOptionsResultCodeSetOptionsSuccess)
			if tc.want == "" {
				if got != "" {
					t.Errorf("DiagnoseSetOptions() = %q, want no warning", got)
				}
				return
			}
			if !strings.Contains(got, "Master key lockout risk") || !strings.Contains(got, tc.want) {
				t.Errorf("DiagnoseSetOptions() = %q, want a lockout warning containing %q", got, tc.want)
			}
		})
	}
}

func TestDiagnoseSetOptionsBadSigner(t *testing.T) {
	t.Parallel()

	source := explainTestAccount(0xA1)
	selfSigner := xdr.SetOptionsOp{Signer: &xdr.Signer{
		Key:    xdr.SignerKey{Type: xdr.SignerKeyTypeSignerKeyTypeEd25519, Ed25519: source.Ed25519},
		Weight: 1,
	}}
	emptyPayload := xdr.SetOptionsOp{Signer: &xdr.Signer{
		Key: xdr.SignerKey{
			Type:                 xdr.SignerKeyTypeSignerKeyTypeEd25519SignedPayload,
			Ed25519SignedPayload: &xdr.SignerKeyEd25519SignedPayload{Ed25519: *explainTestKey(0x5E)},
		},
		Weight: 1,
	}}

	got := lens.DiagnoseSetOptionsFromSource(source.Address(), &selfSigner, xdr.SetOptionsResultCodeSetOptionsBadSigner)
	if !strings.Contains(got, "signer of itself") {
		t.Errorf("self signer = %q, want it flagged", got)
	}
	got = lens.DiagnoseSetOptions(&selfSigner, xdr.SetOptionsResultCodeSetOptionsBadSigner)
	if strings.Contains(got, "signer of itself") {
		t.Errorf("self signer without a source = %q, must not claim a match it cannot prove", got)
	}
	got = lens.DiagnoseSetOptions(&emptyPayload, xdr.SetOptionsResultCodeSetOptionsBadSigner)
	if !strings.Contains(got, "empty payload") {
		t.Errorf("empty payload = %q, want it flagged", got)
	}
}

func TestDiagnoseSetOptionsNothingToDiagnose(t *testing.T) {
	t.Parallel()

	if got := lens.DiagnoseSetOptions(nil, xdr.SetOptionsResultCodeSetOptionsBadSigner); got != "" {
		t.Errorf("nil operation = %q, want empty", got)
	}
	if got := lens.DiagnoseSetOptions(&xdr.SetOptionsOp{}, xdr.SetOptionsResultCodeSetOptionsSuccess); got != "" {
		t.Errorf("clean success = %q, want empty", got)
	}
}

// TestExplainPairReportsSetOptionsThresholdHazards drives both warnings through
// the explain path, alongside the failure that actually stopped the operation.
func TestExplainPairReportsSetOptionsThresholdHazards(t *testing.T) {
	t.Parallel()

	op := xdr.SetOptionsOp{
		MasterWeight: optUint32(0),
		LowThreshold: optUint32(20),
		MedThreshold: optUint32(10),
		SetFlags:     optUint32(0x20),
	}
	got := explainSingleOperation(t,
		xdr.OperationBody{Type: xdr.OperationTypeSetOptions, SetOptionsOp: &op},
		xdr.OperationResultTr{
			Type:             xdr.OperationTypeSetOptions,
			SetOptionsResult: &xdr.SetOptionsResult{Code: xdr.SetOptionsResultCodeSetOptionsUnknownFlag},
		})

	assertReasonContains(t, got, false, "0x20", "low=20 > med=10", "Master key lockout risk")
	for _, want := range []string{"Keep low <= med <= high", "before setting the master weight to 0"} {
		if !strings.Contains(got.Hint, want) {
			t.Errorf("Hint = %q, want it to contain %q", got.Hint, want)
		}
	}
}
