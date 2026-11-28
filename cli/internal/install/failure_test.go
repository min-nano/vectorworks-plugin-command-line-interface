package install

import (
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

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/edition"
	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/fakeplugin"
	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/registry"
	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/spool"
)

// errInjected は、テストが差し替えたファイルの操作が返す失敗。
var errInjected = errors.New("injected failure")

// swap は、テストの間のみ差し替え口 *at（ファイルの操作・上限）を value に置換する。返す関数で
// 途中でも元に戻せる（テストの終了時にも戻す）。戻すのは、この swap の前の値であるため、途中で
// 戻してよいのは、その差し替え口を最後に差し替えたもののみ（後から同じ差し替え口を
// 差し替えていると、それも解除される）。
func swap[T any](t *testing.T, at *T, value T) (restore func()) {
	t.Helper()
	original := *at
	*at = value
	restore = func() { *at = original }
	t.Cleanup(restore)
	return restore
}

// renameWhen・pathOpWhen・writeFileWhen・mkdirAllWhen は、テストの間のみその操作の前に when を
// 呼び出し、when が誤りを返せばそれで失敗させる（nil なら元の操作を行う。参照するのみの場合も
// nil を返す）。返す関数で途中でも元に戻せる。pathOpWhen は、パス 1 つを受け取る操作
// （removeAll・removeFile）の差し替え口 at に使用する。
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

// failMkdirAll は、その場所のディレクトリの作成を失敗させる（OS の失敗を模倣する）。
func failMkdirAll(t *testing.T, at string) {
	t.Helper()
	mkdirAllWhen(t, func(path string) error {
		if path == at {
			return errInjected
		}
		return nil
	})
}

// failRemoveAll は、その場所の削除を失敗させる（OS の失敗を模倣する）。
func failRemoveAll(t *testing.T, at string) {
	t.Helper()
	pathOpWhen(t, &removeAll, func(path string) error {
		if path == at {
			return errInjected
		}
		return nil
	})
}

// failRename は、from から to への名前の変更を失敗させる（from が空なら移動するものを、to が空なら
// 移動先を問わない。合致しない変更は通す）。返す関数で途中でも元に戻せる。
func failRename(t *testing.T, from, to string) (restore func()) {
	t.Helper()
	return renameWhen(t, func(oldpath, newpath string) error {
		if (from == "" || oldpath == from) && (to == "" || newpath == to) {
			return errInjected
		}
		return nil
	})
}

// resolved は、リンクを解決したパス（sidestep が移動する実行ファイルのパス）。
func resolved(t *testing.T, path string) string {
	t.Helper()
	got, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// 展開先を作成できなければ、展開を失敗とする。
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

// zip が記録の名前のフォルダを含む場合は、記録を書き込めずに失敗し、何も変更しない。
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

// 取得したものを書き込む場所がフォルダならば、取得を失敗とする。
func TestDownloadCannotWrite(t *testing.T) {
	h := newHarness(t)
	h.serve("/a.zip", []byte("x"))
	o := h.options()
	if err := o.download(h.url("/a.zip"), t.TempDir(), sha256Hex([]byte("x"))); err == nil {
		t.Fatal("should fail")
	}
}

// 本文の途中で切断された場合は、取得を失敗とする（マニフェストも zip も）。転送が 10 回を超えたら
// 停止する。
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
	if err := o.download(h.url("/cut"), path, strings.Repeat("0", 64)); err == nil || exists(path) {
		t.Errorf("a cut zip should fail and leave nothing: %v", err)
	}
	if _, err := o.fetch(h.url("/loop")); err == nil || !strings.Contains(err.Error(), "10 redirects") {
		t.Error(err)
	}
}

// 実行できないフック（他の OS のもの）は、差し替える前に拒否する。記録に複製したフックが
// 実行できない（他の OS から複製したフォルダ）場合は、フックの失敗として巻き戻す。
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
	if exists(filepath.Join(h.plugins, "cli")) {
		t.Fatal("left the plug-in")
	}

	h.mustInstall(release{name: "cli", tag: "c2"})
	dest := filepath.Join(h.plugins, "cli")
	record, _ := readRecord(dest)
	// powershell のスクリプトは .ps1 に限定する（registry.Script）。
	uninstallScript := strings.Replace(script, "install", "uninstall", 1)
	os.WriteFile(filepath.Join(dest, filepath.FromSlash(uninstallScript)), nil, 0o644)
	record.Hooks.Uninstall = []string{interpreter, uninstallScript}
	data, _ := json.Marshal(record)
	os.WriteFile(filepath.Join(dest, RecordFile), data, 0o644)
	if _, err := Uninstall(o, "cli"); err == nil || !strings.Contains(err.Error(), "do not run") || !validRecord(dest, "cli") {
		t.Fatal(err)
	}
}

// フックの 1 つ目が zip の中の実行ファイルならば、それを実行し、残りの引数を渡す。標準エラーの
// 出力先が無ければ破棄する。
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

