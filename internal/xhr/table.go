package xhr

import (
	"sort"
)

type colData struct{ span int }

type tableLayout interface {
	calcMinMaxWidth(lc *LayoutContext)
	layout(lc *LayoutContext)
	reset()
}

// collapsedBorder is CollapsedBorderValue.
type collapsedBorder struct {
	style      string
	width      int
	color      *RGB
	precedence int
}

func (c *collapsedBorder) defined() bool { return c.style != "" }
func (c *collapsedBorder) exists() bool {
	return c.style != "" && c.style != "none" && c.style != "hidden"
}
func (c *collapsedBorder) hidden() bool { return c.style == "hidden" }

func borderLeftCB(b *BorderSet, prec int) *collapsedBorder {
	return &collapsedBorder{b.LeftStyle, int(b.Left), b.LeftColor, prec}
}
func borderRightCB(b *BorderSet, prec int) *collapsedBorder {
	return &collapsedBorder{b.RightStyle, int(b.Right), b.RightColor, prec}
}
func borderTopCB(b *BorderSet, prec int) *collapsedBorder {
	return &collapsedBorder{b.TopStyle, int(b.Top), b.TopColor, prec}
}
func borderBottomCB(b *BorderSet, prec int) *collapsedBorder {
	return &collapsedBorder{b.BottomStyle, int(b.Bottom), b.BottomColor, prec}
}

const (
	bCell     = 10
	bRow      = 9
	bRowGroup = 8
	bCol      = 7
	bTable    = 6
)

var borderPriorities = map[string]int{"double": 1, "solid": 2, "dashed": 3, "dotted": 4, "ridge": 5, "outset": 6, "groove": 7, "inset": 8}

func compareBorders(b1, b2 *collapsedBorder, returnNilOnEqual bool) *collapsedBorder {
	if !b2.defined() {
		return b1
	}
	if !b1.defined() {
		return b2
	}
	if b1.style == "hidden" {
		return b1
	}
	if b2.style == "hidden" {
		return b2
	}
	if b2.style == "none" {
		return b1
	}
	if b1.style == "none" {
		return b2
	}
	if b1.width != b2.width {
		if b1.width > b2.width {
			return b1
		}
		return b2
	}
	if b1.style != b2.style {
		if borderPriorities[b1.style] > borderPriorities[b2.style] {
			return b1
		}
		return b2
	}
	if returnNilOnEqual && b1.precedence == b2.precedence {
		return nil
	}
	if b1.precedence >= b2.precedence {
		return b1
	}
	return b2
}

// ---- TableBox ---------------------------------------------------------------------------

func (t *Box) numEffCols() int { return len(t.columns) }

func (t *Box) spanOfEffCol(i int) int { return t.columns[i].span }

func (t *Box) colToEffCol(col int) int {
	c, i := 0, 0
	for c < col && i < t.numEffCols() {
		c += t.spanOfEffCol(i)
		i++
	}
	return i
}

func (t *Box) effColToCol(effCol int) int {
	c := 0
	for i := 0; i < effCol; i++ {
		c += t.spanOfEffCol(i)
	}
	return c
}

func (t *Box) appendColumn(span int) {
	t.columns = append(t.columns, &colData{span: span})
	for _, s := range t.children {
		s.extendGridToColumnCount(len(t.columns))
	}
}

func (t *Box) splitColumn(pos, firstSpan int) {
	nc := &colData{span: firstSpan}
	t.columns = append(t.columns, nil)
	copy(t.columns[pos+1:], t.columns[pos:])
	t.columns[pos] = nc
	t.columns[pos+1].span -= firstSpan
	for _, s := range t.children {
		s.sectionSplitColumn(pos)
	}
}

func (t *Box) marginsBordersPaddingAndSpacing(lc *LayoutContext, ignoreAutoMargins bool) int {
	res := 0
	m := t.getMargin()
	if !ignoreAutoMargins || !t.Style.IsAutoLeftMargin() {
		res += int(m.Left)
	}
	if !ignoreAutoMargins || !t.Style.IsAutoRightMargin() {
		res += int(m.Right)
	}
	bd := t.getBorder()
	res += int(bd.Left) + int(bd.Right)
	if !t.Style.IsCollapseBorders() {
		p := t.getPadding()
		hs := t.Style.BorderHSpacing()
		res += int(p.Left) + int(p.Right) + (t.numEffCols()+1)*hs
	}
	return res
}

func (t *Box) recalcSections(lc *LayoutContext) {
	t.ensureChildren(lc)
	for _, s := range t.children {
		s.recalcCells(lc)
	}
}

func (t *Box) calcBordersTable(lc *LayoutContext) {
	t.ensureChildren(lc)
	for _, s := range t.children {
		s.sectionCalcBorders(lc)
	}
}

func (t *Box) tableCalcMinMaxWidth(lc *LayoutContext) {
	if t.minMaxCalculated {
		return
	}
	t.recalcSections(lc)
	if t.Style.IsCollapseBorders() {
		t.calcBordersTable(lc)
	}
	t.tableLayout.calcMinMaxWidth(lc)
	t.minMaxCalculated = true
}

func (t *Box) tableCalcWidth() {
	if t.minWidth > t.width() {
		t.contentWidth += t.minWidth - t.width()
	} else if t.Style.IsIdent("width", "auto") && t.maxWidth < t.width() {
		t.contentWidth -= t.width() - t.maxWidth
	}
}

func (t *Box) tableLayoutBox(lc *LayoutContext) {
	t.calcMinMaxWidth(lc)
	t.calcDimensions(lc)
	t.tableCalcWidth()
	t.tableCalcPageClearance(lc)
	if !t.anonymous {
		t.dimensionsCalc = false
		t.calcDimensionsW(lc, t.contentWidth)
	}
	t.tableLayout.layout(lc)
	t.setCellWidthsTable(lc)
	t.layout(lc, 0)
}

func (t *Box) tableCalcPageClearance(lc *LayoutContext) {
	if t.Style.IsCollapseBorders() {
		page := lc.rootLayer.firstPage(lc, t)
		if page != nil {
			row := t.firstRow()
			if row != nil {
				spill := 0
				for _, cell := range row.children {
					if cb := cell.collapsedPaintingBorder; cb != nil {
						if s := int(cb.Top) / 2; s > spill {
							spill = s
						}
					}
				}
				borderTop := t.absY + int(t.getMargin().Top) - spill
				delta := page.top - borderTop
				if delta > 0 {
					t.y += delta
					t.pageClearance = delta
					t.calcCanvasLocation()
					lc.translate(0, delta)
				}
			}
		}
	}
}

func (t *Box) firstRow() *Box {
	for _, s := range t.children {
		if len(s.children) > 0 {
			return s.children[0]
		}
	}
	return nil
}

