package fakeplugin

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/spool"
)

// 作法の見本（protocol/fixtures。形は docs/fixtures.md）を読み、偽のプラグインがプラグイン
// （src/core の Server。同じ見本を読む）と同じに振る舞うかを確かめる。

// fixtures は作法の見本の場所（go test はパッケージのディレクトリで走る）。
const fixtures = "../../../protocol/fixtures"

// sessionMark は、要求の見本では発行された印に置き換え、期待する応答では id の綴りを満たす
// 文字列に合わせてその値を覚える印。
const sessionMark = "$session"

type fakeTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	Kind        string          `json:"kind"`
	Does        string          `json:"does"`
}

type call struct {
	tool string
	args json.RawMessage
}

type fixtureCase struct {
	Description string        `json:"description"`
	Steps       []fixtureStep `json:"steps"`
}

type fixtureStep struct {
	Description string                     `json:"description"`
	OtherBridge bool                       `json:"other_bridge"`
	Waits       map[string]string          `json:"waits"`
	Files       map[string]string          `json:"files"`
	Requests    map[string]json.RawMessage `json:"requests"`
	Expect      struct {
		Phase string `json:"phase"`
		Quit  bool   `json:"quit"`
		Calls []struct {
			Tool string          `json:"tool"`
			Args json.RawMessage `json:"args"`
		} `json:"calls"`
		Responses map[string]expectedResponse `json:"responses"`
		Files     []string                    `json:"files"`
	} `json:"expect"`
}

// expectedResponse は期待する応答。ok は必須で、result は書いたときだけ比べる。
type expectedResponse struct {
	OK     *bool           `json:"ok"`
	Code   string          `json:"code"`
	Result json.RawMessage `json:"result"`
}

// readFixture は見本のファイルを読む。
func readFixture(t *testing.T, name string, v any) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixtures, name))
	if err != nil {
		t.Fatal(err)
	}
	strictDecode(t, name, data, v)
}

// strictDecode は知らないキーを誤りとして読む。見本のキーの綴りを誤ると、その確かめが黙って
// 省かれるため。
func strictDecode(t *testing.T, where string, data []byte, v any) {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		t.Fatalf("%s: %v", where, err)
	}
}

// decode は数を綴りのまま読む（見本とプラグインは数を丸めない）。
func decode(t *testing.T, data []byte) any {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var v any
	if err := decoder.Decode(&v); err != nil {
		t.Fatalf("decode %q: %v", data, err)
	}
	return v
}

// fakeHandler は見本の偽の道具の表（fake-tools.json）で応える。呼ばれた道具を calls に残す。
func fakeHandler(t *testing.T, tools []fakeTool, calls *[]call) Handler {
	return func(tool string, args json.RawMessage) spool.Response {
		if tool == spool.ToolTools {
			type entry struct {
				Name        string          `json:"name"`
				Description string          `json:"description"`
				InputSchema json.RawMessage `json:"inputSchema"`
				Kind        string          `json:"kind"`
			}
			list := struct {
				Protocol int     `json:"protocol"`
				Tools    []entry `json:"tools"`
			}{Protocol: spool.ProtocolVersion, Tools: []entry{}}
			for _, fake := range tools {
				list.Tools = append(list.Tools, entry{fake.Name, fake.Description, fake.InputSchema, fake.Kind})
			}
			result, _ := json.Marshal(list)
			return spool.Response{OK: true, Result: result}
		}
		for _, fake := range tools {
			if fake.Name != tool {
				continue
			}
			*calls = append(*calls, call{tool, args})
			switch fake.Does {
			case "echo":
				return spool.Response{OK: true, Result: args}
			case "empty":
				return spool.Response{OK: true, Result: json.RawMessage(`{}`)}
			case "invalid_args":
				return spool.Response{Code: spool.CodeInvalidArgs, Error: "invalid args"}
			case "internal", "throw":
				// Go の偽の道具は例外を投げないので、throw にはプラグインが例外を受けたときの応答を返す。
				return spool.Response{Code: spool.CodeInternal, Error: "the tool failed"}
			default:
				t.Fatalf("fake-tools.json: unknown does %q", fake.Does)
			}
		}
		return spool.Response{Code: spool.CodeUnknownTool, Error: "unknown tool: " + tool}
	}
}

