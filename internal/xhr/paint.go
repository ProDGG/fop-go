package xhr

import (
	"math"
	"sort"

	"github.com/ProDGG/fop-go/internal/font"
	"github.com/ProDGG/fop-go/internal/pdf"
)

// Border side bit flags (BorderPainter).
const (
	sideTop    = 1
	sideLeft   = 2
	sideBottom = 4
	sideRight  = 8
	sideAll    = 15
)

type painter struct {
	d      *Doc
	lc     *LayoutContext
	pdoc   *pdf.Document
	page   *pdf.Page
	pageH  float64 // points
	dpp    float64
	tx, ty float64 // translation in dots
	clip   Rect    // clip in document coordinates

	fonts   map[*FontDesc]*pdf.Font
	imgs    map[*Image]*pdf.Image
	curPage *PageBox
	pageNo  int
	pageCnt int
	layer   *Layer

	linkSeen map[string]bool
	color    RGB
	fillSet  bool
}

func (p *painter) px(x float64) float64 { return float64(float32((x + p.tx) / p.dpp)) }
func (p *painter) py(y float64) float64 {
	return float64(float32(p.pageH) - float32((y+p.ty)/p.dpp))
}

func (p *painter) setFill(c *RGB) {
	if c == nil {
		return
	}
	p.page.FillRGB(float64(c.R)/255, float64(c.G)/255, float64(c.B)/255)
	p.color = *c
}

func (p *painter) fillPoly(c *RGB, pts [][2]float64) {
	if c == nil || len(pts) < 3 {
		return
	}
	p.setFill(c)
	p.page.MoveTo(p.px(pts[0][0]), p.py(pts[0][1]))
	for _, q := range pts[1:] {
		p.page.LineTo(p.px(q[0]), p.py(q[1]))
	}
	p.page.ClosePath()
	p.page.Fill()
}

func (p *painter) fillRectDots(c *RGB, x, y, w, h int) {
	if w == 0 || h == 0 {
		return
	}
	fx, fy, fw, fh := float64(x), float64(y), float64(w), float64(h)
	p.fillPoly(c, [][2]float64{{fx, fy}, {fx + fw, fy}, {fx + fw, fy + fh}, {fx, fy + fh}})
}

// ---- page ------------------------------------------------------------------------------

func (p *painter) paintPage(pb *PageBox, no, count int) {
	p.curPage = pb
	p.pageNo = no
	p.pageCnt = count
	pw := float64(float32(pb.width()) / float32(p.dpp))
	ph := float64(float32(pb.height()) / float32(p.dpp))
	p.page = p.pdoc.NewPage(pw, ph)
	p.pageH = ph
	p.tx, p.ty = 0, 0
	p.fillSet = false
	// page background (page-level background is rare; paint when set)
	if bg := pb.style.Color("background-color"); bg != nil {
		p.fillRectDots(bg, 0, 0, pb.width(), pb.height())
	}
	// margin areas
	m := pb.margin()
	for _, mc := range pb.areas {
		if mc == nil {
			continue
		}
		x, y := mc.area.pos(pb, m)
		p.tx, p.ty = float64(x), float64(y)
		p.clip = Rect{-1 << 30, -1 << 30, 1 << 31, 1 << 31}
		p.layer = mc.layer
		p.paintLayer(mc.table, mc.layer)
		p.tx, p.ty = 0, 0
	}
	// content
	left := pb.mbp(Left)
	top := -pb.paintingTop + pb.mbp(Top)
	p.page.Save()
	cx := float64(left)
	cy := float64(pb.mbp(Top))
	cw := float64(pb.contentWidth())
	ch := float64(pb.contentHeight() - 1)
	p.page.Rect(p.px0(cx), p.py0(cy+ch), cw/p.dpp, ch/p.dpp)
	p.page.Clip()
	p.tx, p.ty = float64(left), float64(top)
	p.clip = Rect{0, pb.paintingTop, pb.contentWidth(), pb.contentHeight() - 1}
	p.layer = p.d.mainLayer
	p.paintLayer(p.d.mainLayer.master, p.d.mainLayer)
	p.page.Restore()
}

