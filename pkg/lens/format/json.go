package format

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

// JSONFormatter renders a value as JSON with a documented, stable shape
// suitable for piping into jq.
//
// The output contract is:
//
//	{
//	  "type":  "<XDR type name>",
//	  "value": <the decoded tree>
//	}
//
// Within "value", struct nodes become JSON objects keyed by field name, list
// nodes become JSON arrays, and scalars become JSON strings, numbers or
// booleans. Absent union arms and nil optionals are omitted entirely rather
// than emitted as null, which is what keeps the output readable.
//
// When annotations are enabled, a scalar that carries an enricher note is
// emitted as {"value": <raw>, "display": "<note>"} instead of a bare scalar.
// Annotations are off by default so that the common case stays easy to
// address with jq.
type JSONFormatter struct {
	indent   string
	annotate bool
}

// JSONOption configures a JSONFormatter.
type JSONOption func(*JSONFormatter)

// WithIndent sets the indentation string. An empty string emits compact JSON
// on a single line.
func WithIndent(indent string) JSONOption {
	return func(j *JSONFormatter) { j.indent = indent }
}

// WithAnnotations includes enricher notes alongside raw values.
func WithAnnotations(on bool) JSONOption {
	return func(j *JSONFormatter) { j.annotate = on }
}

// NewJSONFormatter returns a JSONFormatter indented with two spaces.
func NewJSONFormatter(opts ...JSONOption) *JSONFormatter {
	j := &JSONFormatter{indent: "  "}
	for _, o := range opts {
		o(j)
	}
	return j
}

// document is the stable top-level JSON shape.
type document struct {
	Type  string `json:"type"`
	Value any    `json:"value"`
}

// Format implements Formatter.
func (j *JSONFormatter) Format(w io.Writer, v *lens.Value) error {
	if v == nil || v.Node == nil {
		return fmt.Errorf("format: nil value")
	}
	doc := document{Type: v.Type, Value: j.convert(v.Node)}

	enc := json.NewEncoder(w)
	if j.indent != "" {
		enc.SetIndent("", j.indent)
	}
	// XDR carries opaque binary rendered as hex strings; HTML escaping would
	// corrupt nothing but makes the output noisier for no benefit.
	enc.SetEscapeHTML(false)
	if err := enc.Encode(doc); err != nil {
		return fmt.Errorf("format: encode json: %w", err)
	}
	return nil
}

// convert turns a Node tree into plain Go values that encoding/json can emit.
//
// Struct nodes use json.RawMessage-free ordered emission via a map; Go's JSON
// encoder sorts map keys, which gives deterministic output — important because
// this format is meant to be diffed and asserted on in CI.
func (j *JSONFormatter) convert(n *lens.Node) any {
	if n == nil {
		return nil
	}
	switch n.Kind {
	case lens.KindList:
		out := make([]any, 0, len(n.Children))
		for _, c := range n.Children {
			out = append(out, j.convert(c))
		}
		return out

	case lens.KindStruct:
		out := make(map[string]any, len(n.Children))
		for _, c := range n.Children {
			key := c.Name
			if key == "" {
				continue
			}
			out[key] = j.convert(c)
		}
		return out

	default:
		if j.annotate && n.Note != "" {
			return map[string]any{"value": n.Value, "display": n.Note}
		}
		return n.Value
	}
}
