#!/usr/bin/env python3
"""Pixel comparison of two PDFs (rendered with pdftoppm): imgdiff.py mine.pdf ref.pdf [dpi] [outprefix].

Prints per page the share of differing pixels and the bounding box of the largest
differing cluster; with outprefix it writes side-by-side/diff PNGs. Needs Pillow.
"""
import subprocess, sys, tempfile, os, glob
from PIL import Image, ImageChops

def render(pdf, d, tag, dpi):
    subprocess.run(['pdftoppm', '-r', str(dpi), '-png', pdf, os.path.join(d, tag)], check=True)
    return sorted(glob.glob(os.path.join(d, tag + '-*.png')))

def main():
    a, b = sys.argv[1], sys.argv[2]
    dpi = int(sys.argv[3]) if len(sys.argv) > 3 else 72
    out = sys.argv[4] if len(sys.argv) > 4 else None
    with tempfile.TemporaryDirectory() as d:
        pa, pb = render(a, d, 'a', dpi), render(b, d, 'b', dpi)
        print(f'pages: mine={len(pa)} ref={len(pb)}')
        for i, (x, y) in enumerate(zip(pa, pb)):
            ia, ib = Image.open(x).convert('L'), Image.open(y).convert('L')
            if ia.size != ib.size:
                print(f'p{i+1}: size differs {ia.size} {ib.size}'); continue
            diff = ImageChops.difference(ia, ib).point(lambda v: 255 if v > 64 else 0)
            n = sum(1 for v in diff.tobytes() if v)
            bbox = diff.getbbox()
            print(f'p{i+1}: {n} px differ ({100.0*n/(ia.size[0]*ia.size[1]):.3f}%) bbox={bbox}')
            if out and n:
                w, h = ia.size
                sheet = Image.new('RGB', (w * 3, h), 'white')
                sheet.paste(ia.convert('RGB'), (0, 0)); sheet.paste(ib.convert('RGB'), (w, 0))
                sheet.paste(Image.merge('RGB', (diff, diff.point(lambda v: 0), diff.point(lambda v: 0))), (2 * w, 0))
                sheet.save(f'{out}-{i+1}.png')

main()
