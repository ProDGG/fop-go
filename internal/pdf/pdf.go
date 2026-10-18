// Package pdf is a small PDF 1.7 writer: pages with content streams, embedded
// TrueType subsets, images, link annotations and outlines.
package pdf

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/ProDGG/fop-go/internal/font"
)

// ErrPageLimit is returned when a document needs more pages than the
// configured limit (a guard against runaway layouts).
var ErrPageLimit = errors.New("page limit exceeded")

// Info is the document information dictionary.
type Info struct {
	Title, Author, Subject, Keywords, Creator, Producer string
	Created                                             time.Time
}

// Document accumulates pages and resources and writes the PDF file.
type Document struct {
	Info     Info
	Compress bool
	Pages    []*Page
	Outlines []*Outline

	fonts  []*Font
	fontBy map[*font.Font]*Font
	images []*Image
	imgBy  map[string]*Image

	shadings []*Shading

	// ITextNumbers formats numbers like iText 5's ByteBuffer.formatDouble
	// (two decimals, five below one).
	ITextNumbers bool
}

// NewDocument creates an empty document.
func NewDocument() *Document {
	return &Document{Compress: true, fontBy: map[*font.Font]*Font{}, imgBy: map[string]*Image{}}
}

// Outline is a bookmark entry.
type Outline struct {
	Title    string
	Page     int // page index; -1 when unresolved
	Y        float64
	FitH     bool // destination is the top of the page (/FitH Y) instead of /XYZ
	Children []*Outline
	Open     bool
}

// Font is a font resource used by the document.
type Font struct {
	Font *font.Font
	name string
	used map[uint16]rune
}

// UseFont returns the document font resource for ft.
func (d *Document) UseFont(ft *font.Font) *Font {
	if f, ok := d.fontBy[ft]; ok {
		return f
	}
	f := &Font{Font: ft, name: "F" + strconv.Itoa(len(d.fonts)+1), used: map[uint16]rune{}}
	d.fonts = append(d.fonts, f)
	d.fontBy[ft] = f
	return f
}

// Image is an image XObject.
type Image struct {
	Width, Height int
	ColorSpace    string // DeviceRGB, DeviceGray, DeviceCMYK
	BPC           int
	Filter        string // "" (raw samples) or DCTDecode
	Data          []byte
	Alpha         []byte // 8-bit soft mask samples (Width*Height), optional
	Decode        []float64
	key           string
	name          string
}

// AddImage registers an image; key (if non-empty) deduplicates identical sources.
func (d *Document) AddImage(key string, img *Image) *Image {
	if key != "" {
		if e, ok := d.imgBy[key]; ok {
			return e
		}
		d.imgBy[key] = img
	}
	img.key = key
	img.name = "Im" + strconv.Itoa(len(d.images)+1)
	d.images = append(d.images, img)
	return img
}

// Annot is a link annotation.
type Annot struct {
	X, Y, W, H float64
	URI        string
	DestPage   int // -1 for URI links
	DestY      float64
}

// Page is a single page being drawn.
type Page struct {
	Width, Height float64
	buf           bytes.Buffer
	fonts         map[*Font]bool
	images        map[*Image]bool
	shadings      map[*Shading]bool
	gstates       map[string]string // name -> dict
	Annots        []Annot
	doc           *Document
}

// NewPage appends a page of the given size in points.
func (d *Document) NewPage(w, h float64) *Page {
	p := &Page{Width: w, Height: h, fonts: map[*Font]bool{}, images: map[*Image]bool{}, shadings: map[*Shading]bool{}, gstates: map[string]string{}, doc: d}
	d.Pages = append(d.Pages, p)
	return p
}

// Num formats a number compactly (at most three decimals, no trailing zeros).
func Num(f float64) string { return string(appendNum(make([]byte, 0, 16), f)) }

