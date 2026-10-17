package xmldom

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// Parse reads an XML document into a tree.
func Parse(r io.Reader) (*Node, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return ParseBytes(data)
}

// ParseString parses an XML document held in a string.
func ParseString(s string) (*Node, error) { return ParseBytes([]byte(s)) }

// ParseBytes parses an XML document.
func ParseBytes(data []byte) (*Node, error) {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	d := xml.NewDecoder(bytes.NewReader(data))
	d.Strict = true
	d.Entity = xml.HTMLEntity
	d.CharsetReader = charsetReader
	doc := NewDocument()
	cur := doc
	for {
		tok, err := d.RawToken()
		if err == io.EOF {
			break
		}
		line, _ := d.InputPos()
		if err != nil {
			return nil, fmt.Errorf("xml: line %d: %w", line, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			el := newNode(ElementNode)
			el.Line = line
			el.Prefix, el.Name = t.Name.Space, t.Name.Local
			var plain []xml.Attr
			for _, a := range t.Attr {
				switch {
				case a.Name.Space == "" && a.Name.Local == "xmlns":
					el.NS = append(el.NS, NSDecl{"", a.Value})
				case a.Name.Space == "xmlns":
					el.NS = append(el.NS, NSDecl{a.Name.Local, a.Value})
				default:
					plain = append(plain, a)
				}
			}
			cur.AppendChild(el)
			// resolve element namespace
			if uri, ok := el.LookupNamespace(el.Prefix); ok {
				el.URI = uri
			} else if el.Prefix != "" {
				return nil, fmt.Errorf("xml: line %d: undeclared namespace prefix %q", line, el.Prefix)
			}
			for _, a := range plain {
				an := NewAttr(a.Name.Space, a.Name.Local, "", unescapeNewlines(a.Value))
				if a.Name.Space != "" {
					uri, ok := el.LookupNamespace(a.Name.Space)
					if !ok {
						return nil, fmt.Errorf("xml: line %d: undeclared namespace prefix %q", line, a.Name.Space)
					}
					an.URI = uri
				}
				an.Parent = el
				el.Attrs = append(el.Attrs, an)
			}
			cur = el
		case xml.EndElement:
			if cur.Type != ElementNode || cur.Name != t.Name.Local || cur.Prefix != t.Name.Space {
				return nil, fmt.Errorf("xml: line %d: unexpected end element </%s>", line, t.Name.Local)
			}
			cur = cur.Parent
		case xml.CharData:
			if cur == doc {
				if len(bytes.TrimSpace(t)) != 0 {
					return nil, fmt.Errorf("xml: line %d: text outside root element", line)
				}
				continue
			}
			tn := NewText(string(t))
			tn.Line = line
			cur.AppendChild(tn)
		case xml.Comment:
			c := NewComment(string(t))
			c.Parent = cur
			cur.Children = append(cur.Children, c)
		case xml.ProcInst:
			if t.Target == "xml" {
				continue
			}
			p := NewPI(t.Target, string(t.Inst))
			p.Parent = cur
			cur.Children = append(cur.Children, p)
		}
	}
	if cur != doc {
		return nil, fmt.Errorf("xml: unexpected EOF, element <%s> not closed", cur.Name)
	}
	if doc.DocumentElement() == nil {
		return nil, fmt.Errorf("xml: document has no root element")
	}
	return doc, nil
}

func unescapeNewlines(s string) string { return s }

func charsetReader(label string, input io.Reader) (io.Reader, error) {
	switch strings.ToLower(strings.ReplaceAll(label, "_", "-")) {
	case "utf-8", "utf8", "us-ascii", "ascii":
		return input, nil
	case "iso-8859-1", "latin1", "latin-1", "iso8859-1":
		return mapReader(input, nil), nil
	case "windows-1251", "cp1251", "win-1251":
		return mapReader(input, cp1251[:]), nil
	case "iso-8859-5":
		return mapReader(input, iso88595[:]), nil
	case "koi8-r":
		return mapReader(input, koi8r[:]), nil
	case "windows-1252", "cp1252":
		return mapReader(input, cp1252[:]), nil
	}
	return nil, fmt.Errorf("unsupported encoding %q", label)
}

// mapReader converts a single-byte encoding to UTF-8. high maps bytes 0x80..0xFF
// (nil means ISO-8859-1, identity).
func mapReader(r io.Reader, high []rune) io.Reader {
	data, err := io.ReadAll(r)
	if err != nil {
		return &errReader{err}
	}
	out := make([]byte, 0, len(data)+len(data)/4)
	for _, b := range data {
		if b < 0x80 {
			out = append(out, b)
			continue
		}
		rn := rune(b)
		if high != nil {
			rn = high[b-0x80]
		}
		out = utf8.AppendRune(out, rn)
	}
	return bytes.NewReader(out)
}

type errReader struct{ err error }

func (e *errReader) Read([]byte) (int, error) { return 0, e.err }

var cp1251 = [128]rune{
	0x0402, 0x0403, 0x201A, 0x0453, 0x201E, 0x2026, 0x2020, 0x2021, 0x20AC, 0x2030, 0x0409, 0x2039, 0x040A, 0x040C, 0x040B, 0x040F,
	0x0452, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014, 0xFFFD, 0x2122, 0x0459, 0x203A, 0x045A, 0x045C, 0x045B, 0x045F,
	0x00A0, 0x040E, 0x045E, 0x0408, 0x00A4, 0x0490, 0x00A6, 0x00A7, 0x0401, 0x00A9, 0x0404, 0x00AB, 0x00AC, 0x00AD, 0x00AE, 0x0407,
	0x00B0, 0x00B1, 0x0406, 0x0456, 0x0491, 0x00B5, 0x00B6, 0x00B7, 0x0451, 0x2116, 0x0454, 0x00BB, 0x0458, 0x0405, 0x0455, 0x0457,
	0x0410, 0x0411, 0x0412, 0x0413, 0x0414, 0x0415, 0x0416, 0x0417, 0x0418, 0x0419, 0x041A, 0x041B, 0x041C, 0x041D, 0x041E, 0x041F,
	0x0420, 0x0421, 0x0422, 0x0423, 0x0424, 0x0425, 0x0426, 0x0427, 0x0428, 0x0429, 0x042A, 0x042B, 0x042C, 0x042D, 0x042E, 0x042F,
	0x0430, 0x0431, 0x0432, 0x0433, 0x0434, 0x0435, 0x0436, 0x0437, 0x0438, 0x0439, 0x043A, 0x043B, 0x043C, 0x043D, 0x043E, 0x043F,
	0x0440, 0x0441, 0x0442, 0x0443, 0x0444, 0x0445, 0x0446, 0x0447, 0x0448, 0x0449, 0x044A, 0x044B, 0x044C, 0x044D, 0x044E, 0x044F,
}

var iso88595 = func() (t [128]rune) {
	for i := range t {
		t[i] = rune(0x80 + i)
	}
	for i := 0xA1; i <= 0xAC; i++ {
		t[i-0x80] = rune(0x0401 + i - 0xA1)
	}
	t[0xAE-0x80], t[0xAF-0x80] = 0x040E, 0x040F
	for i := 0xB0; i <= 0xEF; i++ {
		t[i-0x80] = rune(0x0410 + i - 0xB0)
	}
	t[0xF0-0x80] = 0x2116
	for i := 0xF1; i <= 0xFC; i++ {
		t[i-0x80] = rune(0x0451 + i - 0xF1)
	}
	t[0xFD-0x80], t[0xFE-0x80], t[0xFF-0x80] = 0x00A7, 0x045E, 0x045F
	return
}()

var koi8r = [128]rune{
	0x2500, 0x2502, 0x250C, 0x2510, 0x2514, 0x2518, 0x251C, 0x2524, 0x252C, 0x2534, 0x253C, 0x2580, 0x2584, 0x2588, 0x258C, 0x2590,
	0x2591, 0x2592, 0x2593, 0x2320, 0x25A0, 0x2219, 0x221A, 0x2248, 0x2264, 0x2265, 0x00A0, 0x2321, 0x00B0, 0x00B2, 0x00B7, 0x00F7,
	0x2550, 0x2551, 0x2552, 0x0451, 0x2553, 0x2554, 0x2555, 0x2556, 0x2557, 0x2558, 0x2559, 0x255A, 0x255B, 0x255C, 0x255D, 0x255E,
	0x255F, 0x2560, 0x2561, 0x0401, 0x2562, 0x2563, 0x2564, 0x2565, 0x2566, 0x2567, 0x2568, 0x2569, 0x256A, 0x256B, 0x256C, 0x00A9,
	0x044E, 0x0430, 0x0431, 0x0446, 0x0434, 0x0435, 0x0444, 0x0433, 0x0445, 0x0438, 0x0439, 0x043A, 0x043B, 0x043C, 0x043D, 0x043E,
	0x043F, 0x044F, 0x0440, 0x0441, 0x0442, 0x0443, 0x0436, 0x0432, 0x044C, 0x044B, 0x0437, 0x0448, 0x044D, 0x0449, 0x0447, 0x044A,
	0x042E, 0x0410, 0x0411, 0x0426, 0x0414, 0x0415, 0x0424, 0x0413, 0x0425, 0x0418, 0x0419, 0x041A, 0x041B, 0x041C, 0x041D, 0x041E,
	0x041F, 0x042F, 0x0420, 0x0421, 0x0422, 0x0423, 0x0416, 0x0412, 0x042C, 0x042B, 0x0417, 0x0428, 0x042D, 0x0429, 0x0427, 0x042A,
}

var cp1252 = func() (t [128]rune) {
	for i := range t {
		t[i] = rune(0x80 + i)
	}
	spec := map[int]rune{0x80: 0x20AC, 0x82: 0x201A, 0x83: 0x0192, 0x84: 0x201E, 0x85: 0x2026, 0x86: 0x2020, 0x87: 0x2021,
		0x88: 0x02C6, 0x89: 0x2030, 0x8A: 0x0160, 0x8B: 0x2039, 0x8C: 0x0152, 0x8E: 0x017D, 0x91: 0x2018, 0x92: 0x2019,
		0x93: 0x201C, 0x94: 0x201D, 0x95: 0x2022, 0x96: 0x2013, 0x97: 0x2014, 0x98: 0x02DC, 0x99: 0x2122, 0x9A: 0x0161,
		0x9B: 0x203A, 0x9C: 0x0153, 0x9E: 0x017E, 0x9F: 0x0178}
	for k, v := range spec {
		t[k-0x80] = v
	}
	return
}()
