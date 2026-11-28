//go:build !windows

package spool

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// lockState はロックファイルをプラグインが掴んでいるか（docs/protocol.md「生存の判定」）。
//
// プラグインは flock(LOCK_EX) を終了まで持つ。こちらは共有ロックを試し、取れたらすぐ放す。
// 取れなければ掴まれている。flock はプロセスが終われば（異常終了でも）カーネルが放すので、
// pid やプロセス名を見なくても止まったことが分かる。
//
// 無い以外の理由でロックを調べられなければ（開けない・通常のファイルでない・flock が
// EWOULDBLOCK 以外で失敗する）、その誤りも返す（install・uninstall が「動いていない」とみなさないため）。
//
// プラグインと同じく、開いて待たない（docs/protocol.md「受け付けの手順」）。FIFO を O_NONBLOCK なしで開くと書き手を
// 待って止まるので O_NONBLOCK で開き、リンクは O_NOFOLLOW でたどらず（リンクの先のデバイスやパイプを開かない）、
// 開いた後に通常のファイルかを確かめる。
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
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("%s is not a regular file", path)
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
