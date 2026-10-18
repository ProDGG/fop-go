package xhr

import (
	"math"
	"sort"
	"strings"
	"sync"

	"github.com/ProDGG/fop-go/internal/font"
)

// FontDesc is one concrete font (family member) with the iText/Flying Saucer
// metrics that affect layout.
type FontDesc struct {
	Face   *font.Font
	Weight int
	Italic bool

	upem       float32
	yMax, yMin float32 // head table bbox in font units

	// raw (unscaled) values as Flying Saucer's TrueTypeUtil reads them
	underlinePos, underlineThick float32
	strikePos, strikeSize        float32

	// afmWidths are the base-14 glyph widths (WinAnsi only); nil for TrueType fonts.
	afmWidths map[rune]int

	mu     sync.Mutex
	widths map[rune]int
}

// NewFontDesc wraps a parsed font. bbox may override the head bbox (used when a
// metric-compatible substitute stands in for a font with different vertical
// metrics).
func NewFontDesc(f *font.Font, weight int, italic bool, bbox *[2]int) *FontDesc {
	d := &FontDesc{Face: f, Weight: weight, Italic: italic, upem: float32(f.UnitsPerEm), widths: map[rune]int{}}
	d.yMax, d.yMin = float32(f.BBox[3]), float32(f.BBox[1])
	if bbox != nil {
		d.yMax, d.yMin = float32(bbox[0]), float32(bbox[1])
	}
	d.underlinePos, d.underlineThick = -50, 50
	if os2 := f.Table("OS/2"); len(os2) >= 32 {
		d.strikeSize = float32(int16(uint16(os2[26])<<8 | uint16(os2[27])))
		d.strikePos = float32(int16(uint16(os2[28])<<8 | uint16(os2[29])))
	}
	if post := f.Table("post"); len(post) >= 12 {
		d.underlinePos = float32(int16(uint16(post[8])<<8 | uint16(post[9])))
		d.underlineThick = float32(int16(uint16(post[10])<<8 | uint16(post[11])))
	}
	return d
}

// afmMetrics are the AFM values iText uses for the PDF base-14 fonts.
type afmMetrics struct{ lly, ury, xTop int }

// newBase14Desc wraps a metric-compatible substitute so that vertical metrics and
// decoration geometry follow the base-14 font it stands in for.
func newBase14Desc(f *font.Font, weight int, italic bool, m afmMetrics, widthsKey string) *FontDesc {
	d := &FontDesc{Face: f, Weight: weight, Italic: italic, upem: 1000, widths: map[rune]int{}, afmWidths: base14Widths[widthsKey]}
	d.yMax, d.yMin = float32(m.ury), float32(m.lly)
	d.underlinePos, d.underlineThick = -50, 50
	d.strikePos = float32(m.xTop/2 + 50)
	d.strikeSize = 100
	return d
}

var base14AFM = map[string]map[[2]int]afmMetrics{
	"Helvetica": {{400, 0}: {-225, 931, 523}, {700, 0}: {-228, 962, 532}, {400, 1}: {-225, 931, 523}, {700, 1}: {-228, 962, 532}},
	"Times":     {{400, 0}: {-218, 898, 450}, {700, 0}: {-218, 935, 461}, {400, 1}: {-217, 883, 441}, {700, 1}: {-218, 921, 462}},
	"Courier":   {{400, 0}: {-250, 805, 426}, {700, 0}: {-250, 801, 439}, {400, 1}: {-250, 805, 426}, {700, 1}: {-250, 801, 439}},
}

