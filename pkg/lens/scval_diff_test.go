package lens_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

func symbol(value string) xdr.ScVal {
	sym := xdr.ScSymbol(value)
	return xdr.ScVal{Type: xdr.ScValTypeScvSymbol, Sym: &sym}
}

func scVec(items ...xdr.ScVal) xdr.ScVal {
	vec := xdr.ScVec(items)
	vecPointer := &vec
	return xdr.ScVal{Type: xdr.ScValTypeScvVec, Vec: &vecPointer}
}

func scMap(entries ...xdr.ScMapEntry) xdr.ScVal {
	mapping := xdr.ScMap(entries)
	mapPointer := &mapping
	return xdr.ScVal{Type: xdr.ScValTypeScvMap, Map: &mapPointer}
}

func TestDiffScValMatchesMapEntriesByKey(t *testing.T) {
	t.Parallel()

	left := scMap(
		xdr.ScMapEntry{Key: symbol("a"), Val: symbol("one")},
		xdr.ScMapEntry{Key: symbol("b"), Val: symbol("two")},
	)
	right := scMap(
		xdr.ScMapEntry{Key: symbol("new"), Val: symbol("added")},
		xdr.ScMapEntry{Key: symbol("a"), Val: symbol("one")},
		xdr.ScMapEntry{Key: symbol("b"), Val: symbol("two")},
	)

	got, err := lens.DiffScVal(left, right)
	if err != nil {
		t.Fatalf("DiffScVal() error = %v", err)
	}
	if len(got.Changes) != 1 || got.Changes[0].Op != lens.ChangeAdded {
		t.Fatalf("DiffScVal() changes = %+v, want one added map entry", got.Changes)
	}
	if !strings.Contains(got.Changes[0].Path, "Map.key:") {
		t.Errorf("map change path = %q, want an exact-key path", got.Changes[0].Path)
	}
}

func TestDiffScValRecursesThroughNestedVectorsAndMaps(t *testing.T) {
	t.Parallel()

	left := scMap(xdr.ScMapEntry{Key: symbol("items"), Val: scVec(symbol("before"))})
	right := scMap(xdr.ScMapEntry{Key: symbol("items"), Val: scVec(symbol("after"))})
	got, err := lens.DiffScVal(left, right)
	if err != nil {
		t.Fatalf("DiffScVal() error = %v", err)
	}
	if len(got.Changes) != 1 {
		t.Fatalf("DiffScVal() changes = %+v, want one nested change", got.Changes)
	}
	if !strings.Contains(got.Changes[0].Path, "Map.key:") || !strings.HasSuffix(got.Changes[0].Path, "Vec[0].Sym") {
		t.Errorf("nested path = %q, want map key and vector index", got.Changes[0].Path)
	}
}

func TestDiffScValRejectsMissingRecursiveArms(t *testing.T) {
	t.Parallel()

	tests := []xdr.ScVal{
		{Type: xdr.ScValTypeScvVec},
		{Type: xdr.ScValTypeScvMap},
	}
	for _, value := range tests {
		if _, err := lens.DiffScVal(value, value); !errors.Is(err, lens.ErrInvalidArgument) {
			t.Errorf("DiffScVal(%s) error = %v, want ErrInvalidArgument", value.Type, err)
		}
	}
}

func TestDiffSemanticNormalizesWideIntegers(t *testing.T) {
	t.Parallel()

	leftRaw := xdr.ScVal{Type: xdr.ScValTypeScvI128, I128: &xdr.Int128Parts{Hi: 1, Lo: 5}}
	rightRaw := xdr.ScVal{Type: xdr.ScValTypeScvI128, I128: &xdr.Int128Parts{Hi: 1, Lo: 6}}
	leftNode, err := lens.DecodeAs(mustMarshal(t, leftRaw), "ScVal")
	if err != nil {
		t.Fatalf("DecodeAs(left) error = %v", err)
	}
	rightNode, err := lens.DecodeAs(mustMarshal(t, rightRaw), "ScVal")
	if err != nil {
		t.Fatalf("DecodeAs(right) error = %v", err)
	}
	leftNode.Raw, rightNode.Raw = &leftRaw, &rightRaw

	got, err := lens.DiffSemantic(leftNode, rightNode)
	if err != nil {
		t.Fatalf("DiffSemantic() error = %v", err)
	}
	if len(got.Changes) != 1 || got.Changes[0].Path != "I128" {
		t.Fatalf("DiffSemantic() changes = %+v, want one I128 change", got.Changes)
	}
	if got.Changes[0].Before != "18446744073709551621" || got.Changes[0].After != "18446744073709551622" {
		t.Errorf("semantic values = %v -> %v, want exact decimal integers", got.Changes[0].Before, got.Changes[0].After)
	}
}

