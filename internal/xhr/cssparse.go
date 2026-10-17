// Package xhr is a Go port of the parts of Flying Saucer (xhtmlrenderer 9.1.x with
// the iText 5 PDF back end) needed to turn XHTML + CSS into PDF the way that
// renderer does.
package xhr

import (
	"strconv"
	"strings"
	"unicode"
)

// ValKind classifies a parsed CSS value.
type ValKind int

const (
	VIdent ValKind = iota
	VNumber
	VPercent
	VLength // number with a length unit
	VString
	VURI
	VColor
	VFunc
	VComma // list separator marker
	VSlash
	VOther
)

// Value is one component value of a declaration.
type Value struct {
	Kind    ValKind
	Str     string  // ident (lower-cased), string, uri or function name
	Num     float64 // number, percentage or length magnitude
	Unit    string  // length unit (lower-cased)
	R, G, B float64
	Args    []Value // function arguments
	Raw     string
}

// Decl is a declaration as written.
type Decl struct {
	Name      string
	Vals      []Value
	Important bool
}

// Ruleset is "selectors { declarations }".
type Ruleset struct {
	Selectors []*Selector
	Decls     []Decl
}

// PageRule is an @page rule with its margin boxes.
type PageRule struct {
	Name      string // page name ("" if none)
	Pseudo    string // first, left, right or ""
	Decls     []Decl
	MarginBox []MarginBoxRule
	Order     int
}

// MarginBoxRule is "@top-center { ... }" inside @page.
type MarginBoxRule struct {
	Name  string
	Decls []Decl
}

// Stylesheet is a parsed style sheet.
type Stylesheet struct {
	Rules []*Ruleset
	Pages []*PageRule
	Order int // origin order helper
}

type cssLexer struct {
	s   []rune
	pos int
}

func (l *cssLexer) eof() bool { return l.pos >= len(l.s) }

func (l *cssLexer) peek() rune {
	if l.pos < len(l.s) {
		return l.s[l.pos]
	}
	return 0
}

func (l *cssLexer) skipWS() {
	for l.pos < len(l.s) {
		c := l.s[l.pos]
		switch {
		case unicode.IsSpace(c):
			l.pos++
		case c == '/' && l.pos+1 < len(l.s) && l.s[l.pos+1] == '*':
			l.pos += 2
			for l.pos+1 < len(l.s) && !(l.s[l.pos] == '*' && l.s[l.pos+1] == '/') {
				l.pos++
			}
			l.pos += 2
			if l.pos > len(l.s) {
				l.pos = len(l.s)
			}
		case c == '<' && strings.HasPrefix(string(l.s[l.pos:min(l.pos+4, len(l.s))]), "<!--"):
			l.pos += 4
		case c == '-' && strings.HasPrefix(string(l.s[l.pos:min(l.pos+3, len(l.s))]), "-->"):
			l.pos += 3
		default:
			return
		}
	}
}

// skipBlock skips a {...} block starting at the current '{' (balanced).
func (l *cssLexer) skipBlock() {
	depth := 0
	for l.pos < len(l.s) {
		c := l.s[l.pos]
		switch c {
		case '"', '\'':
			l.readString()
			continue
		case '/':
			if l.pos+1 < len(l.s) && l.s[l.pos+1] == '*' {
				l.skipWS()
				continue
			}
		case '{':
			depth++
		case '}':
			depth--
			if depth <= 0 {
				l.pos++
				return
			}
		}
		l.pos++
	}
}

func (l *cssLexer) readString() string {
	q := l.s[l.pos]
	l.pos++
	var sb strings.Builder
	for l.pos < len(l.s) {
		c := l.s[l.pos]
		if c == q {
			l.pos++
			break
		}
		if c == '\\' && l.pos+1 < len(l.s) {
			l.pos++
			c = l.s[l.pos]
			if c == '\n' {
				l.pos++
				continue
			}
			if isHex(c) {
				j := l.pos
				for j < len(l.s) && j < l.pos+6 && isHex(l.s[j]) {
					j++
				}
				v, _ := strconv.ParseUint(string(l.s[l.pos:j]), 16, 32)
				sb.WriteRune(rune(v))
				l.pos = j
				if l.pos < len(l.s) && unicode.IsSpace(l.s[l.pos]) {
					l.pos++
				}
				continue
			}
		}
		sb.WriteRune(c)
		l.pos++
	}
	return sb.String()
}

