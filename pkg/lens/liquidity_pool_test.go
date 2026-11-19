package lens_test

import (
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

func TestLiquidityPoolEntryDecoding(t *testing.T) {
	t.Parallel()

	v, err := lens.DecodeAs(loadFixture(t, "liquidity_pool_entry"), "LiquidityPoolEntry")
	if err != nil {
		t.Fatalf("DecodeAs() error = %v", err)
	}
	if v.Type != "LiquidityPoolEntry" {
		t.Errorf("Type = %q, want LiquidityPoolEntry", v.Type)
	}

	// Verify key fields are present in the node tree
	poolID := v.Node.Find("LiquidityPoolId")
	if poolID == nil {
		t.Fatal("LiquidityPoolId not found in tree")
	}
	if poolID.Kind != lens.KindScalar {
		t.Errorf("LiquidityPoolId Kind = %v, want KindScalar", poolID.Kind)
	}

	body := v.Node.Find("Body")
	if body == nil {
		t.Fatal("Body not found in tree")
	}

	constantProduct := body.Child("ConstantProduct")
	if constantProduct == nil {
		t.Fatal("ConstantProduct not found in tree")
	}

	params := constantProduct.Child("Params")
	if params == nil {
		t.Fatal("Params not found in tree")
	}
	if params.Kind != lens.KindScalar {
		t.Errorf("Params Kind = %v, want KindScalar", params.Kind)
	}
	paramsLine, ok := params.Value.(string)
	if !ok {
		t.Fatalf("Params Value = %T, want string", params.Value)
	}
	for _, want := range []string{"USDC", "native", "@ 0.3%"} {
		if !strings.Contains(paramsLine, want) {
			t.Errorf("Params = %q, want to contain %q", paramsLine, want)
		}
	}

	reserveA := constantProduct.Child("ReserveA")
	if reserveA == nil {
		t.Fatal("ReserveA not found in tree")
	}
	if reserveA.Value != int64(1_000_000_000_000) {
		t.Errorf("ReserveA Value = %v, want 1000000000000", reserveA.Value)
	}
	if !strings.Contains(reserveA.Note, "100000.0000000") {
		t.Errorf("ReserveA Note = %q, want formatted amount", reserveA.Note)
	}

	reserveB := constantProduct.Child("ReserveB")
	if reserveB == nil {
		t.Fatal("ReserveB not found in tree")
	}
	if reserveB.Value != int64(5_000_000_000_000) {
		t.Errorf("ReserveB Value = %v, want 5000000000000", reserveB.Value)
	}

	totalPoolShares := constantProduct.Child("TotalPoolShares")
	if totalPoolShares == nil {
		t.Fatal("TotalPoolShares not found in tree")
	}
	if totalPoolShares.Value != int64(2_000_000_000_000) {
		t.Errorf("TotalPoolShares Value = %v, want 2000000000000", totalPoolShares.Value)
	}

	poolSharesTrustLineCount := constantProduct.Child("PoolSharesTrustLineCount")
	if poolSharesTrustLineCount == nil {
		t.Fatal("PoolSharesTrustLineCount not found in tree")
	}
	if poolSharesTrustLineCount.Value != int64(150) {
		t.Errorf("PoolSharesTrustLineCount Value = %v, want 150", poolSharesTrustLineCount.Value)
	}
}

func TestPoolParametersDecoding(t *testing.T) {
	t.Parallel()

	params := xdr.LiquidityPoolConstantProductParameters{
		AssetA: xdr.MustNewNativeAsset(),
		AssetB: creditAsset(t, "USDC", testIssuer),
		Fee:    xdr.Int32(1),
	}
	v, err := lens.DecodeAs(mustMarshal(t, params), "LiquidityPoolConstantProductParameters")
	if err != nil {
		t.Fatalf("DecodeAs() error = %v", err)
	}
	if v.Node.Kind != lens.KindScalar {
		t.Errorf("Kind = %v, want KindScalar", v.Node.Kind)
	}
	line, ok := v.Node.Value.(string)
	if !ok {
		t.Fatalf("Value = %T, want string", v.Node.Value)
	}
	if !strings.HasPrefix(line, "native / USDC:") {
		t.Errorf("Params = %q, want 'native / USDC:...'", line)
	}
	if !strings.HasSuffix(line, "@ 0.01%") {
		t.Errorf("Params = %q, want '@ 0.01%%' suffix", line)
	}
}

func TestLiquidityPoolEntryEnrichment(t *testing.T) {
	t.Parallel()

	v, err := lens.DecodeAs(loadFixture(t, "liquidity_pool_entry"), "LiquidityPoolEntry")
	if err != nil {
		t.Fatalf("DecodeAs() error = %v", err)
	}

	// The pool id keeps its hex value and names what the hex identifies.
	poolID := v.Node.Find("LiquidityPoolId")
	if poolID == nil {
		t.Fatal("LiquidityPoolId not found in tree")
	}
	hexID, ok := poolID.Value.(string)
	if !ok {
		t.Fatalf("LiquidityPoolId Value = %T, want string hex", poolID.Value)
	}
	if !strings.HasPrefix(hexID, "4141414141") {
		t.Errorf("LiquidityPoolId Value = %q, want the fixture hash hex", hexID)
	}
	for _, want := range []string{"constant product pool", "USDC", "native", "@ 0.3%"} {
		if !strings.Contains(poolID.Note, want) {
			t.Errorf("LiquidityPoolId Note = %q, want to contain %q", poolID.Note, want)
		}
	}

	// Pool shares are decimal tokens, not lumens: the note has to say so.
	totalPoolShares := v.Node.Find("Body.ConstantProduct.TotalPoolShares")
	if totalPoolShares == nil {
		t.Fatal("TotalPoolShares not found in tree")
	}
	for _, want := range []string{"200000.0000000", "pool share tokens", "stroops"} {
		if !strings.Contains(totalPoolShares.Note, want) {
			t.Errorf("TotalPoolShares Note = %q, want to contain %q", totalPoolShares.Note, want)
		}
	}

	trustLineCount := v.Node.Find("Body.ConstantProduct.PoolSharesTrustLineCount")
	if trustLineCount == nil {
		t.Fatal("PoolSharesTrustLineCount not found in tree")
	}
	if trustLineCount.Note != "held by 150 trustlines" {
		t.Errorf("PoolSharesTrustLineCount Note = %q, want %q", trustLineCount.Note, "held by 150 trustlines")
	}
}

func TestLiquidityPoolEntryDetection(t *testing.T) {
	t.Parallel()

	cands, err := lens.Detect(loadFixture(t, "liquidity_pool_entry"))
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if len(cands) == 0 {
		t.Fatal("Detect() returned no candidates")
	}
	found := false
	for _, c := range cands {
		if c.Type == "LiquidityPoolEntry" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Detect() did not find LiquidityPoolEntry among candidates: %v", cands)
	}
}

func TestLiquidityPoolEntryAmountFieldsFormatted(t *testing.T) {
	t.Parallel()

	v, err := lens.DecodeAs(loadFixture(t, "liquidity_pool_entry"), "LiquidityPoolEntry")
	if err != nil {
		t.Fatalf("DecodeAs() error = %v", err)
	}

	// Check that amount fields have formatted notes
	amountFields := []string{"ReserveA", "ReserveB", "TotalPoolShares"}
	for _, field := range amountFields {
		node := v.Node.Find("Body.ConstantProduct." + field)
		if node == nil {
			t.Errorf("Field %s not found", field)
			continue
		}
		if node.Note == "" {
			t.Errorf("Field %s has no formatted note", field)
		}
		if !strings.Contains(node.Note, "stroops") {
			t.Errorf("Field %s note %q does not contain stroops", field, node.Note)
		}
	}
}

func TestLiquidityPoolEntryExplainNotSupported(t *testing.T) {
	t.Parallel()

	v, err := lens.DecodeAs(loadFixture(t, "liquidity_pool_entry"), "LiquidityPoolEntry")
	if err != nil {
		t.Fatalf("DecodeAs() error = %v", err)
	}

	// Explain only supports TransactionEnvelope and TransactionResult
	_, err = lens.Explain(v)
	if err == nil {
		t.Fatal("Explain() should return error for LiquidityPoolEntry")
	}
	if !strings.Contains(err.Error(), "cannot explain") {
		t.Errorf("Explain() error = %v, want 'cannot explain'", err)
	}
}

func TestDiagnosePoolDeposit(t *testing.T) {
	t.Parallel()

	deposit := func(a, b xdr.Int64, minPrice, maxPrice xdr.Price) *xdr.LiquidityPoolDepositOp {
		return &xdr.LiquidityPoolDepositOp{
			LiquidityPoolId: testPoolID,
			MaxAmountA:      a,
			MaxAmountB:      b,
			MinPrice:        minPrice,
			MaxPrice:        maxPrice,
		}
	}
	const (
		malformed = xdr.LiquidityPoolDepositResultCodeLiquidityPoolDepositMalformed
		badPrice  = xdr.LiquidityPoolDepositResultCodeLiquidityPoolDepositBadPrice
		poolFull  = xdr.LiquidityPoolDepositResultCodeLiquidityPoolDepositPoolFull
	)

	tests := []struct {
		name    string
		op      *xdr.LiquidityPoolDepositOp
		code    xdr.LiquidityPoolDepositResultCode
		want    []string
		notWant []string
	}{
		{
			name: "implied price below minPrice",
			op:   deposit(10000000, 40000000, xdr.Price{N: 1, D: 2}, xdr.Price{N: 2, D: 1}),
			code: badPrice,
			want: []string{"1/4 (≈0.2500000)", "below minPrice 1/2 (≈0.5000000)", "price ratio drift"},
		},
		{
			name: "implied price above maxPrice",
			op:   deposit(40000000, 10000000, xdr.Price{N: 1, D: 2}, xdr.Price{N: 2, D: 1}),
			code: badPrice,
			want: []string{"4/1 (≈4.0000000)", "above maxPrice 2/1 (≈2.0000000)", "price ratio drift"},
		},
		{
			name:    "implied price on the minPrice boundary is inside the window",
			op:      deposit(10000000, 20000000, xdr.Price{N: 1, D: 2}, xdr.Price{N: 2, D: 1}),
			code:    badPrice,
			want:    []string{"1/2 (≈0.5000000)", "inside the window", "reserve ratio drifted"},
			notWant: []string{"below minPrice"},
		},
		{
			name:    "implied price on the maxPrice boundary is inside the window",
			op:      deposit(20000000, 10000000, xdr.Price{N: 1, D: 2}, xdr.Price{N: 2, D: 1}),
			code:    badPrice,
			want:    []string{"2/1 (≈2.0000000)", "inside the window"},
			notWant: []string{"above maxPrice"},
		},
		{
			name: "comparison stays exact where rounding would hide the breach",
			op:   deposit(1000000001, 1000000000, xdr.Price{N: 1, D: 1}, xdr.Price{N: 1, D: 1}),
			code: badPrice,
			want: []string{"1000000001/1000000000", "above maxPrice 1/1"},
		},
		{
			name: "extreme amounts and bounds do not overflow",
			op:   deposit(9223372036854775807, 1, xdr.Price{N: 1, D: 2147483647}, xdr.Price{N: 2147483647, D: 1}),
			code: badPrice,
			want: []string{"9223372036854775807/1", "above maxPrice 2147483647/1"},
		},
		{
			name: "bad price with an empty window defers to malformed",
			op:   deposit(10000000, 10000000, xdr.Price{N: 2, D: 1}, xdr.Price{N: 1, D: 2}),
			code: badPrice,
		},
		{
			name: "bad price with a zero amount does not divide by zero",
			op:   deposit(10000000, 0, xdr.Price{N: 1, D: 2}, xdr.Price{N: 2, D: 1}),
			code: badPrice,
		},
		{
			name: "malformed names the violated boundary",
			op:   deposit(10000000, 10000000, xdr.Price{N: 2, D: 1}, xdr.Price{N: 1, D: 2}),
			code: malformed,
			want: []string{"boundary rule", "minPrice 2/1 exceeds maxPrice 1/2"},
		},
		{
			name: "malformed without a provable cause says nothing",
			op:   deposit(10000000, 10000000, xdr.Price{N: 1, D: 2}, xdr.Price{N: 2, D: 1}),
			code: malformed,
		},
		{
			name: "pool full names the amounts and the protocol maximum",
			op:   deposit(10000000, 20000000, xdr.Price{N: 1, D: 2}, xdr.Price{N: 2, D: 1}),
			code: poolFull,
			want: []string{"1.0000000", "2.0000000", "2^63-1"},
		},
		{
			name: "unrelated code says nothing",
			op:   deposit(10000000, 20000000, xdr.Price{N: 1, D: 2}, xdr.Price{N: 2, D: 1}),
			code: xdr.LiquidityPoolDepositResultCodeLiquidityPoolDepositNoTrust,
		},
		{
			name: "nil operation says nothing",
			code: badPrice,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := lens.DiagnosePoolDeposit(tc.op, tc.code)
			if len(tc.want) == 0 && got != "" {
				t.Fatalf("DiagnosePoolDeposit() = %q, want empty", got)
			}
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("DiagnosePoolDeposit() = %q, want it to contain %q", got, want)
				}
			}
			for _, notWant := range tc.notWant {
				if strings.Contains(got, notWant) {
					t.Errorf("DiagnosePoolDeposit() = %q, must not contain %q", got, notWant)
				}
			}
		})
	}
}