// appendNum is the allocation-free core of Num. It rounds f*1000 to the nearest
// integer instead of going through strconv's slow exact path.
func appendNum(b []byte, f float64) []byte {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		// not representable in PDF; keep the content stream valid
		return append(b, '0')
	}
	if math.Abs(f) >= 1e12 {
		return strconv.AppendFloat(b, f, 'f', -1, 64)
	}
	neg := f < 0
	if neg {
		f = -f
	}
	v := int64(math.RoundToEven(f * 1000))
	if v == 0 {
		return append(b, '0')
	}
	if neg {
		b = append(b, '-')
	}
	b = strconv.AppendInt(b, v/1000, 10)
	if fr := v % 1000; fr != 0 {
		b = append(b, '.', byte('0'+fr/100))
		if fr%100 != 0 {
			b = append(b, byte('0'+fr/10%10))
			if fr%10 != 0 {
				b = append(b, byte('0'+fr%10))
			}
		}
	}
	return b
}

func (d *Document) num(f float64) string {
	return string(d.appendNum(make([]byte, 0, 16), f))
}

func (d *Document) appendNum(b []byte, f float64) []byte {
	if d != nil && d.ITextNumbers {
		return appendITextNum(b, f)
	}
	return appendNum(b, f)
}

func (p *Page) n(f float64) string { return p.doc.num(f) }

// itextNum mirrors com.itextpdf.text.pdf.ByteBuffer.formatDouble.
func itextNum(d float64) string { return string(appendITextNum(make([]byte, 0, 16), d)) }

func appendITextNum(b []byte, d float64) []byte {
	if math.Abs(d) < 0.000015 || math.IsNaN(d) || math.IsInf(d, 0) {
		return append(b, '0')
	}
	if d < 0 {
		b = append(b, '-')
		d = -d
	}
	switch {
	case d < 1.0:
		d += 0.000005
		if d >= 1 {
			return append(b, '1')
		}
		v := int(d * 100000)
		b = append(b, '0', '.')
		var dig [5]byte
		for i := 4; i >= 0; i-- {
			dig[i] = byte('0' + v%10)
			v /= 10
		}
		n := 5
		for n > 0 && dig[n-1] == '0' {
			n--
		}
		return append(b, dig[:n]...)
	case d <= 32767:
		d += 0.005
		v := int(d * 100)
		b = strconv.AppendInt(b, int64(v/100), 10)
		if f := v % 100; f != 0 {
			b = append(b, '.', byte('0'+f/10))
			if f%10 != 0 {
				b = append(b, byte('0'+f%10))
			}
		}
		return b
	default:
		d += 0.5
		return strconv.AppendInt(b, int64(d), 10)
	}
}

func (p *Page) op(format string, args ...interface{}) {
	fmt.Fprintf(&p.buf, format, args...)
	p.buf.WriteByte('\n')
}

// Content returns the content stream written so far.
func (p *Page) Content() []byte { return p.buf.Bytes() }

// Save pushes the graphics state.
func (p *Page) Save() { p.buf.WriteString("q\n") }

// Restore pops the graphics state.
func (p *Page) Restore() { p.buf.WriteString("Q\n") }

// Transform concatenates a matrix.
func (p *Page) Transform(a, b, c, d, e, f float64) {
	p.op("%s %s %s %s %s %s cm", p.n(a), p.n(b), p.n(c), p.n(d), p.n(e), p.n(f))
}

// FillRGB sets the non-stroking colour.
func (p *Page) FillRGB(r, g, b float64) { p.op("%s %s %s rg", p.n(r), p.n(g), p.n(b)) }

// StrokeRGB sets the stroking colour.
func (p *Page) StrokeRGB(r, g, b float64) { p.op("%s %s %s RG", p.n(r), p.n(g), p.n(b)) }

// LineWidth sets the stroke width.
func (p *Page) LineWidth(w float64) { p.op("%s w", p.n(w)) }

// Dash sets a dash pattern (nil = solid).
func (p *Page) Dash(on, off float64) {
	if on < 0 || off <= 0 {
		p.op("[] 0 d")
		return
	}
	p.op("[%s %s] 0 d", p.n(on), p.n(off))
}

