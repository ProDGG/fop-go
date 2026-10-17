package xhr

import (
	"strings"
	"unicode/utf8"
)

// Offsets are rune indices (Java uses UTF-16 units; identical for BMP text).

func utf16Units(s string) []rune { return []rune(s) }

// urlAwareBreaks mirrors UrlAwareLineBreakIterator over the JDK boundaries.
type urlAwareBreaker struct {
	text    []rune
	bounds  []int
	bi      int // index into bounds of the delegate's next boundary
	dcur    int // delegate.current()
	rstart  int
	rstop   int
	done    bool
	started bool
}

func newURLAwareBreaker(text string) *urlAwareBreaker {
	return &urlAwareBreaker{text: utf16Units(text), bounds: jdkLineBreaks(text)}
}

const breakDone = -1

func (u *urlAwareBreaker) delegateNext() int {
	if u.bi >= len(u.bounds) {
		return breakDone
	}
	v := u.bounds[u.bi]
	u.bi++
	u.dcur = v
	return v
}

func (u *urlAwareBreaker) charAt(i int) rune {
	if i < 0 || i >= len(u.text) {
		return 0
	}
	return u.text[i]
}

func (u *urlAwareBreaker) sub(start, stop int) string {
	if start < 0 {
		start = 0
	}
	if stop > len(u.text) {
		stop = len(u.text)
	}
	if start > stop {
		return ""
	}
	return string(u.text[start:stop])
}

const breakingChars = ".,:;!?- \n\r\t/"

func rng(start, stop int) (int, int) {
	if stop < start {
		stop = start
	}
	return start, stop
}

// next returns the next break position or breakDone.
func (u *urlAwareBreaker) next() int {
	if !u.started {
		u.started = true
		u.rstart, u.rstop = 0, 0
		u.dcur = 0
	}
	sStart, sStop := u.rstart, u.rstop // searchRange
	if u.rstart == u.dcur {            // in sync
		nx := u.delegateNext()
		u.rstart, u.rstop = rng(u.rstart, nx)
		if nx == breakDone {
			return breakDone
		}
		// "://" check: Range(currentRange.getStop(), -1, 2)
		if u.sub(max(0, u.rstop-1), min(len(u.text), u.rstop+2)) == "://" {
			sStart = u.rstop + 2
			// advance delegate again
			nx2 := u.delegateNext()
			u.rstart, u.rstop = rng(u.rstart, nx2)
		}
	}
	sStart, sStop = rng(sStart, u.rstop)
	// trimSearchRange
	for sStart < u.rstop && strings.ContainsRune(breakingChars, u.charAt(sStart)) {
		sStart++
		sStop = max(sStop, sStart)
	}
	for sStop > sStart && strings.ContainsRune(breakingChars, u.charAt(sStop-1)) {
		sStop--
		if sStart > sStop {
			sStart = sStop
		}
	}
	slash := -1
	for i := sStart; i < len(u.text); i++ {
		if u.text[i] == '/' {
			slash = i
			break
		}
	}
	if !(slash > -1 && slash < sStop) {
		slash = -1
	}
	if slash > -1 {
		u.rstart = slash
	} else {
		u.rstart = u.dcur
	}
	u.rstop = max(u.rstop, u.rstart)
	return u.rstart
}

// lastRuneIndex helps tests.
func runeLen(s string) int { return utf8.RuneCountInString(s) }
