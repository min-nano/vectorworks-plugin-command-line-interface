package install

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/edition"
	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/spool"
)

// workDirName は、利用者ごとの一時フォルダの下の <Work> の名前の先頭
// （docs/plugin/install-and-update.md「作業領域」。WorkDirFor が <Plug-Ins> ごとの鍵を追加する）。
// vw2026 のみが使用するため、通信規則のフィクスチャには含めない。版ごとに区別する（2025 は
// vectorworks2025-cli。docs/plugin/editions.md）。
const workDirName = "vectorworks" + edition.Year + "-cli"

// WorkDir は <Work> の先頭（利用者ごとの一時フォルダの下の workDirName。WorkDirFor に渡す）を返す。
// 場所の根は環境変数を読み取らずに OS の定められた場所から導出する（実行の方法で場所が変わると、
// 以前の回が残置した old/ を次の回が発見できなくなるため）。
func WorkDir() (string, error) {
	temp, err := userTempDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(temp, workDirName), nil
}

// WorkDirFor は、<Plug-Ins> plugins の <Work>（先頭 base に - と鍵を追加した名前）を返す。
// <Work>/old/<name>/ を <Plug-Ins> ごとに区別する（同じ利用者が別の <Plug-Ins> にも
// インストールしている場合に、一方の install が、他方の差し替えの途中で退避した唯一の版を前の版と
// 取り違えて削除しないため）。鍵は、リンクを解決したパス（resolvedPath）の sha256 の先頭 12 桁
// （インストールされた CLI が導出する場所と、初回スクリプトに渡した表記が一致する）。
// 同じフォルダの綴りの相違は統一する。Windows は小文字とし、mac はディスク上の綴りとする
// （onDiskCase）。mac で小文字にしないのは、大文字と小文字を区別するボリュームの、
// それのみが異なる 2 つのフォルダに 1 つの <Work> を使用させないため。統一しないと、
// 区別しない既定のボリュームで綴りを変えて渡した場合に <Work> が分かれ、差し替えで old/cli/spool へ
// 移動したロックを次の回が見落とす。
func WorkDirFor(base, plugins string) string {
	key := resolvedPath(filepath.Clean(plugins))
	switch runtime.GOOS {
	case "windows":
		key = strings.ToLower(key)
	case "darwin":
		key = onDiskCase(key)
	}
	sum := sha256.Sum256([]byte(key))
	return base + "-" + hex.EncodeToString(sum[:6])
}

// onDiskCase は、絶対パス path の各要素を、親のディレクトリに実際に存在する名前の綴りとする
// （大文字と小文字を区別しないボリュームで、綴りの異なる同じフォルダを統一する）。
// そのままの名前が存在すればそれを、無ければ大文字と小文字のみが異なる名前が 1 つのみ存在する
// 場合にそれを使用する（区別するボリュームでも、そのままの名前のフォルダを別のものと
// 取り違えない）。読み取れないディレクトリ（まだ存在しない）から先は、記述したまま。
func onDiskCase(path string) string {
	if !filepath.IsAbs(path) {
		return path
	}
	volume := filepath.VolumeName(path)
	separator := string(filepath.Separator)
	parts := strings.Split(strings.Trim(path[len(volume):], separator), separator)
	current := volume + separator
	for i, part := range parts {
		entries, err := os.ReadDir(current)
		if err != nil {
			return filepath.Join(append([]string{current}, parts[i:]...)...)
		}
		current = filepath.Join(current, nameOnDisk(entries, part))
	}
	return current
}

// nameOnDisk は、entries の中の name の綴り（onDiskCase）。
func nameOnDisk(entries []os.DirEntry, name string) string {
	match, count := name, 0
	for _, entry := range entries {
		if entry.Name() == name {
			return name
		}
		if strings.EqualFold(entry.Name(), name) {
			match = entry.Name()
			count++
		}
	}
	if count != 1 {
		return name
	}
	return match
}

// DefaultPluginsDir は既定の <Plug-Ins> を返す（docs/plugin/identifiers.md「配置場所」）。
func DefaultPluginsDir() (string, error) {
	return defaultPluginsDir()
}

