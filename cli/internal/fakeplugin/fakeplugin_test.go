package fakeplugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/spool"
)

func ok(string, json.RawMessage) spool.Response {
	return spool.Response{OK: true}
}

// put は呼ぶ側を真似て、待つ印を掴んでから要求を置く。待つ印はテストの終わりに放す。
func put(t *testing.T, dir, id, text string) {
	t.Helper()
	unlock, err := lock(filepath.Join(dir, id+spool.WaitSuffix))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(unlock)
	write(t, dir, id+spool.RequestSuffix, text)
}

func write(t *testing.T, dir, name, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func exists(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}

func count(t *testing.T, dir, suffix string) int {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "*"+suffix))
	if err != nil {
		t.Fatal(err)
	}
	return len(matches)
}

func responseOf(t *testing.T, dir, id string) spool.Response {
	t.Helper()
	text, err := os.ReadFile(filepath.Join(dir, id+spool.ResponseSuffix))
	if err != nil {
		t.Fatal(err)
	}
	var response spool.Response
	if err := json.Unmarshal(text, &response); err != nil {
		t.Fatal(err)
	}
	return response
}

// 回の始めに並べた要求はすべて処理し（件数の上限は無い）、その後に置かれた要求は次の回に回す。
func TestServeTakesOnlyRequestsListedAtStart(t *testing.T) {
	dir := NewDir(t)
	const listed = 20
	for i := range listed {
		put(t, dir, fmt.Sprintf("%04d", i), `{"tool":"ping"}`)
	}
	added := false
	(&server{dir: dir, handle: func(string, json.RawMessage) spool.Response {
		if !added {
			added = true
			put(t, dir, "9999", `{"tool":"ping"}`)
		}
		return spool.Response{OK: true}
	}}).serveOnce()
	if got := count(t, dir, spool.ResponseSuffix); got != listed {
		t.Fatalf("want %d responses, got %d", listed, got)
	}
	if !exists(dir, "9999"+spool.RequestSuffix) {
		t.Fatal("a request put during the round should wait for the next one")
	}
}

// 綴りを満たさない id の要求は、読まず・消さず・応えない。
func TestServeIgnoresInvalidID(t *testing.T) {
	dir := NewDir(t)
	put(t, dir, "a.b", `{"tool":"ping"}`)
	(&server{dir: dir, handle: func(tool string, _ json.RawMessage) spool.Response {
		t.Fatalf("%s must not run", tool)
		return spool.Response{}
	}}).serveOnce()
	if !exists(dir, "a.b"+spool.RequestSuffix) || count(t, dir, spool.ResponseSuffix) != 0 {
		t.Fatal("the request should be left as is")
	}
}

// 大きすぎる・入れ子が深すぎる・壊れている・tool の無い要求は invalid_request。
func TestServeRejectsUnreadableRequests(t *testing.T) {
	dir := NewDir(t)
	requests := map[string]string{
		"big":    `{"tool":"ping","args":{"s":"` + strings.Repeat("x", spool.MaxRequestBytes) + `"}}`,
		"deep":   `{"tool":"ping","args":{"a":` + strings.Repeat("[", spool.MaxNestingDepth) + strings.Repeat("]", spool.MaxNestingDepth) + `}}`,
		"broken": `{"tool":`,
		"notool": `{"args":{}}`,
	}
	for id, text := range requests {
		put(t, dir, id, text)
	}
	(&server{dir: dir, handle: func(tool string, _ json.RawMessage) spool.Response {
		t.Fatalf("%s must not run", tool)
		return spool.Response{}
	}}).serveOnce()
	for id := range requests {
		if response := responseOf(t, dir, id); response.OK || response.Code != spool.CodeInvalidRequest {
			t.Errorf("%s: want invalid_request, got %+v", id, response)
		}
	}
}

