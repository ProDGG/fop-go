package xhr

import "sort"

// Positioned content: a port of the Layer parts of Flying Saucer that deal
// with relative / absolute / fixed boxes and stacking.

func newLayer(parent *Layer, master *Box) *Layer {
	l := &Layer{parent: parent, master: master}
	l.stackingContext = master.Style.IsPositioned() && !master.Style.IsAutoZIndex()
	master.layer = l
	master.containingLayer = l
	return l
}

func (l *Layer) addChild(c *Layer) { l.children = append(l.children, c) }

func (l *Layer) isRootLayer() bool { return l.parent == nil && l.stackingContext }

func (l *Layer) zIndex() int { return int(l.master.Style.AsFloat("z-index")) }

func (l *Layer) remove(c *Layer) {
	for i, x := range l.children {
		if x == c {
			l.children = append(l.children[:i:i], l.children[i+1:]...)
			return
		}
	}
	panic("xhr: could not find layer to remove")
}

func (l *Layer) detach() {
	if l.parent != nil {
		l.parent.remove(l)
	}
}

// Layer collection kinds.
const (
	layersPositive = iota + 1
	layersZero
	layersNegative
	layersAuto
)

func (l *Layer) stackingContextLayers(which int) []*Layer {
	var res []*Layer
	for _, t := range l.children {
		if !t.stackingContext {
			continue
		}
		z := t.zIndex()
		switch {
		case which == layersNegative && z < 0,
			which == layersPositive && z > 0,
			which == layersZero && z == 0:
			res = append(res, t)
		}
	}
	return res
}

func (l *Layer) collectLayers(which int) []*Layer {
	var res []*Layer
	if which != layersAuto {
		res = append(res, l.stackingContextLayers(which)...)
	}
	for _, c := range l.children {
		if !c.stackingContext {
			if which == layersAuto {
				res = append(res, c)
			}
			res = append(res, c.collectLayers(which)...)
		}
	}
	return res
}

func (l *Layer) sortedLayers(which int) []*Layer {
	res := l.collectLayers(which)
	sort.SliceStable(res, func(i, j int) bool { return res[i].zIndex() < res[j].zIndex() })
	return res
}

// finish is Layer.finish: lay out absolute children (print) and position
// relative content once the layer's master is complete.
func (l *Layer) finish(lc *LayoutContext) {
	l.layoutAbsoluteChildren(lc)
	if !l.inline {
		l.positionChildren(lc)
	}
}

func (l *Layer) positionChildren(lc *LayoutContext) {
	for _, c := range l.children {
		c.position(lc)
	}
}

func (l *Layer) position(lc *LayoutContext) {
	st := l.master.Style
	if st.IsRelative() && (l.inline || l.master.isInlineChild()) {
		l.master.positionRelativeBox()
		if !l.inline {
			l.master.calcCanvasLocation()
			l.master.calcChildLocations()
		}
	}
}

func (l *Layer) layoutAbsoluteChildren(lc *LayoutContext) {
	children := append([]*Layer(nil), l.children...)
	if len(children) == 0 {
		return
	}
	state := lc.captureLayoutState()
	for _, child := range children {
		if !child.requiresLayout {
			continue
		}
		l.layoutAbsoluteChild(lc, child)
		cm := child.master
		if cm.Style.IsAvoidPageBreakInside() && cm.crossesPageBreak(lc) {
			cm.reset(lc)
			cm.needPageClear = true
			l.layoutAbsoluteChild(lc, child)
			if cm.crossesPageBreak(lc) {
				cm.reset(lc)
				l.layoutAbsoluteChild(lc, child)
			}
		}
		child.requiresLayout = false
		child.finish(lc)
		lc.rootLayer.ensureHasPage(lc, child.master)
	}
	lc.restoreLayoutState(state)
}

func (l *Layer) layoutAbsoluteChild(lc *LayoutContext, child *Layer) {
	master := child.master
	if master.Style.IsBottomAuto() {
		// set top, left
		master.positionAbsolute(posBoth)
		master.positionAbsoluteOnPage(lc)
		lc.reInit(true)
		master.layoutBox(lc)
		// set right
		master.positionAbsolute(posHorizontally)
		return
	}
	// FIXME (as in the reference) not right in the face of pagination
	lc.reInit(true)
	master.layoutBox(lc)
	before := master.boxDimensions()
	master.reset(lc)
	after := master.boxDimensions()
	master.setBoxDimensions(before)
	master.positionAbsolute(posBoth)
	master.positionAbsoluteOnPage(lc)
	master.setBoxDimensions(after)
	lc.reInit(true)
	master.layoutBox(lc)
}

type boxDimensions struct{ leftMBP, rightMBP, contentWidth, height int }

