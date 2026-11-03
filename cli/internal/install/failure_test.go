package install

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/fakeplugin"
	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/registry"
	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/spool"
)

// errInjected は、テストが差し替えたファイルの操作が返す失敗。
var errInjected = errors.New("injected failure")

// swap は、テストの間だけ差し替え口 *at（ファイルの操作・上限）を value に替える。返す関数で途中でも元に戻せる
// （テストの終わりにも戻す）。戻すのは、この swap の前の値なので、途中で戻してよいのは、その差し替え口を最後に
// 差し替えたものだけ（あとから同じ差し替え口を差し替えていると、それも外れる）。
func swap[T any](t *testing.T, at *T, value T) (restore func()) {
	t.Helper()
	original := *at
	*at = value
	restore = func() { *at = original }
	t.Cleanup(restore)
	return restore
}

// renameWhen・pathOpWhen・writeFileWhen・mkdirAllWhen は、テストの間だけその操作の前に when を呼び、when が誤りを返せば
// それで失敗させる（nil なら元の操作をする。見るだけのときも nil を返す）。返す関数で途中でも元に戻せる。
// pathOpWhen は、パス 1 つを受ける操作（removeAll・removeFile）の差し替え口 at に使う。
func renameWhen(t *testing.T, when func(from, to string) error) (restore func()) {
	t.Helper()
	original := rename
	return swap(t, &rename, func(from, to string) error {
		if err := when(from, to); err != nil {
			return err
		}
		return original(from, to)
	})
}

func pathOpWhen(t *testing.T, at *func(string) error, when func(path string) error) (restore func()) {
	t.Helper()
	original := *at
	return swap(t, at, func(path string) error {
		if err := when(path); err != nil {
			return err
		}
		return original(path)
	})
}

func writeFileWhen(t *testing.T, when func(path string) error) (restore func()) {
	t.Helper()
	original := writeFile
	return swap(t, &writeFile, func(path string, data []byte, perm fs.FileMode) error {
		if err := when(path); err != nil {
			return err
		}
		return original(path, data, perm)
	})
}

func mkdirAllWhen(t *testing.T, when func(path string) error) (restore func()) {
	t.Helper()
	original := mkdirAll
	return swap(t, &mkdirAll, func(path string, perm fs.FileMode) error {
		if err := when(path); err != nil {
			return err
		}
		return original(path, perm)
	})
}

// failMkdirAll は、その場所のディレクトリの作成を失敗させる（OS の失敗を真似る）。
func failMkdirAll(t *testing.T, at string) {
	t.Helper()
	mkdirAllWhen(t, func(path string) error {
		if path == at {
			return errInjected
		}
		return nil
	})
}

// failRemoveAll は、その場所の削除を失敗させる（OS の失敗を真似る）。
func failRemoveAll(t *testing.T, at string) {
	t.Helper()
	pathOpWhen(t, &removeAll, func(path string) error {
		if path == at {
			return errInjected
		}
		return nil
	})
}

// failRename は、from から to への名前の変更を失敗させる（to が空なら行き先を問わない。同じ from からほかへの
// 変更は通す）。返す関数で途中でも元に戻せる。
func failRename(t *testing.T, from, to string) (restore func()) {
	t.Helper()
	return renameWhen(t, func(oldpath, newpath string) error {
		if oldpath == from && (to == "" || newpath == to) {
			return errInjected
		}
		return nil
	})
}

// resolved は、リンクを解いたパス（sidestep が移す実行ファイルのパス）。
func resolved(t *testing.T, path string) string {
	t.Helper()
	got, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// 展開先を作れなければ、展開を失敗にする。
func TestExtractCannotMakeTheFolder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.zip")
	os.WriteFile(path, makeZip(t, []entry{{name: "a", body: "x"}}), 0o644)
	out := filepath.Join(dir, "out")
	failMkdirAll(t, out)
	if _, err := extract(path, out); !errors.Is(err, errInjected) {
		t.Fatal(err)
	}
}

// zip が記録の名前のフォルダを持てば、記録を書けずに失敗し、何も変えない。
func TestInstallCannotWriteTheRecord(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	h.publish(release{name: "cli", tag: "c2", extra: []entry{{name: RecordFile + "/x", body: "x"}}})
	before := snapshot(t, h.plugins)
	if _, err := h.install("cli", "c2"); err == nil {
		t.Fatal("should fail")
	}
	sameSnapshot(t, before, snapshot(t, h.plugins))
}

// 取ったものを書く場所がフォルダなら、取るのを失敗にする。
func TestDownloadCannotWrite(t *testing.T) {
	h := newHarness(t)
	h.serve("/a.zip", []byte("x"))
	o := h.options()
	if err := o.download(h.url("/a.zip"), t.TempDir(), sha256Hex([]byte("x"))); err == nil {
		t.Fatal("should fail")
	}
}

// 本文の途中で切れたら、取るのを失敗にする（マニフェストも zip も）。転送が 10 回を超えたら止める。
func TestTruncatedAndLoopingResponses(t *testing.T) {
	h := newHarness(t)
	inner := h.server.Config.Handler
	h.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cut":
			w.Header().Set("Content-Length", "100")
			w.Write([]byte("0123456789"))
		case "/loop":
			http.Redirect(w, r, h.url("/loop"), http.StatusFound)
		default:
			inner.ServeHTTP(w, r)
		}
	})
	o := h.options()
	if _, err := o.fetch(h.url("/cut")); err == nil {
		t.Error("a cut manifest should fail")
	}
	path := filepath.Join(t.TempDir(), "a.zip")
	if err := o.download(h.url("/cut"), path, strings.Repeat("0", 64)); err == nil || exist(path) {
		t.Errorf("a cut zip should fail and leave nothing: %v", err)
	}
	if _, err := o.fetch(h.url("/loop")); err == nil || !strings.Contains(err.Error(), "10 redirects") {
		t.Error(err)
	}
}

// Default を渡さなければ、CLI に埋め込んだ既定のレジストリを引く。
func TestLookupUsesTheEmbeddedRegistry(t *testing.T) {
	o := Options{}
	entry, source, err := o.lookup("cli")
	if err != nil || source != registry.DefaultName || !strings.Contains(entry.Manifests[0].Path, "github.com") {
		t.Fatalf("%v %s %v", entry, source, err)
	}
}

// <Plug-Ins>・<Work> は絶対パスだけ（相対の <Plug-Ins> は使い方の誤り）。整えた形で使う。
func TestRelativePlaces(t *testing.T) {
	dir := t.TempDir()
	if _, err := (&Options{PluginsDir: "Plug-Ins", WorkDir: dir}).places("cli"); !IsUsage(err) {
		t.Fatalf("a relative folder of plug-ins is wrong usage: %v", err)
	}
	if _, err := (&Options{PluginsDir: dir, WorkDir: "work"}).places("cli"); err == nil {
		t.Fatal("a relative work folder should fail")
	}
	p, err := (&Options{PluginsDir: filepath.Join(dir, "a", "..", "Plug-Ins"), WorkDir: filepath.Join(dir, "work") + string(filepath.Separator)}).places("cli")
	if err != nil || p.plugins != filepath.Join(dir, "Plug-Ins") || p.work != filepath.Join(dir, "work") {
		t.Fatalf("%+v %v", p, err)
	}
}

// 走らせられないフック（ほかの OS のもの）は、付け替える前に断る。記録に写したフックが走らない
// （ほかの OS から写したフォルダ）ときは、フックの失敗として戻す。
func TestHookOfAnotherOS(t *testing.T) {
	h := newHarness(t)
	interpreter, script := "powershell", "hooks/install.ps1"
	if runtime.GOOS == "windows" {
		interpreter, script = "sh", "hooks/install.sh"
	}
	hooks := registry.Hooks{Install: []string{interpreter, script}}
	h.publish(release{name: "cli", tag: "c1", hooks: &hooks, entries: []entry{
		{name: "cli.vwlibrary/x", body: "m"}, {name: script, body: ""},
	}})
	o := h.options()
	o.Interpreters = nil
	if _, err := Install(o, "cli", "c1", false); err == nil || !strings.Contains(err.Error(), "do not run") {
		t.Fatal(err)
	}
	if exist(filepath.Join(h.plugins, "cli")) {
		t.Fatal("left the plug-in")
	}

	h.mustInstall(release{name: "cli", tag: "c2"})
	dest := filepath.Join(h.plugins, "cli")
	record, _ := readRecord(dest)
	// powershell のスクリプトは .ps1 に限る（registry.Script）。
	uninstallScript := strings.Replace(script, "install", "uninstall", 1)
	os.WriteFile(filepath.Join(dest, filepath.FromSlash(uninstallScript)), nil, 0o644)
	record.Hooks.Uninstall = []string{interpreter, uninstallScript}
	data, _ := json.Marshal(record)
	os.WriteFile(filepath.Join(dest, RecordFile), data, 0o644)
	if _, err := Uninstall(o, "cli"); err == nil || !strings.Contains(err.Error(), "do not run") || !validRecord(dest, "cli") {
		t.Fatal(err)
	}
}

// フックの 1 つ目が zip の中の実行ファイルなら、それを走らせ、残りの引数を渡す。標準エラーの
// 行き先が無ければ捨てる。
func TestHookExecutableInTheZip(t *testing.T) {
	h := newHarness(t)
	script := entry{name: "hooks/run", body: "#!/bin/sh\nprintf '{\"arg\":\"%s\"}' \"$1\"\necho noise >&2\n", mode: 0o755}
	if runtime.GOOS == "windows" {
		script = entry{name: "hooks/run.cmd", body: "@echo {\"arg\":\"%1\"}\r\n@echo noise 1>&2\r\n"}
	}
	hooks := registry.Hooks{Install: []string{script.name, "a"}}
	h.publish(release{name: "cli", tag: "c1", hooks: &hooks, entries: []entry{{name: "cli.vwlibrary/x", body: "m"}, script}})
	o := h.options()
	o.Stderr = nil
	result, err := Install(o, "cli", "c1", false)
	if err != nil || string(result.Hook) != `{"arg":"a"}` {
		t.Fatalf("%+v %v", result, err)
	}
}

// 使い方の誤りは、何がいけないかを示す。
func TestUsageErrorMessage(t *testing.T) {
	_, err := Install(Options{}, "Bad", "latest", false)
	if !IsUsage(err) || !strings.Contains(err.Error(), `"Bad"`) {
		t.Fatal(err)
	}
	if _, err := Uninstall(Options{}, "Bad"); !IsUsage(err) {
		t.Fatal(err)
	}
}

