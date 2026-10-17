package cli

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens/format"
)

func newDecodeCmd(g *globalFlags, stdout io.Writer, stdin io.Reader) *cobra.Command {
	var (
		asJSON      bool
		asYAML      bool
		isHex       bool
		isMulti     bool
		inspectWASM bool
		asSummary   bool
		asTxRep     bool
		asCSV       bool
		interactive bool
		specPath    string
		formatName  string
		network     string
		typeName    string
		maxDepth    int
		showTypes   bool
		annotate    bool
		listCands   bool
		compactOut  bool
		stream      bool
		canonical   bool
	)

	cmd := &cobra.Command{
		Use:   "decode [base64]",
		Short: "Decode XDR and print it as a tree, JSON, summary, txrep, or CSV",
		Long: `Decode base64-encoded XDR.

The XDR type is detected automatically by trying each known type and keeping
the ones that decode cleanly. Detection is reliable for transaction-sized
payloads, but short values are genuinely ambiguous — an empty Memo and a
native Asset are both "AAAAAA==" — so use --type when you need certainty and
--candidates to see what else matched.

Use --summary for single-line output suited for grep and shell pipelines.
Use --txrep for SEP-0011 human-readable representation of transaction envelopes.
Use --csv for a flat, RFC 4180 row of the high-level transaction fields, which
is what batch exports concatenate (see pkg/lens/format for the batch API).
Use --spec to supply a contract WASM or spec file for decoded Soroban contract fields.
Use --interactive (or 'lens explore') for an interactive terminal tree viewer.`,
		Example: `  lens decode "$XDR"
  lens decode --hex "$HEX"
  lens decode --multi -f envelopes.txt
  lens decode --yaml "$XDR"
  lens decode --wasm "$WASM_OR_ENVELOPE"
  lens decode --json "$XDR" | jq '.value.V1.Tx.Fee'
  lens decode --summary "$XDR"
  lens decode --txrep "$XDR"
  lens decode --csv "$XDR" >> envelopes.csv
  lens decode --spec contract.wasm "$XDR"
  lens decode --interactive "$XDR"
  lens decode --type TransactionResult "$XDR"
  lens decode --candidates "AAAAAA=="
  lens decode --stream --file records.xdr
  lens decode --canonical "$XDR"
  echo "$XDR" | lens decode --depth 3`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if stream {
				return runStream(stdout, g, firstArg(args), stdin, typeName, asJSON, compactOut, annotate, maxDepth, showTypes, canonical)
			}

			if isMulti {
				var r io.Reader
				if g.file != "" {
					f, err := os.Open(g.file)
					if err != nil {
						return fmt.Errorf("opening %s: %w", g.file, err)
					}
					defer f.Close()
					r = f
				} else if firstArg(args) != "" && firstArg(args) != stdinMarker {
					r = strings.NewReader(firstArg(args))
				} else {
					r = stdin
				}

				return runMultiStream(r, stdout, g, typeName, isHex, asJSON, asYAML, compactOut, annotate, maxDepth, showTypes)
			}

			payload, err := resolveInput(firstArg(args), g.file, stdin)
			if err != nil {
				return err
			}

			if isHex || strings.HasPrefix(payload, "0x") || strings.HasPrefix(payload, "0X") {
				b64, convErr := lens.HexToBase64(payload)
				if convErr == nil {
					payload = b64
				} else if isHex {
					return fmt.Errorf("decoding hex input: %w", convErr)
				}
			}

			if canonical {
				raw, decErr := base64.StdEncoding.DecodeString(payload)
				if decErr != nil {
					return fmt.Errorf("decoding base64: %w", decErr)
				}
				if typeName != "" {
					if err := lens.ValidateTypeCanonicalXDR(raw, typeName); err != nil {
						return fmt.Errorf("canonical validation failed: %w", err)
					}
				} else {
					if err := lens.ValidateCanonicalXDR(raw); err != nil {
						return fmt.Errorf("canonical validation failed: %w", err)
					}
				}
			}

			if listCands {
				return runCandidates(stdout, payload)
			}

			value, err := decodeValue(payload, typeName)
			if err != nil {
				if inspectWASM {
					// Attempt direct WASM decode from payload
					return runDirectWASM(stdout, payload, asJSON)
				}
				return err
			}

			if inspectWASM {
				return runInspectWASM(stdout, value, payload, asJSON)
			}

			if asYAML {
				yamlBytes, err := lens.ValueToYAML(value, annotate)
				if err != nil {
					return fmt.Errorf("formatting YAML: %w", err)
				}
				_, err = stdout.Write(yamlBytes)
				return err
			}

			if specPath != "" {
				spec, err := lens.LoadContractSpecFile(specPath)
				if err != nil {
					return fmt.Errorf("loading contract spec: %w", err)
				}
				spec.ApplyToValue(value)
			}

			if interactive {
				palette, err := paletteForOutput(stdout, g.color)
				if err != nil {
					return err
				}
				tui := NewTUITree(value, stdin, stdout, palette)
				return tui.Run()
			}

			if asSummary || formatName == "summary" {
				palette, err := paletteForOutput(stdout, g.color)
				if err != nil {
					return err
				}
				f := format.NewCompactFormatter(
					format.WithCompactPalette(palette),
				)
				return f.Format(stdout, value)
			}

			if asTxRep || formatName == "txrep" {
				palette, err := paletteForOutput(stdout, g.color)
				if err != nil {
					return err
				}
				f := format.NewTxRepFormatter(
					format.WithTxRepPalette(palette),
					format.WithTxRepNetwork(network),
				)
				return f.Format(stdout, value)
			}

			if asCSV || formatName == "csv" {
				return format.NewCSVFormatter().Format(stdout, value)
			}

			if asJSON || formatName == "json" {
				indent := "  "
				if compactOut {
					indent = ""
				}
				f := format.NewJSONFormatter(
					format.WithIndent(indent),
					format.WithAnnotations(annotate),
				)
				return f.Format(stdout, value)
			}

			palette, err := paletteForOutput(stdout, g.color)
			if err != nil {
				return err
			}
			f := format.NewTreeFormatter(
				format.WithPalette(palette),
				format.WithMaxDepth(maxDepth),
				format.WithTypeNames(showTypes),
			)
			return f.Format(stdout, value)
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "emit stable JSON instead of a tree")
	cmd.Flags().BoolVar(&asYAML, "yaml", false, "emit formatted YAML instead of a tree")
	cmd.Flags().BoolVar(&isHex, "hex", false, "parse input as hexadecimal XDR instead of base64")
	cmd.Flags().BoolVar(&isMulti, "multi", false, "decode multiple newline-separated XDR payloads")
	cmd.Flags().BoolVar(&inspectWASM, "wasm", false, "extract and inspect embedded WebAssembly bytecode")
	cmd.Flags().BoolVar(&compactOut, "compact", false, "with --json, emit a single line")
	cmd.Flags().BoolVar(&asSummary, "summary", false, "emit a terse single-line summary suitable for grep and pipelines")
	cmd.Flags().BoolVar(&asTxRep, "txrep", false, "emit SEP-0011 TxRep format for transaction envelopes")
	cmd.Flags().BoolVar(&asCSV, "csv", false, "emit a flat CSV row of the high-level transaction fields")
	cmd.Flags().BoolVarP(&interactive, "interactive", "i", false, "explore the decoded tree interactively")
	cmd.Flags().StringVar(&specPath, "spec", "", "path to contract WASM or ScSpecEntry file for spec-aware decoding")
	cmd.Flags().StringVar(&formatName, "format", "", `output format: "tree", "json", "summary", "txrep", or "csv"`)
	cmd.Flags().StringVar(&network, "network", "", "network passphrase for txrep formatting")
	cmd.Flags().StringVarP(&typeName, "type", "t", "",
		"decode as this XDR type instead of detecting it (see `lens types`)")
	cmd.Flags().IntVar(&maxDepth, "depth", 0, "limit tree depth (0 means unlimited)")
	cmd.Flags().BoolVar(&showTypes, "show-types", false, "annotate tree nodes with their XDR type")
	cmd.Flags().BoolVar(&listCands, "candidates", false,
		"list every XDR type the input decodes as, best first")
	cmd.Flags().BoolVar(&stream, "stream", false, "stream and decode framed XDR chunks (RFC 5531 or length-prefixed)")
	cmd.Flags().BoolVar(&canonical, "canonical", false, "verify that input adheres strictly to canonical XDR serialization")

	return cmd
}

