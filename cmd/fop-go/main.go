// Command fop-go converts XML + XSLT (or XSL-FO) to PDF, in the spirit of the
// Apache FOP command line.
//
//	fop-go -xml data.xml -xsl template.xsl -pdf out.pdf
//	fop-go -fo document.fo -pdf out.pdf
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	fop "github.com/ProDGG/fop-go"
	"github.com/ProDGG/fop-go/xmldom"
)

type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(v string) error { *m = append(*m, v); return nil }

func main() {
	var (
		xmlPath  = flag.String("xml", "", "XML input file")
		xslPath  = flag.String("xsl", "", "XSLT stylesheet")
		foPath   = flag.String("fo", "", "XSL-FO input file (instead of -xml/-xsl)")
		pdfPath  = flag.String("pdf", "", "PDF output file")
		foOut    = flag.String("foout", "", "also write the intermediate XSL-FO to this file")
		baseDir  = flag.String("base", "", "base directory for relative image paths")
		title    = flag.String("title", "", "PDF title")
		author   = flag.String("author", "", "PDF author")
		noComp   = flag.Bool("nocompress", false, "do not compress PDF streams")
		noSys    = flag.Bool("nosystemfonts", false, "do not scan system font directories")
		dpi      = flag.Float64("dpi", 72, "source resolution used to convert px to pt")
		remote   = flag.Bool("remote", false, "allow loading http(s) images")
		xmlRoot  = flag.String("xmlroot", "", "use the first element with this name as the document element of the XML input")
		quiet    = flag.Bool("q", false, "suppress warnings")
		maxPages = flag.Int("maxpages", 0, "abort when the document needs more pages (0 = default 100000, -1 = no limit)")
		showVer  = flag.Bool("version", false, "print the version and exit")
		params   multi
		fonts    multi
		fontDirs multi
	)
	flag.Var(&params, "param", "stylesheet parameter name=value (repeatable)")
	flag.Var(&fonts, "font", "extra TrueType font family=path[,weight[,italic]] (repeatable)")
	flag.Var(&fontDirs, "fontdir", "directory with extra fonts (repeatable)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage:\n  fop-go -xml data.xml -xsl template.xsl -pdf out.pdf [options]\n  fop-go -fo doc.fo -pdf out.pdf [options]\n\nOptions:\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if *showVer {
		fmt.Println("fop-go", buildVersion())
		return
	}

	opts := fop.Options{
		BaseDir: *baseDir, Title: *title, Author: *author, NoCompress: *noComp, NoSystemFonts: *noSys,
		SourceResolution: *dpi, AllowRemote: *remote, FontDirs: fontDirs, SourceRoot: *xmlRoot, MaxPages: *maxPages,
	}
	if !*quiet {
		opts.Warn = func(s string) { fmt.Fprintln(os.Stderr, "warning:", s) }
	}
	if len(params) > 0 {
		opts.Params = map[string]string{}
		for _, p := range params {
			k, v, ok := strings.Cut(p, "=")
			if !ok {
				fatal("bad -param %q (expected name=value)", p)
			}
			opts.Params[k] = v
		}
	}
	for _, f := range fonts {
		spec, err := fop.ParseFontFlag(f)
		if err != nil {
			fatal("%v", err)
		}
		opts.Fonts = append(opts.Fonts, spec)
	}
	if *pdfPath == "" {
		flag.Usage()
		os.Exit(2)
	}

	switch {
	case *foPath != "":
		if *foOut != "" {
			fatal("-foout cannot be used together with -fo")
		}
		if err := fop.RenderFOFile(*foPath, *pdfPath, opts); err != nil {
			fatal("%v", err)
		}
	case *xmlPath != "" && *xslPath != "":
		if *foOut == "" {
			if err := fop.RenderFiles(*xmlPath, *xslPath, *pdfPath, opts); err != nil {
				fatal("%v", err)
			}
			break
		}
		xmlData, err := os.ReadFile(*xmlPath)
		if err != nil {
			fatal("%v", err)
		}
		xslData, err := os.ReadFile(*xslPath)
		if err != nil {
			fatal("%v", err)
		}
		if opts.BaseDir == "" {
			opts.BaseDir = filepath.Dir(*xmlPath)
		}
		opts.XSLBase = *xslPath
		res, err := fop.Transform(bytes.NewReader(xmlData), bytes.NewReader(xslData), opts)
		if err != nil {
			fatal("%v", err)
		}
		if err := os.WriteFile(*foOut, fop.SerializeFO(res), 0o644); err != nil {
			fatal("%v", err)
		}
		if err := renderDoc(res, *pdfPath, opts); err != nil {
			fatal("%v", err)
		}
	default:
		flag.Usage()
		os.Exit(2)
	}
}

func renderDoc(doc *xmldom.Node, pdfPath string, opts fop.Options) error {
	var buf bytes.Buffer
	if err := fop.RenderFODocument(doc, &buf, opts); err != nil {
		return err
	}
	return os.WriteFile(pdfPath, buf.Bytes(), 0o644)
}

func fatal(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "fop-go: "+format+"\n", args...)
	os.Exit(1)
}

// version can be set at build time: -ldflags "-X main.version=v1.2.3".
var version = ""

// buildVersion reports the module version (set by go install pkg@version) or,
// for a local build, the VCS revision.
func buildVersion() string {
	if version != "" {
		return version
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	v := bi.Main.Version
	if v != "" && v != "(devel)" {
		return v
	}
	rev, dirty := "", false
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if rev == "" {
		return "devel"
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	if dirty {
		rev += "-dirty"
	}
	return "devel " + rev
}
