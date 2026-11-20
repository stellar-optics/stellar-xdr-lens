package lens_test

import (
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens/format"
)

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
