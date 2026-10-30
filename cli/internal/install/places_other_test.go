//go:build !darwin && !windows

package install

import (
	"errors"
	"testing"
)

// Vectorworks の無い OS では、場所もターゲットも決めずに失敗にする。
func TestPlacesOnAnotherOS(t *testing.T) {
	if _, err := WorkDir(); !errors.Is(err, errUnsupported) {
		t.Error(err)
	}
	if _, err := DefaultPluginsDir(); !errors.Is(err, errUnsupported) {
		t.Error(err)
	}
	if _, err := Target(); !errors.Is(err, errUnsupported) || machineIsArm64() {
		t.Error(err)
	}
}
