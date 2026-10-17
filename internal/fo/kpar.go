package fo

import (
	"math"

	"github.com/ProDGG/fop-go/internal/pdf"
)

// Paragraph element generation and line assembly: a port of the relevant parts of
// Apache FOP's TextLayoutManager, LeaderLayoutManager and
// LineLayoutManager (Paragraph, LineBreakingAlgorithm.updateData2,
// findOptimalBreakingPoints).

// alignment values used for element generation: "start", "end", "center",
// "justify", or "" for "last line justified, the others not".

func glueStretchShrink(a *atom) (st, sh int) {
	return a.stretch, a.shrink
}

// spaceElements mirrors TextLayoutManager.getElementsForBreakingSpace.
// atomIdx is the index of the space atom; wm its optimum width; stretch/shrink
// are the adjustability of the space.
func spaceElements(al string, atomIdx, wm, st, sh int, preserved bool) []kelem {
	var els []kelem
	if preserved {
		// a breaking space that needs to be preserved (skipZeroCheck = true)
		switch al {
		case "center":
			els = append(els,
				glueEl(0, 3*defaultSpaceW, 0, -1, true), penEl(0, 0, false, -1, true),
				glueEl(0, -6*defaultSpaceW, 0, -1, true), boxEl(0, -1, true), penEl(0, kInfinite, false, -1, true),
				glueEl(wm, 3*defaultSpaceW, 0, atomIdx, false))
		case "start", "end":
			els = append(els,
				glueEl(0, 3*defaultSpaceW, 0, -1, true), penEl(0, 0, false, -1, true),
				glueEl(0, -3*defaultSpaceW, 0, -1, true), boxEl(0, -1, true), penEl(0, kInfinite, false, -1, true),
				glueEl(wm, 0, 0, atomIdx, false))
		case "justify":
			els = append(els,
				glueEl(0, 0, 0, -1, true), penEl(0, 0, false, -1, true),
				glueEl(0, st, sh, -1, true), boxEl(0, -1, true), penEl(0, kInfinite, false, -1, true),
				glueEl(wm, 0, 0, atomIdx, false))
		default:
			els = append(els,
				glueEl(0, 0, 0, -1, true), penEl(0, 0, false, -1, true),
				glueEl(0, st, 0, -1, true), boxEl(0, -1, true), penEl(0, kInfinite, false, -1, true),
				glueEl(wm, 0, 0, atomIdx, false))
		}
		return els
	}
	switch al {
	case "center":
		els = append(els,
			glueEl(0, 3*defaultSpaceW, 0, -1, true), penEl(0, 0, false, -1, true),
			glueEl(wm, -6*defaultSpaceW, 0, atomIdx, false), boxEl(0, -1, true), penEl(0, kInfinite, false, -1, true),
			glueEl(0, 3*defaultSpaceW, 0, -1, true))
	case "start", "end":
		els = append(els,
			glueEl(0, 3*defaultSpaceW, 0, -1, true), penEl(0, 0, false, -1, true),
			glueEl(wm, -3*defaultSpaceW, 0, atomIdx, false))
	case "justify":
		els = append(els, glueEl(wm, st, sh, atomIdx, false))
	default:
		els = append(els, glueEl(wm, st, 0, atomIdx, false))
	}
	return els
}

// hyphenElements mirrors TextLayoutManager.addElementsForAHyphen for the case
// without line start/end border or padding (unflagged break).
func hyphenElements(al string, widthIfBreak, widthIfNoBreak int, atomIdx int, flagged bool, centerW int) []kelem {
	pen := softHyphenPen
	if flagged {
		pen = kFlaggedPen
	}
	var els []kelem
	hy := func() kelem {
		e := penEl(widthIfBreak, pen, flagged, atomIdx, true)
		e.hyphen = widthIfBreak > 0
		return e
	}
	switch al {
	case "center":
		// FOP quirk: the centered case always uses the width of the hyphen glyph for the
		// penalty, even for an explicit hyphen character where nothing is added on a break
		cp := penEl(centerW, pen, flagged, atomIdx, true)
		cp.hyphen = widthIfBreak > 0
		els = append(els,
			penEl(0, kInfinite, false, -1, true), glueEl(0, 3*defaultSpaceW, 0, -1, true),
			cp, glueEl(0, -6*defaultSpaceW, 0, -1, false), boxEl(0, -1, true), penEl(0, kInfinite, false, -1, true),
			glueEl(0, 3*defaultSpaceW, 0, -1, true))
	case "start", "end":
		els = append(els,
			penEl(0, kInfinite, false, -1, true), glueEl(0, 3*defaultSpaceW, 0, -1, false),
			hy(), glueEl(widthIfNoBreak, -3*defaultSpaceW, 0, -1, false))
	default:
		els = append(els, hy())
		if widthIfNoBreak != 0 {
			els = append(els, glueEl(widthIfNoBreak, 0, 0, -1, false))
		}
	}
	return els
}

