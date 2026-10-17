package fo

import (
	"fmt"
	"os"
	"testing"

	"github.com/ProDGG/fop-go/xmldom"
)

func TestDebugParagraph(t *testing.T) {
	if os.Getenv("FOPGO_DEBUG") == "" {
		t.Skip()
	}
	src := `<fo:root xmlns:fo="http://www.w3.org/1999/XSL/Format" font-family="Liberation Sans" font-size="11pt"><fo:layout-master-set/><fo:page-sequence><fo:flow><fo:block text-align="justify">Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat. Съешь же ещё этих мягких французских булок, да выпей чаю. Эхо-<fo:inline font-weight="bold">жирный фрагмент</fo:inline> и курсив.</fo:block></fo:flow></fo:page-sequence></fo:root>`
	doc, err := xmldom.ParseString(src)
	if err != nil {
		t.Fatal(err)
	}
	tree, err := BuildTree(doc)
	if err != nil {
		t.Fatal(err)
	}
	debugKnuth = true
	defer func() { debugKnuth = false }()
	e := newTestEngine()
	lc := &lctx{e: e, refW: 481.89}
	blk := tree.Find("page-sequence").Find("flow").Find("block")
	it := lc.layoutBlock(blk).(*block)
	for _, c := range it.children {
		if l, ok := c.(*lineBox); ok {
			s := ""
			for _, i := range l.items {
				if i.kind == itText {
					for _, r := range i.runs {
						for _, g := range r.glyphs {
							s += string(g.Rune)
						}
					}
				} else {
					s += " "
				}
			}
			fmt.Printf("%q\n", s)
		}
	}
}
