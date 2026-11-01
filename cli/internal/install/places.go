package install

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/spool"
)

// WorkDirName は、利用者ごとの一時フォルダの下の <Work> の名前の頭（docs/plugin/install-and-update.md
// 「作業の場所」。WorkDirFor が <Plug-Ins> ごとの鍵を足す）。vw2026 だけが使うので、通信規則の見本には載せない。
const WorkDirName = "vectorworks2026-cli"

// WorkDir は <Work> の頭（利用者ごとの一時フォルダの下の WorkDirName。WorkDirFor に渡す）を返す。場所の根は環境変数を
// 読まずに OS の決まった場所から求める（走らせ方で場所が変わると、前の回が残した old/ を次の回が見つけられなくなる
// ため）。
func WorkDir() (string, error) {
	temp, err := userTempDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(temp, WorkDirName), nil
}

// WorkDirFor は、<Plug-Ins> plugins の <Work>（頭 base に - と鍵を足した名前）を返す。<Work>/old/<name>/ を
// <Plug-Ins> ごとに分ける（同じ利用者が別の <Plug-Ins> にも入れているとき、片方の install が、もう片方の付け替えの
// 途中で退避した唯一の版を前の版と取り違えて消さないため）。鍵は、リンクを解いたパス（resolvedPath。mac と Windows
// では小文字にする）の sha256 の先頭 12 桁（入った CLI が求める場所と、初回のスクリプトに渡した書き方が揃う）。
func WorkDirFor(base, plugins string) string {
	key := resolvedPath(filepath.Clean(plugins))
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		key = strings.ToLower(key)
	}
	sum := sha256.Sum256([]byte(key))
	return base + "-" + hex.EncodeToString(sum[:6])
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
// <X>/cli/bin/ にあり、<X>/cli/ に cli のフォルダの印（記録・モジュール・スプールのどれか）があれば、<X> を返す
// （入った CLI は、自分の入った <Plug-Ins> を使う。入れた場所を記録しなくても、場所を取り違えない）。印は、あるかだけを
// 見て中身は読まない（module が欠けた・記録が壊れた・記録が消えた cli でも、既定の場所に別の cli を入れず、自分の
// 入ったフォルダで install・uninstall が記録を確かめて、入れ直すか断る）。初回のスクリプトの一時フォルダの CLI・
// 手でビルドした CLI（リポジトリの cli/bin/ など。どの印も無い）なら ok は偽。
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
	for _, mark := range []string{RecordFile, CLIName + ".vwlibrary", CLIName + ".vlb", spool.SpoolDirName} {
		if _, err := os.Lstat(filepath.Join(cli, mark)); err == nil {
			return filepath.Dir(cli), true
		}
	}
	return "", false
}
