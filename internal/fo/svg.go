package fo

import (
	"math"
	"strconv"
	"strings"

	"github.com/ProDGG/fop-go/internal/pdf"
	"github.com/ProDGG/fop-go/xmldom"
)

// A small SVG renderer (the role Batik plays inside Apache FOP): basic shapes,
// paths, gradients, text, transforms, opacity and dashes are drawn as PDF vector
// graphics.

type svgDoc struct {
	root               *xmldom.Node
	w, h               float64 // intrinsic size in points
	vbX, vbY, vbW, vbH float64
	hasVB              bool
	align              string
	slice              bool
	ids                map[string]*xmldom.Node
	pxPt               float64
}

const svgNS = "http://www.w3.org/2000/svg"

func attr(n *xmldom.Node, name string) string {
	for _, a := range n.Attrs {
		if a.Name == name && a.Prefix == "" {
			return strings.TrimSpace(a.Value)
		}
	}
	return ""
}

func svgNumber(s string) (float64, bool) {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f, err == nil
}

// svgLen parses a length; percent is the base for % values.
func svgLen(s string, pxPt, fontSize, percent float64) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	units := []struct {
		suf string
		mul float64
	}{{"px", pxPt}, {"pt", 1}, {"pc", 12}, {"mm", 72 / 25.4}, {"cm", 72 / 2.54}, {"in", 72}, {"em", fontSize}, {"ex", fontSize / 2}}
	if strings.HasSuffix(s, "%") {
		if f, ok := svgNumber(strings.TrimSuffix(s, "%")); ok {
			return f * percent / 100, true
		}
		return 0, false
	}
	for _, u := range units {
		if strings.HasSuffix(s, u.suf) {
			if f, ok := svgNumber(strings.TrimSuffix(s, u.suf)); ok {
				return f * u.mul, true
			}
			return 0, false
		}
	}
	f, ok := svgNumber(s)
	return f * pxPt, ok // unit-less user units
}