// 使用方法の誤りは、何が不正であるかを示す。
func TestUsageErrorMessage(t *testing.T) {
	_, err := Install(Options{}, "Bad", "latest", false)
	if !IsUsage(err) || !strings.Contains(err.Error(), `"Bad"`) {
		t.Fatal(err)
	}
	if _, err := Uninstall(Options{}, "Bad"); !IsUsage(err) {
		t.Fatal(err)
	}
}

// スプールのロックを、存在しない以外の理由で調査できなければ（ここではスプールの場所がファイル）、
// 実行中か否かが判明しないため、install も uninstall も何も変更せずに拒否する（「実行中でない」
// とみなして差し替えない）。
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

// --registry の記法の誤りは、cli の無い <Plug-Ins> でも使用方法の誤りとする（cli の検証より先）。
func TestRegistryUsageBeforeTheCLICheck(t *testing.T) {
	h := newHarness(t)
	o := h.options()
	o.Registries = []string{"/plugins.json"}
	if _, err := Install(o, "elsewhere", "1", false); !IsUsage(err) {
		t.Fatal(err)
	}
}

// cli の無い <Plug-Ins> では、レジストリを取得する前に拒否する（到達できないレジストリによって、
// 本来の理由が隠れない）。
func TestInstallChecksCLIBeforeTheRegistry(t *testing.T) {
	h := newHarness(t)
	o := h.options()
	o.Registries = []string{"https://127.0.0.1:1/unreachable.json"}
	// 既定のレジストリに無い名前であるため、検証が後ならば到達できないレジストリを取得しに行く。
	if _, err := Install(o, "elsewhere", "1", false); err == nil || !strings.Contains(err.Error(), "has no cli") {
		t.Fatal(err)
	}
}

// 名前がいずれのレジストリにも無い・規則が合致しない・マニフェストが読み取れない場合は、何も
// 変更せずに失敗とする。
func TestInstallManifestErrors(t *testing.T) {
	h := newHarness(t)
	// cli でない名前は cli のある <Plug-Ins> でのみ検索する（無ければ、レジストリを取得する前に
	// 拒否する）。
	h.installCLI()
	before := snapshot(t, h.plugins)
	// 検索したレジストリを示す。
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
	h.serve("/cli/broken/"+edition.Command+".json", []byte("{"))
	if _, err := h.install("cli", "broken"); err == nil || !strings.Contains(err.Error(), "manifest") {
		t.Error(err)
	}
	sameSnapshot(t, before, snapshot(t, h.plugins))
}

// <Plug-Ins> がファイルならば、何も変更せずに失敗とする。
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

// <Work> がファイルならば（作業領域を作成できない）、何も変更せずに失敗とする。
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

// <Work>/old を作成できなければ（OS の失敗）、install も uninstall も何も変更せずに失敗とする。
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

// 前回の構築の残存物を削除できなければ（zip の場所が内容のあるフォルダ・フォルダを削除できない）、
// 何も変更しない。
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

// 実行中の CLI を退避できなければ、差し替えずに失敗とする。
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
	if exists(filepath.Join(h.work, "staging", "cli")) {
		t.Error("left the staging folder")
	}
}

// <Plug-Ins> を作成できなければ、退避した以前の版を復元する。
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

// 差し替えの 2 つ目が失敗し、以前の版も復元できなければ、手動で復元する場所を示す。
func TestInstallCannotPutTheOldVersionBack(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	h.publish(release{name: "cli", tag: "c2"})
	old := filepath.Join(h.work, "old", "cli")
	failRename(t, filepath.Join(h.work, "staging", "cli"), "")
	failRename(t, old, "")
	// 実行中の CLI を退避していれば、その場所も示す（復元した版のフォルダには CLI が無いため）。
	o := h.options()
	o.Executable = h.fakeExecutable()
	if _, err := Install(o, "cli", "c2", false); err == nil || !strings.Contains(err.Error(), "by hand") ||
		!strings.Contains(err.Error(), old) || !strings.Contains(err.Error(), "move the running "+edition.Command+" back") {
		t.Fatal(err)
	}
}

// フックが失敗して巻き戻す場合、新しい版を退避できなければ残置し、以前の版を復元できなければ手動で
// 復元する場所を示す。
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
			// 以前の版を復元できない場合は、退避した CLI の場所も示す。
			if name == "old version" && !strings.Contains(err.Error(), "move the running "+edition.Command+" back") {
				t.Fatal(err)
			}
		})
	}
}

// 退避した CLI を復元できなければ、いずれの巻き戻しも、復元したとは述べずに、手動で復元する場所を
// 示す（黙って残置すると、次の後処理が退避した名前のまま唯一の vw2026 を削除するため）。
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
			executable := filepath.Join(h.plugins, "cli", "bin", edition.Command)
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

// uninstall の標識を書き込めなければ（標識の場所がフォルダ）、何も変更しない。
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

