package spool

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

// AppDirName は CLI の場所（<CLI>）のディレクトリ名。スプールはその下の SpoolDirName に置く
// （docs/protocol.md「スプールの場所」）。名前に Vectorworks の版を入れ、版の違う
// Vectorworks のプラグインが同居しても要求を取り合わないようにする。
const (
	AppDirName   = "vectorworks2026-cli"
	SpoolDirName = "spool"
)

// DefaultDir はスプールの場所を返す。
//
// 一時ディレクトリではなく利用者ごとに 1 つに決まる場所に置く。一時ディレクトリは環境変数で
// 決まり、GUI アプリの Vectorworks と ssh などから起動された CLI とで食い違いうるため。
// Windows は %LOCALAPPDATA%、そのほかは os.UserConfigDir（macOS では
// ~/Library/Application Support）。プラグイン側も同じ場所を求める。
func DefaultDir() (string, error) {
	var root string
	if runtime.GOOS == "windows" {
		root = os.Getenv("LOCALAPPDATA")
		if root == "" {
			return "", errors.New("LOCALAPPDATA is not set")
		}
	} else {
		dir, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		root = dir
	}
	return filepath.Join(root, AppDirName, SpoolDirName), nil
}
