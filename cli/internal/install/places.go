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
// 途中で退避した唯一の版を前の版と取り違えて消さないため）。鍵は、リンクを解いたパス（resolvedPath）の sha256 の
// 先頭 12 桁（入った CLI が求める場所と、初回のスクリプトに渡した書き方が揃う）。同じフォルダの綴りの違いは揃える。
// Windows は小文字にし、mac はディスクにある綴りにする（onDiskCase）。mac で小文字にしないのは、大文字と小文字を区別する
// ボリュームの、それだけが違う 2 つのフォルダに 1 つの <Work> を使わせないため。揃えないと、区別しない既定のボリュームで
// 綴りを変えて渡したときに <Work> が分かれ、付け替えで old/cli/spool へ移ったロックを次の回が見落とす。
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

// onDiskCase は、絶対パス path の各要素を、親のディレクトリに実際にある名前の綴りにする（大文字と小文字を区別しない
// ボリュームで、綴りの違う同じフォルダを揃える）。そのままの名前があればそれを、無ければ大文字と小文字だけが違う名前が
// 1 つだけあるときにそれを使う（区別するボリュームでも、そのままの名前のフォルダを別のものと取り違えない）。読めない
// ディレクトリ（まだ無い）から先は、書いたまま。
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
	// 名前は、Windows では大文字と小文字を区別せず、ほかでは区別して比べる（install が作る名前は bin と cli。区別する
	// ボリュームの CLI/bin を cli/bin と取り違えて、別のフォルダのロックと記録を見ない）。
	same := func(a, b string) bool { return a == b }
	if runtime.GOOS == "windows" {
		same = strings.EqualFold
	}
	if !same(filepath.Base(bin), "bin") || !same(filepath.Base(cli), CLIName) {
		return "", false
	}
	for _, mark := range []string{RecordFile, CLIName + ".vwlibrary", CLIName + ".vlb", spool.SpoolDirName} {
		if _, err := os.Lstat(filepath.Join(cli, mark)); err == nil {
			return filepath.Dir(cli), true
		}
	}
	return "", false
}

// places は、Options の場所から決まるパス。
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
	// 場所は絶対パスだけを受け付ける（今の作業ディレクトリから絶対にすると、それが分からないときに
	// 相対パスのまま名前の変更や削除へ進みうる。消すコードの歯止めに関わるため、組み立てない）。
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
	// 片方がもう片方の中にあれば（初回のスクリプトに <Work>/old を --plugins-dir として渡したなど）、片付けが
	// 入れたばかりのプラグインを消し、付け替えが自分を動かすので断る（消すコードの歯止め。守ること 9）。
	if overlap(plugins, work) {
		return places{}, fmt.Errorf("the folder of plug-ins %s and the work folder %s overlap (one is in the other); "+
			"nothing is changed", plugins, work)
	}
	staging := filepath.Join(work, "staging")
	old := filepath.Join(work, "old")
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

// overlap は、a と b の片方がもう片方と同じか、その中か。書いたままのパスと、リンクを解いたパス（resolvedPath。それぞれ
// 1 回だけ解く）の書き方と、ディレクトリの同一性（SameDirectory・within）で決める。書き方は、まだ無い場所も見つける
// （リンクを通して <Work> の下を指す --plugins-dir も、解いた書き方で見つける）。同一性は、Windows で EvalSymlinks が
// 解かないジャンクション・subst のドライブ・割り当てたドライブ・短い名前も見つける。親を順に調べる within は、ほかで
// 決まらないときだけ走らせる。
func overlap(a, b string) bool {
	ra, rb := resolvedPath(a), resolvedPath(b)
	return lexicallyIn(a, b) || lexicallyIn(b, a) || lexicallyIn(ra, rb) || lexicallyIn(rb, ra) ||
		SameDirectory(a, b) || isWithin(a, b) || isWithin(b, a)
}

// isWithin は within の真偽だけ。
func isWithin(path, dir string) bool {
	_, inside := within(path, dir)
	return inside
}

// SameDirectory は、a と b が同じディレクトリか（書き方ではなく同一性で決める。どちらかが無ければ偽）。
func SameDirectory(a, b string) bool {
	x, errX := os.Stat(a)
	y, errY := os.Stat(b)
	return errX == nil && errY == nil && os.SameFile(x, y)
}

// lexicallyIn は、書き方で path が dir と同じか、その中か。大文字と小文字を区別しないのが既定の OS（mac・Windows）
// だけ、その違いを除いて比べる（区別するボリュームの、大文字と小文字だけが違う別のフォルダを重ねない。区別しない
// 名前の別の書き方は、あれば同一性の確かめでも見つける）。
func lexicallyIn(path, dir string) bool {
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		path, dir = strings.ToLower(path), strings.ToLower(dir)
	}
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// resolvedPath は、path のリンクを解いた書き方。まだ無ければ、あるところまでの親を解き、残りをそのまま足す（根まで
// 解けなければ、書いたまま）。
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
