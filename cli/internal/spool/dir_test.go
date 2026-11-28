package spool

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/edition"
)

// fakeExecutable は <dir>/cli/bin/vw2026 に空のファイルを作成し、そのパスと、期待するスプール
// （<dir>/cli/spool）を返す。一時ディレクトリはリンクを解決した形にする（mac の /var は
// /private/var へのリンクで、DirOf はリンクを解決した場所を返すため）。
func fakeExecutable(t *testing.T) (executable, spool string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "cli", "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	executable = filepath.Join(bin, edition.Command)
	if err := os.WriteFile(executable, nil, 0o700); err != nil {
		t.Fatal(err)
	}
	return executable, filepath.Join(root, "cli", SpoolDirName)
}

func TestDirOfIsNextToTheBinFolder(t *testing.T) {
	executable, want := fakeExecutable(t)
	if got := DirOf(executable); got != want {
		t.Fatalf("DirOf(%s) = %s, want %s", executable, got, want)
	}
}

// mac の ~/.local/bin/vw2026 は <Plug-Ins>/cli/bin/vw2026 へのリンクなので、リンクを解決してから
// 導出する。
func TestDirOfFollowsLinks(t *testing.T) {
	executable, want := fakeExecutable(t)
	link := filepath.Join(t.TempDir(), edition.Command)
	if err := os.Symlink(executable, link); err != nil {
		// Windows はシンボリックリンクを作成するのに権限（開発者モード）が必要である。PATH に
		// 追加するのはフォルダなので、リンクを経由して実行されることは無い。
		t.Skipf("cannot create a symbolic link: %v", err)
	}
	if got := DirOf(link); got != want {
		t.Fatalf("DirOf(%s) = %s, want %s", link, got, want)
	}
}

// リンクを解決できなければ（ここでは実行ファイルが存在しない）、解決する前のパスから導出する。
func TestDirOfFallsBackToTheGivenPath(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "cli", "bin", edition.Command)
	if got, want := DirOf(executable), filepath.Join(root, "cli", SpoolDirName); got != want {
		t.Fatalf("DirOf(%s) = %s, want %s", executable, got, want)
	}
}

// 場所は実行ファイルのみから決定され、環境変数を読み取らない。プラグインは環境変数を
// 読み取らないので、呼び出し元のみが別の場所を指定できても要求が届かないため
// （docs/protocol.md「スプールの場所」）。
func TestDirIgnoresEnvironment(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	want := DirOf(executable)
	for _, name := range []string{edition.EnvPrefix + "SPOOL", "HOME", "LOCALAPPDATA", "APPDATA", "TMPDIR"} {
		t.Setenv(name, t.TempDir())
	}
	if got, err := Dir(); err != nil || got != want {
		t.Fatalf("Dir() = %s, %v; want %s", got, err, want)
	}
}