// LineCap sets the line cap style (0 butt, 1 round, 2 square).
func (p *Page) LineCap(c int) { p.op("%d J", c) }

// Rect appends a rectangle path (lower-left origin).
func (p *Page) Rect(x, y, w, h float64) {
	p.op("%s %s %s %s re", p.n(x), p.n(y), p.n(w), p.n(h))
}

// MoveTo starts a subpath.
func (p *Page) MoveTo(x, y float64) { p.op("%s %s m", p.n(x), p.n(y)) }

// LineTo adds a line segment.
func (p *Page) LineTo(x, y float64) { p.op("%s %s l", p.n(x), p.n(y)) }

// ClosePath closes the current subpath.
func (p *Page) ClosePath() { p.buf.WriteString("h\n") }

// Fill fills the current path.
func (p *Page) Fill() { p.buf.WriteString("f\n") }

// Stroke strokes the current path.
func (p *Page) Stroke() { p.buf.WriteString("S\n") }

// CurveTo adds a cubic Bezier segment.
func (p *Page) CurveTo(x1, y1, x2, y2, x3, y3 float64) {
	p.op("%s %s %s %s %s %s c", p.n(x1), p.n(y1), p.n(x2), p.n(y2), p.n(x3), p.n(y3))
}

// FillEO fills with the even-odd rule.
func (p *Page) FillEO() { p.buf.WriteString("f*\n") }

// FillStroke fills and strokes the current path.
func (p *Page) FillStroke() { p.buf.WriteString("B\n") }

// FillStrokeEO fills (even-odd) and strokes the current path.
func (p *Page) FillStrokeEO() { p.buf.WriteString("B*\n") }

// ClipEO intersects the clip region using the even-odd rule.
func (p *Page) ClipEO() { p.buf.WriteString("W* n\n") }

// EndPath ends the current path without painting.
func (p *Page) EndPath() { p.buf.WriteString("n\n") }

// LineJoin sets the line join style (0 miter, 1 round, 2 bevel).
func (p *Page) LineJoin(j int) { p.op("%d j", j) }

// MiterLimit sets the miter limit.
func (p *Page) MiterLimit(m float64) { p.op("%s M", p.n(m)) }

// DashArray sets an arbitrary dash pattern.
func (p *Page) DashArray(a []float64, phase float64) {
	if len(a) == 0 {
		p.op("[] 0 d")
		return
	}
	var sb strings.Builder
	for i, v := range a {
		if i > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(p.n(v))
	}
	p.op("[%s] %s d", sb.String(), p.n(phase))
}

// SetAlpha sets the fill and stroke opacity through an ExtGState.
func (p *Page) SetAlpha(fill, stroke float64) {
	dict := fmt.Sprintf("<< /Type /ExtGState /ca %s /CA %s >>", p.n(fill), p.n(stroke))
	name := ""
	for n, d := range p.gstates {
		if d == dict {
			name = n
		}
	}
	if name == "" {
		name = "GS" + strconv.Itoa(len(p.gstates)+1)
		p.gstates[name] = dict
	}
	p.op("/%s gs", name)
}

// GradStop is one colour stop of a gradient.
type GradStop struct {
	Off     float64
	R, G, B float64
}

// Shading is an axial or radial gradient used as a fill pattern. Matrix maps the
// gradient's coordinate space to the default coordinate space of the page.
type Shading struct {
	Radial bool
	Coords []float64
	Stops  []GradStop
	Matrix [6]float64
	name   string
}

// AddShading registers a gradient with the document.
func (d *Document) AddShading(sh *Shading) *Shading {
	sh.name = "Sh" + strconv.Itoa(len(d.shadings)+1)
	d.shadings = append(d.shadings, sh)
	return sh
}

// FillShading selects the gradient as the fill colour.
func (p *Page) FillShading(sh *Shading) {
	p.shadings[sh] = true
	p.op("/Pattern cs /%s scn", sh.name)
}

