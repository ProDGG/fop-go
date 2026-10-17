package fo

import "math"

type brk int

const (
	brkNone brk = iota
	brkColumn
	brkPage
	brkOdd
	brkEven
)

func maxBrk(a, b brk) brk {
	if a > b {
		return a
	}
	return b
}

// vitem is a vertically stacked layout object (line, block, table row, ...).
type vitem interface {
	height() float64
	before() float64
	after() float64
	keepNext() bool
	keepPrev() bool
	breakBefore() brk
	breakAfter() brk
	// split cuts the item so that head fits into avail. head is nil if no
	// acceptable cut exists (unless force is set, which accepts any cut).
	split(avail float64, force bool) (head, tail vitem)
	// forcedSplit splits at the first forced break (break-before/after) inside the item.
	forcedSplit() (head, tail vitem, kind brk)
	render(r *renderCtx, x, y float64)
}

type border struct {
	w     float64
	style string
	c     Color
}

type overlay struct {
	item   vitem
	dx, dy float64
}

type markerDef struct {
	class string
	node  *Node
}

// block is a vertical container with optional border, padding and background.
type block struct {
	left, width float64
	b           [4]border // top, right, bottom, left
	p           [4]float64
	bg          Color
	bgImg       *imageData
	bgRepeat    string
	collapse    bool

	children []vitem
	gaps     []float64
	endGap   float64

	hdr, ftr  []vitem
	hdrGaps   []float64
	repeatHdr bool
	repeatFtr bool

	spBefore, spAfter       float64
	kNext, kPrev, kTogether bool
	bBefore, bAfter         brk
	orphans, widows         int
	minH                    float64
	fixedH                  bool // minH is an exact height (content is clipped)
	displayAlign            string
	overlays                []overlay
	ids                     []string
	markers                 []*markerDef

	first, last bool
	forceH      float64
}

func newBlock() *block { return &block{first: true, last: true, orphans: 2, widows: 2} }

func (b *block) topDeco() float64 {
	if b.first {
		return b.b[0].w + b.p[0]
	}
	return 0
}

func (b *block) botDeco() float64 {
	if b.last {
		return b.b[2].w + b.p[2]
	}
	return 0
}

func (b *block) showHdr() bool { return len(b.hdr) > 0 && (b.first || b.repeatHdr) }
func (b *block) showFtr() bool { return len(b.ftr) > 0 && (b.last || b.repeatFtr) }

func stackHeight(items []vitem, gaps []float64) float64 {
	h := 0.0
	for i, it := range items {
		if i < len(gaps) {
			h += gaps[i]
		}
		h += it.height()
	}
	return h
}

func (b *block) contentHeight() float64 {
	h := 0.0
	if b.showHdr() {
		h += stackHeight(b.hdr, b.hdrGaps)
	}
	h += stackHeight(b.children, b.gaps)
	if b.last && len(b.children) > 0 {
		h += b.endGap
	}
	if b.showFtr() {
		h += stackHeight(b.ftr, nil)
	}
	return h
}

func (b *block) height() float64 {
	h := b.topDeco() + b.contentHeight() + b.botDeco()
	if b.minH > h || (b.fixedH && b.last) {
		h = b.minH
	}
	return h
}

func (b *block) before() float64 { return b.spBefore }
func (b *block) after() float64  { return b.spAfter }
func (b *block) keepNext() bool  { return b.kNext }
func (b *block) keepPrev() bool  { return b.kPrev }

func (b *block) breakBefore() brk {
	k := b.bBefore
	if len(b.children) > 0 && b.first {
		k = maxBrk(k, b.children[0].breakBefore())
	}
	return k
}

func (b *block) breakAfter() brk {
	k := b.bAfter
	if n := len(b.children); n > 0 && b.last {
		k = maxBrk(k, b.children[n-1].breakAfter())
	}
	return k
}

// validBreak reports whether the flow may be cut between children[i-1] and children[i].
func (b *block) validBreak(i int) bool {
	if i <= 0 || i >= len(b.children) {
		return true
	}
	prev, cur := b.children[i-1], b.children[i]
	if prev.keepNext() || cur.keepPrev() {
		return false
	}
	if pl, ok := prev.(*lineBox); ok {
		if cl, ok := cur.(*lineBox); ok && pl.paraID == cl.paraID {
			before := cl.idx
			after := cl.nlines - cl.idx
			orphans, widows := b.orphans, b.widows
			if cl.nlines < orphans+widows {
				return false
			}
			if before < orphans || after < widows {
				return false
			}
		}
	}
	return true
}