// match は期待する値と実際の値を比べる。期待する値の $session は id の綴りを満たす文字列に合い、
// その値を発行された印として覚える。
func match(expected, actual any, session *string) bool {
	if s, ok := expected.(string); ok && s == sessionMark {
		got, ok := actual.(string)
		if ok && spool.ValidID(got) {
			*session = got
		}
		return ok && spool.ValidID(got)
	}
	switch e := expected.(type) {
	case map[string]any:
		a, ok := actual.(map[string]any)
		if !ok || len(a) != len(e) {
			return false
		}
		for key, value := range e {
			other, found := a[key]
			if !found || !match(value, other, session) {
				return false
			}
		}
		return true
	case []any:
		a, ok := actual.([]any)
		if !ok || len(a) != len(e) {
			return false
		}
		for i := range e {
			if !match(e[i], a[i], session) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(expected, actual)
}

type caseRunner struct {
	t       *testing.T
	name    string
	dir     string
	server  *server
	calls   []call
	held    map[string]func()
	session string
}

func (r *caseRunner) hold(id string) {
	unlock, err := lock(filepath.Join(r.dir, id+spool.WaitSuffix))
	if err != nil {
		r.t.Fatalf("%s: hold %s: %v", r.name, id, err)
	}
	r.held[id] = unlock
}

func (r *caseRunner) request(spec json.RawMessage) []byte {
	var sample string
	if json.Unmarshal(spec, &sample) == nil {
		data, err := os.ReadFile(filepath.Join(fixtures, "requests", sample))
		if err != nil {
			r.t.Fatal(err)
		}
		if bytes.Contains(data, []byte(sessionMark)) && r.session == "" {
			r.t.Fatalf("%s: no session has been issued for %s", r.name, sample)
		}
		return bytes.ReplaceAll(data, []byte(sessionMark), []byte(r.session))
	}
	var fill struct {
		Fill struct {
			Head string `json:"head"`
			Pad  string `json:"pad"`
			Tail string `json:"tail"`
			Size int    `json:"size"`
		} `json:"fill"`
	}
	strictDecode(r.t, r.name, spec, &fill)
	f := fill.Fill
	if len(f.Pad) != 1 || f.Size < len(f.Head)+len(f.Tail) {
		r.t.Fatalf("%s: bad fill %s", r.name, spec)
	}
	return []byte(f.Head + strings.Repeat(f.Pad, f.Size-len(f.Head)-len(f.Tail)) + f.Tail)
}

func (r *caseRunner) listing() []string {
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		r.t.Fatal(err)
	}
	names := []string{}
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names
}

func (r *caseRunner) step(step fixtureStep) {
	t := r.t
	if step.OtherBridge {
		unlock, err := lock(filepath.Join(r.dir, spool.LockFile))
		if err != nil {
			t.Fatal(err)
		}
		defer unlock()
	}

	// 置く: 待つ印 → ファイル → 要求。要求の待つ印は、waits に無ければ掴む。
	waits := map[string]string{}
	for id, state := range step.Waits {
		waits[id] = state
	}
	for id := range step.Requests {
		if _, found := waits[id]; !found {
			waits[id] = "held"
		}
	}
	for id, state := range waits {
		switch state {
		case "held":
			r.hold(id)
		case "released":
			write(t, r.dir, id+spool.WaitSuffix, "")
		case "missing":
		default:
			t.Fatalf("%s: unknown wait state %q", r.name, state)
		}
	}
	for name, text := range step.Files {
		write(t, r.dir, name, text)
	}
	for id, spec := range step.Requests {
		if err := os.WriteFile(filepath.Join(r.dir, id+spool.RequestSuffix), r.request(spec), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	r.calls = nil
	serving, quit := r.server.serveOnce()
	want := step.Expect
	phase := "waiting"
	if serving {
		phase = "serving"
	}
	if want.Phase == "" {
		want.Phase = "serving"
	}
	if phase != want.Phase || quit != want.Quit {
		t.Errorf("%s: phase %s quit %v, want %s %v", r.name, phase, quit, want.Phase, want.Quit)
	}

	if len(r.calls) != len(want.Calls) {
		t.Errorf("%s: %d calls, want %d", r.name, len(r.calls), len(want.Calls))
	} else {
		for i, c := range want.Calls {
			if r.calls[i].tool != c.Tool {
				t.Errorf("%s: call %d is %s, want %s", r.name, i+1, r.calls[i].tool, c.Tool)
			}
			if c.Args != nil && !reflect.DeepEqual(decode(t, r.calls[i].args), decode(t, c.Args)) {
				t.Errorf("%s: call %d args %s, want %s", r.name, i+1, r.calls[i].args, c.Args)
			}
		}
	}

	// この回より前に置いたファイルを除き、スプールにある応答はすべて期待に載っていなければならない。
	for _, name := range r.listing() {
		if _, put := step.Files[name]; put || !strings.HasSuffix(name, spool.ResponseSuffix) {
			continue
		}
		if _, found := want.Responses[strings.TrimSuffix(name, spool.ResponseSuffix)]; !found {
			t.Errorf("%s: unexpected response %s", r.name, name)
		}
	}
	for id, expected := range want.Responses {
		if expected.OK == nil {
			t.Fatalf("%s: the expected response for %s has no ok", r.name, id)
		}
		data, err := os.ReadFile(filepath.Join(r.dir, id+spool.ResponseSuffix))
		if err != nil {
			t.Errorf("%s: no response for %s", r.name, id)
			continue
		}
		actual, _ := decode(t, data).(map[string]any)
		good := actual != nil && actual["ok"] == *expected.OK
		if *expected.OK {
			result, found := actual["result"]
			good = good && found
			if found && expected.Result != nil {
				good = good && match(decode(t, expected.Result), result, &r.session)
			}
		} else {
			_, hasError := actual["error"].(string)
			good = good && hasError && actual["code"] == expected.Code
		}
		if !good {
			t.Errorf("%s: %s responded %s, want %+v", r.name, id, data, expected)
		}
	}

	if want.Files != nil {
		expected := append([]string{}, want.Files...)
		sort.Strings(expected)
		if got := r.listing(); !reflect.DeepEqual(got, expected) {
			t.Errorf("%s: files %v, want %v", r.name, got, expected)
		}
	}

	// 呼ぶ側が応答を受け取ったことを真似る（応答を消し、待つ印を放して消す）。
	for id := range want.Responses {
		_ = os.Remove(filepath.Join(r.dir, id+spool.ResponseSuffix))
		if unlock, found := r.held[id]; found {
			unlock()
			delete(r.held, id)
		}
		_ = os.Remove(filepath.Join(r.dir, id+spool.WaitSuffix))
	}
}

func TestFixtureServeCases(t *testing.T) {
	var table struct {
		Tools []fakeTool `json:"tools"`
	}
	readFixture(t, "fake-tools.json", &table)
	for _, fake := range table.Tools {
		if fake.Kind != "read" && fake.Kind != "write" && fake.Kind != "app" {
			t.Fatalf("fake-tools.json: unknown kind %q", fake.Kind)
		}
	}
	paths, err := filepath.Glob(filepath.Join(fixtures, "serve", "*.json"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no serve cases: %v", err)
	}
	for _, path := range paths {
		name := filepath.Base(path)
		t.Run(strings.TrimSuffix(name, ".json"), func(t *testing.T) {
			var fixture fixtureCase
			readFixture(t, filepath.Join("serve", name), &fixture)
			if len(fixture.Steps) == 0 {
				t.Fatalf("%s: no steps", name)
			}
			r := &caseRunner{t: t, dir: NewDir(t), held: map[string]func(){}}
			r.server = &server{dir: r.dir, handle: fakeHandler(t, table.Tools, &r.calls)}
			// 一時ディレクトリを消す前に、ロックと待つ印を放す（Windows は開いたままでは消せない）。
			t.Cleanup(func() {
				r.server.release()
				for _, unlock := range r.held {
					unlock()
				}
			})
			for i, step := range fixture.Steps {
				r.name = name + " step " + strconv.Itoa(i+1)
				r.step(step)
			}
		})
	}
}
