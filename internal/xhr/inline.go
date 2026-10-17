package xhr

import (
	"math"
	"strings"
	"unicode"
)

// ---- LineBreakContext ----------------------------------------------------------------

type lbContext struct {
	master       []rune
	start, end   int
	savedEnd     int
	unbreakable  bool
	needsNewLine bool
	width        int
	endsOnNL     bool
	textNode     interface{}
}

func (c *lbContext) last() int { return len(c.master) }
func (c *lbContext) reset() {
	c.width = 0
	c.unbreakable = false
	c.needsNewLine = false
}
func (c *lbContext) startSubstring() []rune { return c.master[c.start:] }
func (c *lbContext) calcSubstring() []rune {
	if c.end > 0 && c.master[c.end-1] == '\n' {
		return c.master[c.start : c.end-1]
	}
	return c.master[c.start:c.end]
}
func (c *lbContext) isFinished() bool { return c.end == len(c.master) }
func (c *lbContext) resetEnd()        { c.end = c.savedEnd }
func (c *lbContext) saveEnd()         { c.savedEnd = c.end }

func (f *FSFont) widthRunes(r []rune) int { return f.Width(string(r)) }

// breakText mirrors Breaker.breakText.
func breakText(ctx *lbContext, avail int, style *Style) {
	font := style.FSFont()
	ws := style.Whitespace()
	if ws == "nowrap" {
		ctx.end = ctx.last()
		ctx.width = font.widthRunes(ctx.calcSubstring())
		return
	}
	if ws == "pre" || ws == "pre-wrap" || ws == "pre-line" {
		n := -1
		for i, r := range ctx.startSubstring() {
			if r == '\n' {
				n = i
				break
			}
		}
		if n > -1 {
			ctx.end = ctx.start + n + 1
			ctx.width = font.widthRunes(ctx.calcSubstring())
			ctx.needsNewLine = true
			ctx.endsOnNL = true
		} else if ws == "pre" {
			ctx.end = ctx.last()
			ctx.width = font.widthRunes(ctx.calcSubstring())
		}
	}
	if ws == "pre" || (ctx.needsNewLine && ctx.width <= avail) {
		return
	}
	ctx.endsOnNL = false
	doBreakText(ctx, avail, style, false)
}

func isFirstLetterSeparatorChar(r rune) bool {
	return unicode.In(r, unicode.Ps, unicode.Pe, unicode.Pi, unicode.Pf, unicode.Po, unicode.Zs)
}

func breakFirstLetter(ctx *lbContext, avail int, style *Style) {
	font := style.FSFont()
	end := len(ctx.master)
	letterFound := false
	e := end
	for i := ctx.start; i < end; i++ {
		if !isFirstLetterSeparatorChar(ctx.master[i]) {
			if letterFound {
				e = i
				break
			}
			letterFound = true
		}
	}
	ctx.end = e
	ctx.width = font.widthRunes(ctx.calcSubstring())
	if ctx.width > avail {
		ctx.needsNewLine = true
		ctx.unbreakable = true
	}
}

func doBreakText(ctx *lbContext, avail int, style *Style, anywhere bool) {
	f := style.FSFont()
	current := ctx.startSubstring()
	type breaker interface{ next() int }
	var it breaker
	if anywhere {
		it = &anywhereBreaker{n: len(current)}
	} else {
		it = newURLAwareBreaker(string(current))
	}
	bp := it.next()
	right := -1
	previousWidth := 0
	previousPosition := 0
	for bp != breakDone {
		curW := f.widthRunes(current[previousPosition:bp])
		wh := previousWidth + curW
		previousWidth = wh
		previousPosition = bp
		if wh > avail {
			break
		}
		right = previousPosition
		bp = it.next()
	}
	if bp == breakDone {
		ctx.width = f.widthRunes(current)
		ctx.end = len(ctx.master)
		return
	}
	ctx.needsNewLine = true
	if right <= 0 && style.Ident("word-wrap") == "break-word" {
		if !anywhere {
			doBreakText(ctx, avail, style, true)
			return
		}
	}
	if right > 0 {
		ctx.end = ctx.start + right
		ctx.width = f.widthRunes(ctx.master[ctx.start : ctx.start+right])
		return
	}
	ctx.end = ctx.start + len(current)
	ctx.unbreakable = true
	ctx.width = f.widthRunes(ctx.calcSubstring())
}

type anywhereBreaker struct{ n, pos int }

// next mirrors BreakAnywhereLineBreakStrategy: positions 0..n-1, so the last
// character is never tested against the available width.
func (a *anywhereBreaker) next() int {
	if a.pos+1 > a.n {
		return breakDone
	}
	p := a.pos
	a.pos++
	return p
}

// ---- InlineSrc min/max ----------------------------------------------------------------

func (s *InlineSrc) textWidth(str string) int { return s.style.FSFont().Width(str) }

func (s *InlineSrc) spaceWidth() int { return s.style.FSFont().Width(" ") }

func (s *InlineSrc) trailingSpaceWidth() int {
	if len(s.text) > 0 && s.text[len(s.text)-1] == ' ' {
		return s.spaceWidth()
	}
	return 0
}

func (s *InlineSrc) maxCharWidth(str string) int {
	r := 0
	for _, c := range str {
		if w := s.textWidth(string(c)); w > r {
			r = w
		}
	}
	return r
}

func (s *InlineSrc) getText(trimLeading bool) string {
	if !trimLeading {
		return s.text
	}
	if len(s.text) > 0 && s.text[0] == ' ' {
		return s.text[1:]
	}
	return s.text
}

func (s *InlineSrc) inlineMBP(cbWidth int) int {
	return s.style.MarginBorderPadding(cbWidth, Left) + s.style.MarginBorderPadding(cbWidth, Right)
}

