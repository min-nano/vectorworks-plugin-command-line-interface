package fakeplugin

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/spool"
)

// 作法の見本（protocol/fixtures/serve/）を偽のプラグインに照らす。プラグイン（C++）も同じ見本を
// 読む（tests/ProtocolFixturesTests.cpp）ので、両側の振る舞いが食い違えばどちらかが落ちる。
// 見本の形は protocol/fixtures/README.md。

const fixturesDir = "../../../protocol/fixtures"

// fixture は見本 1 つ。
type fixture struct {
	Description string  `json:"description"`
	Rounds      []round `json:"rounds"`
}

// round は受け付け 1 回の前に置くものと、そのあとに残るもの。
type round struct {
	LockHeldElsewhere bool              `json:"lock_held_elsewhere"`
	Files             []fixtureFile     `json:"files"`
	Waits             map[string]string `json:"waits"`
	Requests          []fixtureRequest  `json:"requests"`
	Expect            struct {
		Phase     string                     `json:"phase"`
		Quit      bool                       `json:"quit"`
		Files     []string                   `json:"files"`
		Responses map[string]json.RawMessage `json:"responses"`
	} `json:"expect"`
}

type fixtureFile struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

// fixtureRequest は要求 1 件。テキストは json・text・base64・padded・nested のどれか 1 つで作る。
type fixtureRequest struct {
	ID     string          `json:"id"`
	Wait   string          `json:"wait"`
	JSON   json.RawMessage `json:"json"`
	Text   *string         `json:"text"`
	Base64 string          `json:"base64"`
	Padded int             `json:"padded"`
	Nested int             `json:"nested"`
}

// fixtureTools は偽の道具（tests/FakeTools.h と同じ振る舞い）。
func fixtureTools(tool string, args json.RawMessage) spool.Response {
	switch tool {
	case "ping":
		return spool.Response{OK: true, Result: json.RawMessage(`{"pong":true}`)}
	case "echo":
		result, _ := json.Marshal(map[string]json.RawMessage{"args": args})
		return spool.Response{OK: true, Result: result}
	case "fail":
		return spool.Response{OK: false, Code: spool.CodeInvalidArgs, Error: "bad args"}
	case "boom":
		return spool.Response{OK: false, Code: spool.CodeInternal, Error: "boom"}
	case "quit":
		return spool.Response{OK: true, Result: json.RawMessage(`{"quitting":true}`)}
	}
	return spool.Response{OK: false, Code: spool.CodeUnknownTool, Error: "unknown tool: " + tool}
}

// text は要求のテキスト。"$session" を session_start が返した印に置き換える。
func (r fixtureRequest) text(t *testing.T, session string) []byte {
	t.Helper()
	switch {
	case r.JSON != nil:
		return []byte(strings.ReplaceAll(string(r.JSON), `"$session"`, `"`+session+`"`))
	case r.Text != nil:
		return []byte(*r.Text)
	case r.Base64 != "":
		data, err := base64.StdEncoding.DecodeString(r.Base64)
		if err != nil {
			t.Fatal(err)
		}
		return data
	case r.Padded > 0:
		// ちょうどその大きさの ping の要求。
		head, tail := `{"tool":"ping","args":{"pad":"`, `"}}`
		return []byte(head + strings.Repeat("x", r.Padded-len(head)-len(tail)) + tail)
	case r.Nested > 0:
		// ちょうどその深さの ping の要求（外側と args で 2、残りを配列で重ねる）。
		return []byte(`{"tool":"ping","args":{"a":` + strings.Repeat("[", r.Nested-2) + strings.Repeat("]", r.Nested-2) + `}}`)
	}
	t.Fatalf("request %s has no text", r.ID)
	return nil
}

