package xpath

import (
	"fmt"
	"strings"

	"github.com/ProDGG/fop-go/xmldom"
)

// NSResolver maps a namespace prefix to a URI.
type NSResolver func(prefix string) (string, bool)

type axisKind int

const (
	axChild axisKind = iota
	axDescendant
	axParent
	axAncestor
	axFollowingSibling
	axPrecedingSibling
	axFollowing
	axPreceding
	axAttribute
	axNamespace
	axSelf
	axDescendantOrSelf
	axAncestorOrSelf
)

var axisNames = map[string]axisKind{
	"child": axChild, "descendant": axDescendant, "parent": axParent, "ancestor": axAncestor,
	"following-sibling": axFollowingSibling, "preceding-sibling": axPrecedingSibling,
	"following": axFollowing, "preceding": axPreceding, "attribute": axAttribute,
	"namespace": axNamespace, "self": axSelf, "descendant-or-self": axDescendantOrSelf,
	"ancestor-or-self": axAncestorOrSelf,
}

func (a axisKind) reverse() bool {
	switch a {
	case axParent, axAncestor, axPrecedingSibling, axPreceding, axAncestorOrSelf:
		return true
	}
	return false
}

type testKind int

const (
	testName testKind = iota // name test (possibly wildcard)
	testNode
	testText
	testComment
	testPI
)

type nodeTest struct {
	kind    testKind
	uri     string
	local   string // "" with anyLocal
	any     bool   // '*'
	anyName bool   // prefix:* → any local in uri
	piName  string
}

type step struct {
	axis  axisKind
	test  nodeTest
	preds []expr
}

type expr interface{}

type (
	numLit   struct{ v float64 }
	strLit   struct{ v string }
	varRef   struct{ uri, local, qname string }
	funcCall struct {
		uri, local, qname string
		args              []expr
	}
	binary struct {
		op   tokKind
		name string // for tOpName
		l, r expr
	}
	negate     struct{ e expr }
	unionExpr  struct{ l, r expr }
	filterExpr struct {
		primary expr
		preds   []expr
	}
	pathExpr struct {
		filter   expr // optional leading filter expression
		absolute bool
		steps    []*step
	}
)

// Expr is a compiled XPath expression.
type Expr struct {
	Source string
	root   expr
}

type parser struct {
	toks []token
	pos  int
	ns   NSResolver
	src  string
}

// Compile parses an XPath expression. ns resolves prefixes used in name tests.
func Compile(src string, ns NSResolver) (*Expr, error) {
	toks, err := tokenize(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks, ns: ns, src: src}
	e, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tEOF {
		return nil, p.errf("unexpected token %q", p.peek().text)
	}
	return &Expr{Source: src, root: e}, nil
}

// MustCompile is Compile that panics on error.
func MustCompile(src string, ns NSResolver) *Expr {
	e, err := Compile(src, ns)
	if err != nil {
		panic(err)
	}
	return e
}

func (p *parser) peek() token { return p.toks[p.pos] }
func (p *parser) next() token {
	t := p.toks[p.pos]
	if t.kind != tEOF {
		p.pos++
	}
	return t
}
func (p *parser) errf(f string, a ...interface{}) error {
	return fmt.Errorf("xpath: %s (in %q)", fmt.Sprintf(f, a...), p.src)
}
func (p *parser) expect(k tokKind, what string) error {
	if p.peek().kind != k {
		return p.errf("expected %s, found %q", what, p.peek().text)
	}
	p.pos++
	return nil
}

func (p *parser) resolve(qname string, isElement bool) (uri, local string, err error) {
	if i := strings.IndexByte(qname, ':'); i >= 0 {
		prefix := qname[:i]
		local = qname[i+1:]
		if p.ns != nil {
			if u, ok := p.ns(prefix); ok {
				return u, local, nil
			}
		}
		if prefix == "xml" {
			return xmldom.XMLNS, local, nil
		}
		return "", "", p.errf("undeclared namespace prefix %q", prefix)
	}
	return "", qname, nil
}

func (p *parser) parseOr() (expr, error) {
	l, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tOpName && p.peek().text == "or" {
		p.next()
		r, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		l = &binary{op: tOpName, name: "or", l: l, r: r}
	}
	return l, nil
}

