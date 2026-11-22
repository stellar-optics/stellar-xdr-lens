package lens

import (
	"fmt"
	"reflect"

	"github.com/stellar/go-stellar-sdk/network"
	"github.com/stellar/go-stellar-sdk/xdr"
)

// Contract executables are the union that says how a Soroban contract
// instance is implemented: a WASM module identified by its SHA-256 hash, the
// protocol's built-in Stellar Asset Contract, or — from Protocol 23 — an
// external reference owned by another address.
//
// A raw discriminant plus a bare hash is the least useful rendering of a
// contract: the hash is the only handle that ties a deployed instance back to
// its uploaded bytecode, and "1" says nothing about the built-in token. The
// enricher here folds each arm into one scalar so the value stays readable in
// every output format. It reads the decoded value only: no network calls, no
// clock use.

func init() {
	registerEnricher(reflect.TypeOf(xdr.ContractExecutable{}), enrichContractExecutable)
	nodeHooks[reflect.TypeOf(xdr.CreateContractArgs{})] = decorateCreateContractArgs
	nodeHooks[reflect.TypeOf(xdr.CreateContractArgsV2{})] = decorateCreateContractArgsV2
	nodeHooks[reflect.TypeOf(xdr.ContractIdPreimage{})] = decorateContractIdPreimage
	registerDerivedNodeFields(reflect.TypeOf(xdr.CreateContractArgs{}), "ContractID")
	registerDerivedNodeFields(reflect.TypeOf(xdr.CreateContractArgsV2{}), "ContractID")
	registerDerivedNodeFields(reflect.TypeOf(xdr.ContractIdPreimage{}), "ContractID")
}

// FormatSACNote formats the SAC enrichment note for an asset.
// For native XLM, it displays "Built-in Stellar Asset Contract (SAC) wrapping Native XLM".
// For classic assets, it displays "Built-in Stellar Asset Contract (SAC) wrapping asset CODE:ISSUER".
func FormatSACNote(asset xdr.Asset) string {
	if asset.IsNative() {
		return "Built-in Stellar Asset Contract (SAC) wrapping Native XLM"
	}
	return "Built-in Stellar Asset Contract (SAC) wrapping asset " + asset.StringCanonical()
}

// FormatSACContractIDNote returns a note with the deterministic contract address derived from the asset.
func FormatSACContractIDNote(asset xdr.Asset, networkPassphrase string) (string, error) {
	if networkPassphrase == "" {
		networkPassphrase = network.PublicNetworkPassphrase
	}
	contractID, err := SACContractID(asset, networkPassphrase)
	if err != nil {
		return "", err
	}
	return "contract address: " + contractID, nil
}

// EnrichSACExecutable describes a built-in Stellar Asset Contract executable with its asset and contract ID.
func EnrichSACExecutable(asset xdr.Asset, networkPassphrase string) (note string, contractID string, err error) {
	if networkPassphrase == "" {
		networkPassphrase = network.PublicNetworkPassphrase
	}
	baseNote := FormatSACNote(asset)
	id, err := SACContractID(asset, networkPassphrase)
	if err != nil {
		return baseNote, "", err
	}
	fullNote := fmt.Sprintf("%s (contract address: %s)", baseNote, id)
	return fullNote, id, nil
}

// DecodeSACAsset extracts the underlying classic xdr.Asset from a ContractIdPreimage if it represents a SAC asset.
func DecodeSACAsset(preimage xdr.ContractIdPreimage) (*xdr.Asset, bool) {
	if preimage.Type == xdr.ContractIdPreimageTypeContractIdPreimageFromAsset && preimage.FromAsset != nil {
		return preimage.FromAsset, true
	}
	return nil, false
}

// EnrichContractExecutableWithAsset enriches a SAC ContractExecutable using an explicit asset and network passphrase.
func EnrichContractExecutableWithAsset(exec xdr.ContractExecutable, asset xdr.Asset, networkPassphrase string) (note string, contractID string, ok bool) {
	if exec.Type != xdr.ContractExecutableTypeContractExecutableStellarAsset {
		return "", "", false
	}
	note, contractID, err := EnrichSACExecutable(asset, networkPassphrase)
	if err != nil {
		return FormatSACNote(asset), "", false
	}
	return note, contractID, true
}

