//go:build !darwin && !windows

package install

import (
	"errors"
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

func machineIsArm64() bool {
	return false
}

func target(func() bool) (string, error) {
	return "", errUnsupported
}
