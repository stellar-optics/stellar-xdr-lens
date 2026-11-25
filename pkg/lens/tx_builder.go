package lens

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/stellar/go-stellar-sdk/xdr"
)

// ErrInvalidTxSpec is returned when a transaction template is missing required fields or malformed.
var ErrInvalidTxSpec = errors.New("invalid transaction specification")

// TxSpec defines a simplified JSON specification for constructing an xdr.TransactionEnvelope.
type TxSpec struct {
	Type            string            `json:"type,omitempty"` // "v1" (default), "v0", "fee_bump"
	SourceAccount   string            `json:"source_account"`
	Fee             any               `json:"fee"`
	Sequence        any               `json:"sequence"`
	TimeBounds      *TimeBoundsSpec   `json:"time_bounds,omitempty"`
	LedgerBounds    *LedgerBoundsSpec `json:"ledger_bounds,omitempty"`
	MinSeqNum       any               `json:"min_seq_num,omitempty"`
	MinSeqAge       any               `json:"min_seq_age,omitempty"`
	MinSeqLedgerGap any               `json:"min_seq_ledger_gap,omitempty"`
	ExtraSigners    []string          `json:"extra_signers,omitempty"`
	Memo            any               `json:"memo,omitempty"`
	Operations      []OperationSpec   `json:"operations"`
	Signatures      []SignatureSpec   `json:"signatures,omitempty"`
	SorobanData     *SorobanDataSpec  `json:"soroban_data,omitempty"`
	FeeBump         *FeeBumpSpec      `json:"fee_bump,omitempty"`
}

// TimeBoundsSpec defines transaction valid time ranges.
type TimeBoundsSpec struct {
	Min any `json:"min"`
	Max any `json:"max"`
}

// LedgerBoundsSpec defines transaction valid ledger ranges.
type LedgerBoundsSpec struct {
	Min any `json:"min"`
	Max any `json:"max"`
}

// SignatureSpec defines an existing cryptographic signature with optional hint.
type SignatureSpec struct {
	Hint      string `json:"hint,omitempty"`
	Signature string `json:"signature"`
}

// FeeBumpSpec defines parameters for fee-bump transactions.
type FeeBumpSpec struct {
	FeeSource string  `json:"fee_source"`
	MaxFee    any     `json:"max_fee"`
	InnerTx   *TxSpec `json:"inner_tx,omitempty"`
}

// SorobanDataSpec defines Soroban footprint and resource fee data.
type SorobanDataSpec struct {
	ResourceFee   any      `json:"resource_fee"`
	DiskReadBytes any      `json:"disk_read_bytes,omitempty"`
	WriteBytes    any      `json:"write_bytes,omitempty"`
	ReadOnly      []string `json:"read_only,omitempty"`
	ReadWrite     []string `json:"read_write,omitempty"`
}

// OperationSpec defines an individual transaction operation.
type OperationSpec struct {
	Type            string `json:"type"`
	SourceAccount   string `json:"source_account,omitempty"`
	Destination     string `json:"destination,omitempty"`
	StartingBalance any    `json:"starting_balance,omitempty"`
	Asset           any    `json:"asset,omitempty"`
	Line            any    `json:"line,omitempty"`
	Amount          any    `json:"amount,omitempty"`
	Price           any    `json:"price,omitempty"`
	OfferID         any    `json:"offer_id,omitempty"`
	Selling         any    `json:"selling,omitempty"`
	Buying          any    `json:"buying,omitempty"`
	BuyAmount       any    `json:"buy_amount,omitempty"`
	SendAsset       any    `json:"send_asset,omitempty"`
	SendMax         any    `json:"send_max,omitempty"`
	SendAmount      any    `json:"send_amount,omitempty"`
	DestAsset       any    `json:"dest_asset,omitempty"`
	DestMin         any    `json:"dest_min,omitempty"`
	DestAmount      any    `json:"dest_amount,omitempty"`
	Path            []any  `json:"path,omitempty"`
	Name            string `json:"name,omitempty"`
	Value           any    `json:"value,omitempty"`
	DataName        string `json:"data_name,omitempty"`
	DataValue       any    `json:"data_value,omitempty"`
	BumpTo          any    `json:"bump_to,omitempty"`
	InflationDest   string `json:"inflation_dest,omitempty"`
	SetFlags        any    `json:"set_flags,omitempty"`
	ClearFlags      any    `json:"clear_flags,omitempty"`
	MasterWeight    any    `json:"master_weight,omitempty"`
	LowThreshold    any    `json:"low_threshold,omitempty"`
	MedThreshold    any    `json:"med_threshold,omitempty"`
	HighThreshold   any    `json:"high_threshold,omitempty"`
	HomeDomain      string `json:"home_domain,omitempty"`
	Signer          any    `json:"signer,omitempty"`
	Function        string `json:"function,omitempty"`
	ContractID      string `json:"contract_id,omitempty"`
	FunctionName    string `json:"function_name,omitempty"`
	Args            []any  `json:"args,omitempty"`
	Wasm            any    `json:"wasm,omitempty"`
	Limit           any    `json:"limit,omitempty"`
	Authorize       any    `json:"authorize,omitempty"`
	Trustor         string `json:"trustor,omitempty"`
	Auth            []any  `json:"auth,omitempty"`
	LedgerKey       any    `json:"ledger_key,omitempty"`
	ExtendTo        any    `json:"extend_to,omitempty"`
}

