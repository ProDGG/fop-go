package xhr

import "strings"

// ---- layout entry points ---------------------------------------------------

// layoutBox is the virtual layout(LayoutContext) of the box classes.
func (b *Box) layoutBox(lc *LayoutContext) {
	switch b.Kind {
	case KAnon:
		b.layoutInlineChildren(lc, 0, b.calcInitialBreakAtLine(lc), true)
	case KTable:
		b.tableLayoutBox(lc)
	default:
		b.layout(lc, 0)
	}
}

func (b *Box) isMarginAreaRoot() bool { return b.Kind == KTable && b.marginAreaRoot }

// getCSSWidth mirrors the (overridden) getCSSWidth.
func (b *Box) getCSSWidth(shrinkingToFit bool) int {
	if b.Kind == KTable {
		if b.Style.IsAutoWidth() {
			return -1
		}
		r := int(b.Style.FloatProportional("width", float32(b.containingBlock().getContentWidth())))
		bd := b.getBorder()
		r -= int(bd.Left) + int(bd.Right)
		if !b.Style.IsCollapseBorders() {
			p := b.getPadding()
			r -= int(p.Left) + int(p.Right)
		}
		if r >= 0 {
			return r
		}
		return -1
	}
	if !b.anonymous {
		if !b.Style.IsAutoWidth() {
			if shrinkingToFit && !b.Style.IsAbsoluteWidth() {
				return -1
			}
			r := int(b.Style.FloatProportional("width", float32(b.containingBlock().getContentWidth())))
			if r >= 0 {
				return r
			}
			return -1
		}
	}
	return -1
}

func (b *Box) getCSSFitToWidth() int {
	if !b.anonymous {
		if !b.Style.IsIdent("-fs-fit-images-to-width", "auto") {
			r := int(b.Style.FloatProportional("-fs-fit-images-to-width", float32(b.containingBlock().getContentWidth())))
			if r >= 0 {
				return r
			}
			return -1
		}
	}
	return -1
}

func (b *Box) getCSSHeight() int {
	if b.Kind == KCell {
		if b.Style.IsAutoHeight() {
			return -1
		}
		r := int(b.Style.FloatProportional("height", float32(b.containingBlock().getContentWidth())))
		bd := b.getBorder()
		r -= int(bd.Top) + int(bd.Bottom)
		p := b.getPadding()
		r -= int(p.Top) + int(p.Bottom)
		if r >= 0 {
			return r
		}
		return -1
	}
	if !b.anonymous {
		if !b.isAutoHeight() {
			if b.Style.HasAbsoluteUnit("height") {
				return int(b.Style.FloatProportional("height", 0))
			}
			return int(b.Style.FloatProportional("height", float32(b.containingBlock().getCSSHeight())))
		}
	}
	return -1
}

func (b *Box) isAutoHeight() bool {
	if b.Kind == KRow || b.Kind == KCell {
		return b.Style.IsAutoHeight() || !b.Style.HasAbsoluteUnit("height")
	}
	if b.Kind == KSection {
		return true
	}
	if b.Style.IsAutoHeight() {
		return true
	}
	if b.Style.HasAbsoluteUnit("height") {
		return false
	}
	cb := b.containingBlock()
	if cb.isStyled() && cb.isBlockish() {
		return cb.isAutoHeight()
	}
	if cb.isBlockish() && cb.isInitialCB {
		return false
	}
	return true
}

func (b *Box) isBlockish() bool { return b.Kind != KLine && b.Kind != KInline }

func (b *Box) cssMinWidth() int {
	return int(b.Style.FloatProportional("min-width", float32(b.containingBlockWidth())))
}
func (b *Box) cssMaxWidth() int {
	return int(b.Style.FloatProportional("max-width", float32(b.containingBlockWidth())))
}
func (b *Box) cssMinHeight() int {
	return int(b.Style.FloatProportional("min-height", float32(b.containingBlockCSSHeight())))
}
func (b *Box) cssMaxHeight() int {
	return int(b.Style.FloatProportional("max-height", float32(b.containingBlockCSSHeight())))
}

func (b *Box) containingBlockCSSHeight() int {
	cb := b.containingBlock()
	if !cb.isStyled() || cb.Style.IsAutoHeight() {
		return 0
	}
	if cb.Style.HasAbsoluteUnit("height") {
		return int(cb.Style.FloatProportional("height", 0))
	}
	return 0
}

func (b *Box) isFixedWidthAdvisoryOnly() bool {
	if b.Kind == KCell {
		return b.table().Style.IsIdent("table-layout", "auto")
	}
	return false
}

func (b *Box) isAllowHeightToShrink() bool {
	return !(b.Kind == KTable || b.Kind == KCell)
}

func (b *Box) getPageClearance() int {
	if b.Kind == KTable {
		return b.pageClearance
	}
	return 0
}

func (b *Box) isSkipWhenCollapsingMargins() bool {
	switch b.Kind {
	case KSection, KRow, KCell:
		return true
	case KAnon:
		for _, s := range b.inlineContent {
			st := s.GetStyle()
			if !(st.IsFloated() || st.IsAbsolute() || st.IsFixed() || st.IsRunning()) {
				return false
			}
		}
		return true
	}
	return false
}

func (b *Box) isMayCollapseMarginsWithChildren() bool {
	if b.Kind == KTable || b.Kind == KAnon {
		return false
	}
	return !b.isRoot() && b.Style.IsMayCollapseMarginsWithChildren()
}

// ---- BlockBox.calcDimensions ---------------------------------------------------

func (b *Box) calcDimensions(lc *LayoutContext) { b.calcDimensionsW(lc, b.getCSSWidth(false)) }

func (b *Box) resolveAutoMargins(lc *LayoutContext, cssWidth int, padding *RectSet, border *BorderSet) {
	if b.Kind == KTable {
		if b.minWidth <= b.contentWidth+b.marginsBordersPaddingAndSpacing(lc, true) {
			b.resolveAutoMarginsBase(lc, cssWidth, padding, border)
		} else {
			if b.Style.IsAutoLeftMargin() {
				b.setMarginLeft(0)
			}
			if b.Style.IsAutoRightMargin() {
				b.setMarginRight(0)
			}
		}
		return
	}
	b.resolveAutoMarginsBase(lc, cssWidth, padding, border)
}

