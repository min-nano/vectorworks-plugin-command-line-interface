//go:build darwin || windows

package install

import (
	"path/filepath"
	"runtime"
	"testing"
)

// Windows は windows-x64 だけ（Windows on ARM でも Vectorworks は x64 で動く）。mac は places_darwin_test.go。
func TestTarget(t *testing.T) {
	got, err := Target()
	if err != nil || (runtime.GOOS == "windows" && got != "windows-x64") {
		t.Fatalf("%q %v", got, err)
	}
}

// 場所の根は環境変数を読まずに求める（走らせ方で場所が変わらないように）。
func TestPlacesIgnoreEnvironment(t *testing.T) {
	work, err := WorkDir()
	if err != nil {
		t.Fatal(err)
	}
	plugins, err := DefaultPluginsDir()
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(work) || filepath.Base(work) != WorkDirName || filepath.Base(plugins) != "Plug-Ins" {
		t.Fatalf("%s %s", work, plugins)
	}
	elsewhere := t.TempDir()
	for _, name := range []string{"TMPDIR", "TMP", "TEMP", "HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA"} {
		t.Setenv(name, elsewhere)
	}
	if again, _ := WorkDir(); again != work {
		t.Errorf("WorkDir changed with the environment: %s", again)
	}
	if again, _ := DefaultPluginsDir(); again != plugins {
		t.Errorf("DefaultPluginsDir changed with the environment: %s", again)
	}
}