// BuildTransactionEnvelopeFromJSON parses JSON bytes and constructs a TransactionEnvelope.
func BuildTransactionEnvelopeFromJSON(data []byte) (xdr.TransactionEnvelope, error) {
	if len(data) == 0 {
		return xdr.TransactionEnvelope{}, fmt.Errorf("%w: empty JSON payload", ErrInvalidTxSpec)
	}

	var rawMap map[string]any
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.UseNumber()
	if err := dec.Decode(&rawMap); err != nil {
		return xdr.TransactionEnvelope{}, fmt.Errorf("%w: invalid JSON syntax: %w", ErrInvalidTxSpec, err)
	}

	spec, err := unmarshalTxSpecFromMap(rawMap)
	if err != nil {
		return xdr.TransactionEnvelope{}, err
	}
	return BuildTransactionEnvelope(spec)
}

// BuildTransactionEnvelopeFromJSONStrict validates JSON against the strict schema before building.
func BuildTransactionEnvelopeFromJSONStrict(data []byte) (xdr.TransactionEnvelope, error) {
	if err := ValidateJSON(data, StrictTxSpecSchema()); err != nil {
		return xdr.TransactionEnvelope{}, err
	}
	return BuildTransactionEnvelopeFromJSON(data)
}

// EncodeTransactionEnvelopeBase64 builds the transaction envelope and serialises it to base64 XDR.
func EncodeTransactionEnvelopeBase64(spec *TxSpec) (string, error) {
	env, err := BuildTransactionEnvelope(spec)
	if err != nil {
		return "", err
	}
	bytes, err := env.MarshalBinary()
	if err != nil {
		return "", fmt.Errorf("envelope encode binary: %w", err)
	}
	return base64.StdEncoding.EncodeToString(bytes), nil
}

// BuildTransactionEnvelope creates an xdr.TransactionEnvelope from a TxSpec.
func BuildTransactionEnvelope(spec *TxSpec) (xdr.TransactionEnvelope, error) {
	var env xdr.TransactionEnvelope
	if spec == nil {
		return env, fmt.Errorf("%w: nil TxSpec", ErrInvalidTxSpec)
	}

	typ := strings.ToLower(strings.TrimSpace(spec.Type))
	if typ == "fee_bump" || (typ == "" && spec.FeeBump != nil) {
		feeBumpTx, err := BuildFeeBumpTransaction(spec)
		if err != nil {
			return env, err
		}
		sigs, err := buildSignatures(spec.Signatures)
		if err != nil {
			return env, err
		}
		return xdr.TransactionEnvelope{
			Type: xdr.EnvelopeTypeEnvelopeTypeTxFeeBump,
			FeeBump: &xdr.FeeBumpTransactionEnvelope{
				Tx:         feeBumpTx,
				Signatures: sigs,
			},
		}, nil
	}

	if typ == "v0" {
		txV0, err := BuildTransactionV0(spec)
		if err != nil {
			return env, err
		}
		sigs, err := buildSignatures(spec.Signatures)
		if err != nil {
			return env, err
		}
		return xdr.TransactionEnvelope{
			Type: xdr.EnvelopeTypeEnvelopeTypeTxV0,
			V0: &xdr.TransactionV0Envelope{
				Tx:         txV0,
				Signatures: sigs,
			},
		}, nil
	}

	// Default to V1 envelope
	txV1, err := BuildTransaction(spec)
	if err != nil {
		return env, err
	}
	sigs, err := buildSignatures(spec.Signatures)
	if err != nil {
		return env, err
	}
	return xdr.TransactionEnvelope{
		Type: xdr.EnvelopeTypeEnvelopeTypeTx,
		V1: &xdr.TransactionV1Envelope{
			Tx:         txV1,
			Signatures: sigs,
		},
	}, nil
}

// BuildTransaction constructs an xdr.Transaction (V1).
func BuildTransaction(spec *TxSpec) (xdr.Transaction, error) {
	var tx xdr.Transaction
	if spec == nil {
		return tx, fmt.Errorf("%w: nil TxSpec", ErrInvalidTxSpec)
	}

	sourceMux, err := CoerceMuxedAccount(spec.SourceAccount)
	if err != nil {
		return tx, fmt.Errorf("transaction source account: %w", err)
	}

	fee, err := CoerceUint32(spec.Fee)
	if err != nil {
		return tx, fmt.Errorf("transaction fee: %w", err)
	}

	seqNum, err := CoerceInt64(spec.Sequence)
	if err != nil {
		return tx, fmt.Errorf("transaction sequence: %w", err)
	}

	cond, err := buildPreconditions(spec)
	if err != nil {
		return tx, err
	}

	memo, err := buildMemo(spec.Memo)
	if err != nil {
		return tx, err
	}

	ops, err := buildOperations(spec.Operations)
	if err != nil {
		return tx, err
	}

	ext, err := buildTransactionExt(spec.SorobanData)
	if err != nil {
		return tx, err
	}

	return xdr.Transaction{
		SourceAccount: sourceMux,
		Fee:           xdr.Uint32(fee),
		SeqNum:        xdr.SequenceNumber(seqNum),
		Cond:          cond,
		Memo:          memo,
		Operations:    ops,
		Ext:           ext,
	}, nil
}