func (b *Box) resolveAutoMarginsBase(lc *LayoutContext, cssWidth int, padding *RectSet, border *BorderSet) {
	withoutMargins := int(border.Left) + int(padding.Left) + cssWidth + int(padding.Right) + int(border.Right)
	if withoutMargins < b.containingBlockWidth() {
		avail := b.containingBlockWidth() - withoutMargins
		autoLeft := b.Style.IsAutoLeftMargin()
		autoRight := b.Style.IsAutoRightMargin()
		switch {
		case autoLeft && autoRight:
			b.setMarginLeft(avail / 2)
			b.setMarginRight(avail / 2)
		case autoLeft:
			b.setMarginLeft(avail)
		case autoRight:
			b.setMarginRight(avail)
		}
	}
}

func (b *Box) calcPinnedContentWidth() int {
	if !b.Style.IsIdent("left", "auto") && !b.Style.IsIdent("right", "auto") {
		pe := b.containingBlock().paddingEdge(0, 0)
		left := int(b.Style.FloatProportional("left", float32(pe.W)))
		right := int(b.Style.FloatProportional("right", float32(pe.W)))
		r := pe.W - left - right - b.leftMBP - b.rightMBP
		if r < 0 {
			return 0
		}
		return r
	}
	return -1
}

func (b *Box) calcEffPageRelativeWidth(lc *LayoutContext) int {
	totalLeft, totalRight := 0, 0
	usePage := true
	cur := b
	for {
		st := cur.Style
		if st.IsAutoWidth() && !st.IsCanBeShrunkToFit() {
			totalLeft += cur.leftMBP
			totalRight += cur.rightMBP
		} else {
			usePage = false
			break
		}
		if cur.containingBlock().isInitialCB {
			break
		}
		cur = cur.containingBlock()
	}
	if usePage {
		pb := lc.rootLayer.firstPage(lc, b)
		return pb.contentWidth() - totalLeft - totalRight
	}
	return b.containingBlockWidth() - b.leftMBP - b.rightMBP
}

func (b *Box) calcDimensionsW(lc *LayoutContext, cssWidth int) {
	if b.dimensionsCalc {
		return
	}
	style := b.Style
	padding := b.getPadding()
	border := b.getBorder()
	if cssWidth != -1 && !b.anonymous &&
		(style.IsIdent("margin-left", "auto") || style.IsIdent("margin-right", "auto")) &&
		style.IsNeedAutoMarginResolution() {
		b.resolveAutoMargins(lc, cssWidth, padding, border)
	}
	b.recalcMargin()
	margin := b.getMargin()
	b.leftMBP = int(margin.Left) + int(border.Left) + int(padding.Left)
	b.rightMBP = int(padding.Right) + int(border.Right) + int(margin.Right)
	if style.IsDynamicAutoWidth() {
		b.contentWidth = b.calcEffPageRelativeWidth(lc)
	} else {
		b.contentWidth = b.containingBlockWidth() - b.leftMBP - b.rightMBP
	}
	b.height = 0
	if !b.anonymous || (b.fromCaptionedTbl && b.floated != nil) {
		pinned := -1
		borderBox := style.IsBorderBox()
		if cssWidth != -1 {
			if borderBox {
				b.contentWidth = cssWidth - int(border.Width()) - int(padding.Width())
			} else {
				b.contentWidth = cssWidth
			}
		} else if style.IsAbsolute() || style.IsFixed() {
			pinned = b.calcPinnedContentWidth()
			if pinned != -1 {
				b.contentWidth = pinned
			}
		}
		cssHeight := b.getCSSHeight()
		if cssHeight != -1 {
			if borderBox {
				b.height = cssHeight - int(padding.Height()) - int(border.Height())
			} else {
				b.height = cssHeight
			}
		}
		re := b.replaced
		if re == nil {
			re = lc.doc.createReplacedElement(lc, b, cssWidth, cssHeight)
			if re != nil {
				re = b.fitReplacedElement(lc, re)
			}
		}
		if re != nil {
			b.contentWidth = re.intrinsicWidth()
			b.height = re.intrinsicHeight()
			b.replaced = re
		} else if cssWidth == -1 && pinned == -1 && style.IsCanBeShrunkToFit() {
			b.needShrinkToFit = true
		}
		if b.replaced == nil {
			b.applyCSSMinMaxWidth()
		}
	}
	b.dimensionsCalc = true
}

func (b *Box) fitReplacedElement(lc *LayoutContext, re *Replaced) *Replaced {
	maxW := b.getCSSFitToWidth()
	if maxW > -1 && re.intrinsicWidth() > maxW {
		oldW := float64(re.intrinsicWidth())
		scale := float64(maxW) / oldW
		return lc.doc.createReplacedElement(lc, b, maxW, int(rint(scale*float64(re.intrinsicHeight()))))
	}
	return re
}

func rint(f float64) float64 {
	// Math.rint: round half to even
	r := float64(int64(f))
	d := f - r
	switch {
	case d > 0.5 || (d == 0.5 && int64(r)%2 != 0):
		return r + 1
	case d < -0.5 || (d == -0.5 && int64(r)%2 != 0):
		return r - 1
	}
	return r
}

func (b *Box) applyCSSMinMaxWidth() {
	if !b.Style.IsMaxWidthNone() {
		mx := b.cssMaxWidth()
		if b.contentWidth > mx {
			b.contentWidth = mx
		}
	}
	mn := b.cssMinWidth()
	if mn > 0 && b.contentWidth < mn {
		b.contentWidth = mn
	}
}

func (b *Box) applyCSSMinMaxHeight() {
	if !b.Style.IsMaxHeightNone() {
		mx := b.cssMaxHeight()
		if b.height > mx {
			b.height = mx
		}
	}
	mn := b.cssMinHeight()
	if mn > 0 && b.height < mn {
		b.height = mn
	}
}

