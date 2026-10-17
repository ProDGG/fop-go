package fo

import (
	"github.com/ProDGG/fop-go/internal/font"
	"github.com/ProDGG/fop-go/internal/pdf"
)

// SVGDrawing is a parsed SVG document that can be painted into another
// renderer's PDF page (used for inline images by the HTML engine).
type SVGDrawing struct {
	// W and H are the intrinsic size in CSS pixels.
	W, H float64
	e    *Engine
	doc  *svgDoc
}

// ParseSVGDrawing parses SVG data with the same renderer that serves
// fo:instream-foreign-object.
func ParseSVGDrawing(data []byte, fonts *font.Manager) (*SVGDrawing, error) {
	e := newTestEngine()
	if fonts != nil {
		e.fonts = fonts
		e.opts.Fonts = fonts
	}
	im, err := e.decodeSVG(data)
	if err != nil {
		return nil, err
	}
	return &SVGDrawing{W: im.svg.w, H: im.svg.h, e: e, doc: im.svg}, nil
}

// Draw paints the drawing into the w x h box whose lower-left corner is
// (x, y) in PDF user space.
func (s *SVGDrawing) Draw(pg *pdf.Page, x, y, w, h float64) {
	r := &renderCtx{e: s.e, doc: nil, pg: pg, pageH: pg.Height, pageW: pg.Width}
	r.drawSVG(s.doc, x, pg.Height-y-h, w, h)
}
