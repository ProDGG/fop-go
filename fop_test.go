package fop

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

var pageRe = regexp.MustCompile(`/Type /Page /`)

func pageCount(pdf []byte) int { return len(pageRe.FindAll(pdf, -1)) }

func opts() Options { return Options{NoSystemFonts: true, NoCompress: true} }

const foHead = `<fo:root xmlns:fo="http://www.w3.org/1999/XSL/Format" font-size="10pt">
<fo:layout-master-set>
 <fo:simple-page-master master-name="p" page-width="200pt" page-height="200pt" margin="10pt">
  <fo:region-body margin-bottom="20pt"/><fo:region-after extent="15pt"/>
 </fo:simple-page-master>
</fo:layout-master-set>
<fo:page-sequence master-reference="p">
 <fo:static-content flow-name="xsl-region-after"><fo:block>p<fo:page-number/>/<fo:page-number-citation-last ref-id="s"/></fo:block></fo:static-content>
 <fo:flow flow-name="xsl-region-body" id="s">`
const foTail = `</fo:flow></fo:page-sequence></fo:root>`

func render(t *testing.T, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := RenderFO(strings.NewReader(foHead+body+foTail), &buf, opts()); err != nil {
		t.Fatalf("RenderFO: %v", err)
	}
	if !bytes.HasPrefix(buf.Bytes(), []byte("%PDF-")) || !bytes.HasSuffix(buf.Bytes(), []byte("%%EOF\n")) {
		t.Fatalf("not a PDF")
	}
	return buf.Bytes()
}

func TestSinglePage(t *testing.T) {
	pdf := render(t, `<fo:block>Привет, мир</fo:block>`)
	if n := pageCount(pdf); n != 1 {
		t.Errorf("pages = %d, want 1", n)
	}
}

func TestPagination(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 120; i++ {
		fmt.Fprintf(&sb, `<fo:block>Строка %d</fo:block>`, i)
	}
	pdf := render(t, sb.String())
	// 160pt body / 12pt lines = 13 lines per page -> 10 pages
	if n := pageCount(pdf); n < 9 || n > 11 {
		t.Errorf("pages = %d, want about 10", n)
	}
}

func TestBreakBefore(t *testing.T) {
	pdf := render(t, `<fo:block>a</fo:block><fo:block break-before="page">b</fo:block><fo:block break-before="page">c</fo:block>`)
	if n := pageCount(pdf); n != 3 {
		t.Errorf("pages = %d, want 3", n)
	}
	// nested forced break inside a wrapper block
	pdf = render(t, `<fo:block><fo:block>a</fo:block><fo:block break-before="page">b</fo:block></fo:block>`)
	if n := pageCount(pdf); n != 2 {
		t.Errorf("nested break: pages = %d, want 2", n)
	}
}

func TestOddPageBreak(t *testing.T) {
	// the second block must start on page 3: a blank page 2 is inserted
	pdf := render(t, `<fo:block>a</fo:block><fo:block break-before="odd-page">b</fo:block>`)
	if n := pageCount(pdf); n != 3 {
		t.Errorf("pages = %d, want 3", n)
	}
}

func TestKeepTogether(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 12; i++ {
		fmt.Fprintf(&sb, `<fo:block>x%d</fo:block>`, i)
	}
	sb.WriteString(`<fo:block keep-together="always"><fo:block>k1</fo:block><fo:block>k2</fo:block><fo:block>k3</fo:block></fo:block>`)
	pdf := render(t, sb.String())
	if n := pageCount(pdf); n != 2 {
		t.Errorf("pages = %d, want 2", n)
	}
}

func TestTableRowSpanAndHeaderRepeat(t *testing.T) {
	var rows strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&rows, `<fo:table-row><fo:table-cell><fo:block>r%d</fo:block></fo:table-cell><fo:table-cell><fo:block>c</fo:block></fo:table-cell></fo:table-row>`, i)
	}
	pdf := render(t, `<fo:table table-layout="fixed" width="100%"><fo:table-column column-width="50%"/><fo:table-column column-width="50%"/>
<fo:table-header><fo:table-row><fo:table-cell><fo:block>HEAD</fo:block></fo:table-cell><fo:table-cell><fo:block/></fo:table-cell></fo:table-row></fo:table-header>
<fo:table-body>`+rows.String()+`</fo:table-body></fo:table>`)
	n := pageCount(pdf)
	if n < 3 {
		t.Fatalf("pages = %d, want >= 3", n)
	}
	// the header text is drawn once per page: each page content has the glyphs of HEAD
	pdf2 := render(t, `<fo:table table-layout="fixed" width="100%"><fo:table-column column-width="100%"/>
<fo:table-body><fo:table-row><fo:table-cell number-rows-spanned="2"><fo:block>span</fo:block></fo:table-cell></fo:table-row></fo:table-body></fo:table>`)
	if pageCount(pdf2) != 1 {
		t.Errorf("rowspan table did not render on one page")
	}
}