// BuildTransactionV0 constructs an xdr.TransactionV0.
func BuildTransactionV0(spec *TxSpec) (xdr.TransactionV0, error) {
	var tx xdr.TransactionV0
	if spec == nil {
		return tx, fmt.Errorf("%w: nil TxSpec", ErrInvalidTxSpec)
	}

	acc, err := CoerceAccountID(spec.SourceAccount)
	if err != nil {
		return tx, fmt.Errorf("v0 transaction source account: %w", err)
	}

	fee, err := CoerceUint32(spec.Fee)
	if err != nil {
		return tx, fmt.Errorf("v0 transaction fee: %w", err)
	}

	seqNum, err := CoerceInt64(spec.Sequence)
	if err != nil {
		return tx, fmt.Errorf("v0 transaction sequence: %w", err)
	}

	var tb *xdr.TimeBounds
	if spec.TimeBounds != nil {
		minTime, err1 := CoerceUint64(spec.TimeBounds.Min)
		maxTime, err2 := CoerceUint64(spec.TimeBounds.Max)
		if err1 == nil && err2 == nil {
			tb = &xdr.TimeBounds{
				MinTime: xdr.TimePoint(minTime),
				MaxTime: xdr.TimePoint(maxTime),
			}
		}
	}

	memo, err := buildMemo(spec.Memo)
	if err != nil {
		return tx, err
	}

	ops, err := buildOperations(spec.Operations)
	if err != nil {
		return tx, err
	}

	return xdr.TransactionV0{
		SourceAccountEd25519: acc.MustEd25519(),
		Fee:                  xdr.Uint32(fee),
		SeqNum:               xdr.SequenceNumber(seqNum),
		TimeBounds:           tb,
		Memo:                 memo,
		Operations:           ops,
	}, nil
}

// BuildFeeBumpTransaction constructs an xdr.FeeBumpTransaction.
func BuildFeeBumpTransaction(spec *TxSpec) (xdr.FeeBumpTransaction, error) {
	var fb xdr.FeeBumpTransaction
	if spec == nil || spec.FeeBump == nil {
		return fb, fmt.Errorf("%w: missing fee_bump specification", ErrInvalidTxSpec)
	}

	feeSource, err := CoerceMuxedAccount(spec.FeeBump.FeeSource)
	if err != nil {
		return fb, fmt.Errorf("fee bump source: %w", err)
	}

	maxFee, err := CoerceInt64(spec.FeeBump.MaxFee)
	if err != nil {
		return fb, fmt.Errorf("fee bump max fee: %w", err)
	}

	var innerEnvelope xdr.FeeBumpTransactionInnerTx
	if spec.FeeBump.InnerTx != nil {
		innerTx, err := BuildTransaction(spec.FeeBump.InnerTx)
		if err != nil {
			return fb, fmt.Errorf("fee bump inner transaction: %w", err)
		}
		innerSigs, err := buildSignatures(spec.FeeBump.InnerTx.Signatures)
		if err != nil {
			return fb, fmt.Errorf("fee bump inner signatures: %w", err)
		}
		innerEnvelope = xdr.FeeBumpTransactionInnerTx{
			Type: xdr.EnvelopeTypeEnvelopeTypeTx,
			V1: &xdr.TransactionV1Envelope{
				Tx:         innerTx,
				Signatures: innerSigs,
			},
		}
	} else {
		// Fallback to building inner from spec root if not nested
		innerTx, err := BuildTransaction(spec)
		if err != nil {
			return fb, fmt.Errorf("fee bump inner transaction: %w", err)
		}
		innerEnvelope = xdr.FeeBumpTransactionInnerTx{
			Type: xdr.EnvelopeTypeEnvelopeTypeTx,
			V1: &xdr.TransactionV1Envelope{
				Tx: innerTx,
			},
		}
	}

	return xdr.FeeBumpTransaction{
		FeeSource: feeSource,
		Fee:       xdr.Int64(maxFee),
		InnerTx:   innerEnvelope,
	}, nil
}

