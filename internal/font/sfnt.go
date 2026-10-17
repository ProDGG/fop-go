// Package font parses TrueType fonts, subsets them for PDF embedding and
// selects fonts by family/weight/style for the layout engine.
package font

import (
	"encoding/binary"
	"fmt"
	"sort"
	"strings"
	"unicode/utf16"
)

// Font is a parsed TrueType font (glyf outlines).
type Font struct {
	Data       []byte
	Family     string
	SubFamily  string
	PSName     string
	Bold       bool
	Italic     bool
	UnitsPerEm int
	Ascent     int // hhea, in font units
	Descent    int // negative
	LineGap    int
	CapHeight  int
	XHeight    int
	BBox       [4]int
	ItalicAng  float64
	FixedPitch bool
	NumGlyphs  int
	Weight     int

	// Metrics in 1/1000 em derived like Apache FOP (OpenFont.determineAscDesc):
	// OS/2 typo values when they fit into the em box, otherwise hhea.
	Asc1000, Desc1000, XHeight1000, Cap1000 int

	tables  map[string][]byte
	advance []uint16
	cmap    map[rune]uint16
	loca    []uint32
	kern    map[uint32]int16

	gposDone    bool
	kernLookups []pairLookup
}

func be16(b []byte, o int) int  { return int(binary.BigEndian.Uint16(b[o:])) }
func bes16(b []byte, o int) int { return int(int16(binary.BigEndian.Uint16(b[o:]))) }
func be32(b []byte, o int) int  { return int(binary.BigEndian.Uint32(b[o:])) }

// Parse reads a TrueType font file (a TrueType collection yields its first font).
func Parse(data []byte) (*Font, error) {
	if len(data) < 12 {
		return nil, fmt.Errorf("font: file too short")
	}
	base := 0
	tag := string(data[:4])
	if tag == "ttcf" {
		base = be32(data, 12)
		tag = string(data[base : base+4])
	}
	switch tag {
	case "OTTO":
		return nil, fmt.Errorf("font: OpenType fonts with CFF outlines are not supported (use TrueType .ttf)")
	case "\x00\x01\x00\x00", "true":
	default:
		return nil, fmt.Errorf("font: unrecognised font format %q", tag)
	}
	n := be16(data, base+4)
	f := &Font{Data: data, tables: map[string][]byte{}}
	for i := 0; i < n; i++ {
		o := base + 12 + i*16
		if o+16 > len(data) {
			return nil, fmt.Errorf("font: truncated table directory")
		}
		name := string(data[o : o+4])
		off, ln := be32(data, o+8), be32(data, o+12)
		if off < 0 || ln < 0 || off+ln > len(data) {
			return nil, fmt.Errorf("font: table %q out of bounds", name)
		}
		f.tables[name] = data[off : off+ln]
	}
	for _, req := range []string{"head", "hhea", "maxp", "hmtx", "cmap", "loca", "glyf"} {
		if _, ok := f.tables[req]; !ok {
			return nil, fmt.Errorf("font: missing required table %q", req)
		}
	}
	head := f.tables["head"]
	if len(head) < 54 {
		return nil, fmt.Errorf("font: bad head table")
	}
	f.UnitsPerEm = be16(head, 18)
	if f.UnitsPerEm == 0 {
		return nil, fmt.Errorf("font: unitsPerEm is zero")
	}
	f.BBox = [4]int{bes16(head, 36), bes16(head, 38), bes16(head, 40), bes16(head, 42)}
	macStyle := be16(head, 44)
	f.Bold = macStyle&1 != 0
	f.Italic = macStyle&2 != 0
	locaFmt := bes16(head, 50)

	hhea := f.tables["hhea"]
	f.Ascent, f.Descent, f.LineGap = bes16(hhea, 4), bes16(hhea, 6), bes16(hhea, 8)
	nh := be16(hhea, 34)
	f.NumGlyphs = be16(f.tables["maxp"], 4)

	hmtx := f.tables["hmtx"]
	f.advance = make([]uint16, f.NumGlyphs)
	var last uint16
	for g := 0; g < f.NumGlyphs; g++ {
		if g < nh {
			if g*4+2 > len(hmtx) {
				break
			}
			last = uint16(be16(hmtx, g*4))
		}
		f.advance[g] = last
	}

	// loca
	loca := f.tables["loca"]
	f.loca = make([]uint32, f.NumGlyphs+1)
	for g := 0; g <= f.NumGlyphs; g++ {
		if locaFmt == 0 {
			if g*2+2 > len(loca) {
				return nil, fmt.Errorf("font: truncated loca")
			}
			f.loca[g] = uint32(be16(loca, g*2)) * 2
		} else {
			if g*4+4 > len(loca) {
				return nil, fmt.Errorf("font: truncated loca")
			}
			f.loca[g] = uint32(be32(loca, g*4))
		}
	}

	if post, ok := f.tables["post"]; ok && len(post) >= 16 {
		f.ItalicAng = float64(int32(binary.BigEndian.Uint32(post[4:]))) / 65536
		f.FixedPitch = be32(post, 12) != 0
	}
	f.Weight = 400
	if os2, ok := f.tables["OS/2"]; ok && len(os2) >= 78 {
		f.Weight = be16(os2, 4)
		if len(os2) >= 90 && be16(os2, 0) >= 2 {
			f.XHeight = bes16(os2, 86)
			f.CapHeight = bes16(os2, 88)
		}
		fsSel := be16(os2, 62)
		if fsSel&1 != 0 {
			f.Italic = true
		}
		if fsSel&0x20 != 0 {
			f.Bold = true
		}
		if f.Weight >= 600 {
			f.Bold = true
		}
	}
	if f.CapHeight == 0 {
		f.CapHeight = f.Ascent * 7 / 10
	}
	if f.XHeight == 0 {
		f.XHeight = f.CapHeight * 7 / 10
	}
	f.computeFOPMetrics()
	f.parseNames()
	if err := f.parseCmap(); err != nil {
		return nil, err
	}
	f.parseKern()
	return f, nil
}

