// Package pdf is a small PDF 1.7 writer: pages with content streams, embedded
// TrueType subsets, images, link annotations and outlines.
package pdf

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/ProDGG/fop-go/internal/font"
)

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
	Annots        []Annot
	doc           *Document
}

// NewPage appends a page of the given size in points.
func (d *Document) NewPage(w, h float64) *Page {
	p := &Page{Width: w, Height: h, fonts: map[*Font]bool{}, images: map[*Image]bool{}, doc: d}
	d.Pages = append(d.Pages, p)
	return p
}

// Num formats a number compactly.
func Num(f float64) string {
	s := strconv.FormatFloat(f, 'f', 3, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimSuffix(s, ".")
	}
	if s == "-0" || s == "" {
		return "0"
	}
	return s
}

func (p *Page) op(format string, args ...interface{}) {
	fmt.Fprintf(&p.buf, format, args...)
	p.buf.WriteByte('\n')
}

// Save pushes the graphics state.
func (p *Page) Save() { p.buf.WriteString("q\n") }

// Restore pops the graphics state.
func (p *Page) Restore() { p.buf.WriteString("Q\n") }

// Transform concatenates a matrix.
func (p *Page) Transform(a, b, c, d, e, f float64) {
	p.op("%s %s %s %s %s %s cm", Num(a), Num(b), Num(c), Num(d), Num(e), Num(f))
}

// FillRGB sets the non-stroking colour.
func (p *Page) FillRGB(r, g, b float64) { p.op("%s %s %s rg", Num(r), Num(g), Num(b)) }

// StrokeRGB sets the stroking colour.
func (p *Page) StrokeRGB(r, g, b float64) { p.op("%s %s %s RG", Num(r), Num(g), Num(b)) }

// LineWidth sets the stroke width.
func (p *Page) LineWidth(w float64) { p.op("%s w", Num(w)) }

// Dash sets a dash pattern (nil = solid).
func (p *Page) Dash(on, off float64) {
	if on <= 0 {
		p.op("[] 0 d")
		return
	}
	p.op("[%s %s] 0 d", Num(on), Num(off))
}

// LineCap sets the line cap style (0 butt, 1 round, 2 square).
func (p *Page) LineCap(c int) { p.op("%d J", c) }

// Rect appends a rectangle path (lower-left origin).
func (p *Page) Rect(x, y, w, h float64) {
	p.op("%s %s %s %s re", Num(x), Num(y), Num(w), Num(h))
}

// MoveTo starts a subpath.
func (p *Page) MoveTo(x, y float64) { p.op("%s %s m", Num(x), Num(y)) }

// LineTo adds a line segment.
func (p *Page) LineTo(x, y float64) { p.op("%s %s l", Num(x), Num(y)) }

// ClosePath closes the current subpath.
func (p *Page) ClosePath() { p.buf.WriteString("h\n") }

// Fill fills the current path.
func (p *Page) Fill() { p.buf.WriteString("f\n") }

// Stroke strokes the current path.
func (p *Page) Stroke() { p.buf.WriteString("S\n") }

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
	p.op("BT /%s %s Tf %s %s Td", f.name, Num(size), Num(x), Num(y))
	p.buf.WriteByte('[')
	inStr := false
	for _, g := range glyphs {
		if _, ok := f.used[g.GID]; !ok {
			f.used[g.GID] = g.Rune
		}
		if !inStr {
			p.buf.WriteByte('<')
			inStr = true
		}
		fmt.Fprintf(&p.buf, "%04X", g.GID)
		if g.Extra != 0 {
			p.buf.WriteByte('>')
			inStr = false
			fmt.Fprintf(&p.buf, "%s", Num(-g.Extra*1000/size))
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

func (d *Document) stream(dict string, data []byte, forceCompress bool) []byte {
	var out bytes.Buffer
	if d.Compress || forceCompress {
		var zb bytes.Buffer
		zw, _ := zlib.NewWriterLevel(&zb, zlib.BestCompression)
		zw.Write(data)
		zw.Close()
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
		r := strings.NewReplacer(`\`, `\\`, `(`, `\(`, `)`, `\)`)
		return "(" + r.Replace(s) + ")"
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
		res.WriteString(" >>")
		var annots []string
		for _, a := range p.Annots {
			var action string
			if a.DestPage >= 0 && a.DestPage < len(d.Pages) {
				action = fmt.Sprintf("/Dest [%d 0 R /XYZ 0 %s null]", pageObjs[a.DestPage], Num(a.DestY))
			} else if a.URI != "" {
				action = "/A << /S /URI /URI " + pdfString(a.URI) + " >>"
			} else {
				continue
			}
			n := w.add([]byte(fmt.Sprintf("<< /Type /Annot /Subtype /Link /Rect [%s %s %s %s] /Border [0 0 0] %s >>",
				Num(a.X), Num(a.Y), Num(a.X+a.W), Num(a.Y+a.H), action)))
			annots = append(annots, fmt.Sprintf("%d 0 R", n))
		}
		var pd strings.Builder
		fmt.Fprintf(&pd, "<< /Type /Page /Parent %d 0 R /MediaBox [0 0 %s %s] /Contents %d 0 R /Resources %s",
			pagesRoot, Num(p.Width), Num(p.Height), contents, res.String())
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
				fmt.Fprintf(&sb, " /Dest [%d 0 R /XYZ 0 %s null]", pageObjs[it.Page], Num(it.Y))
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
	psname := strings.NewReplacer(" ", "", "(", "", ")", "").Replace(ft.PSName)
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
		fmt.Fprintf(&ws, "%d[%d]", g, int(float64(ft.Advance(g))*scale+0.5))
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
