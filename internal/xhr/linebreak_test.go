package xhr

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestLineBreakAgainstJava compares with a corpus broken by java.text.BreakIterator
// (set XHR_BREAK_CORPUS and XHR_BREAK_JAVA).
func TestLineBreakAgainstJava(t *testing.T) {
	cp, jp := os.Getenv("XHR_BREAK_CORPUS"), os.Getenv("XHR_BREAK_JAVA")
	if cp == "" || jp == "" {
		t.Skip("corpus not set")
	}
	cf, _ := os.Open(cp)
	jf, _ := os.Open(jp)
	cs, js := bufio.NewScanner(cf), bufio.NewScanner(jf)
	cs.Buffer(make([]byte, 1<<20), 1<<20)
	bad := 0
	for cs.Scan() && js.Scan() {
		line := cs.Text()
		var got []string
		for _, b := range jdkLineBreaks(line) {
			got = append(got, fmt.Sprint(b))
		}
		if strings.Join(got, " ") != js.Text() {
			bad++
			if bad < 10 {
				t.Errorf("%q: go=%v java=%q", line, got, js.Text())
			}
		}
	}
	if bad > 0 {
		t.Fatalf("%d lines differ", bad)
	}
}
