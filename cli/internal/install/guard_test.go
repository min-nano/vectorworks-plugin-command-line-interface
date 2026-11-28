package install

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// failLstat は、テストの間だけ at の lstat を失敗させる（権限・届かない共有で調べられない）。
func failLstat(t *testing.T, at string) {
	t.Helper()
	original := lstat
	swap(t, &lstat, func(path string) (fs.FileInfo, error) {
		if path == at {
			return nil, &fs.PathError{Op: "lstat", Path: path, Err: errInjected}
		}
		return original(path)
	})
}

// 中断した uninstall の印を消せなければ（ウイルス対策が開いているなど）、印を残したまま付け替えへ進まず、何も
// 変えない（付け替えの途中で止まると、次の uninstall が唯一残った old/<name>/ を消しうるため）。
func TestInstallCannotDeleteAStaleMarker(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	h.publish(release{name: "cli", tag: "c2"})
	marker := filepath.Join(h.work, "old", "cli.uninstalling")
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	pathOpWhen(t, &removeFile, func(path string) error {
		if path == marker {
			return errInjected
		}
		return nil
	})
	before, beforeWork := snapshot(t, h.plugins), snapshot(t, h.work)
	_, err := h.install("cli", "c2")
	if err == nil || !strings.Contains(err.Error(), marker) || !strings.Contains(err.Error(), "nothing is changed") {
		t.Fatal(err)
	}
	sameSnapshot(t, before, snapshot(t, h.plugins))
	sameSnapshot(t, beforeWork, snapshot(t, h.work))
}

// 消す・移すかを決めるもの（old/<name>/・印）を調べられなければ、無いとみなさずに、install も uninstall も何も
// 変えずに失敗にする（唯一の版かもしれない old/<name>/ を見落とさない）。
func TestUncheckablePlacesChangeNothing(t *testing.T) {
	for _, at := range []string{"old/cli", "old/cli.uninstalling"} {
		t.Run(at, func(t *testing.T) {
			h := newHarness(t)
			h.mustInstall(release{name: "cli", tag: "c1"})
			h.publish(release{name: "cli", tag: "c2"})
			failLstat(t, filepath.Join(h.work, filepath.FromSlash(at)))
			before := snapshot(t, h.plugins)
			if _, err := h.install("cli", "c2"); !errors.Is(err, errInjected) ||
				!strings.Contains(err.Error(), "cannot check") {
				t.Errorf("install: %v", err)
			}
			if _, err := Uninstall(h.options(), "cli"); !errors.Is(err, errInjected) ||
				!strings.Contains(err.Error(), "nothing is removed") {
				t.Errorf("uninstall: %v", err)
			}
			sameSnapshot(t, before, snapshot(t, h.plugins))
			if h.installedVersion("cli") != "c1" {
				t.Error("the installed version changed")
			}
		})
	}
}

// <Work>・<Work>/staging・<Work>/old がリンク（Windows ではジャンクションも）なら、install も uninstall も、リンクの
// 先に触れずに何も変えない（名前をそのまま組み立てた staging/<name>/ や old/<name>/ が、リンクを通して別の
// フォルダを指さないように）。
func TestWorkFoldersMustBePlain(t *testing.T) {
	kinds := []string{"symlink"}
	if runtime.GOOS == "windows" {
		kinds = append(kinds, "junction")
	}
	for _, kind := range kinds {
		for _, sub := range []string{".", "staging", "old"} {
			t.Run(kind+"/"+sub, func(t *testing.T) {
				h := newHarness(t)
				h.mustInstall(release{name: "cli", tag: "c1"})
				h.publish(release{name: "cli", tag: "c2"})
				// リンクの先には、staging/<name>/ や old/<name>/ として見えてしまうフォルダを置く。
				target := filepath.Join(h.tmp, "elsewhere")
				for _, dir := range []string{"cli", "staging/cli", "old/cli"} {
					mine := filepath.Join(target, filepath.FromSlash(dir))
					if err := os.MkdirAll(mine, 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(mine, "mine.txt"), []byte("mine"), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				link := filepath.Join(h.work, sub)
				if err := os.RemoveAll(link); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
					t.Fatal(err)
				}
				makeLink(t, kind, target, link)
				before, beforeTarget := snapshot(t, h.plugins), snapshot(t, target)
				if _, err := h.install("cli", "c2"); err == nil || !strings.Contains(err.Error(), "not a plain folder") {
					t.Errorf("install: %v", err)
				}
				if _, err := Uninstall(h.options(), "cli"); err == nil || !strings.Contains(err.Error(), "not a plain folder") {
					t.Errorf("uninstall: %v", err)
				}
				sameSnapshot(t, before, snapshot(t, h.plugins))
				sameSnapshot(t, beforeTarget, snapshot(t, target))
			})
		}
	}
}

// <Work>/old がファイルでも、何も変えずに失敗にする。
func TestOldFolderIsAFile(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	h.publish(release{name: "cli", tag: "c2"})
	old := filepath.Join(h.work, "old")
	os.RemoveAll(old)
	if err := os.WriteFile(old, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, h.plugins)
	if _, err := h.install("cli", "c2"); err == nil || !strings.Contains(err.Error(), "not a plain folder") {
		t.Errorf("install: %v", err)
	}
	if _, err := Uninstall(h.options(), "cli"); err == nil || !strings.Contains(err.Error(), "not a plain folder") {
		t.Errorf("uninstall: %v", err)
	}
	sameSnapshot(t, before, snapshot(t, h.plugins))
	if data, err := os.ReadFile(old); err != nil || string(data) != "mine" {
		t.Error("the file changed", err)
	}
}

// makeLink は link に target を指すリンクを作る（作れなければ飛ばす。Windows のシンボリックリンクは権限が要る）。
func makeLink(t *testing.T, kind, target, link string) {
	t.Helper()
	if kind == "junction" {
		if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
			t.Skip("cannot make a junction:", string(out), err)
		}
		return
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skip("cannot make a symbolic link:", err)
	}
}
