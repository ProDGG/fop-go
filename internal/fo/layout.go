package fo

import (
	"math"
	"strconv"
	"strings"
)

// lctx is the layout context for the content of one reference area.
type lctx struct {
	e       *Engine
	refW    float64 // width of the reference area
	refH    float64 // height of the containing region (0 if unknown)
	S, E    float64 // content-box start/end indents (absolute within the reference area)
	pageNum int
	pageFmt string
	// list context for label-end() / body-start()
	listS, listDist, listSep float64
	markers                  []marker // markers available to retrieve-marker
	carry                    map[string]*Node
}

func (lc *lctx) contentW() float64 { return lc.refW - lc.S - lc.E }

func (lc *lctx) lengthCtx(n *Node, pct float64) *LengthCtx {
	ctx := lc.e.lengthCtx(n, pct)
	ctx.Funcs = map[string]func([]float64) float64{
		"label-end":  func([]float64) float64 { return lc.refW - (lc.listS + lc.listDist - lc.listSep) },
		"body-start": func([]float64) float64 { return lc.listS + lc.listDist },
	}
	return ctx
}

// length resolves property name on n (own or inherited value for inherited props).
func (lc *lctx) length(n *Node, name string, pct float64) (float64, bool) {
	v, ok := n.Own(name)
	if !ok || v == "" || v == "auto" {
		return 0, false
	}
	r, err := EvalLength(v, lc.lengthCtx(n, pct))
	if err != nil {
		lc.e.warn("cannot parse %s=%q: %v", name, v, err)
		return 0, false
	}
	return r, true
}

func (lc *lctx) lengthOr(n *Node, name string, pct, def float64) float64 {
	if v, ok := lc.length(n, name, pct); ok {
		return v
	}
	return def
}

// ---------- box properties ----------

func parseBrk(v string) brk {
	switch strings.TrimSpace(v) {
	case "page":
		return brkPage
	case "column":
		return brkColumn
	case "odd-page":
		return brkOdd
	case "even-page":
		return brkEven
	}
	return brkNone
}

func isKeep(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" || v == "auto" {
		return false
	}
	if v == "always" {
		return true
	}
	if n, err := strconv.Atoi(v); err == nil {
		return n > 0
	}
	return false
}

func firstToken(s string) string {
	f := strings.Fields(s)
	if len(f) == 0 {
		return ""
	}
	return f[0]
}

func (lc *lctx) borderOf(n *Node, side string, cw float64) border {
	style := n.Props["border-"+side+"-style"]
	if style == "" || style == "none" || style == "hidden" {
		return border{style: "none"}
	}
	w := 1.0
	if v, ok := n.Props["border-"+side+"-width"]; ok {
		if r, ok := lc.e.parseLength(n, v, cw); ok {
			w = r
		}
	} else {
		w = 1
	}
	c, ok := ParseColor(n.Props["border-"+side+"-color"])
	if !ok || c.A == 0 {
		c, _ = ParseColor(n.Get("color"))
		if c.A == 0 {
			c = Color{0, 0, 0, 1}
		}
	}
	return border{w: w, style: style, c: c}
}

// boxProps fills padding, borders, background, spacing, keeps and breaks.
func (lc *lctx) boxProps(n *Node, b *block, cw float64) {
	sides := []string{"top", "right", "bottom", "left"}
	for i, s := range sides {
		b.p[i] = lc.lengthOr(n, "padding-"+s, cw, 0)
		b.b[i] = lc.borderOf(n, s, cw)
	}
	if c, ok := ParseColor(n.Props["background-color"]); ok {
		b.bg = c
	}
	if v := n.Props["background-image"]; v != "" && v != "none" {
		if im, err := lc.e.loadImage(v); err == nil {
			b.bgImg = im
			b.bgRepeat = n.Props["background-repeat"]
		} else {
			lc.e.warn("background image %q: %v", unwrapURL(v), err)
		}
	}
	b.spB = lc.spaces(n, "space-before")
	b.spA = lc.spaces(n, "space-after")
	b.kTogether = isKeep(n.Get("keep-together"))
	b.kNext = isKeep(n.Props["keep-with-next"])
	b.kPrev = isKeep(n.Props["keep-with-previous"])
	b.bBefore = parseBrk(n.Props["break-before"])
	b.bAfter = parseBrk(n.Props["break-after"])
	if v, err := strconv.Atoi(strings.TrimSpace(n.Get("orphans"))); err == nil {
		b.orphans = v
	}
	if v, err := strconv.Atoi(strings.TrimSpace(n.Get("widows"))); err == nil {
		b.widows = v
	}
	if id := n.Props["id"]; id != "" {
		b.ids = append(b.ids, id)
	}
	switch v := n.Props["display-align"]; v {
	case "center", "after":
		b.displayAlign = v
	}
}

