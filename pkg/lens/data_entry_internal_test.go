package lens

import (
	"reflect"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"
)

func TestEnrichDataValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		value    xdr.DataValue
		wantNote string
		wantRaw  any
	}{
		{"empty value", xdr.DataValue{}, "empty", ""},
		{"text value", xdr.DataValue("hello"), "UTF-8 text, 5 byte(s)", "hello"},
		{"binary value", xdr.DataValue{0x00, 0xff}, "binary, 2 byte(s)", "00ff"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			note, raw, ok := enrichDataValue(reflect.ValueOf(tc.value))
			if !ok || note != tc.wantNote || raw != tc.wantRaw {
				t.Errorf("enrichDataValue(%v) = (%q, %v, %v), want (%q, %v, true)",
					tc.value, note, raw, ok, tc.wantNote, tc.wantRaw)
			}
		})
	}
}

func TestEnrichDataValueIgnoresOtherTypes(t *testing.T) {
	t.Parallel()

	if _, _, ok := enrichDataValue(reflect.ValueOf(xdr.String64("x"))); ok {
		t.Error("enrichDataValue(String64) ok = true, want false")
	}
}

// TestEnrichDataValueReachesTheWalker pins the type-based registration: the
// payload must be rendered through the enricher, not as a byte list.
func TestEnrichDataValueReachesTheWalker(t *testing.T) {
	t.Parallel()

	entry := xdr.DataEntry{DataName: "k", DataValue: xdr.DataValue("v")}
	node := buildNode("DataEntry", reflect.ValueOf(entry), 0)
	if node == nil {
		t.Fatal("buildNode(DataEntry) returned nil")
	}
	value := node.Child("DataValue")
	if value == nil || value.Note != "UTF-8 text, 1 byte(s)" || value.Value != "v" {
		t.Errorf("DataValue = %v, want the text rendering", value)
	}
}

// TestManageDataOpDataValueUTF8Detection pins the ManageData path from issue
// #111: ManageDataOp.DataValue is a *DataValue that the walker unwraps, and
// the registered DataValue enricher must fire for it exactly as it does for a
// standalone DataEntry. Printable UTF-8 becomes the text itself; arbitrary
// binary stays hex.
func TestManageDataOpDataValueUTF8Detection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		value     xdr.DataValue
		wantValue any
		wantNote  string
	}{
		{
			name:      "printable URL",
			value:     xdr.DataValue("https://example.com/meta"),
			wantValue: "https://example.com/meta",
			wantNote:  "UTF-8 text, 24 byte(s)",
		},
		{
			name:      "printable IPFS-style hash",
			value:     xdr.DataValue("QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG"),
			wantValue: "QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG",
			wantNote:  "UTF-8 text, 46 byte(s)",
		},
		{
			name:      "short ascii metadata",
			value:     xdr.DataValue("hello"),
			wantValue: "hello",
			wantNote:  "UTF-8 text, 5 byte(s)",
		},
		{
			name:      "arbitrary binary",
			value:     xdr.DataValue{0x00, 0xff, 0xfe, 0x01},
			wantValue: "00fffe01",
			wantNote:  "binary, 4 byte(s)",
		},
		{
			name:      "png signature",
			value:     xdr.DataValue{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a},
			wantValue: "89504e470d0a1a0a",
			wantNote:  "binary, 8 byte(s)",
		},
		{
			name:      "embedded newline is not one-line text",
			value:     xdr.DataValue("a\nb"),
			wantValue: "610a62",
			wantNote:  "binary, 3 byte(s)",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			body := xdr.OperationBody{
				Type: xdr.OperationTypeManageData,
				ManageDataOp: &xdr.ManageDataOp{
					DataName:  xdr.String64("key"),
					DataValue: ptrToDataValue(tc.value),
				},
			}
			node := buildNode("Body", reflect.ValueOf(body), 0)
			if node == nil {
				t.Fatal("buildNode(OperationBody) returned nil")
			}
			value := node.Find("ManageDataOp.DataValue")
			if value == nil {
				t.Fatal("ManageDataOp.DataValue node is missing")
			}
			if value.Value != tc.wantValue {
				t.Errorf("DataValue Value = %v, want %v", value.Value, tc.wantValue)
			}
			if value.Note != tc.wantNote {
				t.Errorf("DataValue Note = %q, want %q", value.Note, tc.wantNote)
			}
		})
	}
}

// TestManageDataOpNilDataValueIsPruned covers the data-delete shape: a nil
// DataValue is an absent optional field and must not render at all.
func TestManageDataOpNilDataValueIsPruned(t *testing.T) {
	t.Parallel()

	body := xdr.OperationBody{
		Type: xdr.OperationTypeManageData,
		ManageDataOp: &xdr.ManageDataOp{
			DataName:  xdr.String64("removed"),
			DataValue: nil,
		},
	}
	node := buildNode("Body", reflect.ValueOf(body), 0)
	if node == nil {
		t.Fatal("buildNode(OperationBody) returned nil")
	}
	if dv := node.Find("ManageDataOp.DataValue"); dv != nil {
		t.Errorf("ManageDataOp.DataValue = %v, want nil (pruned absent optional)", dv)
	}
}

func ptrToDataValue(v xdr.DataValue) *xdr.DataValue { return &v }
