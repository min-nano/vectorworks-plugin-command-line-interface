package registry

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// absPath は、この OS の絶対パス（JSON の文字列に埋め込めるように / で区切る）。
func absPath(name string) string {
	return strings.ReplaceAll(filepath.Join(os.TempDir(), name), `\`, "/")
}

// fileURL は、一時ディレクトリの下の name を指す file:// の URL（Windows は file:///C:/…）。
func fileURL(name string) string {
	path := absPath(name)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return (&url.URL{Scheme: "file", Path: path}).String()
}
