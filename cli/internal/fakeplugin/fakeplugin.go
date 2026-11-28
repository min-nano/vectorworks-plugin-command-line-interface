// Package fakeplugin はプラグイン側の受付処理を模倣する（docs/protocol.md「プラグイン側の義務」・
// docs/plugin/bridge.md「1 回の受付処理」）。spool と vw2026 の単体テストのみが使用する。
//
// 通信規則より緩く模倣すると、CLI がプラグインの拒否する要求を送信しても単体テストが
// 検出しないので、関門（id の綴り・回の開始時に並んでいた要求のみの処理・大きさと入れ子の深さの
// 上限・占有・ロックを取得した場合の掃除）はプラグインと同一に保持する。プラグインと同一に
// 振る舞うかは、両方のテストが読み込む通信規則のフィクスチャ（protocol/fixtures。
// docs/fixtures.md）で検証する。ツールの一覧は保持しないので、ツールの種類は StartWithKinds で
// 受け取り、受け取らないツールは quit ならば種類 app、他を read とみなす。種類 write と app は
// 占有の中でのみ呼び出せる。終了の依頼は種類では決定せず、実際のツールの一覧と同様に quit のみが
// 要求するとみなし、quit が成功したらその回の残りを取り出さない。受付処理の入れ子は模倣しない
// （Go の擬似ツールはイベントを処理しない）。
package fakeplugin

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/spool"
)

// waitState は <id>.wait の状態。
type waitState int

const (
	// 呼び出し元が待機している
	waitHeld waitState = iota
	// 呼び出し元が解放した・異常終了した
	waitReleased
	// 存在しない（通信規則に反する。通常のファイルでない場合も）
	waitMissing
	// 確かめられない（無い以外の理由で開けない・試せない）。実行せず、掃除では消さない
	waitUnknown
)

// Handler はツール 1 つの呼び出しに応答する。
type Handler func(tool string, args json.RawMessage) spool.Response

// NewDir は 0700 のスプールを作成する。
func NewDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), spool.SpoolDirName)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

// NewHeldDir は NewDir で作成したスプールのロックを取得する（Vectorworks が動作している）。返す
// 関数で解放する（終了を模倣する。テストの終了時にも解放する）。
func NewHeldDir(t *testing.T) (dir string, release func()) {
	t.Helper()
	dir = NewDir(t)
	return dir, HoldLock(t, dir)
}

// HoldLock はロックファイルを取得する（Vectorworks が動作している）。返す関数で解放する（終了を
// 模倣する）。テストの終了時にも解放する。
func HoldLock(t *testing.T, dir string) (release func()) {
	t.Helper()
	release, err := TryHoldLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	return release
}

// TryHoldLock は HoldLock と同様にロックファイルを取得するが、取得できなければテストを停止せずに
// エラーを返す（テストのゴルーチンの外から取得するため。t.Fatal はテストのゴルーチンからのみ
// 呼び出せる）。返す関数は何度呼び出してもよく、テストの終了時に解放するのは呼び出し元の責任で
// ある。
func TryHoldLock(dir string) (release func(), err error) {
	unlock, err := lock(filepath.Join(extendedPath(dir), spool.LockFile))
	if err != nil {
		return nil, err
	}
	return sync.OnceFunc(unlock), nil
}

// Start はロックを取得し、前回の残骸を削除してから、要求に handle で応答し続ける。テストの終了時に
// 停止する。返す関数で途中でも停止できる（Vectorworks の終了を模倣する）。占有のセッション ID は
// Start ごとに保持する（Vectorworks を再起動すると消失するのと同様）。quit のみを種類 app、他を
// read とみなす。
func Start(t *testing.T, dir string, handle Handler) (stop func()) {
	t.Helper()
	return StartWithKinds(t, dir, nil, handle)
}

// StartWithKinds は Start と同様で、ツールの種類（read・write・app）をツール名ごとに受け取る。
// kinds に存在しないツールは、quit ならば app、他は read とみなす。
func StartWithKinds(t *testing.T, dir string, kinds map[string]string, handle Handler) (stop func()) {
	t.Helper()
	// プラグインと同様に、予約された名前（NUL など）の要求もファイルとして開く
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
	stop = sync.OnceFunc(func() {
		close(quit)
		<-done
		s.release()
	})
	t.Cleanup(stop)
	return stop
}