// フックが失敗し、フォルダを復元できなければ、手動で復元する場所を示す。
func TestUninstallCannotPutTheFolderBack(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1", uninstall: &hookPlan{Exit: 1}})
	old := filepath.Join(h.work, "old", "cli")
	failRename(t, old, "")
	if _, err := Uninstall(h.options(), "cli"); err == nil || !strings.Contains(err.Error(), "by hand") {
		t.Fatal(err)
	}
}

// フックの後で移動したフォルダを構築の場所へ移動できなければ（Windows で内部のファイルが
// 開かれている）、何も削除せずに標識を残置して失敗とし、再度実行すると続きから削除する。
func TestUninstallCannotDeleteTheFolder(t *testing.T) {
	h := newHarness(t)
	h.installCLI()
	old := filepath.Join(h.work, "old", "cli")
	marker := filepath.Join(h.work, "old", "cli.uninstalling")
	restore := failRename(t, old, filepath.Join(h.work, "staging", "cli"))
	if _, err := Uninstall(h.options(), "cli"); err == nil ||
		!strings.Contains(err.Error(), "run uninstall cli again") {
		t.Fatal(err)
	}
	if !exists(marker) || !validRecord(old, "cli") || exists(filepath.Join(h.plugins, "cli")) {
		t.Fatal("should keep the marker and the whole of old")
	}
	restore()
	if result, err := Uninstall(h.options(), "cli"); err != nil || result.Outcome != outcomeUninstalled || exists(old) {
		t.Fatalf("%+v %v", result, err)
	}
}

// 構築の場所へ移動した後の削除が途中で停止しても、アンインストールしたことは変わらず（old にも
// 標識にも残らない）、残存物は次の uninstall が削除する。
func TestUninstallLeavesWhatCannotBeDeletedInStaging(t *testing.T) {
	h := newHarness(t)
	h.installCLI()
	stage := filepath.Join(h.work, "staging", "cli")
	// 移動した後（内容がある場合）のみ失敗させる（移動する前の、前回の残存物を削除する呼び出しは
	// 通す）。
	restore := pathOpWhen(t, &removeAll, func(path string) error {
		if path == stage && exists(stage) {
			return errInjected
		}
		return nil
	})
	o, stderr := h.capturing()
	if result, err := Uninstall(o, "cli"); err != nil || result.Outcome != outcomeUninstalled {
		t.Fatalf("%+v %v", result, err)
	}
	if !exists(stage) || exists(filepath.Join(h.work, "old", "cli")) || exists(filepath.Join(h.work, "old", "cli.uninstalling")) ||
		!strings.Contains(stderr.String(), stage) {
		t.Fatalf("should leave only the staging folder and say so: %s", stderr.String())
	}
	restore()
	if result, err := Uninstall(h.options(), "cli"); err != nil || result.Outcome != outcomeNotInstalled || exists(stage) {
		t.Fatalf("%+v %v", result, err)
	}
}

// 差し替えで退避したフォルダの記録の module が存在しなければ（利用者のファイルが格納されている
// 可能性がある）、削除せずに <Work>/old/<name>.kept-<時刻>/ へ移動して通知する（遵守事項 9）。
// old/<name>/ には残置しないため、次の install・uninstall も通る。
func TestInstallKeepsAFolderWithoutModule(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	dest := filepath.Join(h.plugins, "cli")
	os.RemoveAll(filepath.Join(dest, "cli.vwlibrary"))
	os.WriteFile(filepath.Join(dest, "mine.txt"), []byte("mine"), 0o644)
	o, stderr := h.capturing()
	if result, err := Install(o, "cli", "c1", false); err != nil || result.Outcome != outcomeInstalled {
		t.Fatalf("%+v %v", result, err)
	}
	kept, _ := filepath.Glob(filepath.Join(h.work, "old", "cli.kept-*"))
	if len(kept) != 1 || exists(filepath.Join(h.work, "old", "cli")) || !strings.Contains(stderr.String(), kept[0]) {
		t.Fatalf("should move the previous folder aside and say so: %v %s", kept, stderr.String())
	}
	if data, _ := os.ReadFile(filepath.Join(kept[0], "mine.txt")); string(data) != "mine" {
		t.Fatal("lost the file")
	}
	h.mustInstall(release{name: "cli", tag: "c2"})
	if result, err := Uninstall(h.options(), "cli"); err != nil || result.Outcome != outcomeUninstalled {
		t.Fatalf("%+v %v", result, err)
	}
	if !exists(filepath.Join(kept[0], "mine.txt")) {
		t.Fatal("removed the kept folder")
	}
}

// 退避したフォルダを別の場所へ移動できなければ、インストールしたことは変更せずに通知し、
// old/<name>/ に残置する。
func TestInstallCannotMoveAsideAFolderWithoutModule(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	os.RemoveAll(filepath.Join(h.plugins, "cli", "cli.vwlibrary"))
	old := filepath.Join(h.work, "old", "cli")
	failRename(t, old, "")
	o, stderr := h.capturing()
	if result, err := Install(o, "cli", "c1", false); err != nil || result.Outcome != outcomeInstalled {
		t.Fatalf("%+v %v", result, err)
	}
	if !exists(old) || !strings.Contains(stderr.String(), old) {
		t.Fatalf("should leave old and say so: %s", stderr.String())
	}
}

