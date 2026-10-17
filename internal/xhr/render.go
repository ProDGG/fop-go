package xhr

import (
	"bytes"
	"fmt"
	"os"
	"runtime/debug"
	"strings"

	"github.com/ProDGG/fop-go/internal/pdf"
	"github.com/ProDGG/fop-go/xmldom"
)

// Options configures Render.
type Options struct {
	// ResolveResource loads external resources (images, style sheets) by URI.
	ResolveResource func(uri string) ([]byte, error)
	// Fonts adds TrueType fonts: family name (empty = the font's own) -> data.
	Fonts []FontData
	// Info is stored in the PDF information dictionary.
	Title, Author string
	// Debug dumps the box tree to the given writer when set.
	Dump *strings.Builder
}

// FontData is a font file to register.
type FontData struct {
	Family string
	Data   []byte
}

// Render lays out an (X)HTML document and returns the PDF the way Flying
// Saucer + iText 5 would produce it.
func Render(doc *xmldom.Node, opts Options) ([]byte, error) {
	var out []byte
	var rerr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				rerr = fmt.Errorf("xhr: %v", r)
				if os.Getenv("XHR_DEBUG") != "" {
					rerr = fmt.Errorf("xhr: %v\n%s", r, debug.Stack())
				}
			}
		}()
		out, rerr = render(doc, opts)
	}()
	return out, rerr
}

func render(docNode *xmldom.Node, opts Options) ([]byte, error) {
	root := docNode
	if root.Type == xmldom.DocumentNode {
		root = root.DocumentElement()
	}
	if root == nil {
		return nil, fmt.Errorf("xhr: document has no root element")
	}
	fe := NewFontEngine()
	for _, f := range opts.Fonts {
		if err := fe.AddFont(f.Family, f.Data); err != nil {
			return nil, err
		}
	}
	ctx := NewCtx(fe)
	m := BuildMatcher(root, func(href string) string {
		if opts.ResolveResource == nil {
			return ""
		}
		b, err := opts.ResolveResource(href)
		if err != nil {
			return ""
		}
		return string(b)
	})
	d := NewDoc(ctx, m, root, NewImageCache(opts.ResolveResource))
	lc := newLayoutContext(d)

	rootBox := d.createRootBox(lc)
	first := createPageBox(lc, "first")
	vp := newBox(KBlock)
	vp.contentWidth = first.contentWidth()
	vp.height = first.contentHeight()
	vp.isInitialCB = true
	rootBox.containingBlk = vp
	rootBox.layoutBox(lc)
	d.mainLayer = lc.rootLayer
	dimH := paintingHeight(rootBox)
	lc.rootLayer.trimEmptyPages(dimH)
	if opts.Dump != nil {
		dumpBox(opts.Dump, rootBox, 0)
	}
	lc.rootLayer.layoutPages(lc)

	pdoc := pdf.NewDocument()
	pdoc.ITextNumbers = true
	pdoc.Info = pdf.Info{Title: opts.Title, Author: opts.Author, Producer: "fop-go (Flying Saucer compatible renderer)"}
	pn := &painter{d: d, lc: lc, pdoc: pdoc, dpp: float64(ctx.DotsPerPoint), fonts: map[*FontDesc]*pdf.Font{}, imgs: map[*Image]*pdf.Image{}, linkSeen: map[string]bool{}}
	main := d.mainLayer
	main.assignPagePaintingPositions()
	count := len(main.pages)
	for i, pb := range main.pages {
		pn.paintPage(pb, i, count)
	}
	pn.writeOutline()
	var buf bytes.Buffer
	if err := pdoc.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// paintingHeight returns the bottom of the outer margin boxes of the whole tree.
func paintingHeight(b *Box) int {
	maxY := b.absY + b.height
	var walk func(x *Box)
	walk = func(x *Box) {
		if y := x.absY + x.height; y > maxY && x.Kind != KInline {
			maxY = y
		}
		for _, c := range x.children {
			walk(c)
		}
		if x.persistentBFC != nil {
			x.persistentBFC.performOnFloats(func(f *Box) { walk(f) })
		}
	}
	walk(b)
	return maxY
}

// writeOutline builds bookmarks from h1..h6 like HTMLOutline.
func (p *painter) writeOutline() {
	type entry struct {
		level int
		title string
		box   *Box
	}
	var entries []entry
	var walkBoxes func(b *Box)
	walkBoxes = func(b *Box) {
		if b.Elem != nil && !b.anonymous && b.Kind == KBlock {
			n := b.Elem.Name
			if len(n) == 2 && n[0] == 'h' && n[1] >= '1' && n[1] <= '6' {
				entries = append(entries, entry{int(n[1] - '0'), elementTextDeep(b.Elem), b})
			}
		}
		for _, c := range b.children {
			walkBoxes(c)
		}
	}
	walkBoxes(p.d.mainLayer.master)
	if len(entries) == 0 {
		return
	}
	var roots []*pdf.Outline
	type frame struct {
		level int
		o     *pdf.Outline
	}
	var stack []frame
	for _, e := range entries {
		pb := p.d.mainLayer.pageAt(p.lc, e.box.absY)
		dist := pb.mbp(Top) + e.box.absY - pb.top
		o := &pdf.Outline{Title: e.title, Page: pb.pageNo, Y: float64(pb.height())/p.dpp - float64(dist)/p.dpp}
		for len(stack) > 0 && stack[len(stack)-1].level >= e.level {
			stack = stack[:len(stack)-1]
		}
		if len(stack) == 0 {
			roots = append(roots, o)
		} else {
			par := stack[len(stack)-1].o
			par.Children = append(par.Children, o)
		}
		stack = append(stack, frame{e.level, o})
	}
	p.pdoc.Outlines = roots
}

func elementTextDeep(e *xmldom.Node) string {
	var sb strings.Builder
	var walk func(n *xmldom.Node)
	walk = func(n *xmldom.Node) {
		for _, c := range n.Children {
			switch c.Type {
			case xmldom.TextNode:
				sb.WriteString(c.Value)
			case xmldom.ElementNode:
				walk(c)
			}
		}
	}
	walk(e)
	return strings.TrimSpace(strings.Join(strings.Fields(sb.String()), " "))
}