func newExploreCmd(g *globalFlags, stdout io.Writer, stdin io.Reader) *cobra.Command {
	var (
		typeName string
		specPath string
	)

	cmd := &cobra.Command{
		Use:     "explore [base64]",
		Aliases: []string{"tui"},
		Short:   "Interactively explore an XDR structure as an expandable tree",
		Long: `Interactively explore an XDR structure in an expandable terminal tree viewer.

Use arrow keys or j/k to navigate, Enter or Space to toggle node expansion,
'e' to expand all, 'c' to collapse all, and 'q' to quit.`,
		Example: `  lens explore "$XDR"
  lens tui --file tx.env.txt
  lens explore --spec contract.wasm "$XDR"`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload, err := resolveInput(firstArg(args), g.file, stdin)
			if err != nil {
				return err
			}

			value, err := decodeValue(payload, typeName)
			if err != nil {
				return err
			}

			if specPath != "" {
				spec, err := lens.LoadContractSpecFile(specPath)
				if err != nil {
					return fmt.Errorf("loading contract spec: %w", err)
				}
				spec.ApplyToValue(value)
			}

			palette, err := paletteForOutput(stdout, g.color)
			if err != nil {
				return err
			}
			tui := NewTUITree(value, stdin, stdout, palette)
			return tui.Run()
		},
	}

	cmd.Flags().StringVarP(&typeName, "type", "t", "",
		"decode as this XDR type instead of detecting it")
	cmd.Flags().StringVar(&specPath, "spec", "",
		"path to contract WASM or ScSpecEntry file for spec-aware decoding")

	return cmd
}

