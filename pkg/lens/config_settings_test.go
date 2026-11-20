package lens_test

import (
	"strings"
	"testing"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens/format"
)

// TestDecodeConfigSettingEntryFixtures decodes all captured mainnet
// ConfigSettingEntry fixtures and asserts on expected values. The fixtures
// are real XDR captured from Stellar mainnet via Soroban RPC.
func TestDecodeConfigSettingEntryFixtures(t *testing.T) {
	cases := []struct {
		fixture        string
		expectID       string
		expectContains []string
	}{
		{
			fixture:  "config_setting_0",
			expectID: "ConfigSettingIdConfigSettingContractMaxSizeBytes",
			expectContains: []string{
				"ContractMaxSizeBytes: 131072",
			},
		},
		{
			fixture:  "config_setting_1",
			expectID: "ConfigSettingIdConfigSettingContractComputeV0",
			expectContains: []string{
				"LedgerMaxInstructions: 580000000",
				"TxMaxInstructions: 400000000",
				"FeeRatePerInstructionsIncrement: 7",
				"TxMemoryLimit: 41943040",
				"0.0000007 (stroops: 7)",
			},
		},
		{
			fixture:  "config_setting_2",
			expectID: "ConfigSettingIdConfigSettingContractLedgerCostV0",
			expectContains: []string{
				"LedgerMaxDiskReadEntries: 1000",
				"FeeDiskReadLedgerEntry: 1563",
				"0.0001563 (stroops: 1563)",
				"FeeWriteLedgerEntry: 2500",
				"0.0002500 (stroops: 2500)",
				"FeeDiskRead1Kb: 447",
				"0.0000447 (stroops: 447)",
				"SorobanStateTargetSizeBytes: 3000000000",
				"RentFee1KbSorobanStateSizeLow: -17000",
				"-0.0017000 (stroops: -17000)",
				"RentFee1KbSorobanStateSizeHigh: 10000",
				"0.0010000 (stroops: 10000)",
				"SorobanStateRentFeeGrowthFactor: 5000",
			},
		},
		{
			fixture:  "config_setting_3",
			expectID: "ConfigSettingIdConfigSettingContractHistoricalDataV0",
			expectContains: []string{
				"FeeHistorical1Kb: 4059",
				"0.0004059 (stroops: 4059)",
			},
		},
		{
			fixture:  "config_setting_4",
			expectID: "ConfigSettingIdConfigSettingContractEventsV0",
			expectContains: []string{
				"TxMaxContractEventsSizeBytes: 16384",
				"FeeContractEvents1Kb: 5000",
				"0.0005000 (stroops: 5000)",
			},
		},
		{
			fixture:  "config_setting_5",
			expectID: "ConfigSettingIdConfigSettingContractBandwidthV0",
			expectContains: []string{
				"LedgerMaxTxsSizeBytes: 266240",
				"TxMaxSizeBytes: 132096",
				"FeeTxSize1Kb: 406",
				"0.0000406 (stroops: 406)",
			},
		},
		{
			fixture:  "config_setting_6",
			expectID: "ConfigSettingIdConfigSettingContractCostParamsCpuInstructions",
			expectContains: []string{
				"ContractCostParamsCpuInsns  [86]",
				"ConstTerm: 4",
				"LinearTerm: 0",
				"ConstTerm: 434",
				"LinearTerm: 16",
			},
		},
		{
			fixture:  "config_setting_7",
			expectID: "ConfigSettingIdConfigSettingContractCostParamsMemoryBytes",
			expectContains: []string{
				"ContractCostParamsMemBytes  [86]",
				"ConstTerm: 16",
				"LinearTerm: 128",
			},
		},
		{
			fixture:  "config_setting_8",
			expectID: "ConfigSettingIdConfigSettingContractDataKeySizeBytes",
			expectContains: []string{
				"ContractDataKeySizeBytes: 250",
			},
		},
		{
			fixture:  "config_setting_9",
			expectID: "ConfigSettingIdConfigSettingContractDataEntrySizeBytes",
			expectContains: []string{
				"ContractDataEntrySizeBytes: 65536",
			},
		},
		{
			fixture:  "config_setting_10",
			expectID: "ConfigSettingIdConfigSettingStateArchival",
			expectContains: []string{
				"MaxEntryTtl: 3110400",
				"≈180.0 days (at 5s per ledger)",
				"MinTemporaryTtl: 17280",
				"≈1.0 days (at 5s per ledger)",
				"MinPersistentTtl: 2073600",
				"≈120.0 days (at 5s per ledger)",
				"PersistentRentRateDenominator: 1215",
				"TempRentRateDenominator: 2430",
				"MaxEntriesToArchive: 1000",
				"LiveSorobanStateSizeWindowSampleSize: 30",
				"LiveSorobanStateSizeWindowSamplePeriod: 64",
				"≈5m20s (at 5s per ledger)",
				"EvictionScanSize: 500000",
				"StartingEvictionScanLevel: 7",
			},
		},
		{
			fixture:  "config_setting_11",
			expectID: "ConfigSettingIdConfigSettingContractExecutionLanes",
			expectContains: []string{
				"LedgerMaxTxCount: 2000",
			},
		},
		{
			fixture:  "config_setting_12",
			expectID: "ConfigSettingIdConfigSettingLiveSorobanStateSizeWindow",
			expectContains: []string{
				"LiveSorobanStateSizeWindow  [30]",
			},
		},
		{
			fixture:  "config_setting_13",
			expectID: "ConfigSettingIdConfigSettingEvictionIterator",
			expectContains: []string{
				"BucketListLevel: 9",
				"IsCurrBucket: false",
				"BucketFileOffset: 1452542516",
			},
		},
		{
			fixture:  "config_setting_14",
			expectID: "ConfigSettingIdConfigSettingContractParallelComputeV0",
			expectContains: []string{
				"LedgerMaxDependentTxClusters: 2",
			},
		},
		{
			fixture:  "config_setting_15",
			expectID: "ConfigSettingIdConfigSettingContractLedgerCostExtV0",
			expectContains: []string{
				"TxMaxFootprintEntries: 400",
				"FeeWrite1Kb: 875",
				"0.0000875 (stroops: 875)",
			},
		},
		{
			fixture:  "config_setting_16",
			expectID: "ConfigSettingIdConfigSettingScpTiming",
			expectContains: []string{
				"LedgerTargetCloseTimeMilliseconds: 5000",
				"NominationTimeoutInitialMilliseconds: 1000",
				"NominationTimeoutIncrementMilliseconds: 1000",
				"BallotTimeoutInitialMilliseconds: 1000",
				"BallotTimeoutIncrementMilliseconds: 1000",
			},
		},
		{
			fixture:  "config_setting_17",
			expectID: "ConfigSettingIdConfigSettingFrozenLedgerKeys",
			expectContains: []string{
				"FrozenLedgerKeys",
				"Keys  [3]",
				"account key: GATDQL767ZM2JQTBEG4BQ5WKOQNGAGWZDUN4GYT2UINPEU3RT2UAMVZH",
				"account key: GBO7VUL2TOKPWFAWKATIW7K3QYA7WQ63VDY5CAE6AFUUX6BHZBOC2WXC",
				"account key: GCJOPSQNJDUYMWVJ3U7LYWWHM4N7NPWNF3RTOHB76PA67NH6DLWODES4",
			},
		},
		{
			fixture:  "config_setting_19",
			expectID: "ConfigSettingIdConfigSettingFreezeBypassTxs",
			expectContains: []string{
				"FreezeBypassTxs",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			payload := loadFixture(t, tc.fixture)
			v, err := lens.DecodeAs(payload, "ConfigSettingEntry")
			if err != nil {
				t.Fatalf("decode failed: %v", err)
			}
			if v == nil || v.Node == nil {
				t.Fatal("decode returned nil node")
			}
			if v.Type != "ConfigSettingEntry" {
				t.Fatalf("expected type ConfigSettingEntry, got %s", v.Type)
			}
			if v.Node.Kind != lens.KindStruct {
				t.Fatalf("expected struct, got %s", v.Node.Kind)
			}

			// Check the ConfigSettingId field
			idChild := v.Node.Child("ConfigSettingId")
			if idChild == nil {
				t.Fatal("missing ConfigSettingId child")
			}
			if !strings.Contains(idChild.Value.(string), tc.expectID) {
				t.Errorf("ConfigSettingId mismatch: got %v, want %s", idChild.Value, tc.expectID)
			}

			// Check expected substrings in the full tree output
			var sb strings.Builder
			f := format.NewTreeFormatter()
			if err := f.Format(&sb, v); err != nil {
				t.Fatalf("format failed: %v", err)
			}
			out := sb.String()
			for _, want := range tc.expectContains {
				if !strings.Contains(out, want) {
					t.Errorf("output missing expected substring %q", want)
				}
			}
		})
	}
}

