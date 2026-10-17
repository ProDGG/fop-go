package xhr

import (
	"math"
	"strings"
)

// Declared is one longhand declaration produced from the parsed CSS.
type Declared struct {
	Name      string
	Vals      []Value
	Inherit   bool
	Initial   bool // FS_INITIAL_VALUE (border shorthand parts left out)
	Important bool
}

type propInfo struct {
	inherits bool
	def      string
}

// propTable mirrors Flying Saucer's CSSName table: initial values and
// inheritance. A default starting with '=' refers to another property.
var propTable = map[string]propInfo{
	"color":                         {true, "black"},
	"background-color":              {false, "transparent"},
	"background-image":              {false, "none"},
	"background-repeat":             {false, "repeat"},
	"background-attachment":         {false, "scroll"},
	"background-position":           {false, "0% 0%"},
	"background-size":               {false, "auto auto"},
	"border-collapse":               {true, "separate"},
	"-fs-border-spacing-horizontal": {false, "0"},
	"-fs-border-spacing-vertical":   {false, "0"},
	"-fs-dynamic-auto-width":        {false, "static"},
	"-fs-keep-with-inline":          {false, "auto"},
	"-fs-page-width":                {false, "auto"},
	"-fs-page-height":               {false, "auto"},
	"-fs-page-sequence":             {false, "auto"},
	"-fs-page-orientation":          {false, "auto"},
	"-fs-table-paginate":            {false, "auto"},
	"-fs-text-decoration-extent":    {false, "line"},
	"-fs-fit-images-to-width":       {false, "auto"},
	"-fs-table-cell-colspan":        {false, "1"},
	"-fs-table-cell-rowspan":        {false, "1"},
	"bottom":                        {false, "auto"},
	"caption-side":                  {true, "top"},
	"clear":                         {false, "none"},
	"content":                       {false, "normal"},
	"counter-increment":             {false, "none"},
	"counter-reset":                 {false, "none"},
	"cursor":                        {true, "auto"},
	"display":                       {false, "inline"},
	"empty-cells":                   {true, "show"},
	"float":                         {false, "none"},
	"font-style":                    {true, "normal"},
	"font-variant":                  {true, "normal"},
	"font-weight":                   {true, "normal"},
	"font-size":                     {true, "medium"},
	"line-height":                   {true, "normal"},
	"font-family":                   {true, "serif"},
	"height":                        {false, "auto"},
	"left":                          {false, "auto"},
	"letter-spacing":                {true, "normal"},
	"list-style-type":               {true, "disc"},
	"list-style-position":           {true, "outside"},
	"list-style-image":              {true, "none"},
	"max-height":                    {false, "none"},
	"max-width":                     {false, "none"},
	"min-height":                    {false, "0"},
	"min-width":                     {false, "0"},
	"orphans":                       {true, "2"},
	"overflow":                      {false, "visible"},
	"page":                          {true, "auto"},
	"page-break-after":              {false, "auto"},
	"page-break-before":             {false, "auto"},
	"page-break-inside":             {true, "auto"},
	"position":                      {false, "static"},
	"right":                         {false, "auto"},
	"table-layout":                  {false, "auto"},
	"text-align":                    {true, "left"},
	"text-decoration":               {false, "none"},
	"text-indent":                   {true, "0"},
	"text-transform":                {true, "none"},
	"top":                           {false, "auto"},
	"vertical-align":                {false, "baseline"},
	"visibility":                    {true, "visible"},
	"white-space":                   {true, "normal"},
	"widows":                        {true, "2"},
	"width":                         {false, "auto"},
	"word-spacing":                  {true, "normal"},
	"word-wrap":                     {true, "normal"},
	"hyphens":                       {true, "none"},
	"justify-content":               {false, "flex-start"},
	"align-items":                   {false, "stretch"},
	"flex-direction":                {false, "row"},
	"z-index":                       {false, "auto"},
	"border-top-color":              {false, "=color"},
	"border-right-color":            {false, "=color"},
	"border-bottom-color":           {false, "=color"},
	"border-left-color":             {false, "=color"},
	"border-top-style":              {false, "none"},
	"border-right-style":            {false, "none"},
	"border-bottom-style":           {false, "none"},
	"border-left-style":             {false, "none"},
	"border-top-width":              {false, "medium"},
	"border-right-width":            {false, "medium"},
	"border-bottom-width":           {false, "medium"},
	"border-left-width":             {false, "medium"},
	"margin-top":                    {false, "0"},
	"margin-right":                  {false, "0"},
	"margin-bottom":                 {false, "0"},
	"margin-left":                   {false, "0"},
	"padding-top":                   {false, "0"},
	"padding-right":                 {false, "0"},
	"padding-bottom":                {false, "0"},
	"padding-left":                  {false, "0"},
	"box-sizing":                    {false, "content-box"},
	"tab-size":                      {true, "8"},
}

