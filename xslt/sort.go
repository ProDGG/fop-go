package xslt

import (
	"math"
	"sort"
	"unicode"

	"github.com/ProDGG/fop-go/xmldom"
	"github.com/ProDGG/fop-go/xpath"
)

type sortKey struct {
	sel                        *xpath.Expr
	dataType, order, caseOrder *avt
}

// compileSorts extracts the xsl:sort children of n. The returned node carries
// the remaining children for body compilation.
func (s *Stylesheet) compileSorts(n *xmldom.Node) ([]*sortKey, *xmldom.Node, error) {
	var keys []*sortKey
	rest := &xmldom.Node{Type: xmldom.ElementNode, Name: n.Name, URI: n.URI, Parent: n.Parent, Line: n.Line}
	for _, c := range n.Children {
		if !isXSL(c, "sort") {
			rest.Children = append(rest.Children, c)
			continue
		}
		k := &sortKey{}
		var err error
		if _, ok := c.AttrOK("select"); ok {
			k.sel, err = s.mustExpr(c, "select")
		} else {
			k.sel, err = xpath.Compile(".", nil)
		}
		if err != nil {
			return nil, nil, err
		}
		if k.dataType, err = s.optAVT(c, "data-type"); err != nil {
			return nil, nil, err
		}
		if k.order, err = s.optAVT(c, "order"); err != nil {
			return nil, nil, err
		}
		if k.caseOrder, err = s.optAVT(c, "case-order"); err != nil {
			return nil, nil, err
		}
		keys = append(keys, k)
	}
	return keys, rest, nil
}

type sortItem struct {
	node *xmldom.Node
	strs []string
	nums []float64
}

func sortNodes(c *execCtx, ns xpath.NodeSet, keys []*sortKey) (xpath.NodeSet, error) {
	type spec struct {
		numeric, desc, upperFirst bool
	}
	specs := make([]spec, len(keys))
	for i, k := range keys {
		dt, err := k.dataType.eval(c)
		if err != nil {
			return nil, err
		}
		ord, err := k.order.eval(c)
		if err != nil {
			return nil, err
		}
		co, err := k.caseOrder.eval(c)
		if err != nil {
			return nil, err
		}
		specs[i] = spec{numeric: dt == "number", desc: ord == "descending", upperFirst: co == "upper-first"}
	}
	items := make([]sortItem, len(ns))
	for i, n := range ns {
		it := sortItem{node: n, strs: make([]string, len(keys)), nums: make([]float64, len(keys))}
		sub := c.sub(n, i+1, len(ns))
		for j, k := range keys {
			v, err := sub.eval(k.sel)
			if err != nil {
				return nil, err
			}
			if specs[j].numeric {
				it.nums[j] = xpath.ToNumber(v)
			} else {
				it.strs[j] = xpath.ToString(v)
			}
		}
		items[i] = it
	}
	sort.SliceStable(items, func(a, b int) bool {
		for j := range keys {
			var r int
			if specs[j].numeric {
				x, y := items[a].nums[j], items[b].nums[j]
				switch {
				case math.IsNaN(x) && math.IsNaN(y):
					r = 0
				case math.IsNaN(x):
					r = -1
				case math.IsNaN(y):
					r = 1
				case x < y:
					r = -1
				case x > y:
					r = 1
				}
			} else {
				r = collate(items[a].strs[j], items[b].strs[j], specs[j].upperFirst)
			}
			if r == 0 {
				continue
			}
			if specs[j].desc {
				return r > 0
			}
			return r < 0
		}
		return false
	})
	out := make(xpath.NodeSet, len(items))
	for i, it := range items {
		out[i] = it.node
	}
	return out, nil
}

// primaryKey maps a rune to its primary collation weight (case-insensitive,
// with Cyrillic ё sorted directly after е).
func primaryKey(r rune) int {
	l := unicode.ToLower(r)
	switch l {
	case 'ё':
		return int('е')*2 + 1
	}
	return int(l) * 2
}

// collate compares two strings: first ignoring case, then by case.
func collate(a, b string, upperFirst bool) int {
	ra, rb := []rune(a), []rune(b)
	for i := 0; i < len(ra) && i < len(rb); i++ {
		ka, kb := primaryKey(ra[i]), primaryKey(rb[i])
		if ka != kb {
			if ka < kb {
				return -1
			}
			return 1
		}
	}
	if len(ra) != len(rb) {
		if len(ra) < len(rb) {
			return -1
		}
		return 1
	}
	for i := range ra {
		if ra[i] != rb[i] {
			aUpper := unicode.IsUpper(ra[i])
			if aUpper == upperFirst {
				return -1
			}
			return 1
		}
	}
	return 0
}
