package xpath

import (
	"fmt"
	"math"

	"github.com/ProDGG/fop-go/xmldom"
)

// VarResolver looks up a variable by expanded name.
type VarResolver func(uri, local string) (Value, bool)

// FuncResolver evaluates extension functions. It reports false if it does not
// know the function.
type FuncResolver func(ctx *Context, uri, local string, args []Value) (Value, bool, error)

// Context is the dynamic evaluation context.
type Context struct {
	Node    *xmldom.Node
	Pos     int
	Size    int
	Vars    VarResolver
	Funcs   FuncResolver
	Current *xmldom.Node // XSLT current() node
	Data    interface{}  // free slot for the XSLT engine
}

// Sub returns a copy of c with a different focus.
func (c *Context) Sub(n *xmldom.Node, pos, size int) *Context {
	cc := *c
	cc.Node, cc.Pos, cc.Size = n, pos, size
	return &cc
}

// Eval evaluates the expression.
func (e *Expr) Eval(ctx *Context) (Value, error) {
	ev := &evaluator{}
	return ev.eval(e.root, ctx)
}

// EvalString evaluates and converts to a string.
func (e *Expr) EvalString(ctx *Context) (string, error) {
	v, err := e.Eval(ctx)
	if err != nil {
		return "", err
	}
	return ToString(v), nil
}

// EvalBool evaluates and converts to boolean.
func (e *Expr) EvalBool(ctx *Context) (bool, error) {
	v, err := e.Eval(ctx)
	if err != nil {
		return false, err
	}
	return ToBool(v), nil
}

// EvalNodes evaluates an expression that must yield a node-set.
func (e *Expr) EvalNodes(ctx *Context) (NodeSet, error) {
	v, err := e.Eval(ctx)
	if err != nil {
		return nil, err
	}
	ns, ok := v.(NodeSet)
	if !ok {
		return nil, fmt.Errorf("xpath: expression %q does not yield a node-set", e.Source)
	}
	return ns, nil
}

type evaluator struct{}

func (ev *evaluator) eval(e expr, ctx *Context) (Value, error) {
	switch x := e.(type) {
	case *numLit:
		return x.v, nil
	case *strLit:
		return x.v, nil
	case *varRef:
		if ctx.Vars != nil {
			if v, ok := ctx.Vars(x.uri, x.local); ok {
				return v, nil
			}
		}
		return nil, fmt.Errorf("xpath: undefined variable $%s", x.qname)
	case *negate:
		v, err := ev.eval(x.e, ctx)
		if err != nil {
			return nil, err
		}
		return -ToNumber(v), nil
	case *binary:
		return ev.binary(x, ctx)
	case *unionExpr:
		l, err := ev.eval(x.l, ctx)
		if err != nil {
			return nil, err
		}
		r, err := ev.eval(x.r, ctx)
		if err != nil {
			return nil, err
		}
		ln, ok1 := l.(NodeSet)
		rn, ok2 := r.(NodeSet)
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("xpath: operands of | must be node-sets")
		}
		all := make([]*xmldom.Node, 0, len(ln)+len(rn))
		all = append(all, ln...)
		all = append(all, rn...)
		return SortNodes(all), nil
	case *funcCall:
		return ev.call(x, ctx)
	case *filterExpr:
		v, err := ev.eval(x.primary, ctx)
		if err != nil {
			return nil, err
		}
		ns, ok := v.(NodeSet)
		if !ok {
			return nil, fmt.Errorf("xpath: predicate applied to non node-set")
		}
		return ev.filter(ns, x.preds, ctx, false)
	case *pathExpr:
		return ev.path(x, ctx)
	}
	return nil, fmt.Errorf("xpath: unknown expression type %T", e)
}

func (ev *evaluator) binary(b *binary, ctx *Context) (Value, error) {
	if b.op == tOpName && (b.name == "and" || b.name == "or") {
		l, err := ev.eval(b.l, ctx)
		if err != nil {
			return nil, err
		}
		lb := ToBool(l)
		if b.name == "and" && !lb {
			return false, nil
		}
		if b.name == "or" && lb {
			return true, nil
		}
		r, err := ev.eval(b.r, ctx)
		if err != nil {
			return nil, err
		}
		return ToBool(r), nil
	}
	l, err := ev.eval(b.l, ctx)
	if err != nil {
		return nil, err
	}
	r, err := ev.eval(b.r, ctx)
	if err != nil {
		return nil, err
	}
	switch b.op {
	case tPlus:
		return ToNumber(l) + ToNumber(r), nil
	case tMinus:
		return ToNumber(l) - ToNumber(r), nil
	case tMul:
		return ToNumber(l) * ToNumber(r), nil
	case tOpName:
		switch b.name {
		case "div":
			return ToNumber(l) / ToNumber(r), nil
		case "mod":
			return math.Mod(ToNumber(l), ToNumber(r)), nil
		}
	case tEq, tNe, tLt, tLe, tGt, tGe:
		return compare(b.op, l, r), nil
	}
	return nil, fmt.Errorf("xpath: bad operator")
}