func (s *InlineSrc) calcMaxWidthFromLineLength(cbWidth int, trim bool) {
	last := 0
	runes := []rune(s.text)
	for {
		cur := -1
		for i := last; i < len(runes); i++ {
			if runes[i] == '\n' {
				cur = i
				break
			}
		}
		if cur == -1 {
			break
		}
		target := string(runes[last:cur])
		if trim {
			target = javaTrim(target)
		}
		length := s.textWidth(target)
		if last == 0 {
			length += s.style.MarginBorderPadding(cbWidth, Left)
		}
		if length > s.maxWidth {
			s.maxWidth = length
		}
		if last == 0 {
			s.firstLineWidth = length
		}
		last = cur + 1
	}
	target := string(runes[last:])
	if trim {
		target = javaTrim(target)
	}
	length := s.textWidth(target)
	length += s.style.MarginBorderPadding(cbWidth, Right)
	if length > s.maxWidth {
		s.maxWidth = length
	}
	if last == 0 {
		s.firstLineWidth = length
	}
}

func (s *InlineSrc) calcMinWidthFromWordLength(cbWidth int, trimLeading, includeWS bool) int {
	spaceWidth := s.spaceWidth()
	last, cur := 0, 0
	maxWidth := 0
	spaceCount := 0
	haveFirst := false
	firstWord, lastWord := 0, 0
	text := []rune(s.getText(trimLeading))
	it := newURLAwareBreaker(string(text))
	for {
		cur = it.next()
		if cur == breakDone {
			break
		}
		word := string(text[last:cur])
		wordWidth := s.textWidth(word)
		minWordWidth := wordWidth
		if s.style.Ident("word-wrap") == "break-word" {
			minWordWidth = s.maxCharWidth(word)
		}
		if spaceCount > 0 {
			if includeWS {
				for i := 0; i < spaceCount; i++ {
					wordWidth += spaceWidth
					minWordWidth += spaceWidth
				}
			} else {
				maxWidth += spaceWidth
			}
			spaceCount = 0
		}
		if minWordWidth > 0 {
			if !haveFirst {
				firstWord = minWordWidth
			}
			lastWord = minWordWidth
		}
		if minWordWidth > s.minWidth {
			s.minWidth = minWordWidth
		}
		maxWidth += wordWidth
		last = cur
		for i := cur; i < len(text); i++ {
			if text[i] == ' ' {
				spaceCount++
				last++
			} else {
				break
			}
		}
	}
	word := string(text[last:])
	wordWidth := s.textWidth(word)
	minWordWidth := wordWidth
	if s.style.Ident("word-wrap") == "break-word" {
		minWordWidth = s.maxCharWidth(word)
	}
	if spaceCount > 0 {
		if includeWS {
			for i := 0; i < spaceCount; i++ {
				wordWidth += spaceWidth
				minWordWidth += spaceWidth
			}
		} else {
			maxWidth += spaceWidth
		}
	}
	if minWordWidth > 0 {
		if !haveFirst {
			firstWord = minWordWidth
		}
		lastWord = minWordWidth
	}
	if minWordWidth > s.minWidth {
		s.minWidth = minWordWidth
	}
	maxWidth += wordWidth
	if s.startsHere {
		left := s.style.MarginBorderPadding(cbWidth, Left)
		if firstWord+left > s.minWidth {
			s.minWidth = firstWord + left
		}
		maxWidth += left
	}
	if s.endsHere {
		right := s.style.MarginBorderPadding(cbWidth, Right)
		if lastWord+right > s.minWidth {
			s.minWidth = lastWord + right
		}
		maxWidth += right
	}
	return maxWidth
}

func (s *InlineSrc) calcMinMaxWidth(cbWidth int, trimLeading bool) {
	if s.minMaxCalculated {
		return
	}
	switch s.style.Whitespace() {
	case "nowrap":
		s.minWidth = s.inlineMBP(cbWidth) + s.textWidth(s.getText(trimLeading))
		s.maxWidth = s.minWidth
	case "pre":
		s.calcMaxWidthFromLineLength(cbWidth, false)
		s.minWidth = s.maxWidth
	case "pre-wrap":
		s.calcMinWidthFromWordLength(cbWidth, false, true)
		s.calcMaxWidthFromLineLength(cbWidth, false)
	case "pre-line":
		s.calcMinWidthFromWordLength(cbWidth, trimLeading, false)
		s.calcMaxWidthFromLineLength(cbWidth, true)
	default:
		s.maxWidth = s.calcMinWidthFromWordLength(cbWidth, trimLeading, false)
	}
	if s.maxWidth < s.minWidth {
		s.minWidth = s.maxWidth
	}
	s.minMaxCalculated = true
}

// ---- InlineLayoutBox ---------------------------------------------------------------------

func newInlineLayoutBox(lc *LayoutContext, elem interface{}, style *Style, cbWidth int, src *InlineSrc) *Box {
	b := &Box{Kind: KInline}
	b.Style = style
	if src != nil {
		b.Elem = src.elem
	}
	b.cbWidth = cbWidth
	b.setMarginTop(0)
	b.setMarginBottom(0)
	b.pending = true
	b.calculateHeight()
	return b
}

func (b *Box) calculateHeight() {
	border := b.getBorder()
	padding := b.getPadding()
	m := b.Style.FontMetrics()
	b.height = int(math.Ceil(float64(border.Top + padding.Top + m.Ascent + m.Descent + padding.Bottom + border.Bottom)))
}

func (b *Box) addInlineChild(child interface{}, callUnmarkPending bool) {
	b.inlineChildren = append(b.inlineChildren, child)
	if callUnmarkPending && b.pending {
		b.unmarkPending()
	}
	switch c := child.(type) {
	case *Box:
		c.parent = b
	case *InlineText:
		c.parent = b
	}
}

