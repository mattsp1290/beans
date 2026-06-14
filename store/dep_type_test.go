package store

import (
	"strings"
	"testing"
)

func TestValidateDepType(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "empty defaults to blocks", in: "", want: DepTypeBlocks},
		{name: "whitespace defaults to blocks", in: "   ", want: DepTypeBlocks},
		{name: "blocks", in: "blocks", want: DepTypeBlocks},
		{name: "parent-child", in: "parent-child", want: DepTypeParentChild},
		{name: "trims surrounding space", in: "  parent-child  ", want: DepTypeParentChild},
		// bd-compat: custom/association types are accepted, not rejected.
		{name: "custom bd type accepted", in: "discovered-from", want: "discovered-from"},
		{name: "too long rejected", in: strings.Repeat("x", maxDepTypeLen+1), wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ValidateDepType(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ValidateDepType(%q) err = nil, want error", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateDepType(%q) err = %v, want nil", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("ValidateDepType(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