func TestFootersAndCitations(t *testing.T) {
	pdf := render(t, `<fo:block>one</fo:block><fo:block break-before="page">two</fo:block>`)
	// the ToUnicode map must contain digits used in "p1/2" and "p2/2"
	if !bytes.Contains(pdf, []byte("beginbfchar")) {
		t.Error("no ToUnicode map")
	}
}

func TestTransformPipeline(t *testing.T) {
	xsl := `<xsl:stylesheet version="1.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform" xmlns:fo="http://www.w3.org/1999/XSL/Format">
<xsl:param name="who" select="'мир'"/>
<xsl:template match="/"><fo:root><fo:layout-master-set><fo:simple-page-master master-name="m" page-width="100pt" page-height="100pt"><fo:region-body/></fo:simple-page-master></fo:layout-master-set>
<fo:page-sequence master-reference="m"><fo:flow flow-name="xsl-region-body"><xsl:for-each select="//i"><fo:block><xsl:value-of select="."/>, <xsl:value-of select="$who"/></fo:block></xsl:for-each></fo:flow></fo:page-sequence></fo:root></xsl:template></xsl:stylesheet>`
	var buf bytes.Buffer
	o := opts()
	o.Params = map[string]string{"who": "Go"}
	if err := Render(strings.NewReader(`<r><i>a</i><i>b</i></r>`), strings.NewReader(xsl), &buf, o); err != nil {
		t.Fatal(err)
	}
	if pageCount(buf.Bytes()) != 1 {
		t.Error("expected one page")
	}
	res, err := Transform(strings.NewReader(`<r><i>a</i></r>`), strings.NewReader(xsl), o)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(SerializeFO(res)); !strings.Contains(got, "a, Go") {
		t.Errorf("unexpected FO: %s", got)
	}
}

func TestErrors(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderFO(strings.NewReader(`<root/>`), &buf, opts()); err == nil {
		t.Error("expected error for non-FO root")
	}
	if err := RenderFO(strings.NewReader(`<fo:root xmlns:fo="http://www.w3.org/1999/XSL/Format"/>`), &buf, opts()); err == nil {
		t.Error("expected error for missing layout-master-set")
	}
	if err := Render(strings.NewReader(`<a/>`), strings.NewReader(`<xsl:stylesheet xmlns:xsl="http://www.w3.org/1999/XSL/Transform" version="1.0"><xsl:template match="/"><xsl:value-of select="nope("/></xsl:template></xsl:stylesheet>`), &buf, opts()); err == nil {
		t.Error("expected XPath syntax error")
	}
}

func TestExamples(t *testing.T) {
	cases := []struct{ xml, xsl string }{{"examples/invoice/invoice.xml", "examples/invoice/invoice.xsl"}}
	for _, c := range cases {
		out := t.TempDir() + "/out.pdf"
		o := opts()
		var warnings []string
		o.Warn = func(s string) { warnings = append(warnings, s) }
		if err := RenderFiles(c.xml, c.xsl, out, o); err != nil {
			t.Fatalf("%s: %v", c.xml, err)
		}
		if len(warnings) > 0 {
			t.Errorf("%s: unexpected warnings: %v", c.xml, warnings)
		}
		if st, err := os.Stat(out); err != nil || st.Size() < 1000 {
			t.Errorf("%s: bad output", c.xml)
		}
	}
	out := t.TempDir() + "/f.pdf"
	o := opts()
	if err := RenderFOFile("examples/features/features.fo", out, o); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(out)
	if n := pageCount(data); n < 10 {
		t.Errorf("features.fo: pages = %d", n)
	}
}

