package fop

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
)

// BenchmarkHTMLReport runs XML + XSLT -> HTML -> PDF on the report named by
// FOP_BENCH_XML / FOP_BENCH_XSL (skipped when unset; report data is not kept in the repository).
func BenchmarkHTMLReport(b *testing.B) {
	x, s := os.Getenv("FOP_BENCH_XML"), os.Getenv("FOP_BENCH_XSL")
	if x == "" || s == "" {
		b.Skip("set FOP_BENCH_XML and FOP_BENCH_XSL")
	}
	tmp := b.TempDir() + "/o.pdf"
	o := Options{SourceRoot: "Root", NoSystemFonts: true}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if err := RenderFiles(x, s, tmp, o); err != nil {
			b.Fatal(err)
		}
	}
}

func benchRender(b *testing.B, f func() error) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := f(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkInvoice: XML + XSLT -> XSL-FO -> PDF for the small example (1 page).
func BenchmarkInvoice(b *testing.B) {
	benchRender(b, func() error {
		return RenderFiles("examples/invoice/invoice.xml", "examples/invoice/invoice.xsl", os.DevNull, Options{NoSystemFonts: true})
	})
}

// BenchmarkFeaturesFO: the multi-page XSL-FO showcase (tables, columns, leaders, images).
func BenchmarkFeaturesFO(b *testing.B) {
	benchRender(b, func() error {
		return RenderFOFile("examples/features/features.fo", os.DevNull, Options{NoSystemFonts: true})
	})
}

const benchXSL = `<xsl:stylesheet version="1.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
<xsl:output method="html" indent="yes"/>
<xsl:template match="/Report"><html><head><style>
@page{size:A4 landscape;margin:1cm;@bottom-center{content:"Page " counter(page) " of " counter(pages)}}
body{font-family:Arial;font-size:9pt} table{width:100%;border-collapse:collapse}
td,th{border:1px solid #888;padding:4px;vertical-align:top} tr.odd td{background:#eef}
</style></head><body><h1>Synthetic report</h1><table><thead><tr><th>No</th><th>Name</th><th>Description</th><th>Amount</th></tr></thead><tbody>
<xsl:for-each select="Item"><tr><xsl:if test="position() mod 2 = 1"><xsl:attribute name="class">odd</xsl:attribute></xsl:if>
<td><xsl:value-of select="position()"/></td><td><xsl:value-of select="@name"/></td><td><xsl:value-of select="@text"/></td><td><xsl:value-of select="@amount"/></td></tr></xsl:for-each>
</tbody></table></body></html></xsl:template></xsl:stylesheet>`

// BenchmarkHTMLSynthetic: XML + XSLT -> HTML -> PDF for a generated ~50 page table report.
func BenchmarkHTMLSynthetic(b *testing.B) {
	var x strings.Builder
	x.WriteString("<Report>")
	for i := 0; i < 1500; i++ {
		fmt.Fprintf(&x, `<Item name="Item %d" text="Some longer description text that wraps inside the column number %d" amount="%d.50"/>`, i, i, i*7)
	}
	x.WriteString("</Report>")
	var out bytes.Buffer
	benchRender(b, func() error {
		out.Reset()
		return Render(strings.NewReader(x.String()), strings.NewReader(benchXSL), &out, Options{NoSystemFonts: true})
	})
	b.ReportMetric(float64(strings.Count(out.String(), "/Type /Page /")), "pages")
	_ = io.Discard
}
