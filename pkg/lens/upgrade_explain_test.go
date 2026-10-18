package lens_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

func TestDiagnoseProtocolUpgrade(t *testing.T) {
	t.Parallel()
	previous := mustLedgerHeader(t)
	current := *previous
	current.LedgerSeq++
	current.LedgerVersion++
	rejected := &xdr.TransactionResult{Result: xdr.TransactionResultResult{Code: xdr.TransactionResultCodeTxNotSupported}}
	d, err := lens.DiagnoseProtocolUpgrade(previous, &current, rejected)
	if err != nil {
		t.Fatalf("DiagnoseProtocolUpgrade() error = %v", err)
	}
	if d == nil || !d.LikelyUpgradeRelated || d.PreviousProtocol != 22 || d.CurrentProtocol != 23 || d.BoundaryLedger != uint32(current.LedgerSeq) {
		t.Errorf("diagnosis = %+v, want protocol 22->23 at boundary", d)
	}
	if !strings.Contains(d.Summary, "does not prove") || d.Hint == "" {
		t.Errorf("diagnosis = %+v, want correlation caveat and next step", d)
	}

	unrelated := &xdr.TransactionResult{Result: xdr.TransactionResultResult{Code: xdr.TransactionResultCodeTxBadSeq}}
	d, err = lens.DiagnoseProtocolUpgrade(previous, &current, unrelated)
	if err != nil || d == nil || d.LikelyUpgradeRelated {
		t.Errorf("unrelated rejection diagnosis = %+v, %v", d, err)
	}
	operationResults := []xdr.OperationResult{{Code: xdr.OperationResultCodeOpNotSupported}}
	unsupportedOperation := &xdr.TransactionResult{Result: xdr.TransactionResultResult{
		Code: xdr.TransactionResultCodeTxFailed, Results: &operationResults,
	}}
	d, err = lens.DiagnoseProtocolUpgrade(previous, &current, unsupportedOperation)
	if err != nil || d == nil || !d.LikelyUpgradeRelated {
		t.Errorf("unsupported operation diagnosis = %+v, %v; want upgrade-related", d, err)
	}
}

func TestDiagnoseProtocolUpgradeValidationAndFixture(t *testing.T) {
	t.Parallel()
	previous := mustLedgerHeader(t)
	current := *previous
	current.LedgerSeq++
	current.LedgerVersion++
	rejected := &xdr.TransactionResult{Result: xdr.TransactionResultResult{Code: xdr.TransactionResultCodeTxNotSupported}}
	if _, err := lens.DiagnoseProtocolUpgrade(nil, &current, rejected); !errors.Is(err, lens.ErrInvalidArgument) {
		t.Errorf("nil previous header error = %v, want ErrInvalidArgument", err)
	}
	if _, err := lens.DiagnoseProtocolUpgrade(previous, previous, rejected); !errors.Is(err, lens.ErrInvalidArgument) {
		t.Errorf("non-adjacent headers error = %v, want ErrInvalidArgument", err)
	}
	if d, err := lens.DiagnoseProtocolUpgrade(previous, previous, nil); !errors.Is(err, lens.ErrInvalidArgument) || d != nil {
		t.Errorf("nil result = %+v, %v; want ErrInvalidArgument", d, err)
	}
	success := &xdr.TransactionResult{Result: xdr.TransactionResultResult{Code: xdr.TransactionResultCodeTxSuccess}}
	if d, err := lens.DiagnoseProtocolUpgrade(previous, &current, success); err != nil || d != nil {
		t.Errorf("successful transaction diagnosis = %+v, %v; want nil, nil", d, err)
	}
	current.LedgerVersion = previous.LedgerVersion
	if d, err := lens.DiagnoseProtocolUpgrade(previous, &current, rejected); err != nil || d != nil {
		t.Errorf("unchanged protocol diagnosis = %+v, %v; want nil, nil", d, err)
	}
	current.LedgerVersion = previous.LedgerVersion - 1
	if _, err := lens.DiagnoseProtocolUpgrade(previous, &current, rejected); !errors.Is(err, lens.ErrInvalidArgument) {
		t.Errorf("decreasing protocol version error = %v, want ErrInvalidArgument", err)
	}
}

func mustLedgerHeader(t *testing.T) *xdr.LedgerHeader {
	t.Helper()
	value, err := lens.DecodeAs(loadFixture(t, "ledger_header"), "LedgerHeader")
	if err != nil {
		t.Fatalf("DecodeAs(ledger_header) error = %v", err)
	}
	header, ok := value.Raw.(*xdr.LedgerHeader)
	if !ok {
		t.Fatalf("ledger header fixture decoded to %T", value.Raw)
	}
	return header
}
