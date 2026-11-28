package fakeplugin

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/spool"
)

// 名前が長すぎて待つ印を置けない id（書きかけの名前が上限の長さ）は、待つ印が無いものとして掃除が消す
// （プラグインの BridgeSweepRemovesFilesWhoseWaitCannotExist と同じ）。
func TestSweepRemovesFilesWhoseWaitCannotExist(t *testing.T) {
	dir := t.TempDir()
	id := strings.Repeat("a", 251)
	temp := filepath.Join(dir, id+spool.TempSuffix)
	if err := os.WriteFile(temp, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if state := waitStateOf(filepath.Join(dir, id+spool.WaitSuffix)); state != waitMissing {
		t.Fatalf("state %d", state)
	}
	sweep(dir)
	if _, err := os.Lstat(temp); !os.IsNotExist(err) {
		t.Fatal("left the file", err)
	}
}

// 無い以外の理由で開けない待つ印は、確かめられない。掃除はその id のファイルを残し、要求は実行せずに no_wait で
// 応える（プラグインの BridgeKeepsTheFilesOfAWaitItCannotCheck と同じ）。root は権限を無視して開けるので確かめない。
func TestWaitThatCannotBeChecked(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a file that the user cannot open")
	}
	dir := t.TempDir()
	wait := filepath.Join(dir, "u"+spool.WaitSuffix)
	response := filepath.Join(dir, "u"+spool.ResponseSuffix)
	for _, path := range []string{wait, response} {
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(wait, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(wait, 0o600) })
	if state := waitStateOf(wait); state != waitUnknown {
		t.Fatalf("state %d", state)
	}
	sweep(dir)
	for _, path := range []string{wait, response} {
		if _, err := os.Lstat(path); err != nil {
			t.Errorf("removed %s", filepath.Base(path))
		}
	}
}

// 通常のファイルでない待つ印（フォルダ）は無いものとする（どの OS でも同じ）。
func TestWaitThatIsAFolderIsMissing(t *testing.T) {
	dir := t.TempDir()
	wait := filepath.Join(dir, "d"+spool.WaitSuffix)
	if err := os.Mkdir(wait, 0o700); err != nil {
		t.Fatal(err)
	}
	if state := waitStateOf(wait); state != waitMissing {
		t.Fatalf("state %d", state)
	}
}