func TestDiagnosePoolWithdraw(t *testing.T) {
	t.Parallel()

	valid := &xdr.LiquidityPoolWithdrawOp{
		LiquidityPoolId: testPoolID,
		Amount:          100000000,
		MinAmountA:      5000000,
		MinAmountB:      7000000,
	}

	tests := []struct {
		name string
		op   *xdr.LiquidityPoolWithdrawOp
		code xdr.LiquidityPoolWithdrawResultCode
		want []string
	}{
		{
			name: "under minimum states the burn, both minimums and the remedy",
			op:   valid,
			code: xdr.LiquidityPoolWithdrawResultCodeLiquidityPoolWithdrawUnderMinimum,
			want: []string{
				"10.0000000 pool shares", "0.5000000 of asset A", "0.7000000 of asset B",
				"slippage tolerance", "check the pool balance",
			},
		},
		{
			name: "underfunded states the share burn",
			op:   valid,
			code: xdr.LiquidityPoolWithdrawResultCodeLiquidityPoolWithdrawUnderfunded,
			want: []string{"10.0000000 pool shares", "pool-share trustline"},
		},
		{
			name: "malformed flags a non-positive burn",
			op:   &xdr.LiquidityPoolWithdrawOp{LiquidityPoolId: testPoolID, MinAmountA: 1, MinAmountB: 1},
			code: xdr.LiquidityPoolWithdrawResultCodeLiquidityPoolWithdrawMalformed,
			want: []string{"0.0000000 pool shares", "must be positive"},
		},
		{
			name: "malformed flags a negative minimum",
			op:   &xdr.LiquidityPoolWithdrawOp{LiquidityPoolId: testPoolID, Amount: 1, MinAmountB: -1},
			code: xdr.LiquidityPoolWithdrawResultCodeLiquidityPoolWithdrawMalformed,
			want: []string{"-0.0000001 for asset B", "negative"},
		},
		{
			name: "malformed without a provable cause says nothing",
			op:   valid,
			code: xdr.LiquidityPoolWithdrawResultCodeLiquidityPoolWithdrawMalformed,
		},
		{
			name: "unrelated code says nothing",
			op:   valid,
			code: xdr.LiquidityPoolWithdrawResultCodeLiquidityPoolWithdrawNoTrust,
		},
		{
			name: "nil operation says nothing",
			code: xdr.LiquidityPoolWithdrawResultCodeLiquidityPoolWithdrawUnderMinimum,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := lens.DiagnosePoolWithdraw(tc.op, tc.code)
			if len(tc.want) == 0 && got != "" {
				t.Fatalf("DiagnosePoolWithdraw() = %q, want empty", got)
			}
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("DiagnosePoolWithdraw() = %q, want it to contain %q", got, want)
				}
			}
		})
	}
}

