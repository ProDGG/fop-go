package font

import "testing"

func TestBundled(t *testing.T) {
	m := NewManager()
	m.DisableSystemFonts()
	f := m.Select([]string{"Helvetica"}, 400, false)
	ft, err := f.Font()
	if err != nil {
		t.Fatal(err)
	}
	if ft.Family != "Liberation Sans" {
		t.Errorf("family = %q", ft.Family)
	}
	g := ft.GlyphIndex('Ж')
	if g == 0 {
		t.Fatal("no Cyrillic glyph")
	}
	if w := ft.Advance(ft.GlyphIndex('A')); w != 1366 {
		t.Errorf("advance A = %d", w)
	}
	sub := ft.Subset([]uint16{g, ft.GlyphIndex('A')})
	if len(sub) > len(ft.Data)/4 {
		t.Errorf("subset too big: %d", len(sub))
	}
	if _, err := Parse(sub); err == nil {
		// subset has no cmap on purpose; Parse must complain
		t.Errorf("expected parse failure for subset without cmap")
	}
	b := m.Select([]string{"Times New Roman"}, 700, true)
	if bf := b.MustFont(); bf == nil || !bf.Bold || !bf.Italic || bf.Family != "Liberation Serif" {
		t.Errorf("bad bold italic serif: %+v", bf)
	}
}