// スプールのロックを、無い以外の理由で調べられなければ（ここではスプールの場所がファイル）、動いているかが分からない
// ので、install も uninstall も何も変えずに断る（「動いていない」とみなして付け替えない）。
func TestLockCannotBeRead(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows reports a path below a file as not found")
	}
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	spoolDir := filepath.Join(h.plugins, "cli", spool.SpoolDirName)
	os.RemoveAll(spoolDir)
	os.WriteFile(spoolDir, nil, 0o600)
	h.publish(release{name: "cli", tag: "c2"})
	before := snapshot(t, h.plugins)
	if _, err := h.install("cli", "c2"); err == nil || !strings.Contains(err.Error(), "cannot tell whether Vectorworks is running") {
		t.Fatal(err)
	}
	if _, err := Uninstall(h.options(), "cli"); err == nil || !strings.Contains(err.Error(), "cannot tell whether Vectorworks is running") {
		t.Fatal(err)
	}
	sameSnapshot(t, before, snapshot(t, h.plugins))
}

// --registry の書き方の誤りは、cli の無い <Plug-Ins> でも使い方の誤りにする（cli の確かめより先）。
func TestRegistryUsageBeforeTheCLICheck(t *testing.T) {
	h := newHarness(t)
	o := h.options()
	o.Registries = []string{"/plugins.json"}
	if _, err := Install(o, "elsewhere", "1", false); !IsUsage(err) {
		t.Fatal(err)
	}
}

// cli の無い <Plug-Ins> では、レジストリを取りに行く前に断る（届かないレジストリで、本当の理由が隠れない）。
func TestInstallChecksCLIBeforeTheRegistry(t *testing.T) {
	h := newHarness(t)
	o := h.options()
	o.Registries = []string{"https://127.0.0.1:1/unreachable.json"}
	// 既定のレジストリに無い名前なので、確かめが後なら届かないレジストリを取りに行く。
	if _, err := Install(o, "elsewhere", "1", false); err == nil || !strings.Contains(err.Error(), "has no cli") {
		t.Fatal(err)
	}
}

// 名前がどのレジストリにも無い・規則が合わない・マニフェストが読めないときは、何も変えずに失敗にする。
func TestInstallManifestErrors(t *testing.T) {
	h := newHarness(t)
	// cli でない名前は cli のある <Plug-Ins> でだけ引く（無ければ、レジストリを取る前に断る）。
	h.installCLI()
	before := snapshot(t, h.plugins)
	// 引いたレジストリを示す。
	if _, err := h.install("nowhere", "1"); err == nil || !strings.Contains(err.Error(), "no registry (looked in: default") {
		t.Error(err)
	}
	narrow := fileURL(filepath.Join(h.tmp, "narrow.json"))
	os.WriteFile(filepath.Join(h.tmp, "narrow.json"), []byte(`{"registry_version":1,"plugins":{"other":{"manifests":[{"match":"v*","path":"`+
		h.url("/other/{tag}.json")+`"}]}}}`), 0o644)
	o := h.options()
	o.Registries = []string{narrow}
	if _, err := Install(o, "other", "latest", false); err == nil || !strings.Contains(err.Error(), "no rule") {
		t.Error(err)
	}
	h.serve("/cli/broken/vw2026.json", []byte("{"))
	if _, err := h.install("cli", "broken"); err == nil || !strings.Contains(err.Error(), "manifest") {
		t.Error(err)
	}
	sameSnapshot(t, before, snapshot(t, h.plugins))
}

// <Plug-Ins> がファイルなら、何も変えずに失敗にする。
func TestInstallPluginsDirIsAFile(t *testing.T) {
	h := newHarness(t)
	h.publish(release{name: "cli", tag: "c1"})
	file := filepath.Join(h.tmp, "file")
	os.WriteFile(file, []byte("mine"), 0o644)
	o := h.options()
	o.PluginsDir = file
	if _, err := Install(o, "cli", "c1", false); err == nil {
		t.Fatal("should fail")
	}
	if data, _ := os.ReadFile(file); string(data) != "mine" {
		t.Fatal("changed the file")
	}
}

// <Work> がファイルなら（作業の場所を作れない）、何も変えずに失敗にする。
func TestWorkDirIsAFile(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	h.publish(release{name: "cli", tag: "c2"})
	os.RemoveAll(h.work)
	os.WriteFile(h.work, []byte("mine"), 0o644)
	before := snapshot(t, h.plugins)
	if _, err := h.install("cli", "c2"); err == nil {
		t.Error("install should fail")
	}
	if _, err := Uninstall(h.options(), "cli"); err == nil {
		t.Error("uninstall should fail")
	}
	sameSnapshot(t, before, snapshot(t, h.plugins))
}

// <Work>/old を作れなければ（OS の失敗）、install も uninstall も何も変えずに失敗にする。
func TestCannotMakeTheOldFolder(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	h.publish(release{name: "cli", tag: "c2"})
	failMkdirAll(t, filepath.Join(h.work, "old"))
	before := snapshot(t, h.plugins)
	if _, err := h.install("cli", "c2"); !errors.Is(err, errInjected) {
		t.Error(err)
	}
	if _, err := Uninstall(h.options(), "cli"); !errors.Is(err, errInjected) {
		t.Error(err)
	}
	sameSnapshot(t, before, snapshot(t, h.plugins))
}

// 前回の残りの組み立てを消せなければ（zip の場所が中身のあるフォルダ・フォルダを消せない）、何も変えない。
func TestInstallCannotRemoveTheStaging(t *testing.T) {
	for name, prepare := range map[string]func(h *harness){
		"zip": func(h *harness) {
			os.MkdirAll(filepath.Join(h.work, "staging", "cli.zip", "x"), 0o700)
		},
		"folder": func(h *harness) {
			failRemoveAll(t, filepath.Join(h.work, "staging", "cli"))
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.mustInstall(release{name: "cli", tag: "c1"})
			h.publish(release{name: "cli", tag: "c2"})
			prepare(h)
			before := snapshot(t, h.plugins)
			if _, err := h.install("cli", "c2"); err == nil {
				t.Fatal("should fail")
			}
			sameSnapshot(t, before, snapshot(t, h.plugins))
		})
	}
}

// 走らせている CLI をよけられなければ、付け替えずに失敗にする。
func TestCannotSidestepTheCLI(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	h.publish(release{name: "cli", tag: "c2"})
	failRename(t, resolved(t, h.fakeExecutable()), "")
	before := snapshot(t, h.plugins)
	o := h.options()
	o.Executable = h.fakeExecutable()
	if _, err := Install(o, "cli", "c2", false); err == nil {
		t.Error("install should fail")
	}
	sameSnapshot(t, before, snapshot(t, h.plugins))
	if exist(filepath.Join(h.work, "staging", "cli")) {
		t.Error("left the staging folder")
	}
}

// <Plug-Ins> を作れなければ、退避した前の版を戻す。
func TestInstallCannotMakeThePluginsDir(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	h.publish(release{name: "cli", tag: "c2"})
	failMkdirAll(t, h.plugins)
	before := snapshot(t, h.plugins)
	if _, err := h.install("cli", "c2"); !errors.Is(err, errInjected) {
		t.Fatal(err)
	}
	sameSnapshot(t, before, snapshot(t, h.plugins))
}

// 付け替えの 2 つ目が失敗し、前の版も戻せなければ、手で戻す場所を示す。
func TestInstallCannotPutTheOldVersionBack(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	h.publish(release{name: "cli", tag: "c2"})
	old := filepath.Join(h.work, "old", "cli")
	failRename(t, filepath.Join(h.work, "staging", "cli"), "")
	failRename(t, old, "")
	// 走らせている CLI をよけていれば、その場所も示す（戻した版のフォルダには CLI が無いため）。
	o := h.options()
	o.Executable = h.fakeExecutable()
	if _, err := Install(o, "cli", "c2", false); err == nil || !strings.Contains(err.Error(), "by hand") ||
		!strings.Contains(err.Error(), old) || !strings.Contains(err.Error(), "move the running vw2026 back") {
		t.Fatal(err)
	}
}

// フックが失敗して戻すとき、新しい版をよけられなければ残し、前の版を戻せなければ手で戻す場所を示す。
func TestInstallHookFailureCannotUndo(t *testing.T) {
	for name, c := range map[string]struct {
		move func(h *harness) [2]string
		want string
	}{
		"new version": {func(h *harness) [2]string {
			return [2]string{filepath.Join(h.plugins, "cli"), filepath.Join(h.work, "staging", "cli")}
		}, "cannot be moved away"},
		"old version": {func(h *harness) [2]string {
			return [2]string{filepath.Join(h.work, "old", "cli"), filepath.Join(h.plugins, "cli")}
		}, "cannot be put back"},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.mustInstall(release{name: "cli", tag: "c1"})
			h.publish(release{name: "cli", tag: "c2", install: &hookPlan{Exit: 1}})
			move := c.move(h)
			failRename(t, move[0], move[1])
			o := h.options()
			o.Executable = h.fakeExecutable()
			_, err := Install(o, "cli", "c2", false)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatal(err)
			}
			// 前の版を戻せないときは、よけた CLI の場所も示す。
			if name == "old version" && !strings.Contains(err.Error(), "move the running vw2026 back") {
				t.Fatal(err)
			}
		})
	}
}

// よけた CLI を戻せなければ、どの戻しも、戻したとは言わずに、手で戻す場所を示す（黙って残すと、次の後始末が
// よけた名前のまま唯一の vw2026 を消すため）。
func TestCannotRestoreTheSidesteppedCLI(t *testing.T) {
	for name, c := range map[string]struct {
		fail func(h *harness)
		plan *hookPlan
	}{
		"first rename": {fail: func(h *harness) {
			failRename(t, filepath.Join(h.plugins, "cli"), filepath.Join(h.work, "old", "cli"))
		}},
		"second rename": {fail: func(h *harness) { failRename(t, filepath.Join(h.work, "staging", "cli"), "") }},
		"hook":          {fail: func(h *harness) {}, plan: &hookPlan{Exit: 1}},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.mustInstall(release{name: "cli", tag: "c1"})
			h.publish(release{name: "cli", tag: "c2", install: c.plan})
			c.fail(h)
			executable := filepath.Join(h.plugins, "cli", "bin", "vw2026")
			renameWhen(t, func(from, to string) error {
				if to == executable && strings.HasPrefix(filepath.Base(from), oldCLIName) {
					return errInjected
				}
				return nil
			})
			o := h.options()
			o.Executable = executable
			_, err := Install(o, "cli", "c2", false)
			if err == nil || !strings.Contains(err.Error(), "cannot be moved back") ||
				!strings.Contains(err.Error(), executable) || strings.Contains(err.Error(), "restored") {
				t.Fatal(err)
			}
		})
	}
}

