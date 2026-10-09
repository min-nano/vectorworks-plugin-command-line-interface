// Package spool は、プラグイン側のブリッジとファイルで受け渡しをする作法（docs/protocol.md）の
// CLI 側の実装である。
//
// 作法の真実は docs/protocol.md で、プラグイン側（C++）とこのパッケージはその対になる。
// どちらかを変えたら仕様書と両方を直し、形を変えたなら ProtocolVersion を上げる。
//
// このパッケージは**排他を持たない**。同時に複数のプロセスが要求を置いても受け渡しは
// 壊れないが、図面に対する操作の順序や占有は呼ぶ側の責任である
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
	StatusFile     = "bridge.json"
	LockFile       = "bridge.lock"
	TempSuffix     = ".tmp"

	// ProtocolVersion は受け渡しの版。要求／応答／生存の印の形を変えたら上げる。
	ProtocolVersion = 1

	// StaleSeconds より古い生存の印は「応えていない」と判定する。プラグイン側は数秒ごとに
	// 書き直す。
	StaleSeconds = 15

	// MaxRequestBytes はプラグイン側が受け付ける要求 1 件の上限。超える要求は置く前に断る
	// （置いても「読めない要求」として失敗が返るだけなので）。
	MaxRequestBytes = 1 << 20
)

// idPattern は id として使ってよい綴り。プラグイン側はこれを満たさない要求を受け付けない
// （ファイル名に使うので、スプールの外へ書かせないための関門）。
var idPattern = regexp.MustCompile(`^[0-9A-Za-z_-]{1,64}$`)

// ValidID は id の綴りが正しいか。
func ValidID(id string) bool {
	return idPattern.MatchString(id)
}

// Status は生存の印（bridge.json）。未知のフィールドは Raw に残る。
type Status struct {
	Version  string  `json:"version"`
	Branch   string  `json:"branch"`
	Protocol int     `json:"protocol"`
	Beat     float64 `json:"beat"`
	PID      int     `json:"pid"`

	Raw json.RawMessage `json:"-"`
}

