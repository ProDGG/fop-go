package xhr

const noPageTrim = -1

type relayoutData struct {
	state       *LayoutState
	startsRun   bool
	endsRun     bool
	inRun       bool
	childOffset int
}

type relayoutDataList struct{ hints []*relayoutData }

func newRelayoutDataList(n int) *relayoutDataList {
	l := &relayoutDataList{}
	for i := 0; i < n; i++ {
		l.hints = append(l.hints, &relayoutData{})
	}
	return l
}

func (l *relayoutDataList) markRun(offset int, previous, current *Box) {
	prevData := l.hints[offset-1]
	curData := l.hints[offset]
	prevAfter := previous.Style.Ident("page-break-after")
	curBefore := current.Style.Ident("page-break-before")
	if (prevAfter == "avoid" && curBefore == "auto") ||
		(prevAfter == "auto" && curBefore == "avoid") ||
		(prevAfter == "avoid" && curBefore == "avoid") {
		if !prevData.inRun {
			prevData.startsRun = true
		}
		prevData.inRun = true
		curData.inRun = true
		if offset == len(l.hints)-1 {
			curData.endsRun = true
		}
	} else if prevData.inRun {
		prevData.endsRun = true
	}
}

func (l *relayoutDataList) runStart(runEnd int) int {
	offset := runEnd
	cur := l.hints[offset]
	for !cur.startsRun {
		offset--
		cur = l.hints[offset]
	}
	return offset
}

// layoutBlockContent is BlockBoxing.layoutContent.
func (block *Box) layoutBlockContent(lc *LayoutContext, contentStart int) {
	offset := -1
	local := append([]*Box(nil), block.children...)
	childOffset := block.height + contentStart
	rdl := newRelayoutDataList(len(local))
	pageCount := noPageTrim
	var previousChild *Box
	for _, child := range local {
		offset++
		rd := rdl.hints[offset]
		mayCheckKeepTogether := false
		rd.state = lc.copyStateForRelayout()
		rd.childOffset = childOffset
		pageCount = len(lc.rootLayer.pages)
		child.needPageClear = false
		if (child.Style.IsAvoidPageBreakInside() || child.Style.IsKeepWithInline()) && lc.mayCheckKeepTogether {
			mayCheckKeepTogether = true
			lc.mayCheckKeepTogether = false
		}
		layoutBlockChild(lc, block, child, false, childOffset, noPageTrim, rd.state)
		needPageClear := child.needPageClear
		if needPageClear || mayCheckKeepTogether {
			lc.mayCheckKeepTogether = mayCheckKeepTogether
			tryToAvoid := child.Style.IsAvoidPageBreakInside() && child.crossesPageBreak(lc)
			keepWithInline := child.isNeedsKeepWithInline(lc)
			if tryToAvoid || needPageClear || keepWithInline {
				lc.restoreStateForRelayout(rd.state)
				child.reset(lc)
				layoutBlockChild(lc, block, child, true, childOffset, pageCount, rd.state)
				if tryToAvoid && child.crossesPageBreak(lc) && !keepWithInline {
					lc.restoreStateForRelayout(rd.state)
					child.reset(lc)
					layoutBlockChild(lc, block, child, false, childOffset, pageCount, rd.state)
				}
			}
		}
		lc.rootLayer.ensureHasPage(lc, child)
		if child.relativeOffset == nil {
			childOffset = child.y + child.height
		} else {
			childOffset = child.y - child.relativeOffset[1] + child.height
		}
		if childOffset > block.height {
			block.height = childOffset
		}
		if child.Style.IsForcePageBreakAfter() {
			block.forcePageBreakAfter(lc, child.Style.Ident("page-break-after"))
			childOffset = block.height
		}
		if previousChild != nil {
			rdl.markRun(offset, previousChild, child)
		}
		changed, newOffset := processPageBreakAvoidRun(lc, block, local, offset, rdl, rd, child)
		if changed {
			childOffset = newOffset
			if childOffset > block.height {
				block.height = childOffset
			}
		}
		previousChild = child
	}
}

func processPageBreakAvoidRun(lc *LayoutContext, block *Box, local []*Box, offset int, rdl *relayoutDataList, rd *relayoutData, child *Box) (bool, int) {
	if offset <= 0 {
		return false, 0
	}
	might := false
	runEnd := -1
	if offset == len(local)-1 && rd.endsRun {
		might = true
		runEnd = offset
	} else if offset > 0 {
		prev := rdl.hints[offset-1]
		if prev.endsRun {
			might = true
			runEnd = offset - 1
		}
	}
	if !might {
		return false, 0
	}
	runStart := rdl.runStart(runEnd)
	if isPageBreakBetweenChildBoxes(rdl, runStart, runEnd, lc, block) {
		block.resetChildrenRange(lc, runStart, offset)
		co := relayoutRun(lc, local, block, rdl, runStart, offset, true)
		if isPageBreakBetweenChildBoxes(rdl, runStart, runEnd, lc, block) {
			block.resetChildrenRange(lc, runStart, offset)
			co = relayoutRun(lc, local, block, rdl, runStart, offset, false)
		}
		return true, co
	}
	return false, 0
}

