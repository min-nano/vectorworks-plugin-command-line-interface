package launch

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/edition"
)

// helperEnv が設定されていれば、テストの実行ファイルは Start に起動される側として振る舞い、
// 指定されたファイルを書き込んで直ちに終了する（TestMain）。
const helperEnv = edition.EnvPrefix + "LAUNCH_TEST_MARKER"

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

// Vectorworks の動作しない OS では、既定のものを起動しない。
func TestCommandUnsupportedOS(t *testing.T) {
	if argv, err := command("plan9", ""); err == nil {
		t.Fatalf("want an error, got %v", argv)
	}
}

// macOS の経路は計算機に依存しないので、いずれの OS でも検証する。
func TestCommandMac(t *testing.T) {
	argv, err := command("darwin", "")
	if err != nil || !reflect.DeepEqual(argv, []string{"/usr/bin/open", "-a", DefaultMacApp}) {
		t.Fatalf("got %v, %v", argv, err)
	}
	// .app は存在を確認せずに open -a へ渡す（open が探索する）。
	app := "/Applications/" + edition.Product + "/" + edition.Product + ".app/"
	argv, err = command("darwin", app)
	if err != nil || !reflect.DeepEqual(argv, []string{"/usr/bin/open", "-a", app}) {
		t.Fatalf("got %v, %v", argv, err)
	}
}

// Windows の経路は %ProgramFiles% と区切り文字に依存するので、Windows でのみ検証する。実行中の
// OS の既定の経路を Command で検証する（macOS は TestCommandMac が command で検証する）。
func TestCommandDefault(t *testing.T) {
	switch runtime.GOOS {
	case "darwin":
		argv, err := Command("")
		if err != nil || !reflect.DeepEqual(argv, []string{"/usr/bin/open", "-a", DefaultMacApp}) {
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
// 起動そのものが失敗するので、各 OS で実際に起動して検証する。
func TestStartDetaches(t *testing.T) {
	// テストの実行ファイルを別名で複製して起動する。Windows では同じ名前のプロセス
	// （このテスト自身）が動作していると、起動済みとみなして起動しないため。
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

// 起動できなければ（実行ファイルが存在しない）、起動のエラーを返す。
func TestStartFailsForMissingExecutable(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing"+exeSuffix())
	if err := Start([]string{missing}); err == nil {
		t.Fatalf("want a start error, got %v", err)
	}
}

// Windows では、同一の実行ファイルが動作していれば起動せずに成功とする。このテスト自身が
// 動作している実行ファイルで検証する（誤って起動すると、起動された側は標識を書き込んで直ちに
// 終了する）。
func TestStartSkipsWhenRunningOnWindows(t *testing.T) {
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
	marker := filepath.Join(t.TempDir(), "started")
	t.Setenv(helperEnv, marker)
	if err := Start([]string{self}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Second)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("started a second process")
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