func (t *Box) setCellWidthsTable(lc *LayoutContext) {
	for _, b := range t.children {
		if b.Style.IsTableSection() {
			b.setCellWidths(lc)
		}
	}
}

func (t *Box) tableLayoutChildren(lc *LayoutContext, contentStart int) {
	t.ensureChildren(lc)
	// -fs-table-paginate is not supported: header/footer repetition is skipped
	t.layoutChildrenBlockOnly(lc, contentStart)
}

func (b *Box) layoutChildrenBlockOnly(lc *LayoutContext, contentStart int) {
	b.ensureChildren(lc)
	switch b.childrenType {
	case ContentInline:
		b.layoutInlineChildren(lc, contentStart, b.calcInitialBreakAtLine(lc), true)
	case ContentBlock:
		b.layoutBlockContent(lc, contentStart)
	}
}

func (t *Box) colElement(col int) *TableColumn {
	if len(t.styleColumns) == 0 {
		return nil
	}
	cCol := 0
	for _, ce := range t.styleColumns {
		cCol += ce.style.ColSpan()
		if cCol > col {
			return ce
		}
	}
	return nil
}

func (t *Box) sectionAbove(section *Box, skipEmpty bool) *Box {
	prev := section.previousSibling()
	if prev == nil {
		return nil
	}
	for prev != nil {
		if prev.numRows() > 0 || !skipEmpty {
			break
		}
		prev = prev.previousSibling()
	}
	return prev
}

func (t *Box) sectionBelow(section *Box, skipEmpty bool) *Box {
	next := section.nextSibling()
	if next == nil {
		return nil
	}
	for next != nil {
		if next.numRows() > 0 || !skipEmpty {
			break
		}
		next = next.nextSibling()
	}
	return next
}

func (t *Box) cellAbove(cell *Box) *Box {
	r := cell.row
	var section *Box
	rAbove := 0
	if r > 0 {
		section = cell.section()
		rAbove = r - 1
	} else {
		section = t.sectionAbove(cell.section(), true)
		if section != nil {
			rAbove = section.numRows() - 1
		}
	}
	if section != nil {
		effCol := t.colToEffCol(cell.col)
		var above *Box
		for {
			above = section.cellAt(rAbove, effCol)
			effCol--
			if !(above == spanningCell && effCol >= 0) {
				break
			}
		}
		if above == spanningCell {
			return nil
		}
		return above
	}
	return nil
}

func (t *Box) cellBelow(cell *Box) *Box {
	r := cell.row + cell.Style.RowSpan() - 1
	var section *Box
	rBelow := 0
	if r < cell.section().numRows()-1 {
		section = cell.section()
		rBelow = r + 1
	} else {
		section = t.sectionBelow(cell.section(), true)
		rBelow = 0
	}
	if section != nil {
		effCol := t.colToEffCol(cell.col)
		var below *Box
		for {
			below = section.cellAt(rBelow, effCol)
			effCol--
			if !(below == spanningCell && effCol >= 0) {
				break
			}
		}
		if below == spanningCell {
			return nil
		}
		return below
	}
	return nil
}

func (t *Box) cellLeft(cell *Box) *Box {
	section := cell.section()
	effCol := t.colToEffCol(cell.col)
	if effCol == 0 {
		return nil
	}
	var prev *Box
	for {
		prev = section.cellAt(cell.row, effCol-1)
		effCol--
		if !(prev == spanningCell && effCol >= 0) {
			break
		}
	}
	if prev == spanningCell {
		return nil
	}
	return prev
}

func (t *Box) cellRight(cell *Box) *Box {
	effCol := t.colToEffCol(cell.col + cell.Style.ColSpan())
	if effCol >= t.numEffCols() {
		return nil
	}
	r := cell.section().cellAt(cell.row, effCol)
	if r == spanningCell {
		return nil
	}
	return r
}

func (t *Box) calcFixedHeightRowBottom() int {
	if !t.anonymous {
		cssHeight := t.getCSSHeight()
		if cssHeight != -1 {
			return t.absY + cssHeight - int(t.getBorder().Bottom) - int(t.getPadding().Bottom) - t.Style.BorderVSpacing()
		}
	}
	return -1
}

// ---- section ---------------------------------------------------------------------------------

var spanningCell = &Box{Kind: KCell}

func (s *Box) extendGridToColumnCount(n int) {
	for i := range s.grid {
		for len(s.grid[i]) < n {
			s.grid[i] = append(s.grid[i], nil)
		}
	}
}

func (s *Box) sectionSplitColumn(pos int) {
	for i, row := range s.grid {
		cur := row[pos]
		var ins *Box
		if cur != nil {
			ins = spanningCell
		}
		row = append(row, nil)
		copy(row[pos+2:], row[pos+1:])
		row[pos+1] = ins
		s.grid[i] = row
	}
}

func (s *Box) numRows() int { return len(s.grid) }

func (s *Box) cellAt(row, col int) *Box {
	if row >= len(s.grid) {
		return nil
	}
	r := s.grid[row]
	if col >= len(r) {
		return nil
	}
	return r[col]
}

func (s *Box) setCellAt(row, col int, cell *Box) { s.grid[row][col] = cell }

func (s *Box) ensureRows(n int) {
	nCols := s.parent.numEffCols()
	for len(s.grid) < n {
		row := make([]*Box, nCols)
		s.grid = append(s.grid, row)
	}
}

func (s *Box) recalcCells(lc *LayoutContext) {
	s.grid = nil
	s.ensureChildren(lc)
	for cRow, row := range s.children {
		row.ensureChildren(lc)
		for _, cell := range row.children {
			s.addCell(row, cell, cRow)
		}
	}
}

func (s *Box) sectionCalcBorders(lc *LayoutContext) {
	s.ensureChildren(lc)
	for _, row := range s.children {
		row.ensureChildren(lc)
		for _, cell := range row.children {
			cell.calcCollapsedBorder()
		}
	}
}

func (s *Box) addCell(row, cell *Box, cRow int) {
	rSpan := cell.Style.RowSpan()
	cSpan := cell.Style.ColSpan()
	table := s.parent
	nCols := len(table.columns)
	cCol := 0
	s.ensureRows(cRow + rSpan)
	for cCol < nCols && s.cellAt(cRow, cCol) != nil {
		cCol++
	}
	col := cCol
	set := cell
	for cSpan > 0 {
		for cCol >= len(table.columns) {
			table.appendColumn(1)
		}
		cData := table.columns[cCol]
		if cSpan < cData.span {
			table.splitColumn(cCol, cSpan)
		}
		cData = table.columns[cCol]
		currentSpan := cData.span
		for r := 0; r < rSpan; r++ {
			if s.cellAt(cRow+r, cCol) == nil {
				s.setCellAt(cRow+r, cCol, set)
			}
		}
		cCol++
		cSpan -= currentSpan
		set = spanningCell
	}
	cell.row = cRow
	cell.col = table.effColToCol(col)
}

