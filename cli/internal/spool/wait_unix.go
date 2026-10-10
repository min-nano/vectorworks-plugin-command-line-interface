//go:build !windows

package spool

import (
	"os"
	"syscall"
)

// holdWait は <id>.wait を作って掴む（docs/protocol.md「待つ印」）。返す関数で放す。
//
// 掴み方は bridge.lock と同じ flock(LOCK_EX)。プロセスが終われば（異常終了でも）カーネルが
// 放すので、プラグインは掴めるかどうかで呼ぶ側の生死を判定できる。Go の os.OpenFile は
// O_CLOEXEC を付けるので、子プロセスに受け継がれて放されないことは無い。
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
