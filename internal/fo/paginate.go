package fo

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/ProDGG/fop-go/internal/font"
	"github.com/ProDGG/fop-go/internal/pdf"
)

type qEntry struct {
	node  *Node
	item  vitem
	w     float64 // column width the item was laid out for
	brk   brk
	noBrk bool
}

type placedItem struct {
	it  vitem
	col int
	y   float64
}

type seqRun struct {
	e       *Engine
	ps      *Node
	masters map[string]*pageMaster
	seqs    map[string]*seqMaster
	statics map[string]*Node
	doc     *pdf.Document
	links   *[]pendingLink
	pageFmt string
	carry   map[string]*Node
	lastNum string

	nextInitial string // initial-page-number of the next page-sequence
	hasNext     bool
}

// initialPageNumber mirrors AbstractPageSequence.initPageNumber.
func initialPageNumber(v string, endPrev int) int {
	v = strings.TrimSpace(v)
	switch v {
	case "", "auto", "auto-odd", "auto-even":
		start := endPrev + 1
		if v == "auto-odd" && start%2 == 0 {
			start++
		} else if v == "auto-even" && start%2 != 0 {
			start++
		}
		return start
	}
	if n, err := strconv.Atoi(v); err == nil && n > 0 {
		return n
	}
	return 1
}

// Render lays out the FO tree and returns the PDF document.
func Render(root *Node, opts Options) (*pdf.Document, error) {
	if opts.Fonts == nil {
		opts.Fonts = font.NewManager()
	}
	if opts.Fonts.Warn == nil {
		opts.Fonts.Warn = opts.Warn
	}
	if opts.PxPerPt <= 0 {
		opts.PxPerPt = 1
	}
	e := &Engine{opts: opts, fonts: opts.Fonts, warned: map[string]bool{}, styles: map[*Node]*textStyle{}, links: map[*Node]*linkTarget{},
		images: map[string]*imageData{}, pxPt: opts.PxPerPt, kerning: !opts.NoKerning}
	var doc *pdf.Document
	for pass := 0; pass < 6; pass++ {
		e.ids = map[string]idInfo{}
		e.cited = false
		e.cites = map[string]bool{}
		var err error
		doc, err = e.renderOnce(root)
		if err != nil {
			return nil, err
		}
		if !e.cited {
			return doc, nil
		}
		if sameIDs(e.ids, e.prevIDs) {
			for id := range e.cites {
				if _, ok := e.ids[id]; !ok {
					e.warn("page-number-citation: ref-id %q not found", strings.TrimSuffix(id, "\x00last"))
				}
			}
			return doc, nil
		}
		e.prevIDs = e.ids
	}
	e.warn("page number references did not stabilise after several layout passes")
	return doc, nil
}

func sameIDs(a, b map[string]idInfo) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w.pageNum != v.pageNum {
			return false
		}
	}
	return true
}

