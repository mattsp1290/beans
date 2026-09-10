package markdown

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "update golden files")

// testRenderer returns a Renderer with a fixed, deterministic configuration
// used by the golden tests: it resolves the targets "note" and "img.png",
// leaves everything else unresolved, and embeds the note "note" (whole, or
// just its "heading" section).
func testRenderer() *Renderer {
	resolved := map[string]string{
		"note":    "/notes/note",
		"img.png": "/assets/img.png",
	}
	return &Renderer{
		Resolve: func(target string) (string, bool) {
			href, ok := resolved[target]
			return href, ok
		},
		Embed: func(target, fragment string) (string, bool) {
			if target != "note" {
				return "", false
			}
			switch fragment {
			case "":
				return "<p>embedded body</p>", true
			case "heading":
				return "<p>section only</p>", true
			default:
				return "", false
			}
		},
	}
}

func TestGolden(t *testing.T) {
	files, err := filepath.Glob("testdata/*.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no testdata/*.md files found")
	}

	for _, mdPath := range files {
		name := strings.TrimSuffix(filepath.Base(mdPath), ".md")
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(mdPath)
			if err != nil {
				t.Fatal(err)
			}

			r := testRenderer()
			html, _, err := r.HTML(src)
			if err != nil {
				t.Fatalf("HTML: %v", err)
			}

			goldenPath := filepath.Join("testdata", name+".html")
			if *update {
				if err := os.WriteFile(goldenPath, html, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}

			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("read golden: %v (run with -update to create it)", err)
			}
			if string(html) != string(want) {
				t.Errorf("golden mismatch for %s:\n--- got ---\n%s\n--- want ---\n%s", name, html, want)
			}
		})
	}
}

func TestHeadingsTOC(t *testing.T) {
	src, err := os.ReadFile("testdata/headings.md")
	if err != nil {
		t.Fatal(err)
	}
	r := testRenderer()
	_, toc, err := r.HTML(src)
	if err != nil {
		t.Fatalf("HTML: %v", err)
	}

	want := []Heading{
		{Level: 1, ID: "title-one", Text: "Title One"},
		{Level: 2, ID: "sub-heading", Text: "Sub Heading"},
		{Level: 3, ID: "sub-sub-heading", Text: "Sub Sub Heading"},
	}
	if len(toc) != len(want) {
		t.Fatalf("toc length = %d, want %d (toc=%+v)", len(toc), len(want), toc)
	}
	for i, h := range want {
		if toc[i] != h {
			t.Errorf("toc[%d] = %+v, want %+v", i, toc[i], h)
		}
	}
}

func TestLinks(t *testing.T) {
	src := []byte(`See [[note]] and [[note]] again.

Embed: ![[note]].

Heading link: [[note#Section]].

Inline code with ` + "`[[not a link]]`" + ` should not count.

` + "```" + `
[[also not a link]]
` + "```" + `
`)

	got := Links(src)
	want := []Link{
		{Target: "note", Fragment: "", Alias: "", Embed: false},
		{Target: "note", Fragment: "", Alias: "", Embed: true},
		{Target: "note", Fragment: "Section", Alias: "", Embed: false},
	}

	if len(got) != len(want) {
		t.Fatalf("Links() length = %d, want %d (got=%+v)", len(got), len(want), got)
	}
	for i, l := range want {
		if got[i] != l {
			t.Errorf("Links()[%d] = %+v, want %+v", i, got[i], l)
		}
	}
}

func TestLinksAlias(t *testing.T) {
	src := []byte(`[[note|Custom Label]]`)
	got := Links(src)
	want := []Link{
		{Target: "note", Fragment: "", Alias: "Custom Label", Embed: false},
	}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("Links() = %+v, want %+v", got, want)
	}
}

func TestNoEmbedsRendersEmbedAsLink(t *testing.T) {
	r := testRenderer()
	full, _, err := r.HTML([]byte("![[note]]"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(full), `class="embed"`) {
		t.Fatalf("embed expected with Embed set:\n%s", full)
	}
	inner, _, err := r.NoEmbeds().HTML([]byte("![[note]]"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(inner), `class="embed"`) || !strings.Contains(string(inner), `class="wikilink"`) {
		t.Fatalf("NoEmbeds must render a plain link:\n%s", inner)
	}
	if r.Embed == nil {
		t.Fatal("NoEmbeds must return a copy, not mutate the receiver")
	}
}
