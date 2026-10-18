package xhr

import (
	"math"
	"strings"
	"sync"
)

// Ctx holds the unit conversion constants shared by all styles (Flying
// Saucer's SharedContext with the iText renderer's 20 dots per pixel).
type Ctx struct {
	DotsPerPixel float32
	DotsPerPoint float32
	MmPerDot     float32
	Fonts        *FontEngine
}

// NewCtx creates the context used by ITextRenderer.
func NewCtx(fe *FontEngine) *Ctx {
	dpp := float32(20) * 4 / 3
	dpi := float32(72) * dpp
	return &Ctx{DotsPerPixel: 20, DotsPerPoint: dpp, MmPerDot: float32(2.54) * 10 / dpi, Fonts: fe}
}

// RGB is an sRGB colour.
type RGB struct{ R, G, B uint8 }

// RectSet is a margin/padding rectangle in dots.
type RectSet struct{ Top, Right, Bottom, Left float32 }

func (r *RectSet) Width() float32  { return r.Left + r.Right }
func (r *RectSet) Height() float32 { return r.Top + r.Bottom }
func (r RectSet) IsAllZeros() bool { return r.Top == 0 && r.Right == 0 && r.Bottom == 0 && r.Left == 0 }

// BorderSet mirrors BorderPropertySet.
type BorderSet struct {
	RectSet
	TopStyle, RightStyle, BottomStyle, LeftStyle string
	TopColor, RightColor, BottomColor, LeftColor *RGB
}

func (b *BorderSet) HasHidden() bool {
	return b.TopStyle == "hidden" || b.RightStyle == "hidden" || b.BottomStyle == "hidden" || b.LeftStyle == "hidden"
}

// EmptyBorder is the all-zero border.
var EmptyBorder = &BorderSet{}

var allZeros = &RectSet{}

// FontSpec is Flying Saucer's FontSpecification.
type FontSpec struct {
	Families []string
	Size     float32
	Weight   string
	Style    string
	Variant  string
}

// dv is a resolved property value.
type dv struct {
	vals []Value
}

func (d *dv) ident() string {
	if len(d.vals) > 0 && d.vals[0].Kind == VIdent {
		return d.vals[0].Str
	}
	return ""
}

// Style is Flying Saucer's CalculatedStyle.
type Style struct {
	ctx    *Ctx
	parent *Style
	decls  map[string]*Declared
	memo   map[string]*dv

	font          *FontSpec
	fsFont        *FSFont
	metrics       *FontMetrics
	lineHeight    float32
	lineHeightSet bool

	border  *BorderSet
	margin  *RectSet
	padding *RectSet

	marginsAllowed, paddingAllowed, bordersAllowed bool
	anon                                           map[string]*Style
}

// NewEmptyStyle creates the root style that holds only initial values.
func NewEmptyStyle(ctx *Ctx) *Style {
	return &Style{ctx: ctx, decls: map[string]*Declared{}, memo: map[string]*dv{}, marginsAllowed: true, paddingAllowed: true, bordersAllowed: true}
}

// Derive creates a child style from cascaded declarations.
func (s *Style) Derive(decls map[string]*Declared) *Style {
	c := &Style{ctx: s.ctx, parent: s, decls: decls, memo: map[string]*dv{}, marginsAllowed: true, paddingAllowed: true, bordersAllowed: true}
	if c.decls == nil {
		c.decls = map[string]*Declared{}
	}
	c.checkAllowed()
	return c
}

// CreateAnonymousStyle derives a style that only sets display.
func (s *Style) CreateAnonymousStyle(display string) *Style {
	if a, ok := s.anon[display]; ok {
		return a
	}
	if s.anon == nil {
		s.anon = map[string]*Style{}
	}
	a := s.deriveAnon(display)
	s.anon[display] = a
	return a
}

func (s *Style) deriveAnon(display string) *Style {
	return s.Derive(map[string]*Declared{"display": {Name: "display", Vals: []Value{{Kind: VIdent, Str: display}}}})
}