// refAreaFO reports whether the FO generates a reference area (FOP:
// FObj.generatesReferenceAreas), which stops start-indent inheritance when the
// indent is derived from margins.
func refAreaFO(n *Node) bool {
	switch n.Name {
	case "block-container", "inline-container", "table-cell", "flow", "static-content":
		return true
	}
	return false
}

// indents derives start-indent/end-indent the way Apache FOP does
// (IndentPropertyMaker.computeConforming, XSL 1.1 section 5.3.2):
//
//   - an explicit margin-left/right is converted into the indent:
//     inherited indent (unless the FO generates a reference area) + margin + padding + border;
//   - otherwise an explicit start-indent/end-indent is used as is;
//   - otherwise (including padding/border without margin) the inherited value is
//     used, so padding and borders protrude outside the content rectangle.
//
// It also sets the border-box position of b relative to the parent content box.
func (lc *lctx) indents(n *Node, b *block) (S, E float64) {
	cw := lc.contentW()
	base, baseE := lc.S, lc.E
	if refAreaFO(n) {
		base, baseE = 0, 0
	}
	derive := func(abs string, forced bool, margin float64, pad, bw float64, inherited, parentBase float64, indent string) float64 {
		_, hasMargin := n.Props[abs]
		if forced || (hasMargin && !n.Has(indent)) {
			return parentBase + margin + pad + bw
		}
		if v, ok := lc.length(n, indent, cw); ok {
			return v
		}
		return inherited
	}
	ml := lc.lengthOr(n, "margin-left", cw, 0)
	mr := lc.lengthOr(n, "margin-right", cw, 0)
	S = derive("margin-left", n.Props["\x00ml"] != "", ml, b.p[3], b.b[3].w, lc.S, base, "start-indent")
	E = derive("margin-right", n.Props["\x00mr"] != "", mr, b.p[1], b.b[1].w, lc.E, baseE, "end-indent")
	b.left = S - b.p[3] - b.b[3].w - lc.S
	b.width = lc.refW - S - E + b.p[3] + b.b[3].w + b.p[1] + b.b[1].w
	if lc.refW-S-E < 1 {
		lc.e.warn("<fo:%s>: no horizontal room left (indents exceed the available width)", n.Name)
		E = lc.refW - S - 1
		b.width = 1 + b.p[3] + b.b[3].w + b.p[1] + b.b[1].w
	}
	return
}

// ---------- block-level dispatch ----------

func isBlockLevel(name string) bool {
	switch name {
	case "block", "block-container", "table", "table-and-caption", "list-block", "table-caption":
		return true
	}
	return false
}

func containsBlock(n *Node) bool {
	for _, c := range n.Children {
		if isBlockLevel(c.Name) {
			return true
		}
		switch c.Name {
		case "wrapper", "inline", "basic-link", "inline-container", "bidi-override":
			if containsBlock(c) {
				return true
			}
		}
	}
	return false
}

// layoutBlockLevel lays out one block-level FO node.
func (lc *lctx) layoutBlockLevel(n *Node) vitem {
	switch n.Name {
	case "block":
		return lc.layoutBlock(n)
	case "block-container":
		return lc.layoutBlockContainer(n)
	case "list-block":
		return lc.layoutList(n)
	case "table":
		return lc.layoutTable(n)
	case "table-and-caption":
		return lc.layoutTableAndCaption(n)
	case "table-caption":
		return lc.layoutBlock(n)
	}
	return nil
}

// ---------- block building ----------

type blockBuilder struct {
	lc      *lctx
	n       *Node
	items   []vitem
	ib      *inlineBuilder
	strut   *textStyle
	paraSeq int
	markers []*markerDef
}

func (lc *lctx) newBlockBuilder(n *Node) *blockBuilder {
	return &blockBuilder{lc: lc, n: n, ib: newInlineBuilder(lc), strut: lc.e.styleFor(n)}
}

func alignValue(v string) string {
	switch strings.TrimSpace(v) {
	case "left", "start", "inside", "":
		return "start"
	case "right", "end", "outside":
		return "end"
	case "center":
		return "center"
	case "justify":
		return "justify"
	}
	return "start"
}

