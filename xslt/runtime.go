package xslt

import (
	"fmt"
	"strings"

	"github.com/ProDGG/fop-go/xmldom"
	"github.com/ProDGG/fop-go/xpath"
)

// scope is a linked list of local variable bindings.
type scope struct {
	parent *scope
	name   string
	val    xpath.Value
}

func (s *scope) lookup(name string) (xpath.Value, bool) {
	for ; s != nil; s = s.parent {
		if s.name == name {
			return s.val, true
		}
	}
	return nil, false
}

// Transformer holds the state of one transformation run.
type Transformer struct {
	s        *Stylesheet
	src      *xmldom.Node
	params   map[string]xpath.Value
	globals  map[string]*globalState
	keyIdx   map[*xmldom.Node]map[string]map[string][]*xmldom.Node
	docs     map[string]*xmldom.Node
	messages func(string)
	baseURI  string // location of the source document
	depth    int
}

type globalState struct {
	state int // 0 new, 1 evaluating, 2 done
	val   xpath.Value
}

// TransformOptions control a transformation run.
type TransformOptions struct {
	// Params are stylesheet parameters; values may be string, float64, bool,
	// int or xpath.NodeSet.
	Params map[string]interface{}
	// Messages receives xsl:message output (default: discarded).
	Messages func(string)
	// SourceURI is the location of the source document (for document()).
	SourceURI string
}

// Transform applies the stylesheet to a parsed source document and returns the
// result tree as a document node.
func (s *Stylesheet) Transform(src *xmldom.Node, o TransformOptions) (result *xmldom.Node, err error) {
	t := &Transformer{
		s:        s,
		src:      src,
		params:   map[string]xpath.Value{},
		globals:  map[string]*globalState{},
		keyIdx:   map[*xmldom.Node]map[string]map[string][]*xmldom.Node{},
		docs:     map[string]*xmldom.Node{},
		messages: o.Messages,
		baseURI:  o.SourceURI,
	}
	for k, v := range o.Params {
		switch x := v.(type) {
		case int:
			t.params[k] = float64(x)
		case int64:
			t.params[k] = float64(x)
		case string, float64, bool, xpath.NodeSet:
			t.params[k] = x
		default:
			t.params[k] = fmt.Sprint(x)
		}
	}
	s.StripSpace(src)
	defer func() {
		if r := recover(); r != nil {
			if te, ok := r.(transformPanic); ok {
				err = te.err
				result = nil
				return
			}
			panic(r)
		}
	}()
	out := &builder{doc: xmldom.NewDocument()}
	out.cur = out.doc
	c := &execCtx{t: t, node: src, pos: 1, size: 1, out: out}
	if err := c.applyTemplates(xpath.NodeSet{src}, "", nil, nil); err != nil {
		return nil, err
	}
	return out.doc, nil
}

type transformPanic struct{ err error }

// execCtx is the dynamic context of instruction execution.
type execCtx struct {
	t    *Transformer
	node *xmldom.Node
	pos  int
	size int
	vars *scope
	mode string
	tmpl *Template
	out  *builder
}

func (c *execCtx) lookupVar(uri, local string) (xpath.Value, bool) {
	key := local
	if uri != "" {
		key = "{" + uri + "}" + local
	}
	return c.lookupKey(key)
}

func (c *execCtx) lookupKey(key string) (xpath.Value, bool) {
	if v, ok := c.vars.lookup(key); ok {
		return v, true
	}
	return c.t.global(key)
}

// xp builds an XPath context for the current focus.
func (c *execCtx) xp() *xpath.Context {
	return &xpath.Context{
		Node: c.node, Pos: c.pos, Size: c.size,
		Vars: c.lookupVar, Funcs: c.t.callFunc, Current: c.node, Data: c,
	}
}

func (c *execCtx) sub(n *xmldom.Node, pos, size int) *execCtx {
	cc := *c
	cc.node, cc.pos, cc.size = n, pos, size
	return &cc
}

func (c *execCtx) eval(e *xpath.Expr) (xpath.Value, error) { return e.Eval(c.xp()) }

func (c *execCtx) evalString(e *xpath.Expr) (string, error) {
	v, err := e.Eval(c.xp())
	if err != nil {
		return "", err
	}
	return xpath.ToString(v), nil
}

func (c *execCtx) evalBool(e *xpath.Expr) (bool, error) {
	v, err := e.Eval(c.xp())
	if err != nil {
		return false, err
	}
	return xpath.ToBool(v), nil
}

// global returns the value of a global variable/parameter, evaluating it on demand.
func (t *Transformer) global(key string) (xpath.Value, bool) {
	gv, ok := t.s.globalIdx[key]
	if !ok {
		return nil, false
	}
	st := t.globals[key]
	if st == nil {
		st = &globalState{}
		t.globals[key] = st
	}
	switch st.state {
	case 2:
		return st.val, true
	case 1:
		panic(transformPanic{errAt(gv.el, "circular definition of global variable %q", gv.decl.qname)})
	}
	st.state = 1
	if gv.isParam {
		if v, ok := t.params[gv.decl.qname]; ok {
			st.val, st.state = v, 2
			return v, true
		}
	}
	out := &builder{doc: xmldom.NewDocument()}
	out.cur = out.doc
	c := &execCtx{t: t, node: t.src, pos: 1, size: 1, out: out}
	v, err := c.paramValue(gv.decl)
	if err != nil {
		panic(transformPanic{err})
	}
	st.val, st.state = v, 2
	return v, true
}

