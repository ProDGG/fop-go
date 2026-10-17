package fo

import (
	"math"
	"strings"
	"unicode"

	"github.com/ProDGG/fop-go/internal/font"
	"github.com/ProDGG/fop-go/internal/pdf"
)

type atomKind int

const (
	aWord atomKind = iota
	aGlue
	aBox
	aLeader
	aOpen
	aClose
	aAnchor
	aBreak
	aSoftHyphen
	aZW
)

type atom struct {
	kind       atomKind
	w          float64
	runs       []textRun
	style      *textStyle
	img        *imageBox
	leader     *leaderSpec
	decor      *inlineDecor
	id         string
	breakAfter bool
	fixed      bool // non-collapsible, non-stretchable space (preserved text)
	hyphen     []textRun
	hyphenW    float64
	stretch    int // adjustability in millipoints (spaces, leaders)
	shrink     int
	nb         bool // non-breaking space
	rise       float64
	text       string // for words: the source text (used when splitting overlong words)
}

type imageBox struct {
	img    *imageData
	w, h   float64 // viewport
	cw, ch float64 // content size
	clip   bool
}

type leaderSpec struct {
	pattern   string // dots, rule, space, use-content
	minW      float64
	optW      float64
	maxW      float64
	thickness float64
	rstyle    string
	color     Color
	patRuns   []textRun // one pattern cell
	patW      float64
	style     *textStyle
}

type inlineDecor struct {
	bg    Color
	b     [4]border
	p     [4]float64
	style *textStyle
}

type itemKind int

const (
	itText itemKind = iota
	itGlue
	itImage
	itLeader
)

type lineItem struct {
	kind  itemKind
	x, w  float64
	style *textStyle
	runs  []textRun
	img   *imageBox
	lead  *leaderSpec
	rise  float64
}

type decorSeg struct {
	x0, x1 float64
	d      *inlineDecor
	first  bool // segment includes the start of the inline
	last   bool // segment includes the end of the inline
}

type lineBox struct {
	h, base float64
	w       float64
	items   []lineItem
	decors  []decorSeg
	ids     []string
	paraID  int
	idx     int
	nlines  int

	// source paragraph, kept so that a continuation can be re-broken when the
	// next page is narrower or wider (FOP restarts layout when the IPD changes)
	src       *paraSrc
	atomStart int
	act       []*inlineDecor // inline decorations open at the start of the line
}

type paraSrc struct {
	lc    *lctx
	atoms []atom
	po    paraOpts
	first bool // the paragraph's first line takes text-indent
}

// ---------- building atoms from text ----------

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) ||
		(r >= 0x3000 && r <= 0x303F) || (r >= 0xFF00 && r <= 0xFFEF)
}

type inlineBuilder struct {
	lc        *lctx
	atoms     []atom
	pending   bool // collapsed whitespace waiting to be emitted
	pendStyle *textStyle
	atStart   bool // no content yet on the current line/paragraph
	lastGlue  bool
	foTexts   [][]rune // raw text nodes of this block so far (FOText.prevFOTextThisBlock chain)
}

func newInlineBuilder(lc *lctx) *inlineBuilder {
	return &inlineBuilder{lc: lc, atStart: true}
}

func (ib *inlineBuilder) reset() {
	ib.atoms = ib.atoms[:0]
	ib.pending = false
	ib.atStart = true
}

// spaceAtom builds a space glue the way FOP's TextLayoutManager does: with
// word-spacing "normal" a space may shrink by a third and stretch by a half of its
// width (SpaceVal.makeWordSpacing); an explicit word-spacing makes it rigid.
func (e *Engine) spaceAtom(st *textStyle) atom {
	s := e.spaceMpt(st)
	a := atom{kind: aGlue, style: st, rise: st.shift}
	if st.wsExplicit {
		a.w = float64(s+mpt(st.ws)) / 1000
	} else {
		a.w = float64(s+2*mpt(st.ls)) / 1000
		a.stretch = s / 2
		a.shrink = s / 3
	}
	return a
}

func (ib *inlineBuilder) emitGlue(st *textStyle) {
	ib.atoms = append(ib.atoms, ib.lc.e.spaceAtom(st))
}

