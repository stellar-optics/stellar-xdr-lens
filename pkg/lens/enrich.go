package lens

import (
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/stellar/go-stellar-sdk/amount"
	"github.com/stellar/go-stellar-sdk/xdr"
)

// Enricher turns a decoded XDR value into a human-friendly rendering.
//
// It receives the reflected value and returns the display string plus the
// underlying raw value to retain in machine-readable output. Returning
// ok=false leaves the value to the generic tree walker.
//
// Enrichers are the primary extension point of this package. To teach the
// tool to render a new XDR type nicely, write an Enricher and register it in
// defaultEnrichers — nothing else needs to change, and every output format
// picks it up at once. See docs/architecture.md.
type Enricher func(rv reflect.Value) (display string, raw any, ok bool)

// enricherFor maps a concrete XDR type to its Enricher. It is populated once
// at init and read-only thereafter, so it needs no synchronisation.
var enricherFor map[reflect.Type]Enricher

func init() {
	enricherFor = map[reflect.Type]Enricher{
		reflect.TypeOf(xdr.AccountId{}):        enrichAccountID,
		reflect.TypeOf(xdr.MuxedAccount{}):     enrichMuxedAccount,
		reflect.TypeOf(xdr.Asset{}):            enrichAsset,
		reflect.TypeOf(xdr.ChangeTrustAsset{}): enrichChangeTrustAsset,
		reflect.TypeOf(xdr.TrustLineAsset{}):   enrichTrustLineAsset,
		reflect.TypeOf(xdr.TimePoint(0)):       enrichTimePoint,
		reflect.TypeOf(xdr.Duration(0)):        enrichDuration,
	}
}

// enrich looks up and applies the Enricher registered for rv's type.
//
// Generated XDR accessors dereference union arms without checking the
// discriminant, so they panic on a zero or malformed value. Since this tool
// is expected to decode untrusted, possibly truncated blobs, every enricher
// runs behind a recover: a value we cannot prettify falls back to the generic
// rendering instead of taking the process down.
func enrich(rv reflect.Value) (display string, raw any, ok bool) {
	fn, found := enricherFor[rv.Type()]
	if !found {
		return "", nil, false
	}
	defer func() {
		if r := recover(); r != nil {
			display, raw, ok = "", nil, false
		}
	}()
	return fn(rv)
}

func enrichAccountID(rv reflect.Value) (string, any, bool) {
	aid, valid := rv.Interface().(xdr.AccountId)
	if !valid {
		return "", nil, false
	}
	addr, err := aid.GetAddress()
	if err != nil || addr == "" {
		return "", nil, false
	}
	return "", addr, true
}

func enrichMuxedAccount(rv reflect.Value) (string, any, bool) {
	ma, valid := rv.Interface().(xdr.MuxedAccount)
	if !valid {
		return "", nil, false
	}
	// Address has a pointer receiver and a reflected struct field is not
	// addressable, so operate on a copy we own.
	addr, err := (&ma).GetAddress()
	if err != nil || addr == "" {
		return "", nil, false
	}
	// A multiplexed account also carries a subaccount id worth surfacing.
	if ma.Type == xdr.CryptoKeyTypeKeyTypeMuxedEd25519 {
		if id, idErr := (&ma).GetId(); idErr == nil {
			return "muxed id " + strconv.FormatUint(id, 10), addr, true
		}
	}
	return "", addr, true
}

func enrichAsset(rv reflect.Value) (string, any, bool) {
	a, valid := rv.Interface().(xdr.Asset)
	if !valid {
		return "", nil, false
	}
	return "", a.StringCanonical(), true
}

func enrichChangeTrustAsset(rv reflect.Value) (string, any, bool) {
	a, valid := rv.Interface().(xdr.ChangeTrustAsset)
	if !valid {
		return "", nil, false
	}
	// Liquidity pool shares are a distinct arm with no canonical string.
	if a.Type == xdr.AssetTypeAssetTypePoolShare {
		return "", nil, false
	}
	return "", a.ToAsset().StringCanonical(), true
}

func enrichTrustLineAsset(rv reflect.Value) (string, any, bool) {
	a, valid := rv.Interface().(xdr.TrustLineAsset)
	if !valid {
		return "", nil, false
	}
	if a.Type == xdr.AssetTypeAssetTypePoolShare {
		return "", nil, false
	}
	return "", a.ToAsset().StringCanonical(), true
}

func enrichTimePoint(rv reflect.Value) (string, any, bool) {
	tp, valid := rv.Interface().(xdr.TimePoint)
	if !valid {
		return "", nil, false
	}
	if tp == 0 {
		return "", uint64(0), true
	}
	return time.Unix(int64(tp), 0).UTC().Format(time.RFC3339), uint64(tp), true
}

func enrichDuration(rv reflect.Value) (string, any, bool) {
	d, valid := rv.Interface().(xdr.Duration)
	if !valid {
		return "", nil, false
	}
	if d == 0 {
		return "", uint64(0), true
	}
	return (time.Duration(d) * time.Second).String(), uint64(d), true
}

// amountFields are the field names whose xdr.Int64 payload is a stroop
// amount. XDR models amounts, sequence numbers and offer ids with the same
// Int64 type, so the field name is the only available signal. Anything not
// listed here is left as a plain integer, which is the safe default: showing
// a sequence number as "12345.0000000" would be worse than showing it raw.
var amountFields = map[string]bool{
	"Amount":           true,
	"StartingBalance":  true,
	"Limit":            true,
	"SendAmount":       true,
	"SendMax":          true,
	"DestAmount":       true,
	"DestMin":          true,
	"BuyAmount":        true,
	"MaxAmountA":       true,
	"MaxAmountB":       true,
	"MinAmountA":       true,
	"MinAmountB":       true,
	"Balance":          true,
	"FeeCharged":       true,
	"ClaimableBalance": true,
}

// enrichAmountField applies stroop formatting when a field is known to carry
// an amount. It is applied by the walker after the type-based registry, since
// it depends on the field name rather than the type alone.
func enrichAmountField(fieldName string, rv reflect.Value) (string, bool) {
	if !amountFields[fieldName] {
		return "", false
	}
	v, ok := rv.Interface().(xdr.Int64)
	if !ok {
		return "", false
	}
	return amount.String(v) + " (stroops: " + strconv.FormatInt(int64(v), 10) + ")", true
}

// isEnumType reports whether t is a generated XDR enum. Enums carry a
// ValidEnum method; plain numeric aliases such as Int64 and Uint32 do not,
// which keeps ordinary numbers from being rendered as constant names.
func isEnumType(t reflect.Type) bool {
	_, ok := t.MethodByName("ValidEnum")
	return ok
}

// operationTypeLabel converts an XDR operation type constant into the
// snake_case name used by Horizon and the Stellar docs, which is what users
// recognise. Falls back to the raw constant name for unknown types.
func operationTypeLabel(t xdr.OperationType) string {
	name := t.String()
	name = strings.TrimPrefix(name, "OperationType")
	return toSnake(name)
}

func toSnake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(r + ('a' - 'A'))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
