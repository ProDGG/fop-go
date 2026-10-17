package xslt

import (
	"fmt"
	"strings"

	"github.com/ProDGG/fop-go/xmldom"
	"github.com/ProDGG/fop-go/xpath"
)

// ---------- attribute value templates ----------

type avtPart struct {
	lit  string
	expr *xpath.Expr
}

type avt struct {
	parts  []avtPart
	static bool
	lit    string
}

func (s *Stylesheet) compileAVT(el *xmldom.Node, src string) (*avt, error) {
	if !strings.ContainsAny(src, "{}") {
		return &avt{static: true, lit: src}, nil
	}
	a := &avt{}
	var lit strings.Builder
	flush := func() {
		if lit.Len() > 0 {
			a.parts = append(a.parts, avtPart{lit: lit.String()})
			lit.Reset()
		}
	}
	for i := 0; i < len(src); i++ {
		ch := src[i]
		switch {
		case ch == '{' && i+1 < len(src) && src[i+1] == '{':
			lit.WriteByte('{')
			i++
		case ch == '}' && i+1 < len(src) && src[i+1] == '}':
			lit.WriteByte('}')
			i++
		case ch == '{':
			// find matching }, skipping string literals
			j := i + 1
			var quote byte
			for ; j < len(src); j++ {
				c := src[j]
				if quote != 0 {
					if c == quote {
						quote = 0
					}
					continue
				}
				if c == '\'' || c == '"' {
					quote = c
				} else if c == '}' {
					break
				}
			}
			if j >= len(src) {
				return nil, errAt(el, "unterminated expression in attribute value template %q", src)
			}
			flush()
			e, err := xpath.Compile(src[i+1:j], nsResolverFor(el))
			if err != nil {
				return nil, errAt(el, "%v", err)
			}
			a.parts = append(a.parts, avtPart{expr: e})
			i = j
		case ch == '}':
			return nil, errAt(el, "unmatched } in attribute value template %q", src)
		default:
			lit.WriteByte(ch)
		}
	}
	flush()
	return a, nil
}

func (a *avt) eval(c *execCtx) (string, error) {
	if a == nil {
		return "", nil
	}
	if a.static {
		return a.lit, nil
	}
	var sb strings.Builder
	for _, p := range a.parts {
		if p.expr == nil {
			sb.WriteString(p.lit)
			continue
		}
		s, err := c.evalString(p.expr)
		if err != nil {
			return "", err
		}
		sb.WriteString(s)
	}
	return sb.String(), nil
}

// ---------- compilation ----------

func (s *Stylesheet) compileBody(parent *xmldom.Node) (*block, error) {
	b := &block{}
	for _, ch := range parent.Children {
		in, err := s.compileNode(ch)
		if err != nil {
			return nil, err
		}
		if in == nil {
			continue
		}
		if _, ok := in.(*varInstr); ok {
			b.hasVar = true
		}
		b.instrs = append(b.instrs, in)
	}
	return b, nil
}

func (s *Stylesheet) mustExpr(el *xmldom.Node, attr string) (*xpath.Expr, error) {
	v, ok := el.AttrOK(attr)
	if !ok {
		return nil, errAt(el, "xsl:%s requires a %s attribute", el.Name, attr)
	}
	e, err := xpath.Compile(v, nsResolverFor(el))
	if err != nil {
		return nil, errAt(el, "%v", err)
	}
	return e, nil
}

func (s *Stylesheet) optExpr(el *xmldom.Node, attr string) (*xpath.Expr, error) {
	if _, ok := el.AttrOK(attr); !ok {
		return nil, nil
	}
	return s.mustExpr(el, attr)
}

func (s *Stylesheet) optAVT(el *xmldom.Node, attr string) (*avt, error) {
	v, ok := el.AttrOK(attr)
	if !ok {
		return nil, nil
	}
	return s.compileAVT(el, v)
}

