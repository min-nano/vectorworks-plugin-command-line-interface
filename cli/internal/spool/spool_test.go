package spool_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
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
	// 応答と .wait は読み取った後に削除する。
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

// 待機しきれなかった要求は、.wait を中止してから削除する（誰も待機しない要求も応答も残さない）。
func TestCallTimeoutWithdrawsRequest(t *testing.T) {
	dir, _ := fakeplugin.NewHeldDir(t)
	// 動作しているが応答しない（ダイアログの最中など）
	_, err := Open(dir).Call("ping", nil, "", 200*time.Millisecond)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("want ErrTimeout, got %v", err)
	}
	assertNoFiles(t, dir, RequestSuffix)
	assertNoFiles(t, dir, WaitSuffix)
}

// 猶予の間に届いた no_wait は「実行しなかった」なので、ツールの失敗ではなく ErrNoWait。
func TestCallNoWaitDuringGrace(t *testing.T) {
	dir, _ := fakeplugin.NewHeldDir(t)
	taken := takeRequest(t, dir)
	go func() {
		id := <-taken
		// 期限（200 ms）を過ぎ、.wait が消失してから拒否する
		time.Sleep(400 * time.Millisecond)
		_ = WriteAtomically(filepath.Join(dir, id+ResponseSuffix), []byte(`{"ok":false,"code":"no_wait","error":"no wait file"}`))
	}()
	_, err := Open(dir).Call("ping", nil, "", 200*time.Millisecond)
	if !errors.Is(err, ErrNoWait) {
		t.Fatalf("want ErrNoWait, got %v", err)
	}
	assertNoFiles(t, dir, ResponseSuffix)
}

// 期限の前に届いた no_wait（掃除が待機標識を削除した場合など）は、待機せずに ErrNoWait で終了し、
// 待機しきれなかったとは示さない。
func TestCallNoWaitBeforeDeadline(t *testing.T) {
	dir, _ := fakeplugin.NewHeldDir(t)
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

// ロックファイルが存在しても、取得されていなければ（異常終了の後）停止している。
func TestLockFileWithoutHolderIsDown(t *testing.T) {
	dir := fakeplugin.NewDir(t)
	if err := os.WriteFile(filepath.Join(dir, LockFile), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if bridge := Open(dir); bridge.Running || bridge.Reason != "not running" {
		t.Fatalf("want down, got %+v", bridge)
	}
}

// スプールが存在してもロックファイルが無ければ（プラグインが一度も取得していない）停止している。
func TestDirWithoutLockFileIsDown(t *testing.T) {
	if bridge := Open(fakeplugin.NewDir(t)); bridge.Running || bridge.Reason != "not running" {
		t.Fatalf("want down, got %+v", bridge)
	}
}

// LockHeld は、取得されたロックを真、存在しないロック・解放されたロックを偽にし、いずれも
// エラーにしない。
func TestLockHeld(t *testing.T) {
	dir := fakeplugin.NewDir(t)
	if held, err := LockHeld(dir); held || err != nil {
		t.Fatal("no lock:", held, err)
	}
	if held, err := LockHeld(filepath.Join(dir, "missing")); held || err != nil {
		t.Fatal("no folder:", held, err)
	}
	release := fakeplugin.HoldLock(t, dir)
	if held, err := LockHeld(dir); !held || err != nil {
		t.Fatal("held:", held, err)
	}
	release()
	if held, err := LockHeld(dir); held || err != nil {
		t.Fatal("released:", held, err)
	}
}

// ロックを存在しない以外の理由で調査できなければ、動作しているかが判明しないというエラーを返す
// （mac・Linux では、ファイルの下のパス。Windows はそれを存在しないと応答するので、名前に
// 使用できない文字で調査できなくする）。
func TestLockHeldCannotTell(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	dir := file
	if runtime.GOOS == "windows" {
		dir = filepath.Join(t.TempDir(), "bad<name>")
	}
	if held, err := LockHeld(dir); held || err == nil {
		t.Fatal(held, err)
	}
}

func TestReleasedLockIsDown(t *testing.T) {
	dir, release := fakeplugin.NewHeldDir(t)
	if !Open(dir).Running {
		t.Fatal("want running")
	}
	release()
	if Open(dir).Running {
		t.Fatal("want down after release")
	}
}

func TestCallIsServedWhenServingResumes(t *testing.T) {
	dir, release := fakeplugin.NewHeldDir(t)
	bridge := Open(dir)
	// ダイアログが閉じて受付処理が戻るのを模倣する: 要求を配置した後で応答を開始する。
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
	dir, release := fakeplugin.NewHeldDir(t)
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

func assertNoFiles(t *testing.T, dir, suffix string) {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), suffix) {
			t.Fatalf("leftover %s", entry.Name())
		}
	}
}

