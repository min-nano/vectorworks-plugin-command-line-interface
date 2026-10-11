//go:build !windows

package spool

import "os"

// appRoot は <CLI> の親（macOS では ~/Library/Application Support）を返す。
//
// os.UserConfigDir は macOS では HOME から決まる。HOME は ssh でも GUI アプリでも利用者の
// ホームを指すので、プラグインと同じ値になる（docs/protocol.md「スプールの場所」）。
func appRoot() (string, error) {
	return os.UserConfigDir()
}
