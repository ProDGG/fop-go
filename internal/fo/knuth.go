package fo

import "math"

// This file is a port of Apache FOP's org.apache.fop.layoutmgr.BreakingAlgorithm
// (Knuth/Plass total-fit line breaking) and the element model it works on.
// All lengths are integer millipoints, as in FOP.

var debugKnuth = false

const (
	kBox = iota
	kGlue
	kPenalty
)

const (
	kInfinite     = 1000 // KnuthElement.INFINITE
	kFlaggedPen   = 50   // KnuthPenalty.FLAGGED_PENALTY
	infiniteRatio = 1000.0
	infDemerits   = math.MaxFloat64
	defaultSpaceW = 3336 // LineLayoutManager.DEFAULT_SPACE_WIDTH
	softHyphenPen = 1    // TextLayoutManager.SOFT_HYPHEN_PENALTY
)

// kelem is a Knuth box, glue or penalty.
type kelem struct {
	kind    uint8
	w       int
	stretch int
	shrink  int
	pen     int
	flagged bool
	aux     bool
	atom    int // index of the originating atom (-1 if none)
	hyphen  bool
}

func (e *kelem) isBox() bool     { return e.kind == kBox }
func (e *kelem) isGlue() bool    { return e.kind == kGlue }
func (e *kelem) isPenalty() bool { return e.kind == kPenalty }
func (e *kelem) forced() bool    { return e.kind == kPenalty && e.pen == -kInfinite }

func boxEl(w int, atom int, aux bool) kelem {
	return kelem{kind: kBox, w: w, atom: atom, aux: aux}
}
func glueEl(w, st, sh int, atom int, aux bool) kelem {
	return kelem{kind: kGlue, w: w, stretch: st, shrink: sh, atom: atom, aux: aux}
}
func penEl(w, pen int, flagged bool, atom int, aux bool) kelem {
	return kelem{kind: kPenalty, w: w, pen: pen, flagged: flagged, atom: atom, aux: aux}
}

const (
	fitVeryTight = 0
	fitTight     = 1
	fitLoose     = 2
	fitVeryLoose = 3
)

func computeFitness(r float64) int {
	switch {
	case r < -0.5:
		return fitVeryTight
	case r <= 0.5:
		return fitTight
	case r <= 1.0:
		return fitLoose
	}
	return fitVeryLoose
}

type kNode struct {
	position, line, fitness               int
	totalWidth, totalStretch, totalShrink int
	adjustRatio                           float64
	availableShrink, availableStretch     int
	difference                            int
	totalDemerits                         float64
	previous, next                        *kNode
}

type bestRecords struct {
	demerits  [4]float64
	node      [4]*kNode
	adjust    [4]float64
	shrink    [4]int
	stretch   [4]int
	diff      [4]int
	bestIndex int
}

func (b *bestRecords) reset() {
	for i := range b.demerits {
		b.demerits[i] = infDemerits
	}
	b.bestIndex = -1
}

func (b *bestRecords) add(dem float64, n *kNode, adj float64, sh, st, diff, fit int) {
	b.demerits[fit] = dem
	b.node[fit] = n
	b.adjust[fit] = adj
	b.shrink[fit] = sh
	b.stretch[fit] = st
	b.diff[fit] = diff
	if b.bestIndex == -1 || dem < b.demerits[b.bestIndex] {
		b.bestIndex = fit
	}
}

func (b *bestRecords) has() bool { return b.bestIndex != -1 }
func (b *bestRecords) minDemerits() float64 {
	if b.bestIndex != -1 {
		return b.demerits[b.bestIndex]
	}
	return infDemerits
}

// breaker is the Knuth breaking algorithm over a kelem sequence.
type breaker struct {
	par       []kelem
	threshold float64
	force     bool
	lineWidth int

	repeatedFlaggedDemerit     int
	incompatibleFitnessDemerit int
	maxFlaggedPenaltiesCount   int

	totalWidth, totalStretch, totalShrink int
	activeLines                           []*kNode // head, tail pairs
	activeNodeCount                       int
	startLine, endLine                    int
	best                                  bestRecords
	lastTooShort, lastTooLong             *kNode
	lastDeactivated                       *kNode
	lastRecovered                         *kNode
	considerTooShort                      bool
	partOverflowRecovery                  bool
	alignment                             string
	result                                *kNode
	indentFirstPart                       bool
}

