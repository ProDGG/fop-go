// Package xslt implements an XSLT 1.0 processor (plus a few widely used
// XSLT 2.0 conveniences) on top of the xmldom and xpath packages.
package xslt

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/ProDGG/fop-go/xmldom"
	"github.com/ProDGG/fop-go/xpath"
)

// Resolver loads documents referenced from a stylesheet (xsl:include,
// xsl:import, document()).
type Resolver interface {
	// Resolve loads href relative to base and returns the content and the
	// resolved location (used as the base for nested references).
	Resolve(href, base string) (data []byte, location string, err error)
}

// FileResolver resolves hrefs against the local file system.
type FileResolver struct{}

// Resolve implements Resolver.
func (FileResolver) Resolve(href, base string) ([]byte, string, error) {
	href = strings.TrimPrefix(href, "file://")
	p := href
	if !filepath.IsAbs(p) && base != "" {
		p = filepath.Join(filepath.Dir(base), href)
	}
	data, err := os.ReadFile(p)
	return data, p, err
}

// Options configure stylesheet compilation.
type Options struct {
	Resolver Resolver
	BaseURI  string // location of the main stylesheet
}

// OutputProps holds the xsl:output settings.
type OutputProps struct {
	Method   string
	Indent   bool
	Encoding string
}

// DecimalFormat is an xsl:decimal-format declaration.
type DecimalFormat struct {
	DecimalSep, GroupSep       rune
	Infinity, NaN              string
	MinusSign, Percent, PerMil rune
	ZeroDigit, Digit, PatSep   rune
}

func defaultDecimalFormat() *DecimalFormat {
	return &DecimalFormat{'.', ',', "Infinity", "NaN", '-', '%', '‰', '0', '#', ';'}
}

// Template is a compiled xsl:template.
type Template struct {
	name     string
	mode     string
	priority float64
	prec     int
	order    int
	params   []*paramDecl
	body     *block
	el       *xmldom.Node
	alt      *xpath.PatternAlt // the alternative of the match pattern (nil for named-only)
	pattern  *xpath.Pattern
}

type paramDecl struct {
	name  string // expanded name key
	qname string
	sel   *xpath.Expr
	body  *block
}

type globalVar struct {
	decl    *paramDecl
	isParam bool
	prec    int
	el      *xmldom.Node
}

type keyDef struct {
	match *xpath.Pattern
	use   *xpath.Expr
}

type attrSet struct {
	name string
	uses []string
	body *block
}

type spaceRule struct {
	uri, local string
	any        bool // "*"
	anyLocal   bool // prefix:*
	prec       int
	priority   float64
	order      int
	strip      bool
}

type nsAlias struct{ prefix, uri string }

// Stylesheet is a compiled XSLT stylesheet.
type Stylesheet struct {
	opts      Options
	doc       *xmldom.Node
	order     int
	nextPrec  int
	templates []*Template
	named     map[string]*Template
	modes     map[string]*modeTable
	globals   []*globalVar
	globalIdx map[string]*globalVar
	keys      map[string][]*keyDef
	attrSets  map[string]*attrSet
	formats   map[string]*DecimalFormat
	space     []spaceRule
	aliases   map[string]nsAlias // by stylesheet namespace URI
	Output    OutputProps
	loaded    map[string]bool
}

type modeTable struct {
	sorted []*Template
	mu     sync.Mutex
	cache  map[cacheKey][]*Template
}

type cacheKey struct {
	typ        xmldom.NodeType
	uri, local string
}

// Compile compiles a parsed stylesheet document.
func Compile(doc *xmldom.Node, opts Options) (*Stylesheet, error) {
	if opts.Resolver == nil {
		opts.Resolver = FileResolver{}
	}
	s := &Stylesheet{
		opts:      opts,
		doc:       doc,
		named:     map[string]*Template{},
		modes:     map[string]*modeTable{},
		globalIdx: map[string]*globalVar{},
		keys:      map[string][]*keyDef{},
		attrSets:  map[string]*attrSet{},
		formats:   map[string]*DecimalFormat{"": defaultDecimalFormat()},
		aliases:   map[string]nsAlias{},
		loaded:    map[string]bool{},
		Output:    OutputProps{Method: "xml", Encoding: "UTF-8"},
	}
	if err := s.loadModule(doc, opts.BaseURI); err != nil {
		return nil, err
	}
	s.finish()
	return s, nil
}