func (s *Style) checkAllowed() {
	v := s.Ident("display")
	switch v {
	case "table-header-group", "table-row-group", "table-footer-group", "table-row":
		s.paddingAllowed = false
		s.bordersAllowed = false
	}
	if (v == "table" || v == "inline-table") && s.IsCollapseBorders() {
		s.paddingAllowed = false
	}
	switch v {
	case "table-header-group", "table-row-group", "table-footer-group", "table-row", "table-cell":
		s.marginsAllowed = false
	}
}

var (
	initialCache   = map[string]*dv{}
	initialCacheMu sync.RWMutex
)

func initialValue(name string) *dv {
	initialCacheMu.RLock()
	d, ok := initialCache[name]
	initialCacheMu.RUnlock()
	if ok {
		return d
	}
	info, ok := propTable[name]
	if !ok {
		return &dv{}
	}
	l := &cssLexer{s: []rune(info.def)}
	vals, _, _ := l.parseValue()
	r, ok := buildSimple(name, vals, false)
	d = &dv{vals: r.Vals}
	if !ok {
		d = &dv{vals: vals}
	}
	initialCacheMu.Lock()
	initialCache[name] = d
	initialCacheMu.Unlock()
	return d
}

// value implements CalculatedStyle.valueByName.
func (s *Style) value(name string) *dv {
	if d, ok := s.memo[name]; ok {
		return d
	}
	var res *dv
	decl := s.decls[name]
	switch {
	case decl != nil && decl.Inherit:
		if s.parent != nil {
			res = s.parent.value(name)
		} else {
			res = initialValue(name)
		}
	case decl != nil && !decl.Initial:
		res = &dv{vals: decl.Vals}
	default:
		info := propTable[name]
		if decl == nil && info.inherits && s.parent != nil {
			res = s.parent.value(name)
		} else if strings.HasPrefix(info.def, "=") {
			res = s.value(info.def[1:])
		} else {
			res = initialValue(name)
		}
	}
	s.memo[name] = res
	return res
}

// Ident returns the ident string of a property ("" if not an ident).
func (s *Style) Ident(name string) string { return s.value(name).ident() }

// IsIdent tests whether a property is the given ident.
func (s *Style) IsIdent(name, val string) bool { return s.value(name).ident() == val }

func (s *Style) first(name string) Value {
	v := s.value(name)
	if len(v.vals) == 0 {
		return Value{}
	}
	return v.vals[0]
}

// IsLength reports whether the value is a length (not number/percent/ident).
func (s *Style) IsLength(name string) bool {
	v := s.first(name)
	return v.Kind == VLength || v.Kind == VPercent
}

// IsLengthOrNumber mirrors CalculatedStyle.isLengthOrNumber.
func (s *Style) IsLengthOrNumber(name string) bool {
	v := s.first(name)
	return v.Kind == VLength || v.Kind == VPercent || v.Kind == VNumber
}

// HasAbsoluteUnit mirrors FSDerivedValue.hasAbsoluteUnit.
func (s *Style) HasAbsoluteUnit(name string) bool {
	v := s.first(name)
	switch v.Kind {
	case VPercent:
		return false
	case VLength, VNumber:
		return true
	}
	return false
}

// AsFloat returns the raw float of a number/length value.
func (s *Style) AsFloat(name string) float32 {
	v := s.first(name)
	return float32(v.Num)
}

// AsString returns the first value's string.
func (s *Style) AsString(name string) string {
	v := s.first(name)
	return v.Str
}

// FloatProportional implements getFloatPropertyProportionalTo.
func (s *Style) FloatProportional(name string, base float32) float32 {
	v := s.first(name)
	switch v.Kind {
	case VNumber:
		return float32(v.Num)
	case VLength, VPercent:
		return s.calcLength(name, v, base)
	}
	return 0
}

func round32(f float32) float32 {
	return float32(math.Floor(float64(f) + 0.5))
}