func isHex(c rune) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func isIdentStart(c rune) bool {
	return c == '_' || c == '-' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c > 127 || c == '\\'
}

func isIdentChar(c rune) bool { return isIdentStart(c) || c >= '0' && c <= '9' }

func (l *cssLexer) readIdent() string {
	var sb strings.Builder
	for l.pos < len(l.s) && isIdentChar(l.s[l.pos]) {
		if l.s[l.pos] == '\\' && l.pos+1 < len(l.s) {
			l.pos++
		}
		sb.WriteRune(l.s[l.pos])
		l.pos++
	}
	return sb.String()
}

// ParseStylesheet parses CSS text with the error recovery of CSS 2.1; media
// selects the rules of "@media" blocks that apply (e.g. "print").
func ParseStylesheet(css, media string) *Stylesheet {
	l := &cssLexer{s: []rune(css)}
	ss := &Stylesheet{}
	l.parseRules(ss, media, true)
	return ss
}

func (l *cssLexer) parseRules(ss *Stylesheet, media string, top bool) {
	for {
		l.skipWS()
		if l.eof() {
			return
		}
		c := l.peek()
		if c == '}' {
			if top {
				l.pos++
				continue
			}
			return
		}
		if c == '@' {
			l.pos++
			name := strings.ToLower(l.readIdent())
			switch name {
			case "media":
				l.skipWS()
				start := l.pos
				for !l.eof() && l.peek() != '{' && l.peek() != ';' {
					l.pos++
				}
				mq := string(l.s[start:l.pos])
				if l.peek() != '{' {
					l.pos++
					continue
				}
				l.pos++
				if mediaMatches(mq, media) {
					l.parseRules(ss, media, false)
					l.skipWS()
					if l.peek() == '}' {
						l.pos++
					}
				} else {
					l.pos--
					l.skipBlock()
				}
			case "page":
				l.parsePage(ss, !top)
			case "font-face":
				l.skipWS()
				if l.peek() == '{' {
					l.skipBlock()
				}
			default: // @import, @charset, unknown
				for !l.eof() && l.peek() != ';' && l.peek() != '{' {
					if l.peek() == '"' || l.peek() == '\'' {
						l.readString()
						continue
					}
					l.pos++
				}
				if l.peek() == '{' {
					l.skipBlock()
				} else {
					l.pos++
				}
			}
			continue
		}
		l.parseRuleset(ss)
	}
}

func mediaMatches(mq, media string) bool {
	mq = strings.ToLower(strings.TrimSpace(mq))
	if mq == "" {
		return true
	}
	for _, m := range strings.Split(mq, ",") {
		m = strings.TrimSpace(m)
		if m == "all" || m == media {
			return true
		}
	}
	return false
}

func (l *cssLexer) parseRuleset(ss *Stylesheet) {
	start := l.pos
	for !l.eof() && l.peek() != '{' && l.peek() != '}' {
		if l.peek() == '"' || l.peek() == '\'' {
			l.readString()
			continue
		}
		l.pos++
	}
	selText := string(l.s[start:l.pos])
	if l.peek() != '{' {
		return
	}
	l.pos++
	decls := l.parseDecls()
	sels, ok := parseSelectorList(selText)
	if !ok {
		return
	}
	ss.Rules = append(ss.Rules, &Ruleset{Selectors: sels, Decls: decls})
}

