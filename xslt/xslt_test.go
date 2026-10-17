package xslt

import (
	"strings"
	"testing"

	"github.com/ProDGG/fop-go/xmldom"
)

func run(t *testing.T, xsl, xml string, params map[string]interface{}) string {
	t.Helper()
	ss, err := CompileBytes([]byte(xsl), Options{})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	src, err := xmldom.ParseString(xml)
	if err != nil {
		t.Fatal(err)
	}
	res, err := ss.Transform(src, TransformOptions{Params: params})
	if err != nil {
		t.Fatalf("transform: %v", err)
	}
	return res.String()
}

const hdr = `<xsl:stylesheet version="1.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform">`

func TestBasic(t *testing.T) {
	xsl := hdr + `
<xsl:output method="xml"/>
<xsl:param name="title" select="'Default'"/>
<xsl:variable name="total" select="sum(/list/item/@price)"/>
<xsl:key name="byid" match="item" use="@id"/>
<xsl:template match="/list">
  <out title="{$title}" n="{count(item)}">
    <xsl:apply-templates select="item"><xsl:sort select="@price" data-type="number" order="descending"/></xsl:apply-templates>
    <total><xsl:value-of select="format-number($total, '#,##0.00')"/></total>
    <k><xsl:value-of select="key('byid','b')/name"/></k>
  </out>
</xsl:template>
<xsl:template match="item">
  <i pos="{position()}" id="{@id}"><xsl:value-of select="name"/></i>
</xsl:template>
</xsl:stylesheet>`
	xml := `<list><item id="a" price="1500.5"><name>Alpha</name></item><item id="b" price="30000"><name>Beta</name></item><item id="c" price="2"><name>Гамма</name></item></list>`
	got := run(t, xsl, xml, map[string]interface{}{"title": "T1"})
	want := `<out title="T1" n="3"><i pos="1" id="b">Beta</i><i pos="2" id="a">Alpha</i><i pos="3" id="c">Гамма</i><total>31,502.50</total><k>Beta</k></out>`
	if !strings.Contains(got, want) {
		t.Errorf("got\n%s\nwant substring\n%s", got, want)
	}
}

func TestNamedAndChoose(t *testing.T) {
	xsl := hdr + `
<xsl:template name="fact"><xsl:param name="n" select="1"/>
  <xsl:choose><xsl:when test="$n &lt;= 1">1</xsl:when>
  <xsl:otherwise><xsl:variable name="r"><xsl:call-template name="fact"><xsl:with-param name="n" select="$n - 1"/></xsl:call-template></xsl:variable>
  <xsl:value-of select="$n * $r"/></xsl:otherwise></xsl:choose>
</xsl:template>
<xsl:template match="/"><r><xsl:call-template name="fact"><xsl:with-param name="n" select="6"/></xsl:call-template></r></xsl:template>
</xsl:stylesheet>`
	got := run(t, xsl, `<a/>`, nil)
	if !strings.Contains(got, "<r>720</r>") {
		t.Errorf("got %s", got)
	}
}

func TestNumberAndCopy(t *testing.T) {
	xsl := hdr + `
<xsl:template match="/"><o><xsl:apply-templates/></o></xsl:template>
<xsl:template match="sec"><s n="{count(preceding-sibling::sec)+1}"><xsl:copy-of select="@*"/><xsl:number format="1.a "/><xsl:apply-templates/></s></xsl:template>
<xsl:template match="p"><xsl:copy><xsl:apply-templates select="@*|node()"/></xsl:copy></xsl:template>
<xsl:template match="@*"><xsl:copy/></xsl:template>
</xsl:stylesheet>`
	got := run(t, xsl, `<doc><sec id="x"><p a="1">t</p></sec><sec><p/></sec></doc>`, nil)
	if !strings.Contains(got, `<s n="1" id="x">1 <p a="1">t</p></s>`) {
		t.Errorf("got %s", got)
	}
}

func TestPatterns(t *testing.T) {
	xsl := hdr + `
<xsl:template match="/"><o><xsl:apply-templates select="//*"/></o></xsl:template>
<xsl:template match="a/b">[ab]</xsl:template>
<xsl:template match="b">[b]</xsl:template>
<xsl:template match="*[@x]" priority="2">[x]</xsl:template>
<xsl:template match="a"/>
<xsl:template match="c[2]">[c2]</xsl:template>
<xsl:template match="c">[c]</xsl:template>
</xsl:stylesheet>`
	got := run(t, xsl, `<a><b/><d><b/></d><c/><c/><e x="1"/></a>`, nil)
	if !strings.Contains(got, `<o>[ab][b][b][c][c2][x]</o>`) {
		t.Errorf("got %s", got)
	}
}

func TestLiteralNamespaces(t *testing.T) {
	xsl := `<xsl:stylesheet version="1.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform" xmlns:fo="http://www.w3.org/1999/XSL/Format">
<xsl:template match="/"><fo:root><fo:block font-size="{12+2}pt">Привет</fo:block></fo:root></xsl:template></xsl:stylesheet>`
	got := run(t, xsl, `<a/>`, nil)
	if !strings.Contains(got, `<fo:root xmlns:fo="http://www.w3.org/1999/XSL/Format"><fo:block font-size="14pt">Привет</fo:block></fo:root>`) {
		t.Errorf("got %s", got)
	}
}
