package spool

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
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

// 2 つの橋が同じスプールを見ても（Windows で Vectorworks を 2 つ起動したとき）、要求は
// どちらか一方だけが確保して応え、偽の失敗が先に届かないことを確かめる。
func TestTwoBridgesDoNotAnswerWithFalseFailure(t *testing.T) {
	dir := newSpoolDir(t)
	startFake(t, dir, echo)
	startFake(t, dir, echo)
	bridge, _, err := Find([]string{dir}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		response, err := bridge.Call("ping", nil, 5*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if !response.OK {
			t.Fatalf("call %d: false failure: %s", i, response.Error)
		}
	}
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

// fakeProcess は印の pid のプロセスが動いているかを差し替える。
func fakeProcess(t *testing.T, running bool) {
	t.Helper()
	saved := ProcessRunning
	ProcessRunning = func(int) bool { return running }
	t.Cleanup(func() { ProcessRunning = saved })
}

func staleStatus() map[string]any {
	return liveStatus(map[string]any{"beat": float64(time.Now().Unix() - StaleSeconds - 5)})
}

func TestStaleStatusWithoutProcessIsDown(t *testing.T) {
	fakeProcess(t, false)
	dir := newSpoolDir(t)
	writeStatus(t, dir, staleStatus())
	if _, _, err := Find([]string{dir}, time.Now()); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("stale status without the process should be down: %v", err)
	}
}

func TestStaleStatusWithProcessIsUnresponsive(t *testing.T) {
	fakeProcess(t, true)
	dir := newSpoolDir(t)
	writeStatus(t, dir, staleStatus())
	bridge, _, err := Find([]string{dir}, time.Now())
	if err != nil || bridge.State != StateUnresponsive {
		t.Fatalf("want unresponsive, got %+v %v", bridge, err)
	}
}

func TestFindPrefersLiveOverUnresponsive(t *testing.T) {
	fakeProcess(t, true)
	stale := newSpoolDir(t)
	writeStatus(t, stale, staleStatus())
	live := newSpoolDir(t)
	writeStatus(t, live, liveStatus(nil))
	bridge, searched, err := Find([]string{stale, live}, time.Now())
	if err != nil || bridge.Dir != live || bridge.State != StateLive {
		t.Fatalf("want live, got %+v %v", bridge, err)
	}
	if len(searched) != 1 || searched[0].Reason != "unresponsive" {
		t.Fatalf("unexpected searched: %+v", searched)
	}
}

func TestCallWhileUnresponsiveIsServedWhenItRecovers(t *testing.T) {
	fakeProcess(t, true)
	dir := newSpoolDir(t)
	writeStatus(t, dir, staleStatus())
	bridge, _, _ := Find([]string{dir}, time.Now())
	// ダイアログが閉じて受け付けが戻るのを真似る: しばらくしてから応える。
	go func() {
		time.Sleep(300 * time.Millisecond)
		startFake(t, dir, func(id, tool string, args map[string]any) Response {
			return Response{OK: true, Result: json.RawMessage(`{}`)}
		})
	}()
	response, err := bridge.Call("ping", nil, 5*time.Second)
	if err != nil || !response.OK {
		t.Fatalf("%+v %v", response, err)
	}
}

func TestCallTimeoutWhileUnresponsive(t *testing.T) {
	fakeProcess(t, true)
	dir := newSpoolDir(t)
	writeStatus(t, dir, staleStatus())
	bridge, _, _ := Find([]string{dir}, time.Now())
	_, err := bridge.Call("ping", nil, 200*time.Millisecond)
	if !errors.Is(err, ErrUnresponsive) {
		t.Fatalf("want ErrUnresponsive, got %v", err)
	}
	assertNoFiles(t, dir, RequestSuffix)
}

func TestProcessRunning(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("relies on ps")
	}
	if _, exists := processImage(os.Getpid()); !exists {
		t.Fatal("own process should exist")
	}
	// テストの実行ファイルは Vectorworks ではない（pid の再利用を見分ける）。
	if vectorworksRunning(os.Getpid()) {
		t.Fatal("test binary should not be taken for Vectorworks")
	}
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Skip(err)
	}
	if _, exists := processImage(cmd.Process.Pid); exists {
		t.Fatal("finished process should not exist")
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

func TestFindReportsFreshShellDiagnostic(t *testing.T) {
	fresh := newSpoolDir(t)
	stale := newSpoolDir(t)
	write := func(dir string, beat int64) {
		text, _ := json.Marshal(map[string]any{
			"plugin": "cli", "protocol": ProtocolVersion, "beat": float64(beat), "pid": 4242,
			"error": map[string]any{"code": "abi_mismatch", "message": "payload abi 2, shell abi 1"},
		})
		if err := os.WriteFile(filepath.Join(dir, ShellFile), text, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(fresh, time.Now().Unix())
	write(stale, time.Now().Unix()-StaleSeconds-5)

	_, searched, err := Find([]string{stale, fresh}, time.Now())
	if !errors.Is(err, ErrNotRunning) {
		t.Fatalf("a shell diagnostic is not a live bridge: %v", err)
	}
	if len(searched) != 2 || searched[0].Shell != nil || searched[1].Shell == nil {
		t.Fatalf("unexpected searched: %+v", searched)
	}
	if !strings.Contains(string(searched[1].Shell), "abi_mismatch") {
		t.Fatalf("shell diagnostic not passed through: %s", searched[1].Shell)
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
