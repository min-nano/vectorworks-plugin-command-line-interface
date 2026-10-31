package install

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/registry"
)

func TestInstallFirstTime(t *testing.T) {
	h := newHarness(t)
	result := h.mustInstall(release{name: "cli", tag: "c1"})
	if result.Outcome != OutcomeInstalled || result.Tag != "c1" || result.Requested != "c1" ||
		result.Target != testTarget || result.Registry != "default" || result.PluginsDir != h.plugins ||
		string(result.Hook) != `{"ran":"install","tag":"c1"}` {
		t.Fatalf("%+v", result)
	}
	if h.installedVersion("cli") != "c1" {
		t.Fatal("not installed")
	}
	// 実行ビットを保つ。
	if info, err := os.Stat(filepath.Join(h.plugins, "cli", "bin", "vw2026")); err != nil || info.Mode().Perm()&0o100 == 0 && os.PathSeparator == '/' {
		t.Fatalf("%v %v", info, err)
	}
	record, err := readRecord(filepath.Join(h.plugins, "cli"))
	if err != nil {
		t.Fatal(err)
	}
	if record.Name != "cli" || record.Registry != "default" || record.Tag != "c1" || record.Target != testTarget ||
		record.Module != "cli.vwlibrary" || strings.Join(record.Hooks.Install, " ") != "sh hooks/install.hook" {
		t.Fatalf("%+v", record)
	}
	// 組み立ての場所は残らない。
	if exist(filepath.Join(h.work, "staging", "cli")) || exist(filepath.Join(h.work, "staging", "cli.zip")) ||
		exist(filepath.Join(h.work, "old", "cli")) {
		t.Fatal("left the staging or old folder")
	}
}

// フックの走らせ方: 作業ディレクトリはプラグインのフォルダで、VW2026_HOOK_* を渡す。sh の後ろの
// 引数はそのまま渡す。
func TestInstallHookEnvironment(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	hooks := registry.Hooks{Install: []string{"sh", "hooks/install.hook", "a b", "--x"}, Uninstall: []string{"sh", "hooks/uninstall.hook"}}
	t.Setenv("VW2026_HOOK_TAG", "from the caller")
	h.mustInstall(release{name: "cli", tag: "c2", hooks: &hooks})
	runs := h.hookRuns("cli")
	if len(runs) != 2 {
		t.Fatalf("%+v", runs)
	}
	dest := filepath.Join(h.plugins, "cli")
	first, second := runs[0], runs[1]
	if first.Env["VW2026_HOOK_PREVIOUS_TAG"] != "" || first.Env["VW2026_HOOK_TAG"] != "c1" {
		t.Errorf("first: %+v", first)
	}
	want := map[string]string{
		"VW2026_HOOK_NAME":         "cli",
		"VW2026_HOOK_TAG":          "c2",
		"VW2026_HOOK_PREVIOUS_TAG": "c1",
		"VW2026_HOOK_TARGET":       testTarget,
		"VW2026_HOOK_PLUGIN_DIR":   dest,
		"VW2026_HOOK_INSTALL_DIR":  dest,
	}
	for name, value := range want {
		if second.Env[name] != value {
			t.Errorf("%s = %q, want %q", name, second.Env[name], value)
		}
	}
	if !sameDir(t, second.Dir, dest) || strings.Join(second.Args, "|") != "a b|--x" {
		t.Errorf("second: %+v", second)
	}
}

func sameDir(t *testing.T, a, b string) bool {
	t.Helper()
	ia, err1 := os.Stat(a)
	ib, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(ia, ib)
}

func TestInstallUpdatesAndCleansUp(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	result := h.mustInstall(release{name: "cli", tag: "c2"})
	if result.Outcome != OutcomeInstalled || h.installedVersion("cli") != "c2" {
		t.Fatalf("%+v", result)
	}
	for _, path := range []string{"staging/cli", "staging/cli.zip", "old/cli"} {
		if exist(filepath.Join(h.work, filepath.FromSlash(path))) {
			t.Errorf("left %s", path)
		}
	}
}

