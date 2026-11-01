package install

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// 入った CLI（<X>/cli/bin/ にあり、<X>/cli/ に記録・モジュール・スプールのどれかがある。中身は読まない）なら <X>。
// リンクは解く。それ以外は偽。
func TestPluginsDirOf(t *testing.T) {
	h := newHarness(t)
	h.installCLI()
	executable := filepath.Join(h.plugins, "cli", "bin", "vw2026")
	// リンクを解いた場所を返すので、書き方（mac の /var と /private/var、Windows の短い名前）ではなく同一性で比べる。
	if dir, ok := PluginsDirOf(executable); !ok || !SameDirectory(dir, h.plugins) {
		t.Fatal(dir, ok)
	}
	link := filepath.Join(h.tmp, "vw2026-link")
	if err := os.Symlink(executable, link); err == nil {
		if dir, ok := PluginsDirOf(link); !ok || !SameDirectory(dir, h.plugins) {
			t.Fatal("link:", dir, ok)
		}
	}
	for _, path := range []string{"", filepath.Join(h.tmp, "vw2026"), filepath.Join(h.plugins, "cli", "vw2026")} {
		if dir, ok := PluginsDirOf(path); ok {
			t.Error(path, dir)
		}
	}
	// 記録の module が無くても（隔離された）入った CLI とみなす（既定の場所に別の cli を入れない）。
	os.RemoveAll(filepath.Join(h.plugins, "cli", "cli.vwlibrary"))
	if _, ok := PluginsDirOf(executable); !ok {
		t.Fatal("without the module")
	}
	// 記録が読めなくても入った CLI とみなす（既定の場所へ移らず、install が自分のフォルダで断る）。
	os.WriteFile(filepath.Join(h.plugins, "cli", RecordFile), []byte("broken"), 0o644)
	if _, ok := PluginsDirOf(executable); !ok {
		t.Fatal("with a broken record")
	}
	// 記録が消えても、スプールがあれば入った CLI とみなす（既定の場所に別の cli を入れない）。
	os.Remove(filepath.Join(h.plugins, "cli", RecordFile))
	os.MkdirAll(filepath.Join(h.plugins, "cli", "spool"), 0o700)
	if _, ok := PluginsDirOf(executable); !ok {
		t.Error("with the spool only")
	}
	// どの印も無ければ入った CLI とみなさない（リポジトリの cli/bin/ に手でビルドした CLI）。
	os.RemoveAll(filepath.Join(h.plugins, "cli", "spool"))
	if _, ok := PluginsDirOf(executable); ok {
		t.Error("without any mark")
	}
}

// <Work> は <Plug-Ins> ごとに分ける。同じフォルダは書き方（リンク・末尾の区切り）に依らず同じ <Work>、別のフォルダは
// 別の <Work>。
func TestWorkDirFor(t *testing.T) {
	tmp := t.TempDir()
	a := filepath.Join(tmp, "a", "Plug-Ins")
	b := filepath.Join(tmp, "b", "Plug-Ins")
	os.MkdirAll(a, 0o755)
	os.MkdirAll(b, 0o755)
	base := filepath.Join(tmp, WorkDirName)
	work := WorkDirFor(base, a)
	if filepath.Dir(work) != tmp || !strings.HasPrefix(filepath.Base(work), WorkDirName+"-") ||
		len(filepath.Base(work)) != len(WorkDirName)+1+12 {
		t.Fatal(work)
	}
	if WorkDirFor(base, b) == work || WorkDirFor(base, a+string(filepath.Separator)) != work {
		t.Fatal("different folders should differ, and the same folder should not")
	}
	// mac と Windows（大文字と小文字を区別しない既定のボリューム）では、綴りの違う同じフォルダも同じ <Work>。
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		if WorkDirFor(base, strings.ToUpper(a)) != work {
			t.Fatal("the same folder written in another case should give the same work folder")
		}
	}
	link := filepath.Join(tmp, "link")
	if os.Symlink(a, link) == nil && WorkDirFor(base, link) != work {
		t.Fatal("a link to the same folder should give the same work folder")
	}
}

// cli と bin の名前は、Windows でだけ大文字と小文字を区別しない（区別するボリュームの CLI/bin は別のフォルダ）。
func TestPluginsDirOfCase(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "CLI")
	os.MkdirAll(filepath.Join(dir, "BIN"), 0o755)
	os.WriteFile(filepath.Join(dir, RecordFile), nil, 0o644)
	executable := filepath.Join(dir, "BIN", "vw2026")
	os.WriteFile(executable, nil, 0o700)
	if _, ok := PluginsDirOf(executable); ok != (runtime.GOOS == "windows") {
		t.Fatal(ok)
	}
}

// onDiskCase は、ディスクにある綴りにする。そのままの名前があればそれを使い、まだ無い場所から先は書いたまま。
func TestOnDiskCase(t *testing.T) {
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(tmp, "Vectorworks", "Plug-Ins"), 0o755)
	if got, want := onDiskCase(filepath.Join(tmp, "VECTORWORKS", "plug-ins")), filepath.Join(tmp, "Vectorworks", "Plug-Ins"); got != want {
		t.Errorf("%s, want %s", got, want)
	}
	if got, want := onDiskCase(filepath.Join(tmp, "vectorworks", "Missing", "X")), filepath.Join(tmp, "Vectorworks", "Missing", "X"); got != want {
		t.Errorf("missing: %s, want %s", got, want)
	}
	// 大文字と小文字だけが違う 2 つのフォルダ（区別するボリューム）は、そのままの名前を使い、どちらでもなければ書いたまま。
	other := filepath.Join(tmp, "vectorworks")
	if os.Mkdir(other, 0o755) == nil && !SameDirectory(other, filepath.Join(tmp, "Vectorworks")) {
		if got := onDiskCase(other); got != other {
			t.Errorf("exact: %s", got)
		}
		if got, want := onDiskCase(filepath.Join(tmp, "VectorWorks")), filepath.Join(tmp, "VectorWorks"); got != want {
			t.Errorf("ambiguous: %s, want %s", got, want)
		}
	}
	// 根・相対パス・ファイルの下は、そのまま。
	root := filepath.VolumeName(tmp) + string(filepath.Separator)
	file := filepath.Join(tmp, "file")
	os.WriteFile(file, nil, 0o644)
	for _, path := range []string{root, "relative/path", filepath.Join(file, "x")} {
		if got := onDiskCase(path); got != filepath.Clean(path) && got != path {
			t.Errorf("%s: %s", path, got)
		}
	}
}
