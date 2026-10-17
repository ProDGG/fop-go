package xhr

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/ProDGG/fop-go/xmldom"
)

// TableColumn is a <col>/<colgroup> element.
type TableColumn struct {
	elem   *xmldom.Node
	style  *Style
	parent *TableColumn
}

type childBoxInfo struct {
	containsBlockLevel bool
	containsTable      bool
	layoutRunning      bool
}

// Doc is the shared context of one document (SharedContext + StyleReference).
type Doc struct {
	ctx     *Ctx
	matcher *Matcher
	root    *xmldom.Node
	empty   *Style
	styles  map[*xmldom.Node]*Style
	boxIDs  map[string]*Box
	counter map[*Style]*counterContext
	lc      *LayoutContext
	images  *ImageCache

	mainLayer *Layer
}

// NewDoc creates the document context.
func NewDoc(ctx *Ctx, m *Matcher, root *xmldom.Node, imgs *ImageCache) *Doc {
	return &Doc{
		ctx: ctx, matcher: m, root: root, empty: NewEmptyStyle(ctx),
		styles: map[*xmldom.Node]*Style{}, boxIDs: map[string]*Box{},
		counter: map[*Style]*counterContext{}, images: imgs,
	}
}

func isElem(n *xmldom.Node) bool { return n != nil && n.Type == xmldom.ElementNode }

func parentElem(e *xmldom.Node) *xmldom.Node {
	if e.Parent != nil && isElem(e.Parent) {
		return e.Parent
	}
	return nil
}

func (d *Doc) style(e *xmldom.Node) *Style {
	if s, ok := d.styles[e]; ok {
		return s
	}
	var ps *Style
	if p := parentElem(e); p != nil {
		ps = d.style(p)
	} else {
		ps = d.empty
	}
	nonCSS := ParseStyleAttr(nonCSSStyling(e))
	styleAttr := ParseStyleAttr(elementStyling(e))
	casc := d.matcher.Cascaded(e, "", nonCSS, styleAttr)
	s := ps.Derive(casc)
	d.styles[e] = s
	return s
}

// pseudoStyle returns the cascaded declarations for a pseudo element (nil if none).
func (d *Doc) pseudoStyle(e *xmldom.Node, pe string) map[string]*Declared {
	return d.matcher.Cascaded(e, pe, nil, nil)
}

func (d *Doc) createRootBox(lc *LayoutContext) *Box {
	root := d.root
	style := d.style(root)
	var res *Box
	if style.IsTable() || style.IsInlineTable() {
		res = newBox(KTable)
	} else {
		res = newBox(KBlock)
	}
	res.setStyle(style)
	res.Elem = root
	lc.resolveCounters(style, nil)
	lc.pushLayer(res)
	lc.rootLayer.addPage(lc)
	return res
}

func newBox(k Kind) *Box {
	b := &Box{Kind: k}
	if k == KSection {
		b.needCellWidthCalc = true
		b.needCellRecalc = true
	}
	return b
}

func (b *Box) setStyle(s *Style) {
	b.Style = s
	if b.Kind == KTable {
		switch {
		case b.marginAreaRoot:
			b.tableLayout = &marginTableLayout{autoTableLayout{table: b}}
		case s.IsIdent("table-layout", "auto") || s.IsAutoWidth():
			b.tableLayout = &autoTableLayout{table: b}
		default:
			b.tableLayout = &fixedTableLayout{table: b}
		}
	}
}

// ensureChildren builds the child boxes lazily like BlockBox.ensureChildren.
func (b *Box) ensureChildren(lc *LayoutContext) {
	if b.childrenType == ContentUnknown {
		lc.doc.createChildrenOf(lc, b)
	}
}

func (d *Doc) createChildrenOf(lc *LayoutContext, parent *Box) {
	var children []Styleable
	info := &childBoxInfo{}
	d.createChildren(lc, parent, parent.Elem, &children, info, false)
	nesting := isNestingTableContent(parent.Style.Ident("display"))
	if !nesting && !info.containsTable {
		d.resolveChildren(lc, parent, children, info)
	} else {
		children = stripAllWhitespace(children)
		if nesting {
			d.resolveTableContent(lc, parent, children, info)
		} else {
			d.resolveChildTableContent(lc, parent, children, info, "table-cell")
		}
	}
}

func isNestingTableContent(display string) bool {
	switch display {
	case "table", "inline-table", "table-header-group", "table-row-group", "table-footer-group", "table-row":
		return true
	}
	return false
}