func (e *Engine) parseSVG(root *xmldom.Node) (*imageData, error) {
	d := &svgDoc{root: root, ids: map[string]*xmldom.Node{}, pxPt: e.pxPt, align: "xMidYMid"}
	var walk func(n *xmldom.Node)
	walk = func(n *xmldom.Node) {
		if n.Type != xmldom.ElementNode {
			return
		}
		if id := attr(n, "id"); id != "" {
			d.ids[id] = n
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	if vb := strings.Fields(strings.ReplaceAll(attr(root, "viewBox"), ",", " ")); len(vb) == 4 {
		var v [4]float64
		ok := true
		for i := range v {
			f, k := svgNumber(vb[i])
			v[i], ok = f, ok && k
		}
		if ok && v[2] > 0 && v[3] > 0 {
			d.vbX, d.vbY, d.vbW, d.vbH, d.hasVB = v[0], v[1], v[2], v[3], true
		}
	}
	if par := strings.Fields(attr(root, "preserveAspectRatio")); len(par) > 0 {
		i := 0
		if par[0] == "defer" {
			i++
		}
		if i < len(par) {
			d.align = par[i]
		}
		d.slice = len(par) > i+1 && par[i+1] == "slice"
	}
	w, wok := svgLen(attr(root, "width"), e.pxPt, 12, 0)
	h, hok := svgLen(attr(root, "height"), e.pxPt, 12, 0)
	if strings.HasSuffix(attr(root, "width"), "%") {
		wok = false
	}
	if strings.HasSuffix(attr(root, "height"), "%") {
		hok = false
	}
	switch {
	case wok && hok:
	case d.hasVB && wok:
		h = w * d.vbH / d.vbW
	case d.hasVB && hok:
		w = h * d.vbW / d.vbH
	case d.hasVB:
		w, h = d.vbW*e.pxPt, d.vbH*e.pxPt
	default:
		w, h = 100*e.pxPt, 100*e.pxPt // Batik's default viewport
	}
	d.w, d.h = w, h
	return &imageData{wPt: w, hPt: h, svg: d, hasSVG: true}, nil
}

// ---------- matrices ----------

type mat [6]float64

var ident = mat{1, 0, 0, 1, 0, 0}

// mul returns m·n (n is applied first).
func (m mat) mul(n mat) mat {
	return mat{
		m[0]*n[0] + m[2]*n[1], m[1]*n[0] + m[3]*n[1],
		m[0]*n[2] + m[2]*n[3], m[1]*n[2] + m[3]*n[3],
		m[0]*n[4] + m[2]*n[5] + m[4], m[1]*n[4] + m[3]*n[5] + m[5],
	}
}

func (m mat) apply(x, y float64) (float64, float64) {
	return m[0]*x + m[2]*y + m[4], m[1]*x + m[3]*y + m[5]
}

func parseTransform(s string) mat {
	m := ident
	for {
		s = strings.TrimSpace(strings.TrimLeft(s, ", "))
		i := strings.IndexByte(s, '(')
		j := strings.IndexByte(s, ')')
		if i < 0 || j < i {
			return m
		}
		name := strings.TrimSpace(s[:i])
		var a []float64
		for _, f := range strings.FieldsFunc(s[i+1:j], func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' }) {
			v, _ := svgNumber(f)
			a = append(a, v)
		}
		s = s[j+1:]
		get := func(k int, def float64) float64 {
			if k < len(a) {
				return a[k]
			}
			return def
		}
		var t mat
		switch name {
		case "matrix":
			t = mat{get(0, 1), get(1, 0), get(2, 0), get(3, 1), get(4, 0), get(5, 0)}
		case "translate":
			t = mat{1, 0, 0, 1, get(0, 0), get(1, 0)}
		case "scale":
			t = mat{get(0, 1), 0, 0, get(1, get(0, 1)), 0, 0}
		case "rotate":
			ang := get(0, 0) * math.Pi / 180
			c, sn := math.Cos(ang), math.Sin(ang)
			t = mat{c, sn, -sn, c, 0, 0}
			if len(a) >= 3 {
				t = mat{1, 0, 0, 1, a[1], a[2]}.mul(t).mul(mat{1, 0, 0, 1, -a[1], -a[2]})
			}
		case "skewX":
			t = mat{1, 0, math.Tan(get(0, 0) * math.Pi / 180), 1, 0, 0}
		case "skewY":
			t = mat{1, math.Tan(get(0, 0) * math.Pi / 180), 0, 1, 0, 0}
		default:
			continue
		}
		m = m.mul(t)
	}
}

// ---------- style ----------

type svgPaint struct {
	kind int // 0 none, 1 colour, 2 gradient
	c    Color
	ref  string
}

type svgStyle struct {
	fill, stroke           svgPaint
	fillOp, strokeOp, op   float64
	sw                     float64
	cap, join              int
	miter                  float64
	dash                   []float64
	dashOff                float64
	evenOdd                bool
	family, weight, fstyle string
	size                   float64
	anchor                 string
	color                  Color
	hidden                 bool
}

func defaultSVGStyle() svgStyle {
	return svgStyle{fill: svgPaint{kind: 1, c: Color{0, 0, 0, 1}}, fillOp: 1, strokeOp: 1, op: 1, sw: 1, miter: 4,
		family: "sans-serif", weight: "normal", fstyle: "normal", size: 12, anchor: "start", color: Color{0, 0, 0, 1}}
}

func (d *svgDoc) styleDecls(n *xmldom.Node) map[string]string {
	m := map[string]string{}
	for _, a := range n.Attrs {
		if a.Prefix == "" && a.Name != "style" {
			m[a.Name] = strings.TrimSpace(a.Value)
		}
	}
	for _, part := range strings.Split(attr(n, "style"), ";") {
		if k, v, ok := strings.Cut(part, ":"); ok {
			m[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return m
}

func (d *svgDoc) paint(v string, cur Color) (svgPaint, bool) {
	v = strings.TrimSpace(v)
	switch {
	case v == "none":
		return svgPaint{}, true
	case v == "currentColor":
		return svgPaint{kind: 1, c: cur}, true
	case strings.HasPrefix(v, "url("):
		end := strings.IndexByte(v, ')')
		if end < 0 {
			return svgPaint{}, false
		}
		id := strings.Trim(strings.TrimSpace(v[4:end]), "'\"#")
		if _, ok := d.ids[id]; ok {
			return svgPaint{kind: 2, ref: id}, true
		}
		if rest := strings.TrimSpace(v[end+1:]); rest != "" {
			return d.paint(rest, cur)
		}
		return svgPaint{}, true
	}
	if c, ok := ParseColor(v); ok {
		return svgPaint{kind: 1, c: c}, true
	}
	return svgPaint{}, false
}

func (d *svgDoc) inherit(n *xmldom.Node, p svgStyle) svgStyle {
	s := p
	s.op = 1 // opacity does not inherit
	m := d.styleDecls(n)
	if v, ok := m["color"]; ok {
		if c, ok := ParseColor(v); ok {
			s.color = c
		}
	}
	if v, ok := m["font-size"]; ok {
		if f, ok := svgLen(v, d.pxPt, p.size, p.size); ok {
			s.size = f
		}
	}
	if v, ok := m["fill"]; ok {
		if pt, ok := d.paint(v, s.color); ok {
			s.fill = pt
		}
	}
	if v, ok := m["stroke"]; ok {
		if pt, ok := d.paint(v, s.color); ok {
			s.stroke = pt
		}
	}
	num := func(k string, dst *float64) {
		if v, ok := m[k]; ok {
			if f, ok := svgNumber(v); ok {
				*dst = math.Max(0, math.Min(1, f))
			}
		}
	}
	num("fill-opacity", &s.fillOp)
	num("stroke-opacity", &s.strokeOp)
	num("opacity", &s.op)
	if v, ok := m["stroke-width"]; ok {
		if f, ok := svgLen(v, d.pxPt, s.size, 100); ok {
			s.sw = f
		}
	}
	switch m["stroke-linecap"] {
	case "round":
		s.cap = 1
	case "square":
		s.cap = 2
	case "butt":
		s.cap = 0
	}
	switch m["stroke-linejoin"] {
	case "round":
		s.join = 1
	case "bevel":
		s.join = 2
	case "miter":
		s.join = 0
	}
	if v, ok := m["stroke-miterlimit"]; ok {
		if f, ok := svgNumber(v); ok {
			s.miter = f
		}
	}
	if v, ok := m["stroke-dasharray"]; ok {
		s.dash = nil
		if v != "none" {
			for _, f := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' }) {
				if x, ok := svgLen(f, d.pxPt, s.size, 100); ok {
					s.dash = append(s.dash, x)
				}
			}
		}
	}
	if v, ok := m["stroke-dashoffset"]; ok {
		if f, ok := svgLen(v, d.pxPt, s.size, 100); ok {
			s.dashOff = f
		}
	}
	if v, ok := m["fill-rule"]; ok {
		s.evenOdd = v == "evenodd"
	}
	if v, ok := m["font-family"]; ok {
		s.family = strings.Trim(v, "'\" ")
	}
	if v, ok := m["font-weight"]; ok {
		s.weight = v
	}
	if v, ok := m["font-style"]; ok {
		s.fstyle = v
	}
	if v, ok := m["text-anchor"]; ok {
		s.anchor = v
	}
	if v, ok := m["display"]; ok && v == "none" {
		s.hidden = true
	}
	if v, ok := m["visibility"]; ok && (v == "hidden" || v == "collapse") {
		s.hidden = true
	} else if ok {
		s.hidden = false
	}
	return s
}

// ---------- drawing ----------

type svgPainter struct {
	r   *renderCtx
	d   *svgDoc
	pg  *pdf.Page
	ctm mat // user space of the current element -> PDF page space
}

// drawSVG paints the document into the box (x, y top-left, page coordinates) of
// size w x h.
func (r *renderCtx) drawSVG(d *svgDoc, x, y, w, h float64) {
	if d.w <= 0 || d.h <= 0 {
		return
	}
	// document units -> PDF page space
	base := mat{1, 0, 0, -1, x, r.pageH - y}.mul(mat{w / d.w, 0, 0, h / d.h, 0, 0})
	vw, vh := d.w, d.h
	if d.hasVB {
		sx, sy := vw/d.vbW, vh/d.vbH
		if d.align != "none" {
			sc := math.Min(sx, sy)
			if d.slice {
				sc = math.Max(sx, sy)
			}
			sx, sy = sc, sc
		}
		ox, oy := 0.0, 0.0
		a := d.align
		switch {
		case strings.HasPrefix(a, "xMid"):
			ox = (vw - d.vbW*sx) / 2
		case strings.HasPrefix(a, "xMax"):
			ox = vw - d.vbW*sx
		}
		switch {
		case strings.HasSuffix(a, "YMid"):
			oy = (vh - d.vbH*sy) / 2
		case strings.HasSuffix(a, "YMax"):
			oy = vh - d.vbH*sy
		}
		base = base.mul(mat{sx, 0, 0, sy, ox - d.vbX*sx, oy - d.vbY*sy})
	} else {
		base = base.mul(mat{d.pxPt, 0, 0, d.pxPt, 0, 0})
	}
	p := &svgPainter{r: r, d: d, pg: r.pg, ctm: base}
	r.pg.Save()
	r.pg.Rect(x, r.pageH-y-h, w, h)
	r.pg.Clip()
	r.pg.Transform(base[0], base[1], base[2], base[3], base[4], base[5])
	st := defaultSVGStyle()
	p.ctm = base
	st = d.inherit(d.root, st)
	for _, c := range d.root.Children {
		p.node(c, st)
	}
	r.pg.Restore()
}

func (p *svgPainter) node(n *xmldom.Node, parent svgStyle) {
	if n.Type != xmldom.ElementNode || (n.URI != svgNS && n.URI != "") {
		return
	}
	switch n.Name {
	case "defs", "linearGradient", "radialGradient", "clipPath", "mask", "pattern", "marker", "symbol",
		"title", "desc", "metadata", "style", "filter":
		return
	}
	st := p.d.inherit(n, parent)
	if st.hidden {
		return
	}
	local := ident
	if t := attr(n, "transform"); t != "" {
		local = parseTransform(t)
	}
	saved := p.ctm
	p.pg.Save()
	if local != ident {
		p.pg.Transform(local[0], local[1], local[2], local[3], local[4], local[5])
		p.ctm = p.ctm.mul(local)
	}
	if st.op < 1 {
		p.pg.SetAlpha(st.op, st.op)
	}
	switch n.Name {
	case "g", "a", "svg", "switch":
		if n.Name == "svg" && n != p.d.root {
			x, _ := svgLen(attr(n, "x"), p.d.pxPt, st.size, 0)
			y, _ := svgLen(attr(n, "y"), p.d.pxPt, st.size, 0)
			t := mat{1, 0, 0, 1, x, y}
			p.pg.Transform(t[0], t[1], t[2], t[3], t[4], t[5])
			p.ctm = p.ctm.mul(t)
		}
		for _, c := range n.Children {
			p.node(c, st)
			if n.Name == "switch" && c.Type == xmldom.ElementNode {
				break
			}
		}
	case "use":
		href := attr(n, "href")
		for _, a := range n.Attrs {
			if a.Name == "href" && href == "" {
				href = strings.TrimSpace(a.Value)
			}
		}
		if ref, ok := p.d.ids[strings.TrimPrefix(href, "#")]; ok {
			x, _ := svgLen(attr(n, "x"), p.d.pxPt, st.size, 0)
			y, _ := svgLen(attr(n, "y"), p.d.pxPt, st.size, 0)
			t := mat{1, 0, 0, 1, x, y}
			p.pg.Transform(t[0], t[1], t[2], t[3], t[4], t[5])
			p.ctm = p.ctm.mul(t)
			if ref.Name == "symbol" {
				for _, c := range ref.Children {
					p.node(c, st)
				}
			} else {
				p.node(ref, st)
			}
		}
	case "text":
		p.text(n, st)
	case "image":
		// embedded raster images in SVG are not supported
	default:
		if path := p.d.shapePath(n, st); path != nil {
			p.paintPath(path, n, st)
		}
	}
	p.pg.Restore()
	p.ctm = saved
}

type pathCmd struct {
	op  byte // M L C Z
	pts [6]float64
}

func (d *svgDoc) shapePath(n *xmldom.Node, st svgStyle) []pathCmd {
	f := func(name string) float64 {
		v, _ := svgLen(attr(n, name), d.pxPt, st.size, 0)
		return v
	}
	switch n.Name {
	case "rect":
		x, y, w, h := f("x"), f("y"), f("width"), f("height")
		if w <= 0 || h <= 0 {
			return nil
		}
		rx, ry := f("rx"), f("ry")
		if attr(n, "rx") == "" {
			rx = ry
		}
		if attr(n, "ry") == "" {
			ry = rx
		}
		rx, ry = math.Min(rx, w/2), math.Min(ry, h/2)
		if rx <= 0 || ry <= 0 {
			return []pathCmd{{'M', [6]float64{x, y}}, {'L', [6]float64{x + w, y}}, {'L', [6]float64{x + w, y + h}}, {'L', [6]float64{x, y + h}}, {'Z', [6]float64{}}}
		}
		return roundRect(x, y, w, h, rx, ry)
	case "circle":
		r := f("r")
		if r <= 0 {
			return nil
		}
		return ellipsePath(f("cx"), f("cy"), r, r)
	case "ellipse":
		rx, ry := f("rx"), f("ry")
		if rx <= 0 || ry <= 0 {
			return nil
		}
		return ellipsePath(f("cx"), f("cy"), rx, ry)
	case "line":
		return []pathCmd{{'M', [6]float64{f("x1"), f("y1")}}, {'L', [6]float64{f("x2"), f("y2")}}}
	case "polyline", "polygon":
		var nums []float64
		for _, t := range strings.FieldsFunc(attr(n, "points"), func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\t' }) {
			if v, ok := svgNumber(t); ok {
				nums = append(nums, v*d.pxPt)
			}
		}
		if len(nums) < 4 {
			return nil
		}
		var out []pathCmd
		for i := 0; i+1 < len(nums); i += 2 {
			op := byte('L')
			if i == 0 {
				op = 'M'
			}
			out = append(out, pathCmd{op, [6]float64{nums[i], nums[i+1]}})
		}
		if n.Name == "polygon" {
			out = append(out, pathCmd{'Z', [6]float64{}})
		}
		return out
	case "path":
		return parsePathData(attr(n, "d"), d.pxPt)
	}
	return nil
}

const kappa = 0.5522847498

func ellipsePath(cx, cy, rx, ry float64) []pathCmd {
	kx, ky := rx*kappa, ry*kappa
	return []pathCmd{
		{'M', [6]float64{cx + rx, cy}},
		{'C', [6]float64{cx + rx, cy + ky, cx + kx, cy + ry, cx, cy + ry}},
		{'C', [6]float64{cx - kx, cy + ry, cx - rx, cy + ky, cx - rx, cy}},
		{'C', [6]float64{cx - rx, cy - ky, cx - kx, cy - ry, cx, cy - ry}},
		{'C', [6]float64{cx + kx, cy - ry, cx + rx, cy - ky, cx + rx, cy}},
		{'Z', [6]float64{}},
	}
}

func roundRect(x, y, w, h, rx, ry float64) []pathCmd {
	kx, ky := rx*kappa, ry*kappa
	return []pathCmd{
		{'M', [6]float64{x + rx, y}},
		{'L', [6]float64{x + w - rx, y}},
		{'C', [6]float64{x + w - rx + kx, y, x + w, y + ry - ky, x + w, y + ry}},
		{'L', [6]float64{x + w, y + h - ry}},
		{'C', [6]float64{x + w, y + h - ry + ky, x + w - rx + kx, y + h, x + w - rx, y + h}},
		{'L', [6]float64{x + rx, y + h}},
		{'C', [6]float64{x + rx - kx, y + h, x, y + h - ry + ky, x, y + h - ry}},
		{'L', [6]float64{x, y + ry}},
		{'C', [6]float64{x, y + ry - ky, x + rx - kx, y, x + rx, y}},
		{'Z', [6]float64{}},
	}
}

// parsePathData converts SVG path data into absolute M/L/C/Z commands.
func parsePathData(s string, unit float64) []pathCmd {
	var out []pathCmd
	i := 0
	readNum := func() (float64, bool) {
		for i < len(s) && (s[i] == ' ' || s[i] == ',' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
			i++
		}
		j := i
		if j < len(s) && (s[j] == '+' || s[j] == '-') {
			j++
		}
		dot, exp := false, false
		for j < len(s) {
			c := s[j]
			switch {
			case c >= '0' && c <= '9':
			case c == '.' && !dot && !exp:
				dot = true
			case (c == 'e' || c == 'E') && !exp && j > i:
				exp = true
				if j+1 < len(s) && (s[j+1] == '+' || s[j+1] == '-') {
					j++
				}
			default:
				goto done
			}
			j++
		}
	done:
		if j == i {
			return 0, false
		}
		f, err := strconv.ParseFloat(s[i:j], 64)
		if err != nil {
			return 0, false
		}
		i = j
		return f * unit, true
	}
	readFlag := func() (bool, bool) {
		for i < len(s) && (s[i] == ' ' || s[i] == ',') {
			i++
		}
		if i < len(s) && (s[i] == '0' || s[i] == '1') {
			v := s[i] == '1'
			i++
			return v, true
		}
		return false, false
	}
	var cx, cy, sx, sy, lcx, lcy float64
	var cmd byte
	var prev byte
	for {
		for i < len(s) && (s[i] == ' ' || s[i] == ',' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
			i++
		}
		if i >= len(s) {
			break
		}
		if c := s[i]; (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			cmd = c
			i++
			if cmd == 'z' || cmd == 'Z' {
				out = append(out, pathCmd{'Z', [6]float64{}})
				cx, cy = sx, sy
				prev = 'Z'
				continue
			}
		} else if cmd == 0 || cmd == 'z' || cmd == 'Z' {
			break
		}
		rel := cmd >= 'a'
		up := cmd &^ 0x20
		nums := func(k int) ([]float64, bool) {
			v := make([]float64, k)
			for q := range v {
				f, ok := readNum()
				if !ok {
					return nil, false
				}
				v[q] = f
			}
			return v, true
		}
		off := func(x, y float64) (float64, float64) {
			if rel {
				return x + cx, y + cy
			}
			return x, y
		}
		switch up {
		case 'M':
			v, ok := nums(2)
			if !ok {
				return out
			}
			x, y := off(v[0], v[1])
			out = append(out, pathCmd{'M', [6]float64{x, y}})
			cx, cy, sx, sy = x, y, x, y
			if rel {
				cmd = 'l'
			} else {
				cmd = 'L'
			}
		case 'L':
			v, ok := nums(2)
			if !ok {
				return out
			}
			x, y := off(v[0], v[1])
			out = append(out, pathCmd{'L', [6]float64{x, y}})
			cx, cy = x, y
		case 'H':
			v, ok := nums(1)
			if !ok {
				return out
			}
			x := v[0]
			if rel {
				x += cx
			}
			out = append(out, pathCmd{'L', [6]float64{x, cy}})
			cx = x
		case 'V':
			v, ok := nums(1)
			if !ok {
				return out
			}
			y := v[0]
			if rel {
				y += cy
			}
			out = append(out, pathCmd{'L', [6]float64{cx, y}})
			cy = y
		case 'C', 'S':
			var x1, y1 float64
			if up == 'C' {
				v, ok := nums(2)
				if !ok {
					return out
				}
				x1, y1 = off(v[0], v[1])
			} else {
				x1, y1 = cx, cy
				if prev == 'C' {
					x1, y1 = 2*cx-lcx, 2*cy-lcy
				}
			}
			v, ok := nums(4)
			if !ok {
				return out
			}
			x2, y2 := off(v[0], v[1])
			x, y := off(v[2], v[3])
			out = append(out, pathCmd{'C', [6]float64{x1, y1, x2, y2, x, y}})
			lcx, lcy = x2, y2
			cx, cy = x, y
			prev = 'C'
			continue
		case 'Q', 'T':
			var x1, y1 float64
			if up == 'Q' {
				v, ok := nums(2)
				if !ok {
					return out
				}
				x1, y1 = off(v[0], v[1])
			} else {
				x1, y1 = cx, cy
				if prev == 'Q' {
					x1, y1 = 2*cx-lcx, 2*cy-lcy
				}
			}
			v, ok := nums(2)
			if !ok {
				return out
			}
			x, y := off(v[0], v[1])
			out = append(out, pathCmd{'C', [6]float64{cx + 2.0/3*(x1-cx), cy + 2.0/3*(y1-cy), x + 2.0/3*(x1-x), y + 2.0/3*(y1-y), x, y}})
			lcx, lcy = x1, y1
			cx, cy = x, y
			prev = 'Q'
			continue
		case 'A':
			v, ok := nums(3)
			if !ok {
				return out
			}
			fa, ok1 := readFlag()
			fs, ok2 := readFlag()
			w, ok3 := nums(2)
			if !ok1 || !ok2 || !ok3 {
				return out
			}
			x, y := off(w[0], w[1])
			out = append(out, arcToCurves(cx, cy, v[0], v[1], v[2], fa, fs, x, y)...)
			cx, cy = x, y
		default:
			return out
		}
		prev = up
	}
	return out
}

// arcToCurves converts an SVG elliptical arc into cubic Beziers.
func arcToCurves(x1, y1, rx, ry, phiDeg float64, large, sweep bool, x2, y2 float64) []pathCmd {
	if x1 == x2 && y1 == y2 {
		return nil
	}
	rx, ry = math.Abs(rx), math.Abs(ry)
	if rx == 0 || ry == 0 {
		return []pathCmd{{'L', [6]float64{x2, y2}}}
	}
	phi := phiDeg * math.Pi / 180
	cp, sp := math.Cos(phi), math.Sin(phi)
	dx, dy := (x1-x2)/2, (y1-y2)/2
	x1p, y1p := cp*dx+sp*dy, -sp*dx+cp*dy
	lam := x1p*x1p/(rx*rx) + y1p*y1p/(ry*ry)
	if lam > 1 {
		s := math.Sqrt(lam)
		rx, ry = rx*s, ry*s
	}
	num := rx*rx*ry*ry - rx*rx*y1p*y1p - ry*ry*x1p*x1p
	den := rx*rx*y1p*y1p + ry*ry*x1p*x1p
	co := 0.0
	if den != 0 && num > 0 {
		co = math.Sqrt(num / den)
	}
	if large == sweep {
		co = -co
	}
	cxp, cyp := co*rx*y1p/ry, -co*ry*x1p/rx
	cx := cp*cxp - sp*cyp + (x1+x2)/2
	cy := sp*cxp + cp*cyp + (y1+y2)/2
	ang := func(ux, uy, vx, vy float64) float64 {
		a := math.Atan2(ux*vy-uy*vx, ux*vx+uy*vy)
		return a
	}
	th1 := ang(1, 0, (x1p-cxp)/rx, (y1p-cyp)/ry)
	dth := ang((x1p-cxp)/rx, (y1p-cyp)/ry, (-x1p-cxp)/rx, (-y1p-cyp)/ry)
	if !sweep && dth > 0 {
		dth -= 2 * math.Pi
	} else if sweep && dth < 0 {
		dth += 2 * math.Pi
	}
	n := int(math.Ceil(math.Abs(dth) / (math.Pi / 2)))
	if n < 1 {
		n = 1
	}
	step := dth / float64(n)
	var out []pathCmd
	t := 4.0 / 3 * math.Tan(step/4)
	for k := 0; k < n; k++ {
		a1 := th1 + float64(k)*step
		a2 := a1 + step
		c1, s1 := math.Cos(a1), math.Sin(a1)
		c2, s2 := math.Cos(a2), math.Sin(a2)
		pt := func(x, y float64) (float64, float64) {
			return cp*rx*x - sp*ry*y + cx, sp*rx*x + cp*ry*y + cy
		}
		p1x, p1y := pt(c1-t*s1, s1+t*c1)
		p2x, p2y := pt(c2+t*s2, s2-t*c2)
		ex, ey := pt(c2, s2)
		if k == n-1 {
			ex, ey = x2, y2
		}
		out = append(out, pathCmd{'C', [6]float64{p1x, p1y, p2x, p2y, ex, ey}})
	}
	return out
}

func (p *svgPainter) emit(path []pathCmd) {
	for _, c := range path {
		switch c.op {
		case 'M':
			p.pg.MoveTo(c.pts[0], c.pts[1])
		case 'L':
			p.pg.LineTo(c.pts[0], c.pts[1])
		case 'C':
			p.pg.CurveTo(c.pts[0], c.pts[1], c.pts[2], c.pts[3], c.pts[4], c.pts[5])
		case 'Z':
			p.pg.ClosePath()
		}
	}
}

func pathBBox(path []pathCmd) (x0, y0, x1, y1 float64) {
	x0, y0, x1, y1 = math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, c := range path {
		n := 0
		switch c.op {
		case 'M', 'L':
			n = 1
		case 'C':
			n = 3
		}
		for k := 0; k < n; k++ {
			x, y := c.pts[2*k], c.pts[2*k+1]
			x0, y0, x1, y1 = math.Min(x0, x), math.Min(y0, y), math.Max(x1, x), math.Max(y1, y)
		}
	}
	return
}

// setPaint selects the fill colour or gradient; returns false when nothing is painted.
func (p *svgPainter) setPaint(pt svgPaint, bbox [4]float64, opacity float64, stroke bool) bool {
	switch pt.kind {
	case 1:
		if stroke {
			p.pg.StrokeRGB(pt.c.R, pt.c.G, pt.c.B)
		} else {
			p.pg.FillRGB(pt.c.R, pt.c.G, pt.c.B)
		}
		return true
	case 2:
		sh := p.gradient(pt.ref, bbox)
		if sh == nil {
			return false
		}
		if stroke {
			return false // gradient strokes fall back to no stroke
		}
		p.pg.FillShading(p.r.doc.AddShading(sh))
		return true
	}
	return false
}

func (p *svgPainter) gradient(id string, bbox [4]float64) *pdf.Shading {
	g, ok := p.d.ids[id]
	if !ok || (g.Name != "linearGradient" && g.Name != "radialGradient") {
		return nil
	}
	// follow href for stops and inherited attributes
	get := func(name string) string {
		for n, depth := g, 0; n != nil && depth < 8; depth++ {
			if v := attr(n, name); v != "" {
				return v
			}
			var href string
			for _, a := range n.Attrs {
				if a.Name == "href" {
					href = strings.TrimPrefix(strings.TrimSpace(a.Value), "#")
				}
			}
			next, ok := p.d.ids[href]
			if !ok {
				break
			}
			n = next
		}
		return ""
	}
	var stopsFrom *xmldom.Node
	for n, depth := g, 0; n != nil && depth < 8; depth++ {
		has := false
		for _, c := range n.Children {
			if c.Type == xmldom.ElementNode && c.Name == "stop" {
				has = true
			}
		}
		if has {
			stopsFrom = n
			break
		}
		var href string
		for _, a := range n.Attrs {
			if a.Name == "href" {
				href = strings.TrimPrefix(strings.TrimSpace(a.Value), "#")
			}
		}
		n = p.d.ids[href]
	}
	if stopsFrom == nil {
		return nil
	}
	var stops []pdf.GradStop
	last := 0.0
	for _, c := range stopsFrom.Children {
		if c.Type != xmldom.ElementNode || c.Name != "stop" {
			continue
		}
		m := p.d.styleDecls(c)
		off := 0.0
		if v := m["offset"]; v != "" {
			if strings.HasSuffix(v, "%") {
				f, _ := svgNumber(strings.TrimSuffix(v, "%"))
				off = f / 100
			} else {
				off, _ = svgNumber(v)
			}
		}
		off = math.Max(last, math.Min(1, off))
		last = off
		col, _ := ParseColor(m["stop-color"])
		if m["stop-color"] == "" {
			col = Color{0, 0, 0, 1}
		}
		stops = append(stops, pdf.GradStop{Off: off, R: col.R, G: col.G, B: col.B})
	}
	if len(stops) == 0 {
		return nil
	}
	userSpace := get("gradientUnits") == "userSpaceOnUse"
	bw, bh := bbox[2]-bbox[0], bbox[3]-bbox[1]
	unit := func(s string, def float64, base float64, horiz bool) float64 {
		if s == "" {
			return def
		}
		if strings.HasSuffix(s, "%") {
			f, _ := svgNumber(strings.TrimSuffix(s, "%"))
			if userSpace {
				return f / 100 * base
			}
			return f / 100
		}
		if userSpace {
			v, _ := svgLen(s, p.d.pxPt, 12, base)
			return v
		}
		v, _ := svgNumber(s)
		return v
	}
	vw, vh := p.d.w, p.d.h
	if p.d.hasVB {
		vw, vh = p.d.vbW, p.d.vbH
	}
	var coords []float64
	sh := &pdf.Shading{Stops: stops}
	if g.Name == "linearGradient" {
		def := func(v float64) float64 {
			if userSpace {
				return v * vw
			}
			return v
		}
		coords = []float64{unit(get("x1"), 0, vw, true), unit(get("y1"), 0, vh, false), unit(get("x2"), def(1), vw, true), unit(get("y2"), 0, vh, false)}
	} else {
		half := 0.5
		if userSpace {
			half = 0.5 * vw
		}
		cx, cy := unit(get("cx"), half, vw, true), unit(get("cy"), func() float64 {
			if userSpace {
				return 0.5 * vh
			}
			return 0.5
		}(), vh, false)
		r := unit(get("r"), half, math.Sqrt((vw*vw+vh*vh)/2), true)
		fx, fy := unit(get("fx"), cx, vw, true), unit(get("fy"), cy, vh, false)
		coords = []float64{fx, fy, 0, cx, cy, r}
		sh.Radial = true
	}
	sh.Coords = coords
	m := p.ctm
	if !userSpace {
		if bw <= 0 || bh <= 0 {
			return nil
		}
		m = m.mul(mat{bw, 0, 0, bh, bbox[0], bbox[1]})
	}
	if gt := get("gradientTransform"); gt != "" {
		m = m.mul(parseTransform(gt))
	}
	sh.Matrix = [6]float64(m)
	return sh
}

func (p *svgPainter) paintPath(path []pathCmd, n *xmldom.Node, st svgStyle) {
	x0, y0, x1, y1 := pathBBox(path)
	bbox := [4]float64{x0, y0, x1, y1}
	fillOK := st.fill.kind != 0
	strokeOK := st.stroke.kind == 1 && st.sw > 0
	if fillOK && st.fillOp < 1 || strokeOK && st.strokeOp < 1 {
		fo, so := st.fillOp, st.strokeOp
		if st.op < 1 {
			fo, so = fo*st.op, so*st.op
		}
		p.pg.SetAlpha(fo, so)
	}
	if fillOK {
		if p.setPaint(st.fill, bbox, st.fillOp, false) {
			// ok
		} else {
			fillOK = false
		}
	}
	if strokeOK {
		p.setPaint(st.stroke, bbox, st.strokeOp, true)
		p.pg.LineWidth(st.sw)
		p.pg.LineCap(st.cap)
		p.pg.LineJoin(st.join)
		p.pg.MiterLimit(st.miter)
		if len(st.dash) > 0 {
			d := st.dash
			if len(d)%2 == 1 {
				d = append(append([]float64(nil), d...), d...)
			}
			p.pg.DashArray(d, st.dashOff)
		}
	}
	if !fillOK && !strokeOK {
		return
	}
	p.emit(path)
	switch {
	case fillOK && strokeOK && st.evenOdd:
		p.pg.FillStrokeEO()
	case fillOK && strokeOK:
		p.pg.FillStroke()
	case fillOK && st.evenOdd:
		p.pg.FillEO()
	case fillOK:
		p.pg.Fill()
	default:
		p.pg.Stroke()
	}
}

// text draws <text> with its <tspan> children (single line, absolute positions).
func (p *svgPainter) text(n *xmldom.Node, st svgStyle) {
	e := p.r.e
	x, _ := svgLen(firstTok(attr(n, "x")), p.d.pxPt, st.size, 0)
	y, _ := svgLen(firstTok(attr(n, "y")), p.d.pxPt, st.size, 0)
	type seg struct {
		text string
		st   svgStyle
		dx   float64
		x, y *float64
	}
	var segs []seg
	var collect func(c *xmldom.Node, cs svgStyle, top bool)
	collect = func(c *xmldom.Node, cs svgStyle, top bool) {
		for _, ch := range c.Children {
			switch {
			case ch.Type == xmldom.TextNode:
				t := strings.Join(strings.Fields(ch.Value), " ")
				if t == "" {
					continue
				}
				if strings.HasPrefix(ch.Value, " ") || strings.HasPrefix(ch.Value, "\n") {
					if len(segs) > 0 {
						t = " " + t
					}
				}
				if strings.HasSuffix(ch.Value, " ") || strings.HasSuffix(ch.Value, "\n") {
					t += " "
				}
				segs = append(segs, seg{text: t, st: cs})
			case ch.Type == xmldom.ElementNode && ch.Name == "tspan":
				ts := p.d.inherit(ch, cs)
				first := len(segs)
				collect(ch, ts, false)
				if first < len(segs) {
					if v := firstTok(attr(ch, "x")); v != "" {
						f, _ := svgLen(v, p.d.pxPt, ts.size, 0)
						segs[first].x = &f
					}
					if v := firstTok(attr(ch, "y")); v != "" {
						f, _ := svgLen(v, p.d.pxPt, ts.size, 0)
						segs[first].y = &f
					}
					if v := firstTok(attr(ch, "dx")); v != "" {
						f, _ := svgLen(v, p.d.pxPt, ts.size, 0)
						segs[first].dx = f
					}
				}
			}
		}
	}
	collect(n, st, true)
	if len(segs) == 0 {
		return
	}
	dx0, _ := svgLen(firstTok(attr(n, "dx")), p.d.pxPt, st.size, 0)
	dy0, _ := svgLen(firstTok(attr(n, "dy")), p.d.pxPt, st.size, 0)
	x += dx0
	y += dy0
	// measure to honour text-anchor
	type placed struct {
		runs []textRun
		ts   *textStyle
		st   svgStyle
		x, y float64
	}
	var out []placed
	cx, cy := x, y
	for _, s := range segs {
		if s.x != nil {
			cx = *s.x
		}
		if s.y != nil {
			cy = *s.y
		}
		cx += s.dx
		fake := &Node{Name: "inline", Props: map[string]string{
			"font-family": s.st.family, "font-size": strconv.FormatFloat(s.st.size, 'f', 3, 64) + "pt",
			"font-weight": s.st.weight, "font-style": s.st.fstyle}}
		ts := e.styleFor(fake)
		if ts.ft == nil {
			continue
		}
		runs := e.measure(ts, s.text)
		out = append(out, placed{runs, ts, s.st, cx, cy})
		cx += runsWidth(runs)
	}
	if len(out) == 0 {
		return
	}
	shift := 0.0
	switch st.anchor {
	case "middle":
		shift = -(cx - x) / 2
	case "end":
		shift = -(cx - x)
	}
	for _, o := range out {
		if o.st.fill.kind != 1 {
			continue
		}
		p.pg.Save()
		if o.st.fillOp < 1 {
			p.pg.SetAlpha(o.st.fillOp, o.st.fillOp)
		}
		// draw upright: undo the y flip of the SVG user space around the baseline
		t := mat{1, 0, 0, -1, o.x + shift, o.y}
		p.pg.Transform(t[0], t[1], t[2], t[3], t[4], t[5])
		p.pg.FillRGB(o.st.fill.c.R, o.st.fill.c.G, o.st.fill.c.B)
		xoff := 0.0
		for _, run := range o.runs {
			p.pg.Text(p.r.doc.UseFont(run.ft), o.ts.size, xoff, 0, run.glyphs)
			xoff += run.width
		}
		p.pg.Restore()
	}
}

func firstTok(s string) string {
	f := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' })
	if len(f) == 0 {
		return ""
	}
	return f[0]
}
