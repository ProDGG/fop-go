// Package fop is a pure-Go implementation of the Apache FOP pipeline:
// XML + XSLT stylesheet -> XSL-FO -> PDF.
//
//	err := fop.RenderFiles("data.xml", "template.xsl", "out.pdf", fop.Options{})
//
// The package has no dependencies outside the Go standard library. It ships
// with metric-compatible Liberation fonts (Helvetica/Arial, Times, Courier
// equivalents with Latin, Cyrillic and Greek coverage) and can load further
// TrueType fonts.
package fop

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ProDGG/fop-go/internal/fo"
	"github.com/ProDGG/fop-go/internal/font"
	"github.com/ProDGG/fop-go/internal/pdf"
	"github.com/ProDGG/fop-go/internal/xhr"
	"github.com/ProDGG/fop-go/xmldom"
	"github.com/ProDGG/fop-go/xslt"
)

// FontSpec registers an additional TrueType font.
type FontSpec struct {
	Family string // family name used in font-family
	Weight int    // 100..900; 0 = taken from the font file
	Italic bool
	Path   string // path of a .ttf file (or Data)
	Data   []byte
}

// Options configure a rendering run. The zero value is usable.
type Options struct {
	// BaseDir resolves relative image paths (default: directory of the XML
	// input for RenderFiles, the working directory otherwise).
	BaseDir string
	// XSLBase is the location of the stylesheet; xsl:include/xsl:import and
	// document() resolve relative to it.
	XSLBase string
	// Params are passed to the stylesheet as global xsl:param values.
	Params map[string]string
	// Fonts adds TrueType fonts. FontDirs are scanned for more.
	Fonts    []FontSpec
	FontDirs []string
	// NoSystemFonts disables scanning of system font directories.
	NoSystemFonts bool
	// SourceRoot, when set, makes the first element with that name the document
	// element of the XML input before the stylesheet runs (reports are often
	// wrapped in an envelope while their templates address "/Root/...").
	SourceRoot string
	// PathPlaceholder is replaced by the stylesheet's directory inside the
	// stylesheet text (e.g. "PATH_TO_FILE" in xsl:import hrefs). Default
	// "PATH_TO_FILE"; set to "-" to disable.
	PathPlaceholder string
	// Document metadata.
	Title, Author, Subject, Keywords, Creator, Producer string
	// NoCompress disables Flate compression of PDF streams.
	NoCompress bool
	// SourceResolution is the dpi used to convert px to pt (default 72).
	SourceResolution float64
	// AllowRemote permits http(s) image URLs.
	AllowRemote bool
	readFile    func(path string) ([]byte, error) // set for templates in an fs.FS
	// MaxPages aborts rendering with ErrPageLimit when the document needs more
	// pages: a guard against runaway layouts (e.g. a huge font size coming
	// from the data). 0 means DefaultMaxPages, a negative value no limit.
	MaxPages int
	// Warn receives non-fatal problems (missing fonts/images, unsupported
	// objects, xsl:message output). nil discards them.
	Warn func(string)
}

// DefaultMaxPages is the page limit used when Options.MaxPages is 0.
const DefaultMaxPages = 100000

// ErrPageLimit is returned (wrapped) when a document exceeds Options.MaxPages.
var ErrPageLimit = pdf.ErrPageLimit

func (o Options) maxPages() int {
	switch {
	case o.MaxPages == 0:
		return DefaultMaxPages
	case o.MaxPages < 0:
		return 0
	}
	return o.MaxPages
}

func (o Options) fontManager() (*font.Manager, error) {
	m := font.NewManager()
	m.Warn = o.Warn
	if o.NoSystemFonts {
		m.DisableSystemFonts()
	}
	for _, d := range o.FontDirs {
		m.AddDir(d)
	}
	for _, f := range o.Fonts {
		data := f.Data
		if data == nil {
			var err error
			data, err = os.ReadFile(f.Path)
			if err != nil {
				return nil, fmt.Errorf("fop: font %q: %w", f.Family, err)
			}
		}
		if err := m.AddFont(f.Family, f.Weight, f.Italic, data); err != nil {
			return nil, fmt.Errorf("fop: font %q: %w", f.Family, err)
		}
	}
	return m, nil
}

// Transform applies an XSLT stylesheet to an XML document and returns the
// result tree (normally an XSL-FO document).
func Transform(xmlDoc, xslDoc io.Reader, opts Options) (*xmldom.Node, error) {
	xslData, err := io.ReadAll(xslDoc)
	if err != nil {
		return nil, err
	}
	xmlData, err := io.ReadAll(xmlDoc)
	if err != nil {
		return nil, err
	}
	return transformBytes(xmlData, xslData, opts, "")
}