// Clip intersects the clip region with the current path.
func (p *Page) Clip() { p.buf.WriteString("W n\n") }

// FillRect fills a rectangle with the given RGB colour.
func (p *Page) FillRect(x, y, w, h float64, r, g, b float64) {
	p.FillRGB(r, g, b)
	p.Rect(x, y, w, h)
	p.Fill()
}

// Glyph is a glyph to draw with extra advance (points) applied after it.
type Glyph struct {
	GID   uint16
	Rune  rune
	Extra float64
}

// Text draws glyphs starting at (x, y) on the baseline.
func (p *Page) Text(f *Font, size, x, y float64, glyphs []Glyph) {
	if len(glyphs) == 0 {
		return
	}
	p.fonts[f] = true
	var tmp [48]byte
	b := append(tmp[:0], "BT /"...)
	b = append(b, f.name...)
	b = append(b, ' ')
	b = p.doc.appendNum(b, size)
	b = append(b, " Tf "...)
	b = p.doc.appendNum(b, x)
	b = append(b, ' ')
	b = p.doc.appendNum(b, y)
	b = append(b, " Td\n["...)
	p.buf.Write(b)
	inStr := false
	for _, g := range glyphs {
		if _, ok := f.used[g.GID]; !ok {
			f.used[g.GID] = g.Rune
		}
		if !inStr {
			p.buf.WriteByte('<')
			inStr = true
		}
		const hexd = "0123456789ABCDEF"
		p.buf.Write([]byte{hexd[g.GID>>12&15], hexd[g.GID>>8&15], hexd[g.GID>>4&15], hexd[g.GID&15]})
		if g.Extra != 0 {
			p.buf.WriteByte('>')
			inStr = false
			p.buf.Write(p.doc.appendNum(tmp[:0], -g.Extra*1000/size))
		}
	}
	if inStr {
		p.buf.WriteByte('>')
	}
	p.buf.WriteString("] TJ ET\n")
}

// DrawImage paints an image into the rectangle (lower-left origin).
func (p *Page) DrawImage(img *Image, x, y, w, h float64) {
	p.images[img] = true
	p.Save()
	p.Transform(w, 0, 0, h, x, y)
	p.op("/%s Do", img.name)
	p.Restore()
}

// AddURILink adds a clickable rectangle that opens a URI.
func (p *Page) AddURILink(x, y, w, h float64, uri string) {
	p.Annots = append(p.Annots, Annot{X: x, Y: y, W: w, H: h, URI: uri, DestPage: -1})
}

// AddDestLink adds a clickable rectangle that jumps within the document.
func (p *Page) AddDestLink(x, y, w, h float64, page int, destY float64) {
	p.Annots = append(p.Annots, Annot{X: x, Y: y, W: w, H: h, DestPage: page, DestY: destY})
}

// ---------- serialisation ----------

type writer struct {
	objs [][]byte // 1-based: objs[i] is object i+1 (nil until set)
}

func (w *writer) alloc() int {
	w.objs = append(w.objs, nil)
	return len(w.objs)
}

func (w *writer) set(n int, body []byte) { w.objs[n-1] = body }

func (w *writer) add(body []byte) int {
	n := w.alloc()
	w.set(n, body)
	return n
}

// zlibPool reuses deflate state (a compressor allocates ~1MB). The default
// level is what iText uses and is about twice as fast as BestCompression.
var zlibPool = sync.Pool{New: func() interface{} {
	w, _ := zlib.NewWriterLevel(io.Discard, zlib.DefaultCompression)
	return w
}}

func (d *Document) stream(dict string, data []byte, forceCompress bool) []byte {
	var out bytes.Buffer
	if d.Compress || forceCompress {
		var zb bytes.Buffer
		zw := zlibPool.Get().(*zlib.Writer)
		zw.Reset(&zb)
		zw.Write(data)
		zw.Close()
		zlibPool.Put(zw)
		if zb.Len() < len(data) || forceCompress {
			data = zb.Bytes()
			dict += " /Filter /FlateDecode"
		}
	}
	fmt.Fprintf(&out, "<< %s /Length %d >>\nstream\n", dict, len(data))
	out.Write(data)
	out.WriteString("\nendstream")
	return out.Bytes()
}