// 同じタグなら、zip もマニフェストも取らず（タグで直接指す規則でターゲットも同じ）、付け替えずに
// 記録に写したフックだけを走らせる。
func TestInstallSameTagRunsOnlyTheRecordedHook(t *testing.T) {
	h := newHarness(t)
	zip := h.publish(release{name: "cli", tag: "c1"})
	if _, err := h.install("cli", "c1"); err != nil {
		t.Fatal(err)
	}
	// 提供側が同じ場所のマニフェストを差し替えても、入れていないフックは走らない。
	other := hookPlan{Stdout: `{"ran":"replaced"}`}
	h.publish(release{name: "cli", tag: "c1", install: &other})
	before := snapshot(t, h.plugins)
	result, err := h.install("cli", "c1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != OutcomeUpToDate || string(result.Hook) != `{"ran":"install","tag":"c1"}` {
		t.Fatalf("%+v", result)
	}
	if h.hit("/cli/c1/vw2026.json") != 1 || h.hit(zip) != 1 {
		t.Fatalf("fetched again: manifest %d, zip %d", h.hit("/cli/c1/vw2026.json"), h.hit(zip))
	}
	sameSnapshot(t, before, snapshot(t, h.plugins))
	runs := h.hookRuns("cli")
	if len(runs) != 2 || runs[1].Env["VW2026_HOOK_PREVIOUS_TAG"] != "c1" || runs[1].Env["VW2026_HOOK_TAG"] != "c1" {
		t.Fatalf("%+v", runs)
	}
}

// latest のように決まった場所を指す規則では、今のタグを知るためにマニフェストを取るが、zip は取らない。
func TestInstallLatestFetchesOnlyTheManifest(t *testing.T) {
	h := newHarness(t)
	zip := h.publish(release{name: "cli", tag: "c1", latest: true})
	result, err := h.install("cli", "latest")
	if err != nil || result.Outcome != OutcomeInstalled || result.Tag != "c1" || result.Requested != "latest" {
		t.Fatalf("%+v %v", result, err)
	}
	result, err = h.install("cli", "latest")
	if err != nil || result.Outcome != OutcomeUpToDate || result.Tag != "c1" {
		t.Fatalf("%+v %v", result, err)
	}
	if h.hit("/cli/latest/vw2026.json") != 2 || h.hit(zip) != 1 {
		t.Fatalf("manifest %d, zip %d", h.hit("/cli/latest/vw2026.json"), h.hit(zip))
	}
}

// 同じタグでもターゲットが違えば、マニフェストを取って入れ直す。
func TestInstallSameTagOtherTargetReinstalls(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	path := filepath.Join(h.plugins, "cli", RecordFile)
	rewriteRecord(t, path, func(r *Record) { r.Target = "macos-x64" })
	result, err := h.install("cli", "c1")
	if err != nil || result.Outcome != OutcomeInstalled {
		t.Fatalf("%+v %v", result, err)
	}
	if h.hit("/cli/c1/vw2026.json") != 2 {
		t.Fatal("the manifest was not fetched")
	}
}

// 記録の module が無ければ（ウイルス対策が隔離したなど）、同じタグでも入れ直す。
func TestInstallSameTagWithoutModuleReinstalls(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	if err := os.RemoveAll(filepath.Join(h.plugins, "cli", "cli.vwlibrary")); err != nil {
		t.Fatal(err)
	}
	result, err := h.install("cli", "c1")
	if err != nil || result.Outcome != OutcomeInstalled || !exist(filepath.Join(h.plugins, "cli", "cli.vwlibrary")) {
		t.Fatalf("%+v %v", result, err)
	}
}