func (s *Stylesheet) compileNode(n *xmldom.Node) (instr, error) {
	switch n.Type {
	case xmldom.TextNode:
		return &textInstr{n.Value}, nil
	case xmldom.CommentNode, xmldom.PINode:
		return nil, nil
	case xmldom.ElementNode:
	default:
		return nil, nil
	}
	if n.URI != xmldom.XSLNS {
		return s.compileLiteral(n)
	}
	switch n.Name {
	case "text":
		return &textInstr{n.StringValue()}, nil
	case "value-of":
		sel, err := s.mustExpr(n, "select")
		if err != nil {
			return nil, err
		}
		return &valueOf{sel}, nil
	case "apply-templates":
		return s.compileApplyTemplates(n)
	case "call-template":
		name, err := expandedName(n, n.Attr("name"))
		if err != nil {
			return nil, err
		}
		ps, err := s.compileWithParams(n)
		if err != nil {
			return nil, err
		}
		return &callTemplate{name: name, params: ps, el: n}, nil
	case "apply-imports":
		return &applyImports{}, nil
	case "for-each":
		sel, err := s.mustExpr(n, "select")
		if err != nil {
			return nil, err
		}
		sorts, rest, err := s.compileSorts(n)
		if err != nil {
			return nil, err
		}
		body, err := s.compileBody(rest)
		if err != nil {
			return nil, err
		}
		return &forEach{sel: sel, sorts: sorts, body: body}, nil
	case "if":
		test, err := s.mustExpr(n, "test")
		if err != nil {
			return nil, err
		}
		body, err := s.compileBody(n)
		if err != nil {
			return nil, err
		}
		return &ifInstr{test, body}, nil
	case "choose":
		ch := &chooseInstr{}
		for _, c := range n.Children {
			switch {
			case isXSL(c, "when"):
				test, err := s.mustExpr(c, "test")
				if err != nil {
					return nil, err
				}
				body, err := s.compileBody(c)
				if err != nil {
					return nil, err
				}
				ch.whens = append(ch.whens, whenClause{test, body})
			case isXSL(c, "otherwise"):
				body, err := s.compileBody(c)
				if err != nil {
					return nil, err
				}
				ch.otherwise = body
			default:
				return nil, errAt(c, "unexpected <%s> inside xsl:choose", c.QName())
			}
		}
		return ch, nil
	case "variable", "param":
		pd, err := s.compileParamDecl(n)
		if err != nil {
			return nil, err
		}
		if n.Name == "param" {
			return nil, nil // handled by the template header
		}
		return &varInstr{pd}, nil
	case "copy":
		body, err := s.compileBody(n)
		if err != nil {
			return nil, err
		}
		sets, err := s.attrSetNames(n)
		if err != nil {
			return nil, err
		}
		return &copyInstr{body, sets}, nil
	case "copy-of":
		sel, err := s.mustExpr(n, "select")
		if err != nil {
			return nil, err
		}
		return &copyOf{sel}, nil
	case "element":
		name, err := s.optAVT(n, "name")
		if err != nil || name == nil {
			return nil, errAt(n, "xsl:element requires a name")
		}
		ns, err := s.optAVT(n, "namespace")
		if err != nil {
			return nil, err
		}
		body, err := s.compileBody(n)
		if err != nil {
			return nil, err
		}
		sets, err := s.attrSetNames(n)
		if err != nil {
			return nil, err
		}
		return &elementInstr{name: name, ns: ns, sets: sets, body: body, el: n}, nil
	case "attribute":
		name, err := s.optAVT(n, "name")
		if err != nil || name == nil {
			return nil, errAt(n, "xsl:attribute requires a name")
		}
		ns, err := s.optAVT(n, "namespace")
		if err != nil {
			return nil, err
		}
		body, err := s.compileBody(n)
		if err != nil {
			return nil, err
		}
		return &attributeInstr{name: name, ns: ns, body: body, el: n}, nil
	case "comment":
		body, err := s.compileBody(n)
		if err != nil {
			return nil, err
		}
		return &commentInstr{body}, nil
	case "processing-instruction":
		name, err := s.optAVT(n, "name")
		if err != nil || name == nil {
			return nil, errAt(n, "xsl:processing-instruction requires a name")
		}
		body, err := s.compileBody(n)
		if err != nil {
			return nil, err
		}
		return &piInstr{name, body}, nil
	case "message":
		body, err := s.compileBody(n)
		if err != nil {
			return nil, err
		}
		return &messageInstr{body: body, terminate: n.Attr("terminate") == "yes"}, nil
	case "number":
		return s.compileNumber(n)
	case "fallback", "sort", "with-param":
		return nil, nil
	case "namespace":
		return nil, nil
	}
	// unknown instruction: use xsl:fallback if present
	for _, c := range n.Children {
		if isXSL(c, "fallback") {
			body, err := s.compileBody(c)
			if err != nil {
				return nil, err
			}
			return &blockInstr{body}, nil
		}
	}
	return nil, errAt(n, "unsupported XSLT instruction xsl:%s", n.Name)
}

