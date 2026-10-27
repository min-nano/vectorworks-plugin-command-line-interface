package fakeplugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/spool"
)

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

// 回の始めに並べた要求はすべて処理し（件数の上限は無い）、その後に置かれた要求は次の回に回す。
func TestServeTakesOnlyRequestsListedAtStart(t *testing.T) {
	dir := NewDir(t)
	const listed = 20
	for i := range listed {
		put(t, dir, fmt.Sprintf("%04d", i), `{"tool":"ping"}`)
	}
	added := false
	s := &server{dir: dir, handle: func(string, json.RawMessage) spool.Response {
		if !added {
			added = true
			put(t, dir, "9999", `{"tool":"ping"}`)
		}
		return spool.Response{OK: true}
	}}
	t.Cleanup(s.release)
	s.serveOnce()
	if got := count(t, dir, spool.ResponseSuffix); got != listed {
		t.Fatalf("want %d responses, got %d", listed, got)
	}
	if !exists(dir, "9999"+spool.RequestSuffix) {
		t.Fatal("a request put during the round should wait for the next one")
	}
}

// blockResponse は id の応答の書きかけの名前に、空でないディレクトリを置く（応答を書けなくする）。
func blockResponse(t *testing.T, dir, id string) {
	t.Helper()
	temp := filepath.Join(dir, id+spool.ResponseSuffix+spool.TempSuffix)
	if err := os.MkdirAll(filepath.Join(temp, "keep"), 0o700); err != nil {
		t.Fatal(err)
	}
}

// 占有の印と終了は、応答を書けたときだけ反映する（docs/protocol.md「占有」）。
func TestServeAppliesSessionAndQuitOnlyWhenTheResponseIsWritten(t *testing.T) {
	dir := NewDir(t)
	s := &server{dir: dir, handle: func(string, json.RawMessage) spool.Response {
		return spool.Response{OK: true}
	}}
	t.Cleanup(s.release)
	blockResponse(t, dir, "1")
	put(t, dir, "1", `{"tool":"session_start"}`)
	s.serveOnce()
	if s.session != "" {
		t.Fatal("a session was issued although its response could not be written")
	}
	put(t, dir, "2", `{"tool":"session_start"}`)
	s.serveOnce()
	if s.session == "" {
		t.Fatal("no session was issued")
	}
	session := s.session
	blockResponse(t, dir, "3")
	put(t, dir, "3", fmt.Sprintf(`{"tool":"quit","session":%q}`, session))
	put(t, dir, "3a", fmt.Sprintf(`{"tool":"ping","session":%q}`, session))
	if _, quit := s.serveOnce(); quit {
		t.Fatal("quit although its response could not be written")
	}
	// 終了しないので、同じ回の残りの要求も処理する。
	if !exists(dir, "3a"+spool.ResponseSuffix) {
		t.Fatal("the request after the unanswered quit was not served")
	}
	blockResponse(t, dir, "4")
	put(t, dir, "4", fmt.Sprintf(`{"tool":"session_end","session":%q}`, session))
	s.serveOnce()
	if s.session != session {
		t.Fatal("the session ended although its response could not be written")
	}
}

// 応答はプラグインと同じに整える。detail は失敗にだけ載せ、オブジェクトでなければ internal
// （docs/protocol.md「応答」）。
func TestServeShapesTheDetail(t *testing.T) {
	dir := NewDir(t)
	s := &server{dir: dir, handle: func(tool string, _ json.RawMessage) spool.Response {
		switch tool {
		case "array":
			return spool.Response{Code: spool.CodeInvalidArgs, Error: "x", Detail: json.RawMessage(`[1]`)}
		case "object":
			return spool.Response{Code: spool.CodeInvalidArgs, Error: "x", Detail: json.RawMessage(`{"a":1}`)}
		}
		return spool.Response{OK: true, Result: json.RawMessage(`{}`), Detail: json.RawMessage(`{"a":1}`)}
	}}
	t.Cleanup(s.release)
	put(t, dir, "1", `{"tool":"array"}`)
	put(t, dir, "2", `{"tool":"object"}`)
	put(t, dir, "3", `{"tool":"success"}`)
	s.serveOnce()
	for id, want := range map[string]string{
		"1": `{"ok":false,"code":"internal","error":"the detail of array is not an object"}`,
		"2": `{"ok":false,"code":"invalid_args","error":"x","detail":{"a":1}}`,
		"3": `{"ok":true,"result":{}}`,
	} {
		data, err := os.ReadFile(filepath.Join(dir, id+spool.ResponseSuffix))
		if err != nil || string(data) != want {
			t.Errorf("%s: %s %v, want %s", id, data, err, want)
		}
	}
}
