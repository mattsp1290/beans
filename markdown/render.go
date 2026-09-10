// Package markdown renders Obsidian-flavored markdown to HTML.
package markdown

import (
	"bytes"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/yuin/goldmark"
	gast "github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"

	"go.abhg.dev/goldmark/frontmatter"
	"go.abhg.dev/goldmark/hashtag"
	"go.abhg.dev/goldmark/wikilink"
)

// Heading is one entry of the table of contents.
type Heading struct {
	Level int
	ID    string
	Text  string
}

// Renderer renders Obsidian-flavored markdown to HTML.
type Renderer struct {
	// Resolve maps a wikilink target (text inside [[ ]] before | or #) to
	// an href. exists=false marks the link unresolved.
	Resolve func(target string) (href string, exists bool)

	// Embed returns the rendered HTML of a note embedded with ![[target]]
	// (optionally only the section named by fragment), or ok=false when
	// the target is not an embeddable note. The renderer calls it at
	// most one level deep: the returned HTML must itself have been
	// rendered with embeds disabled.
	Embed func(target, fragment string) (html string, ok bool)

	// AssetHref maps an image embed target (a file with extension png,
	// jpg, jpeg, gif, svg, webp) to a URL; defaults to
	// "/api/assets/" + target.
	AssetHref func(target string) string

	// TagHref maps a #tag to a URL; defaults to "/search?tag=" + tag.
	TagHref func(tag string) string
}

// NoEmbeds returns a copy of r whose Embed is nil.
func (r *Renderer) NoEmbeds() *Renderer {
	cp := *r
	cp.Embed = nil
	return &cp
}

func (r *Renderer) assetHref(target string) string {
	if r.AssetHref != nil {
		return r.AssetHref(target)
	}
	segs := strings.Split(target, "/")
	for i, seg := range segs {
		segs[i] = url.PathEscape(seg)
	}
	return "/api/assets/" + strings.Join(segs, "/")
}

func (r *Renderer) tagHref(tag string) string {
	if r.TagHref != nil {
		return r.TagHref(tag)
	}
	return "/search?tag=" + url.QueryEscape(tag)
}

var imageExts = map[string]bool{
	".png":  true,
	".jpg":  true,
	".jpeg": true,
	".gif":  true,
	".svg":  true,
	".webp": true,
}

func isImageTarget(target string) bool {
	return imageExts[strings.ToLower(filepath.Ext(target))]
}

// newMarkdown builds a goldmark instance configured with GFM, footnotes,
// frontmatter stripping, wikilinks, hashtags, the callout transformer and
// the highlight inline parser.
func (r *Renderer) newMarkdown() goldmark.Markdown {
	return goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			extension.Footnote,
			&frontmatter.Extender{},
			&wikilink.Extender{},
			&hashtag.Extender{},
		),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
			parser.WithASTTransformers(
				util.Prioritized(calloutTransformer{}, 100),
			),
			parser.WithInlineParsers(
				util.Prioritized(highlightParser{}, 500),
			),
		),
		goldmark.WithRendererOptions(
			renderer.WithNodeRenderers(
				util.Prioritized(&wikilinkNodeRenderer{r: r}, 0),
				util.Prioritized(&hashtagNodeRenderer{r: r}, 0),
				util.Prioritized(&calloutNodeRenderer{}, 0),
				util.Prioritized(&highlightNodeRenderer{}, 0),
			),
		),
	)
}

// HTML renders src. toc lists H1 to H3 with their ids (goldmark auto heading
// ids).
func (r *Renderer) HTML(src []byte) (html []byte, toc []Heading, err error) {
	md := r.newMarkdown()
	doc := md.Parser().Parse(text.NewReader(src))

	var buf bytes.Buffer
	if err := md.Renderer().Render(&buf, src, doc); err != nil {
		return nil, nil, err
	}

	return buf.Bytes(), collectTOC(doc, src), nil
}

func collectTOC(doc gast.Node, source []byte) []Heading {
	var toc []Heading
	_ = gast.Walk(doc, func(n gast.Node, entering bool) (gast.WalkStatus, error) {
		if !entering {
			return gast.WalkContinue, nil
		}
		h, ok := n.(*gast.Heading)
		if !ok || h.Level > 3 {
			return gast.WalkContinue, nil
		}
		id := ""
		if v, ok := h.AttributeString("id"); ok {
			switch vv := v.(type) {
			case []byte:
				id = string(vv)
			case string:
				id = vv
			}
		}
		toc = append(toc, Heading{Level: h.Level, ID: id, Text: nodeText(h, source)})
		return gast.WalkContinue, nil
	})
	return toc
}

