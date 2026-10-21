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
	response, err := bridge.Call("layers", json.RawMessage(`{"include_sheets":false}`), "", 5*time.Second)
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
	response, err := Open(dir).Call("ping", nil, "", 5*time.Second)
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
	response, err := Open(dir).Call("nope", nil, "", 5*time.Second)
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
	_, err := Open(dir).Call("ping", nil, "", 200*time.Millisecond)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("want ErrTimeout, got %v", err)
	}
	assertNoFiles(t, dir, RequestSuffix)
	assertNoFiles(t, dir, WaitSuffix)
}

// 猶予の間に届いた no_wait は「実行しなかった」なので、道具の失敗ではなく ErrNoWait。
func TestCallNoWaitDuringGrace(t *testing.T) {
	dir := fakeplugin.NewDir(t)
	fakeplugin.HoldLock(t, dir)
	taken := takeRequest(t, dir)
	go func() {
		id := <-taken
		// 締切（200 ms）を過ぎ、.wait が消えてから断る
		time.Sleep(400 * time.Millisecond)
		_ = WriteAtomically(filepath.Join(dir, id+ResponseSuffix), []byte(`{"ok":false,"code":"no_wait","error":"no wait file"}`))
	}()
	_, err := Open(dir).Call("ping", nil, "", 200*time.Millisecond)
	if !errors.Is(err, ErrNoWait) {
		t.Fatalf("want ErrNoWait, got %v", err)
	}
	assertNoFiles(t, dir, ResponseSuffix)
}

