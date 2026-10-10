package launch

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"
)

// helperEnv が設定されていれば、テストの実行ファイルは Start に起動される側として振る舞い、
// 指定されたファイルを書いてすぐ終わる（TestMain）。
const helperEnv = "VW2026_LAUNCH_TEST_MARKER"

func TestMain(m *testing.M) {
	if marker := os.Getenv(helperEnv); marker != "" {
		_ = os.WriteFile(marker, []byte("started"), 0o600)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func writeFile(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCommandExplicitExecutable(t *testing.T) {
	exe := writeFile(t, filepath.Join(t.TempDir(), "fake-vw"))
	argv, err := Command(exe)
	if err != nil || !reflect.DeepEqual(argv, []string{exe}) {
		t.Fatalf("got %v, %v", argv, err)
	}
}

func TestCommandExplicitMustBeAFile(t *testing.T) {
	dir := t.TempDir()
	for _, app := range []string{filepath.Join(dir, "missing"), dir} {
		if argv, err := Command(app); err == nil {
			t.Errorf("%s: want an error, got %v", app, argv)
		}
	}
}

func TestCommandDefault(t *testing.T) {
	switch runtime.GOOS {
	case "darwin":
		argv, err := Command("")
		if err != nil || !reflect.DeepEqual(argv, []string{"/usr/bin/open", "-a", DefaultMacApp}) {
			t.Fatalf("got %v, %v", argv, err)
		}
		// .app は存在を確かめずに open -a へ渡す（open が探す）。
		app := "/Applications/Vectorworks 2026/Vectorworks 2026.app/"
		argv, err = Command(app)
		if err != nil || !reflect.DeepEqual(argv, []string{"/usr/bin/open", "-a", app}) {
			t.Fatalf("got %v, %v", argv, err)
		}
	case "windows":
		root := t.TempDir()
		t.Setenv("ProgramFiles", root)
		if argv, err := Command(""); err == nil {
			t.Fatalf("not installed: want an error, got %v", argv)
		}
		exe := writeFile(t, filepath.Join(root, defaultWindowsExe))
		argv, err := Command("")
		if err != nil || !reflect.DeepEqual(argv, []string{exe}) {
			t.Fatalf("got %v, %v", argv, err)
		}
	default:
		if argv, err := Command(""); err == nil {
			t.Fatalf("want an error on %s, got %v", runtime.GOOS, argv)
		}
	}
}

// Start は切り離して起動する。切り離しの指定（Setsid・DETACHED_PROCESS）を誤ると
// 起動そのものが失敗するので、各 OS で実際に起動して確かめる。
func TestStartDetaches(t *testing.T) {
	// テストの実行ファイルを別名で写して起動する。Windows では同じ名前のプロセス
	// （このテスト自身）が動いていると、起動済みとみなして起動しないため。
	exe := copyTestBinary(t, filepath.Join(t.TempDir(), "helper"+exeSuffix()))
	marker := filepath.Join(t.TempDir(), "started")
	t.Setenv(helperEnv, marker)
	if err := Start([]string{exe}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the started process did not run")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Windows では、同じ実行ファイルが動いていれば起動しない。このテスト自身が動いている
// 実行ファイルで確かめる。
func TestStartRefusesWhenRunningOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows only")
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if !windowsRunning(self) {
		t.Fatal("windowsRunning does not see the running test binary")
	}
	if err := Start([]string{self}); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("want ErrAlreadyRunning, got %v", err)
	}
}

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

func copyTestBinary(t *testing.T, dest string) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	in, err := os.Open(self)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o700)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	return dest
}
