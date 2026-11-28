//go:build !windows

package spool

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// lockState はロックファイルをプラグインが取得しているか（docs/protocol.md「生存の判定」）。
//
// プラグインは flock(LOCK_EX) を終了まで保持する。こちらは共有ロックを試み、取得できたら直ちに
// 解放する。取得できなければ取得されている。flock はプロセスが終了すれば（異常終了でも）カーネルが
// 解放するので、pid やプロセス名を参照しなくても停止したことが判明する。
//
// 存在しない以外の理由でロックを調査できなければ（開けない・通常のファイルでない・flock が EWOULDBLOCK 以外で
// 失敗する）、そのエラーも返す（install・uninstall が「動作していない」とみなさないため）。
//
// プラグインと同じく、開いて待機しない（docs/protocol.md「ロック（bridge.lock）」と「プラグイン側の義務」）。
// FIFO を O_NONBLOCK なしで開くと書き込み側を待機して停止するため O_NONBLOCK で開き、リンクは O_NOFOLLOW で
// たどらず（リンク先のデバイスやパイプを開かない）、開いた後に通常のファイルかを検証する。
func lockState(path string) (bool, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err == nil && !info.Mode().IsRegular() {
		err = fmt.Errorf("%s is not a regular file", path)
	}
	if err != nil {
		return false, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return true, nil
		}
		return false, err
	}
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	return false, nil
}