type lineRec struct {
	first, last int // element indexes (inclusive of the breaking element)
	line        int
	difference  int
	ratio       float64
	availShrink int
	nodeLine    int
}

// paragraphLines breaks one paragraph (atoms without aBreak) into lines.
func (lc *lctx) paragraphLines(atoms []atom, po paraOpts, firstInBlock bool, paraID *int) []*lineBox {
	lines := lc.breakParagraph(atoms, po, firstInBlock, nil, 0)
	if len(lines) > 0 {
		src := &paraSrc{lc: lc, atoms: atoms, po: po, first: firstInBlock}
		for _, l := range lines {
			l.src = src
		}
	}
	return lines
}

// breakParagraph breaks atoms into lines. active lists the inline decorations open
// at the start; off is the index of atoms[0] in the paragraph's full atom list.
func (lc *lctx) breakParagraph(atoms []atom, po paraOpts, firstInBlock bool, active0 []*inlineDecor, off int) []*lineBox {
	textAlign, textAlignLast := po.align, po.alignLast
	// effective alignment used when generating elements
	eff := textAlign
	if textAlign != "justify" && textAlignLast == "justify" {
		eff = ""
	}
	ipd := mpt(po.width)
	var els []kelem
	ignoreStart := 0
	if textAlign == "center" && textAlignLast != "justify" {
		els = append(els, glueEl(0, 3*defaultSpaceW, 0, -1, false))
		ignoreStart++
	}
	indentMpt := mpt(po.indent)
	if firstInBlock && indentMpt != 0 {
		els = append(els, boxEl(indentMpt, -1, false))
		ignoreStart++
	}
	for i := range atoms {
		a := &atoms[i]
		switch a.kind {
		case aWord:
			w := mpt(a.w)
			ls := mpt(a.style.ls)
			// FOP counts length-1 letter spaces for a word; the last one is only added
			// when more text follows without a space (addALetterSpaceTo)
			trailing := false
			if ls != 0 && !a.breakAfter {
				trailing = true
				for j := i + 1; j < len(atoms); j++ {
					switch atoms[j].kind {
					case aOpen, aClose, aAnchor:
						continue
					case aGlue, aBreak:
						trailing = false
					}
					break
				}
				if i == len(atoms)-1 {
					trailing = false
				}
			}
			if ls != 0 && (a.breakAfter || !trailing) {
				els = append(els, boxEl(w-ls, i, false))
			} else {
				els = append(els, boxEl(w, i, false))
			}
			if a.breakAfter {
				els = append(els, hyphenElements(eff, 0, ls, -1, false, runsWidthMpt(lc.e.measure(a.style, "-")))...)
			}
		case aGlue:
			wm := mpt(a.w)
			switch {
			case a.nb: // non-breaking space
				if eff == "justify" {
					els = append(els, boxEl(0, -1, true), penEl(0, kInfinite, false, -1, true), glueEl(wm, a.stretch, a.shrink, i, false))
				} else {
					els = append(els, boxEl(wm, i, false))
				}
			case a.fixed:
				els = append(els, spaceElements(eff, i, wm, a.stretch, a.shrink, true)...)
			default:
				els = append(els, spaceElements(eff, i, wm, a.stretch, a.shrink, false)...)
			}
		case aZW:
			els = append(els, spaceElements(eff, -1, 0, 0, 0, true)...)
		case aSoftHyphen:
			els = append(els, hyphenElements(eff, mpt(a.hyphenW), 0, i, false, mpt(a.hyphenW))...)
		case aBox:
			els = append(els, boxEl(mpt(a.w), i, false))
		case aOpen, aClose:
			if w := mpt(a.w); w != 0 {
				els = append(els, boxEl(w, i, false))
			} else {
				els = append(els, boxEl(0, i, true))
			}
		case aAnchor:
			els = append(els, boxEl(0, i, true))
		case aLeader:
			ld := a.leader
			opt := mpt(ld.optW)
			els = append(els, boxEl(0, -1, true), penEl(0, kInfinite, false, -1, true))
			if eff == "justify" || eff == "" {
				els = append(els, glueEl(opt, mpt(ld.maxW)-opt, opt-mpt(ld.minW), i, false))
			} else {
				els = append(els, glueEl(opt, 0, 0, i, false))
			}
			els = append(els, boxEl(0, -1, true))
		}
	}
	if len(els) <= ignoreStart {
		return nil
	}
	// end of paragraph (Paragraph.endSequence); lineFiller stretches to the line width
	switch {
	case textAlign == "center" && textAlignLast != "justify":
		els = append(els, glueEl(0, 3*defaultSpaceW, 0, -1, false), penEl(0, -kInfinite, false, -1, false))
	case textAlignLast != "justify":
		els = append(els, penEl(0, kInfinite, false, -1, false), glueEl(0, ipd, 0, -1, false), penEl(0, -kInfinite, false, -1, false))
	default:
		els = append(els, penEl(0, -kInfinite, false, -1, false))
	}

	// find the breaking points (LineLayoutManager.findOptimalBreakingPoints)
	br := newBreaker(ipd, 0)
	br.alignment = textAlign
	canWrap := !po.nowrap
	allowed := onlyForcedBreaks
	if canWrap {
		allowed = noFlaggedPenalties
	}
	n := br.findBreakingPoints(els, 1, false, allowed)
	if n == 0 {
		maxAdj := 5.0
		n = br.findBreakingPoints(els, maxAdj, false, allowed)
		if n == 0 {
			br.findBreakingPoints(els, 20, true, allowed)
		}
	}
	if br.result == nil {
		return nil
	}
	// walk the chosen nodes backwards collecting the lines
	var recs []lineRec
	total := br.result.line
	for nd := br.result; nd != nil && nd.line > 0; nd = nd.previous {
		first := 0
		if nd.line > 1 {
			first = nd.previous.position + 1
		}
		recs = append(recs, lineRec{first: first, last: nd.position, line: nd.line, difference: nd.difference,
			ratio: nd.adjustRatio, availShrink: nd.availableShrink, nodeLine: nd.line})
	}
	for i, j := 0, len(recs)-1; i < j; i, j = i+1, j-1 {
		recs[i], recs[j] = recs[j], recs[i]
	}
	var lines []*lineBox
	active := append([]*inlineDecor(nil), active0...)
	for _, rc := range recs {
		align := textAlign
		if rc.line >= total {
			align = textAlignLast
		}
		diff := rc.difference
		var startIndent int
		switch align {
		case "end":
			startIndent = diff
		case "center":
			startIndent = diff / 2
		}
		if rc.line == 1 && firstInBlock {
			startIndent += indentMpt
		}
		ratio := 0.0
		if align == "justify" || (diff < 0 && -diff <= rc.availShrink) {
			ratio = rc.ratio
		}
		act0 := append([]*inlineDecor(nil), active...)
		lb := lc.assembleLine(els, atoms, rc, ratio, float64(startIndent)/1000, po, &active)
		lb.act = act0
		lb.atomStart = off + len(atoms)
		for k := rc.first; k < rc.last && k < len(els); k++ {
			if els[k].isBox() && !els[k].aux && els[k].atom >= 0 {
				lb.atomStart = off + els[k].atom
				break
			}
		}
		lines = append(lines, lb)
	}
	for i, l := range lines {
		l.idx = i
		l.nlines = len(lines)
	}
	return lines
}