// CompileBytes parses and compiles a stylesheet.
func CompileBytes(data []byte, opts Options) (*Stylesheet, error) {
	doc, err := xmldom.ParseBytes(data)
	if err != nil {
		return nil, fmt.Errorf("xslt: parsing stylesheet: %w", err)
	}
	return Compile(doc, opts)
}

type cctx struct {
	s *Stylesheet
}

func isXSL(n *xmldom.Node, local string) bool {
	return n.Type == xmldom.ElementNode && n.URI == xmldom.XSLNS && n.Name == local
}

func errAt(n *xmldom.Node, f string, a ...interface{}) error {
	msg := fmt.Sprintf(f, a...)
	if n != nil && n.Line > 0 {
		return fmt.Errorf("xslt: line %d: %s", n.Line, msg)
	}
	return fmt.Errorf("xslt: %s", msg)
}

// stripStylesheetSpace removes whitespace-only text nodes that XSLT ignores.
func stripStylesheetSpace(n *xmldom.Node, preserve bool) {
	if n.Type == xmldom.ElementNode {
		if v := n.AttrNS(xmldom.XMLNS, "space"); v == "preserve" {
			preserve = true
		} else if v == "default" {
			preserve = false
		}
		if isXSL(n, "text") {
			preserve = true
		}
	}
	if !preserve {
		kept := n.Children[:0]
		for _, c := range n.Children {
			if c.Type == xmldom.TextNode && strings.TrimSpace(c.Value) == "" {
				continue
			}
			kept = append(kept, c)
		}
		n.Children = kept
	}
	for _, c := range n.Children {
		if c.Type == xmldom.ElementNode {
			stripStylesheetSpace(c, preserve)
		}
	}
}

// topLevel returns the root xsl:stylesheet element, wrapping simplified
// stylesheets (a literal result element with xsl:version).
func topLevel(doc *xmldom.Node) (*xmldom.Node, error) {
	root := doc.DocumentElement()
	if root == nil {
		return nil, fmt.Errorf("xslt: empty stylesheet")
	}
	if isXSL(root, "stylesheet") || isXSL(root, "transform") {
		return root, nil
	}
	if root.AttrNS(xmldom.XSLNS, "version") != "" {
		// simplified stylesheet: wrap in a template matching "/"
		wrap := xmldom.NewElement("xsl", "stylesheet", xmldom.XSLNS)
		wrap.Line = root.Line
		tmpl := xmldom.NewElement("xsl", "template", xmldom.XSLNS)
		tmpl.SetAttr("", "match", "", "/")
		tmpl.Line = root.Line
		wrap.NS = append(wrap.NS, root.NS...)
		wrap.NS = append(wrap.NS, xmldom.NSDecl{Prefix: "xsl", URI: xmldom.XSLNS})
		tmpl.Parent = wrap
		wrap.Children = []*xmldom.Node{tmpl}
		root.Parent = tmpl
		tmpl.Children = []*xmldom.Node{root}
		wrap.Parent = doc
		return wrap, nil
	}
	return nil, errAt(root, "root element <%s> is not xsl:stylesheet or xsl:transform", root.QName())
}

type declItem struct {
	el  *xmldom.Node
	loc string
}

