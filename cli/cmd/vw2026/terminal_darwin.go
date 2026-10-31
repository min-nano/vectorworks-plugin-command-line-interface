package main

import (
	"syscall"
	"unsafe"
)

// terminal は、fd が端末か（端末の設定を読めるか。TIOCGETA）。
func terminal(fd uintptr) bool {
	var state syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TIOCGETA, uintptr(unsafe.Pointer(&state)))
	return errno == 0
}