func (b *Box) unmarkPending() {
	b.pending = false
	if b.parent != nil && b.parent.Kind == KInline && b.parent.pending {
		b.parent.unmarkPending()
	}
	b.startsHere = true
}

func (b *Box) prunePending() {
	for i := len(b.inlineChildren) - 1; i >= 0; i-- {
		c, ok := b.inlineChildren[i].(*Box)
		if !ok || c.Kind != KInline {
			break
		}
		c.prunePending()
		if c.pending {
			b.inlineChildren = append(b.inlineChildren[:i], b.inlineChildren[i+1:]...)
		} else {
			break
		}
	}
}

func (b *Box) leftMarginBorderPadding() int {
	if b.startsHere {
		return b.getMarginBorderPadding(Left)
	}
	return 0
}

func (b *Box) rightMarginBorderPadding() int {
	if b.endsHere {
		return b.getMarginBorderPadding(Right)
	}
	return 0
}

func (b *Box) isContainsVisibleContentInline() bool {
	for _, o := range b.inlineChildren {
		switch c := o.(type) {
		case *InlineText:
			if !c.isEmpty() {
				return true
			}
		case *Box:
			if c.Kind == KInline {
				if c.isContainsVisibleContentInline() {
					return true
				}
			} else if c.width() > 0 || c.height > 0 {
				return true
			}
		}
	}
	return false
}

func (b *Box) findTrailingTextInline() *InlineText {
	if len(b.inlineChildren) == 0 {
		return nil
	}
	var result *InlineText
	for off := len(b.inlineChildren) - 1; off >= 0; off-- {
		switch c := b.inlineChildren[off].(type) {
		case *InlineText:
			result = c
			if result.isEmpty() {
				continue
			}
			return result
		case *Box:
			if c.Kind == KInline {
				result = c.findTrailingTextInline()
				if result != nil && result.isEmpty() {
					continue
				}
				return result
			}
			return nil
		}
	}
	return result
}

func (t *InlineText) trimTrailingSpace() {
	if !t.isEmpty() && t.masterText[t.end-1] == ' ' {
		t.end--
		t.width = t.parent.Style.FSFont().widthRunes(t.masterText[t.start:t.end])
		t.trimmedTrailing = true
	}
}

func (b *Box) countJustifiableChars(counts *charCounts) {
	justifyThis := b.Style.IsTextJustify()
	for _, o := range b.inlineChildren {
		switch c := o.(type) {
		case *Box:
			if c.Kind == KInline {
				c.countJustifiableChars(counts)
			}
		case *InlineText:
			if justifyThis {
				for _, r := range c.masterText[c.start:c.end] {
					if r == ' ' || r == ' ' || r == '　' {
						counts.spaces++
					} else {
						counts.other++
					}
				}
			}
		}
	}
}

type charCounts struct{ spaces, other int }

func (t *InlineText) calcTotalAdjustment(info *JustificationInfo) float32 {
	var res float32
	for _, r := range t.masterText[t.start:t.end] {
		if r == ' ' || r == ' ' || r == '　' {
			res += info.SpaceAdjust
		} else {
			res += info.NonSpaceAdjust
		}
	}
	return res
}

func (b *Box) adjustHorizontalPosition(info *JustificationInfo, adjust float32) float32 {
	runningTotal := adjust
	var result float32
	for _, o := range b.inlineChildren {
		switch c := o.(type) {
		case *InlineText:
			c.x += int(javaRound(result))
			adj := c.calcTotalAdjustment(info)
			result += adj
			runningTotal += adj
		case *Box:
			c.x += int(javaRound(runningTotal))
			if c.Kind == KInline {
				adj := c.adjustHorizontalPosition(info, runningTotal)
				result += adj
				runningTotal += adj
			}
		}
	}
	return result
}

func javaRound(f float32) float32 { return float32(math.Floor(float64(f) + 0.5)) }

// ---- LineBox ----------------------------------------------------------------------------------

func newLine(lc *LayoutContext, y int, box *Box) *Box {
	l := &Box{Kind: KLine}
	l.Style = box.Style.CreateAnonymousStyle("block")
	l.parent = box
	l.y = y
	l.calcCanvasLocation()
	return l
}

func (l *Box) isFirstLine() bool {
	p := l.parent
	return p != nil && len(p.children) > 0 && p.children[0] == l
}

func (l *Box) prunePendingInlineBoxes() {
	for i := len(l.children) - 1; i >= 0; i-- {
		c := l.children[i]
		if c.Kind != KInline {
			break
		}
		c.prunePending()
		if c.pending {
			l.children = append(l.children[:i], l.children[i+1:]...)
		}
	}
}

func (l *Box) isContainsVisibleContent() bool {
	for _, c := range l.children {
		if c.Kind != KInline {
			if c.width() > 0 || c.height > 0 {
				return true
			}
		} else if c.isContainsVisibleContentInline() {
			return true
		}
	}
	return false
}

func (l *Box) isContainsOnlyBlockLevelContent() bool {
	if !l.containsBlockLevel {
		return false
	}
	for _, c := range l.children {
		if c.Kind == KInline {
			return false
		}
	}
	return true
}

func (l *Box) findTrailingText() *InlineText {
	if len(l.children) == 0 {
		return nil
	}
	for off := len(l.children) - 1; off >= 0; off-- {
		c := l.children[off]
		if c.Kind == KInline {
			r := c.findTrailingTextInline()
			if r != nil && r.isEmpty() {
				continue
			}
			return r
		}
		return nil
	}
	return nil
}

func (l *Box) trimTrailingSpace() {
	t := l.findTrailingText()
	if t != nil {
		ws := t.parent.Style.Whitespace()
		if ws == "normal" || ws == "nowrap" {
			t.trimTrailingSpace()
		}
	}
}