func newBreaker(width int, maxFlag int) *breaker {
	b := &breaker{lineWidth: width, repeatedFlaggedDemerit: kFlaggedPen, incompatibleFitnessDemerit: kFlaggedPen,
		maxFlaggedPenaltiesCount: maxFlag}
	b.best.reset()
	return b
}

const (
	allBreaks          = 0
	noFlaggedPenalties = 1
	onlyForcedBreaks   = 2
)

func (b *breaker) newNode(position, line, fitness, tw, ts, tsh int, adj float64, ash, ast, diff int, dem float64, prev *kNode) *kNode {
	return &kNode{position: position, line: line, fitness: fitness, totalWidth: tw, totalStretch: ts, totalShrink: tsh,
		adjustRatio: adj, availableShrink: ash, availableStretch: ast, difference: diff, totalDemerits: dem, previous: prev}
}

func (b *breaker) addNode(line int, n *kNode) {
	head := line * 2
	for head >= len(b.activeLines) {
		b.activeLines = append(b.activeLines, nil, nil)
	}
	n.next = nil
	if b.activeLines[head+1] != nil {
		b.activeLines[head+1].next = n
	} else {
		b.activeLines[head] = n
		b.endLine = line + 1
	}
	b.activeLines[head+1] = n
	b.activeNodeCount++
}

func (b *breaker) getNode(line int) *kNode {
	if line*2 >= len(b.activeLines) {
		return nil
	}
	return b.activeLines[line*2]
}

func (b *breaker) removeNode(line int, n *kNode) {
	head := line * 2
	cur := b.getNode(line)
	if cur != n {
		var prev *kNode
		for cur != n {
			prev = cur
			cur = cur.next
		}
		prev.next = cur.next
		if prev.next == nil {
			b.activeLines[head+1] = prev
		}
	} else {
		b.activeLines[head] = n.next
		if n.next == nil {
			b.activeLines[head+1] = nil
		}
		for b.startLine < b.endLine && b.getNode(b.startLine) == nil {
			b.startLine++
		}
	}
	b.activeNodeCount--
}

func (b *breaker) compareNodes(n1, n2 *kNode) *kNode {
	if n1 == nil || n2.position > n1.position {
		return n2
	}
	if n2.position == n1.position && n2.totalDemerits < n1.totalDemerits {
		return n2
	}
	return n1
}

// findBreakingPoints runs the algorithm; it returns the number of lines (0 if no
// set of breaks was found and force is false).
func (b *breaker) findBreakingPoints(par []kelem, threshold float64, force bool, allowed int) int {
	b.par = par
	b.threshold = threshold
	b.force = force
	b.totalWidth, b.totalStretch, b.totalShrink = 0, 0, 0
	b.lastTooShort, b.lastTooLong, b.lastDeactivated, b.lastRecovered = nil, nil, nil, nil
	b.startLine, b.endLine = 0, 0
	b.activeLines = make([]*kNode, 20)
	b.activeNodeCount = 0
	b.best.reset()

	previousIsBox := false
	previousPosition := 0
	if b.alignment != "center" {
		first := len(par)
		for i := 0; i < len(par); i++ {
			if par[i].isBox() {
				first = i
				break
			}
		}
		if first >= len(par) {
			previousPosition = 0
		} else {
			previousPosition = first - 1
		}
	}
	if previousPosition < 0 {
		previousPosition = 0
	}
	b.addNode(0, b.newNode(previousPosition, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, nil))
	lastForced := b.getNode(0)

	for idx := 0; idx < len(b.par); idx++ {
		previousIsBox = b.handleElementAt(idx, previousIsBox, allowed)
		if b.activeNodeCount == 0 {
			if !b.force {
				return 0
			}
			if b.lastDeactivated != nil && b.lastDeactivated != lastForced {
				b.replaceLastDeactivated()
			}
			if b.lastTooShort == nil || lastForced.position == b.lastTooShort.position {
				lastForced = b.recoverFromOverflow()
			} else {
				lastForced = b.lastTooShort
				b.lastRecovered = nil
			}
			idx = b.restartFrom(lastForced, idx)
		}
	}
	// finish + filterActiveNodes (non-justified page alignment: keep only the best node)
	var bestActive *kNode
	for i := b.startLine; i < b.endLine; i++ {
		for n := b.getNode(i); n != nil; n = n.next {
			bestActive = b.compareNodes(bestActive, n)
			if n != bestActive {
				b.removeNode(i, n)
			}
		}
	}
	if bestActive == nil {
		return 0
	}
	b.result = bestActive
	return bestActive.line
}

