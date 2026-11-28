//go:build !windows

package spool

import (
	"errors"
	"os"
	"syscall"
)

// lockState はロックファイルをプラグインが取得しているか（docs/protocol.md「生存の判定」）。
//
// プラグインは flock(LOCK_EX) を終了まで保持する。こちらは共有ロックを試み、取得できたら直ちに
// 解放する。取得できなければ取得されている。flock はプロセスが終了すれば（異常終了でも）カーネルが
// 解放するので、pid やプロセス名を参照しなくても停止したことが判明する。
//
// 存在しない以外の理由でロックを調査できなければ（開けない・flock が EWOULDBLOCK 以外で失敗する）、
// そのエラーも返す（install・uninstall が「動作していない」とみなさないため）。
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
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return true, nil
		}
		return false, err
	}
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	return false, nil
}
