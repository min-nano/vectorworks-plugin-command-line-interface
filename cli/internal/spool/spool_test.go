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
	fakeplugin.WriteStatus(t, dir, nil) // 動いているが応えない（ダイアログの最中など）
	_, err := Open(dir).Call("ping", nil, 200*time.Millisecond)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("want ErrTimeout, got %v", err)
	}
	assertNoFiles(t, dir, RequestSuffix)
}

func TestProtocolMismatch(t *testing.T) {
	dir := fakeplugin.NewDir(t)
	fakeplugin.HoldLock(t, dir)
	fakeplugin.WriteStatus(t, dir, map[string]any{"protocol": ProtocolVersion + 1})
	_, err := Open(dir).Call("ping", nil, time.Second)
	var protocol *ProtocolError
	if !errors.As(err, &protocol) {
		t.Fatalf("want ProtocolError, got %v", err)
	}
	assertNoFiles(t, dir, RequestSuffix)
}

// 印があっても、ロックが掴まれていなければ（異常終了で残った印）止まっている。
func TestStatusWithoutLockIsDown(t *testing.T) {
	dir := fakeplugin.NewDir(t)
	fakeplugin.WriteStatus(t, dir, nil)
	if bridge := Open(dir); bridge.Running || bridge.Reason != "not running" {
		t.Fatalf("want down, got %+v", bridge)
	}
}

func TestReleasedLockIsDown(t *testing.T) {
	dir := fakeplugin.NewDir(t)
	release := fakeplugin.HoldLock(t, dir)
	fakeplugin.WriteStatus(t, dir, nil)
	if !Open(dir).Running {
		t.Fatal("want running")
	}
	release()
	if Open(dir).Running {
		t.Fatal("want down after release")
	}
}

// ロックを取ってから印を書くまでの間も、動いている（止まったと誤らない）。
func TestLockWithoutStatusIsRunning(t *testing.T) {
	dir := fakeplugin.NewDir(t)
	fakeplugin.HoldLock(t, dir)
	bridge := Open(dir)
	if !bridge.Running || bridge.Status != nil {
		t.Fatalf("want running without status, got %+v", bridge)
	}
	if err := bridge.CheckProtocol(); err != nil {
		t.Fatalf("unknown protocol should pass: %v", err)
	}
}

func TestCallIsServedWhenServingResumes(t *testing.T) {
	dir := fakeplugin.NewDir(t)
	release := fakeplugin.HoldLock(t, dir)
	fakeplugin.WriteStatus(t, dir, nil)
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
	fakeplugin.WriteStatus(t, dir, nil)
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
