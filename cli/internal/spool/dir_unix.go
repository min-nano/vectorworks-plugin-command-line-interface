//go:build !windows

package spool

import (
	"os"
	"path/filepath"
)

// appRoot は <CLI> の親（~/Library/Application Support）を返す。対象は macOS だけで、ほかの
// POSIX でも同じ形にする（テストのため。os.UserConfigDir は Linux では XDG_CONFIG_HOME を
// 読み、環境で場所が変わるので使わない）。
//
// ホームは HOME から求める。HOME は ssh でも GUI アプリでも利用者のホームを指すので、
// プラグインと同じ値になる（docs/protocol.md「スプールの場所」）。
func appRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "Application Support"), nil
}