func (b *Box) boxDimensions() boxDimensions {
	return boxDimensions{b.leftMBP, b.rightMBP, b.contentWidth, b.height}
}

func (b *Box) setBoxDimensions(d boxDimensions) {
	b.leftMBP, b.rightMBP, b.contentWidth, b.height = d.leftMBP, d.rightMBP, d.contentWidth, d.height
}

// Directions of positionAbsolute.
const (
	posVertically   = 1
	posHorizontally = 2
	posBoth         = posVertically | posHorizontally
)

// cbEdges returns the padding edge and the content height of the containing block.
func (b *Box) cbEdges() (bounding Rect, contentHeight int) {
	cb := b.containingBlock()
	if cb.fixedRect != nil {
		r := Rect{-cb.fixedRect.X, -cb.fixedRect.Y, cb.fixedRect.W, cb.fixedRect.H}
		return r, r.H
	}
	if cb.Style == nil {
		r := Rect{0, 0, cb.contentWidth, cb.height}
		return r, r.H
	}
	contentHeight = cb.contentAreaEdge(0, 0).H
	if cb.isBlockish() {
		return cb.paddingEdge(0, 0), contentHeight
	}
	return cb.contentAreaEdge(0, 0), contentHeight
}

func (b *Box) cbContentWidth() int {
	cb := b.containingBlock()
	if cb.fixedRect != nil {
		return cb.fixedRect.W
	}
	return cb.getContentWidth()
}

func (b *Box) calcPinnedHeight() int {
	if !b.Style.IsIdent("top", "auto") && !b.Style.IsIdent("bottom", "auto") {
		pe, _ := b.cbEdges()
		top := int(b.Style.FloatProportional("top", float32(pe.H)))
		bottom := int(b.Style.FloatProportional("bottom", float32(pe.H)))
		r := pe.H - top - bottom
		if r < 0 {
			return 0
		}
		return r
	}
	return -1
}

func (b *Box) alignToStaticEquivalent() {
	if b.staticEquivalent.absY != b.absY {
		b.y = b.staticEquivalent.absY - b.absY
		b.absY = b.staticEquivalent.absY
	}
}

// positionAbsolute is BlockBox.positionAbsolute.
func (b *Box) positionAbsolute(direction int) {
	st := b.Style
	bounding, cbContentHeight := b.cbEdges()
	cbw := b.cbContentWidth()
	if direction&posHorizontally != 0 {
		b.x = 0
		if !st.IsIdent("left", "auto") {
			b.x = int(st.FloatProportional("left", float32(cbw)))
		} else if !st.IsIdent("right", "auto") {
			b.x = bounding.W - int(st.FloatProportional("right", float32(cbw))) - b.width()
		}
		b.x += bounding.X
	}
	if direction&posVertically != 0 {
		b.y = 0
		if !st.IsIdent("top", "auto") {
			b.y = int(st.FloatProportional("top", float32(cbContentHeight)))
		} else if !st.IsIdent("bottom", "auto") {
			b.y = bounding.H - int(st.FloatProportional("bottom", float32(cbContentHeight))) - b.height
		}
		// the containing block must be completely laid out before this
		pinned := b.calcPinnedHeight()
		if pinned != -1 && b.getCSSHeight() == -1 {
			b.height = pinned
			b.applyCSSMinMaxHeight()
		}
		b.y += bounding.Y
	}
	b.calcCanvasLocation()
	if direction&posVertically != 0 && st.IsTopAuto() && st.IsBottomAuto() {
		b.alignToStaticEquivalent()
	}
	b.calcChildLocations()
}

// positionAbsoluteOnPage is BlockBox.positionAbsoluteOnPage.
func (b *Box) positionAbsoluteOnPage(lc *LayoutContext) {
	if b.Style.IsForcePageBreakBefore() || b.needPageClear {
		b.forcePageBreakBefore(lc, b.Style.Ident("page-break-before"), false)
		b.calcCanvasLocation()
		b.calcChildLocations()
		b.needPageClear = false
	}
}

// layoutAbsolute is LayoutUtil.layoutAbsolute (print: the layout is deferred
// until the position is known).
func layoutAbsolute(lc *LayoutContext, currentLine *Box, box *Box) {
	marker := lc.markerData
	lc.markerData = nil
	if box.Style.IsFixed() {
		box.containingBlk = lc.rootLayer.master.containingBlock()
	} else {
		box.containingBlk = lc.layer().master
	}
	box.staticEquivalent = currentLine
	lc.pushLayer(box)
	lc.layer().requiresLayout = true
	lc.popLayer()
	lc.markerData = marker
}

// ---- layer bookkeeping of boxes --------------------------------------------------------------

func (b *Box) resetLayer() {
	if b.layer != nil {
		b.layer.detach()
		b.layer = nil
	}
	b.containingLayer = nil
}