func (bb *blockBuilder) flush() {
	ib := bb.ib
	atoms := ib.atoms
	ib.pending = false
	if !hasContent(atoms) && len(atoms) > 0 {
		// only anchors / spaces: keep anchors alive on a zero-height line
		var ids []string
		for _, a := range atoms {
			if a.kind == aAnchor {
				ids = append(ids, a.id)
			}
		}
		ib.reset()
		if len(ids) > 0 {
			bb.items = append(bb.items, &lineBox{ids: ids, w: bb.lc.contentW()})
		}
		return
	}
	if len(atoms) == 0 {
		ib.reset()
		return
	}
	lc := bb.lc
	n := bb.n
	st := bb.strut
	align := alignValue(n.Get("text-align"))
	// text-align-last is derived from text-align unless specified (foproperties.xml <derive>)
	alignLast := ""
	for c := n; c != nil; c = c.Parent {
		if v, ok := c.Props["text-align-last"]; ok && v != "inherit" {
			alignLast = v
			break
		}
	}
	switch alignLast {
	case "", "relative":
		if align == "justify" {
			alignLast = "start"
		} else {
			alignLast = align
		}
	default:
		alignLast = alignValue(alignLast)
	}
	indent := lc.lengthOr(n, "text-indent", lc.contentW(), 0)
	po := paraOpts{width: lc.contentW(), indent: indent, align: align, alignLast: alignLast, strut: st, nowrap: st.nowrap}
	// a preserved line feed ends a paragraph (FOP: separate Paragraph sequences)
	var segs [][]atom
	cur := []atom(nil)
	for _, a := range atoms {
		if a.kind == aBreak {
			segs = append(segs, cur)
			cur = nil
			continue
		}
		cur = append(cur, a)
	}
	if len(cur) > 0 || len(segs) == 0 {
		segs = append(segs, cur)
	}
	for si, seg := range segs {
		first := len(bb.items) == 0 && bb.paraSeq == 0
		var lines []*lineBox
		if !hasContent(seg) && len(segs) > 1 && si < len(segs) {
			if len(seg) == 0 || !hasContent(seg) {
				// an empty line produced by consecutive preserved line feeds
				lines = []*lineBox{lc.emptyLine(po)}
			}
		} else {
			lines = lc.paragraphLines(append([]atom(nil), seg...), po, first, nil)
		}
		bb.paraSeq++
		for _, l := range lines {
			l.paraID = bb.paraSeq
			bb.items = append(bb.items, l)
		}
	}
	ib.reset()
}

// emptyLine is a blank line (height of the block's strut).
func (lc *lctx) emptyLine(po paraOpts) *lineBox {
	lb := &lineBox{w: po.width}
	lb.h, lb.base = lineMetrics(po.strut, nil)
	return lb
}

func (bb *blockBuilder) process(children []*Node) {
	for _, c := range children {
		bb.node(c)
	}
}

func (bb *blockBuilder) anchor(c *Node) {
	if id := c.Props["id"]; id != "" {
		bb.ib.atoms = append(bb.ib.atoms, atom{kind: aAnchor, id: id})
	}
}