func compare(op tokKind, l, r Value) bool {
	ln, lIsNS := l.(NodeSet)
	rn, rIsNS := r.(NodeSet)
	switch {
	case lIsNS && rIsNS:
		for _, a := range ln {
			as := a.StringValue()
			for _, b := range rn {
				if cmpAtomic(op, as, b.StringValue()) {
					return true
				}
			}
		}
		return false
	case lIsNS:
		return cmpNodeSet(op, ln, r, false)
	case rIsNS:
		return cmpNodeSet(op, rn, l, true)
	}
	return cmpAtomic(op, l, r)
}

func cmpNodeSet(op tokKind, ns NodeSet, other Value, swapped bool) bool {
	if b, ok := other.(bool); ok {
		nb := len(ns) > 0
		if swapped {
			return cmpAtomic(op, b, nb)
		}
		return cmpAtomic(op, nb, b)
	}
	for _, n := range ns {
		var a Value = n.StringValue()
		if _, isNum := other.(float64); isNum {
			a = ToNumber(a)
		}
		var res bool
		if swapped {
			res = cmpAtomic(op, other, a)
		} else {
			res = cmpAtomic(op, a, other)
		}
		if res {
			return true
		}
	}
	return false
}

func cmpAtomic(op tokKind, l, r Value) bool {
	if op == tEq || op == tNe {
		var eq bool
		_, lb := l.(bool)
		_, rb := r.(bool)
		_, ln := l.(float64)
		_, rn := r.(float64)
		switch {
		case lb || rb:
			eq = ToBool(l) == ToBool(r)
		case ln || rn:
			eq = ToNumber(l) == ToNumber(r)
		default:
			eq = ToString(l) == ToString(r)
		}
		if op == tEq {
			return eq
		}
		return !eq
	}
	a, b := ToNumber(l), ToNumber(r)
	switch op {
	case tLt:
		return a < b
	case tLe:
		return a <= b
	case tGt:
		return a > b
	case tGe:
		return a >= b
	}
	return false
}

func (ev *evaluator) filter(ns NodeSet, preds []expr, ctx *Context, reverse bool) (NodeSet, error) {
	cur := ns
	for _, p := range preds {
		var next []*xmldom.Node
		size := len(cur)
		for i, n := range cur {
			pos := i + 1
			if reverse {
				pos = size - i
			}
			sub := ctx.Sub(n, pos, size)
			v, err := ev.eval(p, sub)
			if err != nil {
				return nil, err
			}
			keep := false
			if f, ok := v.(float64); ok {
				keep = float64(pos) == f
			} else {
				keep = ToBool(v)
			}
			if keep {
				next = append(next, n)
			}
		}
		cur = next
	}
	return cur, nil
}

func (ev *evaluator) path(p *pathExpr, ctx *Context) (Value, error) {
	var cur NodeSet
	switch {
	case p.filter != nil:
		v, err := ev.eval(p.filter, ctx)
		if err != nil {
			return nil, err
		}
		ns, ok := v.(NodeSet)
		if !ok {
			return nil, fmt.Errorf("xpath: path applied to non node-set")
		}
		cur = ns
	case p.absolute:
		cur = NodeSet{ctx.Node.Root()}
	default:
		cur = NodeSet{ctx.Node}
	}
	for _, st := range p.steps {
		var out []*xmldom.Node
		for _, n := range cur {
			res, err := ev.step(st, n, ctx)
			if err != nil {
				return nil, err
			}
			out = append(out, res...)
		}
		if len(cur) > 1 {
			cur = SortNodes(out)
		} else {
			cur = SortNodes(out)
		}
	}
	return cur, nil
}

func (ev *evaluator) step(st *step, n *xmldom.Node, ctx *Context) ([]*xmldom.Node, error) {
	var cand []*xmldom.Node
	collect := func(c *xmldom.Node) {
		if matchTest(&st.test, st.axis, c) {
			cand = append(cand, c)
		}
	}
	walkAxis(st.axis, n, collect)
	if len(st.preds) == 0 {
		return cand, nil
	}
	// candidates are in axis order (reverse axes yield nearest first)
	if st.axis.reverse() {
		// collect delivered in reverse document order; filter treats index order as proximity
		res, err := ev.filterAxisOrder(cand, st.preds, ctx)
		if err != nil {
			return nil, err
		}
		return res, nil
	}
	res, err := ev.filter(NodeSet(cand), st.preds, ctx, false)
	return res, err
}