func rewriteRecord(t *testing.T, path string, change func(*Record)) {
	t.Helper()
	record, err := readRecord(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	change(record)
	data, _ := json.Marshal(record)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestInstallCheck(t *testing.T) {
	h := newHarness(t)
	h.publish(release{name: "cli", tag: "c1", latest: true})
	check := func(tag string) *InstallResult {
		t.Helper()
		result, err := Install(h.options(), "cli", tag, true)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	if r := check("latest"); r.Outcome != OutcomeAvailable || r.Tag != "c1" {
		t.Fatalf("nothing installed: %+v", r)
	}
	if exist(filepath.Join(h.plugins, "cli")) || exist(h.work) {
		t.Fatal("--check changed something")
	}
	h.mustInstall(release{name: "cli", tag: "c1", latest: true})
	// --check は Vectorworks が動いていても答える。
	h.holdLock()
	before := snapshot(t, h.plugins)
	if r := check("latest"); r.Outcome != OutcomeUpToDate {
		t.Fatalf("%+v", r)
	}
	h.publish(release{name: "cli", tag: "c2", latest: true})
	if r := check("latest"); r.Outcome != OutcomeAvailable || r.Tag != "c2" {
		t.Fatalf("%+v", r)
	}
	sameSnapshot(t, before, snapshot(t, h.plugins))
	if len(h.hookRuns("cli")) != 1 {
		t.Fatal("--check ran a hook")
	}
}

// Vectorworks が動いていれば、同じタグでもフックを走らせず、何も変えない。
func TestInstallWhileRunning(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	h.publish(release{name: "cli", tag: "c2"})
	h.holdLock()
	before := snapshot(t, h.plugins)
	for _, tag := range []string{"c1", "c2"} {
		result, err := h.install("cli", tag)
		if !errors.Is(err, ErrRunning) || result == nil || result.Outcome != OutcomeRunning {
			t.Fatalf("%s: %+v %v", tag, result, err)
		}
	}
	if h.hit("/cli/c2/cli-"+testTarget+".zip") != 0 {
		t.Fatal("fetched the zip while running")
	}
	sameSnapshot(t, before, snapshot(t, h.plugins))
	if len(h.hookRuns("cli")) != 1 {
		t.Fatal("ran a hook while running")
	}
}

// 手順 4 で見たあと、zip を取る間に起動した Vectorworks も、付け替えの直前に見つけて何も変えない。
func TestInstallLockTakenWhileDownloading(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	zip := h.publish(release{name: "cli", tag: "c2"})
	h.onGet(zip, func() { h.holdLock() })
	before := snapshot(t, h.plugins)
	result, err := h.install("cli", "c2")
	if !errors.Is(err, ErrRunning) || result.Outcome != OutcomeRunning {
		t.Fatalf("%+v %v", result, err)
	}
	sameSnapshot(t, before, snapshot(t, h.plugins))
	if exist(filepath.Join(h.work, "staging", "cli")) || exist(filepath.Join(h.work, "staging", "cli.zip")) {
		t.Fatal("left the staging folder")
	}
}

func TestInstallBadZipChangesNothing(t *testing.T) {
	module := entry{name: "demo.vwlibrary/x", body: "m"}
	for name, r := range map[string]release{
		"sha256":           {sha: strings.Repeat("0", 64)},
		"no module":        {entries: []entry{{name: "a.txt", body: "a"}}},
		"two modules":      {entries: []entry{module, {name: "other.vwlibrary/x", body: "m"}}},
		"windows module":   {entries: []entry{{name: "demo.vlb", body: "m"}}},
		"symbolic link":    {extra: []entry{{name: "link", body: "/etc/passwd", mode: os.ModeSymlink | 0o777}}},
		"dotdot":           {extra: []entry{{name: "../escape", body: "x"}}},
		"dotdot inside":    {extra: []entry{{name: "a/../../escape", body: "x"}}},
		"absolute":         {extra: []entry{{name: "/escape", body: "x"}}},
		"backslash":        {extra: []entry{{name: `hooks\x`, body: "x"}}},
		"duplicate":        {extra: []entry{{name: "version.txt", body: "again"}}},
		"no install hook":  {hooks: &registry.Hooks{Install: []string{"sh", "hooks/missing.hook"}}},
		"no uninstall":     {hooks: &registry.Hooks{Uninstall: []string{"powershell", "hooks/missing.ps1"}}},
		"hook is a folder": {hooks: &registry.Hooks{Install: []string{"sh", "hooks"}}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			h.installCLI()
			h.mustInstall(release{name: "demo", tag: "d1"})
			r.name, r.tag = "demo", "d2"
			if r.entries != nil {
				r.entries = append(r.entries, entry{name: "version.txt", body: "d2"})
			}
			h.publish(r)
			before := snapshot(t, h.plugins)
			if result, err := h.install("demo", "d2"); err == nil {
				t.Fatalf("%+v", result)
			}
			sameSnapshot(t, before, snapshot(t, h.plugins))
			if exist(filepath.Join(h.tmp, "escape")) || exist(filepath.Join(h.work, "escape")) {
				t.Fatal("wrote outside the folder")
			}
			if exist(filepath.Join(h.work, "staging", "demo")) || exist(filepath.Join(h.work, "staging", "demo.zip")) {
				t.Fatal("left the staging folder")
			}
		})
	}
}

// 入れる先に、記録の無い・読めない・別の名前・別のレジストリのフォルダがあれば、何も変えない。
func TestInstallRefusesForeignFolders(t *testing.T) {
	for name, prepare := range map[string]func(t *testing.T, dir string){
		"no record": func(t *testing.T, dir string) { os.Remove(filepath.Join(dir, RecordFile)) },
		"broken":    func(t *testing.T, dir string) { os.WriteFile(filepath.Join(dir, RecordFile), []byte("{"), 0o644) },
		"version": func(t *testing.T, dir string) {
			os.WriteFile(filepath.Join(dir, RecordFile), []byte(`{"record_version":2,"name":"demo"}`), 0o644)
		},
		"name": func(t *testing.T, dir string) {
			rewriteRecord(t, filepath.Join(dir, RecordFile), func(r *Record) { r.Name = "other" })
		},
		"registry": func(t *testing.T, dir string) {
			rewriteRecord(t, filepath.Join(dir, RecordFile), func(r *Record) { r.Registry = "/elsewhere.json" })
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			h.installCLI()
			h.mustInstall(release{name: "demo", tag: "d1"})
			prepare(t, filepath.Join(h.plugins, "demo"))
			h.publish(release{name: "demo", tag: "d2"})
			before := snapshot(t, h.plugins)
			result, err := h.install("demo", "d2")
			if err == nil || !strings.Contains(err.Error(), filepath.Join(h.plugins, "demo")) {
				t.Fatalf("%+v %v", result, err)
			}
			sameSnapshot(t, before, snapshot(t, h.plugins))
		})
	}
}

// cli 以外は、cli が入っている <Plug-Ins> にだけ入れ、そこからだけ外す（Vectorworks が動いているかを
// <Plug-Ins>/cli/spool のロックで判定するため）。
func TestOtherPluginNeedsCLI(t *testing.T) {
	h := newHarness(t)
	h.publish(release{name: "demo", tag: "d1"})
	if _, err := h.install("demo", "d1"); err == nil || !strings.Contains(err.Error(), "has no cli") {
		t.Fatal(err)
	}
	if exist(filepath.Join(h.plugins, "demo")) {
		t.Fatal("installed without cli")
	}
	h.installCLI()
	h.mustInstall(release{name: "demo", tag: "d1"})
	os.RemoveAll(filepath.Join(h.plugins, "cli", "cli.vwlibrary"))
	if _, err := Uninstall(h.options(), "demo"); err == nil || !strings.Contains(err.Error(), "has no cli") {
		t.Fatal(err)
	}
	if !exist(filepath.Join(h.plugins, "demo")) {
		t.Fatal("uninstalled without cli")
	}
}

// 付け替えの 1 つ目（<Plug-Ins>/<name> を移す）と 2 つ目（組み立てたものを置く）の失敗で元に戻る。
func TestInstallRenameFailureRestores(t *testing.T) {
	for name, from := range map[string]func(h *harness) string{
		"first":  func(h *harness) string { return filepath.Join(h.plugins, "cli") },
		"second": func(h *harness) string { return filepath.Join(h.work, "staging", "cli") },
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.mustInstall(release{name: "cli", tag: "c1"})
			h.publish(release{name: "cli", tag: "c2"})
			executable := filepath.Join(h.plugins, "cli", "bin", "vw2026")
			failRename(t, from(h))
			before := snapshot(t, h.plugins)
			o := h.options()
			o.Executable = executable
			if _, err := Install(o, "cli", "c2", false); err == nil {
				t.Fatal("should fail")
			}
			sameSnapshot(t, before, snapshot(t, h.plugins))
			if len(oldCLIs(t, h)) != 0 {
				t.Fatal("the moved CLI was not put back")
			}
		})
	}
}

