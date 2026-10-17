package xhr

import (
	"strings"

	"github.com/ProDGG/fop-go/xmldom"
)

type condKind int

const (
	cID condKind = iota
	cClass
	cAttrExists
	cAttrEq
	cAttrIncludes
	cAttrDash
	cAttrPrefix
	cAttrSuffix
	cAttrContains
	cFirstChild
	cLastChild
	cLang
	cLink
	cNever // :hover, :visited, :focus ... never match in a print renderer
)

type cond struct {
	kind condKind
	name string
	val  string
}

type compound struct {
	tag   string // "" or "*" means any
	conds []cond
	comb  byte // combinator joining this compound to the previous one: ' ', '>', '+'
}

// Selector is a parsed complex selector.
type Selector struct {
	parts   []compound
	Pseudo  string // "before", "after", "" (pseudo-element of the subject)
	Spec    [3]int // id, class/attr/pseudo-class, type counts
	Ordinal int
}

func parseSelectorList(text string) ([]*Selector, bool) {
	var out []*Selector
	for _, part := range splitTop(text, ',') {
		s, ok := parseSelector(strings.TrimSpace(part))
		if !ok {
			return nil, false
		}
		out = append(out, s)
	}
	return out, len(out) > 0
}

func splitTop(s string, sep byte) []string {
	var out []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '[', '(':
			depth++
		case ']', ')':
			depth--
		case '"', '\'':
			q := s[i]
			i++
			for i < len(s) && s[i] != q {
				i++
			}
		case sep:
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

func parseSelector(s string) (*Selector, bool) {
	if s == "" {
		return nil, false
	}
	sel := &Selector{}
	rs := []rune(s)
	i := 0
	comb := byte(' ')
	first := true
	for i < len(rs) {
		// combinators
		sawWS := false
		for i < len(rs) && (rs[i] == ' ' || rs[i] == '\t' || rs[i] == '\n' || rs[i] == '\r') {
			i++
			sawWS = true
		}
		if i >= len(rs) {
			break
		}
		if rs[i] == '>' || rs[i] == '+' {
			comb = byte(rs[i])
			i++
			for i < len(rs) && (rs[i] == ' ' || rs[i] == '\t' || rs[i] == '\n') {
				i++
			}
		} else if sawWS && !first {
			comb = ' '
		}
		if i >= len(rs) {
			return nil, false
		}
		c := compound{comb: comb}
		if first {
			c.comb = 0
		}
		// type selector
		if rs[i] == '*' {
			c.tag = "*"
			i++
		} else if isIdentStart(rs[i]) {
			j := i
			for j < len(rs) && isIdentChar(rs[j]) {
				j++
			}
			c.tag = strings.ToLower(string(rs[i:j]))
			sel.Spec[2]++
			i = j
		}
		for i < len(rs) {
			ch := rs[i]
			if ch == '#' {
				j := i + 1
				for j < len(rs) && isIdentChar(rs[j]) {
					j++
				}
				c.conds = append(c.conds, cond{kind: cID, val: string(rs[i+1 : j])})
				sel.Spec[0]++
				i = j
			} else if ch == '.' {
				j := i + 1
				for j < len(rs) && isIdentChar(rs[j]) {
					j++
				}
				if j == i+1 {
					return nil, false
				}
				c.conds = append(c.conds, cond{kind: cClass, val: string(rs[i+1 : j])})
				sel.Spec[1]++
				i = j
			} else if ch == '[' {
				j := i + 1
				depth := 1
				for j < len(rs) && depth > 0 {
					if rs[j] == '[' {
						depth++
					} else if rs[j] == ']' {
						depth--
					} else if rs[j] == '"' || rs[j] == '\'' {
						q := rs[j]
						j++
						for j < len(rs) && rs[j] != q {
							j++
						}
					}
					j++
				}
				if depth != 0 {
					return nil, false
				}
				cd, ok := parseAttrCond(string(rs[i+1 : j-1]))
				if !ok {
					return nil, false
				}
				c.conds = append(c.conds, cd)
				sel.Spec[1]++
				i = j
			} else if ch == ':' {
				j := i + 1
				if j < len(rs) && rs[j] == ':' {
					j++
				}
				k := j
				for k < len(rs) && isIdentChar(rs[k]) {
					k++
				}
				name := strings.ToLower(string(rs[j:k]))
				arg := ""
				if k < len(rs) && rs[k] == '(' {
					e := k
					for e < len(rs) && rs[e] != ')' {
						e++
					}
					arg = strings.TrimSpace(string(rs[k+1 : e]))
					k = e + 1
				}
				i = k
				switch name {
				case "before", "after", "first-line", "first-letter":
					if name == "before" || name == "after" {
						sel.Pseudo = name
					} else {
						c.conds = append(c.conds, cond{kind: cNever})
					}
					sel.Spec[2]++
				case "first-child":
					c.conds = append(c.conds, cond{kind: cFirstChild})
					sel.Spec[1]++
				case "last-child":
					c.conds = append(c.conds, cond{kind: cLastChild})
					sel.Spec[1]++
				case "lang":
					c.conds = append(c.conds, cond{kind: cLang, val: arg})
					sel.Spec[1]++
				case "link":
					c.conds = append(c.conds, cond{kind: cLink})
					sel.Spec[1]++
				default: // :visited :hover :active :focus ... never match when printing
					c.conds = append(c.conds, cond{kind: cNever})
					sel.Spec[1]++
				}
			} else {
				break
			}
		}
		if c.tag == "" && len(c.conds) == 0 && sel.Pseudo == "" {
			return nil, false
		}
		if c.tag == "" {
			c.tag = "*"
		}
		sel.parts = append(sel.parts, c)
		first = false
		comb = ' '
		if i < len(rs) {
			ch := rs[i]
			if !(ch == ' ' || ch == '\t' || ch == '\n' || ch == '>' || ch == '+' || ch == '\r') {
				return nil, false
			}
		}
	}
	return sel, len(sel.parts) > 0
}

func parseAttrCond(s string) (cond, bool) {
	s = strings.TrimSpace(s)
	ops := []struct {
		op   string
		kind condKind
	}{{"~=", cAttrIncludes}, {"|=", cAttrDash}, {"^=", cAttrPrefix}, {"$=", cAttrSuffix}, {"*=", cAttrContains}, {"=", cAttrEq}}
	for _, o := range ops {
		if k := strings.Index(s, o.op); k > 0 {
			name := strings.TrimSpace(s[:k])
			val := strings.TrimSpace(s[k+len(o.op):])
			val = strings.Trim(val, "\"'")
			return cond{kind: o.kind, name: strings.ToLower(name), val: val}, true
		}
	}
	if s == "" {
		return cond{}, false
	}
	return cond{kind: cAttrExists, name: strings.ToLower(s)}, true
}

func (s *Selector) specValue() int { return s.Spec[0]<<16 | s.Spec[1]<<8 | s.Spec[2] }

// ---------- matching ----------

func elemAttr(n *xmldom.Node, name string) (string, bool) {
	for _, a := range n.Attrs {
		if strings.EqualFold(a.Name, name) && (a.Prefix == "" || a.Prefix == "xml") {
			return a.Value, true
		}
	}
	return "", false
}

func hasClass(n *xmldom.Node, cls string) bool {
	v, ok := elemAttr(n, "class")
	if !ok {
		return false
	}
	for _, f := range strings.Fields(v) {
		if f == cls {
			return true
		}
	}
	return false
}

func (c *compound) matches(n *xmldom.Node) bool {
	if c.tag != "*" && !strings.EqualFold(c.tag, n.Name) {
		return false
	}
	for _, cd := range c.conds {
		switch cd.kind {
		case cID:
			if v, ok := elemAttr(n, "id"); !ok || v != cd.val {
				return false
			}
		case cClass:
			if !hasClass(n, cd.val) {
				return false
			}
		case cAttrExists:
			if _, ok := elemAttr(n, cd.name); !ok {
				return false
			}
		case cAttrEq, cAttrIncludes, cAttrDash, cAttrPrefix, cAttrSuffix, cAttrContains:
			v, ok := elemAttr(n, cd.name)
			if !ok {
				return false
			}
			switch cd.kind {
			case cAttrEq:
				ok = v == cd.val
			case cAttrIncludes:
				ok = false
				for _, f := range strings.Fields(v) {
					if f == cd.val {
						ok = true
					}
				}
			case cAttrDash:
				ok = v == cd.val || strings.HasPrefix(v, cd.val+"-")
			case cAttrPrefix:
				ok = cd.val != "" && strings.HasPrefix(v, cd.val)
			case cAttrSuffix:
				ok = cd.val != "" && strings.HasSuffix(v, cd.val)
			case cAttrContains:
				ok = cd.val != "" && strings.Contains(v, cd.val)
			}
			if !ok {
				return false
			}
		case cFirstChild:
			if prevElem(n) != nil {
				return false
			}
		case cLastChild:
			if nextElem(n) != nil {
				return false
			}
		case cLink:
			if !strings.EqualFold(n.Name, "a") {
				return false
			}
			if _, ok := elemAttr(n, "href"); !ok {
				return false
			}
		case cLang:
			ok := false
			for p := n; p != nil && p.Type == xmldom.ElementNode; p = p.Parent {
				if v, has := elemAttr(p, "lang"); has {
					ok = strings.EqualFold(v, cd.val) || strings.HasPrefix(strings.ToLower(v), strings.ToLower(cd.val)+"-")
					break
				}
			}
			if !ok {
				return false
			}
		case cNever:
			return false
		}
	}
	return true
}

func prevElem(n *xmldom.Node) *xmldom.Node {
	if n.Parent == nil {
		return nil
	}
	var prev *xmldom.Node
	for _, c := range n.Parent.Children {
		if c == n {
			return prev
		}
		if c.Type == xmldom.ElementNode {
			prev = c
		}
	}
	return nil
}

func nextElem(n *xmldom.Node) *xmldom.Node {
	if n.Parent == nil {
		return nil
	}
	seen := false
	for _, c := range n.Parent.Children {
		if c == n {
			seen = true
			continue
		}
		if seen && c.Type == xmldom.ElementNode {
			return c
		}
	}
	return nil
}

// Matches reports whether the selector's subject is element n.
func (s *Selector) Matches(n *xmldom.Node) bool { return s.matchFrom(len(s.parts)-1, n) }

func (s *Selector) matchFrom(i int, n *xmldom.Node) bool {
	if n == nil || n.Type != xmldom.ElementNode || !s.parts[i].matches(n) {
		return false
	}
	if i == 0 {
		return true
	}
	switch s.parts[i].comb {
	case '>':
		return s.matchFrom(i-1, n.Parent)
	case '+':
		return s.matchFrom(i-1, prevElem(n))
	default:
		for p := n.Parent; p != nil && p.Type == xmldom.ElementNode; p = p.Parent {
			if s.matchFrom(i-1, p) {
				return true
			}
		}
		return false
	}
}
