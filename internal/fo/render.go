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
	// page-number-citation-last refers to the last page the id appears on
	r.e.ids[id+"\x00last"] = idInfo{page: r.pageIdx, pageNum: r.pageNum}
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
	if ib.blk != nil {
		ib.blk.render(r, x, y)
		return
	}
	if ib.img.svg != nil {
		if ib.clip {
			r.clipStart(x, y, ib.w, ib.h)
			defer r.clipEnd()
		}
		r.drawSVG(ib.img.svg, x, y, ib.cw, ib.ch)
		return
	}
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
			r.styledLine(bx, by, bx+bw, by+bh, i%2 == 0, b[i].style, c)
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
			if centered {
				// FOP strokes these as lines clipped to the border area
				r.pg.Save()
				r.pg.Rect(bx, r.py(by+bh), bw, bh)
				r.pg.Clip()
				r.pg.StrokeRGB(c.R, c.G, c.B)
				if bw >= bh {
					r.pg.LineWidth(bh)
					r.pg.MoveTo(bx, r.py(by+bh/2))
					r.pg.LineTo(bx+bw, r.py(by+bh/2))
				} else {
					r.pg.LineWidth(bw)
					r.pg.MoveTo(bx+bw/2, r.py(by))
					r.pg.LineTo(bx+bw/2, r.py(by+bh))
				}
				r.pg.Stroke()
				r.pg.Restore()
			} else {
				r.solidSide(i, x, y, w, h, b, c)
			}
		}
	}
}

// solidSide draws a solid border side the way Apache FOP's PDF painter does: a line
// of the border's width stroked inside a trapezoid that is clipped at the corners.
func (r *renderCtx) solidSide(i int, x, y, w, h float64, b [4]border, c Color) {
	var ox, oy, dx, dy, nx, ny, length, pw, nw float64
	switch i {
	case 0:
		ox, oy, dx, dy, nx, ny, length, pw, nw = x, y, 1, 0, 0, 1, w, b[3].w, b[1].w
	case 1:
		ox, oy, dx, dy, nx, ny, length, pw, nw = x+w, y, 0, 1, -1, 0, h, b[0].w, b[2].w
	case 2:
		ox, oy, dx, dy, nx, ny, length, pw, nw = x+w, y+h, -1, 0, 0, -1, w, b[1].w, b[3].w
	default:
		ox, oy, dx, dy, nx, ny, length, pw, nw = x, y+h, 0, -1, 1, 0, h, b[2].w, b[0].w
	}
	bw := b[i].w
	r.pg.Save()
	r.pg.Transform(dx, -dy, nx, -ny, ox, r.pageH-oy)
	r.pg.MoveTo(0, 0)
	r.pg.LineTo(length, 0)
	r.pg.LineTo(length-nw, bw)
	r.pg.LineTo(pw, bw)
	r.pg.ClosePath()
	r.pg.Clip()
	r.pg.StrokeRGB(c.R, c.G, c.B)
	r.pg.LineWidth(bw)
	r.pg.MoveTo(0, bw/2)
	r.pg.LineTo(length, bw/2)
	r.pg.Stroke()
	r.pg.Restore()
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

// styledLine ports PDFGraphicsPainter.drawBorderLine2 for the dashed, dotted
// and double styles; (x1,y1)-(x2,y2) is the line's box in top-down page units.
func (r *renderCtx) styledLine(x1, y1, x2, y2 float64, horz bool, style string, c Color) {
	w, h := x2-x1, y2-y1
	stroke := func(ax, ay, bx, by float64) {
		r.pg.MoveTo(ax, r.py(ay))
		r.pg.LineTo(bx, r.py(by))
		r.pg.Stroke()
	}
	r.pg.Save()
	defer r.pg.Restore()
	r.pg.StrokeRGB(c.R, c.G, c.B)
	// along/across: length of the line and its thickness
	along, across := w, h
	if !horz {
		along, across = h, w
	}
	line := func(off float64, wd float64) {
		r.pg.LineWidth(wd)
		if horz {
			stroke(x1, y1+off, x2, y1+off)
		} else {
			stroke(x1+off, y1, x1+off, y2)
		}
	}
	switch style {
	case "dashed":
		dash := 2 * across
		if across < 3 {
			dash = 6 * across
		}
		period := int((along - dash) / dash / 1.5)
		if period < 0 {
			period = 0
		}
		dw := along / (float64(period)*1.5 + 1)
		if dw == 0 {
			return
		}
		r.pg.Dash(dw, 0.5*dw)
		line(across/2, across)
	case "dotted", "square":
		unit := math.Abs(2 * across)
		rep := int(along / unit)
		if rep%2 == 0 {
			rep++
		}
		unit = along / float64(rep)
		if style == "dotted" {
			r.pg.LineCap(1)
		} else {
			r.pg.LineCap(2)
		}
		r.pg.Dash(0, unit)
		line(across/2, across)
	case "double":
		t := across / 3
		line(t/2, t)
		line(t/2+2*t, t)
	}
}

func (r *renderCtx) drawLeader(ld *leaderSpec, x, baseline, w float64) {
	switch ld.pattern {
	case "rule":
		th := ld.thickness
		c := ld.color
		// PDFGraphicsPainter.drawLine: the line box is [baseline-th, baseline]
		switch ld.rstyle {
		case "none":
		case "dotted", "square":
			half := th / 2
			r.styledLine(x+th+half, baseline-th, x+w-th-half, baseline, true, ld.rstyle, c)
		case "dashed", "double":
			r.styledLine(x, baseline-th, x+w, baseline, true, ld.rstyle, c)
		default:
			r.fillRect(x, baseline-th, w, th, c)
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
		cx := x // FilledArea draws whole units from the start of the area
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