// oldCLIs は <Work>/old/ の直下のよけた CLI。
func oldCLIs(t *testing.T, h *harness) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(h.work, "old", "vw2026*.old-*"))
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

// フックが失敗したら前の版へ戻す（最初に入れたときは何も残さない）。標準出力が JSON のオブジェクト
// 1 つでないときも失敗。
func TestInstallHookFailureRestores(t *testing.T) {
	for name, plan := range map[string]hookPlan{
		"exit":        {Exit: 3},
		"not JSON":    {Stdout: "done"},
		"two values":  {Stdout: `{"a":1}{"b":2}`},
		"array":       {Stdout: `[1]`},
		"broken":      {Stdout: `{"a":`},
		"invalid utf": {Raw: []byte("{\"a\":\"\xff\"}")},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			plan := plan
			if result, err := h.install("cli", "c0"); err == nil {
				t.Fatalf("%+v", result)
			}
			h.publish(release{name: "cli", tag: "c0", install: &plan})
			if _, err := h.install("cli", "c0"); err == nil {
				t.Fatal("should fail")
			}
			if exist(filepath.Join(h.plugins, "cli")) {
				t.Fatal("left the first install")
			}
			h.mustInstall(release{name: "cli", tag: "c1"})
			h.publish(release{name: "cli", tag: "c2", install: &plan})
			executable := filepath.Join(h.plugins, "cli", "bin", "vw2026")
			before := snapshot(t, h.plugins)
			o := h.options()
			o.Executable = executable
			if _, err := Install(o, "cli", "c2", false); err == nil {
				t.Fatal("should fail")
			}
			sameSnapshot(t, before, snapshot(t, h.plugins))
			if exist(filepath.Join(h.work, "old", "cli")) || exist(filepath.Join(h.work, "staging", "cli")) {
				t.Fatal("left old or staging")
			}
		})
	}
}