// TestExplainPairAttachesPoolDiagnoses pins the wiring: the diagnosis reaches
// the operation's Reason when an envelope is paired with its result.
func TestExplainPairAttachesPoolDiagnoses(t *testing.T) {
	t.Parallel()

	depositOp := xdr.LiquidityPoolDepositOp{
		LiquidityPoolId: testPoolID,
		MaxAmountA:      10000000,
		MaxAmountB:      40000000,
		MinPrice:        xdr.Price{N: 1, D: 2},
		MaxPrice:        xdr.Price{N: 2, D: 1},
	}
	withdrawOp := xdr.LiquidityPoolWithdrawOp{
		LiquidityPoolId: testPoolID,
		Amount:          100000000,
		MinAmountA:      5000000,
		MinAmountB:      7000000,
	}

	tests := []struct {
		name  string
		body  xdr.OperationBody
		inner xdr.OperationResultTr
		want  string
	}{
		{
			name: "deposit bad price",
			body: xdr.OperationBody{Type: xdr.OperationTypeLiquidityPoolDeposit, LiquidityPoolDepositOp: &depositOp},
			inner: xdr.OperationResultTr{
				Type: xdr.OperationTypeLiquidityPoolDeposit,
				LiquidityPoolDepositResult: &xdr.LiquidityPoolDepositResult{
					Code: xdr.LiquidityPoolDepositResultCodeLiquidityPoolDepositBadPrice,
				},
			},
			want: "below minPrice 1/2",
		},
		{
			name: "withdraw under minimum",
			body: xdr.OperationBody{Type: xdr.OperationTypeLiquidityPoolWithdraw, LiquidityPoolWithdrawOp: &withdrawOp},
			inner: xdr.OperationResultTr{
				Type: xdr.OperationTypeLiquidityPoolWithdraw,
				LiquidityPoolWithdrawResult: &xdr.LiquidityPoolWithdrawResult{
					Code: xdr.LiquidityPoolWithdrawResultCodeLiquidityPoolWithdrawUnderMinimum,
				},
			},
			want: "0.5000000 of asset A",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := explainSingleOperation(t, tc.body, tc.inner)
			if !strings.Contains(r.Context, tc.want) {
				t.Errorf("Context = %q, want it to contain %q", r.Context, tc.want)
			}
		})
	}
}
