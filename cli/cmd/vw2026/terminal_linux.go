package main

import (
	"syscall"
	"unsafe"
)

// terminal は、fd が端末か（端末の設定を読めるか。TCGETS）。Linux は Vectorworks の動く OS ではないが、
// テストと開発で走らせる。
func terminal(fd uintptr) bool {
	var state syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TCGETS, uintptr(unsafe.Pointer(&state)))
	return errno == 0
}
