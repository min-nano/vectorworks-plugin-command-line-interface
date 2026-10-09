//go:build !windows

package spool

import "os"

// claim は要求を確保する。POSIX の rename(2) は不可分で、元の名前が消えたあとの rename は
// 失敗するので、確保できるのは 1 つだけ。
func claim(from, to string) error {
	return os.Rename(from, to)
}
