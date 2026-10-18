package lens

import (
	"errors"
	"strings"
	"testing"
)

func TestReadPipeNormalizesWhitespace(t *testing.T) {
	const payload = "AABlZg==\n\r"

	got, err := ReadPipe(strings.NewReader(payload))
	if err != nil {
		t.Fatalf("ReadPipe() error = %v", err)
	}
	if got != "AABlZg==" {
		t.Fatalf("ReadPipe() = %q, want %q", got, "AABlZg==")
	}
}

func TestReadPipeRejectsEmptyInput(t *testing.T) {
	_, err := ReadPipe(strings.NewReader(" \n\t\r \n"))
	if !errors.Is(err, ErrEmptyInput) {
		t.Fatalf("ReadPipe() error = %v, want ErrEmptyInput", err)
	}
}

func TestReadPipeRecordsSplitsMultiplePayloads(t *testing.T) {
	input := "AAAAAA==\n\nAAAAAA==\n\tAAAAAA==\r\n"

	got, err := ReadPipeRecords(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ReadPipeRecords() error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("ReadPipeRecords() len = %d, want 3", len(got))
	}
	for i, want := range []string{"AAAAAA==", "AAAAAA==", "AAAAAA=="} {
		if got[i] != want {
			t.Fatalf("ReadPipeRecords()[%d] = %q, want %q", i, got[i], want)
		}
	}
}

func TestReadStdinPipeMatchesReadPipe(t *testing.T) {
	const payload = "\n  AAAA\n"

	want, err := ReadPipe(strings.NewReader(payload))
	if err != nil {
		t.Fatalf("ReadPipe() error = %v", err)
	}
	got, err := ReadStdinPipe(strings.NewReader(payload))
	if err != nil {
		t.Fatalf("ReadStdinPipe() error = %v", err)
	}
	if got != want {
		t.Fatalf("ReadStdinPipe() = %q, want %q", got, want)
	}
}