func idents(s string) map[string]bool {
	m := map[string]bool{}
	for _, f := range strings.Fields(s) {
		m[f] = true
	}
	return m
}

var (
	borderStyles   = idents("none hidden dotted dashed solid double groove ridge inset outset")
	borderWidths   = idents("thin medium thick")
	fontVariants   = idents("normal small-caps")
	fontStyles     = idents("normal italic oblique")
	fontWeightsID  = idents("normal bold bolder lighter")
	listPositions  = idents("inside outside")
	listTypes      = idents("disc circle square decimal decimal-leading-zero lower-roman upper-roman lower-greek lower-latin upper-latin armenian georgian lower-alpha upper-alpha none")
	bgRepeats      = idents("repeat repeat-x repeat-y no-repeat")
	bgAttach       = idents("scroll fixed")
	bgPositions    = idents("left right top bottom center")
	absFontSizes   = idents("xx-small x-small small medium large x-large xx-large")
	relFontSizes   = idents("smaller larger")
	displayValues  = idents("inline block list-item inline-block table inline-table table-row-group table-header-group table-footer-group table-row table-column-group table-column table-cell table-caption none flex")
	positionValues = idents("static relative absolute fixed")
)

// single-ident properties and their allowed sets.
var singleIdent = map[string]map[string]bool{
	"background-repeat":          bgRepeats,
	"background-attachment":      bgAttach,
	"border-collapse":            idents("collapse separate"),
	"border-top-style":           borderStyles,
	"border-right-style":         borderStyles,
	"border-bottom-style":        borderStyles,
	"border-left-style":          borderStyles,
	"caption-side":               idents("top bottom"),
	"clear":                      idents("none left right both"),
	"cursor":                     idents("auto crosshair default pointer move e-resize ne-resize nw-resize n-resize se-resize sw-resize s-resize w-resize text wait help progress"),
	"display":                    displayValues,
	"empty-cells":                idents("show hide"),
	"float":                      idents("left right none"),
	"font-style":                 fontStyles,
	"font-variant":               fontVariants,
	"-fs-page-sequence":          idents("start auto"),
	"-fs-page-orientation":       idents("auto portrait landscape"),
	"-fs-table-paginate":         idents("paginate auto"),
	"-fs-text-decoration-extent": idents("line block"),
	"-fs-dynamic-auto-width":     idents("dynamic static"),
	"-fs-keep-with-inline":       idents("auto keep"),
	"list-style-position":        listPositions,
	"list-style-type":            listTypes,
	"overflow":                   idents("visible hidden"),
	"page-break-after":           idents("auto always avoid left right"),
	"page-break-before":          idents("auto always avoid left right"),
	"page-break-inside":          idents("avoid auto"),
	"table-layout":               idents("auto fixed"),
	"text-align":                 idents("left right center justify"),
	"text-transform":             idents("capitalize uppercase lowercase none"),
	"visibility":                 idents("visible hidden collapse"),
	"white-space":                idents("normal pre nowrap pre-wrap pre-line"),
	"word-wrap":                  idents("normal break-word"),
	"hyphens":                    idents("none manual auto"),
	"justify-content":            idents("flex-start flex-end center space-between space-around space-evenly start end left right normal stretch"),
	"align-items":                idents("flex-start flex-end center baseline stretch start end normal"),
	"flex-direction":             idents("row row-reverse column column-reverse"),
	"box-sizing":                 idents("border-box content-box"),
}

