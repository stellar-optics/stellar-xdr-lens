package lens

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/stellar/go-stellar-sdk/xdr"
)

// Footprint set diffing.
//
// A Soroban transaction declares the ledger entries it will read and write as
// a footprint: a list of read-only keys and a list of read-write keys. The
// lists are sets, not sequences, so the useful question when comparing two
// footprints is set-theoretic: which keys are new, which are gone, and which
// are common to both. These helpers answer that locally, from supplied XDR,
// with no ledger or RPC access.

// FootprintKeySetDiff is the set-theoretic (Venn) difference between two
// collections of ledger keys. Keys are identified by their canonical XDR
// encoding, so a key that appears in both footprints is common regardless of
// its position, and a duplicate is counted once.
type FootprintKeySetDiff struct {
	// Added holds keys present only on the right-hand side.
	Added []string `json:"added,omitempty"`
	// Removed holds keys present only on the left-hand side.
	Removed []string `json:"removed,omitempty"`
	// Common holds keys present on both sides.
	Common []string `json:"common,omitempty"`
}

// Equal reports whether the two key sets were identical.
func (d FootprintKeySetDiff) Equal() bool { return len(d.Added) == 0 && len(d.Removed) == 0 }

// Summary renders a one-line description of the set difference.
func (d FootprintKeySetDiff) Summary() string {
	if d.Equal() {
		return "no footprint changes"
	}
	parts := make([]string, 0, 3)
	if len(d.Removed) > 0 {
		parts = append(parts, strconv.Itoa(len(d.Removed))+" removed")
	}
	if len(d.Added) > 0 {
		parts = append(parts, strconv.Itoa(len(d.Added))+" added")
	}
	parts = append(parts, strconv.Itoa(len(d.Common))+" common")
	return strings.Join(parts, ", ")
}

// FootprintDiff is the read-only and read-write set differences between two
// Soroban ledger footprints.
type FootprintDiff struct {
	// ReadOnly is the difference between the two read-only key sets.
	ReadOnly FootprintKeySetDiff `json:"readOnly"`
	// ReadWrite is the difference between the two read-write key sets.
	ReadWrite FootprintKeySetDiff `json:"readWrite"`
}

// Equal reports whether both halves of the footprint were identical.
func (d FootprintDiff) Equal() bool { return d.ReadOnly.Equal() && d.ReadWrite.Equal() }

// Summary renders a one-line description of both halves.
func (d FootprintDiff) Summary() string {
	if d.Equal() {
		return "no footprint changes"
	}
	return fmt.Sprintf("read-only: %s; read-write: %s", d.ReadOnly.Summary(), d.ReadWrite.Summary())
}

// footprintKey pairs a canonical identity with a human-readable rendering.
type footprintKey struct {
	key   string
	label string
}

// DiffFootprintKeys computes the set difference between two ledger-key
// collections. Order is ignored and duplicates are collapsed, so the function
// reports membership rather than sequence.
func DiffFootprintKeys(left, right []xdr.LedgerKey) (*FootprintKeySetDiff, error) {
	leftKeys, err := footprintKeys(left)
	if err != nil {
		return nil, fmt.Errorf("left footprint: %w", err)
	}
	rightKeys, err := footprintKeys(right)
	if err != nil {
		return nil, fmt.Errorf("right footprint: %w", err)
	}

	rightSet := make(map[string]struct{}, len(rightKeys))
	for _, key := range rightKeys {
		rightSet[key.key] = struct{}{}
	}
	leftSet := make(map[string]struct{}, len(leftKeys))
	for _, key := range leftKeys {
		leftSet[key.key] = struct{}{}
	}

	diff := &FootprintKeySetDiff{}
	for _, key := range leftKeys {
		if _, ok := rightSet[key.key]; ok {
			diff.Common = append(diff.Common, key.label)
		} else {
			diff.Removed = append(diff.Removed, key.label)
		}
	}
	for _, key := range rightKeys {
		if _, ok := leftSet[key.key]; !ok {
			diff.Added = append(diff.Added, key.label)
		}
	}
	return diff, nil
}

// DiffFootprints computes the read-only and read-write set differences between
// two footprints.
func DiffFootprints(left, right xdr.LedgerFootprint) (*FootprintDiff, error) {
	readOnly, err := DiffFootprintKeys(left.ReadOnly, right.ReadOnly)
	if err != nil {
		return nil, fmt.Errorf("read-only keys: %w", err)
	}
	readWrite, err := DiffFootprintKeys(left.ReadWrite, right.ReadWrite)
	if err != nil {
		return nil, fmt.Errorf("read-write keys: %w", err)
	}
	return &FootprintDiff{ReadOnly: *readOnly, ReadWrite: *readWrite}, nil
}