// calcLength is LengthValue.calcFloatProportionalValue.
func (s *Style) calcLength(name string, v Value, base float32) float32 {
	ctx := s.ctx
	rel := float32(v.Num)
	var abs float32
	if v.Kind == VPercent {
		switch name {
		case "vertical-align":
			base = s.parent.LineHeight()
		case "font-size":
			base = s.parent.Font().Size
		case "line-height":
			base = s.Font().Size
		}
		abs = (rel / 100) * base
	} else {
		const cmPerIn = float32(2.54)
		const mmPerCm = float32(10)
		const ptPerIn = float32(1) / float32(72)
		switch v.Unit {
		case "px":
			abs = rel * ctx.DotsPerPixel
		case "in":
			abs = ((rel * cmPerIn) * mmPerCm) / ctx.MmPerDot
		case "cm":
			abs = (rel * mmPerCm) / ctx.MmPerDot
		case "mm":
			abs = rel / ctx.MmPerDot
		case "pt":
			abs = (((rel * ptPerIn) * cmPerIn) * mmPerCm) / ctx.MmPerDot
		case "pc":
			abs = ((((rel * 12) * ptPerIn) * cmPerIn) * mmPerCm) / ctx.MmPerDot
		case "em":
			if name == "font-size" {
				abs = rel * s.parent.Font().Size
			} else {
				abs = rel * s.Font().Size
			}
		case "ex":
			if name == "font-size" {
				abs = rel * s.parent.Font().Size / 2
			} else {
				abs = rel * s.Font().Size / 2
			}
		}
	}
	return float32(math.Floor(float64(abs) + 0.5))
}

// Color returns the colour of a property (nil for transparent).
func (s *Style) Color(name string) *RGB {
	v := s.first(name)
	switch v.Kind {
	case VColor:
		return &RGB{uint8(v.R), uint8(v.G), uint8(v.B)}
	case VIdent:
		if c, ok := namedColors[v.Str]; ok {
			return &RGB{uint8(c >> 16), uint8(c >> 8), uint8(c)}
		}
	}
	return nil
}

// ---- fonts ---------------------------------------------------------------

func (s *Style) resolveAbsoluteFontSize() string {
	fs := s.first("font-size")
	if fs.Kind != VIdent {
		return ""
	}
	if absFontSizes[fs.Str] {
		return fs.Str
	}
	if s.parent == nil {
		return ""
	}
	parent := s.parent.resolveAbsoluteFontSize()
	if parent != "" {
		order := []string{"xx-small", "x-small", "small", "medium", "large", "x-large", "xx-large"}
		for i, n := range order {
			if n == parent {
				if fs.Str == "smaller" {
					if i == 0 {
						return ""
					}
					return order[i-1]
				}
				if fs.Str == "larger" {
					if i+1 < len(order) {
						return order[i+1]
					}
					return ""
				}
			}
		}
	}
	return ""
}

var propFontSizes = map[string]float64{"xx-small": 9, "x-small": 10, "small": 13, "medium": 16, "large": 18, "x-large": 24, "xx-large": 32}
var fixedFontSizes = map[string]float64{"xx-small": 9, "x-small": 10, "small": 12, "medium": 13, "large": 16, "x-large": 20, "xx-large": 26}

// Font mirrors CalculatedStyle.getFont.
func (s *Style) Font() *FontSpec {
	if s.font != nil {
		return s.font
	}
	f := &FontSpec{}
	s.font = f
	for _, v := range s.value("font-family").vals {
		f.Families = append(f.Families, v.Str)
	}
	fs := s.first("font-size")
	if fs.Kind == VIdent {
		resolved := s.resolveAbsoluteFontSize()
		var rep Value
		if resolved != "" {
			mono := false
			for _, fam := range f.Families {
				if fam == "monospace" {
					mono = true
				}
			}
			tbl := propFontSizes
			if mono {
				tbl = fixedFontSizes
			}
			rep = Value{Kind: VLength, Num: tbl[resolved], Unit: "px"}
		} else if fs.Str == "larger" {
			rep = Value{Kind: VLength, Num: 1.2, Unit: "em"}
		} else {
			rep = Value{Kind: VLength, Num: 0.8, Unit: "em"}
		}
		f.Size = s.calcLength("font-size", rep, 0)
	} else {
		f.Size = s.FloatProportional("font-size", 0)
	}
	f.Weight = s.Ident("font-weight")
	f.Style = s.Ident("font-style")
	f.Variant = s.Ident("font-variant")
	return f
}