type lenKind int

const (
	lkLength    lenKind = iota // length only
	lkLengthPct                // length | percentage
)

type lenSpec struct {
	kind   lenKind
	nonNeg bool
	idents map[string]bool // extra allowed idents (nil = none)
}

var autoOnly = idents("auto")
var noneOnly = idents("none")
var normalOnly = idents("normal")

var lenProps = map[string]lenSpec{
	"bottom":                        {lkLengthPct, false, autoOnly},
	"top":                           {lkLengthPct, false, autoOnly},
	"left":                          {lkLengthPct, false, autoOnly},
	"right":                         {lkLengthPct, false, autoOnly},
	"height":                        {lkLengthPct, true, autoOnly},
	"width":                         {lkLengthPct, true, autoOnly},
	"max-height":                    {lkLengthPct, true, noneOnly},
	"max-width":                     {lkLengthPct, true, noneOnly},
	"min-height":                    {lkLengthPct, true, nil},
	"min-width":                     {lkLengthPct, true, nil},
	"margin-top":                    {lkLengthPct, false, autoOnly},
	"margin-right":                  {lkLengthPct, false, autoOnly},
	"margin-bottom":                 {lkLengthPct, false, autoOnly},
	"margin-left":                   {lkLengthPct, false, autoOnly},
	"padding-top":                   {lkLengthPct, true, nil},
	"padding-right":                 {lkLengthPct, true, nil},
	"padding-bottom":                {lkLengthPct, true, nil},
	"padding-left":                  {lkLengthPct, true, nil},
	"text-indent":                   {lkLengthPct, false, nil},
	"letter-spacing":                {lkLength, false, normalOnly},
	"word-spacing":                  {lkLength, false, normalOnly},
	"-fs-border-spacing-horizontal": {lkLength, false, nil},
	"-fs-border-spacing-vertical":   {lkLength, false, nil},
	"-fs-page-width":                {lkLengthPct, true, autoOnly},
	"-fs-page-height":               {lkLengthPct, true, autoOnly},
	"-fs-fit-images-to-width":       {lkLengthPct, true, autoOnly},
	"vertical-align":                {lkLengthPct, false, idents("baseline sub super top text-top middle bottom text-bottom")},
	"border-top-width":              {lkLength, true, nil},
	"border-right-width":            {lkLength, true, nil},
	"border-bottom-width":           {lkLength, true, nil},
	"border-left-width":             {lkLength, true, nil},
}

var lengthUnits = idents("em ex px in cm mm pt pc")

func isLengthV(v Value) bool {
	switch v.Kind {
	case VLength:
		return lengthUnits[v.Unit]
	case VNumber:
		return v.Num == 0
	}
	return false
}

func isColorV(v Value) bool {
	switch v.Kind {
	case VColor:
		return true
	case VIdent:
		if _, ok := namedColors[v.Str]; ok {
			return true
		}
		return v.Str == "transparent"
	}
	return false
}

func isInherit(vals []Value) bool {
	return len(vals) == 1 && vals[0].Kind == VIdent && vals[0].Str == "inherit"
}

