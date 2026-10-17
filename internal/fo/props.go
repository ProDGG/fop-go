// Package fo reads an XSL-FO document, lays it out and renders it to PDF.
package fo

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/ProDGG/fop-go/xmldom"
)

// Node is an element or text run of the XSL-FO tree.
type Node struct {
	Name     string // local name, "#text" for character data
	Text     string
	Props    map[string]string
	Parent   *Node
	Children []*Node
	Src      *xmldom.Node

	fs     float64
	fsDone bool
}

var inherited = map[string]bool{
	"color": true, "font-family": true, "font-size": true, "font-style": true, "font-weight": true,
	"font-variant": true, "font-stretch": true, "letter-spacing": true, "word-spacing": true,
	"line-height": true, "text-align": true, "text-align-last": true, "text-indent": true,
	"text-transform": true, "text-decoration": true, "white-space-collapse": true,
	"white-space-treatment": true, "linefeed-treatment": true, "wrap-option": true, "white-space": true,
	"keep-together": true, "orphans": true, "widows": true, "hyphenate": true, "language": true,
	"country": true, "start-indent": false, "end-indent": false, "visibility": true, "writing-mode": true,
	"direction": true, "display-align": false, "border-collapse": true, "border-separation": true,
	"reference-orientation": false, "line-height-shift-adjustment": true, "role": true,
	"font-kerning": true, "text-shadow": true,
}

var initial = map[string]string{
	"font-family": "serif", "font-size": "12pt", "font-weight": "normal", "font-style": "normal",
	"color": "black", "line-height": "normal", "text-align": "start", "white-space-collapse": "true",
	"linefeed-treatment": "treat-as-space", "white-space-treatment": "ignore-if-surrounding-linefeed",
	"wrap-option": "wrap", "orphans": "2", "widows": "2", "border-collapse": "collapse",
	"text-transform": "none", "text-decoration": "none",
}

// Get returns the specified or inherited value of a property ("" if none).
func (n *Node) Get(name string) string {
	inh := inherited[name]
	for c := n; c != nil; c = c.Parent {
		if v, ok := c.Props[name]; ok {
			if v != "inherit" {
				return v
			}
			continue
		}
		if !inh {
			break
		}
	}
	return initial[name]
}

// Own returns the value specified directly on n.
func (n *Node) Own(name string) (string, bool) {
	v, ok := n.Props[name]
	if ok && v == "inherit" && n.Parent != nil {
		return n.Parent.Get(name), true
	}
	return v, ok
}

// Has reports whether the property is specified on n itself.
func (n *Node) Has(name string) bool { _, ok := n.Props[name]; return ok }

// Str returns the property value or def when empty.
func (n *Node) Str(name, def string) string {
	if v := n.Get(name); v != "" {
		return v
	}
	return def
}

// Elem returns the nearest ancestor-or-self that is not a text node.
func (n *Node) elem() *Node {
	for c := n; c != nil; c = c.Parent {
		if c.Name != "#text" {
			return c
		}
	}
	return n
}

