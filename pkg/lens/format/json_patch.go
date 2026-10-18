package format

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

// JSONPatchFormatter renders a structural diff as an RFC 6902 JSON Patch: a
// JSON array of add, remove and replace operations addressed by RFC 6901 JSON
// Pointers.
//
// The paths are relative to the decoded value itself — the object nested under
// "value" in `lens decode --json` output — so a patch can be applied to that
// value with any compliant JSON Patch implementation. Added and modified
// changes become "add" and "replace" operations carrying the right-hand value;
// removed changes become "remove" operations.
//
// When the two decoded values are supplied with WithJSONPatchSources, the
// operation value is the full JSON subtree taken from the tree, which makes the
// patch applicable even for struct and list changes. Without them the value is
// the scalar recorded on the change, which is exact for leaf changes and a
// descriptive placeholder for container changes. Either way the output is
// deterministic and made offline.
type JSONPatchFormatter struct {
	indent string
	left   *lens.Value
	right  *lens.Value
}

// JSONPatchOption configures a JSONPatchFormatter.
type JSONPatchOption func(*JSONPatchFormatter)

// WithJSONPatchIndent sets the indentation string. An empty string emits the
// patch on a single line.
func WithJSONPatchIndent(indent string) JSONPatchOption {
	return func(f *JSONPatchFormatter) { f.indent = indent }
}

// WithJSONPatchSources supplies the two decoded values the diff was computed
// from, so the formatter can emit complete JSON subtrees for changed nodes.
func WithJSONPatchSources(left, right *lens.Value) JSONPatchOption {
	return func(f *JSONPatchFormatter) {
		f.left = left
		f.right = right
	}
}

// NewJSONPatchFormatter returns a JSONPatchFormatter indented with two spaces.
func NewJSONPatchFormatter(opts ...JSONPatchOption) *JSONPatchFormatter {
	f := &JSONPatchFormatter{indent: "  "}
	for _, o := range opts {
		o(f)
	}
	return f
}

// JSONPatchOperation is one RFC 6902 operation in the emitted array.
type JSONPatchOperation struct {
	// Op is one of "add", "remove" or "replace".
	Op string `json:"op"`
	// Path is the RFC 6901 JSON Pointer locating the change.
	Path string `json:"path"`
	// Value is the right-hand value for add and replace operations. It is
	// omitted for remove.
	Value any `json:"value,omitempty"`
}

// Format implements the diff rendering surface. It writes the RFC 6902 patch
// array for d to w.
func (f *JSONPatchFormatter) Format(w io.Writer, d *lens.DiffResult) error {
	if d == nil {
		return fmt.Errorf("format: nil diff")
	}
	ops := make([]JSONPatchOperation, 0, len(d.Changes))
	for _, change := range d.Changes {
		ops = append(ops, f.operation(change))
	}

	enc := json.NewEncoder(w)
	if f.indent != "" {
		enc.SetIndent("", f.indent)
	}
	enc.SetEscapeHTML(false)
	if err := enc.Encode(ops); err != nil {
		return fmt.Errorf("format: encode json patch: %w", err)
	}
	return nil
}

// Operations returns the patch operations for a diff as a slice, which is
// useful to callers that want to inspect or post-process them.
func (f *JSONPatchFormatter) Operations(d *lens.DiffResult) []JSONPatchOperation {
	if d == nil {
		return []JSONPatchOperation{}
	}
	ops := make([]JSONPatchOperation, 0, len(d.Changes))
	for _, change := range d.Changes {
		ops = append(ops, f.operation(change))
	}
	return ops
}

func (f *JSONPatchFormatter) operation(change lens.Change) JSONPatchOperation {
	pointer := jsonPointer(change.Path)
	switch change.Op {
	case lens.ChangeAdded:
		return JSONPatchOperation{Op: "add", Path: pointer, Value: f.value(f.right, change.Path, change.After)}
	case lens.ChangeRemoved:
		return JSONPatchOperation{Op: "remove", Path: pointer}
	default:
		return JSONPatchOperation{Op: "replace", Path: pointer, Value: f.value(f.right, change.Path, change.After)}
	}
}

// value resolves the JSON value for a change path from the right-hand tree,
// falling back to the scalar the change already carries.
func (f *JSONPatchFormatter) value(source *lens.Value, path string, fallback any) any {
	if source != nil && source.Node != nil {
		if node := source.Node.Find(path); node != nil {
			return convertNode(node, false)
		}
	}
	return fallback
}

// jsonPointer converts a lens change path such as "V1.Tx.Operations[0].Body"
// into an RFC 6901 JSON Pointer, returning "" for the document root.
func jsonPointer(changePath string) string {
	segments := pointerSegments(changePath)
	if len(segments) == 0 {
		return ""
	}
	var b strings.Builder
	for _, segment := range segments {
		b.WriteByte('/')
		b.WriteString(escapePointerToken(segment))
	}
	return b.String()
}

// escapePointerToken applies the RFC 6901 escaping rules.
func escapePointerToken(token string) string {
	if !strings.ContainsAny(token, "~/") {
		return token
	}
	token = strings.ReplaceAll(token, "~", "~0")
	return strings.ReplaceAll(token, "/", "~1")
}

// pointerSegments splits a change path into JSON Pointer tokens, rendering
// bracketed indexes as their own token: "Ops[0].Code" becomes ["Ops", "0",
// "Code"].
func pointerSegments(changePath string) []string {
	var segments []string
	for _, part := range strings.Split(changePath, ".") {
		for part != "" {
			open := strings.IndexByte(part, '[')
			if open < 0 {
				segments = append(segments, part)
				break
			}
			if open > 0 {
				segments = append(segments, part[:open])
			}
			closing := strings.IndexByte(part[open:], ']')
			if closing < 0 {
				segments = append(segments, part[open:])
				break
			}
			closing += open
			segments = append(segments, part[open+1:closing])
			part = part[closing+1:]
		}
	}
	return segments
}
