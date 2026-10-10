package spool_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/fakeplugin"
	. "github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/spool"
)

func echo(tool string, args json.RawMessage) Response {
	result, _ := json.Marshal(map[string]any{"tool": tool, "args": args})
	return Response{OK: true, Result: result}
}

func TestCallRoundTrip(t *testing.T) {
	dir := fakeplugin.NewDir(t)
	fakeplugin.Start(t, dir, echo)
	bridge := Open(dir)
	if !bridge.Running {
		t.Fatalf("want running, got %+v", bridge)
	}
	response, err := bridge.Call("layers", json.RawMessage(`{"include_sheets":false}`), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !response.OK {
		t.Fatalf("not ok: %s", response.Error)
	}
	var got struct {
		Tool string         `json:"tool"`
		Args map[string]any `json:"args"`
	}
	if err := json.Unmarshal(response.Result, &got); err != nil {
		t.Fatal(err)
	}
	if got.Tool != "layers" || got.Args["include_sheets"] != false {
		t.Fatalf("unexpected result: %s", response.Result)
	}
	// 応答は読んだあと消す。
	assertNoFiles(t, dir, ResponseSuffix)
}

func TestCallEmptyArgsBecomesObject(t *testing.T) {
	dir := fakeplugin.NewDir(t)
	fakeplugin.Start(t, dir, echo)
	response, err := Open(dir).Call("ping", nil, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(response.Result), `"args":{}`) {
		t.Fatalf("args should be an empty object: %s", response.Result)
	}
}

func TestCallToolFailure(t *testing.T) {
	dir := fakeplugin.NewDir(t)
	fakeplugin.Start(t, dir, func(tool string, args json.RawMessage) Response {
		return Response{OK: false, Error: "unknown tool: " + tool}
	})
	response, err := Open(dir).Call("nope", nil, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if response.OK || response.Error != "unknown tool: nope" {
		t.Fatalf("unexpected response: %+v", response)
	}
}

func TestCallTimeoutWithdrawsRequest(t *testing.T) {
	dir := fakeplugin.NewDir(t)
	fakeplugin.HoldLock(t, dir)
	// 動いているが応えない（ダイアログの最中など）
	_, err := Open(dir).Call("ping", nil, 200*time.Millisecond)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("want ErrTimeout, got %v", err)
	}
	assertNoFiles(t, dir, RequestSuffix)
}

// ロックファイルがあっても、掴まれていなければ（異常終了のあと）止まっている。
func TestLockFileWithoutHolderIsDown(t *testing.T) {
	dir := fakeplugin.NewDir(t)
	if err := os.WriteFile(filepath.Join(dir, LockFile), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if bridge := Open(dir); bridge.Running || bridge.Reason != "not running" {
		t.Fatalf("want down, got %+v", bridge)
	}
}

func TestReleasedLockIsDown(t *testing.T) {
	dir := fakeplugin.NewDir(t)
	release := fakeplugin.HoldLock(t, dir)
	if !Open(dir).Running {
		t.Fatal("want running")
	}
	release()
	if Open(dir).Running {
		t.Fatal("want down after release")
	}
}

func TestCallIsServedWhenServingResumes(t *testing.T) {
	dir := fakeplugin.NewDir(t)
	release := fakeplugin.HoldLock(t, dir)
	bridge := Open(dir)
	// ダイアログが閉じて受け付けが戻るのを真似る: 要求を置いたあとで応え始める。
	go func() {
		time.Sleep(300 * time.Millisecond)
		release()
		fakeplugin.Start(t, dir, echo)
	}()
	response, err := bridge.Call("ping", nil, 5*time.Second)
	if err != nil || !response.OK {
		t.Fatalf("%+v %v", response, err)
	}
}

func TestCallTimeoutWhenStopped(t *testing.T) {
	dir := fakeplugin.NewDir(t)
	release := fakeplugin.HoldLock(t, dir)
	bridge := Open(dir)
	release()
	_, err := bridge.Call("ping", nil, 200*time.Millisecond)
	if !errors.Is(err, ErrNotRunning) {
		t.Fatalf("want ErrNotRunning, got %v", err)
	}
}

func TestMissingDirIsDown(t *testing.T) {
	if bridge := Open(filepath.Join(t.TempDir(), "missing")); bridge.Running || bridge.Reason != "not found" {
		t.Fatalf("want not found, got %+v", bridge)
	}
}

func TestDefaultDir(t *testing.T) {
	dir, err := DefaultDir()
	if err != nil {
		t.Skip(err)
	}
	if filepath.Base(dir) != SpoolDirName || filepath.Base(filepath.Dir(dir)) != AppDirName {
		t.Fatalf("unexpected spool dir: %s", dir)
	}
}

func TestNewIDIsValidAndOrdered(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	first := NewID(base)
	second := NewID(base.Add(time.Nanosecond))
	if !ValidID(first) || !ValidID(second) {
		t.Fatalf("invalid ids: %s %s", first, second)
	}
	if !(first < second) {
		t.Fatalf("ids must sort in send order: %s %s", first, second)
	}
}

func TestValidID(t *testing.T) {
	for _, bad := range []string{"", "../x", "a/b", "a.b", strings.Repeat("a", 65)} {
		if ValidID(bad) {
			t.Errorf("%q should be invalid", bad)
		}
	}
	if !ValidID("0001-abc_DEF") {
		t.Error("expected valid")
	}
}

func assertNoFiles(t *testing.T, dir, suffix string) {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), suffix) {
			t.Fatalf("leftover %s", entry.Name())
		}
	}
}

// 読み手は、読めない・JSON として壊れている応答を「まだ無い」とみなして読み直す
// （docs/protocol.md「ファイル」）。作法に反して rename を経ずに書く書き手が居ても、
// 書きかけを応答として返さない。
func TestCallRereadsTornResponse(t *testing.T) {
	dir := fakeplugin.NewDir(t)
	fakeplugin.HoldLock(t, dir)
	go func() {
		var request string
		for request == "" {
			matches, _ := filepath.Glob(filepath.Join(dir, "*"+RequestSuffix))
			if len(matches) > 0 {
				request = matches[0]
			}
			time.Sleep(10 * time.Millisecond)
		}
		response := strings.TrimSuffix(request, RequestSuffix) + ResponseSuffix
		_ = os.WriteFile(response, []byte(`{"ok":tr`), 0o600)
		time.Sleep(300 * time.Millisecond)
		_ = os.WriteFile(response, []byte(`{"ok":true,"result":{"done":1}}`), 0o600)
	}()
	response, err := Open(dir).Call("ping", nil, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !response.OK || string(response.Result) != `{"done":1}` {
		t.Fatalf("unexpected response: %+v", response)
	}
}

// 上限を超える要求は、スプールに置く前に断る。
func TestCallRejectsOversizedRequest(t *testing.T) {
	dir := fakeplugin.NewDir(t)
	fakeplugin.HoldLock(t, dir)
	big := json.RawMessage(`{"s":"` + strings.Repeat("x", MaxRequestBytes) + `"}`)
	_, err := Open(dir).Call("ping", big, time.Second)
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("want a size error, got %v", err)
	}
	assertNoFiles(t, dir, RequestSuffix)
	assertNoFiles(t, dir, TempSuffix)
}