func (s *Box) setCellWidths(lc *LayoutContext) {
	table := s.parent
	columnPos := table.columnPos
	hspacing := table.Style.BorderHSpacing()
	for _, row := range s.grid {
		for j, cell := range row {
			if cell == nil || cell == spanningCell {
				continue
			}
			endCol := j
			cspan := cell.Style.ColSpan()
			for cspan > 0 && endCol < len(row) {
				cspan -= table.spanOfEffCol(endCol)
				endCol++
			}
			w := columnPos[endCol] - columnPos[j] - hspacing
			cell.setLayoutWidth(lc, w)
			cell.x = columnPos[j] + hspacing
		}
	}
}

func (s *Box) sectionLayoutChildren(lc *LayoutContext, contentStart int) {
	if s.needCellRecalc {
		s.recalcCells(lc)
		s.needCellRecalc = false
	}
	if s.needCellWidthCalc {
		s.setCellWidths(lc)
		s.needCellWidthCalc = false
	}
	s.layoutChildrenBlockOnly(lc, contentStart)
}

func (s *Box) lastRow() *Box {
	if len(s.children) > 0 {
		return s.children[len(s.children)-1]
	}
	return nil
}

// ---- cell ------------------------------------------------------------------------------------

func (c *Box) setLayoutWidth(lc *LayoutContext, width int) {
	c.calcDimensions(lc)
	c.contentWidth = width - c.leftMBP - c.rightMBP
}

func (c *Box) outerStyleWidth() *Length {
	r := c.Style.AsLength("width")
	if r.IsVariable() || r.IsPercent() {
		return r
	}
	bp := 0
	bd := c.getBorder()
	bp += int(bd.Left) + int(bd.Right)
	p := c.getPadding()
	bp += int(p.Left) + int(p.Right)
	r.Value += int64(bp)
	return r
}

func (c *Box) outerStyleOrColWidth() *Length {
	r := c.outerStyleWidth()
	if c.Style.ColSpan() > 1 || !r.IsVariable() {
		return r
	}
	col := c.table().colElement(c.col)
	if col != nil {
		r = col.style.AsLength("width")
	}
	return r
}

func (c *Box) cellVerticalAlign() string {
	v := c.Style.Ident("vertical-align")
	if v == "top" || v == "middle" || v == "bottom" {
		return v
	}
	return "baseline"
}

func (c *Box) moveContent(lc *LayoutContext, deltaY int) {
	for _, b := range c.children {
		b.y += deltaY
	}
	if c.persistentBFC != nil {
		c.persistentBFC.performOnFloats(func(f *Box) { f.y += deltaY })
	}
	c.calcChildLocations()
}

func (c *Box) isPageBreaksChange(lc *LayoutContext, posDeltaY int) bool {
	if !lc.isPageBreaksAllowed() {
		return false
	}
	page := lc.rootLayer.firstPage(lc, c)
	bottomEdge := c.absY + c.childrenHeight
	return page != nil && (bottomEdge >= page.bottom-lc.extraBottom || bottomEdge+posDeltaY >= page.bottom-lc.extraBottom)
}

func (c *Box) calcCollapsedBorder() {
	top := c.collapsedTopBorder()
	right := c.collapsedRightBorder()
	bottom := c.collapsedBottomBorder()
	left := c.collapsedLeftBorder()
	c.collapsedPaintingBorder = bordersetFromCB(top, right, bottom, left)
	top.width = (top.width + 1) / 2
	right.width = right.width / 2
	bottom.width = bottom.width / 2
	left.width = (left.width + 1) / 2
	c.collapsedLayoutBorder = bordersetFromCB(top, right, bottom, left)
	c.cbTop, c.cbRight, c.cbBottom, c.cbLeft = top, right, bottom, left
}

func bordersetFromCB(top, right, bottom, left *collapsedBorder) *BorderSet {
	return &BorderSet{
		RectSet:  RectSet{Top: float32(top.width), Right: float32(right.width), Bottom: float32(bottom.width), Left: float32(left.width)},
		TopStyle: top.style, RightStyle: right.style, BottomStyle: bottom.style, LeftStyle: left.style,
		TopColor: top.color, RightColor: right.color, BottomColor: bottom.color, LeftColor: left.color,
	}
}

func (c *Box) collapsedLeftBorder() *collapsedBorder {
	table := c.table()
	border := c.Style.Border()
	result := borderLeftCB(border, bCell)
	prev := table.cellLeft(c)
	if prev != nil {
		result = compareBorders(result, borderRightCB(prev.Style.Border(), bCell), false)
		if result.hidden() {
			return result
		}
	} else if c.col == 0 {
		result = compareBorders(result, borderLeftCB(c.parent.Style.Border(), bRow), false)
		if result.hidden() {
			return result
		}
		result = compareBorders(result, borderLeftCB(c.section().Style.Border(), bRowGroup), false)
		if result.hidden() {
			return result
		}
	}
	if ce := table.colElement(c.col); ce != nil {
		result = compareBorders(result, borderLeftCB(ce.style.Border(), bCol), false)
		if result.hidden() {
			return result
		}
	}
	if c.col > 0 {
		if ce := table.colElement(c.col - 1); ce != nil {
			result = compareBorders(result, borderRightCB(ce.style.Border(), bCol), false)
			if result.hidden() {
				return result
			}
		}
	}
	if c.col == 0 {
		result = compareBorders(result, borderLeftCB(table.Style.Border(), bTable), false)
		if result.hidden() {
			return result
		}
	}
	return result
}

func (c *Box) collapsedRightBorder() *collapsedBorder {
	table := c.table()
	inLast := false
	effCol := table.colToEffCol(c.col + c.Style.ColSpan() - 1)
	if effCol == table.numEffCols()-1 {
		inLast = true
	}
	result := borderRightCB(c.Style.Border(), bCell)
	if !inLast {
		next := table.cellRight(c)
		if next != nil {
			result = compareBorders(result, borderLeftCB(next.Style.Border(), bCell), false)
			if result.hidden() {
				return result
			}
		}
	} else {
		result = compareBorders(result, borderRightCB(c.parent.Style.Border(), bRow), false)
		if result.hidden() {
			return result
		}
		result = compareBorders(result, borderRightCB(c.section().Style.Border(), bRowGroup), false)
		if result.hidden() {
			return result
		}
	}
	if ce := table.colElement(c.col + c.Style.ColSpan() - 1); ce != nil {
		result = compareBorders(result, borderRightCB(ce.style.Border(), bCol), false)
		if result.hidden() {
			return result
		}
	}
	if !inLast {
		if ce := table.colElement(c.col + c.Style.ColSpan()); ce != nil {
			result = compareBorders(result, borderLeftCB(ce.style.Border(), bCol), false)
			if result.hidden() {
				return result
			}
		}
	} else {
		result = compareBorders(result, borderRightCB(table.Style.Border(), bTable), false)
		if result.hidden() {
			return result
		}
	}
	return result
}