func (p *painter) px0(x float64) float64 { return float64(float32(x / p.dpp)) }
func (p *painter) py0(y float64) float64 { return float64(float32(p.pageH) - float32(y/p.dpp)) }

// ---- layer painting ------------------------------------------------------------------------

type collected struct {
	blocks []*Box
	lines  []Painted
}

// Painted is anything in the inline content list.
type Painted interface{}

func (p *painter) intersectsBlock(b *Box) bool {
	return p.clip.Intersects(p.paintingClipEdge(b))
}

func (p *painter) paintingClipEdge(b *Box) Rect {
	switch b.Kind {
	case KLine:
		parent := b.parent
		if parent.Style.IsIdent("-fs-text-decoration-extent", "block") || b.justification != nil {
			return Rect{b.absX, b.absY + b.paintingTop, parent.absX + parent.tx + parent.contentWidth - b.absX, b.paintingHeight}
		}
		return Rect{b.absX, b.absY + b.paintingTop, b.contentWidth, b.paintingHeight}
	case KCell:
		if b.collapsedPaintingBorder != nil {
			return b.collapsedBorderBounds()
		}
	}
	return b.paintingBorderEdge()
}

func (p *painter) collect(container *Box, col *collected) {
	if container.Kind == KLine {
		if p.clip.Intersects(p.paintingClipEdge(container)) {
			col.lines = append(col.lines, container)
			for _, c := range container.children {
				p.addInline(c, col)
			}
		}
		return
	}
	if p.intersectsBlock(container) || container.Kind == KTable {
		col.blocks = append(col.blocks, container)
	}
	// prune subtrees that start below the visible area
	if container.absY >= p.clip.Y+p.clip.H && container.parent != nil && container.Kind != KTable {
		// children never start above their parent
		if container.Kind != KSection && container.Kind != KRow {
			return
		}
	}
	for _, c := range container.children {
		p.collect(c, col)
	}
}

func (p *painter) addInline(c *Box, col *collected) {
	col.lines = append(col.lines, c)
	if c.Kind == KInline {
		for _, o := range c.inlineChildren {
			if cb, ok := o.(*Box); ok {
				p.addInline(cb, col)
			}
		}
	}
}

func (p *painter) paintLayer(master *Box, layer *Layer) {
	p.paintRootBackground(master)
	col := &collected{}
	p.collect(master, col)
	p.paintBackgroundsAndBorders(col.blocks)
	for i := len(layer.floats) - 1; i >= 0; i-- {
		p.paintAsLayer(layer.floats[i])
	}
	p.paintListMarkers(col.blocks)
	p.paintInlineContent(col.lines)
	p.paintReplacedElements(col.blocks)
}

func (p *painter) paintAsLayer(start *Box) {
	col := &collected{}
	p.collect(start, col)
	p.paintBackgroundsAndBorders(col.blocks)
	p.paintListMarkers(col.blocks)
	p.paintInlineContent(col.lines)
	p.paintReplacedElements(col.blocks)
}

func (p *painter) paintRootBackground(master *Box) {
	if master.Style == nil || !master.isRoot() {
		return
	}
	bgBox := master
	if !hasBackground(master.Style) {
		if len(master.children) == 0 {
			return
		}
		bgBox = master.children[0]
		if bgBox.Style == nil {
			return
		}
	}
	if c := bgBox.Style.Color("background-color"); c != nil {
		pb := p.curPage
		p.fillRectDots(c, 0, pb.paintingTop, pb.contentWidth(), pb.contentHeight())
	}
}

func hasBackground(s *Style) bool {
	return !(s.IsIdent("background-color", "transparent") && s.IsIdent("background-image", "none"))
}

