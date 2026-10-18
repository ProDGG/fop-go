package reportsvc;

import com.itextpdf.text.pdf.BaseFont;
import org.w3c.dom.Document;
import org.w3c.dom.Element;
import org.w3c.dom.Node;
import org.xhtmlrenderer.pdf.ITextRenderer;
import org.xml.sax.InputSource;

import javax.xml.XMLConstants;
import javax.xml.parsers.DocumentBuilder;
import javax.xml.parsers.DocumentBuilderFactory;
import javax.xml.transform.Templates;
import javax.xml.transform.TransformerFactory;
import javax.xml.transform.dom.DOMResult;
import javax.xml.transform.dom.DOMSource;
import javax.xml.transform.stream.StreamSource;
import java.io.ByteArrayInputStream;
import java.io.ByteArrayOutputStream;
import java.io.StringReader;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.ConcurrentMap;

/**
 * XML + XSLT -> HTML -> PDF, the way the production pipeline does it:
 * the stylesheet result is serialised (indent) and parsed again by Flying
 * Saucer, fonts are registered as "Arial", PDF comes from iText 5.
 */
final class ReportRenderer {
    static final class BadRequest extends RuntimeException {
        BadRequest(String m) { super(m); }
    }

    private record Compiled(Templates templates, long mtime, long size) {}

    private final Path templatesDir;
    private final Path fontsDir;
    private final ConcurrentMap<Path, Compiled> cache = new ConcurrentHashMap<>();

    ReportRenderer(Path templatesDir, Path fontsDir) {
        this.templatesDir = templatesDir;
        this.fontsDir = fontsDir;
    }

    /** The HTML that Flying Saucer is given (for debugging templates). */
    String renderHtml(byte[] xml, String templateName, String rootName) throws Exception {
        return transform(source(xml, rootName), compile(template(templateName)));
    }

    byte[] render(byte[] xml, String templateName, String rootName) throws Exception {
        return toPdf(renderHtml(xml, templateName, rootName));
    }

    private Path template(String templateName) {
        Path tpl = templatesDir.resolve(templateName).normalize();
        if (!tpl.startsWith(templatesDir) || !Files.isRegularFile(tpl)) {
            throw new BadRequest("unknown template: " + templateName);
        }
        return tpl;
    }

    private Document source(byte[] xml, String rootName) throws Exception {
        Document source = parse(xml);
        if (rootName != null && !rootName.isEmpty()) source = promoteRoot(source, rootName);
        return source;
    }

    // ---- XML ----

    private static Document parse(byte[] xml) throws Exception {
        DocumentBuilderFactory f = DocumentBuilderFactory.newInstance();
        f.setNamespaceAware(true);
        f.setFeature(XMLConstants.FEATURE_SECURE_PROCESSING, true);
        f.setFeature("http://apache.org/xml/features/disallow-doctype-decl", true);
        f.setXIncludeAware(false);
        f.setExpandEntityReferences(false);
        DocumentBuilder b = f.newDocumentBuilder();
        try {
            return b.parse(new ByteArrayInputStream(xml));
        } catch (org.xml.sax.SAXException e) {
            throw new BadRequest("invalid XML: " + e.getMessage());
        }
    }

    /** Makes the first element called name the document element (reports are often wrapped in an envelope). */
    private static Document promoteRoot(Document src, String name) throws Exception {
        Element found = find(src.getDocumentElement(), name);
        if (found == null) throw new BadRequest("no <" + name + "> element in the XML");
        if (found == src.getDocumentElement()) return src;
        Document doc = src.getImplementation().createDocument(null, null, null);
        doc.appendChild(doc.importNode(found, true));
        return doc;
    }

    private static Element find(Element e, String name) {
        if (name.equals(e.getLocalName() != null ? e.getLocalName() : e.getTagName())) return e;
        for (Node c = e.getFirstChild(); c != null; c = c.getNextSibling()) {
            if (c instanceof Element ce) {
                Element r = find(ce, name);
                if (r != null) return r;
            }
        }
        return null;
    }

    // ---- XSLT ----

    private Templates compile(Path tpl) throws Exception {
        long mtime = Files.getLastModifiedTime(tpl).toMillis(), size = Files.size(tpl);
        Compiled c = cache.get(tpl);
        if (c != null && c.mtime == mtime && c.size == size) return c.templates;
        // PATH_TO_FILE in xsl:import/xsl:include is replaced by the stylesheet's directory
        String text = Files.readString(tpl, StandardCharsets.UTF_8)
                .replace("PATH_TO_FILE", tpl.getParent().toUri().toString().replaceAll("/$", ""));
        TransformerFactory tf = TransformerFactory.newInstance();
        StreamSource src = new StreamSource(new StringReader(text), tpl.toUri().toString());
        Templates t = tf.newTemplates(src);
        cache.put(tpl, new Compiled(t, mtime, size));
        return t;
    }

    /**
     * Runs the stylesheet into a DOM (whatever xsl:output says), then serialises
     * that tree as indented XML, which is what Flying Saucer parses.
     */
    private static String transform(Document source, Templates t) throws Exception {
        DOMResult dom = new DOMResult();
        t.newTransformer().transform(new DOMSource(source), dom);
        return IndentedXml.write(dom.getNode());
    }

    // ---- HTML -> PDF ----

    private byte[] toPdf(String html) throws Exception {
        ITextRenderer r = new ITextRenderer();
        r.getSharedContext().setReplacedElementFactory(new SvgImages(r.getOutputDevice()));
        registerFonts(r);
        r.setDocumentFromString(html, templatesDir.toUri().toString());
        r.layout();
        ByteArrayOutputStream out = new ByteArrayOutputStream();
        r.createPDF(out);
        return out.toByteArray();
    }

    private static final String[][] ARIAL = {
            {"arial.ttf", "LiberationSans-Regular.ttf"},
            {"arialbd.ttf", "LiberationSans-Bold.ttf"},
            {"ariali.ttf", "LiberationSans-Italic.ttf"},
            {"arialbi.ttf", "LiberationSans-BoldItalic.ttf"},
    };

    /**
     * Registers four faces under the family "Arial": arial.ttf / arialbd.ttf /
     * ariali.ttf / arialbi.ttf from FONTS_DIR, falling back to the matching
     * Liberation Sans files (LiberationSans-Regular.ttf and so on).
     */
    private void registerFonts(ITextRenderer r) throws Exception {
        for (String[] names : ARIAL) {
            Path p = fontsDir.resolve(names[0]);
            if (!Files.isRegularFile(p)) p = fontsDir.resolve(names[1]);
            if (Files.isRegularFile(p)) {
                r.getFontResolver().addFont(p.toString(), "Arial", BaseFont.IDENTITY_H, BaseFont.EMBEDDED, null);
            }
        }
    }
}
