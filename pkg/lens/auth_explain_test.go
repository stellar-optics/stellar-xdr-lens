package lens_test

import (
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

func TestDiagnoseAuthExpirationActiveVsExpired(t *testing.T) {
	t.Parallel()

	accountID := xdr.MustAddress("GAPB7YMUQ6MPQ23PTFDNBZP7HGGTZPI2DT5JJURUOIXNPLGHW2O3WYXZ")
	scAddr, err := xdr.NewScAddress(xdr.ScAddressTypeScAddressTypeAccount, accountID)
	if err != nil {
		t.Fatalf("NewScAddress() error = %v", err)
	}

	makeEntry := func(expirationLedger uint32) xdr.SorobanAuthorizationEntry {
		return xdr.SorobanAuthorizationEntry{
			Credentials: xdr.SorobanCredentials{
				Type: xdr.SorobanCredentialsTypeSorobanCredentialsAddress,
				Address: &xdr.SorobanAddressCredentials{
					Address:                   scAddr,
					Nonce:                     1,
					SignatureExpirationLedger: xdr.Uint32(expirationLedger),
				},
			},
		}
	}

	tests := []struct {
		name          string
		expLedger     uint32
		currentLedger uint32
		wantExpired   bool
	}{
		{
			name:          "active authorization (expiration in the future)",
			expLedger:     1050,
			currentLedger: 1000,
			wantExpired:   false,
		},
		{
			name:          "active authorization (exact current ledger boundary)",
			expLedger:     1000,
			currentLedger: 1000,
			wantExpired:   false,
		},
		{
			name:          "expired authorization (expiration ledger passed)",
			expLedger:     999,
			currentLedger: 1000,
			wantExpired:   true,
		},
		{
			name:          "expired authorization by multiple ledgers",
			expLedger:     500,
			currentLedger: 1000,
			wantExpired:   true,
		},
		{
			name:          "zero current ledger does not expire",
			expLedger:     100,
			currentLedger: 0,
			wantExpired:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			entries := []xdr.SorobanAuthorizationEntry{makeEntry(tc.expLedger)}
			fail := lens.DiagnoseAuthExpiration(entries, tc.currentLedger, 0)

			if !tc.wantExpired {
				if fail != nil {
					t.Fatalf("expected no failure, got: %+v", fail)
				}
				return
			}

			if fail == nil {
				t.Fatal("expected auth failure for expired credentials, got nil")
			}

			if fail.FailureKind != "signature_expired" {
				t.Errorf("FailureKind = %q, want %q", fail.FailureKind, "signature_expired")
			}

			if fail.ExpiredLedger != tc.expLedger {
				t.Errorf("ExpiredLedger = %d, want %d", fail.ExpiredLedger, tc.expLedger)
			}
			if fail.CurrentLedger != tc.currentLedger {
				t.Errorf("CurrentLedger = %d, want %d", fail.CurrentLedger, tc.currentLedger)
			}

			wantHint := "Authorization expired at ledger " +
				string(rune('0'+tc.expLedger/1000)) // ensure formatted check below
			_ = wantHint
			expectedHint := strings.Contains(fail.Hint, "Refresh signature nonce and expiration before resubmitting.")
			if !expectedHint {
				t.Errorf("Hint = %q does not contain expected hint text", fail.Hint)
			}

			report := fail.DiagnosticReport()
			if !strings.Contains(report, "signature_expired") {
				t.Errorf("DiagnosticReport missing failure kind, got:\n%s", report)
			}
		})
	}
}

func TestDiagnoseAuthExpirationCredentialsTypes(t *testing.T) {
	t.Parallel()

	accountID := xdr.MustAddress("GAPB7YMUQ6MPQ23PTFDNBZP7HGGTZPI2DT5JJURUOIXNPLGHW2O3WYXZ")
	scAddr, err := xdr.NewScAddress(xdr.ScAddressTypeScAddressTypeAccount, accountID)
	if err != nil {
		t.Fatalf("NewScAddress() error = %v", err)
	}

	addrCreds := xdr.SorobanAddressCredentials{
		Address:                   scAddr,
		Nonce:                     10,
		SignatureExpirationLedger: 500,
	}

	t.Run("AddressV2", func(t *testing.T) {
		t.Parallel()
		entry := xdr.SorobanAuthorizationEntry{
			Credentials: xdr.SorobanCredentials{
				Type:      xdr.SorobanCredentialsTypeSorobanCredentialsAddressV2,
				AddressV2: &addrCreds,
			},
		}
		fail := lens.DiagnoseAuthExpiration([]xdr.SorobanAuthorizationEntry{entry}, 600, 0)
		if fail == nil || fail.FailureKind != "signature_expired" {
			t.Fatalf("expected expired failure for AddressV2, got: %+v", fail)
		}
	})

	t.Run("AddressWithDelegates", func(t *testing.T) {
		t.Parallel()
		entry := xdr.SorobanAuthorizationEntry{
			Credentials: xdr.SorobanCredentials{
				Type: xdr.SorobanCredentialsTypeSorobanCredentialsAddressWithDelegates,
				AddressWithDelegates: &xdr.SorobanAddressCredentialsWithDelegates{
					AddressCredentials: addrCreds,
				},
			},
		}
		fail := lens.DiagnoseAuthExpiration([]xdr.SorobanAuthorizationEntry{entry}, 600, 0)
		if fail == nil || fail.FailureKind != "signature_expired" {
			t.Fatalf("expected expired failure for AddressWithDelegates, got: %+v", fail)
		}
	})

	t.Run("SourceAccountCredentials (no address)", func(t *testing.T) {
		t.Parallel()
		entry := xdr.SorobanAuthorizationEntry{
			Credentials: xdr.SorobanCredentials{
				Type: xdr.SorobanCredentialsTypeSorobanCredentialsSourceAccount,
			},
		}
		fail := lens.DiagnoseAuthExpiration([]xdr.SorobanAuthorizationEntry{entry}, 600, 0)
		if fail != nil {
			t.Fatalf("expected nil for source account credentials, got: %+v", fail)
		}
	})
}

