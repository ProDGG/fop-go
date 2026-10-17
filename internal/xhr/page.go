package xhr

import "math"

// PageBox is a page of the output.
type PageBox struct {
	style                       *Style
	top, bottom                 int
	paintingTop, paintingBottom int
	pageNo                      int
	outerPageWidth              int
	dims                        *[2]int
	info                        *PageInfo
	areas                       [8]*marginAreaContainer
}

type marginAreaContainer struct {
	area  *marginArea
	table *Box
	layer *Layer
}

type marginArea struct {
	names     []string
	direction int // 1 vertical, 2 horizontal
	dim       func(pb *PageBox, m *RectSet) (int, int)
	pos       func(pb *PageBox, m *RectSet) (int, int)
}

const (
	marginVertical   = 1
	marginHorizontal = 2
)

var marginAreaDefs = []*marginArea{
	{names: []string{"top-left-corner"}, direction: marginHorizontal,
		dim: func(pb *PageBox, m *RectSet) (int, int) { return int(m.Left), int(m.Top) },
		pos: func(pb *PageBox, m *RectSet) (int, int) { return 0, 0 }},
	{names: []string{"top-left", "top-center", "top-right"}, direction: marginHorizontal,
		dim: func(pb *PageBox, m *RectSet) (int, int) { return pb.contentWidth(), int(m.Top) },
		pos: func(pb *PageBox, m *RectSet) (int, int) { return int(m.Left), 0 }},
	{names: []string{"top-right-corner"}, direction: marginHorizontal,
		dim: func(pb *PageBox, m *RectSet) (int, int) { return int(m.Right), int(m.Top) },
		pos: func(pb *PageBox, m *RectSet) (int, int) { return pb.width() - int(m.Right), 0 }},
	{names: []string{"left-top", "left-middle", "left-bottom"}, direction: marginVertical,
		dim: func(pb *PageBox, m *RectSet) (int, int) { return int(m.Left), pb.contentHeight() },
		pos: func(pb *PageBox, m *RectSet) (int, int) { return 0, int(m.Top) }},
	{names: []string{"right-top", "right-middle", "right-bottom"}, direction: marginVertical,
		dim: func(pb *PageBox, m *RectSet) (int, int) { return int(m.Left), pb.contentHeight() },
		pos: func(pb *PageBox, m *RectSet) (int, int) { return pb.width() - int(m.Right), int(m.Top) }},
	{names: []string{"bottom-left-corner"}, direction: marginHorizontal,
		dim: func(pb *PageBox, m *RectSet) (int, int) { return int(m.Left), int(m.Bottom) },
		pos: func(pb *PageBox, m *RectSet) (int, int) { return 0, pb.height() - int(m.Bottom) }},
	{names: []string{"bottom-left", "bottom-center", "bottom-right"}, direction: marginHorizontal,
		dim: func(pb *PageBox, m *RectSet) (int, int) { return pb.contentWidth(), int(m.Bottom) },
		pos: func(pb *PageBox, m *RectSet) (int, int) { return int(m.Left), pb.height() - int(m.Bottom) }},
	{names: []string{"bottom-right-corner"}, direction: marginHorizontal,
		dim: func(pb *PageBox, m *RectSet) (int, int) { return int(m.Right), int(m.Bottom) },
		pos: func(pb *PageBox, m *RectSet) (int, int) {
			return pb.width() - int(m.Right), pb.height() - int(m.Bottom)
		}},
}

func createPageBox(lc *LayoutContext, pseudo string) *PageBox {
	pb := &PageBox{}
	pb.info = lc.doc.matcher.Page(lc.pageName, pseudo)
	pb.style = lc.doc.empty.Derive(pb.info.Style)
	pb.outerPageWidth = pb.width()
	return pb
}

func (pb *PageBox) resolveDims() {
	if pb.dims != nil {
		return
	}
	st := pb.style
	var w, h int
	if st.IsLength("-fs-page-width") {
		w = int(st.FloatProportional("-fs-page-width", 0))
	} else {
		w = int(st.calcLength("-fs-page-width", Value{Kind: VLength, Num: 210, Unit: "mm"}, 0))
	}
	if st.IsLength("-fs-page-height") {
		h = int(st.FloatProportional("-fs-page-height", 0))
	} else {
		h = int(st.calcLength("-fs-page-height", Value{Kind: VLength, Num: 297, Unit: "mm"}, 0))
	}
	if st.IsIdent("-fs-page-orientation", "landscape") {
		w, h = h, w
	}
	pb.dims = &[2]int{w, h}
}

func (pb *PageBox) width() int  { pb.resolveDims(); return pb.dims[0] }
func (pb *PageBox) height() int { pb.resolveDims(); return pb.dims[1] }

func (pb *PageBox) margin() *RectSet { return pb.style.MarginRectNoCache(float32(pb.outerPageWidth)) }

func (pb *PageBox) mbp(which int) int {
	return pb.style.MarginBorderPadding(pb.outerPageWidth, which)
}

func (pb *PageBox) contentHeight() int {
	r := pb.height() - pb.mbp(Top) - pb.mbp(Bottom)
	if r <= 0 {
		panic("xhr: the content height cannot be zero or less; check the @page margin definition")
	}
	return r
}

