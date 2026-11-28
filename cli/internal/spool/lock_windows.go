//go:build windows

package spool

import (
	"errors"
	"os"
	"syscall"
)

// errorSharingViolation は ERROR_SHARING_VIOLATION。
const errorSharingViolation = syscall.Errno(32)

// lockState はロックファイルをプラグインが取得しているか（docs/protocol.md「生存の判定」）。
//
// プラグインは共有なし（share mode 0）で開いたハンドルを終了まで保持する。こちらが開けずに
// 共有違反になれば取得されている。開けたら直ちに閉じる。ハンドルはプロセスが終了すれば（異常終了
// でも）OS が閉じるので、pid やプロセス名を参照しなくても停止したことが判明する。
//
// 存在しない以外の理由でロックを開けなければ（共有違反でないエラー）、そのエラーも
// 返す（install・uninstall が「動作していない」とみなさないため）。
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
