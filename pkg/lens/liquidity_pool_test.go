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
