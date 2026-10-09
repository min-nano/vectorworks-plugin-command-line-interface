//go:build !windows

package spool

import (
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// processImage は pid のプロセスの実行ファイルの名前と、プロセスがあるか。名前が取れなければ "" 。
//
// あるかどうかは kill(pid, 0) で確かめる（EPERM は「あるが送れない」なので、あるとみなす）。
// 名前は ps に頼る（標準ライブラリだけで macOS のプロセス名を取る手段が無いため）。
func processImage(pid int) (string, bool) {
	if err := syscall.Kill(pid, 0); err != nil && !errors.Is(err, syscall.EPERM) {
		return "", false
	}
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "comm=").Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			// ps は該当が無いと 1 で終わる（kill のあとに終わった）。
			return "", false
		}
		return "", true
	}
	return strings.TrimSpace(string(out)), true
}
