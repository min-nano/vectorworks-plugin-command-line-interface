package install

import (
	"path/filepath"
)

// WorkDirName は、利用者ごとの一時フォルダの下の <Work> の名前（docs/plugin/install-and-update.md
// 「作業の場所」）。vw2026 だけが使うので、通信規則の見本には載せない。
const WorkDirName = "vectorworks2026-cli"

// WorkDir は <Work> を返す。場所の根は環境変数を読まずに OS の決まった場所から求める（走らせ方で
// 場所が変わると、前の回が残した old/ を次の回が見つけられなくなるため）。
func WorkDir() (string, error) {
	temp, err := userTempDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(temp, WorkDirName), nil
}

// DefaultPluginsDir は既定の <Plug-Ins> を返す（docs/plugin/identifiers.md「置き場所」）。
func DefaultPluginsDir() (string, error) {
	return defaultPluginsDir()
}

// Target は計算機からターゲットを 1 つに決める（docs/registry.md「名前・タグ・ターゲット」）。
func Target() (string, error) {
	return target()
}