// フックの出力: 空なら {}、BOM と前後の空白は許す。
func TestHookOutput(t *testing.T) {
	for in, want := range map[string]string{
		"":                             "{}",
		" \n":                          "{}",
		"\xef\xbb\xbf{\"a\": 1}":       `{"a":1}`,
		"\r\n{\n \"a\": [1, 2]\n}\r\n": `{"a":[1,2]}`,
	} {
		got, err := hookOutput([]byte(in))
		if err != nil || string(got) != want {
			t.Errorf("%q: %s %v", in, got, err)
		}
	}
}

// フックが走る間に起動した Vectorworks が新しい版を読み込んでロックを掴んだら、戻さずに新しい版を残す。
func TestInstallHookFailureWhileRunningKeepsTheNewVersion(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	signal := filepath.Join(h.tmp, "hook-started")
	await := filepath.Join(h.tmp, "locked")
	plan := hookPlan{Exit: 1, Signal: signal, Await: await}
	h.publish(release{name: "cli", tag: "c2", install: &plan})
	done := make(chan struct{})
	var release func()
	go func() {
		defer close(done)
		// フックの合図を待つ
		for !exist(signal) {
			time.Sleep(10 * time.Millisecond)
		}
		release = h.holdLock()
		os.WriteFile(await, nil, 0o600)
	}()
	_, err := h.install("cli", "c2")
	<-done
	if err == nil || !strings.Contains(err.Error(), "Vectorworks started") {
		t.Fatal(err)
	}
	old := filepath.Join(h.work, "old", "cli")
	if h.installedVersion("cli") != "c2" || !exist(old) {
		t.Fatal("the new version should stay, and the previous one in old")
	}
	release()
	// Vectorworks を終了しても、install を終えるまで uninstall は old を消さずに断り、同じタグの install を示す。
	if _, err := Uninstall(h.options(), "cli"); err == nil || !strings.Contains(err.Error(), "install cli@c2") {
		t.Fatal(err)
	}
	// 同じタグで走らせ直すと、フックだけが走り、退避した前の版を片付ける。
	os.WriteFile(filepath.Join(h.plugins, "cli", "hooks", "install.hook"), []byte(planJSON(t, hookPlan{Stdout: "{}"})), 0o644)
	if result, err := h.install("cli", "c2"); err != nil || result.Outcome != OutcomeUpToDate {
		t.Fatalf("%+v %v", result, err)
	}
	if exist(old) {
		t.Fatal("left the previous version")
	}
	if result, err := Uninstall(h.options(), "cli"); err != nil || result.Outcome != OutcomeUninstalled {
		t.Fatalf("%+v %v", result, err)
	}
}