func transformBytes(xmlData, xslData []byte, opts Options, xmlPath string) (*xmldom.Node, error) {
	xslData = replacePathPlaceholder(xslData, opts)
	ss, err := xslt.CompileBytes(xslData, xslt.Options{BaseURI: xslBase(opts)})
	if err != nil {
		return nil, err
	}
	return runStylesheet(ss, xmlData, opts, xmlPath)
}

// runStylesheet applies a compiled stylesheet to the XML input.
func runStylesheet(ss *xslt.Stylesheet, xmlData []byte, opts Options, xmlPath string) (*xmldom.Node, error) {
	src, err := xmldom.ParseBytes(xmlData)
	if err != nil {
		return nil, fmt.Errorf("parsing XML input: %w", err)
	}
	if opts.SourceRoot != "" {
		src = promoteRoot(src, opts.SourceRoot)
	}
	params := map[string]interface{}{}
	for k, v := range opts.Params {
		params[k] = v
	}
	var msg func(string)
	if opts.Warn != nil {
		msg = func(s string) { opts.Warn("xsl:message: " + s) }
	}
	return ss.Transform(src, xslt.TransformOptions{Params: params, Messages: msg, SourceURI: xmlPath})
}

// promoteRoot returns a document whose element is the first element named name
// (searching breadth first); the input is returned when there is none.
func promoteRoot(doc *xmldom.Node, name string) *xmldom.Node {
	de := doc.DocumentElement()
	if de == nil || de.Name == name {
		return doc
	}
	queue := []*xmldom.Node{de}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for _, c := range n.Children {
			if c.Type != xmldom.ElementNode {
				continue
			}
			if c.Name == name {
				nd := xmldom.NewDocument()
				nd.AppendChild(c)
				return nd
			}
			queue = append(queue, c)
		}
	}
	return doc
}

func replacePathPlaceholder(xsl []byte, o Options) []byte {
	ph := o.PathPlaceholder
	if ph == "-" {
		return xsl
	}
	if ph == "" {
		ph = "PATH_TO_FILE"
	}
	if !bytes.Contains(xsl, []byte(ph)) {
		return xsl
	}
	// imports resolve against the stylesheet location, so "." is the template directory
	return bytes.ReplaceAll(xsl, []byte(ph), []byte("."))
}

func xslBase(o Options) string {
	if o.XSLBase != "" {
		return o.XSLBase
	}
	dir := o.BaseDir
	if dir == "" {
		dir = "."
	}
	return filepath.Join(dir, "stylesheet.xsl")
}

// SerializeFO writes the XSL-FO result tree as indented XML.
func SerializeFO(doc *xmldom.Node) []byte {
	var buf bytes.Buffer
	_ = xmldom.Serialize(&buf, doc, true)
	return buf.Bytes()
}

// RenderFODocument renders an already parsed XSL-FO tree.
func RenderFODocument(doc *xmldom.Node, w io.Writer, opts Options) error {
	if isHTMLDocument(doc) {
		return renderHTMLDocument(doc, w, opts)
	}
	tree, err := fo.BuildTree(doc)
	if err != nil {
		return err
	}
	fm, err := opts.fontManager()
	if err != nil {
		return err
	}
	res := opts.SourceResolution
	pxPt := 1.0
	if res > 0 {
		pxPt = 72 / res
	}
	pdfDoc, err := fo.Render(tree, fo.Options{
		BaseDir: opts.BaseDir, Fonts: fm, Warn: opts.Warn, PxPerPt: pxPt, Compress: !opts.NoCompress,
		Title: opts.Title, Author: opts.Author, Subject: opts.Subject, Keywords: opts.Keywords,
		Creator: opts.Creator, Producer: opts.Producer, AllowRemote: opts.AllowRemote, MaxPages: opts.maxPages(), ReadFile: opts.readFile,
	})
	if err != nil {
		return err
	}
	return pdfDoc.Write(w)
}

// RenderFO renders an XSL-FO document to PDF.
func RenderFO(foDoc io.Reader, w io.Writer, opts Options) error {
	doc, err := xmldom.Parse(foDoc)
	if err != nil {
		return fmt.Errorf("parsing XSL-FO: %w", err)
	}
	return RenderFODocument(doc, w, opts)
}

// Render runs the whole pipeline: XML + XSLT -> XSL-FO -> PDF.
func Render(xmlDoc, xslDoc io.Reader, w io.Writer, opts Options) error {
	res, err := Transform(xmlDoc, xslDoc, opts)
	if err != nil {
		return err
	}
	return RenderFODocument(res, w, opts)
}