func (bb *blockBuilder) node(c *Node) {
	lc := bb.lc
	ib := bb.ib
	e := lc.e
	switch c.Name {
	case "#text":
		ib.addFOText(c, c.Text)
	case "block", "block-container", "table", "table-and-caption", "list-block":
		bb.flush()
		if it := lc.layoutBlockLevel(c); it != nil {
			bb.items = append(bb.items, it)
		}
	case "wrapper", "inline", "basic-link", "bidi-override", "inline-container", "fox:destination":
		bb.anchor(c)
		if containsBlock(c) {
			bb.flush()
			bb.process(c.Children)
			bb.flush()
			return
		}
		if c.Name == "inline" {
			if d := lc.inlineDecorOf(c); d != nil {
				st := e.styleFor(c)
				ib.atoms = append(ib.atoms, atom{kind: aOpen, w: d.p[3] + d.b[3].w, decor: d, style: st})
				bb.process(c.Children)
				ib.atoms = append(ib.atoms, atom{kind: aClose, w: d.p[1] + d.b[1].w, decor: d, style: st})
				return
			}
		}
		bb.process(c.Children)
	case "character":
		bb.anchor(c)
		if ch := c.Props["character"]; ch != "" {
			ib.addText(c, ch)
		}
	case "page-number":
		bb.anchor(c)
		ib.addText(c, fmtPage(lc.pageNum, lc.pageFmt))
	case "page-number-citation", "page-number-citation-last":
		bb.anchor(c)
		e.cited = true
		id := c.Props["ref-id"]
		if c.Name == "page-number-citation-last" {
			id += "\x00last"
		}
		text := "00"
		e.cites[id] = true
		if info, ok := e.prevIDs[id]; ok {
			text = info.pageNum
		}
		ib.addText(c, text)
	case "external-graphic":
		bb.anchor(c)
		if a, ok := lc.graphicAtom(c); ok {
			ib.addBox(a)
		}
	case "instream-foreign-object":
		e.warn("fo:instream-foreign-object is not supported (skipped)")
	case "leader":
		bb.anchor(c)
		ib.addBox(lc.leaderAtom(c))
	case "marker":
		bb.markers = append(bb.markers, &markerDef{class: c.Props["marker-class-name"], node: c})
	case "retrieve-marker":
		bb.anchor(c)
		if m := lc.findMarker(c); m != nil {
			clone := &Node{Name: "wrapper", Props: map[string]string{}, Parent: c}
			for _, ch := range m.Children {
				clone.Children = append(clone.Children, ch.cloneUnder(clone))
			}
			if containsBlock(clone) {
				bb.flush()
			}
			bb.process(clone.Children)
		}
	case "footnote":
		for _, ch := range c.Children {
			if ch.Name == "inline" {
				bb.node(ch)
			}
		}
		e.warn("fo:footnote bodies are not supported; only the reference is rendered")
	case "initial-property-set", "float":
		if c.Name == "float" {
			e.warn("fo:float is not supported (content rendered inline)")
			bb.process(c.Children)
		}
	default:
		if !strings.HasPrefix(c.Name, "fox:") {
			e.warn("unsupported flow object fo:%s ignored", c.Name)
		}
	}
}

func (lc *lctx) inlineDecorOf(c *Node) *inlineDecor {
	d := &inlineDecor{style: lc.e.styleFor(c)}
	any := false
	if col, ok := ParseColor(c.Props["background-color"]); ok && col.A > 0 {
		d.bg, any = col, true
	}
	for i, s := range []string{"top", "right", "bottom", "left"} {
		d.p[i] = lc.lengthOr(c, "padding-"+s, 0, 0)
		d.b[i] = lc.borderOf(c, s, 0)
		if d.p[i] > 0 || d.b[i].w > 0 {
			any = true
		}
	}
	if !any {
		return nil
	}
	return d
}

func (lc *lctx) findMarker(c *Node) *Node {
	class := c.Props["retrieve-class-name"]
	pos := c.Props["retrieve-position"]
	var found *Node
	for _, m := range lc.markers {
		if m.class != class {
			continue
		}
		if found == nil || strings.HasPrefix(pos, "last") {
			found = m.node
		}
		if found != nil && !strings.HasPrefix(pos, "last") {
			break
		}
	}
	// AbstractPageSequenceLayoutManager.resolveRetrieveMarker: unless the retrieve
	// boundary is "page", earlier pages are searched (last-ending-within-page)
	if found == nil && lc.carry != nil && c.Get("retrieve-boundary") != "page" {
		found = lc.carry[class]
	}
	return found
}

// ---------- fo:block ----------

func (lc *lctx) layoutBlock(n *Node) vitem {
	b := newBlock()
	cw := lc.contentW()
	lc.boxProps(n, b, cw)
	S, E := lc.indents(n, b)
	lc2 := *lc
	lc2.S, lc2.E = S, E
	bb := lc2.newBlockBuilder(n)
	bb.process(n.Children)
	bb.flush()
	b.children = bb.items
	b.markers = bb.markers
	if h, ok := lc.length(n, "block-progression-dimension.minimum", cw); ok {
		b.minH = h
	} else if h, ok := lc.length(n, "height", cw); ok {
		b.minH = h
	}
	b.fillGaps()
	return b
}

// ---------- fo:list-block ----------

func (lc *lctx) layoutList(n *Node) vitem {
	b := newBlock()
	cw := lc.contentW()
	lc.boxProps(n, b, cw)
	S, E := lc.indents(n, b)
	lc2 := *lc
	lc2.S, lc2.E = S, E
	lc2.listS = S
	lc2.listDist = lc.lengthOr(n, "provisional-distance-between-starts", cw, 24)
	lc2.listSep = lc.lengthOr(n, "provisional-label-separation", cw, 6)
	for _, c := range n.Children {
		switch c.Name {
		case "list-item":
			b.children = append(b.children, lc2.layoutListItem(c))
		case "block", "block-container", "table", "list-block":
			if it := lc2.layoutBlockLevel(c); it != nil {
				b.children = append(b.children, it)
			}
		}
	}
	b.fillGaps()
	return b
}

