package xslt

import (
	"fmt"
	"io/fs"
	"path"
	"strings"
)

// FSResolver resolves hrefs inside an fs.FS (for example an embed.FS holding
// the stylesheets). Locations are slash-separated paths within the FS; a
// relative href is resolved against the directory of the referring
// stylesheet, a leading "/" means the root of the FS. References that leave
// the FS (".." above the root) fail.
type FSResolver struct{ FS fs.FS }

// Resolve implements Resolver.
func (r FSResolver) Resolve(href, base string) ([]byte, string, error) {
	href = strings.TrimPrefix(href, "file://")
	p := path.Clean(href)
	if strings.HasPrefix(href, "/") {
		p = strings.TrimPrefix(p, "/")
	} else if base != "" {
		p = path.Join(path.Dir(base), href)
	}
	if !fs.ValidPath(p) {
		return nil, p, fmt.Errorf("%q is outside of the file system", href)
	}
	data, err := fs.ReadFile(r.FS, p)
	return data, p, err
}
