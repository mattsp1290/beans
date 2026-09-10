package markdown

import (
	"bytes"
	"regexp"
	"strings"
	"unicode"

	gast "github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// CalloutBlock is a block node produced by transforming a blockquote whose
// first line matches "[!type]". Its children are the blockquote's children
// with the marker line removed, so lists, code, and nested callouts inside
// the callout render normally.
type CalloutBlock struct {
	gast.BaseBlock

	// CalloutType is the lowercased callout type, e.g. "note", "warning".
	CalloutType string
	// Fold is "+", "-", or "" when no fold marker was given.
	Fold string
	// Title is the callout title (explicit, or the capitalized CalloutType).
	Title string
}

// KindCallout is the NodeKind of CalloutBlock nodes.
var KindCallout = gast.NewNodeKind("Callout")

// Kind implements ast.Node.
func (n *CalloutBlock) Kind() gast.NodeKind { return KindCallout }

// Dump implements ast.Node.
func (n *CalloutBlock) Dump(source []byte, level int) {
	gast.DumpHelper(n, source, level, map[string]string{
		"Type":  n.CalloutType,
		"Fold":  n.Fold,
		"Title": n.Title,
	}, nil)
}

var calloutMarkerRe = regexp.MustCompile(`^\[!([A-Za-z0-9_-]+)\]([+-])?[ \t]*(.*?)[ \t]*$`)

// calloutTransformer is a parser.ASTTransformer that rewrites blockquotes
// whose first paragraph starts with "[!type]" into CalloutBlock nodes. It
// runs after inline parsing, so it removes the marker line's inline nodes
// from the first paragraph and moves the remaining children across.
type calloutTransformer struct{}

func (calloutTransformer) Transform(doc *gast.Document, reader text.Reader, _ parser.Context) {
	source := reader.Source()
	var blockquotes []*gast.Blockquote
	_ = gast.Walk(doc, func(n gast.Node, entering bool) (gast.WalkStatus, error) {
		if bq, ok := n.(*gast.Blockquote); ok && entering {
			blockquotes = append(blockquotes, bq)
		}
		return gast.WalkContinue, nil
	})
	// Innermost first so a nested callout is transformed before its parent
	// moves it.
	for i := len(blockquotes) - 1; i >= 0; i-- {
		transformCallout(blockquotes[i], source)
	}
}

func transformCallout(bq *gast.Blockquote, source []byte) {
	first, ok := bq.FirstChild().(*gast.Paragraph)
	if !ok || first.Lines().Len() == 0 {
		return
	}
	line0 := first.Lines().At(0)
	raw := bytes.TrimRight(line0.Value(source), "\r\n")
	m := calloutMarkerRe.FindSubmatch(raw)
	if m == nil {
		return
	}
	// The title is the raw marker-line text, escaped on output; inline
	// markup inside a callout title is not rendered (accepted scope cut).
	cb := &CalloutBlock{CalloutType: strings.ToLower(string(m[1])), Fold: string(m[2]), Title: string(m[3])}
	if cb.Title == "" {
		cb.Title = capitalizeFirst(cb.CalloutType)
	}

	// Drop the marker line: every inline node up to and including the first
	// one that ends a line.
	for c := first.FirstChild(); c != nil; {
		next := c.NextSibling()
		first.RemoveChild(first, c)
		if t, ok := c.(*gast.Text); ok && (t.SoftLineBreak() || t.HardLineBreak()) {
			break
		}
		c = next
	}
	if first.ChildCount() == 0 {
		bq.RemoveChild(bq, first)
	}
	for c := bq.FirstChild(); c != nil; {
		next := c.NextSibling()
		cb.AppendChild(cb, c)
		c = next
	}
	if parent := bq.Parent(); parent != nil {
		parent.ReplaceChild(parent, bq, cb)
	}
}

func capitalizeFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

type calloutNodeRenderer struct{}

func (r *calloutNodeRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(KindCallout, r.render)
}

func (r *calloutNodeRenderer) render(w util.BufWriter, _ []byte, node gast.Node, entering bool) (gast.WalkStatus, error) {
	cb, ok := node.(*CalloutBlock)
	if !ok {
		return gast.WalkContinue, nil
	}
	if !entering {
		_, _ = w.WriteString("</div></div>\n")
		return gast.WalkContinue, nil
	}
	_, _ = w.WriteString(`<div class="callout" data-callout="`)
	_, _ = w.Write(util.EscapeHTML([]byte(cb.CalloutType)))
	_, _ = w.WriteString(`"`)
	if cb.Fold == "+" || cb.Fold == "-" {
		_, _ = w.WriteString(` data-fold="` + cb.Fold + `"`)
	}
	_, _ = w.WriteString(`><div class="callout-title">`)
	_, _ = w.Write(util.EscapeHTML([]byte(cb.Title)))
	_, _ = w.WriteString("</div><div class=\"callout-content\">\n")
	return gast.WalkContinue, nil
}