// Ancestor finds the nearest ancestor with the given name.
func (n *Node) Ancestor(name string) *Node {
	for c := n.Parent; c != nil; c = c.Parent {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// ---------- colours ----------

// Color is an RGB colour; A == 0 means transparent.
type Color struct {
	R, G, B float64
	A       float64
}

var namedColors = map[string][3]int{
	"black": {0, 0, 0}, "white": {255, 255, 255}, "red": {255, 0, 0}, "green": {0, 128, 0}, "blue": {0, 0, 255},
	"yellow": {255, 255, 0}, "cyan": {0, 255, 255}, "aqua": {0, 255, 255}, "magenta": {255, 0, 255},
	"fuchsia": {255, 0, 255}, "gray": {128, 128, 128}, "grey": {128, 128, 128}, "silver": {192, 192, 192},
	"maroon": {128, 0, 0}, "purple": {128, 0, 128}, "olive": {128, 128, 0}, "navy": {0, 0, 128},
	"teal": {0, 128, 128}, "lime": {0, 255, 0}, "orange": {255, 165, 0}, "brown": {165, 42, 42},
	"pink": {255, 192, 203}, "gold": {255, 215, 0}, "lightgray": {211, 211, 211}, "lightgrey": {211, 211, 211},
	"darkgray": {169, 169, 169}, "darkgrey": {169, 169, 169}, "lightblue": {173, 216, 230},
	"darkblue": {0, 0, 139}, "darkred": {139, 0, 0}, "darkgreen": {0, 100, 0}, "lightgreen": {144, 238, 144},
	"lightyellow": {255, 255, 224}, "whitesmoke": {245, 245, 245}, "gainsboro": {220, 220, 220},
	"crimson": {220, 20, 60}, "indigo": {75, 0, 130}, "violet": {238, 130, 238}, "coral": {255, 127, 80},
	"salmon": {250, 128, 114}, "khaki": {240, 230, 140}, "beige": {245, 245, 220}, "ivory": {255, 255, 240},
	"orchid": {218, 112, 214}, "tan": {210, 180, 140}, "turquoise": {64, 224, 208}, "steelblue": {70, 130, 180},
	"royalblue": {65, 105, 225}, "skyblue": {135, 206, 235}, "slategray": {112, 128, 144}, "dimgray": {105, 105, 105},
}

// ParseColor parses an FO colour value.
func ParseColor(s string) (Color, bool) {
	s = strings.TrimSpace(strings.ToLower(s))
	switch {
	case s == "" || s == "transparent":
		return Color{}, s == "transparent"
	case strings.HasPrefix(s, "#"):
		h := s[1:]
		if len(h) == 3 {
			h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
		}
		if len(h) != 6 {
			return Color{}, false
		}
		v, err := strconv.ParseUint(h, 16, 32)
		if err != nil {
			return Color{}, false
		}
		return Color{float64(v>>16&255) / 255, float64(v>>8&255) / 255, float64(v&255) / 255, 1}, true
	case strings.HasPrefix(s, "rgb-icc("):
		inner := s[strings.IndexByte(s, '(')+1:]
		inner = strings.TrimSuffix(inner, ")")
		parts := strings.Split(inner, ",")
		if len(parts) >= 3 {
			return ParseColor("rgb(" + strings.Join(parts[:3], ",") + ")")
		}
		return Color{}, false
	case strings.HasPrefix(s, "rgb("):
		inner := strings.TrimSuffix(s[4:], ")")
		parts := strings.Split(inner, ",")
		if len(parts) != 3 {
			return Color{}, false
		}
		var c [3]float64
		for i, p := range parts {
			p = strings.TrimSpace(p)
			if strings.HasSuffix(p, "%") {
				f, err := strconv.ParseFloat(strings.TrimSuffix(p, "%"), 64)
				if err != nil {
					return Color{}, false
				}
				c[i] = f / 100
			} else {
				f, err := strconv.ParseFloat(p, 64)
				if err != nil {
					return Color{}, false
				}
				c[i] = f / 255
			}
			c[i] = math.Max(0, math.Min(1, c[i]))
		}
		return Color{c[0], c[1], c[2], 1}, true
	}
	if rgb, ok := namedColors[s]; ok {
		return Color{float64(rgb[0]) / 255, float64(rgb[1]) / 255, float64(rgb[2]) / 255, 1}, true
	}
	return Color{}, false
}

// ---------- shorthand expansion ----------

var sideNames = []string{"top", "right", "bottom", "left"}

func logicalSide(s string) string {
	switch s {
	case "before":
		return "top"
	case "after":
		return "bottom"
	case "start":
		return "left"
	case "end":
		return "right"
	}
	return s
}

// splitTop splits on whitespace but keeps parenthesised groups together.
func splitTop(s string) []string {
	var out []string
	depth := 0
	start := -1
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '(':
			depth++
			if start < 0 {
				start = i
			}
		case c == ')':
			if depth > 0 {
				depth--
			}
		case (c == ' ' || c == '\t' || c == '\n' || c == '\r') && depth == 0:
			if start >= 0 {
				out = append(out, s[start:i])
				start = -1
			}
		default:
			if start < 0 {
				start = i
			}
		}
	}
	if start >= 0 {
		out = append(out, s[start:])
	}
	return out
}

func fourValues(vals []string) [4]string {
	switch len(vals) {
	case 1:
		return [4]string{vals[0], vals[0], vals[0], vals[0]}
	case 2:
		return [4]string{vals[0], vals[1], vals[0], vals[1]}
	case 3:
		return [4]string{vals[0], vals[1], vals[2], vals[1]}
	case 4:
		return [4]string{vals[0], vals[1], vals[2], vals[3]}
	}
	return [4]string{}
}

func isBorderStyle(s string) bool {
	switch s {
	case "none", "hidden", "dotted", "dashed", "solid", "double", "groove", "ridge", "inset", "outset":
		return true
	}
	return false
}

func isLengthLike(s string) bool {
	if s == "thin" || s == "medium" || s == "thick" {
		return true
	}
	if s == "" {
		return false
	}
	c := s[0]
	return (c >= '0' && c <= '9') || c == '.' || c == '-' || c == '+'
}

