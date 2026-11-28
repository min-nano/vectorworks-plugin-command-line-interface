//go:build !windows

package spool

import (
	"os"
	"syscall"
)

// holdWait は <id>.wait を作成して取得する（docs/protocol.md「待機標識」）。返す関数で解放する。
//
// 取得の方法は bridge.lock と同じ flock(LOCK_EX)。プロセスが終了すれば（異常終了でも）カーネルが
// 解放するので、プラグインは取得できるか否かで呼び出し元の生死を判定できる。Go の os.OpenFile は
// O_CLOEXEC を付けるので、子プロセスに継承されて解放されないことは無い。
func holdWait(path string) (func(), error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		_ = os.Remove(path)
		return nil, err
	}
	return func() { file.Close() }, nil
}