// assembleLine turns the elements of one line into a lineBox.
func (lc *lctx) assembleLine(els []kelem, atoms []atom, rc lineRec, ratio float64, x0 float64, po paraOpts, active *[]*inlineDecor) *lineBox {
	// skip leading glue/penalties up to the first box (discarded after a break)
	i := rc.first
	for i < rc.last && !els[i].isBox() {
		i++
	}
	var seq []atom
	seen := -1
	showHyphen := false
	var hyAtom *atom
	add := func(idx int) {
		if idx < 0 || idx == seen {
			return
		}
		seen = idx
		a := atoms[idx]
		switch a.kind {
		case aGlue, aLeader:
			// adjust by the ratio (TextLayoutManager.addAreas / LeaderLayoutManager)
			opt := mpt(a.w)
			stretch, shrink := a.stretch, a.shrink
			if a.kind == aLeader {
				opt = mpt(a.leader.optW)
				stretch, shrink = mpt(a.leader.maxW)-opt, opt-mpt(a.leader.minW)
			}
			adj := 0.0
			if ratio > 0 {
				adj = float64(stretch) * ratio
			} else if ratio < 0 {
				adj = float64(shrink) * ratio
			}
			a.w = float64(opt+int(adj)) / 1000
		}
		seq = append(seq, a)
	}
	for ; i < rc.last; i++ {
		e := &els[i]
		if e.aux && !e.isBox() {
			continue
		}
		add(e.atom)
	}
	if rc.last < len(els) {
		br := &els[rc.last]
		if br.isPenalty() && br.hyphen && br.atom >= 0 {
			showHyphen = true
			hyAtom = &atoms[br.atom]
		}
	}
	// drop trailing collapsible glue
	for len(seq) > 0 {
		l := seq[len(seq)-1]
		if (l.kind == aGlue && !l.fixed) || l.kind == aZW {
			seq = seq[:len(seq)-1]
			continue
		}
		break
	}
	_ = hyAtom
	// the last letter space of a word is only reserved when text follows it directly
	for k := range seq {
		a := &seq[k]
		if a.kind != aWord || a.breakAfter || mpt(a.style.ls) == 0 {
			continue
		}
		trailing := false
		for j := k + 1; j < len(seq); j++ {
			if c := seq[j].kind; c == aOpen || c == aClose || c == aAnchor {
				continue
			}
			trailing = seq[j].kind != aGlue
			break
		}
		if !trailing && len(a.runs) > 0 {
			ls := a.style.ls
			runs := append([]textRun(nil), a.runs...)
			last := &runs[len(runs)-1]
			last.glyphs = append([]pdf.Glyph(nil), last.glyphs...)
			if n := len(last.glyphs); n > 0 {
				last.glyphs[n-1].Extra -= ls
			}
			last.wm -= mpt(ls)
			last.width = float64(last.wm) / 1000
			a.runs = runs
			a.w = runsWidth(runs)
		}
	}
	return lc.buildLine(seq, po, x0, showHyphen, hyAtom, active)
}

func absInt(a int) int {
	if a < 0 {
		return -a
	}
	return a
}

var _ = math.Abs

func runsWidthMpt(rs []textRun) int { return mpt(runsWidth(rs)) }

// reflowLines re-breaks the paragraph from the start of line l0 with a width
// changed by delta; the new lines continue l0's numbering.
func (ps *paraSrc) reflowLines(l0 *lineBox, delta float64) []*lineBox {
	po := ps.po
	po.width += delta
	start := l0.atomStart
	if start > len(ps.atoms) {
		return nil
	}
	first := ps.first && l0.idx == 0
	lines := ps.lc.breakParagraph(ps.atoms[start:], po, first, l0.act, start)
	if len(lines) == 0 {
		return nil
	}
	src := &paraSrc{lc: ps.lc, atoms: ps.atoms, po: po, first: ps.first}
	for i, l := range lines {
		l.src = src
		l.paraID = l0.paraID
		l.idx = l0.idx + i
		l.nlines = l0.idx + len(lines)
	}
	return lines
}
