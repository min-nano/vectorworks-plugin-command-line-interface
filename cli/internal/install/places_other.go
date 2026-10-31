//go:build !darwin && !windows

package install

import (
	"errors"
	"fmt"
	"runtime"
)

// Vectorworks の無い OS では場所もターゲットも決まらない（テストは場所とターゲットを渡す）。
var errUnsupported = errors.New("Vectorworks 2026 does not run on " + runtime.GOOS)

func userTempDir() (string, error) {
	return "", errUnsupported
}

func defaultPluginsDir() (string, error) {
	return "", errUnsupported
}

// target は <OS>-<アーキテクチャ>。どのマニフェストにも無いので、install は「この環境には非対応」で
// 何も変えずに終わる。
func target() string {
	return runtime.GOOS + "-" + runtime.GOARCH
}

// oldCLIName は、よけた CLI の名前の頭（oldCLIPrefixes の 1 つ）。
const oldCLIName = "vw2026.old-"

// defaultInterpreter は、フックの 1 つ目を走らせる argv の頭。/bin/sh だけ。
func defaultInterpreter(name string) ([]string, error) {
	if name == "sh" {
		return []string{"/bin/sh"}, nil
	}
	return nil, fmt.Errorf("%s hooks do not run on %s", name, runtime.GOOS)
}