// RenderFiles renders the files xmlPath and xslPath into pdfPath.
func RenderFiles(xmlPath, xslPath, pdfPath string, opts Options) error {
	xmlData, err := os.ReadFile(xmlPath)
	if err != nil {
		return err
	}
	xslData, err := os.ReadFile(xslPath)
	if err != nil {
		return err
	}
	if opts.BaseDir == "" {
		opts.BaseDir = filepath.Dir(xmlPath)
	}
	if opts.XSLBase == "" {
		opts.XSLBase = xslPath
	}
	res, err := transformBytes(xmlData, xslData, opts, xmlPath)
	if err != nil {
		return err
	}
	return renderToFile(res, pdfPath, opts)
}

// RenderFOFile renders an XSL-FO file into pdfPath.
func RenderFOFile(foPath, pdfPath string, opts Options) error {
	data, err := os.ReadFile(foPath)
	if err != nil {
		return err
	}
	if opts.BaseDir == "" {
		opts.BaseDir = filepath.Dir(foPath)
	}
	doc, err := xmldom.ParseBytes(data)
	if err != nil {
		return fmt.Errorf("parsing XSL-FO: %w", err)
	}
	return renderToFile(doc, pdfPath, opts)
}

func renderToFile(doc *xmldom.Node, pdfPath string, opts Options) error {
	var buf bytes.Buffer
	if err := RenderFODocument(doc, &buf, opts); err != nil {
		return err
	}
	if err := os.WriteFile(pdfPath, buf.Bytes(), 0o644); err != nil {
		return err
	}
	return nil
}

// ParseFontFlag parses "family=path[,weight[,italic]]" (CLI helper).
func ParseFontFlag(s string) (FontSpec, error) {
	fam, rest, ok := strings.Cut(s, "=")
	if !ok || fam == "" || rest == "" {
		return FontSpec{}, fmt.Errorf("font %q: expected family=path[,weight[,italic]]", s)
	}
	parts := strings.Split(rest, ",")
	spec := FontSpec{Family: fam, Path: parts[0]}
	if len(parts) > 1 && parts[1] != "" {
		switch strings.ToLower(parts[1]) {
		case "bold":
			spec.Weight = 700
		case "normal", "regular":
			spec.Weight = 400
		default:
			if _, err := fmt.Sscanf(parts[1], "%d", &spec.Weight); err != nil {
				return FontSpec{}, fmt.Errorf("font %q: bad weight %q", s, parts[1])
			}
		}
	}
	if len(parts) > 2 {
		switch strings.ToLower(parts[2]) {
		case "italic", "oblique", "true", "yes":
			spec.Italic = true
		}
	}
	return spec, nil
}

// isHTMLDocument reports whether the result tree is an (X)HTML document, which
// is rendered by the Flying Saucer compatible engine instead of the FO engine.
func isHTMLDocument(doc *xmldom.Node) bool {
	de := doc
	if doc.Type == xmldom.DocumentNode {
		de = doc.DocumentElement()
	}
	return de != nil && de.Type == xmldom.ElementNode && strings.EqualFold(de.Name, "html") &&
		(de.URI == "" || de.URI == "http://www.w3.org/1999/xhtml")
}

func renderHTMLDocument(doc *xmldom.Node, w io.Writer, opts Options) error {
	base := opts.BaseDir
	if base == "" {
		base = "."
	}
	xo := xhr.Options{
		Title: opts.Title, Author: opts.Author, MaxPages: opts.maxPages(),
		ResolveResource: func(uri string) ([]byte, error) {
			if strings.HasPrefix(uri, "file:") {
				uri = strings.TrimPrefix(strings.TrimPrefix(uri, "file://"), "file:")
			}
			if opts.readFile != nil {
				return opts.readFile(uri)
			}
			if !filepath.IsAbs(uri) {
				uri = filepath.Join(base, uri)
			}
			return os.ReadFile(uri)
		},
	}
	for _, f := range opts.Fonts {
		data := f.Data
		if data == nil {
			var err error
			if data, err = os.ReadFile(f.Path); err != nil {
				return fmt.Errorf("fop: font %q: %w", f.Family, err)
			}
		}
		xo.Fonts = append(xo.Fonts, xhr.FontData{Family: f.Family, Data: data})
	}
	// The reference pipeline serialises the stylesheet result (xsl:output
	// indent="yes") and parses it again, which re-introduces the layout
	// whitespace between elements that the stylesheet processing dropped.
	var ser bytes.Buffer
	if err := xmldom.Serialize(&ser, doc, true); err != nil {
		return err
	}
	reparsed, err := xmldom.ParseBytes(ser.Bytes())
	if err != nil {
		return fmt.Errorf("fop: reparsing HTML result: %w", err)
	}
	pdfData, err := xhr.Render(reparsed, xo)
	if err != nil {
		return err
	}
	_, err = w.Write(pdfData)
	return err
}
