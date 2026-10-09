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
