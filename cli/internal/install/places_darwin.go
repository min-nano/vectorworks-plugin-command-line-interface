package install

import (
	"errors"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
)

// userTempDir は利用者ごとの一時フォルダ（confstr(_CS_DARWIN_USER_TEMP_DIR)）。cgo を使わないので
// getconf を走らせる。環境変数 TMPDIR は読まない。
func userTempDir() (string, error) {
	out, err := exec.Command("/usr/bin/getconf", "DARWIN_USER_TEMP_DIR").Output()
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
	current, err := user.Current()
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

func target(isArm64 func() bool) (string, error) {
	if isArm64() {
		return "macos-arm64", nil
	}
	return "macos-x64", nil
}
