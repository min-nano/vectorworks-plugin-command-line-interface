package install

import (
	"errors"
	"os/user"
	"strings"
	"testing"
)

// getconf と passwd の項目が答えないとき・絶対パスでないときは、場所を決めずに失敗にする。
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