// DiffSorobanFootprints compares the footprints of two Soroban transaction
// data values. A nil value is rejected rather than treated as an empty
// footprint, since "no data" and "empty footprint" are different claims.
func DiffSorobanFootprints(left, right *xdr.SorobanTransactionData) (*FootprintDiff, error) {
	if left == nil || right == nil {
		return nil, fmt.Errorf("%w: two SorobanTransactionData values are required", ErrInvalidArgument)
	}
	return DiffFootprints(left.Resources.Footprint, right.Resources.Footprint)
}

// DiffEnvelopeFootprints compares the Soroban footprints declared by two
// transaction envelopes. It returns an error when either envelope carries no
// Soroban data, because there is then nothing to compare.
func DiffEnvelopeFootprints(left, right *xdr.TransactionEnvelope) (*FootprintDiff, error) {
	if left == nil || right == nil {
		return nil, fmt.Errorf("%w: two TransactionEnvelope values are required", ErrInvalidArgument)
	}
	leftFootprint, ok := FootprintFromEnvelope(left)
	if !ok {
		return nil, fmt.Errorf("%w: left envelope declares no Soroban footprint", ErrUnsupportedType)
	}
	rightFootprint, ok := FootprintFromEnvelope(right)
	if !ok {
		return nil, fmt.Errorf("%w: right envelope declares no Soroban footprint", ErrUnsupportedType)
	}
	return DiffFootprints(leftFootprint, rightFootprint)
}

// FootprintFromEnvelope extracts the ledger footprint declared by a
// transaction envelope, for either a classic V1 transaction or the inner
// transaction of a fee bump. It reports false when the envelope carries no
// Soroban data.
func FootprintFromEnvelope(env *xdr.TransactionEnvelope) (xdr.LedgerFootprint, bool) {
	data := sorobanDataFromEnvelope(env)
	if data == nil {
		return xdr.LedgerFootprint{}, false
	}
	return data.Resources.Footprint, true
}

// FootprintFromValue extracts the footprint from a decoded value that is
// either a TransactionEnvelope or a SorobanTransactionData. It reports false
// for any other type or when no footprint is present.
func FootprintFromValue(v *Value) (xdr.LedgerFootprint, bool) {
	if v == nil {
		return xdr.LedgerFootprint{}, false
	}
	switch raw := v.Raw.(type) {
	case *xdr.TransactionEnvelope:
		return FootprintFromEnvelope(raw)
	case *xdr.SorobanTransactionData:
		if raw == nil {
			return xdr.LedgerFootprint{}, false
		}
		return raw.Resources.Footprint, true
	default:
		return xdr.LedgerFootprint{}, false
	}
}

// sorobanDataFromEnvelope digs out the Soroban transaction data from a V1 or
// fee-bump envelope, returning nil when it is absent.
func sorobanDataFromEnvelope(env *xdr.TransactionEnvelope) *xdr.SorobanTransactionData {
	if env == nil {
		return nil
	}
	switch env.Type {
	case xdr.EnvelopeTypeEnvelopeTypeTx:
		if env.V1 == nil {
			return nil
		}
		return sorobanDataFromExt(env.V1.Tx.Ext)
	case xdr.EnvelopeTypeEnvelopeTypeTxFeeBump:
		if env.FeeBump == nil || env.FeeBump.Tx.InnerTx.V1 == nil {
			return nil
		}
		return sorobanDataFromExt(env.FeeBump.Tx.InnerTx.V1.Tx.Ext)
	default:
		return nil
	}
}

func sorobanDataFromExt(ext xdr.TransactionExt) *xdr.SorobanTransactionData {
	data, ok := ext.GetSorobanData()
	if !ok {
		return nil
	}
	return &data
}

// footprintKeys renders ledger keys as canonical identities plus labels,
// preserving first-seen order and collapsing duplicates.
func footprintKeys(keys []xdr.LedgerKey) ([]footprintKey, error) {
	out := make([]footprintKey, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for i := range keys {
		bytes, err := keys[i].MarshalBinary()
		if err != nil {
			return nil, fmt.Errorf("encoding key %d: %w", i, err)
		}
		id := hex.EncodeToString(bytes)
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		label, ok := ledgerKeySummary(keys[i])
		if !ok || label == "" {
			label = "ledger key " + id
		}
		out = append(out, footprintKey{key: id, label: label})
	}
	return out, nil
}