func (p *parser) parseAnd() (expr, error) {
	l, err := p.parseEquality()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tOpName && p.peek().text == "and" {
		p.next()
		r, err := p.parseEquality()
		if err != nil {
			return nil, err
		}
		l = &binary{op: tOpName, name: "and", l: l, r: r}
	}
	return l, nil
}

func (p *parser) parseEquality() (expr, error) {
	l, err := p.parseRelational()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tEq || p.peek().kind == tNe {
		op := p.next().kind
		r, err := p.parseRelational()
		if err != nil {
			return nil, err
		}
		l = &binary{op: op, l: l, r: r}
	}
	return l, nil
}

func (p *parser) parseRelational() (expr, error) {
	l, err := p.parseAdditive()
	if err != nil {
		return nil, err
	}
	for k := p.peek().kind; k == tLt || k == tLe || k == tGt || k == tGe; k = p.peek().kind {
		p.next()
		r, err := p.parseAdditive()
		if err != nil {
			return nil, err
		}
		l = &binary{op: k, l: l, r: r}
	}
	return l, nil
}

func (p *parser) parseAdditive() (expr, error) {
	l, err := p.parseMultiplicative()
	if err != nil {
		return nil, err
	}
	for k := p.peek().kind; k == tPlus || k == tMinus; k = p.peek().kind {
		p.next()
		r, err := p.parseMultiplicative()
		if err != nil {
			return nil, err
		}
		l = &binary{op: k, l: l, r: r}
	}
	return l, nil
}

func (p *parser) parseMultiplicative() (expr, error) {
	l, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if t.kind == tMul || (t.kind == tOpName && (t.text == "div" || t.text == "mod")) {
			p.next()
			r, err := p.parseUnary()
			if err != nil {
				return nil, err
			}
			l = &binary{op: t.kind, name: t.text, l: l, r: r}
			continue
		}
		return l, nil
	}
}

func (p *parser) parseUnary() (expr, error) {
	if p.peek().kind == tMinus {
		p.next()
		e, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &negate{e}, nil
	}
	return p.parseUnion()
}

func (p *parser) parseUnion() (expr, error) {
	l, err := p.parsePath()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tPipe {
		p.next()
		r, err := p.parsePath()
		if err != nil {
			return nil, err
		}
		l = &unionExpr{l, r}
	}
	return l, nil
}

func isNodeTypeName(n string) bool {
	switch n {
	case "node", "text", "comment", "processing-instruction":
		return true
	}
	return false
}

func (p *parser) startsStep() bool {
	switch p.peek().kind {
	case tName, tStar, tAxis, tAt, tDot, tDotDot:
		return true
	case tFunc:
		return isNodeTypeName(p.peek().text)
	}
	return false
}

func (p *parser) parsePath() (expr, error) {
	t := p.peek()
	switch t.kind {
	case tSlash, tDSlash:
		pe := &pathExpr{absolute: true}
		p.next()
		if t.kind == tDSlash {
			pe.steps = append(pe.steps, descendantOrSelfStep())
		}
		if p.startsStep() {
			if err := p.parseRelative(pe); err != nil {
				return nil, err
			}
		} else if t.kind == tDSlash {
			return nil, p.errf("expected step after //")
		}
		return pe, nil
	}
	// filter expression or relative path
	if !p.startsStep() {
		prim, err := p.parsePrimary()
		if err != nil {
			return nil, err
		}
		var preds []expr
		for p.peek().kind == tLBrack {
			pr, err := p.parsePredicate()
			if err != nil {
				return nil, err
			}
			preds = append(preds, pr)
		}
		var fe expr = prim
		if len(preds) > 0 {
			fe = &filterExpr{prim, preds}
		}
		if k := p.peek().kind; k == tSlash || k == tDSlash {
			pe := &pathExpr{filter: fe}
			p.next()
			if k == tDSlash {
				pe.steps = append(pe.steps, descendantOrSelfStep())
			}
			if err := p.parseRelative(pe); err != nil {
				return nil, err
			}
			return pe, nil
		}
		return fe, nil
	}
	pe := &pathExpr{}
	if err := p.parseRelative(pe); err != nil {
		return nil, err
	}
	return pe, nil
}

func descendantOrSelfStep() *step {
	return &step{axis: axDescendantOrSelf, test: nodeTest{kind: testNode}}
}