func (d *Doc) resolveChildren(lc *LayoutContext, owner *Box, children []Styleable, info *childBoxInfo) {
	if len(children) > 0 {
		if info.containsBlockLevel {
			d.insertAnonymousBlocks(owner, children, info.layoutRunning)
			owner.childrenType = ContentBlock
		} else {
			children = stripInlineContent(children)
			if len(children) > 0 {
				owner.setInlineContent(children)
				owner.childrenType = ContentInline
			} else {
				owner.childrenType = ContentEmpty
			}
		}
	} else {
		owner.childrenType = ContentEmpty
	}
}

func (b *Box) setInlineContent(ic []Styleable) {
	b.inlineContent = ic
	for _, c := range ic {
		if bx, ok := c.(*Box); ok {
			bx.containingBlk = b
		}
	}
}

func isProperTableNesting(parent, child string) bool {
	switch parent {
	case "table":
		return child == "table-header-group" || child == "table-row-group" || child == "table-footer-group" || child == "table-caption"
	case "table-header-group", "table-row-group", "table-footer-group":
		return child == "table-row"
	case "table-row":
		return child == "table-cell"
	case "inline-table":
		return child == "table-header-group" || child == "table-row-group" || child == "table-footer-group"
	}
	return false
}

func isAllProperTableNesting(parentDisplay string, children []Styleable) bool {
	for _, c := range children {
		if !isProperTableNesting(parentDisplay, c.GetStyle().Ident("display")) {
			return false
		}
	}
	return true
}

func nextTableNestingLevel(display string) string {
	switch display {
	case "table", "inline-table":
		return "table-row-group"
	case "table-header-group", "table-row-group", "table-footer-group":
		return "table-row"
	case "table-row":
		return "table-cell"
	}
	return ""
}

func previousTableNestingLevel(display string) string {
	switch display {
	case "table-cell":
		return "table-row"
	case "table-row":
		return "table-row-group"
	case "table-header-group", "table-row-group", "table-footer-group":
		return "table"
	}
	return ""
}

func matchesTableLevel(target, value string) bool {
	if target == "table-row-group" {
		return value == "table-row-group" || value == "table-header-group" || value == "table-footer-group" || value == "table-caption"
	}
	return target == value
}

func (d *Doc) resolveChildTableContent(lc *LayoutContext, parent *Box, children []Styleable, info *childBoxInfo, target string) {
	var forAnon []Styleable
	var withAnon []Styleable
	nextUp := previousTableNestingLevel(target)
	for _, s := range children {
		if matchesTableLevel(target, s.GetStyle().Ident("display")) {
			forAnon = append(forAnon, s)
		} else {
			if len(forAnon) > 0 {
				d.createAnonymousTableContent(lc, forAnon[0].(*Box), nextUp, forAnon, &withAnon)
				forAnon = nil
			}
			withAnon = append(withAnon, s)
		}
	}
	if len(forAnon) > 0 {
		d.createAnonymousTableContent(lc, forAnon[0].(*Box), nextUp, forAnon, &withAnon)
	}
	if nextUp == "table" {
		rebalanceInlineContent(withAnon)
		info.containsBlockLevel = true
		d.resolveChildren(lc, parent, withAnon, info)
	} else {
		d.resolveChildTableContent(lc, parent, withAnon, info, nextUp)
	}
}

func rebalanceInlineContent(content []Styleable) {
	byElem := map[*xmldom.Node]*InlineSrc{}
	var order []*xmldom.Node
	for _, s := range content {
		if ib, ok := s.(*InlineSrc); ok {
			if _, seen := byElem[ib.elem]; !seen {
				ib.startsHere = true
				order = append(order, ib.elem)
			}
			byElem[ib.elem] = ib
		}
	}
	for _, e := range order {
		byElem[e].endsHere = true
	}
}

func stripAllWhitespace(content []Styleable) []Styleable {
	start := 0
	started := false
	cur := 0
	for cur = 0; cur < len(content); cur++ {
		s := content[cur]
		if !isLayedOutInInlineContext(s.GetStyle()) {
			if started {
				before := len(content)
				sub := stripInlineContent(append([]Styleable(nil), content[start:cur]...))
				content = append(append(append([]Styleable(nil), content[:start]...), sub...), content[cur:]...)
				after := len(content)
				cur -= before - after
			}
			started = false
		} else if !started {
			started = true
			start = cur
		}
	}
	if started {
		sub := stripInlineContent(append([]Styleable(nil), content[start:cur]...))
		content = append(append([]Styleable(nil), content[:start]...), sub...)
	}
	return content
}

func isLayedOutInInlineContext(s *Style) bool {
	if s.IsFloated() || s.IsAbsolute() || s.IsFixed() || s.IsRunning() {
		return true
	}
	d := s.Ident("display")
	return d == "inline" || d == "inline-block" || d == "inline-table"
}