func buildPreconditions(spec *TxSpec) (xdr.Preconditions, error) {
	isV2 := spec.LedgerBounds != nil || spec.MinSeqNum != nil || spec.MinSeqAge != nil ||
		spec.MinSeqLedgerGap != nil || len(spec.ExtraSigners) > 0

	if isV2 {
		var tb *xdr.TimeBounds
		if spec.TimeBounds != nil {
			minTime, err1 := CoerceUint64(spec.TimeBounds.Min)
			maxTime, err2 := CoerceUint64(spec.TimeBounds.Max)
			if err1 != nil || err2 != nil {
				return xdr.Preconditions{}, fmt.Errorf("timebounds: %w", errors.Join(err1, err2))
			}
			tb = &xdr.TimeBounds{MinTime: xdr.TimePoint(minTime), MaxTime: xdr.TimePoint(maxTime)}
		}

		var lb *xdr.LedgerBounds
		if spec.LedgerBounds != nil {
			minL, err1 := CoerceUint32(spec.LedgerBounds.Min)
			maxL, err2 := CoerceUint32(spec.LedgerBounds.Max)
			if err1 != nil || err2 != nil {
				return xdr.Preconditions{}, fmt.Errorf("ledgerbounds: %w", errors.Join(err1, err2))
			}
			lb = &xdr.LedgerBounds{MinLedger: xdr.Uint32(minL), MaxLedger: xdr.Uint32(maxL)}
		}

		var minSeq *xdr.SequenceNumber
		if spec.MinSeqNum != nil {
			n, err := CoerceInt64(spec.MinSeqNum)
			if err != nil {
				return xdr.Preconditions{}, fmt.Errorf("min_seq_num: %w", err)
			}
			sn := xdr.SequenceNumber(n)
			minSeq = &sn
		}

		var minSeqAge xdr.Duration
		if spec.MinSeqAge != nil {
			n, err := CoerceUint64(spec.MinSeqAge)
			if err != nil {
				return xdr.Preconditions{}, fmt.Errorf("min_seq_age: %w", err)
			}
			minSeqAge = xdr.Duration(n)
		}

		var minSeqLedgerGap xdr.Uint32
		if spec.MinSeqLedgerGap != nil {
			n, err := CoerceUint32(spec.MinSeqLedgerGap)
			if err != nil {
				return xdr.Preconditions{}, fmt.Errorf("min_seq_ledger_gap: %w", err)
			}
			minSeqLedgerGap = xdr.Uint32(n)
		}

		var extraSigners []xdr.SignerKey
		for _, s := range spec.ExtraSigners {
			acc, err := CoerceAccountID(s)
			if err == nil {
				extraSigners = append(extraSigners, xdr.SignerKey{
					Type:    xdr.SignerKeyTypeSignerKeyTypeEd25519,
					Ed25519: acc.Ed25519,
				})
			}
		}

		return xdr.Preconditions{
			Type: xdr.PreconditionTypePrecondV2,
			V2: &xdr.PreconditionsV2{
				TimeBounds:      tb,
				LedgerBounds:    lb,
				MinSeqNum:       minSeq,
				MinSeqAge:       minSeqAge,
				MinSeqLedgerGap: minSeqLedgerGap,
				ExtraSigners:    extraSigners,
			},
		}, nil
	}

	if spec.TimeBounds != nil {
		minTime, err1 := CoerceUint64(spec.TimeBounds.Min)
		maxTime, err2 := CoerceUint64(spec.TimeBounds.Max)
		if err1 != nil || err2 != nil {
			return xdr.Preconditions{}, fmt.Errorf("timebounds: %w", errors.Join(err1, err2))
		}
		return xdr.Preconditions{
			Type: xdr.PreconditionTypePrecondTime,
			TimeBounds: &xdr.TimeBounds{
				MinTime: xdr.TimePoint(minTime),
				MaxTime: xdr.TimePoint(maxTime),
			},
		}, nil
	}

	return xdr.Preconditions{Type: xdr.PreconditionTypePrecondNone}, nil
}

func buildMemo(memoInput any) (xdr.Memo, error) {
	if memoInput == nil {
		return xdr.Memo{Type: xdr.MemoTypeMemoNone}, nil
	}

	switch m := memoInput.(type) {
	case string:
		if strings.EqualFold(m, "none") || m == "" {
			return xdr.Memo{Type: xdr.MemoTypeMemoNone}, nil
		}
		if len(m) > 28 {
			return xdr.Memo{}, fmt.Errorf("%w: memo text exceeds 28 bytes", ErrInvalidTxSpec)
		}
		return xdr.Memo{Type: xdr.MemoTypeMemoText, Text: &m}, nil
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		n, err := CoerceUint64(m)
		if err != nil {
			return xdr.Memo{}, err
		}
		id := xdr.Uint64(n)
		return xdr.Memo{Type: xdr.MemoTypeMemoId, Id: &id}, nil
	case map[string]any:
		typRaw, ok := m["type"]
		if !ok {
			return xdr.Memo{}, fmt.Errorf("memo object requires string 'type' field")
		}
		typ, ok := typRaw.(string)
		if !ok {
			return xdr.Memo{}, fmt.Errorf("memo 'type' must be string, got %T", typRaw)
		}
		val := m["value"]
		switch strings.ToLower(typ) {
		case "none":
			return xdr.Memo{Type: xdr.MemoTypeMemoNone}, nil
		case "text":
			s, err := CoerceString(val)
			if err != nil {
				return xdr.Memo{}, err
			}
			if len(s) > 28 {
				return xdr.Memo{}, fmt.Errorf("%w: memo text exceeds 28 bytes", ErrInvalidTxSpec)
			}
			return xdr.Memo{Type: xdr.MemoTypeMemoText, Text: &s}, nil
		case "id":
			n, err := CoerceUint64(val)
			if err != nil {
				return xdr.Memo{}, err
			}
			id := xdr.Uint64(n)
			return xdr.Memo{Type: xdr.MemoTypeMemoId, Id: &id}, nil
		case "hash":
			h, err := CoerceHash(val)
			if err != nil {
				return xdr.Memo{}, err
			}
			var hash xdr.Hash = h
			return xdr.Memo{Type: xdr.MemoTypeMemoHash, Hash: &hash}, nil
		case "return":
			h, err := CoerceHash(val)
			if err != nil {
				return xdr.Memo{}, err
			}
			var hash xdr.Hash = h
			return xdr.Memo{Type: xdr.MemoTypeMemoReturn, RetHash: &hash}, nil
		default:
			return xdr.Memo{}, fmt.Errorf("%w: unknown memo type %q", ErrInvalidTxSpec, typ)
		}
	default:
		return xdr.Memo{}, fmt.Errorf("%w: unsupported memo representation %T", ErrInvalidTxSpec, memoInput)
	}
}

func buildOperations(ops []OperationSpec) ([]xdr.Operation, error) {
	res := make([]xdr.Operation, len(ops))
	for i, op := range ops {
		built, err := buildOperation(op)
		if err != nil {
			return nil, fmt.Errorf("operation [%d]: %w", i, err)
		}
		res[i] = built
	}
	return res, nil
}

