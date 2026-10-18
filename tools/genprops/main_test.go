package main

import (
	"strings"
	"testing"
)

func TestGenerate(t *testing.T) {
	src := `<root>
<property><name>color</name><inherited>true</inherited><default>black</default></property>
<property><name>margin-top</name><inherited>false</inherited><default>0pt</default></property>
<property><name>border-before-width</name><default contextdep="true">medium</default></property>
<property><name>noname</name></property>
</root>`
	out := generate(src, "abc123")
	for _, want := range []string{`(rev abc123)`, `"color": true,`, `"color": "black",`, `"margin-top": "0pt",`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, "border-before-width") || strings.Contains(out, `"margin-top": true`) {
		t.Errorf("unexpected entries:\n%s", out)
	}
}
