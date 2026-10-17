package xpath

import (
	"testing"

	"github.com/ProDGG/fop-go/xmldom"
)

const doc = `<root xmlns:a="urn:a"><item id="1" n="5">one</item><item id="2" n="7">two<b>x</b></item><a:z/><item id="3" n="9"/></root>`

func eval(t *testing.T, src string) Value {
	t.Helper()
	d, err := xmldom.ParseString(doc)
	if err != nil {
		t.Fatal(err)
	}
	ns := func(p string) (string, bool) {
		if p == "a" {
			return "urn:a", true
		}
		return "", false
	}
	e, err := Compile(src, ns)
	if err != nil {
		t.Fatalf("compile %q: %v", src, err)
	}
	v, err := e.Eval(&Context{Node: d, Pos: 1, Size: 1})
	if err != nil {
		t.Fatalf("eval %q: %v", src, err)
	}
	return v
}

func TestExpressions(t *testing.T) {
	cases := []struct {
		src  string
		want string
	}{
		{"count(/root/item)", "3"},
		{"count(//item)", "3"},
		{"/root/item[2]/@id", "2"},
		{"/root/item[last()]/@n", "9"},
		{"sum(//item/@n)", "21"},
		{"//item[@n > 6][1]/@id", "2"},
		{"string(/root/item[2])", "twox"},
		{"concat('a', 'b', 1 + 2)", "ab3"},
		{"substring('12345', 2, 3)", "234"},
		{"substring-before('a-b','-')", "a"},
		{"translate('abc','abc','AB')", "AB"},
		{"normalize-space('  a   b ')", "a b"},
		{"count(/root/a:z)", "1"},
		{"count(/root/*)", "4"},
		{"7 mod 3", "1"},
		{"7 div 2", "3.5"},
		{"boolean(//item[@id='2'])", "true"},
		{"name(/root/*[3])", "a:z"},
		{"//b/ancestor::*[1]/@id", "2"},
		{"count(//item[2]/following-sibling::*)", "2"},
		{"count(//item[3]/preceding-sibling::item)", "2"},
		{"count(//item | //b)", "4"},
		{"round(2.5)", "3"},
		{"-1 * 3", "-3"},
		{"string(1 div 0)", "Infinity"},
		{"//item[position() = 2]/@id", "2"},
		{"count(//node())", "9"},
		{"string-length('привет')", "6"},
		{"upper-case('abc')", "ABC"},
		{"count(//item[not(@n=7)])", "2"},
		{"count(/root/item[1]/following::*)", "4"},
		{"count(//b/preceding::*)", "1"},
	}
	for _, c := range cases {
		got := ToString(eval(t, c.src))
		if got != c.want {
			t.Errorf("%s = %q, want %q", c.src, got, c.want)
		}
	}
}