// parseDecls reads declarations up to and including the closing '}'.
func (l *cssLexer) parseDecls() []Decl {
	var out []Decl
	for {
		l.skipWS()
		if l.eof() {
			return out
		}
		switch l.peek() {
		case '}':
			l.pos++
			return out
		case ';':
			l.pos++
			continue
		}
		if !isIdentStart(l.peek()) {
			l.skipDecl()
			continue
		}
		name := strings.ToLower(l.readIdent())
		l.skipWS()
		if l.peek() != ':' {
			l.skipDecl()
			continue
		}
		l.pos++
		vals, imp, ok := l.parseValue()
		if ok {
			out = append(out, Decl{Name: name, Vals: vals, Important: imp})
		}
	}
}

// skipDecl discards input up to the next ';' or the end of the block.
func (l *cssLexer) skipDecl() {
	depth := 0
	for !l.eof() {
		c := l.peek()
		switch c {
		case '"', '\'':
			l.readString()
			continue
		case '{', '(', '[':
			depth++
		case '}', ')', ']':
			if depth == 0 && c == '}' {
				return
			}
			depth--
		case ';':
			if depth <= 0 {
				l.pos++
				return
			}
		}
		l.pos++
	}
}

func (l *cssLexer) parseValue() (vals []Value, important, ok bool) {
	ok = true
	for {
		l.skipWS()
		if l.eof() {
			return
		}
		c := l.peek()
		switch {
		case c == ';':
			l.pos++
			return
		case c == '}':
			return
		case c == '!':
			l.pos++
			l.skipWS()
			if strings.EqualFold(l.readIdent(), "important") {
				important = true
			} else {
				ok = false
			}
		case c == ',':
			l.pos++
			vals = append(vals, Value{Kind: VComma})
		case c == '/':
			l.pos++
			vals = append(vals, Value{Kind: VSlash})
		case c == '"' || c == '\'':
			vals = append(vals, Value{Kind: VString, Str: l.readString()})
		case c == '#':
			l.pos++
			j := l.pos
			for j < len(l.s) && isHex(l.s[j]) {
				j++
			}
			hex := string(l.s[l.pos:j])
			l.pos = j
			if v, k := hexColor(hex); k {
				vals = append(vals, v)
			} else {
				ok = false
			}
		case c == '{':
			l.skipBlock()
			ok = false
		case c >= '0' && c <= '9' || c == '.' || (c == '-' || c == '+') && l.pos+1 < len(l.s) && (l.s[l.pos+1] >= '0' && l.s[l.pos+1] <= '9' || l.s[l.pos+1] == '.'):
			vals = append(vals, l.readNumber())
		case isIdentStart(c):
			id := l.readIdent()
			if l.peek() == '(' {
				l.pos++
				f := l.readFunction(id)
				vals = append(vals, f)
				if f.Kind == VOther {
					ok = false
				}
			} else {
				vals = append(vals, Value{Kind: VIdent, Str: strings.ToLower(id), Raw: id})
			}
		default:
			l.pos++
			ok = false
		}
	}
}

func hexColor(h string) (Value, bool) {
	switch len(h) {
	case 3:
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	case 6:
	default:
		return Value{}, false
	}
	v, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return Value{}, false
	}
	return Value{Kind: VColor, R: float64(v >> 16 & 255), G: float64(v >> 8 & 255), B: float64(v & 255)}, true
}

func (l *cssLexer) readNumber() Value {
	start := l.pos
	if l.peek() == '-' || l.peek() == '+' {
		l.pos++
	}
	for !l.eof() && (l.peek() >= '0' && l.peek() <= '9' || l.peek() == '.') {
		l.pos++
	}
	if l.peek() == 'e' || l.peek() == 'E' {
		// exponent only if followed by digits
		j := l.pos + 1
		if j < len(l.s) && (l.s[j] == '-' || l.s[j] == '+') {
			j++
		}
		if j < len(l.s) && l.s[j] >= '0' && l.s[j] <= '9' {
			l.pos = j
			for !l.eof() && l.peek() >= '0' && l.peek() <= '9' {
				l.pos++
			}
		}
	}
	num, _ := strconv.ParseFloat(string(l.s[start:l.pos]), 64)
	if l.peek() == '%' {
		l.pos++
		return Value{Kind: VPercent, Num: num}
	}
	if isIdentStart(l.peek()) {
		u := strings.ToLower(l.readIdent())
		return Value{Kind: VLength, Num: num, Unit: u}
	}
	return Value{Kind: VNumber, Num: num}
}

