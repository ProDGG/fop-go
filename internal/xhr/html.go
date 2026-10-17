package xhr

import (
	_ "embed"
	"strings"

	"github.com/ProDGG/fop-go/xmldom"
)

//go:embed ua.css
var uaCSS string

func attrTrim(e *xmldom.Node, name string) (string, bool) {
	v, ok := e.AttrOK(name)
	v = strings.TrimSpace(v)
	if !ok || v == "" {
		return "", false
	}
	return v, true
}

func isInteger(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func convertToLength(v string) string {
	if isInteger(v) {
		return v + "px"
	}
	return v
}

// elementStyling mirrors XhtmlCssOnlyNamespaceHandler.getElementStyling: some
// attributes mapped to CSS followed by the style attribute.
func elementStyling(e *xmldom.Node) string {
	var sb strings.Builder
	switch e.Name {
	case "td", "th":
		if s, ok := attrTrim(e, "colspan"); ok {
			sb.WriteString("-fs-table-cell-colspan: " + s + ";")
		}
		if s, ok := attrTrim(e, "rowspan"); ok {
			sb.WriteString("-fs-table-cell-rowspan: " + s + ";")
		}
	case "img":
		if s, ok := attrTrim(e, "width"); ok {
			sb.WriteString("width: " + convertToLength(s) + ";")
		}
		if s, ok := attrTrim(e, "height"); ok {
			sb.WriteString("height: " + convertToLength(s) + ";")
		}
	case "colgroup", "col":
		if s, ok := attrTrim(e, "span"); ok {
			sb.WriteString("-fs-table-cell-colspan: " + s + ";")
		}
		if s, ok := attrTrim(e, "width"); ok {
			sb.WriteString("width: " + convertToLength(s) + ";")
		}
	}
	sb.WriteString(e.Attr("style"))
	return sb.String()
}

func looksLikeMangledColor(s string) bool {
	if len(s) != 6 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func findTable(cell *xmldom.Node) *xmldom.Node {
	n := cell.Parent
	if isElem(n) && n.Name == "tr" {
		n = n.Parent
		if isElem(n) {
			switch n.Name {
			case "table":
				return n
			case "tbody", "tfoot", "thead":
				n = n.Parent
				if isElem(n) && n.Name == "table" {
					return n
				}
			}
		}
	}
	return nil
}

func applyFloatingAlign(e *xmldom.Node, sb *strings.Builder) {
	if s, ok := attrTrim(e, "align"); ok {
		switch strings.ToLower(strings.TrimSpace(s)) {
		case "left":
			sb.WriteString("float: left;")
		case "right":
			sb.WriteString("float: right;")
		case "center":
			sb.WriteString("margin-left: auto; margin-right: auto;")
		}
	}
}

func applyTableContentAlign(e *xmldom.Node, sb *strings.Builder) {
	if s, ok := attrTrim(e, "align"); ok {
		sb.WriteString("text-align: " + strings.ToLower(s) + ";")
	}
	if s, ok := attrTrim(e, "valign"); ok {
		sb.WriteString("vertical-align: " + strings.ToLower(s) + ";")
	}
}

// nonCSSStyling mirrors the presentational-hint mapping of FS's XhtmlNamespaceHandler.
func nonCSSStyling(e *xmldom.Node) string {
	var sb strings.Builder
	switch e.Name {
	case "table":
		if s, ok := attrTrim(e, "width"); ok {
			sb.WriteString("width: " + convertToLength(s) + ";")
		}
		if s, ok := attrTrim(e, "border"); ok {
			sb.WriteString("border: " + convertToLength(s) + " inset black;")
		}
		if s, ok := attrTrim(e, "cellspacing"); ok {
			sb.WriteString("border-collapse: separate; border-spacing: " + convertToLength(s) + ";")
		}
		if s, ok := attrTrim(e, "bgcolor"); ok {
			s = strings.ToLower(s)
			if looksLikeMangledColor(s) {
				s = "#" + s
			}
			sb.WriteString("background-color: " + s + ";")
		}
		if s, ok := attrTrim(e, "background"); ok {
			sb.WriteString("background-image: url(" + s + ");")
		}
		applyFloatingAlign(e, &sb)
	case "td", "th":
		if t := findTable(e); t != nil {
			if s, ok := attrTrim(t, "cellpadding"); ok {
				sb.WriteString("padding: " + convertToLength(s) + ";")
			}
			if s, ok := attrTrim(t, "border"); ok && s != "0" {
				sb.WriteString("border: 1px outset black;")
			}
		}
		if s, ok := attrTrim(e, "width"); ok {
			sb.WriteString("width: " + convertToLength(s) + ";")
		}
		if s, ok := attrTrim(e, "height"); ok {
			sb.WriteString("height: " + convertToLength(s) + ";")
		}
		applyTableContentAlign(e, &sb)
		if s, ok := attrTrim(e, "bgcolor"); ok {
			s = strings.ToLower(s)
			if looksLikeMangledColor(s) {
				s = "#" + s
			}
			sb.WriteString("background-color: " + s + ";")
		}
		if s, ok := attrTrim(e, "background"); ok {
			sb.WriteString("background-image: url(" + s + ");")
		}
	case "tr":
		applyTableContentAlign(e, &sb)
	case "img":
		applyFloatingAlign(e, &sb)
	case "p", "div":
		if s, ok := attrTrim(e, "align"); ok {
			switch s = strings.ToLower(strings.TrimSpace(s)); s {
			case "left", "right", "center", "justify":
				sb.WriteString("text-align: " + s + ";")
			}
		}
	}
	return sb.String()
}

func elementText(e *xmldom.Node) string {
	var sb strings.Builder
	for _, c := range e.Children {
		if c.Type == xmldom.TextNode {
			sb.WriteString(c.Value)
		}
	}
	return sb.String()
}

func findChild(parent *xmldom.Node, name string) *xmldom.Node {
	for _, c := range parent.Children {
		if isElem(c) && c.Name == name {
			return c
		}
	}
	return nil
}

// BuildMatcher collects the user agent sheet and the document's <style> elements.
func BuildMatcher(doc *xmldom.Node, loadCSS func(href string) string) *Matcher {
	m := &Matcher{}
	m.AddSheet(ParseStylesheet(uaCSS, "print"), originUA)
	root := doc
	if root.Type == xmldom.DocumentNode {
		root = root.DocumentElement()
	}
	if root != nil {
		if head := findChild(root, "head"); head != nil {
			for _, c := range head.Children {
				if !isElem(c) {
					continue
				}
				switch c.Name {
				case "style":
					media := strings.ToLower(strings.TrimSpace(c.Attr("media")))
					if media == "" {
						media = "all"
					}
					if !mediaMatches(media, "print") {
						continue
					}
					css := strings.TrimSpace(elementText(c))
					if css != "" {
						m.AddSheet(ParseStylesheet(css, "print"), originAuthor)
					}
				case "link":
					rel := strings.ToLower(c.Attr("rel"))
					if strings.Contains(rel, "alternate") || !strings.Contains(rel, "stylesheet") {
						continue
					}
					t := c.Attr("type")
					if !(t == "" || t == "text/css") {
						continue
					}
					media := strings.ToLower(strings.TrimSpace(c.Attr("media")))
					if media == "" {
						media = "all"
					}
					if !mediaMatches(media, "print") || loadCSS == nil {
						continue
					}
					if css := loadCSS(c.Attr("href")); css != "" {
						m.AddSheet(ParseStylesheet(css, "print"), originAuthor)
					}
				}
			}
		}
	}
	m.Finish()
	return m
}
