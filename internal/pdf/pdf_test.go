package pdf

import (
	"bytes"
	"os"
	"testing"

	"github.com/ProDGG/fop-go/internal/font"
)

func TestWrite(t *testing.T) {
	m := font.NewManager()
	m.DisableSystemFonts()
	ft := m.Select([]string{"serif"}, 400, false).MustFont()
	d := NewDocument()
	d.Info.Title = "Тест"
	pf := d.UseFont(ft)
	p := d.NewPage(595, 842)
	p.FillRect(50, 700, 200, 40, 0.9, 0.9, 1)
	p.FillRGB(0, 0, 0)
	var gl []Glyph
	for _, r := range "Привет, мир! Hello" {
		g := ft.GlyphIndex(r)
		gl = append(gl, Glyph{GID: g, Rune: r})
	}
	p.Text(pf, 18, 60, 715, gl)
	p.AddURILink(60, 715, 100, 20, "https://example.com")
	d.Outlines = []*Outline{{Title: "Раздел", Page: 0, Y: 800}}
	var buf bytes.Buffer
	if err := d.Write(&buf); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(buf.Bytes(), []byte("%PDF-1.7")) {
		t.Fatal("bad header")
	}
	if out := os.Getenv("PDF_OUT"); out != "" {
		os.WriteFile(out, buf.Bytes(), 0o644)
	}
}