// 締切の前に届いた no_wait（掃除が待つ印を消したときなど）は、待たずに ErrNoWait で終わり、
// 待ちきれなかったとは言わない。
func TestCallNoWaitBeforeDeadline(t *testing.T) {
	dir := fakeplugin.NewDir(t)
	fakeplugin.HoldLock(t, dir)
	answer(t, dir, `{"ok":false,"code":"no_wait","error":"no wait file"}`)
	start := time.Now()
	_, err := Open(dir).Call("ping", nil, "", 30*time.Second)
	if !errors.Is(err, ErrNoWait) || errors.Is(err, ErrTimeout) || strings.Contains(err.Error(), "30s") {
		t.Fatalf("want ErrNoWait without a timeout, got %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("should not wait for the deadline")
	}
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

// スプールがあってもロックファイルが無ければ（プラグインが一度も掴んでいない）止まっている。
func TestDirWithoutLockFileIsDown(t *testing.T) {
	if bridge := Open(fakeplugin.NewDir(t)); bridge.Running || bridge.Reason != "not running" {
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
	response, err := bridge.Call("ping", nil, "", 5*time.Second)
	if err != nil || !response.OK {
		t.Fatalf("%+v %v", response, err)
	}
}

func TestCallTimeoutWhenStopped(t *testing.T) {
	dir := fakeplugin.NewDir(t)
	release := fakeplugin.HoldLock(t, dir)
	bridge := Open(dir)
	release()
	_, err := bridge.Call("ping", nil, "", 200*time.Millisecond)
	if !errors.Is(err, ErrNotRunning) {
		t.Fatalf("want ErrNotRunning, got %v", err)
	}
}

func TestMissingDirIsDown(t *testing.T) {
	if bridge := Open(filepath.Join(t.TempDir(), "missing")); bridge.Running || bridge.Reason != "not found" {
		t.Fatalf("want not found, got %+v", bridge)
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

// 読めない（まだ無い・開けない）応答は「まだ無い」とみなして読み直す（docs/protocol.md「ファイル」）。
// 応答の名前に開けないもの（ディレクトリ）がある間も壊れた応答とせず、あとから公開された応答を返す。
func TestCallRereadsUnreadableResponse(t *testing.T) {
	dir := fakeplugin.NewDir(t)
	fakeplugin.HoldLock(t, dir)
	taken := takeRequest(t, dir)
	go func() {
		id := <-taken
		response := filepath.Join(dir, id+ResponseSuffix)
		_ = os.Mkdir(response, 0o700)
		time.Sleep(300 * time.Millisecond)
		_ = os.Remove(response)
		_ = WriteAtomically(response, []byte(`{"ok":true,"result":{"done":1}}`))
	}()
	response, err := Open(dir).Call("ping", nil, "", 5*time.Second)
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
	_, err := Open(dir).Call("ping", big, "", time.Second)
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
			_ = WriteAtomically(filepath.Join(dir, id+ResponseSuffix), []byte(text))
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
	_, err := Open(dir).Call("ping", nil, "", 200*time.Millisecond)
	if !errors.Is(err, ErrNoResponse) || errors.Is(err, ErrTimeout) {
		t.Fatalf("want ErrNoResponse, got %v", err)
	}
	<-taken
	assertNoFiles(t, dir, WaitSuffix)
}

// 受け取られた要求の応答が締切のあと猶予の内に届けば、その応答で終わる（ErrNoResponse ではない）。
// 届いた応答が壊れていれば ErrMalformedResponse（応答は届いていて、書き手の不具合なので）。
//
// 時間に依らないよう、要求を受け取ってから、Call が待つのをやめた（.wait を消した）のを見て
// 応える。こうすると必ず猶予の分岐を通る。締切（1 秒）は受け取り（10 ms ごとの見回り）に十分な
// 長さにする。受け取りが締切に間に合わなければ、Call は要求を消して ErrTimeout で終わり、テストは
// 落ちる（どの分岐を通ったかが分かる）。
func TestCallTakenThenAnsweredInGrace(t *testing.T) {
	for _, c := range []struct {
		name, response string
		check          func(*Response, error) bool
	}{
		{"valid", `{"ok":true,"result":{"late":1}}`, func(r *Response, err error) bool {
			return err == nil && r.OK && string(r.Result) == `{"late":1}`
		}},
		{"malformed", `{"ok":tr`, func(_ *Response, err error) bool {
			return errors.Is(err, ErrMalformedResponse)
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			SetTakenGrace(t, 5*time.Second)
			dir := fakeplugin.NewDir(t)
			fakeplugin.HoldLock(t, dir)
			taken := takeRequest(t, dir)
			go func() {
				var id string
				select {
				case id = <-taken:
				case <-t.Context().Done():
					return
				}
				for exists(filepath.Join(dir, id+WaitSuffix)) {
					select {
					case <-t.Context().Done():
						return
					case <-time.After(10 * time.Millisecond):
					}
				}
				_ = WriteAtomically(filepath.Join(dir, id+ResponseSuffix), []byte(c.response))
			}()
			response, err := Open(dir).Call("ping", nil, "", time.Second)
			if !c.check(response, err) {
				t.Fatalf("%+v %v", response, err)
			}
			assertNoFiles(t, dir, ResponseSuffix)
			assertNoFiles(t, dir, WaitSuffix)
		})
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
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
	response, err := Open(dir).Call("ping", nil, "", 5*time.Second)
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
		_, err := Open(dir).Call("ping", nil, "", 5*time.Second)
		if !errors.Is(err, ErrMalformedResponse) {
			t.Fatalf("%s: want ErrMalformedResponse, got %v", text, err)
		}
	}
}

// 応答のファイルが JSON として壊れていれば、読み直さず、締切まで待たずに ErrMalformedResponse で
// 終え、ファイルを消す（rename で公開されるので待っても直らない。docs/protocol.md「ファイル」）。
func TestCallStopsOnMalformedResponse(t *testing.T) {
	dir := fakeplugin.NewDir(t)
	fakeplugin.HoldLock(t, dir)
	answer(t, dir, `{"ok":tr`)
	start := time.Now()
	_, err := Open(dir).Call("ping", nil, "", 5*time.Second)
	if !errors.Is(err, ErrMalformedResponse) {
		t.Fatalf("want ErrMalformedResponse, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("should not wait for the deadline: %s", elapsed)
	}
	assertNoFiles(t, dir, ResponseSuffix)
	assertNoFiles(t, dir, WaitSuffix)
}

// 入れ子が深すぎる要求は、スプールに置く前に断る（プラグインが invalid_request で断るので）。
func TestCallRejectsDeepRequest(t *testing.T) {
	dir := fakeplugin.NewDir(t)
	fakeplugin.HoldLock(t, dir)
	deep := json.RawMessage(`{"a":` + strings.Repeat("[", MaxNestingDepth) + strings.Repeat("]", MaxNestingDepth) + `}`)
	_, err := Open(dir).Call("ping", deep, "", time.Second)
	if err == nil || !strings.Contains(err.Error(), "nested too deeply") {
		t.Fatalf("want a depth error, got %v", err)
	}
	assertNoFiles(t, dir, RequestSuffix)
}

func TestNestingDepth(t *testing.T) {
	for text, want := range map[string]int{
		`{}`:                 1,
		`{"a":[1,{"b":[]}]}`: 4,
		`{"s":"[[[{{\"]"}`:   1,
		`"x"`:                0,
	} {
		if got := NestingDepth([]byte(text)); got != want {
			t.Errorf("%s: want %d, got %d", text, want, got)
		}
	}
}

// 書きかけの名前に hard link が置かれていても、リンクの先（スプールの外のファイル）を変えない
// （docs/protocol.md「ファイル」）。
func TestWriteAtomicallyDoesNotWriteThroughLinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "x"+RequestSuffix)
	if err := os.Link(target, path+TempSuffix); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomically(path, []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(target); string(got) != "keep" {
		t.Errorf("target = %q, want %q", got, "keep")
	}
	if got, _ := os.ReadFile(path); string(got) != "{}" {
		t.Errorf("request = %q, want %q", got, "{}")
	}
}

// 書きかけを作れなければ（同じ名前の空でないディレクトリがある）、要求を公開しない。
func TestWriteAtomicallyFailsWhenTheTempCannotBeCreated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x"+RequestSuffix)
	if err := os.MkdirAll(filepath.Join(path+TempSuffix, "keep"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomically(path, []byte("{}")); err == nil {
		t.Fatal("WriteAtomically succeeded, want an error")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("request was published: %v", err)
	}
}

// 公開（rename）できなければ、書きかけを残さない。
func TestWriteAtomicallyRemovesTheTempWhenItCannotPublish(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x"+RequestSuffix)
	if err := os.MkdirAll(filepath.Join(path, "keep"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomically(path, []byte("{}")); err == nil {
		t.Fatal("WriteAtomically succeeded, want an error")
	}
	if _, err := os.Stat(path + TempSuffix); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the temp was left: %v", err)
	}
}

// 要求を公開できなければ、どの要求かと、どの段（書きかけを開く・rename）で失敗したかを返す
// （段は os のエラーの文言が示す）。
func TestCallNamesTheRequestItCannotPublish(t *testing.T) {
	for _, c := range []struct{ blocked, stage string }{
		{"x" + RequestSuffix + TempSuffix, "open"},
		{"x" + RequestSuffix, "rename"},
	} {
		t.Run(c.stage, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.MkdirAll(filepath.Join(dir, c.blocked, "keep"), 0o700); err != nil {
				t.Fatal(err)
			}
			_, err := WithNewID(&Bridge{Dir: dir}, "x").Call("ping", nil, "", time.Second)
			want := "publish x" + RequestSuffix + ": " + c.stage + " "
			if err == nil || !strings.HasPrefix(err.Error(), want) {
				t.Fatalf("Call = %v, want an error starting with %q", err, want)
			}
		})
	}
}

// 引数が JSON として読めなければ、何も置かずに断る（vw2026 は先に確かめるが、Call を直接
// 呼ぶ側のため）。
func TestCallRejectsInvalidArgs(t *testing.T) {
	dir := fakeplugin.NewDir(t)
	fakeplugin.HoldLock(t, dir)
	_, err := Open(dir).Call("ping", json.RawMessage(`{"a":`), "", time.Second)
	if err == nil || !strings.HasPrefix(err.Error(), "encode request: ") {
		t.Fatalf("want an encode error, got %v", err)
	}
	assertNoFiles(t, dir, RequestSuffix)
	assertNoFiles(t, dir, WaitSuffix)
}

// 待つ印（.wait）を作れなければ、どの印かを返し、要求を公開しない。.wait は O_EXCL で作るので、
// 同じ名前が残っていれば作れない。
func TestCallNamesTheWaitItCannotHold(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x"+WaitSuffix), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := WithNewID(&Bridge{Dir: dir}, "x").Call("ping", nil, "", time.Second)
	if want := "hold x" + WaitSuffix + ": "; err == nil || !strings.HasPrefix(err.Error(), want) {
		t.Fatalf("Call = %v, want an error starting with %q", err, want)
	}
	assertNoFiles(t, dir, RequestSuffix)
}
