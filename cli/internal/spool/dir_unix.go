//go:build !windows

package spool

import (
	"os/user"
	"path/filepath"
)

// appRoot は <CLI> の親（~/Library/Application Support）を返す。対象は macOS だけで、ほかの
// POSIX でも同じ形にする（テストのため）。
//
// ホームは環境変数 HOME ではなく利用者の passwd の項目（getpwuid）から求める。HOME は
// launchd のジョブで無い・ツールの実行環境で上書きされることがあり、プラグインと食い違うと
// 要求が届かなくなるため（docs/protocol.md「スプールの場所」）。darwin の os/user は cgo なしでも
// libSystem の getpwuid_r を呼ぶ。
func appRoot() (string, error) {
	current, err := user.Current()
	if err != nil {
		return "", err
	}
	return filepath.Join(current.HomeDir, "Library", "Application Support"), nil
}
