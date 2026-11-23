package lens_test

import (
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens/format"
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

func makeContractAddress(name string) xdr.ScAddress {
	var cId xdr.ContractId
	copy(cId[:], []byte(name))
	return xdr.ScAddress{
		Type:       xdr.ScAddressTypeScAddressTypeContract,
		ContractId: &cId,
	}
}

func makeSymbolScVal(s string) xdr.ScVal {
	sym := xdr.ScSymbol(s)
	return xdr.ScVal{
		Type: xdr.ScValTypeScvSymbol,
		Sym:  &sym,
	}
}

func makeU32ScVal(u uint32) xdr.ScVal {
	v := xdr.Uint32(u)
	return xdr.ScVal{
		Type: xdr.ScValTypeScvU32,
		U32:  &v,
	}
}

func makeContractFn(contract, function string, args ...xdr.ScVal) xdr.SorobanAuthorizedFunction {
	return xdr.SorobanAuthorizedFunction{
		Type: xdr.SorobanAuthorizedFunctionTypeSorobanAuthorizedFunctionTypeContractFn,
		ContractFn: &xdr.InvokeContractArgs{
			ContractAddress: makeContractAddress(contract),
			FunctionName:    xdr.ScSymbol(function),
			Args:            args,
		},
	}
}

func TestFormatAuthTree(t *testing.T) {
	t.Parallel()

	t.Run("single root invocation without sub-invocations", func(t *testing.T) {
		inv := xdr.SorobanAuthorizedInvocation{
			Function: makeContractFn("TokenContract", "balance", makeSymbolScVal("user")),
		}

		out := lens.FormatAuthTree(inv)
		expected := "└── Authorized Call: TokenContract.balance(user)"
		if out != expected {
			t.Errorf("got:\n%s\nwant:\n%s", out, expected)
		}
	})

	t.Run("complex multi-contract authorization tree hierarchy", func(t *testing.T) {
		// Matches issue hierarchy:
		// └── Authorized Call: RouterContract.swap(tokenA, tokenB, 1000)
		//     ├── Sub-Invocation: TokenA.transfer(user, pool, 1000)
		//     └── Sub-Invocation: Pool.execute_trade(...)
		//         └── Sub-Invocation: TokenB.transfer(pool, user, 950)

		tokenBTransfer := xdr.SorobanAuthorizedInvocation{
			Function: makeContractFn("TokenB", "transfer",
				makeSymbolScVal("pool"),
				makeSymbolScVal("user"),
				makeU32ScVal(950),
			),
		}

		poolTrade := xdr.SorobanAuthorizedInvocation{
			Function: makeContractFn("Pool", "execute_trade"),
			SubInvocations: []xdr.SorobanAuthorizedInvocation{
				tokenBTransfer,
			},
		}

		tokenATransfer := xdr.SorobanAuthorizedInvocation{
			Function: makeContractFn("TokenA", "transfer",
				makeSymbolScVal("user"),
				makeSymbolScVal("pool"),
				makeU32ScVal(1000),
			),
		}

		routerSwap := xdr.SorobanAuthorizedInvocation{
			Function: makeContractFn("RouterContract", "swap",
				makeSymbolScVal("tokenA"),
				makeSymbolScVal("tokenB"),
				makeU32ScVal(1000),
			),
			SubInvocations: []xdr.SorobanAuthorizedInvocation{
				tokenATransfer,
				poolTrade,
			},
		}

		out := lens.FormatAuthTree(routerSwap)
		expected := strings.Join([]string{
			"└── Authorized Call: RouterContract.swap(tokenA, tokenB, 1000)",
			"    ├── Sub-Invocation: TokenA.transfer(user, pool, 1000)",
			"    └── Sub-Invocation: Pool.execute_trade()",
			"        └── Sub-Invocation: TokenB.transfer(pool, user, 950)",
		}, "\n")

		if out != expected {
			t.Errorf("got:\n%s\nwant:\n%s", out, expected)
		}

		// Also verify RenderAuthHierarchy from format package
		fmtOut := format.RenderAuthHierarchy(routerSwap)
		if fmtOut != expected {
			t.Errorf("format.RenderAuthHierarchy got:\n%s\nwant:\n%s", fmtOut, expected)
		}
	})

	t.Run("FormatAuthorizationEntries with multiple roots", func(t *testing.T) {
		entry1 := xdr.SorobanAuthorizationEntry{
			RootInvocation: xdr.SorobanAuthorizedInvocation{
				Function: makeContractFn("Contract1", "op1"),
			},
		}
		entry2 := xdr.SorobanAuthorizationEntry{
			RootInvocation: xdr.SorobanAuthorizedInvocation{
				Function: makeContractFn("Contract2", "op2"),
			},
		}

		out := lens.FormatAuthorizationEntries([]xdr.SorobanAuthorizationEntry{entry1, entry2})
		expected := strings.Join([]string{
			"├── Authorized Call: Contract1.op1()",
			"└── Authorized Call: Contract2.op2()",
		}, "\n")

		if out != expected {
			t.Errorf("got:\n%s\nwant:\n%s", out, expected)
		}
	})

	t.Run("FormatScValShort argument types", func(t *testing.T) {
		bTrue := true
		if s := lens.FormatScValShort(xdr.ScVal{Type: xdr.ScValTypeScvBool, B: &bTrue}); s != "true" {
			t.Errorf("expected 'true', got %s", s)
		}

		strVal := xdr.ScString("hello")
		if s := lens.FormatScValShort(xdr.ScVal{Type: xdr.ScValTypeScvString, Str: &strVal}); s != "hello" {
			t.Errorf("expected 'hello', got %s", s)
		}

		if s := lens.FormatScValShort(xdr.ScVal{Type: xdr.ScValTypeScvVoid}); s != "()" {
			t.Errorf("expected '()', got %s", s)
		}
	})

	t.Run("Create contract functions", func(t *testing.T) {
		createFn := xdr.SorobanAuthorizedFunction{
			Type: xdr.SorobanAuthorizedFunctionTypeSorobanAuthorizedFunctionTypeCreateContractHostFn,
		}
		if s := lens.FormatContractCall(createFn); s != "create_contract(...)" {
			t.Errorf("expected create_contract(...), got %s", s)
		}

		createV2Fn := xdr.SorobanAuthorizedFunction{
			Type: xdr.SorobanAuthorizedFunctionTypeSorobanAuthorizedFunctionTypeCreateContractV2HostFn,
		}
		if s := lens.FormatContractCall(createV2Fn); s != "create_contract_v2(...)" {
			t.Errorf("expected create_contract_v2(...), got %s", s)
		}
	})
}
