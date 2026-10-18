package pdf

import (
	"bytes"
	"fmt"
	"math"
	"math/rand"
	"os"
	"strconv"
	"strings"
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

func refNum(f float64) string {
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

func refITextNum(d float64) string {
	if math.Abs(d) < 0.000015 {
		return "0"
	}
	neg := false
	if d < 0 {
		neg = true
		d = -d
	}
	var sb strings.Builder
	if neg {
		sb.WriteByte('-')
	}
	switch {
	case d < 1.0:
		d += 0.000005
		if d >= 1 {
			sb.WriteByte('1')
			return sb.String()
		}
		v := int(d * 100000)
		sb.WriteString("0.")
		sb.WriteString(strings.TrimRight(fmt.Sprintf("%05d", v), "0"))
	case d <= 32767:
		d += 0.005
		v := int(d * 100)
		sb.WriteString(strconv.Itoa(v / 100))
		if f := v % 100; f != 0 {
			if f%10 == 0 {
				fmt.Fprintf(&sb, ".%d", f/10)
			} else {
				fmt.Fprintf(&sb, ".%02d", f)
			}
		}
	default:
		d += 0.5
		sb.WriteString(strconv.FormatInt(int64(d), 10))
	}
	return sb.String()
}

func TestNumFormatsMatchReference(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	bad := 0
	for i := 0; i < 400000; i++ {
		var f float64
		switch i % 4 {
		case 0:
			f = (rng.Float64() - 0.5) * 2000
		case 1:
			f = float64(rng.Intn(2000000)-1000000) / 1000
		case 2:
			f = rng.Float64()
		default:
			f = float64(float32((rng.Float64() - 0.5) * 900))
		}
		if got, want := Num(f), refNum(f); got != want {
			bad++
			if bad < 5 {
				t.Errorf("Num(%v) = %s, want %s", f, got, want)
			}
		}
		if got, want := itextNum(f), refITextNum(f); got != want {
			t.Errorf("itextNum(%v) = %s, want %s", f, got, want)
		}
	}
	if bad > 20 { // only exact decimal ties may round differently
		t.Errorf("%d Num mismatches", bad)
	}
}