func (s *Stylesheet) attrSetNames(n *xmldom.Node) ([]string, error) {
	v := n.Attr("use-attribute-sets")
	if v == "" {
		v = n.AttrNS(xmldom.XSLNS, "use-attribute-sets")
	}
	var out []string
	for _, u := range strings.Fields(v) {
		en, err := expandedName(n, u)
		if err != nil {
			return nil, err
		}
		out = append(out, en)
	}
	return out, nil
}

func (s *Stylesheet) compileLiteral(n *xmldom.Node) (instr, error) {
	le := &literalElem{prefix: n.Prefix, local: n.Name, uri: n.URI}
	if al, ok := s.aliases[n.URI]; ok {
		le.prefix, le.uri = al.prefix, al.uri
	}
	sets, err := s.attrSetNames(n)
	if err != nil {
		return nil, err
	}
	le.sets = sets
	for _, a := range n.Attrs {
		if a.URI == xmldom.XSLNS {
			continue
		}
		v, err := s.compileAVT(n, a.Value)
		if err != nil {
			return nil, err
		}
		la := litAttr{prefix: a.Prefix, local: a.Name, uri: a.URI, val: v}
		if al, ok := s.aliases[a.URI]; ok && a.URI != "" {
			la.prefix, la.uri = al.prefix, al.uri
		}
		le.attrs = append(le.attrs, la)
	}
	body, err := s.compileBody(n)
	if err != nil {
		return nil, err
	}
	le.body = body
	return le, nil
}

func (s *Stylesheet) compileWithParams(n *xmldom.Node) ([]*paramDecl, error) {
	var out []*paramDecl
	for _, c := range n.Children {
		if isXSL(c, "with-param") {
			pd, err := s.compileParamDecl(c)
			if err != nil {
				return nil, err
			}
			out = append(out, pd)
		}
	}
	return out, nil
}

func (s *Stylesheet) compileApplyTemplates(n *xmldom.Node) (instr, error) {
	at := &applyTemplates{}
	if _, ok := n.AttrOK("select"); ok {
		sel, err := s.mustExpr(n, "select")
		if err != nil {
			return nil, err
		}
		at.sel = sel
	}
	if m := n.Attr("mode"); m != "" && m != "#default" {
		en, err := expandedName(n, m)
		if err != nil {
			return nil, err
		}
		at.mode = en
	}
	sorts, _, err := s.compileSorts(n)
	if err != nil {
		return nil, err
	}
	at.sorts = sorts
	at.params, err = s.compileWithParams(n)
	if err != nil {
		return nil, err
	}
	return at, nil
}

// ---------- simple instructions ----------

type textInstr struct{ s string }

func (t *textInstr) exec(c *execCtx) error { c.out.text(t.s); return nil }

type blockInstr struct{ b *block }

func (b *blockInstr) exec(c *execCtx) error { return b.b.exec(c) }

type valueOf struct{ sel *xpath.Expr }

func (v *valueOf) exec(c *execCtx) error {
	s, err := c.evalString(v.sel)
	if err != nil {
		return err
	}
	c.out.text(s)
	return nil
}

type ifInstr struct {
	test *xpath.Expr
	body *block
}

