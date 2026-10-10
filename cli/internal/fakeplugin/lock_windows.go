//go:build windows

package fakeplugin

import (
	"errors"
	"syscall"
)

// lock はプラグインと同じく共有なし（share mode 0）でロックファイルを開いたままにする。
func lock(path string) (func(), error) {
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

// errorSharingViolation は ERROR_SHARING_VIOLATION。
const errorSharingViolation = syscall.Errno(32)

// waitStateOf は <id>.wait の状態（docs/protocol.md「待つ印」）。共有なしで開ければ呼ぶ側が
// 放した・死んだ。共有違反なら呼ぶ側が待っている。
func waitStateOf(path string) waitState {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return waitMissing
	}
	handle, err := syscall.CreateFile(name, syscall.GENERIC_READ, 0, nil,
		syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err == nil {
		syscall.CloseHandle(handle)
		return waitReleased
	}
	if errors.Is(err, errorSharingViolation) {
		return waitHeld
	}
	return waitMissing
}