func (e *Engine) renderOnce(root *Node) (doc *pdf.Document, err error) {
	defer func() {
		if r := recover(); r != nil {
			pl, ok := r.(pageLimit)
			if !ok {
				panic(r)
			}
			doc, err = nil, pl.err
		}
	}()
	masters, seqs, err := e.parseMasters(root)
	if err != nil {
		return nil, err
	}
	doc = pdf.NewDocument()
	doc.Compress = e.opts.Compress
	var links []pendingLink
	endPrev := 0 // ending page number of the previous sequence (FOP: Root.endingPageNumberOfPreviousSequence)
	title := ""
	nseq := 0
	var seqNodes []*Node
	for _, c := range root.Children {
		if c.Name == "page-sequence" {
			seqNodes = append(seqNodes, c)
		}
	}
	for si, c := range seqNodes {
		nseq++
		sr := &seqRun{e: e, ps: c, masters: masters, seqs: seqs, doc: doc, links: &links,
			statics: map[string]*Node{}, carry: map[string]*Node{}}
		for _, sc := range c.FindAll("static-content") {
			sr.statics[sc.Props["flow-name"]] = sc
		}
		sr.pageFmt = c.Props["format"]
		if si+1 < len(seqNodes) {
			sr.nextInitial = strings.TrimSpace(seqNodes[si+1].Props["initial-page-number"])
			sr.hasNext = true
		}
		start := initialPageNumber(c.Props["initial-page-number"], endPrev)
		if title == "" {
			if t := c.Find("title"); t != nil {
				title = strings.Join(strings.Fields(t.TextContent()), " ")
			}
		}
		n, err := sr.run(start)
		if err != nil {
			return nil, err
		}
		endPrev = n - 1
	}
	if nseq == 0 {
		return nil, fmt.Errorf("fo: document contains no fo:page-sequence")
	}
	// resolve links
	for _, l := range links {
		t := l.target
		switch {
		case t.destID != "":
			if info, ok := e.ids[t.destID]; ok {
				l.page.AddDestLink(l.x, l.y, l.w, l.h, info.page, info.y)
			} else {
				e.warn("link destination %q not found", t.destID)
			}
		case t.uri != "":
			l.page.AddURILink(l.x, l.y, l.w, l.h, t.uri)
		}
	}
	// bookmarks
	var buildBM func(n *Node) []*pdf.Outline
	buildBM = func(n *Node) []*pdf.Outline {
		var out []*pdf.Outline
		for _, b := range n.FindAll("bookmark") {
			o := &pdf.Outline{Page: -1, Open: b.Props["starting-state"] != "hide"}
			if t := b.Find("bookmark-title"); t != nil {
				o.Title = strings.Join(strings.Fields(t.TextContent()), " ")
			}
			if info, ok := e.ids[b.Props["internal-destination"]]; ok {
				o.Page, o.Y = info.page, info.y
			}
			o.Children = buildBM(b)
			out = append(out, o)
		}
		return out
	}
	for _, bt := range root.FindAll("bookmark-tree") {
		doc.Outlines = append(doc.Outlines, buildBM(bt)...)
	}
	doc.Info.Title = firstNonEmpty(e.opts.Title, title)
	doc.Info.Author = e.opts.Author
	doc.Info.Subject = e.opts.Subject
	doc.Info.Keywords = e.opts.Keywords
	doc.Info.Creator = firstNonEmpty(e.opts.Creator, "fop-go")
	doc.Info.Producer = firstNonEmpty(e.opts.Producer, "fop-go (Go implementation of XSL-FO to PDF)")
	doc.Info.Created = time.Now()
	return doc, nil
}