func (i *ifInstr) exec(c *execCtx) error {
	ok, err := c.evalBool(i.test)
	if err != nil {
		return err
	}
	if ok {
		return i.body.exec(c)
	}
	return nil
}

type whenClause struct {
	test *xpath.Expr
	body *block
}

type chooseInstr struct {
	whens     []whenClause
	otherwise *block
}

func (ch *chooseInstr) exec(c *execCtx) error {
	for _, w := range ch.whens {
		ok, err := c.evalBool(w.test)
		if err != nil {
			return err
		}
		if ok {
			return w.body.exec(c)
		}
	}
	return ch.otherwise.exec(c)
}

type varInstr struct{ decl *paramDecl }

func (v *varInstr) exec(c *execCtx) error {
	val, err := c.paramValue(v.decl)
	if err != nil {
		return err
	}
	c.vars = &scope{parent: c.vars, name: v.decl.name, val: val}
	return nil
}

type forEach struct {
	sel   *xpath.Expr
	sorts []*sortKey
	body  *block
}

func (f *forEach) exec(c *execCtx) error {
	v, err := c.eval(f.sel)
	if err != nil {
		return err
	}
	ns, ok := v.(xpath.NodeSet)
	if !ok {
		return fmt.Errorf("xslt: xsl:for-each select %q did not yield a node-set", f.sel.Source)
	}
	if len(f.sorts) > 0 {
		ns, err = sortNodes(c, ns, f.sorts)
		if err != nil {
			return err
		}
	}
	for i, n := range ns {
		if err := f.body.exec(c.sub(n, i+1, len(ns))); err != nil {
			return err
		}
	}
	return nil
}

type copyOf struct{ sel *xpath.Expr }

func (cp *copyOf) exec(c *execCtx) error {
	v, err := c.eval(cp.sel)
	if err != nil {
		return err
	}
	if ns, ok := v.(xpath.NodeSet); ok {
		for _, n := range ns {
			if err := c.out.copyNode(n); err != nil {
				return err
			}
		}
		return nil
	}
	c.out.text(xpath.ToString(v))
	return nil
}

type copyInstr struct {
	body *block
	sets []string
}

func (cp *copyInstr) exec(c *execCtx) error {
	n := c.node
	switch n.Type {
	case xmldom.ElementNode:
		c.out.startElement(n.Prefix, n.Name, n.URI)
		if err := c.applyAttrSets(cp.sets); err != nil {
			return err
		}
		if err := cp.body.exec(c); err != nil {
			return err
		}
		c.out.endElement()
	case xmldom.DocumentNode:
		return cp.body.exec(c)
	case xmldom.AttributeNode:
		return c.out.attr(n.Prefix, n.Name, n.URI, n.Value)
	case xmldom.TextNode:
		c.out.text(n.Value)
	case xmldom.CommentNode:
		c.out.cur.AppendChild(xmldom.NewComment(n.Value))
	case xmldom.PINode:
		c.out.cur.AppendChild(xmldom.NewPI(n.Name, n.Value))
	}
	return nil
}

type litAttr struct {
	prefix, local, uri string
	val                *avt
}

type literalElem struct {
	prefix, local, uri string
	attrs              []litAttr
	sets               []string
	body               *block
}

func (l *literalElem) exec(c *execCtx) error {
	c.out.startElement(l.prefix, l.local, l.uri)
	if err := c.applyAttrSets(l.sets); err != nil {
		return err
	}
	for _, a := range l.attrs {
		v, err := a.val.eval(c)
		if err != nil {
			return err
		}
		if err := c.out.attr(a.prefix, a.local, a.uri, v); err != nil {
			return err
		}
	}
	if err := l.body.exec(c); err != nil {
		return err
	}
	c.out.endElement()
	return nil
}

func (c *execCtx) applyAttrSets(names []string) error {
	for _, nm := range names {
		if err := c.applyAttrSet(nm, 0); err != nil {
			return err
		}
	}
	return nil
}

