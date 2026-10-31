package install

import (
	"errors"
	"fmt"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
)

// userTempDir は利用者ごとの一時フォルダ（confstr(_CS_DARWIN_USER_TEMP_DIR)）。cgo を使わないので
// getconf を走らせる。環境変数 TMPDIR は読まない。
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
// 求める（環境変数 HOME は読まない）。
func defaultPluginsDir() (string, error) {
	current, err := currentUser()
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(current.HomeDir) {
		return "", errors.New("the home folder is unknown")
	}
	return filepath.Join(current.HomeDir, "Library", "Application Support", "Vectorworks", "2026", "Plug-Ins"), nil
}

// machineIsArm64 は計算機が arm64 か。mac の CLI は universal なので、Rosetta の下の端末からは
// x86_64 として動くが、Vectorworks は arm64 で動く。プロセスの種類ではなく計算機で決める。
// Intel の mac にはこの項目が無く、sysctl は失敗する。
func machineIsArm64() bool {
	out, err := exec.Command("/usr/sbin/sysctl", "-n", "hw.optional.arm64").Output()
	return err == nil && strings.TrimSpace(string(out)) == "1"
}

// getconf と currentUser は、OS が答えられないときを確かめるテストが差し替える。
var (
	getconf = func() ([]byte, error) {
		return exec.Command("/usr/bin/getconf", "DARWIN_USER_TEMP_DIR").Output()
	}
	currentUser = user.Current
)

func target() string {
	return targetOf(machineIsArm64)
}

// targetOf は、計算機が arm64 かを答える関数からターゲットを決める（テストが関数を差し替える）。
func targetOf(isArm64 func() bool) string {
	if isArm64() {
		return "macos-arm64"
	}
	return "macos-x64"
}

// oldCLIName は、よけた CLI の名前の頭（oldCLIPrefixes の 1 つ）。
const oldCLIName = "vw2026.old-"

// defaultInterpreter は、フックの 1 つ目を走らせる argv の頭。mac は /bin/sh だけ。
func defaultInterpreter(name string) ([]string, error) {
	if name == "sh" {
		return []string{"/bin/sh"}, nil
	}
	return nil, fmt.Errorf("%s hooks do not run on macOS", name)
}