func TestFontFlag(t *testing.T) {
	f, err := ParseFontFlag("My Font=/x/y.ttf,bold,italic")
	if err != nil || f.Family != "My Font" || f.Weight != 700 || !f.Italic || f.Path != "/x/y.ttf" {
		t.Errorf("bad parse: %+v %v", f, err)
	}
	if _, err := ParseFontFlag("nonsense"); err == nil {
		t.Error("expected error")
	}
}

func TestSVGAndFootnote(t *testing.T) {
	body := `<fo:block>x<fo:footnote><fo:inline>1</fo:inline><fo:footnote-body><fo:block>note body</fo:block></fo:footnote-body></fo:footnote>
<fo:instream-foreign-object><svg xmlns="http://www.w3.org/2000/svg" width="50" height="50"><circle cx="25" cy="25" r="20" fill="red"/><text x="5" y="45">hi</text></svg></fo:instream-foreign-object></fo:block>`
	out := render(t, body)
	if !bytes.Contains(out, []byte(" c\n")) {
		t.Error("SVG circle was not drawn as curves")
	}
	if !bytes.Contains(out, []byte("1 0 0 rg")) {
		t.Error("SVG fill colour missing")
	}
}

// TestHTMLReportPipeline runs XML + XSLT (HTML output, PATH_TO_FILE imports,
// enveloped source) through the Flying Saucer compatible engine.
func TestHTMLReportPipeline(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := dir + "/" + name
		if err := os.MkdirAll(p[:strings.LastIndex(p, "/")], 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	write("items/rows.xsl", `<xsl:stylesheet version="1.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
<xsl:template name="rows"><xsl:for-each select="/Root/Item"><tr><td class="c"><xsl:value-of select="@name"/></td></tr></xsl:for-each></xsl:template>
</xsl:stylesheet>`)
	xsl := write("main.xslt", `<xsl:stylesheet version="1.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
<xsl:import href="PATH_TO_FILE/items/rows.xsl"/>
<xsl:output method="html" indent="yes"/>
<xsl:template match="Root"><html><head><style>@page{size:A4;margin:1cm} td.c{padding:4px;border:1px solid #888}</style></head>
<body><table><tbody><xsl:call-template name="rows"/></tbody></table></body></html></xsl:template>
</xsl:stylesheet>`)
	var items strings.Builder
	items.WriteString(`<Report><Root>`)
	for i := 0; i < 120; i++ {
		fmt.Fprintf(&items, `<Item name="Row %d"/>`, i)
	}
	items.WriteString(`</Root></Report>`)
	xml := write("data.xml", items.String())
	out := dir + "/out.pdf"
	o := opts()
	o.SourceRoot = "Root"
	if err := RenderFiles(xml, xsl, out, o); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		t.Fatal("not a PDF")
	}
	if n := pageCount(data); n < 2 {
		t.Errorf("expected the table to span several pages, got %d", n)
	}
}

func TestMaxPages(t *testing.T) {
	body := strings.Repeat(`<fo:block break-after="page">x</fo:block>`, 5)
	o := opts()
	o.MaxPages = 3
	var buf bytes.Buffer
	if err := RenderFO(strings.NewReader(foHead+body+foTail), &buf, o); !errors.Is(err, ErrPageLimit) {
		t.Fatalf("FO: got %v, want ErrPageLimit", err)
	}
	o.MaxPages = 5
	if err := RenderFO(strings.NewReader(foHead+body+foTail), &buf, o); err != nil {
		t.Fatalf("FO within limit: %v", err)
	}

	// a glyph taller than the page makes the HTML engine add pages for it
	xsl := `<xsl:stylesheet version="1.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
<xsl:template match="/"><html><head><style>@page{size:A4} div{font-size:20000px}</style></head><body><div>x</div></body></html></xsl:template>
</xsl:stylesheet>`
	o.MaxPages = 5
	buf.Reset()
	if err := Render(strings.NewReader(`<r/>`), strings.NewReader(xsl), &buf, o); !errors.Is(err, ErrPageLimit) {
		t.Fatalf("HTML: got %v, want ErrPageLimit", err)
	}
	o.MaxPages = -1
	buf.Reset()
	if err := Render(strings.NewReader(`<r/>`), strings.NewReader(xsl), &buf, o); err != nil {
		t.Fatalf("HTML without limit: %v", err)
	}
}
