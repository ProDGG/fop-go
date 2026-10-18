package xhr

import (
	"github.com/ProDGG/fop-go/xmldom"
)

// Kind classifies a Box (Flying Saucer's Box subclasses).
type Kind int

const (
	KBlock Kind = iota
	KAnon
	KTable
	KSection
	KRow
	KCell
	KLine
	KInline // InlineLayoutBox
)

// Content types of a block box.
const (
	ContentUnknown = 0
	ContentInline  = 1
	ContentBlock   = 2
	ContentEmpty   = 4
)

const noBaseline = -1 << 31

// Rect is an integer rectangle in dots.
type Rect struct{ X, Y, W, H int }

func (r Rect) Translate(dx, dy int) Rect { r.X += dx; r.Y += dy; return r }
func (r Rect) Intersects(o Rect) bool {
	return r.W > 0 && r.H > 0 && o.W > 0 && o.H > 0 &&
		o.X < r.X+r.W && o.X+o.W > r.X && o.Y < r.Y+r.H && o.Y+o.H > r.Y
}
func (r Rect) Contains(x, y int) bool { return x >= r.X && y >= r.Y && x < r.X+r.W && y < r.Y+r.H }

// Styleable is something that has a style: *InlineSrc or *Box.
type Styleable interface {
	GetStyle() *Style
}

// InlineSrc is Flying Saucer's InlineBox: a run of text (or an inline element
// start/end marker) before line layout.
type InlineSrc struct {
	elem            *xmldom.Node
	originalText    string
	text            string
	removableWS     bool
	startsHere      bool
	endsHere        bool
	style           *Style
	contentFunction *contentFunc
	pseudo          string
	textNode        *xmldom.Node

	minMaxCalculated bool
	maxWidth         int
	minWidth         int
	firstLineWidth   int
}

func (s *InlineSrc) GetStyle() *Style { return s.style }

// contentFunc is a dynamic content function (page counters).
type contentFunc struct {
	kind  string // "page" or "pages"
	style string // list style type
}

// InlineText is a run of laid-out text inside an InlineLayoutBox.
type InlineText struct {
	parent          *Box
	x               int
	masterText      []rune
	start, end      int
	width           int
	fn              *contentFunc
	containedLF     bool
	trimmedLeading  bool
	trimmedTrailing bool
	textNode        *xmldom.Node
}

func (t *InlineText) isEmpty() bool { return t.start == t.end && !t.containedLF }

func (t *InlineText) substring() []rune { return t.masterText[t.start:t.end] }

func (t *InlineText) setSubstring(start, end int) {
	t.start, t.end = start, end
	if t.end > 0 && t.masterText[t.end-1] == '\n' {
		t.containedLF = true
		t.end--
	}
}

// floatedData is FloatedBoxData.
type floatedData struct {
	manager           *FloatManager
	drawingLayer      *Layer
	marginFromSibling int
}

