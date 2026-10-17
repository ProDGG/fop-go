package fo

import (
	"github.com/ProDGG/fop-go/internal/font"
	"github.com/ProDGG/fop-go/internal/pdf"
)

// textRun is a sequence of glyphs in one font.
type textRun struct {
	ft     *font.Font
	glyphs []pdf.Glyph
	width  float64
}

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
		adv := float64(use.Advance(gid)) * style.size / float64(use.UnitsPerEm)
		extra := style.ls
		if r == 0xA0 {
			extra += style.ws
		}
		cur.glyphs = append(cur.glyphs, pdf.Glyph{GID: gid, Rune: r, Extra: extra})
		cur.width += adv + extra
		_ = cur
		// runs slice may be reallocated on append; refresh pointer
		cur = &runs[len(runs)-1]
	}
	return runs
}

func runsWidth(runs []textRun) float64 {
	w := 0.0
	for _, r := range runs {
		w += r.width
	}
	return w
}

// spaceWidth returns the advance of a space in the style's font.
func (e *Engine) spaceWidth(style *textStyle) float64 {
	gid := style.ft.GlyphIndex(' ')
	return float64(style.ft.Advance(gid)) * style.size / float64(style.ft.UnitsPerEm)
}

// ascDesc returns the font's ascent and descent (positive) at the style's size.
func (st *textStyle) ascDesc() (float64, float64) {
	upm := float64(st.ft.UnitsPerEm)
	return float64(st.ft.Ascent) * st.size / upm, float64(-st.ft.Descent) * st.size / upm
}
