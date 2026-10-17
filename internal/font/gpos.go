package font

// GPOS 'kern' support (PairPos lookups, formats 1 and 2), which is what Apache
// FOP applies by default ("complex scripts" enabled) to Latin, Cyrillic and Greek text.

type pairLookup struct {
	subs []pairSub
}

type pairSub struct {
	format   int
	cov      map[uint16]int // glyph -> coverage index
	vf1, vf2 int
	// format 1
	sets map[uint16]map[uint16]int // first -> second -> xAdvance
	// format 2
	class1, class2 map[uint16]int
	n2             int
	matrix         []int // [c1*n2+c2] xAdvance
}

func coverageOf(b []byte, off int) map[uint16]int {
	m := map[uint16]int{}
	if off+4 > len(b) {
		return m
	}
	switch be16(b, off) {
	case 1:
		n := be16(b, off+2)
		for i := 0; i < n && off+4+i*2+2 <= len(b); i++ {
			m[uint16(be16(b, off+4+i*2))] = i
		}
	case 2:
		n := be16(b, off+2)
		for i := 0; i < n && off+4+i*6+6 <= len(b); i++ {
			s, e, idx := be16(b, off+4+i*6), be16(b, off+6+i*6), be16(b, off+8+i*6)
			for g := s; g <= e; g++ {
				m[uint16(g)] = idx + g - s
			}
		}
	}
	return m
}

func classDefOf(b []byte, off int) map[uint16]int {
	m := map[uint16]int{}
	if off+4 > len(b) {
		return m
	}
	switch be16(b, off) {
	case 1:
		start, n := be16(b, off+2), be16(b, off+4)
		for i := 0; i < n && off+6+i*2+2 <= len(b); i++ {
			m[uint16(start+i)] = be16(b, off+6+i*2)
		}
	case 2:
		n := be16(b, off+2)
		for i := 0; i < n && off+4+i*6+6 <= len(b); i++ {
			s, e, c := be16(b, off+4+i*6), be16(b, off+6+i*6), be16(b, off+8+i*6)
			for g := s; g <= e; g++ {
				m[uint16(g)] = c
			}
		}
	}
	return m
}

func valueSize(vf int) int {
	n := 0
	for i := 0; i < 8; i++ {
		if vf&(1<<i) != 0 {
			n += 2
		}
	}
	return n
}

// xAdvanceOf extracts the XAdvance of a value record of format vf at off.
func xAdvanceOf(b []byte, off, vf int) int {
	if vf&0x0004 == 0 || off < 0 {
		return 0
	}
	p := off
	if vf&0x0001 != 0 {
		p += 2
	}
	if vf&0x0002 != 0 {
		p += 2
	}
	if p+2 > len(b) {
		return 0
	}
	return bes16(b, p)
}

