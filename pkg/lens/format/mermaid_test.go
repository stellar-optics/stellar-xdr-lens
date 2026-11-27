package format_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens/format"
)

func TestMermaidFormatterSorobanEnvelopeSequence(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := format.NewMermaidFormatter().Format(&buf, loadValue(t, "tx_success.env")); err != nil {
		t.Fatalf("Format: %v", err)
	}
	out := buf.String()

	validateSequence(t, out)
	if strings.Contains(out, "graph TD") {
		t.Errorf("Soroban envelope rendered as a flowchart:\n%s", out)
	}
	if !strings.Contains(out, "CCNXGPE4AQCSNEBZO3XJDKKDI3CRLYMVS6UWBBTVDLALLWMJEXBORQ2A") {
		t.Errorf("missing contract participant:\n%s", out)
	}
	if !strings.Contains(out, "swap(") {
		t.Errorf("missing function call message:\n%s", out)
	}
	if !strings.Contains(out, "authorize swap(") {
		t.Errorf("missing authorization message:\n%s", out)
	}
	if !strings.Contains(out, "-->>") {
		t.Errorf("authorization should use a dashed arrow:\n%s", out)
	}
}

// validateSequence checks the invariants of a Mermaid sequenceDiagram:
// participants are declared before the first message, and every message
// refers to a declared participant.
func validateSequence(t *testing.T, out string) {
	t.Helper()

	if !strings.HasPrefix(out, "sequenceDiagram\n") {
		t.Fatalf("output does not start with sequenceDiagram:\n%s", out)
	}
	declared := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "participant ") {
			fields := strings.Fields(line)
			if len(fields) < 4 || fields[2] != "as" {
				t.Errorf("malformed participant declaration: %q", line)
				continue
			}
			declared[fields[1]] = true
		}
		if idx := strings.Index(line, ": "); idx > 0 && strings.Contains(line, ">") {
			arrow := line[:idx]
			from, to, ok := splitArrow(arrow)
			if !ok {
				t.Errorf("malformed message: %q", line)
				continue
			}
			if !declared[from] {
				t.Errorf("message sender %q is not declared: %q", from, line)
			}
			if !declared[to] {
				t.Errorf("message receiver %q is not declared: %q", to, line)
			}
		}
	}
	if len(declared) == 0 {
		t.Fatalf("no participants declared:\n%s", out)
	}
}

// splitArrow splits "p_a->>p_b" or "p_a-->>p_b" into its endpoints.
func splitArrow(s string) (from, to string, ok bool) {
	for _, arrow := range []string{"-->>", "->>"} {
		if i := strings.Index(s, arrow); i >= 0 {
			return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+len(arrow):]), true
		}
	}
	return "", "", false
}

func sorobanEntry() *xdr.SorobanAuthorizationEntry {
	u := xdr.Uint32(7)
	account := xdr.MustAddress("GAPB7YMUQ6MPQ23PTFDNBZP7HGGTZPI2DT5JJURUOIXNPLGHW2O3WYXZ")
	contract := xdr.ScAddress{
		Type:       xdr.ScAddressTypeScAddressTypeContract,
		ContractId: &xdr.ContractId{},
	}
	var id xdr.Hash
	copy(id[:], bytes.Repeat([]byte{0xab}, len(id)))
	contract.ContractId = (*xdr.ContractId)(&id)

	subAddress := xdr.ScAddress{
		Type:      xdr.ScAddressTypeScAddressTypeAccount,
		AccountId: &account,
	}
	return &xdr.SorobanAuthorizationEntry{
		Credentials: xdr.SorobanCredentials{
			Type: xdr.SorobanCredentialsTypeSorobanCredentialsAddress,
			Address: &xdr.SorobanAddressCredentials{
				Address: subAddress,
			},
		},
		RootInvocation: xdr.SorobanAuthorizedInvocation{
			Function: xdr.SorobanAuthorizedFunction{
				Type: xdr.SorobanAuthorizedFunctionTypeSorobanAuthorizedFunctionTypeContractFn,
				ContractFn: &xdr.InvokeContractArgs{
					ContractAddress: contract,
					FunctionName:    "transfer",
					Args:            []xdr.ScVal{{Type: xdr.ScValTypeScvU32, U32: &u}},
				},
			},
			SubInvocations: []xdr.SorobanAuthorizedInvocation{{
				Function: xdr.SorobanAuthorizedFunction{
					Type: xdr.SorobanAuthorizedFunctionTypeSorobanAuthorizedFunctionTypeContractFn,
					ContractFn: &xdr.InvokeContractArgs{
						ContractAddress: subAddress,
						FunctionName:    "notify",
					},
				},
			}},
		},
	}
}

func TestMermaidFormatterAuthorizationEntry(t *testing.T) {
	t.Parallel()

	entry := sorobanEntry()
	value := &lens.Value{
		Type: "SorobanAuthorizationEntry",
		Node: &lens.Node{TypeName: "SorobanAuthorizationEntry", Kind: lens.KindStruct},
		Raw:  entry,
	}

	var buf bytes.Buffer
	if err := format.NewMermaidFormatter().Format(&buf, value); err != nil {
		t.Fatalf("Format: %v", err)
	}
	out := buf.String()
	validateSequence(t, out)

	for _, want := range []string{
		"participant",
		"transfer(",
		"authorize transfer(",
		"notify()",
		"-->>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "graph TD") {
		t.Errorf("authorization entry rendered as a flowchart:\n%s", out)
	}
}

func TestMermaidFormatterInvokeHostFunctionOp(t *testing.T) {
	t.Parallel()

	u := xdr.Uint32(1)
	op := &xdr.InvokeHostFunctionOp{
		HostFunction: xdr.HostFunction{
			Type: xdr.HostFunctionTypeHostFunctionTypeInvokeContract,
			InvokeContract: &xdr.InvokeContractArgs{
				ContractAddress: xdr.ScAddress{
					Type:       xdr.ScAddressTypeScAddressTypeContract,
					ContractId: (*xdr.ContractId)(&xdr.Hash{1, 2, 3}),
				},
				FunctionName: "swap",
				Args:         []xdr.ScVal{{Type: xdr.ScValTypeScvU32, U32: &u}},
			},
		},
	}
	value := &lens.Value{
		Type: "InvokeHostFunctionOp",
		Node: &lens.Node{TypeName: "InvokeHostFunctionOp", Kind: lens.KindStruct},
		Raw:  op,
	}

	var buf bytes.Buffer
	if err := format.NewMermaidFormatter().Format(&buf, value); err != nil {
		t.Fatalf("Format: %v", err)
	}
	out := buf.String()
	validateSequence(t, out)

	if !strings.Contains(out, "swap(") {
		t.Errorf("missing swap invocation:\n%s", out)
	}
	if !strings.Contains(out, "Transaction") {
		t.Errorf("missing transaction participant:\n%s", out)
	}
}

func TestMermaidFormatterFlowchartFallbackUnchanged(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := format.NewMermaidFormatter().Format(&buf, loadValue(t, "trustline_entry_simple")); err != nil {
		t.Fatalf("Format: %v", err)
	}
	out := buf.String()

	if !strings.HasPrefix(out, "graph TD\n") {
		t.Errorf("non-Soroban value should keep the flowchart format:\n%s", out)
	}
	if strings.Contains(out, "sequenceDiagram") {
		t.Errorf("non-Soroban value rendered as a sequence diagram:\n%s", out)
	}
}

func TestMermaidFormatterRejectsNilValue(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := format.NewMermaidFormatter().Format(&buf, nil); err == nil {
		t.Fatal("Format(nil) = nil error, want error")
	}
}