// expand returns the top-level declarations of a module with includes inlined
// and imports processed (imports get lower precedence, so they are loaded first).
func (s *Stylesheet) expand(doc *xmldom.Node, loc string, depth int) ([]declItem, error) {
	if depth > 50 {
		return nil, fmt.Errorf("xslt: include/import nesting too deep")
	}
	stripStylesheetSpace(doc, false)
	root, err := topLevel(doc)
	if err != nil {
		return nil, err
	}
	var items []declItem
	for _, d := range root.Children {
		if d.Type != xmldom.ElementNode {
			continue
		}
		switch {
		case isXSL(d, "import"):
			sub, subLoc, err := s.loadHref(d, loc)
			if err != nil {
				return nil, err
			}
			if err := s.loadModule(sub, subLoc); err != nil {
				return nil, err
			}
		case isXSL(d, "include"):
			sub, subLoc, err := s.loadHref(d, loc)
			if err != nil {
				return nil, err
			}
			inner, err := s.expand(sub, subLoc, depth+1)
			if err != nil {
				return nil, err
			}
			items = append(items, inner...)
		default:
			items = append(items, declItem{d, loc})
		}
	}
	return items, nil
}

func (s *Stylesheet) loadHref(d *xmldom.Node, base string) (*xmldom.Node, string, error) {
	href := d.Attr("href")
	if href == "" {
		return nil, "", errAt(d, "xsl:%s requires href", d.Name)
	}
	data, loc, err := s.opts.Resolver.Resolve(href, base)
	if err != nil {
		return nil, "", errAt(d, "cannot load %q: %v", href, err)
	}
	doc, err := xmldom.ParseBytes(data)
	if err != nil {
		return nil, "", errAt(d, "parsing %q: %v", href, err)
	}
	return doc, loc, nil
}

func (s *Stylesheet) loadModule(doc *xmldom.Node, loc string) error {
	items, err := s.expand(doc, loc, 0)
	if err != nil {
		return err
	}
	prec := s.nextPrec
	s.nextPrec++
	for _, it := range items {
		if err := s.declare(it.el, prec); err != nil {
			return err
		}
	}
	return nil
}

func nsResolverFor(el *xmldom.Node) xpath.NSResolver {
	return func(prefix string) (string, bool) { return el.LookupNamespace(prefix) }
}

// expandedName resolves a QName attribute value to a lookup key.
func expandedName(el *xmldom.Node, qname string) (string, error) {
	qname = strings.TrimSpace(qname)
	if i := strings.IndexByte(qname, ':'); i >= 0 {
		uri, ok := el.LookupNamespace(qname[:i])
		if !ok {
			return "", errAt(el, "undeclared prefix in name %q", qname)
		}
		return "{" + uri + "}" + qname[i+1:], nil
	}
	return qname, nil
}

