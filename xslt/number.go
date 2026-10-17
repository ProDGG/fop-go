package xslt

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"

	"github.com/ProDGG/fop-go/xmldom"
	"github.com/ProDGG/fop-go/xpath"
)

// ---------- format-number ----------

type subPattern struct {
	prefix, suffix  string
	minInt, minFrac int
	maxFrac         int
	groupSize       int
	percent, perMil bool
}

func parseSubPattern(p string, df *DecimalFormat) (*subPattern, error) {
	sp := &subPattern{}
	rs := []rune(p)
	isNum := func(r rune) bool {
		return r == df.Digit || r == df.ZeroDigit || r == df.GroupSep || r == df.DecimalSep || (r >= '0' && r <= '9' && df.ZeroDigit == '0')
	}
	i := 0
	var prefix, suffix strings.Builder
	for i < len(rs) && !isNum(rs[i]) {
		if rs[i] == df.Percent {
			sp.percent = true
		}
		if rs[i] == df.PerMil {
			sp.perMil = true
		}
		if rs[i] == '\'' && i+1 < len(rs) {
			i++
		}
		prefix.WriteRune(rs[i])
		i++
	}
	start := i
	for i < len(rs) && isNum(rs[i]) {
		i++
	}
	num := rs[start:i]
	for ; i < len(rs); i++ {
		if rs[i] == df.Percent {
			sp.percent = true
		}
		if rs[i] == df.PerMil {
			sp.perMil = true
		}
		if rs[i] == '\'' && i+1 < len(rs) {
			i++
		}
		suffix.WriteRune(rs[i])
	}
	sp.prefix, sp.suffix = prefix.String(), suffix.String()
	// integer / fraction parts
	dot := -1
	for j, r := range num {
		if r == df.DecimalSep {
			dot = j
			break
		}
	}
	intPart, fracPart := num, []rune(nil)
	if dot >= 0 {
		intPart, fracPart = num[:dot], num[dot+1:]
	}
	lastGroup := -1
	for j, r := range intPart {
		if r == df.GroupSep {
			lastGroup = j
		}
		if r == df.ZeroDigit || (r >= '0' && r <= '9') {
			sp.minInt++
		}
	}
	if lastGroup >= 0 {
		sp.groupSize = len(intPart) - lastGroup - 1
	}
	for _, r := range fracPart {
		if r == df.ZeroDigit {
			sp.minFrac++
			sp.maxFrac++
		} else if r == df.Digit {
			sp.maxFrac++
		}
	}
	return sp, nil
}

// FormatNumber implements the XSLT format-number() function.
func FormatNumber(f float64, pattern string, df *DecimalFormat) (string, error) {
	if df == nil {
		df = defaultDecimalFormat()
	}
	if math.IsNaN(f) {
		return df.NaN, nil
	}
	parts := strings.SplitN(pattern, string(df.PatSep), 2)
	pos, err := parseSubPattern(parts[0], df)
	if err != nil {
		return "", err
	}
	neg := false
	sp := pos
	if f < 0 || (f == 0 && math.Signbit(f)) {
		neg = true
		if len(parts) == 2 {
			sp, err = parseSubPattern(parts[1], df)
			if err != nil {
				return "", err
			}
			neg = false
		}
		f = -f
	}
	var body string
	if math.IsInf(f, 1) {
		body = df.Infinity
	} else {
		if sp.percent {
			f *= 100
		}
		if sp.perMil {
			f *= 1000
		}
		body = formatDigits(f, sp, df)
	}
	res := sp.prefix + body + sp.suffix
	if neg {
		res = string(df.MinusSign) + res
	}
	return res, nil
}

