package xhr

import (
	"sort"
)

// BlockFormattingContext tracks translation for float placement.
type BlockFormattingContext struct {
	x, y int
	fm   *FloatManager
}

func (b *BlockFormattingContext) translate(x, y int) { b.x -= x; b.y -= y }

// LayoutState is the part of the layout context saved for relayout.
type LayoutState struct {
	markerData  *MarkerData
	pageName    string
	bfcs        []*BlockFormattingContext
	extraTop    int
	extraBottom int
	noPageBreak int
}

// LayoutContext is Flying Saucer's LayoutContext.
type LayoutContext struct {
	doc                  *Doc
	rootLayer            *Layer
	rootDocLayer         *Layer
	layers               []*Layer
	bfcs                 []*BlockFormattingContext
	markerData           *MarkerData
	extraTop             int
	extraBottom          int
	noPageBreak          int
	pageName             string
	pendingPage          string
	page                 *PageBox
	mayCheckKeepTogether bool
	breakAtLine          *breakAtLineContext
}

type breakAtLineContext struct {
	block *Box
	line  int
}

func newLayoutContext(d *Doc) *LayoutContext {
	lc := &LayoutContext{doc: d, mayCheckKeepTogether: true}
	d.lc = lc
	return lc
}

func (lc *LayoutContext) isPrint() bool { return true }

func (lc *LayoutContext) isPageBreaksAllowed() bool { return lc.noPageBreak == 0 }

func (lc *LayoutContext) bfc() *BlockFormattingContext { return lc.bfcs[len(lc.bfcs)-1] }

func (lc *LayoutContext) pushBFC(b *BlockFormattingContext) { lc.bfcs = append(lc.bfcs, b) }
func (lc *LayoutContext) popBFC()                           { lc.bfcs = lc.bfcs[:len(lc.bfcs)-1] }

func (lc *LayoutContext) translate(x, y int) { lc.bfc().translate(x, y) }

// layer is the innermost layer being laid out.
func (lc *LayoutContext) layer() *Layer { return lc.layers[len(lc.layers)-1] }

// pushLayer creates a layer mastered by the box (the first one is the root layer).
func (lc *LayoutContext) pushLayer(master *Box) {
	var l *Layer
	if lc.rootLayer == nil {
		l = newLayer(nil, master)
		l.stackingContext = true
		l.running = map[string][]*Box{}
		lc.rootLayer = l
	} else {
		parent := lc.layer()
		l = newLayer(parent, master)
		parent.addChild(l)
	}
	lc.layers = append(lc.layers, l)
}

func (lc *LayoutContext) pushLayerObj(l *Layer) { lc.layers = append(lc.layers, l) }

func (lc *LayoutContext) popLayer() {
	l := lc.layer()
	l.finish(lc)
	lc.layers = lc.layers[:len(lc.layers)-1]
}

func (lc *LayoutContext) reInit(keepLayers bool) {
	lc.markerData = nil
	lc.bfcs = nil
	if !keepLayers {
		lc.rootLayer = nil
		lc.layers = nil
	}
	lc.extraTop = 0
	lc.extraBottom = 0
}

func (lc *LayoutContext) captureLayoutState() *LayoutState {
	return &LayoutState{markerData: lc.markerData, pageName: lc.pageName, bfcs: lc.bfcs,
		extraTop: lc.extraTop, extraBottom: lc.extraBottom, noPageBreak: lc.noPageBreak}
}

func (lc *LayoutContext) restoreLayoutState(s *LayoutState) {
	lc.markerData = s.markerData
	lc.bfcs = s.bfcs
	lc.pageName = s.pageName
	lc.extraTop = s.extraTop
	lc.extraBottom = s.extraBottom
	lc.noPageBreak = s.noPageBreak
}

func (lc *LayoutContext) copyStateForRelayout() *LayoutState {
	return &LayoutState{markerData: lc.markerData, pageName: lc.pageName}
}