func (f *Font) parseNames() {
	name, ok := f.tables["name"]
	if !ok || len(name) < 6 {
		return
	}
	count, strOff := be16(name, 2), be16(name, 4)
	get := func(id int) string {
		best := ""
		for i := 0; i < count; i++ {
			o := 6 + i*12
			if o+12 > len(name) {
				break
			}
			pid, eid, nid := be16(name, o), be16(name, o+2), be16(name, o+6)
			ln, off := be16(name, o+8), be16(name, o+10)
			if nid != id || strOff+off+ln > len(name) {
				continue
			}
			raw := name[strOff+off : strOff+off+ln]
			var s string
			switch {
			case pid == 3 || pid == 0:
				u := make([]uint16, len(raw)/2)
				for j := range u {
					u[j] = uint16(be16(raw, j*2))
				}
				s = string(utf16.Decode(u))
			case pid == 1 && eid == 0:
				s = string(raw)
			default:
				continue
			}
			if best == "" || pid == 3 {
				best = s
			}
		}
		return best
	}
	f.Family = get(16)
	if f.Family == "" {
		f.Family = get(1)
	}
	f.SubFamily = get(17)
	if f.SubFamily == "" {
		f.SubFamily = get(2)
	}
	f.PSName = get(6)
	sub := strings.ToLower(f.SubFamily)
	if strings.Contains(sub, "bold") {
		f.Bold = true
	}
	if strings.Contains(sub, "italic") || strings.Contains(sub, "oblique") {
		f.Italic = true
	}
}

func (f *Font) parseCmap() error {
	cm := f.tables["cmap"]
	if len(cm) < 4 {
		return fmt.Errorf("font: bad cmap")
	}
	n := be16(cm, 2)
	type sub struct{ pid, eid, off int }
	var subs []sub
	for i := 0; i < n; i++ {
		o := 4 + i*8
		if o+8 > len(cm) {
			break
		}
		subs = append(subs, sub{be16(cm, o), be16(cm, o+2), be32(cm, o+4)})
	}
	rank := func(s sub) int {
		switch {
		case s.pid == 3 && s.eid == 10:
			return 0
		case s.pid == 0 && (s.eid == 4 || s.eid == 6):
			return 1
		case s.pid == 3 && s.eid == 1:
			return 2
		case s.pid == 0:
			return 3
		case s.pid == 1 && s.eid == 0:
			return 5
		}
		return 9
	}
	sort.SliceStable(subs, func(i, j int) bool { return rank(subs[i]) < rank(subs[j]) })
	f.cmap = map[rune]uint16{}
	for _, s := range subs {
		if rank(s) >= 9 || s.off >= len(cm) {
			continue
		}
		t := cm[s.off:]
		if err := f.readCmapTable(t); err == nil && len(f.cmap) > 0 {
			return nil
		}
	}
	if len(f.cmap) == 0 {
		return fmt.Errorf("font: no usable Unicode cmap")
	}
	return nil
}