func formatDigits(f float64, sp *subPattern, df *DecimalFormat) string {
	s := strconv.FormatFloat(f, 'f', 12, 64)
	intS, fracS := s, ""
	if i := strings.IndexByte(s, '.'); i >= 0 {
		intS, fracS = s[:i], s[i+1:]
	}
	// decimal half-up rounding to maxFrac digits
	if len(fracS) > sp.maxFrac {
		roundUp := fracS[sp.maxFrac] >= '5'
		fracS = fracS[:sp.maxFrac]
		if roundUp {
			digits := []byte(intS + fracS)
			k := len(digits) - 1
			for k >= 0 {
				if digits[k] == '9' {
					digits[k] = '0'
					k--
					continue
				}
				digits[k]++
				break
			}
			ds := string(digits)
			if k < 0 {
				ds = "1" + ds
			}
			intS, fracS = ds[:len(ds)-sp.maxFrac], ds[len(ds)-sp.maxFrac:]
		}
	}
	fracS = strings.TrimRight(fracS, "0")
	for len(fracS) < sp.minFrac {
		fracS += "0"
	}
	intS = strings.TrimLeft(intS, "0")
	for len(intS) < sp.minInt {
		intS = "0" + intS
	}
	if sp.groupSize > 0 {
		var sb strings.Builder
		for i, r := range intS {
			if i > 0 && (len(intS)-i)%sp.groupSize == 0 {
				sb.WriteRune(df.GroupSep)
			}
			sb.WriteRune(r)
		}
		intS = sb.String()
	}
	out := intS
	if fracS != "" {
		out += string(df.DecimalSep) + fracS
	}
	if out == "" {
		out = string(df.ZeroDigit)
	}
	if df.ZeroDigit != '0' {
		var sb strings.Builder
		for _, r := range out {
			if r >= '0' && r <= '9' {
				r = df.ZeroDigit + (r - '0')
			}
			sb.WriteRune(r)
		}
		out = sb.String()
	}
	return out
}

// ---------- xsl:number ----------

type numberInstr struct {
	value  *xpath.Expr
	count  *xpath.Pattern
	from   *xpath.Pattern
	level  string
	format *avt
	gsep   *avt
	gsize  *avt
	el     *xmldom.Node
}

func (s *Stylesheet) compileNumber(n *xmldom.Node) (instr, error) {
	ni := &numberInstr{level: n.Attr("level"), el: n}
	if ni.level == "" {
		ni.level = "single"
	}
	var err error
	if ni.value, err = s.optExpr(n, "value"); err != nil {
		return nil, err
	}
	if v := n.Attr("count"); v != "" {
		if ni.count, err = xpath.CompilePattern(v, nsResolverFor(n)); err != nil {
			return nil, errAt(n, "%v", err)
		}
	}
	if v := n.Attr("from"); v != "" {
		if ni.from, err = xpath.CompilePattern(v, nsResolverFor(n)); err != nil {
			return nil, errAt(n, "%v", err)
		}
	}
	if ni.format, err = s.optAVT(n, "format"); err != nil {
		return nil, err
	}
	if ni.gsep, err = s.optAVT(n, "grouping-separator"); err != nil {
		return nil, err
	}
	if ni.gsize, err = s.optAVT(n, "grouping-size"); err != nil {
		return nil, err
	}
	return ni, nil
}

func (ni *numberInstr) matchCount(c *execCtx, xc *xpath.Context, n *xmldom.Node, cur *xmldom.Node) bool {
	if ni.count != nil {
		return ni.count.Match(xc, n)
	}
	if n.Type != cur.Type {
		return false
	}
	if n.Type == xmldom.ElementNode || n.Type == xmldom.AttributeNode {
		return n.Name == cur.Name && n.URI == cur.URI
	}
	return true
}

func (ni *numberInstr) exec(c *execCtx) error {
	var nums []int
	if ni.value != nil {
		v, err := c.eval(ni.value)
		if err != nil {
			return err
		}
		f := math.Floor(xpath.ToNumber(v) + 0.5)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			c.out.text(xpath.NumberToString(f))
			return nil
		}
		nums = []int{int(f)}
	} else {
		xc := c.xp()
		cur := c.node
		isFrom := func(n *xmldom.Node) bool { return ni.from != nil && ni.from.Match(xc, n) }
		switch ni.level {
		case "any":
			count := 0
			// walk preceding nodes in reverse document order, then self
			var walk func(n *xmldom.Node) bool
			stop := false
			var all []*xmldom.Node
			var collect func(n *xmldom.Node)
			collect = func(n *xmldom.Node) {
				all = append(all, n)
				for _, ch := range n.Children {
					collect(ch)
				}
			}
			_ = walk
			collect(cur.Root())
			idx := -1
			for i, n := range all {
				if n == cur {
					idx = i
					break
				}
			}
			for i := idx; i >= 0 && !stop; i-- {
				n := all[i]
				if ni.matchCount(c, xc, n, cur) {
					count++
				}
				if isFrom(n) {
					stop = true
				}
			}
			if count > 0 {
				nums = []int{count}
			}
		default:
			var chain []*xmldom.Node
			for n := cur; n != nil; n = n.Parent {
				if isFrom(n) && n != cur {
					break
				}
				chain = append(chain, n)
			}
			var targets []*xmldom.Node
			for _, n := range chain {
				if ni.matchCount(c, xc, n, cur) {
					targets = append(targets, n)
					if ni.level == "single" {
						break
					}
				}
			}
			for i := len(targets) - 1; i >= 0; i-- {
				t := targets[i]
				pos := 1
				if t.Parent != nil {
					for _, sib := range t.Parent.Children {
						if sib == t {
							break
						}
						if ni.matchCount(c, xc, sib, cur) {
							pos++
						}
					}
				}
				nums = append(nums, pos)
			}
		}
	}
	format := "1"
	if ni.format != nil {
		var err error
		if format, err = ni.format.eval(c); err != nil {
			return err
		}
	}
	gsep, gsize := "", 0
	if ni.gsep != nil {
		gsep, _ = ni.gsep.eval(c)
	}
	if ni.gsize != nil {
		gs, _ := ni.gsize.eval(c)
		gsize, _ = strconv.Atoi(gs)
	}
	c.out.text(formatNumbers(nums, format, gsep, gsize))
	return nil
}