func TestDiagnoseAuthExpirationFromBoundsAndHeader(t *testing.T) {
	t.Parallel()

	accountID := xdr.MustAddress("GAPB7YMUQ6MPQ23PTFDNBZP7HGGTZPI2DT5JJURUOIXNPLGHW2O3WYXZ")
	scAddr, err := xdr.NewScAddress(xdr.ScAddressTypeScAddressTypeAccount, accountID)
	if err != nil {
		t.Fatalf("NewScAddress() error = %v", err)
	}

	entry := xdr.SorobanAuthorizationEntry{
		Credentials: xdr.SorobanCredentials{
			Type: xdr.SorobanCredentialsTypeSorobanCredentialsAddress,
			Address: &xdr.SorobanAddressCredentials{
				Address:                   scAddr,
				Nonce:                     1,
				SignatureExpirationLedger: 500,
			},
		},
	}
	auth := []xdr.SorobanAuthorizationEntry{entry}

	// 1. From LedgerBounds
	lbExpired := &xdr.LedgerBounds{MinLedger: 600, MaxLedger: 700}
	if fail := lens.DiagnoseAuthExpirationFromBounds(auth, lbExpired, 0); fail == nil {
		t.Error("expected auth expiration from LedgerBounds (min: 600), got nil")
	}

	lbActive := &xdr.LedgerBounds{MinLedger: 400, MaxLedger: 450}
	if fail := lens.DiagnoseAuthExpirationFromBounds(auth, lbActive, 0); fail != nil {
		t.Errorf("expected active auth for LedgerBounds (400..450), got: %+v", fail)
	}

	if fail := lens.DiagnoseAuthExpirationFromBounds(auth, nil, 0); fail != nil {
		t.Errorf("expected nil for nil LedgerBounds, got: %+v", fail)
	}

	// 2. From LedgerHeader
	hdrExpired := &xdr.LedgerHeader{LedgerSeq: 600}
	if fail := lens.DiagnoseAuthExpirationFromHeader(auth, hdrExpired, 0); fail == nil {
		t.Error("expected auth expiration from LedgerHeader (seq: 600), got nil")
	}

	hdrActive := &xdr.LedgerHeader{LedgerSeq: 400}
	if fail := lens.DiagnoseAuthExpirationFromHeader(auth, hdrActive, 0); fail != nil {
		t.Errorf("expected active auth from LedgerHeader (seq: 400), got: %+v", fail)
	}

	if fail := lens.DiagnoseAuthExpirationFromHeader(auth, nil, 0); fail != nil {
		t.Errorf("expected nil for nil LedgerHeader, got: %+v", fail)
	}
}

func TestDiagnoseInvokeHostFunctionAuth(t *testing.T) {
	t.Parallel()

	accountID := xdr.MustAddress("GAPB7YMUQ6MPQ23PTFDNBZP7HGGTZPI2DT5JJURUOIXNPLGHW2O3WYXZ")
	scAddr, err := xdr.NewScAddress(xdr.ScAddressTypeScAddressTypeAccount, accountID)
	if err != nil {
		t.Fatalf("NewScAddress() error = %v", err)
	}

	op := xdr.InvokeHostFunctionOp{
		HostFunction: xdr.HostFunction{
			Type: xdr.HostFunctionTypeHostFunctionTypeInvokeContract,
		},
		Auth: []xdr.SorobanAuthorizationEntry{
			{
				Credentials: xdr.SorobanCredentials{
					Type: xdr.SorobanCredentialsTypeSorobanCredentialsAddress,
					Address: &xdr.SorobanAddressCredentials{
						Address:                   scAddr,
						Nonce:                     5,
						SignatureExpirationLedger: 120,
					},
				},
			},
		},
	}

	fail := lens.DiagnoseInvokeHostFunctionAuth(op, 200, 1)
	if fail == nil {
		t.Fatal("expected failure for expired InvokeHostFunctionOp, got nil")
	}
	if fail.OperationIndex != 1 {
		t.Errorf("OperationIndex = %d, want 1", fail.OperationIndex)
	}
	if fail.ExpiredLedger != 120 || fail.CurrentLedger != 200 {
		t.Errorf("ExpiredLedger/CurrentLedger = %d/%d, want 120/200", fail.ExpiredLedger, fail.CurrentLedger)
	}
	expectedHint := "Authorization expired at ledger 120 (current ledger: 200). Refresh signature nonce and expiration before resubmitting."
	if fail.Hint != expectedHint {
		t.Errorf("Hint = %q, want %q", fail.Hint, expectedHint)
	}
}
