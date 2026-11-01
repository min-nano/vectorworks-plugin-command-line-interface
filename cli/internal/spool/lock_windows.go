//go:build windows

package spool

import (
	"errors"
	"os"
	"syscall"
)

// errorSharingViolation は ERROR_SHARING_VIOLATION。
const errorSharingViolation = syscall.Errno(32)

// lockState はロックファイルをプラグインが掴んでいるか（docs/protocol.md「生存の判定」）。
//
// プラグインは共有なし（share mode 0）で開いたハンドルを終了まで持つ。こちらが開けずに
// 共有違反になれば掴まれている。開けたらすぐ閉じる。ハンドルはプロセスが終われば（異常終了
// でも）OS が閉じるので、pid やプロセス名を見なくても止まったことが分かる。
//
// 無い以外の理由でロックを開けなければ（共有違反でない誤り）、その誤りも
// 返す（install・uninstall が「動いていない」とみなさないため）。
func lockState(path string) (bool, error) {
	file, err := os.Open(path)
	switch {
	case err == nil:
		file.Close()
		return false, nil
	case errors.Is(err, errorSharingViolation):
		return true, nil
	case errors.Is(err, os.ErrNotExist):
		return false, nil
	}
	return false, err
}
