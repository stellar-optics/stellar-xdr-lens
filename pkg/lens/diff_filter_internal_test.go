package lens

import (
	"reflect"
	"testing"
)

func TestPathSegments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"FeeCharged", []string{"FeeCharged"}},
		{"Result.Code", []string{"Result", "Code"}},
		{"Operations[0].Body", []string{"Operations", "0", "Body"}},
		{"Ops[0][1]", []string{"Ops", "0", "1"}},
		{"[0].Code", []string{"0", "Code"}},
		{"Ops[*]", []string{"Ops", "*"}},
		{"A.[bad", []string{"A", "[bad"}},
	}

	for _, tc := range tests {
		if got := pathSegments(tc.in); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("pathSegments(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestMatchSegments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		pattern string
		path    string
		want    bool
	}{
		{"A", "A.B.C", true},
		{"A.B.C", "A.B", false},
		{"**", "A.B.C", true},
		{"**", "", true},
		{"A.**", "A", true},
		{"A.**", "A.B.C", true},
		{"A.*", "A.B", true},
		{"A.*", "A.B.C", true}, // matches a prefix, selecting the subtree
		{"A.*.C", "A.B.C", true},
		{"**.C", "A.B.C", true},
		{"A.C", "A.B.C", false},
	}

	for _, tc := range tests {
		got := matchSegments(pathSegments(tc.pattern), pathSegments(tc.path))
		if got != tc.want {
			t.Errorf("match(%q, %q) = %v, want %v", tc.pattern, tc.path, got, tc.want)
		}
	}
}

func TestValidatePattern(t *testing.T) {
	t.Parallel()

	if err := validatePattern("Result.["); err == nil {
		t.Error("validatePattern() accepted a malformed bracket pattern")
	}
	if err := validatePattern("Result.**"); err != nil {
		t.Errorf("validatePattern() rejected **: %v", err)
	}
	if err := validatePattern("Result.Code"); err != nil {
		t.Errorf("validatePattern() rejected a literal pattern: %v", err)
	}
}
