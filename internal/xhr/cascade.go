package xhr

import (
	"sort"
	"strings"

	"github.com/ProDGG/fop-go/xmldom"
)

type origin int

const (
	originUA origin = iota
	originAuthor
)

type builtRule struct {
	decls []Declared
}

type selEntry struct {
	sel    *Selector
	rule   *builtRule
	pos    int
	origin origin
	key    [4]int
}

type builtPage struct {
	name, pseudo string
	decls        []Declared
	boxes        map[string][]Declared
	order        [4]int
	origin       origin
}

// Matcher applies style sheets to elements like Flying Saucer's Matcher.
type Matcher struct {
	entries []*selEntry
	pages   []*builtPage
	pos     int
	ppos    int
}

func buildDecls(ds []Decl) []Declared {
	var out []Declared
	for _, d := range ds {
		r, ok := BuildDeclarations(d)
		if ok {
			out = append(out, r...)
		}
	}
	return out
}

// AddSheet registers a parsed sheet; sheets must be added in cascade order
// (user agent first).
func (m *Matcher) AddSheet(ss *Stylesheet, o origin) {
	for _, rs := range ss.Rules {
		br := &builtRule{decls: buildDecls(rs.Decls)}
		for _, sel := range rs.Selectors {
			m.pos++
			m.entries = append(m.entries, &selEntry{
				sel: sel, rule: br, pos: m.pos, origin: o,
				key: [4]int{sel.Spec[0], sel.Spec[1], sel.Spec[2], m.pos},
			})
		}
	}
	for _, pr := range ss.Pages {
		m.ppos++
		bp := &builtPage{name: pr.Name, pseudo: pr.Pseudo, decls: buildDecls(pr.Decls), boxes: map[string][]Declared{}, origin: o}
		for _, mb := range pr.MarginBox {
			bp.boxes[mb.Name] = buildDecls(mb.Decls)
		}
		f, g, h := 0, 0, 0
		if pr.Name != "" {
			f = 1
		}
		if pr.Pseudo == "first" {
			g = 1
		} else if pr.Pseudo != "" {
			h = 1
		}
		bp.order = [4]int{f, g, h, m.ppos}
		m.pages = append(m.pages, bp)
	}
}

// Finish sorts the selector table.
func (m *Matcher) Finish() {
	sort.SliceStable(m.entries, func(i, j int) bool {
		a, b := m.entries[i].key, m.entries[j].key
		for k := 0; k < 4; k++ {
			if a[k] != b[k] {
				return a[k] < b[k]
			}
		}
		return false
	})
	sort.SliceStable(m.pages, func(i, j int) bool {
		a, b := m.pages[i].order, m.pages[j].order
		for k := 0; k < 4; k++ {
			if a[k] != b[k] {
				return a[k] < b[k]
			}
		}
		return false
	})
}

type tagged struct {
	d      *Declared
	origin origin
}

// cascadeInto merges declarations in cascade order: UA, author normal, author
// important (Flying Saucer buckets).
func cascadeInto(list []tagged) map[string]*Declared {
	res := map[string]*Declared{}
	for _, t := range list {
		if t.origin == originUA {
			res[t.d.Name] = t.d
		}
	}
	for _, t := range list {
		if t.origin == originAuthor && !t.d.Important {
			res[t.d.Name] = t.d
		}
	}
	for _, t := range list {
		if t.origin == originAuthor && t.d.Important {
			res[t.d.Name] = t.d
		}
	}
	return res
}

func declsOf(rule []Declared, o origin) []tagged {
	out := make([]tagged, len(rule))
	for i := range rule {
		out[i] = tagged{&rule[i], o}
	}
	return out
}

// Cascaded returns the cascaded declarations of an element; pseudo is "" for
// the element itself.
func (m *Matcher) Cascaded(e *xmldom.Node, pseudo string, nonCSS, styleAttr []Declared) map[string]*Declared {
	var list []tagged
	if pseudo == "" {
		list = append(list, declsOf(nonCSS, originAuthor)...)
	}
	for _, en := range m.entries {
		if en.sel.Pseudo != pseudo {
			continue
		}
		if !en.sel.Matches(e) {
			continue
		}
		list = append(list, declsOf(en.rule.decls, en.origin)...)
	}
	if pseudo == "" {
		list = append(list, declsOf(styleAttr, originAuthor)...)
	}
	if len(list) == 0 {
		return nil
	}
	return cascadeInto(list)
}

