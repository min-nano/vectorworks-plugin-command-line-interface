package install

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// oldCLIPrefixes は、よけた CLI の名前の頭（<Work>/old/ の直下の vw2026.old-* / vw2026.exe.old-*）。
// 名前に . を含むので、<Work>/old/<name>/（名前は . を含まない）と重ならない。
var oldCLIPrefixes = []string{"vw2026.old-", "vw2026.exe.old-"}

// movedCLI は、よけた CLI の元の場所と移した先。nil ならよけていない。
type movedCLI struct {
	from string
	to   string
}

// restore はよけた CLI を元の場所へ戻す（動いていても名前の変更はできる）。
func (m *movedCLI) restore() {
	if m != nil {
		_ = rename(m.to, m.from)
	}
}

// sidestep は、走らせている CLI の実行ファイルが dir の中にあれば、old へ名前の変更で移す
// （docs/plugin/install-and-update.md「動いている CLI のよけ方」）。Windows では開いている
// ファイル（動いている実行ファイル）を含むフォルダの名前を変えられないが、実行ファイルそのものは
// 同じボリュームの別のディレクトリへ移せる。mac でも同じ手順を踏む（OS で分けない）。
func (o *Options) sidestep(dir, old string) (*movedCLI, error) {
	if o.Executable == "" {
		return nil, nil
	}
	executable := o.Executable
	// mac の ~/.local/bin/vw2026 はリンクなので、解いた先で決める。
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		executable = resolved
	}
	rel, ok := within(executable, dir)
	if !ok {
		return nil, nil
	}
	if err := os.MkdirAll(old, 0o700); err != nil {
		return nil, err
	}
	to := filepath.Join(old, filepath.Base(executable)+".old-"+o.now().UTC().Format("20060102T150405.000000000"))
	if err := rename(executable, to); err != nil {
		return nil, err
	}
	return &movedCLI{from: filepath.Join(dir, rel), to: to}, nil
}

// within は、path が dir の中にあれば dir からの相対パスを返す。パスの文字列ではなく
// ディレクトリの同一性で決める（path のあるディレクトリから親へ 1 つずつ上り、os.SameFile で比べる）。
// dir は --plugins-dir・環境変数・既定の場所のどれからでも来て、実行ファイルのパスと書き方
// （大文字と小文字・8.3 形式・ジャンクション・末尾の区切り）が揃うとは限らないため。
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

// cleanOldCLIs は <Work>/old/ の直下のよけた CLI（ファイルだけ）を消してみて、消せなかったものを
// 返す（Windows では動いている実行ファイルを消せない）。ほかのものは消さない。
func cleanOldCLIs(old string) []string {
	entries, err := os.ReadDir(old)
	if err != nil {
		return nil
	}
	var left []string
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !hasOldCLIPrefix(entry.Name()) {
			continue
		}
		path := filepath.Join(old, entry.Name())
		if err := removeFile(path); err != nil {
			left = append(left, path)
		}
	}
	sort.Strings(left)
	return left
}

func hasOldCLIPrefix(name string) bool {
	for _, prefix := range oldCLIPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// rename と removeFile は、付け替えと後始末の失敗を確かめるテストが差し替える。
var (
	rename     = os.Rename
	removeFile = os.Remove
)