// 読み取れない（未だ存在しない・開けない）応答は「未だ存在しない」とみなして読み直す
// （docs/protocol.md「ファイル」）。応答の名前に開けないもの（ディレクトリ）がある間も破損した
// 応答とせず、後から公開された応答を返す。
func TestCallRereadsUnreadableResponse(t *testing.T) {
	dir, _ := fakeplugin.NewHeldDir(t)
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

// 上限を超える要求は、スプールに配置する前に拒否する。
func TestCallRejectsOversizedRequest(t *testing.T) {
	dir, _ := fakeplugin.NewHeldDir(t)
	big := json.RawMessage(`{"s":"` + strings.Repeat("x", MaxRequestBytes) + `"}`)
	_, err := Open(dir).Call("ping", big, "", time.Second)
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("want a size error, got %v", err)
	}
	assertNoFiles(t, dir, RequestSuffix)
	assertNoFiles(t, dir, TempSuffix)
}

// takeRequest はプラグインが要求を受け取ったこと（読み取って削除した）を模倣する。応答は
// 書き込まない。受け取った要求の id を返す。
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

// answer は要求を受け取り、text を応答として書き込む（プラグインを模倣する）。
func answer(t *testing.T, dir, text string) {
	t.Helper()
	taken := takeRequest(t, dir)
	go func() {
		if id, ok := <-taken; ok {
			_ = WriteAtomically(filepath.Join(dir, id+ResponseSuffix), []byte(text))
		}
	}()
}

// 待機を中止した時点で要求が既に受け取られており、猶予の間にも応答が無ければ ErrNoResponse
// （「実行されていない」と区別する）。
func TestCallTakenWithoutResponse(t *testing.T) {
	SetTakenGrace(t, 200*time.Millisecond)
	dir, _ := fakeplugin.NewHeldDir(t)
	taken := takeRequest(t, dir)
	_, err := Open(dir).Call("ping", nil, "", 200*time.Millisecond)
	if !errors.Is(err, ErrNoResponse) || errors.Is(err, ErrTimeout) {
		t.Fatalf("want ErrNoResponse, got %v", err)
	}
	<-taken
	assertNoFiles(t, dir, WaitSuffix)
}

