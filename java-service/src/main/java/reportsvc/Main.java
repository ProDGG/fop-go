package reportsvc;

import com.sun.net.httpserver.HttpExchange;
import com.sun.net.httpserver.HttpServer;

import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.InetSocketAddress;
import java.net.URLDecoder;
import java.nio.charset.StandardCharsets;
import java.nio.file.Path;
import java.util.HashMap;
import java.util.Map;
import java.util.concurrent.Executors;

/**
 * Minimal HTTP front end:
 *
 * <pre>
 * POST /pdf?template=IndividualAdvancedReport.xslt[&amp;root=Root]   body: XML   -> application/pdf
 * (add &amp;format=html to get the HTML that is fed to the renderer)
 * GET  /health
 * </pre>
 *
 * Configuration (environment): PORT (8080), TEMPLATES_DIR (./templates),
 * FONTS_DIR (./fonts), MAX_BODY_MB (50), THREADS (4).
 */
public final class Main {
    public static void main(String[] args) throws Exception {
        int port = Integer.parseInt(env("PORT", "8080"));
        Path templates = Path.of(env("TEMPLATES_DIR", "templates")).toAbsolutePath().normalize();
        Path fonts = Path.of(env("FONTS_DIR", "fonts")).toAbsolutePath().normalize();
        long maxBody = Long.parseLong(env("MAX_BODY_MB", "50")) * 1024 * 1024;
        int threads = Integer.parseInt(env("THREADS", "4"));

        ReportRenderer renderer = new ReportRenderer(templates, fonts);
        HttpServer server = HttpServer.create(new InetSocketAddress(port), 0);
        server.createContext("/health", ex -> reply(ex, 200, "text/plain", "ok\n".getBytes(StandardCharsets.UTF_8)));
        server.createContext("/pdf", ex -> {
            try {
                if (!"POST".equals(ex.getRequestMethod())) {
                    reply(ex, 405, "text/plain", "POST only\n".getBytes(StandardCharsets.UTF_8));
                    return;
                }
                Map<String, String> q = query(ex.getRequestURI().getRawQuery());
                String template = q.get("template");
                if (template == null || template.isEmpty()) {
                    reply(ex, 400, "text/plain", "missing ?template=\n".getBytes(StandardCharsets.UTF_8));
                    return;
                }
                byte[] xml = readBody(ex.getRequestBody(), maxBody);
                if (xml == null) {
                    reply(ex, 413, "text/plain", "body too large\n".getBytes(StandardCharsets.UTF_8));
                    return;
                }
                if ("html".equals(q.get("format"))) {
                    reply(ex, 200, "text/html; charset=utf-8", renderer.renderHtml(xml, template, q.get("root")).getBytes(StandardCharsets.UTF_8));
                    return;
                }
                long t0 = System.nanoTime();
                byte[] pdf = renderer.render(xml, template, q.get("root"));
                System.err.printf("rendered %s: %d bytes in %d ms%n", template, pdf.length, (System.nanoTime() - t0) / 1_000_000);
                reply(ex, 200, "application/pdf", pdf);
            } catch (ReportRenderer.BadRequest e) {
                reply(ex, 400, "text/plain", (e.getMessage() + "\n").getBytes(StandardCharsets.UTF_8));
            } catch (Throwable e) {
                e.printStackTrace();
                reply(ex, 500, "text/plain", ("render failed: " + e + "\n").getBytes(StandardCharsets.UTF_8));
            }
        });
        server.setExecutor(Executors.newFixedThreadPool(threads));
        server.start();
        System.err.println("report-service listening on :" + port + ", templates=" + templates + ", fonts=" + fonts);
    }

    private static String env(String k, String def) {
        String v = System.getenv(k);
        return v == null || v.isEmpty() ? def : v;
    }

    private static Map<String, String> query(String raw) {
        Map<String, String> m = new HashMap<>();
        if (raw == null) return m;
        for (String kv : raw.split("&")) {
            int i = kv.indexOf('=');
            String k = i < 0 ? kv : kv.substring(0, i);
            String v = i < 0 ? "" : kv.substring(i + 1);
            m.put(URLDecoder.decode(k, StandardCharsets.UTF_8), URLDecoder.decode(v, StandardCharsets.UTF_8));
        }
        return m;
    }

    /** Reads at most max bytes; returns null when the body is larger. */
    private static byte[] readBody(InputStream in, long max) throws IOException {
        ByteArrayOutputStream out = new ByteArrayOutputStream();
        byte[] buf = new byte[65536];
        long total = 0;
        for (int n; (n = in.read(buf)) > 0; ) {
            total += n;
            if (total > max) return null;
            out.write(buf, 0, n);
        }
        return out.toByteArray();
    }

    private static void reply(HttpExchange ex, int code, String type, byte[] body) throws IOException {
        ex.getResponseHeaders().set("Content-Type", type);
        ex.sendResponseHeaders(code, body.length);
        try (OutputStream os = ex.getResponseBody()) {
            os.write(body);
        }
    }
}
