package xpath

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ProDGG/fop-go/xmldom"
)

func (ev *evaluator) call(fc *funcCall, ctx *Context) (Value, error) {
	args := make([]Value, len(fc.args))
	// lazy handling is not needed in XPath 1.0 (no short-circuit in function args)
	for i, a := range fc.args {
		v, err := ev.eval(a, ctx)
		if err != nil {
			return nil, err
		}
		args[i] = v
	}
	if fc.uri != "" || strings.Contains(fc.qname, ":") {
		if ctx.Funcs != nil {
			if v, ok, err := ctx.Funcs(ctx, fc.uri, fc.local, args); ok || err != nil {
				return v, err
			}
		}
		if v, ok, err := builtinExt(fc.uri, fc.local, ctx, args); ok || err != nil {
			return v, err
		}
		return nil, fmt.Errorf("xpath: unknown function %s()", fc.qname)
	}
	if ctx.Funcs != nil {
		if v, ok, err := ctx.Funcs(ctx, "", fc.local, args); ok || err != nil {
			return v, err
		}
	}
	v, ok, err := coreFunc(fc.local, ctx, args)
	if err != nil {
		return nil, fmt.Errorf("xpath: %s(): %w", fc.local, err)
	}
	if !ok {
		return nil, fmt.Errorf("xpath: unknown function %s()", fc.local)
	}
	return v, nil
}

// HasCoreFunction reports whether name is a built-in function.
func HasCoreFunction(name string) bool {
	_, ok, _ := coreFunc(name, &Context{Node: xmldom.NewDocument()}, nil)
	if ok {
		return true
	}
	_, known := coreNames[name]
	return known
}

var coreNames = map[string]struct{}{}

func init() {
	for _, n := range strings.Fields(`last position count id local-name namespace-uri name string concat starts-with
		contains substring-before substring-after substring string-length normalize-space translate boolean not true
		false lang number sum floor ceiling round upper-case lower-case ends-with abs matches replace string-join
		current-date current-dateTime current-time min max`) {
		coreNames[n] = struct{}{}
	}
}

func argStr(ctx *Context, args []Value, i int) string {
	if i < len(args) {
		return ToString(args[i])
	}
	return ctx.Node.StringValue()
}

func needArgs(name string, args []Value, min, max int) error {
	if len(args) < min || (max >= 0 && len(args) > max) {
		return fmt.Errorf("wrong number of arguments (%d)", len(args))
	}
	return nil
}

func nodeArg(ctx *Context, args []Value) (*xmldom.Node, bool, error) {
	if len(args) == 0 {
		return ctx.Node, true, nil
	}
	ns, ok := args[0].(NodeSet)
	if !ok {
		return nil, false, fmt.Errorf("argument must be a node-set")
	}
	if len(ns) == 0 {
		return nil, false, nil
	}
	return ns[0], true, nil
}

