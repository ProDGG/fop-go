package fo

import (
	"math"
	"strings"

	"github.com/ProDGG/fop-go/internal/font"
	"github.com/ProDGG/fop-go/internal/pdf"
)

type pendingLink struct {
	page       *pdf.Page
	x, y, w, h float64
	target     *linkTarget
}

type marker struct {
	class string
	node  *Node
}

// renderCtx draws one page.
type renderCtx struct {
	e       *Engine
	doc     *pdf.Document
	pg      *pdf.Page
	pageIdx int
	pageNum string
	pageW   float64
	pageH   float64
	region  rect // containing region of the item being drawn (for absolute positioning)
	markers []marker
	links   *[]pendingLink
	clips   int
}

type rect struct{ x, y, w, h float64 }

func (r *renderCtx) py(y float64) float64 { return r.pageH - y }

func (r *renderCtx) fillRect(x, y, w, h float64, c Color) {
	if w <= 0 || h <= 0 || c.A == 0 {
		return
	}
	r.pg.FillRect(x, r.py(y+h), w, h, c.R, c.G, c.B)
}

func (r *renderCtx) registerID(id string, y float64) {
	if id == "" {
		return
	}
	if _, ok := r.e.ids[id]; !ok {
		r.e.ids[id] = idInfo{page: r.pageIdx, pageNum: r.pageNum, y: r.pageH - y + 2}
	}
}

func (r *renderCtx) addMarkers(ms []*markerDef) {
	for _, m := range ms {
		r.markers = append(r.markers, marker{m.class, m.node})
	}
}

func (r *renderCtx) addLink(t *linkTarget, x, y, w, h float64) {
	*r.links = append(*r.links, pendingLink{page: r.pg, x: x, y: r.py(y + h), w: w, h: h, target: t})
}

func (r *renderCtx) clipStart(x, y, w, h float64) {
	r.pg.Save()
	r.pg.Rect(x, r.py(y+h), w, h)
	r.pg.Clip()
}

func (r *renderCtx) clipEnd() { r.pg.Restore() }

func (r *renderCtx) drawGlyphs(st *textStyle, ft *font.Font, x, baseline float64, gl []pdf.Glyph) {
	if len(gl) == 0 || ft == nil {
		return
	}
	r.pg.FillRGB(st.color.R, st.color.G, st.color.B)
	r.pg.Text(r.doc.UseFont(ft), st.size, x, r.py(baseline), gl)
}

func (r *renderCtx) drawImage(ib *imageBox, x, y float64) {
	im := r.e.pdfImage(r.doc, ib.img)
	if ib.clip {
		r.clipStart(x, y, ib.w, ib.h)
		r.pg.DrawImage(im, x, r.py(y+ib.ch), ib.cw, ib.ch)
		r.clipEnd()
		return
	}
	r.pg.DrawImage(im, x, r.py(y+ib.ch), ib.cw, ib.ch)
}

func (e *Engine) pdfImage(doc *pdf.Document, id *imageData) *pdf.Image {
	cp := *id.img
	return doc.AddImage(id.src, &cp)
}

// drawBorders paints the four border bands of a box. When centered is true the
// bands are centred on the box edges (collapsed table borders).
func (r *renderCtx) drawBorders(x, y, w, h float64, b [4]border, centered bool) {
	// top, right, bottom, left
	sideRect := func(i int) (float64, float64, float64, float64) {
		bw := b[i].w
		off := 0.0
		if centered {
			off = bw / 2
		}
		switch i {
		case 0:
			return x - off, y - off, w + 2*off, bw
		case 1:
			return x + w - bw + off, y + b[0].w - off, bw, h - b[0].w - b[2].w + 2*off
		case 2:
			return x - off, y + h - bw + off, w + 2*off, bw
		}
		return x - off, y + b[0].w - off, bw, h - b[0].w - b[2].w + 2*off
	}
	for i := 0; i < 4; i++ {
		if b[i].w <= 0 || b[i].style == "none" || b[i].style == "hidden" || b[i].c.A == 0 {
			continue
		}
		bx, by, bw, bh := sideRect(i)
		if bw <= 0 || bh <= 0 {
			continue
		}
		c := b[i].c
		switch b[i].style {
		case "dashed", "dotted":
			r.pg.Save()
			r.pg.StrokeRGB(c.R, c.G, c.B)
			th := b[i].w
			r.pg.LineWidth(th)
			if b[i].style == "dashed" {
				r.pg.Dash(th*3, th*2)
			} else {
				r.pg.LineCap(1)
				r.pg.Dash(0.01, th*2)
			}
			if i%2 == 0 {
				cy := by + bh/2
				r.pg.MoveTo(bx, r.py(cy))
				r.pg.LineTo(bx+bw, r.py(cy))
			} else {
				cx := bx + bw/2
				r.pg.MoveTo(cx, r.py(by))
				r.pg.LineTo(cx, r.py(by+bh))
			}
			r.pg.Stroke()
			r.pg.Restore()
		case "double":
			third := b[i].w / 3
			if i%2 == 0 {
				r.fillRect(bx, by, bw, third, c)
				r.fillRect(bx, by+bh-third, bw, third, c)
			} else {
				r.fillRect(bx, by, third, bh, c)
				r.fillRect(bx+bw-third, by, third, bh, c)
			}
		default:
			r.fillRect(bx, by, bw, bh, c)
		}
	}
}

