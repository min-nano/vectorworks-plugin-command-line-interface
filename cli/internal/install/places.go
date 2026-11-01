package install

import (
	"os"
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
// <X>/cli/bin/ にあり、<X>/cli/ に記録のファイルがあれば、<X> を返す（入った CLI は、自分の入った <Plug-Ins> を
// 使う。入れた場所を記録しなくても、場所を取り違えない）。記録は、あるかだけを見て中身は読まない（module が欠けた・
// 記録が壊れた cli でも、既定の場所に別の cli を入れず、自分の入ったフォルダで install・uninstall が記録を確かめて、
// 入れ直すか断る）。記録は install だけが書くので、初回のスクリプトの一時フォルダの CLI・手でビルドした CLI
// （リポジトリの cli/bin/ など）・リリースの zip を cli という名前のフォルダへ手で展開した CLI なら ok は偽。
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
	if _, err := os.Lstat(filepath.Join(cli, RecordFile)); err != nil {
		return "", false
	}
	return filepath.Dir(cli), true
}
