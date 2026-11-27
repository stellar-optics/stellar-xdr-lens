package lens_test

import (
	"errors"
	"fmt"
	"strings"

	"github.com/stellar/go-stellar-sdk/network"
	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

func exampleFixture(name string) string {
	payload, err := readFixture(name)
	if err != nil {
		panic(err)
	}
	return payload
}

func ExampleDecode() {
	value, err := lens.Decode(exampleFixture("tx_failed.res"))
	if err != nil {
		panic(err)
	}

	fmt.Println(value.Type)
	fmt.Println(value.Node.Find("Result.Code"))
	// Output:
	// TransactionResult
	// TransactionResultCodeTxFeeBumpInnerFailed
}

func ExampleDetect() {
	candidates, err := lens.Detect("AAAAAA==")
	if err != nil {
		panic(err)
	}

	fmt.Printf("%d candidate types; preferred: %s\n", len(candidates), candidates[0].Type)
	// Output:
	// 5 candidate types; preferred: Memo
}

func ExampleDecodeAs() {
	value, err := lens.DecodeAs("AAAAAA==", "Asset")
	if err != nil {
		panic(err)
	}

	fmt.Println(value.Type)
	// Output:
	// Asset
}

func ExampleExplainPair() {
	envelope, err := lens.Decode(exampleFixture("tx_failed.env"))
	if err != nil {
		panic(err)
	}
	result, err := lens.Decode(exampleFixture("tx_failed.res"))
	if err != nil {
		panic(err)
	}

	summary, err := lens.ExplainPair(envelope, result)
	if err != nil {
		panic(err)
	}

	fmt.Println(summary.Kind)
	fmt.Println(summary.Operations[0].Type)
	fmt.Println(summary.Outcome.Reason.Code)
	// Output:
	// transaction
	// invoke_host_function
	// tx_fee_bump_inner_failed
}

func ExampleDiffBase64() {
	diff, err := lens.DiffBase64(
		exampleFixture("tx_failed.res"),
		exampleFixture("tx_soroban.res"),
		"TransactionResult",
	)
	if err != nil {
		panic(err)
	}

	fmt.Println(diff.LeftType, "vs", diff.RightType)
	fmt.Println(len(diff.Changes) > 0)
	// Output:
	// TransactionResult vs TransactionResult
	// true
}

func ExampleDescribeAsset() {
	details, err := lens.DescribeAsset(exampleFixture("asset_usdc"), network.PublicNetworkPassphrase)
	if err != nil {
		panic(err)
	}

	fmt.Println(details.Canonical)
	fmt.Println(strings.HasPrefix(details.ContractID, "C"))
	// Output:
	// USDC:GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN
	// true
}

func ExampleNode_Find() {
	value, err := lens.Decode(exampleFixture("tx_failed.res"))
	if err != nil {
		panic(err)
	}

	fmt.Println(value.Node.Find("Result.Code"))
	fmt.Println(value.Node.Find("missing.path") == nil)
	// Output:
	// TransactionResultCodeTxFeeBumpInnerFailed
	// true
}

func ExampleDescribeDataValue() {
	text, textual := lens.DescribeDataValue([]byte("hello"))
	hex, hexTextual := lens.DescribeDataValue([]byte{0, 255})

	fmt.Println(text, textual)
	fmt.Println(hex, hexTextual)
	// Output:
	// hello true
	// 00ff false
}

func ExampleReasonByCode() {
	reason, ok := lens.ReasonByCode("  TX_BAD_SEQ ")
	fmt.Println(ok)
	fmt.Println(reason.Code)
	fmt.Println(reason.Success)
	fmt.Println(len(lens.AllReasons()) == len(lens.ReasonLookupTable()))
	// Output:
	// true
	// tx_bad_seq
	// false
	// true
}

func ExampleProtocol21Reasons() {
	reason, ok := lens.ExplainProtocol21Code("tx_frozen_key_accessed")
	fmt.Println(lens.IsProtocol21Code("tx_frozen_key_accessed"))
	fmt.Println(ok, reason.Code)
	fmt.Println(len(lens.Protocol21Reasons()) > 0)
	// Output:
	// true
	// true tx_frozen_key_accessed
	// true
}

func ExampleRegistryIndex() {
	index := lens.RegistryIndex()
	names := lens.TypeNames()

	fmt.Println(len(index) > 0)
	fmt.Println(index[0].Name == names[0])
	fmt.Println(index[0].Category != "")
	// Output:
	// true
	// true
	// true
}

func ExampleParseContractCode() {
	value, err := lens.DecodeAs(exampleFixture("contract_code"), "ContractCodeEntry")
	if err != nil {
		panic(err)
	}
	contract, ok := value.Raw.(*xdr.ContractCodeEntry)
	if !ok {
		panic(fmt.Sprintf("unexpected decoded value %T", value.Raw))
	}

	parsed, err := lens.ParseContractCode(contract.Code)
	if err != nil {
		panic(err)
	}
	spec, err := lens.ParseContractSpec(contract.Code)
	if err != nil {
		panic(err)
	}
	extracted, err := lens.ExtractWasmContractSpec(contract.Code)
	if err != nil {
		panic(err)
	}

	fmt.Println(len(extracted) > 0)
	fmt.Println(len(parsed.Spec) > 0)
	fmt.Println(len(spec.Functions) > 0)
	// Output:
	// true
	// true
	// true
}

func ExampleDecode_errorClassification() {
	_, err := lens.Decode("not base64")
	fmt.Println(errors.Is(err, lens.ErrInvalidBase64))

	var kind lens.ErrorKind
	fmt.Println(errors.As(err, &kind), kind)
	// Output:
	// true
	// true input is not valid base64
}
