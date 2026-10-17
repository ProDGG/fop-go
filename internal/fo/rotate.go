package fo

import "math"

// rotItem draws a block-container whose content is rotated by
// reference-orientation (90, 180 or 270 degrees counter-clockwise).
type rotItem struct {
	content  *block
	w, h     float64 // size of the container in the parent frame
	rot      int
	spB, spA []spc
	kNext    bool
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
	X, Yt, w, h := ox, y, r.w, r.h
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
// widthOverride, when non-nil, is the container width (absolute positioning).
func (lc *lctx) layoutRotated(n *Node, rot int, widthOverride *float64) vitem {
	cw := lc.contentW()
	b := newBlock()
	lc.boxProps(n, b, cw)
	var w float64
	if widthOverride != nil {
		w = *widthOverride
	} else if v, ok := lc.length(n, "width", cw); ok {
		w = v
	} else if v, ok := lc.length(n, "inline-progression-dimension", cw); ok {
		w = v
	} else {
		w = cw
	}
	h, hasH := lc.length(n, "height", lc.refH)
	if !hasH {
		h, hasH = lc.length(n, "block-progression-dimension", lc.refH)
	}
	// inline progression of the content runs along the container's width for
	// 0/180 degrees and along its height for 90/270
	ipd := w
	if rot%180 != 0 {
		if hasH {
			ipd = h
		} else {
			ipd = w
			h = w
		}
	}
	inner := ipd - b.p[3] - b.b[3].w - b.p[1] - b.b[1].w
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
	item := &rotItem{content: b, rot: rot, spB: lc.spaces(n, "space-before"), spA: lc.spaces(n, "space-after"), kNext: isKeep(n.Props["keep-with-next"])}
	ch := b.height()
	if rot%180 == 0 {
		item.w = w
		item.h = ch
		if hasH {
			item.h = math.Max(h, ch)
			b.minH = item.h
		}
	} else {
		item.w = math.Max(w, ch)
		item.h = h
		if widthOverride == nil && w > cw {
			item.w = w
		}
	}
	return item
}
