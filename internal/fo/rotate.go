package fo

// rotItem draws a block-container whose content is rotated by
// reference-orientation (90, 180 or 270 degrees counter-clockwise).
type rotItem struct {
	content  *block
	w, h     float64 // size of the container in the parent frame
	rot      int
	spB, spA []spc
	kNext    bool
	sx, sy   float64 // offset of the border box from the content origin
}

func (r *rotItem) height() float64                    { return r.h }
func (r *rotItem) before() float64                    { return resolveSpaces(r.spB) }
func (r *rotItem) after() float64                     { return resolveSpaces(r.spA) }
func (r *rotItem) beforeSp() []spc                    { return r.spB }
func (r *rotItem) afterSp() []spc                     { return r.spA }
func (r *rotItem) keepNext() bool                     { return r.kNext }
func (r *rotItem) keepPrev() bool                     { return false }
func (r *rotItem) breakBefore() brk                   { return brkNone }
func (r *rotItem) breakAfter() brk                    { return brkNone }
func (r *rotItem) split(float64, bool) (vitem, vitem) { return nil, nil }
func (r *rotItem) forcedSplit() (vitem, vitem, brk)   { return nil, nil, brkNone }

func (r *rotItem) render(rc *renderCtx, ox, y float64) {
	X, Yt, w, h := ox+r.sx, y+r.sy, r.w, r.h
	H := rc.pageH
	rc.pg.Save()
	switch r.rot {
	case 90:
		rc.pg.Transform(0, 1, -1, 0, X+H, H-Yt-h)
	case 180:
		rc.pg.Transform(-1, 0, 0, -1, X+w, 2*H-Yt-h)
	case 270:
		rc.pg.Transform(0, -1, 1, 0, X+w-H, H-Yt)
	}
	r.content.render(rc, 0, 0)
	rc.pg.Restore()
}

// layoutRotated lays out a block-container with a non-zero reference-orientation.
// As in FOP, width and height are the content's inline- and block-progression
// dimensions in the rotated frame; on the page a 90/270 degree container is
// bpd wide and ipd tall. absW, when non-nil, is the border-box width of an
// absolutely positioned container.
func (lc *lctx) layoutRotated(n *Node, rot int, absW *float64) vitem {
	cw := lc.contentW()
	b := newBlock()
	lc.boxProps(n, b, cw)
	decoI := b.p[3] + b.b[3].w + b.p[1] + b.b[1].w
	decoB := b.topDeco() + b.botDeco()
	var ipd float64 // border-box size along the rotated inline direction
	if absW != nil {
		ipd = *absW
	} else if v, ok := lc.length(n, "width", cw); ok {
		ipd = v + decoI
	} else if v, ok := lc.length(n, "inline-progression-dimension", cw); ok {
		ipd = v + decoI
	} else {
		ipd = cw
	}
	h, hasH := lc.length(n, "height", lc.refH)
	if !hasH {
		h, hasH = lc.length(n, "block-progression-dimension", lc.refH)
	}
	inner := ipd - decoI
	if inner < 1 {
		inner = 1
	}
	b.width = ipd
	b.left = 0
	lc2 := &lctx{e: lc.e, refW: inner, refH: lc.refH, pageNum: lc.pageNum, pageFmt: lc.pageFmt, markers: lc.markers, carry: lc.carry}
	bb := lc2.newBlockBuilder(n)
	bb.process(n.Children)
	bb.flush()
	b.children = bb.items
	b.fillGaps()
	b.spB, b.spA = nil, nil
	bpd := b.height()
	if hasH {
		bpd = h + decoB
		b.minH = bpd
		b.fixedH = true
	}
	// FOP places the content rectangle at the container's origin; border and padding
	// stick out to the page-left (and, when absolutely positioned, page-top) side
	deco := func(i int) float64 { return b.b[i].w + b.p[i] }
	var left, top int
	switch rot {
	case 90:
		left, top = 0, 1
	case 180:
		left, top = 1, 2
	default:
		left, top = 2, 3
	}
	item := &rotItem{content: b, sx: -deco(left), rot: rot, spB: lc.spaces(n, "space-before"), spA: lc.spaces(n, "space-after"), kNext: isKeep(n.Props["keep-with-next"])}
	if absW != nil {
		item.sy = -deco(top)
	}
	if rot%180 == 0 {
		item.w, item.h = ipd, bpd
	} else {
		item.w, item.h = bpd, ipd
	}
	return item
}