func (l *Box) checkPagePosition(lc *LayoutContext, alwaysBreak bool) {
	if !lc.isPageBreaksAllowed() {
		return
	}
	pb := lc.rootLayer.firstPage(lc, l)
	if pb != nil {
		needs := alwaysBreak || l.absY+l.height >= pb.bottom-lc.extraBottom
		if needs {
			l.forcePageBreakBefore(lc, "always", false)
			l.calcCanvasLocation()
		} else if pb.top+lc.extraTop > l.absY {
			diff := pb.top + lc.extraTop - l.absY
			l.y += diff
			l.calcCanvasLocation()
		}
	}
}

func (l *Box) isLastLineWithContent() bool {
	cur := l.nextSibling()
	if !l.endsOnNL {
		for cur != nil {
			if cur.containsContent {
				return false
			}
			cur = cur.nextSibling()
		}
	}
	return true
}

func (l *Box) countJustifiableCharsLine() *charCounts {
	cc := &charCounts{}
	for _, c := range l.children {
		if c.Kind == KInline {
			c.countJustifiableChars(cc)
		}
	}
	return cc
}

const (
	justifyNonSpaceShare = float32(0.20)
	justifySpaceShare    = 1 - justifyNonSpaceShare
)

func (l *Box) justify() {
	if l.isLastLineWithContent() {
		return
	}
	left, right := l.floatDistances.Left, l.floatDistances.Right
	available := l.parent.getContentWidth() - left - right - l.contentStart
	if available > l.contentWidth {
		toAdd := available - l.contentWidth
		counts := l.countJustifiableCharsLine()
		info := &JustificationInfo{}
		if !l.parent.Style.IsIdent("letter-spacing", "normal") {
			info.NonSpaceAdjust = 0
			info.SpaceAdjust = float32(toAdd) / float32(counts.spaces)
		} else {
			if counts.other > 1 {
				info.NonSpaceAdjust = float32(toAdd) * justifyNonSpaceShare / float32(counts.other-1)
			}
			if counts.spaces > 0 {
				info.SpaceAdjust = float32(toAdd) * justifySpaceShare / float32(counts.spaces)
			}
		}
		l.adjustChildren(info)
		l.justification = info
	}
}

func (l *Box) adjustChildren(info *JustificationInfo) {
	var adjust float32
	for _, c := range l.children {
		c.x += int(javaRound(adjust))
		if c.Kind == KInline {
			adjust += c.adjustHorizontalPosition(info, adjust)
		}
	}
	l.calcChildLocations()
}

func (l *Box) align(dynamic bool) {
	align := l.parent.Style.Ident("text-align")
	calcX := 0
	switch align {
	case "left", "justify":
		calcX = l.contentStart + l.floatDistances.Left
		if align == "justify" && dynamic {
			l.justify()
		}
	case "center":
		left, right := l.floatDistances.Left, l.floatDistances.Right
		mid := left + (l.parent.getContentWidth()-left-right)/2
		calcX = mid - (l.contentWidth+l.contentStart)/2
	case "right":
		calcX = l.parent.getContentWidth() - l.floatDistances.Right - l.contentWidth
	}
	if calcX != l.x {
		l.x = calcX
		l.calcCanvasLocation()
		l.calcChildLocations()
	}
}

// ---- InlineBoxing ----------------------------------------------------------------------------------

func positionHorizontallyBox(current *Box, start int) int {
	x := start
	var curIB *Box
	if current.Kind == KInline {
		curIB = current
		x += curIB.leftMarginBorderPadding()
	}
	for _, c := range current.children {
		if c.Kind == KInline {
			c.x = x
			x += positionHorizontallyInline(c, x)
		} else {
			c.x = x
			x += c.width()
		}
	}
	if curIB != nil {
		x += curIB.rightMarginBorderPadding()
		curIB.inlineWidth = x - start
	}
	return x - start
}

func positionHorizontallyInline(cur *Box, start int) int {
	x := start
	x += cur.leftMarginBorderPadding()
	for _, o := range cur.inlineChildren {
		switch c := o.(type) {
		case *Box:
			if c.Kind == KInline {
				c.x = x
				x += positionHorizontallyInline(c, x)
			} else {
				c.x = x
				x += c.width()
			}
		case *InlineText:
			c.x = x - start
			x += c.width
		}
	}
	x += cur.rightMarginBorderPadding()
	cur.inlineWidth = x - start
	return x - start
}

type inlineMeasurements struct {
	textTop, textBottom, baseline int
	inlineTop, inlineBottom       int
	paintingTop, paintingBottom   int
}

type vaChild struct {
	root *Box
	ctx  *vaContext
}

type vaContext struct {
	measurements                      []*inlineMeasurements
	inlineTop, inlineBottom           int
	inlineTopSet, inlineBottomSet     bool
	paintingTop, paintingBottom       int
	paintingTopSet, paintingBottomSet bool
	children                          []*vaChild
	parent                            *vaContext
}

func (v *vaContext) updateInlineTop(x int) {
	if !v.inlineTopSet || x < v.inlineTop {
		v.inlineTop, v.inlineTopSet = x, true
	}
}
func (v *vaContext) updatePaintingTop(x int) {
	if !v.paintingTopSet || x < v.paintingTop {
		v.paintingTop, v.paintingTopSet = x, true
	}
}
func (v *vaContext) updateInlineBottom(x int) {
	if !v.inlineBottomSet || x > v.inlineBottom {
		v.inlineBottom, v.inlineBottomSet = x, true
	}
}
func (v *vaContext) updatePaintingBottom(x int) {
	if !v.paintingBottomSet || x > v.paintingBottom {
		v.paintingBottom, v.paintingBottomSet = x, true
	}
}
func (v *vaContext) lineBoxHeight() int { return v.inlineBottom - v.inlineTop }

