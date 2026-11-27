package lens

import (
	"encoding/hex"
	"fmt"

	"github.com/stellar/go-stellar-sdk/xdr"
)

// DiffScVal recursively compares two Soroban values. Vector order is
// significant; map entries are matched by their canonical encoded key.
func DiffScVal(left, right xdr.ScVal) (*DiffResult, error) {
	return diffScVal(left, right, false)
}

func diffScVal(left, right xdr.ScVal, semantic bool) (*DiffResult, error) {
	leftNode, err := scValNode("", left, semantic)
	if err != nil {
		return nil, fmt.Errorf("building left ScVal: %w", err)
	}
	rightNode, err := scValNode("", right, semantic)
	if err != nil {
		return nil, fmt.Errorf("building right ScVal: %w", err)
	}
	result := &DiffResult{LeftType: "ScVal", RightType: "ScVal"}
	diffNodesWithOptions(leftNode, rightNode, "", &result.Changes, DiffOptions{})
	return result, nil
}

func scValNode(name string, value xdr.ScVal, semantic bool) (*Node, error) {
	node := nodeFor(name, value)
	if node == nil {
		return nil, fmt.Errorf("%w: nil ScVal node", ErrInvalidArgument)
	}

	if semantic {
		if numeric, ok := semanticScValInteger(value); ok {
			arm := semanticScValIntegerArm(value.Type)
			return &Node{
				Name: name, Kind: KindStruct, TypeName: "ScVal",
				Children: []*Node{
					{Name: "Type", Kind: KindScalar, Value: value.Type.String()},
					{Name: arm, Kind: KindScalar, TypeName: "integer", Value: numeric},
				},
			}, nil
		}
	}

	switch value.Type {
	case xdr.ScValTypeScvVec:
		if value.Vec == nil || *value.Vec == nil {
			return nil, fmt.Errorf("%w: ScVal vector arm is missing", ErrInvalidArgument)
		}
		vec := **value.Vec
		children := make([]*Node, 0, len(vec))
		for _, item := range vec {
			child, err := scValNode("", item, semantic)
			if err != nil {
				return nil, fmt.Errorf("vector item %d: %w", len(children), err)
			}
			children = append(children, child)
		}
		replaceChild(node, "Vec", &Node{Name: "Vec", Kind: KindList, TypeName: "ScVec", Children: children})

	case xdr.ScValTypeScvMap:
		if value.Map == nil || *value.Map == nil {
			return nil, fmt.Errorf("%w: ScVal map arm is missing", ErrInvalidArgument)
		}
		entries := **value.Map
		mapNode := &Node{Name: "Map", Kind: KindStruct, TypeName: "ScMap"}
		for _, entry := range entries {
			keyBytes, err := entry.Key.MarshalBinary()
			if err != nil {
				return nil, fmt.Errorf("encoding map key: %w", err)
			}
			keyName := "key:" + hex.EncodeToString(keyBytes)
			child, err := scValNode(keyName, entry.Val, semantic)
			if err != nil {
				return nil, fmt.Errorf("map value %s: %w", keyName, err)
			}
			mapNode.Children = append(mapNode.Children, child)
		}
		replaceChild(node, "Map", mapNode)
	}

	return node, nil
}

func semanticScValIntegerArm(valueType xdr.ScValType) string {
	switch valueType {
	case xdr.ScValTypeScvU128:
		return "U128"
	case xdr.ScValTypeScvI128:
		return "I128"
	case xdr.ScValTypeScvU256:
		return "U256"
	case xdr.ScValTypeScvI256:
		return "I256"
	default:
		return "Value"
	}
}

func replaceChild(parent *Node, name string, replacement *Node) {
	for i, child := range parent.Children {
		if child.Name == name {
			parent.Children[i] = replacement
			return
		}
	}
	parent.Children = append(parent.Children, replacement)
}

// DiffContractData compares two ContractDataEntry values by durability and the
// Val ScVal recursively.  Durability is compared as a plain string change;
// the Val field uses the same ScVal differ so nested vectors and maps get
// key-based matching rather than positional matching.
func DiffContractData(left, right *xdr.ContractDataEntry, opts DiffOptions) (*DiffResult, error) {
	if left == nil || right == nil {
		return nil, fmt.Errorf("%w: ContractDataEntry must not be nil", ErrInvalidArgument)
	}
	result := &DiffResult{LeftType: "ContractDataEntry", RightType: "ContractDataEntry"}

	// Durability is a plain enum; compare it directly.
	leftDur := left.Durability.String()
	rightDur := right.Durability.String()
	if leftDur != rightDur {
		result.Changes = append(result.Changes, Change{
			Path:   "Durability",
			Op:     ChangeModified,
			Before: leftDur,
			After:  rightDur,
		})
	}

	// Val is an ScVal; recurse through it with key-based map matching.
	semantic := opts.Semantic
	leftNode, err := scValNode("Val", left.Val, semantic)
	if err != nil {
		return nil, fmt.Errorf("building left ContractDataEntry.Val: %w", err)
	}
	rightNode, err := scValNode("Val", right.Val, semantic)
	if err != nil {
		return nil, fmt.Errorf("building right ContractDataEntry.Val: %w", err)
	}
	diffNodesWithOptions(leftNode, rightNode, "Val", &result.Changes, opts)

	return result, nil
}

func isScVal(raw any) (xdr.ScVal, bool) {
	switch value := raw.(type) {
	case *xdr.ScVal:
		if value != nil {
			return *value, true
		}
	case xdr.ScVal:
		return value, true
	}
	return xdr.ScVal{}, false
}

func scValNodeForValue(value *Value, semantic bool) (*Node, error) {
	if value == nil {
		return nil, fmt.Errorf("%w: nil decoded value", ErrInvalidArgument)
	}
	if scVal, ok := isScVal(value.Raw); ok {
		return scValNode("", scVal, semantic)
	}
	if value.Node == nil {
		return nil, fmt.Errorf("%w: decoded value has no node", ErrInvalidArgument)
	}
	return value.Node, nil
}