// 同じタグでフックだけを走らせた回で退避した前の版を片付けられなくても、up_to_date のまま知らせる。
func TestInstallSameTagCannotSettleOld(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	old := filepath.Join(h.work, "old", "cli")
	copyDir(t, filepath.Join(h.plugins, "cli"), old)
	failRename(t, old)
	var stderr bytes.Buffer
	o := h.options()
	o.Stderr = &stderr
	if result, err := Install(o, "cli", "c1", false); err != nil || result.Outcome != OutcomeUpToDate {
		t.Fatalf("%+v %v", result, err)
	}
	if !exist(old) || !strings.Contains(stderr.String(), old) {
		t.Fatalf("should leave old and say so: %s", stderr.String())
	}
}

// 付け替えに成功した install は、残っていた中断した uninstall の印を消す。
func TestInstallRemovesUninstallMarker(t *testing.T) {
	h := newHarness(t)
	marker := filepath.Join(h.work, "old", "cli.uninstalling")
	os.MkdirAll(filepath.Dir(marker), 0o700)
	os.WriteFile(marker, nil, 0o600)
	h.mustInstall(release{name: "cli", tag: "c1"})
	if exist(marker) {
		t.Fatal("left the marker")
	}
}

// 前の回が残した <Work>/old/<name>/: <Plug-Ins>/<name>/ があり記録が正しければ消し、記録が正しくなければ
// 脇へ移す。<Plug-Ins>/<name>/ が無ければ何も変えずに断る。
func TestInstallLeftoverOld(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	old := filepath.Join(h.work, "old", "cli")
	copyDir(t, filepath.Join(h.plugins, "cli"), old)
	h.mustInstall(release{name: "cli", tag: "c2"})
	if exist(old) {
		t.Fatal("left old")
	}

	copyDir(t, filepath.Join(h.plugins, "cli"), old)
	os.WriteFile(filepath.Join(old, RecordFile), []byte("{"), 0o644)
	h.mustInstall(release{name: "cli", tag: "c3"})
	kept, _ := filepath.Glob(filepath.Join(h.work, "old", "cli.kept-*"))
	if exist(old) || len(kept) != 1 || !exist(filepath.Join(kept[0], RecordFile)) {
		t.Fatalf("should move old aside: %v", kept)
	}

	// <Plug-Ins>/<name>/ が無ければ、old が唯一の版かもしれない。
	if err := os.Rename(filepath.Join(h.plugins, "cli"), old); err != nil {
		t.Fatal(err)
	}
	h.publish(release{name: "cli", tag: "c4"})
	if _, err := h.install("cli", "c4"); err == nil || !strings.Contains(err.Error(), "only copy") {
		t.Fatal(err)
	}
	if !exist(filepath.Join(old, "version.txt")) || exist(filepath.Join(h.plugins, "cli")) {
		t.Fatal("changed something")
	}
	// 印もあれば、止まった uninstall の続きを示す。
	os.WriteFile(filepath.Join(h.work, "old", "cli.uninstalling"), nil, 0o600)
	if _, err := h.install("cli", "c4"); err == nil || !strings.Contains(err.Error(), "run uninstall cli again") {
		t.Fatal(err)
	}
	if !exist(filepath.Join(old, "version.txt")) || exist(filepath.Join(h.plugins, "cli")) {
		t.Fatal("changed something")
	}
}

