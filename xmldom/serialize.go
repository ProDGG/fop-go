package xmldom

import (
	"io"
	"sort"
	"strings"
)

// Serialize writes the node as XML. Namespace declarations are synthesised
// from the prefix/URI of elements and attributes where the tree does not
// already carry them.
func Serialize(w io.Writer, n *Node, indent bool) error {
	s := &serializer{w: w, indent: indent}
	s.node(n, map[string]string{"xml": XMLNS}, 0)
	return s.err
}

// String returns the serialisation of n.
func (n *Node) String() string {
	var sb strings.Builder
	_ = Serialize(&sb, n, false)
	return sb.String()
}

type serializer struct {
	w      io.Writer
	indent bool
	err    error
}

func (s *serializer) write(str string) {
	if s.err == nil {
		_, s.err = io.WriteString(s.w, str)
	}
}

func escapeText(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}

func escapeAttr(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "\n", "&#10;", "\r", "&#13;", "\t", "&#9;")
	return r.Replace(s)
}

func (s *serializer) node(n *Node, scope map[string]string, depth int) {
	switch n.Type {
	case DocumentNode:
		s.write(`<?xml version="1.0" encoding="UTF-8"?>`)
		if s.indent {
			s.write("\n")
		}
		for _, c := range n.Children {
			s.node(c, scope, 0)
		}
	case TextNode:
		s.write(escapeText(n.Value))
	case CommentNode:
		s.write("<!--" + n.Value + "-->")
	case PINode:
		s.write("<?" + n.Name + " " + n.Value + "?>")
	case AttributeNode:
		s.write(n.QName() + `="` + escapeAttr(n.Value) + `"`)
	case ElementNode:
		if s.indent && depth > 0 {
			s.write("\n" + strings.Repeat("  ", depth))
		}
		local := make(map[string]string, len(scope))
		for k, v := range scope {
			local[k] = v
		}
		var decls []NSDecl
		declare := func(prefix, uri string) {
			if cur, ok := local[prefix]; ok && cur == uri {
				return
			}
			if prefix == "" && uri == "" {
				if _, ok := local[""]; !ok {
					return
				}
			}
			local[prefix] = uri
			decls = append(decls, NSDecl{prefix, uri})
		}
		declare(n.Prefix, n.URI)
		for _, a := range n.Attrs {
			if a.URI != "" && a.Prefix != "" {
				declare(a.Prefix, a.URI)
			}
		}
		sort.SliceStable(decls, func(i, j int) bool { return decls[i].Prefix < decls[j].Prefix })
		s.write("<" + n.QName())
		for _, d := range decls {
			if d.Prefix == "" {
				s.write(` xmlns="` + escapeAttr(d.URI) + `"`)
			} else {
				s.write(" xmlns:" + d.Prefix + `="` + escapeAttr(d.URI) + `"`)
			}
		}
		for _, a := range n.Attrs {
			s.write(" ")
			s.node(a, local, 0)
		}
		if len(n.Children) == 0 {
			s.write("/>")
			return
		}
		s.write(">")
		hasElem := false
		for _, c := range n.Children {
			if c.Type == ElementNode {
				hasElem = true
			}
		}
		onlyElems := s.indent && hasElem
		if onlyElems {
			for _, c := range n.Children {
				if c.Type == TextNode && strings.TrimSpace(c.Value) != "" {
					onlyElems = false
				}
			}
		}
		for _, c := range n.Children {
			if onlyElems && c.Type == TextNode {
				continue
			}
			if onlyElems {
				s.node(c, local, depth+1)
			} else {
				s.node(c, local, -1<<20)
			}
		}
		if onlyElems {
			s.write("\n" + strings.Repeat("  ", depth))
		}
		s.write("</" + n.QName() + ">")
	}
}