// 初回のインストールでフックが失敗し、その間に Vectorworks が起動した場合は、新しい版を残置し、
// 以前の版の場所は示さない。
func TestFirstInstallHookFailureWhileRunning(t *testing.T) {
	h := newHarness(t)
	h.publish(release{name: "cli", tag: "c1", install: h.lockingHook(hookPlan{Exit: 1})})
	done, _ := h.lockWhenHookStarts(h.cliSpool())
	_, err := h.install("cli", "c1")
	<-done
	if err == nil || !strings.Contains(err.Error(), "Vectorworks started") || strings.Contains(err.Error(), "previous") {
		t.Fatal(err)
	}
	if h.installedVersion("cli") != "c1" {
		t.Fatal("the new version should stay")
	}
	// 取得した zip は残置しない（同じタグで再実行する install は zip を取得せず、削除する経路を
	// 通らない）。
	if exists(filepath.Join(h.work, "staging", "cli.zip")) {
		t.Fatal("should remove the downloaded zip")
	}
}

// 新しい版を残置した install の後、同じタグの install は、フックが成功したら構築の場所の残存物も
// 削除する。
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
		if exists(path) {
			t.Fatal("left", path)
		}
	}
}

// uninstall が空の <Work>/staging を削除した後も、old へ移動したフォルダを構築の場所を経由して
// 削除できる（discard が <Work>/staging を作成する）。uninstall cli と、同じタグの install の
// 後処理の両方。
func TestDiscardWithoutStaging(t *testing.T) {
	h := newHarness(t)
	h.installCLI()
	h.mustInstall(release{name: "demo", tag: "d1"})
	if result, err := Uninstall(h.options(), "demo"); err != nil || result.Outcome != outcomeUninstalled ||
		exists(filepath.Join(h.work, "staging")) {
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
	if result, err := h.install("cli", "c1"); err != nil || result.Outcome != outcomeUpToDate || exists(old) {
		t.Fatalf("%+v %v", result, err)
	}
}

// uninstall の discard が失敗したら（<Work>/staging を作成できない・old から構築の場所へ
// 移動できない）、old へ移動したフォルダと標識を残置して失敗とする（再実行すると続きから
// 実行される）。再実行の案内（run uninstall demo again）は 1 つのみで、誤りの連鎖は discard の
// 内部まで続かない（cmd が ErrRunning などを discard の内部から拾わないように）。
func TestUninstallDiscardFailure(t *testing.T) {
	for name, fail := range map[string]func(t *testing.T, h *harness){
		"staging": func(t *testing.T, h *harness) {
			staging := filepath.Join(h.work, "staging")
			os.RemoveAll(staging)
			failMkdirAll(t, staging)
		},
		"rename": func(t *testing.T, h *harness) {
			failRename(t, filepath.Join(h.work, "old", "demo"), filepath.Join(h.work, "staging", "demo"))
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.installCLI()
			h.mustInstall(release{name: "demo", tag: "d1"})
			fail(t, h)
			_, err := Uninstall(h.options(), "demo")
			if err == nil || !strings.Contains(err.Error(), "demo is out of the folder of plug-ins, but") ||
				strings.Count(err.Error(), "again") != 1 ||
				!strings.Contains(err.Error(), "run uninstall demo again") ||
				errors.Is(err, errInjected) {
				t.Fatal(err)
			}
			if !validRecord(filepath.Join(h.work, "old", "demo"), "demo") || !exists(filepath.Join(h.work, "old", "demo.uninstalling")) {
				t.Fatal("should keep old and the marker")
			}
		})
	}
}

// install の差し替えの前の後処理（settleOld の discard）が失敗したら、何も変更せずに失敗とし、
// 再実行する install <name>@<tag> を 1 つのみ案内する（settleOld が受け取った again が
// discard・renameError まで届く）。
func TestInstallDiscardFailure(t *testing.T) {
	h := newHarness(t)
	h.installCLI()
	// 前回の残存物（記録の正しい以前の版）が old にあると、差し替えの前に構築の場所へ移動してから
	// 削除する。
	old := filepath.Join(h.work, "old", "cli")
	if err := os.CopyFS(old, os.DirFS(filepath.Join(h.plugins, "cli"))); err != nil {
		t.Fatal(err)
	}
	h.publish(release{name: "cli", tag: "c2"})
	failRename(t, old, filepath.Join(h.work, "staging", "cli"))
	_, err := h.install("cli", "c2")
	if err == nil || strings.Count(err.Error(), "again") != 1 ||
		!strings.Contains(err.Error(), "run install cli@c2 again") {
		t.Fatal(err)
	}
	if h.installedVersion("cli") != "c1" || !validRecord(old, "cli") {
		t.Fatal("should change nothing")
	}
}