func expandBorder(props map[string]string, sides []string, val string) {
	var width, style, color string
	for _, tok := range splitTop(val) {
		switch {
		case isBorderStyle(tok):
			style = tok
		case isLengthLike(tok):
			width = tok
		default:
			color = tok
		}
	}
	for _, s := range sides {
		if style != "" {
			props["border-"+s+"-style"] = style
		}
		if width != "" {
			props["border-"+s+"-width"] = width
		}
		if color != "" {
			props["border-"+s+"-color"] = color
		}
		if style == "" && width == "" && color == "" {
			props["border-"+s+"-style"] = "none"
		}
	}
}

func parseFontShorthand(props map[string]string, val string) {
	toks := splitTop(val)
	i := 0
	for ; i < len(toks); i++ {
		t := strings.ToLower(toks[i])
		switch {
		case t == "italic" || t == "oblique":
			props["font-style"] = t
		case t == "normal":
		case t == "bold" || t == "bolder" || t == "lighter" || (len(t) == 3 && t[1:] == "00" && t[0] >= '1' && t[0] <= '9'):
			props["font-weight"] = t
		case t == "small-caps":
			props["font-variant"] = t
		default:
			goto size
		}
	}
size:
	if i >= len(toks) {
		return
	}
	sz := toks[i]
	if j := strings.IndexByte(sz, '/'); j > 0 {
		props["line-height"] = sz[j+1:]
		sz = sz[:j]
	}
	props["font-size"] = sz
	i++
	if i < len(toks) {
		props["font-family"] = strings.Join(toks[i:], " ")
	}
}

func parseBackground(props map[string]string, val string) {
	for _, tok := range splitTop(val) {
		switch {
		case strings.HasPrefix(tok, "url("):
			props["background-image"] = tok
		case tok == "repeat" || tok == "no-repeat" || tok == "repeat-x" || tok == "repeat-y":
			props["background-repeat"] = tok
		case tok == "scroll" || tok == "fixed":
		default:
			if _, ok := ParseColor(tok); ok {
				props["background-color"] = tok
			}
		}
	}
}

// setProps expands shorthands and logical names into the canonical property set.
func setProps(attrs [][2]string) map[string]string {
	props := map[string]string{}
	// pass 1: shorthands
	for _, a := range attrs {
		name, val := a[0], a[1]
		switch name {
		case "margin":
			v := fourValues(splitTop(val))
			for i, s := range sideNames {
				if v[i] != "" {
					props["margin-"+s] = v[i]
				}
			}
		case "padding":
			v := fourValues(splitTop(val))
			for i, s := range sideNames {
				if v[i] != "" {
					props["padding-"+s] = v[i]
				}
			}
		case "border":
			expandBorder(props, sideNames, val)
		case "border-top", "border-right", "border-bottom", "border-left":
			expandBorder(props, []string{strings.TrimPrefix(name, "border-")}, val)
		case "border-before", "border-after", "border-start", "border-end":
			expandBorder(props, []string{logicalSide(strings.TrimPrefix(name, "border-"))}, val)
		case "border-width", "border-style", "border-color":
			v := fourValues(splitTop(val))
			for i, s := range sideNames {
				if v[i] != "" {
					props["border-"+s+"-"+strings.TrimPrefix(name, "border-")] = v[i]
				}
			}
		case "background":
			parseBackground(props, val)
		case "font":
			parseFontShorthand(props, val)
		case "page-break-before", "page-break-after":
			k := "break-" + strings.TrimPrefix(name, "page-break-")
			switch val {
			case "always":
				props[k] = "page"
			case "left":
				props[k] = "even-page"
			case "right":
				props[k] = "odd-page"
			}
		case "page-break-inside":
			if val == "avoid" {
				props["keep-together"] = "always"
			}
		}
	}
	// pass 2: specific properties
	for _, a := range attrs {
		name, val := a[0], a[1]
		switch name {
		case "margin", "padding", "border", "border-top", "border-right", "border-bottom", "border-left",
			"border-before", "border-after", "border-start", "border-end", "border-width", "border-style",
			"border-color", "background", "font", "page-break-before", "page-break-after", "page-break-inside":
			continue
		}
		name = strings.TrimSuffix(name, ".length")
		for _, pfx := range []string{"border", "padding"} {
			if strings.HasPrefix(name, pfx+"-") {
				parts := strings.Split(name, "-")
				if len(parts) >= 2 {
					parts[1] = logicalSide(parts[1])
					name = strings.Join(parts, "-")
				}
			}
		}
		if strings.HasSuffix(name, ".conditionality") || strings.HasSuffix(name, ".precedence") {
			continue
		}
		switch name {
		case "keep-together.within-page", "keep-together.within-column":
			name = "keep-together"
		case "keep-with-next.within-page", "keep-with-next.within-column":
			name = "keep-with-next"
		case "keep-with-previous.within-page", "keep-with-previous.within-column":
			name = "keep-with-previous"
		case "break-before.page":
			name = "break-before"
		}
		props[name] = val
	}
	return props
}