func (lc *lctx) layoutListItem(n *Node) vitem {
	b := newBlock()
	cw := lc.contentW()
	lc.boxProps(n, b, cw)
	S, E := lc.indents(n, b)
	lc2 := *lc
	lc2.S, lc2.E = S, E
	label, body := n.Find("list-item-label"), n.Find("list-item-body")
	var labelItem, bodyItem vitem
	if label != nil {
		labelItem = lc2.layoutBlock(label)
	}
	if body != nil {
		bodyItem = lc2.layoutBlock(body)
	}
	if bodyItem != nil {
		b.children = []vitem{bodyItem}
	}
	if labelItem != nil {
		b.overlays = []overlay{{item: labelItem}}
		lh := labelItem.height()
		if bodyItem == nil || lh > bodyItem.height() {
			b.minH = math.Max(b.minH, lh+b.topDeco()+b.botDeco())
		}
		if len(b.children) == 0 {
			b.children = []vitem{&spacer{h: lh}}
		}
	}
	b.fillGaps()
	return b
}

// spacer is an empty vertical item of fixed height.
type spacer struct{ h float64 }

func (s *spacer) height() float64                     { return s.h }
func (s *spacer) before() float64                     { return 0 }
func (s *spacer) after() float64                      { return 0 }
func (s *spacer) beforeSp() []spc                     { return nil }
func (s *spacer) afterSp() []spc                      { return nil }
func (s *spacer) keepNext() bool                      { return false }
func (s *spacer) keepPrev() bool                      { return false }
func (s *spacer) breakBefore() brk                    { return brkNone }
func (s *spacer) breakAfter() brk                     { return brkNone }
func (s *spacer) split(float64, bool) (vitem, vitem)  { return nil, nil }
func (s *spacer) forcedSplit() (vitem, vitem, brk)    { return nil, nil, brkNone }
func (s *spacer) render(*renderCtx, float64, float64) {}

func (l *lineBox) forcedSplit() (vitem, vitem, brk) { return nil, nil, brkNone }

// ---------- fo:block-container ----------

func (lc *lctx) layoutBlockContainer(n *Node) vitem {
	b := newBlock()
	cw := lc.contentW()
	lc.boxProps(n, b, cw)
	abs := n.Props["absolute-position"]
	if abs == "absolute" || abs == "fixed" {
		return lc.layoutAbsolute(n, abs == "fixed")
	}
	if rot := lc.orientation(n); rot != 0 {
		return lc.layoutRotated(n, rot, nil)
	}
	S, E := lc.indents(n, b)
	Sc, Ec := S, E // indent property values inherited by the container's children
	// explicit width narrows the container
	// width and height of a block-container are content dimensions in FOP
	deco := b.p[3] + b.b[3].w + b.p[1] + b.b[1].w
	if w, ok := lc.length(n, "width", cw); ok {
		b.width = w + deco
		b.fixedW = true
		E = lc.refW - S - w
	} else if w, ok := lc.length(n, "inline-progression-dimension", cw); ok {
		b.width = w + deco
		b.fixedW = true
		E = lc.refW - S - w
	}
	inner := b.width - deco
	if inner < 1 {
		inner = 1
	}
	_ = E
	// a block-container establishes a new reference area
	lc2 := &lctx{e: lc.e, refW: inner, refH: lc.refH, S: Sc, E: Ec, pageNum: lc.pageNum, pageFmt: lc.pageFmt, markers: lc.markers, carry: lc.carry}
	bb := lc2.newBlockBuilder(n)
	bb.process(n.Children)
	bb.flush()
	b.children = bb.items
	b.markers = bb.markers
	if h, ok := lc.length(n, "height", cw); ok {
		b.minH = h + b.topDeco() + b.botDeco()
		b.fixedH = true
		b.clipH = needClip(n)
	} else if h, ok := lc.length(n, "block-progression-dimension.minimum", cw); ok {
		b.minH = h
	} else if h, ok := lc.length(n, "block-progression-dimension", cw); ok {
		b.minH = h
	}
	if b.displayAlign == "" {
		if v := n.Props["display-align"]; v == "center" || v == "after" {
			b.displayAlign = v
		}
	}
	b.fillGaps()
	return b
}

