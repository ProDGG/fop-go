package fo

import (
	"math"

	"github.com/ProDGG/fop-go/internal/font"
	"github.com/ProDGG/fop-go/internal/pdf"
)

// textRun is a sequence of glyphs in one font.
type textRun struct {
	ft     *font.Font
	glyphs []pdf.Glyph
	width  float64 // points
	wm     int     // width in millipoints (FOP's integer arithmetic)
}

// charMpt returns the width of a glyph in millipoints computed like Apache FOP:
// (font size in mpt) * (width in 1/1000 em) / 1000 with integer arithmetic.
func charMpt(ft *font.Font, gid uint16, sizeMpt int) int {
	w1000 := ft.Advance(gid) * 1000 / ft.UnitsPerEm // FOP truncates (OpenFont.convertTTFUnit2PDFUnit)
	return sizeMpt * w1000 / 1000
}

func mpt(pt float64) int { return int(math.Round(pt * 1000)) }

func (e *Engine) fallbackFont(style *textStyle, r rune) *font.Font {
	if f := e.fonts.FallbackFor(style.face, r); f != nil {
		return f.MustFont()
	}
	return nil
}

func isZeroWidth(r rune) bool {
	switch r {
	case 0x200B, 0x200C, 0x200D, 0x2060, 0xFEFF, 0xAD, 0x200E, 0x200F:
		return true
	}
	return r < 0x20 && r != '\t'
}

// measure converts text into runs (splitting where a fallback font is needed).
func (e *Engine) measure(style *textStyle, s string) []textRun {
	var runs []textRun
	var cur *textRun
	ft := style.ft
	for _, r := range s {
		if isZeroWidth(r) {
			continue
		}
		lookup := r
		if r == 0xA0 || r == '\t' || r == 0x2007 || r == 0x202F || r == 0x2002 || r == 0x2003 || r == 0x2009 {
			lookup = ' '
		}
		use := ft
		gid, ok := ft.Runes()[lookup]
		if !ok {
			if fb := e.fallbackFont(style, lookup); fb != nil {
				use = fb
				gid = fb.Runes()[lookup]
			} else {
				gid = 0
			}
		}
		if cur == nil || cur.ft != use {
			runs = append(runs, textRun{ft: use})
			cur = &runs[len(runs)-1]
		}
		wm := charMpt(use, gid, mpt(style.size))
		extra := style.ls
		if r == 0xA0 {
			extra += style.ws
		}
		// pair kerning between consecutive glyphs of the same font (GPOS 'kern')
		if n := len(cur.glyphs); n > 0 && e.kerning {
			if k := use.Kern(cur.glyphs[n-1].GID, gid); k != 0 {
				adj := mpt(style.size) * (k * 1000 / use.UnitsPerEm) / 1000
				cur.glyphs[n-1].Extra += float64(adj) / 1000
				cur.wm += adj
			}
		}
		cur.glyphs = append(cur.glyphs, pdf.Glyph{GID: gid, Rune: r, Extra: extra})
		cur.wm += wm + mpt(extra)
		cur.width = float64(cur.wm) / 1000
		// runs slice may be reallocated on append; refresh pointer
		cur = &runs[len(runs)-1]
	}
	return runs
}

func runsWidth(runs []textRun) float64 {
	return float64(runsMpt(runs)) / 1000
}

func runsMpt(runs []textRun) int {
	w := 0
	for _, r := range runs {
		w += r.wm
	}
	return w
}

// spaceWidth returns the advance of a space in the style's font.
func (e *Engine) spaceWidth(style *textStyle) float64 {
	return float64(e.spaceMpt(style)) / 1000
}

// spaceMpt is the width of a space in millipoints.
func (e *Engine) spaceMpt(style *textStyle) int {
	return charMpt(style.ft, style.ft.GlyphIndex(' '), mpt(style.size))
}

// ascDesc returns the font's ascender and descender (positive) at the style's
// size in points, using FOP's integer millipoint arithmetic.
func (st *textStyle) ascDesc() (float64, float64) {
	a, d := st.ascDescMpt()
	return float64(a) / 1000, float64(d) / 1000
}

func (st *textStyle) ascDescMpt() (int, int) {
	sz := mpt(st.size)
	return sz * st.ft.Asc1000 / 1000, sz * -st.ft.Desc1000 / 1000
}

// xHeightMpt is the font's x-height in millipoints.
func (st *textStyle) xHeightMpt() int { return mpt(st.size) * st.ft.XHeight1000 / 1000 }
