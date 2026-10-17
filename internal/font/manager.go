package font

import (
	"embed"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
)

//go:embed data/*.ttf
var bundled embed.FS

// Face is a font registered with the manager.
type Face struct {
	Family string // lower-case family name
	Name   string // display name
	Weight int
	Italic bool

	path string
	data []byte
	once sync.Once
	font *Font
	err  error
}

// Font returns the parsed font (parsing lazily on first use).
func (f *Face) Font() (*Font, error) {
	f.once.Do(func() {
		data := f.data
		if data == nil && f.path != "" {
			var err error
			data, err = os.ReadFile(f.path)
			if err != nil {
				f.err = err
				return
			}
		}
		f.font, f.err = Parse(data)
	})
	return f.font, f.err
}

// MustFont returns the parsed font or nil when it cannot be parsed.
func (f *Face) MustFont() *Font {
	ft, _ := f.Font()
	return ft
}

// Manager selects fonts for text.
type Manager struct {
	mu          sync.Mutex
	faces       []*Face
	aliases     map[string]string
	sysScanned  bool
	sysDirs     []string
	noSystem    bool
	fallbackFam []string
	cache       map[string]*Face
	warned      map[string]bool
	Warn        func(string)
}

// NewManager creates a manager that knows the bundled Liberation fonts under
// the usual PDF/CSS family names.
func NewManager() *Manager {
	m := &Manager{
		aliases: map[string]string{
			"helvetica": "liberation sans", "arial": "liberation sans", "sans-serif": "liberation sans",
			"sans": "liberation sans", "swiss": "liberation sans", "verdana": "liberation sans", "tahoma": "liberation sans",
			"calibri": "liberation sans", "segoe ui": "liberation sans",
			"times": "liberation serif", "times new roman": "liberation serif", "timesroman": "liberation serif",
			"serif": "liberation serif", "georgia": "liberation serif", "cambria": "liberation serif",
			"courier": "liberation mono", "courier new": "liberation mono", "monospace": "liberation mono",
			"mono": "liberation mono", "consolas": "liberation mono",
			"symbol": "liberation sans", "zapfdingbats": "liberation sans", "any": "liberation sans",
		},
		fallbackFam: []string{"dejavu sans", "noto sans", "freesans", "liberation sans", "dejavu serif"},
		cache:       map[string]*Face{},
		warned:      map[string]bool{},
		sysDirs:     defaultFontDirs(),
	}
	type spec struct {
		file, family string
		weight       int
		italic       bool
	}
	var specs []spec
	for _, fam := range []struct{ file, name string }{{"LiberationSans", "liberation sans"}, {"LiberationSerif", "liberation serif"}, {"LiberationMono", "liberation mono"}} {
		specs = append(specs,
			spec{fam.file + "-Regular.ttf", fam.name, 400, false},
			spec{fam.file + "-Bold.ttf", fam.name, 700, false},
			spec{fam.file + "-Italic.ttf", fam.name, 400, true},
			spec{fam.file + "-BoldItalic.ttf", fam.name, 700, true})
	}
	for _, s := range specs {
		data, err := fs.ReadFile(bundled, "data/"+s.file)
		if err != nil {
			continue
		}
		m.faces = append(m.faces, &Face{Family: s.family, Name: s.file, Weight: s.weight, Italic: s.italic, data: data})
	}
	return m
}

func defaultFontDirs() []string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "windows":
		return []string{filepath.Join(os.Getenv("WINDIR"), "Fonts")}
	case "darwin":
		return []string{"/System/Library/Fonts", "/Library/Fonts", filepath.Join(home, "Library", "Fonts")}
	}
	return []string{"/usr/share/fonts", "/usr/local/share/fonts", filepath.Join(home, ".fonts"), filepath.Join(home, ".local", "share", "fonts")}
}

// DisableSystemFonts stops the manager from scanning system font directories.
func (m *Manager) DisableSystemFonts() { m.noSystem = true }

// AddDir adds a directory to scan for fonts (in addition to system dirs).
func (m *Manager) AddDir(dir string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sysDirs = append([]string{dir}, m.sysDirs...)
	m.sysScanned = false
	m.cache = map[string]*Face{}
}

// AddFont registers a font under the given family name. weight 0 means "use the
// weight stored in the font".
func (m *Manager) AddFont(family string, weight int, italic bool, data []byte) error {
	ft, err := Parse(data)
	if err != nil {
		return err
	}
	if weight == 0 {
		weight = ft.Weight
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	fc := &Face{Family: strings.ToLower(family), Name: family, Weight: weight, Italic: italic, data: data, font: ft}
	fc.once.Do(func() {})
	m.faces = append([]*Face{fc}, m.faces...)
	m.cache = map[string]*Face{}
	return nil
}

// AddFile registers a font file.
func (m *Manager) AddFile(family string, weight int, italic bool, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return m.AddFont(family, weight, italic, data)
}

func (m *Manager) scanSystem() {
	if m.sysScanned || m.noSystem {
		return
	}
	m.sysScanned = true
	seen := map[string]bool{}
	for _, f := range m.faces {
		if f.path != "" {
			seen[f.path] = true
		}
	}
	for _, dir := range m.sysDirs {
		_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || seen[p] {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(p))
			if ext != ".ttf" && ext != ".ttc" {
				return nil
			}
			info, ok := readNameInfo(p)
			if !ok {
				return nil
			}
			seen[p] = true
			m.faces = append(m.faces, &Face{Family: strings.ToLower(info.family), Name: info.family, Weight: info.weight, Italic: info.italic, path: p})
			return nil
		})
	}
}