// FSFont resolves the concrete font.
func (s *Style) FSFont() *FSFont {
	if s.fsFont == nil {
		f := s.Font()
		s.fsFont = s.ctx.Fonts.Resolve(f.Families, f.Size, f.Weight, f.Style)
	}
	return s.fsFont
}

// FontMetrics returns ascent/descent etc.
func (s *Style) FontMetrics() *FontMetrics {
	if s.metrics == nil {
		m := s.FSFont().Metrics()
		s.metrics = &m
	}
	return s.metrics
}

// LineHeight mirrors CalculatedStyle.getLineHeight.
func (s *Style) LineHeight() float32 {
	if s.lineHeightSet {
		return s.lineHeight
	}
	v := s.first("line-height")
	switch {
	case v.Kind == VIdent && v.Str == "normal":
		lh1 := s.Font().Size * 1.1
		m := s.FontMetrics()
		lh2 := float32(math.Ceil(float64(m.Descent + m.Ascent)))
		if lh1 > lh2 {
			s.lineHeight = lh1
		} else {
			s.lineHeight = lh2
		}
	case v.Kind == VLength || v.Kind == VPercent:
		s.lineHeight = s.FloatProportional("line-height", 0)
	default:
		s.lineHeight = s.Font().Size * float32(v.Num)
	}
	s.lineHeightSet = true
	return s.lineHeight
}

// ---- rects ---------------------------------------------------------------

func (s *Style) rect(sides [4]string, cb float32) *RectSet {
	get := func(n string) float32 {
		if !s.IsLengthOrNumber(n) {
			return 0
		}
		return s.FloatProportional(n, cb)
	}
	return &RectSet{Top: get(sides[0]), Right: get(sides[1]), Bottom: get(sides[2]), Left: get(sides[3])}
}

// MarginRect mirrors getMarginRect (cached after the first call).
func (s *Style) MarginRect(cb float32) *RectSet {
	if !s.marginsAllowed {
		return allZeros
	}
	if s.margin == nil {
		r := s.rect([4]string{"margin-top", "margin-right", "margin-bottom", "margin-left"}, cb)
		if r.IsAllZeros() {
			r = allZeros
		}
		s.margin = r
	}
	return s.margin
}

// MarginRectNoCache recomputes the margin without caching.
func (s *Style) MarginRectNoCache(cb float32) *RectSet {
	if !s.marginsAllowed {
		return allZeros
	}
	return s.rect([4]string{"margin-top", "margin-right", "margin-bottom", "margin-left"}, cb)
}

// PaddingRect mirrors getPaddingRect.
func (s *Style) PaddingRect(cb float32) *RectSet {
	if !s.paddingAllowed {
		return allZeros
	}
	if s.padding == nil {
		r := s.rect([4]string{"padding-top", "padding-right", "padding-bottom", "padding-left"}, cb)
		if r.IsAllZeros() {
			r = allZeros
		} else {
			if r.Top < 0 {
				r.Top = 0
			}
			if r.Right < 0 {
				r.Right = 0
			}
			if r.Bottom < 0 {
				r.Bottom = 0
			}
			if r.Left < 0 {
				r.Left = 0
			}
		}
		s.padding = r
	}
	return s.padding
}

