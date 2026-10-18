package cli

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

type encodeOptions struct {
	targetType string
	strict     bool
	scval      bool
	envelope   bool
}

func newEncodeCmd(g *globalFlags, stdout io.Writer, stdin io.Reader) *cobra.Command {
	opts := &encodeOptions{}

	cmd := &cobra.Command{
		Use:   "encode [JSON]",
		Short: "Encode simplified JSON templates into base64 XDR",
		Long: `Encode simplified JSON into base64-encoded XDR offline.

Supported targets:
  - TransactionEnvelope: full envelopes from simplified templates
  - ScVal: Soroban values from intuitive JSON literals

The input can be passed as a positional argument, read from a file with
--file / -f, or piped in via standard input.`,
		Example: `  # Encode a transaction envelope from a JSON template
  lens encode --file tx.json

  # Strict validation of transaction JSON schema
  lens encode --strict --file tx.json

  # Construct an ScVal from an intuitive JSON literal
  lens encode --scval '{"symbol": "hello"}'

  # Encode and pipe directly to lens decode for verification
  lens encode --file tx.json | lens decode`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			arg := ""
			if len(args) > 0 {
				arg = args[0]
			}
			raw, err := resolveInput(arg, g.file, stdin)
			if err != nil {
				return err
			}
			jsonBytes := []byte(strings.TrimSpace(raw))
			if len(jsonBytes) == 0 {
				return errors.New("empty JSON input")
			}

			// Determine target type
			target := opts.targetType
			if opts.scval {
				target = "ScVal"
			} else if opts.envelope {
				target = "TransactionEnvelope"
			}

			if target == "" {
				target = detectEncodeType(jsonBytes)
			}

			switch strings.ToLower(target) {
			case "scval":
				if opts.strict {
					if err := lens.ValidateJSON(jsonBytes, lens.StrictScValSpecSchema()); err != nil {
						return fmt.Errorf("schema validation: %w", err)
					}
				}
				scVal, err := lens.ParseScValJSON(jsonBytes)
				if err != nil {
					return fmt.Errorf("encode scval: %w", err)
				}
				bin, err := scVal.MarshalBinary()
				if err != nil {
					return fmt.Errorf("marshal scval binary: %w", err)
				}
				_, err = fmt.Fprintln(stdout, base64.StdEncoding.EncodeToString(bin))
				return err

			case "transactionenvelope", "tx", "envelope":
				if opts.strict {
					env, err := lens.BuildTransactionEnvelopeFromJSONStrict(jsonBytes)
					if err != nil {
						return fmt.Errorf("strict encode transaction: %w", err)
					}
					bin, err := env.MarshalBinary()
					if err != nil {
						return fmt.Errorf("marshal envelope binary: %w", err)
					}
					_, err = fmt.Fprintln(stdout, base64.StdEncoding.EncodeToString(bin))
					return err
				}

				env, err := lens.BuildTransactionEnvelopeFromJSON(jsonBytes)
				if err != nil {
					return fmt.Errorf("encode transaction: %w", err)
				}
				bin, err := env.MarshalBinary()
				if err != nil {
					return fmt.Errorf("marshal envelope binary: %w", err)
				}
				_, err = fmt.Fprintln(stdout, base64.StdEncoding.EncodeToString(bin))
				return err

			default:
				return fmt.Errorf("unsupported target encode type %q", target)
			}
		},
	}

	cmd.Flags().StringVarP(&opts.targetType, "type", "t", "",
		"target XDR type (TransactionEnvelope, ScVal)")
	cmd.Flags().BoolVar(&opts.strict, "strict", false,
		"validate JSON input against strict schema definitions before encoding")
	cmd.Flags().BoolVar(&opts.scval, "scval", false,
		"shortcut to encode input as ScVal")
	cmd.Flags().BoolVar(&opts.envelope, "tx", false,
		"shortcut to encode input as TransactionEnvelope")
	mustRegisterFlagCompletionFunc(cmd, "type", typeNameCompletion)

	return cmd
}

func detectEncodeType(data []byte) string {
	var m map[string]any
	if err := json.Unmarshal(data, &m); err == nil {
		if _, ok := m["source_account"]; ok {
			return "TransactionEnvelope"
		}
		if _, ok := m["sourceAccount"]; ok {
			return "TransactionEnvelope"
		}
		if _, ok := m["operations"]; ok {
			return "TransactionEnvelope"
		}
		if _, ok := m["fee_bump"]; ok {
			return "TransactionEnvelope"
		}
	}
	return "ScVal"
}