func runStream(stdout io.Writer, g *globalFlags, arg string, stdin io.Reader, typeName string, asJSON, compactOut, annotate bool, maxDepth int, showTypes, canonical bool) error {
	var r io.Reader
	if g.file != "" {
		f, err := os.Open(g.file)
		if err != nil {
			return err
		}
		defer f.Close()
		r = f
	} else if arg != "" && arg != stdinMarker {
		r = strings.NewReader(arg)
	} else {
		r = stdin
	}

	parser := lens.NewStreamParser(r)
	for {
		raw, err := parser.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		if canonical {
			if typeName != "" {
				if err := lens.ValidateTypeCanonicalXDR(raw, typeName); err != nil {
					return fmt.Errorf("chunk [%d] canonical error: %w", parser.ChunkIndex()-1, err)
				}
			} else {
				if err := lens.ValidateCanonicalXDR(raw); err != nil {
					return fmt.Errorf("chunk [%d] canonical error: %w", parser.ChunkIndex()-1, err)
				}
			}
		}

		b64 := base64.StdEncoding.EncodeToString(raw)
		val, err := decodeValue(b64, typeName)
		if err != nil {
			return fmt.Errorf("chunk [%d]: %w", parser.ChunkIndex()-1, err)
		}

		if asJSON {
			indent := "  "
			if compactOut {
				indent = ""
			}
			f := format.NewJSONFormatter(
				format.WithIndent(indent),
				format.WithAnnotations(annotate),
			)
			if err := f.Format(stdout, val); err != nil {
				return err
			}
			continue
		}

		f := format.NewTreeFormatter(
			format.WithPalette(format.PaletteFor(stdout, g.color)),
			format.WithMaxDepth(maxDepth),
			format.WithTypeNames(showTypes),
		)
		if err := f.Format(stdout, val); err != nil {
			return err
		}
	}
}

// decodeValue decodes a payload, honouring an explicit type when given.
func decodeValue(payload, typeName string) (*lens.Value, error) {
	if typeName != "" {
		v, err := lens.DecodeAs(payload, typeName)
		if err != nil {
			return nil, err
		}
		return v, nil
	}
	v, err := lens.Decode(payload)
	if err != nil {
		return nil, err
	}
	return v, nil
}

