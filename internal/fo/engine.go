package fo

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ProDGG/fop-go/internal/font"
)

// Options configure rendering.
type Options struct {
	BaseDir  string
	Fonts    *font.Manager
	Warn     func(string)
	PxPerPt  float64 // points per CSS pixel (default 1, i.e. 72 dpi source resolution)
	Compress bool
	Title    string
	Author   string
	Subject  string
	Keywords string
	Creator  string
	Producer string
	// AllowRemote enables loading http(s) images.
	AllowRemote bool
}

// Engine holds state shared by all pages of one rendering run.
type Engine struct {
	opts   Options
	fonts  *font.Manager
	warned map[string]bool

	styles  map[*Node]*textStyle
	links   map[*Node]*linkTarget
	images  map[string]*imageData
	ids     map[string]idInfo // from the pass in progress
	prevIDs map[string]idInfo // from the previous pass
	cited   bool              // page-number-citation seen
	cites   map[string]bool   // ids cited in the pass in progress
	pxPt    float64
}

type idInfo struct {
	page    int    // zero-based page index
	pageNum string // formatted page number
	y       float64
}

func (e *Engine) warn(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	if e.warned[msg] {
		return
	}
	e.warned[msg] = true
	if e.opts.Warn != nil {
		e.opts.Warn(msg)
	}
}

// ---------- text styles ----------

type textStyle struct {
	face       *font.Face
	ft         *font.Font
	size       float64
	color      Color
	lh         float64 // line height in points
	ls, ws     float64 // letter and word spacing
	underline  bool
	overline   bool
	strike     bool
	shift      float64 // baseline shift (positive raises)
	upper      int     // text-transform: 0 none, 1 upper, 2 lower, 3 capitalize
	link       *linkTarget
	node       *Node
	preserve   bool // white-space-collapse=false
	nowrap     bool
	lfPreserve bool
}

type linkTarget struct {
	uri    string
	destID string
}