// matches は応答を見本と比べる。"$any" は何にでも合い、"$session" は印として受け取る。
func matches(expected, actual any, session *string) bool {
	if text, ok := expected.(string); ok && text == "$any" {
		return true
	}
	if text, ok := expected.(string); ok && text == "$session" {
		got, ok := actual.(string)
		if !ok || !spool.ValidID(got) {
			return false
		}
		*session = got
		return true
	}
	want, wantObject := expected.(map[string]any)
	got, gotObject := actual.(map[string]any)
	if wantObject && gotObject {
		if len(want) != len(got) {
			return false
		}
		for key, value := range want {
			if other, found := got[key]; !found || !matches(value, other, session) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(expected, actual)
}

// listNames はスプールの中の名前（bridge.lock を除く）を昇順で返す。
func listNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, entry := range entries {
		if entry.Name() != spool.LockFile {
			names = append(names, entry.Name())
		}
	}
	slices.Sort(names)
	return names
}

func TestFixtures(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join(fixturesDir, "serve", "*.json"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var f fixture
			if err := json.Unmarshal(data, &f); err != nil {
				t.Fatal(err)
			}
			runFixture(t, f)
		})
	}
}

// runFixture は見本 1 つを走らせる。占有の印は server が持つので、回をまたいで同じ server を使う。
func runFixture(t *testing.T, f fixture) {
	dir := NewDir(t)
	s := &server{dir: dir, handle: fixtureTools}
	locked := false
	waits := map[string]func(){}
	t.Cleanup(func() {
		for _, unlock := range waits {
			unlock()
		}
	})
	hold := func(id string) {
		unlock, err := lock(filepath.Join(dir, id+spool.WaitSuffix))
		if err != nil {
			t.Fatal(err)
		}
		waits[id] = unlock
	}
	session := ""
	for number, r := range f.Rounds {
		for _, file := range r.Files {
			write(t, dir, file.Name, file.Text)
		}
		for id, state := range r.Waits {
			if state == "held" {
				hold(id)
			} else {
				write(t, dir, id+spool.WaitSuffix, "")
			}
		}
		for _, request := range r.Requests {
			switch request.Wait {
			case "", "held":
				hold(request.ID)
			case "released":
				write(t, dir, request.ID+spool.WaitSuffix, "")
			}
			if err := os.WriteFile(filepath.Join(dir, request.ID+spool.RequestSuffix), request.text(t, session), 0o600); err != nil {
				t.Fatal(err)
			}
		}

		// ロックを取れない回は何もしない。取れたら 1 度だけ掃除してから受け付ける。
		var release func()
		if r.LockHeldElsewhere {
			release = HoldLock(t, dir)
		}
		phase, quit := "waiting", false
		if !locked {
			if unlock, err := lock(filepath.Join(dir, spool.LockFile)); err == nil {
				locked = true
				t.Cleanup(unlock)
				sweep(dir)
			}
		}
		if locked {
			phase = "serving"
			quit = s.serveOnce()
		}
		if release != nil {
			release()
		}

		if phase != r.Expect.Phase || quit != r.Expect.Quit {
			t.Errorf("round %d: phase %s quit %v, want %s %v", number+1, phase, quit, r.Expect.Phase, r.Expect.Quit)
		}
		want := slices.Clone(r.Expect.Files)
		slices.Sort(want)
		if got := listNames(t, dir); !slices.Equal(got, want) {
			t.Errorf("round %d: files %v, want %v", number+1, got, want)
		}
		// 応えられたものは呼ぶ側の真似として読んで消し、待つ印を放して消す。
		for id, raw := range r.Expect.Responses {
			path := filepath.Join(dir, id+spool.ResponseSuffix)
			text, err := os.ReadFile(path)
			if err != nil {
				t.Errorf("round %d: %s: %v", number+1, id, err)
				continue
			}
			var expected, actual any
			if err := json.Unmarshal(raw, &expected); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(text, &actual); err != nil {
				t.Fatal(err)
			}
			// error は人向けの文言なので比べず、失敗の応答で空でないことだけを確かめる。
			fields, _ := actual.(map[string]any)
			message, hasError := fields["error"]
			delete(fields, "error")
			if failed, _ := fields["ok"].(bool); !failed && (!hasError || message == "") {
				t.Errorf("round %d: %s: failure without an error: %s", number+1, id, text)
			}
			if !matches(expected, actual, &session) {
				t.Errorf("round %d: %s: %s, want %s", number+1, id, text, raw)
			}
			_ = os.Remove(path)
			if unlock, found := waits[id]; found {
				unlock()
				delete(waits, id)
			}
			_ = os.Remove(filepath.Join(dir, id+spool.WaitSuffix))
		}
	}
}
