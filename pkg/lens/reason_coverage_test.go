package lens_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-xdr/stellar-xdr-lens/pkg/lens"
)

// resultCodeTypes lists every result-code enum in the protocol. A new enum
// added by a future SDK will not be covered until it is listed here, which is
// deliberate: the list is the explicit statement of what this build claims to
// explain.
var resultCodeTypes = []any{
	xdr.TransactionResultCode(0),
	xdr.OperationResultCode(0),
	xdr.CreateAccountResultCode(0),
	xdr.PaymentResultCode(0),
	xdr.PathPaymentStrictReceiveResultCode(0),
	xdr.PathPaymentStrictSendResultCode(0),
	xdr.ManageSellOfferResultCode(0),
	xdr.ManageBuyOfferResultCode(0),
	xdr.SetOptionsResultCode(0),
	xdr.ChangeTrustResultCode(0),
	xdr.AllowTrustResultCode(0),
	xdr.AccountMergeResultCode(0),
	xdr.InflationResultCode(0),
	xdr.ManageDataResultCode(0),
	xdr.BumpSequenceResultCode(0),
	xdr.CreateClaimableBalanceResultCode(0),
	xdr.ClaimClaimableBalanceResultCode(0),
	xdr.BeginSponsoringFutureReservesResultCode(0),
	xdr.EndSponsoringFutureReservesResultCode(0),
	xdr.RevokeSponsorshipResultCode(0),
	xdr.ClawbackResultCode(0),
	xdr.ClawbackClaimableBalanceResultCode(0),
	xdr.SetTrustLineFlagsResultCode(0),
	xdr.LiquidityPoolDepositResultCode(0),
	xdr.LiquidityPoolWithdrawResultCode(0),
	xdr.InvokeHostFunctionResultCode(0),
	xdr.ExtendFootprintTtlResultCode(0),
	xdr.RestoreFootprintResultCode(0),
}

// unknownCodeMarker is the phrase ExplainCode uses when it has no entry.
// Matching on it is how this test tells a real explanation from the fallback.
const unknownCodeMarker = "No description on file"

// TestEveryResultCodeIsExplained enumerates every valid value of every result
// enum in the SDK and asserts that lens has a written explanation for it.
//
// This is the test that keeps the tool honest as the protocol grows: when a
// new result code ships, this fails and names it, instead of the CLI quietly
// printing a shrug.
func TestEveryResultCodeIsExplained(t *testing.T) {
	t.Parallel()

	var missing []string
	total := 0

	for _, proto := range resultCodeTypes {
		rt := reflect.TypeOf(proto)
		for _, name := range enumConstantNames(t, rt) {
			total++
			got := lens.ExplainCode(name)
			if strings.Contains(got.Summary, unknownCodeMarker) {
				missing = append(missing, name)
			}
			if got.Code == "" {
				t.Errorf("ExplainCode(%q).Code is empty", name)
			}
		}
	}

	if total == 0 {
		t.Fatal("enumerated no result codes; the reflection helper is broken")
	}
	t.Logf("checked %d result codes across %d enums", total, len(resultCodeTypes))

	if len(missing) > 0 {
		t.Errorf("%d result code(s) have no explanation:\n  %s",
			len(missing), strings.Join(missing, "\n  "))
	}
}

// TestResultCodeShortFormsAreUnique guards the short codes used in output and
// in scripts. Two distinct protocol codes collapsing to one short form would
// make the CLI ambiguous.
func TestResultCodeShortFormsAreUnique(t *testing.T) {
	t.Parallel()

	seen := make(map[string]string)
	for _, proto := range resultCodeTypes {
		rt := reflect.TypeOf(proto)
		for _, name := range enumConstantNames(t, rt) {
			code := lens.ExplainCode(name).Code
			if prev, dup := seen[code]; dup && prev != name {
				t.Errorf("short code %q is produced by both %q and %q", code, prev, name)
			}
			seen[code] = name
		}
	}
}

// enumConstantNames returns the constant name of every valid value of a
// generated XDR enum.
//
// The generated code keeps its value-to-name map unexported, but exposes
// ValidEnum and String, so the valid values are recovered by scanning a range
// wide enough to cover every result code the protocol defines.
func enumConstantNames(t *testing.T, rt reflect.Type) []string {
	t.Helper()

	validEnum, ok := rt.MethodByName("ValidEnum")
	if !ok {
		t.Fatalf("%s has no ValidEnum method; it is not a generated XDR enum", rt.Name())
	}

	var names []string
	seen := make(map[string]bool)

	// Result codes are small negative integers, with success at 0. The range
	// is deliberately wider than any current enum so new codes are caught.
	for i := int32(-64); i <= 64; i++ {
		v := reflect.New(rt).Elem()
		v.SetInt(int64(i))

		out := validEnum.Func.Call([]reflect.Value{v, reflect.ValueOf(i)})
		if len(out) != 1 || !out[0].Bool() {
			continue
		}

		s, ok := v.Interface().(fmt.Stringer)
		if !ok {
			t.Fatalf("%s does not implement fmt.Stringer", rt.Name())
		}
		name := s.String()
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}

	if len(names) == 0 {
		t.Fatalf("%s yielded no valid enum values", rt.Name())
	}
	return names
}