// paramValue computes the value of a variable/param declaration.
func (c *execCtx) paramValue(d *paramDecl) (xpath.Value, error) {
	switch {
	case d.sel != nil:
		return c.eval(d.sel)
	case d.body != nil:
		return c.rtf(d.body)
	}
	return "", nil
}

// rtf evaluates a body into a result tree fragment (a document node).
func (c *execCtx) rtf(b *block) (xpath.Value, error) {
	doc := xmldom.NewDocument()
	sub := *c
	sub.out = &builder{doc: doc, cur: doc}
	if err := b.exec(&sub); err != nil {
		return nil, err
	}
	return xpath.NodeSet{doc}, nil
}

// builder constructs the result tree.
type builder struct {
	doc *xmldom.Node
	cur *xmldom.Node
}

func (b *builder) startElement(prefix, local, uri string) *xmldom.Node {
	el := xmldom.NewElement(prefix, local, uri)
	b.cur.AppendChild(el)
	b.cur = el
	return el
}

func (b *builder) endElement() { b.cur = b.cur.Parent }

func (b *builder) text(s string) {
	if s == "" {
		return
	}
	b.cur.AppendChild(xmldom.NewText(s))
}

func (b *builder) attr(prefix, local, uri, value string) error {
	if b.cur.Type != xmldom.ElementNode {
		return fmt.Errorf("xslt: attribute %q created outside an element", local)
	}
	if len(b.cur.Children) > 0 {
		return fmt.Errorf("xslt: attribute %q added after child nodes of <%s>", local, b.cur.QName())
	}
	b.cur.SetAttr(prefix, local, uri, value)
	return nil
}

// copyNode copies a source node to the output.
func (b *builder) copyNode(n *xmldom.Node) error {
	switch n.Type {
	case xmldom.DocumentNode:
		for _, ch := range n.Children {
			if err := b.copyNode(ch); err != nil {
				return err
			}
		}
	case xmldom.AttributeNode:
		return b.attr(n.Prefix, n.Name, n.URI, n.Value)
	case xmldom.TextNode:
		b.text(n.Value)
	case xmldom.NamespaceNode:
	default:
		b.cur.AppendChild(n.DeepCopy())
	}
	return nil
}

// block is a sequence of instructions sharing a variable scope.
type block struct {
	instrs []instr
	hasVar bool
}

type instr interface {
	exec(c *execCtx) error
}

func (b *block) exec(c *execCtx) error {
	if b == nil {
		return nil
	}
	cc := c
	if b.hasVar {
		x := *c
		cc = &x
	}
	for _, in := range b.instrs {
		if err := in.exec(cc); err != nil {
			return err
		}
	}
	return nil
}

// text of a body (concatenated string value of the result of executing it)
func (c *execCtx) bodyString(b *block) (string, error) {
	if b == nil {
		return "", nil
	}
	v, err := c.rtf(b)
	if err != nil {
		return "", err
	}
	return xpath.ToString(v), nil
}

// StripSpace removes whitespace-only text nodes from a source document
// according to xsl:strip-space and xsl:preserve-space.
func (s *Stylesheet) StripSpace(doc *xmldom.Node) {
	if len(s.space) == 0 {
		return
	}
	var walk func(n *xmldom.Node, preserve bool)
	walk = func(n *xmldom.Node, preserve bool) {
		if n.Type == xmldom.ElementNode {
			if v := n.AttrNS(xmldom.XMLNS, "space"); v == "preserve" {
				preserve = true
			} else if v == "default" {
				preserve = false
			}
			if !preserve && s.shouldStrip(n) {
				kept := n.Children[:0]
				for _, c := range n.Children {
					if c.Type == xmldom.TextNode && strings.TrimSpace(c.Value) == "" {
						continue
					}
					kept = append(kept, c)
				}
				n.Children = kept
			}
		}
		for _, c := range n.Children {
			if c.Type == xmldom.ElementNode {
				walk(c, preserve)
			}
		}
	}
	walk(doc, false)
}

func (s *Stylesheet) shouldStrip(el *xmldom.Node) bool {
	var best *spaceRule
	for i := range s.space {
		r := &s.space[i]
		ok := false
		switch {
		case r.any:
			ok = true
		case r.anyLocal:
			ok = el.URI == r.uri
		default:
			ok = el.URI == r.uri && el.Name == r.local
		}
		if !ok {
			continue
		}
		if best == nil || r.prec > best.prec || (r.prec == best.prec && (r.priority > best.priority ||
			(r.priority == best.priority && r.order > best.order))) {
			best = r
		}
	}
	return best != nil && best.strip
}
