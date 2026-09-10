package markdown

import (
	gast "github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Highlight is an inline node produced by ==text==, rendered as <mark>.
type Highlight struct {
	gast.BaseInline
}

// KindHighlight is the NodeKind of Highlight nodes.
var KindHighlight = gast.NewNodeKind("Highlight")

// Kind implements ast.Node.
func (n *Highlight) Kind() gast.NodeKind { return KindHighlight }

// Dump implements ast.Node.
func (n *Highlight) Dump(source []byte, level int) {
	gast.DumpHelper(n, source, level, nil, nil)
}

// NewHighlight returns a new Highlight node.
func NewHighlight() *Highlight { return &Highlight{} }

type highlightDelimiterProcessor struct{}

func (highlightDelimiterProcessor) IsDelimiter(b byte) bool { return b == '=' }

func (highlightDelimiterProcessor) CanOpenCloser(opener, closer *parser.Delimiter) bool {
	return opener.Char == closer.Char
}

func (highlightDelimiterProcessor) OnMatch(consumes int) gast.Node { return NewHighlight() }

var defaultHighlightDelimiterProcessor = highlightDelimiterProcessor{}

// highlightParser parses "==text==" as a Highlight node. It requires an
// exact run of two '=' characters, so a single '=' never triggers it.
type highlightParser struct{}

func (highlightParser) Trigger() []byte { return []byte{'='} }

func (highlightParser) Parse(parent gast.Node, block text.Reader, pc parser.Context) gast.Node {
	before := block.PrecendingCharacter()
	line, segment := block.PeekLine()
	// Only an exact run of two "=" is a delimiter: a longer run is left
	// alone entirely, so "===" never opens a mark one character in.
	if len(line) < 2 || line[0] != '=' || line[1] != '=' || (len(line) > 2 && line[2] == '=') {
		return nil
	}
	if before == '=' {
		return nil
	}
	node := parser.ScanDelimiter(line, before, 2, defaultHighlightDelimiterProcessor)
	if node == nil || node.OriginalLength != 2 {
		return nil
	}
	node.Segment = segment.WithStop(segment.Start + node.OriginalLength)
	block.Advance(node.OriginalLength)
	pc.PushDelimiter(node)
	return node
}

type highlightNodeRenderer struct{}

func (r *highlightNodeRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(KindHighlight, r.render)
}

func (r *highlightNodeRenderer) render(w util.BufWriter, _ []byte, _ gast.Node, entering bool) (gast.WalkStatus, error) {
	if entering {
		_, _ = w.WriteString("<mark>")
	} else {
		_, _ = w.WriteString("</mark>")
	}
	return gast.WalkContinue, nil
}
