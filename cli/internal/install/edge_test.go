package install

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/registry"
)

// 本物の sh（mac）と powershell（Windows）でフックを走らせる（Interpreters を差し替えない）。
// 走らせるものは OS の決まった場所のもので、PATH から探さない。
func TestRealInterpreter(t *testing.T) {
	h := newHarness(t)
	hooks := registry.Hooks{Install: []string{"sh", "hooks/install.sh", "x y"}}
	script := entry{name: "hooks/install.sh", body: `printf '{"arg":"%s","tag":"%s"}' "$1" "$VW2026_HOOK_TAG"` + "\n"}
	want := `{"arg":"x y","tag":"r1"}`
	if runtime.GOOS == "windows" {
		hooks = registry.Hooks{Install: []string{"powershell", "hooks/install.ps1", "x y"}}
		script = entry{name: "hooks/install.ps1", body: `[Console]::Out.Write('{"arg":"' + $args[0] + '","tag":"' + $env:VW2026_HOOK_TAG + '"}')`}
	}
	h.publish(release{name: "cli", tag: "r1", hooks: &hooks, entries: []entry{
		{name: "cli.vwlibrary/x", body: "m"},
		script,
	}})
	o := h.options()
	o.Interpreters = nil
	result, err := Install(o, "cli", "r1", false)
	if err != nil || string(result.Hook) != want {
		t.Fatalf("%+v %v", result, err)
	}
}

// sh のフックは Windows で、powershell のフックは mac で走らない（失敗にして戻す）。
func TestInterpreterOfAnotherOS(t *testing.T) {
	o := Options{}
	other := "powershell"
	if runtime.GOOS == "windows" {
		other = "sh"
	}
	if _, err := o.interpreter(other); err == nil {
		t.Fatalf("%s should not run on %s", other, runtime.GOOS)
	}
}

// 転送は追うが、行き先も https:// に限る。200 のほかは失敗。
func TestOpenRefusesRedirectsAndErrors(t *testing.T) {
	h := newHarness(t)
	h.serve("/ok.json", []byte("{}"))
	mux := http.NewServeMux()
	mux.HandleFunc("/to-http", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://example.invalid/x", http.StatusFound)
	})
	mux.HandleFunc("/to-ok", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, h.url("/ok.json"), http.StatusFound)
	})
	mux.HandleFunc("/big", func(w http.ResponseWriter, r *http.Request) {
		w.Write(make([]byte, maxDocument+1))
	})
	inner := h.server.Config.Handler
	h.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, pattern := mux.Handler(r); pattern != "" {
			mux.ServeHTTP(w, r)
			return
		}
		inner.ServeHTTP(w, r)
	})
	o := h.options()
	if data, err := o.fetch(h.url("/to-ok")); err != nil || string(data) != "{}" {
		t.Fatalf("an https redirect should be followed: %q %v", data, err)
	}
	for path, want := range map[string]string{
		"/to-http": "not https://",
		"/missing": "404",
		"/big":     "larger than",
	} {
		if _, err := o.fetch(h.url(path)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", path, err)
		}
	}
	for _, ref := range []string{"relative.json", "http://example.invalid/x"} {
		if _, err := o.fetch(ref); err == nil {
			t.Errorf("%s should be refused", ref)
		}
	}
	if err := o.download(h.url("/missing"), filepath.Join(t.TempDir(), "x.zip"), ""); err == nil {
		t.Error("download of a missing asset should fail")
	}
}

func TestRegistrySource(t *testing.T) {
	if _, err := RegistrySource(""); !IsUsage(err) {
		t.Fatalf("an empty registry is wrong usage: %v", err)
	}
	if got, _ := RegistrySource("https://Example.com/R.json"); got != "https://Example.com/R.json" {
		t.Fatalf("a URL is kept as it is: %s", got)
	}
	dir := t.TempDir()
	t.Chdir(dir)
	got, err := RegistrySource(filepath.Join("a", "..", "plugins.json"))
	if err != nil || !filepath.IsAbs(got) || filepath.Base(got) != "plugins.json" || strings.Contains(got, "..") {
		t.Fatalf("%s %v", got, err)
	}
}

