package xhr

import (
	"github.com/ProDGG/fop-go/internal/fo"
	"github.com/ProDGG/fop-go/internal/pdf"
)

func init() {
	SVGLoader = func(data []byte) (*SVGImage, error) {
		d, err := fo.ParseSVGDrawing(data, nil)
		if err != nil {
			return nil, err
		}
		return &SVGImage{W: d.W, H: d.H, Draw: func(p *pdf.Page, x, y, w, h float64) { d.Draw(p, x, y, w, h) }}, nil
	}
}