// sweep はロックを取得した直後の掃除（docs/protocol.md「ロック」）。待機標識を
// 取得できる・存在しない id の要求・応答・待機標識・書き込み途中のファイルを削除し、待機している
// 呼び出し元のもの（確認できないものも）は残す。id は最初の . より前。
func sweep(dir string) {
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		name := entry.Name()
		if !(strings.HasSuffix(name, spool.RequestSuffix) || strings.HasSuffix(name, spool.ResponseSuffix) ||
			strings.HasSuffix(name, spool.WaitSuffix) || strings.HasSuffix(name, spool.TempSuffix)) {
			continue
		}
		id, _, _ := strings.Cut(name, ".")
		if state := waitStateOf(filepath.Join(dir, id+spool.WaitSuffix)); state == waitReleased || state == waitMissing {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
}

// server は受付処理を行うブリッジ 1 つ。占有のセッション ID（session）はメモリにのみ保持する
// （docs/protocol.md「占有」）。
type server struct {
	dir    string
	handle Handler
	// ツール名ごとの種類。存在しないツールは、quit ならば app、他は read
	kinds map[string]string
	// 成功したら終了を依頼するツール（ツールが返す終了の依頼）。nil ならば quit のみ
	quits map[string]bool
	// 受付処理の外の状態（"quitting"）。空でなければ一覧のツールを全て busy で拒否する（SDK を
	// 呼び出さない @tools・@session_start・@session_end と、一覧に無いツールは除く）
	activity string
	// 取得しているロックを解放する。nil ならばロックを取得していない
	unlock func()
	// 発行している占有のセッション ID。空ならば占有されていない
	session string
}

// take はロックの取得を試み、取得できたら前回の残骸を削除する。
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

// serveOnce は 1 回の受付処理。ロックを取得していなければ取得を試み、取得できなければ何もせずに
// serving を偽で返す。要求を名前の昇順で読み取り、削除し、呼び出し元が待機していれば実行する。
// 処理するのは回の開始時に並んでいた要求のみで（件数と時間の上限は無い）、取り出した要求には同じ
// 回の中で応答する。終了を依頼するツール（quits）が成功したら、その回の残りの要求を取り出さずに
// quit を真で返す。
func (s *server) serveOnce() (serving, quit bool) {
	if s.unlock == nil && !s.take() {
		return false, false
	}
	entries, _ := os.ReadDir(s.dir)
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		// 綴りを満たさない id の要求は受け付けない（読み取らず・削除せず・応答しない）。
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
			// 呼び出し元が取り下げた。黙って飛ばす。
			continue
		}
		if os.Remove(path) != nil {
			// 削除できない要求は実行しない（残すと次の回に再び実行してしまう。消失していたならば
			// 呼び出し元が取り下げた）。
			continue
		}
		// 待機する者の存在しなくなった要求は実行も応答もしない。.wait の無い要求は実行せずに
		// 拒否する。
		wait := filepath.Join(s.dir, id+spool.WaitSuffix)
		state := waitStateOf(wait)
		if state == waitReleased {
			_ = os.Remove(wait)
			continue
		}
		var response spool.Response
		var tool string
		var session *string
		if state == waitMissing || state == waitUnknown {
			response = spool.Response{OK: false, Code: spool.CodeNoWait, Error: "no wait file"}
		} else if name, args, held, ok := parseRequest(data); !ok {
			response = spool.Response{OK: false, Code: spool.CodeInvalidRequest, Error: "unreadable request"}
		} else {
			tool = name
			response, session = s.answer(tool, args, held)
		}
		out, _ := json.Marshal(response)
		// 占有のセッション ID と終了は、応答を書き込めた場合のみ反映する
		// （docs/protocol.md「占有」）。
		if spool.WriteAtomically(filepath.Join(s.dir, id+spool.ResponseSuffix), out) != nil {
			continue
		}
		if session != nil {
			s.session = *session
		}
		// 終了したら応答できないので、残りは配置したままにする。
		if s.quitsOn(tool) && response.OK {
			return true, true
		}
	}
	return true, false
}