// Border mirrors getBorder.
func (s *Style) Border() *BorderSet {
	if !s.bordersAllowed {
		return EmptyBorder
	}
	if s.border == nil {
		b := &BorderSet{}
		side := func(n string) (float32, string, *RGB) {
			st := s.Ident("border-" + n + "-style")
			w := float32(0)
			if st != "none" && st != "hidden" {
				w = s.FloatProportional("border-"+n+"-width", 0)
			}
			return w, st, s.Color("border-" + n + "-color")
		}
		b.Top, b.TopStyle, b.TopColor = side("top")
		b.Right, b.RightStyle, b.RightColor = side("right")
		b.Bottom, b.BottomStyle, b.BottomColor = side("bottom")
		b.Left, b.LeftStyle, b.LeftColor = side("left")
		allZ := b.IsAllZeros()
		if allZ && !b.HasHidden() {
			s.border = EmptyBorder
		} else {
			if b.Top < 0 {
				b.Top = 0
			}
			if b.Right < 0 {
				b.Right = 0
			}
			if b.Bottom < 0 {
				b.Bottom = 0
			}
			if b.Left < 0 {
				b.Left = 0
			}
			s.border = b
		}
	}
	return s.border
}

const (
	Left   = 1
	Right  = 2
	Top    = 3
	Bottom = 4
)

// MarginBorderPadding mirrors getMarginBorderPadding.
func (s *Style) MarginBorderPadding(cb int, which int) int {
	b := s.Border()
	m := s.MarginRect(float32(cb))
	p := s.PaddingRect(float32(cb))
	switch which {
	case Left:
		return int(m.Left + b.Left + p.Left)
	case Right:
		return int(m.Right + b.Right + p.Right)
	case Top:
		return int(m.Top + b.Top + p.Top)
	default:
		return int(m.Bottom + b.Bottom + p.Bottom)
	}
}

// ---- predicates ----------------------------------------------------------

func (s *Style) IsRunning() bool {
	v := s.first("position")
	return v.Kind == VFunc
}

func (s *Style) RunningName() string {
	v := s.first("position")
	if v.Kind == VFunc && len(v.Args) > 0 {
		return v.Args[0].Str
	}
	return ""
}

