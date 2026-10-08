// Package launch は Vectorworks を起動する。
//
// 起動は OS の標準の方法にだけ頼る。macOS は `open -a`（既に動いていれば前面に出すだけで、
// 2 つ目は起動しない）。Windows は既定のインストール先の実行ファイルを探し、既に
// 動いていれば起動しない（実行ファイルを直接起動すると 2 つ目が立ち上がりうる）。
// app（VW2026_APP）で明示でき、.app で終わらなければ実行ファイルとしてそのまま起動する
// （テスト用の代替プログラムもこの経路）。
package launch

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// DefaultMacApp は macOS で起動するアプリの名前。プラグインは Vectorworks 2026 用なので、
// その版だけを起動する（別の版ではプラグインが読み込まれない）。
const DefaultMacApp = "Vectorworks 2026"

// defaultWindowsGlobs は Windows で探す実行ファイル。
var defaultWindowsGlobs = []string{
	`%ProgramFiles%\Vectorworks 2026\Vectorworks2026.exe`,
	`%ProgramFiles%\Vectorworks 2026*\Vectorworks*.exe`,
}

// ErrAlreadyRunning は Windows で同じ実行ファイルが既に動いている。
var ErrAlreadyRunning = errors.New("vectorworks is already running")

// Command は起動するコマンド（argv）を返す。
func Command(app string) ([]string, error) {
	if app != "" && !(runtime.GOOS == "darwin" && strings.HasSuffix(strings.TrimRight(app, "/"), ".app")) {
		if info, err := os.Stat(app); err != nil || info.IsDir() {
			return nil, fmt.Errorf("VW2026_APP does not point to a file: %s", app)
		}
		return []string{app}, nil
	}
	switch runtime.GOOS {
	case "darwin":
		if app == "" {
			app = DefaultMacApp
		}
		return []string{"/usr/bin/open", "-a", app}, nil
	case "windows":
		for _, pattern := range defaultWindowsGlobs {
			found, _ := filepath.Glob(os.ExpandEnv(strings.ReplaceAll(pattern, "%ProgramFiles%", "${ProgramFiles}")))
			if len(found) > 0 {
				sort.Strings(found)
				return []string{found[0]}, nil
			}
		}
		return nil, errors.New(`Vectorworks 2026 was not found under %ProgramFiles%; set VW2026_APP to Vectorworks2026.exe`)
	default:
		return nil, errors.New("launching is not supported on this OS; set VW2026_APP")
	}
}

// Start は起動して切り離す（CLI が終わっても Vectorworks は残る）。
func Start(argv []string) error {
	if runtime.GOOS == "windows" && windowsRunning(argv[0]) {
		return ErrAlreadyRunning
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// windowsRunning は Windows で同じ実行ファイルが動いているか（分からなければ false）。
func windowsRunning(exe string) bool {
	name := filepath.Base(exe)
	out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq "+name, "/NH", "/FO", "CSV").Output()
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(out)), `"`+strings.ToLower(name)+`"`)
}