// 空の <Work> を削除できなくても、アンインストールしたことは変わらない（残存した <Work> は次の
// install・uninstall が使用する）。
func TestUninstallCannotRemoveTheEmptyWork(t *testing.T) {
	h := newHarness(t)
	h.installCLI()
	pathOpWhen(t, &removeFile, func(path string) error {
		if path == h.work {
			return errInjected
		}
		return nil
	})
	result, err := Uninstall(h.options(), "cli")
	if err != nil || result.Outcome != outcomeUninstalled {
		t.Fatalf("%+v %v", result, err)
	}
	if !exists(h.work) || exists(filepath.Join(h.work, "staging")) || exists(filepath.Join(h.work, "old")) {
		t.Fatal("should remove the empty staging and old, and keep the work folder it cannot remove")
	}
}

// インタプリタを経由しないフックが Windows で実行しない拡張子ならば、差し替えの前に拒否する。
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

// インタプリタを経由しないフックに実行の権限が無ければ（mac）、差し替えの前に拒否する。
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

// uninstall のフックが失敗し、その間に Vectorworks が起動した場合は、フォルダを復元せずに標識を
// 残置する。uninstall cli では、ロックのある <Plug-Ins>/cli/spool/ ごと old へ移動するため、
// 復元する前に移動先のスプールのロックを確認する（mac では、移動する直前に読み込んだプラグインの
// ロックがフォルダと共に移動する）。
func TestUninstallHookFailureWhileRunning(t *testing.T) {
	for _, name := range []string{"demo", "cli"} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			plan := h.lockingHook(hookPlan{Exit: 1})
			old := filepath.Join(h.work, "old", name)
			lockAt := h.cliSpool()
			if name == "cli" {
				h.mustInstall(release{name: "cli", tag: "c1", uninstall: plan})
				lockAt = filepath.Join(old, spool.SpoolDirName)
			} else {
				h.installCLI()
				h.mustInstall(release{name: name, tag: "d1", uninstall: plan})
			}
			done, _ := h.lockWhenHookStarts(lockAt)
			_, err := Uninstall(h.options(), name)
			<-done
			if err == nil || !strings.Contains(err.Error(), "Vectorworks started") {
				t.Fatal(err)
			}
			if exists(filepath.Join(h.plugins, name)) || !exists(old) || !exists(filepath.Join(h.work, "old", name+".uninstalling")) {
				t.Fatal("should keep the folder in old with the marker")
			}
		})
	}
}

// 資産が上限より大きければ、取得を失敗とし、取得したものを残置しない。
func TestDownloadTooLarge(t *testing.T) {
	swap(t, &maxAsset, 4)
	h := newHarness(t)
	h.serve("/big.zip", []byte("0123456789"))
	path := filepath.Join(t.TempDir(), "big.zip")
	o, stderr := h.capturing()
	// 大きさ（Content-Length）が判明すれば、書き込む前に拒否する（取得を開始しない）。
	if err := o.download(h.url("/big.zip"), path, sha256Hex([]byte("0123456789"))); err == nil ||
		!strings.Contains(err.Error(), "larger") || exists(path) || strings.Contains(stderr.String(), "downloading") {
		t.Fatal(err, stderr.String())
	}
}

// 退避した CLI の名前は、実行ファイルの名前（大文字の名前・別の名前の複製）に依らず決定され、
// 後処理が削除する。
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
	if !strings.HasPrefix(filepath.Base(moved), oldCLIName) || exists(moved) {
		t.Fatalf("moved to %s", moved)
	}
}

// 展開した大きさの合計が上限を超える zip は、何も展開せずに拒否する（小さな zip が大きく
// 展開されないように）。
func TestExtractTooLarge(t *testing.T) {
	swap(t, &maxExtracted, 5)
	dir := t.TempDir()
	path := filepath.Join(dir, "x.zip")
	os.WriteFile(path, makeZip(t, []entry{{name: "a", body: "abc"}, {name: "b", body: "abc"}}), 0o644)
	out := filepath.Join(dir, "out")
	if _, err := extract(path, out); err == nil || !strings.Contains(err.Error(), "expands to more than 5 bytes") {
		t.Fatal(err)
	}
	if exists(out) {
		t.Fatal("extracted something")
	}
	maxExtracted = 6
	if _, err := extract(path, out); err != nil {
		t.Fatal(err)
	}
}

// 項目の数が上限を超える zip は、何も展開せずに拒否する（空の項目の多い小さな zip が、ファイルを
// 作成し続けないように）。
func TestExtractTooManyEntries(t *testing.T) {
	swap(t, &maxEntries, 1)
	dir := t.TempDir()
	path := filepath.Join(dir, "x.zip")
	os.WriteFile(path, makeZip(t, []entry{{name: "a"}, {name: "b"}}), 0o644)
	out := filepath.Join(dir, "out")
	if _, err := extract(path, out); err == nil || !strings.Contains(err.Error(), "more than 1 entries") || exists(out) {
		t.Fatal(err)
	}
	maxEntries = 2
	if _, err := extract(path, out); err != nil {
		t.Fatal(err)
	}
}