func (b *Box) recalcMargin() {
	if b.topMarginCalc && b.bottomMarginCalc {
		return
	}
	tm := b.Style.value("margin-top")
	resetTop := len(tm.vals) > 0 && tm.vals[0].Kind == VLength || len(tm.vals) > 0 && tm.vals[0].Kind == VPercent
	// LengthValue && !hasAbsoluteUnit  => percentage margins
	resetTop = len(tm.vals) > 0 && tm.vals[0].Kind == VPercent
	bm := b.Style.value("margin-bottom")
	resetBottom := len(bm.vals) > 0 && bm.vals[0].Kind == VPercent
	if !resetTop && !resetBottom {
		return
	}
	styleMargin := b.styleMarginNC()
	working := b.getMargin()
	if !b.topMarginCalc && styleMargin.Top != working.Top {
		b.setMarginTop(int(styleMargin.Top))
	}
	if !b.bottomMarginCalc && styleMargin.Bottom != working.Bottom {
		b.setMarginBottom(int(styleMargin.Bottom))
	}
}

func (b *Box) calcClearance(lc *LayoutContext) {
	if b.Style.IsCleared() && !b.Style.IsFloated() {
		lc.translate(0, -b.y)
		lc.bfc().fm.clear(lc.bfc(), b)
		lc.translate(0, b.y)
		b.calcCanvasLocation()
	}
}

func (b *Box) calcExtraPageClearance(lc *LayoutContext) {
	if lc.isPageBreaksAllowed() && lc.extraTop > 0 && (b.Style.IsSpecifiedAsBlock() || b.Style.IsListItem()) {
		first := lc.rootLayer.firstPage(lc, b)
		if first != nil && first.top+lc.extraTop > b.absY {
			diff := first.top + lc.extraTop - b.absY
			b.y += diff
			lc.translate(0, diff)
			b.calcCanvasLocation()
		}
	}
}

func (b *Box) addBoxID(lc *LayoutContext) {
	if !b.anonymous && b.Elem != nil {
		if b.Elem.Name == "a" {
			if n, ok := b.Elem.AttrOK("name"); ok && n != "" {
				lc.doc.boxIDs[n] = b
			}
		}
		if id := b.Elem.Attr("id"); id != "" {
			lc.doc.boxIDs[id] = b
		}
	}
}

// layout is BlockBox.layout(LayoutContext, int).
func (b *Box) layout(lc *LayoutContext, contentStart int) {
	style := b.Style
	b.calcClearance(lc)
	pushedBFC := false
	if b.isRoot() || style.EstablishesBFC() || b.isMarginAreaRoot() {
		lc.pushBFC(newBFC(b))
		pushedBFC = true
	}
	b.addBoxID(lc)
	if style.IsIdent("-fs-page-sequence", "start") {
		lc.rootLayer.addPageSequence(b)
	}
	b.calcDimensions(lc)
	b.calcShrinkToFitWidthIfNeeded(lc)
	b.collapseMargins(lc)
	b.calcExtraPageClearance(lc)
	if pb := lc.rootLayer.firstPage(lc, b); pb != nil && pb.top == b.absY-b.getPageClearance() {
		b.resetTopMargin()
	}
	border := b.getBorder()
	margin := b.getMargin()
	padding := b.getPadding()
	originalHeight := b.height
	if b.replaced == nil {
		b.height = 0
	}
	didSetMarker := false
	if style.IsListItem() {
		b.createMarkerData(lc)
		lc.markerData = b.markerData
		didSetMarker = true
	}
	tx := int(margin.Left) + int(border.Left) + int(padding.Left)
	ty := int(margin.Top) + int(border.Top) + int(padding.Top)
	b.tx, b.ty = tx, ty
	lc.translate(b.tx, b.ty)
	if b.replaced == nil {
		b.layoutChildren(lc, contentStart)
	}
	lc.translate(-b.tx, -b.ty)
	b.childrenHeight = b.height
	if b.replaced == nil {
		if !b.isAutoHeight() {
			delta := originalHeight - b.height
			if delta > 0 || b.isAllowHeightToShrink() {
				b.height = originalHeight
			}
		}
		b.applyCSSMinMaxHeight()
	}
	if b.isRoot() || style.EstablishesBFC() {
		if style.IsAutoHeight() {
			delta := lc.bfc().fm.getClearDelta(b.ty + b.height)
			if delta > 0 {
				b.height += delta
				b.childrenHeight += delta
			}
		}
	}
	if didSetMarker {
		lc.markerData = nil
	}
	b.calcLayoutHeight(lc, border, margin, padding)
	if pushedBFC {
		lc.popBFC()
	}
}

func newBFC(master *Box) *BlockFormattingContext {
	fm := &FloatManager{master: master}
	master.persistentBFC = fm
	return &BlockFormattingContext{fm: fm}
}

func (b *Box) calcLayoutHeight(lc *LayoutContext, border *BorderSet, margin *RectSet, padding *RectSet) {
	switch b.Kind {
	case KRow:
		b.rowCalcLayoutHeight(lc, border, margin, padding)
		return
	}
	extra := int(margin.Top) + int(border.Top) + int(padding.Top) + int(padding.Bottom) + int(border.Bottom) + int(margin.Bottom)
	b.height += extra
	b.childrenHeight += extra
	if b.Kind == KTable && len(b.children) > 0 {
		b.height += b.Style.BorderVSpacing()
	}
}

func (b *Box) calcShrinkToFitWidthIfNeeded(lc *LayoutContext) {
	if b.needShrinkToFit {
		b.contentWidth = b.calcShrinkToFitWidth(lc) - b.leftMBP - b.rightMBP
		b.applyCSSMinMaxWidth()
		b.needShrinkToFit = false
	}
}

func (b *Box) calcShrinkToFitWidth(lc *LayoutContext) int {
	b.calcMinMaxWidth(lc)
	mw := b.minWidth
	av := b.availableWidth(lc)
	if av > mw {
		mw = av
	}
	if b.maxWidth < mw {
		return b.maxWidth
	}
	return mw
}

func (b *Box) availableWidth(lc *LayoutContext) int {
	if !b.Style.IsAbsolute() {
		return b.containingBlockWidth()
	}
	left, right := 0, 0
	if !b.Style.IsIdent("left", "auto") {
		left = int(b.Style.FloatProportional("left", float32(b.containingBlock().getContentWidth())))
	}
	if !b.Style.IsIdent("right", "auto") {
		right = int(b.Style.FloatProportional("right", float32(b.containingBlock().getContentWidth())))
	}
	return b.containingBlock().paddingWidth() - left - right
}