// HasPseudo reports whether any rule could apply to the pseudo element of e.
func (m *Matcher) HasPseudo(e *xmldom.Node, pseudo string) bool {
	for _, en := range m.entries {
		if en.sel.Pseudo == pseudo && en.sel.Matches(e) {
			return true
		}
	}
	return false
}

// PageInfo is the cascaded @page information for a page.
type PageInfo struct {
	Style map[string]*Declared
	Boxes map[string][]Declared
	Decls []Declared
}

func pageApplies(p *builtPage, pageName, pseudo string) bool {
	switch {
	case p.name == "" && p.pseudo == "":
		return true
	case p.name == "" && p.pseudo != "":
		return p.pseudo == pseudo || (p.pseudo == "right" && pseudo == "first")
	case p.name != "" && p.name == pageName && p.pseudo == "":
		return true
	case p.name != "" && p.name == pageName && p.pseudo == pseudo:
		return true
	}
	return false
}

// Page mirrors Matcher.getPageCascadedStyle.
func (m *Matcher) Page(pageName, pseudo string) *PageInfo {
	var list []tagged
	boxes := map[string][]Declared{}
	var all []Declared
	for _, p := range m.pages {
		if !pageApplies(p, pageName, pseudo) {
			continue
		}
		list = append(list, declsOf(p.decls, p.origin)...)
		all = append(all, p.decls...)
		for k, v := range p.boxes {
			boxes[k] = v
		}
	}
	return &PageInfo{Style: cascadeInto(list), Boxes: boxes, Decls: all}
}

// HasAny reports whether any of the margin boxes is defined.
func (pi *PageInfo) HasAny(names []string) bool {
	for _, n := range names {
		if _, ok := pi.Boxes[n]; ok {
			return true
		}
	}
	return false
}

var marginBoxDefaults = map[string][2]string{ // text-align, vertical-align
	"top-left-corner":     {"right", "middle"},
	"top-left":            {"left", "middle"},
	"top-center":          {"center", "middle"},
	"top-right":           {"right", "middle"},
	"top-right-corner":    {"left", "middle"},
	"bottom-left-corner":  {"right", "middle"},
	"bottom-left":         {"left", "middle"},
	"bottom-center":       {"center", "middle"},
	"bottom-right":        {"right", "middle"},
	"bottom-right-corner": {"left", "middle"},
	"left-top":            {"center", "top"},
	"left-middle":         {"center", "middle"},
	"left-bottom":         {"center", "bottom"},
	"right-top":           {"center", "top"},
	"right-middle":        {"center", "middle"},
	"right-bottom":        {"center", "bottom"},
}

// MarginBoxStyle mirrors PageInfo.createMarginBoxStyle (nil if absent and
// !alwaysCreate).
func (pi *PageInfo) MarginBoxStyle(name string, alwaysCreate bool) map[string]*Declared {
	props, ok := pi.Boxes[name]
	if (!ok || len(props) == 0) && !alwaysCreate {
		return nil
	}
	var list []tagged
	list = append(list, declsOf(props, originAuthor)...)
	disp := Declared{Name: "display", Vals: []Value{{Kind: VIdent, Str: "table-cell"}}, Important: true}
	def := marginBoxDefaults[name]
	va := Declared{Name: "vertical-align", Vals: []Value{{Kind: VIdent, Str: def[1]}}}
	ta := Declared{Name: "text-align", Vals: []Value{{Kind: VIdent, Str: def[0]}}}
	list = append(list, tagged{&disp, originAuthor}, tagged{&va, originUA}, tagged{&ta, originUA})
	return cascadeInto(list)
}

// ParseStyleAttr parses declaration text such as a style attribute.
func ParseStyleAttr(text string) []Declared {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	l := &cssLexer{s: []rune(text + "}")}
	return buildDecls(l.parseDecls())
}