// 上限ちょうどの深さは受け付ける。args がオブジェクトでなければ {} として渡す。
func TestServeAcceptsLimitDepthAndDefaultsArgs(t *testing.T) {
	dir := NewDir(t)
	nested := strings.Repeat("[", spool.MaxNestingDepth-2) + strings.Repeat("]", spool.MaxNestingDepth-2)
	put(t, dir, "deep", `{"tool":"ping","args":{"a":`+nested+`}}`)
	put(t, dir, "list", `{"tool":"ping","args":[1]}`)
	got := map[string]string{}
	(&server{dir: dir, handle: func(_ string, args json.RawMessage) spool.Response {
		got[string(args)] = ""
		return spool.Response{OK: true}
	}}).serveOnce()
	if _, ok := got["{}"]; !ok || len(got) != 2 {
		t.Fatalf("unexpected args: %v", got)
	}
	if !responseOf(t, dir, "deep").OK {
		t.Fatal("a request at the depth limit should run")
	}
}

// ロックを取ったら、待つ印を掴める・無い id のファイルだけを消す。
func TestStartSweepsOnlyFilesWithoutWaiter(t *testing.T) {
	dir := NewDir(t)
	write(t, dir, "dead"+spool.WaitSuffix, "")
	write(t, dir, "dead"+spool.ResponseSuffix, `{"ok":true}`)
	write(t, dir, "dead"+spool.RequestSuffix+spool.TempSuffix, "")
	write(t, dir, "gone"+spool.ResponseSuffix, `{"ok":true}`)
	unlock, err := lock(filepath.Join(dir, "live"+spool.WaitSuffix))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(unlock)
	write(t, dir, "live"+spool.ResponseSuffix, `{"ok":true}`)
	Start(t, dir, ok)
	for _, name := range []string{"dead" + spool.WaitSuffix, "dead" + spool.ResponseSuffix, "dead" + spool.RequestSuffix + spool.TempSuffix, "gone" + spool.ResponseSuffix} {
		if exists(dir, name) {
			t.Errorf("%s should be swept", name)
		}
	}
	for _, name := range []string{"live" + spool.WaitSuffix, "live" + spool.ResponseSuffix} {
		if !exists(dir, name) {
			t.Errorf("%s should be kept", name)
		}
	}
}

// 占有の印は 1 つだけ発行され、その印の要求だけが実行される。quit は占有の中でだけ呼べる。
func TestServeSession(t *testing.T) {
	dir := NewDir(t)
	s := &server{dir: dir, handle: ok}
	step := func(id, text string) spool.Response {
		t.Helper()
		put(t, dir, id, text)
		s.serveOnce()
		return responseOf(t, dir, id)
	}
	if r := step("q0", `{"tool":"quit"}`); r.Code != spool.CodeNoSession {
		t.Fatalf("quit without a session: %+v", r)
	}
	if r := step("s0", `{"tool":"session_end","session":"x"}`); r.Code != spool.CodeNoSession {
		t.Fatalf("end without a session: %+v", r)
	}
	started := step("s1", `{"tool":"session_start"}`)
	var issued struct {
		Session string `json:"session"`
	}
	if !started.OK || json.Unmarshal(started.Result, &issued) != nil || !spool.ValidID(issued.Session) {
		t.Fatalf("start: %+v", started)
	}
	if r := step("s2", `{"tool":"session_start"}`); r.Code != spool.CodeBusy {
		t.Fatalf("a second start: %+v", r)
	}
	for id, text := range map[string]string{
		"b1": `{"tool":"layers"}`,
		"b2": `{"tool":"layers","session":"other"}`,
		"b3": `{"tool":"quit"}`,
		"b4": `{"tool":"session_end","session":"other"}`,
	} {
		if r := step(id, text); r.Code != spool.CodeBusy {
			t.Errorf("%s: want busy, got %+v", text, r)
		}
	}
	if r := step("p1", `{"tool":"ping"}`); !r.OK {
		t.Fatalf("ping is for everyone: %+v", r)
	}
	for id, tool := range map[string]string{"r1": "layers", "r2": "quit"} {
		if r := step(id, `{"tool":"`+tool+`","session":"`+issued.Session+`"}`); !r.OK {
			t.Errorf("%s in the session: %+v", tool, r)
		}
	}
	if r := step("e1", `{"tool":"session_end","session":"`+issued.Session+`"}`); !r.OK {
		t.Fatalf("end: %+v", r)
	}
	if r := step("a1", `{"tool":"layers","session":"`+issued.Session+`"}`); r.Code != spool.CodeNoSession {
		t.Fatalf("the ended session: %+v", r)
	}
	if r := step("a2", `{"tool":"layers"}`); !r.OK {
		t.Fatalf("without a session: %+v", r)
	}
}