// フックの後で構築の場所を空にできなければ（zip の場所が内容のあるフォルダ）、移動したフォルダを
// 削除せずに標識を残置して失敗とする。
func TestUninstallCannotEmptyTheStaging(t *testing.T) {
	h := newHarness(t)
	h.installCLI()
	old := filepath.Join(h.work, "old", "cli")
	os.MkdirAll(filepath.Join(h.work, "staging", "cli.zip", "x"), 0o700)
	if _, err := Uninstall(h.options(), "cli"); err == nil ||
		!strings.Contains(err.Error(), "run uninstall cli again") {
		t.Fatal(err)
	}
	if !validRecord(old, "cli") || !exists(filepath.Join(h.work, "old", "cli.uninstalling")) {
		t.Fatal("should keep old and the marker")
	}
}

// 差し替えの前に前回の残存物の old を整理する際、構築の場所へ移動したものを削除しきれなければ
// （Windows でウイルス対策が開いているファイルなど）、構築せずに失敗とする（以前の版のファイルを
// 新しい版に混入させない）。
func TestInstallDoesNotMixTheLeftoverIntoTheNewVersion(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	h.publish(release{name: "cli", tag: "c2"})
	dest := filepath.Join(h.plugins, "cli")
	old := filepath.Join(h.work, "old", "cli")
	copyDir(t, dest, old)
	os.WriteFile(filepath.Join(old, "stale.txt"), []byte("old"), 0o644)
	stage := filepath.Join(h.work, "staging", "cli")
	// stale.txt のみが削除できない（他は削除される）。
	restore := pathOpWhen(t, &removeAll, func(path string) error {
		if path == stage && exists(filepath.Join(stage, "stale.txt")) {
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
	// 削除できるようになれば、次の install が残存物を削除してインストールする。
	restore()
	h.mustInstall(release{name: "cli", tag: "c2"})
	if exists(filepath.Join(dest, "stale.txt")) || exists(stage) {
		t.Fatal("the leftover is in the new version or left in staging")
	}
}

// <Plug-Ins>/<name>/ が存在するなら、中断した uninstall の標識は以前の回の残存物であるため、同じ
// タグの経路でも、差し替えの前に失敗した回でも削除する（残置すると、後の install が差し替えの途中で
// 停止した場合に、次の uninstall が唯一残存した版を削除しうる）。
func TestInstallRemovesAStaleMarkerOnEveryPath(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	marker := filepath.Join(h.work, "old", "cli.uninstalling")
	os.WriteFile(marker, nil, 0o600)
	if result, err := h.install("cli", "c1"); err != nil || result.Outcome != outcomeUpToDate || exists(marker) {
		t.Fatalf("same tag: %+v %v", result, err)
	}
	os.WriteFile(marker, nil, 0o600)
	h.publish(release{name: "cli", tag: "c2", sha: strings.Repeat("0", 64)})
	if _, err := h.install("cli", "c2"); err == nil || exists(marker) {
		t.Fatalf("failed before the swap: %v", err)
	}
}

// 大きさの判明しない応答（Content-Length が無い）が上限を超えたら、取得を完了したとは書き込まずに、
// 停止したと書き込む。
func TestDownloadTooLargeSaysStopped(t *testing.T) {
	swap(t, &maxAsset, 4)
	h := newHarness(t)
	h.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.(http.Flusher).Flush()
		w.Write([]byte("0123456789"))
	})
	o, stderr := h.capturing()
	if err := o.download(h.url("/big.zip"), filepath.Join(t.TempDir(), "big.zip"), ""); err == nil {
		t.Fatal("should fail")
	}
	if got := stderr.String(); strings.Contains(got, "downloaded") || !strings.Contains(got, "stopped after") {
		t.Fatalf("%q", got)
	}
}

// モジュールは、拡張子のみでなく、mac はフォルダ（バンドル）、Windows はファイルであることも
// 検証する。
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

// フックの標準出力は上限（maxDocument）までのみを蓄積し、超えたら失敗とする（フックは最後まで
// 実行する）。
func TestHookPrintsTooMuch(t *testing.T) {
	h := newHarness(t)
	h.publish(release{name: "cli", tag: "c1", install: &hookPlan{Stdout: `{"x":"` + strings.Repeat("a", maxDocument) + `"}`}})
	if _, err := h.install("cli", "c1"); err == nil || !strings.Contains(err.Error(), "more than") {
		t.Fatal(err)
	}
	if exists(filepath.Join(h.plugins, "cli")) {
		t.Fatal("should undo the install")
	}
}

// フックのインタプリタがこの OS で実行できなければ、差し替える前に拒否する（<Plug-Ins> を
// 変更しない）。
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
	// 差し替え（<Plug-Ins>/cli の名前の変更）まで進まないことを確認する。
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

// 続きから実行される uninstall cli は、移動先のスプールのロックが取得されていれば何もせずに
// ErrRunning で終了する（<Plug-Ins>/cli/spool/ は既に存在しないため、最初の検証では見えない）。
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
	if !errors.Is(err, ErrRunning) || result.Outcome != outcomeRunning || !exists(old) || !exists(marker) {
		t.Fatalf("%+v %v", result, err)
	}
	if runs := h.hookRuns("cli"); len(runs) != 1 {
		t.Fatalf("should not run the uninstall hook: %+v", runs)
	}
}