func TestDiffSemanticNormalizesAllWideIntegerVariants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		left, right xdr.ScVal
		before      string
		after       string
	}{
		{
			name:   "u128",
			left:   xdr.ScVal{Type: xdr.ScValTypeScvU128, U128: &xdr.UInt128Parts{Hi: 1, Lo: 2}},
			right:  xdr.ScVal{Type: xdr.ScValTypeScvU128, U128: &xdr.UInt128Parts{Hi: 1, Lo: 3}},
			before: "18446744073709551618", after: "18446744073709551619",
		},
		{
			name:   "i128 negative",
			left:   xdr.ScVal{Type: xdr.ScValTypeScvI128, I128: &xdr.Int128Parts{Hi: -1, Lo: xdr.Uint64(^uint64(0))}},
			right:  xdr.ScVal{Type: xdr.ScValTypeScvI128, I128: &xdr.Int128Parts{Hi: -1, Lo: xdr.Uint64(^uint64(0) - 1)}},
			before: "-1", after: "-2",
		},
		{
			name:   "u256",
			left:   xdr.ScVal{Type: xdr.ScValTypeScvU256, U256: &xdr.UInt256Parts{HiHi: 1}},
			right:  xdr.ScVal{Type: xdr.ScValTypeScvU256, U256: &xdr.UInt256Parts{HiHi: 1, LoLo: 1}},
			before: "6277101735386680763835789423207666416102355444464034512896",
			after:  "6277101735386680763835789423207666416102355444464034512897",
		},
		{
			name:   "i256 negative",
			left:   xdr.ScVal{Type: xdr.ScValTypeScvI256, I256: &xdr.Int256Parts{HiHi: -1, HiLo: xdr.Uint64(^uint64(0)), LoHi: xdr.Uint64(^uint64(0)), LoLo: xdr.Uint64(^uint64(0))}},
			right:  xdr.ScVal{Type: xdr.ScValTypeScvI256, I256: &xdr.Int256Parts{HiHi: -1, HiLo: xdr.Uint64(^uint64(0)), LoHi: xdr.Uint64(^uint64(0)), LoLo: xdr.Uint64(^uint64(0) - 1)}},
			before: "-1", after: "-2",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			left, err := lens.DecodeAs(mustMarshal(t, tc.left), "ScVal")
			if err != nil {
				t.Fatalf("DecodeAs(left) error = %v", err)
			}
			right, err := lens.DecodeAs(mustMarshal(t, tc.right), "ScVal")
			if err != nil {
				t.Fatalf("DecodeAs(right) error = %v", err)
			}
			left.Raw, right.Raw = &tc.left, &tc.right
			got, err := lens.DiffSemantic(left, right)
			if err != nil {
				t.Fatalf("DiffSemantic() error = %v", err)
			}
			if len(got.Changes) != 1 || got.Changes[0].Before != tc.before || got.Changes[0].After != tc.after {
				t.Errorf("DiffSemantic() changes = %+v, want %q -> %q", got.Changes, tc.before, tc.after)
			}
		})
	}
}

