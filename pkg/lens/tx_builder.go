package lens

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
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
	Claimants       any    `json:"claimants,omitempty"`
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
		lineVal := op.Line
		if lineVal == nil {
			lineVal = op.Asset
		}
		cta, err := CoerceChangeTrustAsset(lineVal)
		if err != nil {
			return operation, fmt.Errorf("change_trust line: %w", err)
		}
		limit := int64(xdr.Int64(0x7fffffffffffffff))
		if op.Limit != nil {
			lim, err := CoerceAmount(op.Limit)
			if err == nil {
				limit = lim
			}
		}
		operation.Body = xdr.OperationBody{
			Type: xdr.OperationTypeChangeTrust,
			ChangeTrustOp: &xdr.ChangeTrustOp{
				Line:  cta,
				Limit: xdr.Int64(limit),
			},
		}
		return operation, nil

	case "manage_data":
		if len(op.DataName) > 64 {
			return operation, fmt.Errorf("%w: data name exceeds 64 characters", ErrInvalidTxSpec)
		}
		var valPtr *xdr.DataValue
		if op.DataValue != nil {
			b, err := CoerceBytes(op.DataValue)
			if err != nil {
				return operation, fmt.Errorf("manage_data value: %w", err)
			}
			dv := xdr.DataValue(b)
			valPtr = &dv
		}
		operation.Body = xdr.OperationBody{
			Type: xdr.OperationTypeManageData,
			ManageDataOp: &xdr.ManageDataOp{
				DataName:  xdr.String64(op.DataName),
				DataValue: valPtr,
			},
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
		selling, err := CoerceAsset(op.Selling)
		if err != nil {
			return operation, fmt.Errorf("manage_buy_offer selling: %w", err)
		}
		buying, err := CoerceAsset(op.Buying)
		if err != nil {
			return operation, fmt.Errorf("manage_buy_offer buying: %w", err)
		}
		buyAmt, err := CoerceAmount(op.BuyAmount)
		if err != nil {
			return operation, fmt.Errorf("manage_buy_offer buy_amount: %w", err)
		}
		price, err := CoercePrice(op.Price)
		if err != nil {
			return operation, fmt.Errorf("manage_buy_offer price: %w", err)
		}
		var offerID int64
		if op.OfferID != nil {
			id, err := CoerceInt64(op.OfferID)
			if err != nil {
				return operation, fmt.Errorf("manage_buy_offer offer_id: %w", err)
			}
			offerID = id
		}
		operation.Body = xdr.OperationBody{
			Type: xdr.OperationTypeManageBuyOffer,
			ManageBuyOfferOp: &xdr.ManageBuyOfferOp{
				Selling:   selling,
				Buying:    buying,
				BuyAmount: xdr.Int64(buyAmt),
				Price:     price,
				OfferId:   xdr.Int64(offerID),
			},
		}
		return operation, nil

	case "manage_sell_offer":
		selling, err := CoerceAsset(op.Selling)
		if err != nil {
			return operation, fmt.Errorf("manage_sell_offer selling: %w", err)
		}
		buying, err := CoerceAsset(op.Buying)
		if err != nil {
			return operation, fmt.Errorf("manage_sell_offer buying: %w", err)
		}
		amt, err := CoerceAmount(op.Amount)
		if err != nil {
			return operation, fmt.Errorf("manage_sell_offer amount: %w", err)
		}
		price, err := CoercePrice(op.Price)
		if err != nil {
			return operation, fmt.Errorf("manage_sell_offer price: %w", err)
		}
		var offerID int64
		if op.OfferID != nil {
			id, err := CoerceInt64(op.OfferID)
			if err != nil {
				return operation, fmt.Errorf("manage_sell_offer offer_id: %w", err)
			}
			offerID = id
		}
		operation.Body = xdr.OperationBody{
			Type: xdr.OperationTypeManageSellOffer,
			ManageSellOfferOp: &xdr.ManageSellOfferOp{
				Selling: selling,
				Buying:  buying,
				Amount:  xdr.Int64(amt),
				Price:   price,
				OfferId: xdr.Int64(offerID),
			},
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

	case "create_claimable_balance":
		m := map[string]any{
			"asset":     op.Asset,
			"amount":    op.Amount,
			"claimants": op.Claimants,
		}
		cbOp, err := buildClaimableBalanceOp(m)
		if err != nil {
			return operation, fmt.Errorf("create_claimable_balance: %w", err)
		}
		operation.Body = xdr.OperationBody{
			Type:                     xdr.OperationTypeCreateClaimableBalance,
			CreateClaimableBalanceOp: cbOp,
		}
		return operation, nil

	default:
		return operation, fmt.Errorf("%w: unsupported operation type %q", ErrInvalidTxSpec, op.Type)
	}
}

