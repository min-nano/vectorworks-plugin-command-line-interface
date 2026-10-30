package registry

import (
	"os"
	"path/filepath"
	"strings"
)

// absPath は、この OS の絶対パス（JSON の文字列に埋め込めるように / で区切る）。
func absPath(name string) string {
	return strings.ReplaceAll(filepath.Join(os.TempDir(), name), `\`, "/")
}