func (lc *LayoutContext) restoreStateForRelayout(s *LayoutState) {
	lc.markerData = s.markerData
	lc.pageName = s.pageName
}

// Layer is the root layer: it owns the page list and running elements.
type Layer struct {
	parent          *Layer
	stackingContext bool
	children        []*Layer
	end             *Box
	fixedBackground bool
	inline          bool
	requiresLayout  bool
	master          *Box
	pages           []*PageBox
	lastRequested   *PageBox
	running         map[string][]*Box
	floats          []*Box
	pageSeqs        []*Box
}

func (l *Layer) addFloat(b *Box) {
	l.floats = append(l.floats, b)
	b.floated.drawingLayer = l
}
func (l *Layer) removeFloat(b *Box) {
	for i, f := range l.floats {
		if f == b {
			l.floats = append(l.floats[:i], l.floats[i+1:]...)
			return
		}
	}
}

func (l *Layer) addPageSequence(b *Box) { l.pageSeqs = append(l.pageSeqs, b) }

func (l *Layer) addPage(lc *LayoutContext) {
	var pseudo string
	switch {
	case len(l.pages) == 0:
		pseudo = "first"
	case len(l.pages)%2 == 0:
		pseudo = "right"
	default:
		pseudo = "left"
	}
	pb := createPageBox(lc, pseudo)
	if len(l.pages) == 0 {
		pb.setTopAndBottom(0)
	} else {
		prev := l.pages[len(l.pages)-1]
		pb.setTopAndBottom(prev.bottom)
	}
	pb.pageNo = len(l.pages)
	l.pages = append(l.pages, pb)
}

func (l *Layer) removeLastPage() {
	pb := l.pages[len(l.pages)-1]
	l.pages = l.pages[:len(l.pages)-1]
	if pb == l.lastRequested {
		l.lastRequested = nil
	}
}

func (l *Layer) lastPage() *PageBox {
	if len(l.pages) == 0 {
		return nil
	}
	return l.pages[len(l.pages)-1]
}

func (l *Layer) firstPage(lc *LayoutContext, b *Box) *PageBox { return l.pageAt(lc, b.absY) }
func (l *Layer) lastPageOf(lc *LayoutContext, b *Box) *PageBox {
	return l.pageAt(lc, b.absY+b.height-1)
}
func (l *Layer) ensureHasPage(lc *LayoutContext, b *Box) { l.lastPageOf(lc, b) }

func (l *Layer) pageAt(lc *LayoutContext, y int) *PageBox {
	if y < 0 {
		return nil
	}
	if lr := l.lastRequested; lr != nil && y >= lr.top && y < lr.bottom {
		return lr
	}
	last := l.pages[len(l.pages)-1]
	if y < last.bottom {
		count := len(l.pages)
		for i := count - 1; i >= 0 && i >= count-5; i-- {
			pb := l.pages[i]
			if y >= pb.top && y < pb.bottom {
				l.lastRequested = pb
				return pb
			}
		}
		low, high := 0, count-6
		for low <= high {
			mid := (low + high) >> 1
			pb := l.pages[mid]
			if y >= pb.top && y < pb.bottom {
				l.lastRequested = pb
				return pb
			}
			if pb.top < y {
				low = mid + 1
			} else {
				high = mid - 1
			}
		}
		panic("xhr: internal error: page not found")
	}
	for y >= l.pages[len(l.pages)-1].bottom {
		l.addPage(lc)
	}
	res := l.pages[len(l.pages)-1]
	l.lastRequested = res
	return res
}

func (l *Layer) trimEmptyPages(maxY int) {
	for i := len(l.pages) - 1; i > 0; i-- {
		pb := l.pages[i]
		if pb.top >= maxY {
			if pb == l.lastRequested {
				l.lastRequested = nil
			}
			l.pages = l.pages[:i]
		} else {
			break
		}
	}
}

func (l *Layer) trimPageCount(n int) {
	for len(l.pages) > n {
		pb := l.pages[len(l.pages)-1]
		l.pages = l.pages[:len(l.pages)-1]
		if pb == l.lastRequested {
			l.lastRequested = nil
		}
	}
}