// wikilinkLabel returns the display text for a wikilink node: the alias
// when one was given ([[target|alias]]), otherwise the target (plus
// "#fragment" when present), matching what the wikilink parser stores as
// the node's single text child in both cases.
func wikilinkLabel(n *wikilink.Node, source []byte) string {
	if c := n.FirstChild(); c != nil {
		if t, ok := c.(*gast.Text); ok {
			return string(t.Segment.Value(source))
		}
	}
	s := string(n.Target)
	if len(n.Fragment) > 0 {
		s += "#" + string(n.Fragment)
	}
	return s
}

// wikilinkNodeRenderer renders *wikilink.Node, replacing the wikilink
// extension's default renderer so the exact classes and unresolved form
// required by the vault contract are produced.
type wikilinkNodeRenderer struct {
	r *Renderer
}

func (rr *wikilinkNodeRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(wikilink.Kind, rr.render)
}

func (rr *wikilinkNodeRenderer) render(w util.BufWriter, source []byte, node gast.Node, entering bool) (gast.WalkStatus, error) {
	if !entering {
		return gast.WalkContinue, nil
	}
	n, ok := node.(*wikilink.Node)
	if !ok {
		return gast.WalkContinue, nil
	}
	target := string(n.Target)
	fragment := string(n.Fragment)
	label := wikilinkLabel(n, source)

	if n.Embed {
		if isImageTarget(target) {
			href := rr.r.assetHref(target)
			_, _ = w.WriteString(`<img class="embed" src="`)
			_, _ = w.Write(util.EscapeHTML([]byte(href)))
			_, _ = w.WriteString(`" alt="`)
			_, _ = w.Write(util.EscapeHTML([]byte(target)))
			_, _ = w.WriteString(`">`)
			return gast.WalkSkipChildren, nil
		}
		if rr.r.Embed != nil {
			if embedHTML, ok := rr.r.Embed(target, fragment); ok {
				_, _ = w.WriteString(`<div class="embed" data-embed="`)
				_, _ = w.Write(util.EscapeHTML([]byte(target)))
				_, _ = w.WriteString(`">`)
				_, _ = w.WriteString(embedHTML)
				_, _ = w.WriteString(`</div>`)
				return gast.WalkSkipChildren, nil
			}
		}
		// Fall through: render as a normal wikilink (resolved or
		// unresolved).
	}

	rr.renderWikilink(w, target, fragment, label)
	return gast.WalkSkipChildren, nil
}

func (rr *wikilinkNodeRenderer) renderWikilink(w util.BufWriter, target, fragment, label string) {
	var href string
	var exists bool
	if rr.r.Resolve != nil {
		href, exists = rr.r.Resolve(target)
	}
	if exists {
		full := href
		if fragment != "" {
			full += "#" + url.PathEscape(fragment)
		}
		_, _ = w.WriteString(`<a class="wikilink" href="`)
		_, _ = w.Write(util.EscapeHTML([]byte(full)))
		_, _ = w.WriteString(`">`)
		_, _ = w.Write(util.EscapeHTML([]byte(label)))
		_, _ = w.WriteString(`</a>`)
		return
	}
	_, _ = w.WriteString(`<a class="wikilink is-unresolved" href="/wiki/new?name=`)
	_, _ = w.WriteString(url.QueryEscape(target))
	_, _ = w.WriteString(`">`)
	_, _ = w.Write(util.EscapeHTML([]byte(label)))
	_, _ = w.WriteString(`</a>`)
}

// hashtagNodeRenderer renders *hashtag.Node, replacing the hashtag
// extension's default <span class="hashtag"> renderer with a plain link.
type hashtagNodeRenderer struct {
	r *Renderer
}

func (rr *hashtagNodeRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(hashtag.Kind, rr.render)
}

func (rr *hashtagNodeRenderer) render(w util.BufWriter, source []byte, node gast.Node, entering bool) (gast.WalkStatus, error) {
	if !entering {
		return gast.WalkContinue, nil
	}
	n, ok := node.(*hashtag.Node)
	if !ok {
		return gast.WalkContinue, nil
	}
	tag := string(n.Tag)
	href := rr.r.tagHref(tag)
	_, _ = w.WriteString(`<a class="hashtag" href="`)
	_, _ = w.Write(util.EscapeHTML([]byte(href)))
	_, _ = w.WriteString(`">#`)
	_, _ = w.Write(util.EscapeHTML([]byte(tag)))
	_, _ = w.WriteString(`</a>`)
	return gast.WalkSkipChildren, nil
}

// nodeText concatenates the text segments under n (headings hold Text nodes
// and inline containers such as emphasis).
func nodeText(n gast.Node, source []byte) string {
	var b strings.Builder
	_ = gast.Walk(n, func(c gast.Node, entering bool) (gast.WalkStatus, error) {
		if !entering {
			return gast.WalkContinue, nil
		}
		if t, ok := c.(*gast.Text); ok {
			b.Write(t.Segment.Value(source))
		}
		return gast.WalkContinue, nil
	})
	return b.String()
}