// uninstall の印を書けなければ（印の場所がフォルダ）、何も変えない。
func TestUninstallCannotWriteTheMarker(t *testing.T) {
	h := newHarness(t)
	h.installCLI()
	os.MkdirAll(filepath.Join(h.work, "old", "cli.uninstalling"), 0o700)
	before := snapshot(t, h.plugins)
	if _, err := Uninstall(h.options(), "cli"); err == nil {
		t.Fatal("should fail")
	}
	sameSnapshot(t, before, snapshot(t, h.plugins))
}

// フックが失敗し、フォルダを戻せなければ、手で戻す場所を示す。
func TestUninstallCannotPutTheFolderBack(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1", uninstall: &hookPlan{Exit: 1}})
	old := filepath.Join(h.work, "old", "cli")
	failRename(t, old, "")
	if _, err := Uninstall(h.options(), "cli"); err == nil || !strings.Contains(err.Error(), "by hand") {
		t.Fatal(err)
	}
}

// フックのあとで移したフォルダを組み立ての場所へ移せなければ（Windows で中のファイルが開かれている）、
// 何も消さずに印を残して失敗にし、もう一度走らせると続きから消す。
func TestUninstallCannotDeleteTheFolder(t *testing.T) {
	h := newHarness(t)
	h.installCLI()
	old := filepath.Join(h.work, "old", "cli")
	marker := filepath.Join(h.work, "old", "cli.uninstalling")
	restore := failRename(t, old, filepath.Join(h.work, "staging", "cli"))
	if _, err := Uninstall(h.options(), "cli"); err == nil || !strings.Contains(err.Error(), "run uninstall again") {
		t.Fatal(err)
	}
	if !exist(marker) || !validRecord(old, "cli") || exist(filepath.Join(h.plugins, "cli")) {
		t.Fatal("should keep the marker and the whole of old")
	}
	restore()
	if result, err := Uninstall(h.options(), "cli"); err != nil || result.Outcome != outcomeUninstalled || exist(old) {
		t.Fatalf("%+v %v", result, err)
	}
}

// 組み立ての場所へ移したあとで消すのが途中で止まっても、外したことは変わらず（old にも印にも残らない）、
// 残りは次の uninstall が消す。
func TestUninstallLeavesWhatCannotBeDeletedInStaging(t *testing.T) {
	h := newHarness(t)
	h.installCLI()
	stage := filepath.Join(h.work, "staging", "cli")
	// 移したあと（中身があるとき）だけ失敗させる（移す前の、前回の残りを消す呼び出しは通す）。
	restore := pathOpWhen(t, &removeAll, func(path string) error {
		if path == stage && exist(stage) {
			return errInjected
		}
		return nil
	})
	var stderr bytes.Buffer
	o := h.options()
	o.Stderr = &stderr
	if result, err := Uninstall(o, "cli"); err != nil || result.Outcome != outcomeUninstalled {
		t.Fatalf("%+v %v", result, err)
	}
	if !exist(stage) || exist(filepath.Join(h.work, "old", "cli")) || exist(filepath.Join(h.work, "old", "cli.uninstalling")) ||
		!strings.Contains(stderr.String(), stage) {
		t.Fatalf("should leave only the staging folder and say so: %s", stderr.String())
	}
	restore()
	if result, err := Uninstall(h.options(), "cli"); err != nil || result.Outcome != outcomeNotInstall || exist(stage) {
		t.Fatalf("%+v %v", result, err)
	}
}

// 付け替えで退避したフォルダの記録の module が無ければ（利用者のファイルが入っているかもしれない）、
// 消さずに <Work>/old/<name>.kept-<時刻>/ へ移して知らせる（守ること 9）。old/<name>/ には残さないので、
// 次の install・uninstall も通る。
func TestInstallKeepsAFolderWithoutModule(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	dest := filepath.Join(h.plugins, "cli")
	os.RemoveAll(filepath.Join(dest, "cli.vwlibrary"))
	os.WriteFile(filepath.Join(dest, "mine.txt"), []byte("mine"), 0o644)
	var stderr bytes.Buffer
	o := h.options()
	o.Stderr = &stderr
	if result, err := Install(o, "cli", "c1", false); err != nil || result.Outcome != outcomeInstalled {
		t.Fatalf("%+v %v", result, err)
	}
	kept, _ := filepath.Glob(filepath.Join(h.work, "old", "cli.kept-*"))
	if len(kept) != 1 || exist(filepath.Join(h.work, "old", "cli")) || !strings.Contains(stderr.String(), kept[0]) {
		t.Fatalf("should move the previous folder aside and say so: %v %s", kept, stderr.String())
	}
	if data, _ := os.ReadFile(filepath.Join(kept[0], "mine.txt")); string(data) != "mine" {
		t.Fatal("lost the file")
	}
	h.mustInstall(release{name: "cli", tag: "c2"})
	if result, err := Uninstall(h.options(), "cli"); err != nil || result.Outcome != outcomeUninstalled {
		t.Fatalf("%+v %v", result, err)
	}
	if !exist(filepath.Join(kept[0], "mine.txt")) {
		t.Fatal("removed the kept folder")
	}
}

// 退避したフォルダを脇へ移せなければ、入れたことは変えずに知らせ、old/<name>/ に残す。
func TestInstallCannotMoveAsideAFolderWithoutModule(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	os.RemoveAll(filepath.Join(h.plugins, "cli", "cli.vwlibrary"))
	old := filepath.Join(h.work, "old", "cli")
	failRename(t, old, "")
	var stderr bytes.Buffer
	o := h.options()
	o.Stderr = &stderr
	if result, err := Install(o, "cli", "c1", false); err != nil || result.Outcome != outcomeInstalled {
		t.Fatalf("%+v %v", result, err)
	}
	if !exist(old) || !strings.Contains(stderr.String(), old) {
		t.Fatalf("should leave old and say so: %s", stderr.String())
	}
}

// 初めて入れるときにフックが失敗し、その間に Vectorworks が起動したら、新しい版を残し、前の版の場所は示さない。
func TestFirstInstallHookFailureWhileRunning(t *testing.T) {
	h := newHarness(t)
	signal := filepath.Join(h.tmp, "hook-started")
	await := filepath.Join(h.tmp, "locked")
	h.publish(release{name: "cli", tag: "c1", install: &hookPlan{Exit: 1, Signal: signal, Await: await}})
	done := lockWhenSignaled(h, signal, await)
	_, err := h.install("cli", "c1")
	<-done
	if err == nil || !strings.Contains(err.Error(), "Vectorworks started") || strings.Contains(err.Error(), "previous") {
		t.Fatal(err)
	}
	if h.installedVersion("cli") != "c1" {
		t.Fatal("the new version should stay")
	}
	// 取った zip は残さない（同じタグで走らせ直す install は zip を取らず、消す道を通らない）。
	if exist(filepath.Join(h.work, "staging", "cli.zip")) {
		t.Fatal("should remove the downloaded zip")
	}
}

// 新しい版を残した install のあと、同じタグの install は、フックが通ったら組み立ての場所の残りも消す。
func TestInstallSameTagCleansTheStaging(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	leftovers := []string{filepath.Join(h.work, "staging", "cli", "x"), filepath.Join(h.work, "staging", "cli.zip")}
	for _, path := range leftovers {
		os.MkdirAll(filepath.Dir(path), 0o700)
		os.WriteFile(path, nil, 0o600)
	}
	if result, err := h.install("cli", "c1"); err != nil || result.Outcome != outcomeUpToDate {
		t.Fatalf("%+v %v", result, err)
	}
	for _, path := range leftovers {
		if exist(path) {
			t.Fatal("left", path)
		}
	}
}

// uninstall が空の <Work>/staging を消したあとも、old へ移したフォルダを組み立ての場所を通して消せる
// （discard が <Work>/staging を作る）。uninstall cli と、同じタグの install の片付けの両方。
func TestDiscardWithoutStaging(t *testing.T) {
	h := newHarness(t)
	h.installCLI()
	h.mustInstall(release{name: "demo", tag: "d1"})
	if result, err := Uninstall(h.options(), "demo"); err != nil || result.Outcome != outcomeUninstalled ||
		exist(filepath.Join(h.work, "staging")) {
		t.Fatalf("%+v %v", result, err)
	}
	if result, err := Uninstall(h.options(), "cli"); err != nil || result.Outcome != outcomeUninstalled {
		t.Fatalf("%+v %v", result, err)
	}

	h.installCLI()
	installed := filepath.Join(h.plugins, "cli")
	old := filepath.Join(h.work, "old", "cli")
	os.MkdirAll(filepath.Dir(old), 0o700)
	if err := os.CopyFS(old, os.DirFS(installed)); err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(filepath.Join(h.work, "staging"))
	if result, err := h.install("cli", "c1"); err != nil || result.Outcome != outcomeUpToDate || exist(old) {
		t.Fatalf("%+v %v", result, err)
	}
}

// discard が <Work>/staging を作れなければ、old へ移したフォルダと印を残して失敗にする（走らせ直すと続きから走る）。
func TestDiscardCannotMakeTheStaging(t *testing.T) {
	h := newHarness(t)
	h.installCLI()
	h.mustInstall(release{name: "demo", tag: "d1"})
	staging := filepath.Join(h.work, "staging")
	os.RemoveAll(staging)
	failMkdirAll(t, staging)
	if _, err := Uninstall(h.options(), "demo"); err == nil || !strings.Contains(err.Error(), "run uninstall again") {
		t.Fatal(err)
	}
	if !validRecord(filepath.Join(h.work, "old", "demo"), "demo") || !exist(filepath.Join(h.work, "old", "demo.uninstalling")) {
		t.Fatal("should keep old and the marker")
	}
}

// インタプリタを通さないフックが Windows の走らせない拡張子なら、付け替えの前に断る。
func TestHookNotRunnableOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("only Windows decides by the extension whether a file runs")
	}
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	before := snapshot(t, h.plugins)
	h.publish(release{name: "cli", tag: "c2", hooks: &registry.Hooks{Install: []string{"hooks/install.sh"}},
		extra: []entry{{name: "hooks/install.sh", body: "#!/bin/sh\n", mode: 0o755}}})
	if _, err := h.install("cli", "c2"); err == nil || !strings.Contains(err.Error(), "does not run on Windows") {
		t.Fatal(err)
	}
	sameSnapshot(t, before, snapshot(t, h.plugins))
}

