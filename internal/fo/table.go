package fo

import (
	"math"
	"strconv"
	"strings"
)

type colDef struct {
	kind int // 0 auto, 1 fixed, 2 proportional, 3 percent
	val  float64
	w    float64
	node *Node
}

type cellInfo struct {
	node   *Node
	row    int
	col    int
	rs, cs int
	x, w   float64
	blk    *block
	b      [4]border // own borders (before collapse resolution)
}

type rowInfo struct {
	node    *Node
	secNode *Node
	section int // 0 header, 1 body, 2 footer
	cells   []*cellInfo
	h       float64
	minH    float64
	b       [4]border
}

type rowCell struct {
	x   float64
	blk *block
}

// rowBox is one table row; its cells are laid out side by side.
type rowBox struct {
	cells    []rowCell
	h        float64
	spBefore float64 // border-separation (kept at breaks)
	spAfter  float64
	pad      float64 // border-separation.block-progression-dimension (half above, half below the cells)
	kNext    bool
	kTogeth  bool
	noSplit  bool
	bBefore  brk
	bAfter   brk
	ids      []string
}

func (rb *rowBox) height() float64 { return rb.h }
func (rb *rowBox) before() float64 { return rb.spBefore }
func (rb *rowBox) after() float64  { return rb.spAfter }
func (rb *rowBox) beforeSp() []spc {
	if rb.spBefore == 0 {
		return nil
	}
	return []spc{{opt: rb.spBefore, retain: true}}
}
func (rb *rowBox) afterSp() []spc {
	if rb.spAfter == 0 {
		return nil
	}
	return []spc{{opt: rb.spAfter, retain: true}}
}
func (rb *rowBox) keepNext() bool   { return rb.kNext }
func (rb *rowBox) keepPrev() bool   { return false }
func (rb *rowBox) breakBefore() brk { return rb.bBefore }
func (rb *rowBox) breakAfter() brk  { return rb.bAfter }

func (rb *rowBox) forcedSplit() (vitem, vitem, brk) { return nil, nil, brkNone }

func (rb *rowBox) setHeights() {
	rb.h = 0
	for _, c := range rb.cells {
		if c.blk.height() > rb.h && c.blk.forceH == 0 {
			rb.h = c.blk.height()
		}
	}
	for _, c := range rb.cells {
		if c.blk.forceH == 0 || c.blk.forceH < rb.h {
			c.blk.forceH = rb.h
		}
	}
	rb.h += rb.pad
}

func (rb *rowBox) split(avail float64, force bool) (vitem, vitem) {
	if (rb.noSplit || rb.kTogeth) && !force {
		return nil, nil
	}
	avail -= rb.pad
	n := len(rb.cells)
	heads := make([]*block, n)
	tails := make([]*block, n)
	anyTail := false
	maxH := 0.0
	for i, c := range rb.cells {
		blk := *c.blk
		blk.forceH = 0
		if blk.height() <= avail+0.01 {
			heads[i] = &blk
		} else {
			h, t := blk.split(avail, force)
			if h == nil {
				if !force {
					return nil, nil
				}
				heads[i] = &blk
			} else {
				heads[i], tails[i] = h.(*block), t.(*block)
				anyTail = true
			}
		}
		maxH = math.Max(maxH, heads[i].height())
	}
	if !anyTail {
		return nil, nil
	}
	headRow := &rowBox{h: maxH + rb.pad, pad: rb.pad, spBefore: rb.spBefore, kNext: false, bBefore: rb.bBefore, ids: rb.ids}
	tailRow := &rowBox{pad: rb.pad, spAfter: rb.spAfter, kNext: rb.kNext, bAfter: rb.bAfter}
	for i, c := range rb.cells {
		heads[i].forceH = maxH
		headRow.cells = append(headRow.cells, rowCell{c.x, heads[i]})
		t := tails[i]
		if t == nil {
			e := *c.blk
			e.first, e.children, e.gaps, e.ids, e.markers, e.overlays = false, nil, nil, nil, nil, nil
			e.minH, e.fixedH, e.forceH, e.endGap = 0, false, 0, 0
			t = &e
		}
		tailRow.cells = append(tailRow.cells, rowCell{c.x, t})
	}
	tailRow.setHeights()
	return headRow, tailRow
}

