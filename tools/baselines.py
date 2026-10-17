#!/usr/bin/env python3
"""List text baselines (top-down page coordinates) of a PDF's first page by
interpreting q/Q/cm/BT/Tm/Td/Tf in the content stream. Works for fop-go and Apache FOP output."""
import re, sys, zlib

def content(path):
    d = open(path, 'rb').read()
    res = []
    for m in re.finditer(rb'stream\r?\n', d):
        s = m.end(); e = d.find(b'endstream', s)
        try: res.append(zlib.decompress(d[s:e]))
        except Exception:
            if b' BT' in d[s:e] or d[s:e].startswith(b'BT'): res.append(d[s:e])
    return [c for c in res if b'BT' in c and b'Tf' in c]

def mul(a, b):  # a then b (PDF row-vector convention): result = a x b
    return (a[0]*b[0]+a[1]*b[2], a[0]*b[1]+a[1]*b[3], a[2]*b[0]+a[3]*b[2], a[2]*b[1]+a[3]*b[3],
            a[4]*b[0]+a[5]*b[2]+b[4], a[4]*b[1]+a[5]*b[3]+b[5])

def run(stream, page_h):
    toks = re.findall(rb'\[[^\]]*\]|<[0-9A-Fa-f]*>|\([^)]*\)|/[^\s/\[\]()<>]+|[^\s/\[\]()<>]+', stream)
    ctm, stack, st = (1, 0, 0, 1, 0, 0), [], []
    out = []
    tm = (1, 0, 0, 1, 0, 0); size = 0; intext = False; first = None; first_td = True
    for t in toks:
        if t == b'q': stack.append(ctm)
        elif t == b'Q': ctm = stack.pop() if stack else ctm
        elif t == b'cm': ctm = mul(tuple(float(x) for x in st[-6:]), ctm)
        elif t == b'BT': intext = True; tm = (1, 0, 0, 1, 0, 0); first = None
        elif t == b'Tm': tm = tuple(float(x) for x in st[-6:])
        elif t == b'Td' and intext:
            dx, dy = float(st[-2]), float(st[-1]); tm = (tm[0], tm[1], tm[2], tm[3], tm[4]+dx*tm[0]+dy*tm[2], tm[5]+dx*tm[1]+dy*tm[3])
        elif t == b'Tf': size = float(st[-1])
        elif t in (b'Tj', b'TJ'):
            m = mul(tm, ctm)
            out.append((m[4], m[5], size, m))
        elif t == b'ET':
            intext = False
        st.append(t)
        if len(st) > 12: st = st[-12:]
    return out

if __name__ == '__main__':
    for f in sys.argv[1:]:
        cs = content(f)
        print('==', f)
        for c in cs[:1]:
            for x, y, sz, m in run(c, 841.89):
                yy = y if m[3] > 0 else y   # raw
                print(f'x={x:8.2f} y={y:8.2f} size={sz}')
