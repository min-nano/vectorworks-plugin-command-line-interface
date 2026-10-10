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

// holdLock はロックファイルを掴む（無ければ作る）。返す関数で放す。session.lock に使う
// （docs/protocol.md「占有」）。掴み方は bridge.lock と同じ flock(LOCK_EX)。Go の os.OpenFile は
// O_CLOEXEC を付けるので、子プロセスに受け継がれない。
func holdLock(path string) (func(), error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, err
	}
	return func() { file.Close() }, nil
}

// lockBusy は holdLock の失敗が「ほかが掴んでいる」によるものか。
func lockBusy(err error) bool {
	return errors.Is(err, syscall.EWOULDBLOCK)
}
