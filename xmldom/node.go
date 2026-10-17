// Package xmldom implements a small namespace-aware XML document model that is
// shared by the XPath and XSLT engines and by the XSL-FO reader.
package xmldom

import (
	"strings"
	"sync/atomic"
)

// NodeType is the kind of a Node (XPath data model).
type NodeType int

const (
	DocumentNode NodeType = iota + 1
	ElementNode
	TextNode
	AttributeNode
	CommentNode
	PINode
	NamespaceNode
)

// Well-known namespace URIs.
const (
	XMLNS = "http://www.w3.org/XML/1998/namespace"
	XSLNS = "http://www.w3.org/1999/XSL/Transform"
	FONS  = "http://www.w3.org/1999/XSL/Format"
)

// NSDecl is an xmlns declaration made on an element.
type NSDecl struct{ Prefix, URI string }

var orderCounter uint64

// Node is a node of an XML tree. A single struct is used for all node kinds.
type Node struct {
	Type     NodeType
	Name     string // local name (PI target for PINode, prefix for NamespaceNode)
	Prefix   string
	URI      string
	Value    string // text, attribute value, comment text, PI data, namespace URI
	Parent   *Node
	Children []*Node
	Attrs    []*Node
	NS       []NSDecl // namespace declarations made on this element
	Line     int
	order    uint64
}

func newNode(t NodeType) *Node {
	return &Node{Type: t, order: atomic.AddUint64(&orderCounter, 1)}
}

// NewDocument creates an empty document node.
func NewDocument() *Node { return newNode(DocumentNode) }

// NewElement creates a detached element node.
func NewElement(prefix, local, uri string) *Node {
	n := newNode(ElementNode)
	n.Prefix, n.Name, n.URI = prefix, local, uri
	return n
}

// NewText creates a detached text node.
func NewText(s string) *Node {
	n := newNode(TextNode)
	n.Value = s
	return n
}

// NewComment creates a detached comment node.
func NewComment(s string) *Node {
	n := newNode(CommentNode)
	n.Value = s
	return n
}

// NewPI creates a detached processing instruction node.
func NewPI(target, data string) *Node {
	n := newNode(PINode)
	n.Name, n.Value = target, data
	return n
}

// NewAttr creates a detached attribute node.
func NewAttr(prefix, local, uri, value string) *Node {
	n := newNode(AttributeNode)
	n.Prefix, n.Name, n.URI, n.Value = prefix, local, uri, value
	return n
}

// Order returns a number that sorts nodes into document order for nodes of
// the same tree.
func (n *Node) Order() uint64 { return n.order }

// QName returns the prefixed name of an element or attribute.
func (n *Node) QName() string {
	if n.Prefix != "" {
		return n.Prefix + ":" + n.Name
	}
	return n.Name
}

// AppendChild adds c as the last child of n. Adjacent text nodes are merged.
func (n *Node) AppendChild(c *Node) {
	if c.Type == TextNode && len(n.Children) > 0 {
		last := n.Children[len(n.Children)-1]
		if last.Type == TextNode {
			last.Value += c.Value
			return
		}
	}
	c.Parent = n
	n.Children = append(n.Children, c)
}

// SetAttr sets (or replaces) an attribute on an element.
func (n *Node) SetAttr(prefix, local, uri, value string) {
	for _, a := range n.Attrs {
		if a.Name == local && a.URI == uri {
			a.Value = value
			a.Prefix = prefix
			return
		}
	}
	a := NewAttr(prefix, local, uri, value)
	a.Parent = n
	n.Attrs = append(n.Attrs, a)
}

// Attr returns the value of the no-namespace attribute with the given name.
func (n *Node) Attr(local string) string {
	v, _ := n.AttrOK(local)
	return v
}

// AttrOK is like Attr but also reports whether the attribute exists.
func (n *Node) AttrOK(local string) (string, bool) {
	for _, a := range n.Attrs {
		if a.Name == local && a.URI == "" {
			return a.Value, true
		}
	}
	return "", false
}

// AttrNS returns the value of the namespaced attribute.
func (n *Node) AttrNS(uri, local string) string {
	for _, a := range n.Attrs {
		if a.Name == local && a.URI == uri {
			return a.Value
		}
	}
	return ""
}

// Root returns the topmost ancestor of n (the document node for attached nodes).
func (n *Node) Root() *Node {
	for n.Parent != nil {
		n = n.Parent
	}
	return n
}

// DocumentElement returns the root element of a document node.
func (n *Node) DocumentElement() *Node {
	for _, c := range n.Children {
		if c.Type == ElementNode {
			return c
		}
	}
	return nil
}

// StringValue returns the XPath string-value of the node.
func (n *Node) StringValue() string {
	switch n.Type {
	case TextNode, AttributeNode, CommentNode, PINode, NamespaceNode:
		return n.Value
	}
	var sb strings.Builder
	n.collectText(&sb)
	return sb.String()
}

func (n *Node) collectText(sb *strings.Builder) {
	for _, c := range n.Children {
		switch c.Type {
		case TextNode:
			sb.WriteString(c.Value)
		case ElementNode:
			c.collectText(sb)
		}
	}
}

// LookupNamespace resolves a prefix to a namespace URI using the in-scope
// declarations of n (or its nearest element ancestor).
func (n *Node) LookupNamespace(prefix string) (string, bool) {
	if prefix == "xml" {
		return XMLNS, true
	}
	for e := n; e != nil; e = e.Parent {
		if e.Type == AttributeNode {
			continue
		}
		for _, d := range e.NS {
			if d.Prefix == prefix {
				return d.URI, d.URI != "" || prefix == ""
			}
		}
	}
	return "", false
}

// InScopeNamespaces returns all prefix declarations visible on n.
func (n *Node) InScopeNamespaces() map[string]string {
	var chain []*Node
	for e := n; e != nil; e = e.Parent {
		chain = append(chain, e)
	}
	m := map[string]string{"xml": XMLNS}
	for i := len(chain) - 1; i >= 0; i-- {
		for _, d := range chain[i].NS {
			if d.URI == "" {
				delete(m, d.Prefix)
			} else {
				m[d.Prefix] = d.URI
			}
		}
	}
	return m
}

// IsElement reports whether n is an element with the given namespace and local name.
func (n *Node) IsElement(uri, local string) bool {
	return n.Type == ElementNode && n.URI == uri && n.Name == local
}

// FirstChildElement returns the first child element (any name).
func (n *Node) ChildElements() []*Node {
	var out []*Node
	for _, c := range n.Children {
		if c.Type == ElementNode {
			out = append(out, c)
		}
	}
	return out
}

// DeepCopy clones the subtree rooted at n; the copy is detached.
func (n *Node) DeepCopy() *Node {
	c := newNode(n.Type)
	c.Name, c.Prefix, c.URI, c.Value, c.Line = n.Name, n.Prefix, n.URI, n.Value, n.Line
	c.NS = append([]NSDecl(nil), n.NS...)
	for _, a := range n.Attrs {
		ca := a.DeepCopy()
		ca.Parent = c
		c.Attrs = append(c.Attrs, ca)
	}
	for _, ch := range n.Children {
		cc := ch.DeepCopy()
		cc.Parent = c
		c.Children = append(c.Children, cc)
	}
	return c
}

// CompareOrder returns -1, 0, 1 for a before, equal, after b in document order.
func CompareOrder(a, b *Node) int {
	switch {
	case a == b:
		return 0
	case a.order < b.order:
		return -1
	default:
		return 1
	}
}