// インタプリタを通さないフックに実行の権限が無ければ（mac）、付け替えの前に断る。
func TestHookWithoutExecutePermission(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not decide by the permission whether a file runs")
	}
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	before := snapshot(t, h.plugins)
	h.publish(release{name: "cli", tag: "c2", hooks: &registry.Hooks{Install: []string{"hooks/setup"}},
		extra: []entry{{name: "hooks/setup", body: "#!/bin/sh\n", mode: 0o644}}})
	if _, err := h.install("cli", "c2"); err == nil || !strings.Contains(err.Error(), "permission to execute") {
		t.Fatal(err)
	}
	sameSnapshot(t, before, snapshot(t, h.plugins))
}

// uninstall のフックが失敗し、その間に Vectorworks が起動したら、フォルダを戻さずに印を残す。
func TestUninstallHookFailureWhileRunning(t *testing.T) {
	h := newHarness(t)
	h.installCLI()
	signal := filepath.Join(h.tmp, "hook-started")
	await := filepath.Join(h.tmp, "locked")
	h.mustInstall(release{name: "demo", tag: "d1", uninstall: &hookPlan{Exit: 1, Signal: signal, Await: await}})
	done := lockWhenSignaled(h, signal, await)
	_, err := Uninstall(h.options(), "demo")
	<-done
	if err == nil || !strings.Contains(err.Error(), "Vectorworks started") {
		t.Fatal(err)
	}
	if exist(filepath.Join(h.plugins, "demo")) || !exist(filepath.Join(h.work, "old", "demo")) ||
		!exist(filepath.Join(h.work, "old", "demo.uninstalling")) {
		t.Fatal("should keep the folder in old with the marker")
	}
}

// lockWhenSignaled は、フックの合図を待ってロックを掴み、フックに知らせる。
func lockWhenSignaled(h *harness, signal, await string) <-chan struct{} {
	return lockAtWhenSignaled(h, filepath.Join(h.plugins, cliName, spool.SpoolDirName), signal, await)
}

// lockAtWhenSignaled は、フックの合図を待ってスプール dir のロックを掴み、フックに知らせる。
func lockAtWhenSignaled(h *harness, dir, signal, await string) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for deadline := time.Now().Add(10 * time.Second); !exist(signal) && time.Now().Before(deadline); {
			time.Sleep(10 * time.Millisecond)
		}
		os.MkdirAll(dir, 0o700)
		fakeplugin.HoldLock(h.t, dir)
		os.WriteFile(await, nil, 0o600)
	}()
	return done
}

// 資産が上限より大きければ、取るのを失敗にして、取ったものを残さない。
func TestDownloadTooLarge(t *testing.T) {
	swap(t, &maxAsset, 4)
	h := newHarness(t)
	h.serve("/big.zip", []byte("0123456789"))
	path := filepath.Join(t.TempDir(), "big.zip")
	var stderr bytes.Buffer
	o := h.options()
	o.Stderr = &stderr
	// 大きさ（Content-Length）が分かれば、書く前に断る（取り始めない）。
	if err := o.download(h.url("/big.zip"), path, sha256Hex([]byte("0123456789"))); err == nil ||
		!strings.Contains(err.Error(), "larger") || exist(path) || strings.Contains(stderr.String(), "downloading") {
		t.Fatal(err, stderr.String())
	}
}

// ./ で始まる項目の zip（zip -r x.zip ./ が作る）も入れられる。
func TestInstallZipWithDotSlash(t *testing.T) {
	h := newHarness(t)
	h.installCLI()
	h.publish(release{name: "demo", tag: "d1", hooks: &registry.Hooks{}, entries: []entry{
		{name: "./", mode: fs.ModeDir | 0o755},
		{name: "./demo.vwlibrary/x", body: "m"},
		{name: "./version.txt", body: "d1"},
	}})
	if result, err := h.install("demo", "d1"); err != nil || result.Outcome != outcomeInstalled || h.installedVersion("demo") != "d1" {
		t.Fatalf("%+v %v", result, err)
	}
}

// よけた CLI の名前は、実行ファイルの名前（大文字の名前・別の名前の写し）に依らず決まり、後始末が消す。
func TestSidestepNameIgnoresTheExecutableName(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1", extra: []entry{{name: "bin/VW2026-dev", body: "copy", mode: 0o755}}})
	h.publish(release{name: "cli", tag: "c2"})
	o := h.options()
	o.Executable = filepath.Join(h.plugins, "cli", "bin", "VW2026-dev")
	var moved string
	renameWhen(t, func(from, to string) error {
		if filepath.Base(from) == "VW2026-dev" {
			moved = to
		}
		return nil
	})
	if _, err := Install(o, "cli", "c2", false); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(filepath.Base(moved), oldCLIName) || exist(moved) {
		t.Fatalf("moved to %s", moved)
	}
}

// 展開した大きさの合計が上限を超える zip は、何も展開せずに断る（小さな zip が大きく展開されないように）。
func TestExtractTooLarge(t *testing.T) {
	swap(t, &maxExtracted, 5)
	dir := t.TempDir()
	path := filepath.Join(dir, "x.zip")
	os.WriteFile(path, makeZip(t, []entry{{name: "a", body: "abc"}, {name: "b", body: "abc"}}), 0o644)
	out := filepath.Join(dir, "out")
	if _, err := extract(path, out); err == nil || !strings.Contains(err.Error(), "expands to more than 5 bytes") {
		t.Fatal(err)
	}
	if exist(out) {
		t.Fatal("extracted something")
	}
	maxExtracted = 6
	if _, err := extract(path, out); err != nil {
		t.Fatal(err)
	}
}

// 項目の数が上限を超える zip は、何も展開せずに断る（空の項目の多い小さな zip が、ファイルを作り続けないように）。
func TestExtractTooManyEntries(t *testing.T) {
	swap(t, &maxEntries, 1)
	dir := t.TempDir()
	path := filepath.Join(dir, "x.zip")
	os.WriteFile(path, makeZip(t, []entry{{name: "a"}, {name: "b"}}), 0o644)
	out := filepath.Join(dir, "out")
	if _, err := extract(path, out); err == nil || !strings.Contains(err.Error(), "more than 1 entries") || exist(out) {
		t.Fatal(err)
	}
	maxEntries = 2
	if _, err := extract(path, out); err != nil {
		t.Fatal(err)
	}
}

// フックのあとで組み立ての場所を空けられなければ（zip の場所が中身のあるフォルダ）、移したフォルダを
// 消さずに印を残して失敗にする。
func TestUninstallCannotEmptyTheStaging(t *testing.T) {
	h := newHarness(t)
	h.installCLI()
	old := filepath.Join(h.work, "old", "cli")
	os.MkdirAll(filepath.Join(h.work, "staging", "cli.zip", "x"), 0o700)
	if _, err := Uninstall(h.options(), "cli"); err == nil || !strings.Contains(err.Error(), "run uninstall again") {
		t.Fatal(err)
	}
	if !validRecord(old, "cli") || !exist(filepath.Join(h.work, "old", "cli.uninstalling")) {
		t.Fatal("should keep old and the marker")
	}
}

// 付け替えの前に前回の残りの old を片付けるとき、組み立ての場所へ移したものを消しきれなければ（Windows で
// ウイルス対策が開いているファイルなど）、組み立てずに失敗にする（前の版のファイルを新しい版に混ぜない）。
func TestInstallDoesNotMixTheLeftoverIntoTheNewVersion(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	h.publish(release{name: "cli", tag: "c2"})
	dest := filepath.Join(h.plugins, "cli")
	old := filepath.Join(h.work, "old", "cli")
	copyDir(t, dest, old)
	os.WriteFile(filepath.Join(old, "stale.txt"), []byte("old"), 0o644)
	stage := filepath.Join(h.work, "staging", "cli")
	// stale.txt だけが消せない（ほかは消える）。
	restore := pathOpWhen(t, &removeAll, func(path string) error {
		if path == stage && exist(filepath.Join(stage, "stale.txt")) {
			entries, _ := os.ReadDir(stage)
			for _, entry := range entries {
				if entry.Name() != "stale.txt" {
					os.RemoveAll(filepath.Join(stage, entry.Name()))
				}
			}
			return errInjected
		}
		return nil
	})
	before := snapshot(t, h.plugins)
	if _, err := h.install("cli", "c2"); !errors.Is(err, errInjected) {
		t.Fatal(err)
	}
	sameSnapshot(t, before, snapshot(t, h.plugins))
	// 消せるようになれば、次の install が残りを消して入れる。
	restore()
	h.mustInstall(release{name: "cli", tag: "c2"})
	if exist(filepath.Join(dest, "stale.txt")) || exist(stage) {
		t.Fatal("the leftover is in the new version or left in staging")
	}
}

// <Plug-Ins>/<name>/ があるなら、中断した uninstall の印は前の回の残りなので、同じタグの道でも、付け替えの
// 前に失敗した回でも消す（残すと、あとの install が付け替えの途中で止まったときに、次の uninstall が唯一
// 残った版を消しうる）。
func TestInstallRemovesAStaleMarkerOnEveryPath(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	marker := filepath.Join(h.work, "old", "cli.uninstalling")
	os.WriteFile(marker, nil, 0o600)
	if result, err := h.install("cli", "c1"); err != nil || result.Outcome != outcomeUpToDate || exist(marker) {
		t.Fatalf("same tag: %+v %v", result, err)
	}
	os.WriteFile(marker, nil, 0o600)
	h.publish(release{name: "cli", tag: "c2", sha: strings.Repeat("0", 64)})
	if _, err := h.install("cli", "c2"); err == nil || exist(marker) {
		t.Fatalf("failed before the swap: %v", err)
	}
}

// uninstall cli では、ロックのある <Plug-Ins>/cli/spool/ ごと old へ移すので、フックが失敗して戻す前には
// 移した先のスプールのロックを見る（mac では、移す直前に読み込んだプラグインのロックがフォルダと一緒に移る）。
func TestUninstallCLIHookFailureWhileLockedInOld(t *testing.T) {
	h := newHarness(t)
	signal := filepath.Join(h.tmp, "hook-started")
	await := filepath.Join(h.tmp, "locked")
	h.mustInstall(release{name: "cli", tag: "c1", uninstall: &hookPlan{Exit: 1, Signal: signal, Await: await}})
	old := filepath.Join(h.work, "old", "cli")
	done := lockAtWhenSignaled(h, filepath.Join(old, spool.SpoolDirName), signal, await)
	_, err := Uninstall(h.options(), "cli")
	<-done
	if err == nil || !strings.Contains(err.Error(), "Vectorworks started") {
		t.Fatal(err)
	}
	if exist(filepath.Join(h.plugins, "cli")) || !exist(old) || !exist(filepath.Join(h.work, "old", "cli.uninstalling")) {
		t.Fatal("should keep the folder in old with the marker")
	}
}