func (d *Doc) resolveTableContent(lc *LayoutContext, parent *Box, children []Styleable, info *childBoxInfo) {
	parentDisplay := parent.Style.Ident("display")
	next := nextTableNestingLevel(parentDisplay)
	if next == "" && parent.anonymous && containsOrphanedTableContent(children) {
		d.resolveChildTableContent(lc, parent, children, info, "table-cell")
	} else if next == "" || isAllProperTableNesting(parentDisplay, children) {
		if parent.anonymous {
			rebalanceInlineContent(children)
		}
		d.resolveChildren(lc, parent, children, info)
	} else {
		var forAnon []Styleable
		var withAnon []Styleable
		for _, ch := range children {
			cd := ch.GetStyle().Ident("display")
			if isProperTableNesting(parentDisplay, cd) {
				if len(forAnon) > 0 {
					d.createAnonymousTableContent(lc, parent, next, forAnon, &withAnon)
					forAnon = nil
				}
				withAnon = append(withAnon, ch)
			} else {
				forAnon = append(forAnon, ch)
			}
		}
		if len(forAnon) > 0 {
			d.createAnonymousTableContent(lc, parent, next, forAnon, &withAnon)
		}
		info.containsBlockLevel = true
		d.resolveChildren(lc, parent, withAnon, info)
	}
}

func containsOrphanedTableContent(children []Styleable) bool {
	for _, c := range children {
		switch c.GetStyle().Ident("display") {
		case "table-header-group", "table-row-group", "table-footer-group", "table-row":
			return true
		}
	}
	return false
}

func isParentInline(b *Box) bool {
	ps := b.Style.parent
	return ps != nil && ps.IsInline()
}

func (d *Doc) createAnonymousTableContent(lc *LayoutContext, source *Box, next string, forAnon []Styleable, withAnon *[]Styleable) {
	nested := lookForBlockContent(forAnon)
	anonDisplay := next
	if isParentInline(source) && next == "table" {
		anonDisplay = "inline-table"
	}
	anonStyle := source.Style.CreateAnonymousStyle(anonDisplay)
	anonBox := createBlockBox(anonStyle, nested, false)
	anonBox.setStyle(anonStyle)
	anonBox.anonymous = true
	anonBox.Elem = source.Elem
	d.resolveTableContent(lc, anonBox, forAnon, nested)
	if next == "table" {
		*withAnon = append(*withAnon, d.reorderTableContent(lc, anonBox))
	} else {
		*withAnon = append(*withAnon, anonBox)
	}
}

func lookForBlockContent(s []Styleable) *childBoxInfo {
	r := &childBoxInfo{}
	for _, x := range s {
		if !isLayedOutInInlineContext(x.GetStyle()) {
			r.containsBlockLevel = true
			break
		}
	}
	return r
}

func (d *Doc) reorderTableContent(lc *LayoutContext, table *Box) *Box {
	var topCaptions, bodies, bottomCaptions []*Box
	var header, footer *Box
	for _, b := range table.children {
		display := b.Style.Ident("display")
		switch {
		case display == "table-caption":
			if b.Style.Ident("caption-side") == "bottom" {
				bottomCaptions = append(bottomCaptions, b)
			} else {
				topCaptions = append(topCaptions, b)
			}
		case display == "table-header-group" && header == nil:
			header = b
		case display == "table-footer-group" && footer == nil:
			footer = b
		default:
			bodies = append(bodies, b)
		}
	}
	table.removeAllChildren()
	if header != nil {
		header.header = true
		table.addChild(header)
	}
	table.addAllChildren(bodies)
	if footer != nil {
		footer.footer = true
		table.addChild(footer)
	}
	if len(topCaptions) == 0 && len(bottomCaptions) == 0 {
		return table
	}
	var anonStyle *Style
	if table.Style.IsFloated() {
		anonStyle = table.Style.Derive(map[string]*Declared{
			"display": {Name: "display", Vals: []Value{{Kind: VIdent, Str: "block"}}},
			"float":   {Name: "float", Vals: []Value{{Kind: VIdent, Str: table.Style.Ident("float")}}},
		})
	} else {
		anonStyle = table.Style.CreateAnonymousStyle("block")
	}
	anon := newBox(KBlock)
	anon.setStyle(anonStyle)
	anon.anonymous = true
	anon.fromCaptionedTbl = true
	anon.Elem = table.Elem
	anon.childrenType = ContentBlock
	anon.addAllChildren(topCaptions)
	anon.addChild(table)
	anon.addAllChildren(bottomCaptions)
	if table.Style.IsFloated() {
		anon.floated = &floatedData{}
		table.floated = nil
		// the table itself must not float any more
		orig := map[string]*Declared{}
		for k, v := range table.Style.decls {
			orig[k] = v
		}
		orig["float"] = &Declared{Name: "float", Vals: []Value{{Kind: VIdent, Str: "none"}}}
		table.setStyle(table.Style.parent.Derive(orig))
	}
	return anon
}