func (p *painter) paintBackgroundsAndBorders(blocks []*Box) {
	// collapsed table borders are painted when the last cell of a table has been painted
	cellsByTable := map[*Box][]*Box{}
	triggers := map[*Box]*Box{}
	for _, b := range blocks {
		if b.Kind == KCell && b.collapsedPaintingBorder != nil {
			t := b.table()
			cellsByTable[t] = append(cellsByTable[t], b)
			triggers[t] = b
		}
	}
	allSeen := map[*collapsedBorder]bool{}
	type side struct {
		cell *Box
		side int
	}
	bordersFor := map[*Box][]side{}
	for t, cells := range cellsByTable {
		var list []side
		for _, cell := range cells {
			add := func(cb *collapsedBorder, s int) {
				if cb.exists() && !allSeen[cb] {
					allSeen[cb] = true
					list = append(list, side{cell, s})
				}
			}
			add(cell.cbTop, sideTop)
			add(cell.cbRight, sideRight)
			add(cell.cbBottom, sideBottom)
			add(cell.cbLeft, sideLeft)
		}
		sort.SliceStable(list, func(i, j int) bool {
			a, b := list[i], list[j]
			va, vb := cbOfSide(a.cell, a.side), cbOfSide(b.cell, b.side)
			r := compareBorders(va, vb, true)
			if r == nil {
				return false
			}
			return r != va
		})
		bordersFor[triggers[t]] = list
	}
	for _, b := range blocks {
		p.paintBackground(b)
		p.paintBorder(b)
		if b.Kind == KCell && b.collapsedPaintingBorder != nil {
			for _, s := range bordersFor[b] {
				p.paintCollapsedBorder(s.cell, s.side)
			}
		}
	}
}

func cbOfSide(c *Box, s int) *collapsedBorder {
	switch s {
	case sideTop:
		return c.cbTop
	case sideRight:
		return c.cbRight
	case sideBottom:
		return c.cbBottom
	}
	return c.cbLeft
}

func (p *painter) paintBackground(b *Box) {
	switch b.Kind {
	case KSection, KRow:
		return
	}
	if !b.Style.IsVisible() {
		return
	}
	if b.Kind == KCell {
		if !p.cellPaintsBackgroundsAndBorders(b) {
			return
		}
		p.paintCellBackgroundStack(b)
		return
	}
	bounds := b.paintingBorderEdge()
	p.paintBackground0(b.Style, bounds, bounds)
	if b.Kind == KInline || b.Kind == KBlock || b.Kind == KAnon {
		p.processLink(b)
	}
}

func (p *painter) cellPaintsBackgroundsAndBorders(b *Box) bool {
	return b.Style.IsShowEmptyCells() || b.childrenType != ContentEmpty
}

func (p *painter) paintCellBackgroundStack(cell *Box) {
	bounds := cell.paintingBorderEdge()
	table := cell.table()
	row := cell.parent
	section := row.parent
	// column background
	if col := table.colElement(cell.col); col != nil {
		p.paintBackground0(col.style, bounds, bounds)
	}
	p.paintBackground0(section.Style, bounds, bounds)
	p.paintBackground0(row.Style, bounds, bounds)
	p.paintBackground0(cell.Style, bounds, bounds)
}

func (p *painter) paintBackground0(st *Style, bounds, container Rect) {
	bg := st.Color("background-color")
	if bg != nil {
		p.fillRectDots(bg, bounds.X, bounds.Y, bounds.W, bounds.H)
	}
	if st.first("background-image").Kind == VURI {
		p.paintBackgroundImage(st, bounds, container)
	}
}

func (p *painter) paintBackgroundImage(st *Style, bounds, container Rect) {
	uri := st.first("background-image").Str
	img := p.d.images.get(uri)
	if img == nil || img.width() == 0 || img.height() == 0 {
		return
	}
	// only the common no-scaling case is supported; tiles are clipped to the bounds
	p.page.Save()
	p.clipRect(bounds)
	pos := st.value("background-position").vals
	calc := func(v Value, boundsDim, imgDim int) int {
		if v.Kind == VPercent {
			pc := float32(v.Num) / 100
			return int(javaRound(float32(boundsDim)*pc - float32(imgDim)*pc))
		}
		return int(st.calcLength("background-position", v, 0))
	}
	xoff := container.X + calc(pos[0], container.W, img.width())
	yoff := container.Y + calc(pos[1], container.H, img.height())
	rep := st.Ident("background-repeat")
	hr := rep == "repeat" || rep == "repeat-x"
	vr := rep == "repeat" || rep == "repeat-y"
	adj := func(target, cur, dim int) int {
		r := cur
		if r > target {
			for r > target {
				r -= dim
			}
		} else if r < target {
			for r < target {
				r += dim
			}
			if r != target {
				r -= dim
			}
		}
		return r
	}
	switch {
	case !hr && !vr:
		p.drawImage(img, xoff, yoff)
	case hr && vr:
		for x := adj(bounds.X, xoff, img.width()); x < bounds.X+bounds.W; x += img.width() {
			for y := adj(bounds.Y, yoff, img.height()); y < bounds.Y+bounds.H; y += img.height() {
				p.drawImage(img, x, y)
			}
		}
	case hr:
		for x := adj(bounds.X, xoff, img.width()); x < bounds.X+bounds.W; x += img.width() {
			p.drawImage(img, x, yoff)
		}
	default:
		for y := adj(bounds.Y, yoff, img.height()); y < bounds.Y+bounds.H; y += img.height() {
			p.drawImage(img, xoff, y)
		}
	}
	p.page.Restore()
	p.fillSet = false
}

