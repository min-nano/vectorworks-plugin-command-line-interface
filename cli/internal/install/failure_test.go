package install

import (
	"archive/zip"
	"bytes"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/registry"
)

// errInjected は、テストが差し替えたファイルの操作が返す失敗。
var errInjected = errors.New("injected failure")

// failMkdirAll・failRemoveAll・failWriteFile は、その場所への操作を失敗させる（OS の失敗を真似る）。
func failMkdirAll(t *testing.T, at string) {
	t.Helper()
	original := mkdirAll
	mkdirAll = func(path string, perm fs.FileMode) error {
		if path == at {
			return errInjected
		}
		return original(path, perm)
	}
	t.Cleanup(func() { mkdirAll = original })
}

func failRemoveAll(t *testing.T, at string) {
	t.Helper()
	original := removeAll
	removeAll = func(path string) error {
		if path == at {
			return errInjected
		}
		return original(path)
	}
	t.Cleanup(func() { removeAll = original })
}

// failRenameTo は、from から to への名前の変更だけを失敗させる（同じ from からほかへの変更は通す）。
func failRenameTo(t *testing.T, from, to string) {
	t.Helper()
	original := rename
	rename = func(oldpath, newpath string) error {
		if oldpath == from && newpath == to {
			return errInjected
		}
		return original(oldpath, newpath)
	}
	t.Cleanup(func() { rename = original })
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

// 展開先の中にとどまる .. も断る（区切りの .. は名前に使えない）。
func TestExtractRefusesDotDotInside(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.zip")
	os.WriteFile(path, makeZip(t, []entry{{name: "a/../b", body: "x"}}), 0o644)
	if _, err := extract(path, filepath.Join(dir, "out")); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatal(err)
	}
}

// ファイルの項目の下に項目があれば（フォルダを作れない）、展開を失敗にする。
func TestExtractFileUnderAFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.zip")
	os.WriteFile(path, makeZip(t, []entry{{name: "a", body: "file"}, {name: "a/b", body: "x"}}), 0o644)
	if _, err := extract(path, filepath.Join(dir, "out")); err == nil {
		t.Fatal("should fail")
	}
}

// 知らない圧縮の方法の項目は読めないので、展開を失敗にする。
func TestExtractUnknownCompression(t *testing.T) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, err := w.CreateRaw(&zip.FileHeader{Name: "a", Method: 99})
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte("x"))
	w.Close()
	dir := t.TempDir()
	path := filepath.Join(dir, "x.zip")
	os.WriteFile(path, buf.Bytes(), 0o644)
	if _, err := extract(path, filepath.Join(dir, "out")); err == nil {
		t.Fatal("should fail")
	}
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

// 相対パスで渡した <Plug-Ins>・<Work> は、今の作業ディレクトリから絶対にする。
func TestRelativePlaces(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	p, err := (&Options{PluginsDir: "Plug-Ins", WorkDir: filepath.Join("a", "..", "work")}).places("cli")
	if err != nil || !filepath.IsAbs(p.plugins) || filepath.Base(p.plugins) != "Plug-Ins" ||
		!filepath.IsAbs(p.work) || strings.Contains(p.work, "..") {
		t.Fatalf("%+v %v", p, err)
	}
}

// 走らせられないフック（ほかの OS のもの）は、フックの失敗として戻す。
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

