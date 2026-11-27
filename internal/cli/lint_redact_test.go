package cli_test

import (
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/internal/cli"
)

const lintTestAccount = "GAPB7YMUQ6MPQ23PTFDNBZP7HGGTZPI2DT5JJURUOIXNPLGHW2O3WYXZ"

func lintTestEnvelope(t *testing.T, fee uint32, signed bool) string {
	t.Helper()
	env := xdr.TransactionEnvelope{
		Type: xdr.EnvelopeTypeEnvelopeTypeTx,
		V1: &xdr.TransactionV1Envelope{
			Tx: xdr.Transaction{
				SourceAccount: xdr.MustMuxedAddress(lintTestAccount),
				Fee:           xdr.Uint32(fee),
				SeqNum:        xdr.SequenceNumber(1),
				Cond:          xdr.Preconditions{Type: xdr.PreconditionTypePrecondNone},
				Memo:          xdr.Memo{Type: xdr.MemoTypeMemoNone},
				Operations: []xdr.Operation{{
					Body: xdr.OperationBody{
						Type:           xdr.OperationTypeBumpSequence,
						BumpSequenceOp: &xdr.BumpSequenceOp{BumpTo: xdr.SequenceNumber(2)},
					},
				}},
				Ext: xdr.TransactionExt{V: 0},
			},
		},
	}
	if signed {
		env.V1.Signatures = []xdr.DecoratedSignature{{
			Hint:      xdr.SignatureHint{1, 2, 3, 4},
			Signature: []byte{0xde, 0xad, 0xbe, 0xef},
		}}
	}
	payload, err := xdr.MarshalBase64(env)
	if err != nil {
		t.Fatalf("MarshalBase64() error = %v", err)
	}
	return payload
}

func TestLintCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		payload     string
		wantCode    int
		wantContain []string
	}{
		{
			name:        "clean envelope",
			payload:     lintTestEnvelope(t, 100, true),
			wantCode:    0,
			wantContain: []string{"no lint issues found"},
		},
		{
			name:        "fee below minimum warns",
			payload:     lintTestEnvelope(t, 50, true),
			wantCode:    2,
			wantContain: []string{"fee-below-minimum", "WARN", "hint:"},
		},
		{
			name:        "zero fee and missing signature are errors",
			payload:     lintTestEnvelope(t, 0, false),
			wantCode:    2,
			wantContain: []string{"zero-fee-bid", "missing-signatures", "ERROR"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			stdout, _, err := run(t, "", "lint", tc.payload)
			if code, _ := cli.ExitCode(err); code != tc.wantCode {
				t.Errorf("exit code = %d, want %d (err=%v)", code, tc.wantCode, err)
			}
			for _, want := range tc.wantContain {
				if !strings.Contains(stdout, want) {
					t.Errorf("output does not contain %q:\n%s", want, stdout)
				}
			}
		})
	}
}

func TestLintCommandQuiet(t *testing.T) {
	t.Parallel()

	stdout, _, err := run(t, "", "lint", "--quiet", lintTestEnvelope(t, 50, true))
	if stdout != "" {
		t.Errorf("--quiet printed output:\n%s", stdout)
	}
	if code, _ := cli.ExitCode(err); code != cli.ExitDiffFound {
		t.Errorf("exit code = %d, want %d", code, cli.ExitDiffFound)
	}
}

func TestLintCommandRejectsNonEnvelope(t *testing.T) {
	t.Parallel()

	_, _, err := run(t, "", "lint", fixture(t, "asset_native"))
	if err == nil {
		t.Fatal("lint accepted an Asset payload")
	}
}

func TestRedactAddressesFlag(t *testing.T) {
	t.Parallel()

	full := "GAPB7YMUQ6MPQ23PTFDNBZP7HGGTZPI2DT5JJURUOIXNPLGHW2O3WYXZ"
	masked := "GAPB...WYXZ"

	tests := []struct {
		name string
		args []string
	}{
		{name: "tree", args: []string{"decode", fixture(t, "tx_failed.env")}},
		{name: "json", args: []string{"decode", "--json", fixture(t, "tx_failed.env")}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			stdout, _, err := run(t, "", append([]string{"--redact-addresses"}, tc.args...)...)
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if strings.Contains(stdout, full) {
				t.Errorf("output still contains the full address:\n%s", stdout)
			}
			if !strings.Contains(stdout, masked) {
				t.Errorf("output does not contain the masked address %q:\n%s", masked, stdout)
			}
		})
	}

	t.Run("disabled by default", func(t *testing.T) {
		t.Parallel()

		stdout, _, err := run(t, "", "decode", fixture(t, "tx_failed.env"))
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !strings.Contains(stdout, full) {
			t.Errorf("default output lost the plain address:\n%s", stdout)
		}
	})
}