func (b *breaker) handleElementAt(pos int, previousIsBox bool, allowed int) bool {
	el := &b.par[pos]
	switch el.kind {
	case kBox:
		b.totalWidth += el.w
	case kGlue:
		if previousIsBox && allowed != onlyForcedBreaks {
			b.considerLegalBreak(el, pos)
		}
		b.totalWidth += el.w
		b.totalStretch += el.stretch
		b.totalShrink += el.shrink
	case kPenalty:
		if el.pen < kInfinite && (allowed != noFlaggedPenalties || !el.flagged) && (allowed != onlyForcedBreaks || el.forced()) {
			b.considerLegalBreak(el, pos)
		}
	}
	return el.isBox()
}

func (b *breaker) replaceLastDeactivated() {
	if b.lastDeactivated.adjustRatio > 0 {
		b.lastTooShort = b.lastDeactivated
	} else {
		b.lastTooLong = b.lastDeactivated
	}
}

func (b *breaker) recoverFromOverflow() *kNode {
	// part overflow recovery is disabled for line breaking (as in FOP)
	return b.lastTooLong
}

func (b *breaker) restartFrom(n *kNode, currentIndex int) int {
	n.totalDemerits = 0
	b.addNode(n.line, n)
	b.startLine = n.line
	b.endLine = b.startLine + 1
	b.totalWidth, b.totalStretch, b.totalShrink = n.totalWidth, n.totalStretch, n.totalShrink
	b.lastTooShort, b.lastTooLong = nil, nil
	ri := n.position
	for ri+1 < len(b.par) && !b.par[ri+1].isBox() {
		ri++
	}
	return ri
}

func (b *breaker) considerLegalBreak(el *kelem, idx int) {
	b.lastDeactivated = nil
	b.lastTooLong = nil
	for line := b.startLine; line < b.endLine; line++ {
		for n := b.getNode(line); n != nil; {
			next := n.next
			if n.position == idx {
				n = next
				continue
			}
			diff := b.computeDifference(n, el)
			if el.isPenalty() && el.pen >= kInfinite {
				break // elementCanEndLine
			}
			r := b.computeAdjustmentRatio(n, diff)
			availShrink := b.totalShrink - n.totalShrink
			availStretch := b.totalStretch - n.totalStretch
			if r < -1 || el.forced() {
				b.deactivateNode(n, line)
			}
			fit := computeFitness(r)
			dem := b.computeDemerits(n, el, fit, r)
			if debugKnuth {
				println("break at", idx, "from", n.position, "line", line+1, "diff", diff, "r*1000", int(r*1000), "dem", int(dem), "thr", int(b.threshold))
			}
			if r >= -1 && r <= b.threshold {
				b.activateNode(n, diff, r, dem, fit, availShrink, availStretch)
			}
			if b.force && (r <= -1 || r > b.threshold) {
				b.forceNode(n, line, idx, diff, r, dem, fit, availShrink, availStretch)
			}
			n = next
		}
		b.addBreaks(line, idx)
	}
}

