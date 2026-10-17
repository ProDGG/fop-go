package xhr

// FloatManager is Flying Saucer's FloatManager.
type FloatManager struct {
	left, right []*boxOffset
	master      *Box
}

type boxOffset struct {
	box  *Box
	x, y int
}

const (
	floatLeft  = 1
	floatRight = 2
)

func (fm *FloatManager) floats(dir int) *[]*boxOffset {
	if dir == floatLeft {
		return &fm.left
	}
	return &fm.right
}

func (fm *FloatManager) opposing(dir int) *[]*boxOffset {
	if dir == floatLeft {
		return &fm.right
	}
	return &fm.left
}

func (fm *FloatManager) floatBox(lc *LayoutContext, bfc *BlockFormattingContext, box *Box) {
	if box.Style.IsFloatedLeft() {
		fm.position(bfc, box, floatLeft)
		fm.save(lc, box, bfc, floatLeft)
	} else if box.Style.IsFloatedRight() {
		fm.position(bfc, box, floatRight)
		fm.save(lc, box, bfc, floatRight)
	}
}

func (fm *FloatManager) clear(bfc *BlockFormattingContext, box *Box) {
	if box.Style.IsClearLeft() {
		fm.moveClear(bfc, box, *fm.floats(floatLeft))
	}
	if box.Style.IsClearRight() {
		fm.moveClear(bfc, box, *fm.floats(floatRight))
	}
}

func (fm *FloatManager) save(lc *LayoutContext, cur *Box, bfc *BlockFormattingContext, dir int) {
	l := fm.floats(dir)
	*l = append(*l, &boxOffset{cur, bfc.x, bfc.y})
	lc.layer().addFloat(cur)
	cur.floated.manager = fm
	cur.calcCanvasLocation()
	cur.calcChildLocations()
}

func (fm *FloatManager) position(bfc *BlockFormattingContext, cur *Box, dir int) {
	fm.moveAllTheWayOver(cur, dir)
	fm.alignToLastOpposingFloat(bfc, cur, dir)
	fm.alignToLastFloat(bfc, cur, dir)
	if !fm.fitsInContainingBlock(cur) || fm.overlaps(bfc, cur, *fm.floats(dir)) {
		fm.moveAllTheWayOver(cur, dir)
		fm.moveFloatBelow(bfc, cur, *fm.floats(dir))
	}
	if fm.overlaps(bfc, cur, *fm.opposing(dir)) {
		fm.moveAllTheWayOver(cur, dir)
		fm.moveFloatBelow(bfc, cur, *fm.floats(dir))
		fm.moveFloatBelow(bfc, cur, *fm.opposing(dir))
	}
	if cur.Style.IsCleared() {
		if cur.Style.IsClearLeft() && dir == floatLeft {
			fm.moveAllTheWayOver(cur, floatLeft)
		} else if cur.Style.IsClearRight() && dir == floatRight {
			fm.moveAllTheWayOver(cur, floatRight)
		}
		fm.moveFloatBelow(bfc, cur, *fm.floats(dir))
	}
}

func (fm *FloatManager) alignToLastFloat(bfc *BlockFormattingContext, cur *Box, dir int) {
	floats := *fm.floats(dir)
	if len(floats) == 0 {
		return
	}
	lastOff := floats[len(floats)-1]
	last := lastOff.box
	cb := cur.marginEdgeXY(-bfc.x, -bfc.y)
	lb := last.marginEdgeXY(-lastOff.x, -lastOff.y)
	moveOver := false
	if cb.Y < lb.Y {
		cb = cb.Translate(0, lb.Y-cb.Y)
		moveOver = true
	}
	if cb.Y >= lb.Y && cb.Y < lb.Y+lb.H {
		moveOver = true
	}
	if moveOver {
		if dir == floatLeft {
			cb.X = lb.X + last.width()
		} else {
			cb.X = lb.X - cur.width()
		}
		cb = cb.Translate(bfc.x, bfc.y)
		cur.x = cb.X
		cur.y = cb.Y
	}
}

func (fm *FloatManager) alignToLastOpposingFloat(bfc *BlockFormattingContext, cur *Box, dir int) {
	floats := *fm.opposing(dir)
	if len(floats) == 0 {
		return
	}
	lastOff := floats[len(floats)-1]
	cb := cur.marginEdgeXY(-bfc.x, -bfc.y)
	lb := lastOff.box.marginEdgeXY(-lastOff.x, -lastOff.y)
	if cb.Y < lb.Y {
		cb = cb.Translate(0, lb.Y-cb.Y)
		cb = cb.Translate(bfc.x, bfc.y)
		cur.y = cb.Y
	}
}