func (v *vaContext) pushMeasurements(m *inlineMeasurements) {
	v.measurements = append(v.measurements, m)
	v.updateInlineTop(m.inlineTop)
	v.updateInlineBottom(m.inlineBottom)
	v.updatePaintingTop(m.paintingTop)
	v.updatePaintingBottom(m.paintingBottom)
}
func (v *vaContext) parentMeasurements() *inlineMeasurements {
	return v.measurements[len(v.measurements)-1]
}
func (v *vaContext) popMeasurements() { v.measurements = v.measurements[:len(v.measurements)-1] }

func (v *vaContext) root() *vaContext {
	if v.parent != nil {
		return v.parent
	}
	return v
}

func (v *vaContext) createChild(root *Box) *vaContext {
	res := &vaContext{}
	vaRoot := v.root()
	res.parent = vaRoot
	res.pushMeasurements(vaRoot.measurements[0])
	vaRoot.children = append(vaRoot.children, &vaChild{root, res})
	return res
}

func (v *vaContext) moveTracked(ty int) {
	if v.inlineTopSet {
		v.inlineTop += ty
	}
	if v.inlineBottomSet {
		v.inlineBottom += ty
	}
	if v.paintingTopSet {
		v.paintingTop += ty
	}
	if v.paintingBottomSet {
		v.paintingBottom += ty
	}
}

func (v *vaContext) merge(o *vaContext) {
	v.updateInlineBottom(o.inlineBottom)
	v.updateInlineTop(o.inlineTop)
	v.updatePaintingBottom(o.paintingBottom)
	v.updatePaintingTop(o.paintingTop)
}

func moveInlineContentsVA(box, root *Box, ty int) {
	va := box.Style.Ident("vertical-align")
	if box == root || !(va == "top" || va == "bottom") {
		box.y += ty
		if box.Kind == KInline {
			for _, o := range box.inlineChildren {
				if c, ok := o.(*Box); ok {
					moveInlineContentsVA(c, root, ty)
				}
			}
		}
	}
}

func (v *vaContext) alignChildren() {
	for _, d := range v.children {
		va := d.root.Style.Ident("vertical-align")
		delta := 0
		if va == "top" {
			delta = d.ctx.root().inlineTop - d.ctx.inlineTop
		} else if va == "bottom" {
			delta = d.ctx.root().inlineBottom - d.ctx.inlineBottom
		}
		d.ctx.moveTracked(delta)
		moveInlineContentsVA(d.root, d.root, delta)
		v.merge(d.ctx)
	}
}

func getInitialMeasurements(container *Box, strut *FontMetrics) *inlineMeasurements {
	lineHeight := container.Style.LineHeight()
	halfLeading := int(javaRound((lineHeight - container.Style.Font().Size) / 2))
	if halfLeading > 0 {
		halfLeading = int(javaRound((lineHeight - (strut.Descent + strut.Ascent)) / 2))
	}
	m := &inlineMeasurements{}
	m.baseline = int(float32(halfLeading) + strut.Ascent)
	m.textTop = halfLeading
	m.textBottom = int(float32(m.baseline) + strut.Descent)
	m.inlineTop = halfLeading
	m.inlineBottom = int(float32(halfLeading) + lineHeight)
	return m
}

func createDefaultStrutMetrics(container *Box) StrutMetrics {
	sm := container.Style.FontMetrics()
	m := getInitialMeasurements(container, sm)
	return StrutMetrics{Ascent: sm.Ascent, Baseline: m.baseline, Descent: sm.Descent}
}

func calculateTextDecorations(box *Box, baseline int, fm *FontMetrics) []*TextDecoration {
	idents := box.Style.TextDecorations()
	if idents == nil {
		return nil
	}
	var res []*TextDecoration
	has := func(s string) bool {
		for _, i := range idents {
			if i == s {
				return true
			}
		}
		return false
	}
	if has("underline") {
		d := &TextDecoration{Ident: "underline"}
		if fm.UnderlineOffset == 0 {
			d.Offset = int(javaRound(float32(baseline) + fm.UnderlineThickness))
		} else {
			d.Offset = int(javaRound(float32(baseline) + fm.UnderlineOffset))
		}
		d.Thickness = int(javaRound(fm.UnderlineThickness))
		if fm.UnderlineOffset == 0 {
			maxOffset := baseline + int(fm.Descent) - d.Thickness
			if d.Offset > maxOffset {
				d.Offset = maxOffset
			}
		}
		res = append(res, d)
	}
	if has("line-through") {
		d := &TextDecoration{Ident: "line-through"}
		d.Offset = int(javaRound(float32(baseline) + fm.StrikeOffset))
		d.Thickness = int(javaRound(fm.StrikeThickness))
		res = append(res, d)
	}
	if has("overline") {
		d := &TextDecoration{Ident: "overline", Offset: 0}
		d.Thickness = int(javaRound(fm.UnderlineThickness))
		res = append(res, d)
	}
	return res
}

func alignInlineContent(box *Box, ascent, descent float32, va *vaContext) {
	m := va.parentMeasurements()
	st := box.Style
	if st.IsLength("vertical-align") {
		box.y = int(float32(m.baseline) - ascent - st.FloatProportional("vertical-align", st.LineHeight()))
		return
	}
	switch st.Ident("vertical-align") {
	case "baseline":
		box.y = int(javaRound(float32(m.baseline) - ascent))
	case "text-top":
		box.y = m.textTop
	case "text-bottom":
		box.y = int(javaRound(float32(m.textBottom) - descent - ascent))
	case "middle":
		box.y = int(javaRound(float32(m.baseline-m.textTop)/2 - (ascent+descent)/2))
	case "super":
		box.y = int(javaRound(float32(m.baseline) - (3*ascent)/2))
	case "sub":
		box.y = int(javaRound(float32(m.baseline) - ascent/2))
	default:
		box.y = int(javaRound(float32(m.baseline) - ascent))
	}
}

