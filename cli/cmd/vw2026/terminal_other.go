//go:build !darwin && !linux && !windows

package main

// terminal は、他の OS では端末とみなさない（進捗を 1 行ずつ追加する）。
func terminal(uintptr) bool {
	return false
}
