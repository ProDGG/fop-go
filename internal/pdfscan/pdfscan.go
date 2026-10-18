// Package pdfscan extracts text origins from PDF page content streams. It
// understands q/Q/cm/BT/Tm/Td/Tf and is meant for comparing PDFs produced by
// fop-go, Apache FOP, Flying Saucer + iText and similar writers.
package pdfscan

import (
	"bytes"
	"compress/zlib"
	"io"
	"os"
	"regexp"
	"strconv"
)

// Text is one text-showing operation: the origin on the PDF page (y up), the
// font size and the full text matrix.
type Text struct {
	X, Y, Size float64
	M          [6]float64
}

// Streams returns the decoded content streams that contain text operators.
func Streams(pdf []byte) [][]byte {
	var res [][]byte
	rest := pdf
	for {
		i := bytes.Index(rest, []byte("stream"))
		if i < 0 {
			break
		}
		// skip "endstream" occurrences
		if i >= 3 && string(rest[i-3:i]) == "end" {
			rest = rest[i+6:]
			continue
		}
		s := i + 6
		if s < len(rest) && rest[s] == '\r' {
			s++
		}
		if s < len(rest) && rest[s] == '\n' {
			s++
		}
		e := bytes.Index(rest[s:], []byte("endstream"))
		if e < 0 {
			break
		}
		body := rest[s : s+e]
		rest = rest[s+e+9:]
		var data []byte
		if zr, err := zlib.NewReader(bytes.NewReader(body)); err == nil {
			if d, err := io.ReadAll(zr); err == nil {
				data = d
			}
		}
		if data == nil {
			if bytes.Contains(body, []byte(" BT")) || bytes.HasPrefix(body, []byte("BT")) {
				data = body
			} else {
				continue
			}
		}
		if bytes.Contains(data, []byte("BT")) && bytes.Contains(data, []byte("Tf")) {
			res = append(res, data)
		}
	}
	return res
}

var tokRe = regexp.MustCompile(`\[[^\]]*\]|<[0-9A-Fa-f]*>|\([^)]*\)|/[^\s/\[\]()<>]+|[^\s/\[\]()<>]+`)

func mul(a, b [6]float64) [6]float64 {
	return [6]float64{
		a[0]*b[0] + a[1]*b[2], a[0]*b[1] + a[1]*b[3], a[2]*b[0] + a[3]*b[2], a[2]*b[1] + a[3]*b[3],
		a[4]*b[0] + a[5]*b[2] + b[4], a[4]*b[1] + a[5]*b[3] + b[5],
	}
}

// Run interprets one content stream.
func Run(stream []byte) []Text {
	toks := tokRe.FindAll(stream, -1)
	ctm := [6]float64{1, 0, 0, 1, 0, 0}
	tm := ctm
	var stack [][6]float64
	var st [][]byte
	var out []Text
	size := 0.0
	intext := false
	num := func(k int) float64 { // k-th token from the end
		if len(st) < k {
			return 0
		}
		f, _ := strconv.ParseFloat(string(st[len(st)-k]), 64)
		return f
	}
	six := func() [6]float64 {
		var a [6]float64
		for i := 0; i < 6; i++ {
			a[i] = num(6 - i)
		}
		return a
	}
	for _, t := range toks {
		switch string(t) {
		case "q":
			stack = append(stack, ctm)
		case "Q":
			if len(stack) > 0 {
				ctm = stack[len(stack)-1]
				stack = stack[:len(stack)-1]
			}
		case "cm":
			ctm = mul(six(), ctm)
		case "BT":
			intext = true
			tm = [6]float64{1, 0, 0, 1, 0, 0}
		case "Tm":
			tm = six()
		case "Td":
			if intext {
				dx, dy := num(2), num(1)
				tm[4] += dx*tm[0] + dy*tm[2]
				tm[5] += dx*tm[1] + dy*tm[3]
			}
		case "Tf":
			size = num(1)
		case "Tj", "TJ":
			m := mul(tm, ctm)
			out = append(out, Text{m[4], m[5], size, m})
		case "ET":
			intext = false
		}
		st = append(st, t)
		if len(st) > 12 {
			st = st[len(st)-12:]
		}
	}
	return out
}

// Pages returns the text operations of every page content stream.
func Pages(pdf []byte) [][]Text {
	var pages [][]Text
	for _, s := range Streams(pdf) {
		pages = append(pages, Run(s))
	}
	return pages
}

// File reads a PDF and returns Pages.
func File(path string) ([][]Text, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Pages(b), nil
}