// runMultiStream decodes and formats multiple concatenated records.
func runMultiStream(r io.Reader, stdout io.Writer, g *globalFlags, typeName string, isHex, asJSON, asYAML, compactOut, annotate bool, maxDepth int, showTypes bool) error {
	opts := []lens.MultiStreamOption{
		lens.WithMultiStreamHex(isHex),
		lens.WithMultiStreamStopOnError(true),
	}
	if typeName != "" {
		opts = append(opts, lens.WithMultiStreamType(typeName))
	}

	decoder := lens.NewMultiStreamDecoder(r, opts...)
	records, err := decoder.DecodeAll()
	if err != nil {
		return err
	}

	if asYAML {
		for i, rec := range records {
			if i > 0 {
				if _, err := fmt.Fprintln(stdout, "---"); err != nil {
					return err
				}
			}
			y, err := lens.ValueToYAML(rec.Value, annotate)
			if err != nil {
				return err
			}
			if _, err := stdout.Write(y); err != nil {
				return err
			}
		}
		return nil
	}

	if asJSON {
		if compactOut {
			// NDJSON emission
			for _, rec := range records {
				var buf bytes.Buffer
				f := format.NewJSONFormatter(format.WithIndent(""), format.WithAnnotations(annotate))
				if err := f.Format(&buf, rec.Value); err != nil {
					return err
				}
				if _, err := fmt.Fprintf(stdout, "%s\n", strings.TrimSpace(buf.String())); err != nil {
					return err
				}
			}
			return nil
		}

		type multiJSONItem struct {
			Index      int    `json:"index"`
			LineNumber int    `json:"line_number"`
			Type       string `json:"type"`
			Value      any    `json:"value"`
		}
		items := make([]multiJSONItem, len(records))
		for i, rec := range records {
			items[i] = multiJSONItem{
				Index:      rec.Index,
				LineNumber: rec.LineNumber,
				Type:       rec.Value.Type,
				Value:      lens.NodeToGeneric(rec.Value.Node, annotate),
			}
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(items)
	}

	tf := format.NewTreeFormatter(
		format.WithPalette(format.PaletteFor(stdout, g.color)),
		format.WithMaxDepth(maxDepth),
		format.WithTypeNames(showTypes),
	)

	for _, rec := range records {
		if _, err := fmt.Fprintf(stdout, "=== Record %d (line %d) Type: %s ===\n", rec.Index, rec.LineNumber, rec.Value.Type); err != nil {
			return err
		}
		if err := tf.Format(stdout, rec.Value); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(stdout); err != nil {
			return err
		}
	}

	return nil
}

// runInspectWASM extracts and disassembles WebAssembly bytecode.
func runInspectWASM(stdout io.Writer, val *lens.Value, payload string, asJSON bool) error {
	wasmBytes, err := lens.ExtractWASMFromXDR(val)
	if err != nil {
		return runDirectWASM(stdout, payload, asJSON)
	}

	mod, err := lens.ParseWASM(wasmBytes)
	if err != nil {
		return fmt.Errorf("parsing extracted WebAssembly: %w", err)
	}

	if asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(mod)
	}

	_, err = fmt.Fprint(stdout, mod.Inspect())
	return err
}

// runDirectWASM attempts to parse a raw/hex/base64 payload directly as WASM.
func runDirectWASM(stdout io.Writer, payload string, asJSON bool) error {
	var wasmBytes []byte
	if lens.IsHexString(payload) {
		b, err := lens.HexToBytes(payload)
		if err == nil {
			wasmBytes = b
		}
	}
	if len(wasmBytes) == 0 {
		b, err := base64.StdEncoding.DecodeString(payload)
		if err == nil {
			wasmBytes = b
		} else {
			wasmBytes = []byte(payload)
		}
	}

	mod, err := lens.ParseWASM(wasmBytes)
	if err != nil {
		return fmt.Errorf("decoding WebAssembly payload: %w", err)
	}

	if asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(mod)
	}

	_, err = fmt.Fprint(stdout, mod.Inspect())
	return err
}

// runCandidates prints every type the payload decodes as, which is the honest
// answer for ambiguous input.
func runCandidates(stdout io.Writer, payload string) error {
	cands, err := lens.Detect(payload)
	if err != nil {
		return err
	}
	for i, c := range cands {
		marker := " "
		if i == 0 {
			marker = "*"
		}
		if _, err := fmt.Fprintf(stdout, "%s %s\n", marker, c.Type); err != nil {
			return fmt.Errorf("writing candidates: %w", err)
		}
	}
	if len(cands) > 1 {
		if _, err := fmt.Fprintf(stdout,
			"\n%d types matched; the starred one is used by default. Use --type to choose.\n",
			len(cands)); err != nil {
			return fmt.Errorf("writing candidates: %w", err)
		}
	}
	return nil
}

func newTypesCmd(stdout io.Writer) *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "types",
		Short: "List the XDR types lens can decode",
		Long: `List every XDR type lens can decode.

With --json the command emits the introspection index: the name, category,
one-line description, detection priority and minimum encoded size of each type.
The shape is stable and intended for scripts.`,
		Example: `  lens types
  lens types --json | jq '.[] | select(.category == "overlay") | .name'`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if asJSON {
				enc := json.NewEncoder(stdout)
				enc.SetIndent("", "  ")
				if err := enc.Encode(lens.RegistryIndex()); err != nil {
					return fmt.Errorf("encoding type index: %w", err)
				}
				return nil
			}
			_, err := fmt.Fprintln(stdout, strings.Join(lens.TypeNames(), "\n"))
			return err
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "emit the introspection index as JSON")

	return cmd
}

func firstArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}