// buildPaymentOp builds an xdr.PaymentOp from the intuitive JSON template
// `lens encode` accepts:
//
//	{"type": "payment", "destination": "G...", "amount": "10.5",
//	 "asset": {"code": "USDC", "issuer": "G..."}}
//
// The destination is a G... account or an M... muxed address. The amount
// accepts a human decimal ("10.5" becomes 105000000 stroops) as well as an
// integer stroop count. The asset is "native" or a code/issuer object and
// defaults to native when omitted. An asset that cannot be parsed is an error
// rather than a silent fall back to native, so a typo in an issuer cannot
// change the meaning of the operation.
func buildPaymentOp(spec map[string]any) (*xdr.PaymentOp, error) {
	if spec == nil {
		return nil, fmt.Errorf("%w: payment operation requires 'destination'", ErrInvalidTxSpec)
	}

	destination := spec["destination"]
	if destination == nil {
		return nil, fmt.Errorf("%w: payment operation requires 'destination'", ErrInvalidTxSpec)
	}
	if text, ok := destination.(string); ok && strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("%w: payment operation requires 'destination'", ErrInvalidTxSpec)
	}
	dest, err := CoerceMuxedAccount(destination)
	if err != nil {
		return nil, fmt.Errorf("payment destination: %w", err)
	}

	amount := spec["amount"]
	if amount == nil {
		return nil, fmt.Errorf("%w: payment operation requires 'amount'", ErrInvalidTxSpec)
	}
	stroops, err := CoerceAmount(amount)
	if err != nil {
		return nil, fmt.Errorf("payment amount: %w", err)
	}

	asset := xdr.Asset{Type: xdr.AssetTypeAssetTypeNative}
	if raw, ok := spec["asset"]; ok && raw != nil {
		parsed, err := CoerceAsset(raw)
		if err != nil {
			return nil, fmt.Errorf("payment asset: %w", err)
		}
		asset = parsed
	}

	return &xdr.PaymentOp{
		Destination: dest,
		Asset:       asset,
		Amount:      xdr.Int64(stroops),
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

func unmarshalTxSpecFromMap(m map[string]any) (*TxSpec, error) {
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

// BuildClaimableBalanceOp constructs an xdr.CreateClaimableBalanceOp from a JSON specification map.
func BuildClaimableBalanceOp(spec map[string]any) (*xdr.CreateClaimableBalanceOp, error) {
	return buildClaimableBalanceOp(spec)
}

func buildClaimableBalanceOp(spec map[string]any) (*xdr.CreateClaimableBalanceOp, error) {
	if spec == nil {
		return nil, fmt.Errorf("%w: missing create_claimable_balance spec", ErrInvalidTxSpec)
	}

	astVal := spec["asset"]
	var ast xdr.Asset
	if astVal == nil || astVal == "" || astVal == "native" {
		ast = xdr.Asset{Type: xdr.AssetTypeAssetTypeNative}
	} else {
		a, err := CoerceAsset(astVal)
		if err != nil {
			return nil, fmt.Errorf("claimable balance asset: %w", err)
		}
		ast = a
	}

	amtVal, ok := spec["amount"]
	if !ok || amtVal == nil {
		return nil, fmt.Errorf("%w: missing amount for create_claimable_balance", ErrInvalidTxSpec)
	}
	amt, err := CoerceAmount(amtVal)
	if err != nil {
		return nil, fmt.Errorf("claimable balance amount: %w", err)
	}

	claimantsRaw, ok := spec["claimants"]
	if !ok || claimantsRaw == nil {
		return nil, fmt.Errorf("%w: missing claimants for create_claimable_balance", ErrInvalidTxSpec)
	}

	claimantsSlice, err := coerceToSlice(claimantsRaw)
	if err != nil {
		return nil, fmt.Errorf("claimable balance claimants: %w", err)
	}
	if len(claimantsSlice) == 0 {
		return nil, fmt.Errorf("%w: at least one claimant required", ErrInvalidTxSpec)
	}
	if len(claimantsSlice) > 10 {
		return nil, fmt.Errorf("%w: claimants count %d exceeds maximum of 10", ErrInvalidTxSpec, len(claimantsSlice))
	}

	claimants := make([]xdr.Claimant, len(claimantsSlice))
	for i, c := range claimantsSlice {
		switch cv := c.(type) {
		case string:
			acc, err := CoerceAccountID(cv)
			if err != nil {
				return nil, fmt.Errorf("claimant [%d] destination: %w", i, err)
			}
			claimants[i] = xdr.Claimant{
				Type: xdr.ClaimantTypeClaimantTypeV0,
				V0: &xdr.ClaimantV0{
					Destination: acc,
					Predicate:   xdr.ClaimPredicate{Type: xdr.ClaimPredicateTypeClaimPredicateUnconditional},
				},
			}
		case map[string]any:
			destRaw, ok := cv["destination"]
			if !ok || destRaw == nil {
				return nil, fmt.Errorf("%w: claimant [%d] missing destination", ErrInvalidTxSpec, i)
			}
			destStr, err := CoerceString(destRaw)
			if err != nil {
				return nil, fmt.Errorf("claimant [%d] destination: %w", i, err)
			}
			acc, err := CoerceAccountID(destStr)
			if err != nil {
				return nil, fmt.Errorf("claimant [%d] destination: %w", i, err)
			}

			predSpec := cv["predicate"]
			pred, err := buildClaimPredicate(predSpec)
			if err != nil {
				return nil, fmt.Errorf("claimant [%d] predicate: %w", i, err)
			}

			claimants[i] = xdr.Claimant{
				Type: xdr.ClaimantTypeClaimantTypeV0,
				V0: &xdr.ClaimantV0{
					Destination: acc,
					Predicate:   pred,
				},
			}
		default:
			return nil, fmt.Errorf("%w: unsupported claimant representation %T at index %d", ErrInvalidTxSpec, c, i)
		}
	}

	return &xdr.CreateClaimableBalanceOp{
		Asset:     ast,
		Amount:    xdr.Int64(amt),
		Claimants: claimants,
	}, nil
}

// BuildClaimPredicate parses a JSON specification into a validated xdr.ClaimPredicate.
func BuildClaimPredicate(spec any) (xdr.ClaimPredicate, error) {
	return buildClaimPredicate(spec)
}

func buildClaimPredicate(spec any) (xdr.ClaimPredicate, error) {
	pred, err := parseClaimPredicate(spec)
	if err != nil {
		return xdr.ClaimPredicate{}, err
	}
	if err := ValidateClaimPredicate(pred); err != nil {
		return xdr.ClaimPredicate{}, err
	}
	return pred, nil
}

func parseClaimPredicate(spec any) (xdr.ClaimPredicate, error) {
	if spec == nil {
		return xdr.ClaimPredicate{Type: xdr.ClaimPredicateTypeClaimPredicateUnconditional}, nil
	}

	switch s := spec.(type) {
	case string:
		str := strings.ToLower(strings.TrimSpace(s))
		if str == "unconditional" || str == "" {
			return xdr.ClaimPredicate{Type: xdr.ClaimPredicateTypeClaimPredicateUnconditional}, nil
		}
		return xdr.ClaimPredicate{}, fmt.Errorf("%w: unknown claim predicate string %q", ErrInvalidTxSpec, s)

	case xdr.ClaimPredicate:
		return s, nil

	case map[string]any:
		if _, ok := s["unconditional"]; ok {
			return xdr.ClaimPredicate{Type: xdr.ClaimPredicateTypeClaimPredicateUnconditional}, nil
		}

		if val, ok := s["before_time"]; ok {
			abs, err := CoerceInt64(val)
			if err != nil {
				return xdr.ClaimPredicate{}, fmt.Errorf("before_time: %w", err)
			}
			v := xdr.Int64(abs)
			return xdr.ClaimPredicate{Type: xdr.ClaimPredicateTypeClaimPredicateBeforeAbsoluteTime, AbsBefore: &v}, nil
		}
		if val, ok := s["before_absolute_time"]; ok {
			abs, err := CoerceInt64(val)
			if err != nil {
				return xdr.ClaimPredicate{}, fmt.Errorf("before_absolute_time: %w", err)
			}
			v := xdr.Int64(abs)
			return xdr.ClaimPredicate{Type: xdr.ClaimPredicateTypeClaimPredicateBeforeAbsoluteTime, AbsBefore: &v}, nil
		}
		if val, ok := s["abs_before"]; ok {
			abs, err := CoerceInt64(val)
			if err != nil {
				return xdr.ClaimPredicate{}, fmt.Errorf("abs_before: %w", err)
			}
			v := xdr.Int64(abs)
			return xdr.ClaimPredicate{Type: xdr.ClaimPredicateTypeClaimPredicateBeforeAbsoluteTime, AbsBefore: &v}, nil
		}

		if val, ok := s["before_relative_time"]; ok {
			rel, err := CoerceInt64(val)
			if err != nil {
				return xdr.ClaimPredicate{}, fmt.Errorf("before_relative_time: %w", err)
			}
			v := xdr.Int64(rel)
			return xdr.ClaimPredicate{Type: xdr.ClaimPredicateTypeClaimPredicateBeforeRelativeTime, RelBefore: &v}, nil
		}
		if val, ok := s["rel_before"]; ok {
			rel, err := CoerceInt64(val)
			if err != nil {
				return xdr.ClaimPredicate{}, fmt.Errorf("rel_before: %w", err)
			}
			v := xdr.Int64(rel)
			return xdr.ClaimPredicate{Type: xdr.ClaimPredicateTypeClaimPredicateBeforeRelativeTime, RelBefore: &v}, nil
		}

		if val, ok := s["after_time"]; ok {
			abs, err := CoerceInt64(val)
			if err != nil {
				return xdr.ClaimPredicate{}, fmt.Errorf("after_time: %w", err)
			}
			v := xdr.Int64(abs)
			inner := xdr.ClaimPredicate{Type: xdr.ClaimPredicateTypeClaimPredicateBeforeAbsoluteTime, AbsBefore: &v}
			innerPtr := &inner
			return xdr.ClaimPredicate{Type: xdr.ClaimPredicateTypeClaimPredicateNot, NotPredicate: &innerPtr}, nil
		}
		if val, ok := s["after_absolute_time"]; ok {
			abs, err := CoerceInt64(val)
			if err != nil {
				return xdr.ClaimPredicate{}, fmt.Errorf("after_absolute_time: %w", err)
			}
			v := xdr.Int64(abs)
			inner := xdr.ClaimPredicate{Type: xdr.ClaimPredicateTypeClaimPredicateBeforeAbsoluteTime, AbsBefore: &v}
			innerPtr := &inner
			return xdr.ClaimPredicate{Type: xdr.ClaimPredicateTypeClaimPredicateNot, NotPredicate: &innerPtr}, nil
		}
		if val, ok := s["after_relative_time"]; ok {
			rel, err := CoerceInt64(val)
			if err != nil {
				return xdr.ClaimPredicate{}, fmt.Errorf("after_relative_time: %w", err)
			}
			v := xdr.Int64(rel)
			inner := xdr.ClaimPredicate{Type: xdr.ClaimPredicateTypeClaimPredicateBeforeRelativeTime, RelBefore: &v}
			innerPtr := &inner
			return xdr.ClaimPredicate{Type: xdr.ClaimPredicateTypeClaimPredicateNot, NotPredicate: &innerPtr}, nil
		}

		if val, ok := s["not"]; ok {
			inner, err := parseClaimPredicate(val)
			if err != nil {
				return xdr.ClaimPredicate{}, fmt.Errorf("not predicate: %w", err)
			}
			innerPtr := &inner
			return xdr.ClaimPredicate{Type: xdr.ClaimPredicateTypeClaimPredicateNot, NotPredicate: &innerPtr}, nil
		}

		if val, ok := s["and"]; ok {
			return parseCompoundPredicate(xdr.ClaimPredicateTypeClaimPredicateAnd, val)
		}
		if val, ok := s["or"]; ok {
			return parseCompoundPredicate(xdr.ClaimPredicateTypeClaimPredicateOr, val)
		}

		if typRaw, ok := s["type"]; ok {
			if typ, ok := typRaw.(string); ok {
				switch strings.ToLower(typ) {
				case "unconditional":
					return xdr.ClaimPredicate{Type: xdr.ClaimPredicateTypeClaimPredicateUnconditional}, nil
				case "before_absolute_time", "before_time":
					val := s["value"]
					if val == nil {
						val = s["abs_before"]
					}
					abs, err := CoerceInt64(val)
					if err != nil {
						return xdr.ClaimPredicate{}, fmt.Errorf("before_time: %w", err)
					}
					v := xdr.Int64(abs)
					return xdr.ClaimPredicate{Type: xdr.ClaimPredicateTypeClaimPredicateBeforeAbsoluteTime, AbsBefore: &v}, nil
				case "before_relative_time":
					val := s["value"]
					if val == nil {
						val = s["rel_before"]
					}
					rel, err := CoerceInt64(val)
					if err != nil {
						return xdr.ClaimPredicate{}, fmt.Errorf("before_relative_time: %w", err)
					}
					v := xdr.Int64(rel)
					return xdr.ClaimPredicate{Type: xdr.ClaimPredicateTypeClaimPredicateBeforeRelativeTime, RelBefore: &v}, nil
				case "after_absolute_time", "after_time":
					val := s["value"]
					if val == nil {
						val = s["abs_before"]
					}
					abs, err := CoerceInt64(val)
					if err != nil {
						return xdr.ClaimPredicate{}, fmt.Errorf("after_time: %w", err)
					}
					v := xdr.Int64(abs)
					inner := xdr.ClaimPredicate{Type: xdr.ClaimPredicateTypeClaimPredicateBeforeAbsoluteTime, AbsBefore: &v}
					innerPtr := &inner
					return xdr.ClaimPredicate{Type: xdr.ClaimPredicateTypeClaimPredicateNot, NotPredicate: &innerPtr}, nil
				case "not":
					val := s["predicate"]
					if val == nil {
						val = s["value"]
					}
					inner, err := parseClaimPredicate(val)
					if err != nil {
						return xdr.ClaimPredicate{}, fmt.Errorf("not: %w", err)
					}
					innerPtr := &inner
					return xdr.ClaimPredicate{Type: xdr.ClaimPredicateTypeClaimPredicateNot, NotPredicate: &innerPtr}, nil
				case "and":
					val := s["predicates"]
					if val == nil {
						val = s["value"]
					}
					return parseCompoundPredicate(xdr.ClaimPredicateTypeClaimPredicateAnd, val)
				case "or":
					val := s["predicates"]
					if val == nil {
						val = s["value"]
					}
					return parseCompoundPredicate(xdr.ClaimPredicateTypeClaimPredicateOr, val)
				}
			}
		}

		return xdr.ClaimPredicate{}, fmt.Errorf("%w: unrecognized claim predicate map: %v", ErrInvalidTxSpec, s)

	case []any:
		return parseCompoundPredicate(xdr.ClaimPredicateTypeClaimPredicateAnd, s)

	default:
		return xdr.ClaimPredicate{}, fmt.Errorf("%w: unsupported predicate specification %T", ErrInvalidTxSpec, spec)
	}
}

func parseCompoundPredicate(opType xdr.ClaimPredicateType, val any) (xdr.ClaimPredicate, error) {
	slice, err := coerceToSlice(val)
	if err != nil {
		return xdr.ClaimPredicate{}, fmt.Errorf("%w: compound predicate requires array of predicates", ErrInvalidTxSpec)
	}
	if len(slice) == 0 {
		return xdr.ClaimPredicate{}, fmt.Errorf("%w: compound predicate array cannot be empty", ErrInvalidTxSpec)
	}
	if len(slice) == 1 {
		return parseClaimPredicate(slice[0])
	}
	if len(slice) == 2 {
		left, err := parseClaimPredicate(slice[0])
		if err != nil {
			return xdr.ClaimPredicate{}, fmt.Errorf("child [0]: %w", err)
		}
		right, err := parseClaimPredicate(slice[1])
		if err != nil {
			return xdr.ClaimPredicate{}, fmt.Errorf("child [1]: %w", err)
		}
		children := []xdr.ClaimPredicate{left, right}
		if opType == xdr.ClaimPredicateTypeClaimPredicateAnd {
			return xdr.ClaimPredicate{Type: opType, AndPredicates: &children}, nil
		}
		return xdr.ClaimPredicate{Type: opType, OrPredicates: &children}, nil
	}

	// For > 2 predicates, fold from right to left into binary tree
	sub, err := parseCompoundPredicate(opType, slice[1:])
	if err != nil {
		return xdr.ClaimPredicate{}, err
	}
	first, err := parseClaimPredicate(slice[0])
	if err != nil {
		return xdr.ClaimPredicate{}, fmt.Errorf("child [0]: %w", err)
	}
	children := []xdr.ClaimPredicate{first, sub}
	if opType == xdr.ClaimPredicateTypeClaimPredicateAnd {
		return xdr.ClaimPredicate{Type: opType, AndPredicates: &children}, nil
	}
	return xdr.ClaimPredicate{Type: opType, OrPredicates: &children}, nil
}

func coerceToSlice(val any) ([]any, error) {
	if val == nil {
		return nil, nil
	}
	switch v := val.(type) {
	case []any:
		return v, nil
	case []string:
		res := make([]any, len(v))
		for i, s := range v {
			res[i] = s
		}
		return res, nil
	case []map[string]any:
		res := make([]any, len(v))
		for i, m := range v {
			res[i] = m
		}
		return res, nil
	}
	rv := reflect.ValueOf(val)
	if rv.Kind() == reflect.Slice {
		n := rv.Len()
		res := make([]any, n)
		for i := 0; i < n; i++ {
			res[i] = rv.Index(i).Interface()
		}
		return res, nil
	}
	return nil, fmt.Errorf("expected slice, got %T", val)
}
