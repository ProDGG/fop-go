// Package xpath implements an XPath 1.0 parser and evaluator over xmldom trees,
// with a handful of common XPath 2.0 / EXSLT string helpers.
package xpath

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

type tokKind int

const (
	tEOF tokKind = iota
	tNum
	tLit
	tName   // NCName, QName or prefix:*  (text holds it)
	tOpName // and or mod div
	tStar   // wildcard *
	tMul    // multiply operator
	tAxis   // axis name followed by ::
	tFunc   // function name or node-type test followed by (
	tDollar // $
	tLParen // (
	tRParen // )
	tLBrack // [
	tRBrack // ]
	tDot    // .
	tDotDot // ..
	tAt     // @
	tComma  // ,
	tSlash  // /
	tDSlash // //
	tPipe   // |
	tPlus   // +
	tMinus  // -
	tEq     // =
	tNe     // !=
	tLt     // <
	tLe     // <=
	tGt     // >
	tGe     // >=
	tColCol // ::  (unused: folded into tAxis)
)

type token struct {
	kind tokKind
	text string
	num  float64
	pos  int
}

type lexer struct {
	src  string
	pos  int
	toks []token
}

func isNCNameStart(r rune) bool {
	return r == '_' || unicode.IsLetter(r)
}

func isNCNameChar(r rune) bool {
	return r == '_' || r == '-' || r == '.' || unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Mc, r) || r == 0xB7
}

func tokenize(src string) ([]token, error) {
	lx := &lexer{src: src}
	for {
		lx.skipSpace()
		if lx.pos >= len(lx.src) {
			lx.toks = append(lx.toks, token{kind: tEOF, pos: lx.pos})
			return lx.toks, nil
		}
		if err := lx.next(); err != nil {
			return nil, err
		}
	}
}

func (lx *lexer) skipSpace() {
	for lx.pos < len(lx.src) {
		c := lx.src[lx.pos]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			lx.pos++
		} else {
			return
		}
	}
}

// prevAllowsOperand reports whether the previous token means that the next
// token starts an operand (so '*' is a wildcard and names are not operators).
func (lx *lexer) operandExpected() bool {
	if len(lx.toks) == 0 {
		return true
	}
	switch lx.toks[len(lx.toks)-1].kind {
	case tAt, tAxis, tLParen, tLBrack, tComma, tOpName, tMul, tSlash, tDSlash, tPipe, tPlus, tMinus,
		tEq, tNe, tLt, tLe, tGt, tGe, tDollar:
		return true
	}
	return false
}

func (lx *lexer) emit(k tokKind, text string, start int) {
	lx.toks = append(lx.toks, token{kind: k, text: text, pos: start})
}