func (c *Box) collapsedTopBorder() *collapsedBorder {
	table := c.table()
	result := borderTopCB(c.Style.Border(), bCell)
	prev := table.cellAbove(c)
	if prev != nil {
		result = compareBorders(result, borderBottomCB(prev.Style.Border(), bCell), false)
		if result.hidden() {
			return result
		}
	}
	result = compareBorders(result, borderTopCB(c.parent.Style.Border(), bRow), false)
	if result.hidden() {
		return result
	}
	if prev != nil {
		var prevRow *Box
		if prev.section() == c.section() {
			prevRow = c.parent.previousSibling()
		} else {
			prevRow = prev.section().lastRow()
		}
		if prevRow != nil {
			result = compareBorders(result, borderBottomCB(prevRow.Style.Border(), bRow), false)
			if result.hidden() {
				return result
			}
		}
	}
	curSection := c.section()
	if c.row == 0 {
		result = compareBorders(result, borderTopCB(curSection.Style.Border(), bRowGroup), false)
		if result.hidden() {
			return result
		}
		curSection = table.sectionAbove(curSection, false)
		if curSection != nil {
			result = compareBorders(result, borderBottomCB(curSection.Style.Border(), bRowGroup), false)
			if result.hidden() {
				return result
			}
		}
	}
	if curSection == nil {
		if ce := table.colElement(c.col); ce != nil {
			result = compareBorders(result, borderTopCB(ce.style.Border(), bCol), false)
			if result.hidden() {
				return result
			}
		}
		result = compareBorders(result, borderTopCB(table.Style.Border(), bTable), false)
		if result.hidden() {
			return result
		}
	}
	return result
}

func (c *Box) collapsedBottomBorder() *collapsedBorder {
	table := c.table()
	result := borderBottomCB(c.Style.Border(), bCell)
	next := table.cellBelow(c)
	if next != nil {
		result = compareBorders(result, borderTopCB(next.Style.Border(), bCell), false)
		if result.hidden() {
			return result
		}
	}
	result = compareBorders(result, borderBottomCB(c.parent.Style.Border(), bRow), false)
	if result.hidden() {
		return result
	}
	if next != nil {
		result = compareBorders(result, borderTopCB(next.parent.Style.Border(), bRow), false)
		if result.hidden() {
			return result
		}
	}
	curSection := c.section()
	if c.row+c.Style.RowSpan() >= curSection.numRows() {
		result = compareBorders(result, borderBottomCB(curSection.Style.Border(), bRowGroup), false)
		if result.hidden() {
			return result
		}
		curSection = table.sectionBelow(curSection, false)
		if curSection != nil {
			result = compareBorders(result, borderTopCB(curSection.Style.Border(), bRowGroup), false)
			if result.hidden() {
				return result
			}
		}
	}
	if curSection == nil {
		if ce := table.colElement(c.col); ce != nil {
			result = compareBorders(result, borderBottomCB(ce.style.Border(), bCol), false)
			if result.hidden() {
				return result
			}
		}
		result = compareBorders(result, borderBottomCB(table.Style.Border(), bTable), false)
		if result.hidden() {
			return result
		}
	}
	return result
}

func (c *Box) collapsedBorderBounds() Rect {
	b := c.collapsedPaintingBorder
	bounds := c.paintingBorderEdge()
	bounds.X -= int(b.Left) / 2
	bounds.Y -= int(b.Top) / 2
	bounds.W += int(b.Left)/2 + (int(b.Right)+1)/2
	bounds.H += int(b.Top)/2 + (int(b.Bottom)+1)/2
	return bounds
}

// ---- row -------------------------------------------------------------------------------------

func (r *Box) rowLayoutChildren(lc *LayoutContext, contentStart int) {
	r.ensureChildren(lc)
	section := r.section()
	if section.needCellWidthCalc {
		section.setCellWidths(lc)
		section.needCellWidthCalc = false
	}
	if r.childrenType != ContentEmpty {
		for _, cell := range r.children {
			r.layoutCell(lc, cell, 0)
		}
	}
}

func (r *Box) layoutCell(lc *LayoutContext, cell *Box, contentStart int) {
	cell.calcCanvasLocation()
	cell.layout(lc, contentStart)
}

func (r *Box) alignBaselineAlignedCells(lc *LayoutContext) {
	baselines := make([]int, len(r.children))
	lowest := noBaseline
	found := false
	for i, cell := range r.children {
		if cell.cellVerticalAlign() == "baseline" {
			bl := cell.calcBaseline(lc)
			baselines[i] = bl
			if bl > lowest {
				lowest = bl
			}
			found = true
		}
	}
	if found {
		for i, cell := range r.children {
			if cell.cellVerticalAlign() == "baseline" {
				deltaY := lowest - baselines[i]
				if deltaY != 0 {
					if lc.isPrint() && cell.isPageBreaksChange(lc, deltaY) {
						r.relayoutCell(lc, cell, deltaY)
					} else {
						cell.moveContent(lc, deltaY)
						cell.height += deltaY
					}
				}
			}
		}
		r.baseline = lowest - r.absY
		r.haveBaseline = true
	}
}

func (r *Box) relayoutCell(lc *LayoutContext, cell *Box, contentStart int) {
	width := cell.width()
	cell.reset(lc)
	cell.setLayoutWidth(lc, width)
	r.layoutCell(lc, cell, contentStart)
}

func (r *Box) calcMiddleBottomDeltaY(cell *Box, va string) int {
	var res int
	if cell.Style.RowSpan() == 1 {
		res = r.height - cell.childrenHeight
	} else {
		res = r.absY + r.height - (cell.absY + cell.childrenHeight)
	}
	if va == "middle" {
		return res / 2
	}
	return res
}

