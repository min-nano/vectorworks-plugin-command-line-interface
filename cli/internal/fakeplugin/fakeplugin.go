// Package fakeplugin はプラグイン側の受け付けを真似る（docs/protocol.md「プラグイン側の義務」・
// docs/plugin/bridge.md「受け付け 1 回」）。spool と vw2026 の単体テストだけが使う。
//
// 通信規則より緩く真似ると、CLI がプラグインの断る要求を送っても単体テストが気づかないので、
// 関門（id の綴り・回の始めに並べた要求だけの処理・大きさと入れ子の深さの上限・占有・ロックを取ったときの掃除）は
// プラグインと同じに持つ。プラグインと同じに振る舞うかは、両方のテストが読む通信規則の見本
// （protocol/fixtures。docs/fixtures.md）で確かめる。道具の表は持たないので、道具の種類は
// StartWithKinds で受け取り、受け取らない道具は quit なら種類 app、ほかを read とみなす。種類
// write と app は占有の中でだけ呼べ、種類 app の道具（quit）が成功したらその回の残りを取り出さない。
// 受け付けの入れ子は真似ない（Go の偽の道具はイベントを回さない）。
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
	// 呼ぶ側が待っている
	waitHeld waitState = iota
	// 呼ぶ側が放した・死んだ
	waitReleased
	// 無い（通信規則に反する）
	waitMissing
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
	dir = extendedPath(dir)
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
// 持つ（Vectorworks を起動し直すと消えるのと同じ）。quit だけを種類 app、ほかを read とみなす。
func Start(t *testing.T, dir string, handle Handler) (stop func()) {
	t.Helper()
	return StartWithKinds(t, dir, nil, handle)
}

// StartWithKinds は Start と同じで、道具の種類（read・write・app）を道具の名前ごとに受け取る。
// kinds に無い道具は、quit なら app、ほかは read とみなす。
func StartWithKinds(t *testing.T, dir string, kinds map[string]string, handle Handler) (stop func()) {
	t.Helper()
	// プラグインと同じく、予約された名前（NUL など）の要求もファイルとして開く
	// （docs/protocol.md「id」）。
	s := &server{dir: extendedPath(dir), handle: handle, kinds: kinds}
	if !s.take() {
		t.Fatalf("cannot take %s", spool.LockFile)
	}
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
			s.release()
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
	dir    string
	handle Handler
	// 道具の名前ごとの種類。無い道具は、quit なら app、ほかは read
	kinds map[string]string
	// 受け付けの外の状態（"quitting"）。空でなければ種類 write と app を busy で断る
	activity string
	// 掴んでいるロックを放す。nil ならロックを掴んでいない
	unlock func()
	// 発行している占有の印。空なら占有されていない
	session string
}

// take はロックを取ってみて、取れたら前の回の残骸を消す。
func (s *server) take() bool {
	unlock, err := lock(filepath.Join(s.dir, spool.LockFile))
	if err != nil {
		return false
	}
	s.unlock = unlock
	sweep(s.dir)
	return true
}

func (s *server) release() {
	if s.unlock != nil {
		s.unlock()
		s.unlock = nil
	}
}

// serveOnce は受け付け 1 回。ロックを掴んでいなければ取ってみて、取れなければ何もせずに
// serving を偽で返す。要求を名前の昇順で読み、消し、呼ぶ側が待っていれば実行する。処理するのは
// 回の始めに並べた要求だけで（件数と時間の上限は無い）、取り出した要求には同じ回の中で応える。
// quit が成功したら、その回の残りの要求を取り出さずに quit を真で返す。
func (s *server) serveOnce() (serving, quit bool) {
	if s.unlock == nil && !s.take() {
		return false, false
	}
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
		var tool string
		var session *string
		if state == waitMissing {
			response = spool.Response{OK: false, Code: spool.CodeNoWait, Error: "no wait file"}
		} else if name, args, held, ok := parseRequest(data); !ok {
			response = spool.Response{OK: false, Code: spool.CodeInvalidRequest, Error: "unreadable request"}
		} else {
			tool = name
			response, session = s.answer(tool, args, held)
		}
		out, _ := json.Marshal(response)
		// 占有の印と終了は、応答を書けたときだけ反映する（docs/protocol.md「占有」）。
		if spool.WriteAtomically(filepath.Join(s.dir, id+spool.ResponseSuffix), out) != nil {
			continue
		}
		if session != nil {
			s.session = *session
		}
		// 終了したら応えられないので、残りは置いたままにする。
		if s.kind(tool) == "app" && response.OK {
			return true, true
		}
	}
	return true, false
}