func (l *cssLexer) readFunction(name string) Value {
	lname := strings.ToLower(name)
	if lname == "url" {
		l.skipWS()
		var uri string
		if l.peek() == '"' || l.peek() == '\'' {
			uri = l.readString()
		} else {
			start := l.pos
			for !l.eof() && l.peek() != ')' {
				l.pos++
			}
			uri = strings.TrimSpace(string(l.s[start:l.pos]))
		}
		l.skipWS()
		if l.peek() == ')' {
			l.pos++
		}
		return Value{Kind: VURI, Str: uri}
	}
	f := Value{Kind: VFunc, Str: lname}
	for {
		l.skipWS()
		if l.eof() {
			f.Kind = VOther
			return f
		}
		switch c := l.peek(); {
		case c == ')':
			l.pos++
			if lname == "rgb" || lname == "rgba" {
				if len(f.Args) >= 3 {
					comp := func(v Value) float64 {
						if v.Kind == VPercent {
							return float64(int(v.Num*255/100 + 0.5))
						}
						return v.Num
					}
					return Value{Kind: VColor, R: comp(f.Args[0]), G: comp(f.Args[1]), B: comp(f.Args[2])}
				}
				f.Kind = VOther
			}
			return f
		case c == ',':
			l.pos++
		case c == '"' || c == '\'':
			f.Args = append(f.Args, Value{Kind: VString, Str: l.readString()})
		case c >= '0' && c <= '9' || c == '.' || c == '-' || c == '+':
			f.Args = append(f.Args, l.readNumber())
		case isIdentStart(c):
			id := l.readIdent()
			if l.peek() == '(' {
				l.pos++
				f.Args = append(f.Args, l.readFunction(id))
			} else {
				f.Args = append(f.Args, Value{Kind: VIdent, Str: strings.ToLower(id), Raw: id})
			}
		default:
			l.pos++
		}
	}
}

// parsePage reads an @page rule (the "@page" token has been consumed).
func (l *cssLexer) parsePage(ss *Stylesheet, nested bool) {
	start := l.pos
	for !l.eof() && l.peek() != '{' && l.peek() != ';' {
		l.pos++
	}
	head := strings.TrimSpace(string(l.s[start:l.pos]))
	if l.peek() != '{' {
		l.pos++
		return
	}
	if nested {
		// Flying Saucer cannot parse an @page inside @media and recovers by skipping it
		l.skipBlock()
		return
	}
	l.pos++
	pr := &PageRule{Order: len(ss.Pages)}
	if head != "" {
		name, pseudo, _ := strings.Cut(head, ":")
		pr.Name = strings.TrimSpace(name)
		pr.Pseudo = strings.ToLower(strings.TrimSpace(pseudo))
	}
	for {
		l.skipWS()
		if l.eof() {
			break
		}
		c := l.peek()
		if c == '}' {
			l.pos++
			break
		}
		if c == ';' {
			l.pos++
			continue
		}
		if c == '@' {
			l.pos++
			bn := strings.ToLower(l.readIdent())
			l.skipWS()
			if l.peek() == '{' {
				l.pos++
				pr.MarginBox = append(pr.MarginBox, MarginBoxRule{Name: bn, Decls: l.parseDecls()})
			}
			continue
		}
		// one declaration
		if !isIdentStart(c) {
			l.skipDecl()
			continue
		}
		name := strings.ToLower(l.readIdent())
		l.skipWS()
		if l.peek() != ':' {
			l.skipDecl()
			continue
		}
		l.pos++
		vals, imp, ok := l.parseValue()
		if ok {
			pr.Decls = append(pr.Decls, Decl{Name: name, Vals: vals, Important: imp})
		}
	}
	ss.Pages = append(ss.Pages, pr)
}