func buildOperation(op OperationSpec) (xdr.Operation, error) {
	var operation xdr.Operation
	var source *xdr.MuxedAccount
	if op.SourceAccount != "" {
		mux, err := CoerceMuxedAccount(op.SourceAccount)
		if err != nil {
			return operation, fmt.Errorf("operation source account: %w", err)
		}
		source = &mux
	}
	operation.SourceAccount = source

	typ := strings.ToLower(strings.TrimSpace(op.Type))
	switch typ {
	case "payment":
		paymentOp, err := buildPaymentOp(map[string]any{
			"destination": op.Destination,
			"amount":      op.Amount,
			"asset":       op.Asset,
		})
		if err != nil {
			return operation, err
		}
		operation.Body = xdr.OperationBody{
			Type:      xdr.OperationTypePayment,
			PaymentOp: paymentOp,
		}
		return operation, nil

	case "create_account":
		dest, err := CoerceAccountID(op.Destination)
		if err != nil {
			return operation, fmt.Errorf("create_account destination: %w", err)
		}
		bal, err := CoerceAmount(op.StartingBalance)
		if err != nil {
			return operation, fmt.Errorf("create_account starting balance: %w", err)
		}
		operation.Body = xdr.OperationBody{
			Type: xdr.OperationTypeCreateAccount,
			CreateAccountOp: &xdr.CreateAccountOp{
				Destination:     dest,
				StartingBalance: xdr.Int64(bal),
			},
		}
		return operation, nil

	case "change_trust":
		opMap := map[string]any{}
		if op.Line != nil {
			opMap["line"] = op.Line
		} else if op.Asset != nil {
			opMap["asset"] = op.Asset
		}
		if op.Limit != nil {
			opMap["limit"] = op.Limit
		}
		ctOp, err := buildChangeTrustOp(opMap)
		if err != nil {
			return operation, err
		}
		operation.Body = xdr.OperationBody{
			Type:          xdr.OperationTypeChangeTrust,
			ChangeTrustOp: ctOp,
		}
		return operation, nil

	case "manage_data":
		name := op.DataName
		if name == "" {
			name = op.Name
		}
		val := op.DataValue
		if val == nil {
			val = op.Value
		}
		mdOp, err := buildManageDataOp(map[string]any{
			"name":  name,
			"value": val,
		})
		if err != nil {
			return operation, err
		}
		operation.Body = xdr.OperationBody{
			Type:         xdr.OperationTypeManageData,
			ManageDataOp: mdOp,
		}
		return operation, nil

	case "bump_sequence":
		bumpTo, err := CoerceInt64(op.BumpTo)
		if err != nil {
			return operation, fmt.Errorf("bump_sequence bump_to: %w", err)
		}
		operation.Body = xdr.OperationBody{
			Type: xdr.OperationTypeBumpSequence,
			BumpSequenceOp: &xdr.BumpSequenceOp{
				BumpTo: xdr.SequenceNumber(bumpTo),
			},
		}
		return operation, nil

	case "account_merge":
		dest, err := CoerceMuxedAccount(op.Destination)
		if err != nil {
			return operation, fmt.Errorf("account_merge destination: %w", err)
		}
		operation.Body = xdr.OperationBody{
			Type:        xdr.OperationTypeAccountMerge,
			Destination: &dest,
		}
		return operation, nil

	case "manage_buy_offer":
		buyAmt := op.BuyAmount
		if buyAmt == nil {
			buyAmt = op.Amount
		}
		mboOp, err := buildManageBuyOfferOp(map[string]any{
			"selling":    op.Selling,
			"buying":     op.Buying,
			"buy_amount": buyAmt,
			"price":      op.Price,
			"offer_id":   op.OfferID,
		})
		if err != nil {
			return operation, err
		}
		operation.Body = xdr.OperationBody{
			Type:             xdr.OperationTypeManageBuyOffer,
			ManageBuyOfferOp: mboOp,
		}
		return operation, nil

	case "manage_sell_offer":
		msoOp, err := buildManageSellOfferOp(map[string]any{
			"selling":  op.Selling,
			"buying":   op.Buying,
			"amount":   op.Amount,
			"price":    op.Price,
			"offer_id": op.OfferID,
		})
		if err != nil {
			return operation, err
		}
		operation.Body = xdr.OperationBody{
			Type:              xdr.OperationTypeManageSellOffer,
			ManageSellOfferOp: msoOp,
		}
		return operation, nil

	case "set_options":
		soMap := map[string]any{}
		if op.InflationDest != "" {
			soMap["inflation_dest"] = op.InflationDest
		}
		if op.ClearFlags != nil {
			soMap["clear_flags"] = op.ClearFlags
		}
		if op.SetFlags != nil {
			soMap["set_flags"] = op.SetFlags
		}
		if op.MasterWeight != nil {
			soMap["master_weight"] = op.MasterWeight
		}
		if op.LowThreshold != nil {
			soMap["low_threshold"] = op.LowThreshold
		}
		if op.MedThreshold != nil {
			soMap["med_threshold"] = op.MedThreshold
		}
		if op.HighThreshold != nil {
			soMap["high_threshold"] = op.HighThreshold
		}
		if op.HomeDomain != "" {
			soMap["home_domain"] = op.HomeDomain
		}
		if op.Signer != nil {
			soMap["signer"] = op.Signer
		}
		soOp, err := buildSetOptionsOp(soMap)
		if err != nil {
			return operation, err
		}
		operation.Body = xdr.OperationBody{
			Type:         xdr.OperationTypeSetOptions,
			SetOptionsOp: soOp,
		}
		return operation, nil

	case "invoke_host_function":
		var hostFn xdr.HostFunction
		fnType := strings.ToLower(strings.TrimSpace(op.Function))
		if fnType == "" || fnType == "invoke_contract" {
			addrScVal, err := ParseScVal(op.ContractID)
			if err != nil {
				return operation, fmt.Errorf("invoke_contract contract_id: %w", err)
			}
			scAddress := *addrScVal.Address
			sym := xdr.ScSymbol(op.FunctionName)
			args := make([]xdr.ScVal, len(op.Args))
			for idx, arg := range op.Args {
				scArg, err := ParseScVal(arg)
				if err != nil {
					return operation, fmt.Errorf("invoke_contract arg [%d]: %w", idx, err)
				}
				args[idx] = scArg
			}
			hostFn = xdr.HostFunction{
				Type: xdr.HostFunctionTypeHostFunctionTypeInvokeContract,
				InvokeContract: &xdr.InvokeContractArgs{
					ContractAddress: scAddress,
					FunctionName:    sym,
					Args:            args,
				},
			}
		}
		operation.Body = xdr.OperationBody{
			Type: xdr.OperationTypeInvokeHostFunction,
			InvokeHostFunctionOp: &xdr.InvokeHostFunctionOp{
				HostFunction: hostFn,
			},
		}
		return operation, nil

	default:
		return operation, fmt.Errorf("%w: unsupported operation type %q", ErrInvalidTxSpec, op.Type)
	}
}