func (c *execCtx) applyAttrSet(name string, depth int) error {
	as, ok := c.t.s.attrSets[name]
	if !ok {
		return fmt.Errorf("xslt: unknown attribute-set %q", name)
	}
	if depth > 50 {
		return fmt.Errorf("xslt: attribute-set %q is recursive", name)
	}
	for _, u := range as.uses {
		if err := c.applyAttrSet(u, depth+1); err != nil {
			return err
		}
	}
	sub := *c
	sub.vars = nil
	return as.body.exec(&sub)
}

// splitQName resolves a QName to prefix/local/uri using explicit namespace or scope.
func resolveQName(el *xmldom.Node, qname, nsAttr string, hasNS bool, useDefault bool) (prefix, local, uri string, err error) {
	qname = strings.TrimSpace(qname)
	if i := strings.IndexByte(qname, ':'); i >= 0 {
		prefix, local = qname[:i], qname[i+1:]
	} else {
		local = qname
	}
	if hasNS {
		return prefix, local, nsAttr, nil
	}
	if prefix != "" {
		u, ok := el.LookupNamespace(prefix)
		if !ok {
			return "", "", "", errAt(el, "undeclared namespace prefix %q", prefix)
		}
		return prefix, local, u, nil
	}
	if useDefault {
		if u, ok := el.LookupNamespace(""); ok {
			return "", local, u, nil
		}
	}
	return "", local, "", nil
}

type elementInstr struct {
	name, ns *avt
	sets     []string
	body     *block
	el       *xmldom.Node
}

func (e *elementInstr) exec(c *execCtx) error {
	name, err := e.name.eval(c)
	if err != nil {
		return err
	}
	nsv, hasNS := "", e.ns != nil
	if hasNS {
		if nsv, err = e.ns.eval(c); err != nil {
			return err
		}
	}
	prefix, local, uri, err := resolveQName(e.el, name, nsv, hasNS, true)
	if err != nil {
		return err
	}
	c.out.startElement(prefix, local, uri)
	if err := c.applyAttrSets(e.sets); err != nil {
		return err
	}
	if err := e.body.exec(c); err != nil {
		return err
	}
	c.out.endElement()
	return nil
}

type attributeInstr struct {
	name, ns *avt
	body     *block
	el       *xmldom.Node
}

func (a *attributeInstr) exec(c *execCtx) error {
	name, err := a.name.eval(c)
	if err != nil {
		return err
	}
	nsv, hasNS := "", a.ns != nil
	if hasNS {
		if nsv, err = a.ns.eval(c); err != nil {
			return err
		}
	}
	prefix, local, uri, err := resolveQName(a.el, name, nsv, hasNS, false)
	if err != nil {
		return err
	}
	val, err := c.bodyString(a.body)
	if err != nil {
		return err
	}
	return c.out.attr(prefix, local, uri, val)
}

type commentInstr struct{ body *block }

func (ci *commentInstr) exec(c *execCtx) error {
	s, err := c.bodyString(ci.body)
	if err != nil {
		return err
	}
	c.out.cur.AppendChild(xmldom.NewComment(s))
	return nil
}

type piInstr struct {
	name *avt
	body *block
}

func (p *piInstr) exec(c *execCtx) error {
	name, err := p.name.eval(c)
	if err != nil {
		return err
	}
	s, err := c.bodyString(p.body)
	if err != nil {
		return err
	}
	c.out.cur.AppendChild(xmldom.NewPI(name, s))
	return nil
}

type messageInstr struct {
	body      *block
	terminate bool
}

func (m *messageInstr) exec(c *execCtx) error {
	s, err := c.bodyString(m.body)
	if err != nil {
		return err
	}
	if c.t.messages != nil {
		c.t.messages(s)
	}
	if m.terminate {
		return fmt.Errorf("xslt: terminated by xsl:message: %s", s)
	}
	return nil
}

// ---------- templates ----------

type applyTemplates struct {
	sel    *xpath.Expr
	mode   string
	sorts  []*sortKey
	params []*paramDecl
}

type paramVal struct {
	name string
	val  xpath.Value
}