// pdfString encodes a text string (PDFDocEncoding when ASCII, else UTF-16BE).
func pdfString(s string) string {
	ascii := true
	for _, r := range s {
		if r > 126 || r < 32 {
			ascii = false
			break
		}
	}
	if ascii {
		return "(" + pdfStringEscaper.Replace(s) + ")"
	}
	u := utf16.Encode([]rune(s))
	var sb strings.Builder
	sb.WriteString("<FEFF")
	for _, c := range u {
		fmt.Fprintf(&sb, "%04X", c)
	}
	sb.WriteString(">")
	return sb.String()
}

func pdfDate(t time.Time) string {
	_, off := t.Zone()
	sign := "+"
	if off < 0 {
		sign = "-"
		off = -off
	}
	return fmt.Sprintf("(D:%s%s%02d'%02d')", t.Format("20060102150405"), sign, off/3600, (off%3600)/60)
}

func subsetTag(n int, key string) string {
	h := uint32(2166136261)
	for i := 0; i < len(key); i++ {
		h = (h ^ uint32(key[i])) * 16777619
	}
	h += uint32(n) * 2654435761
	b := make([]byte, 6)
	for i := range b {
		b[i] = 'A' + byte(h%26)
		h /= 26
		if h == 0 {
			h = 0x9E3779B9 * uint32(i+3)
		}
	}
	return string(b)
}