// Box is the union of all Flying Saucer box classes.
type Box struct {
	Kind  Kind
	Style *Style
	Elem  *xmldom.Node

	x, y, absX, absY  int
	contentWidth      int
	leftMBP, rightMBP int
	height            int
	tx, ty            int

	parent          *Box
	children        []*Box
	index           int
	layer           *Layer // layer mastered by this box
	containingLayer *Layer
	containingBlk   *Box
	relativeOffset  *[2]int
	workingMargin   *RectSet
	pseudo          string
	anonymous       bool

	// BlockBox
	markerData       *MarkerData
	listCounter      int
	persistentBFC    *FloatManager
	staticEquivalent *Box
	needPageClear    bool
	replaced         *Replaced
	childrenType     int
	inlineContent    []Styleable
	topMarginCalc    bool
	bottomMarginCalc bool
	pendingCollapse  *marginCollapseResult
	minWidth         int
	maxWidth         int
	minMaxCalculated bool
	dimensionsCalc   bool
	needShrinkToFit  bool
	floated          *floatedData
	childrenHeight   int
	fromCaptionedTbl bool
	openInlineBoxes  []*InlineSrc // AnonymousBlockBox
	fixedRect        *Rect        // viewport / margin box pseudo containing block extent
	bottomSet        bool
	bottomMemo       int
	isInitialCB      bool // viewport / page area pseudo containing block
	marginAreaRoot   bool

	// TableBox
	columns       []*colData
	columnPos     []int
	tableLayout   tableLayout
	styleColumns  []*TableColumn
	pageClearance int
	extraTop      int
	extraBottom   int

	// TableSectionBox
	grid              [][]*Box
	needCellWidthCalc bool
	needCellRecalc    bool
	footer, header    bool

	// TableRowBox
	baseline       int
	haveBaseline   bool
	heightOverride int

	// TableCellBox
	row, col                         int
	collapsedLayoutBorder            *BorderSet
	collapsedPaintingBorder          *BorderSet
	cbTop, cbRight, cbBottom, cbLeft *collapsedBorder

	// LineBox
	endsOnNL           bool
	containsContent    bool
	containsBlockLevel bool
	floatDistances     *FloatDistances
	floatDistFn        func() *FloatDistances
	textDecorations    []*TextDecoration
	paintingTop        int
	paintingHeight     int
	nonFlowContent     []*Box
	containsDynamicFn  bool
	contentStart       int
	justification      *JustificationInfo
	lineBaseline       int

	// InlineLayoutBox
	startsHere     bool
	endsHere       bool
	inlineChildren []interface{} // *Box or *InlineText
	pending        bool
	inlineWidth    int
	cbWidth        int // containing block width for inline boxes
}

// GetStyle implements Styleable.
func (b *Box) GetStyle() *Style { return b.Style }

// FloatDistances holds left/right float offsets for a line.
type FloatDistances struct{ Left, Right int }

// TextDecoration is a pending decoration line.
type TextDecoration struct {
	Ident     string
	Offset    int
	Thickness int
}

// JustificationInfo is the extra space distributed on a justified line.
type JustificationInfo struct {
	NonSpaceAdjust, SpaceAdjust float32
}

// MarkerData describes a list marker.
type MarkerData struct {
	strut                      StrutMetrics
	textMarker                 *textMarker
	glyphMarker                *glyphMarker
	imageMarker                *imageMarker
	referenceLine, prevRefLine *Box
}

type textMarker struct {
	text        string
	layoutWidth int
}
type glyphMarker struct{ diameter, layoutWidth int }
type imageMarker struct {
	img         *Image
	layoutWidth int
}

func (m *MarkerData) layoutWidth() int {
	switch {
	case m.textMarker != nil:
		return m.textMarker.layoutWidth
	case m.glyphMarker != nil:
		return m.glyphMarker.layoutWidth
	case m.imageMarker != nil:
		return m.imageMarker.layoutWidth
	}
	return 0
}

func (m *MarkerData) setReferenceLine(l *Box) {
	m.prevRefLine = m.referenceLine
	m.referenceLine = l
}

func (m *MarkerData) restorePreviousReferenceLine(cur *Box) {
	if cur == m.referenceLine {
		m.referenceLine = m.prevRefLine
	}
}

// StrutMetrics describes the parent strut for list markers.
type StrutMetrics struct {
	Ascent   float32
	Baseline int
	Descent  float32
}

// ---- generic box operations ------------------------------------------------

func (b *Box) width() int { return b.contentWidth + b.leftMBP + b.rightMBP }

func (b *Box) addChild(c *Box) {
	c.parent = b
	c.index = len(b.children)
	b.children = append(b.children, c)
}

func (b *Box) addAllChildren(cs []*Box) {
	for _, c := range cs {
		b.addChild(c)
	}
}

func (b *Box) removeAllChildren() { b.children = nil }