// BuildDeclarations validates a declaration and expands shorthands. ok is false
// when the declaration must be dropped.
func BuildDeclarations(d Decl) (out []Declared, ok bool) {
	name := strings.ToLower(d.Name)
	vals := d.Vals
	if len(vals) == 0 {
		return nil, false
	}
	mk := func(n string, v []Value) Declared {
		return Declared{Name: n, Vals: v, Important: d.Important}
	}
	mkInh := func(n string) Declared {
		return Declared{Name: n, Inherit: true, Important: d.Important}
	}
	inh := isInherit(vals)

	switch name {
	case "margin", "padding", "border-width", "border-style", "border-color":
		var names [4]string
		var sub string
		switch name {
		case "margin":
			names = [4]string{"margin-top", "margin-right", "margin-bottom", "margin-left"}
			sub = "margin-top"
		case "padding":
			names = [4]string{"padding-top", "padding-right", "padding-bottom", "padding-left"}
			sub = "padding-top"
		case "border-width":
			names = [4]string{"border-top-width", "border-right-width", "border-bottom-width", "border-left-width"}
			sub = "border-top-width"
		case "border-style":
			names = [4]string{"border-top-style", "border-right-style", "border-bottom-style", "border-left-style"}
			sub = "border-top-style"
		default:
			names = [4]string{"border-top-color", "border-right-color", "border-bottom-color", "border-left-color"}
			sub = "border-top-color"
		}
		if inh {
			for _, n := range names {
				out = append(out, mkInh(n))
			}
			return out, true
		}
		if len(vals) > 4 {
			return nil, false
		}
		var decls []Declared
		for _, v := range vals {
			if isInherit([]Value{v}) {
				return nil, false
			}
			r, ok := buildSimple(sub, []Value{v}, d.Important)
			if !ok {
				return nil, false
			}
			decls = append(decls, r)
		}
		pick := func(i int) Declared { return decls[i] }
		var idx [4]int
		switch len(decls) {
		case 1:
			idx = [4]int{0, 0, 0, 0}
		case 2:
			idx = [4]int{0, 1, 0, 1}
		case 3:
			idx = [4]int{0, 1, 2, 1}
		default:
			idx = [4]int{0, 1, 2, 3}
		}
		for i, n := range names {
			c := pick(idx[i])
			c.Name = n
			out = append(out, c)
		}
		return out, true
	case "border", "border-top", "border-right", "border-bottom", "border-left":
		sides := []string{"top", "right", "bottom", "left"}
		if name != "border" {
			sides = []string{strings.TrimPrefix(name, "border-")}
		}
		if inh {
			for _, s := range sides {
				out = append(out, mkInh("border-"+s+"-width"), mkInh("border-"+s+"-style"), mkInh("border-"+s+"-color"))
			}
			return out, true
		}
		if len(vals) > 3 {
			return nil, false
		}
		var w, st, col *Value
		var wv, cv []Value
		for i := range vals {
			v := vals[i]
			matched := false
			if (v.Kind == VIdent && borderWidths[v.Str]) || (v.Kind != VIdent && isLengthV(v)) {
				if w != nil {
					return nil, false
				}
				if v.Kind == VIdent {
					wv = []Value{borderWidthIdent(v.Str)}
				} else {
					if v.Num < 0 {
						return nil, false
					}
					wv = []Value{v}
				}
				w = &vals[i]
				matched = true
			}
			if v.Kind == VIdent && borderStyles[v.Str] {
				if st != nil {
					return nil, false
				}
				st = &vals[i]
				matched = true
			}
			if v.Kind == VColor || (v.Kind == VIdent && isColorV(v)) {
				if col != nil {
					return nil, false
				}
				col = &vals[i]
				cv = []Value{v}
				matched = true
			}
			if !matched {
				return nil, false
			}
		}
		for _, s := range sides {
			if w != nil {
				out = append(out, mk("border-"+s+"-width", wv))
			} else {
				out = append(out, Declared{Name: "border-" + s + "-width", Initial: true, Important: d.Important})
			}
			if st != nil {
				out = append(out, mk("border-"+s+"-style", []Value{*st}))
			} else {
				out = append(out, Declared{Name: "border-" + s + "-style", Initial: true, Important: d.Important})
			}
			if col != nil {
				out = append(out, mk("border-"+s+"-color", cv))
			} else {
				out = append(out, Declared{Name: "border-" + s + "-color", Initial: true, Important: d.Important})
			}
		}
		return out, true
	case "border-spacing":
		if inh {
			return []Declared{mkInh("-fs-border-spacing-horizontal"), mkInh("-fs-border-spacing-vertical")}, true
		}
		if len(vals) < 1 || len(vals) > 2 {
			return nil, false
		}
		for _, v := range vals {
			if !isLengthV(v) || v.Num < 0 {
				return nil, false
			}
		}
		h, vv := vals[0], vals[0]
		if len(vals) == 2 {
			vv = vals[1]
		}
		return []Declared{mk("-fs-border-spacing-horizontal", []Value{h}), mk("-fs-border-spacing-vertical", []Value{vv})}, true
	case "background":
		return buildBackground(d)
	case "font":
		return buildFont(d)
	case "list-style":
		return buildListStyle(d)
	case "size":
		return buildSize(d)
	}
	r, ok := buildSimple(name, vals, d.Important)
	if !ok {
		return nil, false
	}
	return []Declared{r}, true
}