// 大きさの分からない応答（Content-Length が無い）が上限を超えたら、取り終えたとは書かずに、止まったと書く。
func TestDownloadTooLargeSaysStopped(t *testing.T) {
	swap(t, &maxAsset, 4)
	h := newHarness(t)
	h.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.(http.Flusher).Flush()
		w.Write([]byte("0123456789"))
	})
	var stderr bytes.Buffer
	o := h.options()
	o.Stderr = &stderr
	if err := o.download(h.url("/big.zip"), filepath.Join(t.TempDir(), "big.zip"), ""); err == nil {
		t.Fatal("should fail")
	}
	if got := stderr.String(); strings.Contains(got, "downloaded") || !strings.Contains(got, "stopped after") {
		t.Fatalf("%q", got)
	}
}

// モジュールは、拡張子だけでなく、mac はフォルダ（バンドル）、Windows はファイルであることも確かめる。
func TestFindModuleChecksTheKind(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "file.vwlibrary"), nil, 0o644)
	os.Mkdir(filepath.Join(dir, "folder.vwlibrary"), 0o755)
	os.WriteFile(filepath.Join(dir, "file.vlb"), nil, 0o644)
	os.Mkdir(filepath.Join(dir, "folder.vlb"), 0o755)
	for _, c := range []struct {
		name, target string
		ok           bool
	}{
		{"folder.vwlibrary", "macos-arm64", true},
		{"file.vwlibrary", "macos-arm64", false},
		{"file.vlb", "windows-x64", true},
		{"folder.vlb", "windows-x64", false},
	} {
		got, err := findModule(dir, []string{c.name}, c.target)
		if c.ok && (err != nil || got != c.name) || !c.ok && (err == nil || !strings.Contains(err.Error(), "is not a")) {
			t.Errorf("%s for %s: %q %v", c.name, c.target, got, err)
		}
	}
}

// フックの標準出力は上限（maxDocument）までだけを溜め、超えたら失敗にする（フックは最後まで走らせる）。
func TestHookPrintsTooMuch(t *testing.T) {
	h := newHarness(t)
	h.publish(release{name: "cli", tag: "c1", install: &hookPlan{Stdout: `{"x":"` + strings.Repeat("a", maxDocument) + `"}`}})
	if _, err := h.install("cli", "c1"); err == nil || !strings.Contains(err.Error(), "more than") {
		t.Fatal(err)
	}
	if exist(filepath.Join(h.plugins, "cli")) {
		t.Fatal("should undo the install")
	}
}

// フックのインタプリタがこの OS で走らなければ、付け替える前に断る（<Plug-Ins> を変えない）。
func TestHookInterpreterOfAnotherOSBeforeTheSwap(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	other := "powershell"
	if runtime.GOOS == "windows" {
		other = "sh"
	}
	hooks := registry.Hooks{Install: []string{other, "hooks/install.hook"}}
	h.publish(release{name: "cli", tag: "c2", hooks: &hooks})
	before := snapshot(t, h.plugins)
	// 付け替え（<Plug-Ins>/cli の名前の変更）まで進まないことを確かめる。
	dest := filepath.Join(h.plugins, "cli")
	swapped := false
	renameWhen(t, func(oldpath, newpath string) error {
		if oldpath == dest {
			swapped = true
		}
		return nil
	})
	o := h.options()
	o.Interpreters = nil
	if _, err := Install(o, "cli", "c2", false); err == nil || !strings.Contains(err.Error(), other) || swapped {
		t.Fatal(err, swapped)
	}
	sameSnapshot(t, before, snapshot(t, h.plugins))
}

// 続きから走る uninstall cli は、移した先のスプールのロックが掴まれていれば何もせずに ErrRunning で終わる
// （<Plug-Ins>/cli/spool/ はもう無いので、始めの確かめでは見えない）。
func TestUninstallCLIResumeWhileLockedInOld(t *testing.T) {
	h := newHarness(t)
	h.installCLI()
	old := filepath.Join(h.work, "old", "cli")
	marker := filepath.Join(h.work, "old", "cli.uninstalling")
	os.Rename(filepath.Join(h.plugins, "cli"), old)
	os.WriteFile(marker, nil, 0o600)
	dir := filepath.Join(old, spool.SpoolDirName)
	os.MkdirAll(dir, 0o700)
	fakeplugin.HoldLock(t, dir)
	result, err := Uninstall(h.options(), "cli")
	if !errors.Is(err, ErrRunning) || result.Outcome != outcomeRunning || !exist(old) || !exist(marker) {
		t.Fatalf("%+v %v", result, err)
	}
	if runs := h.hookRuns("cli"); len(runs) != 1 {
		t.Fatalf("should not run the uninstall hook: %+v", runs)
	}
}

// フックが通っても、その間に移した先のスプールのロックが掴まれたら（cli）、消さずに印を残して失敗する（フォルダを移して
// フックも走らせたので、「何もしなかった」の ErrRunning にはしない）。
func TestUninstallCLIHookSuccessWhileLockedInOld(t *testing.T) {
	h := newHarness(t)
	signal := filepath.Join(h.tmp, "hook-started")
	await := filepath.Join(h.tmp, "locked")
	h.mustInstall(release{name: "cli", tag: "c1", uninstall: &hookPlan{Stdout: "{}", Signal: signal, Await: await}})
	old := filepath.Join(h.work, "old", "cli")
	done := lockAtWhenSignaled(h, filepath.Join(old, spool.SpoolDirName), signal, await)
	result, err := Uninstall(h.options(), "cli")
	<-done
	if err == nil || errors.Is(err, ErrRunning) || result != nil || !strings.Contains(err.Error(), old) {
		t.Fatalf("%+v %v", result, err)
	}
	if !validRecord(old, "cli") || !exist(filepath.Join(h.work, "old", "cli.uninstalling")) {
		t.Fatal("should keep old and the marker")
	}
}

// sha256 が合わず、取った zip を消せなくても失敗で終わり、次の install が組み立ての場所の残りとして消す。
func TestDownloadMismatchCannotRemove(t *testing.T) {
	h := newHarness(t)
	h.publish(release{name: "cli", tag: "c1", sha: strings.Repeat("0", 64)})
	zip := filepath.Join(h.work, "staging", "cli.zip")
	// 組み立ての前の片付け（まだ無い zip を消す）は通し、取ったあとの zip だけを消せなくする。
	restore := pathOpWhen(t, &removeFile, func(path string) error {
		if path == zip && exist(path) {
			return errInjected
		}
		return nil
	})
	var stderr bytes.Buffer
	o := h.options()
	o.Stderr = &stderr
	// 取り終えた行は、sha256 を照らしてから書く（合わなければ取り終えたとは書かない）。
	if _, err := Install(o, "cli", "c1", false); err == nil || !strings.Contains(err.Error(), "sha256") || !exist(zip) ||
		strings.Contains(stderr.String(), "downloaded") || !strings.Contains(stderr.String(), "stopped after") {
		t.Fatal(err, stderr.String())
	}
	restore()
	h.mustInstall(release{name: "cli", tag: "c1"})
	if exist(zip) {
		t.Fatal("left the zip")
	}
}

// cli の install のフックの間に、前の版を読み込んだ Vectorworks のロックが old/cli/spool/ にあれば（付け替えの
// 直前に起動した）、フックが失敗しても戻さずに新しい版を残す。
func TestInstallCLIHookFailureWhileLockedInOld(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	signal := filepath.Join(h.tmp, "hook-started")
	await := filepath.Join(h.tmp, "locked")
	h.publish(release{name: "cli", tag: "c2", install: &hookPlan{Exit: 1, Signal: signal, Await: await}})
	old := filepath.Join(h.work, "old", "cli")
	done := lockAtWhenSignaled(h, filepath.Join(old, spool.SpoolDirName), signal, await)
	_, err := h.install("cli", "c2")
	<-done
	if err == nil || !strings.Contains(err.Error(), "Vectorworks started") {
		t.Fatal(err)
	}
	if h.installedVersion("cli") != "c2" || !validRecord(old, "cli") {
		t.Fatal("should keep the new version and the previous one in old")
	}
}

// フックが通っても、old/cli/spool/ のロックが掴まれていれば、退避した版を消さずに知らせる。そのあとは、同じタグの
// install も、ほかのプラグインの install・uninstall も、Vectorworks が動いているとみなして何もしない（終了してから
// 走らせ直すと片付ける）。
func TestInstallCLIKeepsOldWhileLockedInOld(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	signal := filepath.Join(h.tmp, "hook-started")
	await := filepath.Join(h.tmp, "locked")
	h.publish(release{name: "cli", tag: "c2", install: &hookPlan{Stdout: "{}", Signal: signal, Await: await}})
	old := filepath.Join(h.work, "old", "cli")
	done := lockAtWhenSignaled(h, filepath.Join(old, spool.SpoolDirName), signal, await)
	var stderr bytes.Buffer
	o := h.options()
	o.Stderr = &stderr
	result, err := Install(o, "cli", "c2", false)
	<-done
	if err != nil || result.Outcome != outcomeInstalled || !validRecord(old, "cli") ||
		!strings.Contains(stderr.String(), "Vectorworks is running") {
		t.Fatalf("%+v %v %s", result, err, stderr.String())
	}
	before := snapshot(t, h.plugins)
	h.publish(release{name: "cli", tag: "c2"})
	if result, err := Install(o, "cli", "c2", false); !errors.Is(err, ErrRunning) || result.Outcome != outcomeRunning {
		t.Fatalf("%+v %v", result, err)
	}
	h.publish(release{name: "demo", tag: "d1"})
	if result, err := Install(o, "demo", "d1", false); !errors.Is(err, ErrRunning) || result.Outcome != outcomeRunning {
		t.Fatalf("%+v %v", result, err)
	}
	if result, err := Uninstall(o, "cli"); !errors.Is(err, ErrRunning) || result.Outcome != outcomeRunning {
		t.Fatalf("%+v %v", result, err)
	}
	sameSnapshot(t, before, snapshot(t, h.plugins))
	if !validRecord(old, "cli") {
		t.Fatal("should keep old")
	}
}

