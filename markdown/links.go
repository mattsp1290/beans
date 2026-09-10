package markdown

import (
	"github.com/yuin/goldmark"
	gast "github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"

	"go.abhg.dev/goldmark/frontmatter"
	"go.abhg.dev/goldmark/wikilink"
)

// Link is one wikilink target found in a document body.
type Link struct {
	Target   string
	Fragment string
	Alias    string
	Embed    bool
}

// Links returns every wikilink target in src (body links only;
// deduplicated, in order of appearance), with Embed set for ![[ ]] forms.
// Code spans and fenced blocks never produce links.
func Links(src []byte) []Link {
	md := goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			&frontmatter.Extender{},
			&wikilink.Extender{},
		),
	)
	doc := md.Parser().Parse(text.NewReader(src))

	type key struct {
		target, fragment string
		embed            bool
	}
	seen := make(map[key]bool)
	var links []Link

	_ = gast.Walk(doc, func(n gast.Node, entering bool) (gast.WalkStatus, error) {
		if !entering {
			return gast.WalkContinue, nil
		}
		wn, ok := n.(*wikilink.Node)
		if !ok {
			return gast.WalkContinue, nil
		}
		target := string(wn.Target)
		fragment := string(wn.Fragment)

		label := wikilinkLabel(wn, src)
		def := target
		if fragment != "" {
			def += "#" + fragment
		}
		alias := ""
		if label != def {
			alias = label
		}

		k := key{target, fragment, wn.Embed}
		if seen[k] {
			return gast.WalkContinue, nil
		}
		seen[k] = true
		links = append(links, Link{Target: target, Fragment: fragment, Alias: alias, Embed: wn.Embed})
		return gast.WalkContinue, nil
	})

	return links
}