func calculateInlineMeasurements(iB *Box, va *vaContext) *inlineMeasurements {
	fm := iB.Style.FontMetrics()
	st := iB.Style
	lineHeight := st.LineHeight()
	halfLeading := int(javaRound((lineHeight - st.Font().Size) / 2))
	if halfLeading > 0 {
		halfLeading = int(javaRound((lineHeight - (fm.Descent + fm.Ascent)) / 2))
	}
	iB.lineBaseline = int(javaRound(fm.Ascent))
	alignInlineContent(iB, fm.Ascent, fm.Descent, va)
	if d := calculateTextDecorations(iB, iB.lineBaseline, fm); d != nil {
		iB.textDecorations = d
	}
	r := &inlineMeasurements{}
	r.baseline = iB.y + iB.lineBaseline
	r.inlineTop = iB.y - halfLeading
	r.inlineBottom = int(javaRound(float32(r.inlineTop) + lineHeight))
	r.textTop = iB.y
	r.textBottom = int(float32(r.baseline) + fm.Descent)
	pad := iB.getPadding()
	bd := iB.getBorder()
	r.paintingTop = int(math.Floor(float64(float32(iB.y) - bd.Top - pad.Top)))
	r.paintingBottom = int(math.Ceil(float64(float32(iB.y) + fm.Ascent + fm.Descent + bd.Bottom + pad.Bottom)))
	return r
}

func positionInlineChildrenVertically(cur *Box, va *vaContext) {
	for _, o := range cur.inlineChildren {
		if c, ok := o.(*Box); ok {
			positionInlineContentVertically(va, c)
		}
	}
}

func positionInlineContentVertically(va *vaContext, child *Box) {
	target := va
	if !child.Style.IsLength("vertical-align") {
		v := child.Style.Ident("vertical-align")
		if v == "top" || v == "bottom" {
			target = va.createChild(child)
		}
	}
	if child.Kind == KInline {
		m := calculateInlineMeasurements(child, target)
		target.pushMeasurements(m)
		positionInlineChildrenVertically(child, target)
		target.popMeasurements()
	} else {
		positionInlineBlockVertically(target, child)
	}
}

func positionInlineBlockVertically(va *vaContext, ib *Box) {
	baseline := ib.calcInlineBaseline()
	ascent := baseline
	descent := ib.height - baseline
	alignInlineContent(ib, float32(ascent), float32(descent), va)
	va.updateInlineTop(ib.y)
	va.updatePaintingTop(ib.y)
	va.updateInlineBottom(ib.y + ib.height)
	va.updatePaintingBottom(ib.y + ib.height)
}

func moveLineContents(line *Box, ty int) {
	for _, c := range line.children {
		c.y += ty
		if c.Kind == KInline {
			moveInlineContents(c, ty)
		}
	}
}

func moveInlineContents(b *Box, ty int) {
	for _, o := range b.inlineChildren {
		if c, ok := o.(*Box); ok {
			c.y += ty
			if c.Kind == KInline {
				moveInlineContents(c, ty)
			}
		}
	}
}

func positionVertically(lc *LayoutContext, container, cur *Box, marker *MarkerData) {
	if len(cur.children) == 0 || !cur.isContainsVisibleContent() {
		cur.height = 0
		return
	}
	strut := container.Style.FontMetrics()
	va := &vaContext{}
	m := getInitialMeasurements(container, strut)
	va.measurements = append(va.measurements, m)
	lbDecor := calculateTextDecorations(container, m.baseline, strut)
	if lbDecor != nil {
		cur.textDecorations = lbDecor
	}
	for _, c := range cur.children {
		positionInlineContentVertically(va, c)
	}
	va.alignChildren()
	cur.height = va.lineBoxHeight()
	paintingTop := va.paintingTop
	paintingBottom := va.paintingBottom
	if va.inlineTop < 0 {
		moveLineContents(cur, -va.inlineTop)
		for _, d := range lbDecor {
			d.Offset -= va.inlineTop
		}
		paintingTop -= va.inlineTop
		paintingBottom -= va.inlineTop
	}
	if marker != nil {
		marker.strut.Baseline = m.baseline - va.inlineTop
		marker.setReferenceLine(cur)
		cur.markerData = marker
	}
	cur.lineBaseline = m.baseline - va.inlineTop
	cur.paintingTop = paintingTop
	cur.paintingHeight = paintingBottom - paintingTop
}

func (box *Box) saveLine(lc *LayoutContext, current *Box, minHeight, maxAvail int, pendingFloats *[]*floatLayoutResult, marker *MarkerData, contentStart int, alwaysBreak bool) {
	current.contentStart = contentStart
	current.prunePendingInlineBoxes()
	total := positionHorizontallyBox(current, 0)
	current.contentWidth = total
	positionVertically(lc, box, current, marker)
	if current.height != 0 && current.height < minHeight && !current.isContainsOnlyBlockLevelContent() {
		current.height = minHeight
	}
	current.checkPagePosition(lc, alwaysBreak)
	alignLine(lc, current, maxAvail)
	current.calcChildLocations()
	box.addChild(current)
	if len(*pendingFloats) > 0 {
		for _, r := range *pendingFloats {
			layoutFloated(lc, current, r.block, maxAvail, nil)
			current.nonFlowContent = append(current.nonFlowContent, r.block)
		}
		*pendingFloats = nil
	}
}

func alignLine(lc *LayoutContext, cur *Box, maxAvail int) {
	cur.floatDistances = &FloatDistances{
		Left:  lc.bfc().fm.leftFloatDistance(lc.bfc(), cur, maxAvail),
		Right: lc.bfc().fm.rightFloatDistance(lc.bfc(), cur, maxAvail),
	}
	cur.align(false)
}