// layoutChildren dispatches to the class specific implementation.
func (b *Box) layoutChildren(lc *LayoutContext, contentStart int) {
	switch b.Kind {
	case KTable:
		b.tableLayoutChildren(lc, contentStart)
		return
	case KSection:
		b.sectionLayoutChildren(lc, contentStart)
		return
	case KRow:
		b.rowLayoutChildren(lc, contentStart)
		return
	}
	b.ensureChildren(lc)
	switch b.childrenType {
	case ContentInline:
		b.layoutInlineChildren(lc, contentStart, b.calcInitialBreakAtLine(lc), true)
	case ContentBlock:
		if b.Style != nil && b.Style.IsFlex() && !strings.HasPrefix(b.Style.Ident("flex-direction"), "column") {
			b.layoutFlexRow(lc)
		} else {
			b.layoutBlockContent(lc, contentStart)
		}
	}
}

// layoutFlexRow lays the children out as a single flex row (no wrapping).
func (b *Box) layoutFlexRow(lc *LayoutContext) {
	items := append([]*Box(nil), b.children...)
	var laid []*Box
	for _, it := range items {
		if it.Kind == KAnon {
			// anonymous text between items takes no part in the row
			continue
		}
		layoutBlockChild0(lc, b, it, false, 0, noPageTrim)
		laid = append(laid, it)
	}
	avail := b.contentWidth
	total := 0
	for _, it := range laid {
		total += it.width()
	}
	free := avail - total
	n := len(laid)
	pos := 0
	gap := 0
	switch b.Style.Ident("justify-content") {
	case "flex-end", "end", "right":
		pos = free
	case "center":
		pos = free / 2
	case "space-between":
		if n > 1 && free > 0 {
			gap = free / (n - 1)
		}
	case "space-around":
		if n > 0 && free > 0 {
			gap = free / n
			pos = gap / 2
		}
	case "space-evenly":
		if n > 0 && free > 0 {
			gap = free / (n + 1)
			pos = gap
		}
	}
	h := 0
	for i, it := range laid {
		if free < 0 && b.Style.Ident("justify-content") == "flex-end" {
			pos = 0
		}
		it.x = pos
		pos += it.width() + gap
		if i == n-1 && b.Style.Ident("justify-content") == "space-between" && n > 1 && free > 0 {
			it.x = avail - it.width()
		}
		it.calcCanvasLocation()
		it.calcChildLocations()
		if y := it.y + it.height; y > h {
			h = y
		}
	}
	if h > b.height {
		b.height = h
	}
}

func (b *Box) layoutInlineChildren(lc *LayoutContext, contentStart, breakAtLine int, tryAgain bool) {
	b.layoutInlineContent(lc, contentStart, breakAtLine)
	if lc.isPageBreaksAllowed() && len(b.children) > 1 {
		b.satisfyWidowsAndOrphans(lc, contentStart, tryAgain)
	}
	if tryAgain && b.Style.IsTextJustify() {
		for _, l := range b.children {
			l.justify()
		}
	}
}

func (b *Box) satisfyWidowsAndOrphans(lc *LayoutContext, contentStart int, tryAgain bool) {
	first := b.children[0]
	firstPage := lc.rootLayer.firstPage(lc, first)
	if firstPage == nil {
		return
	}
	noContent := 0
	i := 0
	cCount := len(b.children)
	for i < cCount {
		l := b.children[i]
		if l.absY >= firstPage.bottom {
			break
		}
		if !l.containsContent {
			noContent++
		}
		i++
	}
	if i != cCount {
		orphans := int(b.Style.AsFloat("orphans"))
		if i-noContent < orphans {
			b.needPageClear = true
		} else {
			lastLine := b.children[cCount-1]
			pages := lc.rootLayer.pages
			lastPage := pages[firstPage.pageNo+1]
			for lastPage.pageNo != len(pages)-1 && lastPage.bottom < lastLine.absY {
				lastPage = pages[lastPage.pageNo+1]
			}
			noContent = 0
			i = cCount - 1
			for i >= 0 && b.children[i].absY >= lastPage.top {
				l := b.children[i]
				if l.absY < lastPage.top {
					break
				}
				if !l.containsContent {
					noContent++
				}
				i--
			}
			widows := int(b.Style.AsFloat("widows"))
			if cCount-1-i-noContent < widows {
				if cCount-1-widows < orphans {
					b.needPageClear = true
				} else if tryAgain {
					breakAt := cCount - 1 - widows
					b.resetChildren(lc)
					b.removeAllChildren()
					b.layoutInlineChildren(lc, contentStart, breakAt, false)
				}
			}
		}
	}
}

func (b *Box) calcInitialBreakAtLine(lc *LayoutContext) int {
	if lc.breakAtLine != nil && lc.breakAtLine.block == b {
		return lc.breakAtLine.line
	}
	return 0
}

func (b *Box) isCurrentBreakAtLineContext(lc *LayoutContext) bool {
	return lc.breakAtLine != nil && lc.breakAtLine.block == b
}

func (b *Box) calcBreakAtLineContext(lc *LayoutContext) *breakAtLineContext {
	if !b.Style.IsKeepWithInline() {
		return nil
	}
	breakLine := b.findLastNthLineBox(int(b.Style.AsFloat("widows")))
	if breakLine != nil {
		linePage := lc.rootLayer.lastPageOf(lc, breakLine)
		ourPage := lc.rootLayer.lastPageOf(lc, b)
		if linePage != nil && ourPage != nil && linePage.pageNo+1 == ourPage.pageNo {
			bb := breakLine.parent
			return &breakAtLineContext{block: bb, line: bb.findOffset(breakLine)}
		}
	}
	return nil
}

func (b *Box) findOffset(child *Box) int {
	for i, c := range b.children {
		if c == child {
			return i
		}
	}
	return -1
}

type lastLineCtx struct {
	current int
	line    *Box
}