func (p *painter) clipRect(r Rect) {
	p.page.Rect(p.px(float64(r.X)), p.py(float64(r.Y+r.H)), float64(r.W)/p.dpp, float64(r.H)/p.dpp)
	p.page.Clip()
}

func (p *painter) paintBorder(b *Box) {
	switch b.Kind {
	case KSection, KRow:
		return
	}
	if !b.Style.IsVisible() && b.Kind != KInline {
		return
	}
	if b.Kind == KCell {
		if !p.cellPaintsBackgroundsAndBorders(b) || b.collapsedPaintingBorder != nil {
			return
		}
	}
	if b.Kind == KInline && !b.Style.IsVisible() {
		return
	}
	sides := sideAll
	if b.Kind == KInline {
		sides = sideTop + sideBottom
		if b.startsHere {
			sides += sideLeft
		}
		if b.endsHere {
			sides += sideRight
		}
	}
	edge := b.paintingBorderEdge()
	p.paintBorders(edge, sides, b.getBorder(), true)
}

func (p *painter) paintCollapsedBorder(cell *Box, side int) {
	p.paintBorders(cell.collapsedBorderBounds(), side, cell.collapsedPaintingBorder, false)
}

func (b *BorderSet) noSide(side int) bool {
	switch side {
	case sideTop:
		return b.TopStyle == "none" || int(b.Top) == 0
	case sideLeft:
		return b.LeftStyle == "none" || int(b.Left) == 0
	case sideBottom:
		return b.BottomStyle == "none" || int(b.Bottom) == 0
	}
	return b.RightStyle == "none" || int(b.Right) == 0
}

func (p *painter) paintBorders(bounds Rect, sides int, border *BorderSet, bevel bool) {
	for _, s := range []int{sideTop, sideLeft, sideBottom, sideRight} {
		if sides&s == s && border.noSide(s) {
			sides -= s
		}
	}
	for _, s := range []int{sideTop, sideBottom, sideLeft, sideRight} {
		if sides&s != s {
			continue
		}
		var col *RGB
		var style string
		switch s {
		case sideTop:
			col, style = border.TopColor, border.TopStyle
		case sideBottom:
			col, style = border.BottomColor, border.BottomStyle
		case sideLeft:
			col, style = border.LeftColor, border.LeftStyle
		default:
			col, style = border.RightColor, border.RightStyle
		}
		if col == nil {
			continue
		}
		p.paintBorderSide(bounds, border, s, style, col)
	}
}

