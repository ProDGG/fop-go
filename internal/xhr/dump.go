package xhr

import (
	"fmt"
	"strings"
)

var kindNames = map[Kind]string{
	KBlock: "BlockBox", KAnon: "AnonymousBlockBox", KTable: "TableBox", KSection: "TableSectionBox",
	KRow: "TableRowBox", KCell: "TableCellBox", KLine: "LineBox", KInline: "InlineLayoutBox",
}

// dumpBox writes the box tree in the format of the reference BoxDump harness,
// used to diff the layout against Flying Saucer.
func dumpBox(sb *strings.Builder, b *Box, depth int) {
	sb.WriteString(strings.Repeat(" ", depth))
	fmt.Fprintf(sb, "%s %d,%d %dx%d cw=%d", kindNames[b.Kind], b.absX, b.absY, b.width(), b.height, b.contentWidth)
	if b.Elem != nil {
		fmt.Fprintf(sb, " <%s>", b.Elem.Name)
	}
	sb.WriteByte('\n')
	if b.Kind == KInline {
		for _, c := range b.inlineChildren {
			switch t := c.(type) {
			case *Box:
				dumpBox(sb, t, depth+1)
			case *InlineText:
				fmt.Fprintf(sb, "%sT x=%d w=%d [%s]\n", strings.Repeat(" ", depth+1), t.x, t.width, string(t.substring()))
			}
		}
	}
	for _, c := range b.children {
		dumpBox(sb, c, depth+1)
	}
}