func (r *Box) alignMiddleAndBottomAlignedCells(lc *LayoutContext) bool {
	needRecalc := false
	cRow := r.index
	section := r.section()
	totalRows := section.numRows()
	if len(section.grid) > 0 && cRow < len(section.grid) {
		row := section.grid[cRow]
		for cCol := range row {
			cell := row[cCol]
			if cell == nil || cell == spanningCell {
				continue
			}
			if cRow < totalRows-1 && section.cellAt(cRow+1, cCol) == cell {
				continue
			}
			va := cell.cellVerticalAlign()
			if va == "middle" || va == "bottom" {
				deltaY := r.calcMiddleBottomDeltaY(cell, va)
				if deltaY > 0 {
					if lc.isPrint() && cell.isPageBreaksChange(lc, deltaY) {
						oldH := cell.height
						r.relayoutCell(lc, cell, deltaY)
						if oldH+deltaY != cell.height {
							needRecalc = true
						}
					} else {
						cell.moveContent(lc, deltaY)
						cell.height += deltaY
					}
				}
			}
		}
	}
	return needRecalc
}

func (r *Box) rowCalcLayoutHeight(lc *LayoutContext, border *BorderSet, margin *RectSet, padding *RectSet) {
	if r.heightOverride > 0 {
		r.height = r.heightOverride
	}
	r.alignBaselineAlignedCells(lc)
	r.calcRowHeight()
	if r.alignMiddleAndBottomAlignedCells(lc) {
		r.calcRowHeight()
	}
	if !r.haveBaseline {
		r.calcDefaultBaseline()
	}
	r.setCellHeights()
}

func (r *Box) isLastRow() bool {
	table := r.table()
	section := r.section()
	if table.sectionBelow(section, true) == nil {
		return section.children[len(section.children)-1] == r
	}
	return false
}

func (r *Box) calcRowHeight() {
	y1 := r.absY
	y2 := y1
	if r.height != 0 {
		y2 = y1 + r.height
	}
	if r.isLastRow() {
		bottom := r.table().calcFixedHeightRowBottom()
		if bottom > 0 && bottom > y2 {
			y2 = bottom
		}
	}
	cRow := r.index
	section := r.section()
	totalRows := section.numRows()
	if len(section.grid) > 0 && cRow < len(section.grid) {
		row := section.grid[cRow]
		for cCol := range row {
			cell := row[cCol]
			if cell == nil || cell == spanningCell {
				continue
			}
			if cRow < totalRows-1 && section.cellAt(cRow+1, cCol) == cell {
				continue
			}
			bottomEdge := cell.absY + cell.height
			if bottomEdge > y2 {
				y2 = bottomEdge
			}
		}
	}
	r.height = y2 - y1
}

func (r *Box) calcDefaultBaseline() {
	lowest := 0
	cRow := r.index
	section := r.section()
	totalRows := section.numRows()
	if len(section.grid) > 0 && cRow < len(section.grid) {
		row := section.grid[cRow]
		for cCol := range row {
			cell := row[cCol]
			if cell == nil || cell == spanningCell {
				continue
			}
			if cRow < totalRows-1 && section.cellAt(cRow+1, cCol) == cell {
				continue
			}
			ca := cell.contentAreaEdge(cell.absX, cell.absY)
			bottom := ca.Y + ca.H
			if bottom > lowest {
				lowest = bottom
			}
		}
	}
	if lowest > 0 {
		r.baseline = lowest - r.absY
	}
	r.haveBaseline = true
}

func (r *Box) setCellHeights() {
	cRow := r.index
	section := r.section()
	totalRows := section.numRows()
	if len(section.grid) > 0 && cRow < len(section.grid) {
		row := section.grid[cRow]
		for cCol := range row {
			cell := row[cCol]
			if cell == nil || cell == spanningCell {
				continue
			}
			if cRow < totalRows-1 && section.cellAt(cRow+1, cCol) == cell {
				continue
			}
			if cell.Style.RowSpan() == 1 {
				cell.height = r.height
			} else {
				cell.height = r.absY + r.height - cell.absY
			}
		}
	}
}

// ---- table layouts ----------------------------------------------------------------------------

type fixedTableLayout struct {
	table  *Box
	widths []*Length
}

func (f *fixedTableLayout) reset() { f.widths = nil }

func (f *fixedTableLayout) initWidths() {
	f.widths = make([]*Length, f.table.numEffCols())
	for i := range f.widths {
		f.widths[i] = NewLength()
	}
}

func (f *fixedTableLayout) calcWidthArray(lc *LayoutContext) int {
	f.initWidths()
	table := f.table
	cCol := 0
	nEffCols := table.numEffCols()
	usedWidth := 0
	for _, col := range table.styleColumns {
		span := col.style.ColSpan()
		w := col.style.AsLength("width")
		if w.IsVariable() && col.parent != nil {
			w = col.parent.style.AsLength("width")
		}
		var effWidth int64
		if w.IsFixed() && w.Value > 0 {
			effWidth = w.Value
			if effWidth > LenMaxWidth {
				effWidth = LenMaxWidth
			}
		}
		usedSpan := 0
		i := 0
		for usedSpan < span {
			if cCol+i >= nEffCols {
				table.appendColumn(span - usedSpan)
				nEffCols++
				f.widths = append(f.widths, NewLength())
			}
			eSpan := table.spanOfEffCol(cCol + i)
			if (w.IsFixed() || w.IsPercent()) && w.Value > 0 {
				f.widths[cCol+i] = &Length{Type: w.Type, Value: w.Value * int64(eSpan)}
				usedWidth += int(effWidth) * eSpan
			}
			usedSpan += eSpan
			i++
		}
		cCol += i
	}
	cCol = 0
	firstRow := table.firstRow()
	if firstRow != nil {
		for _, cell := range firstRow.children {
			w := cell.outerStyleWidth()
			span := cell.Style.ColSpan()
			var effWidth int64
			if w.IsFixed() && w.Value > 0 {
				effWidth = w.Value
			}
			usedSpan := 0
			i := 0
			for usedSpan < span {
				eSpan := table.spanOfEffCol(cCol + i)
				cw := f.widths[cCol+i]
				if cw.IsVariable() && !w.IsVariable() {
					f.widths[cCol+i] = &Length{Type: w.Type, Value: w.Value * int64(eSpan)}
					usedWidth += int(effWidth) * eSpan
				}
				usedSpan += eSpan
				i++
			}
			cCol += i
		}
	}
	return usedWidth
}

func (f *fixedTableLayout) calcMinMaxWidth(lc *LayoutContext) {
	t := f.table
	bs := t.marginsBordersPaddingAndSpacing(lc, true)
	t.calcDimensions(lc)
	t.dimensionsCalc = false
	mw := f.calcWidthArray(lc) + bs
	t.minWidth = mw
	if t.width() > mw {
		t.minWidth = t.width()
	}
	t.maxWidth = t.minWidth
	haveNonFixed := false
	for _, w := range f.widths {
		if !w.IsFixed() {
			haveNonFixed = true
			break
		}
	}
	if haveNonFixed {
		t.maxWidth = LenMaxWidth
	}
}