// filterAxisOrder filters nodes given in proximity (axis) order.
func (ev *evaluator) filterAxisOrder(cand []*xmldom.Node, preds []expr, ctx *Context) (NodeSet, error) {
	cur := cand
	for _, p := range preds {
		var next []*xmldom.Node
		size := len(cur)
		for i, n := range cur {
			pos := i + 1
			sub := ctx.Sub(n, pos, size)
			v, err := ev.eval(p, sub)
			if err != nil {
				return nil, err
			}
			keep := false
			if f, ok := v.(float64); ok {
				keep = float64(pos) == f
			} else {
				keep = ToBool(v)
			}
			if keep {
				next = append(next, n)
			}
		}
		cur = next
	}
	return NodeSet(cur), nil
}

func matchTest(t *nodeTest, ax axisKind, n *xmldom.Node) bool {
	switch t.kind {
	case testNode:
		return true
	case testText:
		return n.Type == xmldom.TextNode
	case testComment:
		return n.Type == xmldom.CommentNode
	case testPI:
		return n.Type == xmldom.PINode && (t.piName == "" || t.piName == n.Name)
	}
	// name test: principal node type
	if ax == axAttribute {
		if n.Type != xmldom.AttributeNode {
			return false
		}
	} else if ax == axNamespace {
		return n.Type == xmldom.NamespaceNode
	} else if n.Type != xmldom.ElementNode {
		return false
	}
	if t.any {
		return true
	}
	if n.URI != t.uri {
		return false
	}
	return t.anyName || n.Name == t.local
}

func walkAxis(ax axisKind, n *xmldom.Node, f func(*xmldom.Node)) {
	switch ax {
	case axChild:
		for _, c := range n.Children {
			f(c)
		}
	case axDescendant:
		descend(n, f)
	case axDescendantOrSelf:
		f(n)
		descend(n, f)
	case axSelf:
		f(n)
	case axParent:
		if n.Parent != nil {
			f(n.Parent)
		}
	case axAncestor:
		for p := n.Parent; p != nil; p = p.Parent {
			f(p)
		}
	case axAncestorOrSelf:
		for p := n; p != nil; p = p.Parent {
			f(p)
		}
	case axAttribute:
		if n.Type == xmldom.ElementNode {
			for _, a := range n.Attrs {
				f(a)
			}
		}
	case axFollowingSibling:
		if n.Parent == nil || n.Type == xmldom.AttributeNode {
			return
		}
		sib := n.Parent.Children
		i := indexOf(sib, n)
		for _, c := range sib[i+1:] {
			f(c)
		}
	case axPrecedingSibling:
		if n.Parent == nil || n.Type == xmldom.AttributeNode {
			return
		}
		sib := n.Parent.Children
		i := indexOf(sib, n)
		for j := i - 1; j >= 0; j-- {
			f(sib[j])
		}
	case axFollowing:
		start := n
		if n.Type == xmldom.AttributeNode {
			start = n.Parent
			if start == nil {
				return
			}
			descend(start, f)
		}
		for cur := start; cur != nil && cur.Parent != nil; cur = cur.Parent {
			sib := cur.Parent.Children
			i := indexOf(sib, cur)
			for _, c := range sib[i+1:] {
				f(c)
				descend(c, f)
			}
		}
	case axPreceding:
		start := n
		if n.Type == xmldom.AttributeNode {
			start = n.Parent
		}
		for cur := start; cur != nil && cur.Parent != nil; cur = cur.Parent {
			sib := cur.Parent.Children
			i := indexOf(sib, cur)
			for j := i - 1; j >= 0; j-- {
				descendReverse(sib[j], f)
			}
		}
	}
}

func descend(n *xmldom.Node, f func(*xmldom.Node)) {
	for _, c := range n.Children {
		f(c)
		if len(c.Children) > 0 {
			descend(c, f)
		}
	}
}

// descendReverse visits n's subtree in reverse document order.
func descendReverse(n *xmldom.Node, f func(*xmldom.Node)) {
	for i := len(n.Children) - 1; i >= 0; i-- {
		descendReverse(n.Children[i], f)
	}
	f(n)
}

func indexOf(list []*xmldom.Node, n *xmldom.Node) int {
	for i, c := range list {
		if c == n {
			return i
		}
	}
	return -1
}