func (s *Style) Display() string { return s.Ident("display") }
func (s *Style) IsInline() bool {
	return s.IsIdent("display", "inline") && !(s.IsFloated() || s.IsAbsolute() || s.IsFixed() || s.IsRunning())
}
func (s *Style) IsInlineBlock() bool      { return s.IsIdent("display", "inline-block") }
func (s *Style) IsTable() bool            { return s.IsIdent("display", "table") }
func (s *Style) IsInlineTable() bool      { return s.IsIdent("display", "inline-table") }
func (s *Style) IsTableCell() bool        { return s.IsIdent("display", "table-cell") }
func (s *Style) IsTableCaption() bool     { return s.IsIdent("display", "table-caption") }
func (s *Style) IsTableHeader() bool      { return s.IsIdent("display", "table-header-group") }
func (s *Style) IsTableFooter() bool      { return s.IsIdent("display", "table-footer-group") }
func (s *Style) IsTableRow() bool         { return s.IsIdent("display", "table-row") }
func (s *Style) IsDisplayNone() bool      { return s.IsIdent("display", "none") }
func (s *Style) IsListItem() bool         { return s.IsIdent("display", "list-item") }
func (s *Style) IsSpecifiedAsBlock() bool { return s.IsIdent("display", "block") }
func (s *Style) IsTableSection() bool {
	d := s.Ident("display")
	return d == "table-row-group" || d == "table-header-group" || d == "table-footer-group"
}
func (s *Style) IsAbsolute() bool { return s.IsIdent("position", "absolute") }
func (s *Style) IsFixed() bool    { return s.IsIdent("position", "fixed") }
func (s *Style) IsRelative() bool { return s.IsIdent("position", "relative") }
func (s *Style) IsFloated() bool {
	f := s.Ident("float")
	return f == "left" || f == "right"
}
func (s *Style) IsFloatedLeft() bool     { return s.IsIdent("float", "left") }
func (s *Style) IsFloatedRight() bool    { return s.IsIdent("float", "right") }
func (s *Style) IsPositioned() bool      { return s.IsAbsolute() || s.IsFixed() || s.IsRelative() }
func (s *Style) IsAutoWidth() bool       { return s.IsIdent("width", "auto") }
func (s *Style) IsAutoHeight() bool      { return s.IsIdent("height", "auto") }
func (s *Style) IsAutoLeftMargin() bool  { return s.IsIdent("margin-left", "auto") }
func (s *Style) IsAutoRightMargin() bool { return s.IsIdent("margin-right", "auto") }
func (s *Style) IsAutoZIndex() bool      { return s.IsIdent("z-index", "auto") }
func (s *Style) IsCleared() bool         { return !s.IsIdent("clear", "none") }
func (s *Style) IsClearLeft() bool {
	c := s.Ident("clear")
	return c == "left" || c == "both"
}
func (s *Style) IsClearRight() bool {
	c := s.Ident("clear")
	return c == "right" || c == "both"
}
func (s *Style) IsCollapseBorders() bool      { return s.IsIdent("border-collapse", "collapse") }
func (s *Style) IsVisible() bool              { return s.IsIdent("visibility", "visible") }
func (s *Style) IsBorderBox() bool            { return s.IsIdent("box-sizing", "border-box") }
func (s *Style) IsMaxWidthNone() bool         { return s.IsIdent("max-width", "none") }
func (s *Style) IsMaxHeightNone() bool        { return s.IsIdent("max-height", "none") }
func (s *Style) IsAvoidPageBreakInside() bool { return s.IsIdent("page-break-inside", "avoid") }
func (s *Style) IsPaginateTable() bool        { return s.IsIdent("-fs-table-paginate", "paginate") }
func (s *Style) IsKeepWithInline() bool       { return s.IsIdent("-fs-keep-with-inline", "keep") }
func (s *Style) IsDynamicAutoWidth() bool     { return s.IsIdent("-fs-dynamic-auto-width", "dynamic") }
func (s *Style) IsListMarkerInside() bool     { return s.IsIdent("list-style-position", "inside") }
func (s *Style) IsTopAuto() bool              { return s.IsIdent("top", "auto") }
func (s *Style) IsBottomAuto() bool           { return s.IsIdent("bottom", "auto") }
func (s *Style) IsForcePageBreakBefore() bool {
	v := s.Ident("page-break-before")
	return v == "always" || v == "left" || v == "right"
}
func (s *Style) IsForcePageBreakAfter() bool {
	v := s.Ident("page-break-after")
	return v == "always" || v == "left" || v == "right"
}
func (s *Style) IsShowEmptyCells() bool {
	return s.IsCollapseBorders() || s.IsIdent("empty-cells", "show")
}
func (s *Style) IsNonFlowContent() bool {
	return s.IsFloated() || s.IsAbsolute() || s.IsFixed() || s.IsRunning()
}
func (s *Style) IsTextJustify() bool {
	return s.IsIdent("text-align", "justify") && !(s.IsIdent("white-space", "pre") || s.IsIdent("white-space", "pre-line"))
}
func (s *Style) IsCanBeShrunkToFit() bool {
	return s.IsInlineBlock() || s.IsFloated() || s.IsAbsolute() || s.IsFixed() || s.IsFlexItem()
}

// IsFlex reports display:flex (a minimal single-line row flex container).
func (s *Style) IsFlex() bool { return s.IsIdent("display", "flex") }

