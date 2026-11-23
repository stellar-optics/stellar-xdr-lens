package cli

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

const (
	convertBase64 = "base64"
	convertHex    = "hex"
	convertRaw    = "raw"
)

func newConvertCmd(g *globalFlags, stdout io.Writer, stdin io.Reader) *cobra.Command {
	var (
		from     string
		to       string
		toHex    bool
		toBase64 bool
	)

	cmd := &cobra.Command{
		Use:   "convert [input...]",
		Short: "Convert XDR payloads between base64, hex, and raw binary",
		Long: `Convert a payload between the representations Stellar tooling uses.

Base64 is what Horizon and Soroban RPC return, hex is what contract bytecode
and hashes are written in, and raw binary is what files and pipes carry.
Conversion never unmarshals the XDR, so it works on arbitrary bytes and cannot
reject a structurally invalid value.

Input is read from the positional arguments, --file, or stdin in that order.
Whitespace is ignored in base64 and hex input, so multi-line pastes work.
Malformed input is reported with its line, column and byte offset.`,
		Example: `  lens convert "$XDR"
  lens convert --from hex --to base64 "$HEX"
  lens convert --hex "$XDR"
  lens convert --base64 "$HEX"
  lens convert --from base64 --to raw "$XDR" > payload.bin
  cat payload.bin | lens convert --from raw --hex`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if toHex && toBase64 {
				return fmt.Errorf("%w: --hex and --base64 are mutually exclusive", lens.ErrInvalidArgument)
			}
			if toHex {
				to = convertHex
			}
			if toBase64 {
				to = convertBase64
			}

			fromFormat, err := normaliseConvertFormat(from)
			if err != nil {
				return err
			}
			toFormat, err := normaliseConvertFormat(to)
			if err != nil {
				return err
			}

			data, err := readConvertInput(g, args, stdin)
			if err != nil {
				return err
			}
			converted, err := convertPayload(data, fromFormat, toFormat)
			if err != nil {
				return err
			}

			if _, err := stdout.Write(converted); err != nil {
				return fmt.Errorf("writing converted payload: %w", err)
			}
			if toFormat == convertRaw {
				return nil
			}
			if _, err := fmt.Fprintln(stdout); err != nil {
				return fmt.Errorf("writing converted payload: %w", err)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&from, "from", convertBase64, `input format: "base64", "hex", or "raw"`)
	cmd.Flags().StringVar(&to, "to", convertHex, `output format: "base64", "hex", or "raw"`)
	cmd.Flags().BoolVar(&toHex, "hex", false, "shortcut for --to hex")
	cmd.Flags().BoolVar(&toBase64, "base64", false, "shortcut for --to base64")

	return cmd
}

// normaliseConvertFormat canonicalises the user-facing format names.
func normaliseConvertFormat(name string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case convertBase64:
		return convertBase64, nil
	case convertHex:
		return convertHex, nil
	case convertRaw, "binary", "bin":
		return convertRaw, nil
	default:
		return "", fmt.Errorf("%w: unknown conversion format %q (want base64, hex, or raw)",
			lens.ErrInvalidArgument, name)
	}
}

// readConvertInput resolves the payload from --file, positional arguments, or
// stdin. Raw input is returned untouched; text inputs keep their whitespace
// until convertPayload strips it.
func readConvertInput(g *globalFlags, args []string, stdin io.Reader) ([]byte, error) {
	if g.file != "" {
		data, err := os.ReadFile(g.file)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", g.file, err)
		}
		return data, nil
	}
	if len(args) == 1 && args[0] == stdinMarker {
		args = nil
	}
	if len(args) > 0 {
		return []byte(strings.Join(args, "\n")), nil
	}
	data, err := io.ReadAll(stdin)
	if err != nil {
		return nil, fmt.Errorf("reading stdin: %w", err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errNoInput
	}
	return data, nil
}