// Target は計算機からターゲットを 1 つに決める（docs/registry.md「名前・タグ・ターゲット」）。
func Target() string {
	return target()
}

// PluginsDirOf は、実行ファイル executable（リンクは解決する。mac の ~/.local/bin/vw2026 は
// リンク）が <X>/cli/bin/ にあり、<X>/cli/ に cli のフォルダであることを示すもの（記録・
// モジュール・スプールのいずれか）があれば、<X> を返す（インストールされた CLI は、自身の
// インストール先の <Plug-Ins> を使用する。インストール先を記録しなくても、場所を取り違えない）。
// これらは、存在するかのみを確認して内容は読み取らない（module が欠落した・記録が破損した・
// 記録が削除された cli でも、既定の場所に別の cli をインストールせず、自身のインストール先の
// フォルダで install・uninstall が記録を検証して、再インストールするか拒否する）。
// 初回スクリプトの一時フォルダの CLI・手作業でビルドした CLI（リポジトリの cli/bin/ など。
// いずれも無い）ならば ok は偽。
func PluginsDirOf(executable string) (dir string, ok bool) {
	if executable == "" {
		return "", false
	}
	bin := filepath.Dir(spool.ResolveLinks(executable))
	cli := filepath.Dir(bin)
	// 名前は、Windows では大文字と小文字を区別せず、他では区別して比較する（install が作成する
	// 名前は bin と cli。区別するボリュームの CLI/bin を cli/bin と取り違えて、
	// 別のフォルダのロックと記録を参照しない）。
	same := func(a, b string) bool { return a == b }
	if runtime.GOOS == "windows" {
		same = strings.EqualFold
	}
	if !same(filepath.Base(bin), "bin") || !same(filepath.Base(cli), cliName) {
		return "", false
	}
	for _, mark := range []string{RecordFile, cliName + ".vwlibrary", cliName + ".vlb", spool.SpoolDirName} {
		if _, err := os.Lstat(filepath.Join(cli, mark)); err == nil {
			return filepath.Dir(cli), true
		}
	}
	return "", false
}

// places は、Options の場所から決定されるパス。
type places struct {
	plugins  string
	dest     string
	work     string
	staging  string
	stageDir string
	stageZip string
	old      string
	oldDir   string
	marker   string
}

func (o *Options) places(name string) (places, error) {
	// 場所は絶対パスのみを受け付ける（現在の作業ディレクトリから絶対パスにすると、
	// それが不明な場合に相対パスのまま名前の変更や削除へ進みうる。削除処理の制約に関わるため、
	// 構築しない）。
	switch {
	case o.PluginsDir == "" || o.WorkDir == "":
		return places{}, errors.New("the folder of plug-ins or the work folder is unknown")
	case !filepath.IsAbs(o.PluginsDir):
		return places{}, Usagef("the folder of plug-ins %q is not an absolute path", o.PluginsDir)
	case !filepath.IsAbs(o.WorkDir):
		return places{}, fmt.Errorf("the work folder %q is not an absolute path", o.WorkDir)
	}
	plugins := filepath.Clean(o.PluginsDir)
	work := filepath.Clean(o.WorkDir)
	// 一方が他方の中にあれば（初回スクリプトに <Work>/old を --plugins-dir として渡したなど）、
	// 整理がインストールしたばかりのプラグインを削除し、差し替えが自身を移動するため拒否する
	// （削除処理の制約。遵守事項 9）。
	if overlap(plugins, work) {
		return places{}, fmt.Errorf("the folder of plug-ins %s and the work folder %s overlap (one is in the other); "+
			"nothing is changed", plugins, work)
	}
	staging := filepath.Join(work, "staging")
	old := filepath.Join(work, "old")
	if err := plainWorkFolders(work, staging, old); err != nil {
		return places{}, err
	}
	return places{
		plugins:  plugins,
		dest:     filepath.Join(plugins, name),
		work:     work,
		staging:  staging,
		stageDir: filepath.Join(staging, name),
		stageZip: filepath.Join(staging, name+".zip"),
		old:      old,
		oldDir:   filepath.Join(old, name),
		marker:   filepath.Join(old, name+".uninstalling"),
	}, nil
}

