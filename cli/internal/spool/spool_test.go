package spool

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func echo(id, tool string, args map[string]any) Response {
	result, _ := json.Marshal(map[string]any{"tool": tool, "args": args})
	return Response{OK: true, Result: result}
}

func TestCallRoundTrip(t *testing.T) {
	dir := newSpoolDir(t)
	startFake(t, dir, echo)
	bridge, _, err := Find([]string{dir}, time.Now())
	if err != nil {
		t.Fatal(err)
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
	dir := newSpoolDir(t)
	startFake(t, dir, echo)
	bridge, _, _ := Find([]string{dir}, time.Now())
	response, err := bridge.Call("ping", nil, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(response.Result), `"args":{}`) {
		t.Fatalf("args should be an empty object: %s", response.Result)
	}
}

func TestCallToolFailure(t *testing.T) {
	dir := newSpoolDir(t)
	startFake(t, dir, func(id, tool string, args map[string]any) Response {
		return Response{OK: false, Error: "unknown tool: " + tool}
	})
	bridge, _, _ := Find([]string{dir}, time.Now())
	response, err := bridge.Call("nope", nil, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if response.OK || response.Error != "unknown tool: nope" {
		t.Fatalf("unexpected response: %+v", response)
	}
}

func TestCallTimeoutWithdrawsRequest(t *testing.T) {
	dir := newSpoolDir(t)
	writeStatus(t, dir, liveStatus(nil)) // 印はあるが誰も応えない
	bridge, _, err := Find([]string{dir}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	_, err = bridge.Call("ping", nil, 200*time.Millisecond)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("want ErrTimeout, got %v", err)
	}
	assertNoFiles(t, dir, RequestSuffix)
}

func TestCallWaitsWhileBusyWithThisRequest(t *testing.T) {
	dir := newSpoolDir(t)
	writeStatus(t, dir, liveStatus(nil))
	bridge, _, _ := Find([]string{dir}, time.Now())

	// 長く走る道具を真似る: 要求を取り出したら busy_id を書き、締切を過ぎてから応答する。
	go func() {
		var id string
		for id == "" {
			entries, _ := os.ReadDir(dir)
			for _, entry := range entries {
				if strings.HasSuffix(entry.Name(), RequestSuffix) {
					id = strings.TrimSuffix(entry.Name(), RequestSuffix)
					_ = os.Remove(filepath.Join(dir, entry.Name()))
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
		writeStatus(t, dir, liveStatus(map[string]any{
			"busy": "long", "busy_id": id, "busy_until": float64(time.Now().Add(5 * time.Second).Unix()),
		}))
		time.Sleep(600 * time.Millisecond)
		out, _ := json.Marshal(Response{ID: id, OK: true, Result: json.RawMessage(`{"done":true}`)})
		_ = os.WriteFile(filepath.Join(dir, id+ResponseSuffix), out, 0o600)
	}()

	response, err := bridge.Call("long", nil, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("should keep waiting while busy with this request: %v", err)
	}
	if string(response.Result) != `{"done":true}` {
		t.Fatalf("unexpected result: %s", response.Result)
	}
}

func TestCallBusyWithAnotherRequestDoesNotExtend(t *testing.T) {
	dir := newSpoolDir(t)
	writeStatus(t, dir, liveStatus(map[string]any{
		"busy": "long", "busy_id": "someone-else", "busy_until": float64(time.Now().Add(5 * time.Second).Unix()),
	}))
	bridge, _, _ := Find([]string{dir}, time.Now())
	start := time.Now()
	_, err := bridge.Call("ping", nil, 200*time.Millisecond)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("want ErrTimeout, got %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("waited for another request's busy_until")
	}
}

func TestProtocolMismatch(t *testing.T) {
	dir := newSpoolDir(t)
	writeStatus(t, dir, liveStatus(map[string]any{"protocol": ProtocolVersion + 1}))
	bridge, _, err := Find([]string{dir}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	_, err = bridge.Call("ping", nil, time.Second)
	var protocol *ProtocolError
	if !errors.As(err, &protocol) {
		t.Fatalf("want ProtocolError, got %v", err)
	}
	assertNoFiles(t, dir, RequestSuffix)
}

func TestStaleStatusIsNotLive(t *testing.T) {
	dir := newSpoolDir(t)
	writeStatus(t, dir, liveStatus(map[string]any{"beat": float64(time.Now().Unix() - StaleSeconds - 5)}))
	if _, _, err := Find([]string{dir}, time.Now()); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("stale status should not be live: %v", err)
	}
}

func TestStaleButBusyIsLive(t *testing.T) {
	dir := newSpoolDir(t)
	writeStatus(t, dir, liveStatus(map[string]any{
		"beat":       float64(time.Now().Unix() - 120),
		"busy":       "long",
		"busy_until": float64(time.Now().Unix() + 60),
	}))
	if _, _, err := Find([]string{dir}, time.Now()); err != nil {
		t.Fatalf("busy status should be live: %v", err)
	}
}

func TestFindSkipsUnsafeAndPicksFirstLive(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not checked on Windows")
	}
	unsafe := newSpoolDir(t)
	writeStatus(t, unsafe, liveStatus(nil))
	if err := os.Chmod(unsafe, 0o777); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "missing")
	good := newSpoolDir(t)
	writeStatus(t, good, liveStatus(nil))

	bridge, searched, err := Find([]string{missing, unsafe, good}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if bridge.Dir != good {
		t.Fatalf("picked %s", bridge.Dir)
	}
	if len(searched) != 2 || searched[0].Reason != "not found" || searched[1].Reason != "writable by others" {
		t.Fatalf("unexpected searched: %+v", searched)
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

func TestCandidatesOverride(t *testing.T) {
	got := Candidates("stable", "/somewhere")
	if len(got) != 1 || got[0] != "/somewhere" {
		t.Fatalf("override should be the only candidate: %v", got)
	}
}

func TestCandidatesUsesTempDirAndPluginName(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TMPDIR", root)
	t.Setenv("TMP", root)
	t.Setenv("TEMP", root)
	if runtime.GOOS == "darwin" {
		return // 利用者ごとの一時ディレクトリが先頭に来る
	}
	for channel, name := range map[string]string{"": StableSpoolName, "stable": StableSpoolName, "dev": DevSpoolName} {
		got := Candidates(channel, "")
		want := filepath.Join(root, name)
		if len(got) != 1 || got[0] != want {
			t.Fatalf("%q: got %v, want [%s]", channel, got, want)
		}
	}
	if got := Candidates("nightly", ""); len(got) != 0 {
		t.Fatalf("unknown channel should have no candidates: %v", got)
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
