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

// callerGone は <id>.wait を掴めるか（呼ぶ側が放した・死んだ）。.wait が無ければ false
// （待つ者の有無が分からないので実行する。docs/protocol.md「待つ印」）。
func callerGone(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	return syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) == nil
}