func (fm *FloatManager) moveAllTheWayOver(cur *Box, dir int) {
	if dir == floatLeft {
		cur.x = 0
	} else {
		cur.x = cur.containingBlock().getContentWidth() - cur.width()
	}
}

func (fm *FloatManager) fitsInContainingBlock(cur *Box) bool {
	return cur.x >= 0 && cur.x+cur.width() <= cur.containingBlock().getContentWidth()
}

func (fm *FloatManager) findLowestY(floats []*boxOffset) int {
	res := 0
	for _, f := range floats {
		b := f.box.marginEdgeXY(-f.x, -f.y)
		if b.Y+b.H > res {
			res = b.Y + b.H
		}
	}
	return res
}

func (fm *FloatManager) getClearDelta(bfcRelativeY int) int {
	l := fm.findLowestY(fm.left)
	r := fm.findLowestY(fm.right)
	if r > l {
		l = r
	}
	return l - bfcRelativeY
}

func (fm *FloatManager) overlaps(bfc *BlockFormattingContext, cur *Box, floats []*boxOffset) bool {
	bounds := cur.marginEdgeXY(-bfc.x, -bfc.y)
	for _, f := range floats {
		fb := f.box.marginEdgeXY(-f.x, -f.y)
		if fb.Intersects(bounds) {
			return true
		}
	}
	return false
}

func (fm *FloatManager) moveFloatBelow(bfc *BlockFormattingContext, cur *Box, floats []*boxOffset) {
	if len(floats) == 0 {
		return
	}
	boxY := cur.y - bfc.y
	floatY := fm.findLowestY(floats)
	if floatY-boxY > 0 {
		cur.y += floatY - boxY
	}
}

func (fm *FloatManager) moveClear(bfc *BlockFormattingContext, cur *Box, floats []*boxOffset) {
	if len(floats) == 0 {
		return
	}
	bounds := cur.borderEdge(cur.x-bfc.x, cur.y-bfc.y)
	y := fm.findLowestY(floats)
	if bounds.Y < y {
		bounds.Y = y
		bounds = bounds.Translate(bfc.x, bfc.y)
		cur.y = bounds.Y - int(cur.getMargin().Top)
	}
}

func (fm *FloatManager) removeFloat(b *Box) {
	remove := func(l *[]*boxOffset) {
		out := (*l)[:0:0]
		for _, f := range *l {
			if f.box == b {
				b.floated.manager = nil
				continue
			}
			out = append(out, f)
		}
		*l = out
	}
	remove(&fm.left)
	remove(&fm.right)
}

func (fm *FloatManager) calcFloatLocations() {
	for _, l := range [][]*boxOffset{fm.left, fm.right} {
		for _, f := range l {
			f.box.calcCanvasLocation()
			f.box.calcChildLocations()
		}
	}
}

func (fm *FloatManager) offsetOf(b *Box) (int, int) {
	l := fm.right
	if b.Style.IsFloatedLeft() {
		l = fm.left
	}
	for _, f := range l {
		if f.box == b {
			return f.x, f.y
		}
	}
	return 0, 0
}

func (fm *FloatManager) performOnFloats(op func(*Box)) {
	for _, l := range [][]*boxOffset{fm.left, fm.right} {
		for _, f := range l {
			f.box.absX = f.box.x + fm.master.absX - f.x
			f.box.absY = f.box.y + fm.master.absY - f.y
			op(f.box)
		}
	}
}

type boxDistance struct {
	box  *Box
	dist int
}

func (fm *FloatManager) floatDistanceDir(bfc *BlockFormattingContext, line *Box, cbWidth int, floats []*boxOffset, dir int) boxDistance {
	if len(floats) == 0 {
		return boxDistance{nil, 0}
	}
	lineBounds := line.marginEdgeXY(-bfc.x, -bfc.y)
	lineBounds.W = cbWidth
	farthest := lineBounds.X
	if dir == floatRight {
		farthest = lineBounds.X + lineBounds.W
	}
	if line.height == 0 {
		lineBounds.H = int(line.Style.LineHeight())
	}
	var farBox *Box
	for _, f := range floats {
		fr := f.box.marginEdgeXY(-f.x, -f.y)
		if lineBounds.Intersects(fr) {
			if dir == floatLeft && fr.X+fr.W > farthest {
				farthest = fr.X + fr.W
			} else if dir == floatRight && fr.X < farthest {
				farthest = fr.X
			}
			farBox = f.box
		}
	}
	if dir == floatLeft {
		return boxDistance{farBox, farthest - lineBounds.X}
	}
	return boxDistance{farBox, lineBounds.X + lineBounds.W - farthest}
}