// <Plug-Ins>/<name> を、無い以外の理由で調べられなければ（ここでは <Plug-Ins> がファイル）、uninstall は入って
// いないとみなさずに失敗にし、何も消さない（印も残す）。
func TestUninstallCannotLookAtTheFolder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows reports a path below a file as not found")
	}
	h := newHarness(t)
	plugins := filepath.Join(h.tmp, "file")
	os.WriteFile(plugins, nil, 0o600)
	marker := filepath.Join(h.work, "old", "cli.uninstalling")
	os.MkdirAll(filepath.Dir(marker), 0o700)
	os.WriteFile(marker, nil, 0o600)
	o := h.options()
	o.PluginsDir = plugins
	if result, err := Uninstall(o, "cli"); err == nil || result != nil || !exist(marker) {
		t.Fatalf("%+v %v", result, err)
	}
}

// フォルダを調べられなければ（無い以外の誤り）、入っていないとみなさずに誤りを返す（ロックの確かめが先に断るので、
// 直に確かめる）。
func TestPluginFolderCannotTell(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows reports a path below a file as not found")
	}
	file := filepath.Join(t.TempDir(), "file")
	os.WriteFile(file, nil, 0o600)
	if found, err := pluginFolder(filepath.Join(file, "cli")); found || err == nil {
		t.Fatal(found, err)
	}
}

// 記録の先頭の BOM は許す（BOM を書くエディタで保存した記録も読む）。
func TestRecordWithBOM(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	path := filepath.Join(h.plugins, "cli", RecordFile)
	data, _ := os.ReadFile(path)
	os.WriteFile(path, append([]byte("\xef\xbb\xbf"), data...), 0o644)
	if result, err := h.install("cli", "c1"); err != nil || result.Outcome != outcomeUpToDate {
		t.Fatalf("%+v %v", result, err)
	}
}

// keepNewVersion は、c1 を入れてから c2 を残した状態（c1 は old/cli）にする（keepVersion）。
func keepNewVersion(t *testing.T, h *harness) {
	t.Helper()
	h.mustInstall(release{name: "cli", tag: "c1"})
	keepVersion(t, h, "c2")
}

// keepVersion は、tag の install のフックの間に Vectorworks を起動させ（ロックを掴み）、tag を残した状態にする。
// 終わったらロックを放す（Vectorworks を終了した）。残した版のフックは、記録を書き足す通るものにしておく。
func keepVersion(t *testing.T, h *harness, tag string) {
	t.Helper()
	signal := filepath.Join(h.tmp, "hook-started")
	await := filepath.Join(h.tmp, "locked")
	h.publish(release{name: "cli", tag: tag, install: &hookPlan{Exit: 1, Signal: signal, Await: await}})
	spoolDir := filepath.Join(h.plugins, "cli", spool.SpoolDirName)
	var unlock func()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for deadline := time.Now().Add(10 * time.Second); !exist(signal) && time.Now().Before(deadline); {
			time.Sleep(10 * time.Millisecond)
		}
		os.MkdirAll(spoolDir, 0o700)
		unlock = fakeplugin.HoldLock(h.t, spoolDir)
		os.WriteFile(await, nil, 0o600)
	}()
	_, err := h.install("cli", tag)
	<-done
	if err == nil || !strings.Contains(err.Error(), "Vectorworks started") || h.installedVersion("cli") != tag {
		t.Fatal(err)
	}
	if record, err := readRecord(filepath.Join(h.plugins, "cli")); err != nil || !record.HookPending {
		t.Fatal("the kept version should be marked as hook_pending:", record, err)
	}
	unlock()
	hook := filepath.Join(h.plugins, "cli", "hooks", "install.hook")
	os.WriteFile(hook, []byte(planJSON(t, hookPlan{Stdout: "{}", Record: h.runs("cli")})), 0o644)
}

// 新しい版を残した install を同じタグで終える回は、old に残った前の版のタグを前のタグとしてフックに渡す。
func TestFinishingInstallPassesThePreviousTag(t *testing.T) {
	h := newHarness(t)
	keepNewVersion(t, h)
	if result, err := h.install("cli", "c2"); err != nil || result.Outcome != outcomeUpToDate {
		t.Fatalf("%+v %v", result, err)
	}
	runs := h.hookRuns("cli")
	last := runs[len(runs)-1]
	if last.Env["VW2026_HOOK_PREVIOUS_TAG"] != "c1" || last.Env["VW2026_HOOK_TAG"] != "c2" {
		t.Fatalf("%+v", last.Env)
	}
	if exist(filepath.Join(h.work, "old", "cli")) {
		t.Fatal("left the previous version")
	}
	if record, err := readRecord(filepath.Join(h.plugins, "cli")); err != nil || record.HookPending {
		t.Fatal("the hook succeeded, so hook_pending should be gone:", record, err)
	}
}

// フックが通ったあとに hook_pending を外せなくても、入れたことは変わらず、知らせるだけにする。
func TestCannotClearHookPending(t *testing.T) {
	h := newHarness(t)
	keepNewVersion(t, h)
	// 記録は隣に書いてから置き換えるので、隣のファイルを書けなくする。
	record := filepath.Join(h.plugins, "cli", RecordFile) + ".new-"
	restore := writeFileWhen(t, func(path string) error {
		if strings.HasPrefix(path, record) {
			return errInjected
		}
		return nil
	})
	var stderr bytes.Buffer
	o := h.options()
	o.Stderr = &stderr
	if result, err := Install(o, "cli", "c2", false); err != nil || result.Outcome != outcomeUpToDate ||
		!strings.Contains(stderr.String(), "hook succeeded") {
		t.Fatalf("%+v %v %s", result, err, stderr.String())
	}
	// 記録は壊さない（書き換えが途中で止まっても、元の記録が残る）。記録はまだ通っていないと言うので、退避した
	// 版も片付けない（前のタグを失わない）。次の同じタグの install が外して片付ける。
	if got, err := readRecord(filepath.Join(h.plugins, "cli")); err != nil || !got.HookPending {
		t.Fatal("the record should stay as it was:", got, err)
	}
	old := filepath.Join(h.work, "old", "cli")
	if !validRecord(old, "cli") {
		t.Fatal("removed the previous version while the record still says hook_pending")
	}
	restore()
	if result, err := Install(o, "cli", "c2", false); err != nil || result.Outcome != outcomeUpToDate || exist(old) {
		t.Fatalf("%+v %v", result, err)
	}
}

// フックが記録を壊しても（ここでは記録に書き足す）、hook_pending を外せないと知らせるだけで、入れたことは変わらない。
func TestHookBreaksTheRecord(t *testing.T) {
	h := newHarness(t)
	record := filepath.Join(h.plugins, "cli", RecordFile)
	h.publish(release{name: "cli", tag: "c1", install: &hookPlan{Stdout: "{}", Record: record}})
	var stderr bytes.Buffer
	o := h.options()
	o.Stderr = &stderr
	if result, err := Install(o, "cli", "c1", false); err != nil || result.Outcome != outcomeInstalled ||
		!strings.Contains(stderr.String(), "hook succeeded") {
		t.Fatalf("%+v %v %s", result, err, stderr.String())
	}
}

// フックの通っていない版の module が欠けると（隔離された）、同じタグでも入れ直しになる。付け替えの前の片付けが
// old（フックの通った最後の版）を消さないよう、何も変えずに断り、前の版を戻すか消すかを示す（install と
// uninstall が互いを指し合わない）。示したとおり前の版を戻せば、ふつうに入れ直せる。
func TestReinstallingAKeptVersionWithoutItsModule(t *testing.T) {
	h := newHarness(t)
	keepNewVersion(t, h)
	dest := filepath.Join(h.plugins, "cli")
	old := filepath.Join(h.work, "old", "cli")
	os.RemoveAll(filepath.Join(dest, "cli.vwlibrary"))
	h.publish(release{name: "cli", tag: "c2"})
	before := snapshot(t, h.plugins)
	if _, err := h.install("cli", "c2"); err == nil || !strings.Contains(err.Error(), "move it back there") ||
		!strings.Contains(err.Error(), old) {
		t.Fatal(err)
	}
	sameSnapshot(t, before, snapshot(t, h.plugins))
	if !validRecord(old, "cli") {
		t.Fatal("removed the previous version")
	}
	os.RemoveAll(dest)
	os.Rename(old, dest)
	if result, err := h.install("cli", "c2"); err != nil || result.Outcome != outcomeInstalled {
		t.Fatalf("%+v %v", result, err)
	}
	runs := h.hookRuns("cli")
	if last := runs[len(runs)-1]; last.Env["VW2026_HOOK_PREVIOUS_TAG"] != "c1" || last.Env["VW2026_HOOK_TAG"] != "c2" {
		t.Fatalf("%+v", last.Env)
	}
}

// 新しい版を残すとき、よけた CLI は前の版のフォルダ（old）の元の場所へ戻す（前の版を手で戻しても CLI が欠けず、
// 後始末がよけた名前のまま消さない）。
func TestKeepPutsTheMovedCLIIntoThePreviousVersion(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	signal := filepath.Join(h.tmp, "hook-started")
	await := filepath.Join(h.tmp, "locked")
	h.publish(release{name: "cli", tag: "c2", install: &hookPlan{Exit: 1, Signal: signal, Await: await}})
	done := lockWhenSignaled(h, signal, await)
	o := h.options()
	o.Executable = h.fakeExecutable()
	_, err := Install(o, "cli", "c2", false)
	<-done
	if err == nil || !strings.Contains(err.Error(), "Vectorworks started") {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(h.work, "old", "cli", "bin", "vw2026")); err != nil || string(data) != "cli c1" {
		t.Fatal("the previous version should have its vw2026 back:", string(data), err)
	}
	if moved, _ := filepath.Glob(filepath.Join(h.work, "old", oldCLIName+"*")); len(moved) != 0 {
		t.Fatal(moved)
	}
}

// よけた CLI を前の版のフォルダへ戻せなければ、その場所を知らせる。
func TestKeepCannotPutTheMovedCLIBack(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	signal := filepath.Join(h.tmp, "hook-started")
	await := filepath.Join(h.tmp, "locked")
	h.publish(release{name: "cli", tag: "c2", install: &hookPlan{Exit: 1, Signal: signal, Await: await}})
	target := filepath.Join(h.work, "old", "cli", "bin", "vw2026")
	renameWhen(t, func(from, to string) error {
		if to == target {
			return errInjected
		}
		return nil
	})
	done := lockWhenSignaled(h, signal, await)
	o := h.options()
	o.Executable = h.fakeExecutable()
	_, err := Install(o, "cli", "c2", false)
	<-done
	if err == nil || !strings.Contains(err.Error(), "the vw2026 of the previous version is in") ||
		!strings.Contains(err.Error(), target) {
		t.Fatal(err)
	}
}

