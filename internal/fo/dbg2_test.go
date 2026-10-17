package fo

import (
	"fmt"
	"os"
	"testing"

	"github.com/ProDGG/fop-go/xmldom"
)

func TestDebugSpace(t *testing.T) {
	if os.Getenv("FOPGO_DEBUG") == "" {
		t.Skip()
	}
	src := `<fo:root xmlns:fo="http://www.w3.org/1999/XSL/Format" font-family="Liberation Sans" font-size="11pt"><fo:layout-master-set/><fo:page-sequence><fo:flow><fo:block text-align="center">— <fo:page-number/> —</fo:block></fo:flow></fo:page-sequence></fo:root>`
	doc, _ := xmldom.ParseString(src)
	tree, _ := BuildTree(doc)
	e := newTestEngine()
	lc := &lctx{e: e, refW: 481.89, pageNum: 3}
	blk := tree.Find("page-sequence").Find("flow").Find("block")
	it := lc.layoutBlock(blk).(*block)
	for _, c := range it.children {
		if l, ok := c.(*lineBox); ok {
			for _, i := range l.items {
				fmt.Printf("kind=%d x=%.2f w=%.2f", i.kind, i.x, i.w)
				for _, r := range i.runs {
					for _, g := range r.glyphs {
						fmt.Printf(" %c", g.Rune)
					}
				}
				fmt.Println()
			}
		}
	}
}