// answer は要求を占有と照合してから応答する（docs/protocol.md「占有」）。
//
//   - @tools と種類 read のツールは占有と照合しない（図面を変更しないので、占有している呼び出し元の
//     作業と混在しない）。一覧に無いツールも占有と照合せず、handle が unknown_tool で応答する
//     （Vectorworks が処理中でも）。
//   - @session_start は占有されていなければセッション ID を発行し、占有されていれば occupied。
//   - @session_end と種類 write・app は占有の中でのみ呼び出せる。
//   - Vectorworks が処理中（activity）ならば、一覧のツールを全て busy で拒否する（種類 read と
//     ping も）。SDK を呼び出さない @tools・@session_start・@session_end は応答する。種類
//     write・app は占有との照合が先。
//
// 一覧に無いツールは kinds で判別する（kinds に無く quit でもないツール）。kinds を渡さない
// Start では判別できないので、いずれのツールも一覧にあるとみなす（処理中は unknown_tool ではなく
// busy になる）。
//
// 占有のセッション ID は変更せず、新しいセッション ID（空ならば占有の終了）を返す。反映するのは
// 応答を書き込めた場合のみ。
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
	}
	if s.activity != "" && s.inTable(tool) {
		return refuse(spool.CodeBusy), nil
	}
	return s.run(tool, args), nil
}

// run はツールを呼び出す。応答はプラグインと同一に整える（通信規則「応答」）。成功の result は必ず
// オブジェクトにし、値を返さなければ {}、オブジェクトでない値はツールの不具合として
// internal。detail は失敗にのみ含め、オブジェクトでない detail はツールの不具合として
// internal（detail なし）。null の detail は、プラグイン（Json の null）と同様に
// 「存在しない」とみなす。
func (s *server) run(tool string, args json.RawMessage) spool.Response {
	response := s.handle(tool, args)
	if string(bytes.TrimSpace(response.Detail)) == "null" {
		response.Detail = nil
	}
	if !response.OK {
		if len(response.Detail) > 0 && !spool.IsObject(response.Detail) {
			return spool.Response{Code: spool.CodeInternal, Error: "the detail of " + tool + " is not an object"}
		}
		return response
	}
	response.Detail = nil
	switch {
	case len(response.Result) == 0 || string(response.Result) == "null":
		response.Result = json.RawMessage(`{}`)
	case !spool.IsObject(response.Result):
		return spool.Response{Code: spool.CodeInternal, Error: "the result of " + tool + " is not an object"}
	}
	return response
}

// kind はツールの種類。kinds に存在しないツールは、quit ならば app、他は read（実際のツールの一覧の
// quit は種類 app なので、kinds を渡したテストでも quit は占有の中でのみ呼び出せる）。
func (s *server) kind(tool string) string {
	if kind, found := s.kinds[tool]; found {
		return kind
	}
	if tool == "quit" {
		return "app"
	}
	return "read"
}

// inTable は、ツールがツールの一覧にあるか。kinds が無ければ判別できないので、いずれの
// ツールもあるとみなす。
func (s *server) inTable(tool string) bool {
	if s.kinds == nil || tool == "quit" {
		return true
	}
	_, found := s.kinds[tool]
	return found
}

// quitsOn は、ツールが成功したら終了を依頼するか。quits が無ければ quit のみ（実際のツールの一覧で
// 終了を依頼するのは quit のみ）。
func (s *server) quitsOn(tool string) bool {
	if s.quits == nil {
		return tool == "quit"
	}
	return s.quits[tool]
}

// check は占有の中でのみ呼び出せる要求のセッション ID を占有と照合する。実行してよければ空、
// 拒否するならばその code。
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

// parseRequest は要求を読み取る（docs/protocol.md「要求」）。大きすぎる・入れ子が
// 深すぎる・JSON として破損している・tool が無い（null・空を含む）・既知のフィールドの型が異なる
// 要求は読み取れない（プラグインは深さを解析の中で数えるが、encoding/json は上限が異なるので、
// 解析の前に数える）。args は省略できるフィールドなので、存在しない・null ならば既定値の {} とし、
// オブジェクト以外は読み取れない要求とする。session の null は存在しないものとする。
//
// 構造体へ直接解析すると encoding/json はキーを大文字と小文字を区別せずに照合し、ARGS のような
// 未知のキーまで取得してしまう。通信規則ではキーは区別して照合するので、map に解析して正確な綴りで
// 参照する。
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
	if !spool.IsObject(args) {
		return "", nil, "", false
	}
	return tool, args, session, true
}
