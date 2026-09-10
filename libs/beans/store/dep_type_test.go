package store

import (
	"slices"
	"strings"
	"testing"
)

// TestDedupeImportInputsUnionsEdges locks the dedupe contract: for a repeated
// id, edge slices (Deps, ParentEdges) are UNIONED (duplicates tolerated) while
// scalar fields are REPLACED wholesale (last write wins).
func TestDedupeImportInputsUnionsEdges(t *testing.T) {
	t.Parallel()

	items := []ImportInput{
		{ID: "p-leaf", Prefix: "p", Title: "first", Deps: []string{"p-b1"}, ParentEdges: []string{"p-epic1"}},
		{ID: "p-leaf", Prefix: "p", Title: "second", Deps: []string{"p-b1", "p-b2"}, ParentEdges: []string{"p-epic1", "p-epic2"}},
	}
	out := dedupeImportInputs(items)
	if len(out) != 1 {
		t.Fatalf("dedupe len = %d, want 1", len(out))
	}
	got := out[0]
	// Scalar replaced: last write wins.
	if got.Title != "second" {
		t.Fatalf("Title = %q, want last-write-wins 'second'", got.Title)
	}
	// Edges unioned, not de-duplicated (duplicates absorbed downstream).
	if !slices.Equal(got.Deps, []string{"p-b1", "p-b1", "p-b2"}) {
		t.Fatalf("Deps = %#v, want unioned with duplicate", got.Deps)
	}
	if !slices.Equal(got.ParentEdges, []string{"p-epic1", "p-epic1", "p-epic2"}) {
		t.Fatalf("ParentEdges = %#v, want unioned with duplicate", got.ParentEdges)
	}
}

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