func coreFunc(name string, ctx *Context, args []Value) (Value, bool, error) {
	switch name {
	case "last":
		return float64(ctx.Size), true, nil
	case "position":
		return float64(ctx.Pos), true, nil
	case "count":
		if err := needArgs(name, args, 1, 1); err != nil {
			return nil, true, err
		}
		ns, ok := args[0].(NodeSet)
		if !ok {
			return nil, true, fmt.Errorf("argument must be a node-set")
		}
		return float64(len(ns)), true, nil
	case "id":
		return NodeSet(nil), true, nil
	case "local-name", "namespace-uri", "name":
		n, ok, err := nodeArg(ctx, args)
		if err != nil {
			return nil, true, err
		}
		if !ok {
			return "", true, nil
		}
		switch name {
		case "local-name":
			if n.Type == xmldom.ElementNode || n.Type == xmldom.AttributeNode || n.Type == xmldom.PINode {
				return n.Name, true, nil
			}
			return "", true, nil
		case "namespace-uri":
			return n.URI, true, nil
		default:
			if n.Type == xmldom.ElementNode || n.Type == xmldom.AttributeNode {
				return n.QName(), true, nil
			}
			if n.Type == xmldom.PINode {
				return n.Name, true, nil
			}
			return "", true, nil
		}
	case "string":
		if len(args) == 0 {
			return ctx.Node.StringValue(), true, nil
		}
		return ToString(args[0]), true, nil
	case "concat":
		if len(args) < 2 {
			return nil, true, fmt.Errorf("concat requires at least 2 arguments")
		}
		var sb strings.Builder
		for _, a := range args {
			sb.WriteString(ToString(a))
		}
		return sb.String(), true, nil
	case "starts-with":
		if err := needArgs(name, args, 2, 2); err != nil {
			return nil, true, err
		}
		return strings.HasPrefix(ToString(args[0]), ToString(args[1])), true, nil
	case "ends-with":
		if err := needArgs(name, args, 2, 2); err != nil {
			return nil, true, err
		}
		return strings.HasSuffix(ToString(args[0]), ToString(args[1])), true, nil
	case "contains":
		if err := needArgs(name, args, 2, 2); err != nil {
			return nil, true, err
		}
		return strings.Contains(ToString(args[0]), ToString(args[1])), true, nil
	case "substring-before":
		if err := needArgs(name, args, 2, 2); err != nil {
			return nil, true, err
		}
		s, sep := ToString(args[0]), ToString(args[1])
		if i := strings.Index(s, sep); i >= 0 {
			return s[:i], true, nil
		}
		return "", true, nil
	case "substring-after":
		if err := needArgs(name, args, 2, 2); err != nil {
			return nil, true, err
		}
		s, sep := ToString(args[0]), ToString(args[1])
		if i := strings.Index(s, sep); i >= 0 {
			return s[i+len(sep):], true, nil
		}
		return "", true, nil
	case "substring":
		if err := needArgs(name, args, 2, 3); err != nil {
			return nil, true, err
		}
		return substring(ToString(args[0]), args[1:]), true, nil
	case "string-length":
		return float64(utf8.RuneCountInString(argStr(ctx, args, 0))), true, nil
	case "normalize-space":
		return strings.Join(strings.Fields(argStr(ctx, args, 0)), " "), true, nil
	case "translate":
		if err := needArgs(name, args, 3, 3); err != nil {
			return nil, true, err
		}
		return translate(ToString(args[0]), ToString(args[1]), ToString(args[2])), true, nil
	case "upper-case":
		return strings.ToUpper(argStr(ctx, args, 0)), true, nil
	case "lower-case":
		return strings.ToLower(argStr(ctx, args, 0)), true, nil
	case "boolean":
		if err := needArgs(name, args, 1, 1); err != nil {
			return nil, true, err
		}
		return ToBool(args[0]), true, nil
	case "not":
		if err := needArgs(name, args, 1, 1); err != nil {
			return nil, true, err
		}
		return !ToBool(args[0]), true, nil
	case "true":
		return true, true, nil
	case "false":
		return false, true, nil
	case "lang":
		if err := needArgs(name, args, 1, 1); err != nil {
			return nil, true, err
		}
		want := strings.ToLower(ToString(args[0]))
		for n := ctx.Node; n != nil; n = n.Parent {
			if n.Type == xmldom.ElementNode {
				if l := n.AttrNS(xmldom.XMLNS, "lang"); l != "" {
					l = strings.ToLower(l)
					return l == want || strings.HasPrefix(l, want+"-"), true, nil
				}
			}
		}
		return false, true, nil
	case "number":
		if len(args) == 0 {
			return ToNumber(ctx.Node.StringValue()), true, nil
		}
		return ToNumber(args[0]), true, nil
	case "sum":
		if err := needArgs(name, args, 1, 1); err != nil {
			return nil, true, err
		}
		ns, ok := args[0].(NodeSet)
		if !ok {
			return nil, true, fmt.Errorf("argument must be a node-set")
		}
		sum := 0.0
		for _, n := range ns {
			sum += ToNumber(n.StringValue())
		}
		return sum, true, nil
	case "min", "max":
		if err := needArgs(name, args, 1, 1); err != nil {
			return nil, true, err
		}
		ns, ok := args[0].(NodeSet)
		if !ok {
			return nil, true, fmt.Errorf("argument must be a node-set")
		}
		if len(ns) == 0 {
			return math.NaN(), true, nil
		}
		best := ToNumber(ns[0].StringValue())
		for _, n := range ns[1:] {
			f := ToNumber(n.StringValue())
			if math.IsNaN(f) {
				return math.NaN(), true, nil
			}
			if (name == "min" && f < best) || (name == "max" && f > best) {
				best = f
			}
		}
		return best, true, nil
	case "floor":
		if err := needArgs(name, args, 1, 1); err != nil {
			return nil, true, err
		}
		return math.Floor(ToNumber(args[0])), true, nil
	case "ceiling":
		if err := needArgs(name, args, 1, 1); err != nil {
			return nil, true, err
		}
		return math.Ceil(ToNumber(args[0])), true, nil
	case "round":
		if err := needArgs(name, args, 1, 1); err != nil {
			return nil, true, err
		}
		f := ToNumber(args[0])
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return f, true, nil
		}
		return math.Floor(f + 0.5), true, nil
	case "abs":
		if err := needArgs(name, args, 1, 1); err != nil {
			return nil, true, err
		}
		return math.Abs(ToNumber(args[0])), true, nil
	case "matches":
		if err := needArgs(name, args, 2, 3); err != nil {
			return nil, true, err
		}
		re, err := compileRegex(ToString(args[1]), flagArg(args, 2))
		if err != nil {
			return nil, true, err
		}
		return re.MatchString(ToString(args[0])), true, nil
	case "replace":
		if err := needArgs(name, args, 3, 4); err != nil {
			return nil, true, err
		}
		re, err := compileRegex(ToString(args[1]), flagArg(args, 3))
		if err != nil {
			return nil, true, err
		}
		repl := regexp.MustCompile(`\$(\d+)`).ReplaceAllString(ToString(args[2]), "$${$1}")
		return re.ReplaceAllString(ToString(args[0]), repl), true, nil
	case "string-join":
		if err := needArgs(name, args, 1, 2); err != nil {
			return nil, true, err
		}
		sep := ""
		if len(args) == 2 {
			sep = ToString(args[1])
		}
		ns, ok := args[0].(NodeSet)
		if !ok {
			return ToString(args[0]), true, nil
		}
		parts := make([]string, len(ns))
		for i, n := range ns {
			parts[i] = n.StringValue()
		}
		return strings.Join(parts, sep), true, nil
	case "current-dateTime":
		return time.Now().Format("2006-01-02T15:04:05Z07:00"), true, nil
	case "current-date":
		return time.Now().Format("2006-01-02"), true, nil
	case "current-time":
		return time.Now().Format("15:04:05Z07:00"), true, nil
	}
	return nil, false, nil
}

