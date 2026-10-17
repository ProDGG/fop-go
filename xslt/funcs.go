package xslt

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ProDGG/fop-go/xmldom"
	"github.com/ProDGG/fop-go/xpath"
)

var instructionNames = map[string]bool{
	"apply-templates": true, "call-template": true, "apply-imports": true, "for-each": true, "value-of": true,
	"copy-of": true, "number": true, "choose": true, "if": true, "text": true, "copy": true, "variable": true,
	"message": true, "fallback": true, "element": true, "attribute": true, "comment": true,
	"processing-instruction": true, "sort": true, "param": true, "with-param": true,
}

// callFunc implements XSLT-specific functions for the XPath evaluator.
func (t *Transformer) callFunc(ctx *xpath.Context, uri, local string, args []xpath.Value) (xpath.Value, bool, error) {
	if uri != "" && uri != xmldom.XSLNS {
		return nil, false, nil
	}
	c, _ := ctx.Data.(*execCtx)
	switch local {
	case "current":
		if ctx.Current == nil {
			return xpath.NodeSet{}, true, nil
		}
		return xpath.NodeSet{ctx.Current}, true, nil
	case "generate-id":
		n := ctx.Node
		if len(args) > 0 {
			ns, ok := args[0].(xpath.NodeSet)
			if !ok {
				return nil, true, fmt.Errorf("generate-id: argument must be a node-set")
			}
			if len(ns) == 0 {
				return "", true, nil
			}
			n = ns[0]
		}
		return "id" + strconv.FormatUint(n.Order(), 36), true, nil
	case "key":
		if len(args) != 2 {
			return nil, true, fmt.Errorf("key() requires 2 arguments")
		}
		v, err := t.key(c, ctx, xpath.ToString(args[0]), args[1])
		return v, true, err
	case "document":
		if len(args) < 1 {
			return nil, true, fmt.Errorf("document() requires an argument")
		}
		v, err := t.document(args)
		return v, true, err
	case "format-number":
		if len(args) < 2 {
			return nil, true, fmt.Errorf("format-number() requires 2 or 3 arguments")
		}
		name := ""
		if len(args) > 2 {
			name = xpath.ToString(args[2])
		}
		df, ok := t.s.formats[name]
		if !ok {
			return nil, true, fmt.Errorf("format-number: unknown decimal-format %q", name)
		}
		s, err := FormatNumber(xpath.ToNumber(args[0]), xpath.ToString(args[1]), df)
		return s, true, err
	case "system-property":
		switch xpath.ToString(argAt(args, 0)) {
		case "xsl:version":
			return "1.0", true, nil
		case "xsl:vendor":
			return "fop-go", true, nil
		case "xsl:vendor-url":
			return "https://github.com/ProDGG/fop-go", true, nil
		}
		return "", true, nil
	case "element-available":
		name := xpath.ToString(argAt(args, 0))
		if i := strings.IndexByte(name, ':'); i >= 0 {
			name = name[i+1:]
		}
		return instructionNames[name], true, nil
	case "function-available":
		name := xpath.ToString(argAt(args, 0))
		switch name {
		case "current", "generate-id", "key", "document", "format-number", "system-property",
			"element-available", "function-available", "unparsed-entity-uri":
			return true, true, nil
		}
		return xpath.HasCoreFunction(name), true, nil
	case "unparsed-entity-uri":
		return "", true, nil
	}
	return nil, false, nil
}

func argAt(args []xpath.Value, i int) xpath.Value {
	if i < len(args) {
		return args[i]
	}
	return ""
}

func (t *Transformer) key(c *execCtx, ctx *xpath.Context, name string, val xpath.Value) (xpath.Value, error) {
	defs, ok := t.s.keys[name]
	if !ok {
		for k, d := range t.s.keys {
			if strings.HasSuffix(k, "}"+name) || strings.HasSuffix(name, ":"+k) {
				defs, ok = d, true
				name = k
				break
			}
		}
	}
	if !ok {
		return nil, fmt.Errorf("key(): no xsl:key named %q", name)
	}
	if c == nil {
		return nil, fmt.Errorf("key(): no execution context")
	}
	root := ctx.Node.Root()
	byName := t.keyIdx[root]
	if byName == nil {
		byName = map[string]map[string][]*xmldom.Node{}
		t.keyIdx[root] = byName
	}
	idx := byName[name]
	if idx == nil {
		idx = map[string][]*xmldom.Node{}
		var errOut error
		var walk func(n *xmldom.Node)
		visit := func(n *xmldom.Node) {
			for _, d := range defs {
				sc := c.sub(n, 1, 1)
				if !d.match.Match(sc.xp(), n) {
					continue
				}
				v, err := sc.eval(d.use)
				if err != nil {
					errOut = err
					return
				}
				if ns, ok := v.(xpath.NodeSet); ok {
					seen := map[string]bool{}
					for _, x := range ns {
						sv := x.StringValue()
						if !seen[sv] {
							seen[sv] = true
							idx[sv] = append(idx[sv], n)
						}
					}
				} else {
					sv := xpath.ToString(v)
					idx[sv] = append(idx[sv], n)
				}
			}
		}
		walk = func(n *xmldom.Node) {
			visit(n)
			for _, a := range n.Attrs {
				visit(a)
			}
			for _, ch := range n.Children {
				walk(ch)
			}
		}
		walk(root)
		if errOut != nil {
			return nil, errOut
		}
		byName[name] = idx
	}
	var out []*xmldom.Node
	if ns, ok := val.(xpath.NodeSet); ok {
		for _, n := range ns {
			out = append(out, idx[n.StringValue()]...)
		}
	} else {
		out = append(out, idx[xpath.ToString(val)]...)
	}
	return xpath.SortNodes(out), nil
}

func (t *Transformer) loadDoc(href string) (*xmldom.Node, error) {
	if href == "" {
		return t.s.doc, nil
	}
	if d, ok := t.docs[href]; ok {
		return d, nil
	}
	var lastErr error
	for _, base := range []string{t.s.opts.BaseURI, t.baseURI} {
		data, loc, err := t.s.opts.Resolver.Resolve(href, base)
		if err != nil {
			lastErr = err
			continue
		}
		doc, err := xmldom.ParseBytes(data)
		if err != nil {
			return nil, fmt.Errorf("document(%q): %w", href, err)
		}
		t.s.StripSpace(doc)
		t.docs[href] = doc
		t.docs[loc] = doc
		return doc, nil
	}
	return nil, fmt.Errorf("document(%q): %w", href, lastErr)
}

func (t *Transformer) document(args []xpath.Value) (xpath.Value, error) {
	var hrefs []string
	if ns, ok := args[0].(xpath.NodeSet); ok {
		for _, n := range ns {
			hrefs = append(hrefs, n.StringValue())
		}
	} else {
		hrefs = []string{xpath.ToString(args[0])}
	}
	var out []*xmldom.Node
	for _, h := range hrefs {
		d, err := t.loadDoc(h)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return xpath.SortNodes(out), nil
}
