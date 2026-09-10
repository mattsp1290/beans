package issue

import (
	"strings"
	"testing"
)

func TestSlug(t *testing.T) {
	cases := []struct {
		name  string
		title string
		want  string
	}{
		{"unicode", "Héllo Wörld", "h-llo-w-rld"},
		{"punctuation", "Hello, World!!!", "hello-world"},
		{"already-clean", "simple-title", "simple-title"},
		{"leading-trailing-punct", "---Trim Me---", "trim-me"},
		{"only-punctuation", "!!! --- ???", ""},
		{"digits", "Release 1.2.3", "release-1-2-3"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Slug(c.title)
			if got != c.want {
				t.Errorf("Slug(%q) = %q, want %q", c.title, got, c.want)
			}
		})
	}
}

func TestSlugLongTitleCutsAtWordBoundary(t *testing.T) {
	title := "this is a very long issue title that keeps going and going and going and going past the limit for sure"
	got := Slug(title)

	if len(got) > SlugMaxLen {
		t.Fatalf("Slug length %d exceeds SlugMaxLen %d: %q", len(got), SlugMaxLen, got)
	}
	if strings.HasSuffix(got, "-") {
		t.Fatalf("Slug has trailing dash: %q", got)
	}
	if got == "" {
		t.Fatal("Slug is empty")
	}

	// The cut result must be a prefix of the un-cut slug, ending exactly at
	// a dash boundary (i.e. the uncut slug continues with "-" or ends right
	// there).
	full := slugNoCut(title)
	if !strings.HasPrefix(full, got) {
		t.Fatalf("cut slug %q is not a prefix of the uncut slug %q", got, full)
	}
	if len(full) > len(got) {
		next := full[len(got)]
		if next != '-' {
			t.Fatalf("cut slug %q does not end at a dash boundary in uncut slug %q (next char %q)", got, full, next)
		}
	}
}

func TestSlugEmptyFilenameFallback(t *testing.T) {
	title := "!!! --- ???"
	slug := Slug(title)
	if slug != "" {
		t.Fatalf("expected empty slug, got %q", slug)
	}
	if got := Filename("proj-a1b2", slug); got != "proj-a1b2.md" {
		t.Errorf("Filename with empty slug = %q, want %q", got, "proj-a1b2.md")
	}
}

func TestFilename(t *testing.T) {
	if got := Filename("proj-a1b2", "my-title"); got != "proj-a1b2-my-title.md" {
		t.Errorf("Filename = %q", got)
	}
	if got := Filename("proj-a1b2", ""); got != "proj-a1b2.md" {
		t.Errorf("Filename with empty slug = %q", got)
	}
}

// slugNoCut replicates Slug's normalization without the 60-char cut, for
// asserting the cut result is a genuine prefix at a word boundary.
func slugNoCut(title string) string {
	var b strings.Builder
	lastDash := true
	for _, r := range strings.ToLower(title) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func TestValidID(t *testing.T) {
	accept := []string{"beans-a3f2", "bean-counter-x1", "beans-ceh.15"}
	reject := []string{"Beans-A3", "beans", "-x", "a-"}

	for _, id := range accept {
		if !ValidID(id) {
			t.Errorf("ValidID(%q) = false, want true", id)
		}
	}
	for _, id := range reject {
		if ValidID(id) {
			t.Errorf("ValidID(%q) = true, want false", id)
		}
	}
}

func TestNewID(t *testing.T) {
	calls := 0
	exists := func(id string) bool {
		calls++
		return calls <= 8
	}
	id := NewID("beans", exists, 4)

	if calls != 9 {
		t.Fatalf("exists was called %d times, want 9", calls)
	}
	wantLen := len("beans") + 1 + 5
	if len(id) != wantLen {
		t.Fatalf("NewID id = %q (len %d), want hash length 5 (total len %d)", id, len(id), wantLen)
	}
	if !ValidID(id) {
		t.Errorf("NewID produced an invalid id: %q", id)
	}
	if !strings.HasPrefix(id, "beans-") {
		t.Errorf("NewID id = %q, want prefix %q", id, "beans-")
	}
}

func TestNewIDAllGeneratedAreValid(t *testing.T) {
	for i := 0; i < 50; i++ {
		id := NewID("proj", nil, 4)
		if !ValidID(id) {
			t.Fatalf("generated id %q is not valid", id)
		}
	}
}

func TestNewIDDefaultLength(t *testing.T) {
	id := NewID("proj", nil, 0)
	want := len("proj") + 1 + DefaultIDLength
	if len(id) != want {
		t.Fatalf("NewID with length<=0 = %q (len %d), want len %d", id, len(id), want)
	}
}
