package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
)

func render(pdf, dir, tag string, dpi int) ([]string, error) {
	if out, err := exec.Command("pdftoppm", "-r", strconv.Itoa(dpi), "-png", pdf, filepath.Join(dir, tag)).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("pdftoppm: %v: %s", err, out)
	}
	files, _ := filepath.Glob(filepath.Join(dir, tag+"-*.png"))
	sort.Strings(files)
	return files, nil
}

func load(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return png.Decode(f)
}

func gray(c color.Color) int {
	r, g, b, _ := c.RGBA()
	return int((299*r + 587*g + 114*b) / 1000 >> 8)
}

func cmdImg(a, b string, rest []string) error {
	dpi := 72
	if len(rest) > 0 {
		if n, err := strconv.Atoi(rest[0]); err == nil {
			dpi = n
		}
	}
	out := ""
	if len(rest) > 1 {
		out = rest[1]
	}
	dir, err := os.MkdirTemp("", "pdfcmp")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	pa, err := render(a, dir, "a", dpi)
	if err != nil {
		return err
	}
	pb, err := render(b, dir, "b", dpi)
	if err != nil {
		return err
	}
	fmt.Printf("pages: mine=%d ref=%d\n", len(pa), len(pb))
	for i := 0; i < len(pa) && i < len(pb); i++ {
		ia, err := load(pa[i])
		if err != nil {
			return err
		}
		ib, err := load(pb[i])
		if err != nil {
			return err
		}
		ra, rb := ia.Bounds(), ib.Bounds()
		if ra.Dx() != rb.Dx() || ra.Dy() != rb.Dy() {
			fmt.Printf("p%d: size differs %v %v\n", i+1, ra.Size(), rb.Size())
			continue
		}
		w, h := ra.Dx(), ra.Dy()
		diff := image.NewGray(image.Rect(0, 0, w, h))
		n := 0
		minX, minY, maxX, maxY := w, h, -1, -1
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				d := gray(ia.At(ra.Min.X+x, ra.Min.Y+y)) - gray(ib.At(rb.Min.X+x, rb.Min.Y+y))
				if d < 0 {
					d = -d
				}
				if d > 64 {
					diff.SetGray(x, y, color.Gray{255})
					n++
					minX, minY = min(minX, x), min(minY, y)
					maxX, maxY = max(maxX, x+1), max(maxY, y+1)
				}
			}
		}
		bbox := "none"
		if n > 0 {
			bbox = fmt.Sprintf("(%d,%d,%d,%d)", minX, minY, maxX, maxY)
		}
		fmt.Printf("p%d: %d px differ (%.3f%%) bbox=%s\n", i+1, n, 100*float64(n)/float64(w*h), bbox)
		if out != "" && n > 0 {
			sheet := image.NewRGBA(image.Rect(0, 0, w*3, h))
			for y := 0; y < h; y++ {
				for x := 0; x < w; x++ {
					sheet.Set(x, y, ia.At(ra.Min.X+x, ra.Min.Y+y))
					sheet.Set(w+x, y, ib.At(rb.Min.X+x, rb.Min.Y+y))
					if diff.GrayAt(x, y).Y != 0 {
						sheet.Set(2*w+x, y, color.RGBA{255, 0, 0, 255})
					} else {
						sheet.Set(2*w+x, y, color.White)
					}
				}
			}
			f, err := os.Create(fmt.Sprintf("%s-%d.png", out, i+1))
			if err != nil {
				return err
			}
			png.Encode(f, sheet)
			f.Close()
		}
	}
	return nil
}