// charWidth returns the iText glyph width (1/1000 em, integer) of r.
func (d *FontDesc) charWidth(r rune) int {
	if d.afmWidths != nil {
		return d.afmWidths[r]
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if w, ok := d.widths[r]; ok {
		return w
	}
	w := 0
	if d.Face.HasGlyph(r) {
		w = d.Face.Advance(d.Face.GlyphIndex(r)) * 1000 / d.Face.UnitsPerEm
	}
	d.widths[r] = w
	return w
}

// HasGlyph reports whether iText would draw r (unmapped characters are skipped).
func (d *FontDesc) HasGlyph(r rune) bool {
	if d.afmWidths != nil {
		_, ok := d.afmWidths[r]
		return ok
	}
	return d.Face.HasGlyph(r)
}

// FSFont is a font at a given size in dots.
type FSFont struct {
	Desc *FontDesc
	Size float32
}

// Width implements ITextTextRenderer.getWidth.
func (f *FSFont) Width(s string) int {
	sum := 0
	for _, r := range s {
		sum += f.Desc.charWidth(r)
	}
	result := float32(sum) * 0.001
	result *= f.Size
	if float64(result)-math.Floor(float64(result)) < 0.01 {
		return int(result)
	}
	return int(math.Ceil(float64(result)))
}

// FontMetrics mirrors FSFontMetrics as filled by ITextTextRenderer.
type FontMetrics struct {
	Ascent, Descent                     float32
	StrikeOffset, StrikeThickness       float32
	UnderlineOffset, UnderlineThickness float32
}

// Metrics implements ITextTextRenderer.getFSFontMetrics.
func (f *FSFont) Metrics() FontMetrics {
	d := f.Desc
	size := f.Size
	var m FontMetrics
	m.Ascent = size * d.yMax / d.upem
	m.Descent = -(size * d.yMin / d.upem)
	m.StrikeOffset = -d.strikePos / 1000 * size
	if d.strikeSize != 0 {
		m.StrikeThickness = d.strikeSize / 1000 * size
	} else {
		m.StrikeThickness = size / 12
	}
	m.UnderlineOffset = -d.underlinePos / 1000 * size
	m.UnderlineThickness = d.underlineThick / 1000 * size
	return m
}

// FontFamily is a set of fonts under one family name.
type FontFamily struct {
	Name      string
	descs     []*FontDesc
	userAdded map[*FontDesc]bool
}

func (fam *FontFamily) add(d *FontDesc) {
	fam.descs = append(fam.descs, d)
	sort.SliceStable(fam.descs, func(i, j int) bool { return fam.descs[i].Weight < fam.descs[j].Weight })
}

func (fam *FontFamily) match(weight int, italicKind string) *FontDesc {
	// italicKind: "normal", "italic" or "oblique"
	var cands []*FontDesc
	for _, d := range fam.descs {
		st := "normal"
		if d.Italic {
			st = "italic"
		}
		if st == italicKind || (italicKind == "oblique" && d.Italic) {
			cands = append(cands, d)
		}
	}
	if len(cands) == 0 {
		switch italicKind {
		case "italic":
			return fam.match(weight, "oblique")
		case "oblique":
			return fam.match(weight, "normal")
		default:
			cands = fam.descs
		}
	}
	for _, d := range cands {
		if d.Weight == weight {
			return d
		}
	}
	if weight <= 500 {
		// lighter or darker
		var descr *FontDesc
		off := 0
		for off = 0; off < len(cands); off++ {
			descr = cands[off]
			if descr.Weight > weight {
				break
			}
		}
		if off == len(cands) {
			return descr
		}
		if off > 0 && descr.Weight > weight {
			return cands[off-1]
		}
		return descr
	}
	var descr *FontDesc
	off := len(cands) - 1
	for ; off >= 0; off-- {
		descr = cands[off]
		if descr.Weight < weight {
			break
		}
	}
	if off < 0 {
		return descr
	}
	if off != len(cands)-1 && descr.Weight < weight {
		return cands[off+1]
	}
	return descr
}

// FontEngine resolves CSS font specifications like ITextFontResolver.
type FontEngine struct {
	mu       sync.Mutex
	families map[string]*FontFamily
	cache    map[string]*FontDesc
	// Fallback is used when no listed family exists (Flying Saucer falls back
	// to its built-in "Serif").
	Warn func(string)
}

// NewFontEngine builds an engine with the bundled Liberation fonts registered
// under the PDF base-14 family names, plus "Arial" (Liberation Sans carrying
// Arial's vertical metrics).
func NewFontEngine() *FontEngine {
	baseOnce.Do(func() { baseFamilies = buildBaseFamilies() })
	fe := &FontEngine{families: make(map[string]*FontFamily, len(baseFamilies)+4), cache: map[string]*FontDesc{}}
	for k, v := range baseFamilies {
		fe.families[k] = v
	}
	return fe
}

var (
	baseOnce     sync.Once
	baseFamilies map[string]*FontFamily
)

// buildBaseFamilies parses the bundled fonts once; engines share the (read-only) families.
func buildBaseFamilies() map[string]*FontFamily {
	fe := &FontEngine{families: map[string]*FontFamily{}}
	fm := font.NewManager()
	type member struct {
		w  int
		it bool
	}
	members := []member{{400, false}, {700, false}, {400, true}, {700, true}}
	families := []struct {
		names  []string
		family string
		bbox   map[member]*[2]int
		afm    string
	}{
		{[]string{"Arial"}, "liberation sans", map[member]*[2]int{
			{400, false}: {2060, -665}, {700, false}: {2084, -771},
			{400, true}: {2060, -665}, {700, true}: {2084, -771}}, ""},
		{[]string{"Helvetica", "SansSerif", "Dialog"}, "liberation sans", nil, "Helvetica"},
		{[]string{"Serif", "TimesRoman", "Times"}, "liberation serif", nil, "Times"},
		{[]string{"Courier", "Monospaced", "DialogInput"}, "liberation mono", nil, "Courier"},
	}
	for _, fdef := range families {
		fam := &FontFamily{Name: fdef.names[0]}
		for _, m := range members {
			face := fm.Select([]string{fdef.family}, m.w, m.it)
			if face == nil {
				continue
			}
			ft := face.MustFont()
			if ft == nil {
				continue
			}
			var bb *[2]int
			if fdef.bbox != nil {
				bb = fdef.bbox[m]
			}
			if tbl := base14AFM[fdef.afm]; tbl != nil {
				it := 0
				if m.it {
					it = 1
				}
				key := fdef.afm
				if key == "Times" && m.w < 700 && !m.it {
					key = "TimesRoman"
				}
				if m.w >= 700 {
					key += "Bold"
				}
				if m.it && fdef.afm == "Times" {
					key += "Italic"
				} else if m.it {
					key += "Oblique"
				}
				fam.add(newBase14Desc(ft, m.w, m.it, tbl[[2]int{m.w, it}], key))
				continue
			}
			fam.add(NewFontDesc(ft, m.w, m.it, bb))
		}
		for _, n := range fdef.names {
			fe.families[n] = fam
		}
	}
	return fe.families
}

// AddFont registers a TrueType font under family (replacing nothing; members
// are added to the family and sorted by weight).
func (fe *FontEngine) AddFont(family string, data []byte) error {
	ft, err := font.Parse(data)
	if err != nil {
		return err
	}
	fe.mu.Lock()
	defer fe.mu.Unlock()
	if family == "" {
		family = ft.Family
	}
	fam := fe.families[family]
	if fam == nil || fam.userAdded == nil {
		fam = &FontFamily{Name: family}
		fam.userAdded = map[*FontDesc]bool{}
		fe.families[family] = fam
	}
	d := NewFontDesc(ft, ft.Weight, ft.Italic, nil)
	fam.add(d)
	fam.userAdded[d] = true
	fe.cache = map[string]*FontDesc{}
	return nil
}

func normalizeFamily(f string) string {
	r := strings.TrimPrefix(f, `"`)
	r = strings.TrimSuffix(r, `"`)
	switch strings.ToLower(r) {
	case "serif":
		return "Serif"
	case "sans-serif":
		return "SansSerif"
	case "monospace":
		return "Monospaced"
	}
	return r
}

func weightToInt(w string) int {
	switch w {
	case "normal":
		return 400
	case "bold":
		return 700
	case "100", "200", "300", "400", "500", "600", "700", "800", "900":
		n := 0
		for _, c := range w {
			n = n*10 + int(c-'0')
		}
		return n
	case "lighter":
		return 400
	case "bolder":
		return 700
	}
	return 400
}

// Resolve picks the font for a specification.
func (fe *FontEngine) Resolve(families []string, size float32, weight, style string) *FSFont {
	fe.mu.Lock()
	defer fe.mu.Unlock()
	if style != "normal" && style != "oblique" && style != "italic" {
		style = "normal"
	}
	w := weightToInt(weight)
	try := func(name string) *FontDesc {
		n := normalizeFamily(name)
		key := n + "-" + weight + "-" + style
		if d, ok := fe.cache[key]; ok {
			return d
		}
		if fam := fe.families[n]; fam != nil {
			if d := fam.match(w, style); d != nil {
				fe.cache[key] = d
				return d
			}
		}
		return nil
	}
	for _, f := range families {
		if d := try(f); d != nil {
			return &FSFont{Desc: d, Size: size}
		}
	}
	if d := try("Serif"); d != nil {
		return &FSFont{Desc: d, Size: size}
	}
	return nil
}
