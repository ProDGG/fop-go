package xpath

import (
	"fmt"

	"github.com/ProDGG/fop-go/xmldom"
)

// Pattern is a compiled XSLT match pattern (a union of path alternatives).
type Pattern struct {
	Source string
	Alts   []*PatternAlt
}

type patStep struct {
	desc  bool // separator before this step is '//' (ancestor, not parent)
	axis  axisKind
	test  nodeTest
	preds []expr
}

// PatternAlt is one alternative of a union pattern.
type PatternAlt struct {
	absolute bool
	steps    []patStep
	filter   *funcCall // id()/key() start of pattern
	priority float64
}

// Priority returns the default priority of the alternative.
func (a *PatternAlt) Priority() float64 { return a.priority }

// NodeKind describes what kind of node an alternative can match, for indexing.
// It returns the node type (0 = any), the namespace URI and local name ("" = any).
func (a *PatternAlt) NodeKind() (typ xmldom.NodeType, uri, local string, anyName bool) {
	if len(a.steps) == 0 {
		if a.filter != nil {
			return 0, "", "", true
		}
		return xmldom.DocumentNode, "", "", true
	}
	st := a.steps[len(a.steps)-1]
	switch st.test.kind {
	case testText:
		return xmldom.TextNode, "", "", true
	case testComment:
		return xmldom.CommentNode, "", "", true
	case testPI:
		return xmldom.PINode, "", "", true
	case testNode:
		return 0, "", "", true
	}
	typ = xmldom.ElementNode
	if st.axis == axAttribute {
		typ = xmldom.AttributeNode
	}
	if st.test.any {
		return typ, "", "", true
	}
	if st.test.anyName {
		return typ, st.test.uri, "", true
	}
	return typ, st.test.uri, st.test.local, false
}

// CompilePattern parses an XSLT match pattern.
func CompilePattern(src string, ns NSResolver) (*Pattern, error) {
	e, err := Compile(src, ns)
	if err != nil {
		return nil, err
	}
	p := &Pattern{Source: src}
	var walk func(x expr) error
	walk = func(x expr) error {
		switch v := x.(type) {
		case *unionExpr:
			if err := walk(v.l); err != nil {
				return err
			}
			return walk(v.r)
		case *pathExpr:
			alt, err := buildAlt(v)
			if err != nil {
				return fmt.Errorf("xpath: pattern %q: %w", src, err)
			}
			p.Alts = append(p.Alts, alt)
			return nil
		case *funcCall:
			if v.local == "key" || v.local == "id" {
				p.Alts = append(p.Alts, &PatternAlt{filter: v, priority: 0.5})
				return nil
			}
		}
		return fmt.Errorf("xpath: %q is not a valid pattern", src)
	}
	if err := walk(e.root); err != nil {
		return nil, err
	}
	return p, nil
}

func buildAlt(pe *pathExpr) (*PatternAlt, error) {
	alt := &PatternAlt{absolute: pe.absolute}
	if pe.filter != nil {
		fc, ok := pe.filter.(*funcCall)
		if !ok || (fc.local != "key" && fc.local != "id") {
			return nil, fmt.Errorf("invalid pattern start")
		}
		alt.filter = fc
	}
	pendingDesc := false
	for _, st := range pe.steps {
		if st.axis == axDescendantOrSelf && st.test.kind == testNode && len(st.preds) == 0 {
			pendingDesc = true
			continue
		}
		if st.axis != axChild && st.axis != axAttribute {
			return nil, fmt.Errorf("axis not allowed in pattern")
		}
		alt.steps = append(alt.steps, patStep{desc: pendingDesc, axis: st.axis, test: st.test, preds: st.preds})
		pendingDesc = false
	}
	if pendingDesc {
		return nil, fmt.Errorf("trailing //")
	}
	// default priority
	alt.priority = 0.5
	if alt.filter == nil && len(alt.steps) == 1 && !alt.absolute && len(alt.steps[0].preds) == 0 {
		t := alt.steps[0].test
		switch t.kind {
		case testName:
			switch {
			case t.any:
				alt.priority = -0.5
			case t.anyName:
				alt.priority = -0.25
			default:
				alt.priority = 0
			}
		case testPI:
			if t.piName != "" {
				alt.priority = 0
			} else {
				alt.priority = -0.5
			}
		default:
			alt.priority = -0.5
		}
	}
	return alt, nil
}

// Match reports whether node n matches any alternative of the pattern.
func (p *Pattern) Match(ctx *Context, n *xmldom.Node) bool {
	for _, a := range p.Alts {
		if a.Match(ctx, n) {
			return true
		}
	}
	return false
}

// Match reports whether n matches this alternative.
func (a *PatternAlt) Match(ctx *Context, n *xmldom.Node) bool {
	if a.filter != nil {
		ev := &evaluator{}
		v, err := ev.call(a.filter, ctx.Sub(n, 1, 1))
		if err != nil {
			return false
		}
		ns, ok := v.(NodeSet)
		if !ok {
			return false
		}
		if len(a.steps) == 0 {
			for _, x := range ns {
				if x == n {
					return true
				}
			}
			return false
		}
		return false
	}
	if len(a.steps) == 0 {
		return a.absolute && n.Type == xmldom.DocumentNode
	}
	return a.matchStep(ctx, len(a.steps)-1, n)
}

func (a *PatternAlt) matchStep(ctx *Context, i int, n *xmldom.Node) bool {
	st := &a.steps[i]
	if !a.stepMatches(ctx, st, n) {
		return false
	}
	if i == 0 {
		if a.absolute {
			if st.desc {
				return n.Root().Type == xmldom.DocumentNode
			}
			return n.Parent != nil && n.Parent.Type == xmldom.DocumentNode
		}
		return true
	}
	if !st.desc {
		return n.Parent != nil && a.matchStep(ctx, i-1, n.Parent)
	}
	for anc := n.Parent; anc != nil; anc = anc.Parent {
		if a.matchStep(ctx, i-1, anc) {
			return true
		}
	}
	return false
}

func (a *PatternAlt) stepMatches(ctx *Context, st *patStep, n *xmldom.Node) bool {
	if st.axis == axAttribute {
		if n.Type != xmldom.AttributeNode {
			return false
		}
	} else if n.Type == xmldom.AttributeNode || n.Type == xmldom.DocumentNode {
		return false
	}
	if !matchTest(&st.test, st.axis, n) {
		return false
	}
	if len(st.preds) == 0 {
		return true
	}
	if n.Parent == nil {
		return false
	}
	var siblings []*xmldom.Node
	var pool []*xmldom.Node
	if st.axis == axAttribute {
		pool = n.Parent.Attrs
	} else {
		pool = n.Parent.Children
	}
	for _, c := range pool {
		if matchTest(&st.test, st.axis, c) {
			siblings = append(siblings, c)
		}
	}
	ev := &evaluator{}
	res, err := ev.filter(NodeSet(siblings), st.preds, ctx, false)
	if err != nil {
		return false
	}
	for _, r := range res {
		if r == n {
			return true
		}
	}
	return false
}