// Response は応答 1 件。OK が false のときだけ Error に理由が入る。id はファイル名が持つ。
type Response struct {
	OK     bool            `json:"ok"`
	Error  string          `json:"error,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
}

// ErrNotRunning はブリッジが見つからない（Vectorworks が起動していない・プラグインが
// 読み込まれていない・場所が食い違っている）。
var ErrNotRunning = errors.New("bridge is not running")

// ErrUnresponsive は、Vectorworks は動いているがブリッジが応えない（モーダルダイアログ・
// undo の記録の最中で、プラグインが受け付けを見送っている）。
var ErrUnresponsive = errors.New("vectorworks is running but the bridge is not responding")

// State はブリッジの状態（docs/protocol.md「生存の判定」）。
type State string

const (
	// StateLive はロックが掴まれていて、印が新しい。要求に応える。
	StateLive State = "live"
	// StateUnresponsive はロックが掴まれているが、印が古い（または無い）。プラグインが受け付けを
	// 見送っている間（mac ではモーダルの最中はタイマーも刻まない）は印が書き直されないので、
	// 古いことだけで「止まった」とはみなさない。置いた要求は、受け付けが戻れば処理される。
	StateUnresponsive State = "unresponsive"
	// StateDown はロックが掴まれていない（Vectorworks が終わった・プラグインが居ない）。
	StateDown State = "down"
)

// ErrTimeout は締切までに応答が無かった。
var ErrTimeout = errors.New("timed out waiting for the response")

// ProtocolError はプラグインと CLI の版が食い違っている。
type ProtocolError struct {
	Plugin int
	CLI    int
}

func (e *ProtocolError) Error() string {
	return fmt.Sprintf("protocol mismatch (plugin %d / cli %d)", e.Plugin, e.CLI)
}

// Bridge はスプール 1 つと、その状態。
type Bridge struct {
	Dir    string
	State  State
	Status *Status // 印が読めなければ nil
	Reason string  // StateDown の理由
}

// Open はその場所のブリッジの状態を判定する（docs/protocol.md「生存の判定」）。
//
// 持ち主と権限が安全で、プラグインがロックファイルを掴んでいれば Vectorworks は動いている。
// そのうえで印の beat が now から StaleSeconds 以内なら StateLive、そうでなければ
// StateUnresponsive。ロックの有無だけで生死を分けるので、印が古いことを「止まった」と誤らない。
func Open(dir string, now time.Time) *Bridge {
	if reason := checkSafe(dir); reason != "" {
		return &Bridge{Dir: dir, State: StateDown, Reason: reason}
	}
	if !lockHeld(filepath.Join(dir, LockFile)) {
		return &Bridge{Dir: dir, State: StateDown, Reason: "not running"}
	}
	bridge := &Bridge{Dir: dir, State: StateUnresponsive}
	text, err := os.ReadFile(filepath.Join(dir, StatusFile))
	if err != nil {
		// ロックを取ってから印を書くまでの間。動いてはいる。
		return bridge
	}
	var status Status
	if json.Unmarshal(text, &status) != nil {
		return bridge
	}
	status.Raw = append(json.RawMessage(nil), text...)
	bridge.Status = &status
	if float64(now.UnixNano())/1e9-status.Beat <= StaleSeconds {
		bridge.State = StateLive
	}
	return bridge
}

// CheckProtocol は版が一致するか。印が読めないうちは判定できないので通す。
func (b *Bridge) CheckProtocol() error {
	if b.Status != nil && b.Status.Protocol != ProtocolVersion {
		return &ProtocolError{Plugin: b.Status.Protocol, CLI: ProtocolVersion}
	}
	return nil
}

// NewID は要求の id を作る。**名前の昇順が送った順になる**ように時刻を先頭へ置く
// （プラグイン側は名前の昇順で取り出す。別々のプロセスからの要求もこれで送った順に並ぶ）。
func NewID(now time.Time) string {
	var suffix [4]byte
	_, _ = rand.Read(suffix[:])
	return fmt.Sprintf("%019d-%s", now.UnixNano(), hex.EncodeToString(suffix[:]))
}

// Call は道具を 1 つ呼び、応答を待つ。
//
// ブリッジが StateUnresponsive でも要求を置いて timeout まで待つ（受け付けが戻れば処理
// される）。待つのを諦めたときは置いた要求を取り下げ、理由をその時点の状態で
// ErrNotRunning / ErrUnresponsive / ErrTimeout に分ける（呼ぶ側が起動し直すべきか、
// 待てばよいかを判定できるように）。
func (b *Bridge) Call(tool string, args json.RawMessage, timeout time.Duration) (*Response, error) {
	if err := b.CheckProtocol(); err != nil {
		return nil, err
	}
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	payload, err := json.Marshal(struct {
		Tool string          `json:"tool"`
		Args json.RawMessage `json:"args"`
	}{tool, args})
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	if len(payload) > MaxRequestBytes {
		return nil, fmt.Errorf("request is too large (%d bytes, limit %d)", len(payload), MaxRequestBytes)
	}

	id := NewID(time.Now())
	requestPath := filepath.Join(b.Dir, id+RequestSuffix)
	if err := writeAtomically(requestPath, payload); err != nil {
		return nil, err
	}

	responsePath := filepath.Join(b.Dir, id+ResponseSuffix)
	deadline := time.Now().Add(timeout)
	for {
		if response, ok := readResponse(responsePath); ok {
			// 消せなくても応答は取得できている。残骸はプラグイン側が開始時に掃除する。
			_ = os.Remove(responsePath)
			return response, nil
		}
		now := time.Now()
		if now.After(deadline) {
			// 置いたままの要求を取り下げる（あとで読み取られて、誰も待たない応答が残らないように）。
			_ = os.Remove(requestPath)
			switch Open(b.Dir, now).State {
			case StateDown:
				return nil, fmt.Errorf("%w (stopped while waiting for %s)", ErrNotRunning, tool)
			case StateUnresponsive:
				return nil, fmt.Errorf("%w (%s, %s)", ErrUnresponsive, tool, timeout)
			}
			return nil, fmt.Errorf("%w (%s, %s)", ErrTimeout, tool, timeout)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// readResponse は応答を読む。まだ無い・書きかけなら false（もう一度読めばよい）。
func readResponse(path string) (*Response, bool) {
	text, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var response Response
	if json.Unmarshal(text, &response) != nil {
		return nil, false
	}
	return &response, true
}

// writeAtomically は同じディレクトリへ書いてから rename する（読み手に書きかけを見せない）。
func writeAtomically(path string, data []byte) error {
	temp := path + TempSuffix
	if err := os.WriteFile(temp, data, 0o600); err != nil {
		return fmt.Errorf("write request: %w", err)
	}
	if err := os.Rename(temp, path); err != nil {
		_ = os.Remove(temp)
		return fmt.Errorf("publish request: %w", err)
	}
	return nil
}
