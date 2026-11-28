// Package launch は Vectorworks を起動する。
//
// 起動は OS の標準の方法のみに依存する。macOS は `open -a`（既に動作していれば前面に表示
// するのみで、2 つ目は起動しない）。Windows は既定のインストール先の実行ファイルを起動し、既に
// 動作していれば起動しない（実行ファイルを直接起動すると 2 つ目が起動しうる）。いずれも
// 既に動作していた場合を成功とし、OS によって結果を変更しない。
// 既定の場所を探索しない（他の場所は app で明示する）。
// app で明示でき、.app で終わらなければ実行ファイルとしてそのまま起動する（テスト用の代替
// プログラムもこの経路）。app をいずれの指定（フラグ・環境変数）で受け取るかは呼び出し元が決定し、
// エラーの案内も呼び出し元が付ける。
package launch

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/edition"
)

// DefaultMacApp は macOS で起動するアプリの名前（Vectorworks 2026。2025 の CLI は
// Vectorworks 2025）。プラグインはその版用なので、その版のみを起動する（別の版ではプラグインが
// 読み込まれない）。
const DefaultMacApp = edition.Product

// defaultWindowsExe は Windows の既定のインストール先の実行ファイル（%ProgramFiles% の下の
// Vectorworks 2026\Vectorworks2026.exe。2025 の CLI は 2025）。
const defaultWindowsExe = edition.Product + `\Vectorworks` + edition.Year + `.exe`

// Command は起動するコマンド（argv）を返す。
func Command(app string) ([]string, error) {
	return command(runtime.GOOS, app)
}

// command は goos での Command（テストが、実行中の OS に依存しない経路 ― macOS・未対応の OS
// ― をいずれの OS でも検証する。Windows の経路は %ProgramFiles% と区切り文字に
// 依存するので、Windows でのみ検証する）。
func command(goos, app string) ([]string, error) {
	if app != "" && !(goos == "darwin" && strings.HasSuffix(strings.TrimRight(app, "/"), ".app")) {
		if info, err := os.Stat(app); err != nil || info.IsDir() {
			return nil, fmt.Errorf("%s is not a file", app)
		}
		return []string{app}, nil
	}
	switch goos {
	case "darwin":
		if app == "" {
			app = DefaultMacApp
		}
		return []string{"/usr/bin/open", "-a", app}, nil
	case "windows":
		exe := filepath.Join(os.Getenv("ProgramFiles"), defaultWindowsExe)
		if info, err := os.Stat(exe); err == nil && !info.IsDir() {
			return []string{exe}, nil
		}
		return nil, fmt.Errorf("%s was not found at %s", edition.Product, exe)
	default:
		return nil, errors.New("launching Vectorworks is not supported on this OS")
	}
}

// Start は起動して切り離す（CLI が終了しても Vectorworks は残る）。Windows で同一の実行ファイルが
// 既に動作していれば、2 つ目を起動せずに成功とする（macOS の open -a が前面に表示するのみで成功する
// のと統一し、呼び出し元が OS ごとに区別しなくて済むように）。
func Start(argv []string) error {
	if runtime.GOOS == "windows" && windowsRunning(argv[0]) {
		return nil
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// windowsRunning は Windows で同一の実行ファイルが動作しているか（判明しなければ false）。
func windowsRunning(exe string) bool {
	name := filepath.Base(exe)
	out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq "+name, "/NH", "/FO", "CSV").Output()
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(out)), `"`+strings.ToLower(name)+`"`)
}
