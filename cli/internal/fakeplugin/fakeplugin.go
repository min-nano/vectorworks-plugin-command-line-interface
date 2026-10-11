// Package fakeplugin はプラグイン側の受け付けを真似る（docs/protocol.md「プラグイン側の義務」・
// docs/plugin/bridge.md「受け付け 1 回」）。spool と vw2026 の単体テストだけが使う。
//
// 作法より緩く真似ると、CLI がプラグインの断る要求を送っても単体テストが気づかないので、
// 関門（id の綴り・回の始めに並べた要求だけの処理・大きさと入れ子の深さの上限・占有・ロックを取ったときの掃除）は
// プラグインと同じに持つ。道具の種類（quit で残りを取り出さない）は道具の表を持たないので真似ない。
package fakeplugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/spool"
)

// waitState は <id>.wait の状態。
type waitState int

const (
	waitHeld     waitState = iota // 呼ぶ側が待っている
	waitReleased                  // 呼ぶ側が放した・死んだ
	waitMissing                   // 無い（作法に反する）
)

// Handler は道具 1 つの呼び出しに応える。
type Handler func(tool string, args json.RawMessage) spool.Response

// NewDir は 0700 のスプールを作る。
func NewDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), spool.SpoolDirName)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

// HoldLock はロックファイルを掴む（Vectorworks が動いている）。返す関数で放す（終了を真似る）。
// テストの終わりにも放す。
func HoldLock(t *testing.T, dir string) (release func()) {
	t.Helper()
	unlock, err := lock(filepath.Join(dir, spool.LockFile))
	if err != nil {
		t.Fatal(err)
	}
	released := false
	release = func() {
		if !released {
			released = true
			unlock()
		}
	}
	t.Cleanup(release)
	return release
}

// Start はロックを掴み、前の回の残骸を消してから、要求に handle で応え続ける。テストの終わりに
// 止める。返す関数で途中でも止められる（Vectorworks の終了を真似る）。占有の印は Start ごとに
// 持つ（Vectorworks を起動し直すと消えるのと同じ）。
func Start(t *testing.T, dir string, handle Handler) (stop func()) {
	t.Helper()
	release := HoldLock(t, dir)
	sweep(dir)
	s := &server{dir: dir, handle: handle}
	quit := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-quit:
				return
			case <-time.After(10 * time.Millisecond):
			}
			s.serveOnce()
		}
	}()
	stopped := false
	stop = func() {
		if !stopped {
			stopped = true
			close(quit)
			<-done
			release()
		}
	}
	t.Cleanup(stop)
	return stop
}

// sweep はロックを取った直後の掃除（docs/protocol.md「ロック」）。待つ印を掴める・無い id の
// 要求・応答・待つ印・書きかけを消し、待っている呼ぶ側のものは残す。id は最初の . より前。
func sweep(dir string) {
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		name := entry.Name()
		if !(strings.HasSuffix(name, spool.RequestSuffix) || strings.HasSuffix(name, spool.ResponseSuffix) ||
			strings.HasSuffix(name, spool.WaitSuffix) || strings.HasSuffix(name, spool.TempSuffix)) {
			continue
		}
		id, _, _ := strings.Cut(name, ".")
		if waitStateOf(filepath.Join(dir, id+spool.WaitSuffix)) != waitHeld {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
}

// server は受け付ける橋 1 つ。占有の印（session）はメモリだけに持つ（docs/protocol.md「占有」）。
type server struct {
	dir     string
	handle  Handler
	session string // 発行している占有の印。空なら占有されていない
}

// serveOnce は受け付け 1 回。要求を名前の昇順で読み、消し、呼ぶ側が待っていれば実行する。
// 処理するのは回の始めに並べた要求だけで（件数と時間の上限は無い）、取り出した要求には同じ回の
// 中で応える。
func (s *server) serveOnce() {
	entries, _ := os.ReadDir(s.dir)
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		// 綴りを満たさない id の要求は受け付けない（読まず・消さず・応えない）。
		if strings.HasSuffix(name, spool.RequestSuffix) && spool.ValidID(strings.TrimSuffix(name, spool.RequestSuffix)) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		id := strings.TrimSuffix(name, spool.RequestSuffix)
		path := filepath.Join(s.dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			// 呼ぶ側が取り下げた。黙って飛ばす。
			continue
		}
		if os.Remove(path) != nil {
			// 消せない要求は実行しない（残すと次の回にまた実行してしまう。消えていたなら
			// 呼ぶ側が取り下げた）。
			continue
		}
		// 待つ者の居なくなった要求は実行も応答もしない。.wait の無い要求は実行せずに断る。
		wait := filepath.Join(s.dir, id+spool.WaitSuffix)
		state := waitStateOf(wait)
		if state == waitReleased {
			_ = os.Remove(wait)
			continue
		}
		var response spool.Response
		if state == waitMissing {
			response = spool.Response{OK: false, Code: spool.CodeNoWait, Error: "no wait file"}
		} else if tool, args, session, ok := parseRequest(data); !ok {
			response = spool.Response{OK: false, Code: spool.CodeInvalidRequest, Error: "unreadable request"}
		} else {
			response = s.answer(tool, args, session)
		}
		out, _ := json.Marshal(response)
		temp := filepath.Join(s.dir, id+spool.ResponseSuffix+spool.TempSuffix)
		_ = os.WriteFile(temp, out, 0o600)
		_ = os.Rename(temp, filepath.Join(s.dir, id+spool.ResponseSuffix))
	}
}