// 入っている版のフックがまだ通っていなければ、--check は同じタグでも available（入れ終えていない）。
func TestCheckAKeptVersion(t *testing.T) {
	h := newHarness(t)
	keepNewVersion(t, h)
	if result, err := Install(h.options(), "cli", "c2", true); err != nil || result.Outcome != outcomeAvailable {
		t.Fatalf("%+v %v", result, err)
	}
	if result, err := h.install("cli", "c2"); err != nil || result.Outcome != outcomeUpToDate {
		t.Fatalf("%+v %v", result, err)
	}
	if result, err := Install(h.options(), "cli", "c2", true); err != nil || result.Outcome != outcomeUpToDate {
		t.Fatalf("after finishing: %+v %v", result, err)
	}
}

// 最初に入れた版を残したあと、同じタグで終える回は、前のタグを空にする（最初に入れるのと同じ）。
func TestFinishingFirstInstallPassesNoPreviousTag(t *testing.T) {
	h := newHarness(t)
	keepVersion(t, h, "c1")
	if result, err := h.install("cli", "c1"); err != nil || result.Outcome != outcomeUpToDate {
		t.Fatalf("%+v %v", result, err)
	}
	runs := h.hookRuns("cli")
	if last := runs[len(runs)-1]; last.Env["VW2026_HOOK_PREVIOUS_TAG"] != "" || last.Env["VW2026_HOOK_TAG"] != "c1" {
		t.Fatalf("%+v", last.Env)
	}
}

// フックは通ったが、退避した版を片付けられずに old に残ったときは、入っている版が入れ終えているので、次の
// install は入っているタグを前のタグにする（old の残りから推し量らない）。
func TestLeftoverOldAfterASuccessfulHookIsNotThePreviousTag(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	h.publish(release{name: "cli", tag: "c2"})
	old := filepath.Join(h.work, "old", "cli")
	restore := renameWhen(t, func(from, to string) error {
		if from == old && to == filepath.Join(h.work, "staging", "cli") {
			return errInjected
		}
		return nil
	})
	result, err := h.install("cli", "c2")
	restore()
	if err != nil || result.Outcome != outcomeInstalled || !validRecord(old, "cli") {
		t.Fatalf("%+v %v", result, err)
	}
	if result, err := h.install("cli", "c2"); err != nil || result.Outcome != outcomeUpToDate {
		t.Fatalf("%+v %v", result, err)
	}
	runs := h.hookRuns("cli")
	if last := runs[len(runs)-1]; last.Env["VW2026_HOOK_PREVIOUS_TAG"] != "c2" {
		t.Fatalf("%+v", last.Env)
	}
}

// フックの通っていない版を残し、前の版が old にあるときは、別のタグへ更新しない（old を消してから新しい版の
// フックが失敗すると、通っていない版だけが残るため）。同じタグで入れ終えれば更新でき、前のタグは入れ終えた版。
func TestUpdatingAKeptVersionNeedsFinishing(t *testing.T) {
	h := newHarness(t)
	keepNewVersion(t, h)
	h.publish(release{name: "cli", tag: "c3"})
	before := snapshot(t, h.plugins)
	if _, err := h.install("cli", "c3"); err == nil || !strings.Contains(err.Error(), "has not finished installing") {
		t.Fatal(err)
	}
	sameSnapshot(t, before, snapshot(t, h.plugins))
	if !validRecord(filepath.Join(h.work, "old", "cli"), "cli") {
		t.Fatal("removed the previous version")
	}
	// --check は断らず、新しいビルドがあると答える（help に、入れ終えるまで install が断ると書く）。
	if result, err := Install(h.options(), "cli", "c3", true); err != nil || result.Outcome != outcomeAvailable {
		t.Fatalf("--check: %+v %v", result, err)
	}
	// Vectorworks が動いていれば、断る理由より先に vectorworks_running と答える（終了コード 6）。
	release := h.holdLock()
	if result, err := h.install("cli", "c3"); !errors.Is(err, ErrRunning) || result.Outcome != outcomeRunning {
		t.Fatalf("running: %+v %v", result, err)
	}
	release()
	sameSnapshot(t, before, snapshot(t, h.plugins))
	if result, err := h.install("cli", "c2"); err != nil || result.Outcome != outcomeUpToDate {
		t.Fatalf("%+v %v", result, err)
	}
	if result, err := h.install("cli", "c3"); err != nil || result.Outcome != outcomeInstalled {
		t.Fatalf("%+v %v", result, err)
	}
	runs := h.hookRuns("cli")
	if last := runs[len(runs)-1]; last.Env["VW2026_HOOK_PREVIOUS_TAG"] != "c2" || last.Env["VW2026_HOOK_TAG"] != "c3" {
		t.Fatalf("%+v", last.Env)
	}
}

// 同じタグでフックだけを走らせた回も、前の install がよけた CLI を消してみる。
func TestInstallSameTagCleansMovedCLIs(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	moved := filepath.Join(h.work, "old", oldCLIName+"20260101T000000.000000000")
	os.WriteFile(moved, nil, 0o700)
	if result, err := h.install("cli", "c1"); err != nil || result.Outcome != outcomeUpToDate || exist(moved) {
		t.Fatalf("%+v %v", result, err)
	}
}

// フックが後ろに子を残して終わっても（標準出力を持ち続ける）、待たずに入れ終え、そう知らせる。
func TestHookLeavesAProcess(t *testing.T) {
	swap(t, &hookWaitDelay, 100*time.Millisecond)
	h := newHarness(t)
	// 子は releaseFile ができるまで残る。テストの片付け（一時ディレクトリを消す）の前に終わらせる（Windows では
	// 動いているテストの実行ファイルと、その作業ディレクトリを消せない）。
	releaseFile := filepath.Join(h.tmp, "release")
	t.Cleanup(func() {
		os.WriteFile(releaseFile, nil, 0o600)
		for deadline := time.Now().Add(10 * time.Second); !exist(releaseFile+".done") && time.Now().Before(deadline); {
			time.Sleep(10 * time.Millisecond)
		}
		time.Sleep(500 * time.Millisecond)
	})
	h.publish(release{name: "cli", tag: "c1", install: &hookPlan{Stdout: `{"ran":"install"}`, Linger: releaseFile}})
	var stderr bytes.Buffer
	o := h.options()
	o.Stderr = &stderr
	start := time.Now()
	result, err := Install(o, "cli", "c1", false)
	if err != nil || result.Outcome != outcomeInstalled || string(result.Hook) != `{"ran":"install"}` {
		t.Fatalf("%+v %v", result, err)
	}
	if elapsed := time.Since(start); elapsed > 30*time.Second || !strings.Contains(stderr.String(), "stopped waiting") {
		t.Fatalf("%v %s", elapsed, stderr.String())
	}
}

// Vectorworks が動いていれば、レジストリを取る前に vectorworks_running と答える（届かないレジストリの誤りより先）。
func TestRunningBeforeTheRegistry(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	h.holdLock()
	o := h.options()
	o.Registries = []string{"https://127.0.0.1:1/unreachable.json"}
	if result, err := Install(o, "elsewhere", "1", false); !errors.Is(err, ErrRunning) || result.Outcome != outcomeRunning {
		t.Fatalf("%+v %v", result, err)
	}
}

// <Plug-Ins> と <Work> の片方がもう片方の中にあれば、何もせずに断る（片付けが入れたばかりのプラグインを消さない）。
func TestOverlappingPlaces(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	h.publish(release{name: "cli", tag: "c2"})
	for _, c := range []struct{ plugins, work string }{
		{filepath.Join(h.work, "old"), h.work},
		{h.plugins, h.plugins},
		{h.plugins, filepath.Join(h.plugins, "work")},
	} {
		o := h.options()
		o.PluginsDir, o.WorkDir = c.plugins, c.work
		before := snapshot(t, h.plugins)
		if _, err := Install(o, "cli", "c2", false); err == nil || !strings.Contains(err.Error(), "overlap") {
			t.Errorf("install %+v: %v", c, err)
		}
		if _, err := Uninstall(o, "cli"); err == nil || !strings.Contains(err.Error(), "overlap") {
			t.Errorf("uninstall %+v: %v", c, err)
		}
		sameSnapshot(t, before, snapshot(t, h.plugins))
	}
	// 大文字と小文字を区別しない OS では、その違いだけの書き方も重なる。区別する OS では別のフォルダ（uninstall で
	// 確かめる。入っていなければ何も作らない）。
	o := h.options()
	o.PluginsDir = strings.ToUpper(filepath.Join(h.work, "old"))
	_, err := Uninstall(o, "cli")
	if folds := runtime.GOOS == "darwin" || runtime.GOOS == "windows"; folds != (err != nil && strings.Contains(err.Error(), "overlap")) {
		t.Errorf("case: %v", err)
	}
	// リンクで別の書き方をしても、同じディレクトリ・その中なら断る（<Work> の下を指すリンクを --plugins-dir に
	// 渡したときも。まだ無い場所の下も、あるところまでのリンクを解いて見つける）。
	link := filepath.Join(h.tmp, "plugins-link")
	staging := filepath.Join(h.tmp, "staging-link")
	os.MkdirAll(filepath.Join(h.work, "staging"), 0o700)
	if os.Symlink(h.plugins, link) == nil && os.Symlink(filepath.Join(h.work, "staging"), staging) == nil {
		for _, c := range []struct{ plugins, work string }{
			{h.plugins, link},
			{h.plugins, filepath.Join(link, "work")},
			{h.plugins, filepath.Join(link, "missing", "work")},
			{staging, h.work},
			{filepath.Join(staging, "missing"), h.work},
		} {
			o := h.options()
			o.PluginsDir, o.WorkDir = c.plugins, c.work
			if _, err := Install(o, "cli", "c2", false); err == nil || !strings.Contains(err.Error(), "overlap") {
				t.Errorf("link %+v: %v", c, err)
			}
		}
	}
}

// 同じタグでも、マニフェストを取る間に起動した Vectorworks の下ではフックを走らせない（始めの確かめのあとで、
// もう一度見る）。
func TestInstallSameTagLockTakenWhileFetching(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1", latest: true})
	h.onGet("/cli/latest/vw2026.json", func() { h.holdLock() })
	runs := len(h.hookRuns("cli"))
	before := snapshot(t, h.plugins)
	if result, err := h.install("cli", "latest"); !errors.Is(err, ErrRunning) || result.Outcome != outcomeRunning {
		t.Fatalf("%+v %v", result, err)
	}
	if len(h.hookRuns("cli")) != runs {
		t.Fatal("ran the hook while running")
	}
	sameSnapshot(t, before, snapshot(t, h.plugins))
}