func hasTrimmableLeadingSpace(line *Box, style *Style, ctx *lbContext, zeroWidthInlineBlock bool) bool {
	ss := ctx.startSubstring()
	if (!line.containsContent || zeroWidthInlineBlock) && len(ss) > 0 && ss[0] == ' ' {
		ws := style.Whitespace()
		if ws == "normal" || ws == "nowrap" || ws == "pre-line" ||
			(ws == "pre-wrap" && ctx.start > 0 && len(ctx.master) > ctx.start-1 && ctx.master[ctx.start-1] != '\n') {
			return true
		}
	}
	return false
}

func trimLeadingSpace(ctx *lbContext) {
	s := ctx.startSubstring()
	i := 0
	for i < len(s) && s[i] == ' ' {
		i++
	}
	ctx.start += i
}

func addOpenInlineBoxes(lc *LayoutContext, line *Box, openParents []*InlineSrc, cbWidth int, ibMap map[*InlineSrc]*Box) *Box {
	var cur, prev *Box
	first := true
	for _, ib := range openParents {
		cur = newInlineLayoutBox(lc, nil, ib.style, cbWidth, ib)
		if p := ibMap[ib]; p != nil {
			cur.pending = p.pending
		}
		ibMap[ib] = cur
		if first {
			line.addChild(cur)
			first = false
		} else {
			prev.addInlineChild(cur, false)
		}
		prev = cur
	}
	return cur
}

func (box *Box) isAlwaysBreak(lc *LayoutContext, breakAtLine, lineOffset int) bool {
	if box.isCurrentBreakAtLineContext(lc) {
		return lineOffset == breakAtLine
	}
	return breakAtLine > 0 && lineOffset == breakAtLine
}

type floatLayoutResult struct {
	block   *Box
	pending bool
}