// 場所が分からなければ、何も変えずに失敗にする。
func TestMissingPlaces(t *testing.T) {
	o := Options{Target: testTarget}
	if _, err := Install(o, "cli", "latest", false); err == nil {
		t.Fatal("install without places should fail")
	}
	if _, err := Uninstall(o, "cli"); err == nil {
		t.Fatal("uninstall without places should fail")
	}
}

// 入れる先にフォルダでないもの（利用者のファイル）があれば触れない。
func TestInstallRefusesAFile(t *testing.T) {
	h := newHarness(t)
	h.publish(release{name: "cli", tag: "c1"})
	path := filepath.Join(h.plugins, "cli")
	os.WriteFile(path, []byte("mine"), 0o644)
	if _, err := h.install("cli", "c1"); err == nil || !strings.Contains(err.Error(), "not a folder") {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != "mine" {
		t.Fatal("changed the file")
	}
}

// 同じタグで記録のフックのファイルが無くなっていれば、フックの失敗として何も変えない。
func TestInstallSameTagWithoutTheHookFile(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	os.Remove(filepath.Join(h.plugins, "cli", "hooks", "install.hook"))
	before := snapshot(t, h.plugins)
	if _, err := h.install("cli", "c1"); err == nil || !strings.Contains(err.Error(), "not a file") {
		t.Fatal(err)
	}
	sameSnapshot(t, before, snapshot(t, h.plugins))
}

// zip でないもの・ファイルでない項目は断る。
func TestExtractRefuses(t *testing.T) {
	dir := t.TempDir()
	notZip := filepath.Join(dir, "x.zip")
	os.WriteFile(notZip, []byte("not a zip"), 0o644)
	if _, err := extract(notZip, filepath.Join(dir, "out")); err == nil {
		t.Fatal("should refuse something that is not a zip")
	}
	device := filepath.Join(dir, "device.zip")
	os.WriteFile(device, makeZip(t, []entry{{name: "dev", mode: os.ModeDevice | 0o644}}), 0o644)
	if _, err := extract(device, filepath.Join(dir, "out2")); err == nil || !strings.Contains(err.Error(), "not a file") {
		t.Fatal(err)
	}
	// 項目の中のディレクトリは作る。
	folders := filepath.Join(dir, "folders.zip")
	os.WriteFile(folders, makeZip(t, []entry{{name: "a/", mode: os.ModeDir | 0o755}, {name: "a/b/c", body: "c"}}), 0o644)
	if _, err := extract(folders, filepath.Join(dir, "out3")); err != nil || !exist(filepath.Join(dir, "out3", "a", "b", "c")) {
		t.Fatal(err)
	}
}

// uninstall の安全弁: モジュールが無いときは、install で直すよう示す。
func TestUninstallMissingModuleSaysHowToRepair(t *testing.T) {
	h := newHarness(t)
	h.installCLI()
	os.RemoveAll(filepath.Join(h.plugins, "cli", "cli.vwlibrary"))
	if _, err := Uninstall(h.options(), "cli"); err == nil || !strings.Contains(err.Error(), "run install to repair") {
		t.Fatal(err)
	}
}

// 前の回の old/<name>/ が読めない記録のままで <Plug-Ins>/<name>/ もあるなら、uninstall も断る。
func TestUninstallRefusesUnreadableLeftover(t *testing.T) {
	h := newHarness(t)
	h.installCLI()
	old := filepath.Join(h.work, "old", "cli")
	os.MkdirAll(old, 0o700)
	if _, err := Uninstall(h.options(), "cli"); err == nil || !strings.Contains(err.Error(), old) {
		t.Fatal(err)
	}
	if !exist(filepath.Join(h.plugins, "cli")) || !exist(old) || exist(filepath.Join(h.work, "old", "cli.uninstalling")) {
		t.Fatal("changed something")
	}
}

// 印があっても、old/<name>/ の記録が読めなければ続けない。
func TestUninstallResumeNeedsAValidRecord(t *testing.T) {
	h := newHarness(t)
	h.installCLI()
	h.mustInstall(release{name: "demo", tag: "d1"})
	old := filepath.Join(h.work, "old", "demo")
	os.Rename(filepath.Join(h.plugins, "demo"), old)
	os.Remove(filepath.Join(old, RecordFile))
	os.WriteFile(filepath.Join(h.work, "old", "demo.uninstalling"), nil, 0o600)
	if _, err := Uninstall(h.options(), "demo"); err == nil || !strings.Contains(err.Error(), old) {
		t.Fatal(err)
	}
	if !exist(old) {
		t.Fatal("removed old")
	}
}

// <Work> にほかのものがあれば、uninstall は <Work> を消さない。<Plug-Ins> の直下のファイルや、
// 名前の違う記録のフォルダは remaining に載せない。
func TestUninstallKeepsOtherThings(t *testing.T) {
	h := newHarness(t)
	h.installCLI()
	h.mustInstall(release{name: "demo", tag: "d1"})
	os.WriteFile(filepath.Join(h.plugins, "notes.txt"), nil, 0o644)
	copyDir(t, filepath.Join(h.plugins, "demo"), filepath.Join(h.plugins, "copy"))
	other := filepath.Join(h.work, "keep.txt")
	os.WriteFile(other, nil, 0o600)
	result, err := Uninstall(h.options(), "demo")
	if err != nil || strings.Join(result.Remaining, ",") != "cli" {
		t.Fatalf("%+v %v", result, err)
	}
	if !exist(other) {
		t.Fatal("removed something else in the work folder")
	}
	if names := installedIn(filepath.Join(h.tmp, "missing")); len(names) != 0 {
		t.Fatal(names)
	}
}

// 走らせている CLI がフォルダの外にあれば、よけない。
func TestSidestepOutside(t *testing.T) {
	h := newHarness(t)
	h.installCLI()
	outside := filepath.Join(h.tmp, "elsewhere", "vw2026")
	os.MkdirAll(filepath.Dir(outside), 0o755)
	os.WriteFile(outside, nil, 0o700)
	o := h.options()
	o.Executable = outside
	if moved, err := o.sidestep(filepath.Join(h.plugins, "cli"), filepath.Join(h.work, "old")); err != nil || moved != nil {
		t.Fatalf("%+v %v", moved, err)
	}
	if !exist(outside) {
		t.Fatal("moved the CLI outside the folder")
	}
}

// 記録は、版・名前・出どころ・タグ・ターゲット・モジュールと正しいフックがそろって読める。
func TestReadRecordRefuses(t *testing.T) {
	for name, body := range map[string]string{
		"no version":    `{"name":"a","registry":"default","tag":"t","target":"macos-arm64","module":"a.vwlibrary"}`,
		"no module":     `{"record_version":1,"name":"a","registry":"default","tag":"t","target":"macos-arm64"}`,
		"bad tag":       `{"record_version":1,"name":"a","registry":"default","tag":"../x","target":"macos-arm64","module":"a.vwlibrary"}`,
		"bad hook":      `{"record_version":1,"name":"a","registry":"default","tag":"t","target":"macos-arm64","module":"a.vwlibrary","hooks":{"install":["sh","../x"]}}`,
		"not an object": `[]`,
	} {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, RecordFile), []byte(body), 0o644)
		if _, err := readRecord(dir); err == nil {
			t.Errorf("%s: should not be readable", name)
		}
	}
}

// 名前が見つかる前の --registry が読めない（壊れている）なら、そこで失敗にする。
func TestLookupBrokenRegistry(t *testing.T) {
	h := newHarness(t)
	broken := filepath.Join(h.tmp, "broken.json")
	os.WriteFile(broken, []byte(`{"registry_version":1,"plugins":{"x":{"manifests":[]}}}`), 0o644)
	o := h.options()
	o.Registries = []string{broken}
	if _, _, err := o.lookup("other"); err == nil || !strings.Contains(err.Error(), broken) {
		t.Fatal(err)
	}
	o.Registries = []string{""}
	if _, _, err := o.lookup("other"); !IsUsage(err) {
		t.Fatal(err)
	}
}