// sidePoly returns the mitred polygon for a border side in device dots.
func sidePoly(bounds Rect, border *BorderSet, side int, scaledOffset float32, widthScale float32) [][2]float64 {
	bx, by, bw, bh := float64(bounds.X), float64(bounds.Y), float64(bounds.W), float64(bounds.H)
	var top, left, right float64
	rot := 0
	switch side {
	case sideTop:
		top, left, right = float64(border.Top), float64(border.Left), float64(border.Right)
	case sideRight:
		top, left, right = float64(border.Right), float64(border.Top), float64(border.Bottom)
		rot = 1
	case sideBottom:
		top, left, right = float64(border.Bottom), float64(border.Right), float64(border.Left)
		rot = 2
	default:
		top, left, right = float64(border.Left), float64(border.Bottom), float64(border.Top)
		rot = 3
	}
	top *= float64(widthScale)
	left *= float64(widthScale)
	right *= float64(widthScale)
	k := float64(1 + scaledOffset)
	swapped := rot == 1 || rot == 3
	var sideWidth float64
	if swapped {
		sideWidth = bh - k*float64(widthScale)*float64(border.Top+border.Bottom)
	} else {
		sideWidth = bw - k*float64(widthScale)*float64(border.Left+border.Right)
	}
	// polygon in the unrotated frame (origin at the inner top-left of the side)
	pts := [][2]float64{{-left, -top}, {sideWidth + right, -top}, {sideWidth, 0}, {0, 0}}
	var tx, ty float64
	if !swapped {
		tx = -bw/2 + k*left
	} else {
		tx = -bh/2 + k*left
	}
	if swapped {
		ty = -bw/2 + k*top
	} else {
		ty = -bh/2 + k*top
	}
	cx, cy := bw/2+bx, bh/2+by
	ang := float64(rot) * math.Pi / 2
	s, c := math.Sin(ang), math.Cos(ang)
	out := make([][2]float64, len(pts))
	for i, q := range pts {
		x, y := q[0]+tx, q[1]+ty
		xr := x*c - y*s
		yr := x*s + y*c
		out[i] = [2]float64{xr + cx, yr + cy}
	}
	return out
}

func (p *painter) paintBorderSide(bounds Rect, border *BorderSet, side int, style string, col *RGB) {
	switch style {
	case "solid":
		p.fillPoly(col, sidePoly(bounds, border, side, 0, 1))
	case "double":
		p.fillPoly(col, sidePoly(bounds, border, side, 0, 1.0/3))
		p.fillPoly(col, sidePoly(bounds, border, side, 2, 1.0/3))
	case "inset", "outset", "groove", "ridge":
		dark := darken(col)
		hi, lo := col, dark
		switch style {
		case "inset":
			hi, lo = dark, col
		}
		c := hi
		if side == sideBottom || side == sideRight {
			c = lo
		}
		p.fillPoly(c, sidePoly(bounds, border, side, 0, 1))
	case "dashed", "dotted":
		var thick int
		switch side {
		case sideTop:
			thick = int(border.Top)
		case sideBottom:
			thick = int(border.Bottom)
		case sideLeft:
			thick = int(border.Left)
		default:
			thick = int(border.Right)
		}
		on, off := float64(thick), float64(thick)
		if style == "dashed" {
			on, off = float64(8+thick*2), float64(4+thick)
		}
		p.page.Save()
		poly := sidePoly(bounds, border, side, 0, 1)
		p.page.MoveTo(p.px(poly[0][0]), p.py(poly[0][1]))
		for _, q := range poly[1:] {
			p.page.LineTo(p.px(q[0]), p.py(q[1]))
		}
		p.page.ClosePath()
		p.page.Clip()
		line := sidePoly(bounds, border, side, 0.5, 1)
		p.page.StrokeRGB(float64(col.R)/255, float64(col.G)/255, float64(col.B)/255)
		p.page.LineWidth(float64(2*thick) / p.dpp)
		p.page.DashArray([]float64{on / p.dpp, off / p.dpp}, 0)
		p.page.MoveTo(p.px((line[0][0]+line[3][0])/2), p.py((line[0][1]+line[3][1])/2))
		p.page.LineTo(p.px((line[1][0]+line[2][0])/2), p.py((line[1][1]+line[2][1])/2))
		p.page.Stroke()
		p.page.Restore()
		p.fillSet = false
	}
}

func darken(c *RGB) *RGB {
	f := func(v uint8) uint8 { return uint8(float64(v) * 0.7) }
	return &RGB{f(c.R), f(c.G), f(c.B)}
}

// ---- list markers ------------------------------------------------------------------------------

func (p *painter) paintListMarkers(blocks []*Box) {
	for _, b := range blocks {
		if b.Style.IsVisible() && b.Style.IsListItem() && b.markerData != nil {
			p.paintListMarker(b)
		}
	}
}