func (lx *lexer) next() error {
	start := lx.pos
	c := lx.src[lx.pos]
	switch c {
	case '(':
		lx.pos++
		lx.emit(tLParen, "(", start)
	case ')':
		lx.pos++
		lx.emit(tRParen, ")", start)
	case '[':
		lx.pos++
		lx.emit(tLBrack, "[", start)
	case ']':
		lx.pos++
		lx.emit(tRBrack, "]", start)
	case ',':
		lx.pos++
		lx.emit(tComma, ",", start)
	case '@':
		lx.pos++
		lx.emit(tAt, "@", start)
	case '|':
		lx.pos++
		lx.emit(tPipe, "|", start)
	case '+':
		lx.pos++
		lx.emit(tPlus, "+", start)
	case '-':
		lx.pos++
		lx.emit(tMinus, "-", start)
	case '=':
		lx.pos++
		lx.emit(tEq, "=", start)
	case '$':
		lx.pos++
		lx.emit(tDollar, "$", start)
	case '!':
		if strings.HasPrefix(lx.src[lx.pos:], "!=") {
			lx.pos += 2
			lx.emit(tNe, "!=", start)
		} else {
			return fmt.Errorf("xpath: unexpected '!' at %d in %q", start, lx.src)
		}
	case '<':
		if strings.HasPrefix(lx.src[lx.pos:], "<=") {
			lx.pos += 2
			lx.emit(tLe, "<=", start)
		} else {
			lx.pos++
			lx.emit(tLt, "<", start)
		}
	case '>':
		if strings.HasPrefix(lx.src[lx.pos:], ">=") {
			lx.pos += 2
			lx.emit(tGe, ">=", start)
		} else {
			lx.pos++
			lx.emit(tGt, ">", start)
		}
	case '/':
		if strings.HasPrefix(lx.src[lx.pos:], "//") {
			lx.pos += 2
			lx.emit(tDSlash, "//", start)
		} else {
			lx.pos++
			lx.emit(tSlash, "/", start)
		}
	case '*':
		lx.pos++
		if lx.operandExpected() {
			lx.emit(tStar, "*", start)
		} else {
			lx.emit(tMul, "*", start)
		}
	case '"', '\'':
		end := strings.IndexByte(lx.src[lx.pos+1:], c)
		if end < 0 {
			return fmt.Errorf("xpath: unterminated string literal in %q", lx.src)
		}
		lit := lx.src[lx.pos+1 : lx.pos+1+end]
		lx.pos += end + 2
		lx.emit(tLit, lit, start)
	case '.':
		if strings.HasPrefix(lx.src[lx.pos:], "..") {
			lx.pos += 2
			lx.emit(tDotDot, "..", start)
		} else if lx.pos+1 < len(lx.src) && lx.src[lx.pos+1] >= '0' && lx.src[lx.pos+1] <= '9' {
			return lx.number()
		} else {
			lx.pos++
			lx.emit(tDot, ".", start)
		}
	default:
		if c >= '0' && c <= '9' {
			return lx.number()
		}
		r, _ := utf8.DecodeRuneInString(lx.src[lx.pos:])
		if isNCNameStart(r) {
			return lx.name()
		}
		return fmt.Errorf("xpath: unexpected character %q at %d in %q", r, start, lx.src)
	}
	return nil
}

func (lx *lexer) number() error {
	start := lx.pos
	for lx.pos < len(lx.src) && lx.src[lx.pos] >= '0' && lx.src[lx.pos] <= '9' {
		lx.pos++
	}
	if lx.pos < len(lx.src) && lx.src[lx.pos] == '.' {
		lx.pos++
		for lx.pos < len(lx.src) && lx.src[lx.pos] >= '0' && lx.src[lx.pos] <= '9' {
			lx.pos++
		}
	}
	text := lx.src[start:lx.pos]
	f, ok := parseNumber(text)
	if !ok {
		return fmt.Errorf("xpath: bad number %q", text)
	}
	lx.toks = append(lx.toks, token{kind: tNum, text: text, num: f, pos: start})
	return nil
}

func (lx *lexer) ncname() string {
	start := lx.pos
	for lx.pos < len(lx.src) {
		r, sz := utf8.DecodeRuneInString(lx.src[lx.pos:])
		if lx.pos == start {
			if !isNCNameStart(r) {
				break
			}
		} else if !isNCNameChar(r) {
			break
		}
		lx.pos += sz
	}
	return lx.src[start:lx.pos]
}

func (lx *lexer) name() error {
	start := lx.pos
	nc := lx.ncname()
	// operator names
	if !lx.operandExpected() {
		switch nc {
		case "and", "or", "mod", "div":
			lx.emit(tOpName, nc, start)
			return nil
		}
	}
	// axis?
	save := lx.pos
	lx.skipSpace()
	if strings.HasPrefix(lx.src[lx.pos:], "::") {
		lx.pos += 2
		lx.emit(tAxis, nc, start)
		return nil
	}
	lx.pos = save
	full := nc
	if lx.pos < len(lx.src) && lx.src[lx.pos] == ':' && !strings.HasPrefix(lx.src[lx.pos:], "::") {
		if lx.pos+1 < len(lx.src) && lx.src[lx.pos+1] == '*' {
			lx.pos += 2
			full = nc + ":*"
		} else {
			p := lx.pos
			lx.pos++
			if r, _ := utf8.DecodeRuneInString(lx.src[lx.pos:]); isNCNameStart(r) {
				loc := lx.ncname()
				full = nc + ":" + loc
			} else {
				lx.pos = p
			}
		}
	}
	// function / node-type?
	save = lx.pos
	lx.skipSpace()
	if lx.pos < len(lx.src) && lx.src[lx.pos] == '(' {
		lx.emit(tFunc, full, start)
		return nil
	}
	lx.pos = save
	lx.emit(tName, full, start)
	return nil
}
