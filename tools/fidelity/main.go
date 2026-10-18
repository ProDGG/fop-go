// Command fidelity compares fop-go with Apache FOP on XSL-FO files.
//
//	FOPLIBS=/path/to/fop/lib [FOPCONF=fop.xconf] go run ./tools/fidelity [file.fo ...]
//
// Without arguments testdata/fidelity/*.fo is used. Needs java, a FOP 2.x
// classpath in $FOPLIBS and poppler-utils (pdftotext). Output goes to $OUT
// (default /tmp/fidelity).
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	libs := os.Getenv("FOPLIBS")
	if st, err := os.Stat(libs); libs == "" || err != nil || !st.IsDir() {
		fmt.Fprintln(os.Stderr, "set FOPLIBS to a directory with Apache FOP jars")
		os.Exit(2)
	}
	out := os.Getenv("OUT")
	if out == "" {
		out = "/tmp/fidelity"
	}
	must(os.MkdirAll(out, 0o755))
	files := os.Args[1:]
	if len(files) == 0 {
		files, _ = filepath.Glob("testdata/fidelity/*.fo")
	}
	bin := filepath.Join(out, "fop-go")
	must(run("go", "build", "-o", bin, "./cmd/fop-go"))
	cmpBin := filepath.Join(out, "pdfcmp")
	must(run("go", "build", "-o", cmpBin, "./tools/pdfcmp"))
	for _, f := range files {
		n := strings.TrimSuffix(filepath.Base(f), ".fo")
		ref, mine := filepath.Join(out, n+".ref.pdf"), filepath.Join(out, n+".pdf")
		args := []string{"-Djava.awt.headless=true", "-cp", filepath.Join(libs, "*"), "org.apache.fop.cli.Main"}
		if c := os.Getenv("FOPCONF"); c != "" {
			args = append(args, "-c", c)
		}
		args = append(args, "-fo", f, "-pdf", ref)
		if o, err := exec.Command("java", args...).CombinedOutput(); err != nil {
			os.WriteFile(filepath.Join(out, n+".ref.log"), o, 0o644)
			fmt.Printf("FOP failed on %s (see %s.ref.log)\n", n, filepath.Join(out, n))
		}
		if err := run(bin, "-q", "-fo", f, "-pdf", mine); err != nil {
			fmt.Printf("%-22s fop-go failed: %v\n", n, err)
			continue
		}
		o, _ := exec.Command(cmpBin, "words", mine, ref).Output()
		lines := strings.Split(strings.TrimSpace(string(o)), "\n")
		if len(lines) > 2 {
			lines = lines[len(lines)-2:]
		}
		fmt.Printf("%-22s %s\n", n, strings.Join(lines, " "))
	}
}

func run(name string, args ...string) error {
	c := exec.Command(name, args...)
	c.Stderr = os.Stderr
	return c.Run()
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "fidelity:", err)
		os.Exit(1)
	}
}
