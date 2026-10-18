// Command pdfcmp compares two PDFs (for example fop-go output against Apache FOP
// or Flying Saucer output).
//
//	pdfcmp words     mine.pdf ref.pdf [tolerance_pt]   word positions (needs pdftotext)
//	pdfcmp lines     mine.pdf ref.pdf [page]           per-line vertical comparison of one page
//	pdfcmp baselines mine.pdf ref.pdf [page]           distinct text baselines side by side
//	pdfcmp texts     mine.pdf ref.pdf [skip_top_pt]    text origins in drawing order
//	pdfcmp img       mine.pdf ref.pdf [dpi] [outprefix] pixel comparison (needs pdftoppm)
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 4 {
		usage()
	}
	cmd, a, b, rest := os.Args[1], os.Args[2], os.Args[3], os.Args[4:]
	var err error
	switch cmd {
	case "words":
		err = cmdWords(a, b, rest)
	case "lines":
		err = cmdLines(a, b, rest)
	case "baselines":
		err = cmdBaselines(a, b, rest)
	case "texts":
		err = cmdTexts(a, b, rest)
	case "img":
		err = cmdImg(a, b, rest)
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "pdfcmp:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: pdfcmp {words|lines|baselines|texts|img} mine.pdf ref.pdf [options]")
	os.Exit(2)
}
