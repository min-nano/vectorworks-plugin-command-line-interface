package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeExecutable は <Plug-Ins>/cli/bin/vw2026（入れた cli の CLI）。
func (h *harness) fakeExecutable() string {
	return filepath.Join(h.plugins, "cli", "bin", "vw2026")
}

// 走らせている CLI が付け替えるフォルダの中にあれば、<Work>/old/ へよけてから付け替える。
// 後始末で消す（mac では動いていても消せる）。
func TestInstallSidestepsTheRunningCLI(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	h.publish(release{name: "cli", tag: "c2"})
	// sidestep はリンクを解いた実行ファイルを移す（mac の一時ディレクトリの /var は /private/var への
	// リンク）ので、解いたパスで比べる。
	resolved, err := filepath.EvalSymlinks(h.fakeExecutable())
	if err != nil {
		t.Fatal(err)
	}
	var moved []string
	original := rename
	rename = func(from, to string) error {
		if from == resolved {
			moved = append(moved, to)
		}
		return original(from, to)
	}
	t.Cleanup(func() { rename = original })
	o := h.options()
	o.Executable = h.fakeExecutable()
	if _, err := Install(o, "cli", "c2", false); err != nil {
		t.Fatal(err)
	}
	if len(moved) != 1 || !strings.HasPrefix(filepath.Base(moved[0]), "vw2026.old-") ||
		filepath.Dir(moved[0]) != filepath.Join(h.work, "old") {
		t.Fatalf("%v", moved)
	}
	if exist(moved[0]) {
		t.Fatal("the moved CLI was not deleted")
	}
}

// 消せないよけた CLI（Windows で動いているもの）は、install では黙って残し、uninstall では left に載せる。
// <Work>/old/ の直下の vw2026.old-* / vw2026.exe.old-* のほかは消さない。
func TestCleanOldCLIs(t *testing.T) {
	h := newHarness(t)
	h.installCLI()
	old := filepath.Join(h.work, "old")
	os.MkdirAll(old, 0o700)
	keep := []string{"vw2026", "vw2026.old", "other.old-1", "cli.uninstalling.old-x"}
	for _, name := range append(keep, "vw2026.old-1", "vw2026.exe.old-2", "vw2026.old-busy") {
		os.WriteFile(filepath.Join(old, name), nil, 0o600)
	}
	os.Mkdir(filepath.Join(old, "vw2026.old-dir"), 0o700)
	busy := filepath.Join(old, "vw2026.old-busy")
	original := removeFile
	removeFile = func(path string) error {
		if path == busy {
			return os.ErrPermission
		}
		return original(path)
	}
	t.Cleanup(func() { removeFile = original })
	h.mustInstall(release{name: "cli", tag: "c2"})
	if !exist(busy) || exist(filepath.Join(old, "vw2026.old-1")) || exist(filepath.Join(old, "vw2026.exe.old-2")) {
		t.Fatal("install should delete what it can")
	}
	for _, name := range append(keep, "vw2026.old-dir") {
		if !exist(filepath.Join(old, name)) {
			t.Errorf("deleted %s", name)
		}
	}
	result, err := Uninstall(h.options(), "cli")
	if err != nil || len(result.Left) != 1 || result.Left[0] != busy {
		t.Fatalf("%+v %v", result, err)
	}
}

// 中にあるかは、パスの文字列ではなくディレクトリの同一性で決める。
func TestWithin(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "Plug-Ins", "cli")
	executable := filepath.Join(dir, "bin", "vw2026")
	os.MkdirAll(filepath.Dir(executable), 0o755)
	os.WriteFile(executable, nil, 0o700)
	outside := filepath.Join(tmp, "elsewhere", "vw2026")
	os.MkdirAll(filepath.Dir(outside), 0o755)
	os.WriteFile(outside, nil, 0o700)

	check := func(name, path, dir string, want bool) {
		t.Helper()
		rel, ok := within(path, dir)
		if ok != want || (ok && filepath.ToSlash(rel) != "bin/vw2026") {
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
	// 大文字と小文字を区別しないボリューム（Windows・mac の既定の APFS）でだけ走らせる。
	upper := filepath.Join(tmp, "PLUG-INS", "CLI")
	if _, err := os.Stat(upper); err == nil {
		check("case", executable, upper, true)
	} else {
		t.Log("the volume is case-sensitive; skipped the case check")
	}
}

// 実行ファイルがリンク（mac の ~/.local/bin/vw2026）なら、解いた先で決める。
func TestSidestepFollowsTheLink(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	link := filepath.Join(h.tmp, "local-bin", "vw2026")
	os.MkdirAll(filepath.Dir(link), 0o755)
	if err := os.Symlink(h.fakeExecutable(), link); err != nil {
		t.Skip("cannot make a symbolic link:", err)
	}
	o := h.options()
	o.Executable = link
	moved, err := o.sidestep(filepath.Join(h.plugins, "cli"), filepath.Join(h.work, "old"))
	if err != nil || moved == nil || moved.from != h.fakeExecutable() || exist(h.fakeExecutable()) {
		t.Fatalf("%+v %v", moved, err)
	}
	moved.restore()
	if !exist(h.fakeExecutable()) {
		t.Fatal("not restored")
	}
}