type nameInfo struct {
	family string
	weight int
	italic bool
}

// readNameInfo reads family and style from the name/OS-2 tables without
// loading the whole file.
func readNameInfo(path string) (nameInfo, bool) {
	fh, err := os.Open(path)
	if err != nil {
		return nameInfo{}, false
	}
	defer fh.Close()
	hdr := make([]byte, 12)
	if _, err := io.ReadFull(fh, hdr); err != nil {
		return nameInfo{}, false
	}
	base := int64(0)
	if string(hdr[:4]) == "ttcf" {
		off := make([]byte, 4)
		if _, err := fh.ReadAt(off, 12); err != nil {
			return nameInfo{}, false
		}
		base = int64(be32(off, 0))
		if _, err := fh.ReadAt(hdr, base); err != nil {
			return nameInfo{}, false
		}
	}
	if t := string(hdr[:4]); t != "\x00\x01\x00\x00" && t != "true" {
		return nameInfo{}, false // CFF or unknown
	}
	n := be16(hdr, 4)
	dir := make([]byte, n*16)
	if _, err := fh.ReadAt(dir, base+12); err != nil {
		return nameInfo{}, false
	}
	var nameOff, nameLen, osOff, osLen int
	hasGlyf := false
	for i := 0; i < n; i++ {
		r := dir[i*16:]
		switch string(r[:4]) {
		case "name":
			nameOff, nameLen = be32(r, 8), be32(r, 12)
		case "OS/2":
			osOff, osLen = be32(r, 8), be32(r, 12)
		case "glyf":
			hasGlyf = true
		}
	}
	if !hasGlyf || nameLen == 0 || nameLen > 1<<20 {
		return nameInfo{}, false
	}
	nb := make([]byte, nameLen)
	if _, err := fh.ReadAt(nb, int64(nameOff)); err != nil {
		return nameInfo{}, false
	}
	f := &Font{tables: map[string][]byte{"name": nb}}
	f.parseNames()
	if f.Family == "" {
		return nameInfo{}, false
	}
	info := nameInfo{family: f.Family, weight: 400, italic: f.Italic}
	if f.Bold {
		info.weight = 700
	}
	if osLen >= 6 {
		ob := make([]byte, 6)
		if _, err := fh.ReadAt(ob, int64(osOff)); err == nil {
			if w := be16(ob, 4); w >= 100 && w <= 900 {
				info.weight = w
			}
		}
	}
	return info, true
}

func (m *Manager) warn(msg string) {
	if m.warned[msg] {
		return
	}
	m.warned[msg] = true
	if m.Warn != nil {
		m.Warn(msg)
	}
}

func bestStyle(cands []*Face, weight int, italic bool) *Face {
	var best *Face
	bestScore := 1 << 30
	for _, c := range cands {
		score := abs(c.Weight-weight) / 10
		if c.Italic != italic {
			score += 1000
		}
		if score < bestScore {
			best, bestScore = c, score
		}
	}
	return best
}

func abs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}

func (m *Manager) facesOf(family string) []*Face {
	var out []*Face
	for _, f := range m.faces {
		if f.Family == family {
			out = append(out, f)
		}
	}
	return out
}

// Select picks the face that best matches the first available family.
func (m *Manager) Select(families []string, weight int, italic bool) *Face {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := fmt.Sprintf("%s|%d|%v", strings.Join(families, ","), weight, italic)
	if f, ok := m.cache[key]; ok {
		return f
	}
	var found *Face
	for _, fam := range families {
		name := strings.ToLower(strings.Trim(strings.TrimSpace(fam), `"'`))
		if name == "" {
			continue
		}
		cands := m.facesOf(name)
		if len(cands) == 0 {
			if a, ok := m.aliases[name]; ok {
				cands = m.facesOf(a)
			}
		}
		if len(cands) == 0 && !m.sysScanned {
			m.scanSystem()
			cands = m.facesOf(name)
		}
		if len(cands) > 0 {
			found = bestStyle(cands, weight, italic)
			break
		}
	}
	if found == nil {
		if len(families) > 0 {
			m.warn(fmt.Sprintf("font family %q not found, using fallback", strings.Join(families, ", ")))
		}
		found = bestStyle(m.facesOf("liberation sans"), weight, italic)
	}
	m.cache[key] = found
	return found
}

// FallbackFor finds a face (similar in weight/style to base) that has a glyph for r.
func (m *Manager) FallbackFor(base *Face, r rune) *Face {
	m.mu.Lock()
	defer m.mu.Unlock()
	try := func(fam string) *Face {
		cands := m.facesOf(fam)
		sort.SliceStable(cands, func(i, j int) bool {
			return styleDist(cands[i], base) < styleDist(cands[j], base)
		})
		for _, c := range cands {
			if ft := c.MustFont(); ft != nil && ft.HasGlyph(r) {
				return c
			}
		}
		return nil
	}
	for _, fam := range m.fallbackFam {
		if f := try(fam); f != nil {
			return f
		}
	}
	m.scanSystem()
	for _, fam := range m.fallbackFam {
		if f := try(fam); f != nil {
			return f
		}
	}
	// any system font with the glyph
	for _, f := range m.faces {
		if f.path == "" || f.Italic != base.Italic {
			continue
		}
		if ft := f.MustFont(); ft != nil && ft.HasGlyph(r) {
			return f
		}
	}
	return nil
}

func styleDist(a, b *Face) int {
	d := abs(a.Weight-b.Weight) / 10
	if a.Italic != b.Italic {
		d += 1000
	}
	return d
}