// needClip: overflow values that clip the content of a block-container.
func needClip(n *Node) bool {
	switch n.Get("overflow") {
	case "hidden", "scroll", "error-if-overflow":
		return true
	}
	return false
}

func (lc *lctx) orientation(n *Node) int {
	v, ok := n.Props["reference-orientation"]
	if !ok {
		return 0
	}
	f, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return 0
	}
	f = ((f % 360) + 360) % 360
	return f
}

// absItem is a zero-height placeholder that draws an absolutely positioned container.
type absItem struct {
	blk    vitem
	w      float64
	x, y   float64
	fixed  bool
	right  bool
	bottom bool
	rx, ry float64
}

func (a *absItem) height() float64                    { return 0 }
func (a *absItem) before() float64                    { return 0 }
func (a *absItem) after() float64                     { return 0 }
func (a *absItem) beforeSp() []spc                    { return nil }
func (a *absItem) afterSp() []spc                     { return nil }
func (a *absItem) keepNext() bool                     { return false }
func (a *absItem) keepPrev() bool                     { return false }
func (a *absItem) breakBefore() brk                   { return brkNone }
func (a *absItem) breakAfter() brk                    { return brkNone }
func (a *absItem) split(float64, bool) (vitem, vitem) { return nil, nil }
func (a *absItem) forcedSplit() (vitem, vitem, brk)   { return nil, nil, brkNone }

func (a *absItem) render(r *renderCtx, _, _ float64) {
	reg := r.region
	if a.fixed {
		reg = rect{0, 0, r.pageW, r.pageH}
	}
	x := reg.x + a.x
	y := reg.y + a.y
	if a.right {
		x = reg.x + reg.w - a.rx - a.w
	}
	if a.bottom {
		y = reg.y + reg.h - a.ry - a.blk.height()
	}
	a.blk.render(r, x, y)
}

func (a *absItem) offsetLeft() float64 { return 0 }

func (lc *lctx) layoutAbsolute(n *Node, fixed bool) vitem {
	b := newBlock()
	regW, regH := lc.refW, lc.refH
	lc.boxProps(n, b, regW)
	var left, top, right, bottom *float64
	get := func(name string, base float64) *float64 {
		if v, ok := lc.length(n, name, base); ok {
			return &v
		}
		return nil
	}
	left, top = get("left", regW), get("top", regH)
	right, bottom = get("right", regW), get("bottom", regH)
	width := get("width", regW)
	if width == nil {
		width = get("inline-progression-dimension", regW)
	}
	var w float64
	deco := b.p[3] + b.b[3].w + b.p[1] + b.b[1].w
	switch {
	case width != nil:
		w = *width + deco // an explicit width is the content width
	case left != nil && right != nil:
		w = regW - *left - *right
	case left != nil:
		w = regW - *left
	case right != nil:
		w = regW - *right
	default:
		w = regW
	}
	if w < 1 {
		w = 1
	}
	if rot := lc.orientation(n); rot != 0 {
		a := &absItem{fixed: fixed}
		it := lc.layoutRotated(n, rot, &w)
		a.blk, a.w = it, it.(*rotItem).w
		if left != nil {
			a.x = *left
		} else if right != nil {
			a.right, a.rx = true, *right
		}
		if top != nil {
			a.y = *top
		} else if bottom != nil {
			a.bottom, a.ry = true, *bottom
		}
		return a
	}
	b.width = w
	b.left = 0
	b.noInset = true
	inner := w - deco
	lc2 := &lctx{e: lc.e, refW: math.Max(inner, 1), refH: regH, pageNum: lc.pageNum, pageFmt: lc.pageFmt, markers: lc.markers, carry: lc.carry}
	bb := lc2.newBlockBuilder(n)
	bb.process(n.Children)
	bb.flush()
	b.children = bb.items
	b.fillGaps()
	b.spB, b.spA = nil, nil
	if h := get("height", regH); h != nil {
		b.minH = *h + b.topDeco() + b.botDeco()
		b.fixedH = true
		b.clipH = needClip(n)
	} else if h := get("block-progression-dimension", regH); h != nil {
		b.minH = *h
	}
	a := &absItem{blk: b, w: b.width, fixed: fixed}
	if left != nil {
		a.x = *left
	} else if right != nil {
		a.right, a.rx = true, *right
	}
	if top != nil {
		a.y = *top
	} else if bottom != nil {
		a.bottom, a.ry = true, *bottom
	}
	return a
}