func (s *Stylesheet) declare(d *xmldom.Node, prec int) error {
	if d.URI != xmldom.XSLNS {
		return nil // user-defined top-level element: ignored
	}
	switch d.Name {
	case "template":
		return s.declTemplate(d, prec)
	case "variable", "param":
		pd, err := s.compileParamDecl(d)
		if err != nil {
			return err
		}
		gv := &globalVar{decl: pd, isParam: d.Name == "param", prec: prec, el: d}
		if old, ok := s.globalIdx[pd.name]; ok {
			if old.prec > prec {
				return nil
			}
			if old.prec == prec {
				return errAt(d, "duplicate global variable %q", pd.qname)
			}
			for i, g := range s.globals {
				if g == old {
					s.globals[i] = gv
				}
			}
		} else {
			s.globals = append(s.globals, gv)
		}
		s.globalIdx[pd.name] = gv
	case "key":
		name, err := expandedName(d, d.Attr("name"))
		if err != nil {
			return err
		}
		m, err := xpath.CompilePattern(d.Attr("match"), nsResolverFor(d))
		if err != nil {
			return errAt(d, "%v", err)
		}
		u, err := xpath.Compile(d.Attr("use"), nsResolverFor(d))
		if err != nil {
			return errAt(d, "%v", err)
		}
		s.keys[name] = append(s.keys[name], &keyDef{m, u})
	case "output":
		if v := d.Attr("method"); v != "" {
			s.Output.Method = v
		}
		if v := d.Attr("indent"); v != "" {
			s.Output.Indent = v == "yes"
		}
		if v := d.Attr("encoding"); v != "" {
			s.Output.Encoding = v
		}
	case "attribute-set":
		name, err := expandedName(d, d.Attr("name"))
		if err != nil {
			return err
		}
		as := &attrSet{name: name}
		for _, u := range strings.Fields(d.Attr("use-attribute-sets")) {
			en, err := expandedName(d, u)
			if err != nil {
				return err
			}
			as.uses = append(as.uses, en)
		}
		body, err := s.compileBody(d)
		if err != nil {
			return err
		}
		as.body = body
		if old, ok := s.attrSets[name]; ok {
			// merge: later declarations are appended
			old.uses = append(old.uses, as.uses...)
			old.body.instrs = append(old.body.instrs, as.body.instrs...)
		} else {
			s.attrSets[name] = as
		}
	case "decimal-format":
		name := d.Attr("name")
		df := defaultDecimalFormat()
		r := func(attr string, dst *rune) {
			if v := d.Attr(attr); v != "" {
				for _, c := range v {
					*dst = c
					break
				}
			}
		}
		r("decimal-separator", &df.DecimalSep)
		r("grouping-separator", &df.GroupSep)
		r("minus-sign", &df.MinusSign)
		r("percent", &df.Percent)
		r("per-mille", &df.PerMil)
		r("zero-digit", &df.ZeroDigit)
		r("digit", &df.Digit)
		r("pattern-separator", &df.PatSep)
		if v := d.Attr("infinity"); v != "" {
			df.Infinity = v
		}
		if v := d.Attr("NaN"); v != "" {
			df.NaN = v
		}
		s.formats[name] = df
	case "strip-space", "preserve-space":
		for _, tok := range strings.Fields(d.Attr("elements")) {
			rule := spaceRule{prec: prec, order: s.order, strip: d.Name == "strip-space"}
			s.order++
			switch {
			case tok == "*":
				rule.any = true
				rule.priority = -0.5
			case strings.HasSuffix(tok, ":*"):
				uri, ok := d.LookupNamespace(strings.TrimSuffix(tok, ":*"))
				if !ok {
					return errAt(d, "undeclared prefix in %q", tok)
				}
				rule.uri, rule.anyLocal, rule.priority = uri, true, -0.25
			default:
				rule.local = tok
				if i := strings.IndexByte(tok, ':'); i >= 0 {
					uri, ok := d.LookupNamespace(tok[:i])
					if !ok {
						return errAt(d, "undeclared prefix in %q", tok)
					}
					rule.uri, rule.local = uri, tok[i+1:]
				}
			}
			s.space = append(s.space, rule)
		}
	case "namespace-alias":
		sp, rp := d.Attr("stylesheet-prefix"), d.Attr("result-prefix")
		if sp == "#default" {
			sp = ""
		}
		if rp == "#default" {
			rp = ""
		}
		suri, ok1 := d.LookupNamespace(sp)
		ruri, ok2 := d.LookupNamespace(rp)
		if !ok1 || !ok2 {
			return errAt(d, "xsl:namespace-alias uses undeclared prefix")
		}
		s.aliases[suri] = nsAlias{prefix: rp, uri: ruri}
	case "import", "include":
		// handled in expand
	case "script", "decl", "fallback":
	default:
		return errAt(d, "unknown top-level element xsl:%s", d.Name)
	}
	return nil
}

func (s *Stylesheet) compileParamDecl(d *xmldom.Node) (*paramDecl, error) {
	qn := d.Attr("name")
	if qn == "" {
		return nil, errAt(d, "xsl:%s requires a name", d.Name)
	}
	name, err := expandedName(d, qn)
	if err != nil {
		return nil, err
	}
	pd := &paramDecl{name: name, qname: qn}
	if sel, ok := d.AttrOK("select"); ok {
		pd.sel, err = xpath.Compile(sel, nsResolverFor(d))
		if err != nil {
			return nil, errAt(d, "%v", err)
		}
	} else if len(d.Children) > 0 {
		pd.body, err = s.compileBody(d)
		if err != nil {
			return nil, err
		}
	}
	return pd, nil
}

