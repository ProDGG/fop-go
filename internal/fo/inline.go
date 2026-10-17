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
}

func newInlineBuilder(lc *lctx) *inlineBuilder {
	return &inlineBuilder{lc: lc, atStart: true}
}

func (ib *inlineBuilder) reset() {
	ib.atoms = ib.atoms[:0]
	ib.pending = false
	ib.atStart = true
}

func (ib *inlineBuilder) emitGlue(st *textStyle) {
	e := ib.lc.e
	ib.atoms = append(ib.atoms, atom{kind: aGlue, w: e.spaceWidth(st) + st.ws + st.ls, style: st, rise: st.shift})
}

func (ib *inlineBuilder) addText(n *Node, text string) {
	e := ib.lc.e
	st := e.styleFor(n)
	if st.ft == nil {
		return
	}
	text = applyTransform(text, st.upper)
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
					ib.atoms = append(ib.atoms, atom{kind: aGlue, w: e.spaceWidth(st), style: st, fixed: true, rise: st.shift})
				}
			case '\r':
			case ' ', '\t':
				flush(false)
				n := 1
				if r == '\t' {
					n = 8
				}
				for k := 0; k < n; k++ {
					ib.atoms = append(ib.atoms, atom{kind: aGlue, w: e.spaceWidth(st) + st.ws + st.ls, style: st, fixed: true, rise: st.shift})
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

// ---------- line breaking ----------

type paraOpts struct {
	width     float64 // available width for lines
	indent    float64 // text-indent for the first line
	align     string
	alignLast string
	strut     *textStyle
	nowrap    bool
}

func (lc *lctx) breakLines(atoms []atom, po paraOpts) []*lineBox {
	e := lc.e
	// trim trailing glue
	for len(atoms) > 0 && atoms[len(atoms)-1].kind == aGlue && !atoms[len(atoms)-1].fixed {
		atoms = atoms[:len(atoms)-1]
	}
	var lines []*lineBox
	var active []*inlineDecor
	pos := 0
	first := true
	for pos < len(atoms) {
		indent := 0.0
		if first {
			indent = po.indent
		}
		avail := po.width - indent
		if po.nowrap {
			avail = math.MaxFloat64 / 4
		}
		// skip leading collapsible glue
		for pos < len(atoms) && atoms[pos].kind == aGlue && !atoms[pos].fixed {
			pos++
		}
		if pos >= len(atoms) {
			break
		}
		end := pos      // exclusive end of atoms on this line
		next := pos     // where the next line starts
		lastBreak := -1 // candidate end index
		lastBreakW := 0.0
		curW := 0.0
		forced := false
		hyphenBreak := false
		i := pos
		for ; i < len(atoms); i++ {
			a := &atoms[i]
			if a.kind == aBreak {
				end, next, forced = i, i+1, true
				break
			}
			if a.kind == aGlue {
				if !a.fixed || !po.nowrap {
					if i > pos {
						lastBreak, lastBreakW = i, curW
					}
				}
				curW += a.w
				continue
			}
			if a.kind == aZW {
				if i > pos {
					lastBreak, lastBreakW = i+1, curW
				}
				continue
			}
			if a.kind == aSoftHyphen {
				if i > pos && curW+a.hyphenW <= avail {
					lastBreak, lastBreakW = i+1, curW+a.hyphenW
					hyphenBreak = true
				}
				continue
			}
			w := a.w
			if a.kind == aLeader {
				w = a.leader.minW
			}
			if curW+w > avail+0.01 && i > pos && hasContent(atoms[pos:i]) {
				// overflow: break at last opportunity
				if lastBreak >= 0 {
					end, next = lastBreak, lastBreak
					if atoms[lastBreak-1].kind == aSoftHyphen {
						end = lastBreak
					}
					curW = lastBreakW
				} else {
					end, next = i, i
				}
				break
			}
			if curW+w > avail+0.01 && a.kind == aWord && len(a.text) > 1 && !po.nowrap {
				// a single over-long word: split it
				head, tail := lc.splitWord(a, avail-curW)
				if head != nil {
					na := make([]atom, 0, len(atoms)+1)
					na = append(na, atoms[:i]...)
					na = append(na, *head, *tail)
					na = append(na, atoms[i+1:]...)
					atoms = na
					a = &atoms[i]
					w = a.w
				}
			}
			curW += w
			if a.kind == aWord && a.breakAfter && i+1 < len(atoms) {
				lastBreak, lastBreakW = i+1, curW
			}
			end, next = i+1, i+1
		}
		if i >= len(atoms) && !forced {
			end, next = len(atoms), len(atoms)
		}
		_ = hyphenBreak
		_ = e
		lineAtoms := atoms[pos:end]
		isLast := forced || next >= len(atoms)
		// when we broke at a soft hyphen, show the hyphen
		showHyphen := end > pos && atoms[end-1].kind == aSoftHyphen
		lb := lc.buildLine(lineAtoms, po, indent, isLast, showHyphen, &active)
		lines = append(lines, lb)
		first = false
		pos = next
		if forced && pos >= len(atoms) {
			break
		}
	}
	for i, l := range lines {
		l.idx = i
		l.nlines = len(lines)
	}
	return lines
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

// splitWord splits a long word so that the head fits within w.
func (lc *lctx) splitWord(a *atom, w float64) (*atom, *atom) {
	rs := []rune(a.text)
	if len(rs) < 2 {
		return nil, nil
	}
	acc := 0.0
	cut := 0
	for i := range rs {
		cw := runsWidth(lc.e.measure(a.style, string(rs[i])))
		if acc+cw > w && i > 0 {
			break
		}
		acc += cw
		cut = i + 1
	}
	if cut == 0 {
		cut = 1
	}
	if cut >= len(rs) {
		return nil, nil
	}
	hr := lc.e.measure(a.style, string(rs[:cut]))
	tr := lc.e.measure(a.style, string(rs[cut:]))
	h := *a
	h.runs, h.w, h.text, h.breakAfter = hr, runsWidth(hr), string(rs[:cut]), true
	t := *a
	t.runs, t.w, t.text = tr, runsWidth(tr), string(rs[cut:])
	return &h, &t
}

func (lc *lctx) buildLine(atoms []atom, po paraOpts, indent float64, isLast, showHyphen bool, active *[]*inlineDecor) *lineBox {
	// drop trailing collapsible glue
	for len(atoms) > 0 {
		l := atoms[len(atoms)-1]
		if (l.kind == aGlue && !l.fixed) || l.kind == aZW {
			atoms = atoms[:len(atoms)-1]
			continue
		}
		break
	}
	lb := &lineBox{w: po.width}
	type pi struct {
		it    lineItem
		glue  bool
		leads bool
		open  *inlineDecor
		close *inlineDecor
	}
	var items []pi
	natural := 0.0
	nGlue := 0
	var leaders []int
	for _, a := range atoms {
		switch a.kind {
		case aWord:
			items = append(items, pi{it: lineItem{kind: itText, w: a.w, style: a.style, runs: a.runs, rise: a.rise}})
			natural += a.w
		case aGlue:
			items = append(items, pi{it: lineItem{kind: itGlue, w: a.w, style: a.style, rise: a.rise}, glue: !a.fixed})
			natural += a.w
			if !a.fixed {
				nGlue++
			}
		case aBox:
			items = append(items, pi{it: lineItem{kind: itImage, w: a.w, img: a.img, style: a.style, rise: a.rise}})
			natural += a.w
		case aLeader:
			items = append(items, pi{it: lineItem{kind: itLeader, w: a.leader.minW, lead: a.leader, style: a.style}, leads: true})
			leaders = append(leaders, len(items)-1)
			natural += a.leader.minW
		case aOpen:
			items = append(items, pi{it: lineItem{kind: itGlue, w: a.w, style: a.style}, open: a.decor})
			natural += a.w
		case aClose:
			items = append(items, pi{it: lineItem{kind: itGlue, w: a.w, style: a.style}, close: a.decor})
			natural += a.w
		case aAnchor:
			lb.ids = append(lb.ids, a.id)
		case aSoftHyphen:
		}
	}
	if showHyphen && len(atoms) > 0 {
		sh := atoms[len(atoms)-1]
		items = append(items, pi{it: lineItem{kind: itText, w: sh.hyphenW, style: sh.style, runs: sh.hyphen}})
		natural += sh.hyphenW
	}
	avail := po.width - indent
	free := avail - natural
	// leaders absorb free space first
	if len(leaders) > 0 && free > 0 {
		share := free
		var growable []int
		for _, li := range leaders {
			if items[li].it.lead.maxW > items[li].it.lead.minW {
				growable = append(growable, li)
			}
		}
		if len(growable) > 0 {
			per := share / float64(len(growable))
			for _, li := range growable {
				ld := items[li].it.lead
				add := math.Min(per, ld.maxW-ld.minW)
				items[li].it.w += add
				free -= add
			}
		}
	}
	align := po.align
	if isLast {
		align = po.alignLast
	}
	offset := indent
	extraPerGlue := 0.0
	switch align {
	case "center":
		offset += free / 2
	case "end", "right":
		offset += free
	case "justify":
		if !isLast && nGlue > 0 && free > 0 {
			extraPerGlue = free / float64(nGlue)
		}
	}
	if free < 0 && align != "start" && align != "left" && align != "justify" {
		// overflowing line: align to the start
		offset = indent
	}
	x := offset
	type openRec struct {
		d  *inlineDecor
		x0 float64
		f  bool
	}
	var open []openRec
	for _, d := range *active {
		open = append(open, openRec{d: d, x0: x})
	}
	for _, p := range items {
		it := p.it
		it.x = x
		if p.glue {
			it.w += extraPerGlue
		}
		if p.open != nil {
			open = append(open, openRec{d: p.open, x0: x, f: true})
			*active = append(*active, p.open)
		}
		x += it.w
		if p.close != nil {
			for k := len(open) - 1; k >= 0; k-- {
				if open[k].d == p.close {
					lb.decors = append(lb.decors, decorSeg{x0: open[k].x0, x1: x, d: p.close, first: open[k].f, last: true})
					open = append(open[:k], open[k+1:]...)
					break
				}
			}
			for k, d := range *active {
				if d == p.close {
					*active = append((*active)[:k], (*active)[k+1:]...)
					break
				}
			}
		}
		if it.kind != itGlue || it.style != nil {
			lb.items = append(lb.items, it)
		}
	}
	for _, o := range open { // still open at end of line
		lb.decors = append(lb.decors, decorSeg{x0: o.x0, x1: x, d: o.d, first: o.f})
	}
	// vertical metrics
	above, below := 0.0, 0.0
	consider := func(st *textStyle, rise float64) {
		if st == nil || st.ft == nil {
			return
		}
		asc, desc := st.ascDesc()
		lead := st.lh - (asc + desc)
		a := asc + lead/2 + rise
		b := desc + lead/2 - rise
		above, below = math.Max(above, a), math.Max(below, b)
	}
	consider(po.strut, 0)
	for _, it := range lb.items {
		switch it.kind {
		case itText, itGlue, itLeader:
			consider(it.style, it.rise)
		case itImage:
			above = math.Max(above, it.img.h+it.rise)
		}
	}
	lb.h = above + below
	lb.base = above
	return lb
}

// ---------- rendering of lines ----------

func (l *lineBox) height() float64                    { return l.h }
func (l *lineBox) before() float64                    { return 0 }
func (l *lineBox) after() float64                     { return 0 }
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
			if it.style == nil || it.style.ft == nil {
				flush()
				continue
			}
			if len(glyphs) > 0 && curFont == it.style.ft && curStyle == it.style {
				gid := curFont.GlyphIndex(' ')
				adv := float64(curFont.Advance(gid)) * it.style.size / float64(curFont.UnitsPerEm)
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
