// Package spool は、プラグイン側のブリッジとファイルで受け渡しを行う通信規則（docs/protocol.md）の
// CLI 側の実装である。
//
// 通信規則の真実は docs/protocol.md で、プラグイン側（C++）と本パッケージはその対になる。いずれかを
// 変更したら仕様書と両方を修正する。ProtocolVersion を上げるかは docs/protocol.md「互換性」に従う。
//
// 本パッケージは排他を保持しない。排他はプラグインの占有（docs/protocol.md「占有」）のみで、CLI は
// セッション ID を要求に含めるのみである。占有しない呼び出し元同士が同時に要求を配置しても
// 受け渡しは破綻しないが、図面に対する操作の順序は呼び出し元の責任である
// （docs/design.md「CLI のプリミティブ性の維持」）。
package spool

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

// スプールに配置するファイルの綴り。プラグイン側と対。
const (
	RequestSuffix  = ".req.json"
	ResponseSuffix = ".res.json"
	WaitSuffix     = ".wait"
	LockFile       = "bridge.lock"
	TempSuffix     = ".tmp"

	// ProtocolVersion は受け渡しの版（上げる場合は docs/protocol.md「互換性」）。表示のためのみで、
	// 実行時に照合はしない（CLI とプラグインは同一の zip から同時にインストールされ、更新は
	// Vectorworks の終了後にのみ行うので、両側は常に同一のビルドである。docs/protocol.md「版」）。
	ProtocolVersion = 7

	// MaxRequestBytes はプラグイン側が受け付ける要求 1 件の上限。超える要求は配置する前に拒否する
	// （配置しても「読み取れない要求」として失敗が返るのみなので）。
	MaxRequestBytes = 1 << 20

	// MaxNestingDepth はプラグイン側が受け付ける要求の入れ子（オブジェクトと配列）の深さの上限。
	// 要求の外側のオブジェクトを 1 と数える。深すぎる要求で JSON の解析がスタックを
	// 溢れさせ、Vectorworks ごと異常終了しないための関門（docs/protocol.md「要求」）。超える要求は
	// 配置する前に拒否する。
	MaxNestingDepth = 64
)

// idPattern は id として使用してよい綴り。プラグイン側はこれを満たさない要求を受け付けない
// （ファイル名に使用するので、スプールの外へ書き込ませないための関門）。
var idPattern = regexp.MustCompile(`^[0-9A-Za-z_-]{1,64}$`)

// ValidID は id の綴りが正しいか。
func ValidID(id string) bool {
	return idPattern.MatchString(id)
}

// Request は要求 1 件の封筒（docs/protocol.md「要求」）。Call と擬似プラグインが同一の型を使用し、
// 片側のみ綴りが変わらないようにする。
type Request struct {
	Tool    string          `json:"tool"`
	Args    json.RawMessage `json:"args"`
	Session string          `json:"session,omitempty"`
}