// addFOText adds a character data node; text-transform follows FOText.textTransform,
// including its look-behind into the previous text nodes of the block.
func (ib *inlineBuilder) addFOText(n *Node, text string) {
	st := ib.lc.e.styleFor(n)
	raw := []rune(text)
	t := text
	if st.ft != nil {
		t = fopTransform(raw, st.upper, ib.foTexts)
	}
	ib.foTexts = append(ib.foTexts, raw)
	ib.addTextRaw(n, t)
}

func (ib *inlineBuilder) addText(n *Node, text string) {
	ib.addTextRaw(n, applyTransform(text, ib.lc.e.styleFor(n).upper))
}

func (ib *inlineBuilder) addTextRaw(n *Node, text string) {
	e := ib.lc.e
	st := e.styleFor(n)
	if st.ft == nil {
		return
	}
	var word strings.Builder
	flush := func(breakAfter bool) {
		if word.Len() == 0 {
			return
		}
		s := word.String()
		word.Reset()
		runs := e.measure(st, s)
		ib.atoms = append(ib.atoms, atom{kind: aWord, w: runsWidth(runs), runs: runs, style: st, breakAfter: breakAfter, rise: st.shift, text: s})
		ib.atStart = false
	}
	content := func() {
		if ib.pending && !ib.atStart {
			ps := ib.pendStyle
			if ps == nil {
				ps = st
			}
			ib.emitGlue(ps)
		}
		ib.pending = false
	}
	rs := []rune(text)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case st.preserve:
			switch r {
			case '\n':
				flush(false)
				if st.lfPreserve {
					ib.atoms = append(ib.atoms, atom{kind: aBreak, style: st})
					ib.atStart = true
				} else {
					sp := e.spaceAtom(st)
					sp.fixed = true
					ib.atoms = append(ib.atoms, sp)
				}
			case '\r':
			case ' ', '\t':
				flush(false)
				n := 1
				if r == '\t' {
					n = 8
				}
				for k := 0; k < n; k++ {
					sp := e.spaceAtom(st)
					sp.fixed = true
					ib.atoms = append(ib.atoms, sp)
				}
				ib.atStart = false
			default:
				ib.addRune(&word, r, st, flush)
			}
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			flush(false)
			if !ib.pending {
				ib.pending = true
				ib.pendStyle = st
			}
		default:
			if !(r == 0xAD || r == 0x200B) {
				content()
			}
			ib.addRune(&word, r, st, flush)
			if r == '-' && i+1 < len(rs) && rs[i+1] != ' ' && rs[i+1] != '-' && i > 0 && unicode.IsLetter(rs[i-1]) {
				flush(true)
			}
		}
	}
	flush(false)
}

func (ib *inlineBuilder) addRune(word *strings.Builder, r rune, st *textStyle, flush func(bool)) {
	e := ib.lc.e
	switch {
	case r == 0xAD:
		flush(false)
		hy := e.measure(st, "-")
		ib.atoms = append(ib.atoms, atom{kind: aSoftHyphen, hyphen: hy, hyphenW: runsWidth(hy), style: st})
	case r == 0x200B:
		flush(false)
		ib.atoms = append(ib.atoms, atom{kind: aZW, style: st})
	case r == 0xA0:
		flush(false)
		sp := e.spaceAtom(st)
		sp.nb = true
		ib.atoms = append(ib.atoms, sp)
	case isCJK(r):
		flush(false)
		word.WriteRune(r)
		flush(true)
	default:
		word.WriteRune(r)
	}
}

// addBox appends an inline box (image, etc.).
func (ib *inlineBuilder) addBox(a atom) {
	if ib.pending && !ib.atStart {
		ps := ib.pendStyle
		if ps == nil {
			ps = a.style
		}
		ib.emitGlue(ps)
	}
	ib.pending = false
	ib.atStart = false
	ib.atoms = append(ib.atoms, a)
}

// ---------- line assembly ----------

type paraOpts struct {
	width     float64 // available width for lines
	indent    float64 // text-indent for the first line
	align     string
	alignLast string
	strut     *textStyle
	nowrap    bool
}

func hasContent(as []atom) bool {
	for _, a := range as {
		switch a.kind {
		case aWord, aBox, aLeader:
			return true
		}
	}
	return false
}

