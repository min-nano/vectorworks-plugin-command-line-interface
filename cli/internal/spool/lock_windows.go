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

// holdLock はロックファイルを掴む（無ければ作る）。返す関数で放す。session.lock に使う
// （docs/protocol.md「占有」）。掴み方は bridge.lock と同じく共有なしで開いたハンドルを
// 持ち続ける。lpSecurityAttributes に nil を渡し、子プロセスに受け継がせない。
func holdLock(path string) (func(), error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := syscall.CreateFile(name, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil,
		syscall.OPEN_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, err
	}
	return func() { syscall.CloseHandle(handle) }, nil
}

// lockBusy は holdLock の失敗が「ほかが掴んでいる」によるものか。
func lockBusy(err error) bool {
	return errors.Is(err, errorSharingViolation)
}