// TestDiffContractDataDetectsDurabilityChange verifies issue #281: DiffContractData
// reports a change when the Durability field differs between two entries.
func TestDiffContractDataDetectsDurabilityChange(t *testing.T) {
	t.Parallel()

	key := xdr.ScVal{Type: xdr.ScValTypeScvU32, U32: &[]xdr.Uint32{1}[0]}
	left := &xdr.ContractDataEntry{
		Contract:   xdr.ScAddress{Type: xdr.ScAddressTypeScAddressTypeContract},
		Key:        key,
		Durability: xdr.ContractDataDurabilityPersistent,
		Val:        symbol("before"),
	}
	right := &xdr.ContractDataEntry{
		Contract:   xdr.ScAddress{Type: xdr.ScAddressTypeScAddressTypeContract},
		Key:        key,
		Durability: xdr.ContractDataDurabilityTemporary,
		Val:        symbol("before"),
	}

	got, err := lens.DiffContractData(left, right, lens.DiffOptions{})
	if err != nil {
		t.Fatalf("DiffContractData() error = %v", err)
	}
	if len(got.Changes) != 1 {
		t.Fatalf("DiffContractData() changes = %+v, want one Durability change", got.Changes)
	}
	if got.Changes[0].Path != "Durability" {
		t.Errorf("path = %q, want Durability", got.Changes[0].Path)
	}
}

// TestDiffContractDataMatchesMapEntriesByKey verifies issue #281: DiffContractData
// uses key-based matching for ScVal map entries in the Val field.
func TestDiffContractDataMatchesMapEntriesByKey(t *testing.T) {
	t.Parallel()

	key := xdr.ScVal{Type: xdr.ScValTypeScvU32, U32: &[]xdr.Uint32{1}[0]}
	leftVal := scMap(
		xdr.ScMapEntry{Key: symbol("a"), Val: symbol("one")},
		xdr.ScMapEntry{Key: symbol("b"), Val: symbol("two")},
	)
	rightVal := scMap(
		xdr.ScMapEntry{Key: symbol("new"), Val: symbol("added")},
		xdr.ScMapEntry{Key: symbol("a"), Val: symbol("one")},
		xdr.ScMapEntry{Key: symbol("b"), Val: symbol("two")},
	)

	left := &xdr.ContractDataEntry{
		Contract:   xdr.ScAddress{Type: xdr.ScAddressTypeScAddressTypeContract},
		Key:        key,
		Durability: xdr.ContractDataDurabilityPersistent,
		Val:        leftVal,
	}
	right := &xdr.ContractDataEntry{
		Contract:   xdr.ScAddress{Type: xdr.ScAddressTypeScAddressTypeContract},
		Key:        key,
		Durability: xdr.ContractDataDurabilityPersistent,
		Val:        rightVal,
	}

	got, err := lens.DiffContractData(left, right, lens.DiffOptions{})
	if err != nil {
		t.Fatalf("DiffContractData() error = %v", err)
	}
	if len(got.Changes) != 1 || got.Changes[0].Op != lens.ChangeAdded {
		t.Fatalf("DiffContractData() changes = %+v, want one added map entry", got.Changes)
	}
}

func TestDiffSemanticFallsBackForNonScVal(t *testing.T) {
	t.Parallel()

	left, err := lens.DecodeAs(mustMarshal(t, xdr.Price{N: 1, D: 2}), "Price")
	if err != nil {
		t.Fatalf("DecodeAs(left) error = %v", err)
	}
	right, err := lens.DecodeAs(mustMarshal(t, xdr.Price{N: 2, D: 3}), "Price")
	if err != nil {
		t.Fatalf("DecodeAs(right) error = %v", err)
	}
	left.Raw, right.Raw = int64(1), int64(2)
	got, err := lens.DiffSemantic(left, right)
	if err != nil {
		t.Fatalf("DiffSemantic() error = %v", err)
	}
	if len(got.Changes) != 1 {
		t.Errorf("DiffSemantic() changes = %+v, want the structural Price change", got.Changes)
	}

	missingNode := &lens.Value{Type: "opaque", Raw: "not an ScVal"}
	if _, err := lens.DiffSemantic(missingNode, right); !errors.Is(err, lens.ErrInvalidArgument) {
		t.Errorf("DiffSemantic(missing node) error = %v, want ErrInvalidArgument", err)
	}
}
