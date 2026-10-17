package fo

import (
	"fmt"
	"strings"

	"github.com/ProDGG/fop-go/xmldom"
)

// FOXNS is the Apache FOP extension namespace.
const FOXNS = "http://xmlgraphics.apache.org/fop/extensions"

var structural = map[string]bool{
	"root": true, "layout-master-set": true, "declarations": true, "simple-page-master": true,
	"page-sequence-master": true, "repeatable-page-master-alternatives": true, "page-sequence": true,
	"flow": true, "static-content": true, "table-and-caption": true, "table": true, "table-header": true,
	"table-footer": true, "table-body": true, "table-row": true, "list-block": true, "list-item": true,
	"bookmark-tree": true, "bookmark": true, "region-body": true, "region-before": true, "region-after": true,
	"region-start": true, "region-end": true, "table-column": true, "multi-switch": true,
	"conditional-page-master-reference": true, "single-page-master-reference": true,
	"repeatable-page-master-reference": true, "color-profile": true,
}

// BuildTree converts the FO result document into a Node tree.
func BuildTree(doc *xmldom.Node) (*Node, error) {
	root := doc.DocumentElement()
	if root == nil {
		return nil, fmt.Errorf("fo: empty document")
	}
	if root.URI != xmldom.FONS || root.Name != "root" {
		return nil, fmt.Errorf("fo: root element must be fo:root in namespace %s, found <%s> in %q", xmldom.FONS, root.QName(), root.URI)
	}
	return buildNode(root, nil), nil
}

func buildNode(el *xmldom.Node, parent *Node) *Node {
	n := &Node{Name: el.Name, Parent: parent, Src: el}
	if el.URI == FOXNS {
		n.Name = "fox:" + el.Name
	}
	var attrs [][2]string
	for _, a := range el.Attrs {
		switch {
		case a.URI == "":
			attrs = append(attrs, [2]string{a.Name, strings.TrimSpace(a.Value)})
		case a.URI == xmldom.XMLNS && a.Name == "lang":
			attrs = append(attrs, [2]string{"language", a.Value})
		case a.URI == FOXNS:
			attrs = append(attrs, [2]string{"fox:" + a.Name, a.Value})
		}
	}
	n.Props = setProps(attrs)
	if n.Name == "instream-foreign-object" {
		return n // foreign content is kept in Src
	}
	for _, c := range el.Children {
		switch c.Type {
		case xmldom.TextNode:
			if structural[n.Name] && strings.TrimSpace(c.Value) == "" {
				continue
			}
			if n.Name == "#text" {
				continue
			}
			n.Children = append(n.Children, &Node{Name: "#text", Text: c.Value, Parent: n})
		case xmldom.ElementNode:
			if c.URI == xmldom.FONS || c.URI == FOXNS {
				n.Children = append(n.Children, buildNode(c, n))
			}
		}
	}
	return n
}

// Find returns the first child element with the given name.
func (n *Node) Find(name string) *Node {
	for _, c := range n.Children {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// FindAll returns all child elements with the given name.
func (n *Node) FindAll(name string) []*Node {
	var out []*Node
	for _, c := range n.Children {
		if c.Name == name {
			out = append(out, c)
		}
	}
	return out
}

// TextContent concatenates the text of the subtree.
func (n *Node) TextContent() string {
	if n.Name == "#text" {
		return n.Text
	}
	var sb strings.Builder
	for _, c := range n.Children {
		sb.WriteString(c.TextContent())
	}
	return sb.String()
}

// cloneUnder deep-copies n's subtree, re-parenting the copy under parent.
func (n *Node) cloneUnder(parent *Node) *Node {
	c := &Node{Name: n.Name, Text: n.Text, Props: n.Props, Parent: parent, Src: n.Src}
	for _, ch := range n.Children {
		c.Children = append(c.Children, ch.cloneUnder(c))
	}
	return c
}
