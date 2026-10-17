package fo

import (
	"fmt"
	"strconv"
	"strings"
)

type regionDef struct {
	n            *Node
	name         string
	extent       float64
	precedence   bool
	mt, mb       float64
	ml, mr       float64
	columns      int
	gap          float64
	bg           Color
	displayAlign string
}

type pageMaster struct {
	name       string
	w, h       float64
	mt, mb     float64
	ml, mr     float64
	body       *regionDef
	before     *regionDef
	after      *regionDef
	start, end *regionDef
}

type altRef struct {
	ref    string
	pos    string
	parity string
	blank  string
}

type subseq struct {
	kind int // 0 single, 1 repeatable, 2 alternatives
	ref  string
	max  int
	alts []altRef
}

type seqMaster struct {
	name string
	subs []*subseq
}

type pageCtx struct {
	first, last bool
	odd         bool
	blank       bool
}

type masterPicker struct {
	sm      *seqMaster
	single  *pageMaster
	masters map[string]*pageMaster
	cur     int
	used    int
}

func (mp *masterPicker) pick(pc pageCtx) (*pageMaster, error) {
	if mp.single != nil {
		return mp.single, nil
	}
	subs := mp.sm.subs
	for mp.cur < len(subs) {
		s := subs[mp.cur]
		switch s.kind {
		case 0:
			if mp.used == 0 {
				mp.used++
				return mp.lookup(s.ref)
			}
		case 1:
			if s.max <= 0 || mp.used < s.max {
				mp.used++
				return mp.lookup(s.ref)
			}
		case 2:
			if s.max <= 0 || mp.used < s.max {
				mp.used++
				for _, a := range s.alts {
					if a.matches(pc) {
						return mp.lookup(a.ref)
					}
				}
				if len(s.alts) > 0 {
					return mp.lookup(s.alts[len(s.alts)-1].ref)
				}
			}
		}
		mp.cur++
		mp.used = 0
	}
	// subsequences exhausted: keep using the last master
	if len(subs) > 0 {
		last := subs[len(subs)-1]
		switch last.kind {
		case 2:
			if len(last.alts) > 0 {
				for _, a := range last.alts {
					if a.matches(pc) {
						return mp.lookup(a.ref)
					}
				}
				return mp.lookup(last.alts[len(last.alts)-1].ref)
			}
		default:
			return mp.lookup(last.ref)
		}
	}
	return nil, fmt.Errorf("page-sequence-master %q has no page masters", mp.sm.name)
}

func (mp *masterPicker) lookup(ref string) (*pageMaster, error) {
	m, ok := mp.masters[ref]
	if !ok {
		return nil, fmt.Errorf("unknown master-name %q", ref)
	}
	return m, nil
}

func (a altRef) matches(pc pageCtx) bool {
	switch a.pos {
	case "first":
		if !pc.first {
			return false
		}
	case "last":
		if !pc.last {
			return false
		}
	case "rest":
		if pc.first || pc.last {
			return false
		}
	case "only":
		if !(pc.first && pc.last) {
			return false
		}
	}
	switch a.parity {
	case "odd":
		if !pc.odd {
			return false
		}
	case "even":
		if pc.odd {
			return false
		}
	}
	switch a.blank {
	case "blank":
		if !pc.blank {
			return false
		}
	case "not-blank":
		if pc.blank {
			return false
		}
	}
	return true
}

// ---------- parsing ----------

const a4W, a4H = 595.28, 841.89

