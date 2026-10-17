package lens

import (
	"reflect"

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
		// The arm is void: which asset a Stellar Asset Contract wraps is
		// encoded in the contract address, not here, so the executable can
		// only be named, not given an asset string.
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
