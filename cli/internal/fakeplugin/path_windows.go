//go:build windows

package fakeplugin

import (
	"path/filepath"
	"strings"
)

// extendedPath は \\?\ を付けた絶対パスにする。プラグインと同じく、CON・NUL などの名前を
// デバイスではなくファイルとして開くため（docs/protocol.md「id」）。
func extendedPath(dir string) string {
	if strings.HasPrefix(dir, `\\?\`) {
		return dir
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir
	}
	if strings.HasPrefix(abs, `\\`) {
		return `\\?\UNC\` + abs[2:]
	}
	return `\\?\` + abs
}