func (p *painter) refX(b *Box) int {
	md := b.markerData
	if md.referenceLine != nil {
		return md.referenceLine.absX
	}
	return b.absX + int(b.getMargin().Left)
}

func (p *painter) refBaseline(b *Box) int {
	md := b.markerData
	if md.referenceLine != nil {
		return md.referenceLine.absY + md.strut.Baseline
	}
	return b.absY + b.ty + md.strut.Baseline
}

func (p *painter) paintListMarker(b *Box) {
	md := b.markerData
	st := b.Style
	col := st.Color("color")
	switch {
	case md.imageMarker != nil:
		img := md.imageMarker.img
		x := p.refX(b) - md.imageMarker.layoutWidth + (md.imageMarker.layoutWidth/2 - img.width()/2)
		y := int(float32(p.refBaseline(b)) - md.strut.Ascent/2 - float32(img.height())/2)
		p.drawImage(img, x, y)
	case md.glyphMarker != nil:
		gm := md.glyphMarker
		x := p.refX(b) - gm.layoutWidth
		y := p.refBaseline(b) - int(md.strut.Ascent)/2 - gm.diameter/2
		switch st.Ident("list-style-type") {
		case "disc":
			p.fillOval(col, x, y, gm.diameter, gm.diameter)
		case "square":
			p.fillRectDots(col, x, y, gm.diameter, gm.diameter)
		case "circle":
			p.strokeOval(col, x, y, gm.diameter, gm.diameter)
		}
	case md.textMarker != nil:
		tm := md.textMarker
		x := p.refX(b) - tm.layoutWidth
		y := p.refBaseline(b)
		p.drawString(st.FSFont(), col, tm.text, float64(x), float64(y), nil)
	}
}

func (p *painter) ovalPath(x, y, w, h float64) {
	const k = 0.5522847498
	rx, ry := w/2, h/2
	cx, cy := x+rx, y+ry
	pg := p.page
	pg.MoveTo(p.px(cx+rx), p.py(cy))
	pg.CurveTo(p.px(cx+rx), p.py(cy+k*ry), p.px(cx+k*rx), p.py(cy+ry), p.px(cx), p.py(cy+ry))
	pg.CurveTo(p.px(cx-k*rx), p.py(cy+ry), p.px(cx-rx), p.py(cy+k*ry), p.px(cx-rx), p.py(cy))
	pg.CurveTo(p.px(cx-rx), p.py(cy-k*ry), p.px(cx-k*rx), p.py(cy-ry), p.px(cx), p.py(cy-ry))
	pg.CurveTo(p.px(cx+k*rx), p.py(cy-ry), p.px(cx+rx), p.py(cy-k*ry), p.px(cx+rx), p.py(cy))
	pg.ClosePath()
}

func (p *painter) fillOval(c *RGB, x, y, w, h int) {
	if c == nil {
		return
	}
	p.setFill(c)
	p.ovalPath(float64(x), float64(y), float64(w), float64(h))
	p.page.Fill()
}

func (p *painter) strokeOval(c *RGB, x, y, w, h int) {
	if c == nil {
		return
	}
	p.page.StrokeRGB(float64(c.R)/255, float64(c.G)/255, float64(c.B)/255)
	p.page.LineWidth(1 / 1.0)
	p.ovalPath(float64(x), float64(y), float64(w), float64(h))
	p.page.Stroke()
}

// ---- inline content ---------------------------------------------------------------------------------

func (p *painter) paintInlineContent(lines []Painted) {
	for _, l := range lines {
		b := l.(*Box)
		switch b.Kind {
		case KLine:
			p.paintLineBox(b)
		case KInline:
			p.paintInlineBox(b)
		default:
			// inline-block / inline-table
			p.paintAsLayer(b)
		}
	}
}

func (p *painter) paintLineBox(l *Box) {
	if !l.parent.Style.IsVisible() {
		return
	}
	if l.containsDynamicFn {
		p.updateDynamicFunctions(l)
		total := positionHorizontallyBox(l, 0)
		l.contentWidth = total
		l.calcChildLocations()
		l.align(true)
	}
	if l.textDecorations != nil {
		p.drawLineDecorations(l)
	}
}