func (p *parser) parseRelative(pe *pathExpr) error {
	for {
		st, err := p.parseStep()
		if err != nil {
			return err
		}
		pe.steps = append(pe.steps, st)
		switch p.peek().kind {
		case tSlash:
			p.next()
		case tDSlash:
			p.next()
			pe.steps = append(pe.steps, descendantOrSelfStep())
		default:
			return nil
		}
	}
}

func (p *parser) parseStep() (*step, error) {
	t := p.peek()
	switch t.kind {
	case tDot:
		p.next()
		return &step{axis: axSelf, test: nodeTest{kind: testNode}}, nil
	case tDotDot:
		p.next()
		return &step{axis: axParent, test: nodeTest{kind: testNode}}, nil
	}
	st := &step{axis: axChild}
	switch t.kind {
	case tAt:
		p.next()
		st.axis = axAttribute
	case tAxis:
		p.next()
		ax, ok := axisNames[t.text]
		if !ok {
			return nil, p.errf("unknown axis %q", t.text)
		}
		st.axis = ax
	}
	nt, err := p.parseNodeTest(st.axis)
	if err != nil {
		return nil, err
	}
	st.test = nt
	for p.peek().kind == tLBrack {
		pr, err := p.parsePredicate()
		if err != nil {
			return nil, err
		}
		st.preds = append(st.preds, pr)
	}
	return st, nil
}

func (p *parser) parseNodeTest(ax axisKind) (nodeTest, error) {
	t := p.next()
	switch t.kind {
	case tStar:
		return nodeTest{kind: testName, any: true}, nil
	case tName:
		if strings.HasSuffix(t.text, ":*") {
			prefix := strings.TrimSuffix(t.text, ":*")
			uri, _, err := p.resolve(prefix+":x", true)
			if err != nil {
				return nodeTest{}, err
			}
			return nodeTest{kind: testName, uri: uri, anyName: true}, nil
		}
		uri, local, err := p.resolve(t.text, true)
		if err != nil {
			return nodeTest{}, err
		}
		return nodeTest{kind: testName, uri: uri, local: local}, nil
	case tFunc:
		if !isNodeTypeName(t.text) {
			return nodeTest{}, p.errf("unexpected function %q in step", t.text)
		}
		if err := p.expect(tLParen, "("); err != nil {
			return nodeTest{}, err
		}
		nt := nodeTest{}
		switch t.text {
		case "node":
			nt.kind = testNode
		case "text":
			nt.kind = testText
		case "comment":
			nt.kind = testComment
		case "processing-instruction":
			nt.kind = testPI
			if p.peek().kind == tLit {
				nt.piName = p.next().text
			}
		}
		if err := p.expect(tRParen, ")"); err != nil {
			return nodeTest{}, err
		}
		return nt, nil
	}
	return nodeTest{}, p.errf("expected node test, found %q", t.text)
}

func (p *parser) parsePredicate() (expr, error) {
	p.next() // [
	e, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if err := p.expect(tRBrack, "]"); err != nil {
		return nil, err
	}
	return e, nil
}

func (p *parser) parsePrimary() (expr, error) {
	t := p.next()
	switch t.kind {
	case tNum:
		return &numLit{t.num}, nil
	case tLit:
		return &strLit{t.text}, nil
	case tDollar:
		n := p.next()
		if n.kind != tName {
			return nil, p.errf("expected variable name after $")
		}
		uri, local, err := p.resolve(n.text, false)
		if err != nil {
			return nil, err
		}
		return &varRef{uri: uri, local: local, qname: n.text}, nil
	case tLParen:
		e, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if err := p.expect(tRParen, ")"); err != nil {
			return nil, err
		}
		return e, nil
	case tFunc:
		uri, local, err := p.resolve(t.text, false)
		if err != nil {
			return nil, err
		}
		fc := &funcCall{uri: uri, local: local, qname: t.text}
		if err := p.expect(tLParen, "("); err != nil {
			return nil, err
		}
		if p.peek().kind != tRParen {
			for {
				a, err := p.parseOr()
				if err != nil {
					return nil, err
				}
				fc.args = append(fc.args, a)
				if p.peek().kind == tComma {
					p.next()
					continue
				}
				break
			}
		}
		if err := p.expect(tRParen, ")"); err != nil {
			return nil, err
		}
		return fc, nil
	}
	return nil, p.errf("unexpected token %q", t.text)
}