func (b *Box) findLastNthLineBox(n int) *Box {
	ctx := &lastLineCtx{current: n}
	b.findLastLineBoxCtx(ctx)
	return ctx.line
}

func (b *Box) findLastLineBoxCtx(ctx *lastLineCtx) {
	n := len(b.children)
	if n == 0 {
		return
	}
	switch b.childrenType {
	case ContentInline:
		for i := n - 1; i >= 0; i-- {
			c := b.children[i]
			if c.height > 0 {
				ctx.line = c
				ctx.current--
				if ctx.current == 0 {
					return
				}
			}
		}
	case ContentBlock:
		for i := n - 1; i >= 0; i-- {
			b.children[i].findLastLineBoxCtx(ctx)
			if ctx.current == 0 {
				break
			}
		}
	}
}

func (b *Box) findLastLineBox() *Box {
	n := len(b.children)
	if n == 0 {
		return nil
	}
	switch b.childrenType {
	case ContentInline:
		for i := n - 1; i >= 0; i-- {
			if b.children[i].height > 0 {
				return b.children[i]
			}
		}
	case ContentBlock:
		for i := n - 1; i >= 0; i-- {
			if r := b.children[i].findLastLineBox(); r != nil {
				return r
			}
		}
	}
	return nil
}

func (b *Box) findFirstLineBox() *Box {
	n := len(b.children)
	if n == 0 {
		return nil
	}
	switch b.childrenType {
	case ContentInline:
		for i := 0; i < n; i++ {
			if b.children[i].height > 0 {
				return b.children[i]
			}
		}
	case ContentBlock:
		for i := 0; i < n; i++ {
			if r := b.children[i].findFirstLineBox(); r != nil {
				return r
			}
		}
	}
	return nil
}

func (b *Box) isNeedsKeepWithInline(lc *LayoutContext) bool {
	if b.Style.IsKeepWithInline() {
		line := b.findFirstLineBox()
		if line != nil {
			lp := lc.rootLayer.firstPage(lc, line)
			op := lc.rootLayer.firstPage(lc, b)
			return lp != nil && op != nil && lp.pageNo == op.pageNo+1
		}
	}
	return false
}

// ---- margin collapsing -----------------------------------------------------------

func (b *Box) collapseMargins(lc *LayoutContext) {
	if b.topMarginCalc && b.bottomMarginCalc {
		return
	}
	b.recalcMargin()
	margin := b.getMargin()
	if !b.topMarginCalc && !b.bottomMarginCalc && b.isVerticalMarginsAdjoin(lc) {
		cm := b.pendingCollapse
		if cm == nil {
			cm = &marginCollapseResult{}
		}
		b.collapseEmptySubtreeMargins(lc, cm)
		b.setCollapsedBottomMargin(lc, margin, cm)
		return
	}
	if !b.topMarginCalc {
		cm := b.pendingCollapse
		if cm == nil {
			cm = &marginCollapseResult{}
		}
		b.collapseTopMargin(lc, true, cm)
		if int(margin.Top) != cm.margin() {
			b.setMarginTop(cm.margin())
		}
	}
	if !b.bottomMarginCalc {
		cm := &marginCollapseResult{}
		b.collapseBottomMargin(lc, true, cm)
		b.setCollapsedBottomMargin(lc, margin, cm)
	}
}

func (b *Box) setCollapsedBottomMargin(lc *LayoutContext, margin *RectSet, cm *marginCollapseResult) {
	var next *Box
	if !b.isInlineChild() {
		next = b.nextCollapsableSibling(cm)
	}
	if next != nil && next.Kind != KAnon && cm.hasMargin() {
		next.pendingCollapse = cm
		b.setMarginBottom(0)
	} else if int(margin.Bottom) != cm.margin() {
		b.setMarginBottom(cm.margin())
	}
}

func (b *Box) nextCollapsableSibling(cm *marginCollapseResult) *Box {
	next := b.nextSibling()
	for next != nil {
		if next.Kind == KAnon {
			next.provideSiblingMarginToFloats(cm.margin())
		}
		if !next.isSkipWhenCollapsingMargins() {
			break
		}
		next = next.nextSibling()
	}
	return next
}

func (b *Box) provideSiblingMarginToFloats(margin int) {
	for _, s := range b.inlineContent {
		if bx, ok := s.(*Box); ok && bx.floated != nil {
			bx.floated.marginFromSibling = margin
		}
	}
}

func (b *Box) collapseTopMargin(lc *LayoutContext, calcRoot bool, res *marginCollapseResult) {
	if b.topMarginCalc {
		return
	}
	if !b.isSkipWhenCollapsingMargins() {
		b.calcDimensions(lc)
		if b.Style.IsDynamicAutoWidthApplicable() {
			b.dimensionsCalc = false
		}
		margin := b.getMargin()
		res.update(int(margin.Top))
		if !calcRoot && int(margin.Top) != 0 {
			b.setMarginTop(0)
		}
		if b.isMayCollapseMarginsWithChildren() && b.noTopPaddingOrBorder() {
			b.ensureChildren(lc)
			if b.childrenType == ContentBlock {
				for _, child := range b.children {
					child.collapseTopMargin(lc, false, res)
					if child.isSkipWhenCollapsingMargins() {
						continue
					}
					break
				}
			}
		}
	}
	b.topMarginCalc = true
}

func (b *Box) collapseBottomMargin(lc *LayoutContext, calcRoot bool, res *marginCollapseResult) {
	if b.bottomMarginCalc {
		return
	}
	if !b.isSkipWhenCollapsingMargins() {
		b.calcDimensions(lc)
		if b.Style.IsDynamicAutoWidthApplicable() {
			b.dimensionsCalc = false
		}
		margin := b.getMargin()
		res.update(int(margin.Bottom))
		if !calcRoot && int(margin.Bottom) != 0 {
			b.setMarginBottom(0)
		}
		if b.isMayCollapseMarginsWithChildren() && !b.Style.IsTable() && b.noBottomPaddingOrBorder() {
			b.ensureChildren(lc)
			if b.childrenType == ContentBlock {
				for i := len(b.children) - 1; i >= 0; i-- {
					child := b.children[i]
					if child.isSkipWhenCollapsingMargins() {
						continue
					}
					child.collapseBottomMargin(lc, false, res)
					break
				}
			}
		}
	}
	b.bottomMarginCalc = true
}

