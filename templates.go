package fop

import (
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
	"sync"

	"github.com/ProDGG/fop-go/xslt"
)

// Templates renders XML into PDF with stylesheets that live in an fs.FS, for
// example an embed.FS compiled into the binary:
//
//	//go:embed templates
//	var tpl embed.FS
//
//	t := fop.NewTemplates(tpl, fop.Options{SourceRoot: "Root"})
//	err := t.Render("templates/IndividualAdvancedReport.xslt", xmlReader, pdfWriter)
//
// xsl:import, xsl:include, document() and relative image paths are resolved
// inside the same fs.FS (relative to the stylesheet that refers to them). The
// stylesheets are compiled on first use and cached, so a Templates value is
// meant to be created once and shared: it is safe for concurrent use.
type Templates struct {
	fsys  fs.FS
	opts  Options
	cache *tplCache
}

type tplCache struct {
	mu sync.RWMutex
	m  map[string]*xslt.Stylesheet
}

// NewTemplates returns a renderer for the stylesheets in fsys. opts applies to
// every Render call (fonts, SourceRoot, Params, MaxPages, ...); BaseDir and
// XSLBase are not used, because every path is resolved inside fsys.
func NewTemplates(fsys fs.FS, opts Options) *Templates {
	return &Templates{fsys: fsys, opts: opts, cache: &tplCache{m: map[string]*xslt.Stylesheet{}}}
}

// WithParams returns a renderer that passes the given xsl:param values to the
// stylesheets (they replace opts.Params). It shares the compiled stylesheets
// with t.
func (t *Templates) WithParams(params map[string]string) *Templates {
	c := *t
	c.opts.Params = params
	return &c
}

// Render transforms the XML read from xml with the stylesheet name (a
// slash-separated path inside the fs.FS) and writes the PDF to w. The
// stylesheet may produce XSL-FO or HTML.
func (t *Templates) Render(name string, xml io.Reader, w io.Writer) error {
	ss, err := t.compile(name)
	if err != nil {
		return err
	}
	xmlData, err := io.ReadAll(xml)
	if err != nil {
		return err
	}
	opts := t.opts
	opts.readFile = t.readResource(name)
	res, err := runStylesheet(ss, xmlData, opts, "")
	if err != nil {
		return err
	}
	return RenderFODocument(res, w, opts)
}

// Compile compiles the named stylesheets now (and keeps them cached), so that
// errors in them show up at start-up rather than on the first request.
func (t *Templates) Compile(names ...string) error {
	for _, n := range names {
		if _, err := t.compile(n); err != nil {
			return err
		}
	}
	return nil
}

func (t *Templates) compile(name string) (*xslt.Stylesheet, error) {
	name = path.Clean(strings.TrimPrefix(name, "/"))
	t.cache.mu.RLock()
	ss := t.cache.m[name]
	t.cache.mu.RUnlock()
	if ss != nil {
		return ss, nil
	}
	data, err := fs.ReadFile(t.fsys, name)
	if err != nil {
		return nil, fmt.Errorf("fop: template %q: %w", name, err)
	}
	data = replacePathPlaceholder(data, t.opts)
	ss, err = xslt.CompileBytes(data, xslt.Options{Resolver: xslt.FSResolver{FS: t.fsys}, BaseURI: name})
	if err != nil {
		return nil, fmt.Errorf("fop: template %q: %w", name, err)
	}
	t.cache.mu.Lock()
	t.cache.m[name] = ss
	t.cache.mu.Unlock()
	return ss, nil
}

// readResource loads images and similar files from the fs.FS: first relative
// to the directory of the stylesheet, then relative to the root.
func (t *Templates) readResource(tpl string) func(string) ([]byte, error) {
	dir := path.Dir(path.Clean(tpl))
	return func(p string) ([]byte, error) {
		p = strings.TrimPrefix(p, "file://")
		var first error
		for _, c := range []string{path.Join(dir, p), path.Clean(strings.TrimPrefix(p, "/"))} {
			if !fs.ValidPath(c) {
				continue
			}
			data, err := fs.ReadFile(t.fsys, c)
			if err == nil {
				return data, nil
			}
			if first == nil {
				first = err
			}
		}
		if first == nil {
			first = fs.ErrInvalid
		}
		return nil, first
	}
}

// RenderFS is a one-shot form of NewTemplates(fsys, opts).Render(name, xml, w).
func RenderFS(fsys fs.FS, name string, xml io.Reader, w io.Writer, opts Options) error {
	return NewTemplates(fsys, opts).Render(name, xml, w)
}