func (r *renderCtx) drawBlockDecor(b *block, x, y, h float64) {
	w := b.width
	bd := b.b
	if !b.first {
		bd[0].w = 0
	}
	if !b.last {
		bd[2].w = 0
	}
	if b.collapse {
		for i := range bd {
			bd[i].w = b.b[i].w * 2 // stored widths are halves
		}
	}
	if b.bg.A > 0 {
		r.fillRect(x, y, w, h, b.bg)
	}
	if b.bgImg != nil {
		r.drawBackgroundImage(b, x, y, w, h)
	}
	r.drawBorders(x, y, w, h, bd, b.collapse)
}

func (r *renderCtx) drawBackgroundImage(b *block, x, y, w, h float64) {
	im := b.bgImg
	iw, ih := im.wPt, im.hPt
	if iw <= 0 || ih <= 0 {
		return
	}
	r.clipStart(x, y, w, h)
	defer r.clipEnd()
	rep := b.bgRepeat
	nx, ny := 1, 1
	if rep == "" || rep == "repeat" || rep == "repeat-x" {
		nx = int(math.Ceil(w / iw))
	}
	if rep == "" || rep == "repeat" || rep == "repeat-y" {
		ny = int(math.Ceil(h / ih))
	}
	if nx*ny > 2000 {
		nx, ny = 1, 1
	}
	pimg := r.e.pdfImage(r.doc, im)
	for j := 0; j < ny; j++ {
		for i := 0; i < nx; i++ {
			r.pg.DrawImage(pimg, x+float64(i)*iw, r.py(y+float64(j+1)*ih), iw, ih)
		}
	}
}

func (r *renderCtx) drawLeader(ld *leaderSpec, x, baseline, w float64) {
	switch ld.pattern {
	case "rule":
		th := ld.thickness
		c := ld.color
		switch ld.rstyle {
		case "dashed", "dotted":
			r.pg.Save()
			r.pg.StrokeRGB(c.R, c.G, c.B)
			r.pg.LineWidth(th)
			if ld.rstyle == "dashed" {
				r.pg.Dash(th*3, th*2)
			} else {
				r.pg.LineCap(1)
				r.pg.Dash(0.01, th*2)
			}
			r.pg.MoveTo(x, r.py(baseline-th))
			r.pg.LineTo(x+w, r.py(baseline-th))
			r.pg.Stroke()
			r.pg.Restore()
		case "double":
			r.fillRect(x, baseline-th*1.2, w, th/3, c)
			r.fillRect(x, baseline-th*0.5, w, th/3, c)
		case "none":
		default:
			r.fillRect(x, baseline-th*1.5, w, th, c)
		}
	case "dots", "use-content":
		if len(ld.patRuns) == 0 || ld.patW <= 0 {
			return
		}
		n := int(w / ld.patW)
		if n <= 0 {
			return
		}
		cellW := runsWidth(ld.patRuns)
		slack := w - float64(n)*ld.patW // pattern is right-aligned in the leader area
		cx := x + slack
		for i := 0; i < n; i++ {
			for k, rr := range ld.patRuns {
				gl := append([]pdf.Glyph(nil), rr.glyphs...)
				if k == len(ld.patRuns)-1 && len(gl) > 0 {
					gl[len(gl)-1].Extra += ld.patW - cellW // pad the cell to the pattern pitch
				}
				r.drawGlyphs(ld.style, rr.ft, cx, baseline, gl)
				cx += rr.width
			}
			cx += ld.patW - cellW
		}
	}
}

func fmtPage(n int, format string) string {
	format = strings.TrimSpace(format)
	if format == "" {
		format = "1"
	}
	switch format {
	case "i":
		return strings.ToLower(romanNum(n))
	case "I":
		return romanNum(n)
	case "a":
		return alphaNum(n, false)
	case "A":
		return alphaNum(n, true)
	}
	s := itoa(n)
	if len(format) > 1 && format[len(format)-1] == '1' {
		for len(s) < len(format) {
			s = "0" + s
		}
	}
	return s
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

func alphaNum(n int, upper bool) string {
	if n <= 0 {
		return itoa(n)
	}
	base := byte('a')
	if upper {
		base = 'A'
	}
	var out []byte
	for n > 0 {
		n--
		out = append([]byte{base + byte(n%26)}, out...)
		n /= 26
	}
	return string(out)
}

func romanNum(n int) string {
	if n <= 0 || n >= 5000 {
		return itoa(n)
	}
	vals := []int{1000, 900, 500, 400, 100, 90, 50, 40, 10, 9, 5, 4, 1}
	syms := []string{"M", "CM", "D", "CD", "C", "XC", "L", "XL", "X", "IX", "V", "IV", "I"}
	var sb strings.Builder
	for i, v := range vals {
		for n >= v {
			sb.WriteString(syms[i])
			n -= v
		}
	}
	return sb.String()
}
