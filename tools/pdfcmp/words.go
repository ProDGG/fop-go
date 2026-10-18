package main

import (
	"fmt"
	"html"
	"math"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/ProDGG/fop-go/internal/pdfscan"
)

type word struct {
	text           string
	x0, y0, x1, y1 float64
}

var wordRe = regexp.MustCompile(`\s*<word xMin="([\d.]+)" yMin="([\d.]+)" xMax="([\d.]+)" yMax="([\d.]+)">(.*)</word>`)

// words returns the words of every page as reported by pdftotext -bbox.
func words(path string) ([][]word, error) {
	out, err := exec.Command("pdftotext", "-bbox", path, "-").Output()
	if err != nil {
		return nil, fmt.Errorf("pdftotext %s: %w", path, err)
	}
	var pages [][]word
	cur := -1
	for _, ln := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), "<page") {
			pages = append(pages, nil)
			cur++
		}
		if m := wordRe.FindStringSubmatch(ln); m != nil && cur >= 0 {
			f := func(s string) float64 { v, _ := strconv.ParseFloat(s, 64); return v }
			pages[cur] = append(pages[cur], word{html.UnescapeString(m[5]), f(m[1]), f(m[2]), f(m[3]), f(m[4])})
		}
	}
	return pages, nil
}

// lines groups words into lines (same baseline within tol).
func lines(ws []word, tol float64) [][]word {
	ws = append([]word(nil), ws...)
	sort.SliceStable(ws, func(i, j int) bool {
		a, b := math.Round(ws[i].y0/tol), math.Round(ws[j].y0/tol)
		if a != b {
			return a < b
		}
		return ws[i].x0 < ws[j].x0
	})
	var out [][]word
	var cur []word
	cy := math.NaN()
	for _, w := range ws {
		if math.IsNaN(cy) || math.Abs(w.y0-cy) > tol {
			if len(cur) > 0 {
				out = append(out, cur)
			}
			cur, cy = nil, w.y0
		}
		cur = append(cur, w)
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}

func lineText(l []word) string {
	s := make([]string, len(l))
	for i, w := range l {
		s[i] = w.text
	}
	return strings.Join(s, " ")
}

func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

func cmdWords(a, b string, rest []string) error {
	wa, err := words(a)
	if err != nil {
		return err
	}
	wb, err := words(b)
	if err != nil {
		return err
	}
	fmt.Printf("pages: mine=%d ref=%d\n", len(wa), len(wb))
	total, bad := 0, 0
	maxdx, maxdy := 0.0, 0.0
	for p := 0; p < len(wa) && p < len(wb); p++ {
		la, lb := lines(wa[p], 2), lines(wb[p], 2)
		for i := 0; i < len(la) || i < len(lb); i++ {
			x, y := "<none>", "<none>"
			if i < len(la) {
				x = lineText(la[i])
			}
			if i < len(lb) {
				y = lineText(lb[i])
			}
			total++
			if x != y {
				bad++
				if bad <= 12 {
					fmt.Printf("p%d line %d differs:\n   mine: %s\n   ref : %s\n", p+1, i+1, trunc(x, 110), trunc(y, 110))
				}
				continue
			}
			for k := range la[i] {
				maxdx = math.Max(maxdx, math.Abs(la[i][k].x0-lb[i][k].x0))
				maxdy = math.Max(maxdy, math.Abs(la[i][k].y0-lb[i][k].y0))
			}
		}
	}
	if ba, err := pdfscan.File(a); err == nil {
		if bb, err := pdfscan.File(b); err == nil {
			bdiff, bmis := 0.0, 0
			for p := 0; p < len(ba) && p < len(bb); p++ {
				ya, yb := distinctBaselines(ba[p]), distinctBaselines(bb[p])
				if len(ya) != len(yb) {
					bmis++
					continue
				}
				for i := range ya {
					bdiff = math.Max(bdiff, math.Abs(ya[i]-yb[i]))
				}
			}
			fmt.Printf("baselines: max |dy|=%.2fpt, pages with different line count=%d\n", bdiff, bmis)
		}
	}
	fmt.Printf("lines compared: %d, differing text: %d; max |dx|=%.2fpt max |dy|=%.2fpt (identical lines only)\n", total, bad, maxdx, maxdy)
	return nil
}

func pageArg(rest []string) int {
	if len(rest) > 0 {
		if n, err := strconv.Atoi(rest[0]); err == nil && n > 0 {
			return n - 1
		}
	}
	return 0
}

func cmdLines(a, b string, rest []string) error {
	wa, err := words(a)
	if err != nil {
		return err
	}
	wb, err := words(b)
	if err != nil {
		return err
	}
	p := pageArg(rest)
	if p >= len(wa) || p >= len(wb) {
		return fmt.Errorf("page %d does not exist in both files", p+1)
	}
	la, lb := lines(wa[p], 2), lines(wb[p], 2)
	for i := 0; i < len(la) || i < len(lb); i++ {
		var ya, yb, ta, tb string = "-", "-", "", ""
		if i < len(la) {
			ya, ta = fmt.Sprintf("%.1f", la[i][0].y0), trunc(lineText(la[i]), 40)
		}
		if i < len(lb) {
			yb, tb = fmt.Sprintf("%.1f", lb[i][0].y0), trunc(lineText(lb[i]), 40)
		}
		fmt.Printf("%d %s %s | %s | %s\n", i+1, ya, yb, ta, tb)
	}
	return nil
}

// distinctBaselines lists the distinct baselines (rounded to 0.01pt) of a page, top first.
func distinctBaselines(ts []pdfscan.Text) []float64 {
	m := map[float64]bool{}
	for _, t := range ts {
		m[math.Round(t.Y*100)/100] = true
	}
	var out []float64
	for y := range m {
		out = append(out, y)
	}
	sort.Sort(sort.Reverse(sort.Float64Slice(out)))
	return out
}