// lineMetrics computes the line height and baseline position the way
// Apache FOP's LineLayoutManager.makeLineBreakPosition does: the leading comes from
// the block's line-height and font, inline fonts only extend lead/follow.
func lineMetrics(strut *textStyle, items []lineItem) (h, base float64) {
	var lead, follow, lh int
	if strut != nil && strut.ft != nil {
		lead, follow = strut.ascDescMpt()
		lh = mpt(strut.lh)
	}
	spaceBefore := (lh - lead - follow) / 2
	spaceAfter := lh - lead - follow - spaceBefore
	lineLead, lineFollow := lead, follow
	for _, it := range items {
		switch it.kind {
		case itText:
			if it.style == nil || it.style.ft == nil {
				continue
			}
			a, d := it.style.ascDescMpt()
			rise := mpt(it.rise)
			lineLead = max(lineLead, a+rise)
			lineFollow = max(lineFollow, d-rise)
		case itImage:
			lineLead = max(lineLead, mpt(it.img.h+it.rise))
			lineFollow = max(lineFollow, mpt(-it.rise))
		case itLeader:
			if it.lead != nil && it.lead.pattern == "rule" {
				lineLead = max(lineLead, mpt(it.lead.thickness))
			}
		}
	}
	return float64(spaceBefore+lineLead+lineFollow+spaceAfter) / 1000, float64(spaceBefore+lineLead) / 1000
}

// buildLine positions the atoms of one line starting at x0 (widths are already
// adjusted by the breaking algorithm's ratio) and computes the line metrics.
func (lc *lctx) buildLine(atoms []atom, po paraOpts, x0 float64, showHyphen bool, hy *atom, active *[]*inlineDecor) *lineBox {
	lb := &lineBox{w: po.width}
	x := x0
	type openRec struct {
		d  *inlineDecor
		x0 float64
		f  bool
	}
	var open []openRec
	for _, d := range *active {
		open = append(open, openRec{d: d, x0: x})
	}
	push := func(it lineItem) {
		it.x = x
		lb.items = append(lb.items, it)
		x += it.w
	}
	for _, a := range atoms {
		switch a.kind {
		case aWord:
			push(lineItem{kind: itText, w: a.w, style: a.style, runs: a.runs, rise: a.rise})
		case aGlue:
			push(lineItem{kind: itGlue, w: a.w, style: a.style, rise: a.rise})
		case aBox:
			push(lineItem{kind: itImage, w: a.w, img: a.img, style: a.style, rise: a.rise})
		case aLeader:
			push(lineItem{kind: itLeader, w: a.w, lead: a.leader, style: a.style})
		case aOpen:
			open = append(open, openRec{d: a.decor, x0: x, f: true})
			*active = append(*active, a.decor)
			push(lineItem{kind: itGlue, w: a.w, style: a.style})
		case aClose:
			push(lineItem{kind: itGlue, w: a.w, style: a.style})
			for k := len(open) - 1; k >= 0; k-- {
				if open[k].d == a.decor {
					lb.decors = append(lb.decors, decorSeg{x0: open[k].x0, x1: x, d: a.decor, first: open[k].f, last: true})
					open = append(open[:k], open[k+1:]...)
					break
				}
			}
			for k, d := range *active {
				if d == a.decor {
					*active = append((*active)[:k], (*active)[k+1:]...)
					break
				}
			}
		case aAnchor:
			lb.ids = append(lb.ids, a.id)
		}
	}
	if showHyphen && hy != nil {
		push(lineItem{kind: itText, w: hy.hyphenW, style: hy.style, runs: hy.hyphen})
	}
	for _, o := range open { // still open at the end of the line
		lb.decors = append(lb.decors, decorSeg{x0: o.x0, x1: x, d: o.d, first: o.f})
	}
	lb.h, lb.base = lineMetrics(po.strut, lb.items)
	return lb
}

// ---------- rendering of lines ----------

func (l *lineBox) height() float64                    { return l.h }
func (l *lineBox) before() float64                    { return 0 }
func (l *lineBox) after() float64                     { return 0 }
func (l *lineBox) beforeSp() []spc                    { return nil }
func (l *lineBox) afterSp() []spc                     { return nil }
func (l *lineBox) keepNext() bool                     { return false }
func (l *lineBox) keepPrev() bool                     { return false }
func (l *lineBox) breakBefore() brk                   { return brkNone }
func (l *lineBox) breakAfter() brk                    { return brkNone }
func (l *lineBox) split(float64, bool) (vitem, vitem) { return nil, nil }

