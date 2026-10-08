//go:build windows

package spool

import "os"

// checkSafe はそのスプールを使ってよいか。Windows の %TEMP% は利用者ごとで、ACL は
// stat では判定できないので、プラグイン側と同じく存在だけを確かめる。
func checkSafe(dir string) string {
	info, err := os.Stat(dir)
	if err != nil {
		return "not found"
	}
	if !info.IsDir() {
		return "not a directory"
	}
	return ""
}