func flowID(ps *Node) string {
	if f := ps.Find("flow"); f != nil {
		return f.Props["id"]
	}
	return ""
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// makeQueue turns the flow's children into layout entries; runs of inline
// content are wrapped into anonymous blocks.
func (s *seqRun) makeQueue(flow *Node) []qEntry {
	var q []qEntry
	if flow == nil {
		return q
	}
	var group []*Node
	flush := func() {
		if len(group) == 0 {
			return
		}
		blank := true
		for _, g := range group {
			if g.Name != "#text" || strings.TrimSpace(g.Text) != "" {
				blank = false
			}
		}
		if !blank {
			an := &Node{Name: "block", Props: map[string]string{}, Parent: flow}
			for _, g := range group {
				an.Children = append(an.Children, g)
			}
			q = append(q, qEntry{node: an})
		}
		group = nil
	}
	for _, c := range flow.Children {
		switch {
		case isBlockLevel(c.Name):
			flush()
			q = append(q, qEntry{node: c})
		case c.Name == "wrapper" && containsBlock(c):
			flush()
			// wrapper around blocks: flatten, keeping the wrapper's properties for inheritance
			q = append(q, s.makeQueueFrom(c)...)
		default:
			group = append(group, c)
		}
	}
	flush()
	return q
}

func (s *seqRun) makeQueueFrom(w *Node) []qEntry { return s.makeQueue(w) }

func entryBreak(en qEntry) brk {
	if en.noBrk {
		return brkNone
	}
	k := en.brk
	if en.item != nil {
		return maxBrk(k, en.item.breakBefore())
	}
	if en.node != nil {
		k = maxBrk(k, parseBrk(en.node.Props["break-before"]))
	}
	return k
}

func (s *seqRun) run(startNum int) (int, error) {
	ps := s.ps
	ref := ps.Props["master-reference"]
	if ref == "" {
		return 0, fmt.Errorf("fo:page-sequence without master-reference")
	}
	picker := &masterPicker{masters: s.masters}
	if sm, ok := s.seqs[ref]; ok {
		picker.sm = sm
	} else if pm, ok := s.masters[ref]; ok {
		picker.single = pm
	} else {
		return 0, fmt.Errorf("fo:page-sequence refers to unknown master %q", ref)
	}
	queue := s.makeQueue(ps.Find("flow"))
	if ps.Find("flow") == nil {
		s.e.warn("fo:page-sequence has no fo:flow")
	}
	pnum := startNum
	idx := 0
	pending := brkNone
	firstPageNum := startNum
	for {
		kind := pending
		if len(queue) > 0 {
			kind = maxBrk(kind, entryBreak(queue[0]))
		}
		needBlank := (kind == brkOdd && pnum%2 == 0) || (kind == brkEven && pnum%2 == 1)
		if needBlank {
			m, err := picker.pick(pageCtx{first: idx == 0, odd: pnum%2 == 1, blank: true})
			if err != nil {
				return 0, err
			}
			s.renderPage(m, nil, pnum, idx == 0)
			pnum++
			idx++
			continue
		}
		pc := pageCtx{first: idx == 0, odd: pnum%2 == 1}
		state0 := *picker
		m, err := picker.pick(pc)
		if err != nil {
			return 0, err
		}
		pl, rest, pend := s.fill(m, queue, pnum)
		if len(rest) == 0 {
			lp := state0
			lastPC := pc
			lastPC.last = true
			if lm, err := lp.pick(lastPC); err == nil && lm != m {
				if pl2, rest2, _ := s.fill(lm, queue, pnum); len(rest2) == 0 {
					m, pl, rest = lm, pl2, rest2
					*picker = lp
				}
			}
		}
		if len(rest) == 0 {
			s.lastNum = fmtPage(pnum, s.pageFmt)
		}
		s.renderPage(m, pl, pnum, idx == 0)
		queue = rest
		pending = pend
		pnum++
		idx++
		if len(queue) == 0 {
			break
		}
		if idx > 100000 {
			return 0, fmt.Errorf("fo: too many pages (infinite pagination loop?)")
		}
	}
	pnum = s.doForcePageCount(picker, firstPageNum, pnum)
	for _, id := range []string{ps.Props["id"], flowID(ps)} {
		if id != "" {
			s.e.ids[id+"\x00last"] = idInfo{pageNum: s.lastNum}
		}
	}
	return pnum, nil
}

// fill places queue entries onto one page.
func (s *seqRun) fill(pm *pageMaster, queue []qEntry, pnum int) (placed []placedItem, rest []qEntry, kbrk brk) {
	const eps = 0.01
	q := append([]qEntry(nil), queue...)
	br := pm.bodyRect()
	ncols := pm.body.columns
	colW := (br.w - pm.body.gap*float64(ncols-1)) / float64(ncols)
	colH := br.h
	lc := &lctx{e: s.e, refW: colW, refH: colH, pageNum: pnum, pageFmt: s.pageFmt, carry: s.carry}
	lc.fn = s.footnoteCtx(colW, pnum)
	col := 0
	y := 0.0
	// footnotes of the column being filled
	var colFn []*footnoteRef
	fnUsed := 0.0
	flushFn := func() {
		if len(colFn) > 0 {
			placed = append(placed, placedItem{&fnArea{refs: colFn, h: fnUsed}, col, colH - fnUsed})
		}
		colFn, fnUsed = nil, 0
		lc.fn.have = false
	}
	defer flushFn()
	fnAdd := func(it vitem) {
		refs := fnOf(it)
		if len(refs) == 0 {
			return
		}
		fnUsed += fnCost(refs, len(colFn) > 0)
		colFn = append(colFn, refs...)
		lc.fn.have = true
	}
	var prev vitem
	laidOut := func(i int) bool {
		if q[i].item != nil {
			if d := colW - q[i].w; q[i].w != 0 && math.Abs(d) > 0.001 {
				if b, ok := q[i].item.(*block); ok {
					b.reflow(d)
				}
			}
			q[i].w = colW
			return true
		}
		it := lc.layoutBlockLevel(q[i].node)
		if it == nil {
			return false
		}
		q[i].item = it
		q[i].w = colW
		return true
	}
	for len(q) > 0 {
		if !laidOut(0) {
			q = q[1:]
			continue
		}
		ent := q[0]
		it := ent.item
		if h, t, k := it.forcedSplit(); h != nil {
			ins := []qEntry{{item: h, brk: ent.brk, noBrk: ent.noBrk, w: ent.w}, {item: t, brk: k, w: ent.w}}
			q = append(ins, q[1:]...)
			continue
		}
		k := maxBrk(ent.brk, it.breakBefore())
		if ent.noBrk {
			k = brkNone
		}
		if k != brkNone && len(placed) > 0 {
			if k == brkColumn && col+1 < ncols {
				flushFn()
				col++
				y = 0
				prev = nil
				q[0].noBrk = true
				continue
			}
			q[0].noBrk, q[0].brk = true, brkNone
			return placed, q, k
		}
		var gap float64
		if prev != nil {
			gap = resolveSpaces(concatSp(prev.afterSp(), it.beforeSp()))
		} else {
			// start of a column: conditional space is discarded, retained space stays
			gap = resolveSpaces(discardAtBreak(it.beforeSp(), false))
		}
		// keep-with-next chains
		if y > 0 && (it.keepNext() || (len(q) > 1 && laidOut(1) && q[1].item.keepPrev())) {
			total := gap + it.height() + fnCost(fnOf(it), len(colFn) > 0)
			cur := it
			j := 0
			lastGap := gap // gap before the last item of the chain
			for j+1 < len(q) && laidOut(j+1) {
				nx := q[j+1].item
				if !(cur.keepNext() || nx.keepPrev()) {
					break
				}
				if !q[j+1].noBrk && maxBrk(q[j+1].brk, nx.breakBefore()) != brkNone {
					break
				}
				lastGap = resolveSpaces(concatSp(cur.afterSp(), nx.beforeSp()))
				total += lastGap + nx.height() + fnCost(fnOf(nx), true)
				cur = nx
				j++
			}
			// the last block of the chain may still be split (keep-with-next only
			// forbids a break right after the preceding block)
			chainFits := y+total <= colH+eps
			if !chainFits && j > 0 && y+total-cur.height()-lastGap <= colH+eps {
				used := y + total - cur.height()
				if hd, _ := cur.split(colH-used, false); hd != nil {
					chainFits = true
				}
			}
			if !chainFits && total <= colH+eps {
				if col+1 < ncols {
					flushFn()
					col++
					y = 0
					prev = nil
					continue
				}
				return placed, q, brkNone
			}
		}
		avail := colH - y - gap - fnUsed
		h := it.height() + fnCost(fnOf(it), false)
		if h <= avail+eps {
			placed = append(placed, placedItem{it, col, y + gap})
			y += gap + it.height()
			fnAdd(it)
			prev = it
			q = q[1:]
			if bk := it.breakAfter(); bk != brkNone && len(q) > 0 {
				q[0].brk = maxBrk(q[0].brk, bk)
			}
			continue
		}
		atTop := prev == nil
		head, tail := it.split(avail, atTop)
		if head != nil {
			placed = append(placed, placedItem{head, col, y + gap})
			fnAdd(head)
			q[0] = qEntry{item: tail, w: colW}
			if col+1 < ncols {
				flushFn()
				col++
				y = 0
				prev = nil
				continue
			}
			return placed, q, brkNone
		}
		if atTop {
			placed = append(placed, placedItem{it, col, y})
			fnAdd(it)
			y += h
			prev = it
			q = q[1:]
			continue
		}
		if col+1 < ncols {
			flushFn()
			col++
			y = 0
			prev = nil
			continue
		}
		return placed, q, brkNone
	}
	return placed, nil, brkNone
}

func (s *seqRun) renderPage(pm *pageMaster, placed []placedItem, pnum int, first bool) {
	e := s.e
	if e.opts.MaxPages > 0 && len(s.doc.Pages) >= e.opts.MaxPages {
		panic(pageLimit{fmt.Errorf("%w: more than %d pages", pdf.ErrPageLimit, e.opts.MaxPages)})
	}
	pg := s.doc.NewPage(pm.w, pm.h)
	pageIdx := len(s.doc.Pages) - 1
	rc := &renderCtx{e: e, doc: s.doc, pg: pg, pageIdx: pageIdx, pageNum: fmtPage(pnum, s.pageFmt),
		pageW: pm.w, pageH: pm.h, links: s.links}
	if first {
		if id := s.ps.Props["id"]; id != "" {
			e.ids[id] = idInfo{page: pageIdx, pageNum: rc.pageNum, y: pm.h}
		}
	}
	br := pm.bodyRect()
	if pm.body.bg.A > 0 {
		rc.fillRect(br.x, br.y, br.w, br.h, pm.body.bg)
	}
	regions := []struct {
		r    *regionDef
		kind string
	}{{pm.before, "before"}, {pm.after, "after"}, {pm.start, "start"}, {pm.end, "end"}}
	for _, rg := range regions {
		if rg.r != nil && rg.r.bg.A > 0 {
			rr := pm.regionRect(rg.r, rg.kind)
			rc.fillRect(rr.x, rr.y, rr.w, rr.h, rg.r.bg)
		}
	}
	ncols := pm.body.columns
	colW := (br.w - pm.body.gap*float64(ncols-1)) / float64(ncols)
	rc.region = br
	for _, p := range placed {
		x := br.x + float64(p.col)*(colW+pm.body.gap)
		p.it.render(rc, x, br.y+p.y)
	}
	// static content
	for _, rg := range regions {
		if rg.r == nil {
			continue
		}
		sc := s.statics[rg.r.name]
		if sc == nil {
			continue
		}
		rr := pm.regionRect(rg.r, rg.kind)
		lc := &lctx{e: e, refW: rr.w, refH: rr.h, pageNum: pnum, pageFmt: s.pageFmt, markers: rc.markers, carry: s.carry}
		bb := lc.newBlockBuilder(sc)
		bb.process(sc.Children)
		bb.flush()
		tmp := newBlock()
		tmp.children = bb.items
		tmp.fillGaps()
		tmp.width = rr.w
		tmp.displayAlign = rg.r.displayAlign
		tmp.forceH = rr.h
		rc.region = rr
		tmp.render(rc, rr.x, rr.y)
	}
	for _, m := range rc.markers {
		s.carry[m.class] = m.node
	}
}

// doForcePageCount is a port of AbstractPageSequenceLayoutManager.doForcePageCount;
// pnum is the number of the next page (the last page has number pnum-1).
func (s *seqRun) doForcePageCount(picker *masterPicker, startNum, pnum int) int {
	fpc := strings.TrimSpace(s.ps.Props["force-page-count"])
	if fpc == "" {
		fpc = "auto"
	}
	if fpc == "auto" && s.hasNext {
		switch ni := s.nextInitial; ni {
		case "", "auto":
			fpc = "no-force"
		case "auto-odd":
			fpc = "end-on-even"
		case "auto-even":
			fpc = "end-on-odd"
		default:
			n, err := strconv.Atoi(ni)
			if err != nil || n <= 0 {
				n = 1
			}
			if n%2 == 0 {
				fpc = "end-on-odd"
			} else {
				fpc = "end-on-even"
			}
		}
	}
	cur := pnum - 1
	addBlank := func() {
		if m, err := picker.pick(pageCtx{odd: pnum%2 == 1, blank: true, last: true}); err == nil {
			s.renderPage(m, nil, pnum, false)
			pnum++
		}
	}
	rel := cur - startNum + 1
	switch fpc {
	case "even":
		if rel%2 != 0 {
			addBlank()
		}
	case "odd":
		if rel%2 == 0 {
			addBlank()
		}
	case "end-on-even":
		if cur%2 != 0 {
			addBlank()
		}
	case "end-on-odd":
		if cur%2 == 0 {
			addBlank()
		}
	case "doubly-even":
		for (pnum-1-startNum+1)%4 != 0 {
			addBlank()
		}
	case "end-on-doubly-even":
		for (pnum-1)%4 != 0 {
			addBlank()
		}
	case "doubly-odd":
		for (pnum-1-startNum+1)%4 != 3 {
			addBlank()
		}
	case "end-on-doubly-odd":
		for (pnum-1)%4 != 3 {
			addBlank()
		}
	}
	return pnum
}

// footnoteCtx prepares the footnote geometry for a page whose columns are colW wide.
func (s *seqRun) footnoteCtx(colW float64, pnum int) *fnCtx {
	ctx := &fnCtx{bodyW: colW}
	if sc := s.statics["xsl-footnote-separator"]; sc != nil {
		lc := &lctx{e: s.e, refW: colW, pageNum: pnum, pageFmt: s.pageFmt, carry: s.carry}
		bb := lc.newBlockBuilder(sc)
		bb.process(sc.Children)
		bb.flush()
		blk := newBlock()
		blk.children = bb.items
		blk.fillGaps()
		blk.width = colW
		ctx.sepBlk = blk
		ctx.sepH = blk.height()
	}
	return ctx
}

// pageLimit carries ErrPageLimit out of the page loop.
type pageLimit struct{ err error }
