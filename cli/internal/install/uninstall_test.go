package install

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestUninstall(t *testing.T) {
	h := newHarness(t)
	h.installCLI()
	h.mustInstall(release{name: "demo", tag: "d1"})
	result, err := Uninstall(h.options(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != OutcomeUninstalled || string(result.Hook) != `{"ran":"uninstall"}` ||
		!reflect.DeepEqual(result.Remaining, []string{"cli"}) || len(result.Left) != 0 {
		t.Fatalf("%+v", result)
	}
	if exist(filepath.Join(h.plugins, "demo")) || exist(filepath.Join(h.work, "old", "demo")) ||
		exist(filepath.Join(h.work, "old", "demo.uninstalling")) {
		t.Fatal("left something")
	}
	// 記録のフックを、移した先で走らせる。
	runs := h.hookRuns("demo")
	last := runs[len(runs)-1]
	old := filepath.Join(h.work, "old", "demo")
	if last.Env["VW2026_HOOK_PLUGIN_DIR"] != old || last.Env["VW2026_HOOK_INSTALL_DIR"] != filepath.Join(h.plugins, "demo") ||
		last.Env["VW2026_HOOK_TAG"] != "d1" || last.Env["VW2026_HOOK_PREVIOUS_TAG"] != "" || !strings.HasSuffix(last.Dir, "demo") {
		t.Fatalf("%+v", last)
	}
	again, err := Uninstall(h.options(), "demo")
	if err != nil || again.Outcome != OutcomeNotInstall {
		t.Fatalf("%+v %v", again, err)
	}
}

// uninstall cli のあと、<Work> に空の staging / old しか無ければ <Work> も消す。remaining は
// どの名前でも出す。
func TestUninstallCLIRemovesTheWorkFolder(t *testing.T) {
	h := newHarness(t)
	h.installCLI()
	result, err := Uninstall(h.options(), "cli")
	if err != nil || result.Outcome != OutcomeUninstalled || len(result.Remaining) != 0 {
		t.Fatalf("%+v %v", result, err)
	}
	if exist(h.work) {
		t.Fatal("left the work folder")
	}
}

func TestUninstallWhileRunning(t *testing.T) {
	h := newHarness(t)
	h.installCLI()
	h.holdLock()
	before := snapshot(t, h.plugins)
	result, err := Uninstall(h.options(), "cli")
	if !errors.Is(err, ErrRunning) || result.Outcome != OutcomeRunning {
		t.Fatalf("%+v %v", result, err)
	}
	sameSnapshot(t, before, snapshot(t, h.plugins))
}

// 安全弁: 記録が無い・名前が違う・モジュールが無いフォルダは消さない。
func TestUninstallRefusesForeignFolders(t *testing.T) {
	for name, prepare := range map[string]func(t *testing.T, dir string){
		"no record": func(t *testing.T, dir string) { os.Remove(filepath.Join(dir, RecordFile)) },
		"name": func(t *testing.T, dir string) {
			rewriteRecord(t, filepath.Join(dir, RecordFile), func(r *Record) { r.Name = "other" })
		},
		"module": func(t *testing.T, dir string) { os.RemoveAll(filepath.Join(dir, "demo.vwlibrary")) },
		"module path": func(t *testing.T, dir string) {
			rewriteRecord(t, filepath.Join(dir, RecordFile), func(r *Record) { r.Module = "../cli/cli.vwlibrary" })
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			h.installCLI()
			h.mustInstall(release{name: "demo", tag: "d1"})
			prepare(t, filepath.Join(h.plugins, "demo"))
			before := snapshot(t, h.plugins)
			if _, err := Uninstall(h.options(), "demo"); err == nil {
				t.Fatal("should fail")
			}
			sameSnapshot(t, before, snapshot(t, h.plugins))
			if len(h.hookRuns("demo")) != 1 {
				t.Fatal("ran the uninstall hook")
			}
		})
	}
}

// フォルダを移せなければ、フックを走らせず、何も変えない。
func TestUninstallRenameFailure(t *testing.T) {
	h := newHarness(t)
	h.installCLI()
	failRename(t, filepath.Join(h.plugins, "cli"))
	before := snapshot(t, h.plugins)
	if _, err := Uninstall(h.options(), "cli"); err == nil || !strings.Contains(err.Error(), "current directory") {
		t.Fatal(err)
	}
	sameSnapshot(t, before, snapshot(t, h.plugins))
	if len(h.hookRuns("cli")) != 1 || exist(filepath.Join(h.work, "old", "cli.uninstalling")) {
		t.Fatal("ran the hook or left the marker")
	}
}

// フックが失敗したら、フォルダを戻し、印を消す。
func TestUninstallHookFailureRestores(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1", uninstall: &hookPlan{Exit: 1}})
	before := snapshot(t, h.plugins)
	if _, err := Uninstall(h.options(), "cli"); err == nil {
		t.Fatal("should fail")
	}
	sameSnapshot(t, before, snapshot(t, h.plugins))
	if exist(filepath.Join(h.work, "old", "cli.uninstalling")) || exist(filepath.Join(h.work, "old", "cli")) {
		t.Fatal("left the marker or old")
	}
}

// 手順 3 のあとで止まった uninstall: 印があれば手順 4 から続け、印が無ければ断る。
func TestUninstallResumes(t *testing.T) {
	h := newHarness(t)
	h.installCLI()
	h.mustInstall(release{name: "demo", tag: "d1"})
	old := filepath.Join(h.work, "old", "demo")
	marker := filepath.Join(h.work, "old", "demo.uninstalling")
	if err := os.Rename(filepath.Join(h.plugins, "demo"), old); err != nil {
		t.Fatal(err)
	}
	if _, err := Uninstall(h.options(), "demo"); err == nil || !strings.Contains(err.Error(), old) {
		t.Fatalf("without the marker: %v", err)
	}
	if !exist(old) {
		t.Fatal("removed old without the marker")
	}
	os.WriteFile(marker, nil, 0o600)
	result, err := Uninstall(h.options(), "demo")
	if err != nil || result.Outcome != OutcomeUninstalled || exist(old) || exist(marker) {
		t.Fatalf("%+v %v", result, err)
	}
	if runs := h.hookRuns("demo"); runs[len(runs)-1].Env["VW2026_HOOK_PLUGIN_DIR"] != old {
		t.Fatalf("%+v", runs)
	}
}

// 続きから走った回でフックが失敗しても、<Plug-Ins> へ戻さない（一部だけ残ったフォルダかもしれない）。
func TestUninstallResumedHookFailureDoesNotRestore(t *testing.T) {
	h := newHarness(t)
	h.installCLI()
	h.mustInstall(release{name: "demo", tag: "d1", uninstall: &hookPlan{Exit: 1}})
	old := filepath.Join(h.work, "old", "demo")
	marker := filepath.Join(h.work, "old", "demo.uninstalling")
	os.Rename(filepath.Join(h.plugins, "demo"), old)
	os.WriteFile(marker, nil, 0o600)
	if _, err := Uninstall(h.options(), "demo"); err == nil || !strings.Contains(err.Error(), old) {
		t.Fatal(err)
	}
	if exist(filepath.Join(h.plugins, "demo")) || !exist(old) || !exist(marker) {
		t.Fatal("should leave old and the marker as they are")
	}
}

// 前の回の残りの old/<name>/ があれば、uninstall は消さずに断る（守ること 9。install のフックが失敗して
// Vectorworks が起動したときの前の版かもしれない）。
func TestUninstallRefusesLeftoverOld(t *testing.T) {
	h := newHarness(t)
	h.installCLI()
	h.mustInstall(release{name: "demo", tag: "d1"})
	old := filepath.Join(h.work, "old", "demo")
	copyDir(t, filepath.Join(h.plugins, "demo"), old)
	before := snapshot(t, h.plugins)
	if _, err := Uninstall(h.options(), "demo"); err == nil || !strings.Contains(err.Error(), old) {
		t.Fatal(err)
	}
	sameSnapshot(t, before, snapshot(t, h.plugins))
	if !exist(filepath.Join(old, "version.txt")) || exist(filepath.Join(h.work, "old", "demo.uninstalling")) {
		t.Fatal("removed old or left the marker")
	}
}

// 名前のフォルダが無く、印だけが残っていれば、入っていない（印は消す）。
func TestUninstallStaleMarker(t *testing.T) {
	h := newHarness(t)
	h.installCLI()
	marker := filepath.Join(h.work, "old", "demo.uninstalling")
	os.MkdirAll(filepath.Dir(marker), 0o700)
	os.WriteFile(marker, nil, 0o600)
	result, err := Uninstall(h.options(), "demo")
	if err != nil || result.Outcome != OutcomeNotInstall || !reflect.DeepEqual(result.Remaining, []string{"cli"}) || exist(marker) {
		t.Fatalf("%+v %v", result, err)
	}
}

// 走らせている CLI が外すフォルダの中にあれば（入れた cli の vw2026 で uninstall cli）、何も変えずに
// ErrRunsInside で断る（フォルダの外の CLI、初回のスクリプトで外す）。リンクを通して走らせても同じ。
func TestUninstallFromInsideTheFolder(t *testing.T) {
	h := newHarness(t)
	h.installCLI()
	before := snapshot(t, h.plugins)
	o := h.options()
	o.Executable = filepath.Join(h.plugins, "cli", "bin", "vw2026")
	if _, err := Uninstall(o, "cli"); !errors.Is(err, ErrRunsInside) {
		t.Fatal(err)
	}
	sameSnapshot(t, before, snapshot(t, h.plugins))
	if len(h.hookRuns("cli")) != 1 || exist(filepath.Join(h.work, "old", "cli.uninstalling")) {
		t.Fatal("ran the hook or left the marker")
	}
	// フォルダの外の CLI なら外せる。
	o.Executable = filepath.Join(h.tmp, "elsewhere", "vw2026")
	if result, err := Uninstall(o, "cli"); err != nil || result.Outcome != OutcomeUninstalled {
		t.Fatalf("%+v %v", result, err)
	}
}