func copyDir(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.Walk(from, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, path)
		target := filepath.Join(to, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestInstallUsageErrors(t *testing.T) {
	h := newHarness(t)
	for _, c := range [][2]string{{"CLI", "latest"}, {"cli", ".."}, {"cli", "."}, {"cli", "-x"}, {"cli", ""}, {"con", "1"}} {
		if _, err := h.install(c[0], c[1]); !IsUsage(err) {
			t.Errorf("%v: %v", c, err)
		}
	}
}

// レジストリの順と規則の選び方。
func TestLookup(t *testing.T) {
	h := newHarness(t)
	h.installCLI()
	local := filepath.Join(h.tmp, "plugins.json")
	write := func(path, body string) {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	extra := func(name, path string) string {
		return `{"registry_version":1,"plugins":{"` + name + `":{"manifests":[{"match":"*","path":"` + path + `"}]}}}`
	}
	// 既定のレジストリの名前は上書きできない。
	write(local, extra("cli", "https://example.invalid/{tag}.json"))
	o := h.options()
	o.Registries = []string{local}
	entry, source, err := o.lookup("cli")
	if err != nil || source != "default" || entry.Manifests[0].Path != h.url("/cli/latest/vw2026.json") {
		t.Fatalf("%v %s %v", entry, source, err)
	}

	// 名前が見つかる前に取れないレジストリがあれば、そこで失敗にする（後ろに名前があっても）。
	h.serve("/registry.json", []byte(extra("other", h.url("/other/{tag}/vw2026.json"))))
	o.Registries = []string{h.url("/missing.json"), h.url("/registry.json")}
	if _, _, err := o.lookup("other"); err == nil {
		t.Fatal("should fail on the missing registry")
	}
	// 先に見つかった名前を使う。
	write(local, extra("other", "https://example.invalid/{tag}.json"))
	o.Registries = []string{h.url("/registry.json"), local}
	if _, source, err := o.lookup("other"); err != nil || source != h.url("/registry.json") {
		t.Fatalf("%s %v", source, err)
	}
	// https:// で取ったレジストリは手元のパスを指せない。
	h.serve("/local.json", []byte(extra("other", strings.ReplaceAll(filepath.Join(h.tmp, "{tag}.json"), `\`, "/"))))
	o.Registries = []string{h.url("/local.json")}
	if _, _, err := o.lookup("other"); err == nil {
		t.Fatal("a remote registry should not refer to a path")
	}
	// 見つからない名前。
	o.Registries = nil
	if _, _, err := o.lookup("nowhere"); err == nil {
		t.Fatal("should fail")
	}
}

// 合った規則のマニフェストが取れなければ、次の規則へ進まずに失敗にする。{tag} のある規則で
// マニフェストのタグが違えば失敗にする。
func TestInstallRuleSelection(t *testing.T) {
	h := newHarness(t)
	h.installCLI()
	registryPath := filepath.Join(h.tmp, "plugins.json")
	os.WriteFile(registryPath, []byte(`{"registry_version":1,"plugins":{"other":{"manifests":[`+
		`{"match":"stable","path":"`+h.url("/other/missing.json")+`"},`+
		`{"match":"*","path":"`+h.url("/other/{tag}/vw2026.json")+`"}]}}}`), 0o644)
	o := h.options()
	o.Registries = []string{registryPath}
	h.publish(release{name: "other", tag: "stable"})
	if _, err := Install(o, "other", "stable", false); err == nil || !strings.Contains(err.Error(), "missing.json") {
		t.Fatal(err)
	}
	// /other/o2/vw2026.json にタグ o1 のマニフェストを置く。
	h.publish(release{name: "other", tag: "o1"})
	h.mu.Lock()
	h.files["/other/o2/vw2026.json"] = h.files["/other/o1/vw2026.json"]
	h.mu.Unlock()
	if _, err := Install(o, "other", "o2", false); err == nil || !strings.Contains(err.Error(), `"o1"`) {
		t.Fatal(err)
	}
	// 入れた記録には、整えたレジストリの場所を残す。
	result, err := Install(o, "other", "o1", false)
	if err != nil || result.Registry != registryPath {
		t.Fatalf("%+v %v", result, err)
	}
	// 同じファイルを相対パスで渡しても、整えた文字列は同じ。
	t.Chdir(h.tmp)
	o.Registries = []string{"plugins.json"}
	h.publish(release{name: "other", tag: "o3"})
	if result, err := Install(o, "other", "o3", false); err != nil || result.Outcome != OutcomeInstalled {
		t.Fatalf("%+v %v", result, err)
	}
}

// マニフェストに今のターゲットが無ければ、何も変えずに失敗にする。
func TestInstallUnsupportedTarget(t *testing.T) {
	h := newHarness(t)
	h.publish(release{name: "cli", tag: "c1"})
	o := h.options()
	o.Target = "windows-x64"
	if _, err := Install(o, "cli", "c1", false); err == nil || !strings.Contains(err.Error(), "windows-x64") {
		t.Fatal(err)
	}
}

// 手元のパスのマニフェストと資産も取れる（社内のファイル共有など）。
func TestInstallFromPaths(t *testing.T) {
	h := newHarness(t)
	h.installCLI()
	zip := makeZip(t, []entry{{name: "local.vwlibrary/x", body: "m"}, {name: "version.txt", body: "l1"}})
	zipPath := filepath.Join(h.tmp, "local.zip")
	os.WriteFile(zipPath, zip, 0o644)
	sum := sha256Hex(zip)
	manifest := filepath.Join(h.tmp, "l1.json")
	os.WriteFile(manifest, []byte(`{"manifest_version":1,"tag":"l1","targets":{"`+testTarget+`":{"asset":`+
		jsonString(zipPath)+`,"sha256":"`+sum+`"}}}`), 0o644)
	registryPath := filepath.Join(h.tmp, "plugins.json")
	os.WriteFile(registryPath, []byte(`{"registry_version":1,"plugins":{"local":{"manifests":[{"match":"*","path":`+
		jsonString(filepath.Join(h.tmp, "{tag}.json"))+`}]}}}`), 0o644)
	o := h.options()
	o.Registries = []string{registryPath}
	result, err := Install(o, "local", "l1", false)
	if err != nil || result.Outcome != OutcomeInstalled || result.Hook != nil || h.installedVersion("local") != "l1" {
		t.Fatalf("%+v %v", result, err)
	}
}

func jsonString(s string) string {
	data, _ := json.Marshal(s)
	return string(data)
}