func (p *painter) updateDynamicFunctions(l *Box) {
	for _, c := range l.children {
		if c.Kind == KInline {
			p.lookForDynamicFunctions(c)
		}
	}
}

func (p *painter) lookForDynamicFunctions(b *Box) {
	for _, o := range b.inlineChildren {
		switch c := o.(type) {
		case *InlineText:
			if c.fn != nil {
				p.updateDynamicValue(c)
			}
		case *Box:
			if c.Kind == KInline {
				p.lookForDynamicFunctions(c)
			}
		}
	}
}

func (p *painter) updateDynamicValue(t *InlineText) {
	var v int
	switch t.fn.kind {
	case "page":
		v = p.lc.rootDocLayer.relativePageNo(p.lc, p.pageNo, p.curPage) + 1
	default:
		v = p.lc.rootDocLayer.relativePageCount(p.lc, p.pageCnt, p.curPage)
	}
	val := createCounterText(t.fn.style, v)
	t.masterText = []rune(val)
	t.start = 0
	t.end = len(t.masterText)
	t.width = t.parent.Style.FSFont().Width(val)
}

func (p *painter) drawLineDecorations(l *Box) {
	col := l.Style.Color("color")
	parent := l.parent
	for _, d := range l.textDecorations {
		if parent.Style.IsIdent("-fs-text-decoration-extent", "block") {
			p.fillRectDots(col, l.absX, l.absY+d.Offset, parent.absX+parent.tx+parent.contentWidth-l.absX, d.Thickness)
		} else {
			p.fillRectDots(col, l.absX, l.absY+d.Offset, l.contentWidth, d.Thickness)
		}
	}
}

func (p *painter) paintInlineBox(b *Box) {
	if !b.Style.IsVisible() {
		return
	}
	p.paintBackground(b)
	p.paintBorder(b)
	col := b.Style.Color("color")
	for _, d := range b.textDecorations {
		if d.Ident == "underline" || d.Ident == "overline" {
			edge := b.contentAreaEdge(b.absX, b.absY)
			p.fillRectDots(col, edge.X, b.absY+d.Offset, edge.W, d.Thickness)
		}
	}
	for _, o := range b.inlineChildren {
		if t, ok := o.(*InlineText); ok {
			p.drawText(t)
		}
	}
	for _, d := range b.textDecorations {
		if d.Ident == "line-through" {
			edge := b.contentAreaEdge(b.absX, b.absY)
			p.fillRectDots(col, edge.X, b.absY+d.Offset, edge.W, d.Thickness)
		}
	}
}

func (p *painter) drawText(t *InlineText) {
	iB := t.parent
	s := t.substring()
	if len(s) == 0 {
		return
	}
	var info *JustificationInfo
	if iB.Style.IsTextJustify() {
		info = iB.lineBoxOf().justification
	}
	p.drawString(iB.Style.FSFont(), iB.Style.Color("color"), string(s), float64(iB.absX+t.x), float64(iB.absY+iB.lineBaseline), info)
}

func (p *painter) pdfFont(fd *FontDesc) *pdf.Font {
	if f, ok := p.fonts[fd]; ok {
		return f
	}
	f := p.pdoc.UseFont(fd.Face)
	p.fonts[fd] = f
	return f
}

func (p *painter) drawString(f *FSFont, col *RGB, s string, x, y float64, info *JustificationInfo) {
	if len(s) == 0 || f == nil {
		return
	}
	if col != nil {
		p.setFill(col)
	}
	pf := p.pdfFont(f.Desc)
	size := float64(f.Size) / p.dpp
	rs := []rune(s)
	var glyphs []pdf.Glyph
	for i, r := range rs {
		if !f.Desc.HasGlyph(r) {
			continue
		}
		g := pdf.Glyph{GID: f.Desc.Face.GlyphIndex(r), Rune: r}
		if info != nil && i != len(rs)-1 {
			off := info.NonSpaceAdjust
			if r == ' ' || r == ' ' || r == '　' {
				off = info.SpaceAdjust
			}
			g.Extra = float64(off) / p.dpp
		}
		glyphs = append(glyphs, g)
	}
	// the text origin
	p.page.Text(pf, size, p.px(x), p.py(y), glyphs)
}