func (b *block) split(avail float64, force bool) (vitem, vitem) {
	if len(b.children) == 0 {
		return nil, nil
	}
	if b.kTogether && !force {
		return nil, nil
	}
	const eps = 0.01
	used := b.topDeco()
	if b.showHdr() {
		used += stackHeight(b.hdr, b.hdrGaps)
	}
	limit := avail
	if b.repeatFtr && len(b.ftr) > 0 {
		limit -= stackHeight(b.ftr, nil)
	}
	y := used
	n := 0 // number of whole children that fit
	var partHead, partTail vitem
	var partGap float64
	for i, c := range b.children {
		g := b.gaps[i]
		if y+g+c.height() <= limit+eps {
			y += g + c.height()
			n++
			continue
		}
		room := limit - y - g
		if room > 0 {
			h, t := c.split(room, force && n == 0)
			if h != nil && t != nil {
				partHead, partTail, partGap = h, t, g
			}
		}
		break
	}
	if n == len(b.children) && partHead == nil {
		return nil, nil // everything fits (e.g. only the fixed height does not)
	}
	if partHead == nil {
		// cut between children; find a valid position
		k := n
		for k > 0 && !b.validBreak(k) {
			k--
		}
		if k == 0 {
			if !force {
				return nil, nil
			}
			k = n
			if k == 0 {
				k = 1
			}
		}
		n = k
	}
	head := *b
	tail := *b
	head.children = append([]vitem(nil), b.children[:n]...)
	head.gaps = append([]float64(nil), b.gaps[:n]...)
	rest := b.children[n:]
	restGaps := b.gaps[n:]
	if partHead != nil {
		head.children = append(head.children, partHead)
		head.gaps = append(head.gaps, partGap)
		rest = rest[1:]
		restGaps = restGaps[1:]
		tail.children = append([]vitem{partTail}, rest...)
		tail.gaps = append([]float64{0}, restGaps...)
	} else {
		tail.children = append([]vitem(nil), rest...)
		tail.gaps = append([]float64{0}, restGaps[1:]...)
	}
	head.last, head.endGap, head.minH, head.fixedH = false, 0, 0, false
	head.bAfter = brkNone
	head.spAfter = 0
	head.forceH = 0
	tail.first, tail.ids, tail.markers, tail.overlays = false, nil, nil, nil
	tail.bBefore, tail.spBefore, tail.kPrev = brkNone, 0, false
	tail.forceH = 0
	if tail.minH > 0 {
		tail.minH = math.Max(0, tail.minH-(head.height()-head.botDeco()))
	}
	if len(head.children) == 0 {
		return nil, nil
	}
	return &head, &tail
}

func (b *block) forcedSplit() (vitem, vitem, brk) {
	for i, c := range b.children {
		if i > 0 {
			k := maxBrk(b.children[i-1].breakAfter(), c.breakBefore())
			if k != brkNone {
				head := *b
				tail := *b
				head.children = append([]vitem(nil), b.children[:i]...)
				head.gaps = append([]float64(nil), b.gaps[:i]...)
				tail.children = append([]vitem(nil), b.children[i:]...)
				tail.gaps = append([]float64{0}, b.gaps[i+1:]...)
				finishForced(&head, &tail)
				return &head, &tail, k
			}
		}
		if h, t, k := c.forcedSplit(); h != nil {
			head := *b
			tail := *b
			head.children = append(append([]vitem(nil), b.children[:i]...), h)
			head.gaps = append([]float64(nil), b.gaps[:i+1]...)
			tail.children = append([]vitem{t}, b.children[i+1:]...)
			tail.gaps = append([]float64{0}, b.gaps[i+1:]...)
			finishForced(&head, &tail)
			return &head, &tail, k
		}
	}
	return nil, nil, brkNone
}

func finishForced(head, tail *block) {
	head.last, head.endGap, head.minH, head.fixedH, head.bAfter, head.spAfter = false, 0, 0, false, brkNone, 0
	tail.first, tail.ids, tail.markers, tail.overlays = false, nil, nil, nil
	tail.bBefore, tail.spBefore, tail.kPrev = brkNone, 0, false
}

func (b *block) render(r *renderCtx, ox, y float64) {
	x := ox + b.left
	nat := b.height()
	h := nat
	if b.forceH > h {
		h = b.forceH
	}
	for _, id := range b.ids {
		r.registerID(id, y)
	}
	if b.first && len(b.markers) > 0 {
		r.addMarkers(b.markers)
	}
	r.drawBlockDecor(b, x, y, h)
	cx := x + b.b[3].w + b.p[3]
	cy := y + b.topDeco()
	if free := h - nat; free > 0 {
		switch b.displayAlign {
		case "center":
			cy += free / 2
		case "after":
			cy += free
		}
	}
	if b.fixedH {
		r.clipStart(x, y, b.width, h)
		defer r.clipEnd()
	}
	if b.showHdr() {
		hy := cy
		for i, it := range b.hdr {
			if i < len(b.hdrGaps) {
				hy += b.hdrGaps[i]
			}
			it.render(r, cx, hy)
			hy += it.height()
		}
		cy = hy
	}
	if b.first {
		for _, o := range b.overlays {
			o.item.render(r, cx, cy+o.dy)
		}
	}
	for i, c := range b.children {
		cy += b.gaps[i]
		c.render(r, cx, cy)
		cy += c.height()
	}
	if b.last && len(b.children) > 0 {
		cy += b.endGap
	}
	if b.showFtr() {
		for _, it := range b.ftr {
			it.render(r, cx, cy)
			cy += it.height()
		}
	}
}

// fillGaps computes the gaps between children and hoists the first/last
// child's spacing into the block when no border or padding separates them.
func (b *block) fillGaps() {
	n := len(b.children)
	b.gaps = make([]float64, n)
	if n == 0 {
		return
	}
	hasTop := b.b[0].w+b.p[0] > 0
	hasBot := b.b[2].w+b.p[2] > 0
	first := b.children[0].before()
	if hasTop || len(b.hdr) > 0 {
		b.gaps[0] = first
	} else {
		b.spBefore = math.Max(b.spBefore, first)
	}
	for i := 1; i < n; i++ {
		b.gaps[i] = math.Max(b.children[i-1].after(), b.children[i].before())
	}
	last := b.children[n-1].after()
	if hasBot || len(b.ftr) > 0 {
		b.endGap = last
	} else {
		b.spAfter = math.Max(b.spAfter, last)
	}
}