func (b *Box) noTopPaddingOrBorder() bool {
	return int(b.getPadding().Top) == 0 && int(b.getBorder().Top) == 0
}
func (b *Box) noBottomPaddingOrBorder() bool {
	return int(b.getPadding().Bottom) == 0 && int(b.getBorder().Bottom) == 0
}

func (b *Box) collapseEmptySubtreeMargins(lc *LayoutContext, res *marginCollapseResult) {
	margin := b.getMargin()
	res.update(int(margin.Top))
	res.update(int(margin.Bottom))
	b.setMarginTop(0)
	b.topMarginCalc = true
	b.setMarginBottom(0)
	b.bottomMarginCalc = true
	b.ensureChildren(lc)
	if b.childrenType == ContentBlock {
		for _, c := range b.children {
			c.collapseEmptySubtreeMargins(lc, res)
		}
	}
}

func (b *Box) isVerticalMarginsAdjoin(lc *LayoutContext) bool {
	style := b.Style
	bw := style.Border()
	padding := b.getPadding()
	if int(bw.Top) != 0 || int(bw.Bottom) != 0 || int(padding.Top) != 0 || int(padding.Bottom) != 0 {
		return false
	}
	b.ensureChildren(lc)
	if b.childrenType == ContentInline {
		return false
	}
	if b.childrenType == ContentBlock {
		for _, c := range b.children {
			if c.isSkipWhenCollapsingMargins() || !c.isVerticalMarginsAdjoin(lc) {
				return false
			}
		}
	}
	return style.AsFloat("min-height") == 0 && (b.isAutoHeight() || style.AsFloat("height") == 0)
}

// ---- min/max widths -----------------------------------------------------------------

func (b *Box) calcMinMaxWidth(lc *LayoutContext) {
	if b.Kind == KTable {
		b.tableCalcMinMaxWidth(lc)
		return
	}
	if b.minMaxCalculated {
		return
	}
	margin := b.getMargin()
	border := b.getBorder()
	padding := b.getPadding()
	width := b.getCSSWidth(true)
	if width == -1 {
		if b.replaced != nil {
			width = b.replaced.intrinsicWidth()
		} else {
			height := b.getCSSHeight()
			re := lc.doc.createReplacedElement(lc, b, width, height)
			if re != nil {
				re = b.fitReplacedElement(lc, re)
				b.replaced = re
				width = re.intrinsicWidth()
			}
		}
	}
	mbp := int(margin.Left) + int(border.Left) + int(padding.Left) + int(margin.Right) + int(border.Right) + int(padding.Right)
	if b.replaced != nil || (width != -1 && !b.isFixedWidthAdvisoryOnly()) {
		b.minWidth = mbp + width
		b.maxWidth = b.minWidth
	} else {
		cw := -1
		if width != -1 {
			cw = b.getContentWidth()
			b.contentWidth = width
		}
		b.minWidth = mbp
		b.maxWidth = mbp
		minimumMax := b.maxWidth
		if width != -1 {
			minimumMax += width
		}
		b.ensureChildren(lc)
		switch b.childrenType {
		case ContentBlock:
			b.calcMinMaxWidthBlockChildren(lc)
		case ContentInline:
			b.calcMinMaxWidthInlineChildren(lc)
		}
		if minimumMax > b.maxWidth {
			b.maxWidth = minimumMax
		}
		if cw != -1 {
			b.contentWidth = cw
		}
	}
	if b.replaced == nil {
		b.calcMinMaxCSSMinMaxWidth(margin, border, padding)
	}
	b.minMaxCalculated = true
}

func (b *Box) calcMinMaxCSSMinMaxWidth(margin *RectSet, border *BorderSet, padding *RectSet) {
	mbp := int(margin.Left) + int(border.Left) + int(padding.Left) + int(margin.Right) + int(border.Right) + int(padding.Right)
	mn := b.cssMinWidth()
	if mn > 0 {
		mn += mbp
		if b.minWidth < mn {
			b.minWidth = mn
		}
	}
	if !b.Style.IsMaxWidthNone() {
		mx := b.cssMaxWidth()
		mx += mbp
		if b.maxWidth > mx {
			if mx > b.minWidth {
				b.maxWidth = mx
			} else {
				b.maxWidth = b.minWidth
			}
		}
	}
}

func (b *Box) calcMinMaxWidthBlockChildren(lc *LayoutContext) {
	cmin, cmax := 0, 0
	for _, c := range b.children {
		c.calcMinMaxWidth(lc)
		if c.minWidth > cmin {
			cmin = c.minWidth
		}
		if c.maxWidth > cmax {
			cmax = c.maxWidth
		}
	}
	b.minWidth += cmin
	b.maxWidth += cmax
}

