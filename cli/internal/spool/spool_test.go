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
	// 応答と .wait は読んだあと消す。
	assertNoFiles(t, dir, ResponseSuffix)
	assertNoFiles(t, dir, WaitSuffix)
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
		return Response{OK: false, Code: CodeUnknownTool, Error: "unknown tool: " + tool}
	})
	response, err := Open(dir).Call("nope", nil, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if response.OK || response.Code != CodeUnknownTool || response.Error != "unknown tool: nope" {
		t.Fatalf("unexpected response: %+v", response)
	}
}

// 待ちきれなかった要求は、.wait をやめてから消す（誰も待たない要求も応答も残さない）。
func TestCallTimeoutWithdrawsRequest(t *testing.T) {
	dir := fakeplugin.NewDir(t)
	fakeplugin.HoldLock(t, dir)
	// 動いているが応えない（ダイアログの最中など）
	_, err := Open(dir).Call("ping", nil, 200*time.Millisecond)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("want ErrTimeout, got %v", err)
	}
	assertNoFiles(t, dir, RequestSuffix)
	assertNoFiles(t, dir, WaitSuffix)
}

// 猶予の間に届いた no_wait は「実行しなかった」なので、道具の失敗ではなく ErrTimeout。
func TestCallNoWaitDuringGraceIsTimeout(t *testing.T) {
	dir := fakeplugin.NewDir(t)
	fakeplugin.HoldLock(t, dir)
	taken := takeRequest(t, dir)
	go func() {
		id := <-taken
		time.Sleep(400 * time.Millisecond) // 締切（200 ms）を過ぎ、.wait が消えてから断る
		_ = os.WriteFile(filepath.Join(dir, id+ResponseSuffix), []byte(`{"ok":false,"code":"no_wait","error":"no wait file"}`), 0o600)
	}()
	_, err := Open(dir).Call("ping", nil, 200*time.Millisecond)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("want ErrTimeout, got %v", err)
	}
	assertNoFiles(t, dir, ResponseSuffix)
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

// takeRequest はプラグインが要求を受け取ったこと（読んで消した）を真似る。応答は書かない。
// 受け取った要求の id を返す。
func takeRequest(t *testing.T, dir string) <-chan string {
	t.Helper()
	taken := make(chan string, 1)
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	go func() {
		for {
			matches, _ := filepath.Glob(filepath.Join(dir, "*"+RequestSuffix))
			if len(matches) > 0 && os.Remove(matches[0]) == nil {
				taken <- strings.TrimSuffix(filepath.Base(matches[0]), RequestSuffix)
				return
			}
			select {
			case <-stop:
				return
			case <-time.After(10 * time.Millisecond):
			}
		}
	}()
	return taken
}

// answer は要求を受け取り、text を応答として書く（プラグインを真似る）。
func answer(t *testing.T, dir, text string) {
	t.Helper()
	taken := takeRequest(t, dir)
	go func() {
		if id, ok := <-taken; ok {
			_ = os.WriteFile(filepath.Join(dir, id+ResponseSuffix), []byte(text), 0o600)
		}
	}()
}

// 待つのをやめたときに要求が既に受け取られていて、猶予の間にも応答が無ければ ErrNoResponse
// （「実行されていない」と区別する）。
func TestCallTakenWithoutResponse(t *testing.T) {
	SetTakenGrace(t, 200*time.Millisecond)
	dir := fakeplugin.NewDir(t)
	fakeplugin.HoldLock(t, dir)
	taken := takeRequest(t, dir)
	_, err := Open(dir).Call("ping", nil, 200*time.Millisecond)
	if !errors.Is(err, ErrNoResponse) || errors.Is(err, ErrTimeout) {
		t.Fatalf("want ErrNoResponse, got %v", err)
	}
	<-taken
	assertNoFiles(t, dir, WaitSuffix)
}

