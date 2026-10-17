package xhr

import (
	_ "embed"
	"encoding/binary"
)

// The line break rules of java.text.BreakIterator.getLineInstance() (JDK's
// RuleBasedBreakIterator tables, extracted from OpenJDK) drive text wrapping,
// exactly like Flying Saucer on the JVM.

//go:embed linebreak.bin
var lineBreakBin []byte

type lbTables struct {
	state []int16
	end   []bool
	look  []bool
	nc    int
	cat   [65536]int8
}

var lbData *lbTables

func loadLB() *lbTables {
	if lbData != nil {
		return lbData
	}
	b := lineBreakBin
	t := &lbTables{}
	p := 0
	n := int(binary.BigEndian.Uint32(b[p:]))
	p += 4
	t.state = make([]int16, n)
	for i := range t.state {
		t.state[i] = int16(binary.BigEndian.Uint16(b[p:]))
		p += 2
	}
	n = int(binary.BigEndian.Uint32(b[p:]))
	p += 4
	t.end = make([]bool, n)
	for i := range t.end {
		t.end[i] = b[p] != 0
		p++
	}
	n = int(binary.BigEndian.Uint32(b[p:]))
	p += 4
	t.look = make([]bool, n)
	for i := range t.look {
		t.look[i] = b[p] != 0
		p++
	}
	t.nc = int(binary.BigEndian.Uint32(b[p:]))
	p += 4
	for i := range t.cat {
		t.cat[i] = int8(b[p])
		p++
	}
	lbData = t
	return t
}

const lbIgnore = -1

// jdkLineBreaks returns the boundaries reported by BreakIterator.getLineInstance()
// for text (ascending, ending with len(text)).
func jdkLineBreaks(text string) []int {
	rs := []rune(text)
	return jdkLineBreaksRunes(rs)
}

func jdkLineBreaksRunes(rs []rune) []int {
	t := loadLB()
	n := len(rs)
	if n == 0 {
		return nil
	}
	var out []int
	pos := 0
	for pos < n {
		pos = lbHandleNext(t, rs, pos)
		out = append(out, pos)
	}
	return out
}

func lbCategory(t *lbTables, r rune) int {
	if r >= 0x10000 {
		return int(t.cat[0xFFFD])
	}
	return int(t.cat[r])
}

// lbHandleNext mirrors RuleBasedBreakIterator.handleNext starting at index start.
func lbHandleNext(t *lbTables, rs []rune, start int) int {
	n := len(rs)
	idx := start
	result := idx + 1
	lookaheadResult := 0
	state := 1
	var c rune
	haveC := idx < n
	if haveC {
		c = rs[idx]
	}
	for haveC && state != 0 {
		category := lbCategory(t, c)
		if category != lbIgnore {
			state = int(t.state[state*t.nc+category])
		}
		if t.look[state] {
			if t.end[state] {
				result = lookaheadResult
			} else {
				lookaheadResult = idx + 1
			}
		} else if t.end[state] {
			result = idx + 1
		}
		idx++
		if idx < n {
			c = rs[idx]
		} else {
			haveC = false
		}
	}
	if !haveC && lookaheadResult == n {
		result = lookaheadResult
	}
	return result
}
