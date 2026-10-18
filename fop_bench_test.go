package fop

import (
	"os"
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
