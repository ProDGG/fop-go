package reportsvc;

import org.w3c.dom.Attr;
import org.w3c.dom.Element;
import org.w3c.dom.NamedNodeMap;
import org.w3c.dom.Node;

import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.TreeMap;

/**
 * Serialises a DOM tree as XML with <code>indent="yes"</code> semantics of the
 * reference pipeline: an element whose children are only elements (and blank
 * text) is indented; as soon as an element has real text, it and everything
 * below it is written verbatim. (The JDK's own indenting serializer also
 * pads mixed content, which changes the text that reaches the renderer.)
 */
final class IndentedXml {
    private final StringBuilder sb = new StringBuilder(1 << 16);

    static String write(Node doc) {
        IndentedXml w = new IndentedXml();
        Map<String, String> scope = new HashMap<>();
        scope.put("xml", "http://www.w3.org/XML/1998/namespace");
        for (Node c = doc.getFirstChild(); c != null; c = c.getNextSibling()) {
            if (c instanceof Element e) w.element(e, scope, 0);
        }
        return w.sb.toString();
    }

    private void element(Element e, Map<String, String> scope, int depth) {
        if (depth > 0) sb.append('\n').append("  ".repeat(depth));
        Map<String, String> local = new HashMap<>(scope);
        TreeMap<String, String> decls = new TreeMap<>();
        declare(e.getPrefix(), e.getNamespaceURI(), local, decls);
        NamedNodeMap attrs = e.getAttributes();
        List<Attr> plain = new ArrayList<>();
        for (int i = 0; i < attrs.getLength(); i++) {
            Attr a = (Attr) attrs.item(i);
            String n = a.getName();
            if (n.equals("xmlns") || n.startsWith("xmlns:")) continue;
            if (a.getNamespaceURI() != null && a.getPrefix() != null) declare(a.getPrefix(), a.getNamespaceURI(), local, decls);
            plain.add(a);
        }
        String qn = e.getTagName();
        sb.append('<').append(qn);
        decls.forEach((p, u) -> sb.append(p.isEmpty() ? " xmlns=\"" : " xmlns:" + p + "=\"").append(attr(u)).append('"'));
        for (Attr a : plain) sb.append(' ').append(a.getName()).append("=\"").append(attr(a.getValue())).append('"');
        if (e.getFirstChild() == null) {
            sb.append("/>");
            return;
        }
        sb.append('>');
        boolean hasElem = false, blankOnly = true;
        for (Node c = e.getFirstChild(); c != null; c = c.getNextSibling()) {
            if (c instanceof Element) hasElem = true;
            else if (c.getNodeType() == Node.TEXT_NODE || c.getNodeType() == Node.CDATA_SECTION_NODE) {
                if (!c.getNodeValue().isBlank()) blankOnly = false;
            }
        }
        boolean onlyElems = hasElem && blankOnly && depth >= 0;
        for (Node c = e.getFirstChild(); c != null; c = c.getNextSibling()) {
            int t = c.getNodeType();
            if (t == Node.ELEMENT_NODE) {
                element((Element) c, local, onlyElems ? depth + 1 : Integer.MIN_VALUE / 2);
            } else if (onlyElems) {
                // blank text between elements is replaced by the indentation
            } else if (t == Node.TEXT_NODE || t == Node.CDATA_SECTION_NODE) {
                sb.append(text(c.getNodeValue()));
            } else if (t == Node.COMMENT_NODE) {
                sb.append("<!--").append(c.getNodeValue()).append("-->");
            } else if (t == Node.PROCESSING_INSTRUCTION_NODE && !c.getNodeName().startsWith("javax.xml.transform.")) {
                // (the JDK marks disable-output-escaping text with PIs of that kind; they are dropped)
                sb.append("<?").append(c.getNodeName()).append(' ').append(c.getNodeValue()).append("?>");
            }
        }
        if (onlyElems) sb.append('\n').append("  ".repeat(depth));
        sb.append("</").append(qn).append('>');
    }

    private static void declare(String prefix, String uri, Map<String, String> local, Map<String, String> decls) {
        String p = prefix == null ? "" : prefix;
        String u = uri == null ? "" : uri;
        String cur = local.get(p);
        if (cur != null && cur.equals(u)) return;
        if (p.isEmpty() && u.isEmpty() && cur == null) return;
        local.put(p, u);
        decls.put(p, u);
    }

    private static String text(String s) {
        return s.replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;");
    }

    private static String attr(String s) {
        return s.replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;").replace("\"", "&quot;")
                .replace("\n", "&#10;").replace("\r", "&#13;").replace("\t", "&#9;");
    }
}