func (l *Layer) crossesPageBreak(lc *LayoutContext, top, bottom int) bool {
	if top < 0 {
		return false
	}
	pb := l.pageAt(lc, top)
	return bottom >= pb.bottom-lc.extraBottom
}

func (l *Layer) assignPagePaintingPositions() {
	paintingTop := 0
	for _, pb := range l.pages {
		pb.paintingTop = paintingTop
		pb.paintingBottom = paintingTop + pb.contentHeight()
		paintingTop = pb.paintingBottom
	}
}

func (l *Layer) addRunningBlock(b *Box) {
	id := b.Style.RunningName()
	l.running[id] = append(l.running[id], b)
	bs := l.running[id]
	sort.SliceStable(bs, func(i, j int) bool { return bs[i].absY < bs[j].absY })
}

func (l *Layer) removeRunningBlock(b *Box) {
	id := b.Style.RunningName()
	bs := l.running[id]
	for i, x := range bs {
		if x == b {
			l.running[id] = append(bs[:i:i], bs[i+1:]...)
			return
		}
	}
}

func (l *Layer) runningBlock(ident string, page *PageBox, which string) *Box {
	blocks := l.running[ident]
	if len(blocks) == 0 {
		return nil
	}
	switch which {
	case "start":
		var prev *Box
		for _, b := range blocks {
			if b.staticEquivalent.absY >= page.top {
				break
			}
			prev = b
		}
		return prev
	case "first":
		for _, b := range blocks {
			y := b.staticEquivalent.absY
			if y >= page.top && y < page.bottom {
				return b
			}
		}
		return l.runningBlock(ident, page, "start")
	case "last":
		var prev *Box
		for _, b := range blocks {
			if b.staticEquivalent.absY > page.bottom {
				break
			}
			prev = b
		}
		return prev
	case "last-except":
		var prev *Box
		for _, b := range blocks {
			y := b.staticEquivalent.absY
			if y >= page.top && y < page.bottom {
				return nil
			}
			if y > page.bottom {
				break
			}
			prev = b
		}
		return prev
	}
	return nil
}

func (l *Layer) sortedPageSequences() []*Box {
	if l.pageSeqs == nil {
		return nil
	}
	seq := append([]*Box(nil), l.pageSeqs...)
	sort.SliceStable(seq, func(i, j int) bool { return seq[i].absY < seq[j].absY })
	return seq
}

// relativePageNo mirrors getRelativePageNo(c) (no initial page offset).
func (l *Layer) relativePageNo(lc *LayoutContext, pageNo int, page *PageBox) int {
	seqs := l.sortedPageSequences()
	if seqs == nil {
		return pageNo
	}
	start := l.pageSequenceStart(seqs, page)
	if start == -1 {
		return pageNo
	}
	return pageNo - l.firstPage(lc, seqs[start]).pageNo
}

func (l *Layer) relativePageCount(lc *LayoutContext, pageCount int, page *PageBox) int {
	seqs := l.sortedPageSequences()
	if seqs == nil {
		return pageCount
	}
	first, last := 0, 0
	start := l.pageSequenceStart(seqs, page)
	if start != -1 {
		first = l.firstPage(lc, seqs[start]).pageNo
	}
	if start < len(seqs)-1 {
		last = l.firstPage(lc, seqs[start+1]).pageNo
	} else {
		last = pageCount
	}
	return last - first
}

func (l *Layer) pageSequenceStart(seqs []*Box, page *PageBox) int {
	for i := len(seqs) - 1; i >= 0; i-- {
		if seqs[i].absY < page.bottom-1 {
			return i
		}
	}
	return -1
}

func (l *Layer) layoutPages(lc *LayoutContext) {
	lc.rootDocLayer = lc.rootLayer
	for _, pb := range append([]*PageBox(nil), l.pages...) {
		pb.layout(lc)
	}
}
