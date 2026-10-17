#!/usr/bin/env python3
"""Per-line vertical comparison of one page of two PDFs: linecmp.py mine.pdf ref.pdf [page]."""
import sys
src = open(__file__.rsplit('/', 1)[0] + '/comparepdf.py').read().replace('\nmain()\n', '\n')
ns = {'__file__': __file__.rsplit('/', 1)[0] + '/comparepdf.py'}
exec(src, ns)
a, b = ns['words'](sys.argv[1]), ns['words'](sys.argv[2])
p = int(sys.argv[3]) - 1 if len(sys.argv) > 3 else 0
la, lb = ns['lines'](a[p]), ns['lines'](b[p])
for i in range(max(len(la), len(lb))):
    x = la[i] if i < len(la) else None
    y = lb[i] if i < len(lb) else None
    t = lambda l: ' '.join(w[0] for w in l)[:40] if l else ''
    print(i + 1, round(x[0][2], 1) if x else None, round(y[0][2], 1) if y else None, '|', t(x), '|', t(y))