// 名前がどのレジストリにも無い・規則が合わない・マニフェストが読めないときは、何も変えずに失敗にする。
func TestInstallManifestErrors(t *testing.T) {
	h := newHarness(t)
	if _, err := h.install("nowhere", "1"); err == nil || !strings.Contains(err.Error(), "no registry") {
		t.Error(err)
	}
	narrow := filepath.Join(h.tmp, "narrow.json")
	os.WriteFile(narrow, []byte(`{"registry_version":1,"plugins":{"other":{"manifests":[{"match":"v*","path":"`+
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
	if exist(filepath.Join(h.plugins, "cli")) || exist(h.work) {
		t.Error("changed something")
	}
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
	failRename(t, resolved(t, h.fakeExecutable()))
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
	failRename(t, filepath.Join(h.work, "staging", "cli"))
	failRename(t, old)
	if _, err := h.install("cli", "c2"); err == nil || !strings.Contains(err.Error(), "by hand") ||
		!strings.Contains(err.Error(), old) {
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
			failRenameTo(t, move[0], move[1])
			if _, err := h.install("cli", "c2"); err == nil || !strings.Contains(err.Error(), c.want) {
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
	failRename(t, old)
	if _, err := Uninstall(h.options(), "cli"); err == nil || !strings.Contains(err.Error(), "by hand") {
		t.Fatal(err)
	}
}

// 移したフォルダを消せなければ、印を残して失敗にし、もう一度走らせると続きから消す。
func TestUninstallCannotDeleteTheFolder(t *testing.T) {
	h := newHarness(t)
	h.installCLI()
	old := filepath.Join(h.work, "old", "cli")
	marker := filepath.Join(h.work, "old", "cli.uninstalling")
	original := removeAll
	failRemoveAll(t, old)
	if _, err := Uninstall(h.options(), "cli"); err == nil || !strings.Contains(err.Error(), "run uninstall again") {
		t.Fatal(err)
	}
	if !exist(marker) || !exist(old) || exist(filepath.Join(h.plugins, "cli")) {
		t.Fatal("should keep the marker and old")
	}
	removeAll = original
	if result, err := Uninstall(h.options(), "cli"); err != nil || result.Outcome != OutcomeUninstalled || exist(old) {
		t.Fatalf("%+v %v", result, err)
	}
}

// 付け替えで退避したフォルダの記録の module が無ければ（利用者のファイルが入っているかもしれない）、
// 消さずに残して知らせる（守ること 9）。
func TestInstallKeepsAFolderWithoutModule(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	dest := filepath.Join(h.plugins, "cli")
	os.RemoveAll(filepath.Join(dest, "cli.vwlibrary"))
	os.WriteFile(filepath.Join(dest, "mine.txt"), []byte("mine"), 0o644)
	var stderr bytes.Buffer
	o := h.options()
	o.Stderr = &stderr
	if result, err := Install(o, "cli", "c1", false); err != nil || result.Outcome != OutcomeInstalled {
		t.Fatalf("%+v %v", result, err)
	}
	old := filepath.Join(h.work, "old", "cli")
	if data, _ := os.ReadFile(filepath.Join(old, "mine.txt")); string(data) != "mine" || !strings.Contains(stderr.String(), old) {
		t.Fatalf("should keep %s and say so: %s", old, stderr.String())
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
	done := make(chan struct{})
	go func() {
		defer close(done)
		for deadline := time.Now().Add(10 * time.Second); !exist(signal) && time.Now().Before(deadline); {
			time.Sleep(10 * time.Millisecond)
		}
		h.holdLock()
		os.WriteFile(await, nil, 0o600)
	}()
	return done
}

// https:// でない URL のレジストリは、パスとして扱わずに使い方の誤りにする。
func TestRegistrySourceRefusesOtherSchemes(t *testing.T) {
	for _, ref := range []string{"http://example.com/r.json", "HTTPS://example.com/r.json", "file:///r.json"} {
		if _, err := RegistrySource(ref); !IsUsage(err) {
			t.Errorf("%s: %v", ref, err)
		}
	}
}

// 資産が上限より大きければ、取るのを失敗にして、取ったものを残さない。
func TestDownloadTooLarge(t *testing.T) {
	original := maxAsset
	maxAsset = 4
	t.Cleanup(func() { maxAsset = original })
	h := newHarness(t)
	h.serve("/big.zip", []byte("0123456789"))
	path := filepath.Join(t.TempDir(), "big.zip")
	o := h.options()
	if err := o.download(h.url("/big.zip"), path, sha256Hex([]byte("0123456789"))); err == nil ||
		!strings.Contains(err.Error(), "larger") || exist(path) {
		t.Fatal(err)
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
	if result, err := h.install("demo", "d1"); err != nil || result.Outcome != OutcomeInstalled || h.installedVersion("demo") != "d1" {
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
	original := rename
	rename = func(from, to string) error {
		if filepath.Base(from) == "VW2026-dev" {
			moved = to
		}
		return original(from, to)
	}
	t.Cleanup(func() { rename = original })
	if _, err := Install(o, "cli", "c2", false); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(filepath.Base(moved), oldCLIName) || exist(moved) {
		t.Fatalf("moved to %s", moved)
	}
}