func (e *Engine) parseMasters(root *Node) (map[string]*pageMaster, map[string]*seqMaster, error) {
	masters := map[string]*pageMaster{}
	seqs := map[string]*seqMaster{}
	lms := root.Find("layout-master-set")
	if lms == nil {
		return nil, nil, fmt.Errorf("fo:root has no fo:layout-master-set")
	}
	lc := &lctx{e: e}
	for _, c := range lms.Children {
		switch c.Name {
		case "simple-page-master":
			name := c.Props["master-name"]
			if name == "" {
				return nil, nil, fmt.Errorf("fo:simple-page-master without master-name")
			}
			pm := &pageMaster{name: name, w: a4W, h: a4H}
			if v, ok := lc.length(c, "page-width", 0); ok {
				pm.w = v
			}
			if v, ok := lc.length(c, "page-height", 0); ok {
				pm.h = v
			}
			pm.mt = lc.lengthOr(c, "margin-top", pm.h, 0)
			pm.mb = lc.lengthOr(c, "margin-bottom", pm.h, 0)
			pm.ml = lc.lengthOr(c, "margin-left", pm.w, 0)
			pm.mr = lc.lengthOr(c, "margin-right", pm.w, 0)
			for _, rn := range c.Children {
				r := &regionDef{n: rn, columns: 1, gap: 12}
				r.extent = lc.lengthOr(rn, "extent", 0, 0)
				r.precedence = rn.Props["precedence"] == "true"
				r.mt = lc.lengthOr(rn, "margin-top", pm.h, 0)
				r.mb = lc.lengthOr(rn, "margin-bottom", pm.h, 0)
				r.ml = lc.lengthOr(rn, "margin-left", pm.w, 0)
				r.mr = lc.lengthOr(rn, "margin-right", pm.w, 0)
				if col, ok := ParseColor(rn.Props["background-color"]); ok {
					r.bg = col
				}
				r.displayAlign = rn.Props["display-align"]
				switch rn.Name {
				case "region-body":
					r.name = rn.Props["region-name"]
					if r.name == "" {
						r.name = "xsl-region-body"
					}
					if v, err := strconv.Atoi(strings.TrimSpace(rn.Props["column-count"])); err == nil && v > 1 {
						r.columns = v
					}
					r.gap = lc.lengthOr(rn, "column-gap", pm.w, 12)
					pm.body = r
				case "region-before":
					r.name = rn.Props["region-name"]
					if r.name == "" {
						r.name = "xsl-region-before"
					}
					pm.before = r
				case "region-after":
					r.name = rn.Props["region-name"]
					if r.name == "" {
						r.name = "xsl-region-after"
					}
					pm.after = r
				case "region-start":
					r.name = rn.Props["region-name"]
					if r.name == "" {
						r.name = "xsl-region-start"
					}
					pm.start = r
				case "region-end":
					r.name = rn.Props["region-name"]
					if r.name == "" {
						r.name = "xsl-region-end"
					}
					pm.end = r
				}
			}
			if pm.body == nil {
				return nil, nil, fmt.Errorf("simple-page-master %q has no region-body", name)
			}
			masters[name] = pm
		case "page-sequence-master":
			sm := &seqMaster{name: c.Props["master-name"]}
			for _, sc := range c.Children {
				ss := &subseq{}
				if v, err := strconv.Atoi(strings.TrimSpace(sc.Props["maximum-repeats"])); err == nil {
					ss.max = v
				}
				switch sc.Name {
				case "single-page-master-reference":
					ss.kind, ss.ref = 0, sc.Props["master-reference"]
				case "repeatable-page-master-reference":
					ss.kind, ss.ref = 1, sc.Props["master-reference"]
				case "repeatable-page-master-alternatives":
					ss.kind = 2
					for _, a := range sc.FindAll("conditional-page-master-reference") {
						ss.alts = append(ss.alts, altRef{
							ref: a.Props["master-reference"], pos: a.Props["page-position"],
							parity: a.Props["odd-or-even"], blank: a.Props["blank-or-not-blank"]})
					}
				default:
					continue
				}
				sm.subs = append(sm.subs, ss)
			}
			seqs[sm.name] = sm
		}
	}
	if len(masters) == 0 {
		return nil, nil, fmt.Errorf("no fo:simple-page-master defined")
	}
	return masters, seqs, nil
}

// regionRects computes the geometry of the regions of a page master.
func (pm *pageMaster) bodyRect() rect {
	x := pm.ml + pm.body.ml
	y := pm.mt + pm.body.mt
	w := pm.w - pm.ml - pm.mr - pm.body.ml - pm.body.mr
	h := pm.h - pm.mt - pm.mb - pm.body.mt - pm.body.mb
	return rect{x, y, w, h}
}

func (pm *pageMaster) regionRect(r *regionDef, kind string) rect {
	px, py := pm.ml, pm.mt
	pw, ph := pm.w-pm.ml-pm.mr, pm.h-pm.mt-pm.mb
	var se, ee float64 // widths of start/end regions that cut into before/after
	if pm.start != nil {
		se = pm.start.extent
	}
	if pm.end != nil {
		ee = pm.end.extent
	}
	switch kind {
	case "before":
		if r.precedence {
			return rect{px, py, pw, r.extent}
		}
		return rect{px + se, py, pw - se - ee, r.extent}
	case "after":
		if r.precedence {
			return rect{px, py + ph - r.extent, pw, r.extent}
		}
		return rect{px + se, py + ph - r.extent, pw - se - ee, r.extent}
	case "start", "end":
		top, bot := 0.0, 0.0
		if pm.before != nil && pm.before.precedence {
			top = pm.before.extent
		}
		if pm.after != nil && pm.after.precedence {
			bot = pm.after.extent
		}
		rr := rect{px, py + top, r.extent, ph - top - bot}
		if kind == "end" {
			rr.x = px + pw - r.extent
		}
		return rr
	}
	return rect{}
}