func borderWidthIdent(s string) Value {
	switch s {
	case "thin":
		return Value{Kind: VLength, Num: 1, Unit: "px"}
	case "thick":
		return Value{Kind: VLength, Num: 3, Unit: "px"}
	}
	return Value{Kind: VLength, Num: 2, Unit: "px"}
}

// buildSimple validates a longhand.
func buildSimple(name string, vals []Value, important bool) (Declared, bool) {
	res := Declared{Name: name, Important: important}
	if isInherit(vals) {
		res.Inherit = true
		if _, known := propTable[name]; !known {
			return res, false
		}
		return res, true
	}
	if _, known := propTable[name]; !known {
		return res, false
	}
	res.Vals = vals
	if set, ok := singleIdent[name]; ok {
		if len(vals) != 1 || vals[0].Kind != VIdent || !set[vals[0].Str] {
			return res, false
		}
		return res, true
	}
	if ls, ok := lenProps[name]; ok {
		if len(vals) != 1 {
			return res, false
		}
		v := vals[0]
		switch {
		case v.Kind == VIdent:
			if ls.idents == nil || !ls.idents[v.Str] {
				return res, false
			}
		case ls.kind == lkLengthPct && v.Kind == VPercent:
			if ls.nonNeg && v.Num < 0 {
				return res, false
			}
		case isLengthV(v):
			if ls.nonNeg && v.Num < 0 {
				return res, false
			}
		default:
			return res, false
		}
		if name == "border-top-width" || name == "border-right-width" || name == "border-bottom-width" || name == "border-left-width" {
			if v.Kind == VIdent {
				if !borderWidths[v.Str] {
					return res, false
				}
				res.Vals = []Value{borderWidthIdent(v.Str)}
			}
		}
		return res, true
	}
	switch name {
	case "color", "background-color", "border-top-color", "border-right-color", "border-bottom-color", "border-left-color":
		if len(vals) != 1 || !isColorV(vals[0]) {
			return res, false
		}
		return res, true
	case "background-image", "list-style-image":
		if len(vals) != 1 || !(vals[0].Kind == VURI || vals[0].Kind == VIdent && vals[0].Str == "none") {
			return res, false
		}
		return res, true
	case "font-size":
		if len(vals) != 1 {
			return res, false
		}
		v := vals[0]
		switch {
		case v.Kind == VIdent:
			if !absFontSizes[v.Str] && !relFontSizes[v.Str] {
				return res, false
			}
		case v.Kind == VPercent || isLengthV(v):
			if v.Num < 0 {
				return res, false
			}
		default:
			return res, false
		}
		return res, true
	case "font-weight":
		if len(vals) != 1 {
			return res, false
		}
		v := vals[0]
		if v.Kind == VIdent && fontWeightsID[v.Str] {
			return res, true
		}
		if v.Kind == VNumber {
			n := int(v.Num)
			if float64(n) == v.Num && n >= 100 && n <= 900 && n%100 == 0 {
				res.Vals = []Value{{Kind: VIdent, Str: itoa(n)}}
				return res, true
			}
		}
		return res, false
	case "font-family":
		var fams []Value
		var cur []string
		flush := func() {
			if len(cur) > 0 {
				s := strings.Join(cur, " ")
				fams = append(fams, Value{Kind: VString, Str: s})
				cur = nil
			}
		}
		expectSep := false
		for _, v := range vals {
			switch v.Kind {
			case VComma:
				flush()
				expectSep = false
			case VString:
				flush()
				fams = append(fams, Value{Kind: VString, Str: v.Str})
				expectSep = true
			case VIdent:
				if expectSep && len(cur) == 0 {
					return res, false
				}
				cur = append(cur, v.Raw)
			default:
				return res, false
			}
		}
		flush()
		if len(fams) == 0 {
			return res, false
		}
		res.Vals = fams
		return res, true
	case "line-height":
		if len(vals) != 1 {
			return res, false
		}
		v := vals[0]
		switch {
		case v.Kind == VIdent && v.Str == "normal":
		case v.Kind == VPercent || v.Kind == VNumber || isLengthV(v):
			if v.Num < 0 {
				return res, false
			}
		default:
			return res, false
		}
		return res, true
	case "text-decoration":
		if len(vals) == 1 && vals[0].Kind == VIdent && vals[0].Str == "none" {
			return res, true
		}
		allowed := idents("underline overline line-through")
		for _, v := range vals {
			if v.Kind != VIdent || !allowed[v.Str] {
				return res, false
			}
		}
		return res, true
	case "position":
		if len(vals) != 1 {
			return res, false
		}
		v := vals[0]
		if v.Kind == VIdent && positionValues[v.Str] {
			return res, true
		}
		if v.Kind == VFunc && v.Str == "running" && len(v.Args) == 1 && v.Args[0].Kind == VIdent {
			return res, true
		}
		return res, false
	case "z-index":
		if len(vals) != 1 {
			return res, false
		}
		v := vals[0]
		if v.Kind == VIdent && v.Str == "auto" {
			return res, true
		}
		if v.Kind == VNumber && v.Num == math.Trunc(v.Num) {
			return res, true
		}
		return res, false
	case "orphans", "widows", "tab-size":
		if len(vals) != 1 || vals[0].Kind != VNumber || vals[0].Num != math.Trunc(vals[0].Num) || vals[0].Num < 0 {
			return res, false
		}
		return res, true
	case "-fs-table-cell-colspan", "-fs-table-cell-rowspan":
		if len(vals) != 1 || vals[0].Kind != VNumber || vals[0].Num < 1 {
			return res, false
		}
		return res, true
	case "page":
		if len(vals) != 1 || vals[0].Kind != VIdent {
			return res, false
		}
		return res, true
	case "background-position":
		return buildBgPosition(res)
	case "background-size":
		return buildBgSize(res)
	case "content":
		return res, true
	case "counter-increment", "counter-reset":
		return res, true
	}
	return res, false
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func buildBgPosition(res Declared) (Declared, bool) {
	vals := res.Vals
	if len(vals) < 1 || len(vals) > 2 {
		return res, false
	}
	for _, v := range vals {
		if !(v.Kind == VIdent && bgPositions[v.Str]) && v.Kind != VPercent && !isLengthV(v) {
			return res, false
		}
	}
	res.Vals = normalizeBgPos(vals)
	if res.Vals == nil {
		return res, false
	}
	return res, true
}

// normalizeBgPos returns [x, y] values (percent or length).
func normalizeBgPos(vals []Value) []Value {
	pct := func(p float64) Value { return Value{Kind: VPercent, Num: p} }
	conv := func(s string) (float64, bool, bool) { // value, isHoriz, isVert
		switch s {
		case "left":
			return 0, true, false
		case "right":
			return 100, true, false
		case "top":
			return 0, false, true
		case "bottom":
			return 100, false, true
		case "center":
			return 50, false, false
		}
		return 0, false, false
	}
	if len(vals) == 1 {
		v := vals[0]
		if v.Kind == VIdent {
			p, h, vt := conv(v.Str)
			switch {
			case vt:
				return []Value{pct(50), pct(p)}
			default:
				_ = h
				return []Value{pct(p), pct(50)}
			}
		}
		return []Value{v, pct(50)}
	}
	a, b := vals[0], vals[1]
	if a.Kind == VIdent && b.Kind == VIdent {
		pa, ha, va := conv(a.Str)
		pb, hb, vb := conv(b.Str)
		if va || hb { // swapped order
			if ha || vb || (!va && !hb) {
				return nil
			}
			return []Value{pct(pb), pct(pa)}
		}
		_ = ha
		_ = vb
		return []Value{pct(pa), pct(pb)}
	}
	cv := func(v Value) Value {
		if v.Kind == VIdent {
			p, _, _ := conv(v.Str)
			return pct(p)
		}
		return v
	}
	return []Value{cv(a), cv(b)}
}

func buildBgSize(res Declared) (Declared, bool) {
	vals := res.Vals
	if len(vals) < 1 || len(vals) > 2 {
		return res, false
	}
	ok := func(v Value) bool {
		return v.Kind == VIdent && v.Str == "auto" || v.Kind == VPercent && v.Num >= 0 || isLengthV(v) && v.Num >= 0
	}
	if len(vals) == 1 {
		v := vals[0]
		if v.Kind == VIdent && (v.Str == "contain" || v.Str == "cover") {
			return res, true
		}
		if !ok(v) {
			return res, false
		}
		if v.Kind == VIdent {
			res.Vals = []Value{v, v}
		} else {
			res.Vals = []Value{v, {Kind: VIdent, Str: "auto"}}
		}
		return res, true
	}
	if !ok(vals[0]) || !ok(vals[1]) {
		return res, false
	}
	return res, true
}

func buildBackground(d Decl) ([]Declared, bool) {
	names := []string{"background-color", "background-image", "background-repeat", "background-attachment", "background-position"}
	if isInherit(d.Vals) {
		var out []Declared
		for _, n := range names {
			out = append(out, Declared{Name: n, Inherit: true, Important: d.Important})
		}
		return out, true
	}
	var color, image, repeat, attach *Value
	var pos []Value
	for i := 0; i < len(d.Vals); i++ {
		v := d.Vals[i]
		switch {
		case v.Kind == VColor || (v.Kind == VIdent && isColorV(v)):
			if color != nil {
				return nil, false
			}
			color = &d.Vals[i]
		case v.Kind == VURI || v.Kind == VIdent && v.Str == "none":
			if image != nil {
				return nil, false
			}
			image = &d.Vals[i]
		case v.Kind == VIdent && bgRepeats[v.Str]:
			if repeat != nil {
				return nil, false
			}
			repeat = &d.Vals[i]
		case v.Kind == VIdent && bgAttach[v.Str]:
			if attach != nil {
				return nil, false
			}
			attach = &d.Vals[i]
		case v.Kind == VIdent && bgPositions[v.Str], v.Kind == VPercent, isLengthV(v):
			pos = append(pos, v)
		default:
			return nil, false
		}
	}
	if len(pos) > 2 {
		return nil, false
	}
	var out []Declared
	add := func(n string, v *Value) {
		if v == nil {
			out = append(out, Declared{Name: n, Initial: true, Important: d.Important})
		} else {
			out = append(out, Declared{Name: n, Vals: []Value{*v}, Important: d.Important})
		}
	}
	add("background-color", color)
	add("background-image", image)
	add("background-repeat", repeat)
	add("background-attachment", attach)
	if len(pos) == 0 {
		out = append(out, Declared{Name: "background-position", Initial: true, Important: d.Important})
	} else {
		np := normalizeBgPos(pos)
		if np == nil {
			return nil, false
		}
		out = append(out, Declared{Name: "background-position", Vals: np, Important: d.Important})
	}
	return out, true
}

func buildListStyle(d Decl) ([]Declared, bool) {
	names := []string{"list-style-type", "list-style-position", "list-style-image"}
	if isInherit(d.Vals) {
		var out []Declared
		for _, n := range names {
			out = append(out, Declared{Name: n, Inherit: true, Important: d.Important})
		}
		return out, true
	}
	var typ, pos, img *Value
	for i := range d.Vals {
		v := d.Vals[i]
		switch {
		case v.Kind == VURI:
			img = &d.Vals[i]
		case v.Kind == VIdent && v.Str == "none":
			// applies to type and image if not set
			if typ == nil {
				typ = &d.Vals[i]
			}
			if img == nil {
				img = &d.Vals[i]
			}
		case v.Kind == VIdent && listPositions[v.Str]:
			pos = &d.Vals[i]
		case v.Kind == VIdent && listTypes[v.Str]:
			typ = &d.Vals[i]
		default:
			return nil, false
		}
	}
	var out []Declared
	add := func(n string, v *Value) {
		if v == nil {
			out = append(out, Declared{Name: n, Initial: true, Important: d.Important})
		} else {
			out = append(out, Declared{Name: n, Vals: []Value{*v}, Important: d.Important})
		}
	}
	add("list-style-type", typ)
	add("list-style-position", pos)
	add("list-style-image", img)
	return out, true
}

func buildSize(d Decl) ([]Declared, bool) {
	// @page { size: A4 landscape }
	vals := d.Vals
	if isInherit(vals) {
		return nil, false
	}
	type sz struct{ w, h float64 }
	named := map[string]sz{
		"a3": {297, 420}, "a4": {210, 297}, "a5": {148, 210}, "b4": {250, 353}, "b5": {176, 250},
		"letter": {215.9, 279.4}, "legal": {215.9, 355.6}, "ledger": {431.8, 279.4},
	}
	orient := "auto"
	var w, h *Value
	var wmm, hmm float64
	for i := range vals {
		v := vals[i]
		switch {
		case v.Kind == VIdent && (v.Str == "portrait" || v.Str == "landscape"):
			orient = v.Str
		case v.Kind == VIdent && v.Str == "auto":
		case v.Kind == VIdent:
			s, ok := named[v.Str]
			if !ok {
				return nil, false
			}
			wmm, hmm = s.w, s.h
			w = &Value{Kind: VLength, Num: s.w, Unit: "mm"}
			h = &Value{Kind: VLength, Num: s.h, Unit: "mm"}
		case isLengthV(v):
			if w == nil {
				w = &vals[i]
				h = &vals[i]
			} else {
				h = &vals[i]
			}
		default:
			return nil, false
		}
	}
	_ = wmm
	_ = hmm
	var out []Declared
	if w != nil {
		out = append(out, Declared{Name: "-fs-page-width", Vals: []Value{*w}, Important: d.Important},
			Declared{Name: "-fs-page-height", Vals: []Value{*h}, Important: d.Important})
	}
	out = append(out, Declared{Name: "-fs-page-orientation", Vals: []Value{{Kind: VIdent, Str: orient}}, Important: d.Important})
	return out, true
}

func buildFont(d Decl) ([]Declared, bool) {
	names := []string{"font-style", "font-variant", "font-weight", "font-size", "line-height", "font-family"}
	if isInherit(d.Vals) {
		var out []Declared
		for _, n := range names {
			out = append(out, Declared{Name: n, Inherit: true, Important: d.Important})
		}
		return out, true
	}
	vals := d.Vals
	var style, variant, weight *Value
	i := 0
	for i < len(vals) {
		v := vals[i]
		if v.Kind == VIdent {
			if v.Str == "normal" {
				i++
				continue
			}
			if fontStyles[v.Str] && style == nil {
				style = &vals[i]
				i++
				continue
			}
			if fontVariants[v.Str] && variant == nil {
				variant = &vals[i]
				i++
				continue
			}
			if fontWeightsID[v.Str] && weight == nil {
				weight = &vals[i]
				i++
				continue
			}
		}
		if v.Kind == VNumber && weight == nil {
			if r, ok := buildSimple("font-weight", []Value{v}, false); ok {
				weight = &r.Vals[0]
				i++
				continue
			}
		}
		break
	}
	if i >= len(vals) {
		return nil, false
	}
	size := vals[i]
	if _, ok := buildSimple("font-size", []Value{size}, false); !ok {
		return nil, false
	}
	i++
	var lh *Value
	if i < len(vals) && vals[i].Kind == VSlash {
		i++
		if i >= len(vals) {
			return nil, false
		}
		if _, ok := buildSimple("line-height", []Value{vals[i]}, false); !ok {
			return nil, false
		}
		lh = &vals[i]
		i++
	}
	if i >= len(vals) {
		return nil, false
	}
	fam, ok := buildSimple("font-family", vals[i:], d.Important)
	if !ok {
		return nil, false
	}
	var out []Declared
	add := func(n string, v *Value) {
		if v == nil {
			out = append(out, Declared{Name: n, Initial: true, Important: d.Important})
		} else {
			out = append(out, Declared{Name: n, Vals: []Value{*v}, Important: d.Important})
		}
	}
	add("font-style", style)
	add("font-variant", variant)
	add("font-weight", weight)
	out = append(out, Declared{Name: "font-size", Vals: []Value{size}, Important: d.Important})
	add("line-height", lh)
	out = append(out, fam)
	return out, true
}
