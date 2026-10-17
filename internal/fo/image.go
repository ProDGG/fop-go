package fo

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ProDGG/fop-go/internal/pdf"
	"github.com/ProDGG/fop-go/xmldom"
)

type imageData struct {
	src    string
	wPx    int
	hPx    int
	wPt    float64 // intrinsic size in points
	hPt    float64
	img    *pdf.Image
	hasSVG bool
	svg    *svgDoc
}

func (e *Engine) readSource(src string) ([]byte, error) {
	src = unwrapURL(src)
	switch {
	case strings.HasPrefix(src, "data:"):
		comma := strings.IndexByte(src, ',')
		if comma < 0 {
			return nil, fmt.Errorf("malformed data URI")
		}
		meta, payload := src[5:comma], src[comma+1:]
		if strings.HasSuffix(meta, ";base64") {
			return base64.StdEncoding.DecodeString(payload)
		}
		s, err := url.PathUnescape(payload)
		return []byte(s), err
	case strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://"):
		if !e.opts.AllowRemote {
			return nil, fmt.Errorf("remote images are disabled (enable AllowRemote)")
		}
		cl := &http.Client{Timeout: 30 * time.Second}
		resp, err := cl.Get(src)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("HTTP %s", resp.Status)
		}
		return io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	}
	p := strings.TrimPrefix(src, "file://")
	if u, err := url.PathUnescape(p); err == nil && strings.HasPrefix(src, "file:") {
		p = u
	}
	cands := []string{p}
	if !filepath.IsAbs(p) && e.opts.BaseDir != "" {
		cands = []string{filepath.Join(e.opts.BaseDir, p), p}
	}
	var lastErr error
	for _, c := range cands {
		data, err := os.ReadFile(c)
		if err == nil {
			return data, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func (e *Engine) loadImage(src string) (*imageData, error) {
	key := src
	if len(key) > 200 {
		key = key[:200] + fmt.Sprint(len(src))
	}
	if im, ok := e.images[key]; ok {
		if im == nil {
			return nil, fmt.Errorf("image previously failed")
		}
		return im, nil
	}
	data, err := e.readSource(src)
	if err != nil {
		e.images[key] = nil
		return nil, err
	}
	var im *imageData
	if t := bytes.TrimSpace(data); len(t) > 0 && t[0] == '<' {
		im, err = e.decodeSVG(data)
	} else {
		im, err = decodeImage(data)
	}
	if err != nil {
		e.images[key] = nil
		return nil, err
	}
	im.src = key
	e.images[key] = im
	return im, nil
}

func (e *Engine) decodeSVG(data []byte) (*imageData, error) {
	doc, err := xmldom.Parse(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	for _, c := range doc.Children {
		if c.Type == xmldom.ElementNode && c.Name == "svg" {
			return e.parseSVG(c)
		}
	}
	return nil, fmt.Errorf("not an SVG document")
}

func decodeImage(data []byte) (*imageData, error) {
	switch {
	case bytes.HasPrefix(data, []byte{0xFF, 0xD8}):
		return decodeJPEG(data)
	case bytes.HasPrefix(data, []byte("\x89PNG")):
		return decodePNG(data)
	case bytes.HasPrefix(data, []byte("GIF8")):
		g, err := gif.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		return fromImage(g, 72, 72)
	}
	t := bytes.TrimSpace(data)
	if len(t) > 0 && t[0] == '<' {
		return nil, fmt.Errorf("SVG images are not supported")
	}
	return nil, fmt.Errorf("unsupported image format")
}

func decodeJPEG(data []byte) (*imageData, error) {
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	im := &pdf.Image{Width: cfg.Width, Height: cfg.Height, BPC: 8, Filter: "DCTDecode", Data: data}
	switch cfg.ColorModel {
	case color.GrayModel:
		im.ColorSpace = "DeviceGray"
	case color.CMYKModel:
		im.ColorSpace = "DeviceCMYK"
		if bytes.Contains(data[:min(len(data), 4096)], []byte("Adobe")) {
			im.Decode = []float64{1, 0, 1, 0, 1, 0, 1, 0}
		}
	default:
		im.ColorSpace = "DeviceRGB"
	}
	dx, dy := 72.0, 72.0
	// JFIF density
	if i := bytes.Index(data[:min(len(data), 64)], []byte("JFIF\x00")); i >= 0 && i+12 <= len(data) {
		units := data[i+7]
		xd := float64(int(data[i+8])<<8 | int(data[i+9]))
		yd := float64(int(data[i+10])<<8 | int(data[i+11]))
		if xd > 0 && yd > 0 {
			switch units {
			case 1:
				dx, dy = xd, yd
			case 2:
				dx, dy = xd*2.54, yd*2.54
			}
		}
	}
	return &imageData{wPx: cfg.Width, hPx: cfg.Height, wPt: float64(cfg.Width) * 72 / dx, hPt: float64(cfg.Height) * 72 / dy, img: im}, nil
}

func decodePNG(data []byte) (*imageData, error) {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	dx, dy := 72.0, 72.0
	// pHYs chunk
	if i := bytes.Index(data, []byte("pHYs")); i >= 0 && i+13 <= len(data) {
		px := float64(uint32(data[i+4])<<24 | uint32(data[i+5])<<16 | uint32(data[i+6])<<8 | uint32(data[i+7]))
		py := float64(uint32(data[i+8])<<24 | uint32(data[i+9])<<16 | uint32(data[i+10])<<8 | uint32(data[i+11]))
		if data[i+12] == 1 && px > 0 && py > 0 {
			dx, dy = px*0.0254, py*0.0254
		}
	}
	return fromImage(img, dx, dy)
}

func fromImage(img image.Image, dx, dy float64) (*imageData, error) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return nil, fmt.Errorf("empty image")
	}
	im := &pdf.Image{Width: w, Height: h, BPC: 8}
	gray := false
	switch img.(type) {
	case *image.Gray, *image.Gray16:
		gray = true
	}
	var alpha []byte
	hasAlpha := false
	if gray {
		im.ColorSpace = "DeviceGray"
		im.Data = make([]byte, 0, w*h)
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				g := color.GrayModel.Convert(img.At(x, y)).(color.Gray)
				im.Data = append(im.Data, g.Y)
			}
		}
	} else {
		im.ColorSpace = "DeviceRGB"
		im.Data = make([]byte, 0, w*h*3)
		alpha = make([]byte, 0, w*h)
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				var c color.NRGBA
				switch t := img.(type) {
				case *image.NRGBA:
					c = t.NRGBAAt(x, y)
				default:
					c = color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
				}
				im.Data = append(im.Data, c.R, c.G, c.B)
				alpha = append(alpha, c.A)
				if c.A != 255 {
					hasAlpha = true
				}
			}
		}
	}
	if hasAlpha {
		im.Alpha = alpha
	}
	return &imageData{wPx: w, hPx: h, wPt: float64(w) * 72 / dx, hPt: float64(h) * 72 / dy, img: im}, nil
}
