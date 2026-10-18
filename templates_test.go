package fop

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
)

func pngBytes(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func templatesFS(t *testing.T) fstest.MapFS {
	return fstest.MapFS{
		"tpl/main.xsl": {Data: []byte(`<xsl:stylesheet version="1.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform" xmlns:fo="http://www.w3.org/1999/XSL/Format">
<xsl:import href="PATH_TO_FILE/parts/rows.xsl"/>
<xsl:param name="title" select="'none'"/>
<xsl:template match="/">
<fo:root><fo:layout-master-set><fo:simple-page-master master-name="p" page-width="200pt" page-height="200pt" margin="10pt"><fo:region-body/></fo:simple-page-master></fo:layout-master-set>
<fo:page-sequence master-reference="p"><fo:flow flow-name="xsl-region-body">
<fo:block><xsl:value-of select="$title"/></fo:block>
<fo:block><fo:external-graphic src="img/dot.png" content-width="10pt"/></fo:block>
<xsl:call-template name="rows"/>
</fo:flow></fo:page-sequence></fo:root>
</xsl:template>
</xsl:stylesheet>`)},
		"tpl/parts/rows.xsl": {Data: []byte(`<xsl:stylesheet version="1.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform" xmlns:fo="http://www.w3.org/1999/XSL/Format">
<xsl:template name="rows"><xsl:for-each select="/Root/Item"><fo:block break-after="page"><xsl:value-of select="@name"/></fo:block></xsl:for-each></xsl:template>
</xsl:stylesheet>`)},
		"tpl/img/dot.png": {Data: pngBytes(t)},
		"tpl/page.xsl": {Data: []byte(`<xsl:stylesheet version="1.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
<xsl:template match="/"><html><head><style>@page{size:A4}</style></head><body><p>Hello</p><img src="img/dot.png" width="20" height="20"/></body></html></xsl:template>
</xsl:stylesheet>`)},
		"tpl/escape.xsl": {Data: []byte(`<xsl:stylesheet version="1.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
<xsl:import href="../secret.xsl"/><xsl:template match="/"><x/></xsl:template></xsl:stylesheet>`)},
		"secret.xsl": {Data: []byte(`<xsl:stylesheet version="1.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform"/>`)},
	}
}

const itemsXML = `<Wrap><Root><Item name="a"/><Item name="b"/><Item name="c"/></Root></Wrap>`

func TestTemplatesFromFS(t *testing.T) {
	o := opts()
	o.SourceRoot = "Root"
	tp := NewTemplates(templatesFS(t), o)

	var buf bytes.Buffer
	if err := tp.WithParams(map[string]string{"title": "Hi"}).Render("tpl/main.xsl", strings.NewReader(itemsXML), &buf); err != nil {
		t.Fatal(err)
	}
	if n := pageCount(buf.Bytes()); n != 3 {
		t.Errorf("pages = %d, want 3", n)
	}
	if !strings.Contains(buf.String(), "/Subtype /Image") {
		t.Error("image from the fs.FS was not embedded")
	}

	// the compiled stylesheet is cached and shared
	if len(tp.cache.m) != 1 {
		t.Errorf("cache has %d entries", len(tp.cache.m))
	}

	buf.Reset()
	if err := tp.Render("tpl/page.xsl", strings.NewReader(`<r/>`), &buf); err != nil {
		t.Fatalf("html: %v", err)
	}
	if !strings.Contains(buf.String(), "/Subtype /Image") {
		t.Error("HTML image from the fs.FS was not embedded")
	}
}

func TestTemplatesErrors(t *testing.T) {
	tp := NewTemplates(templatesFS(t), opts())
	var buf bytes.Buffer
	if err := tp.Render("tpl/missing.xsl", strings.NewReader(`<r/>`), &buf); err == nil {
		t.Error("missing template: no error")
	}
	if err := tp.Render("tpl/escape.xsl", strings.NewReader(`<r/>`), &buf); err == nil {
		t.Error("import above the FS root must fail")
	}
	if err := tp.Compile("tpl/main.xsl", "tpl/page.xsl"); err != nil {
		t.Errorf("Compile: %v", err)
	}
	if err := tp.Compile("nope.xsl"); err == nil {
		t.Error("Compile of a missing template: no error")
	}
}

func TestTemplatesConcurrent(t *testing.T) {
	o := opts()
	o.SourceRoot = "Root"
	tp := NewTemplates(templatesFS(t), o)
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			var buf bytes.Buffer
			name := "tpl/main.xsl"
			if i%2 == 1 {
				name = "tpl/page.xsl"
			}
			if err := tp.Render(name, strings.NewReader(itemsXML), &buf); err != nil {
				errs <- fmt.Errorf("%s: %w", name, err)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