func (b *Box) removeChildBox(target *Box) {
	found := false
	out := b.children[:0:0]
	for _, c := range b.children {
		if c == target {
			found = true
			continue
		}
		if found {
			c.index--
		}
		out = append(out, c)
	}
	b.children = out
}

func (b *Box) previousSibling() *Box {
	p := b.parent
	if p == nil {
		return nil
	}
	return p.previousOf(b)
}

func (b *Box) nextSibling() *Box {
	p := b.parent
	if p == nil {
		return nil
	}
	return p.nextOf(b)
}

func (b *Box) previousOf(child *Box) *Box {
	if b.Kind == KInline {
		for i := 0; i < len(b.inlineChildren)-1; i++ {
			if b.inlineChildren[i] == interface{}(child) {
				if i == 0 {
					return nil
				}
				if p, ok := b.inlineChildren[i-1].(*Box); ok {
					return p
				}
				return nil
			}
		}
		return nil
	}
	if child.index == 0 {
		return nil
	}
	return b.children[child.index-1]
}

func (b *Box) nextOf(child *Box) *Box {
	if b.Kind == KInline {
		for i := 0; i < len(b.inlineChildren)-1; i++ {
			if b.inlineChildren[i] == interface{}(child) {
				if n, ok := b.inlineChildren[i+1].(*Box); ok {
					return n
				}
				return nil
			}
		}
		return nil
	}
	if child.index == len(b.children)-1 {
		return nil
	}
	return b.children[child.index+1]
}

func (b *Box) containingBlock() *Box {
	if b.containingBlk == nil {
		return b.parent
	}
	return b.containingBlk
}

func (b *Box) isStyled() bool { return b.Style != nil }

func (b *Box) isRoot() bool {
	return b.Elem != nil && !b.anonymous && b.Elem.Parent != nil && b.Elem.Parent.Type == xmldom.DocumentNode
}

func (b *Box) isBody() bool { return b.parent != nil && b.parent.isRoot() }

// ---- margins ---------------------------------------------------------------

func (b *Box) containingBlockWidth() int {
	if b.Kind == KInline {
		return b.cbWidth
	}
	return b.containingBlock().getContentWidth()
}

func (b *Box) getContentWidth() int {
	if b.Kind == KAnon {
		return b.containingBlock().getContentWidth()
	}
	return b.contentWidth
}

func (b *Box) styleMargin() *RectSet {
	return b.Style.MarginRect(float32(b.containingBlockWidth()))
}

func (b *Box) styleMarginNC() *RectSet {
	return b.Style.MarginRectNoCache(float32(b.containingBlockWidth()))
}

func (b *Box) ensureWorkingMargin() {
	if b.workingMargin == nil {
		m := *b.styleMargin()
		b.workingMargin = &m
	}
}

func (b *Box) getMargin() *RectSet {
	if b.workingMargin != nil {
		return b.workingMargin
	}
	return b.styleMargin()
}

func (b *Box) setMarginTop(v int)    { b.ensureWorkingMargin(); b.workingMargin.Top = float32(v) }
func (b *Box) setMarginBottom(v int) { b.ensureWorkingMargin(); b.workingMargin.Bottom = float32(v) }
func (b *Box) setMarginLeft(v int)   { b.ensureWorkingMargin(); b.workingMargin.Left = float32(v) }
func (b *Box) setMarginRight(v int)  { b.ensureWorkingMargin(); b.workingMargin.Right = float32(v) }

func (b *Box) resetTopMargin() {
	if b.workingMargin != nil {
		sm := b.styleMargin()
		b.workingMargin.Top = sm.Top
	}
}

func (b *Box) getPadding() *RectSet {
	return b.Style.PaddingRect(float32(b.containingBlockWidth()))
}

// getBorder mirrors Box.getBorder with the table cell override.
func (b *Box) getBorder() *BorderSet {
	if b.Kind == KTable {
		if b.Style.IsCollapseBorders() {
			return EmptyBorder
		}
		return b.Style.Border()
	}
	if b.Kind == KCell {
		if b.table().Style.IsCollapseBorders() {
			if b.collapsedLayoutBorder == nil {
				return EmptyBorder
			}
			return b.collapsedLayoutBorder
		}
	}
	return b.Style.Border()
}

