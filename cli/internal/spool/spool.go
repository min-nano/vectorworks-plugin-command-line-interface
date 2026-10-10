// Package spool は、プラグイン側のブリッジとファイルで受け渡しをする作法（docs/protocol.md）の
// CLI 側の実装である。
//
// 作法の真実は docs/protocol.md で、プラグイン側（C++）とこのパッケージはその対になる。
// どちらかを変えたら仕様書と両方を直す。ProtocolVersion を上げるかは docs/protocol.md「互換性」に従う。
//
// 排他は占有（HoldSession。docs/protocol.md「占有」）だけが持つ。占有しない呼ぶ側どうしが
// 同時に要求を置いても受け渡しは壊れないが、図面に対する操作の順序は呼ぶ側の責任である
// （docs/design.md「CLI はプリミティブに保つ」）。
package spool

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// スプールに置くファイルの綴り。プラグイン側と対。
const (
	RequestSuffix  = ".req.json"
	ResponseSuffix = ".res.json"
	WaitSuffix     = ".wait"
	LockFile       = "bridge.lock"
	TempSuffix     = ".tmp"

	// ProtocolVersion は受け渡しの版（上げる場合は docs/protocol.md「互換性」）。表示のためだけで、
	// 実行時に照合はしない（CLI とプラグインは同じ zip から同時に入り、更新は Vectorworks の
	// 終了後にしか行わないので、両側は常に同じビルドである。docs/protocol.md「版」）。
	ProtocolVersion = 5

	// MaxRequestBytes はプラグイン側が受け付ける要求 1 件の上限。超える要求は置く前に断る
	// （置いても「読めない要求」として失敗が返るだけなので）。
	MaxRequestBytes = 1 << 20

	// MaxNestingDepth はプラグイン側が受け付ける要求の入れ子（オブジェクトと配列）の深さの上限。
	// 要求の外側のオブジェクトを 1 と数える。深すぎる要求で JSON の解析がスタックを溢れさせ、
	// Vectorworks ごと落ちないための関門（docs/protocol.md「要求」）。超える要求は置く前に断る。
	MaxNestingDepth = 64
)

// idPattern は id として使ってよい綴り。プラグイン側はこれを満たさない要求を受け付けない
// （ファイル名に使うので、スプールの外へ書かせないための関門）。
var idPattern = regexp.MustCompile(`^[0-9A-Za-z_-]{1,64}$`)

// ValidID は id の綴りが正しいか。
func ValidID(id string) bool {
	return idPattern.MatchString(id)
}