func (b *Box) calcMinMaxWidthInlineChildren(lc *LayoutContext) {
	textIndent := int(b.Style.FloatProportional("text-indent", float32(b.getContentWidth())))
	if b.Style.IsListItem() && b.Style.IsListMarkerInside() {
		b.createMarkerData(lc)
		textIndent += b.markerData.layoutWidth()
	}
	childMin, childMax, lineWidth := 0, 0, 0
	var trimmable *InlineSrc
	for _, child := range b.inlineContent {
		st := child.GetStyle()
		if st.IsAbsolute() || st.IsFixed() || st.IsRunning() {
			continue
		}
		if st.IsFloated() || st.IsInlineBlock() || st.IsInlineTable() {
			if st.IsFloated() && st.IsCleared() {
				if trimmable != nil {
					lineWidth -= trimmable.trailingSpaceWidth()
				}
				if lineWidth > childMax {
					childMax = lineWidth
				}
				lineWidth = 0
			}
			trimmable = nil
			blk := child.(*Box)
			blk.calcMinMaxWidth(lc)
			lineWidth += blk.maxWidth
			if blk.minWidth > childMin {
				childMin = blk.minWidth
			}
			continue
		}
		ib := child.(*InlineSrc)
		ws := ib.style.Whitespace()
		ib.calcMinMaxWidth(b.getContentWidth(), lineWidth == 0)
		switch ws {
		case "nowrap":
			lineWidth += textIndent + ib.maxWidth
			if ib.minWidth > childMin {
				childMin = ib.minWidth
			}
			trimmable = ib
		case "pre":
			if trimmable != nil {
				lineWidth -= trimmable.trailingSpaceWidth()
			}
			trimmable = nil
			if lineWidth > childMax {
				childMax = lineWidth
			}
			lineWidth = textIndent + ib.firstLineWidth
			if lineWidth > childMin {
				childMin = lineWidth
			}
			lineWidth = ib.maxWidth
			if lineWidth > childMin {
				childMin = lineWidth
			}
			if childMin > childMax {
				childMax = childMin
			}
			lineWidth = 0
		case "pre-wrap", "pre-line":
			lineWidth += textIndent + ib.firstLineWidth
			if trimmable != nil {
				lineWidth -= trimmable.trailingSpaceWidth()
			}
			if lineWidth > childMax {
				childMax = lineWidth
			}
			if ib.maxWidth > childMax {
				childMax = ib.maxWidth
			}
			if ib.minWidth > childMin {
				childMin = ib.minWidth
			}
			if ws == "pre-line" {
				trimmable = ib
			} else {
				trimmable = nil
			}
			lineWidth = 0
		default:
			lineWidth += textIndent + ib.maxWidth
			if ib.minWidth > childMin {
				childMin = textIndent + ib.minWidth
			}
			trimmable = ib
		}
		if textIndent > 0 {
			textIndent = 0
		}
	}
	if trimmable != nil {
		lineWidth -= trimmable.trailingSpaceWidth()
	}
	if lineWidth > childMax {
		childMax = lineWidth
	}
	b.minWidth += childMin
	b.maxWidth += childMax
}

// ---- reset ---------------------------------------------------------------------------

func (b *Box) resetChildren(lc *LayoutContext) {
	switch b.Kind {
	case KInline:
		for _, o := range b.inlineChildren {
			if c, ok := o.(*Box); ok {
				c.reset(lc)
			}
		}
		return
	}
	for _, c := range b.children {
		c.reset(lc)
	}
}

func (b *Box) resetChildrenRange(lc *LayoutContext, start, end int) {
	for i := start; i <= end; i++ {
		b.children[i].reset(lc)
	}
}

// reset is the (virtual) Box.reset.
func (b *Box) reset(lc *LayoutContext) {
	switch b.Kind {
	case KLine:
		for _, c := range b.nonFlowContent {
			c.reset(lc)
		}
		if b.markerData != nil {
			b.markerData.restorePreviousReferenceLine(b)
		}
	case KInline:
		b.resetChildren(lc)
		b.contentWidth = 0
		b.workingMargin = nil
		return
	}
	b.resetChildren(lc)
	b.contentWidth = 0
	b.workingMargin = nil
	if b.Elem != nil {
		if b.Elem.Name == "a" {
			if n, ok := b.Elem.AttrOK("name"); ok {
				delete(lc.doc.boxIDs, n)
			}
		}
		if id := b.Elem.Attr("id"); id != "" {
			delete(lc.doc.boxIDs, id)
		}
	}
	switch b.Kind {
	case KLine:
		return
	}
	// BlockBox.reset
	b.topMarginCalc = false
	b.bottomMarginCalc = false
	b.dimensionsCalc = false
	b.minMaxCalculated = false
	b.childrenHeight = 0
	if b.replaced != nil {
		b.replaced = nil
	}
	if b.childrenType == ContentInline {
		b.removeAllChildren()
	}
	if b.floated != nil && b.floated.manager != nil {
		b.floated.manager.removeFloat(b)
		lc.rootLayer.removeFloat(b)
	}
	if b.Style != nil && b.Style.IsRunning() {
		lc.rootLayer.removeRunningBlock(b)
	}
	switch b.Kind {
	case KTable:
		if b.tableLayout != nil {
			b.tableLayout.reset()
		}
	case KSection:
		b.grid = nil
		b.needCellWidthCalc = true
		b.needCellRecalc = true
	case KRow:
		b.haveBaseline = false
		b.section().needCellWidthCalc = true
	}
}

// ---- page breaking helpers ---------------------------------------------------------------

func (b *Box) forcePageBreakBefore(lc *LayoutContext, value string, pendingPageName bool) int {
	delta := b.forcePageBreakBeforeBase(lc, value, pendingPageName)
	if b.Kind == KRow && b.Style.IsCollapseBorders() {
		page := lc.rootLayer.pageAt(lc, b.absY+delta)
		if page != nil {
			spill := 0
			for _, cell := range b.children {
				if cb := cell.collapsedPaintingBorder; cb != nil {
					if s := int(cb.Top) / 2; s > spill {
						spill = s
					}
				}
			}
			borderTop := b.absY + delta + int(b.getMargin().Top) - spill
			rowDelta := page.top - borderTop
			if rowDelta > 0 {
				b.y += rowDelta
				delta += rowDelta
			}
		}
	}
	return delta
}

func (b *Box) forcePageBreakBeforeBase(lc *LayoutContext, value string, pending bool) int {
	page := lc.rootLayer.firstPage(lc, b)
	if page == nil {
		return 0
	}
	count := 1
	if page.top == b.absY {
		count--
		if pending && page == lc.rootLayer.lastPage() {
			lc.rootLayer.removeLastPage()
			lc.pageName = lc.pendingPage
			lc.rootLayer.addPage(lc)
		}
	}
	if (page.isLeftPage() && value == "left") || (page.isRightPage() && value == "right") {
		count++
	}
	if count == 0 {
		return 0
	}
	if count == 1 && pending {
		lc.pageName = lc.pendingPage
	}
	delta := page.bottom + lc.extraTop - b.absY
	if page == lc.rootLayer.lastPage() {
		lc.rootLayer.addPage(lc)
	}
	if count == 2 {
		page = lc.rootLayer.pages[page.pageNo+1]
		delta += page.contentHeight()
		if pending {
			lc.pageName = lc.pendingPage
		}
		if page == lc.rootLayer.lastPage() {
			lc.rootLayer.addPage(lc)
		}
	}
	b.y += delta
	return delta
}