// Write serialises the document.
func (d *Document) Write(out io.Writer) error {
	w := &writer{}
	catalog := w.alloc()
	pagesRoot := w.alloc()
	pageObjs := make([]int, len(d.Pages))
	for i := range d.Pages {
		pageObjs[i] = w.alloc()
	}

	// fonts
	fontObjs := map[*Font]int{}
	for i, f := range d.fonts {
		fontObjs[f] = d.writeFont(w, f, i)
	}
	// images
	imgObjs := map[*Image]int{}
	for _, im := range d.images {
		imgObjs[im] = d.writeImage(w, im)
	}

	shadingObjs := map[*Shading]int{}
	for _, sh := range d.shadings {
		shadingObjs[sh] = w.add([]byte(shadingDict(sh)))
	}

	for i, p := range d.Pages {
		contents := w.add(d.stream("", p.buf.Bytes(), false))
		var res strings.Builder
		res.WriteString("<< /ProcSet [/PDF /Text /ImageB /ImageC]")
		if len(p.fonts) > 0 {
			res.WriteString(" /Font <<")
			var fs []*Font
			for f := range p.fonts {
				fs = append(fs, f)
			}
			sort.Slice(fs, func(a, b int) bool { return fs[a].name < fs[b].name })
			for _, f := range fs {
				fmt.Fprintf(&res, " /%s %d 0 R", f.name, fontObjs[f])
			}
			res.WriteString(" >>")
		}
		if len(p.images) > 0 {
			res.WriteString(" /XObject <<")
			var is []*Image
			for im := range p.images {
				is = append(is, im)
			}
			sort.Slice(is, func(a, b int) bool { return is[a].name < is[b].name })
			for _, im := range is {
				fmt.Fprintf(&res, " /%s %d 0 R", im.name, imgObjs[im])
			}
			res.WriteString(" >>")
		}
		if len(p.shadings) > 0 {
			res.WriteString(" /Pattern <<")
			var ss []*Shading
			for sh := range p.shadings {
				ss = append(ss, sh)
			}
			sort.Slice(ss, func(a, b int) bool { return ss[a].name < ss[b].name })
			for _, sh := range ss {
				fmt.Fprintf(&res, " /%s %d 0 R", sh.name, shadingObjs[sh])
			}
			res.WriteString(" >>")
		}
		if len(p.gstates) > 0 {
			res.WriteString(" /ExtGState <<")
			var ns []string
			for n := range p.gstates {
				ns = append(ns, n)
			}
			sort.Strings(ns)
			for _, n := range ns {
				fmt.Fprintf(&res, " /%s %s", n, p.gstates[n])
			}
			res.WriteString(" >>")
		}
		res.WriteString(" >>")
		var annots []string
		for _, a := range p.Annots {
			var action string
			if a.DestPage >= 0 && a.DestPage < len(d.Pages) {
				action = fmt.Sprintf("/Dest [%d 0 R /XYZ 0 %s null]", pageObjs[a.DestPage], d.num(a.DestY))
			} else if a.URI != "" {
				action = "/A << /S /URI /URI " + pdfString(a.URI) + " >>"
			} else {
				continue
			}
			n := w.add([]byte(fmt.Sprintf("<< /Type /Annot /Subtype /Link /Rect [%s %s %s %s] /Border [0 0 0] %s >>",
				d.num(a.X), d.num(a.Y), d.num(a.X+a.W), d.num(a.Y+a.H), action)))
			annots = append(annots, fmt.Sprintf("%d 0 R", n))
		}
		var pd strings.Builder
		fmt.Fprintf(&pd, "<< /Type /Page /Parent %d 0 R /MediaBox [0 0 %s %s] /Contents %d 0 R /Resources %s",
			pagesRoot, d.num(p.Width), d.num(p.Height), contents, res.String())
		if len(annots) > 0 {
			fmt.Fprintf(&pd, " /Annots [%s]", strings.Join(annots, " "))
		}
		pd.WriteString(" >>")
		w.set(pageObjs[i], []byte(pd.String()))
	}

	kids := make([]string, len(pageObjs))
	for i, n := range pageObjs {
		kids[i] = fmt.Sprintf("%d 0 R", n)
	}
	w.set(pagesRoot, []byte(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(pageObjs))))

	catDict := fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R", pagesRoot)
	if len(d.Outlines) > 0 {
		root := d.writeOutlines(w, pageObjs)
		catDict += fmt.Sprintf(" /Outlines %d 0 R /PageMode /UseOutlines", root)
	}
	catDict += " >>"
	w.set(catalog, []byte(catDict))

	created := d.Info.Created
	if created.IsZero() {
		created = time.Now()
	}
	var info strings.Builder
	info.WriteString("<<")
	for _, kv := range [][2]string{{"Title", d.Info.Title}, {"Author", d.Info.Author}, {"Subject", d.Info.Subject},
		{"Keywords", d.Info.Keywords}, {"Creator", d.Info.Creator}, {"Producer", d.Info.Producer}} {
		if kv[1] != "" {
			fmt.Fprintf(&info, " /%s %s", kv[0], pdfString(kv[1]))
		}
	}
	fmt.Fprintf(&info, " /CreationDate %s /ModDate %s >>", pdfDate(created), pdfDate(created))
	infoObj := w.add([]byte(info.String()))

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n%\xE2\xE3\xCF\xD3\n")
	offsets := make([]int, len(w.objs))
	for i, body := range w.objs {
		offsets[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n", i+1)
		buf.Write(body)
		buf.WriteString("\nendobj\n")
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(w.objs)+1)
	for _, o := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root %d 0 R /Info %d 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(w.objs)+1, catalog, infoObj, xref)
	_, err := out.Write(buf.Bytes())
	return err
}

