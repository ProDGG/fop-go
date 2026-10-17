package xpath

import (
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/ProDGG/fop-go/xmldom"
)

// NodeSet is an XPath node-set in document order without duplicates.
type NodeSet []*xmldom.Node

// Value is one of NodeSet, string, float64 or bool.
type Value interface{}

func parseNumber(s string) (float64, bool) {
	s = strings.Trim(s, " \t\r\n")
	if s == "" {
		return 0, false
	}
	i := 0
	if s[0] == '-' {
		i = 1
	}
	digits, dot := 0, false
	for j := i; j < len(s); j++ {
		c := s[j]
		switch {
		case c >= '0' && c <= '9':
			digits++
		case c == '.' && !dot:
			dot = true
		default:
			return 0, false
		}
	}
	if digits == 0 {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

// ToString converts a value per the XPath string() function.
func ToString(v Value) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return NumberToString(x)
	case bool:
		if x {
			return "true"
		}
		return "false"
	case NodeSet:
		if len(x) == 0 {
			return ""
		}
		return x[0].StringValue()
	case nil:
		return ""
	}
	return ""
}

// NumberToString formats a number per XPath rules.
func NumberToString(f float64) string {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	case f == 0:
		return "0"
	}
	if f == math.Trunc(f) && math.Abs(f) < 1e18 {
		return strconv.FormatFloat(f, 'f', 0, 64)
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// ToNumber converts a value per the XPath number() function.
func ToNumber(v Value) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case bool:
		if x {
			return 1
		}
		return 0
	case string:
		if f, ok := parseNumber(x); ok {
			return f
		}
		return math.NaN()
	case NodeSet:
		return ToNumber(ToString(x))
	}
	return math.NaN()
}

// ToBool converts a value per the XPath boolean() function.
func ToBool(v Value) bool {
	switch x := v.(type) {
	case bool:
		return x
	case float64:
		return x != 0 && !math.IsNaN(x)
	case string:
		return x != ""
	case NodeSet:
		return len(x) > 0
	}
	return false
}

// SortNodes sorts a slice into document order and removes duplicates.
func SortNodes(ns []*xmldom.Node) NodeSet {
	if len(ns) < 2 {
		return NodeSet(ns)
	}
	sorted := true
	for i := 1; i < len(ns); i++ {
		if ns[i-1].Order() >= ns[i].Order() {
			sorted = false
			break
		}
	}
	if sorted {
		return NodeSet(ns)
	}
	sort.SliceStable(ns, func(i, j int) bool { return ns[i].Order() < ns[j].Order() })
	out := ns[:1]
	for _, n := range ns[1:] {
		if n != out[len(out)-1] {
			out = append(out, n)
		}
	}
	return NodeSet(out)
}