func (b *Box) forcePageBreakAfter(lc *LayoutContext, value string) {
	needSecond := false
	page := lc.rootLayer.lastPageOf(lc, b)
	if page != nil {
		if (page.isLeftPage() && value == "left") || (page.isRightPage() && value == "right") {
			needSecond = true
		}
		delta := page.bottom + lc.extraTop - (b.absY + b.getMarginBorderPadding(Top) + b.height)
		if page == lc.rootLayer.lastPage() {
			lc.rootLayer.addPage(lc)
		}
		if needSecond {
			page = lc.rootLayer.pages[page.pageNo+1]
			delta += page.contentHeight()
			if page == lc.rootLayer.lastPage() {
				lc.rootLayer.addPage(lc)
			}
		}
		b.height += delta
	}
}

func (b *Box) crossesPageBreak(lc *LayoutContext) bool {
	if !lc.isPageBreaksAllowed() {
		return false
	}
	pb := lc.rootLayer.firstPage(lc, b)
	if pb == nil {
		return false
	}
	return b.absY+b.height >= pb.bottom-lc.extraBottom
}

func (b *Box) checkPageContext(lc *LayoutContext) bool {
	if !b.Style.IsIdent("page", "auto") {
		pageName := b.Style.AsString("page")
		if pageName != lc.pageName && b.isInDocumentFlow() && b.isContainsInlineContent(lc) {
			lc.pendingPage = pageName
			return true
		}
	} else if lc.pageName != "" && b.isInDocumentFlow() {
		lc.pendingPage = ""
		return true
	}
	return false
}

func (b *Box) isInDocumentFlow() bool {
	flowRoot := b
	for flowRoot.parent != nil {
		flowRoot = flowRoot.parent
	}
	return flowRoot.isRoot()
}

func (b *Box) isContainsInlineContent(lc *LayoutContext) bool {
	b.ensureChildren(lc)
	switch b.childrenType {
	case ContentInline:
		return true
	case ContentEmpty:
		return false
	case ContentBlock:
		for _, c := range b.children {
			if c.isContainsInlineContent(lc) {
				return true
			}
		}
		return false
	}
	return false
}

// calcBaseline mirrors BlockBox.calcBaseline (+ cell override).
func (b *Box) calcBaseline(lc *LayoutContext) int {
	r := b.calcBlockBaseline(lc)
	if b.Kind == KCell && r == noBaseline {
		ca := b.contentAreaEdge(b.absX, b.absY)
		return ca.Y
	}
	return r
}

func (b *Box) calcBlockBaseline(lc *LayoutContext) int {
	for _, c := range b.children {
		if c.Kind == KLine {
			return c.absY + c.lineBaseline
		}
		if c.Kind == KRow {
			return c.absY + c.baseline
		}
		if r := c.calcBlockBaseline(lc); r != noBaseline {
			return r
		}
	}
	return noBaseline
}

func (b *Box) calcInlineBaseline() int {
	if b.replaced != nil && b.replaced.hasBaseline() {
		ca := b.contentAreaEdge(b.absX, b.absY)
		return ca.Y + b.replaced.baseline() - b.absY
	}
	if b.Kind == KTable {
		for _, sec := range b.children {
			for _, row := range sec.children {
				return row.absY + row.baseline - b.absY
			}
		}
		return b.height
	}
	last := b.findLastLineBox()
	if last == nil {
		return b.height
	}
	return last.absY + last.lineBaseline - b.absY
}

func (b *Box) initStaticPos(lc *LayoutContext, parent *Box, childOffset int) {
	if b.Kind == KRow {
		b.x = 0
		table := b.table()
		b.y = parent.height + table.Style.BorderVSpacing()
		lc.translate(0, b.y-childOffset)
		return
	}
	b.x = 0
	b.y = childOffset
}

// positionRelative applies position:relative offsets.
func (b *Box) positionRelative() (int, int) {
	initialX, initialY := b.x, b.y
	st := b.Style
	cbw := float32(b.containingBlock().getContentWidth())
	if !st.IsIdent("left", "auto") {
		b.x += int(st.FloatProportional("left", cbw))
	} else if !st.IsIdent("right", "auto") {
		b.x -= int(st.FloatProportional("right", cbw))
	}
	cbh := 0
	cb := b.containingBlock()
	if cb.isStyled() && !cb.Style.IsAutoHeight() {
		cbh = int(cb.Style.FloatProportional("height", 0))
	} else if b.Style.IsInlineBlock() && b.isInlineChild() {
		cbh = cb.height
	}
	if !st.IsIdent("top", "auto") {
		b.y += int(st.FloatProportional("top", float32(cbh)))
	} else if !st.IsIdent("bottom", "auto") {
		b.y -= int(st.FloatProportional("bottom", float32(cbh)))
	}
	b.relativeOffset = &[2]int{b.x - initialX, b.y - initialY}
	return b.relativeOffset[0], b.relativeOffset[1]
}

// ---- list markers --------------------------------------------------------------------------

func (b *Box) createMarkerData(lc *LayoutContext) {
	if b.markerData != nil {
		return
	}
	strut := createDefaultStrutMetrics(b)
	md := &MarkerData{strut: strut}
	st := b.Style
	listStyle := st.Ident("list-style-type")
	imageProp := st.first("list-style-image")
	imageMarkerOK := false
	if imageProp.Kind == VURI {
		if img := lc.doc.images.get(imageProp.Str); img != nil {
			im := img.clone()
			if float32(im.height()) > strut.Ascent {
				im.scale(-1, int(strut.Ascent))
			}
			md.imageMarker = &imageMarker{img: im, layoutWidth: im.width() * 2}
			imageMarkerOK = true
		}
	}
	if listStyle != "none" && !imageMarkerOK {
		if listStyle == "circle" || listStyle == "square" || listStyle == "disc" {
			d := int((strut.Ascent + strut.Descent) / 3)
			md.glyphMarker = &glyphMarker{diameter: d, layoutWidth: d * 3}
		} else {
			text := createCounterText(listStyle, b.listCounter) + ".  "
			w := b.Style.FSFont().Width(text)
			md.textMarker = &textMarker{text: text, layoutWidth: w}
		}
	}
	b.markerData = md
}