func (fm *FloatManager) leftFloatDistance(bfc *BlockFormattingContext, line *Box, cbWidth int) int {
	return fm.floatDistanceDir(bfc, line, cbWidth, fm.left, floatLeft).dist
}

func (fm *FloatManager) rightFloatDistance(bfc *BlockFormattingContext, line *Box, cbWidth int) int {
	return fm.floatDistanceDir(bfc, line, cbWidth, fm.right, floatRight).dist
}

func (fm *FloatManager) floatDistance(bfc *BlockFormattingContext, line *Box, cbWidth int) int {
	return fm.leftFloatDistance(bfc, line, cbWidth) + fm.rightFloatDistance(bfc, line, cbWidth)
}

func (fm *FloatManager) nextLineBoxDelta(bfc *BlockFormattingContext, line *Box, cbWidth int) int {
	left := fm.floatDistanceDir(bfc, line, cbWidth, fm.left, floatLeft)
	right := fm.floatDistanceDir(bfc, line, cbWidth, fm.right, floatRight)
	calc := func(bd boxDistance) int {
		f := bd.box
		rect := f.borderEdge(f.absX, f.absY)
		return rect.Y + rect.H - line.absY
	}
	ld, rd := 0, 0
	if left.box != nil {
		ld = calc(left)
	}
	if right.box != nil {
		rd = calc(right)
	}
	if rd > ld {
		return rd
	}
	return ld
}

// layoutFloated is LayoutUtil.layoutFloated.
func layoutFloated(lc *LayoutContext, currentLine, block *Box, avail int, pending *[]*floatLayoutResult) *floatLayoutResult {
	result := &floatLayoutResult{}
	marker := lc.markerData
	lc.markerData = nil
	block.containingBlk = currentLine.parent
	block.containingLayer = currentLine.containingLayer
	block.staticEquivalent = currentLine
	if pending != nil {
		block.y = currentLine.y + block.floated.marginFromSibling
	} else {
		block.y = currentLine.y + currentLine.height
	}
	// calcInitialFloatedCanvasLocation
	bfc := lc.bfc()
	block.absX = bfc.fm.master.absX + block.x - bfc.x
	block.absY = bfc.fm.master.absY + block.y - bfc.y
	initialY := block.y
	block.layoutBox(lc)
	bfc.fm.floatBox(lc, bfc, block)
	if pending != nil && (len(*pending) > 0 || block.width() > avail) && currentLine.containsContent {
		block.reset(lc)
		result.pending = true
	} else {
		positionFloatOnPage(lc, currentLine, block, initialY != block.y)
		lc.rootLayer.ensureHasPage(lc, block)
	}
	result.block = block
	lc.markerData = marker
	return result
}

func positionFloatOnPage(lc *LayoutContext, currentLine, block *Box, movedVertically bool) {
	switch {
	case block.Style.IsForcePageBreakBefore():
		block.forcePageBreakBefore(lc, block.Style.Ident("page-break-before"), false)
		block.calcCanvasLocation()
		resetAndFloatBlock(lc, currentLine, block)
	case block.Style.IsAvoidPageBreakInside() && block.crossesPageBreak(lc):
		clearDelta := block.forcePageBreakBefore(lc, block.Style.Ident("page-break-before"), false)
		block.calcCanvasLocation()
		resetAndFloatBlock(lc, currentLine, block)
		if block.crossesPageBreak(lc) {
			block.y -= clearDelta
			block.calcCanvasLocation()
			resetAndFloatBlock(lc, currentLine, block)
		}
	case movedVertically:
		resetAndFloatBlock(lc, currentLine, block)
	}
}

func resetAndFloatBlock(lc *LayoutContext, currentLine, block *Box) {
	block.reset(lc)
	block.containingLayer = currentLine.containingLayer
	block.layoutBox(lc)
	lc.bfc().fm.floatBox(lc, lc.bfc(), block)
}
