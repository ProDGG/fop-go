package xhr

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"runtime/debug"
	"strconv"
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
	// MaxPages stops layout with pdf.ErrPageLimit once the document needs
	// more pages (0 = unlimited).
	MaxPages int
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
				if e, ok := r.(error); ok && errors.Is(e, pdf.ErrPageLimit) {
					rerr = e
					return
				}
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
	lc.maxPages = opts.MaxPages

	rootBox := d.createRootBox(lc)
	first := createPageBox(lc, "first")
	vp := newBox(KBlock)
	vp.contentWidth = first.contentWidth()
	vp.height = first.contentHeight()
	vp.isInitialCB = true
	vp.fixedRect = &Rect{0, 0, vp.contentWidth, vp.height}
	rootBox.containingBlk = vp
	rootBox.layoutBox(lc)
	d.mainLayer = lc.rootLayer
	dimH := layerPaintingHeight(lc.rootLayer, rootBox)
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
	if opts.Dump != nil && len(main.pages) > 0 {
		for i, a := range main.pages[0].areas {
			if a != nil && a.table != nil {
				fmt.Fprintf(opts.Dump, "-- margin area %d\n", i)
				dumpBox(opts.Dump, a.table, 0)
			}
		}
	}
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

var outlineRoot = map[string]bool{"blockquote": true, "details": true, "fieldset": true, "figure": true, "td": true}

// outlineLevel is HTMLOutline.getOutlineLevel.
func outlineLevel(e *xmldom.Node) string {
	bm := strings.TrimSpace(e.Attr("data-pdf-bookmark"))
	if bm != "" {
		return bm
	}
	n := strings.ToLower(e.Name)
	if len(n) == 2 && n[0] == 'h' && n[1] >= '1' && n[1] <= '6' {
		return string(n[1])
	}
	if outlineRoot[n] {
		return "exclude"
	}
	return "none"
}

type outlineNode struct {
	level    int
	name     string
	parent   *outlineNode
	children []*outlineNode
	box      *Box
	href     string
}

// writeOutline is ITextOutputDevice.writeOutline with HTMLOutline.generate: the
// bookmarks come from the DOM headings (whether or not they produced a box).
func (p *painter) writeOutline() {
	root := &outlineNode{}
	cur := root
	byElem := map[*xmldom.Node]*outlineNode{}
	var walk func(e *xmldom.Node)
	walk = func(e *xmldom.Node) {
		// NodeIterator treats rejected and skipped elements alike: only the element itself is left out
		lvl := outlineLevel(e)
		if !strings.EqualFold(lvl, "none") && !strings.EqualFold(lvl, "exclude") {
			if level, err := strconv.Atoi(lvl); err == nil && level >= 1 {
				name := strings.TrimSpace(e.Attr("data-pdf-bookmark-name"))
				if name == "" {
					name = e.StringValue()
				}
				name = strings.Join(strings.Fields(name), " ")
				if r := []rune(name); len(r) > 200 {
					name = string(r[:200])
				}
				for cur.level >= level {
					cur = cur.parent
				}
				n := &outlineNode{level: level, name: name, parent: cur}
				cur.children = append(cur.children, n)
				cur = n
				byElem[e] = n
			}
		}
		for _, c := range e.Children {
			if c.Type == xmldom.ElementNode {
				walk(c)
			}
		}
	}
	explicit := false
	if p.d.root != nil {
		// <head><bookmarks><bookmark name="" href=""> overrides the generated outline
		for _, h := range p.d.root.Children {
			if h.Type != xmldom.ElementNode || h.Name != "head" {
				continue
			}
			for _, bs := range h.Children {
				if bs.Type != xmldom.ElementNode || bs.Name != "bookmarks" {
					continue
				}
				var load func(parent *outlineNode, e *xmldom.Node)
				load = func(parent *outlineNode, e *xmldom.Node) {
					n := &outlineNode{name: e.Attr("name"), href: e.Attr("href"), parent: parent}
					parent.children = append(parent.children, n)
					explicit = true
					for _, c := range e.Children {
						if c.Type == xmldom.ElementNode && c.Name == "bookmark" {
							load(n, c)
						}
					}
				}
				for _, bm := range bs.Children {
					if bm.Type == xmldom.ElementNode && bm.Name == "bookmark" {
						load(root, bm)
					}
				}
			}
		}
		if !explicit {
			walk(p.d.root)
		}
	}
	if len(root.children) == 0 {
		return
	}
	var initBoxes func(b *Box)
	initBoxes = func(b *Box) {
		if b.Elem != nil {
			if n := byElem[b.Elem]; n != nil {
				n.box = b
			}
		}
		for _, c := range b.children {
			initBoxes(c)
		}
	}
	initBoxes(p.d.mainLayer.master)
	var resolve func(list []*outlineNode)
	resolve = func(list []*outlineNode) {
		for _, n := range list {
			if strings.HasPrefix(n.href, "#") {
				if b := p.d.boxIDs[n.href[1:]]; b != nil {
					n.box = b
				}
			}
			resolve(n.children)
		}
	}
	resolve(root.children)
	var build func(list []*outlineNode) []*pdf.Outline
	build = func(list []*outlineNode) []*pdf.Outline {
		var res []*pdf.Outline
		for _, n := range list {
			o := &pdf.Outline{Title: n.name}
			if n.box != nil {
				pb := p.d.mainLayer.pageAt(p.lc, n.box.absY)
				dist := pb.mbp(Top) + n.box.absY - pb.top
				o.Page = pb.pageNo
				o.Y = float64(pb.height())/p.dpp - float64(dist)/p.dpp
			} else {
				o.Page = 0
				o.FitH = true
				o.Y = float64(p.d.mainLayer.pages[0].height()) / p.dpp
			}
			o.Children = build(n.children)
			res = append(res, o)
		}
		return res
	}
	p.pdoc.Outlines = build(root.children)
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

// layerPaintingHeight is Layer.getPaintingDimension: the master's extent grown
// by absolutely positioned child layers.
func layerPaintingHeight(l *Layer, master *Box) int {
	h := paintingHeight(master)
	for _, c := range l.children {
		if c.master.Style.IsFixed() || !c.master.Style.IsAbsolute() {
			continue
		}
		if ch := layerPaintingHeight(c, c.master); ch > h {
			h = ch
		}
	}
	return h
}
