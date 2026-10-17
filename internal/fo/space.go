package fo

import (
	"math"
	"strconv"
	"strings"
)

// spc is a resolved space-specifier (space-before/after, or margin-top/bottom
// acting as one). It follows XSL 1.1 section 4.3 as implemented by Apache FOP's
// SpaceResolver: only the optimum length takes part (no stretching of spaces).
type spc struct {
	opt    float64
	retain bool // conditionality="retain": not discarded at a break
	force  bool // precedence="force"
	prec   int  // numeric precedence
}

// resolveSpaces applies resolution rules 2 and 3 to a run of adjacent spaces.
func resolveSpaces(list []spc) float64 {
	if len(list) == 0 {
		return 0
	}
	sum, forced := 0.0, false
	for _, s := range list {
		if s.force {
			forced = true
			sum += s.opt
		}
	}
	if forced {
		return sum
	}
	hp := math.MinInt
	for _, s := range list {
		if s.prec > hp {
			hp = s.prec
		}
	}
	best := math.Inf(-1)
	for _, s := range list {
		if s.prec == hp && s.opt > best {
			best = s.opt
		}
	}
	return best
}

// discardAtBreak applies rule 1: conditional spaces next to a break are
// discarded, working away from the break until a retained space is met.
func discardAtBreak(list []spc, fromEnd bool) []spc {
	out := append([]spc(nil), list...)
	if fromEnd {
		for len(out) > 0 && !out[len(out)-1].retain {
			out = out[:len(out)-1]
		}
	} else {
		for len(out) > 0 && !out[0].retain {
			out = out[1:]
		}
	}
	return out
}

func concatSp(a, b []spc) []spc {
	out := make([]spc, 0, len(a)+len(b))
	out = append(out, a...)
	return append(out, b...)
}

// spaces reads space-before/space-after of n. As in FOP (foproperties.xml,
// <corresponding use-if-specified>), margin-top/margin-bottom act as the space
// when the space property is not specified, with conditionality "retain".
func (lc *lctx) spaces(n *Node, name string) []spc {
	var s spc
	found := false
	for _, key := range []string{name + ".optimum", name} {
		if v, ok := n.Props[key]; ok {
			if r, ok := lc.e.parseLength(n, firstToken(v), 0); ok {
				s.opt, found = r, true
				break
			}
		}
	}
	if !found {
		margin := "margin-top"
		if name == "space-after" {
			margin = "margin-bottom"
		}
		if v, ok := n.Props[margin]; ok {
			if r, ok := lc.e.parseLength(n, firstToken(v), lc.contentW()); ok {
				return []spc{{opt: r, retain: true}}
			}
		}
		return nil
	}
	if v := n.Props[name+".conditionality"]; v == "retain" {
		s.retain = true
	}
	switch p := strings.TrimSpace(n.Props[name+".precedence"]); p {
	case "":
	case "force":
		s.force = true
	default:
		if k, err := strconv.Atoi(p); err == nil {
			s.prec = k
		}
	}
	return []spc{s}
}