func createBlockBox(style *Style, info *childBoxInfo, generated bool) *Box {
	switch {
	case style.IsFloated() && !(style.IsAbsolute() || style.IsFixed()):
		var r *Box
		if style.IsTable() || style.IsInlineTable() {
			r = newBox(KTable)
		} else {
			r = newBox(KBlock)
		}
		r.floated = &floatedData{}
		return r
	case style.IsSpecifiedAsBlock():
		return newBox(KBlock)
	case !generated && (style.IsTable() || style.IsInlineTable()):
		return newBox(KTable)
	case style.IsTableCell():
		info.containsTable = true
		return newBox(KCell)
	case !generated && style.IsTableRow():
		info.containsTable = true
		return newBox(KRow)
	case !generated && style.IsTableSection():
		info.containsTable = true
		return newBox(KSection)
	case style.IsTableCaption():
		info.containsTable = true
		return newBox(KBlock)
	}
	return newBox(KBlock)
}

func (d *Doc) addColumns(table *Box, parent *TableColumn) {
	found := false
	for w := parent.elem.Children; len(w) > 0; w = w[1:] {
		n := w[0]
		if isElem(n) {
			st := d.style(n)
			if st.IsIdent("display", "table-column") {
				found = true
				col := &TableColumn{elem: n, style: st, parent: parent}
				table.styleColumns = append(table.styleColumns, col)
			}
		}
	}
	if !found {
		table.styleColumns = append(table.styleColumns, parent)
	}
}

func (d *Doc) addColumnOrColumnGroup(table *Box, e *xmldom.Node, st *Style) {
	if st.IsIdent("display", "table-column") {
		table.styleColumns = append(table.styleColumns, &TableColumn{elem: e, style: st})
	} else {
		d.addColumns(table, &TableColumn{elem: e, style: st})
	}
}

func createInlineBox(text string, parent *xmldom.Node, parentStyle *Style, node *xmldom.Node) *InlineSrc {
	r := &InlineSrc{text: text, originalText: text, textNode: node}
	if parentStyle.IsInline() && !(parent.Parent != nil && parent.Parent.Type == xmldom.DocumentNode) {
		r.style = parentStyle
		r.elem = parent
	} else {
		r.style = parentStyle.CreateAnonymousStyle("inline")
	}
	r.applyTextTransform()
	return r
}

func (s *InlineSrc) applyTextTransform() {
	s.text = transformText(s.originalText, s.style)
}

func (s *InlineSrc) truncateText() {
	s.text = ""
	s.originalText = ""
}

func (d *Doc) createChildren(lc *LayoutContext, blockParent *Box, parent *xmldom.Node, children *[]Styleable, info *childBoxInfo, inline bool) {
	parentStyle := d.style(parent)
	d.insertGeneratedContent(lc, parent, parentStyle, "before", children, info)
	needStartText := inline
	needEndText := inline
	var previousIB *InlineSrc
	for _, working := range parent.Children {
		var child Styleable
		switch working.Type {
		case xmldom.ElementNode:
			style := d.style(working)
			if style.IsDisplayNone() {
				continue
			}
			var start *int
			if working.Name == "ol" {
				if v, ok := working.AttrOK("start"); ok {
					if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
						n--
						start = &n
					}
				}
			} else if working.Name == "li" {
				if v, ok := working.AttrOK("value"); ok {
					if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
						n--
						start = &n
					}
				}
			}
			lc.resolveCounters(style, start)
			if style.IsIdent("display", "table-column") || style.IsIdent("display", "table-column-group") {
				if blockParent != nil && (blockParent.Style.IsTable() || blockParent.Style.IsInlineTable()) {
					d.addColumnOrColumnGroup(blockParent, working, style)
				}
				continue
			}
			if style.IsInline() {
				if needStartText {
					needStartText = false
					ib := createInlineBox("", parent, parentStyle, nil)
					ib.startsHere = true
					ib.endsHere = false
					*children = append(*children, ib)
					previousIB = ib
				}
				d.createChildren(lc, nil, working, children, info, true)
				if inline {
					if previousIB != nil {
						previousIB.endsHere = false
					}
					needEndText = true
				}
			} else {
				cb := createBlockBox(style, info, false)
				cb.setStyle(style)
				cb.Elem = working
				if style.IsListItem() {
					cb.listCounter = lc.counterContext(style).currentCounterValue("list-item")
				}
				var asChild *Box = cb
				if style.IsTable() || style.IsInlineTable() {
					cb.ensureChildren(lc)
					asChild = d.reorderTableContent(lc, cb)
				}
				if !info.containsBlockLevel && !isLayedOutInInlineContext(style) {
					info.containsBlockLevel = true
				}
				blk := asChild
				_ = blk
				cb.ensureChildren(lc)
				child = asChild
			}
		case xmldom.TextNode:
			needStartText = false
			needEndText = false
			ib := createInlineBox(working.Value, parent, parentStyle, working)
			ib.endsHere = true
			if previousIB == nil {
				ib.startsHere = true
			} else {
				previousIB.endsHere = false
			}
			previousIB = ib
			child = ib
		}
		if child != nil {
			*children = append(*children, child)
		}
	}
	if needStartText || needEndText {
		ib := createInlineBox("", parent, parentStyle, nil)
		ib.startsHere = needStartText
		ib.endsHere = needEndText
		*children = append(*children, ib)
	}
	d.insertGeneratedContent(lc, parent, parentStyle, "after", children, info)
}