// BuildOperation builds an xdr.Operation from a generic JSON-like operation map.
func BuildOperation(spec map[string]any) (xdr.Operation, error) {
	var operation xdr.Operation
	if spec == nil {
		return operation, fmt.Errorf("%w: nil operation specification", ErrInvalidTxSpec)
	}

	if src := firstPresent(spec, "source_account", "sourceAccount", "source"); src != nil {
		mux, err := CoerceMuxedAccount(src)
		if err != nil {
			return operation, fmt.Errorf("operation source account: %w", err)
		}
		operation.SourceAccount = &mux
	}

	typRaw, _ := spec["type"].(string)
	typ := strings.ToLower(strings.TrimSpace(typRaw))
	switch typ {
	case "manage_data":
		mdOp, err := buildManageDataOp(spec)
		if err != nil {
			return operation, err
		}
		operation.Body = xdr.OperationBody{
			Type:         xdr.OperationTypeManageData,
			ManageDataOp: mdOp,
		}
		return operation, nil
	case "manage_sell_offer":
		msoOp, err := buildManageSellOfferOp(spec)
		if err != nil {
			return operation, err
		}
		operation.Body = xdr.OperationBody{
			Type:              xdr.OperationTypeManageSellOffer,
			ManageSellOfferOp: msoOp,
		}
		return operation, nil
	case "manage_buy_offer":
		mboOp, err := buildManageBuyOfferOp(spec)
		if err != nil {
			return operation, err
		}
		operation.Body = xdr.OperationBody{
			Type:             xdr.OperationTypeManageBuyOffer,
			ManageBuyOfferOp: mboOp,
		}
		return operation, nil
	case "set_options":
		soOp, err := buildSetOptionsOp(spec)
		if err != nil {
			return operation, err
		}
		operation.Body = xdr.OperationBody{
			Type:         xdr.OperationTypeSetOptions,
			SetOptionsOp: soOp,
		}
		return operation, nil
	case "change_trust":
		ctOp, err := buildChangeTrustOp(spec)
		if err != nil {
			return operation, err
		}
		operation.Body = xdr.OperationBody{
			Type:          xdr.OperationTypeChangeTrust,
			ChangeTrustOp: ctOp,
		}
		return operation, nil
	default:
		norm := normalizeOperationMap(spec)
		raw, err := json.Marshal(norm)
		if err != nil {
			return operation, err
		}
		var opSpec OperationSpec
		if err := json.Unmarshal(raw, &opSpec); err != nil {
			return operation, err
		}
		return buildOperation(opSpec)
	}
}

func buildManageDataOp(spec map[string]any) (*xdr.ManageDataOp, error) {
	if spec == nil {
		return nil, fmt.Errorf("%w: nil manage_data spec", ErrInvalidTxSpec)
	}
	nameRaw := firstPresent(spec, "name", "data_name", "dataName")
	name, _ := nameRaw.(string)
	if err := ValidateDataName(name); err != nil {
		return nil, err
	}

	valRaw, _ := hasAnyKey(spec, "value", "data_value", "dataValue")
	dataVal, err := EncodeDataValue(valRaw)
	if err != nil {
		return nil, err
	}

	return &xdr.ManageDataOp{
		DataName:  xdr.String64(name),
		DataValue: dataVal,
	}, nil
}