func (p *painter) drawImage(img *Image, x, y int) {
	if img.height() <= 0 || img.width() <= 0 {
		return
	}
	px := p.px(float64(x))
	py := p.py(float64(y + img.height()))
	w := float64(img.width()) / p.dpp
	h := float64(img.height()) / p.dpp
	if img.svg != nil {
		p.page.Save()
		img.svg.Draw(p.page, px, py, w, h)
		p.page.Restore()
		p.fillSet = false
		return
	}
	pi, ok := p.imgs[img]
	if !ok {
		key := img.key
		cp := *img.pdfImg
		pi = p.pdoc.AddImage(key, &cp)
		p.imgs[img] = pi
	}
	p.page.DrawImage(pi, px, py, w, h)
}

func (p *painter) paintReplacedElements(blocks []*Box) {
	for _, b := range blocks {
		if b.replaced != nil {
			ca := b.contentAreaEdge(b.absX, b.absY)
			p.drawImage(b.replaced.img, ca.X, ca.Y)
		}
	}
}

// ---- links ---------------------------------------------------------------------------------------------------

func (p *painter) processLink(b *Box) {
	e := b.Elem
	if e == nil || e.Name != "a" {
		return
	}
	href, ok := e.AttrOK("href")
	if !ok {
		return
	}
	if len(href) > 1 && href[0] == '#' {
		target := p.d.boxIDs[href[1:]]
		if target == nil {
			return
		}
		area, ok := p.linkArea(b)
		if !ok {
			return
		}
		pb := p.d.mainLayer.pageAt(p.lc, p.getPageRefY(target))
		dist := pb.mbp(Top) + target.absY + int(target.getMargin().Top) - pb.top
		destY := float64(pb.height())/p.dpp - float64(dist)/p.dpp
		p.page.AddDestLink(area[0], area[1], area[2], area[3], pb.pageNo, destY)
		return
	}
	boxTop := b.absY
	boxBottom := boxTop + b.height
	if boxTop < p.curPage.bottom && boxBottom > p.curPage.top {
		area, ok := p.linkArea(b)
		if !ok {
			return
		}
		p.page.AddURILink(area[0], area[1], area[2], area[3], href)
	}
}

func (p *painter) getPageRefY(b *Box) int {
	if b.Kind == KInline {
		return b.absY + b.lineBaseline
	}
	return b.absY
}

// linkArea returns x, y, w, h in PDF points of the union of sibling boxes of the same element.
func (p *painter) linkArea(box *Box) ([4]float64, bool) {
	cur := box
	for {
		prev := cur.previousSibling()
		if prev == nil || prev.Elem != box.Elem {
			break
		}
		cur = prev
	}
	area := p.localTargetArea(cur)
	for n := cur.nextSibling(); n != nil && n.Elem == box.Elem; n = n.nextSibling() {
		a := p.localTargetArea(n)
		area = unionArea(area, a)
	}
	key := areaKey(area)
	if p.linkSeen[key] {
		return area, false
	}
	p.linkSeen[key] = true
	return area, true
}

func areaKey(a [4]float64) string {
	return pdf.Num(a[0]) + ":" + pdf.Num(a[1]) + ":" + pdf.Num(a[2]) + ":" + pdf.Num(a[3])
}

func unionArea(a, b [4]float64) [4]float64 {
	llx := math.Min(a[0], b[0])
	lly := math.Min(a[1], b[1])
	urx := math.Max(a[0]+a[2], b[0]+b[2])
	ury := math.Max(a[1]+a[3], b[1]+b[3])
	return [4]float64{llx, lly, urx - llx, ury - lly}
}

func (p *painter) localTargetArea(b *Box) [4]float64 {
	bounds := b.contentAreaEdge(b.absX, b.absY)
	x := p.px(float64(bounds.X))
	y := p.py(float64(bounds.Y + bounds.H))
	return [4]float64{x, y, float64(bounds.W) / p.dpp, float64(bounds.H) / p.dpp}
}

var _ = font.Parse