// 受け取られた要求の応答が期限の後、猶予の内に届けば、その応答で終了する（ErrNoResponse では
// ない）。届いた応答が破損していれば ErrMalformedResponse（応答は届いており、書き手の不具合
// なので）。
//
// 時間に依存しないよう、要求を受け取ってから、Call が待機を中止した（.wait を削除した）のを確認して
// 応答する。こうすると必ず猶予の分岐を通過する。期限（1 秒）は受け取り（10 ms ごとの巡回）に十分な
// 長さにする。受け取りが期限に間に合わなければ、Call は要求を削除して ErrTimeout で終了し、テストは
// 失敗する（いずれの分岐を通過したかが判明する）。
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
		// null は JSON として読み取れるが、応答のオブジェクトではない（届かなかったとみなして
		// 待機し続けない）。
		{"null", `null`, func(_ *Response, err error) bool {
			return errors.Is(err, ErrMalformedResponse)
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			SetTakenGrace(t, 5*time.Second)
			dir, _ := fakeplugin.NewHeldDir(t)
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

// 応答のファイルが JSON として破損していれば、読み直さず、期限まで待機せずに
// ErrMalformedResponse で終了し、ファイルを削除する（rename で公開されるので待機しても
// 修正されない。docs/protocol.md「ファイル」）。
func TestCallStopsOnMalformedResponse(t *testing.T) {
	dir, _ := fakeplugin.NewHeldDir(t)
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

// 入れ子が深すぎる要求は、スプールに配置する前に拒否する（プラグインが invalid_request で
// 拒否するので）。
func TestCallRejectsDeepRequest(t *testing.T) {
	dir, _ := fakeplugin.NewHeldDir(t)
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

// 書き込み途中のファイルの名前に hard link が配置されていても、リンクの先（スプールの外のファイル）
// を変更しない（docs/protocol.md「ファイル」）。
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

// 書き込み途中のファイルを作成できなければ（同じ名前の空でないディレクトリがある）、要求を
// 公開しない。
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

// 公開（rename）できなければ、書き込み途中のファイルを残さない。
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

// 要求を公開できなければ、いずれの要求かと、いずれの段階（書き込み途中のファイルを開く・rename）で
// 失敗したかを返す（段階は os のエラーの文言が示す）。
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

// 引数が JSON として読み取れなければ、何も配置せずに拒否する（vw2026 は先に検証するが、Call を直接
// 呼び出す呼び出し元のため）。
func TestCallRejectsInvalidArgs(t *testing.T) {
	dir, _ := fakeplugin.NewHeldDir(t)
	_, err := Open(dir).Call("ping", json.RawMessage(`{"a":`), "", time.Second)
	if err == nil || !strings.HasPrefix(err.Error(), "encode request: ") {
		t.Fatalf("want an encode error, got %v", err)
	}
	assertNoFiles(t, dir, RequestSuffix)
	assertNoFiles(t, dir, WaitSuffix)
}

// 待機標識（.wait）を作成できなければ、いずれの標識かを返し、要求を公開しない。.wait は O_EXCL で
// 作成するので、同じ名前が残っていれば作成できない。
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

func TestIsObject(t *testing.T) {
	for text, want := range map[string]bool{
		`{}`: true, ` {"a":1} `: true, `[]`: false, `null`: false, `"{"`: false, ``: false,
	} {
		if got := IsObject(json.RawMessage(text)); got != want {
			t.Errorf("IsObject(%q) = %v, want %v", text, got, want)
		}
	}
}

// 届いた応答の読み取り方と検証（docs/protocol.md「応答」「互換性」）。ファイルを使用せずに、
// pollResponse と同じ decodeResponse で読み取り、checkResponse で検証する（Call がこの 2 つを通す
// ことは TestCallPassesDetail・TestCallStopsOnMalformedResponse・TestCallTakenThenAnsweredInGrace
// が検証する）。
func TestCheckResponse(t *testing.T) {
	const malformed = "malformed"
	for _, c := range []struct {
		text string
		// want は返る応答の result（成功）か detail（失敗）。malformed ならば破損した応答。
		want string
		// err は「実行しなかった」種別の場合のエラー（want は参照しない）。
		err error
	}{
		// 「実行しなかった」種別は、ツールの失敗ではなく、それぞれのエラーにする。
		{text: `{"ok":false,"code":"no_wait","error":"x"}`, err: ErrNoWait},
		{text: `{"ok":false,"code":"busy","error":"x"}`, err: ErrBusy},
		{text: `{"ok":false,"code":"occupied","error":"x"}`, err: ErrOccupied},
		{text: `{"ok":false,"code":"no_session","error":"x"}`, err: ErrNoSession},
		// JSON として破損している・オブジェクトでない応答。
		{`{"ok":tr`, malformed, nil},
		{`null`, malformed, nil},
		// 未知のフィールドは無視する（「互換性」）。
		{`{"ok":true,"result":{},"future":[1]}`, `{}`, nil},
		// 成功の result は必ずオブジェクト。
		{`{"ok":true}`, malformed, nil},
		{`{"ok":true,"result":null}`, malformed, nil},
		{`{"ok":true,"result":[1]}`, malformed, nil},
		{`{"ok":true,"result":"done"}`, malformed, nil},
		// 失敗の code が無い・未知の値ならば、ツールの失敗ではなく破損した応答（既知のフィールドの
		// 未知の値）。
		{`{"ok":false,"error":"x"}`, malformed, nil},
		{`{"ok":false,"code":"later","error":"x"}`, malformed, nil},
		// 失敗の detail はそのまま渡す。保持できるのは invalid_args・internal のみで、
		// オブジェクトに限る。
		{
			`{"ok":false,"code":"internal","error":"x","detail":{"reason":"documents_open"}}`,
			`{"reason":"documents_open"}`,
			nil,
		},
		{`{"ok":false,"code":"invalid_args","error":"x","detail":{}}`, `{}`, nil},
		{`{"ok":false,"code":"internal","error":"x"}`, ``, nil},
		{`{"ok":false,"code":"internal","error":"x","detail":null}`, malformed, nil},
		{`{"ok":false,"code":"internal","error":"x","detail":[1]}`, malformed, nil},
		{`{"ok":false,"code":"internal","error":"x","detail":"x"}`, malformed, nil},
		{`{"ok":true,"result":{},"detail":{}}`, malformed, nil},
		{`{"ok":false,"code":"unknown_tool","error":"x","detail":{}}`, malformed, nil},
		{`{"ok":false,"code":"invalid_request","error":"x","detail":{}}`, malformed, nil},
		{`{"ok":false,"code":"busy","error":"x","detail":null}`, malformed, nil},
		{`{"ok":false,"code":"no_session","error":"x","detail":{}}`, malformed, nil},
	} {
		t.Run(c.text, func(t *testing.T) {
			response, unreadable := DecodeResponse([]byte(c.text))
			if unreadable {
				if c.want != malformed {
					t.Fatal("cannot decode")
				}
				return
			}
			got, err := CheckResponse("ping", response)
			if c.err != nil {
				if !errors.Is(err, c.err) || got != nil {
					t.Fatalf("want %v, got %+v %v", c.err, got, err)
				}
				return
			}
			if c.want == malformed {
				if !errors.Is(err, ErrMalformedResponse) {
					t.Fatalf("want ErrMalformedResponse, got %+v %v", got, err)
				}
				return
			}
			// 検証した応答は、そのまま返す（ok・code を変更しない）。
			if err != nil || got != response {
				t.Fatalf("got %+v %v, want the response as it is", got, err)
			}
			field := got.Detail
			if got.OK {
				field = got.Result
			}
			if string(field) != c.want {
				t.Fatalf("got %+v, want %s", got, c.want)
			}
		})
	}
}

// Call は、期限の前に届いた応答も検証する。読み取れても規則に合致しない応答は破損した応答にし、
// 応答のファイルを残さない。
func TestCallRejectsInvalidResponse(t *testing.T) {
	dir, _ := fakeplugin.NewHeldDir(t)
	answer(t, dir, `{"ok":true}`)
	if _, err := Open(dir).Call("ping", nil, "", 5*time.Second); !errors.Is(err, ErrMalformedResponse) {
		t.Fatalf("want ErrMalformedResponse, got %v", err)
	}
	assertNoFiles(t, dir, ResponseSuffix)
	assertNoFiles(t, dir, WaitSuffix)
}

// Call は届いた応答を検証して返す（失敗の detail もそのまま渡す）。
func TestCallPassesDetail(t *testing.T) {
	dir, _ := fakeplugin.NewHeldDir(t)
	answer(t, dir, `{"ok":false,"code":"internal","error":"x","detail":{"reason":"documents_open"}}`)
	response, err := Open(dir).Call("quit", nil, "", 5*time.Second)
	if err != nil || response.OK || string(response.Detail) != `{"reason":"documents_open"}` {
		t.Fatalf("%+v %v", response, err)
	}
}