func (d *Document) writeOutlines(w *writer, pageObjs []int) int {
	root := w.alloc()
	var build func(items []*Outline, parent int) (first, last, count int)
	build = func(items []*Outline, parent int) (int, int, int) {
		nums := make([]int, len(items))
		for i := range items {
			nums[i] = w.alloc()
		}
		total := len(items)
		for i, it := range items {
			var sb strings.Builder
			fmt.Fprintf(&sb, "<< /Title %s /Parent %d 0 R", pdfString(it.Title), parent)
			if i > 0 {
				fmt.Fprintf(&sb, " /Prev %d 0 R", nums[i-1])
			}
			if i < len(items)-1 {
				fmt.Fprintf(&sb, " /Next %d 0 R", nums[i+1])
			}
			if it.Page >= 0 && it.Page < len(pageObjs) {
				if it.FitH {
					fmt.Fprintf(&sb, " /Dest [%d 0 R /FitH %s]", pageObjs[it.Page], d.num(it.Y))
				} else {
					fmt.Fprintf(&sb, " /Dest [%d 0 R /XYZ 0 %s null]", pageObjs[it.Page], d.num(it.Y))
				}
			}
			if len(it.Children) > 0 {
				f, l, c := build(it.Children, nums[i])
				cnt := c
				if !it.Open {
					cnt = -c
				} else {
					total += c
				}
				fmt.Fprintf(&sb, " /First %d 0 R /Last %d 0 R /Count %d", f, l, cnt)
			}
			sb.WriteString(" >>")
			w.set(nums[i], []byte(sb.String()))
		}
		return nums[0], nums[len(nums)-1], total
	}
	f, l, c := build(d.Outlines, root)
	w.set(root, []byte(fmt.Sprintf("<< /Type /Outlines /First %d 0 R /Last %d 0 R /Count %d >>", f, l, c)))
	return root
}

func (d *Document) writeImage(w *writer, im *Image) int {
	dict := fmt.Sprintf("/Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /%s /BitsPerComponent %d",
		im.Width, im.Height, im.ColorSpace, im.BPC)
	if len(im.Decode) > 0 {
		var ds []string
		for _, v := range im.Decode {
			ds = append(ds, Num(v))
		}
		dict += " /Decode [" + strings.Join(ds, " ") + "]"
	}
	if len(im.Alpha) > 0 {
		sm := fmt.Sprintf("/Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceGray /BitsPerComponent 8", im.Width, im.Height)
		n := w.add(d.stream(sm, im.Alpha, true))
		dict += fmt.Sprintf(" /SMask %d 0 R", n)
	}
	if im.Filter == "DCTDecode" {
		var out bytes.Buffer
		fmt.Fprintf(&out, "<< %s /Filter /DCTDecode /Length %d >>\nstream\n", dict, len(im.Data))
		out.Write(im.Data)
		out.WriteString("\nendstream")
		return w.add(out.Bytes())
	}
	return w.add(d.stream(dict, im.Data, true))
}

