//go:build windows

package spool

import (
	"errors"
	"os"
	"syscall"
)

// errorSharingViolation は ERROR_SHARING_VIOLATION。
const errorSharingViolation = syscall.Errno(32)

// lockHeld はロックファイルをプラグインが掴んでいるか（docs/protocol.md「生存の判定」）。
//
// プラグインは共有なし（share mode 0）で開いたハンドルを終了まで持つ。こちらが開けずに
// 共有違反になれば掴まれている。開けたらすぐ閉じる。ハンドルはプロセスが終われば（異常終了
// でも）OS が閉じるので、pid やプロセス名を見なくても止まったことが分かる。
func lockHeld(path string) bool {
	file, err := os.Open(path)
	if err == nil {
		file.Close()
		return false
	}
	return errors.Is(err, errorSharingViolation)
}
