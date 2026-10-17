#!/usr/bin/env python3
"""Compare word positions of two PDFs (e.g. fop-go output vs Apache FOP output).

Usage: comparepdf.py mine.pdf reference.pdf [tolerance_pt]
Requires poppler's pdftotext. Prints per-page line-break differences and the
largest horizontal / vertical deviations of matching words.
"""
import re, subprocess, sys, html

def words(path):
    out = subprocess.run(['pdftotext', '-bbox', path, '-'], capture_output=True, text=True).stdout
    pages, cur = [], None
    for line in out.splitlines():
        if line.strip().startswith('<page'):
            cur = []; pages.append(cur)
        m = re.match(r'\s*<word xMin="([\d.]+)" yMin="([\d.]+)" xMax="([\d.]+)" yMax="([\d.]+)">(.*)</word>', line)
        if m and cur is not None:
            x0, y0, x1, y1 = map(float, m.groups()[:4])
            cur.append((html.unescape(m.group(5)), x0, y0, x1, y1))
    return pages

def lines(ws, tol=2.0):
    ws = sorted(ws, key=lambda w: (round(w[2] / tol), w[1]))
    out, cur, cy = [], [], None
    for w in ws:
        if cy is None or abs(w[2] - cy) > tol:
            if cur: out.append(cur)
            cur, cy = [], w[2]
        cur.append(w)
    if cur: out.append(cur)
    return out

def baselines_by_page(path):
    sys.path.insert(0, __file__.rsplit('/', 1)[0])
    import baselines as bl
    res = []
    for c in bl.content(path):
        res.append(sorted({(round(y, 2), sz) for x, y, sz, m in bl.run(c, 0)}, reverse=True))
    return res

def main():
    a, b = words(sys.argv[1]), words(sys.argv[2])
    tol = float(sys.argv[3]) if len(sys.argv) > 3 else 0.5
    print(f'pages: mine={len(a)} ref={len(b)}')
    total = bad_lines = 0
    maxdx = maxdy = 0.0
    for p in range(min(len(a), len(b))):
        la, lb = lines(a[p]), lines(b[p])
        ta = [' '.join(w[0] for w in l) for l in la]
        tb = [' '.join(w[0] for w in l) for l in lb]
        for i in range(max(len(ta), len(tb))):
            x = ta[i] if i < len(ta) else '<none>'
            y = tb[i] if i < len(tb) else '<none>'
            total += 1
            if x != y:
                bad_lines += 1
                if bad_lines <= 12:
                    print(f'p{p+1} line {i+1} differs:\n   mine: {x[:110]}\n   ref : {y[:110]}')
            else:
                for wa, wb in zip(la[i], lb[i]):
                    maxdx = max(maxdx, abs(wa[1] - wb[1])); maxdy = max(maxdy, abs(wa[2] - wb[2]))
    ba, bb = baselines_by_page(sys.argv[1]), baselines_by_page(sys.argv[2])
    bdiff = 0.0; bmis = 0
    for p in range(min(len(ba), len(bb))):
        if len(ba[p]) != len(bb[p]):
            bmis += 1
        else:
            for x, y in zip(ba[p], bb[p]):
                bdiff = max(bdiff, abs(x[0] - y[0]))
    print(f'baselines: max |dy|={bdiff:.2f}pt, pages with different line count={bmis}')
    print(f'lines compared: {total}, differing text: {bad_lines}; max |dx|={maxdx:.2f}pt max |dy|={maxdy:.2f}pt (identical lines only)')

main()
