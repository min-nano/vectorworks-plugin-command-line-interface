// Package fakeplugin はプラグイン側の受け付けを真似る（docs/protocol.md「プラグイン側の義務」・
// docs/plugin/bridge.md「受け付け 1 回」）。spool と vw2026 の単体テストだけが使う。
//
// 作法より緩く真似ると、CLI がプラグインの断る要求を送っても単体テストが気づかないので、
// 関門（id の綴り・回の始めに並べた要求だけの処理・大きさと入れ子の深さの上限・ロックを取ったときの掃除）は
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
// 止める。
func Start(t *testing.T, dir string, handle Handler) {
	t.Helper()
	release := HoldLock(t, dir)
	sweep(dir)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			case <-time.After(10 * time.Millisecond):
			}
			serveOnce(dir, handle)
		}
	}()
	t.Cleanup(func() {
		close(stop)
		<-done
		release()
	})
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

// serveOnce は受け付け 1 回。要求を名前の昇順で読み、消し、呼ぶ側が待っていれば実行する。
// 処理するのは回の始めに並べた要求だけで（件数と時間の上限は無い）、取り出した要求には同じ回の
// 中で応える。
func serveOnce(dir string, handle Handler) {
	entries, _ := os.ReadDir(dir)
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
		path := filepath.Join(dir, name)
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
		wait := filepath.Join(dir, id+spool.WaitSuffix)
		state := waitStateOf(wait)
		if state == waitReleased {
			_ = os.Remove(wait)
			continue
		}
		var response spool.Response
		if state == waitMissing {
			response = spool.Response{OK: false, Code: spool.CodeNoWait, Error: "no wait file"}
		} else if tool, args, ok := parseRequest(data); !ok {
			response = spool.Response{OK: false, Code: spool.CodeInvalidRequest, Error: "unreadable request"}
		} else {
			response = handle(tool, args)
		}
		out, _ := json.Marshal(response)
		temp := filepath.Join(dir, id+spool.ResponseSuffix+spool.TempSuffix)
		_ = os.WriteFile(temp, out, 0o600)
		_ = os.Rename(temp, filepath.Join(dir, id+spool.ResponseSuffix))
	}
}

// parseRequest は要求を読む（docs/protocol.md「要求」）。大きすぎる・入れ子が深すぎる・JSON と
// して壊れている・tool が無い要求は読めない（プラグインは深さを解析の中で数えるが、
// encoding/json は上限が違うので、解析の前に数える）。args が無い・オブジェクトでなければ {} とする。
func parseRequest(data []byte) (tool string, args json.RawMessage, ok bool) {
	if len(data) > spool.MaxRequestBytes || spool.NestingDepth(data) > spool.MaxNestingDepth {
		return "", nil, false
	}
	var request struct {
		Tool string          `json:"tool"`
		Args json.RawMessage `json:"args"`
	}
	if json.Unmarshal(data, &request) != nil || request.Tool == "" {
		return "", nil, false
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(request.Args, &object) != nil || object == nil {
		request.Args = json.RawMessage("{}")
	}
	return request.Tool, request.Args, true
}
