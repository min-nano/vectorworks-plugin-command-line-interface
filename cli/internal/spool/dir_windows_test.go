//go:build windows

package spool

import (
	"path/filepath"
	"testing"
)

// スプールは SHGetKnownFolderPath で決まり、LOCALAPPDATA を変えても動かない。
func TestDefaultDirIgnoresLocalAppData(t *testing.T) {
	want, err := DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(want) || filepath.Base(want) != SpoolDirName || filepath.Base(filepath.Dir(want)) != AppDirName {
		t.Fatalf("unexpected spool dir: %s", want)
	}
	t.Setenv("LOCALAPPDATA", t.TempDir())
	if got, err := DefaultDir(); err != nil || got != want {
		t.Fatalf("DefaultDir() = %s, %v; want %s", got, err, want)
	}
}