// plainWorkFolders は、<Work>・<Work>/staging・<Work>/old のそれぞれが、存在しないか通常のフォルダかを検証する。
// リンク（Windows ではジャンクションも。Go は ModeDir と ModeIrregular を返す）・ファイルであれば、何も変更せずに
// 誤りとする（名前をそのまま用いて構築した staging/<name>/ や old/<name>/ を削除・移動する際に、途中のリンクを
// 経由してリンク先のフォルダに触れないため。遵守事項 9 の制約）。存在しない以外の理由で確認できなければ、その誤りを
// 返す。<Work> の親（mac の /var などはリンク）は検証しない。<Work> から下は vw2026 が作成するため、通常のフォルダ
// のみが存在するはずである。
func plainWorkFolders(dirs ...string) error {
	for _, dir := range dirs {
		info, err := lstat(dir)
		switch {
		case errors.Is(err, os.ErrNotExist):
			continue
		case err != nil:
			return fmt.Errorf("cannot check the work folder %s (%w); nothing is changed", dir, err)
		case info.Mode().Type() != os.ModeDir:
			return fmt.Errorf("the work folder %s is not a plain folder (a link, a junction or a file); nothing is "+
				"changed. Remove it (removing a link does not touch what it points to), then run again", dir)
		}
	}
	return nil
}

// overlap は、a と b の一方が他方と同じか、その中か。記述したままのパスと、リンクを解決したパス
// （resolvedPath。それぞれ 1 回のみ解決する）の表記と、ディレクトリの同一性
// （SameDirectory・within）で決定する。表記は、まだ存在しない場所も検出する（リンクを経由して
// <Work> の下を指す --plugins-dir も、解決した表記で検出する）。同一性は、Windows で
// EvalSymlinks が解決しないジャンクション・subst のドライブ・割り当てたドライブ・短い名前も
// 検出する。親を順に調査する within は、他で決定されない場合のみ実行する。
func overlap(a, b string) bool {
	ra, rb := resolvedPath(a), resolvedPath(b)
	return lexicallyIn(a, b) || lexicallyIn(b, a) || lexicallyIn(ra, rb) || lexicallyIn(rb, ra) ||
		SameDirectory(a, b) || isWithin(a, b) || isWithin(b, a)
}

// isWithin は within の真偽のみ。
func isWithin(path, dir string) bool {
	_, inside := within(path, dir)
	return inside
}

// SameDirectory は、a と b が同じディレクトリか（表記ではなく同一性で決定する。
// いずれかが無ければ偽）。
func SameDirectory(a, b string) bool {
	x, errX := os.Stat(a)
	y, errY := os.Stat(b)
	return errX == nil && errY == nil && os.SameFile(x, y)
}

// lexicallyIn は、表記で path が dir と同じか、その中か。大文字と小文字を区別しないのが既定の
// OS（mac・Windows）のみ、その相違を除いて比較する（区別するボリュームの、大文字と小文字のみが
// 異なる別のフォルダを重ねない。区別しない名前の別の表記は、存在すれば同一性の検証でも検出する）。
func lexicallyIn(path, dir string) bool {
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		path, dir = strings.ToLower(path), strings.ToLower(dir)
	}
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// resolvedPath は、path のリンクを解決した表記。まだ存在しなければ、存在するところまでの親を
// 解決し、残りをそのまま追加する（根まで解決できなければ、記述したまま）。まだ
// 存在しないかもしれない場所に使用する。実在する実行ファイルは spool.ResolveLinks で解決する。
func resolvedPath(path string) string {
	for current, rest := path, ""; ; {
		real, err := filepath.EvalSymlinks(current)
		parent := filepath.Dir(current)
		if err == nil || parent == current {
			return filepath.Join(cmp.Or(real, current), rest)
		}
		rest = filepath.Join(filepath.Base(current), rest)
		current = parent
	}
}