// 同じタグで、マニフェストを取る間にロックを調べられなくなれば、フックを走らせずに失敗にする（終了コード 1）。
func TestInstallSameTagLockUnreadableWhileFetching(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows reports a path below a file as not found")
	}
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1", latest: true})
	spoolDir := filepath.Join(h.plugins, "cli", spool.SpoolDirName)
	h.onGet("/cli/latest/vw2026.json", func() {
		os.RemoveAll(spoolDir)
		os.WriteFile(spoolDir, nil, 0o600)
	})
	runs := len(h.hookRuns("cli"))
	if _, err := h.install("cli", "latest"); err == nil || errors.Is(err, ErrRunning) ||
		!strings.Contains(err.Error(), "cannot tell whether Vectorworks is running") {
		t.Fatal(err)
	}
	if len(h.hookRuns("cli")) != runs {
		t.Fatal("ran the hook")
	}
}

// フックが通ったのに hook_pending を外せず前の版を残すときは、よけた CLI を前の版のフォルダへ戻す（後始末が、
// 前の版の vw2026 をよけた名前のまま消さない）。
func TestCannotClearHookPendingPutsTheMovedCLIBack(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	h.publish(release{name: "cli", tag: "c2"})
	record := filepath.Join(h.plugins, "cli", RecordFile) + ".new-"
	writeFileWhen(t, func(path string) error {
		if strings.HasPrefix(path, record) {
			return errInjected
		}
		return nil
	})
	var stderr bytes.Buffer
	o := h.options()
	o.Stderr = &stderr
	o.Executable = h.fakeExecutable()
	if result, err := Install(o, "cli", "c2", false); err != nil || result.Outcome != outcomeInstalled ||
		!strings.Contains(stderr.String(), "hook succeeded") {
		t.Fatalf("%+v %v %s", result, err, stderr.String())
	}
	if data, err := os.ReadFile(filepath.Join(h.work, "old", "cli", "bin", "vw2026")); err != nil || string(data) != "cli c1" {
		t.Fatal("the previous version should have its vw2026 back:", string(data), err)
	}
}

// Windows のジャンクション（Go は ModeDir と ModeIrregular を返す）も、vw2026 が入れたフォルダとみなさない（リンクの
// 先の作業用のコピーに触れない）。
func TestPluginFolderRefusesAJunction(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("junctions are a Windows feature")
	}
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	target := filepath.Join(h.tmp, "working-copy")
	os.MkdirAll(target, 0o755)
	link := filepath.Join(h.plugins, "demo")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Skip("cannot make a junction:", string(out), err)
	}
	if found, err := pluginFolder(link); found || err == nil || !strings.Contains(err.Error(), "not a plain folder") {
		t.Fatal(found, err)
	}
}

// 前の版を残し、よけた CLI をその版へ戻せなければ、知らせた場所のまま消さない（後始末が消すと、知らせた場所に無い）。
func TestCannotClearHookPendingKeepsTheMovedCLI(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	h.publish(release{name: "cli", tag: "c2"})
	record := filepath.Join(h.plugins, "cli", RecordFile) + ".new-"
	writeFileWhen(t, func(path string) error {
		if strings.HasPrefix(path, record) {
			return errInjected
		}
		return nil
	})
	target := filepath.Join(h.work, "old", "cli", "bin", "vw2026")
	renameWhen(t, func(oldpath, newpath string) error {
		if newpath == target {
			return errInjected
		}
		return nil
	})
	var stderr bytes.Buffer
	o := h.options()
	o.Stderr = &stderr
	o.Executable = h.fakeExecutable()
	if result, err := Install(o, "cli", "c2", false); err != nil || result.Outcome != outcomeInstalled ||
		!strings.Contains(stderr.String(), "the vw2026 of the previous version is in") {
		t.Fatalf("%+v %v %s", result, err, stderr.String())
	}
	if moved, _ := filepath.Glob(filepath.Join(h.work, "old", oldCLIName+"*")); len(moved) != 1 {
		t.Fatal("the moved vw2026 should stay where the message says:", moved)
	}
}

// 記録を置き換えられなければ（書く途中で止まった・名前を変えられない）、この回が隣に書いたファイルを残さず、元の
// 記録も、前からある隣の名前のファイル（この回が書いたものではない）もそのまま。
func TestReplaceFileLeavesNoTemporary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, RecordFile)
	os.WriteFile(path, []byte("old"), 0o644)
	theirs := path + ".new"
	os.WriteFile(theirs, []byte("theirs"), 0o644)
	written := func() []string {
		matches, _ := filepath.Glob(path + ".new-*")
		return matches
	}
	// 書きかけて止まる。
	original := writeFile
	restore := swap(t, &writeFile, func(name string, data []byte, perm fs.FileMode) error {
		original(name, data[:1], perm)
		return errInjected
	})
	if err := replaceFile(path, []byte("new")); !errors.Is(err, errInjected) || len(written()) != 0 {
		t.Fatal("write:", err, written())
	}
	// 書けたが、置き換えられない。
	restore()
	swap(t, &rename, func(string, string) error { return errInjected })
	if err := replaceFile(path, []byte("new")); !errors.Is(err, errInjected) || len(written()) != 0 {
		t.Fatal("rename:", err, written())
	}
	if data, _ := os.ReadFile(path); string(data) != "old" {
		t.Fatal(string(data))
	}
	if data, _ := os.ReadFile(theirs); string(data) != "theirs" {
		t.Fatal("touched a file it did not write:", string(data))
	}
}

// 記録の無い前の版を .kept-<時刻> へ脇へ移したときは、よけた CLI をそこへ戻す（利用者が戻すかもしれない版から、
// vw2026 を消さない）。
func TestKeptPreviousVersionGetsTheMovedCLI(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	h.publish(release{name: "cli", tag: "c2"})
	// module が欠けた（隔離された）入った cli から更新する。前の版は記録が正しくないので、消さずに脇へ移す。
	os.RemoveAll(filepath.Join(h.plugins, "cli", "cli.vwlibrary"))
	var stderr bytes.Buffer
	o := h.options()
	o.Stderr = &stderr
	o.Executable = h.fakeExecutable()
	if result, err := Install(o, "cli", "c2", false); err != nil || result.Outcome != outcomeInstalled {
		t.Fatalf("%+v %v %s", result, err, stderr.String())
	}
	kept, _ := filepath.Glob(filepath.Join(h.work, "old", "cli.kept-*"))
	if len(kept) != 1 {
		t.Fatal("kept:", kept, stderr.String())
	}
	if data, err := os.ReadFile(filepath.Join(kept[0], "bin", "vw2026")); err != nil || string(data) != "cli c1" {
		t.Fatal("the kept version should have its vw2026 back:", string(data), err)
	}
}

// 脇へ移した前の版へよけた CLI を戻せなければ、その場所を知らせ、よけた CLI を消さない。
func TestKeptPreviousVersionCannotGetTheMovedCLI(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	h.publish(release{name: "cli", tag: "c2"})
	os.RemoveAll(filepath.Join(h.plugins, "cli", "cli.vwlibrary"))
	renameWhen(t, func(oldpath, newpath string) error {
		if strings.Contains(newpath, ".kept-") && strings.HasSuffix(newpath, filepath.Join("bin", "vw2026")) {
			return errInjected
		}
		return nil
	})
	var stderr bytes.Buffer
	o := h.options()
	o.Stderr = &stderr
	o.Executable = h.fakeExecutable()
	if result, err := Install(o, "cli", "c2", false); err != nil || result.Outcome != outcomeInstalled ||
		!strings.Contains(stderr.String(), "the vw2026 of the previous version is in") {
		t.Fatalf("%+v %v %s", result, err, stderr.String())
	}
	if moved, _ := filepath.Glob(filepath.Join(h.work, "old", oldCLIName+"*")); len(moved) != 1 {
		t.Fatal("the moved vw2026 should stay where the message says:", moved)
	}
}

// この回が CLI をよけていなければ、前の版を脇へ移しても、前の回までのよけた CLI は消してみる。
func TestKeptPreviousVersionWithoutAMovedCLICleansUp(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	h.publish(release{name: "cli", tag: "c2"})
	// 記録の無い前回の残り（同じタグの install が脇へ移す）と、前の回までのよけた CLI。
	old := filepath.Join(h.work, "old")
	os.MkdirAll(filepath.Join(old, "cli"), 0o700)
	leftover := filepath.Join(old, oldCLIName+"20260101T000000.000000000")
	os.WriteFile(leftover, nil, 0o700)
	if result, err := h.install("cli", "c1"); err != nil || result.Outcome != outcomeUpToDate {
		t.Fatalf("%+v %v", result, err)
	}
	if kept, _ := filepath.Glob(filepath.Join(old, "cli.kept-*")); len(kept) != 1 || exist(leftover) {
		t.Fatal(kept, exist(leftover))
	}
}

// 前の版を Vectorworks が使っているかもしれないので残すときの案内は、呼ぶ場面で変わる。付け替えの前なら何も変えて
// いないので走らせ直すだけ、フックのあとなら新しい版が入っているので、入ったタグで走らせ直して片付ける。
func TestSettleOldAdviceDependsOnTheStage(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	// 付け替えの前: 前回の残りの old/cli があり、マニフェストを取る間に Vectorworks が起動する（片付けの確かめで見つける）。
	old := filepath.Join(h.work, "old", "cli")
	if err := os.CopyFS(old, os.DirFS(filepath.Join(h.plugins, "cli"))); err != nil {
		t.Fatal(err)
	}
	h.publish(release{name: "cli", tag: "c2"})
	unlock := func() {}
	h.onGet("/cli/c2/vw2026.json", func() { unlock = h.holdLock() })
	if result, err := h.install("cli", "c2"); !errors.Is(err, ErrRunning) || result.Outcome != outcomeRunning ||
		!strings.Contains(err.Error(), "nothing is changed") || strings.Contains(err.Error(), "clean it up") {
		t.Fatalf("%+v %v", result, err)
	}
	unlock()
	os.RemoveAll(old)

	// フックのあと: フックの間に Vectorworks が起動し、フックは通る。
	signal := filepath.Join(h.tmp, "hook-started")
	await := filepath.Join(h.tmp, "locked")
	h.publish(release{name: "cli", tag: "c3", install: &hookPlan{Signal: signal, Await: await}})
	done := lockWhenSignaled(h, signal, await)
	var stderr bytes.Buffer
	o := h.options()
	o.Stderr = &stderr
	result, err := Install(o, "cli", "c3", false)
	<-done
	if err != nil || result.Outcome != outcomeInstalled ||
		!strings.Contains(stderr.String(), "the new version is installed; quit Vectorworks, then run install cli@c3 again") {
		t.Fatalf("%+v %v %s", result, err, stderr.String())
	}
}