func (d *Document) writeFont(w *writer, f *Font, idx int) int {
	ft := f.Font
	gids := make([]uint16, 0, len(f.used))
	for g := range f.used {
		gids = append(gids, g)
	}
	sort.Slice(gids, func(i, j int) bool { return gids[i] < gids[j] })
	sub := ft.Subset(gids)
	tag := subsetTag(idx, ft.PSName+ft.Family)
	psname := psNameCleaner.Replace(ft.PSName)
	if psname == "" {
		psname = strings.ReplaceAll(ft.Family+ft.SubFamily, " ", "")
	}
	baseName := tag + "+" + psname

	fontFile := w.add(d.stream(fmt.Sprintf("/Length1 %d", len(sub)), sub, true))

	scale := 1000.0 / float64(ft.UnitsPerEm)
	flags := 4 // symbolic (we use glyph ids directly)
	if ft.FixedPitch {
		flags |= 1
	}
	if ft.Italic {
		flags |= 64
	}
	stemV := 80
	if ft.Bold {
		stemV = 140
	}
	desc := w.add([]byte(fmt.Sprintf("<< /Type /FontDescriptor /FontName /%s /Flags %d /FontBBox [%d %d %d %d] /ItalicAngle %s /Ascent %d /Descent %d /CapHeight %d /StemV %d /FontFile2 %d 0 R >>",
		baseName, flags,
		int(float64(ft.BBox[0])*scale), int(float64(ft.BBox[1])*scale), int(float64(ft.BBox[2])*scale), int(float64(ft.BBox[3])*scale),
		Num(ft.ItalicAng), int(float64(ft.Ascent)*scale), int(float64(ft.Descent)*scale), int(float64(ft.CapHeight)*scale), stemV, fontFile)))

	var ws strings.Builder
	ws.WriteString("[")
	for _, g := range gids {
		fmt.Fprintf(&ws, "%d[%d]", g, ft.Advance(g)*1000/ft.UnitsPerEm)
	}
	ws.WriteString("]")
	cid := w.add([]byte(fmt.Sprintf("<< /Type /Font /Subtype /CIDFontType2 /BaseFont /%s /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /FontDescriptor %d 0 R /CIDToGIDMap /Identity /DW 1000 /W %s >>",
		baseName, desc, ws.String())))

	// ToUnicode
	var cm strings.Builder
	cm.WriteString("/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def\n/CMapName /Adobe-Identity-UCS def\n/CMapType 2 def\n1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n")
	for i := 0; i < len(gids); i += 100 {
		end := i + 100
		if end > len(gids) {
			end = len(gids)
		}
		fmt.Fprintf(&cm, "%d beginbfchar\n", end-i)
		for _, g := range gids[i:end] {
			r := f.used[g]
			var u strings.Builder
			for _, c := range utf16.Encode([]rune{r}) {
				fmt.Fprintf(&u, "%04X", c)
			}
			fmt.Fprintf(&cm, "<%04X> <%s>\n", g, u.String())
		}
		cm.WriteString("endbfchar\n")
	}
	cm.WriteString("endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend\n")
	toUni := w.add(d.stream("", []byte(cm.String()), false))

	return w.add([]byte(fmt.Sprintf("<< /Type /Font /Subtype /Type0 /BaseFont /%s /Encoding /Identity-H /DescendantFonts [%d 0 R] /ToUnicode %d 0 R >>",
		baseName, cid, toUni)))
}

func shadingDict(sh *Shading) string {
	var fn string
	col := func(g GradStop) string { return Num(g.R) + " " + Num(g.G) + " " + Num(g.B) }
	st := sh.Stops
	if len(st) == 1 {
		st = append(st, st[0])
	}
	if len(st) == 2 {
		fn = fmt.Sprintf("<< /FunctionType 2 /Domain [0 1] /C0 [%s] /C1 [%s] /N 1 >>", col(st[0]), col(st[1]))
	} else {
		var fns, bounds, enc []string
		for i := 0; i+1 < len(st); i++ {
			fns = append(fns, fmt.Sprintf("<< /FunctionType 2 /Domain [0 1] /C0 [%s] /C1 [%s] /N 1 >>", col(st[i]), col(st[i+1])))
			enc = append(enc, "0 1")
			if i > 0 {
				bounds = append(bounds, Num(st[i].Off))
			}
		}
		fn = fmt.Sprintf("<< /FunctionType 3 /Domain [0 1] /Functions [%s] /Bounds [%s] /Encode [%s] >>",
			strings.Join(fns, " "), strings.Join(bounds, " "), strings.Join(enc, " "))
	}
	var cs []string
	for _, c := range sh.Coords {
		cs = append(cs, Num(c))
	}
	typ := 2
	if sh.Radial {
		typ = 3
	}
	var m []string
	for _, v := range sh.Matrix {
		m = append(m, strconv.FormatFloat(v, 'f', 5, 64))
	}
	return fmt.Sprintf("<< /PatternType 2 /Matrix [%s] /Shading << /ShadingType %d /ColorSpace /DeviceRGB /Coords [%s] /Function %s /Extend [true true] >> >>",
		strings.Join(m, " "), typ, strings.Join(cs, " "), fn)
}

var (
	pdfStringEscaper = strings.NewReplacer(`\`, `\\`, `(`, `\(`, `)`, `\)`)
	psNameCleaner    = strings.NewReplacer(" ", "", "(", "", ")", "")
)