// ---------- lengths and property expressions ----------

// LengthCtx supplies the context needed to resolve relative lengths.
type LengthCtx struct {
	FontSize float64
	Percent  float64 // base for '%' (0 when not allowed)
	PxPt     float64 // points per pixel
	Prop     func(name string) string
	Funcs    map[string]func(args []float64) float64
	Node     *Node
}

type exprParser struct {
	s   string
	pos int
	ctx *LengthCtx
	err error
}

// EvalLength evaluates an FO length / numeric expression to points.
func EvalLength(s string, ctx *LengthCtx) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty length")
	}
	switch s {
	case "thin":
		return 0.5, nil
	case "medium":
		return 1, nil
	case "thick":
		return 2, nil
	case "auto", "none":
		return 0, fmt.Errorf("not a length: %s", s)
	}
	p := &exprParser{s: s, ctx: ctx}
	v := p.additive()
	p.skipWS()
	if p.err == nil && p.pos < len(p.s) {
		p.err = fmt.Errorf("unexpected %q in %q", p.s[p.pos:], s)
	}
	return v, p.err
}

func (p *exprParser) skipWS() {
	for p.pos < len(p.s) && (p.s[p.pos] == ' ' || p.s[p.pos] == '\t') {
		p.pos++
	}
}

func (p *exprParser) additive() float64 {
	v := p.multiplicative()
	for {
		p.skipWS()
		if p.pos >= len(p.s) {
			return v
		}
		switch p.s[p.pos] {
		case '+':
			p.pos++
			v += p.multiplicative()
		case '-':
			p.pos++
			v -= p.multiplicative()
		default:
			return v
		}
	}
}

func (p *exprParser) multiplicative() float64 {
	v := p.unary()
	for {
		p.skipWS()
		if p.pos >= len(p.s) {
			return v
		}
		switch {
		case p.s[p.pos] == '*':
			p.pos++
			v *= p.unary()
		case strings.HasPrefix(p.s[p.pos:], "div"):
			p.pos += 3
			d := p.unary()
			if d == 0 {
				p.err = fmt.Errorf("division by zero")
				return 0
			}
			v /= d
		case strings.HasPrefix(p.s[p.pos:], "mod"):
			p.pos += 3
			v = math.Mod(v, p.unary())
		default:
			return v
		}
	}
}

func (p *exprParser) unary() float64 {
	p.skipWS()
	if p.pos < len(p.s) && p.s[p.pos] == '-' {
		p.pos++
		return -p.unary()
	}
	if p.pos < len(p.s) && p.s[p.pos] == '+' {
		p.pos++
	}
	return p.primary()
}