// initContainingLayer is Box.initContainingLayer.
func (b *Box) initContainingLayer(lc *LayoutContext) {
	if b.layer != nil {
		b.containingLayer = b.layer
		return
	}
	if b.containingLayer != nil {
		return
	}
	if b.parent == nil || b.parent.containingLayer == nil {
		panic("xhr: internal error: no containing layer")
	}
	b.containingLayer = b.parent.containingLayer
	// block boxes directly wrapped by an inline relative layer
	if cur := lc.layer(); cur.inline {
		for _, c := range cur.master.elementWithContent() {
			if c == b {
				b.containingLayer = cur
				break
			}
		}
	}
}

func (b *Box) connectChildrenToCurrentLayer(lc *LayoutContext) {
	if b.Kind == KInline {
		for _, o := range b.inlineChildren {
			if c, ok := o.(*Box); ok {
				c.containingLayer = lc.layer()
				c.connectChildrenToCurrentLayer(lc)
			}
		}
		return
	}
	for _, c := range b.children {
		c.containingLayer = lc.layer()
		c.connectChildrenToCurrentLayer(lc)
	}
}

// addChildForLayout is Box.addChildForLayout.
func (b *Box) addChildForLayout(lc *LayoutContext, c *Box) {
	b.addChild(c)
	c.initContainingLayer(lc)
}

// ---- inline layers ------------------------------------------------------------------------------

func (b *Box) lineBoxParent() *Box {
	p := b.parent
	for p != nil && p.Kind != KLine {
		p = p.parent
	}
	return p
}

func (b *Box) elementBoxes(elem interface{}) []*Box {
	var res []*Box
	if b.Kind == KInline {
		for _, o := range b.inlineChildren {
			if c, ok := o.(*Box); ok {
				if c.Elem == elem {
					res = append(res, c)
				}
				res = append(res, c.elementBoxes(elem)...)
			}
		}
		return res
	}
	for _, c := range b.children {
		if c.Elem == elem {
			res = append(res, c)
		}
		res = append(res, c.elementBoxes(elem)...)
	}
	return res
}

func (b *Box) addToContentList(list *[]*Box) {
	*list = append(*list, b)
	for _, o := range b.inlineChildren {
		if c, ok := o.(*Box); ok {
			if c.Kind == KInline {
				c.addToContentList(list)
			} else {
				*list = append(*list, c)
			}
		}
	}
}

func (b *Box) containsEnd(list []*Box) bool {
	for _, x := range list {
		if x.Kind == KInline && b.Elem == x.Elem && x.endsHere {
			return true
		}
	}
	return false
}

func addFollowingBlockBoxes(container *Box, list *[]*Box) *Box {
	parent := container.parent
	cur := 0
	for ; cur < len(parent.children); cur++ {
		if parent.children[cur] == container {
			cur++
			break
		}
	}
	for ; cur < len(parent.children); cur++ {
		if parent.children[cur].Kind == KAnon {
			break
		}
		*list = append(*list, parent.children[cur])
	}
	if cur == len(parent.children) {
		return nil
	}
	return parent.children[cur]
}

// elementWithContent is InlineLayoutBox.getElementWithContent.
func (b *Box) elementWithContent() []*Box {
	var res []*Box
	container := b.lineBoxParent().parent
	for {
		for _, iB := range container.elementBoxes(b.Elem) {
			iB.addToContentList(&res)
		}
		if container.Kind != KAnon || b.containsEnd(res) {
			break
		}
		container = addFollowingBlockBoxes(container, &res)
		if container == nil {
			break
		}
	}
	return res
}

// positionRelativeBox is the (virtual) Box.positionRelative.
func (b *Box) positionRelativeBox() (int, int) {
	dx, dy := b.positionRelative()
	if b.Kind != KInline {
		return dx, dy
	}
	b.x -= dx
	b.y -= dy
	for _, c := range b.elementWithContent() {
		c.x += dx
		c.y += dy
		c.calcCanvasLocation()
		c.calcChildLocations()
	}
	return dx, dy
}

// addAllChildren appends the inline children that belong to the given layer.
func (b *Box) addAllChildrenOfLayer(list *[]*Box, layer *Layer) {
	if b.Kind == KLine {
		for _, c := range b.children {
			if b.containingLayer == layer {
				*list = append(*list, c)
				if c.Kind == KInline {
					c.addAllChildrenOfLayer(list, layer)
				}
			}
		}
		return
	}
	for _, o := range b.inlineChildren {
		if c, ok := o.(*Box); ok && c.containingLayer == layer {
			*list = append(*list, c)
			if c.Kind == KInline {
				c.addAllChildrenOfLayer(list, layer)
			}
		}
	}
}