// Response は応答 1 件。OK が false のときだけ Code（機械が読む種別）と Error（人向けの理由）が
// 入る。id はファイル名が持つ。知らないフィールドは無視する（docs/protocol.md「互換性」）。
type Response struct {
	OK     bool            `json:"ok"`
	Code   string          `json:"code,omitempty"`
	Error  string          `json:"error,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
}

// 失敗の種別（Response.Code）。綴りは作法で固定する（docs/protocol.md「応答」）。CLI は
// これを解釈せず、そのまま呼ぶ側へ渡す。
const (
	CodeInvalidRequest = "invalid_request" // 要求が読めない（JSON として壊れている・大きすぎる・tool が無い）
	CodeUnknownTool    = "unknown_tool"    // 知らない道具
	CodeInvalidArgs    = "invalid_args"    // 道具の引数が誤っている
	CodeInternal       = "internal"        // そのほか（道具の中の例外など）
	CodeNoWait         = "no_wait"         // 待つ印（<id>.wait）が無いので実行しなかった
	CodeBusy           = "busy"            // ほかの呼ぶ側が占有しているので実行しなかった
	CodeNoSession      = "no_session"      // 載せた占有の印がいまの占有のものではないので実行しなかった
)

// ErrNotRunning はブリッジが見つからない（Vectorworks が起動していない・プラグインが
// 読み込まれていない・場所が食い違っている）。
var ErrNotRunning = errors.New("bridge is not running")

// ErrTimeout は、Vectorworks は動いている（ロックが掴まれている）が締切までに応答が
// 無かった。受け付けが見送られている（モーダルダイアログ・undo の記録の最中）ことが多い。
var ErrTimeout = errors.New("timed out waiting for the response")

// ErrMalformedResponse は、応答のファイルが JSON として壊れたまま malformedGrace を過ぎた
// （docs/protocol.md「ファイル」）か、失敗の応答の code が無い・知らない値だった
// （docs/protocol.md「互換性」: 知っているフィールドの未知の値はエラー）。
var ErrMalformedResponse = errors.New("malformed response")

// ErrNoResponse は、待つのをやめたときに要求をプラグインが既に受け取っていて、猶予の間にも
// 応答が届かなかった。要求は実行されたかもしれない（ErrTimeout は実行されない）。
var ErrNoResponse = errors.New("the bridge took the request but no response came")

// takenGrace は、待つのをやめたときに要求をプラグインが受け取っていたら、応答を待つ猶予。
// 応答は受け付けの同じ回の中で書かれ、受け付け 1 回の中で数秒を超える道具は持たない
// （docs/protocol.md「応答」）。テストが縮める。
var takenGrace = 5 * time.Second

// malformedGrace は、応答のファイルが JSON として壊れたままなら、壊れた応答とみなすまでの時間。
// rename で公開する作法なので、壊れて見えるのは書き手の不具合のときだけで、締切まで待っても
// 直らない（docs/protocol.md「ファイル」）。テストが縮める。
var malformedGrace = time.Second

// Bridge はスプール 1 つと、その状態。
type Bridge struct {
	Dir     string
	Running bool   // プラグインがロックを掴んでいる（Vectorworks が動いている）
	Reason  string // 動いていない理由
	Session bool   // いずれかの呼ぶ側が占有している（Vectorworks が動いていなくても）
}

// Open はその場所のブリッジの状態を判定する（docs/protocol.md「生存の判定」）。
//
// 生死はロックだけで決まる。受け付けが見送られている間も、ロックは掴まれたまま。
func Open(dir string) *Bridge {
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return &Bridge{Dir: dir, Reason: "not found"}
	}
	session := SessionHeld(dir)
	if !lockHeld(filepath.Join(dir, LockFile)) {
		return &Bridge{Dir: dir, Reason: "not running", Session: session}
	}
	return &Bridge{Dir: dir, Running: true, Session: session}
}

// NewID は要求の id を作る。**名前の昇順が送った順になる**ように時刻を先頭へ置く
// （プラグイン側は名前の昇順で取り出す。別々のプロセスからの要求もこれで送った順に並ぶ）。
func NewID(now time.Time) string {
	var suffix [4]byte
	_, _ = rand.Read(suffix[:])
	return fmt.Sprintf("%019d-%s", now.UnixNano(), hex.EncodeToString(suffix[:]))
}

// Call は道具を 1 つ呼び、応答を待つ。session は占有の印（HoldSession。占有しないなら空）で、
// 要求に載せる（docs/protocol.md「占有」）。
//
// 受け付けが見送られていても要求を置いて timeout まで待つ（受け付けが戻れば処理される）。
// 待つ間は <id>.wait を掴み、プラグインに呼ぶ側が待っていることを示す（docs/protocol.md
// 「待つ印」）。待つのをやめるときは .wait を放して消してから、要求を消す。先に .wait を
// やめるので、要求を消す前にプラグインが取り出しても実行されない。呼ぶ側が強制終了されても
// OS が .wait を放すので、残った要求は実行されず、シグナルを受けて後始末をする必要も無い。
//
//   - 要求を消せた: 取り出されていないので、実行されない。理由をその時点のロックで
//     ErrNotRunning / ErrTimeout に分ける（呼ぶ側が起動し直すべきか、待てばよいかを判定
//     できるように）。
//   - 消せない（無い・Windows でプラグインが開いている）: プラグインが受け取った。やめる前に
//     受け取っていれば実行される。takenGrace だけ応答を待ち、届かなければ ErrNoResponse
//     （実行されたかは分からない）。
//
// no_wait の応答は作法の種別で「実行しなかった」を意味するので、道具の失敗ではなく
// ErrNotRunning / ErrTimeout として返す（やめたあとにプラグインが受け取ったときや、.wait が
// 想定外に消えたときに届く）。busy / no_session も「実行しなかった」なので、ErrBusy /
// ErrNoSession として返す。
func (b *Bridge) Call(tool string, args json.RawMessage, session string, timeout time.Duration) (*Response, error) {
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	payload, err := json.Marshal(struct {
		Tool    string          `json:"tool"`
		Args    json.RawMessage `json:"args"`
		Session string          `json:"session,omitempty"`
	}{tool, args, session})
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	if len(payload) > MaxRequestBytes {
		return nil, fmt.Errorf("request is too large (%d bytes, limit %d)", len(payload), MaxRequestBytes)
	}
	if depth := NestingDepth(payload); depth > MaxNestingDepth {
		return nil, fmt.Errorf("request is nested too deeply (depth %d, limit %d)", depth, MaxNestingDepth)
	}

	id := NewID(time.Now())
	requestPath := filepath.Join(b.Dir, id+RequestSuffix)
	responsePath := filepath.Join(b.Dir, id+ResponseSuffix)
	waitPath := filepath.Join(b.Dir, id+WaitSuffix)

	// 要求を公開する前に .wait を掴む（公開した時点で、プラグインが生死を判定できるように）。
	unlock, err := holdWait(waitPath)
	if err != nil {
		return nil, fmt.Errorf("hold %s: %w", id+WaitSuffix, err)
	}
	waiting := true
	stopWaiting := func() {
		if waiting {
			waiting = false
			unlock()
			// 消せなくても（Windows でプラグインがちょうど開いている）、放してあるので実行されない。
			// 残った .wait はプラグインかロックを取ったときの掃除が消す。
			_ = os.Remove(waitPath)
		}
	}
	defer stopWaiting()
	if err := writeAtomically(requestPath, payload); err != nil {
		return nil, err
	}

	deadline := time.Now().Add(timeout)
	notRun := func(reason string) error {
		if !Open(b.Dir).Running {
			return fmt.Errorf("%w (stopped while waiting for %s)", ErrNotRunning, tool)
		}
		return fmt.Errorf("%w (%s, %s; %s)", ErrTimeout, tool, timeout, reason)
	}
	received := func(response *Response) (*Response, error) {
		if response.OK {
			return response, nil
		}
		switch response.Code {
		case CodeNoWait:
			return nil, notRun("the bridge did not run it")
		case CodeBusy:
			return nil, fmt.Errorf("%w (%s did not run)", ErrBusy, tool)
		case CodeNoSession:
			return nil, fmt.Errorf("%w (%s did not run)", ErrNoSession, tool)
		case CodeInvalidRequest, CodeUnknownTool, CodeInvalidArgs, CodeInternal:
			return response, nil
		default:
			return nil, fmt.Errorf("%w (%s; unknown code %q: %s)", ErrMalformedResponse, tool, response.Code, response.Error)
		}
	}

	poll := &poller{path: responsePath}
	malformed := func() error {
		return fmt.Errorf("%w (%s; the response file stayed broken for %s)", ErrMalformedResponse, tool, malformedGrace)
	}
	if response, err := poll.until(deadline); err != nil {
		return nil, malformed()
	} else if response != nil {
		return received(response)
	}

	// 待つのをやめてから要求を消す（消してからやめると、その間にプラグインが要求を取り出して
	// .wait を試し、実行してしまうことがある）。消せれば、誰も待たない応答も残らない。
	stopWaiting()
	if os.Remove(requestPath) == nil {
		return nil, notRun("a dialog may be open")
	}
	if response, err := poll.until(time.Now().Add(takenGrace)); err != nil {
		return nil, malformed()
	} else if response != nil {
		return received(response)
	}
	return nil, fmt.Errorf("%w (%s, %s; it may have run)", ErrNoResponse, tool, timeout)
}

// poller は応答のファイルを読み直す。壊れたままの応答を見分けるため、壊れていると最初に
// 見た時刻を、締切の前と猶予の間を通して持つ。
type poller struct {
	path        string
	brokenSince time.Time
}

// until は until まで応答を読み直す。届けば読んで消して返す。届かなければ nil。
//
// 読めない（まだ無い・Windows で書き手が開いている）・JSON として壊れている応答は「まだ無い」と
// みなして読み直すが、壊れたまま malformedGrace を過ぎたら、消して ErrMalformedResponse を返す。
func (p *poller) until(until time.Time) (*Response, error) {
	for {
		text, err := os.ReadFile(p.path)
		if err != nil {
			p.brokenSince = time.Time{}
		} else {
			var response Response
			if json.Unmarshal(text, &response) == nil {
				// 消せなくても応答は取得できている。残骸はプラグイン側が開始時に掃除する。
				_ = os.Remove(p.path)
				return &response, nil
			}
			if p.brokenSince.IsZero() {
				p.brokenSince = time.Now()
			} else if time.Since(p.brokenSince) >= malformedGrace {
				_ = os.Remove(p.path)
				return nil, ErrMalformedResponse
			}
		}
		if time.Now().After(until) {
			return nil, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// NestingDepth は JSON の入れ子（オブジェクトと配列）の深さ。外側のオブジェクトを 1 と数える。
// 文字列の中の括弧は数えない。JSON として正しいかは確かめない（解析する前に深さを見るため）。
func NestingDepth(data []byte) int {
	depth, deepest := 0, 0
	inString, escaped := false, false
	for _, c := range data {
		switch {
		case escaped:
			escaped = false
		case inString:
			if c == '\\' {
				escaped = true
			} else if c == '"' {
				inString = false
			}
		case c == '"':
			inString = true
		case c == '{' || c == '[':
			depth++
			deepest = max(deepest, depth)
		case c == '}' || c == ']':
			depth--
		}
	}
	return deepest
}

// writeAtomically は同じディレクトリへ書いてから rename する（読み手に書きかけを見せない）。
//
// Windows では、置き換える先（前の占有の残した session.json）を読み手がちょうど開いていると
// rename が失敗するので、少しの間やり直す。
func writeAtomically(path string, data []byte) error {
	temp := path + TempSuffix
	name := filepath.Base(path)
	if err := os.WriteFile(temp, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		err := os.Rename(temp, path)
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			_ = os.Remove(temp)
			return fmt.Errorf("publish %s: %w", name, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