func (d *Doc) insertGeneratedContent(lc *LayoutContext, e *xmldom.Node, parentStyle *Style, pe string, children *[]Styleable, info *childBoxInfo) {
	peStyle := d.pseudoStyle(e, pe)
	if peStyle == nil {
		return
	}
	contentDecl := peStyle["content"]
	resetDecl := peStyle["counter-reset"]
	incrDecl := peStyle["counter-increment"]
	var calc *Style
	if contentDecl != nil || resetDecl != nil || incrDecl != nil {
		calc = parentStyle.Derive(peStyle)
		if calc.IsDisplayNone() {
			return
		}
		if calc.IsIdent("content", "none") {
			return
		}
		if calc.IsIdent("content", "normal") && (pe == "before" || pe == "after") {
			return
		}
		if calc.IsTable() || calc.IsTableRow() || calc.IsTableSection() {
			ps2 := map[string]*Declared{}
			for k, v := range peStyle {
				ps2[k] = v
			}
			ps2["display"] = &Declared{Name: "display", Vals: []Value{{Kind: VIdent, Str: "block"}}}
			calc = parentStyle.Derive(ps2)
		}
		lc.resolveCounters(calc, nil)
	}
	if contentDecl != nil {
		*children = append(*children, d.createGeneratedContent(lc, e, pe, calc, contentDecl.Vals, info)...)
	}
}

func (d *Doc) createGeneratedContent(lc *LayoutContext, e *xmldom.Node, pe string, style *Style, content []Value, info *childBoxInfo) []Styleable {
	if style.IsDisplayNone() || style.IsIdent("display", "table-column") || style.IsIdent("display", "table-column-group") {
		return nil
	}
	inlineBoxes := d.createGeneratedContentList(lc, e, content, pe, style, false, nil)
	if style.IsInline() {
		var out []Styleable
		for _, ib := range inlineBoxes {
			if s, ok := ib.(*InlineSrc); ok {
				s.style = style
				s.applyTextTransform()
			}
			out = append(out, ib)
		}
		return out
	}
	anon := style.CreateAnonymousStyle("inline")
	for _, x := range inlineBoxes {
		if s, ok := x.(*InlineSrc); ok {
			s.style = anon
			s.applyTextTransform()
			s.elem = nil
		}
	}
	res := createBlockBox(style, info, true)
	res.setStyle(style)
	res.setInlineContent(inlineBoxes)
	res.Elem = e
	res.childrenType = ContentInline
	res.pseudo = pe
	if !isLayedOutInInlineContext(style) {
		info.containsBlockLevel = true
	}
	return []Styleable{res}
}

func (d *Doc) createGeneratedContentList(lc *LayoutContext, e *xmldom.Node, values []Value, pe string, style *Style, marginBox bool, info *childBoxInfo) []Styleable {
	var result []Styleable
	for _, v := range values {
		var cf *contentFunc
		var content string
		has := false
		switch v.Kind {
		case VString:
			content, has = v.Str, true
		case VFunc:
			if !marginBox && v.Str == "attr" && len(v.Args) == 1 && v.Args[0].Kind == VIdent {
				content, has = e.Attr(v.Args[0].Raw), true
				break
			}
			if !marginBox {
				if txt, ok := d.makeCounterFunction(lc, v, style); ok {
					content, has = txt, true
					break
				}
			}
			if marginBox && isElementFunction(v) {
				if t := d.getRunningBlock(lc, v); t != nil {
					result = append(result, t.copyOf())
					info.containsBlockLevel = true
				}
				break
			}
			if f := lookupContentFunction(v); f != nil {
				cf = f
				content, has = "999", true
			}
		case VIdent:
			// open-quote / close-quote not supported (quotes: none)
		}
		if has {
			ib := &InlineSrc{text: content, originalText: content, elem: e, pseudo: pe, startsHere: true, endsHere: true}
			ib.contentFunction = cf
			result = append(result, ib)
		}
	}
	return result
}