// layoutInlineContent is InlineBoxing.layoutContent.
func (box *Box) layoutInlineContent(lc *LayoutContext, initialY, breakAtLine int) {
	maxAvail := box.getContentWidth()
	remaining := maxAvail
	currentLine := newLine(lc, initialY, box)
	var previousLine *Box
	var currentIB, previousIB *Box
	contentStart := 0
	var openInline []*InlineSrc
	ibMap := map[*InlineSrc]*Box{}
	if box.Kind == KAnon {
		openInline = box.openInlineBoxes
		if openInline != nil {
			openInline = append([]*InlineSrc(nil), openInline...)
			currentIB = addOpenInlineBoxes(lc, currentLine, openInline, maxAvail, ibMap)
		}
	}
	remaining -= lc.bfc().fm.floatDistance(lc.bfc(), currentLine, remaining)
	parentStyle := box.Style
	minimumLineHeight := int(parentStyle.LineHeight())
	indent := int(parentStyle.FloatProportional("text-indent", float32(maxAvail)))
	remaining -= indent
	contentStart += indent
	marker := lc.markerData
	if marker != nil && box.Style.IsListMarkerInside() {
		remaining -= marker.layoutWidth()
		contentStart += marker.layoutWidth()
	}
	lc.markerData = nil
	var pendingFloats []*floatLayoutResult
	pendingLeftMBP, pendingRightMBP := 0, 0
	zeroWidthInlineBlock := false
	lineOffset := 0

	for _, node := range box.inlineContent {
		if node.GetStyle().IsInline() {
			iB := node.(*InlineSrc)
			style := iB.style
			if iB.startsHere {
				previousIB = currentIB
				currentIB = newInlineLayoutBox(lc, nil, style, maxAvail, iB)
				openInline = append(openInline, iB)
				ibMap[iB] = currentIB
				if previousIB == nil {
					currentLine.addChild(currentIB)
				} else {
					previousIB.addInlineChild(currentIB, true)
				}
				if currentIB.Elem != nil && currentIB.Elem.Name == "a" {
					if n, ok := currentIB.Elem.AttrOK("name"); ok && n != "" {
						lc.doc.boxIDs[n] = currentIB
					}
				}
				if currentIB.Elem != nil {
					if id := currentIB.Elem.Attr("id"); id != "" {
						lc.doc.boxIDs[id] = currentIB
					}
				}
				pendingLeftMBP += style.MarginBorderPadding(maxAvail, Left)
				pendingRightMBP += style.MarginBorderPadding(maxAvail, Right)
			}
			ctx := &lbContext{}
			ctx.master = []rune(iB.text)
			ctx.textNode = iB.textNode
			if iB.contentFunction != nil {
				ctx.master = []rune("999")
			}
			iter := 0
			for {
				iter++
				if iter > 100000 {
					panic("xhr: too many iterations in inline layout")
				}
				ctx.reset()
				fit := 0
				if ctx.start == 0 {
					fit += pendingLeftMBP + pendingRightMBP
				}
				trimmedLeading := false
				if hasTrimmableLeadingSpace(currentLine, style, ctx, zeroWidthInlineBlock) {
					trimmedLeading = true
					trimLeadingSpace(ctx)
				}
				ctx.endsOnNL = false
				zeroWidthInlineBlock = false
				if len(ctx.startSubstring()) == 0 {
					break
				}
				ctx.saveEnd()
				text := layoutText(iB.style, remaining-fit, ctx)
				if ctx.unbreakable && !currentLine.containsContent {
					delta := lc.bfc().fm.nextLineBoxDelta(lc.bfc(), currentLine, maxAvail)
					if delta > 0 {
						currentLine.y += delta
						currentLine.calcCanvasLocation()
						remaining = maxAvail
						remaining -= lc.bfc().fm.floatDistance(lc.bfc(), currentLine, maxAvail)
						ctx.resetEnd()
						continue
					}
				}
				if !ctx.unbreakable || (ctx.unbreakable && !currentLine.containsContent) {
					if iB.contentFunction != nil {
						text.fn = iB.contentFunction
					}
					text.trimmedLeading = trimmedLeading
					if text.fn != nil {
						currentLine.containsDynamicFn = true
					}
					currentIB.addInlineChild(text, true)
					currentLine.containsContent = true
					ctx.start = ctx.end
					remaining -= text.width
					if currentIB.startsHere {
						mbp := currentIB.Style.MarginBorderPadding(maxAvail, Left)
						pendingLeftMBP -= mbp
						remaining -= mbp
					}
				} else {
					ctx.resetEnd()
				}
				if ctx.needsNewLine {
					if iB.style.IsTextJustify() {
						currentLine.trimTrailingSpace()
					}
					box.saveLine(lc, currentLine, minimumLineHeight, maxAvail, &pendingFloats, marker, contentStart,
						box.isAlwaysBreak(lc, breakAtLine, lineOffset))
					lineOffset++
					marker = nil
					contentStart = 0
					if ctx.endsOnNL {
						currentLine.endsOnNL = true
					}
					previousLine = currentLine
					currentLine = newLine(lc, previousLine.y+previousLine.height, box)
					currentIB = addOpenInlineBoxes(lc, currentLine, openInline, maxAvail, ibMap)
					if currentIB == nil || currentIB.parent.Kind == KLine {
						previousIB = nil
					} else {
						previousIB = currentIB.parent
					}
					remaining = maxAvail
					remaining -= lc.bfc().fm.floatDistance(lc.bfc(), currentLine, remaining)
				}
				if ctx.isFinished() {
					break
				}
			}
			if iB.endsHere {
				right := style.MarginBorderPadding(maxAvail, Right)
				pendingRightMBP -= right
				remaining -= right
				openInline = openInline[:len(openInline)-1]
				if currentIB.pending {
					currentIB.unmarkPending()
					currentIB.startsHere = iB.startsHere
				}
				currentIB.endsHere = true
				previousIB = currentIB
				if currentIB.parent.Kind == KLine {
					currentIB = nil
				} else {
					currentIB = currentIB.parent
				}
			}
		} else {
			child := node.(*Box)
			cs := child.Style
			switch {
			case cs.IsNonFlowContent():
				remaining -= processOutOfFlowContent(lc, currentLine, child, remaining, &pendingFloats)
			case cs.IsInlineBlock() || cs.IsInlineTable():
				layoutInlineBlockContent(lc, box, child, initialY)
				if child.width() > remaining && currentLine.containsContent {
					box.saveLine(lc, currentLine, minimumLineHeight, maxAvail, &pendingFloats, marker, contentStart,
						box.isAlwaysBreak(lc, breakAtLine, lineOffset))
					lineOffset++
					marker = nil
					contentStart = 0
					previousLine = currentLine
					currentLine = newLine(lc, previousLine.y+previousLine.height, box)
					currentIB = addOpenInlineBoxes(lc, currentLine, openInline, maxAvail, ibMap)
					if currentIB == nil || currentIB.parent.Kind == KLine {
						previousIB = nil
					} else {
						previousIB = currentIB.parent
					}
					remaining = maxAvail
					remaining -= lc.bfc().fm.floatDistance(lc.bfc(), currentLine, remaining)
					child.reset(lc)
					layoutInlineBlockContent(lc, box, child, initialY)
				}
				if currentIB == nil {
					currentLine.addChild(child)
				} else {
					currentIB.addInlineChild(child, true)
				}
				currentLine.containsContent = true
				currentLine.containsBlockLevel = true
				remaining -= child.width()
				if currentIB != nil && currentIB.startsHere {
					pendingLeftMBP -= currentIB.Style.MarginBorderPadding(maxAvail, Left)
				}
				if child.width() == 0 {
					zeroWidthInlineBlock = true
				}
			}
		}
	}
	_ = previousIB
	currentLine.trimTrailingSpace()
	box.saveLine(lc, currentLine, minimumLineHeight, maxAvail, &pendingFloats, marker, contentStart,
		box.isAlwaysBreak(lc, breakAtLine, lineOffset))
	if currentLine.isFirstLine() && currentLine.height == 0 && marker != nil {
		lc.markerData = marker
	}
	box.contentWidth = maxAvail
	box.height = currentLine.y + currentLine.height
}

func layoutText(style *Style, remaining int, ctx *lbContext) *InlineText {
	t := &InlineText{}
	breakText(ctx, remaining, style)
	t.masterText = ctx.master
	t.setSubstring(ctx.start, ctx.end)
	t.width = ctx.width
	return t
}

func layoutInlineBlockContent(lc *LayoutContext, containing, inlineBlock *Box, initialY int) {
	inlineBlock.containingBlk = containing
	inlineBlock.initStaticPos(lc, containing, initialY)
	inlineBlock.calcCanvasLocation()
	inlineBlock.layoutBox(lc)
}

func processOutOfFlowContent(lc *LayoutContext, current *Box, block *Box, available int, pending *[]*floatLayoutResult) int {
	result := 0
	st := block.Style
	switch {
	case st.IsAbsolute() || st.IsFixed():
		// absolutely positioned boxes are not supported: they take no space
		block.staticEquivalent = current
	case st.IsFloated():
		r := layoutFloated(lc, current, block, available, pending)
		if r.pending {
			*pending = append(*pending, r)
		} else {
			result = r.block.width()
			current.nonFlowContent = append(current.nonFlowContent, r.block)
		}
	case st.IsRunning():
		block.staticEquivalent = current
		lc.rootLayer.addRunningBlock(block)
	}
	return result
}

var _ = strings.TrimSpace