func (lc *lctx) layoutTableAndCaption(n *Node) vitem {
	b := newBlock()
	cw := lc.contentW()
	lc.boxProps(n, b, cw)
	S, E := lc.indents(n, b)
	lc2 := *lc
	lc2.S, lc2.E = S, E
	for _, c := range n.Children {
		if c.Name == "table-caption" {
			b.children = append(b.children, lc2.layoutBlock(c))
		} else if c.Name == "table" {
			b.children = append(b.children, lc2.layoutTable(c))
		}
	}
	b.fillGaps()
	return b
}

// ---------- images and leaders ----------

func (lc *lctx) graphicAtom(c *Node) (atom, bool) {
	e := lc.e
	src := c.Props["src"]
	if src == "" {
		return atom{}, false
	}
	im, err := e.loadImage(src)
	if err != nil {
		e.warn("image %q: %v", unwrapURL(src), err)
		return atom{}, false
	}
	cw := lc.contentW()
	ow, oh := im.wPt, im.hPt
	vw, vh := -1.0, -1.0
	if v, ok := lc.length(c, "width", cw); ok {
		vw = v
	} else if v, ok := lc.length(c, "inline-progression-dimension", cw); ok {
		vw = v
	}
	if v, ok := lc.length(c, "height", cw); ok {
		vh = v
	} else if v, ok := lc.length(c, "block-progression-dimension", cw); ok {
		vh = v
	}
	cwSpec := strings.TrimSpace(c.Props["content-width"])
	chSpec := strings.TrimSpace(c.Props["content-height"])
	uniform := c.Props["scaling"] != "non-uniform"
	var iw, ih float64 = ow, oh
	switch {
	case cwSpec == "scale-to-fit" || chSpec == "scale-to-fit":
		fw, fh := vw, vh
		if fw < 0 {
			fw = math.Inf(1)
		}
		if fh < 0 {
			fh = math.Inf(1)
		}
		if math.IsInf(fw, 1) && math.IsInf(fh, 1) {
			fw, fh = ow, oh
		}
		if uniform {
			s := math.Min(fw/ow, fh/oh)
			iw, ih = ow*s, oh*s
		} else {
			if !math.IsInf(fw, 1) {
				iw = fw
			}
			if !math.IsInf(fh, 1) {
				ih = fh
			}
		}
	default:
		var wset, hset bool
		if cwSpec != "" && cwSpec != "auto" {
			if v, ok := lc.parseCW(c, cwSpec, ow, cw); ok {
				iw, wset = v, true
			}
		}
		if chSpec != "" && chSpec != "auto" {
			if v, ok := lc.parseCW(c, chSpec, oh, cw); ok {
				ih, hset = v, true
			}
		}
		if wset && !hset && uniform {
			ih = oh * iw / ow
		} else if hset && !wset && uniform {
			iw = ow * ih / oh
		}
	}
	box := &imageBox{img: im, cw: iw, ch: ih, w: iw, h: ih}
	if vw >= 0 {
		box.w = vw
	}
	if vh >= 0 {
		box.h = vh
	}
	if box.w < iw-0.01 || box.h < ih-0.01 {
		box.clip = true
	}
	st := e.styleFor(c)
	return atom{kind: aBox, w: box.w, img: box, style: st, rise: e.graphicRise(c, box.h)}, true
}

