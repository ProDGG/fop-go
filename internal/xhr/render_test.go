package xhr

import (
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ProDGG/fop-go/internal/pdfscan"
	"github.com/ProDGG/fop-go/xmldom"
)

type textPos struct{ x, y, size float64 }

// pageTexts extracts the text origins of every page content stream.
func pageTexts(t *testing.T, pdf []byte) [][]textPos {
	var pages [][]textPos
	for _, p := range pdfscan.Pages(pdf) {
		var ops []textPos
		for _, o := range p {
			ops = append(ops, textPos{o.X, o.Y, o.Size})
		}
		pages = append(pages, ops)
	}
	return pages
}

func loadGolden(t *testing.T, path string) [][]textPos {
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var pages [][]textPos
	for _, ln := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if strings.HasPrefix(ln, "page ") {
			pages = append(pages, nil)
			continue
		}
		f := strings.Fields(ln)
		x, _ := strconv.ParseFloat(f[0], 64)
		y, _ := strconv.ParseFloat(f[1], 64)
		sz, _ := strconv.ParseFloat(f[2], 64)
		pages[len(pages)-1] = append(pages[len(pages)-1], textPos{x, y, sz})
	}
	return pages
}

// TestAgainstFlyingSaucer renders synthetic fixtures and compares every text
// origin with the output of Flying Saucer 9.1.22 + iText 5 for the same HTML
// (goldens were produced with the reference implementation).
func TestAgainstFlyingSaucer(t *testing.T) {
	for _, name := range []string{"abs1", "abs2", "mix", "hdr", "fl"} {
		t.Run(name, func(t *testing.T) {
			dir := "testdata"
			data, err := os.ReadFile(filepath.Join(dir, name+".html"))
			if err != nil {
				t.Fatal(err)
			}
			doc, err := xmldom.ParseBytes(data)
			if err != nil {
				t.Fatal(err)
			}
			out, err := Render(doc, Options{ResolveResource: func(uri string) ([]byte, error) {
				return os.ReadFile(filepath.Join(dir, uri))
			}})
			if err != nil {
				t.Fatal(err)
			}
			got := pageTexts(t, out)
			want := loadGolden(t, filepath.Join(dir, name+".golden"))
			if len(got) != len(want) {
				t.Fatalf("pages: got %d want %d", len(got), len(want))
			}
			for p := range want {
				if len(got[p]) != len(want[p]) {
					t.Errorf("page %d: %d text ops, want %d", p+1, len(got[p]), len(want[p]))
					continue
				}
				for i, w := range want[p] {
					g := got[p][i]
					if math.Abs(g.x-w.x) > 0.03 || math.Abs(g.y-w.y) > 0.03 || math.Abs(g.size-w.size) > 0.02 {
						t.Errorf("page %d op %d: got (%.2f, %.2f, %.2f) want (%.2f, %.2f, %.2f)", p+1, i, g.x, g.y, g.size, w.x, w.y, w.size)
						break
					}
				}
			}
		})
	}
}

// BenchmarkRender measures a multi-page synthetic document (table + positioned boxes).
func BenchmarkRender(b *testing.B) {
	data, err := os.ReadFile("testdata/abs2.html")
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		doc, err := xmldom.ParseBytes(data)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := Render(doc, Options{}); err != nil {
			b.Fatal(err)
		}
	}
}
