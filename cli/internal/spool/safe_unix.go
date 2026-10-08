//go:build !windows

package spool

import (
	"os"
	"syscall"
)

// checkSafe はそのスプールを使ってよいか（持ち主と権限）。使えなければ理由を返す。
//
// プラグイン側は自分が作る場所を 0700・自分の所有に限っているので、こちらも同じ基準で
// 判定する。理由: /tmp のような誰でも書ける場所に偽の印を置かれると、要求（引数）が漏れ、
// 偽の応答を受け取ってしまう。
func checkSafe(dir string) string {
	info, err := os.Stat(dir)
	if err != nil {
		return "not found"
	}
	if !info.IsDir() {
		return "not a directory"
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && int(stat.Uid) != os.Geteuid() {
		return "owned by another user"
	}
	if info.Mode().Perm()&0o022 != 0 {
		return "writable by others"
	}
	return ""
}