func (rb *rowBox) render(r *renderCtx, ox, y float64) {
	for _, id := range rb.ids {
		r.registerID(id, y)
	}
	for _, c := range rb.cells {
		c.blk.render(r, ox+c.x, y+rb.pad/2)
	}
}

// ---------- border resolution (collapsed model) ----------

func borderRank(s string) int {
	switch s {
	case "double":
		return 6
	case "solid":
		return 5
	case "dashed":
		return 4
	case "dotted":
		return 3
	case "ridge", "outset", "groove", "inset":
		return 2
	}
	return 1
}

func pickBorder(a, b border) border {
	if a.style == "hidden" {
		return a
	}
	if b.style == "hidden" {
		return b
	}
	an := a.style == "none" || a.w <= 0
	bn := b.style == "none" || b.w <= 0
	switch {
	case an && bn:
		return border{style: "none"}
	case an:
		return b
	case bn:
		return a
	}
	if b.w > a.w {
		return b
	}
	if b.w == a.w && borderRank(b.style) > borderRank(a.style) {
		return b
	}
	return a
}

// ---------- table layout ----------

func (lc *lctx) layoutTable(n *Node) vitem {
	e := lc.e
	b := newBlock()
	cw := lc.contentW()
	lc.boxProps(n, b, cw)
	collapse := !strings.HasPrefix(n.Get("border-collapse"), "separate")
	var tblBorder [4]border
	if collapse {
		tblBorder = b.b
		b.b = [4]border{{style: "none"}, {style: "none"}, {style: "none"}, {style: "none"}}
		b.p = [4]float64{}
	}
	S, E := lc.indents(n, b)
	hsep, vsep := 0.0, 0.0
	if !collapse {
		if v := n.Get("border-separation"); v != "" {
			toks := strings.Fields(v)
			if r, ok := e.parseLength(n, toks[0], 0); ok {
				hsep, vsep = r, r
			}
			if len(toks) > 1 {
				if r, ok := e.parseLength(n, toks[1], 0); ok {
					vsep = r
				}
			}
		}
		if v, ok := lc.length(n, "border-separation.inline-progression-dimension", cw); ok {
			hsep = v
		}
		if v, ok := lc.length(n, "border-separation.block-progression-dimension", cw); ok {
			vsep = v
		}
	}
	deco := b.p[3] + b.b[3].w + b.p[1] + b.b[1].w
	availW := lc.refW - S - E // content width of the table box
	tw := availW
	if v, ok := lc.length(n, "width", cw); ok {
		tw = v
	} else if v, ok := lc.length(n, "inline-progression-dimension", cw); ok {
		tw = v
	}
	if v, ok := lc.length(n, "inline-progression-dimension.maximum", cw); ok && tw > v {
		tw = v
	}
	b.width = tw + deco
	b.fixedW = true

	// ---- columns ----
	var cols []*colDef
	for _, c := range n.FindAll("table-column") {
		rep := 1
		if v, err := strconv.Atoi(strings.TrimSpace(c.Props["number-columns-repeated"])); err == nil && v > 0 {
			rep = v
		}
		cd := &colDef{node: c}
		spec := strings.TrimSpace(c.Props["column-width"])
		switch {
		case spec == "" || spec == "auto":
		case strings.HasPrefix(spec, "proportional-column-width("):
			inner := strings.TrimSuffix(strings.TrimPrefix(spec, "proportional-column-width("), ")")
			f, err := strconv.ParseFloat(strings.TrimSpace(inner), 64)
			if err != nil {
				f = 1
			}
			cd.kind, cd.val = 2, f
		case strings.HasSuffix(spec, "%"):
			f, err := strconv.ParseFloat(strings.TrimSuffix(spec, "%"), 64)
			if err == nil {
				cd.kind, cd.val = 3, f
			}
		default:
			if r, ok := lc.e.parseLength(c, spec, tw); ok {
				cd.kind, cd.val = 1, r
			}
		}
		for i := 0; i < rep; i++ {
			cp := *cd
			cols = append(cols, &cp)
		}
	}

	// ---- rows and cells ----
	var rows []*rowInfo
	ncols := len(cols)
	addSection := func(sec *Node, section int) {
		var occ []int
		var cur *rowInfo
		startRow := func(rn *Node) {
			cur = &rowInfo{node: rn, secNode: sec, section: section}
			if rn != nil {
				for i, s := range []string{"top", "right", "bottom", "left"} {
					cur.b[i] = lc.borderOf(rn, s, tw)
				}
				if h, ok := lc.length(rn, "block-progression-dimension.minimum", cw); ok {
					cur.minH = h
				} else if h, ok := lc.length(rn, "height", cw); ok {
					cur.minH = h
				} else if h, ok := lc.length(rn, "block-progression-dimension", cw); ok {
					cur.minH = h
				}
			}
			rows = append(rows, cur)
		}
		addCells := func(cells []*Node) {
			col := 0
			for _, cn := range cells {
				cs, rs := 1, 1
				if v, err := strconv.Atoi(strings.TrimSpace(cn.Props["number-columns-spanned"])); err == nil && v > 0 {
					cs = v
				}
				if v, err := strconv.Atoi(strings.TrimSpace(cn.Props["number-rows-spanned"])); err == nil && v > 0 {
					rs = v
				}
				if v, err := strconv.Atoi(strings.TrimSpace(cn.Props["column-number"])); err == nil && v > 0 {
					col = v - 1
				}
				for col < len(occ) && occ[col] > 0 {
					col++
				}
				for len(occ) < col+cs {
					occ = append(occ, 0)
				}
				ci := &cellInfo{node: cn, row: len(rows) - 1, col: col, rs: rs, cs: cs}
				for k := 0; k < cs; k++ {
					occ[col+k] = rs
				}
				cur.cells = append(cur.cells, ci)
				col += cs
				if col > ncols {
					ncols = col
				}
			}
			for i := range occ {
				if occ[i] > 0 {
					occ[i]--
				}
			}
		}
		var pending []*Node
		flushPending := func() {
			if len(pending) > 0 {
				startRow(nil)
				addCells(pending)
				pending = nil
			}
		}
		for _, c := range sec.Children {
			switch c.Name {
			case "table-row":
				flushPending()
				startRow(c)
				var cells []*Node
				for _, cc := range c.Children {
					if cc.Name == "table-cell" {
						cells = append(cells, cc)
					}
				}
				addCells(cells)
			case "table-cell":
				if c.Props["starts-row"] == "true" && len(pending) > 0 {
					flushPending()
				}
				pending = append(pending, c)
				if c.Props["ends-row"] == "true" {
					flushPending()
				}
			}
		}
		flushPending()
	}
	for _, h := range n.FindAll("table-header") {
		addSection(h, 0)
	}
	for _, bd := range n.FindAll("table-body") {
		addSection(bd, 1)
	}
	for _, f := range n.FindAll("table-footer") {
		addSection(f, 2)
	}
	// rows must be ordered header, body, footer (the XML order may differ)
	ordered := make([]*rowInfo, 0, len(rows))
	for sec := 0; sec < 3; sec++ {
		for _, r := range rows {
			if r.section == sec {
				ordered = append(ordered, r)
			}
		}
	}
	rows = ordered
	for i, r := range rows {
		for _, c := range r.cells {
			c.row = i
		}
	}
	if ncols == 0 {
		return b
	}
	for len(cols) < ncols {
		cols = append(cols, &colDef{})
	}

	// ---- column widths ----
	avail := tw // FOP does not subtract border-separation from the column widths
	fixed, propSum := 0.0, 0.0
	autoN := 0
	for _, c := range cols {
		switch c.kind {
		case 1:
			c.w = c.val
			fixed += c.w
		case 3:
			c.w = tw * c.val / 100
			fixed += c.w
		case 2:
			propSum += c.val
		default:
			autoN++
		}
	}
	rem := avail - fixed
	if rem < 0 {
		rem = 0
	}
	switch {
	case propSum > 0:
		for _, c := range cols {
			if c.kind == 2 {
				c.w = rem * c.val / propSum
			}
		}
		if autoN > 0 {
			// auto columns share nothing when proportional columns consume the width
			for _, c := range cols {
				if c.kind == 0 {
					c.w = 0
				}
			}
		}
	case autoN > 0:
		share := rem / float64(autoN)
		for _, c := range cols {
			if c.kind == 0 {
				c.w = share
			}
		}
	}
	// grid
	cumX := make([]float64, ncols+1)
	x := 0.0
	for i, c := range cols {
		cumX[i] = x
		x += c.w
	}
	cumX[ncols] = x
	// assign cell geometry
	for _, r := range rows {
		for _, c := range r.cells {
			end := c.col + c.cs
			if end > ncols {
				end = ncols
			}
			c.x = cumX[c.col] + hsep/2
			c.w = cumX[end] - cumX[c.col] - hsep
		}
	}

	// ---- own borders ----
	for _, r := range rows {
		for _, c := range r.cells {
			for i, s := range []string{"top", "right", "bottom", "left"} {
				c.b[i] = lc.borderOf(c.node, s, c.w)
			}
		}
	}
	// grid occupancy for neighbour lookup
	grid := make([][]*cellInfo, len(rows))
	for i := range grid {
		grid[i] = make([]*cellInfo, ncols)
	}
	for _, r := range rows {
		for _, c := range r.cells {
			for dr := 0; dr < c.rs && c.row+dr < len(rows); dr++ {
				for dc := 0; dc < c.cs && c.col+dc < ncols; dc++ {
					grid[c.row+dr][c.col+dc] = c
				}
			}
		}
	}
	resolved := map[*cellInfo][4]border{}
	if collapse {
		for ri, r := range rows {
			for _, c := range r.cells {
				rb := c.b
				// row-level borders contribute to the top/bottom edges
				if r.node != nil {
					rb[0] = pickBorder(rb[0], r.b[0])
					if c.rs == 1 {
						rb[2] = pickBorder(rb[2], r.b[2])
					}
				}
				// top
				if ri == 0 {
					rb[0] = pickBorder(rb[0], tblBorder[0])
				} else if nb := grid[ri-1][c.col]; nb != nil {
					rb[0] = pickBorder(rb[0], nb.b[2])
					if nr := rows[nb.row]; nr.node != nil && nb.row+nb.rs-1 == ri-1 {
						rb[0] = pickBorder(rb[0], nr.b[2])
					}
				}
				// bottom
				last := c.row + c.rs - 1
				if last >= len(rows)-1 {
					rb[2] = pickBorder(rb[2], tblBorder[2])
				} else if nb := grid[last+1][c.col]; nb != nil {
					rb[2] = pickBorder(rb[2], nb.b[0])
				}
				// left
				if c.col == 0 {
					rb[3] = pickBorder(rb[3], tblBorder[3])
				} else if nb := grid[ri][c.col-1]; nb != nil {
					rb[3] = pickBorder(rb[3], nb.b[1])
				}
				// right
				if c.col+c.cs >= ncols {
					rb[1] = pickBorder(rb[1], tblBorder[1])
				} else if nb := grid[ri][c.col+c.cs]; nb != nil {
					rb[1] = pickBorder(rb[1], nb.b[3])
				}
				resolved[c] = rb
			}
		}
	}

	// ---- lay out cell contents ----
	for _, r := range rows {
		for _, c := range r.cells {
			cb := newBlock()
			lc.boxProps(c.node, cb, c.w)
			if cb.bg.A == 0 {
				// cells without their own background show the row, section or column background
				for _, src := range []*Node{r.node, r.secNode, cols[c.col].node} {
					if src == nil {
						continue
					}
					if col, ok := ParseColor(src.Props["background-color"]); ok && col.A > 0 {
						cb.bg = col
						break
					}
				}
			}
			cb.spB, cb.spA = nil, nil
			cb.kNext, cb.kPrev, cb.bBefore, cb.bAfter = false, false, brkNone, brkNone
			if collapse {
				rb := resolved[c]
				for i := range rb {
					if rb[i].style == "none" || rb[i].style == "hidden" {
						rb[i].w = 0
					}
					rb[i].w /= 2
				}
				cb.b = rb
				cb.collapse = true
			}
			cb.left = 0
			cb.width = c.w
			inner := c.w - cb.p[3] - cb.b[3].w - cb.p[1] - cb.b[1].w
			if collapse {
				inner = c.w - cb.p[3] - cb.b[3].w - cb.p[1] - cb.b[1].w
			}
			if inner < 1 {
				inner = 1
			}
			lcc := &lctx{e: e, refW: inner, refH: lc.refH, S: S, E: E, pageNum: lc.pageNum, pageFmt: lc.pageFmt, markers: lc.markers, carry: lc.carry}
			bb := lcc.newBlockBuilder(c.node)
			bb.process(c.node.Children)
			bb.flush()
			cb.children = bb.items
			cb.markers = bb.markers
			cb.fillGaps()
			cb.spB, cb.spA = nil, nil
			if h, ok := lc.length(c.node, "block-progression-dimension.minimum", cw); ok {
				cb.minH = h
			} else if h, ok := lc.length(c.node, "height", cw); ok {
				cb.minH = h
			} else if h, ok := lc.length(c.node, "block-progression-dimension", cw); ok {
				cb.minH = h
			}
			c.blk = cb
		}
	}

	// ---- row heights ----
	for _, r := range rows {
		r.h = r.minH
		for _, c := range r.cells {
			if c.rs == 1 && c.blk.height() > r.h {
				r.h = c.blk.height()
			}
		}
	}
	for ri, r := range rows {
		for _, c := range r.cells {
			if c.rs > 1 {
				last := ri + c.rs - 1
				if last >= len(rows) {
					last = len(rows) - 1
				}
				sum := vsep * float64(last-ri)
				for k := ri; k <= last; k++ {
					sum += rows[k].h
				}
				if c.blk.height() > sum {
					rows[last].h += c.blk.height() - sum
				}
			}
		}
	}
	spanKeep := make([]bool, len(rows))
	// ---- assemble ----
	var hdr, body, ftr []vitem
	for ri, r := range rows {
		rb := &rowBox{h: r.h + vsep, pad: vsep}
		if r.node != nil {
			rb.kNext = isKeep(r.node.Props["keep-with-next"])
			rb.kTogeth = isKeep(r.node.Get("keep-together"))
			rb.bBefore = parseBrk(r.node.Props["break-before"])
			rb.bAfter = parseBrk(r.node.Props["break-after"])
			if id := r.node.Props["id"]; id != "" {
				rb.ids = append(rb.ids, id)
			}
		}
		for _, c := range r.cells {
			if id := c.node.Props["id"]; id != "" {
				rb.ids = append(rb.ids, id)
			}
			if c.rs > 1 {
				last := ri + c.rs - 1
				sum := vsep * float64(last-ri)
				for k := ri; k <= last && k < len(rows); k++ {
					sum += rows[k].h
					if k < last {
						spanKeep[k] = true
					}
				}
				c.blk.forceH = sum
				rb.noSplit = true
			} else {
				c.blk.forceH = r.h
			}
			rb.cells = append(rb.cells, rowCell{c.x, c.blk})
		}
		if spanKeep[ri] {
			rb.kNext = true
		}
		switch r.section {
		case 0:
			hdr = append(hdr, rb)
		case 2:
			ftr = append(ftr, rb)
		default:
			body = append(body, rb)
		}
	}
	b.children = body
	b.hdr = hdr
	b.ftr = ftr
	b.repeatHdr = n.Props["table-omit-header-at-break"] != "true"
	b.repeatFtr = n.Props["table-omit-footer-at-break"] != "true"
	b.fillGaps()
	if len(body) == 0 {
		b.children = nil
		b.gaps = nil
	}
	return b
}