// answer は要求を占有に照らしてから応える（docs/protocol.md「占有」）。
//
//   - tools と ping は占有に照らさない（調べものと診断のため）。
//   - session_start は占有されていなければ印を発行し、されていれば busy。
//   - session_end と quit は占有の中でだけ呼べる。
//   - そのほかは、占有されていなければ印の無い要求を、されていれば同じ印の要求だけを実行する。
func (s *server) answer(tool string, args json.RawMessage, session string) spool.Response {
	switch tool {
	case "tools", "ping":
		return s.handle(tool, args)
	case spool.ToolSessionStart:
		if s.session != "" {
			return refuse(spool.CodeBusy)
		}
		if code := s.check(session, false); code != "" {
			return refuse(code)
		}
		s.session = spool.NewID(time.Now())
		result, _ := json.Marshal(map[string]string{"session": s.session})
		return spool.Response{OK: true, Result: result}
	case spool.ToolSessionEnd:
		if code := s.check(session, true); code != "" {
			return refuse(code)
		}
		s.session = ""
		return spool.Response{OK: true, Result: json.RawMessage(`{"ended":true}`)}
	}
	// quit は種類 app。Vectorworks を終わらせるので、占有している呼ぶ側だけに認める。
	if code := s.check(session, tool == "quit"); code != "" {
		return refuse(code)
	}
	return s.handle(tool, args)
}

// check は要求の印を占有に照らす。実行してよければ空、断るならその code。needSession は占有の
// 中でしか呼べない道具か。
func (s *server) check(session string, needSession bool) string {
	if s.session == "" {
		if session != "" || needSession {
			return spool.CodeNoSession
		}
		return ""
	}
	if session != s.session {
		return spool.CodeBusy
	}
	return ""
}

func refuse(code string) spool.Response {
	return spool.Response{OK: false, Code: code, Error: "refused by the session"}
}

// parseRequest は要求を読む（docs/protocol.md「要求」）。大きすぎる・入れ子が深すぎる・JSON と
// して壊れている・tool が無い要求は読めない（プラグインは深さを解析の中で数えるが、
// encoding/json は上限が違うので、解析の前に数える）。args が無い・オブジェクトでなければ {} とする。
func parseRequest(data []byte) (tool string, args json.RawMessage, session string, ok bool) {
	if len(data) > spool.MaxRequestBytes || spool.NestingDepth(data) > spool.MaxNestingDepth {
		return "", nil, "", false
	}
	var request struct {
		Tool    string          `json:"tool"`
		Args    json.RawMessage `json:"args"`
		Session string          `json:"session"`
	}
	if json.Unmarshal(data, &request) != nil || request.Tool == "" {
		return "", nil, "", false
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(request.Args, &object) != nil || object == nil {
		request.Args = json.RawMessage("{}")
	}
	return request.Tool, request.Args, request.Session, true
}
