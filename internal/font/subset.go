package font

import (
	"encoding/binary"
	"sort"
)

// compositeComponents lists the glyphs referenced by a composite glyph.
func compositeComponents(g []byte) []uint16 {
	if len(g) < 10 || int16(binary.BigEndian.Uint16(g)) >= 0 {
		return nil
	}
	var out []uint16
	o := 10
	for o+4 <= len(g) {
		flags := be16(g, o)
		out = append(out, uint16(be16(g, o+2)))
		o += 4
		if flags&0x0001 != 0 {
			o += 4
		} else {
			o += 2
		}
		switch {
		case flags&0x0008 != 0:
			o += 2
		case flags&0x0040 != 0:
			o += 4
		case flags&0x0080 != 0:
			o += 8
		}
		if flags&0x0020 == 0 {
			break
		}
	}
	return out
}

func pad4(b []byte) []byte {
	for len(b)%4 != 0 {
		b = append(b, 0)
	}
	return b
}

func checksum(b []byte) uint32 {
	var sum uint32
	for i := 0; i < len(b); i += 4 {
		var w [4]byte
		copy(w[:], b[i:])
		sum += binary.BigEndian.Uint32(w[:])
	}
	return sum
}

// Subset produces a TrueType font containing only the given glyphs. Glyph ids
// are preserved (unused glyphs become empty) so that CIDs equal glyph ids.
func (f *Font) Subset(gids []uint16) []byte {
	used := map[int]bool{0: true}
	var queue []int
	for _, g := range gids {
		if !used[int(g)] {
			used[int(g)] = true
		}
		queue = append(queue, int(g))
	}
	for len(queue) > 0 {
		g := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		for _, c := range compositeComponents(f.glyphData(g)) {
			if !used[int(c)] && int(c) < f.NumGlyphs {
				used[int(c)] = true
				queue = append(queue, int(c))
			}
		}
	}
	order := make([]int, 0, len(used))
	for g := range used {
		order = append(order, g)
	}
	sort.Ints(order)

	// glyf + loca (long format)
	var glyf []byte
	loca := make([]byte, 4*(f.NumGlyphs+1))
	oi := 0
	for g := 0; g < f.NumGlyphs; g++ {
		binary.BigEndian.PutUint32(loca[g*4:], uint32(len(glyf)))
		if oi < len(order) && order[oi] == g {
			oi++
			glyf = pad4(append(glyf, f.glyphData(g)...))
		}
	}
	binary.BigEndian.PutUint32(loca[f.NumGlyphs*4:], uint32(len(glyf)))

	head := append([]byte(nil), f.tables["head"]...)
	binary.BigEndian.PutUint32(head[8:], 0) // checkSumAdjustment
	binary.BigEndian.PutUint16(head[50:], 1)

	tables := map[string][]byte{
		"head": head,
		"hhea": f.tables["hhea"],
		"maxp": f.tables["maxp"],
		"hmtx": f.tables["hmtx"],
		"loca": loca,
		"glyf": glyf,
	}
	for _, t := range []string{"cvt ", "fpgm", "prep"} {
		if d, ok := f.tables[t]; ok {
			tables[t] = d
		}
	}
	// minimal post table (format 3)
	if post, ok := f.tables["post"]; ok && len(post) >= 32 {
		p := append([]byte(nil), post[:32]...)
		binary.BigEndian.PutUint32(p[0:], 0x00030000)
		tables["post"] = p
	}
	tags := make([]string, 0, len(tables))
	for t := range tables {
		tags = append(tags, t)
	}
	sort.Strings(tags)

	n := len(tags)
	out := make([]byte, 12+16*n)
	binary.BigEndian.PutUint32(out[0:], 0x00010000)
	binary.BigEndian.PutUint16(out[4:], uint16(n))
	sr := 1
	es := 0
	for sr*2 <= n {
		sr *= 2
		es++
	}
	binary.BigEndian.PutUint16(out[6:], uint16(sr*16))
	binary.BigEndian.PutUint16(out[8:], uint16(es))
	binary.BigEndian.PutUint16(out[10:], uint16(n*16-sr*16))
	var headOff int
	for i, t := range tags {
		d := tables[t]
		rec := out[12+i*16:]
		copy(rec, t)
		binary.BigEndian.PutUint32(rec[4:], checksum(d))
		binary.BigEndian.PutUint32(rec[8:], uint32(len(out)))
		binary.BigEndian.PutUint32(rec[12:], uint32(len(d)))
		if t == "head" {
			headOff = len(out)
		}
		out = append(out, pad4(append([]byte(nil), d...))...)
	}
	adj := 0xB1B0AFBA - checksum(out)
	binary.BigEndian.PutUint32(out[headOff+8:], adj)
	return out
}