// 受け取られた要求の応答が締切のあと猶予の内に届けば、それを返す。
func TestCallTakenThenAnswered(t *testing.T) {
	dir := fakeplugin.NewDir(t)
	fakeplugin.HoldLock(t, dir)
	taken := takeRequest(t, dir)
	go func() {
		id := <-taken
		time.Sleep(400 * time.Millisecond) // 締切（200 ms）を過ぎてから応える
		_ = os.WriteFile(filepath.Join(dir, id+ResponseSuffix), []byte(`{"ok":true,"result":{"late":1}}`), 0o600)
	}()
	response, err := Open(dir).Call("ping", nil, 200*time.Millisecond)
	if err != nil || !response.OK || string(response.Result) != `{"late":1}` {
		t.Fatalf("%+v %v", response, err)
	}
	assertNoFiles(t, dir, WaitSuffix)
}

// 待つ者の居なくなった要求（.wait が放されている）は、実行も応答もされない。
func TestRequestOfDeadCallerIsDropped(t *testing.T) {
	dir := fakeplugin.NewDir(t)
	called := make(chan string, 1)
	fakeplugin.Start(t, dir, func(tool string, args json.RawMessage) Response {
		called <- tool
		return Response{OK: true}
	})
	id := NewID(time.Now())
	// 異常終了した呼ぶ側の残したもの: 掴まれていない .wait と要求。
	if err := os.WriteFile(filepath.Join(dir, id+WaitSuffix), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+RequestSuffix), []byte(`{"tool":"ping"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, errReq := os.Stat(filepath.Join(dir, id+RequestSuffix))
		_, errWait := os.Stat(filepath.Join(dir, id+WaitSuffix))
		if os.IsNotExist(errReq) && os.IsNotExist(errWait) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the request and the .wait should be removed")
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	select {
	case tool := <-called:
		t.Fatalf("%s must not run", tool)
	default:
	}
	assertNoFiles(t, dir, ResponseSuffix)
}

// .wait の無い要求は実行せず、no_wait で断る（置き忘れた呼ぶ側に理由が伝わるように）。
func TestRequestWithoutWaitIsRefused(t *testing.T) {
	dir := fakeplugin.NewDir(t)
	called := make(chan string, 1)
	fakeplugin.Start(t, dir, func(tool string, args json.RawMessage) Response {
		called <- tool
		return Response{OK: true}
	})
	id := NewID(time.Now())
	if err := os.WriteFile(filepath.Join(dir, id+RequestSuffix), []byte(`{"tool":"ping"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	responsePath := filepath.Join(dir, id+ResponseSuffix)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if text, err := os.ReadFile(responsePath); err == nil {
			var response Response
			if err := json.Unmarshal(text, &response); err != nil || response.OK || response.Code != CodeNoWait {
				t.Fatalf("want no_wait, got %s", text)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no response")
		}
		time.Sleep(20 * time.Millisecond)
	}
	select {
	case tool := <-called:
		t.Fatalf("%s must not run", tool)
	default:
	}
}

// 応答の知らないフィールドは無視する（docs/protocol.md「互換性」）。
func TestResponseIgnoresUnknownFields(t *testing.T) {
	dir := fakeplugin.NewDir(t)
	fakeplugin.HoldLock(t, dir)
	answer(t, dir, `{"ok":true,"result":{},"future":[1]}`)
	response, err := Open(dir).Call("ping", nil, 5*time.Second)
	if err != nil || !response.OK {
		t.Fatalf("%+v %v", response, err)
	}
}

// 失敗の応答の code が無い・知らない値なら、道具の失敗ではなく ErrMalformedResponse
// （知っているフィールドの未知の値はエラー。docs/protocol.md「互換性」）。
func TestCallRejectsUnknownCode(t *testing.T) {
	for _, text := range []string{`{"ok":false,"error":"x"}`, `{"ok":false,"code":"later","error":"x"}`} {
		dir := fakeplugin.NewDir(t)
		fakeplugin.HoldLock(t, dir)
		answer(t, dir, text)
		_, err := Open(dir).Call("ping", nil, 5*time.Second)
		if !errors.Is(err, ErrMalformedResponse) {
			t.Fatalf("%s: want ErrMalformedResponse, got %v", text, err)
		}
	}
}