func isElementFunction(v Value) bool {
	if v.Kind != VFunc || v.Str != "element" {
		return false
	}
	if len(v.Args) < 1 || len(v.Args) > 2 {
		return false
	}
	for _, a := range v.Args {
		if a.Kind != VIdent {
			return false
		}
	}
	return true
}

func lookupContentFunction(v Value) *contentFunc {
	if v.Kind != VFunc || v.Str != "counter" {
		return nil
	}
	if len(v.Args) < 1 || len(v.Args) > 2 || v.Args[0].Kind != VIdent {
		return nil
	}
	name := v.Args[0].Str
	if name != "page" && name != "pages" {
		return nil
	}
	ls := "decimal"
	if len(v.Args) == 2 {
		if v.Args[1].Kind != VIdent {
			return nil
		}
		ls = v.Args[1].Str
	}
	return &contentFunc{kind: name, style: ls}
}

func (d *Doc) makeCounterFunction(lc *LayoutContext, v Value, style *Style) (string, bool) {
	switch v.Str {
	case "counter":
		if len(v.Args) < 1 || len(v.Args) > 2 || v.Args[0].Kind != VIdent {
			return "", false
		}
		name := v.Args[0].Str
		if name == "page" || name == "pages" {
			return "", false
		}
		ls := "decimal"
		if len(v.Args) == 2 {
			if v.Args[1].Kind != VIdent {
				return "", false
			}
			ls = v.Args[1].Str
		}
		cc := lc.counterContext(style)
		if cc == nil {
			return "", false
		}
		return createCounterText(ls, cc.currentCounterValue(name)), true
	case "counters":
		if len(v.Args) < 2 || len(v.Args) > 3 || v.Args[0].Kind != VIdent || v.Args[1].Kind != VString {
			return "", false
		}
		name, sep := v.Args[0].Str, v.Args[1].Str
		ls := "decimal"
		if len(v.Args) == 3 {
			if v.Args[2].Kind != VIdent {
				return "", false
			}
			ls = v.Args[2].Str
		}
		cc := lc.counterContext(style)
		if cc == nil {
			return "", false
		}
		vals := cc.currentCounterValues(name)
		parts := make([]string, len(vals))
		for i, x := range vals {
			parts[i] = createCounterText(ls, x)
		}
		return strings.Join(parts, sep), true
	}
	return "", false
}

func (d *Doc) getRunningBlock(lc *LayoutContext, v Value) *Box {
	ident := v.Args[0].Str
	pos := "first"
	if len(v.Args) == 2 {
		pos = v.Args[1].Str
	}
	return lc.rootDocLayer.runningBlock(ident, lc.page, pos)
}

// copyOf is BlockBox.copyOf: a childless copy that rebuilds its content.
func (b *Box) copyOf() *Box {
	var r *Box
	switch b.Kind {
	case KTable:
		r = newBox(KTable)
	case KSection:
		r = newBox(KSection)
	case KRow:
		r = newBox(KRow)
	case KCell:
		r = newBox(KCell)
	default:
		r = newBox(KBlock)
	}
	r.setStyle(b.Style)
	r.Elem = b.Elem
	return r
}

func (d *Doc) insertAnonymousBlocks(parent *Box, children []Styleable, layoutRunning bool) {
	var inline []Styleable
	var parents []*InlineSrc
	var saved []*InlineSrc
	for _, ch := range children {
		st := ch.GetStyle()
		if isLayedOutInInlineContext(st) && !(layoutRunning && st.IsRunning()) {
			inline = append(inline, ch)
			if st.IsInline() {
				ib := ch.(*InlineSrc)
				if ib.startsHere {
					parents = append(parents, ib)
				}
				if ib.endsHere {
					parents = parents[:len(parents)-1]
				}
			}
		} else {
			if len(inline) > 0 {
				createAnonymousBlock(parent, inline, saved)
				inline = nil
				saved = append([]*InlineSrc(nil), parents...)
			}
			parent.addChild(ch.(*Box))
		}
	}
	createAnonymousBlock(parent, inline, saved)
}