func buildManageSellOfferOp(spec map[string]any) (*xdr.ManageSellOfferOp, error) {
	if spec == nil {
		return nil, fmt.Errorf("%w: nil manage_sell_offer spec", ErrInvalidTxSpec)
	}
	selling, err := CoerceAsset(spec["selling"])
	if err != nil {
		return nil, fmt.Errorf("manage_sell_offer selling: %w", err)
	}
	buying, err := CoerceAsset(spec["buying"])
	if err != nil {
		return nil, fmt.Errorf("manage_sell_offer buying: %w", err)
	}
	amt, err := CoerceAmount(spec["amount"])
	if err != nil {
		return nil, fmt.Errorf("manage_sell_offer amount: %w", err)
	}
	if amt < 0 {
		return nil, fmt.Errorf("%w: manage_sell_offer amount %d must be non-negative", ErrInvalidTxSpec, amt)
	}
	price, err := ParsePriceFraction(spec["price"])
	if err != nil {
		return nil, fmt.Errorf("manage_sell_offer price: %w", err)
	}
	var offerID int64
	if rawID := firstPresent(spec, "offer_id", "offerId"); rawID != nil {
		id, err := CoerceInt64(rawID)
		if err != nil {
			return nil, fmt.Errorf("manage_sell_offer offer_id: %w", err)
		}
		if id < 0 {
			return nil, fmt.Errorf("%w: manage_sell_offer offer_id %d must be non-negative", ErrInvalidTxSpec, id)
		}
		offerID = id
	}
	return &xdr.ManageSellOfferOp{
		Selling: selling,
		Buying:  buying,
		Amount:  xdr.Int64(amt),
		Price:   price,
		OfferId: xdr.Int64(offerID),
	}, nil
}

func buildManageBuyOfferOp(spec map[string]any) (*xdr.ManageBuyOfferOp, error) {
	if spec == nil {
		return nil, fmt.Errorf("%w: nil manage_buy_offer spec", ErrInvalidTxSpec)
	}
	selling, err := CoerceAsset(spec["selling"])
	if err != nil {
		return nil, fmt.Errorf("manage_buy_offer selling: %w", err)
	}
	buying, err := CoerceAsset(spec["buying"])
	if err != nil {
		return nil, fmt.Errorf("manage_buy_offer buying: %w", err)
	}
	amtRaw := firstPresent(spec, "buy_amount", "buyAmount", "amount")
	buyAmt, err := CoerceAmount(amtRaw)
	if err != nil {
		return nil, fmt.Errorf("manage_buy_offer buy_amount: %w", err)
	}
	if buyAmt < 0 {
		return nil, fmt.Errorf("%w: manage_buy_offer buy_amount %d must be non-negative", ErrInvalidTxSpec, buyAmt)
	}
	price, err := ParsePriceFraction(spec["price"])
	if err != nil {
		return nil, fmt.Errorf("manage_buy_offer price: %w", err)
	}
	var offerID int64
	if rawID := firstPresent(spec, "offer_id", "offerId"); rawID != nil {
		id, err := CoerceInt64(rawID)
		if err != nil {
			return nil, fmt.Errorf("manage_buy_offer offer_id: %w", err)
		}
		if id < 0 {
			return nil, fmt.Errorf("%w: manage_buy_offer offer_id %d must be non-negative", ErrInvalidTxSpec, id)
		}
		offerID = id
	}
	return &xdr.ManageBuyOfferOp{
		Selling:   selling,
		Buying:    buying,
		BuyAmount: xdr.Int64(buyAmt),
		Price:     price,
		OfferId:   xdr.Int64(offerID),
	}, nil
}

func buildSetOptionsOp(spec map[string]any) (*xdr.SetOptionsOp, error) {
	if spec == nil {
		return nil, fmt.Errorf("%w: nil set_options spec", ErrInvalidTxSpec)
	}
	op := &xdr.SetOptionsOp{}

	if infRaw := firstPresent(spec, "inflation_dest", "inflationDest"); infRaw != nil {
		acc, err := CoerceAccountID(infRaw)
		if err != nil {
			return nil, fmt.Errorf("set_options inflation_dest: %w", err)
		}
		op.InflationDest = &acc
	}

	if cfRaw := firstPresent(spec, "clear_flags", "clearFlags"); cfRaw != nil {
		flags, err := ParseAccountFlags(cfRaw)
		if err != nil {
			return nil, fmt.Errorf("set_options clear_flags: %w", err)
		}
		op.ClearFlags = &flags
	}

	if sfRaw := firstPresent(spec, "set_flags", "setFlags"); sfRaw != nil {
		flags, err := ParseAccountFlags(sfRaw)
		if err != nil {
			return nil, fmt.Errorf("set_options set_flags: %w", err)
		}
		op.SetFlags = &flags
	}

	if mwRaw := firstPresent(spec, "master_weight", "masterWeight"); mwRaw != nil {
		w, err := ValidateThresholdWeight(mwRaw, "master_weight")
		if err != nil {
			return nil, err
		}
		op.MasterWeight = &w
	}

	if lwRaw := firstPresent(spec, "low_threshold", "lowThreshold"); lwRaw != nil {
		w, err := ValidateThresholdWeight(lwRaw, "low_threshold")
		if err != nil {
			return nil, err
		}
		op.LowThreshold = &w
	}

	if mtRaw := firstPresent(spec, "med_threshold", "medThreshold"); mtRaw != nil {
		w, err := ValidateThresholdWeight(mtRaw, "med_threshold")
		if err != nil {
			return nil, err
		}
		op.MedThreshold = &w
	}

	if htRaw := firstPresent(spec, "high_threshold", "highThreshold"); htRaw != nil {
		w, err := ValidateThresholdWeight(htRaw, "high_threshold")
		if err != nil {
			return nil, err
		}
		op.HighThreshold = &w
	}

	if hdRaw, ok := hasAnyKey(spec, "home_domain", "homeDomain"); ok && hdRaw != nil {
		hd, err := CoerceString(hdRaw)
		if err != nil {
			return nil, fmt.Errorf("set_options home_domain: %w", err)
		}
		if len(hd) > 32 {
			return nil, fmt.Errorf("%w: home_domain exceeds 32 bytes (%d)", ErrInvalidTxSpec, len(hd))
		}
		s32 := xdr.String32(hd)
		op.HomeDomain = &s32
	}

	if sigRaw, ok := spec["signer"]; ok && sigRaw != nil {
		signer, err := ParseSigner(sigRaw)
		if err != nil {
			return nil, err
		}
		op.Signer = signer
	}

	return op, nil
}