// IsFlexItem reports whether the box is a block-level child of a flex container.
func (s *Style) IsFlexItem() bool {
	return s.parent != nil && s.parent.IsFlex() && s.IsIdent("display", "block") && !s.IsRunning()
}
func (s *Style) IsDynamicAutoWidthApplicable() bool {
	return s.IsDynamicAutoWidth() && s.IsAutoWidth() && !s.IsCanBeShrunkToFit()
}
func (s *Style) IsNeedAutoMarginResolution() bool {
	return !(s.IsAbsolute() || s.IsFixed() || s.IsFloated() || s.IsInlineBlock() || s.IsFlexItem())
}
func (s *Style) IsAbsoluteWidth() bool { return s.HasAbsoluteUnit("width") }
func (s *Style) IsOverflowApplies() bool {
	d := s.Ident("display")
	return d == "block" || d == "list-item" || d == "table" || d == "inline-block" || d == "table-cell"
}
func (s *Style) IsOverflowVisible() bool { return s.IsIdent("overflow", "visible") }
func (s *Style) IsAbsFixedOrInlineBlockEquiv() bool {
	return s.IsAbsolute() || s.IsFixed() || s.IsInlineBlock() || s.IsInlineTable()
}
func (s *Style) IsMayCollapseMarginsWithChildren() bool {
	return s.IsIdent("overflow", "visible") && !(s.IsFloated() || s.IsAbsolute() || s.IsFixed() || s.IsInlineBlock() || s.IsFlex() || s.IsFlexItem())
}

// EstablishesBFC mirrors CalculatedStyle.establishesBFC.
func (s *Style) EstablishesBFC() bool {
	if s.IsRunning() {
		return false
	}
	d := s.Ident("display")
	pos := s.Ident("position")
	return s.IsFloated() || pos == "absolute" || pos == "fixed" || d == "inline-block" || d == "table-cell" ||
		!s.IsIdent("overflow", "visible") || s.IsFlexItem()
}

// RequiresLayer mirrors CalculatedStyle.requiresLayer.
func (s *Style) RequiresLayer() bool {
	if s.IsRunning() {
		return false
	}
	pos := s.Ident("position")
	return pos == "absolute" || pos == "relative" || pos == "fixed"
}

func (s *Style) BorderHSpacing() int {
	if s.IsCollapseBorders() {
		return 0
	}
	return int(s.FloatProportional("-fs-border-spacing-horizontal", 0))
}

func (s *Style) BorderVSpacing() int {
	if s.IsCollapseBorders() {
		return 0
	}
	return int(s.FloatProportional("-fs-border-spacing-vertical", 0))
}

func (s *Style) RowSpan() int {
	r := int(s.AsFloat("-fs-table-cell-rowspan"))
	if r > 0 {
		return r
	}
	return 1
}

func (s *Style) ColSpan() int {
	r := int(s.AsFloat("-fs-table-cell-colspan"))
	if r > 0 {
		return r
	}
	return 1
}

func (s *Style) Whitespace() string { return s.Ident("white-space") }

// TextDecorations returns the text-decoration idents (nil for none).
func (s *Style) TextDecorations() []string {
	v := s.value("text-decoration")
	if len(v.vals) == 1 && v.vals[0].Kind == VIdent && v.vals[0].Str == "none" {
		return nil
	}
	var out []string
	for _, x := range v.vals {
		out = append(out, x.Str)
	}
	return out
}

// Length is Flying Saucer's css.style.Length (mutable).
type Length struct {
	Type  int
	Value int64
}

const (
	LenVariable = 1
	LenFixed    = 2
	LenPercent  = 3
	LenMaxWidth = math.MaxInt32 / 2
)

func NewLength() *Length { return &Length{Type: LenVariable} }

func (l *Length) IsVariable() bool { return l.Type == LenVariable }
func (l *Length) IsFixed() bool    { return l.Type == LenFixed }
func (l *Length) IsPercent() bool  { return l.Type == LenPercent }
func (l *Length) MinWidth(maxWidth int) int64 {
	switch l.Type {
	case LenFixed:
		return l.Value
	case LenPercent:
		return int64(maxWidth) * l.Value / 100
	}
	return 0
}

// AsLength mirrors CalculatedStyle.asLength.
func (s *Style) AsLength(name string) *Length {
	r := NewLength()
	v := s.first(name)
	if v.Kind == VLength || v.Kind == VNumber || v.Kind == VPercent {
		if s.HasAbsoluteUnit(name) {
			r.Value = int64(s.FloatProportional(name, 0))
			r.Type = LenFixed
		} else {
			r.Value = int64(float32(v.Num))
			r.Type = LenPercent
		}
	}
	return r
}
