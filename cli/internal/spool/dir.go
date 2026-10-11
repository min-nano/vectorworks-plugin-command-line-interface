package spool

import "path/filepath"

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
// 根（appRoot）はプラグイン側と同じ規則で求める（OS ごとのファイル）。
func DefaultDir() (string, error) {
	root, err := appRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, AppDirName, SpoolDirName), nil
}