func buildChangeTrustOp(spec map[string]any) (*xdr.ChangeTrustOp, error) {
	if spec == nil {
		return nil, fmt.Errorf("%w: nil change_trust spec", ErrInvalidTxSpec)
	}
	lineVal := firstPresent(spec, "line", "asset")
	if lineVal == nil {
		return nil, fmt.Errorf("%w: change_trust requires 'line' or 'asset'", ErrInvalidTxSpec)
	}
	cta, err := CoerceChangeTrustAsset(lineVal)
	if err != nil {
		return nil, fmt.Errorf("change_trust line: %w", err)
	}
	limit := int64(math.MaxInt64)
	if limVal, ok := spec["limit"]; ok && limVal != nil {
		lim, err := CoerceAmount(limVal)
		if err != nil {
			return nil, fmt.Errorf("change_trust limit: %w", err)
		}
		if lim < 0 {
			return nil, fmt.Errorf("%w: change_trust limit %d must be non-negative", ErrInvalidTxSpec, lim)
		}
		limit = lim
	}
	return &xdr.ChangeTrustOp{
		Line:  cta,
		Limit: xdr.Int64(limit),
	}, nil
}

func buildSignatures(specs []SignatureSpec) ([]xdr.DecoratedSignature, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	res := make([]xdr.DecoratedSignature, len(specs))
	for i, s := range specs {
		sigBytes, err := CoerceBytes(s.Signature)
		if err != nil {
			return nil, fmt.Errorf("signature [%d]: %w", i, err)
		}
		var hint xdr.SignatureHint
		if s.Hint != "" {
			hintBytes, err := CoerceBytes(s.Hint)
			if err == nil && len(hintBytes) == 4 {
				copy(hint[:], hintBytes)
			}
		}
		res[i] = xdr.DecoratedSignature{
			Hint:      hint,
			Signature: xdr.Signature(sigBytes),
		}
	}
	return res, nil
}

func buildTransactionExt(spec *SorobanDataSpec) (xdr.TransactionExt, error) {
	if spec == nil {
		return xdr.TransactionExt{V: 0}, nil
	}
	var resourceFee int64
	if spec.ResourceFee != nil {
		rf, err := CoerceInt64(spec.ResourceFee)
		if err != nil {
			return xdr.TransactionExt{}, fmt.Errorf("soroban_data resource_fee: %w", err)
		}
		resourceFee = rf
	}
	var diskRead uint32
	if spec.DiskReadBytes != nil {
		dr, err := CoerceUint32(spec.DiskReadBytes)
		if err != nil {
			return xdr.TransactionExt{}, fmt.Errorf("soroban_data disk_read_bytes: %w", err)
		}
		diskRead = dr
	}
	var writeBytes uint32
	if spec.WriteBytes != nil {
		wb, err := CoerceUint32(spec.WriteBytes)
		if err != nil {
			return xdr.TransactionExt{}, fmt.Errorf("soroban_data write_bytes: %w", err)
		}
		writeBytes = wb
	}

	return xdr.TransactionExt{
		V: 1,
		SorobanData: &xdr.SorobanTransactionData{
			ResourceFee: xdr.Int64(resourceFee),
			Resources: xdr.SorobanResources{
				DiskReadBytes: xdr.Uint32(diskRead),
				WriteBytes:    xdr.Uint32(writeBytes),
			},
		},
	}, nil
}

func normalizeOperationMap(m map[string]any) map[string]any {
	aliases := OperationSpecSchema().Aliases
	out := make(map[string]any, len(m))
	for k, v := range m {
		target := k
		if mapped, ok := aliases[k]; ok {
			target = mapped
		}
		if _, exists := out[target]; !exists || v != nil {
			out[target] = v
		}
	}
	return out
}

func unmarshalTxSpecFromMap(m map[string]any) (*TxSpec, error) {
	if opsRaw, ok := m["operations"].([]any); ok {
		normalized := make([]any, len(opsRaw))
		for i, item := range opsRaw {
			if opMap, ok := item.(map[string]any); ok {
				normalized[i] = normalizeOperationMap(opMap)
			} else {
				normalized[i] = item
			}
		}
		mCopy := make(map[string]any, len(m))
		for k, v := range m {
			mCopy[k] = v
		}
		mCopy["operations"] = normalized
		m = mCopy
	}

	bytes, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	var spec TxSpec
	if err := json.Unmarshal(bytes, &spec); err != nil {
		return nil, err
	}
	// Fallbacks for alternate property casings
	if spec.SourceAccount == "" {
		if s, ok := m["sourceAccount"].(string); ok {
			spec.SourceAccount = s
		} else if s, ok := m["source"].(string); ok {
			spec.SourceAccount = s
		}
	}
	if spec.Sequence == nil {
		if seq, ok := m["seqNum"]; ok {
			spec.Sequence = seq
		} else if seq, ok := m["sequenceNumber"]; ok {
			spec.Sequence = seq
		}
	}
	return &spec, nil
}