// answer は要求を占有に照らしてから応える（docs/protocol.md「占有」）。
//
//   - tools と種類 read の道具は占有に照らさない（図面を変えないので、占有している呼ぶ側の作業と
//     混ざらない）。表に無い道具も read とみなすので、照らさずに handle が unknown_tool で応える。
//   - session_start は占有されていなければ印を発行し、されていれば occupied。
//   - session_end と種類 write・app は占有の中でだけ呼べる。種類 write・app は、Vectorworks が処理中
//     （activity）なら busy で断る。
//
// 占有の印は変えず、新しい印（空なら占有の終わり）を返す。反映するのは応答を書けたときだけ。
func (s *server) answer(tool string, args json.RawMessage, session string) (spool.Response, *string) {
	switch tool {
	case spool.ToolTools:
		return s.run(tool, args), nil
	case spool.ToolSessionStart:
		if s.session != "" {
			return refuse(spool.CodeOccupied), nil
		}
		if session != "" {
			return refuse(spool.CodeNoSession), nil
		}
		issued := spool.NewID(time.Now())
		result, _ := json.Marshal(map[string]string{"session": issued})
		return spool.Response{OK: true, Result: result}, &issued
	case spool.ToolSessionEnd:
		if code := s.check(session); code != "" {
			return refuse(code), nil
		}
		ended := ""
		return spool.Response{OK: true, Result: json.RawMessage(`{"ended":true}`)}, &ended
	}
	if s.kind(tool) != "read" {
		if code := s.check(session); code != "" {
			return refuse(code), nil
		}
		if s.activity != "" {
			return refuse(spool.CodeBusy), nil
		}
	}
	return s.run(tool, args), nil
}

// run は道具を呼ぶ。成功の result はプラグインと同じく必ずオブジェクトにする（通信規則「応答」）。
// 値を返さなければ {}、オブジェクトでない値は道具の不具合として internal。
func (s *server) run(tool string, args json.RawMessage) spool.Response {
	response := s.handle(tool, args)
	if !response.OK {
		return response
	}
	switch {
	case len(response.Result) == 0 || string(response.Result) == "null":
		response.Result = json.RawMessage(`{}`)
	case !spool.IsObject(response.Result):
		return spool.Response{Code: spool.CodeInternal, Error: "the result of " + tool + " is not an object"}
	}
	return response
}

// kind は道具の種類。kinds に無い道具は、quit なら app、ほかは read（本物の道具の表の quit は
// 種類 app なので、kinds を渡したテストでも quit は占有の中でだけ呼べる）。
func (s *server) kind(tool string) string {
	if kind, found := s.kinds[tool]; found {
		return kind
	}
	if tool == "quit" {
		return "app"
	}
	return "read"
}

// check は占有の中でだけ呼べる要求の印を占有に照らす。実行してよければ空、断るならその code。
func (s *server) check(session string) string {
	if s.session == "" {
		return spool.CodeNoSession
	}
	if session != s.session {
		return spool.CodeOccupied
	}
	return ""
}

func refuse(code string) spool.Response {
	return spool.Response{OK: false, Code: code, Error: "refused by the fake plug-in"}
}

// parseRequest は要求を読む（docs/protocol.md「要求」）。大きすぎる・入れ子が深すぎる・JSON と
// して壊れている・tool が無い（null・空を含む）・知っているフィールドの型が違う要求は読めない
// （プラグインは深さを解析の中で数えるが、encoding/json は上限が違うので、解析の前に数える）。
// args は省略できるフィールドなので、無い・null なら既定値の {} とし、オブジェクト以外は読めない
// 要求とする。session の null は無いものとする。
//
// 構造体へ直に解析すると encoding/json はキーを大文字と小文字を区別せずに照合し、ARGS のような
// 知らないキーまで拾ってしまう。通信規則ではキーは区別して照合するので、map に解析して正確な綴りで引く。
func parseRequest(data []byte) (tool string, args json.RawMessage, session string, ok bool) {
	if len(data) > spool.MaxRequestBytes || spool.NestingDepth(data) > spool.MaxNestingDepth {
		return "", nil, "", false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || fields == nil {
		return "", nil, "", false
	}
	if json.Unmarshal(fields["tool"], &tool) != nil || tool == "" {
		return "", nil, "", false
	}
	if raw, found := fields["session"]; found && json.Unmarshal(raw, &session) != nil {
		return "", nil, "", false
	}
	args = fields["args"]
	if len(args) == 0 || string(args) == "null" {
		args = json.RawMessage("{}")
	}
	// RawMessage は解析済みの値をそのまま持つので、先頭の 1 文字でオブジェクトかが分かる。
	if args[0] != '{' {
		return "", nil, "", false
	}
	return tool, args, session, true
}
