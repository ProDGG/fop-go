#!/usr/bin/env python3
"""Print baselines of two PDFs side by side (page N, default 1): bl.py mine.pdf ref.pdf [page]"""
import sys
sys.path.insert(0, __file__.rsplit('/', 1)[0])
import baselines as b
pg = int(sys.argv[3]) - 1 if len(sys.argv) > 3 else 0
def ys(f):
    cs = b.content(f)
    return sorted({(round(y, 2), sz, round(x)) for x, y, sz, m in b.run(cs[pg], 0)}, reverse=True)
a, r = ys(sys.argv[1]), ys(sys.argv[2])
ay = sorted({(y, s) for y, s, x in a}, reverse=True); ry = sorted({(y, s) for y, s, x in r}, reverse=True)
for i in range(max(len(ay), len(ry))):
    x = ay[i] if i < len(ay) else None; y = ry[i] if i < len(ry) else None
    d = '' if not (x and y) else f'{x[0]-y[0]:+.2f}'
    print(x, y, d)