func (s *Stylesheet) declTemplate(d *xmldom.Node, prec int) error {
	var name string
	if v := d.Attr("name"); v != "" {
		var err error
		name, err = expandedName(d, v)
		if err != nil {
			return err
		}
	}
	match, hasMatch := d.AttrOK("match")
	if name == "" && !hasMatch {
		return errAt(d, "xsl:template needs a name or match attribute")
	}
	mode := ""
	if v := d.Attr("mode"); v != "" {
		var err error
		mode, err = expandedName(d, v)
		if err != nil {
			return err
		}
	}
	// params at the start of the body
	body := &xmldom.Node{Type: xmldom.ElementNode, Name: d.Name, URI: d.URI, Line: d.Line, Parent: d.Parent}
	var params []*paramDecl
	i := 0
	for ; i < len(d.Children); i++ {
		c := d.Children[i]
		if !isXSL(c, "param") {
			break
		}
		pd, err := s.compileParamDecl(c)
		if err != nil {
			return err
		}
		params = append(params, pd)
	}
	body.Children = d.Children[i:]
	body.NS = d.NS
	cb, err := s.compileBody(body)
	if err != nil {
		return err
	}
	base := Template{name: name, mode: mode, prec: prec, params: params, body: cb, el: d}
	if name != "" {
		t := base
		t.order = s.order
		s.order++
		if old, ok := s.named[name]; ok && old.prec > prec {
			// lower precedence: ignore
		} else if ok && old.prec == prec {
			return errAt(d, "duplicate named template %q", name)
		} else {
			s.named[name] = &t
		}
	}
	if hasMatch {
		pat, err := xpath.CompilePattern(match, nsResolverFor(d))
		if err != nil {
			return errAt(d, "%v", err)
		}
		var explicit *float64
		if pv := d.Attr("priority"); pv != "" {
			f, err := strconv.ParseFloat(strings.TrimSpace(pv), 64)
			if err != nil {
				return errAt(d, "bad priority %q", pv)
			}
			explicit = &f
		}
		for _, alt := range pat.Alts {
			t := base
			t.order = s.order
			s.order++
			t.alt = alt
			t.pattern = pat
			t.priority = alt.Priority()
			if explicit != nil {
				t.priority = *explicit
			}
			tt := t
			s.templates = append(s.templates, &tt)
		}
	}
	return nil
}

// finish builds the per-mode dispatch tables.
func (s *Stylesheet) finish() {
	byMode := map[string][]*Template{}
	for _, t := range s.templates {
		byMode[t.mode] = append(byMode[t.mode], t)
	}
	for mode, list := range byMode {
		sort.SliceStable(list, func(i, j int) bool {
			a, b := list[i], list[j]
			if a.prec != b.prec {
				return a.prec > b.prec
			}
			if a.priority != b.priority {
				return a.priority > b.priority
			}
			return a.order > b.order
		})
		s.modes[mode] = &modeTable{sorted: list, cache: map[cacheKey][]*Template{}}
	}
}

// candidates returns the templates of a mode that could match nodes of the
// given kind and name, in priority order.
func (m *modeTable) candidates(n *xmldom.Node) []*Template {
	key := cacheKey{typ: n.Type}
	if n.Type == xmldom.ElementNode || n.Type == xmldom.AttributeNode {
		key.uri, key.local = n.URI, n.Name
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.cache[key]; ok {
		return c
	}
	var out []*Template
	for _, t := range m.sorted {
		typ, uri, local, anyName := t.alt.NodeKind()
		if typ != 0 && typ != n.Type {
			continue
		}
		if !anyName && (n.URI != uri || n.Name != local) {
			continue
		}
		if anyName && uri != "" && n.URI != uri {
			continue
		}
		out = append(out, t)
	}
	m.cache[key] = out
	return out
}