// enrichContractExecutable renders all three arms and declines unknown
// discriminants, leaving the generic walker to show them as they arrive.
func enrichContractExecutable(rv reflect.Value) (display string, raw any, ok bool) {
	exec, valid := rv.Interface().(xdr.ContractExecutable)
	if !valid {
		return "", nil, false
	}
	switch exec.Type {
	case xdr.ContractExecutableTypeContractExecutableWasm:
		if exec.WasmHash == nil {
			return "", nil, false
		}
		hexHash := exec.WasmHash.HexString()
		return "wasm:" + hexHash, hexHash, true
	case xdr.ContractExecutableTypeContractExecutableStellarAsset:
		return "stellar asset contract (built-in token)", "stellar_asset", true
	case xdr.ContractExecutableTypeContractExecutableExternalRef:
		if exec.ExternalRef == nil {
			return "", nil, false
		}
		return externalRefExecutable(exec.ExternalRef)
	default:
		return "", nil, false
	}
}

// decorateCreateContractArgs enriches CreateContractArgs when it deploys a SAC instance.
func decorateCreateContractArgs(n *Node, v any) {
	args, ok := v.(xdr.CreateContractArgs)
	if !ok {
		return
	}
	decorateSACDeployment(n, args.ContractIdPreimage, args.Executable)
}

// decorateCreateContractArgsV2 enriches CreateContractArgsV2 when it deploys a SAC instance.
func decorateCreateContractArgsV2(n *Node, v any) {
	args, ok := v.(xdr.CreateContractArgsV2)
	if !ok {
		return
	}
	decorateSACDeployment(n, args.ContractIdPreimage, args.Executable)
}

// decorateSACDeployment annotates the Executable and ContractIdPreimage nodes with SAC metadata and derived contract address.
func decorateSACDeployment(n *Node, preimage xdr.ContractIdPreimage, exec xdr.ContractExecutable) {
	if exec.Type != xdr.ContractExecutableTypeContractExecutableStellarAsset {
		return
	}
	asset, ok := DecodeSACAsset(preimage)
	if !ok || asset == nil {
		return
	}
	note, contractID, err := EnrichSACExecutable(*asset, network.PublicNetworkPassphrase)
	if err != nil {
		return
	}

	if execNode := n.Child("Executable"); execNode != nil {
		execNode.Note = note
	}
	if piNode := n.Child("ContractIdPreimage"); piNode != nil {
		piNode.Note = "SAC contract address: " + contractID
	}
	n.Children = append(n.Children, &Node{
		Name:  "ContractID",
		Kind:  KindScalar,
		Value: contractID,
		Note:  "deterministic contract address for SAC",
	})
}

// decorateContractIdPreimage annotates a standalone ContractIdPreimage when it represents a SAC asset.
func decorateContractIdPreimage(n *Node, v any) {
	preimage, ok := v.(xdr.ContractIdPreimage)
	if !ok {
		return
	}
	asset, ok := DecodeSACAsset(preimage)
	if !ok || asset == nil {
		return
	}
	note, contractID, err := EnrichSACExecutable(*asset, network.PublicNetworkPassphrase)
	if err != nil {
		return
	}
	n.Note = note
	n.Children = append(n.Children, &Node{
		Name:  "ContractID",
		Kind:  KindScalar,
		Value: contractID,
		Note:  "deterministic contract address for SAC",
	})
}

// externalRefExecutable describes a Protocol 23 external executable
// reference: the owning address, and the implementation-defined tag that
// tells hosts which external system it points at. A malformed owner (missing
// union arm) errors inside ScAddress.String and the enrich recover upstream
// falls back to the generic rendering.
func externalRefExecutable(ref *xdr.ContractExecutableExternalRef) (string, any, bool) {
	owner, err := ref.ExecutableOwner.String()
	if err != nil || owner == "" {
		return "", nil, false
	}
	value := owner
	if ref.Tag != "" {
		value = owner + " tag: " + string(ref.Tag)
	}
	return "external contract reference", value, true
}
