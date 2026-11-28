//go:build !darwin && !windows

package install

import (
	"errors"
	"fmt"
	"runtime"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/edition"
)

// Vectorworks の無い OS では場所もターゲットも決定されない（テストは場所とターゲットを渡す）。
var errUnsupported = errors.New(edition.Product + " does not run on " + runtime.GOOS)

func userTempDir() (string, error) {
	return "", errUnsupported
}

func defaultPluginsDir() (string, error) {
	return "", errUnsupported
}

// target は <OS>-<アーキテクチャ>。いずれのマニフェストにも無いため、install は
// 「この環境には非対応」で何も変更せずに終了する。
func target() string {
	return runtime.GOOS + "-" + runtime.GOARCH
}

// defaultInterpreter は、フックの 1 つ目を実行する argv の先頭。/bin/sh のみ。
func defaultInterpreter(name string) ([]string, error) {
	if name == "sh" {
		return []string{"/bin/sh"}, nil
	}
	return nil, fmt.Errorf("%s hooks do not run on %s", name, runtime.GOOS)
}