func (l *lineBox) render(r *renderCtx, x, y float64) {
	for _, id := range l.ids {
		r.registerID(id, y)
	}
	// inline backgrounds and borders first
	for _, d := range l.decors {
		st := d.d.style
		asc, desc := st.ascDesc()
		top := y + l.base - asc - st.shift - d.d.p[0] - d.d.b[0].w
		h := asc + desc + d.d.p[0] + d.d.p[2] + d.d.b[0].w + d.d.b[2].w
		w := d.x1 - d.x0
		bx := x + d.x0
		if d.d.bg.A > 0 {
			r.fillRect(bx, top, w, h, d.d.bg)
		}
		bd := d.d.b
		if !d.first {
			bd[3].w = 0
		}
		if !d.last {
			bd[1].w = 0
		}
		r.drawBorders(bx, top, w, h, bd, false)
	}
	r.drawLineText(l, x, y)
}

func (r *renderCtx) drawLineText(l *lineBox, x, y float64) {
	baseY := y + l.base
	var glyphs []pdf.Glyph
	var curFont *font.Font
	var curStyle *textStyle
	var startX, curRise float64
	flush := func() {
		if len(glyphs) > 0 {
			r.drawGlyphs(curStyle, curFont, startX, baseY-curRise, glyphs)
		}
		glyphs, curFont, curStyle = nil, nil, nil
	}
	for _, it := range l.items {
		switch it.kind {
		case itText:
			off := 0.0
			for _, run := range it.runs {
				if curFont != run.ft || curStyle != it.style || len(glyphs) == 0 {
					flush()
					curFont, curStyle, startX, curRise = run.ft, it.style, x+it.x+off, it.rise
				}
				glyphs = append(glyphs, run.glyphs...)
				off += run.width
			}
		case itGlue:
			if len(glyphs) > 0 && curFont != nil && curStyle != nil {
				// keep a real space glyph in the text run (also across style changes) so
				// the text stays searchable; the glue width is made up with an adjustment
				gid := curFont.GlyphIndex(' ')
				adv := float64(curFont.Advance(gid)) * curStyle.size / float64(curFont.UnitsPerEm)
				glyphs = append(glyphs, pdf.Glyph{GID: gid, Rune: ' ', Extra: it.w - adv})
			} else {
				flush()
			}
		case itImage:
			flush()
			r.drawImage(it.img, x+it.x, baseY-it.rise-it.img.h)
		case itLeader:
			flush()
			r.drawLeader(it.lead, x+it.x, baseY, it.w)
		}
	}
	flush()
	// text decorations: merge adjacent items with identical style
	for idx := 0; idx < len(l.items); {
		it := l.items[idx]
		if it.style == nil || !(it.style.underline || it.style.overline || it.style.strike) || it.kind == itImage || it.kind == itLeader {
			idx++
			continue
		}
		j := idx
		end := it.x + it.w
		for j+1 < len(l.items) && l.items[j+1].style == it.style && l.items[j+1].kind != itImage && l.items[j+1].kind != itLeader {
			j++
			end = l.items[j].x + l.items[j].w
		}
		st := it.style
		asc, _ := st.ascDesc()
		th := math.Max(0.5, st.size/18)
		by := baseY - it.rise
		if st.underline {
			r.fillRect(x+it.x, by+st.size*0.12, end-it.x, th, st.color)
		}
		if st.strike {
			r.fillRect(x+it.x, by-st.size*0.28, end-it.x, th, st.color)
		}
		if st.overline {
			r.fillRect(x+it.x, by-asc, end-it.x, th, st.color)
		}
		idx = j + 1
	}
	// link annotations
	for idx := 0; idx < len(l.items); {
		it := l.items[idx]
		if it.style == nil || it.style.link == nil {
			idx++
			continue
		}
		j := idx
		end := it.x + it.w
		for j+1 < len(l.items) && l.items[j+1].style != nil && l.items[j+1].style.link == it.style.link {
			j++
			end = l.items[j].x + l.items[j].w
		}
		r.addLink(it.style.link, x+it.x, y, end-it.x, l.h)
		idx = j + 1
	}
}
