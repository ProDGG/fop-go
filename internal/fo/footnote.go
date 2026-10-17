package fo

// fnCtx is the per-page-sequence-region state footnotes need while the page is filled.
type fnCtx struct {
	bodyW  float64 // width footnote bodies are laid out for
	have   bool    // the column being filled already holds footnotes (and so the separator)
	sepH   float64 // height of the xsl-footnote-separator content
	sepBlk *block
}

// footnoteRef is one fo:footnote-body attached to the line holding its reference.
type footnoteRef struct {
	blk *block
	ctx *fnCtx
}

// fnOf lists the footnotes carried by a vertical item.
func fnOf(v vitem) []*footnoteRef {
	switch t := v.(type) {
	case *lineBox:
		return t.fns
	case *block:
		return t.fnRefs()
	}
	return nil
}

func (b *block) fnRefs() []*footnoteRef {
	var out []*footnoteRef
	for _, c := range b.children {
		out = append(out, fnOf(c)...)
	}
	return out
}

// fnCost is the page space the footnotes need: their bodies (stacked with the
// usual space resolution) plus the separator when none is on the page yet.
func fnCost(refs []*footnoteRef, have bool) float64 {
	if len(refs) == 0 {
		return 0
	}
	h := 0.0
	for i, r := range refs {
		if i > 0 {
			h += resolveSpaces(concatSp(refs[i-1].blk.afterSp(), r.blk.beforeSp()))
		}
		h += r.blk.height()
	}
	if !have && !refs[0].ctx.have {
		h += refs[0].ctx.sepH
	}
	return h
}

// fnArea draws the footnote separator and bodies at the bottom of a column.
type fnArea struct {
	refs []*footnoteRef
	h    float64
}

func (a *fnArea) height() float64                    { return a.h }
func (a *fnArea) before() float64                    { return 0 }
func (a *fnArea) after() float64                     { return 0 }
func (a *fnArea) beforeSp() []spc                    { return nil }
func (a *fnArea) afterSp() []spc                     { return nil }
func (a *fnArea) keepNext() bool                     { return false }
func (a *fnArea) keepPrev() bool                     { return false }
func (a *fnArea) breakBefore() brk                   { return brkNone }
func (a *fnArea) breakAfter() brk                    { return brkNone }
func (a *fnArea) split(float64, bool) (vitem, vitem) { return nil, nil }
func (a *fnArea) forcedSplit() (vitem, vitem, brk)   { return nil, nil, brkNone }

func (a *fnArea) render(r *renderCtx, ox, y float64) {
	if len(a.refs) == 0 {
		return
	}
	// the bodies sit at the bottom of the area, the separator right above them
	bodies := 0.0
	for i, ref := range a.refs {
		if i > 0 {
			bodies += resolveSpaces(concatSp(a.refs[i-1].blk.afterSp(), ref.blk.beforeSp()))
		}
		bodies += ref.blk.height()
	}
	cy := y + a.h - bodies
	if sb := a.refs[0].ctx.sepBlk; sb != nil {
		sb.render(r, ox, cy-sb.height())
	}
	for i, ref := range a.refs {
		if i > 0 {
			cy += resolveSpaces(concatSp(a.refs[i-1].blk.afterSp(), ref.blk.beforeSp()))
		}
		ref.blk.render(r, ox, cy)
		cy += ref.blk.height()
	}
}

// makeFootnote lays out a fo:footnote-body for the current page width.
func (lc *lctx) makeFootnote(body *Node) *footnoteRef {
	if lc.fn == nil {
		return nil
	}
	w := lc.fn.bodyW
	lcf := &lctx{e: lc.e, refW: w, pageNum: lc.pageNum, pageFmt: lc.pageFmt, carry: lc.carry, fn: lc.fn}
	bb := lcf.newBlockBuilder(body)
	bb.process(body.Children)
	bb.flush()
	blk := newBlock()
	blk.children = bb.items
	blk.markers = nil
	blk.fillGaps()
	blk.width = w
	return &footnoteRef{blk: blk, ctx: lc.fn}
}