// loadKern reads the GPOS kern feature lookups.
func (f *Font) loadKern() {
	f.gposDone = true
	g, ok := f.tables["GPOS"]
	if !ok || len(g) < 10 {
		return
	}
	scriptList, featureList, lookupList := be16(g, 4), be16(g, 6), be16(g, 8)
	// feature indices of default language systems of the Latin, Cyrillic and Greek scripts
	featIdx := map[int]bool{}
	if scriptList+2 <= len(g) {
		ns := be16(g, scriptList)
		for i := 0; i < ns && scriptList+2+i*6+6 <= len(g); i++ {
			tag := string(g[scriptList+2+i*6 : scriptList+6+i*6])
			if tag != "latn" && tag != "cyrl" && tag != "grek" {
				continue
			}
			so := scriptList + be16(g, scriptList+6+i*6)
			if so+4 > len(g) {
				continue
			}
			dl := be16(g, so)
			if dl == 0 {
				continue
			}
			lo := so + dl
			if lo+6 > len(g) {
				continue
			}
			nf := be16(g, lo+4)
			for k := 0; k < nf && lo+6+k*2+2 <= len(g); k++ {
				featIdx[be16(g, lo+6+k*2)] = true
			}
		}
	}
	lookups := map[int]bool{}
	if featureList+2 <= len(g) {
		nf := be16(g, featureList)
		for i := 0; i < nf && featureList+2+i*6+6 <= len(g); i++ {
			if string(g[featureList+2+i*6:featureList+6+i*6]) != "kern" || !featIdx[i] {
				continue
			}
			fo := featureList + be16(g, featureList+6+i*6)
			if fo+4 > len(g) {
				continue
			}
			nl := be16(g, fo+2)
			for k := 0; k < nl && fo+4+k*2+2 <= len(g); k++ {
				lookups[be16(g, fo+4+k*2)] = true
			}
		}
	}
	if lookupList+2 > len(g) {
		return
	}
	nl := be16(g, lookupList)
	for li := 0; li < nl; li++ {
		if !lookups[li] || lookupList+2+li*2+2 > len(g) {
			continue
		}
		lo := lookupList + be16(g, lookupList+2+li*2)
		if lo+6 > len(g) {
			continue
		}
		typ, ns := be16(g, lo), be16(g, lo+4)
		pl := pairLookup{}
		for k := 0; k < ns && lo+6+k*2+2 <= len(g); k++ {
			so := lo + be16(g, lo+6+k*2)
			t := typ
			if typ == 9 { // extension
				if so+8 > len(g) {
					continue
				}
				t = be16(g, so+2)
				so += be32(g, so+4)
			}
			if t != 2 || so+10 > len(g) {
				continue
			}
			if sub, ok := readPairSub(g, so); ok {
				pl.subs = append(pl.subs, sub)
			}
		}
		if len(pl.subs) > 0 {
			f.kernLookups = append(f.kernLookups, pl)
		}
	}
}

func readPairSub(g []byte, so int) (pairSub, bool) {
	ps := pairSub{format: be16(g, so)}
	cov := so + be16(g, so+2)
	ps.cov = coverageOf(g, cov)
	ps.vf1, ps.vf2 = be16(g, so+4), be16(g, so+6)
	s1, s2 := valueSize(ps.vf1), valueSize(ps.vf2)
	switch ps.format {
	case 1:
		n := be16(g, so+8)
		ps.sets = map[uint16]map[uint16]int{}
		first := make([]uint16, len(ps.cov))
		for gid, idx := range ps.cov {
			if idx < len(first) {
				first[idx] = gid
			}
		}
		for i := 0; i < n && so+10+i*2+2 <= len(g); i++ {
			po := so + be16(g, so+10+i*2)
			if po+2 > len(g) || i >= len(first) {
				continue
			}
			cnt := be16(g, po)
			m := map[uint16]int{}
			for k := 0; k < cnt; k++ {
				rec := po + 2 + k*(2+s1+s2)
				if rec+2+s1 > len(g) {
					break
				}
				m[uint16(be16(g, rec))] = xAdvanceOf(g, rec+2, ps.vf1)
			}
			ps.sets[first[i]] = m
		}
		return ps, true
	case 2:
		cd1, cd2 := so+be16(g, so+8), so+be16(g, so+10)
		n1, n2 := be16(g, so+12), be16(g, so+14)
		ps.class1, ps.class2 = classDefOf(g, cd1), classDefOf(g, cd2)
		ps.n2 = n2
		ps.matrix = make([]int, n1*n2)
		rec := so + 16
		for c1 := 0; c1 < n1; c1++ {
			for c2 := 0; c2 < n2; c2++ {
				if rec+s1 > len(g) {
					return ps, true
				}
				ps.matrix[c1*n2+c2] = xAdvanceOf(g, rec, ps.vf1)
				rec += s1 + s2
			}
		}
		return ps, true
	}
	return ps, false
}

// gposKern returns the GPOS pair adjustment (font units) for the glyph pair.
func (f *Font) gposKern(a, b uint16) (int, bool) {
	for _, pl := range f.kernLookups {
		for _, sub := range pl.subs {
			if _, ok := sub.cov[a]; !ok {
				continue
			}
			if sub.format == 1 {
				if set, ok := sub.sets[a]; ok {
					if v, ok := set[b]; ok {
						return v, true
					}
				}
				continue
			}
			c1 := sub.class1[a]
			c2 := sub.class2[b]
			if idx := c1*sub.n2 + c2; idx < len(sub.matrix) {
				return sub.matrix[idx], true
			}
		}
	}
	return 0, false
}
