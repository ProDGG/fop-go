package xhr

import (
	"image"
	"image/color"
	"math"
	"strconv"
	"strings"

	"github.com/ProDGG/fop-go/internal/fo"
	"github.com/ProDGG/fop-go/internal/pdf"
)

// SVGRasterize makes SVG images behave like the reference deployment, which
// turns them into an antialiased bitmap of the intrinsic size before embedding.
var SVGRasterize = true

func init() {
	SVGLoader = func(data []byte) (*SVGImage, error) {
		d, err := fo.ParseSVGDrawing(data, nil)
		if err != nil {
			return nil, err
		}
		s := &SVGImage{W: d.W, H: d.H, Draw: func(p *pdf.Page, x, y, w, h float64) { d.Draw(p, x, y, w, h) }}
		if SVGRasterize {
			s.Raster = rasterizeDrawing(d)
		}
		return s, nil
	}
}

// rasterizeDrawing paints the drawing on a scratch page and scan-converts the
// resulting fills (8x8 supersampling) into an RGBA bitmap.
func rasterizeDrawing(d *fo.SVGDrawing) image.Image {
	w, h := int(math.Ceil(d.W)), int(math.Ceil(d.H))
	if w <= 0 || h <= 0 || w > 4096 || h > 4096 {
		return nil
	}
	doc := pdf.NewDocument()
	pg := doc.NewPage(float64(w), float64(h))
	d.Draw(pg, 0, 0, d.W, d.H)
	return rasterizeOps(string(pg.Content()), w, h)
}

type rpt struct{ x, y float64 }

