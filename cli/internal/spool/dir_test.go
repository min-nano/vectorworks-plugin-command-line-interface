package spool

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeExecutable は <dir>/cli/bin/vw2026 に空のファイルを作り、そのパスと、期待するスプール
// （<dir>/cli/spool）を返す。一時ディレクトリはリンクを解いた形にする（mac の /var は
// /private/var へのリンクで、DirOf はリンクを解いた場所を返すため）。
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
	executable = filepath.Join(bin, "vw2026")
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

// mac の ~/.local/bin/vw2026 は <Plug-Ins>/cli/bin/vw2026 へのリンクなので、リンクを解いてから
// 求める。
func TestDirOfFollowsLinks(t *testing.T) {
	executable, want := fakeExecutable(t)
	link := filepath.Join(t.TempDir(), "vw2026")
	if err := os.Symlink(executable, link); err != nil {
		// Windows はシンボリックリンクを作るのに権限（開発者モード）が要る。PATH に載せるのは
		// フォルダなので、リンクを通して走ることは無い。
		t.Skipf("cannot create a symbolic link: %v", err)
	}
	if got := DirOf(link); got != want {
		t.Fatalf("DirOf(%s) = %s, want %s", link, got, want)
	}
}

// リンクを解けなければ（ここでは実行ファイルが無い）、解く前のパスから求める。
func TestDirOfFallsBackToTheGivenPath(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "cli", "bin", "vw2026")
	if got, want := DirOf(executable), filepath.Join(root, "cli", SpoolDirName); got != want {
		t.Fatalf("DirOf(%s) = %s, want %s", executable, got, want)
	}
}

// 場所は実行ファイルだけから決まり、環境変数を読まない。プラグインは環境変数を読まないので、
// 呼ぶ側だけが別の場所を指せても要求が届かないため（docs/protocol.md「スプールの場所」）。
func TestDirIgnoresEnvironment(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	want := DirOf(executable)
	for _, name := range []string{"VW2026_SPOOL", "HOME", "LOCALAPPDATA", "APPDATA", "TMPDIR"} {
		t.Setenv(name, t.TempDir())
	}
	if got, err := Dir(); err != nil || got != want {
		t.Fatalf("Dir() = %s, %v; want %s", got, err, want)
	}
}