func flagArg(args []Value, i int) string {
	if i < len(args) {
		return ToString(args[i])
	}
	return ""
}

func compileRegex(pat, flags string) (*regexp.Regexp, error) {
	prefix := ""
	for _, f := range flags {
		switch f {
		case 'i', 'm', 's':
			prefix += string(f)
		case 'x':
			pat = regexp.MustCompile(`\s+`).ReplaceAllString(pat, "")
		}
	}
	if prefix != "" {
		pat = "(?" + prefix + ")" + pat
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return nil, fmt.Errorf("bad regular expression %q: %v", pat, err)
	}
	return re, nil
}

func xpRound(f float64) float64 { return math.Floor(f + 0.5) }

func substring(s string, a []Value) string {
	runes := []rune(s)
	start := xpRound(ToNumber(a[0]))
	var end float64
	if len(a) > 1 {
		end = start + xpRound(ToNumber(a[1]))
	} else {
		end = math.Inf(1)
	}
	if math.IsNaN(start) || math.IsNaN(end) {
		return ""
	}
	var sb strings.Builder
	for i, r := range runes {
		p := float64(i + 1)
		if p >= start && p < end {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

func translate(s, from, to string) string {
	fr, tr := []rune(from), []rune(to)
	var sb strings.Builder
	for _, r := range s {
		idx := -1
		for i, f := range fr {
			if f == r {
				idx = i
				break
			}
		}
		switch {
		case idx < 0:
			sb.WriteRune(r)
		case idx < len(tr):
			sb.WriteRune(tr[idx])
		}
	}
	return sb.String()
}

// builtinExt provides a few EXSLT-style functions under any namespace.
func builtinExt(uri, local string, ctx *Context, args []Value) (Value, bool, error) {
	switch local {
	case "node-set":
		if len(args) == 1 {
			if ns, ok := args[0].(NodeSet); ok {
				return ns, true, nil
			}
			doc := xmldom.NewDocument()
			doc.AppendChild(xmldom.NewText(ToString(args[0])))
			return NodeSet{doc}, true, nil
		}
	case "object-type":
		if len(args) == 1 {
			switch args[0].(type) {
			case NodeSet:
				return "node-set", true, nil
			case string:
				return "string", true, nil
			case float64:
				return "number", true, nil
			case bool:
				return "boolean", true, nil
			}
		}
	case "upper-case", "lower-case", "replace", "matches", "ends-with", "abs", "min", "max", "string-join":
		return coreFunc(local, ctx, args)
	case "date-time", "dateTime":
		return time.Now().Format("2006-01-02T15:04:05Z07:00"), true, nil
	case "date":
		return time.Now().Format("2006-01-02"), true, nil
	case "capitalize":
		s := ToString(argOr(args, 0))
		for i, r := range s {
			return string(unicode.ToUpper(r)) + s[i+utf8.RuneLen(r):], true, nil
		}
		return "", true, nil
	}
	return nil, false, nil
}

func argOr(args []Value, i int) Value {
	if i < len(args) {
		return args[i]
	}
	return ""
}
