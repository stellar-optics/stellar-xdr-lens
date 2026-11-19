package lens_test

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

func buildTimeBoundsEnvelope(cond xdr.Preconditions) *xdr.TransactionEnvelope {
	var key xdr.Uint256
	key[0] = 0x01
	src := xdr.MuxedAccount{
		Type:    xdr.CryptoKeyTypeKeyTypeEd25519,
		Ed25519: &key,
	}

	return &xdr.TransactionEnvelope{
		Type: xdr.EnvelopeTypeEnvelopeTypeTx,
		V1: &xdr.TransactionV1Envelope{
			Tx: xdr.Transaction{
				SourceAccount: src,
				Fee:           100,
				SeqNum:        1,
				Cond:          cond,
			},
		},
	}
}

func TestLintTimeBounds(t *testing.T) {
	t.Parallel()

	refNow := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	pastMax := xdr.TimePoint(refNow.Add(-1 * time.Hour).Unix())
	futureMax := xdr.TimePoint(refNow.Add(1 * time.Hour).Unix())
	exactNow := xdr.TimePoint(refNow.Unix())

	tests := []struct {
		name         string
		cond         xdr.Preconditions
		wantCount    int
		wantSeverity lens.LintSeverity
		wantSubstr   string
	}{
		{
			name:         "no preconditions (unbounded)",
			cond:         xdr.Preconditions{Type: xdr.PreconditionTypePrecondNone},
			wantCount:    1,
			wantSeverity: lens.LintSeverityWarning,
			wantSubstr:   "unbounded",
		},
		{
			name: "timebounds with maxTime 0 warns",
			cond: xdr.Preconditions{
				Type: xdr.PreconditionTypePrecondTime,
				TimeBounds: &xdr.TimeBounds{
					MinTime: 1000,
					MaxTime: 0,
				},
			},
			wantCount:    1,
			wantSeverity: lens.LintSeverityWarning,
			wantSubstr:   "unbounded",
		},
		{
			name: "v2 preconditions with maxTime 0 warns",
			cond: xdr.Preconditions{
				Type: xdr.PreconditionTypePrecondV2,
				V2: &xdr.PreconditionsV2{
					TimeBounds: &xdr.TimeBounds{
						MinTime: 0,
						MaxTime: 0,
					},
				},
			},
			wantCount:    1,
			wantSeverity: lens.LintSeverityWarning,
			wantSubstr:   "unbounded",
		},
		{
			name: "expired maxTime flags error",
			cond: xdr.Preconditions{
				Type: xdr.PreconditionTypePrecondTime,
				TimeBounds: &xdr.TimeBounds{
					MinTime: 0,
					MaxTime: pastMax,
				},
			},
			wantCount:    1,
			wantSeverity: lens.LintSeverityError,
			wantSubstr:   "already expired",
		},
		{
			name: "future maxTime produces no findings",
			cond: xdr.Preconditions{
				Type: xdr.PreconditionTypePrecondTime,
				TimeBounds: &xdr.TimeBounds{
					MinTime: 0,
					MaxTime: futureMax,
				},
			},
			wantCount: 0,
		},
		{
			name: "maxTime equal to now is not yet expired",
			cond: xdr.Preconditions{
				Type: xdr.PreconditionTypePrecondTime,
				TimeBounds: &xdr.TimeBounds{
					MinTime: 0,
					MaxTime: exactNow,
				},
			},
			wantCount: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			env := buildTimeBoundsEnvelope(tc.cond)
			raw, err := env.MarshalBinary()
			if err != nil {
				t.Fatalf("MarshalBinary: %v", err)
			}
			val, err := lens.Decode(base64.StdEncoding.EncodeToString(raw))
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			decodedEnv, ok := val.Raw.(*xdr.TransactionEnvelope)
			if !ok {
				t.Fatalf("decoded value is %T, want *xdr.TransactionEnvelope", val.Raw)
			}

			findings := lens.LintTransactionAt(decodedEnv, refNow)
			if len(findings) != tc.wantCount {
				t.Fatalf("got %d findings (%+v), want %d", len(findings), findings, tc.wantCount)
			}
			if tc.wantCount == 0 {
				return
			}

			f := findings[0]
			if f.Rule != "time-bounds" {
				t.Errorf("Rule = %q, want %q", f.Rule, "time-bounds")
			}
			if f.Severity != tc.wantSeverity {
				t.Errorf("Severity = %q, want %q", f.Severity, tc.wantSeverity)
			}
			if f.OpIndex != -1 {
				t.Errorf("OpIndex = %d, want -1", f.OpIndex)
			}
			if !strings.Contains(f.Message, tc.wantSubstr) {
				t.Errorf("Message = %q, want substring %q", f.Message, tc.wantSubstr)
			}
		})
	}
}

func TestLintTimeBoundsWallClockAndFeeBump(t *testing.T) {
	t.Parallel()

	// Expired relative to real wall clock (year 2000).
	expiredEnv := buildTimeBoundsEnvelope(xdr.Preconditions{
		Type: xdr.PreconditionTypePrecondTime,
		TimeBounds: &xdr.TimeBounds{
			MinTime: 0,
			MaxTime: 946684800, // 2000-01-01T00:00:00Z
		},
	})
	raw, err := expiredEnv.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	val, err := lens.Decode(base64.StdEncoding.EncodeToString(raw))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	findings, err := lens.LintValue(val)
	if err != nil {
		t.Fatalf("LintValue: %v", err)
	}
	if len(findings) != 1 || findings[0].Severity != lens.LintSeverityError {
		t.Fatalf("LintValue(expired) = %+v, want 1 error finding", findings)
	}

	// FeeBump wrapping an unbounded inner tx should also warn.
	unboundedInner := buildTimeBoundsEnvelope(xdr.Preconditions{
		Type: xdr.PreconditionTypePrecondTime,
		TimeBounds: &xdr.TimeBounds{
			MinTime: 0,
			MaxTime: 0,
		},
	})
	feeBump := &xdr.TransactionEnvelope{
		Type: xdr.EnvelopeTypeEnvelopeTypeTxFeeBump,
		FeeBump: &xdr.FeeBumpTransactionEnvelope{
			Tx: xdr.FeeBumpTransaction{
				FeeSource: unboundedInner.V1.Tx.SourceAccount,
				Fee:       200,
				InnerTx: xdr.FeeBumpTransactionInnerTx{
					Type: xdr.EnvelopeTypeEnvelopeTypeTx,
					V1:   unboundedInner.V1,
				},
			},
		},
	}
	fbFindings := lens.LintTransaction(feeBump)
	if len(fbFindings) != 1 || fbFindings[0].Severity != lens.LintSeverityWarning {
		t.Fatalf("LintTransaction(feeBump unbounded) = %+v, want 1 warning finding", fbFindings)
	}

	if _, err := lens.LintValue(nil); !errors.Is(err, lens.ErrInvalidArgument) {
		t.Errorf("LintValue(nil) error = %v, want ErrInvalidArgument", err)
	}
}