func (e *Engine) splitFamilies(s string) []string {
	var out []string
	for _, f := range strings.Split(s, ",") {
		f = strings.Trim(strings.TrimSpace(f), `"'`)
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

func parseWeight(s string) int {
	switch strings.TrimSpace(strings.ToLower(s)) {
	case "normal", "":
		return 400
	case "bold", "bolder":
		return 700
	case "lighter":
		return 300
	}
	if v, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && v >= 100 && v <= 900 {
		return v
	}
	return 400
}

// lengthCtx builds a length context for node n.
func (e *Engine) lengthCtx(n *Node, pct float64) *LengthCtx {
	el := n.elem()
	return &LengthCtx{FontSize: el.FontSize(e.pxPt), Percent: pct, PxPt: e.pxPt, Node: el,
		Prop: func(name string) string {
			kind, prop, _ := strings.Cut(name, ":")
			switch kind {
			case "from-parent":
				if el.Parent != nil {
					return el.Parent.Get(prop)
				}
			default:
				return el.Get(prop)
			}
			return ""
		}}
}

// length resolves a property of n as a length in points. ok is false when the
// property is absent or not a length.
func (e *Engine) length(n *Node, name string, pct float64) (float64, bool) {
	v, ok := n.Own(name)
	if !ok || v == "" {
		return 0, false
	}
	return e.parseLength(n, v, pct)
}

func (e *Engine) parseLength(n *Node, v string, pct float64) (float64, bool) {
	r, err := EvalLength(v, e.lengthCtx(n, pct))
	if err != nil {
		if v != "auto" {
			e.warn("cannot parse length %q: %v", v, err)
		}
		return 0, false
	}
	return r, true
}

// lengthOr returns the length property or def.
func (e *Engine) lengthOr(n *Node, name string, pct, def float64) float64 {
	if v, ok := e.length(n, name, pct); ok {
		return v
	}
	return def
}

func (e *Engine) styleFor(n *Node) *textStyle {
	el := n.elem()
	if s, ok := e.styles[el]; ok {
		return s
	}
	fs := el.FontSize(e.pxPt)
	st := &textStyle{size: fs, node: el}
	fam := e.splitFamilies(el.Get("font-family"))
	weight := parseWeight(el.Get("font-weight"))
	fstyle := strings.ToLower(el.Get("font-style"))
	italic := fstyle == "italic" || fstyle == "oblique"
	st.face = e.fonts.Select(fam, weight, italic)
	st.ft = st.face.MustFont()
	if st.ft == nil {
		e.warn("font %q cannot be loaded", st.face.Name)
	}
	if c, ok := ParseColor(el.Get("color")); ok && c.A > 0 {
		st.color = c
	} else {
		st.color = Color{0, 0, 0, 1}
	}
	lh := strings.TrimSpace(el.Get("line-height"))
	switch {
	case lh == "" || lh == "normal":
		st.lh = fs * 1.2
	default:
		if f, err := strconv.ParseFloat(lh, 64); err == nil {
			st.lh = f * fs
		} else if v, ok := e.parseLength(el, lh, fs); ok {
			st.lh = v
		} else {
			st.lh = fs * 1.2
		}
	}
	if v, ok := e.parseLength(el, el.Str("letter-spacing", "0pt"), fs); ok && el.Get("letter-spacing") != "normal" {
		st.ls = v
	}
	if v, ok := e.parseLength(el, el.Str("word-spacing", "0pt"), fs); ok && el.Get("word-spacing") != "normal" {
		st.ws = v
	}
	for _, tok := range strings.Fields(strings.ToLower(el.Get("text-decoration"))) {
		switch tok {
		case "underline":
			st.underline = true
		case "overline":
			st.overline = true
		case "line-through":
			st.strike = true
		}
	}
	switch strings.ToLower(el.Get("text-transform")) {
	case "uppercase":
		st.upper = 1
	case "lowercase":
		st.upper = 2
	case "capitalize":
		st.upper = 3
	}
	// baseline shift (not inherited, applies to this element only)
	if v, ok := el.Props["baseline-shift"]; ok {
		switch v {
		case "super":
			st.shift = fs * 0.4
		case "sub":
			st.shift = -fs * 0.2
		case "baseline":
		default:
			if r, ok := e.parseLength(el, v, fs); ok {
				st.shift = r
			}
		}
	} else if v, ok := el.Props["vertical-align"]; ok && el.Name == "inline" {
		switch v {
		case "super":
			st.shift = fs * 0.4
		case "sub":
			st.shift = -fs * 0.2
		}
	}
	if el.Parent != nil && st.shift == 0 && el.Name == "inline" {
		// shifts accumulate through nested inlines
		if ps := e.styleFor(el.Parent); ps != nil && el.Parent.Name == "inline" {
			st.shift = ps.shift
		}
	} else if el.Parent != nil && st.shift != 0 && el.Name == "inline" && el.Parent.Name == "inline" {
		st.shift += e.styleFor(el.Parent).shift
	}
	wsc := el.Get("white-space-collapse")
	lf := el.Get("linefeed-treatment")
	if ws := el.Get("white-space"); ws != "" {
		switch ws {
		case "pre":
			wsc, lf = "false", "preserve"
			st.nowrap = true
		case "pre-wrap":
			wsc, lf = "false", "preserve"
		case "nowrap":
			st.nowrap = true
		}
	}
	st.preserve = wsc == "false"
	st.lfPreserve = lf == "preserve"
	if el.Get("wrap-option") == "no-wrap" {
		st.nowrap = true
	}
	// link target inherited from enclosing basic-link
	for c := el; c != nil; c = c.Parent {
		if c.Name == "basic-link" {
			if lt, ok := e.links[c]; ok {
				st.link = lt
				break
			}
			lt := &linkTarget{}
			if d := c.Props["internal-destination"]; d != "" {
				lt.destID = d
			} else if d := c.Props["external-destination"]; d != "" {
				lt.uri = unwrapURL(d)
			}
			if lt.destID == "" && lt.uri == "" {
				lt = nil
			}
			e.links[c] = lt
			st.link = lt
			break
		}
	}
	e.styles[el] = st
	return st
}

func unwrapURL(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "url(") && strings.HasSuffix(s, ")") {
		s = strings.TrimSpace(s[4 : len(s)-1])
		s = strings.Trim(s, `"'`)
	}
	return s
}

func applyTransform(s string, mode int) string {
	switch mode {
	case 1:
		return strings.ToUpper(s)
	case 2:
		return strings.ToLower(s)
	case 3:
		var sb strings.Builder
		start := true
		for _, r := range s {
			if start {
				sb.WriteString(strings.ToUpper(string(r)))
			} else {
				sb.WriteRune(r)
			}
			start = r == ' ' || r == '\t' || r == '\n'
		}
		return sb.String()
	}
	return s
}
