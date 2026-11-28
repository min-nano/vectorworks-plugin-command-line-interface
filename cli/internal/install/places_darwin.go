package install

import (
	"errors"
	"fmt"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/edition"
)

// userTempDir は利用者ごとの一時フォルダ（confstr(_CS_DARWIN_USER_TEMP_DIR)）。
// cgo を使用しないため getconf を実行する。環境変数 TMPDIR は読み取らない。
func userTempDir() (string, error) {
	out, err := getconf()
	if err != nil {
		return "", err
	}
	dir := strings.TrimSpace(string(out))
	if !filepath.IsAbs(dir) {
		return "", errors.New("getconf DARWIN_USER_TEMP_DIR printed no absolute path")
	}
	return filepath.Clean(dir), nil
}

// defaultPluginsDir はホームの Library/Application Support の下。ホームは利用者の passwd の項目から
// 導出する（環境変数 HOME は読み取らない）。
func defaultPluginsDir() (string, error) {
	current, err := currentUser()
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(current.HomeDir) {
		return "", errors.New("the home folder is unknown")
	}
	return filepath.Join(current.HomeDir, "Library", "Application Support", "Vectorworks", edition.Year, "Plug-Ins"), nil
}

// machineIsArm64 は計算機が arm64 か。mac の CLI は universal であるため、Rosetta の下の端末からは
// x86_64 として動作するが、Vectorworks は arm64 で動作する。プロセスの種類ではなく計算機で
// 決定する。
// Intel の mac にはこの項目が無く、sysctl は失敗する。
func machineIsArm64() bool {
	out, err := exec.Command("/usr/sbin/sysctl", "-n", "hw.optional.arm64").Output()
	return err == nil && strings.TrimSpace(string(out)) == "1"
}

// getconf と currentUser は、OS が応答できない場合を検証するテストが差し替える。
var (
	getconf = func() ([]byte, error) {
		return exec.Command("/usr/bin/getconf", "DARWIN_USER_TEMP_DIR").Output()
	}
	currentUser = user.Current
)

func target() string {
	return targetOf(machineIsArm64)
}

// targetOf は、計算機が arm64 かを応答する関数からターゲットを決定する（テストが関数を
// 差し替える）。
func targetOf(isArm64 func() bool) string {
	if isArm64() {
		return "macos-arm64"
	}
	return "macos-x64"
}

// defaultInterpreter は、フックの 1 つ目を実行する argv の先頭。mac は /bin/sh のみ。
func defaultInterpreter(name string) ([]string, error) {
	if name == "sh" {
		return []string{"/bin/sh"}, nil
	}
	return nil, fmt.Errorf("%s hooks do not run on macOS", name)
}