func (f *fixedTableLayout) layout(lc *LayoutContext) {
	t := f.table
	tableWidth := t.width() - t.marginsBordersPaddingAndSpacing(lc, false)
	available := tableWidth
	nEffCols := t.numEffCols()
	calcWidth := make([]int64, nEffCols)
	for i := range calcWidth {
		calcWidth[i] = -1
	}
	for i := 0; i < nEffCols; i++ {
		l := f.widths[i]
		if l.IsFixed() {
			calcWidth[i] = l.Value
			available -= int(l.Value)
		}
	}
	if available > 0 {
		totalPercent := 0
		for i := 0; i < nEffCols; i++ {
			if f.widths[i].IsPercent() {
				totalPercent += int(f.widths[i].Value)
			}
		}
		base := tableWidth * totalPercent / 100
		if base > available {
			base = available
		}
		for i := 0; available > 0 && i < nEffCols; i++ {
			l := f.widths[i]
			if l.IsPercent() {
				w := int64(base) * l.Value / int64(totalPercent)
				available -= int(w)
				calcWidth[i] = w
			}
		}
	}
	if available > 0 {
		totalVariable := 0
		for i := 0; i < nEffCols; i++ {
			if f.widths[i].IsVariable() {
				totalVariable++
			}
		}
		for i := 0; available > 0 && i < nEffCols; i++ {
			if f.widths[i].IsVariable() {
				w := available / totalVariable
				available -= w
				calcWidth[i] = int64(w)
				totalVariable--
			}
		}
	}
	for i := 0; i < nEffCols; i++ {
		if calcWidth[i] < 0 {
			calcWidth[i] = 0
		}
	}
	if available > 0 {
		total := nEffCols
		i := nEffCols
		for i--; i >= 0; i-- {
			w := available / total
			available -= w
			total--
			calcWidth[i] += int64(w)
		}
	}
	pos := 0
	hspacing := t.Style.BorderHSpacing()
	columnPos := make([]int, nEffCols+1)
	for i := 0; i < nEffCols; i++ {
		columnPos[i] = pos
		pos += int(calcWidth[i]) + hspacing
	}
	columnPos[len(columnPos)-1] = pos
	t.columnPos = columnPos
}

// autoTableLayout ----------------------------------------------------------------------------------

type atlLayout struct {
	width, effWidth          *Length
	minWidth, maxWidth       int64
	effMinWidth, effMaxWidth int64
	calcWidth                int64
}

func newATLLayout() *atlLayout {
	return &atlLayout{width: NewLength(), effWidth: NewLength(), minWidth: 1, maxWidth: 1}
}

type autoTableLayout struct {
	table        *Box
	layoutStruct []*atlLayout
	spanCells    []*Box
}

type marginTableLayout struct{ autoTableLayout }

func (a *autoTableLayout) reset() {
	a.layoutStruct = nil
	a.spanCells = nil
}

func (a *autoTableLayout) minColWidth() int64   { return 1 }
func (m *marginTableLayout) minColWidth() int64 { return 0 }

func (a *autoTableLayout) isMargin() bool { return false }

func (a *autoTableLayout) fullRecalc(lc *LayoutContext, minCol int64) {
	table := a.table
	a.layoutStruct = make([]*atlLayout, table.numEffCols())
	for i := range a.layoutStruct {
		a.layoutStruct[i] = newATLLayout()
		a.layoutStruct[i].minWidth = minCol
		a.layoutStruct[i].maxWidth = minCol
	}
	a.spanCells = nil
	nEffCols := table.numEffCols()
	cCol := 0
	for _, col := range table.styleColumns {
		span := col.style.ColSpan()
		w := col.style.AsLength("width")
		if w.IsVariable() && col.parent != nil {
			w = col.parent.style.AsLength("width")
		}
		if (w.IsFixed() && w.Value == 0) || (w.IsPercent() && w.Value == 0) {
			w = NewLength()
		}
		cEffCol := table.colToEffCol(cCol)
		if !w.IsVariable() && span == 1 && cEffCol < nEffCols {
			if table.spanOfEffCol(cEffCol) == 1 {
				a.layoutStruct[cEffCol].width = w
				if w.IsFixed() && a.layoutStruct[cEffCol].maxWidth < w.Value {
					a.layoutStruct[cEffCol].maxWidth = w.Value
				}
			}
		}
		cCol += span
	}
	for i := 0; i < nEffCols; i++ {
		a.recalcColumn(lc, i, minCol)
	}
}

