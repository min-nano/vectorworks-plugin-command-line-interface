package install

import (
	"path/filepath"
	"strings"
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
func Target() string {
	return target()
}

// PluginsDirOf は、実行ファイル executable（リンクは解く。mac の ~/.local/bin/vw2026 はリンク）が
// <X>/cli/bin/ にあり、<X>/cli/ に名前が cli の記録があれば、<X> を返す（入った CLI は、自分の入った
// <Plug-Ins> を使う。入れた場所を記録しなくても、場所を取り違えない）。記録の module が無くても（ウイルス
// 対策が隔離した）入った CLI とみなす（既定の場所に別の cli を入れず、同じタグの install が入れ直す）。
// そうでなければ（初回のスクリプトの一時フォルダの CLI・手でビルドした CLI）ok は偽。
func PluginsDirOf(executable string) (dir string, ok bool) {
	if executable == "" {
		return "", false
	}
	path := executable
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		path = resolved
	}
	bin := filepath.Dir(path)
	cli := filepath.Dir(bin)
	if !strings.EqualFold(filepath.Base(bin), "bin") || !strings.EqualFold(filepath.Base(cli), CLIName) {
		return "", false
	}
	if record, err := readRecord(cli); err != nil || record.Name != CLIName {
		return "", false
	}
	return filepath.Dir(cli), true
}
