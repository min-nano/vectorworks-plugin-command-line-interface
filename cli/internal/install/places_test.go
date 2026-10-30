//go:build darwin || windows

package install

import (
	"path/filepath"
	"runtime"
	"testing"
)

// mac は計算機の種類で決め、プロセスの種類では決めない（universal の CLI が Rosetta の下で x86_64 として
// 動いても、Vectorworks は arm64 で動く）。Windows は windows-x64 だけ。
func TestTargetFollowsTheMachine(t *testing.T) {
	for arm64, want := range map[bool]string{true: "macos-arm64", false: "macos-x64"} {
		if runtime.GOOS == "windows" {
			want = "windows-x64"
		}
		if got, err := target(func() bool { return arm64 }); err != nil || got != want {
			t.Errorf("arm64 %v: %q %v", arm64, got, err)
		}
	}
	if _, err := Target(); err != nil {
		t.Fatal(err)
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