// フックが成功しても、その間に移動先のスプールのロックが取得されたら（cli）、削除せずに標識を
// 残置して失敗する（フォルダを移動してフックも実行したため、「何もしなかった」の
// ErrRunning にはしない）。
func TestUninstallCLIHookSuccessWhileLockedInOld(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1", uninstall: h.lockingHook(hookPlan{Stdout: "{}"})})
	old := filepath.Join(h.work, "old", "cli")
	done, _ := h.lockWhenHookStarts(filepath.Join(old, spool.SpoolDirName))
	result, err := Uninstall(h.options(), "cli")
	<-done
	if err == nil || errors.Is(err, ErrRunning) || result != nil || !strings.Contains(err.Error(), old) {
		t.Fatalf("%+v %v", result, err)
	}
	if !validRecord(old, "cli") || !exists(filepath.Join(h.work, "old", "cli.uninstalling")) {
		t.Fatal("should keep old and the marker")
	}
}

// sha256 が合致せず、取得した zip を削除できなくても失敗で終了し、次の install が構築の場所の
// 残存物として削除する。
func TestDownloadMismatchCannotRemove(t *testing.T) {
	h := newHarness(t)
	h.publish(release{name: "cli", tag: "c1", sha: strings.Repeat("0", 64)})
	zip := filepath.Join(h.work, "staging", "cli.zip")
	// 構築の前の後処理（まだ存在しない zip を削除する）は通し、取得した後の zip のみを
	// 削除できなくする。
	restore := pathOpWhen(t, &removeFile, func(path string) error {
		if path == zip && exists(path) {
			return errInjected
		}
		return nil
	})
	o, stderr := h.capturing()
	// 取得完了の行は、sha256 を照合してから書き込む（合致しなければ取得を完了したとは
	// 書き込まない）。
	if _, err := Install(o, "cli", "c1", false); err == nil || !strings.Contains(err.Error(), "sha256") || !exists(zip) ||
		strings.Contains(stderr.String(), "downloaded") || !strings.Contains(stderr.String(), "stopped after") {
		t.Fatal(err, stderr.String())
	}
	restore()
	h.mustInstall(release{name: "cli", tag: "c1"})
	if exists(zip) {
		t.Fatal("left the zip")
	}
}

// cli の install のフックの間に、以前の版を読み込んだ Vectorworks のロックが
// old/cli/spool/ にあれば（差し替えの直前に起動した）、フックが失敗しても巻き戻さずに新しい版を
// 残置する。
func TestInstallCLIHookFailureWhileLockedInOld(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	h.publish(release{name: "cli", tag: "c2", install: h.lockingHook(hookPlan{Exit: 1})})
	old := filepath.Join(h.work, "old", "cli")
	done, _ := h.lockWhenHookStarts(filepath.Join(old, spool.SpoolDirName))
	_, err := h.install("cli", "c2")
	<-done
	if err == nil || !strings.Contains(err.Error(), "Vectorworks started") {
		t.Fatal(err)
	}
	if h.installedVersion("cli") != "c2" || !validRecord(old, "cli") {
		t.Fatal("should keep the new version and the previous one in old")
	}
}

// フックが成功しても、old/cli/spool/ のロックが取得されていれば、退避した版を削除せずに通知する。
// その後は、同じタグの install も、他のプラグインの install・uninstall も、Vectorworks が
// 実行中であるとみなして何もしない（終了してから再実行すると整理する）。
func TestInstallCLIKeepsOldWhileLockedInOld(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	h.publish(release{name: "cli", tag: "c2", install: h.lockingHook(hookPlan{Stdout: "{}"})})
	old := filepath.Join(h.work, "old", "cli")
	done, _ := h.lockWhenHookStarts(filepath.Join(old, spool.SpoolDirName))
	o, stderr := h.capturing()
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

// <Plug-Ins>/<name> を、存在しない以外の理由で調査できなければ（ここでは <Plug-Ins> がファイル）、
// uninstall はインストールされていないとみなさずに失敗とし、何も削除しない（標識も残置する）。
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
	if result, err := Uninstall(o, "cli"); err == nil || result != nil || !exists(marker) {
		t.Fatalf("%+v %v", result, err)
	}
}

