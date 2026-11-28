package install

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// syncLog は、テストの間に Sync したファイルとディレクトリ（差し替えた syncFile・syncDir が書く）。
type syncLog struct {
	mu    sync.Mutex
	files map[string]bool
	dirs  map[string]bool
}

func (l *syncLog) file(path string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.files[path]
}

func (l *syncLog) dir(path string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.dirs[path]
}

// recordSyncs は、テストの間だけ syncFile・syncDir を、記録してから元の操作をするものに替える。fail が誤りを返せば、
// それで失敗させる。
func recordSyncs(t *testing.T, fail func(path string) error) *syncLog {
	t.Helper()
	log := &syncLog{files: map[string]bool{}, dirs: map[string]bool{}}
	if fail == nil {
		fail = func(string) error { return nil }
	}
	originalFile, originalEntry, originalDir := syncFile, syncEntry, syncDir
	logFile := func(original func(*os.File) error) func(*os.File) error {
		return func(file *os.File) error {
			if err := fail(file.Name()); err != nil {
				return err
			}
			log.mu.Lock()
			log.files[file.Name()] = true
			log.mu.Unlock()
			return original(file)
		}
	}
	swap(t, &syncFile, logFile(originalFile))
	swap(t, &syncEntry, logFile(originalEntry))
	swap(t, &syncDir, func(dir string) error {
		if err := fail(dir); err != nil {
			return err
		}
		log.mu.Lock()
		log.dirs[dir] = true
		log.mu.Unlock()
		return originalDir(dir)
	})
	return log
}

// 記録は、隣に書いたファイルを Sync してから名前を変え、置き換えた後に親のディレクトリを Sync する。
func TestReplaceFileSyncsBeforeRename(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, RecordFile)
	log := recordSyncs(t, nil)
	renamed := false
	renameWhen(t, func(from, to string) error {
		if to == path {
			renamed = true
			if !log.file(from) {
				t.Errorf("renamed %s before syncing it", from)
			}
			if log.dir(dir) {
				t.Error("synced the folder before the rename")
			}
		}
		return nil
	})
	if err := replaceFile(path, []byte("new")); err != nil {
		t.Fatal(err)
	}
	if !renamed || !log.dir(dir) {
		t.Fatalf("renamed %v, synced the folder %v", renamed, log.dir(dir))
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "new" {
		t.Fatal(string(data), err)
	}
}

// 置き換えた後に親のディレクトリを Sync できなくても、置き換えは済んでいるので成功にする（ディレクトリの Sync を
// 断るファイルシステムで、置き換えたのに失敗と答えない）。
func TestReplaceFileIgnoresTheFolderSync(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, RecordFile)
	recordSyncs(t, func(at string) error {
		if at == dir {
			return errInjected
		}
		return nil
	})
	if err := replaceFile(path, []byte("new")); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "new" {
		t.Fatal(string(data), err)
	}
}

// 展開したファイルとディレクトリは、全て Sync してから <Plug-Ins> へ付け替える。
func TestInstallSyncsTheStagingBeforeTheSwap(t *testing.T) {
	h := newHarness(t)
	h.publish(release{name: "cli", tag: "c1"})
	log := recordSyncs(t, nil)
	stage := filepath.Join(h.work, "staging", "cli")
	dest := filepath.Join(h.plugins, "cli")
	checked := false
	renameWhen(t, func(from, to string) error {
		if from != stage || to != dest {
			return nil
		}
		checked = true
		err := filepath.WalkDir(stage, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if !log.dir(path) {
					t.Errorf("the folder %s is not synced", path)
				}
			} else if entry.Name() != RecordFile && !log.file(path) {
				// 記録は隣に書いて Sync してから名前を変える（TestReplaceFileSyncsBeforeRename）。
				t.Errorf("the file %s is not synced", path)
			}
			return nil
		})
		if err != nil {
			t.Error(err)
		}
		return nil
	})
	if _, err := h.install("cli", "c1"); err != nil {
		t.Fatal(err)
	}
	if !checked {
		t.Fatal("the swap was not reached")
	}
	if !log.dir(h.plugins) {
		t.Error("the folder of plug-ins is not synced after the swap")
	}
}

// 展開したファイルや組み立ての場所を Sync できなければ、付け替えずに失敗にする。
func TestInstallCannotSyncTheStaging(t *testing.T) {
	// 展開したファイルと、展開したディレクトリ（記録を置き換えた後の syncDir ではなく、syncTree が Sync するもの）。
	for _, at := range []string{"version.txt", "bin"} {
		t.Run(at, func(t *testing.T) {
			h := newHarness(t)
			h.mustInstall(release{name: "cli", tag: "c1"})
			h.publish(release{name: "cli", tag: "c2"})
			target := filepath.Join(h.work, "staging", "cli", at)
			recordSyncs(t, func(path string) error {
				if filepath.Clean(path) == filepath.Clean(target) {
					return errInjected
				}
				return nil
			})
			before := snapshot(t, h.plugins)
			if _, err := h.install("cli", "c2"); !errors.Is(err, errInjected) {
				t.Fatal(err)
			}
			sameSnapshot(t, before, snapshot(t, h.plugins))
		})
	}
}