func formatNumbers(nums []int, format, gsep string, gsize int) string {
	if len(nums) == 0 {
		return ""
	}
	// tokenise the format into alternating separators and format tokens
	var seps, toks []string
	rs := []rune(format)
	i := 0
	isAlnum := func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }
	start := i
	for i < len(rs) && !isAlnum(rs[i]) {
		i++
	}
	prefix := string(rs[start:i])
	for i < len(rs) {
		start = i
		for i < len(rs) && isAlnum(rs[i]) {
			i++
		}
		toks = append(toks, string(rs[start:i]))
		start = i
		for i < len(rs) && !isAlnum(rs[i]) {
			i++
		}
		seps = append(seps, string(rs[start:i]))
	}
	if len(toks) == 0 {
		toks = []string{"1"}
		seps = []string{""}
	}
	suffix := seps[len(seps)-1]
	seps = seps[:len(seps)-1]
	var sb strings.Builder
	sb.WriteString(prefix)
	for k, n := range nums {
		ti := k
		if ti >= len(toks) {
			ti = len(toks) - 1
		}
		if k > 0 {
			si := k - 1
			if si >= len(seps) {
				if len(seps) > 0 {
					si = len(seps) - 1
					sb.WriteString(seps[si])
				} else {
					sb.WriteString(".")
				}
			} else {
				sb.WriteString(seps[si])
			}
		}
		sb.WriteString(formatOne(n, toks[ti], gsep, gsize))
	}
	sb.WriteString(suffix)
	return sb.String()
}

func formatOne(n int, tok, gsep string, gsize int) string {
	switch tok {
	case "a", "A":
		return alpha(n, tok == "A")
	case "i", "I":
		r := roman(n)
		if tok == "i" {
			return strings.ToLower(r)
		}
		return r
	}
	width := len(tok)
	s := strconv.Itoa(n)
	if r := []rune(tok); len(r) > 0 && unicode.IsDigit(r[len(r)-1]) {
		width = len(r)
	} else {
		width = 1
	}
	for len(s) < width {
		s = "0" + s
	}
	if gsep != "" && gsize > 0 {
		var sb strings.Builder
		for i, r := range s {
			if i > 0 && (len(s)-i)%gsize == 0 {
				sb.WriteString(gsep)
			}
			sb.WriteRune(r)
		}
		s = sb.String()
	}
	return s
}

func alpha(n int, upper bool) string {
	if n <= 0 {
		return strconv.Itoa(n)
	}
	var out []byte
	base := byte('a')
	if upper {
		base = 'A'
	}
	for n > 0 {
		n--
		out = append([]byte{base + byte(n%26)}, out...)
		n /= 26
	}
	return string(out)
}

func roman(n int) string {
	if n <= 0 || n >= 5000 {
		return strconv.Itoa(n)
	}
	vals := []int{1000, 900, 500, 400, 100, 90, 50, 40, 10, 9, 5, 4, 1}
	syms := []string{"M", "CM", "D", "CD", "C", "XC", "L", "XL", "X", "IX", "V", "IV", "I"}
	var sb strings.Builder
	for i, v := range vals {
		for n >= v {
			sb.WriteString(syms[i])
			n -= v
		}
	}
	return sb.String()
}

var _ = fmt.Sprintf