func rasterizeOps(ops string, w, h int) image.Image {
	ss := 32 // close to exact area coverage
	if w*h > 40000 {
		ss = 16
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	ctm := [6]float64{1, 0, 0, 1, 0, 0}
	var stack [][6]float64
	var nums []float64
	var path [][]rpt
	var cur []rpt
	var cx, cy float64
	fill := [3]float64{0, 0, 0}
	apply := func(x, y float64) rpt {
		return rpt{ctm[0]*x + ctm[2]*y + ctm[4], float64(h) - (ctm[1]*x + ctm[3]*y + ctm[5])}
	}
	flush := func() {
		if len(cur) > 0 {
			path = append(path, cur)
			cur = nil
		}
	}
	for _, t := range strings.Fields(ops) {
		if v, err := strconv.ParseFloat(t, 64); err == nil {
			nums = append(nums, v)
			continue
		}
		n := len(nums)
		switch t {
		case "q":
			stack = append(stack, ctm)
		case "Q":
			if len(stack) > 0 {
				ctm = stack[len(stack)-1]
				stack = stack[:len(stack)-1]
			}
		case "cm":
			if n >= 6 {
				a := nums[n-6:]
				m := ctm
				ctm = [6]float64{
					a[0]*m[0] + a[1]*m[2], a[0]*m[1] + a[1]*m[3],
					a[2]*m[0] + a[3]*m[2], a[2]*m[1] + a[3]*m[3],
					a[4]*m[0] + a[5]*m[2] + m[4], a[4]*m[1] + a[5]*m[3] + m[5],
				}
			}
		case "rg":
			if n >= 3 {
				fill = [3]float64{nums[n-3], nums[n-2], nums[n-1]}
			}
		case "m":
			flush()
			cx, cy = nums[n-2], nums[n-1]
			cur = []rpt{apply(cx, cy)}
		case "l":
			cx, cy = nums[n-2], nums[n-1]
			cur = append(cur, apply(cx, cy))
		case "c":
			a := nums[n-6:]
			x0, y0 := cx, cy
			for i := 1; i <= 16; i++ {
				u := float64(i) / 16
				v := 1 - u
				x := v*v*v*x0 + 3*v*v*u*a[0] + 3*v*u*u*a[2] + u*u*u*a[4]
				y := v*v*v*y0 + 3*v*v*u*a[1] + 3*v*u*u*a[3] + u*u*u*a[5]
				cur = append(cur, apply(x, y))
			}
			cx, cy = a[4], a[5]
		case "re":
			flush()
			a := nums[n-4:]
			cur = []rpt{apply(a[0], a[1]), apply(a[0]+a[2], a[1]), apply(a[0]+a[2], a[1]+a[3]), apply(a[0], a[1]+a[3])}
			flush()
		case "h":
			flush()
		case "f", "F", "f*":
			flush()
			compositeFill(img, path, fill, t == "f*", ss)
			path = nil
		case "n", "S", "s", "W", "W*":
			if t != "W" && t != "W*" {
				flush()
				path = nil
			}
		}
		nums = nums[:0]
	}
	return img
}

// compositeFill scan-converts the polygons and blends the colour over img.
func compositeFill(img *image.NRGBA, polys [][]rpt, col [3]float64, evenOdd bool, ss int) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	cov := make([]float64, w)
	type edge struct{ x0, y0, x1, y1 float64 }
	var edges []edge
	for _, p := range polys {
		for i := range p {
			q := p[(i+1)%len(p)]
			edges = append(edges, edge{p[i].x, p[i].y, q.x, q.y})
		}
	}
	if len(edges) == 0 {
		return
	}
	minY, maxY := math.Inf(1), math.Inf(-1)
	for _, e := range edges {
		minY = math.Min(minY, math.Min(e.y0, e.y1))
		maxY = math.Max(maxY, math.Max(e.y0, e.y1))
	}
	y0p := int(math.Max(0, math.Floor(minY)))
	y1p := int(math.Min(float64(h), math.Ceil(maxY)))
	type cross struct {
		x   float64
		dir int
	}
	for py := y0p; py < y1p; py++ {
		for i := range cov {
			cov[i] = 0
		}
		touched := false
		for sy := 0; sy < ss; sy++ {
			y := float64(py) + (float64(sy)+0.5)/float64(ss)
			var xs []cross
			for _, e := range edges {
				if (e.y0 <= y && e.y1 > y) || (e.y1 <= y && e.y0 > y) {
					t := (y - e.y0) / (e.y1 - e.y0)
					dir := 1
					if e.y1 < e.y0 {
						dir = -1
					}
					xs = append(xs, cross{e.x0 + t*(e.x1-e.x0), dir})
				}
			}
			if len(xs) == 0 {
				continue
			}
			for i := 1; i < len(xs); i++ { // insertion sort
				for j := i; j > 0 && xs[j].x < xs[j-1].x; j-- {
					xs[j], xs[j-1] = xs[j-1], xs[j]
				}
			}
			wind := 0
			for i := 0; i < len(xs)-1; i++ {
				if evenOdd {
					wind ^= 1
				} else {
					wind += xs[i].dir
				}
				if wind == 0 {
					continue
				}
				a, c := xs[i].x, xs[i+1].x
				// subsample columns whose centre lies in [a, c)
				sa := int(math.Ceil(a*float64(ss) - 0.5))
				sb := int(math.Ceil(c*float64(ss) - 0.5))
				if sa < 0 {
					sa = 0
				}
				if sb > w*ss {
					sb = w * ss
				}
				for s := sa; s < sb; s++ {
					cov[s/ss] += 1 / float64(ss*ss)
					touched = true
				}
			}
		}
		if !touched {
			continue
		}
		for px := 0; px < w; px++ {
			c := cov[px]
			if c <= 0 {
				continue
			}
			if c > 1 {
				c = 1
			}
			d := img.NRGBAAt(px, py)
			da := float64(d.A) / 255
			oa := c + da*(1-c)
			var o [3]float64
			dc := [3]float64{float64(d.R) / 255, float64(d.G) / 255, float64(d.B) / 255}
			for k := 0; k < 3; k++ {
				o[k] = (col[k]*c + dc[k]*da*(1-c)) / oa
			}
			img.SetNRGBA(px, py, color.NRGBA{
				R: uint8(math.Round(o[0] * 255)), G: uint8(math.Round(o[1] * 255)),
				B: uint8(math.Round(o[2] * 255)), A: uint8(math.Round(oa * 255)),
			})
		}
	}
}
