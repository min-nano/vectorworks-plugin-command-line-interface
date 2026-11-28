package main

import (
	"syscall"
	"unsafe"
)

// terminal は、fd が端末か否か（端末の設定を読み取れるか。TCGETS）。Linux は Vectorworks の動作する
// OS ではないが、テストと開発で実行する。
func terminal(fd uintptr) bool {
	var state syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TCGETS, uintptr(unsafe.Pointer(&state)))
	return errno == 0
}