func (f *Font) readCmapTable(t []byte) error {
	if len(t) < 6 {
		return fmt.Errorf("short")
	}
	switch be16(t, 0) {
	case 0:
		for c := 0; c < 256 && 6+c < len(t); c++ {
			if g := t[6+c]; g != 0 {
				f.cmap[rune(c)] = uint16(g)
			}
		}
	case 4:
		segX2 := be16(t, 6)
		endO, startO := 14, 16+segX2
		deltaO, rangeO := startO+segX2, startO+2*segX2
		if rangeO+segX2 > len(t) {
			return fmt.Errorf("truncated cmap 4")
		}
		for i := 0; i < segX2/2; i++ {
			end, start := be16(t, endO+i*2), be16(t, startO+i*2)
			delta, ro := be16(t, deltaO+i*2), be16(t, rangeO+i*2)
			for c := start; c <= end && c != 0xFFFF; c++ {
				var g int
				if ro == 0 {
					g = (c + delta) & 0xFFFF
				} else {
					p := rangeO + i*2 + ro + (c-start)*2
					if p+2 > len(t) {
						continue
					}
					g = be16(t, p)
					if g != 0 {
						g = (g + delta) & 0xFFFF
					}
				}
				if g != 0 {
					f.cmap[rune(c)] = uint16(g)
				}
			}
		}
	case 6:
		first, cnt := be16(t, 6), be16(t, 8)
		for i := 0; i < cnt && 10+i*2+2 <= len(t); i++ {
			if g := be16(t, 10+i*2); g != 0 {
				f.cmap[rune(first+i)] = uint16(g)
			}
		}
	case 12:
		if len(t) < 16 {
			return fmt.Errorf("short cmap 12")
		}
		ng := be32(t, 12)
		for i := 0; i < ng && 16+i*12+12 <= len(t); i++ {
			s, e, g := be32(t, 16+i*12), be32(t, 20+i*12), be32(t, 24+i*12)
			if e-s > 0x10FFFF {
				continue
			}
			for c := s; c <= e; c++ {
				f.cmap[rune(c)] = uint16(g + c - s)
			}
		}
	default:
		return fmt.Errorf("unsupported cmap format")
	}
	return nil
}

// parseKern reads a format 0 kern table (pairs of glyphs).
func (f *Font) parseKern() {
	k, ok := f.tables["kern"]
	if !ok || len(k) < 4 {
		return
	}
	n := be16(k, 2)
	off := 4
	f.kern = map[uint32]int16{}
	for i := 0; i < n && off+6 <= len(k); i++ {
		length := be16(k, off+2)
		cov := be16(k, off+4)
		if cov>>8 == 0 && cov&1 == 1 && off+14 <= len(k) {
			np := be16(k, off+6)
			for j := 0; j < np && off+14+j*6+6 <= len(k); j++ {
				o := off + 14 + j*6
				f.kern[uint32(be16(k, o))<<16|uint32(be16(k, o+2))] = int16(bes16(k, o+4))
			}
		}
		if length == 0 {
			break
		}
		off += length
	}
}

// GlyphIndex returns the glyph for r (0 if missing).
func (f *Font) GlyphIndex(r rune) uint16 { return f.cmap[r] }

// HasGlyph reports whether the font maps r to a real glyph.
func (f *Font) HasGlyph(r rune) bool {
	_, ok := f.cmap[r]
	return ok
}

// Advance returns the advance width of a glyph in font units.
func (f *Font) Advance(gid uint16) int {
	if int(gid) < len(f.advance) {
		return int(f.advance[gid])
	}
	return 0
}

// Kern returns the kerning adjustment between two glyphs in font units: the GPOS
// 'kern' feature when the font has one (what FOP uses with complex scripts
// enabled), otherwise the legacy kern table.
func (f *Font) Kern(a, b uint16) int {
	if !f.gposDone {
		f.loadKern()
	}
	if len(f.kernLookups) > 0 {
		v, _ := f.gposKern(a, b)
		return v
	}
	if f.kern == nil {
		return 0
	}
	return int(f.kern[uint32(a)<<16|uint32(b)])
}

// Runes returns every code point the font maps.
func (f *Font) Runes() map[rune]uint16 { return f.cmap }

// glyphData returns the raw glyf bytes of a glyph.
func (f *Font) glyphData(gid int) []byte {
	glyf := f.tables["glyf"]
	if gid < 0 || gid >= f.NumGlyphs {
		return nil
	}
	s, e := f.loca[gid], f.loca[gid+1]
	if e <= s || int(e) > len(glyf) {
		return nil
	}
	return glyf[s:e]
}

// computeFOPMetrics reproduces Apache FOP's choice of ascender/descender
// (OpenFont.determineAscDesc) and the conversion to 1/1000 em units.
func (f *Font) computeFOPMetrics() {
	var os2Asc, os2Desc int
	if os2, ok := f.tables["OS/2"]; ok && len(os2) >= 72 {
		os2Asc, os2Desc = bes16(os2, 68), bes16(os2, 70)
	}
	hheaAsc, hheaDesc := f.Ascent, f.Descent
	upem := f.UnitsPerEm
	asc, desc := hheaAsc, hheaDesc
	switch {
	case os2Asc > 0 && os2Asc-os2Desc <= upem:
		asc, desc = os2Asc, os2Desc
	case hheaAsc > 0 && hheaAsc-hheaDesc <= upem:
		asc, desc = hheaAsc, hheaDesc
	case os2Asc > 0:
		asc, desc = os2Asc, os2Desc
	}
	conv := func(v int) int { return v * 1000 / upem } // Go truncates toward zero like Java
	f.Asc1000, f.Desc1000 = conv(asc), conv(desc)
	f.XHeight1000, f.Cap1000 = conv(f.XHeight), conv(f.CapHeight)
}
