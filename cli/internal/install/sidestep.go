package install

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/edition"
	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/spool"
)

// oldCLIPrefixes は、退避した CLI の名前の先頭（<Work>/old/ の直下の vw2026.old-* /
// vw2026.exe.old-*。2025 は vw2025）。名前に . を含むため、<Work>/old/<name>/（名前は . を
// 含まない）と重複しない。
var oldCLIPrefixes = []string{edition.Command + ".old-", edition.Command + ".exe.old-"}

// oldCLIName は、この OS で退避した CLI に付ける名前の先頭（oldCLIPrefixes の 1 つ。
// Windows のみ実行ファイルの名前に .exe が付く）。後処理が探索する先頭と同じ一覧から選択するため、
// 食い違わない。
var oldCLIName = func() string {
	if runtime.GOOS == "windows" {
		return oldCLIPrefixes[1]
	}
	return oldCLIPrefixes[0]
}()

// movedCLI は、退避した CLI の元の場所（退避したフォルダ dir と、そこからの相対パス rel）と移動先
// to。nil ならば退避していない。元の場所は dir と rel から導出し（from）、別途保持しない
// （restore と restoreInto の戻し先が乖離しないように）。
type movedCLI struct {
	dir string
	rel string
	to  string
}

// from は、退避した CLI の元の場所。
func (m *movedCLI) from() string {
	return filepath.Join(m.dir, m.rel)
}

// restore は退避した CLI を元の場所へ戻す（実行中でも名前の変更はできる）。戻せなければ（Windows で
// ウイルス対策が退避した実行ファイルを開いているなど）、呼び出し元の文言に追加する、
// 手作業で戻す手順を返す（戻せたら空）。通知せずに残置すると、退避した CLI の名前のまま、
// 次の install・uninstall の後処理が唯一の vw2026 を削除するため。
func (m *movedCLI) restore() string {
	if m == nil {
		return ""
	}
	if err := rename(m.to, m.from()); err != nil {
		return fmt.Sprintf("; the running "+edition.Command+" cannot be moved back (%v): move it back from %s to %s by hand "+
			"before the next install or uninstall (they delete it)", err, m.to, m.from())
	}
	return ""
}

// restoreInto は、退避した CLI を、退避したフォルダの中の元の場所に相当する into の中の場所へ戻す
// （新しい版を残置した場合に、前の版のフォルダへ戻す）。戻せなければ、その場所を示す文言を返す
// （戻せたら空）。
func (m *movedCLI) restoreInto(into string) string {
	if m == nil {
		return ""
	}
	target := filepath.Join(into, m.rel)
	if err := rename(m.to, target); err != nil {
		return fmt.Sprintf("; the "+edition.Command+" of the previous version is in %s (%v): move it to %s if you put the previous "+
			"version back, before the next install or uninstall (they delete it)", m.to, err, target)
	}
	return ""
}

// where は、フォルダを手作業で戻すよう示す文言に追加する、退避した CLI の場所（退避していなければ
// 空）。フォルダを戻すと CLI の元の場所が生じるため、続けて CLI を戻せる（退避した CLI は戻した版の
// フォルダには無い）。
func (m *movedCLI) where() string {
	if m == nil {
		return ""
	}
	return fmt.Sprintf(", then move the running "+edition.Command+" back from %s to %s", m.to, m.from())
}

// sidestep は、実行している CLI の実行ファイルが dir の中にあれば、old へ名前の変更で移動する
// （docs/plugin/install-and-update.md「実行中の CLI の退避」）。Windows では開いているファイル
// （実行中の実行ファイル）を含むフォルダの名前を変更できないが、実行ファイルそのものは同じ
// ボリュームの別のディレクトリへ移動できる。mac でも同じ手順を踏む（OS で区別しない）。
func (o *Options) sidestep(dir, old string) (*movedCLI, error) {
	executable, rel, ok := o.runsFrom(dir)
	if !ok {
		return nil, nil
	}
	// old は呼び出し元（replace）が先に作成している。
	// 名前は実行ファイルの名前に依らず固定とする（cleanOldCLIs が同じ先頭で削除できるように）。
	to := filepath.Join(old, oldCLIName+stamp())
	if err := rename(executable, to); err != nil {
		return nil, err
	}
	return &movedCLI{dir: dir, rel: rel, to: to}, nil
}

// stamp は、<Work>/old/ の中で重複しない名前に付ける時刻。
func stamp() string {
	return time.Now().UTC().Format("20060102T150405.000000000")
}

// runsFrom は、実行している CLI の実行ファイル（リンクを解決したもの）が dir の中にあるかと、
// その場所と dir からの相対パスを返す。Executable が空ならば中に無い。
func (o *Options) runsFrom(dir string) (executable, rel string, ok bool) {
	if o.Executable == "" {
		return "", "", false
	}
	// mac の ~/.local/bin/vw2026 はリンクであるため、解決した先で決定する。
	executable = spool.ResolveLinks(o.Executable)
	rel, ok = within(executable, dir)
	return executable, rel, ok
}

// within は、path が dir の中にあれば dir からの相対パスを返す。パスの文字列ではなくディレクトリの
// 同一性で決定する（path のあるディレクトリから親へ 1 つずつ遡り、os.SameFile で比較する）。
// dir はインストール先・初回スクリプトの --plugins-dir・既定の場所のいずれからでも渡され、
// 実行ファイルのパスと表記（大文字と小文字・8.3 形式・ジャンクション・末尾の区切り）が一致するとは
// 限らないため。
func within(path, dir string) (string, bool) {
	target, err := os.Stat(dir)
	if err != nil {
		return "", false
	}
	for current := filepath.Dir(path); ; {
		if info, err := os.Stat(current); err == nil && os.SameFile(info, target) {
			rel, err := filepath.Rel(current, path)
			return rel, err == nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", false
		}
		current = parent
	}
}

// cleanOldCLIs は <Work>/old/ の直下の退避した CLI（ファイルのみ）の削除を試み、
// 削除できなかったものを返す（Windows では実行中の実行ファイルを削除できない）。
// 他のものは削除しない。
func cleanOldCLIs(old string) []string {
	// 読み取れなければ（無ければ）削除するものも無い。
	entries, _ := os.ReadDir(old)
	// 出力の left は、無くても null ではなく [] とする。os.ReadDir の順序（名前の昇順）のまま
	// 並べる。
	left := []string{}
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !hasOldCLIPrefix(entry.Name()) {
			continue
		}
		path := filepath.Join(old, entry.Name())
		if err := removeFile(path); err != nil {
			left = append(left, path)
		}
	}
	return left
}

func hasOldCLIPrefix(name string) bool {
	return slices.ContainsFunc(oldCLIPrefixes, func(prefix string) bool { return strings.HasPrefix(name, prefix) })
}

// ファイルの操作の内、OS の失敗（権限・ディスクの誤り）でしか失敗しないものは、
// テストが差し替えて失敗させる（失敗した場合に <Plug-Ins> を元に戻すことを検証するため）。
var (
	rename     = os.Rename
	removeFile = os.Remove
	removeAll  = os.RemoveAll
	mkdirAll   = os.MkdirAll
	writeFile  = os.WriteFile
	openFile   = os.OpenFile
)
