//go:build !darwin && !linux && !windows

package main

// terminal は、ほかの OS では端末とみなさない（進み具合を 1 行ずつ足す）。
func terminal(uintptr) bool {
	return false
}