// graphicRise is the height of the bottom of an inline graphic above the line's
// baseline, following AlignmentContext (altitude = height, depth = 0, x-height =
// height for a graphic; the parent's baseline table supplies the target).
func (e *Engine) graphicRise(c *Node, h float64) float64 {
	var pst *textStyle
	if c.Parent != nil {
		pst = e.styleFor(c.Parent)
	}
	if pst == nil || pst.ft == nil {
		return 0
	}
	va := c.Props["vertical-align"]
	ab, aa, bsv := c.Props["alignment-baseline"], c.Props["alignment-adjust"], c.Props["baseline-shift"]
	switch va {
	case "auto", "baseline", "before-edge", "text-before-edge", "middle", "central", "after-edge",
		"text-after-edge", "ideographic", "alphabetic", "hanging", "mathematical":
		if ab == "" {
			ab = va
		}
		if aa == "" {
			aa = va
		}
		if bsv == "" && va == "baseline" {
			bsv = va
		}
	case "sub", "super":
		if bsv == "" {
			bsv = va
		}
	default:
		if va != "" && bsv == "" && va != "top" && va != "bottom" && va != "text-top" && va != "text-bottom" {
			bsv = va
		}
	}
	alt, dep := pst.ascDescMpt()
	altF, depF := float64(alt)/1000, -float64(dep)/1000
	xh := float64(pst.xHeightMpt()) / 1000
	// offset of a baseline above the parent's alphabetic baseline
	parentBase := func(id string) float64 {
		switch id {
		case "text-before-edge", "before-edge":
			return altF
		case "middle":
			return xh / 2
		case "central":
			return (altF-depF)/2 + depF
		case "hanging":
			return math.Round(altF*1000*0.8) / 1000
		case "mathematical":
			return math.Round(altF*1000*0.5) / 1000
		case "ideographic", "text-after-edge", "after-edge":
			return depF
		}
		return 0
	}
	// the graphic's own table: altitude h, depth 0, x-height h
	ownBase := func(id string) float64 {
		switch id {
		case "text-before-edge", "before-edge":
			return h
		case "middle", "central", "mathematical":
			return h / 2
		case "hanging":
			return math.Round(h*1000*0.8) / 1000
		}
		return 0
	}
	if ab == "" || ab == "auto" {
		ab = "alphabetic"
	}
	if ab == "baseline" {
		ab = "alphabetic"
	}
	ap := h - ownBase(ab) // alignment-adjust="auto"
	switch aa {
	case "", "auto":
	case "baseline":
		ap = h
	case "before-edge", "text-before-edge", "middle", "central", "after-edge", "text-after-edge",
		"ideographic", "alphabetic", "hanging", "mathematical":
		ap = h - ownBase(aa)
	default:
		if v, ok := e.parseLength(c, aa, pst.lh); ok {
			ap = h + v
		}
	}
	shift := 0.0
	switch bsv {
	case "", "baseline":
	case "sub":
		shift = -h / 2
	case "super":
		shift = xh
	default:
		if v, ok := e.parseLength(c, bsv, pst.lh); ok {
			shift = v
		}
	}
	return parentBase(ab) + shift + ap - h
}

func (lc *lctx) parseCW(c *Node, spec string, intrinsic, cw float64) (float64, bool) {
	if strings.HasSuffix(spec, "%") {
		f, err := strconv.ParseFloat(strings.TrimSuffix(spec, "%"), 64)
		if err != nil {
			return 0, false
		}
		return intrinsic * f / 100, true
	}
	return lc.e.parseLength(c, spec, cw)
}

func (lc *lctx) leaderAtom(c *Node) atom {
	e := lc.e
	st := e.styleFor(c)
	cw := lc.contentW()
	ld := &leaderSpec{pattern: c.Props["leader-pattern"], thickness: 1, rstyle: "solid", style: st}
	if ld.pattern == "" {
		ld.pattern = "space"
	}
	ld.color = st.color
	if v, ok := lc.length(c, "rule-thickness", cw); ok {
		ld.thickness = v
	}
	if v := c.Props["rule-style"]; v != "" {
		ld.rstyle = v
	}
	get := func(name string, def float64) float64 {
		if v, ok := lc.length(c, name, cw); ok {
			return v
		}
		return def
	}
	// A bare leader-length="L" yields min = opt = max = L (LengthRangeProperty);
	// the component properties override it individually.
	opt, minW, maxW := 12.0, 0.0, cw
	if v, ok := lc.length(c, "leader-length", cw); ok {
		opt, minW, maxW = v, v, v
	}
	opt = get("leader-length.optimum", opt)
	minW = get("leader-length.minimum", minW)
	maxW = get("leader-length.maximum", maxW)
	// LeaderLayoutManager.getLeaderAllocIPD: MinOptMax(min, min(opt, max), max)
	ld.minW, ld.maxW = minW, maxW
	ld.optW = math.Min(opt, maxW)
	switch ld.pattern {
	case "dots":
		ld.patRuns = e.measure(st, ".")
		if len(ld.patRuns) > 0 {
			dot := runsWidth(ld.patRuns)
			ld.patW = math.Max(dot, get("leader-pattern-width", 0))
		}
	case "use-content":
		txt := c.TextContent()
		ld.patRuns = e.measure(st, txt)
		ld.patW = math.Max(runsWidth(ld.patRuns), get("leader-pattern-width", 0))
	}
	a := atom{kind: aLeader, leader: ld, style: st}
	a.w = ld.optW
	return a
}
