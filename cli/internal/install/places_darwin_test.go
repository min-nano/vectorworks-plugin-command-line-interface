package install

import (
	"errors"
	"os/user"
	"strings"
	"testing"
)

// getconf と passwd の項目が応答しない場合・絶対パスでない場合は、場所を決定せずに失敗とする。
func TestPlacesWhenTheOSDoesNotAnswer(t *testing.T) {
	originalGetconf, originalUser := getconf, currentUser
	t.Cleanup(func() { getconf, currentUser = originalGetconf, originalUser })

	getconf = func() ([]byte, error) { return nil, errors.New("no getconf") }
	if _, err := WorkDir(); err == nil {
		t.Error("WorkDir should fail without getconf")
	}
	getconf = func() ([]byte, error) { return []byte("relative\n"), nil }
	if _, err := WorkDir(); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Error(err)
	}

	currentUser = func() (*user.User, error) { return nil, errors.New("no user") }
	if _, err := DefaultPluginsDir(); err == nil {
		t.Error("DefaultPluginsDir should fail without the user")
	}
	currentUser = func() (*user.User, error) { return &user.User{HomeDir: "relative"}, nil }
	if _, err := DefaultPluginsDir(); err == nil || !strings.Contains(err.Error(), "home") {
		t.Error(err)
	}
}

// mac は計算機の種類で決定し、プロセスの種類では決定しない（universal の CLI が Rosetta の下で
// x86_64 として実行されても、Vectorworks は arm64 で実行される）。
func TestTargetFollowsTheMachine(t *testing.T) {
	for arm64, want := range map[bool]string{true: "macos-arm64", false: "macos-x64"} {
		if got := targetOf(func() bool { return arm64 }); got != want {
			t.Errorf("arm64 %v: %q", arm64, got)
		}
	}
}
