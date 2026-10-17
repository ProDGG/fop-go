// Command xhrdev renders an XHTML file with the Flying-Saucer compatible
// engine (development helper).
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ProDGG/fop-go/internal/xhr"
	"github.com/ProDGG/fop-go/xmldom"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: xhrdev in.html out.pdf")
		os.Exit(2)
	}
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	doc, err := xmldom.ParseBytes(data)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	base := filepath.Dir(os.Args[1])
	opts := xhr.Options{ResolveResource: func(uri string) ([]byte, error) {
		return os.ReadFile(filepath.Join(base, uri))
	}}
	var dump strings.Builder
	if len(os.Args) > 3 {
		opts.Dump = &dump
	}
	out, err := xhr.Render(doc, opts)
	if len(os.Args) > 3 {
		os.WriteFile(os.Args[3], []byte(dump.String()), 0o644)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(os.Args[2], out, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
