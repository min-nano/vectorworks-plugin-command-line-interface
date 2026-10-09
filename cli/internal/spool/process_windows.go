//go:build windows

package spool

import (
	"syscall"
	"unsafe"
)

var procQueryFullProcessImageNameW = syscall.NewLazyDLL("kernel32.dll").NewProc("QueryFullProcessImageNameW")

// processImage は pid のプロセスの実行ファイルのパスと、プロセスがあるか。パスが取れなければ "" 。
func processImage(pid int) (string, bool) {
	const (
		processQueryLimitedInformation = 0x1000
		stillActive                    = 259
		errorAccessDenied              = syscall.Errno(5)
	)
	handle, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		// 拒まれたならプロセスはある。それ以外（ERROR_INVALID_PARAMETER）は無い。
		return "", err == errorAccessDenied
	}
	defer syscall.CloseHandle(handle)
	// 終わったプロセスもハンドルが残る間は開けるので、終了コードで確かめる。
	var code uint32
	if syscall.GetExitCodeProcess(handle, &code) == nil && code != stillActive {
		return "", false
	}
	var buf [1024]uint16
	size := uint32(len(buf))
	if r, _, _ := procQueryFullProcessImageNameW.Call(uintptr(handle), 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size))); r == 0 {
		return "", true
	}
	return syscall.UTF16ToString(buf[:size]), true
}