func isPageBreakBetweenChildBoxes(rdl *relayoutDataList, runStart, runEnd int, lc *LayoutContext, block *Box) bool {
	for i := runStart; i < runEnd; i++ {
		prev := block.children[i]
		next := block.children[i+1]
		nextLine := firstLineOf(next)
		if nextLine == nil {
			nextLine = next
		}
		prevEnd := prev.absY + prev.height
		nextLineEnd := nextLine.absY + nextLine.height
		if lc.rootLayer.crossesPageBreak(lc, prevEnd, nextLineEnd) {
			return true
		}
	}
	return false
}

func firstLineOf(b *Box) *Box {
	for c := b; len(c.children) > 0; c = c.children[0] {
		if c.Kind == KLine {
			return c
		}
	}
	return nil
}

func relayoutRun(lc *LayoutContext, local []*Box, block *Box, rdl *relayoutDataList, start, end int, onNewPage bool) int {
	childOffset := rdl.hints[start].childOffset
	if onNewPage {
		startBox := local[start]
		spb := lc.rootLayer.firstPage(lc, startBox)
		childOffset += spb.bottom - startBox.absY
	}
	block.height = childOffset
	for i := start; i <= end; i++ {
		child := local[i]
		rd := rdl.hints[i]
		pageCount := len(lc.rootLayer.pages)
		lc.restoreStateForRelayout(rd.state)
		rd.childOffset = childOffset
		mayCheck := false
		if (child.Style.IsAvoidPageBreakInside() || child.Style.IsKeepWithInline()) && lc.mayCheckKeepTogether {
			mayCheck = true
			lc.mayCheckKeepTogether = false
		}
		layoutBlockChild(lc, block, child, false, childOffset, noPageTrim, rd.state)
		if mayCheck {
			lc.mayCheckKeepTogether = true
			tryToAvoid := child.Style.IsAvoidPageBreakInside() && child.crossesPageBreak(lc)
			needPageClear := child.needPageClear
			keepWithInline := child.isNeedsKeepWithInline(lc)
			if tryToAvoid || needPageClear || keepWithInline {
				lc.restoreStateForRelayout(rd.state)
				child.reset(lc)
				layoutBlockChild(lc, block, child, true, childOffset, pageCount, rd.state)
				if tryToAvoid && child.crossesPageBreak(lc) && !keepWithInline {
					lc.restoreStateForRelayout(rd.state)
					child.reset(lc)
					layoutBlockChild(lc, block, child, false, childOffset, pageCount, rd.state)
				}
			}
		}
		lc.rootLayer.ensureHasPage(lc, child)
		if child.relativeOffset == nil {
			childOffset = child.y + child.height
		} else {
			childOffset = child.y - child.relativeOffset[1] + child.height
		}
		if childOffset > block.height {
			block.height = childOffset
		}
		if child.Style.IsForcePageBreakAfter() {
			block.forcePageBreakAfter(lc, child.Style.Ident("page-break-after"))
			childOffset = block.height
		}
	}
	return childOffset
}

func layoutBlockChild(lc *LayoutContext, parent, child *Box, needPageClear bool, childOffset, trimmedPageCount int, state *LayoutState) {
	layoutBlockChild0(lc, parent, child, needPageClear, childOffset, trimmedPageCount)
	if bc := child.calcBreakAtLineContext(lc); bc != nil {
		lc.breakAtLine = bc
		lc.restoreStateForRelayout(state)
		child.reset(lc)
		layoutBlockChild0(lc, parent, child, needPageClear, childOffset, trimmedPageCount)
		lc.breakAtLine = nil
	}
}

func layoutBlockChild0(lc *LayoutContext, parent, child *Box, needPageClear bool, childOffset, trimmedPageCount int) {
	child.needPageClear = needPageClear
	child.initStaticPos(lc, parent, childOffset)
	child.initContainingLayer(lc)
	child.calcCanvasLocation()
	lc.translate(0, childOffset)
	repositionBox(lc, child, trimmedPageCount)
	child.layoutBox(lc)
	lc.translate(-child.x, -child.y)
}

func repositionBox(lc *LayoutContext, child *Box, trimmedPageCount int) {
	moved := false
	if child.Style.IsRelative() {
		dx, dy := child.positionRelative()
		lc.translate(dx, dy)
		moved = true
	}
	pageClear := child.needPageClear || child.Style.IsForcePageBreakBefore()
	needNewPageContext := child.checkPageContext(lc)
	if needNewPageContext && trimmedPageCount != noPageTrim {
		lc.rootLayer.trimPageCount(trimmedPageCount)
	}
	if pageClear || needNewPageContext {
		delta := child.forcePageBreakBefore(lc, child.Style.Ident("page-break-before"), needNewPageContext)
		lc.translate(0, delta)
		moved = true
		child.needPageClear = false
	}
	if moved {
		child.calcCanvasLocation()
	}
}