func (a *autoTableLayout) recalcColumn(lc *LayoutContext, effCol int, minCol int64) {
	l := a.layoutStruct[effCol]
	for _, section := range a.table.children {
		numRows := section.numRows()
		for i := 0; i < numRows; i++ {
			cell := section.cellAt(i, effCol)
			if cell == spanningCell || cell == nil {
				continue
			}
			if cell.Style.ColSpan() == 1 {
				if minCol > l.minWidth {
					l.minWidth = minCol
				}
				if minCol > l.maxWidth {
					l.maxWidth = minCol
				}
				cell.calcMinMaxWidth(lc)
				if int64(cell.minWidth) > l.minWidth {
					l.minWidth = int64(cell.minWidth)
				}
				if int64(cell.maxWidth) > l.maxWidth {
					l.maxWidth = int64(cell.maxWidth)
				}
				w := cell.outerStyleOrColWidth()
				if w.Value < 0 {
					w.Value = 0
				}
				if w.Value > LenMaxWidth {
					w.Value = LenMaxWidth
				}
				switch w.Type {
				case LenFixed:
					if w.Value > 0 && !l.width.IsPercent() {
						if l.width.IsFixed() {
							if w.Value > l.width.Value {
								l.width.Value = w.Value
							}
						} else {
							l.width = w
						}
						if w.Value > l.maxWidth {
							l.maxWidth = w.Value
						}
					}
				case LenPercent:
					if w.Value > 0 && (!l.width.IsPercent() || w.Value > l.width.Value) {
						l.width = w
					}
				}
			} else {
				if effCol == 0 || section.cellAt(i, effCol-1) != cell {
					if minCol > l.minWidth {
						l.minWidth = minCol
					}
					if minCol > l.maxWidth {
						l.maxWidth = minCol
					}
					a.spanCells = append(a.spanCells, cell)
				}
			}
		}
	}
	if l.minWidth > l.maxWidth {
		l.maxWidth = l.minWidth
	}
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func (a *autoTableLayout) calcEffectiveWidth(lc *LayoutContext) int64 {
	var tMaxWidth int64
	ls := a.layoutStruct
	nEffCols := len(ls)
	hspacing := a.table.Style.BorderHSpacing()
	for i := 0; i < nEffCols; i++ {
		ls[i].effWidth = ls[i].width
		ls[i].effMinWidth = ls[i].minWidth
		ls[i].effMaxWidth = ls[i].maxWidth
	}
	sort.SliceStable(a.spanCells, func(i, j int) bool {
		return a.spanCells[i].Style.ColSpan() < a.spanCells[j].Style.ColSpan()
	})
	for _, cell := range a.spanCells {
		cell.calcMinMaxWidth(lc)
		span := cell.Style.ColSpan()
		w := cell.outerStyleOrColWidth()
		if w.Value == 0 {
			w = NewLength()
		}
		col := a.table.colToEffCol(cell.col)
		lastCol := col
		cMinWidth := int64(cell.minWidth + hspacing)
		cMaxWidth := int64(cell.maxWidth + hspacing)
		var totalPercent int64
		var minWidth, maxWidth int64
		allColsArePercent := true
		allColsAreFixed := true
		haveVariable := false
		var fixedWidth int64
		for lastCol < nEffCols && span > 0 {
			switch ls[lastCol].width.Type {
			case LenPercent:
				totalPercent += ls[lastCol].width.Value
				allColsAreFixed = false
			case LenFixed:
				if ls[lastCol].width.Value > 0 {
					fixedWidth += ls[lastCol].width.Value
					allColsArePercent = false
				} else {
					haveVariable = true
					if !ls[lastCol].effWidth.IsPercent() {
						ls[lastCol].effWidth = NewLength()
						allColsArePercent = false
					} else {
						totalPercent += ls[lastCol].effWidth.Value
					}
					allColsAreFixed = false
				}
			default:
				haveVariable = true
				if !ls[lastCol].effWidth.IsPercent() {
					ls[lastCol].effWidth = NewLength()
					allColsArePercent = false
				} else {
					totalPercent += ls[lastCol].effWidth.Value
				}
				allColsAreFixed = false
			}
			span -= a.table.spanOfEffCol(lastCol)
			minWidth += ls[lastCol].effMinWidth
			maxWidth += ls[lastCol].effMaxWidth
			lastCol++
			cMinWidth -= int64(hspacing)
			cMaxWidth -= int64(hspacing)
		}
		if w.IsPercent() {
			if totalPercent > w.Value || allColsArePercent {
				w = NewLength()
			} else {
				spanMax := max64(maxWidth, cMaxWidth)
				tMaxWidth = max64(tMaxWidth, spanMax*100/w.Value)
				percentMissing := w.Value - totalPercent
				var totalWidth int64
				for pos := col; pos < lastCol; pos++ {
					if !ls[pos].width.IsPercent() {
						totalWidth += ls[pos].effMaxWidth
					}
				}
				for pos := col; pos < lastCol && totalWidth > 0; pos++ {
					if !ls[pos].width.IsPercent() {
						percent := percentMissing * ls[pos].effMaxWidth / totalWidth
						totalWidth -= ls[pos].effMaxWidth
						percentMissing -= percent
						if percent > 0 {
							ls[pos].effWidth = &Length{Type: LenPercent, Value: percent}
						} else {
							ls[pos].effWidth = NewLength()
						}
					}
				}
			}
		}
		if cMinWidth > minWidth {
			if allColsAreFixed {
				for pos := col; fixedWidth > 0 && pos < lastCol; pos++ {
					cWidth := max64(ls[pos].effMinWidth, cMinWidth*ls[pos].width.Value/fixedWidth)
					fixedWidth -= ls[pos].width.Value
					cMinWidth -= cWidth
					ls[pos].effMinWidth = cWidth
				}
			} else if allColsArePercent {
				maxw := maxWidth
				minw := minWidth
				cminw := cMinWidth
				for pos := col; maxw > 0 && pos < lastCol; pos++ {
					if ls[pos].effWidth.IsPercent() && ls[pos].effWidth.Value > 0 && fixedWidth <= cMinWidth {
						cWidth := ls[pos].effMinWidth
						cWidth = max64(cWidth, cminw*ls[pos].effWidth.Value/totalPercent)
						cWidth = min64(ls[pos].effMinWidth+(cMinWidth-minw), cWidth)
						maxw -= ls[pos].effMaxWidth
						minw -= ls[pos].effMinWidth
						cMinWidth -= cWidth
						ls[pos].effMinWidth = cWidth
					}
				}
			} else {
				maxw := maxWidth
				minw := minWidth
				for pos := col; maxw > 0 && pos < lastCol; pos++ {
					if ls[pos].width.IsFixed() && haveVariable && fixedWidth <= cMinWidth {
						cWidth := max64(ls[pos].effMinWidth, ls[pos].width.Value)
						fixedWidth -= ls[pos].width.Value
						minw -= ls[pos].effMinWidth
						maxw -= ls[pos].effMaxWidth
						cMinWidth -= cWidth
						ls[pos].effMinWidth = cWidth
					}
				}
				for pos := col; maxw > 0 && pos < lastCol && minw < cMinWidth; pos++ {
					if !(ls[pos].width.IsFixed() && haveVariable && fixedWidth <= cMinWidth) {
						cWidth := max64(ls[pos].effMinWidth, cMinWidth*ls[pos].effMaxWidth/maxw)
						cWidth = min64(ls[pos].effMinWidth+(cMinWidth-minw), cWidth)
						maxw -= ls[pos].effMaxWidth
						minw -= ls[pos].effMinWidth
						cMinWidth -= cWidth
						ls[pos].effMinWidth = cWidth
					}
				}
			}
		}
		if !w.IsPercent() {
			if cMaxWidth > maxWidth {
				for pos := col; maxWidth > 0 && pos < lastCol; pos++ {
					cWidth := max64(ls[pos].effMaxWidth, cMaxWidth*ls[pos].effMaxWidth/maxWidth)
					maxWidth -= ls[pos].effMaxWidth
					cMaxWidth -= cWidth
					ls[pos].effMaxWidth = cWidth
				}
			}
		} else {
			for pos := col; pos < lastCol; pos++ {
				ls[pos].maxWidth = max64(ls[pos].maxWidth, ls[pos].minWidth)
			}
		}
	}
	return tMaxWidth
}

func (a *autoTableLayout) calcMinMaxWidth(lc *LayoutContext) { a.calcMinMaxWidthImpl(lc, 1, false) }

func (m *marginTableLayout) calcMinMaxWidth(lc *LayoutContext) {
	m.calcMinMaxWidthImpl(lc, 0, true)
}

func (a *autoTableLayout) calcMinMaxWidthImpl(lc *LayoutContext, minCol int64, margin bool) {
	table := a.table
	a.fullRecalc(lc, minCol)
	ls := a.layoutStruct
	spanMaxWidth := a.calcEffectiveWidth(lc)
	var minWidth, maxWidth, maxPercent, maxNonPercent int64
	remainingPercent := int64(100)
	for i := range ls {
		minWidth += ls[i].effMinWidth
		maxWidth += ls[i].effMaxWidth
		if ls[i].effWidth.IsPercent() {
			percent := min64(ls[i].effWidth.Value, remainingPercent)
			pw := (ls[i].effMaxWidth * 100) / max64(percent, 1)
			remainingPercent -= percent
			maxPercent = max64(pw, maxPercent)
		} else {
			maxNonPercent += ls[i].effMaxWidth
		}
	}
	// shouldScaleColumns is always true
	maxNonPercent = (maxNonPercent*100 + 50) / max64(remainingPercent, 1)
	maxWidth = max64(maxNonPercent, maxWidth)
	maxWidth = max64(maxWidth, maxPercent)
	maxWidth = max64(maxWidth, spanMaxWidth)
	bs := int64(table.marginsBordersPaddingAndSpacing(lc, true))
	minWidth += bs
	maxWidth += bs
	tw := table.Style.AsLength("width")
	if tw.IsFixed() && tw.Value > 0 {
		table.calcDimensions(lc)
		width := int64(table.contentWidth + table.marginsBordersPaddingAndSpacing(lc, true))
		minWidth = max64(minWidth, width)
		maxWidth = minWidth
	}
	table.maxWidth = int(min64(maxWidth, LenMaxWidth))
	table.minWidth = int(min64(minWidth, LenMaxWidth))
	if margin && len(ls) == 3 {
		center := ls[1]
		if !(center.width.IsVariable() && center.maxWidth == 0) {
			switch {
			case ls[0].minWidth > ls[2].minWidth:
				ls[2] = ls[0]
			case ls[2].minWidth > ls[0].minWidth:
				ls[0] = ls[2]
			default:
				l := newATLLayout()
				l.minWidth = max64(ls[0].minWidth, ls[2].minWidth)
				l.effMinWidth = l.minWidth
				l.maxWidth = max64(ls[0].maxWidth, ls[2].maxWidth)
				l.effMaxWidth = l.maxWidth
				ls[0], ls[2] = l, l
			}
		}
	}
}

func (a *autoTableLayout) layout(lc *LayoutContext) {
	table := a.table
	tableWidth := table.width() - table.marginsBordersPaddingAndSpacing(lc, false)
	available := int64(tableWidth)
	nEffCols := table.numEffCols()
	havePercent := false
	numVariable, numFixed := 0, 0
	var totalVariable, totalFixed, totalPercent, allocVariable int64
	ls := a.layoutStruct
	for i := 0; i < nEffCols; i++ {
		w := ls[i].effMinWidth
		ls[i].calcWidth = w
		available -= w
		switch ls[i].effWidth.Type {
		case LenPercent:
			havePercent = true
			totalPercent += ls[i].effWidth.Value
		case LenFixed:
			numFixed++
			totalFixed += ls[i].effMaxWidth
		case LenVariable:
			numVariable++
			totalVariable += ls[i].effMaxWidth
			allocVariable += w
		}
	}
	if available > 0 && havePercent {
		for i := 0; i < nEffCols; i++ {
			width := ls[i].effWidth
			if width.IsPercent() {
				w := max64(ls[i].effMinWidth, width.MinWidth(tableWidth))
				available += ls[i].calcWidth - w
				ls[i].calcWidth = w
			}
		}
		if totalPercent > 100 {
			excess := int64(tableWidth) * (totalPercent - 100) / 100
			for i := nEffCols - 1; i >= 0; i-- {
				if ls[i].effWidth.IsPercent() {
					w := ls[i].calcWidth
					reduction := min64(w, excess)
					excess -= reduction
					newWidth := max64(ls[i].effMinWidth, w-reduction)
					available += w - newWidth
					ls[i].calcWidth = newWidth
				}
			}
		}
	}
	if available > 0 {
		for i := 0; i < nEffCols; i++ {
			width := ls[i].effWidth
			if width.IsFixed() && width.Value > ls[i].calcWidth {
				available += ls[i].calcWidth - width.Value
				ls[i].calcWidth = width.Value
			}
		}
	}
	if available > 0 && numVariable > 0 {
		available += allocVariable
		for i := 0; i < nEffCols; i++ {
			width := ls[i].effWidth
			if width.IsVariable() && totalVariable != 0 {
				w := max64(ls[i].calcWidth, available*ls[i].effMaxWidth/totalVariable)
				available -= w
				totalVariable -= ls[i].effMaxWidth
				ls[i].calcWidth = w
			}
		}
	}
	if available > 0 && numFixed > 0 {
		for i := 0; i < nEffCols; i++ {
			width := ls[i].effWidth
			if width.IsFixed() {
				w := available * ls[i].effMaxWidth / totalFixed
				available -= w
				totalFixed -= ls[i].effMaxWidth
				ls[i].calcWidth += w
			}
		}
	}
	if available > 0 && havePercent && totalPercent < 100 {
		for i := 0; i < nEffCols; i++ {
			width := ls[i].effWidth
			if width.IsPercent() {
				w := available * width.Value / totalPercent
				available -= w
				totalPercent -= width.Value
				ls[i].calcWidth += w
				if available == 0 || totalPercent == 0 {
					break
				}
			}
		}
	}
	if available > 0 {
		total := int64(nEffCols)
		i := nEffCols
		for i--; i >= 0; i-- {
			w := available / total
			available -= w
			total--
			ls[i].calcWidth += w
		}
	}
	if available < 0 {
		shrink := func(typ int) {
			if available >= 0 {
				return
			}
			var mw int64
			for i := nEffCols - 1; i >= 0; i-- {
				if ls[i].effWidth.Type == typ {
					mw += ls[i].calcWidth - ls[i].effMinWidth
				}
			}
			for i := nEffCols - 1; i >= 0 && mw > 0; i-- {
				if ls[i].effWidth.Type == typ {
					minMaxDiff := ls[i].calcWidth - ls[i].effMinWidth
					reduce := available * minMaxDiff / mw
					ls[i].calcWidth += reduce
					available -= reduce
					mw -= minMaxDiff
					if available >= 0 {
						break
					}
				}
			}
		}
		shrink(LenVariable)
		shrink(LenFixed)
		shrink(LenPercent)
	}
	pos := 0
	hspacing := table.Style.BorderHSpacing()
	columnPos := make([]int, nEffCols+1)
	for i := 0; i < nEffCols; i++ {
		columnPos[i] = pos
		pos += int(ls[i].calcWidth) + hspacing
	}
	columnPos[len(columnPos)-1] = pos
	table.columnPos = columnPos
}
