//go:build !windows

package fakeplugin

import (
	"os"
	"syscall"
)

// lock はプラグインと同じく flock(LOCK_EX) を掴む。
func lock(path string) (func(), error) {
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

// waitStateOf は <id>.wait の状態（docs/protocol.md「待つ印」）。掴めれば呼ぶ側が放した・死んだ。
func waitStateOf(path string) waitState {
	file, err := os.Open(path)
	if err != nil {
		return waitMissing
	}
	defer file.Close()
	if syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) == nil {
		return waitReleased
	}
	return waitHeld
}