func createAnonymousBlock(parent *Box, inline []Styleable, saved []*InlineSrc) {
	inline = stripInlineContent(inline)
	if len(inline) > 0 {
		anon := newBox(KAnon)
		anon.Elem = parent.Elem
		anon.setStyle(parent.Style.CreateAnonymousStyle("block"))
		anon.anonymous = true
		if len(saved) > 0 {
			anon.openInlineBoxes = saved
		}
		parent.addChild(anon)
		anon.childrenType = ContentInline
		anon.setInlineContent(inline)
	}
}

// ---- WhitespaceStripper ----------------------------------------------------------

var (
	reLinefeedSpace = regexp.MustCompile(`[ \t\n\x0B\f\r]+\n[ \t\n\x0B\f\r]+`)
	reSpaceBeforeLF = regexp.MustCompile(`[ \t\x0B\f\r]\n`)
	reSpaceCollapse = regexp.MustCompile(` +`)
)

// stripInlineContent collapses white space; it returns the (possibly shortened) list.
func stripInlineContent(content []Styleable) []Styleable {
	collapse := false
	allWS := true
	for _, n := range content {
		st := n.GetStyle()
		if st.IsInline() {
			ib := n.(*InlineSrc)
			next := stripWhitespace(ib, collapse)
			if !ib.removableWS {
				allWS = false
			}
			collapse = next
		} else if !canCollapseThrough(st) {
			allWS = false
			collapse = false
		}
	}
	if allWS {
		return stripTextContent(content)
	}
	return content
}

func canCollapseThrough(s *Style) bool {
	return s.IsFloated() || s.IsAbsolute() || s.IsFixed() || s.IsRunning()
}

func stripTextContent(stripped []Styleable) []Styleable {
	onlyAnon := true
	for _, n := range stripped {
		if n.GetStyle().IsInline() {
			ib := n.(*InlineSrc)
			if ib.elem != nil {
				onlyAnon = false
			}
			ib.truncateText()
		}
	}
	if onlyAnon {
		out := stripped[:0:0]
		for _, n := range stripped {
			if n.GetStyle().IsInline() {
				continue
			}
			out = append(out, n)
		}
		return out
	}
	return stripped
}

func javaTrim(s string) string {
	return strings.TrimFunc(s, func(r rune) bool { return r <= ' ' })
}

func stripWhitespace(ib *InlineSrc, collapseLeading bool) bool {
	ws := ib.style.Ident("white-space")
	text := ib.text
	text = collapseWhitespace(ib, ws, text, collapseLeading)
	collapseNext := strings.HasSuffix(text, " ") && (ws == "normal" || ws == "nowrap" || ws == "pre")
	ib.text = text
	ib.originalText = text
	if javaTrim(text) == "" {
		if ws == "normal" || ws == "nowrap" {
			ib.removableWS = true
		} else if ws == "pre" {
			ib.removableWS = false
		} else if !strings.Contains(text, "\n") {
			ib.removableWS = true
		}
	}
	if text == "" {
		return collapseLeading
	}
	return collapseNext
}

func collapseWhitespace(ib *InlineSrc, ws string, text string, collapseLeading bool) string {
	if ws == "normal" || ws == "nowrap" {
		text = reLinefeedSpace.ReplaceAllString(text, "\n")
	} else if ws == "pre" {
		text = reSpaceBeforeLF.ReplaceAllString(text, "\n")
	}
	switch ws {
	case "normal", "nowrap":
		text = strings.ReplaceAll(text, "\n", " ")
		text = strings.ReplaceAll(text, "\t", " ")
		text = reSpaceCollapse.ReplaceAllString(text, " ")
	case "pre", "pre-wrap":
		n := int(ib.style.AsFloat("tab-size"))
		text = strings.ReplaceAll(text, "\t", strings.Repeat(" ", n))
	case "pre-line":
		text = strings.ReplaceAll(text, "\t", " ")
		text = reSpaceCollapse.ReplaceAllString(text, " ")
	}
	if ws == "normal" || ws == "nowrap" {
		if strings.HasPrefix(text, " ") && collapseLeading {
			text = text[1:]
		}
	}
	return text
}

// transformText is TextUtil.transformText.
func transformText(text string, st *Style) string {
	switch st.Ident("text-transform") {
	case "lowercase":
		text = strings.ToLower(text)
	case "uppercase":
		text = strings.ToUpper(text)
	case "capitalize":
		text = capitalizeWords(text)
	}
	if st.Ident("font-variant") == "small-caps" {
		text = strings.ToUpper(text)
	}
	return text
}

func capitalizeWords(text string) string {
	var sb strings.Builder
	cap := true
	for _, r := range text {
		if cap {
			sb.WriteRune(unicode.ToUpper(r))
		} else {
			sb.WriteRune(r)
		}
		cap = r == ' '
	}
	return sb.String()
}