// convertPayload performs the conversion without unmarshalling the XDR.
func convertPayload(data []byte, from, to string) ([]byte, error) {
	var raw []byte

	switch from {
	case convertBase64:
		clean, positions := stripConvertWhitespace(data)
		decoded, err := base64.StdEncoding.DecodeString(string(clean))
		if err != nil {
			return nil, describeConvertError("base64", err, clean, positions, data, lens.ErrInvalidBase64)
		}
		raw = decoded
	case convertHex:
		clean, positions := stripConvertWhitespace(data)
		clean, positions = trimHexPrefix(clean, positions)
		decoded, err := decodeHexWithOffset(clean, positions, data)
		if err != nil {
			return nil, err
		}
		raw = decoded
	case convertRaw:
		raw = data
	default:
		return nil, fmt.Errorf("%w: unknown input format %q", lens.ErrInvalidArgument, from)
	}

	switch to {
	case convertBase64:
		out := make([]byte, base64.StdEncoding.EncodedLen(len(raw)))
		base64.StdEncoding.Encode(out, raw)
		return out, nil
	case convertHex:
		out := make([]byte, hex.EncodedLen(len(raw)))
		hex.Encode(out, raw)
		return out, nil
	case convertRaw:
		return raw, nil
	default:
		return nil, fmt.Errorf("%w: unknown output format %q", lens.ErrInvalidArgument, to)
	}
}

// stripConvertWhitespace removes spaces, tabs and newlines while remembering
// each retained byte's position in the original input, so errors can report a
// line and column rather than an offset into a rewritten string.
func stripConvertWhitespace(data []byte) ([]byte, []int) {
	clean := make([]byte, 0, len(data))
	positions := make([]int, 0, len(data))
	for i, b := range data {
		switch b {
		case ' ', '\t', '\n', '\r':
			continue
		default:
			clean = append(clean, b)
			positions = append(positions, i)
		}
	}
	return clean, positions
}

// trimHexPrefix accepts the 0x/0X prefix developers habitually leave on hex.
func trimHexPrefix(clean []byte, positions []int) ([]byte, []int) {
	if len(clean) >= 2 && clean[0] == '0' && (clean[1] == 'x' || clean[1] == 'X') {
		return clean[2:], positions[2:]
	}
	return clean, positions
}

// decodeHexWithOffset validates every digit before decoding, so the reported
// offset points at the first malformed byte rather than at the whole string.
func decodeHexWithOffset(clean []byte, positions []int, original []byte) ([]byte, error) {
	for i, b := range clean {
		if !isHexDigit(b) {
			return nil, fmt.Errorf("%w: illegal character %q at %s",
				lens.ErrInvalidHex, string(b), describeConvertOffset(original, positions[i]))
		}
	}
	if len(clean)%2 != 0 {
		return nil, fmt.Errorf("%w: odd number of hex digits at %s",
			lens.ErrOddLengthHex, describeConvertOffset(original, positions[len(positions)-1]))
	}

	out := make([]byte, hex.DecodedLen(len(clean)))
	if _, err := hex.Decode(out, clean); err != nil {
		return nil, fmt.Errorf("%w: %w", lens.ErrInvalidHex, err)
	}
	return out, nil
}

// describeConvertError turns a base64 decoder error into a line/column report.
func describeConvertError(format string, err error, clean []byte, positions []int, original []byte, sentinel error) error {
	var corrupt base64.CorruptInputError
	if errors.As(err, &corrupt) {
		idx := int(corrupt)
		if idx < 0 {
			idx = 0
		}
		at := describeConvertOffset(original, positions[min(idx, len(positions)-1)])
		if idx < len(clean) && !isBase64Char(clean[idx]) {
			return fmt.Errorf("%w: illegal character %q in %s at %s",
				sentinel, string(clean[idx]), format, at)
		}
		return fmt.Errorf("%w: invalid %s padding at %s", sentinel, format, at)
	}
	return fmt.Errorf("%w: invalid %s: %w", sentinel, format, err)
}

// describeConvertOffset reports a one-based line and column plus the zero-based
// byte offset in the original input.
func describeConvertOffset(original []byte, index int) string {
	line, column := 1, 1
	for i := 0; i < index && i < len(original); i++ {
		if original[i] == '\n' {
			line++
			column = 1
		} else {
			column++
		}
	}
	return fmt.Sprintf("line %d, column %d (byte offset %d)", line, column, index)
}

func isBase64Char(c byte) bool {
	switch {
	case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		return true
	case c == '+', c == '/', c == '=':
		return true
	default:
		return false
	}
}

func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}
