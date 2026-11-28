package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/edition"
)

// fakeExecutable は <Plug-Ins>/cli/bin/vw2026（インストールした cli の CLI）。
func (h *harness) fakeExecutable() string {
	return filepath.Join(h.plugins, "cli", "bin", edition.Command)
}

// 実行中の CLI が差し替えるフォルダの中にあれば、<Work>/old/ へ退避してから差し替える。後処理で
// 削除する（mac では実行中でも削除できる）。
func TestInstallSidestepsTheRunningCLI(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	h.publish(release{name: "cli", tag: "c2"})
	// sidestep はリンクを解決した実行ファイルを移動する（mac の一時ディレクトリの /var は
	// /private/var へのリンク）ため、解決したパスで比較する。
	resolved, err := filepath.EvalSymlinks(h.fakeExecutable())
	if err != nil {
		t.Fatal(err)
	}
	var moved []string
	renameWhen(t, func(from, to string) error {
		if from == resolved {
			moved = append(moved, to)
		}
		return nil
	})
	o := h.options()
	o.Executable = h.fakeExecutable()
	if _, err := Install(o, "cli", "c2", false); err != nil {
		t.Fatal(err)
	}
	if len(moved) != 1 || !strings.HasPrefix(filepath.Base(moved[0]), oldCLIName) ||
		filepath.Dir(moved[0]) != filepath.Join(h.work, "old") {
		t.Fatalf("%v", moved)
	}
	if exists(moved[0]) {
		t.Fatal("the moved CLI was not deleted")
	}
}

// 削除できない退避した CLI（Windows で実行中のもの）は、install では黙って残置し、uninstall では
// left に含める。<Work>/old/ の直下の vw2026.old-* / vw2026.exe.old-* の他は削除しない。
func TestCleanOldCLIs(t *testing.T) {
	h := newHarness(t)
	h.installCLI()
	old := filepath.Join(h.work, "old")
	os.MkdirAll(old, 0o700)
	keep := []string{edition.Command, edition.Command + ".old", "other.old-1", "cli.uninstalling.old-x"}
	for _, name := range append(keep, edition.Command+".old-1", edition.Command+".exe.old-2", edition.Command+".old-busy") {
		os.WriteFile(filepath.Join(old, name), nil, 0o600)
	}
	os.Mkdir(filepath.Join(old, edition.Command+".old-dir"), 0o700)
	busy := filepath.Join(old, edition.Command+".old-busy")
	pathOpWhen(t, &removeFile, func(path string) error {
		if path == busy {
			return os.ErrPermission
		}
		return nil
	})
	h.mustInstall(release{name: "cli", tag: "c2"})
	if !exists(busy) || exists(filepath.Join(old, edition.Command+".old-1")) || exists(filepath.Join(old, edition.Command+".exe.old-2")) {
		t.Fatal("install should delete what it can")
	}
	for _, name := range append(keep, edition.Command+".old-dir") {
		if !exists(filepath.Join(old, name)) {
			t.Errorf("deleted %s", name)
		}
	}
	result, err := Uninstall(h.options(), "cli")
	if err != nil || len(result.Left) != 1 || result.Left[0] != busy {
		t.Fatalf("%+v %v", result, err)
	}
}

// 内部にあるか否かは、パスの文字列ではなくディレクトリの同一性で決定する。
func TestWithin(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "Plug-Ins", "cli")
	executable := filepath.Join(dir, "bin", edition.Command)
	os.MkdirAll(filepath.Dir(executable), 0o755)
	os.WriteFile(executable, nil, 0o700)
	outside := filepath.Join(tmp, "elsewhere", edition.Command)
	os.MkdirAll(filepath.Dir(outside), 0o755)
	os.WriteFile(outside, nil, 0o700)

	check := func(name, path, dir string, want bool) {
		t.Helper()
		rel, ok := within(path, dir)
		if ok != want || (ok && filepath.ToSlash(rel) != "bin/"+edition.Command) {
			t.Errorf("%s: %q %v", name, rel, ok)
		}
	}
	check("inside", executable, dir, true)
	check("outside", outside, dir, false)
	check("trailing separator", executable, dir+string(os.PathSeparator), true)
	check("dot", executable, filepath.Join(dir, "bin", ".."), true)
	check("missing folder", executable, filepath.Join(tmp, "missing"), false)
	link := filepath.Join(tmp, "link")
	if err := os.Symlink(filepath.Join(tmp, "Plug-Ins"), link); err == nil {
		check("through a link", executable, filepath.Join(link, "cli"), true)
	}
	// 大文字と小文字を区別しないボリューム（Windows・mac の既定の APFS）でのみ実行する。
	upper := filepath.Join(tmp, "PLUG-INS", "CLI")
	if _, err := os.Stat(upper); err == nil {
		check("case", executable, upper, true)
	} else {
		t.Log("the volume is case-sensitive; skipped the case check")
	}
}

// 実行ファイルがリンク（mac の ~/.local/bin/vw2026）ならば、解決した先で決定する。
func TestSidestepFollowsTheLink(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	link := filepath.Join(h.tmp, "local-bin", edition.Command)
	os.MkdirAll(filepath.Dir(link), 0o755)
	if err := os.Symlink(h.fakeExecutable(), link); err != nil {
		t.Skip("cannot make a symbolic link:", err)
	}
	o := h.options()
	o.Executable = link
	moved, err := o.sidestep(filepath.Join(h.plugins, "cli"), filepath.Join(h.work, "old"))
	if err != nil || moved == nil || moved.from() != h.fakeExecutable() || exists(h.fakeExecutable()) {
		t.Fatalf("%+v %v", moved, err)
	}
	if note := moved.restore(); note != "" || !exists(h.fakeExecutable()) {
		t.Fatal("not restored")
	}
}

// 退避していなければ、手動で復元する手順に CLI の場所を追加しない。
func TestMovedCLIWhereWhenNotMoved(t *testing.T) {
	var moved *movedCLI
	if got := moved.where(); got != "" {
		t.Fatal(got)
	}
}