func (p *exprParser) primary() float64 {
	p.skipWS()
	if p.pos >= len(p.s) {
		p.err = fmt.Errorf("unexpected end of expression %q", p.s)
		return 0
	}
	c := p.s[p.pos]
	if c == '(' {
		p.pos++
		v := p.additive()
		p.skipWS()
		if p.pos < len(p.s) && p.s[p.pos] == ')' {
			p.pos++
		} else {
			p.err = fmt.Errorf("missing ) in %q", p.s)
		}
		return v
	}
	if (c >= '0' && c <= '9') || c == '.' {
		start := p.pos
		for p.pos < len(p.s) && ((p.s[p.pos] >= '0' && p.s[p.pos] <= '9') || p.s[p.pos] == '.') {
			p.pos++
		}
		num, err := strconv.ParseFloat(p.s[start:p.pos], 64)
		if err != nil {
			p.err = err
			return 0
		}
		ustart := p.pos
		for p.pos < len(p.s) && ((p.s[p.pos] >= 'a' && p.s[p.pos] <= 'z') || p.s[p.pos] == '%') {
			p.pos++
		}
		unit := p.s[ustart:p.pos]
		if unit == "div" || unit == "mod" { // operator, not a unit
			p.pos = ustart
			unit = ""
		}
		switch unit {
		case "", "pt":
			return num
		case "px":
			if p.ctx.PxPt > 0 {
				return num * p.ctx.PxPt
			}
			return num
		case "mm":
			return num * 72 / 25.4
		case "cm":
			return num * 72 / 2.54
		case "in":
			return num * 72
		case "pc":
			return num * 12
		case "em":
			return num * p.ctx.FontSize
		case "ex":
			return num * p.ctx.FontSize * 0.5
		case "%":
			return num * p.ctx.Percent / 100
		}
		p.err = fmt.Errorf("unknown unit %q", unit)
		return 0
	}
	// function call
	start := p.pos
	for p.pos < len(p.s) && (p.s[p.pos] == '-' || p.s[p.pos] == '_' || (p.s[p.pos] >= 'a' && p.s[p.pos] <= 'z') || (p.s[p.pos] >= 'A' && p.s[p.pos] <= 'Z')) {
		p.pos++
	}
	name := p.s[start:p.pos]
	if name == "" || p.pos >= len(p.s) || p.s[p.pos] != '(' {
		p.err = fmt.Errorf("cannot parse %q as a length", p.s)
		return 0
	}
	p.pos++
	var rawArgs []string
	depth := 0
	argStart := p.pos
	for p.pos < len(p.s) {
		ch := p.s[p.pos]
		if ch == '(' {
			depth++
		} else if ch == ')' {
			if depth == 0 {
				break
			}
			depth--
		} else if ch == ',' && depth == 0 {
			rawArgs = append(rawArgs, strings.TrimSpace(p.s[argStart:p.pos]))
			argStart = p.pos + 1
		}
		p.pos++
	}
	if p.pos >= len(p.s) {
		p.err = fmt.Errorf("missing ) in %q", p.s)
		return 0
	}
	if last := strings.TrimSpace(p.s[argStart:p.pos]); last != "" || len(rawArgs) > 0 {
		rawArgs = append(rawArgs, last)
	}
	p.pos++
	return p.call(name, rawArgs)
}

func (p *exprParser) call(name string, raw []string) float64 {
	str := func(i int) string {
		if i < len(raw) {
			return strings.Trim(raw[i], `"'`)
		}
		return ""
	}
	num := func(i int) float64 {
		if i >= len(raw) {
			return 0
		}
		v, err := EvalLength(raw[i], p.ctx)
		if err != nil && p.err == nil {
			p.err = err
		}
		return v
	}
	switch name {
	case "floor":
		return math.Floor(num(0))
	case "ceiling":
		return math.Ceil(num(0))
	case "round":
		return math.Floor(num(0) + 0.5)
	case "abs":
		return math.Abs(num(0))
	case "min":
		return math.Min(num(0), num(1))
	case "max":
		return math.Max(num(0), num(1))
	case "from-parent", "inherited-property-value", "from-nearest-specified-value", "from-table-column":
		if p.ctx.Prop == nil {
			return 0
		}
		v := p.ctx.Prop(name + ":" + str(0))
		if v == "" {
			return 0
		}
		r, err := EvalLength(v, &LengthCtx{FontSize: p.ctx.FontSize, Percent: p.ctx.Percent, PxPt: p.ctx.PxPt})
		if err != nil {
			return 0
		}
		return r
	case "proportional-column-width":
		return num(0) // resolved by the table layout, which looks at the raw string
	case "rgb":
		return 0
	}
	if f, ok := p.ctx.Funcs[name]; ok {
		var args []float64
		for i := range raw {
			args = append(args, num(i))
		}
		return f(args)
	}
	if p.err == nil {
		p.err = fmt.Errorf("unknown function %s() in %q", name, p.s)
	}
	return 0
}

// FontSizeOf computes the font size of n in points.
func (n *Node) FontSize(pxPt float64) float64 {
	e := n.elem()
	if e.fsDone {
		return e.fs
	}
	parent := 12.0
	if e.Parent != nil {
		parent = e.Parent.FontSize(pxPt)
	}
	val, ok := e.Props["font-size"]
	size := parent
	if !ok {
		if e.Parent == nil {
			size = 12
		}
	} else {
		switch strings.TrimSpace(val) {
		case "inherit":
		case "xx-small":
			size = 7
		case "x-small":
			size = 8.3
		case "small":
			size = 10
		case "medium":
			size = 12
		case "large":
			size = 14.4
		case "x-large":
			size = 17.3
		case "xx-large":
			size = 20.7
		case "larger":
			size = parent * 1.2
		case "smaller":
			size = parent / 1.2
		default:
			v, err := EvalLength(val, &LengthCtx{FontSize: parent, Percent: parent, PxPt: pxPt})
			if err == nil && v > 0 {
				size = v
			}
		}
	}
	e.fs, e.fsDone = size, true
	return size
}
