package main

import "syscall"

// terminal は、fd がコンソールか（コンソールの設定を読めるか。GetConsoleMode）。
func terminal(fd uintptr) bool {
	var mode uint32
	return syscall.GetConsoleMode(syscall.Handle(fd), &mode) == nil
}
