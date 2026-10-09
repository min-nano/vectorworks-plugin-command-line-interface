//go:build !windows

package spool

import (
	"errors"
	"os"
	"syscall"
)

// lockHeld はロックファイルをプラグインが掴んでいるか（docs/protocol.md「生存の判定」）。
//
// プラグインは flock(LOCK_EX) を終了まで持つ。こちらは共有ロックを試し、取れたらすぐ放す。
// 取れなければ掴まれている。flock はプロセスが終われば（異常終了でも）カーネルが放すので、
// pid やプロセス名を見なくても止まったことが分かる。
func lockHeld(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err != nil {
		return errors.Is(err, syscall.EWOULDBLOCK)
	}
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	return false
}