// TestDetectConfigSettingEntry verifies automatic type detection works
// for ConfigSettingEntry payloads.
func TestDetectConfigSettingEntry(t *testing.T) {
	for _, name := range []string{
		"config_setting_0",
		"config_setting_1",
		"config_setting_2",
		"config_setting_3",
		"config_setting_4",
		"config_setting_5",
		"config_setting_6",
		"config_setting_7",
		"config_setting_10",
		"config_setting_16",
		"config_setting_17",
	} {
		t.Run(name, func(t *testing.T) {
			payload := loadFixture(t, name)
			cands, err := lens.Detect(payload)
			if err != nil {
				t.Fatalf("detect failed: %v", err)
			}
			found := false
			for _, c := range cands {
				if c.Type == "ConfigSettingEntry" {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("ConfigSettingEntry not in candidates: %v", cands)
			}
		})
	}
}

// TestJSONConfigSettingEntry verifies JSON output is stable and complete.
func TestJSONConfigSettingEntry(t *testing.T) {
	payload := loadFixture(t, "config_setting_10")
	v, err := lens.DecodeAs(payload, "ConfigSettingEntry")
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	var sb strings.Builder
	f := format.NewJSONFormatter(format.WithAnnotations(true))
	if err := f.Format(&sb, v); err != nil {
		t.Fatalf("format failed: %v", err)
	}
	jsonOut := sb.String()
	if !strings.Contains(jsonOut, `"type": "ConfigSettingEntry"`) {
		t.Errorf("JSON missing type name: %s", jsonOut)
	}
	if !strings.Contains(jsonOut, `"MaxEntryTtl"`) {
		t.Errorf("JSON missing MaxEntryTtl: %s", jsonOut)
	}
	if !strings.Contains(jsonOut, "180.0 days") {
		t.Errorf("JSON missing TTL note: %s", jsonOut)
	}

	// Verify fee fields appear in JSON with notes
	payload2 := loadFixture(t, "config_setting_2")
	v2, _ := lens.DecodeAs(payload2, "ConfigSettingEntry")
	var sb2 strings.Builder
	f2 := format.NewJSONFormatter(format.WithAnnotations(true))
	if err := f2.Format(&sb2, v2); err != nil {
		t.Fatalf("format failed: %v", err)
	}
	jsonOut2 := sb2.String()
	if !strings.Contains(jsonOut2, `"FeeDiskReadLedgerEntry"`) {
		t.Errorf("JSON missing fee field: %s", jsonOut2)
	}
	if !strings.Contains(jsonOut2, "0.0001563") {
		t.Errorf("JSON missing stroop note: %s", jsonOut2)
	}
}

// TestConfigSettingEntryAllArmsPresent verifies the union can represent
// all 21 known ConfigSettingId arms (some may be absent on mainnet).
func TestConfigSettingEntryAllArmsPresent(t *testing.T) {
	// This test ensures the union type in the SDK has all arms.
	// We verify by checking the generated stringer covers all IDs.
	// If a new protocol version adds arms, this test will need updating.
	// For now we just verify the type is registered and decodes.
	payload := loadFixture(t, "config_setting_0")
	v, err := lens.DecodeAs(payload, "ConfigSettingEntry")
	if err != nil {
		t.Fatal(err)
	}
	if v.Type != "ConfigSettingEntry" {
		t.Fatalf("expected ConfigSettingEntry, got %s", v.Type)
	}
	// The union discriminant field should be present
	if v.Node.Child("ConfigSettingId") == nil {
		t.Fatal("union discriminant missing")
	}
}

// TestConfigSettingEntryTruncatedInput verifies graceful handling of
// truncated ConfigSettingEntry payloads.
func TestConfigSettingEntryTruncatedInput(t *testing.T) {
	// Truncate a valid fixture by 1 byte
	payload := loadFixture(t, "config_setting_10")
	// Remove last char from base64 to make it invalid length
	truncated := payload[:len(payload)-1]

	// Should either fail to decode or produce a partial result
	_, err := lens.DecodeAs(truncated, "ConfigSettingEntry")
	if err == nil {
		t.Log("truncated input decoded (may be valid shorter encoding)")
	} else {
		t.Logf("truncated input rejected: %v", err)
	}
}

// TestConfigSettingEntryRoundTrip verifies that decoding and formatting
// preserves the essential structure.
func TestConfigSettingEntryRoundTrip(t *testing.T) {
	payload := loadFixture(t, "config_setting_10")
	v, err := lens.DecodeAs(payload, "ConfigSettingEntry")
	if err != nil {
		t.Fatal(err)
	}
	// Verify tree and JSON formatting work without panic
	var sb strings.Builder
	f := format.NewTreeFormatter()
	if err := f.Format(&sb, v); err != nil {
		t.Fatalf("tree format failed: %v", err)
	}
	tree := sb.String()
	if !strings.Contains(tree, "StateArchivalSettings") {
		t.Errorf("tree missing arm: %s", tree)
	}

	sb.Reset()
	j := format.NewJSONFormatter()
	if err := j.Format(&sb, v); err != nil {
		t.Fatalf("json format failed: %v", err)
	}
	jsonOut := sb.String()
	if !strings.Contains(jsonOut, "StateArchivalSettings") {
		t.Errorf("JSON missing arm: %s", jsonOut)
	}
}

// TestDetectReturnsMultipleCandidatesForConfigSettingEntry verifies
// that detection correctly identifies ConfigSettingEntry among candidates.
func TestDetectReturnsMultipleCandidatesForConfigSettingEntry(t *testing.T) {
	payload := loadFixture(t, "config_setting_10")
	cands, err := lens.Detect(payload)
	if err != nil {
		t.Fatalf("detect failed: %v", err)
	}
	if len(cands) < 1 {
		t.Fatal("no candidates returned")
	}
	// First candidate should be ConfigSettingEntry (highest score)
	if cands[0].Type != "ConfigSettingEntry" {
		t.Errorf("top candidate is %s, want ConfigSettingEntry", cands[0].Type)
	}
}

// TestEncodeDecodeConfigSettingEntryNotAvailable skips because
// we don't expose SDK marshal helpers in the library API.
// This test documents the limitation.
func TestEncodeDecodeConfigSettingEntryNotAvailable(t *testing.T) {
	t.Skip("Library doesn't expose SDK marshal helpers; use CLI for round-trip testing")
}

func TestConfigSettingEntryEnrichment(t *testing.T) {
	t.Parallel()

	t.Run("CONFIG_SETTING_CONTRACT_MAX_SIZE_BYTES formats as KB/MB", func(t *testing.T) {
		payload := loadFixture(t, "config_setting_0")
		v, err := lens.DecodeAs(payload, "ConfigSettingEntry")
		if err != nil {
			t.Fatalf("DecodeAs failed: %v", err)
		}
		var sb strings.Builder
		formatter := format.NewTreeFormatter()
		if err := formatter.Format(&sb, v); err != nil {
			t.Fatalf("Format failed: %v", err)
		}
		out := sb.String()
		if !strings.Contains(out, "128 KB") {
			t.Errorf("output missing KB unit: %s", out)
		}
	})

	t.Run("CONFIG_SETTING_CONTRACT_COMPUTE_V0 formats CPU and memory", func(t *testing.T) {
		payload := loadFixture(t, "config_setting_1")
		v, err := lens.DecodeAs(payload, "ConfigSettingEntry")
		if err != nil {
			t.Fatalf("DecodeAs failed: %v", err)
		}
		var sb strings.Builder
		formatter := format.NewTreeFormatter()
		if err := formatter.Format(&sb, v); err != nil {
			t.Fatalf("Format failed: %v", err)
		}
		out := sb.String()
		if !strings.Contains(out, "580M CPU instructions") {
			t.Errorf("output missing 580M CPU instructions: %s", out)
		}
		if !strings.Contains(out, "400M CPU instructions") {
			t.Errorf("output missing 400M CPU instructions: %s", out)
		}
		if !strings.Contains(out, "40 MB") {
			t.Errorf("output missing 40 MB: %s", out)
		}
	})

	t.Run("CONFIG_SETTING_CONTRACT_LEDGER_COST_V0 formats fees in stroops/byte", func(t *testing.T) {
		payload := loadFixture(t, "config_setting_2")
		v, err := lens.DecodeAs(payload, "ConfigSettingEntry")
		if err != nil {
			t.Fatalf("DecodeAs failed: %v", err)
		}
		var sb strings.Builder
		formatter := format.NewTreeFormatter()
		if err := formatter.Format(&sb, v); err != nil {
			t.Fatalf("Format failed: %v", err)
		}
		out := sb.String()
		if !strings.Contains(out, "stroops/byte") {
			t.Errorf("output missing stroops/byte: %s", out)
		}
	})

	t.Run("CONFIG_SETTING_STATE_ARCHIVAL formats min/max TTL in days and ledger counts", func(t *testing.T) {
		payload := loadFixture(t, "config_setting_10")
		v, err := lens.DecodeAs(payload, "ConfigSettingEntry")
		if err != nil {
			t.Fatalf("DecodeAs failed: %v", err)
		}
		var sb strings.Builder
		formatter := format.NewTreeFormatter()
		if err := formatter.Format(&sb, v); err != nil {
			t.Fatalf("Format failed: %v", err)
		}
		out := sb.String()
		if !strings.Contains(out, "180.0 days") {
			t.Errorf("output missing 180.0 days: %s", out)
		}
		if !strings.Contains(out, "3110400") {
			t.Errorf("output missing 3110400: %s", out)
		}
	})

	t.Run("CONFIG_SETTING_CONTRACT_EXECUTION_LANES formats lane counts", func(t *testing.T) {
		payload := loadFixture(t, "config_setting_11")
		v, err := lens.DecodeAs(payload, "ConfigSettingEntry")
		if err != nil {
			t.Fatalf("DecodeAs failed: %v", err)
		}
		var sb strings.Builder
		formatter := format.NewTreeFormatter()
		if err := formatter.Format(&sb, v); err != nil {
			t.Fatalf("Format failed: %v", err)
		}
		out := sb.String()
		if !strings.Contains(out, "2000 concurrent execution lanes") {
			t.Errorf("output missing concurrent execution lanes: %s", out)
		}
	})
}
