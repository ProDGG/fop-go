package reportsvc;

import com.itextpdf.text.Image;
import org.apache.batik.transcoder.TranscoderInput;
import org.apache.batik.transcoder.TranscoderOutput;
import org.apache.batik.transcoder.image.PNGTranscoder;
import org.w3c.dom.Element;
import org.xhtmlrenderer.extend.FSImage;
import org.xhtmlrenderer.extend.ReplacedElement;
import org.xhtmlrenderer.extend.UserAgentCallback;
import org.xhtmlrenderer.layout.LayoutContext;
import org.xhtmlrenderer.pdf.ITextFSImage;
import org.xhtmlrenderer.pdf.ITextImageElement;
import org.xhtmlrenderer.pdf.ITextOutputDevice;
import org.xhtmlrenderer.pdf.ITextReplacedElementFactory;
import org.xhtmlrenderer.render.BlockBox;

import java.io.ByteArrayInputStream;
import java.io.ByteArrayOutputStream;
import java.util.Base64;

/**
 * Flying Saucer cannot draw SVG. {@code <img src="data:image/svg+xml;base64,...">}
 * is rasterised with Batik at the SVG's own size and embedded as a bitmap.
 */
final class SvgImages extends ITextReplacedElementFactory {
    private static final String PREFIX = "data:image/svg+xml;base64,";

    SvgImages(ITextOutputDevice out) {
        super(out);
    }

    @Override
    public ReplacedElement createReplacedElement(LayoutContext c, BlockBox box, UserAgentCallback uac,
                                                 int cssWidth, int cssHeight) {
        Element e = box.getElement();
        if (e != null && "img".equals(e.getNodeName())) {
            String src = e.getAttribute("src");
            if (src.startsWith(PREFIX)) {
                try {
                    byte[] svg = Base64.getDecoder().decode(src.substring(PREFIX.length()));
                    FSImage img = new ITextFSImage(Image.getInstance(rasterise(svg)));
                    if (cssWidth != -1 || cssHeight != -1) img.scale(cssWidth, cssHeight);
                    return new ITextImageElement(img);
                } catch (Exception ex) {
                    System.err.println("svg image skipped: " + ex);
                    // fall through: Flying Saucer will report it as a broken image
                }
            }
        }
        return super.createReplacedElement(c, box, uac, cssWidth, cssHeight);
    }

    private static byte[] rasterise(byte[] svg) throws Exception {
        PNGTranscoder t = new PNGTranscoder();
        ByteArrayOutputStream out = new ByteArrayOutputStream();
        t.transcode(new TranscoderInput(new ByteArrayInputStream(svg)), new TranscoderOutput(out));
        // re-encode without Batik's pHYs chunk (it would make iText treat the bitmap as 96 dpi)
        java.awt.image.BufferedImage bi = javax.imageio.ImageIO.read(new ByteArrayInputStream(out.toByteArray()));
        ByteArrayOutputStream plain = new ByteArrayOutputStream();
        javax.imageio.ImageIO.write(bi, "png", plain);
        return plain.toByteArray();
    }
}
