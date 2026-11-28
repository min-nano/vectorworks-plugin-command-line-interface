package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/fakeplugin"
	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/spool"
)

// 待つ上限は 0 から time.Duration に収まる秒数までの有限の数だけを受け、それ以外（負・NaN・無限大・大きすぎる・
// 数でない）は、指定でも環境変数でも使い方の誤り（2）にする（time.Duration に直すと負になり、すぐに待つのを
// やめてしまうため）。どのコマンドも同じ型を使う。
func TestTimeoutMustBeInRange(t *testing.T) {
	commands := []struct {
		args []string
		env  string
	}{
		{[]string{"call", "ping"}, "VW2026_CALL_TIMEOUT"},
		{[]string{"tools"}, "VW2026_TOOLS_TIMEOUT"},
		{[]string{"session", "start"}, "VW2026_SESSION_TIMEOUT"},
		{[]string{"session", "end", "--session", "x"}, "VW2026_SESSION_TIMEOUT"},
	}
	for _, command := range commands {
		for _, value := range []string{"-1", "NaN", "inf", "1e10", "9223372037", "soon"} {
			r := invoke(t, opts{}, append(append([]string{}, command.args...), "--timeout", value)...)
			if r.code != exitUsage || r.stdout != "" || !strings.Contains(r.stderr, "--timeout") {
				t.Errorf("%v --timeout %s: %+v", command.args, value, r)
			}
			r = invoke(t, opts{vars: map[string]string{command.env: value}}, command.args...)
			if r.code != exitUsage || r.stdout != "" {
				t.Errorf("%v with %s=%s: %+v", command.args, command.env, value, r)
			}
		}
		// 範囲の両端は受ける（ブリッジが無いので 5 で終わる）。
		for _, value := range []string{"0", "9223372036"} {
			if r := invoke(t, opts{}, append(append([]string{}, command.args...), "--timeout", value)...); r.code != exitDown {
				t.Errorf("%v --timeout %s: %+v", command.args, value, r)
			}
		}
	}
}

// answerRaw は、ロックを掴んだスプール dir に置かれた要求を 1 つ受け取り、text をそのまま応答として書く（偽の
// プラグインは詰めた JSON しか書かないので、空白や改行のある応答を真似る）。
func answerRaw(t *testing.T, dir, text string) {
	t.Helper()
	done := make(chan struct{})
	t.Cleanup(func() { <-done })
	go func() {
		defer close(done)
		for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
			requests, _ := filepath.Glob(filepath.Join(dir, "*"+spool.RequestSuffix))
			if len(requests) == 0 || os.Remove(requests[0]) != nil {
				continue
			}
			id := strings.TrimSuffix(filepath.Base(requests[0]), spool.RequestSuffix)
			_ = spool.WriteAtomically(filepath.Join(dir, id+spool.ResponseSuffix), []byte(text))
			return
		}
	}()
}

// 応答に空白や改行があっても、出力は 1 行の JSON にする。文字列の < > & と数の綴りは、--raw の有無に依らず
// そのまま出す。
func TestCallPrintsOneLine(t *testing.T) {
	response := "{\"ok\": true,\n \"result\": {\n  \"text\": \"x<y&z>\",\n  \"n\": 1.50\n }\n}"
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"call", "x"}, `{"text":"x<y&z>","n":1.50}` + "\n"},
		{[]string{"call", "x", "--raw"}, `{"ok":true,"result":{"text":"x<y&z>","n":1.50}}` + "\n"},
	} {
		dir, _ := fakeplugin.NewHeldDir(t)
		answerRaw(t, dir, response)
		if r := invoke(t, opts{dir: dir}, c.args...); r.code != exitOK || r.stdout != c.want {
			t.Errorf("%v: %+v", c.args, r)
		}
	}
}

// infiniteReader は終わらない標準入力（yes | vw2026 call x - のような誤り）。
type infiniteReader struct{}

func (infiniteReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = ' '
	}
	return len(p), nil
}

// プラグインが読まない要求（大きすぎる・入れ子が深すぎる）は、ブリッジを見る前に使い方の誤り（2）にし、要求を
// 置かない。終わらない標準入力も、上限を超えたところで読むのをやめて 2 にする。
func TestCallRefusesUnreadableRequests(t *testing.T) {
	deep := strings.Repeat(`{"a":`, spool.MaxNestingDepth) + "1" + strings.Repeat("}", spool.MaxNestingDepth)
	large := `{"a":"` + strings.Repeat("x", spool.MaxRequestBytes) + `"}`
	for _, c := range []struct {
		name  string
		args  []string
		stdin io.Reader
		want  string
	}{
		{"deep", []string{"call", "x", deep}, nil, "nested too deeply"},
		{"large", []string{"call", "x", large}, nil, "too large"},
		{"large stdin", []string{"call", "x", "-"}, strings.NewReader(large), "too large"},
		{"endless stdin", []string{"call", "x", "-"}, infiniteReader{}, "too large"},
	} {
		// ブリッジが無くても 5 ではなく 2 にする（引数を直せば良いと分かるように）。
		if r := invoke(t, opts{stdin: c.stdin}, c.args...); r.code != exitUsage || r.stdout != "" ||
			!strings.Contains(r.stderr, c.want) {
			t.Errorf("%s without the bridge: code %d, stderr %q", c.name, r.code, r.stderr)
		}
	}
	dir, _ := fakeplugin.NewHeldDir(t)
	if r := invoke(t, opts{dir: dir}, "call", "x", deep); r.code != exitUsage {
		t.Errorf("deep with the bridge: code %d, stderr %q", r.code, r.stderr)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != spool.LockFile {
			t.Errorf("left %s in the spool", entry.Name())
		}
	}
}