func (b *Box) getMarginBorderPadding(which int) int {
	border := b.getBorder()
	m := b.getMargin()
	p := b.getPadding()
	switch which {
	case Left:
		return int(m.Left + border.Left + p.Left)
	case Right:
		return int(m.Right + border.Right + p.Right)
	case Top:
		return int(m.Top + border.Top + p.Top)
	default:
		return int(m.Bottom + border.Bottom + p.Bottom)
	}
}

// ---- edges -----------------------------------------------------------------

func (b *Box) marginEdge(left, top, tx, ty int) Rect {
	if b.Kind == KLine {
		return Rect{b.x + tx, b.y + ty, b.contentWidth, b.height}
	}
	if b.Kind == KInline {
		r := b.borderEdge(left, top)
		var ml, mr float32
		if b.startsHere || b.endsHere {
			m := b.getMargin()
			if b.startsHere {
				ml = m.Left
			}
			if b.endsHere {
				mr = m.Right
			}
		}
		if mr > 0 {
			r.W = int(float32(r.W) + mr)
		}
		if ml > 0 {
			r.X = int(float32(r.X) - ml)
			r.W = int(float32(r.W) + ml)
		}
		return r.Translate(tx, ty)
	}
	return Rect{left + tx, top + ty, b.width(), b.height}
}

func (b *Box) marginEdgeXY(tx, ty int) Rect { return b.marginEdge(b.x, b.y, tx, ty) }

func (b *Box) borderEdge(left, top int) Rect {
	if b.Kind == KInline {
		var ml, mr float32
		if b.startsHere || b.endsHere {
			m := b.getMargin()
			if b.startsHere {
				ml = m.Left
			}
			if b.endsHere {
				mr = m.Right
			}
		}
		border := b.getBorder()
		pad := b.getPadding()
		return Rect{
			int(float32(left) + ml),
			int(float32(top) - border.Top - pad.Top),
			int(float32(b.inlineWidth) - ml - mr),
			b.height,
		}
	}
	m := b.getMargin()
	return Rect{left + int(m.Left), top + int(m.Top),
		b.width() - int(m.Left) - int(m.Right),
		b.height - int(m.Top) - int(m.Bottom)}
}

func (b *Box) paddingEdge(left, top int) Rect {
	m := b.getMargin()
	bd := b.getBorder()
	return Rect{left + int(m.Left) + int(bd.Left), top + int(m.Top) + int(bd.Top),
		b.width() - int(m.Width()) - int(bd.Width()),
		b.height - int(m.Height()) - int(bd.Height())}
}

func (b *Box) paddingWidth() int {
	p := b.getPadding()
	return int(p.Left) + b.getContentWidth() + int(p.Right)
}

func (b *Box) contentAreaEdge(left, top int) Rect {
	if b.Kind == KInline {
		bd := b.getBorder()
		pad := b.getPadding()
		var ml, mr, bl, br, pl, pr float32
		if b.startsHere || b.endsHere {
			m := b.getMargin()
			if b.startsHere {
				ml, bl, pl = m.Left, bd.Left, pad.Left
			}
			if b.endsHere {
				mr, br, pr = m.Right, bd.Right, pad.Right
			}
		}
		return Rect{
			int(float32(left) + ml + bl + pl),
			int(float32(top) - bd.Top - pad.Top),
			int(float32(b.inlineWidth) - ml - bl - pl - pr - br - mr),
			b.height,
		}
	}
	m := b.getMargin()
	bd := b.getBorder()
	p := b.getPadding()
	return Rect{
		left + int(m.Left) + int(bd.Left) + int(p.Left),
		top + int(m.Top) + int(bd.Top) + int(p.Top),
		b.width() - int(m.Width()) - int(bd.Width()) - int(p.Width()),
		b.height - int(m.Height()) - int(bd.Height()) - int(p.Height()),
	}
}