// uninstall は、印を書いて <Work>/old を Sync してから、プラグインのフォルダを移す。
func TestUninstallSyncsTheMarkerBeforeMoving(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	log := recordSyncs(t, nil)
	marker := filepath.Join(h.work, "old", "cli.uninstalling")
	moved := false
	renameWhen(t, func(from, to string) error {
		if from == filepath.Join(h.plugins, "cli") {
			moved = true
			if !log.file(marker) || !log.dir(filepath.Join(h.work, "old")) {
				t.Error("moved the folder before the marker was synced")
			}
		}
		return nil
	})
	if _, err := Uninstall(h.options(), "cli"); err != nil {
		t.Fatal(err)
	}
	if !moved {
		t.Fatal("the folder was not moved")
	}
}

// 印を書いた後で <Work>/old を Sync できなければ、印を消して、何も変えずに失敗にする。
func TestUninstallCannotSyncTheMarker(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	old := filepath.Join(h.work, "old")
	recordSyncs(t, func(path string) error {
		if path == old {
			return errInjected
		}
		return nil
	})
	before := snapshot(t, h.plugins)
	if _, err := Uninstall(h.options(), "cli"); !errors.Is(err, errInjected) {
		t.Fatal(err)
	}
	sameSnapshot(t, before, snapshot(t, h.plugins))
	if exists(filepath.Join(old, "cli.uninstalling")) {
		t.Error("left the marker")
	}
}

// writeSynced は、書けない・Sync できないときに誤りを返す。syncDirectory は、開けないディレクトリで誤りを返す
// （Windows では何もしない）。
func TestSyncHelpersReportErrors(t *testing.T) {
	dir := t.TempDir()
	if err := writeSynced(filepath.Join(dir, "missing", "file"), nil, 0o600); err == nil {
		t.Error("writeSynced into a missing folder should fail")
	}
	recordSyncs(t, func(string) error { return errInjected })
	if err := writeSynced(filepath.Join(dir, "file"), []byte("x"), 0o600); !errors.Is(err, errInjected) {
		t.Errorf("writeSynced: %v", err)
	}
	err := syncDirectory(filepath.Join(dir, "missing"))
	if runtime.GOOS == "windows" {
		if err != nil {
			t.Errorf("syncDirectory on Windows: %v", err)
		}
	} else if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Errorf("syncDirectory: %v", err)
	}
	if err := syncTree(filepath.Join(dir, "missing")); err == nil {
		t.Error("syncTree of a missing folder should fail")
	}
}

// mkdirAllSynced は、作った段の親のディレクトリを全て Sync する（作った直後のフォルダへ移したものが、電源が
// 落ちた後にフォルダごと失われないように）。既にあれば Sync しない。作れない・Sync できなければ、その誤りを返す。
func TestMkdirAllSyncedSyncsTheParentsOfWhatItMakes(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "a", "b", "c")
	log := recordSyncs(t, nil)
	if err := mkdirAllSynced(target, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, parent := range []string{dir, filepath.Join(dir, "a"), filepath.Join(dir, "a", "b")} {
		if !log.dir(parent) {
			t.Errorf("%s is not synced", parent)
		}
	}
	if log.dir(filepath.Dir(dir)) {
		t.Error("synced the parent of a folder that was already there")
	}
	again := recordSyncs(t, nil)
	if err := mkdirAllSynced(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if len(again.dirs) != 0 {
		t.Errorf("synced %v for a folder that was already there", again.dirs)
	}
	recordSyncs(t, func(string) error { return errInjected })
	if err := mkdirAllSynced(filepath.Join(dir, "d"), 0o700); !errors.Is(err, errInjected) {
		t.Errorf("sync: %v", err)
	}
	failMkdirAll(t, filepath.Join(dir, "e"))
	if err := mkdirAllSynced(filepath.Join(dir, "e"), 0o700); !errors.Is(err, errInjected) {
		t.Errorf("mkdir: %v", err)
	}
}

// uninstall が <Work>/old を作るときは、その項目（<Work> の中）も Sync してから、プラグインのフォルダを移す。
// 移した後にも、移したことを Sync してからフックを走らせる。
func TestUninstallSyncsANewOldFolder(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	if err := os.RemoveAll(filepath.Join(h.work, "old")); err != nil {
		t.Fatal(err)
	}
	log := recordSyncs(t, nil)
	moved := false
	renameWhen(t, func(from, to string) error {
		if from == filepath.Join(h.plugins, "cli") {
			moved = true
			if !log.dir(h.work) {
				t.Error("moved the folder before the new old folder was synced in <Work>")
			}
		}
		return nil
	})
	if _, err := Uninstall(h.options(), "cli"); err != nil {
		t.Fatal(err)
	}
	if !moved || !log.dir(h.plugins) {
		t.Fatalf("moved %v, synced the folder of plug-ins %v", moved, log.dir(h.plugins))
	}
}
