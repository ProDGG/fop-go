package xhr

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"net/url"
	"strings"

	"github.com/ProDGG/fop-go/internal/pdf"
)

// Image is a decoded picture scaled to output resolution (dots).
type Image struct {
	pdfImg *pdf.Image
	svg    *SVGImage
	key    string
	pxW    int
	pxH    int
	w, h   int // current size in dots
}

func (i *Image) width() int  { return i.w }
func (i *Image) height() int { return i.h }

func (i *Image) clone() *Image { c := *i; return &c }

// scale mirrors ITextFSImage.scale.
func (i *Image) scale(width, height int) {
	if width > 0 || height > 0 {
		cw, ch := i.w, i.h
		tw, th := width, height
		if tw == -1 {
			tw = int(float64(cw) * (float64(th) / float64(ch)))
		}
		if th == -1 {
			th = int(float64(ch) * (float64(tw) / float64(cw)))
		}
		if cw != tw || ch != th {
			i.w, i.h = tw, th
		}
	}
}

// SVGImage is a vector image drawn by the SVG renderer (set by the host).
type SVGImage struct {
	W, H float64 // intrinsic size in CSS pixels
	Draw func(p *pdf.Page, x, y, w, h float64)
}

// SVGLoader parses SVG data; installed by the package that owns the SVG renderer.
var SVGLoader func(data []byte) (*SVGImage, error)

// Replaced is a replaced element (image) attached to a box.
type Replaced struct {
	img  *Image
	x, y int
}

func (r *Replaced) intrinsicWidth() int  { return r.img.width() }
func (r *Replaced) intrinsicHeight() int { return r.img.height() }
func (r *Replaced) hasBaseline() bool    { return false }
func (r *Replaced) baseline() int        { return 0 }

// ImageCache loads and caches images.
type ImageCache struct {
	cache   map[string]*Image
	Resolve func(uri string) ([]byte, error)
	doc     *pdf.Document
}

func NewImageCache(resolve func(string) ([]byte, error)) *ImageCache {
	return &ImageCache{cache: map[string]*Image{}, Resolve: resolve}
}

func (c *ImageCache) get(uri string) *Image {
	if im, ok := c.cache[uri]; ok {
		if im == nil {
			return nil
		}
		return im.clone()
	}
	im := c.load(uri)
	c.cache[uri] = im
	if im == nil {
		return nil
	}
	return im.clone()
}

func (c *ImageCache) load(uri string) *Image {
	var data []byte
	if strings.HasPrefix(uri, "data:") {
		i := strings.Index(uri, ",")
		if i < 0 {
			return nil
		}
		meta, payload := uri[5:i], uri[i+1:]
		if strings.HasSuffix(meta, ";base64") {
			d, err := javaBase64(payload)
			if err != nil {
				// Flying Saucer decodes with java.util.Base64.getDecoder(), which rejects
				// line breaks; vector images are handled by a separate (tolerant) loader
				lenient := strings.Map(func(r rune) rune {
					if r == ' ' || r == '\n' || r == '\r' || r == '\t' {
						return -1
					}
					return r
				}, payload)
				d, err = javaBase64(lenient)
				if err != nil || !bytes.HasPrefix(bytes.TrimSpace(d), []byte("<")) {
					return nil
				}
			}
			data = d
		} else {
			s, err := url.PathUnescape(payload)
			if err != nil {
				s = payload
			}
			data = []byte(s)
		}
	} else if c.Resolve != nil {
		d, err := c.Resolve(uri)
		if err != nil {
			return nil
		}
		data = d
	} else {
		return nil
	}
	im, err := decodeImage(data)
	if err != nil {
		return nil
	}
	im.key = uri
	return im
}

func decodeImage(data []byte) (*Image, error) {
	t := bytes.TrimSpace(data)
	if len(t) > 0 && t[0] == '<' {
		if SVGLoader == nil {
			return nil, fmt.Errorf("svg unsupported")
		}
		s, err := SVGLoader(data)
		if err != nil {
			return nil, err
		}
		w := int(s.W * 20)
		h := int(s.H * 20)
		return &Image{svg: s, pxW: int(s.W), pxH: int(s.H), w: w, h: h}, nil
	}
	switch {
	case bytes.HasPrefix(data, []byte{0xFF, 0xD8}):
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
		return &Image{pdfImg: im, pxW: cfg.Width, pxH: cfg.Height, w: cfg.Width * 20, h: cfg.Height * 20}, nil
	case bytes.HasPrefix(data, []byte("\x89PNG")):
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		return fromImage(img)
	case bytes.HasPrefix(data, []byte("GIF8")):
		img, err := gif.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		return fromImage(img)
	}
	return nil, fmt.Errorf("unsupported image format")
}

func fromImage(img image.Image) (*Image, error) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return nil, fmt.Errorf("empty image")
	}
	im := &pdf.Image{Width: w, Height: h, BPC: 8, ColorSpace: "DeviceRGB"}
	im.Data = make([]byte, 0, w*h*3)
	alpha := make([]byte, 0, w*h)
	hasAlpha := false
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			im.Data = append(im.Data, c.R, c.G, c.B)
			alpha = append(alpha, c.A)
			if c.A != 255 {
				hasAlpha = true
			}
		}
	}
	if hasAlpha {
		im.Alpha = alpha
	}
	return &Image{pdfImg: im, pxW: w, pxH: h, w: w * 20, h: h * 20}, nil
}

// createReplacedElement mirrors ITextReplacedElementFactory (images only).
func (d *Doc) createReplacedElement(lc *LayoutContext, b *Box, cssWidth, cssHeight int) *Replaced {
	e := b.Elem
	if e == nil || b.anonymous {
		return nil
	}
	if e.Name == "img" {
		src := e.Attr("src")
		if src != "" && d.images != nil {
			if im := d.images.get(src); im != nil {
				if cssWidth != -1 || cssHeight != -1 {
					im.scale(cssWidth, cssHeight)
				}
				return &Replaced{img: im}
			}
		}
	}
	return nil
}

// javaBase64 mirrors java.util.Base64.getDecoder().decode: only the standard
// alphabet, no whitespace, optional padding.
func javaBase64(s string) ([]byte, error) {
	pad := 0
	for pad < len(s) && s[len(s)-1-pad] == '=' {
		pad++
	}
	if pad > 2 || (pad > 0 && len(s)%4 != 0) {
		return nil, errBase64
	}
	body := s[:len(s)-pad]
	for i := 0; i < len(body); i++ {
		c := body[i]
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '+' || c == '/') {
			return nil, errBase64
		}
	}
	if len(body)%4 == 1 {
		return nil, errBase64
	}
	return base64.RawStdEncoding.DecodeString(body)
}

var errBase64 = errors.New("illegal base64 character")
