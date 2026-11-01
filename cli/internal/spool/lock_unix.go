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
	held, _ := lockState(path)
	return held
}

// lockState は lockHeld と同じ判定で、無い以外の理由でロックを調べられなければ（開けない・flock が
// EWOULDBLOCK 以外で失敗する）、その誤りも返す（install・uninstall が「動いていない」とみなさないため）。
func lockState(path string) (bool, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer file.Close()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err != nil {
		held := errors.Is(err, syscall.EWOULDBLOCK)
		if held {
			err = nil
		}
		return held, err
	}
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	return false, nil
}