// フォルダを調査できなければ（存在しない以外の誤り）、インストールされていないとみなさずに誤りを
// 返す（ロックの検証が先に拒否するため、直接検証する）。
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

// 同じタグでフックのみを実行した回も、以前の install が退避した CLI の削除を試みる。
func TestInstallSameTagCleansMovedCLIs(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	moved := filepath.Join(h.work, "old", oldCLIName+"20260101T000000.000000000")
	os.WriteFile(moved, nil, 0o700)
	if result, err := h.install("cli", "c1"); err != nil || result.Outcome != outcomeUpToDate || exists(moved) {
		t.Fatalf("%+v %v", result, err)
	}
}

// フックが背後に子を残して終了しても（標準出力を保持し続ける）、待機せずにインストールを完了し、
// その旨を通知する。
func TestHookLeavesAProcess(t *testing.T) {
	swap(t, &hookWaitDelay, 100*time.Millisecond)
	h := newHarness(t)
	// 子は releaseFile が作成されるまで残存する。テストの後処理（一時ディレクトリを削除する）の前に
	// 終了させる（Windows では実行中のテストの実行ファイルと、その作業ディレクトリを
	// 削除できない）。
	releaseFile := filepath.Join(h.tmp, "release")
	t.Cleanup(func() {
		os.WriteFile(releaseFile, nil, 0o600)
		for deadline := time.Now().Add(10 * time.Second); !exists(releaseFile+".done") && time.Now().Before(deadline); {
			time.Sleep(10 * time.Millisecond)
		}
		time.Sleep(500 * time.Millisecond)
	})
	h.publish(release{name: "cli", tag: "c1", install: &hookPlan{Stdout: `{"ran":"install"}`, Linger: releaseFile}})
	o, stderr := h.capturing()
	start := time.Now()
	result, err := Install(o, "cli", "c1", false)
	if err != nil || result.Outcome != outcomeInstalled || string(result.Hook) != `{"ran":"install"}` {
		t.Fatalf("%+v %v", result, err)
	}
	if elapsed := time.Since(start); elapsed > 30*time.Second || !strings.Contains(stderr.String(), "stopped waiting") {
		t.Fatalf("%v %s", elapsed, stderr.String())
	}
}

// Vectorworks が実行中であれば、レジストリを取得する前に vectorworks_running と応答する
// （到達できないレジストリの誤りより先）。
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

// <Plug-Ins> と <Work> の一方が他方の内部にあれば、何もせずに拒否する（後処理が
// インストールしたばかりのプラグインを削除しない）。
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
	// 大文字と小文字を区別しない OS では、その違いのみの表記も重複する。区別する OS では別の
	// フォルダ（uninstall で確認する。インストールされていなければ何も作成しない）。
	o := h.options()
	o.PluginsDir = strings.ToUpper(filepath.Join(h.work, "old"))
	_, err := Uninstall(o, "cli")
	if folds := runtime.GOOS == "darwin" || runtime.GOOS == "windows"; folds != (err != nil && strings.Contains(err.Error(), "overlap")) {
		t.Errorf("case: %v", err)
	}
	// リンクで別の表記をしても、同じディレクトリ・その内部ならば拒否する（<Work> の下を指すリンクを
	// --plugins-dir に渡した場合も。まだ存在しない場所の下も、存在する箇所までのリンクを解決して
	// 検出する）。
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

// 同じタグでも、マニフェストを取得する間に起動した Vectorworks の下ではフックを実行しない（最初の
// 検証の後で、再度確認する）。
func TestInstallSameTagLockTakenWhileFetching(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1", latest: true})
	h.onGet("/cli/latest/"+edition.Command+".json", func() { h.holdLock() })
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

// 同じタグで、マニフェストを取得する間にロックを調査できなくなれば、フックを実行せずに失敗とする
// （終了コード 1）。
func TestInstallSameTagLockUnreadableWhileFetching(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows reports a path below a file as not found")
	}
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1", latest: true})
	spoolDir := filepath.Join(h.plugins, "cli", spool.SpoolDirName)
	h.onGet("/cli/latest/"+edition.Command+".json", func() {
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

// Windows のジャンクション（Go は ModeDir と ModeIrregular を返す）も、vw2026 がインストールした
// フォルダとみなさない（リンクの先の作業用の複製に触れない）。
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

// 記録を置換できなければ（書き込みの途中で停止した・名前を変更できない）、この回が隣に書き込んだ
// ファイルを残置せず、元の記録も、以前から存在する隣の名前のファイル（この回が
// 書き込んだものではない）もそのまま。
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
	// 書き込みの途中で停止する。
	original := writeFile
	restore := swap(t, &writeFile, func(name string, data []byte, perm fs.FileMode) error {
		original(name, data[:1], perm)
		return errInjected
	})
	if err := replaceFile(path, []byte("new")); !errors.Is(err, errInjected) || len(written()) != 0 {
		t.Fatal("write:", err, written())
	}
	// 書き込めたが、置換できない。
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
