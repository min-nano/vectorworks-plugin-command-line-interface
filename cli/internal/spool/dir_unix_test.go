//go:build !windows

package spool

import (
	"os/user"
	"path/filepath"
	"testing"
)

// スプールは passwd の項目のホームの下に決まり、HOME を変えても動かない。
func TestDefaultDirIgnoresHome(t *testing.T) {
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(current.HomeDir, "Library", "Application Support", AppDirName, SpoolDirName)
	t.Setenv("HOME", t.TempDir())
	got, err := DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("DefaultDir() = %s, want %s", got, want)
	}
}