func (c *execCtx) evalWithParams(ps []*paramDecl) ([]paramVal, error) {
	if len(ps) == 0 {
		return nil, nil
	}
	out := make([]paramVal, 0, len(ps))
	for _, p := range ps {
		v, err := c.paramValue(p)
		if err != nil {
			return nil, err
		}
		out = append(out, paramVal{p.name, v})
	}
	return out, nil
}

func (a *applyTemplates) exec(c *execCtx) error {
	var ns xpath.NodeSet
	if a.sel == nil {
		for _, ch := range c.node.Children {
			ns = append(ns, ch)
		}
	} else {
		v, err := c.eval(a.sel)
		if err != nil {
			return err
		}
		var ok bool
		ns, ok = v.(xpath.NodeSet)
		if !ok {
			return fmt.Errorf("xslt: xsl:apply-templates select %q did not yield a node-set", a.sel.Source)
		}
	}
	params, err := c.evalWithParams(a.params)
	if err != nil {
		return err
	}
	if len(a.sorts) > 0 {
		ns, err = sortNodes(c, ns, a.sorts)
		if err != nil {
			return err
		}
	}
	return c.applyTemplates(ns, a.mode, params, nil)
}

// applyTemplates processes each node of ns in the given mode.
func (c *execCtx) applyTemplates(ns xpath.NodeSet, mode string, params []paramVal, _ *Template) error {
	for i, n := range ns {
		sub := c.sub(n, i+1, len(ns))
		sub.mode = mode
		if err := sub.processNode(params, nil); err != nil {
			return err
		}
	}
	return nil
}

// processNode finds and runs the best template for c.node.
func (c *execCtx) processNode(params []paramVal, below *Template) error {
	t := c.findTemplate(c.node, c.mode, below)
	if t == nil {
		return c.builtin(params)
	}
	return c.runTemplate(t, params)
}

func (c *execCtx) findTemplate(n *xmldom.Node, mode string, below *Template) *Template {
	mt := c.t.s.modes[mode]
	if mt == nil {
		return nil
	}
	xc := c.xp()
	for _, t := range mt.candidates(n) {
		if below != nil && t.prec >= below.prec {
			continue
		}
		if t.alt.Match(xc, n) {
			return t
		}
	}
	return nil
}

func (c *execCtx) builtin(params []paramVal) error {
	n := c.node
	switch n.Type {
	case xmldom.DocumentNode, xmldom.ElementNode:
		kids := make(xpath.NodeSet, len(n.Children))
		copy(kids, n.Children)
		return c.applyTemplates(kids, c.mode, nil, nil)
	case xmldom.TextNode, xmldom.AttributeNode:
		c.out.text(n.Value)
	}
	return nil
}

func (c *execCtx) runTemplate(t *Template, params []paramVal) error {
	c.t.depth++
	defer func() { c.t.depth-- }()
	if c.t.depth > 5000 {
		return errAt(t.el, "template recursion too deep (possible infinite recursion)")
	}
	sub := *c
	sub.tmpl = t
	sub.vars = nil
	for _, p := range t.params {
		var val xpath.Value
		found := false
		for _, pv := range params {
			if pv.name == p.name {
				val, found = pv.val, true
				break
			}
		}
		if !found {
			var err error
			val, err = sub.paramValue(p)
			if err != nil {
				return err
			}
		}
		sub.vars = &scope{parent: sub.vars, name: p.name, val: val}
	}
	return t.body.exec(&sub)
}

type callTemplate struct {
	name   string
	params []*paramDecl
	el     *xmldom.Node
}

func (ct *callTemplate) exec(c *execCtx) error {
	t, ok := c.t.s.named[ct.name]
	if !ok {
		return errAt(ct.el, "no template named %q", ct.name)
	}
	params, err := c.evalWithParams(ct.params)
	if err != nil {
		return err
	}
	return c.runTemplate(t, params)
}

type applyImports struct{}

func (applyImports) exec(c *execCtx) error {
	if c.tmpl == nil {
		return fmt.Errorf("xslt: xsl:apply-imports outside a template")
	}
	t := c.findTemplate(c.node, c.tmpl.mode, c.tmpl)
	if t == nil {
		return c.builtin(nil)
	}
	return c.runTemplate(t, nil)
}