func (pb *PageBox) contentWidth() int {
	r := pb.width() - pb.mbp(Left) - pb.mbp(Right)
	if r <= 0 {
		panic("xhr: the content width cannot be zero or less; check the @page margin definition")
	}
	return r
}

func (pb *PageBox) setTopAndBottom(top int) {
	pb.top = top
	pb.bottom = top + pb.contentHeight()
}

func (pb *PageBox) isLeftPage() bool  { return pb.pageNo%2 != 0 }
func (pb *PageBox) isRightPage() bool { return pb.pageNo%2 == 0 }

// layout lays out the margin areas (page-level margin boxes).
func (pb *PageBox) layout(lc *LayoutContext) {
	lc.page = pb
	m := pb.margin()
	for i, area := range marginAreaDefs {
		w, h := area.dim(pb, m)
		table := lc.doc.createMarginTable(lc, pb.info, area.names, h, area.direction)
		if table == nil {
			continue
		}
		cb := newBox(KBlock)
		cb.contentWidth = w
		cb.height = h
		table.containingBlk = cb
		lc.noPageBreak = 1
		lc.reInit(false)
		lc.pushLayerBox(table)
		lc.rootLayer.addPage(lc)
		table.layoutBox(lc)
		lc.noPageBreak = 0
		pb.areas[i] = &marginAreaContainer{area: area, table: table, layer: lc.rootLayer}
	}
}

func (d *Doc) createMarginTable(lc *LayoutContext, pi *PageInfo, names []string, height, direction int) *Box {
	if !pi.HasAny(names) {
		return nil
	}
	source := d.root
	info := &childBoxInfo{}
	pageStyle := d.empty.Derive(pi.Style)
	tableStyle := pageStyle.Derive(map[string]*Declared{
		"display": {Name: "display", Vals: []Value{{Kind: VIdent, Str: "table"}}, Important: true},
		"width":   {Name: "width", Vals: []Value{{Kind: VPercent, Num: 100}}, Important: true},
	})
	result := createBlockBox(tableStyle, info, false)
	result.marginAreaRoot = true
	result.setStyle(tableStyle)
	result.Elem = source
	result.anonymous = true
	result.childrenType = ContentBlock

	secStyle := pageStyle.CreateAnonymousStyle("table-row-group")
	section := createBlockBox(secStyle, info, false)
	section.setStyle(secStyle)
	section.Elem = source
	section.anonymous = true
	section.childrenType = ContentBlock
	result.addChild(section)

	var row *Box
	newRow := func() *Box {
		rs := pageStyle.CreateAnonymousStyle("table-row")
		r := createBlockBox(rs, info, false)
		r.setStyle(rs)
		r.Elem = source
		r.anonymous = true
		r.childrenType = ContentBlock
		r.heightOverride = height
		section.addChild(r)
		return r
	}
	if direction == marginHorizontal {
		row = newRow()
	}
	cellCount := 0
	alwaysCreate := len(names) > 1 && direction == marginHorizontal
	for _, n := range names {
		cs := pi.MarginBoxStyle(n, alwaysCreate)
		if cs == nil {
			continue
		}
		cell := d.createMarginBox(lc, cs, alwaysCreate)
		if cell == nil {
			continue
		}
		if direction == marginVertical {
			row = newRow()
		}
		row.addChild(cell)
		cellCount++
	}
	if direction == marginVertical && cellCount > 0 {
		rh := 0
		for _, r := range section.children {
			r.heightOverride = height / cellCount
			rh += r.heightOverride
		}
		for _, r := range section.children {
			if rh >= height {
				break
			}
			r.heightOverride++
			rh++
		}
	}
	if cellCount > 0 {
		return result
	}
	return nil
}

func (d *Doc) createMarginBox(lc *LayoutContext, cs map[string]*Declared, alwaysCreate bool) *Box {
	hasContent := true
	contentDecl := cs["content"]
	style := d.empty.Derive(cs)
	if style.IsDisplayNone() && !alwaysCreate {
		return nil
	}
	if style.IsIdent("content", "none") || style.IsIdent("content", "normal") {
		hasContent = false
	}
	if style.IsAutoWidth() && !alwaysCreate && !hasContent {
		return nil
	}
	var children []Styleable
	info := &childBoxInfo{containsTable: true, layoutRunning: true}
	result := newBox(KCell)
	result.anonymous = true
	result.setStyle(style)
	result.Elem = d.root
	if hasContent && !style.IsDisplayNone() && contentDecl != nil {
		children = append(children, d.createGeneratedMarginBoxContent(lc, d.root, contentDecl.Vals, style, info)...)
		children = stripAllWhitespace(children)
	}
	if len(children) == 0 && style.IsAutoWidth() && !alwaysCreate {
		return nil
	}
	d.resolveChildTableContent(lc, result, children, info, "table-cell")
	return result
}

func (d *Doc) createGeneratedMarginBoxContent(lc *LayoutContext, e interface{}, content []Value, style *Style, info *childBoxInfo) []Styleable {
	res := d.createGeneratedContentList(lc, d.root, content, "", style, true, info)
	anon := style.CreateAnonymousStyle("inline")
	for _, s := range res {
		if ib, ok := s.(*InlineSrc); ok {
			ib.elem = nil
			ib.style = anon
			ib.applyTextTransform()
		}
	}
	return res
}

var _ = math.Floor
