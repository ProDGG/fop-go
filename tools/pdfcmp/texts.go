package main

import (
	"fmt"
	"math"
	"sort"

	"github.com/ProDGG/fop-go/internal/pdfscan"
)

func cmdBaselines(a, b string, rest []string) error {
	pa, err := pdfscan.File(a)
	if err != nil {
		return err
	}
	pb, err := pdfscan.File(b)
	if err != nil {
		return err
	}
	p := pageArg(rest)
	if p >= len(pa) || p >= len(pb) {
		return fmt.Errorf("page %d does not exist in both files", p+1)
	}
	type key struct{ y, size float64 }
	set := func(ts []pdfscan.Text) []key {
		m := map[key]bool{}
		for _, t := range ts {
			m[key{math.Round(t.Y*100) / 100, t.Size}] = true
		}
		var out []key
		for k := range m {
			out = append(out, k)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].y > out[j].y })
		return out
	}
	ka, kb := set(pa[p]), set(pb[p])
	for i := 0; i < len(ka) || i < len(kb); i++ {
		d := ""
		sa, sb := "-", "-"
		if i < len(ka) {
			sa = fmt.Sprintf("%.2f/%g", ka[i].y, ka[i].size)
		}
		if i < len(kb) {
			sb = fmt.Sprintf("%.2f/%g", kb[i].y, kb[i].size)
		}
		if i < len(ka) && i < len(kb) {
			d = fmt.Sprintf("%+.2f", ka[i].y-kb[i].y)
		}
		fmt.Println(sa, sb, d)
	}
	return nil
}

// cmdTexts compares text origins in drawing order, skipping texts whose y is
// above page height - skipTop (headers/footers) when skipTop > 0.
func cmdTexts(a, b string, rest []string) error {
	pa, err := pdfscan.File(a)
	if err != nil {
		return err
	}
	pb, err := pdfscan.File(b)
	if err != nil {
		return err
	}
	skip := 0.0
	if len(rest) > 0 {
		fmt.Sscan(rest[0], &skip)
	}
	filter := func(ts []pdfscan.Text) []pdfscan.Text {
		if skip == 0 {
			return ts
		}
		var out []pdfscan.Text
		for _, t := range ts {
			if t.Y < 595.275-skip {
				out = append(out, t)
			}
		}
		return out
	}
	fmt.Printf("pages: mine=%d ref=%d\n", len(pa), len(pb))
	total, bad := 0, 0
	for p := 0; p < len(pa) && p < len(pb); p++ {
		ra, rb := filter(pa[p]), filter(pb[p])
		n := 0
		var first string
		for i := 0; i < len(ra) && i < len(rb); i++ {
			total++
			x, y := ra[i], rb[i]
			if math.Abs(x.X-y.X) > 0.03 || math.Abs(x.Y-y.Y) > 0.03 || math.Abs(x.Size-y.Size) > 0.02 {
				n++
				bad++
				if first == "" {
					first = fmt.Sprintf("op %d: (%.2f, %.2f, %g) vs (%.2f, %.2f, %g)", i, x.X, x.Y, x.Size, y.X, y.Y, y.Size)
				}
			}
		}
		if len(ra) != len(rb) || n > 0 {
			fmt.Printf("page %d ops %d %d bad %d %s\n", p+1, len(ra), len(rb), n, first)
		}
	}
	fmt.Printf("total %d bad %d\n", total, bad)
	return nil
}