func (b *Box) paintingBorderEdge() Rect { return b.borderEdge(b.absX, b.absY) }

// ---- canvas locations ---------------------------------------------------------

func (b *Box) lineBoxOf() *Box {
	// for inline-block (block child of line/inline box) and InlineLayoutBox
	if b.Kind == KInline {
		p := b.parent
		for p != nil && p.Kind != KLine {
			p = p.parent
		}
		return p
	}
	if !b.isInlineChild() {
		return nil
	}
	p := b.parent
	for p.Kind != KLine {
		p = p.parent
	}
	return p
}

// isInlineChild reports BlockBox.isInline().
func (b *Box) isInlineChild() bool {
	p := b.parent
	return p != nil && (p.Kind == KLine || p.Kind == KInline)
}

func (b *Box) calcCanvasLocation() {
	switch b.Kind {
	case KLine:
		p := b.parent
		b.absX = p.absX + p.tx + b.x
		b.absY = p.absY + p.ty + b.y
	case KInline:
		lb := b.lineBoxOf()
		b.absX = lb.absX + b.x
		b.absY = lb.absY + b.y
	default:
		if b.floated != nil {
			fm := b.floated.manager
			if fm != nil {
				ox, oy := fm.offsetOf(b)
				b.absX = fm.master.absX + b.x - ox
				b.absY = fm.master.absY + b.y - oy
			}
		}
		lb := b.lineBoxOf()
		if lb == nil {
			p := b.parent
			if p != nil {
				b.absX = p.absX + p.tx + b.x
				b.absY = p.absY + p.ty + b.y
			} else if b.isStyled() && b.Style.IsAbsFixedOrInlineBlockEquiv() {
				cb := b.containingBlock()
				if cb != nil {
					b.absX = cb.absX + b.x
					b.absY = cb.absY + b.y
				}
			}
		} else {
			b.absX = lb.absX + b.x
			b.absY = lb.absY + b.y
		}
		if b.replaced != nil {
			b.replaced.x, b.replaced.y = b.absX, b.absY
		}
	}
}

func (b *Box) calcChildLocations() {
	switch b.Kind {
	case KInline:
		for _, o := range b.inlineChildren {
			if c, ok := o.(*Box); ok {
				c.calcCanvasLocation()
				c.calcChildLocations()
			}
		}
		return
	case KLine:
		for _, c := range b.children {
			c.calcCanvasLocation()
			c.calcChildLocations()
		}
		for _, c := range b.nonFlowContent {
			if c.Style.IsAbsolute() {
				c.calcCanvasLocation()
				c.calcChildLocations()
			}
		}
		return
	}
	for _, c := range b.children {
		c.calcCanvasLocation()
		c.calcChildLocations()
	}
	if b.persistentBFC != nil {
		b.persistentBFC.calcFloatLocations()
	}
}

// ---- table navigation -----------------------------------------------------------

func (b *Box) table() *Box {
	switch b.Kind {
	case KCell:
		return b.parent.parent.parent
	case KRow:
		return b.parent.parent
	case KSection:
		return b.parent
	}
	return b
}

func (b *Box) section() *Box {
	if b.Kind == KCell {
		return b.parent.parent
	}
	if b.Kind == KRow {
		return b.parent
	}
	return b
}

// marginCollapseResult is MarginCollapseResult.
type marginCollapseResult struct{ maxPos, maxNeg int }

func (m *marginCollapseResult) update(v int) {
	if v < 0 && v < m.maxNeg {
		m.maxNeg = v
	}
	if v > 0 && v > m.maxPos {
		m.maxPos = v
	}
}
func (m *marginCollapseResult) margin() int     { return m.maxPos + m.maxNeg }
func (m *marginCollapseResult) hasMargin() bool { return m.maxPos != 0 || m.maxNeg != 0 }