func (b *breaker) forceNode(n *kNode, line, idx, diff int, r, dem float64, fit, ash, ast int) {
	nw, ns, nsh := b.totalWidth, b.totalStretch, b.totalShrink
	for i := idx; i < len(b.par); i++ {
		t := &b.par[i]
		if t.isBox() {
			break
		} else if t.isGlue() {
			nw += t.w
			ns += t.stretch
			nsh += t.shrink
		} else if t.forced() && i != idx {
			break
		}
	}
	if r <= -1 {
		if b.lastTooLong == nil || dem < b.lastTooLong.totalDemerits {
			b.lastTooLong = b.newNode(idx, line+1, fit, nw, ns, nsh, r, ash, ast, diff, dem, n)
		}
	} else {
		if b.lastTooShort == nil || dem <= b.lastTooShort.totalDemerits {
			if b.considerTooShort {
				b.best.add(dem, n, r, ash, ast, diff, fit)
			}
			b.lastTooShort = b.newNode(idx, line+1, fit, nw, ns, nsh, r, ash, ast, diff, dem, n)
		}
	}
}

func (b *breaker) activateNode(n *kNode, diff int, r, dem float64, fit, ash, ast int) {
	if dem < b.best.demerits[fit] {
		b.best.add(dem, n, r, ash, ast, diff, fit)
		b.lastTooShort = nil
	}
}

func (b *breaker) deactivateNode(n *kNode, line int) {
	b.removeNode(line, n)
	b.lastDeactivated = b.compareNodes(b.lastDeactivated, n)
}

func (b *breaker) addBreaks(line, idx int) {
	if !b.best.has() {
		return
	}
	nw, ns, nsh := b.totalWidth, b.totalStretch, b.totalShrink
	for i := idx; i < len(b.par); i++ {
		t := &b.par[i]
		if t.isBox() {
			break
		} else if t.isGlue() {
			nw += t.w
			ns += t.stretch
			nsh += t.shrink
		} else if t.forced() && i != idx {
			break
		}
	}
	minDem := b.best.minDemerits() + float64(b.incompatibleFitnessDemerit)
	for i := 0; i <= 3; i++ {
		if b.best.demerits[i] != infDemerits && b.best.demerits[i] <= minDem {
			nn := b.newNode(idx, line+1, i, nw, ns, nsh, b.best.adjust[i], b.best.shrink[i], b.best.stretch[i],
				b.best.diff[i], b.best.demerits[i], b.best.node[i])
			b.addNode(line+1, nn)
		}
	}
	b.best.reset()
}

func (b *breaker) computeDifference(n *kNode, el *kelem) int {
	actual := b.totalWidth - n.totalWidth
	if el.isPenalty() {
		actual += el.w
	}
	return b.lineWidth - actual
}

func (b *breaker) computeAdjustmentRatio(n *kNode, diff int) float64 {
	if diff > 0 {
		if m := b.totalStretch - n.totalStretch; m > 0 {
			return float64(diff) / float64(m)
		}
		return infiniteRatio
	} else if diff < 0 {
		if m := b.totalShrink - n.totalShrink; m > 0 {
			return float64(diff) / float64(m)
		}
		return -infiniteRatio
	}
	return 0
}

func (b *breaker) computeDemerits(n *kNode, el *kelem, fit int, r float64) float64 {
	f := math.Abs(r)
	f = 1 + 100*f*f*f
	var dem float64
	if el.isPenalty() {
		p := float64(el.pen)
		switch {
		case p >= 0:
			f += p
			dem = f * f
		case !el.forced():
			dem = f*f - p*p
		default:
			dem = f * f
		}
	} else {
		dem = f * f
	}
	if el.isPenalty() && el.flagged && b.par[n.position].isPenalty() && b.par[n.position].flagged {
		dem += float64(b.repeatedFlaggedDemerit)
		count := 2
		for prev := n.previous; prev != nil && count <= b.maxFlaggedPenaltiesCount; prev = prev.previous {
			pe := &b.par[prev.position]
			if pe.isPenalty() && pe.flagged {
				count++
			} else {
				break
			}
		}
		if b.maxFlaggedPenaltiesCount >= 1 && count > b.maxFlaggedPenaltiesCount {
			dem += infDemerits
		}
	}
	d := fit - n.fitness
	if d < 0 {
		d = -d
	}
	if d > 1 {
		dem += float64(b.incompatibleFitnessDemerit)
	}
	dem += n.totalDemerits
	return dem
}
