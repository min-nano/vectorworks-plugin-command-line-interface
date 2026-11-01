package install

import (
	"os"
	"path/filepath"
	"testing"
)

// 入った CLI（<X>/cli/bin/ にあり、<X>/cli/ に正しい記録がある）なら <X>。リンクは解く。それ以外は偽。
func TestPluginsDirOf(t *testing.T) {
	h := newHarness(t)
	h.installCLI()
	executable := filepath.Join(h.plugins, "cli", "bin", "vw2026")
	// リンクを解いた場所を返すので、書き方（mac の /var と /private/var、Windows の短い名前）ではなく同一性で比べる。
	if dir, ok := PluginsDirOf(executable); !ok || !sameDirectory(t, dir, h.plugins) {
		t.Fatal(dir, ok)
	}
	link := filepath.Join(h.tmp, "vw2026-link")
	if err := os.Symlink(executable, link); err == nil {
		if dir, ok := PluginsDirOf(link); !ok || !sameDirectory(t, dir, h.plugins) {
			t.Fatal("link:", dir, ok)
		}
	}
	for _, path := range []string{"", filepath.Join(h.tmp, "vw2026"), filepath.Join(h.plugins, "cli", "vw2026")} {
		if dir, ok := PluginsDirOf(path); ok {
			t.Error(path, dir)
		}
	}
	// 記録の module が無くても（隔離された）入った CLI とみなす（既定の場所に別の cli を入れない）。
	os.RemoveAll(filepath.Join(h.plugins, "cli", "cli.vwlibrary"))
	if _, ok := PluginsDirOf(executable); !ok {
		t.Fatal("without the module")
	}
	// 記録が無ければ（手で置いたフォルダ）入った CLI とみなさない。
	os.Remove(filepath.Join(h.plugins, "cli", RecordFile))
	if dir, ok := PluginsDirOf(executable); ok {
		t.Error("without a record:", dir)
	}
}

func sameDirectory(t *testing.T, a, b string) bool {
	t.Helper()
	x, errX := os.Stat(a)
	y, errY := os.Stat(b)
	return errX == nil && errY == nil && os.SameFile(x, y)
}
