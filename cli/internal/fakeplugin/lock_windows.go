//go:build windows

package fakeplugin

import "syscall"

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

// callerGone は <id>.wait を共有なしで開けるか（呼ぶ側が放した・死んだ）。共有違反なら
// 呼ぶ側が待っている。.wait が無ければ false（待つ者の有無が分からないので実行する。
// docs/protocol.md「待つ印」）。
func callerGone(path string) bool {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	handle, err := syscall.CreateFile(name, syscall.GENERIC_READ, 0, nil,
		syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return false
	}
	syscall.CloseHandle(handle)
	return true
}