// Response は応答 1 件。OK が false の場合のみ Code（機械が読み取る種別）と Error（人向けの理由）が
// 入り、ツールが詳細を返した失敗（invalid_args・internal）のみが Detail（JSON のオブジェクト）を
// 有する。id はファイル名が保持する。未知のフィールドは無視する（docs/protocol.md「互換性」）。
type Response struct {
	OK     bool            `json:"ok"`
	Code   string          `json:"code,omitempty"`
	Error  string          `json:"error,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Detail json.RawMessage `json:"detail,omitempty"`
}

// 失敗の種別（Response.Code）。綴りは通信規則で固定する（docs/protocol.md「応答」）。CLI は
// これを解釈せず、そのまま呼び出し元へ渡す。
const (
	// 要求が読み取れない（JSON として破損している・大きすぎる・tool が無い）
	CodeInvalidRequest = "invalid_request"
	// 未知のツール
	CodeUnknownTool = "unknown_tool"
	// ツールの引数が誤っている
	CodeInvalidArgs = "invalid_args"
	// その他（ツールの中の例外など）
	CodeInternal = "internal"
	// 待機標識（<id>.wait）が無いので実行しなかった
	CodeNoWait = "no_wait"
	// Vectorworks かプラグインが処理中（終了の依頼の最中・ツールの中のダイアログで受付処理が
	// 入れ子になった）なので実行しなかった。処理中でも周期処理が来ない間は busy も返らない
	CodeBusy = "busy"
	// 他の呼び出し元が占有しているので実行しなかった
	CodeOccupied = "occupied"
	// 含めたセッション ID が現在の占有のものではない・占有の中でのみ呼び出せるツールなので
	// 実行しなかった
	CodeNoSession = "no_session"
)

// ErrBusy は、Vectorworks かプラグインが処理中なので実行しなかった（要求は実行されていない）。
// 理由（終了の依頼・他の要求の処理）はプラグインの error に記述されており、エラーの
// 文言に含める。
var ErrBusy = errors.New("the bridge is busy")

// ErrNotRunning はブリッジが見つからない（Vectorworks が起動していない・プラグインが
// 読み込まれていない・場所に齟齬がある）。
var ErrNotRunning = errors.New("bridge is not running")

// ErrTimeout は、Vectorworks は動作している（ロックが取得されている）が期限までに応答が
// 無かった。Vectorworks が周期処理を配信しない（重い処理・一部のダイアログの最中）ことが多い。
var ErrTimeout = errors.New("timed out waiting for the response")

// ErrNoWait は、プラグインが no_wait で拒否した（待機標識が無いので実行しなかった）。ErrTimeout と
// 同様に実行されていないが、期限まで待機したとは限らない（掃除が待機標識を削除した場合などは、
// 期限の前に届く）ので、待機しきれなかったとは示さない。
var ErrNoWait = errors.New("the bridge did not run the request, as its wait file was missing")

// ErrMalformedResponse は、応答のファイルが JSON として破損していた（docs/protocol.md「ファイル」）
// か、失敗の応答の code が無い・未知の値だった（docs/protocol.md「互換性」: 既知のフィールドの
// 未知の値はエラー）。
var ErrMalformedResponse = errors.New("malformed response")

// ErrNoResponse は、待機を中止した時点で要求をプラグインが既に受け取っており、猶予の間にも
// 応答が届かなかった。要求は実行されたかもしれない（ErrTimeout は実行されない）。
var ErrNoResponse = errors.New("the bridge took the request but no response came")

// takenGrace は、待機を中止した時点で要求をプラグインが受け取っていた場合に、応答を待機する猶予。
// 待機を中止する直前に受け取られた要求の応答を取得するためのもので、ツールがこれより長く掛かれば
// ErrNoResponse（実行されたかもしれない）で終了する（docs/protocol.md「待機の中止」）。テストが
// 短縮する。
var takenGrace = 5 * time.Second

// Bridge はスプール 1 つと、その状態。
type Bridge struct {
	Dir string
	// プラグインがロックを取得している（Vectorworks が動作している）
	Running bool
	// 動作していない理由
	Reason string
	// newID は Call が要求の id を生成する関数。nil ならば NewID（テストが固定の id に差し替える。
	// パッケージの変数にしないのは、並行するテスト同士で書き換えが競合しないため）。
	newID func(time.Time) string
}

// LockHeld は、スプール dir のロックが取得されているか（Open と同じ判定。
// docs/protocol.md「生存の判定」）。存在しない以外の理由でロックを調査できなければ、そのエラーも
// 返す。Open はそれを「動作していない」とするが、install・uninstall は、動作しているかが判明しない
// として何も変更しない。
func LockHeld(dir string) (bool, error) {
	return lockState(filepath.Join(dir, LockFile))
}

// Open はその場所のブリッジの状態を判定する（docs/protocol.md「生存の判定」）。
//
// 生死はロックのみで決定される。周期処理が届かずに応答しない間も、ロックは取得されたまま。
func Open(dir string) *Bridge {
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return &Bridge{Dir: dir, Reason: "not found"}
	}
	// 調査できないロックは「動作していない」とし、その理由を Reason に記載する（install・uninstall は LockHeld で
	// エラーを確認し、何も変更しない）。
	held, err := LockHeld(dir)
	switch {
	case err != nil:
		return &Bridge{Dir: dir, Reason: "cannot check the lock: " + err.Error()}
	case !held:
		return &Bridge{Dir: dir, Reason: "not running"}
	}
	return &Bridge{Dir: dir, Running: true}
}

// ErrRequestTooLarge と ErrRequestTooDeep は、プラグインが読み取らない要求（MaxRequestBytes を超える・入れ子が
// MaxNestingDepth を超える）であるため配置せずに拒否した（呼び出し元の使用方法の誤り。配置すればプラグインは
// 読み取らずに拒否する）。
var (
	ErrRequestTooLarge = errors.New("request is too large")
	ErrRequestTooDeep  = errors.New("request is nested too deeply")
)

// EncodeRequest は要求を書き込む形式にし、プラグインが読み取らない要求であれば ErrRequestTooLarge・
// ErrRequestTooDeep を包んで返す。args が空なら {}。Call もこれのみを使用するため、呼び出し元はブリッジを
// 確認する前に同じ検証ができる。
func EncodeRequest(tool string, args json.RawMessage, session string) ([]byte, error) {
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	payload, err := json.Marshal(Request{Tool: tool, Args: args, Session: session})
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	if len(payload) > MaxRequestBytes {
		return nil, fmt.Errorf("%w (%d bytes, limit %d)", ErrRequestTooLarge, len(payload), MaxRequestBytes)
	}
	if depth := NestingDepth(payload); depth > MaxNestingDepth {
		return nil, fmt.Errorf("%w (depth %d, limit %d)", ErrRequestTooDeep, depth, MaxNestingDepth)
	}
	return payload, nil
}

// NewID は要求の id を生成する。名前の昇順が概ね送信した順になるように時刻を先頭へ配置する
// （プラグイン側は名前の昇順で取り出す）。順序を保証するのは応答を受け取ってから次を送信した
// 要求のみである（同じ周期処理の id は乱数の部分で並び、時計の調整で時刻は前後し、公開も遅れうる。
// docs/protocol.md「id」）。
func NewID(now time.Time) string {
	var suffix [4]byte
	_, _ = rand.Read(suffix[:])
	return fmt.Sprintf("%019d-%s", now.UnixNano(), hex.EncodeToString(suffix[:]))
}

// Call はツールを 1 つ呼び出し、応答を待機する。session は占有のセッション ID（@session_start が
// 返す。占有しないならば空）で、要求に含める（docs/protocol.md「占有」）。
//
// 周期処理が届かずに応答しない間も要求を配置して timeout まで待機する（周期処理が戻れば
// 処理される）。待機する間は <id>.wait を取得し、プラグインに呼び出し元が待機していることを示す
// （docs/protocol.md「待機標識」）。待機を中止する場合は .wait を解放して削除してから、要求を
// 削除する。先に .wait を中止するので、要求を削除する前にプラグインが取り出しても実行されない。
// 呼び出し元が強制終了されても OS が .wait を解放するので、残った要求は実行されず、シグナルを受けて
// 後処理を行う必要も無い。
//
//   - 要求を削除できた: 取り出されていないので、実行されない。理由をその時点のロックで
//     ErrNotRunning / ErrTimeout に区別する（呼び出し元が再起動すべきか、待機すればよいかを判定
//     できるように）。
//   - 削除できない（存在しない・Windows でプラグインが開いている）: プラグインが受け取った。
//     中止する前に受け取っていれば実行される。takenGrace の間のみ応答を待機し、届かなければ
//     ErrNoResponse（実行されたかは判明しない）。
//
// no_wait の応答は通信規則の種別で「実行しなかった」を意味するので、ツールの失敗ではなく
// ErrNoWait として返す（中止した後にプラグインが受け取った場合や、.wait が想定外に消失した場合に
// 届く）。busy / occupied / no_session も「実行しなかった」なので、ErrBusy / ErrOccupied /
// ErrNoSession として返す。
func (b *Bridge) Call(tool string, args json.RawMessage, session string, timeout time.Duration) (*Response, error) {
	payload, err := EncodeRequest(tool, args, session)
	if err != nil {
		return nil, err
	}

	makeID := b.newID
	if makeID == nil {
		makeID = NewID
	}
	id := makeID(time.Now())
	requestPath := filepath.Join(b.Dir, id+RequestSuffix)
	responsePath := filepath.Join(b.Dir, id+ResponseSuffix)
	waitPath := filepath.Join(b.Dir, id+WaitSuffix)

	// 要求を公開する前に .wait を取得する（公開した時点で、プラグインが生死を判定できるように）。
	unlock, err := holdWait(waitPath)
	if err != nil {
		return nil, fmt.Errorf("hold %s: %w", id+WaitSuffix, err)
	}
	stopWaiting := sync.OnceFunc(func() {
		unlock()
		// 削除できなくても（Windows でプラグインがちょうど開いている）、解放してあるので
		// 実行されない。残った .wait はプラグインかロックを取得したときの掃除が削除する。
		_ = os.Remove(waitPath)
	})
	defer stopWaiting()
	if err := WriteAtomically(requestPath, payload); err != nil {
		return nil, fmt.Errorf("publish %s: %w", id+RequestSuffix, err)
	}

	deadline := time.Now().Add(timeout)
	// receive は until まで応答を待機する。届けば arrived を真にして、検証した結果を返す。
	receive := func(until time.Time) (response *Response, arrived bool, err error) {
		response, malformed := pollResponse(responsePath, until)
		switch {
		case malformed:
			return nil, true, fmt.Errorf("%w (%s; the response file is not a valid response object)", ErrMalformedResponse, tool)
		case response == nil:
			return nil, false, nil
		}
		response, err = checkResponse(tool, response)
		return response, true, err
	}
	if response, arrived, err := receive(deadline); arrived {
		return response, err
	}

	// 待機を中止してから要求を削除する（削除してから中止すると、その間にプラグインが要求を
	// 取り出して .wait を試し、実行してしまうことがある）。削除できれば、誰も待機しない応答も
	// 残らない。
	stopWaiting()
	if os.Remove(requestPath) == nil {
		if !Open(b.Dir).Running {
			return nil, fmt.Errorf("%w (stopped while waiting for %s)", ErrNotRunning, tool)
		}
		return nil, fmt.Errorf("%w (%s, %s; a dialog may be open)", ErrTimeout, tool, timeout)
	}
	if response, arrived, err := receive(time.Now().Add(takenGrace)); arrived {
		return response, err
	}
	return nil, fmt.Errorf("%w (%s, %s; it may have run)", ErrNoResponse, tool, timeout)
}

// checkResponse は届いた応答を検証し、呼び出し元へ返す形にする（docs/protocol.md「応答」）。
// ファイルに触れない。ツールの失敗（invalid_request・unknown_tool・invalid_args・internal）は
// 応答のまま返し、「実行しなかった」種別はそれぞれのエラーに、規則に合致しない応答は
// ErrMalformedResponse にする。
func checkResponse(tool string, response *Response) (*Response, error) {
	// detail を保持できるのはツールが返した失敗（invalid_args・internal）のみで、オブジェクトに限る
	// （docs/protocol.md「応答」）。null も「存在しない」ではなく破損した応答とする。
	if response.Detail != nil {
		if response.OK || (response.Code != CodeInvalidArgs && response.Code != CodeInternal) {
			return nil, fmt.Errorf("%w (%s; a detail on a response that cannot carry one)", ErrMalformedResponse, tool)
		}
		if !IsObject(response.Detail) {
			return nil, fmt.Errorf("%w (%s; the detail is not an object)", ErrMalformedResponse, tool)
		}
	}
	if response.OK {
		// 成功の result は必ずオブジェクト（docs/protocol.md「応答」）。
		if !IsObject(response.Result) {
			return nil, fmt.Errorf("%w (%s; the result is not an object)", ErrMalformedResponse, tool)
		}
		return response, nil
	}
	switch response.Code {
	case CodeNoWait:
		return nil, fmt.Errorf("%w (%s)", ErrNoWait, tool)
	case CodeBusy:
		if response.Error == "" {
			return nil, fmt.Errorf("%w (%s did not run)", ErrBusy, tool)
		}
		return nil, fmt.Errorf("%w (%s did not run: %s)", ErrBusy, tool, response.Error)
	case CodeOccupied:
		return nil, fmt.Errorf("%w (%s did not run)", ErrOccupied, tool)
	case CodeNoSession:
		return nil, fmt.Errorf("%w (%s did not run)", ErrNoSession, tool)
	case CodeInvalidRequest, CodeUnknownTool, CodeInvalidArgs, CodeInternal:
		return response, nil
	default:
		return nil, fmt.Errorf("%w (%s; unknown code %q: %s)", ErrMalformedResponse, tool, response.Code, response.Error)
	}
}

// IsObject は data が JSON のオブジェクトか（前後の空白は除いて確認する）。data は JSON として
// 読み取れたものを渡す（応答の result など）。
func IsObject(data json.RawMessage) bool {
	trimmed := bytes.TrimSpace(data)
	return len(trimmed) > 0 && trimmed[0] == '{'
}

// pollResponse は until まで応答のファイルを読み直す。届けば読み取って削除して返す。届かなければ
// nil。
//
// 読み取れない（未だ存在しない・開けない）応答は「未だ存在しない」とみなして読み直す。読み取れた
// 応答は decodeResponse で読み取り、応答として読み取れなければ待機せずに削除して malformed を
// 真にする。応答は rename で公開されるので、読み取れた応答は書き込みを終えたもので、破損していれば
// 書き手の不具合であり、待機しても修正されない（docs/protocol.md「ファイル」）。
func pollResponse(path string, until time.Time) (response *Response, malformed bool) {
	for {
		if text, err := os.ReadFile(path); err == nil {
			// 削除できなくても応答は取得できている。残骸はプラグイン側が開始時に掃除する。
			_ = os.Remove(path)
			return decodeResponse(text)
		}
		if time.Now().After(until) {
			return nil, false
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// decodeResponse は応答のファイルの内容を読み取る。応答として読み取れなければ（JSON として
// 破損している・形が合致しない）malformed を真にする。未知のフィールドは無視する
// （docs/protocol.md「互換性」）。値で受け取り、null も届いた応答として返す（checkResponse が
// 破損した応答にする）。
func decodeResponse(text []byte) (response *Response, malformed bool) {
	var received Response
	if json.Unmarshal(text, &received) != nil {
		return nil, true
	}
	return &received, false
}

// NestingDepth は JSON の入れ子（オブジェクトと配列）の深さ。外側のオブジェクトを 1 と数える。
// 文字列の中の括弧は数えない。JSON として正しいかは検証しない（解析する前に深さを確認するため）。
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

// WriteAtomically は同一のディレクトリへ書き込んでから rename する（読み手に書き込み途中のものを
// 見せない）。要求を書き込む CLI と、応答を書き込む擬似プラグイン（fakeplugin）が共に使用する。
//
// 書き込み途中のファイルは、存在すれば削除してから新たに作成する
// （O_EXCL。docs/protocol.md「ファイル」）。存在するものを開いて切り詰めると（os.WriteFile）、
// 配置されたリンク（シンボリックリンクでも hard link でも）の先、つまりスプールの外のファイルを
// 破損させてしまう。削除するのは名前のみで、リンクの先は残る。
func WriteAtomically(path string, data []byte) error {
	temp := path + TempSuffix
	if err := writeNew(temp, data); err != nil {
		return err
	}
	if err := os.Rename(temp, path); err != nil {
		_ = os.Remove(temp)
		return err
	}
	return nil
}

// writeNew は path を削除してから新たに作成し、data を書き込む。
func writeNew(path string, data []byte) error {
	_ = os.Remove(path)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, err = file.Write(data)
	return errors.Join(err, file.Close())
}