// ---- counters ---------------------------------------------------------------

type counterContext struct {
	counters map[string]int
	parent   *counterContext
}

func (lc *LayoutContext) counterContext(st *Style) *counterContext { return lc.doc.counter[st] }

type counterData struct {
	name  string
	value int
}

func parseCounterList(v *dv, defVal int) []counterData {
	if len(v.vals) == 0 || (len(v.vals) == 1 && v.vals[0].Kind == VIdent && v.vals[0].Str == "none") {
		return nil
	}
	var out []counterData
	for i := 0; i < len(v.vals); i++ {
		if v.vals[i].Kind != VIdent {
			continue
		}
		cd := counterData{name: v.vals[i].Str, value: defVal}
		if i+1 < len(v.vals) && v.vals[i+1].Kind == VNumber {
			cd.value = int(v.vals[i+1].Num)
			i++
		}
		out = append(out, cd)
	}
	return out
}

func (lc *LayoutContext) resolveCounters(st *Style, start *int) {
	cc := &counterContext{counters: map[string]int{}}
	if start != nil {
		cc.counters["list-item"] = *start
	}
	parent := lc.doc.counter[st.parent]
	if parent == nil {
		parent = &counterContext{counters: map[string]int{}}
	}
	cc.parent = parent
	for _, cd := range parseCounterList(st.value("counter-reset"), 0) {
		parent.resetCounter(cd)
	}
	for _, cd := range parseCounterList(st.value("counter-increment"), 1) {
		if !parent.incrementCounter(cd) {
			parent.resetCounter(counterData{cd.name, 0})
			parent.incrementCounter(cd)
		}
	}
	if st.IsIdent("display", "list-item") {
		if start != nil {
			parent.counters["list-item"] = *start
		}
		parent.incrementListItem(1)
	}
	lc.doc.counter[st] = cc
}

func (c *counterContext) incrementCounter(cd counterData) bool {
	if cd.name == "list-item" {
		c.incrementListItem(cd.value)
		return true
	}
	if v, ok := c.counters[cd.name]; ok {
		c.counters[cd.name] = v + cd.value
		return true
	}
	if c.parent == nil {
		return false
	}
	return c.parent.incrementCounter(cd)
}

func (c *counterContext) incrementListItem(inc int)   { c.counters["list-item"] += inc }
func (c *counterContext) resetCounter(cd counterData) { c.counters[cd.name] = cd.value }

func (c *counterContext) getCounter(name string) (int, bool) {
	if v, ok := c.counters[name]; ok {
		return v, true
	}
	if c.parent == nil {
		return 0, false
	}
	return c.parent.getCounter(name)
}

func (c *counterContext) currentCounterValue(name string) int {
	v, ok := c.parent.getCounter(name)
	if !ok {
		c.parent.resetCounter(counterData{name, 0})
		return 0
	}
	return v
}

func (c *counterContext) getCounterValues(name string, out *[]int) {
	if c.parent != nil {
		c.parent.getCounterValues(name, out)
	}
	if v, ok := c.counters[name]; ok {
		*out = append(*out, v)
	}
}

func (c *counterContext) currentCounterValues(name string) []int {
	var vals []int
	c.parent.getCounterValues(name, &vals)
	if len(vals) == 0 {
		c.parent.resetCounter(counterData{name, 0})
		vals = append(vals, 0)
	}
	return vals
}

func createCounterText(ls string, n int) string {
	switch ls {
	case "lower-latin", "lower-alpha":
		return strings.ToLower(toLatin(n))
	case "upper-latin", "upper-alpha":
		return strings.ToUpper(toLatin(n))
	case "lower-roman":
		return strings.ToLower(toRoman(n))
	case "upper-roman":
		return strings.ToUpper(toRoman(n))
	case "decimal-leading-zero":
		if n >= 10 {
			return strconv.Itoa(n)
		}
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

func toLatin(val int) string {
	res := ""
	val--
	for val >= 0 {
		letter := val % 26
		val = val/26 - 1
		res = string(rune(letter+65)) + res
	}
	return res
}

func toRoman(val int) string {
	ints := []int{1000, 900, 500, 400, 100, 90, 50, 40, 10, 9, 5, 4, 1}
	nums := []string{"M", "CM", "D", "CD", "C", "XC", "L", "XL", "X", "IX", "V", "IV", "I"}
	var sb strings.Builder
	for i := range ints {
		count := val / ints[i]
		for j := 0; j < count; j++ {
			sb.WriteString(nums[i])
		}
		val -= ints[i] * count
	}
	return sb.String()
}
