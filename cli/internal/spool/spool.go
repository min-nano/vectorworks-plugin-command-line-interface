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
	"strings"
	"time"
)

// スプールに置くファイルの綴り。プラグイン側と対。
const (
	RequestSuffix  = ".req.json"
	ResponseSuffix = ".res.json"
	StatusFile     = "bridge.json"
	TempSuffix     = ".tmp"

	// ProtocolVersion は受け渡しの版。要求／応答／生存の印の形を変えたら上げる。
	ProtocolVersion = 1

	// StaleSeconds より古い生存の印は「応えていない」と判定する。プラグイン側は数秒ごとに
	// 書き直す。古いときに動いているかどうかは pid で分ける（State）。
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
	Plugin    string  `json:"plugin"`
	Channel   string  `json:"channel"`
	Version   string  `json:"version"`
	Branch    string  `json:"branch"`
	Protocol  int     `json:"protocol"`
	Beat      float64 `json:"beat"`
	PID       int     `json:"pid"`
	Busy      string  `json:"busy,omitempty"`
	BusyID    string  `json:"busy_id,omitempty"`
	BusyUntil float64 `json:"busy_until,omitempty"`

	Raw json.RawMessage `json:"-"`
}

// Response は応答 1 件。OK が false のときだけ Error に理由が入る。
type Response struct {
	ID     string          `json:"id"`
	OK     bool            `json:"ok"`
	Error  string          `json:"error,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
}

// ErrNotRunning はブリッジが見つからない（Vectorworks が起動していない・プラグインが
// 読み込まれていない・場所が食い違っている）。
var ErrNotRunning = errors.New("bridge is not running")

// ErrUnresponsive は、Vectorworks は動いているがブリッジが応えない（モーダルダイアログ・
// undo の記録・長い処理の最中で、プラグインが受け付けを見送っている）。
var ErrUnresponsive = errors.New("vectorworks is running but the bridge is not responding")

// State はブリッジの状態（docs/protocol.md「生存の判定」）。
type State string

const (
	// StateLive は印が新しい（または長く走る道具の締切の内）。要求に応える。
	StateLive State = "live"
	// StateUnresponsive は印が古いが、印の pid の Vectorworks は動いている。プラグインが受け付けを
	// 見送っている間（mac ではモーダルの最中はタイマーも刻まない）は印が書き直されないので、
	// 古いことだけで「止まった」とはみなさない。置いた要求は、受け付けが戻れば処理される。
	StateUnresponsive State = "unresponsive"
	// StateDown は印が無い・読めない・古くて pid のプロセスも無い。
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

// ReadStatus はその場所の生存の印を読み、状態を判定する。StateDown なら nil と理由。
//
// 印が使えるとは: 持ち主と権限が安全で、印が読めること。そのうえで、beat が now から
// StaleSeconds 以内か busy_until が未来なら StateLive（長く走る道具の最中、プラグインは
// 印を書き直せない）。そうでなくても pid の Vectorworks が動いていれば StateUnresponsive。
func ReadStatus(dir string, now time.Time) (*Status, State, string) {
	if reason := checkSafe(dir); reason != "" {
		return nil, StateDown, reason
	}
	text, err := os.ReadFile(filepath.Join(dir, StatusFile))
	if err != nil {
		return nil, StateDown, "no status file"
	}
	var status Status
	if err := json.Unmarshal(text, &status); err != nil {
		// 書きかけを読んだか、壊れている。どちらも「ここではない」。
		return nil, StateDown, "unreadable status file"
	}
	status.Raw = append(json.RawMessage(nil), text...)
	if status.Beat <= 0 {
		return nil, StateDown, "status has no beat"
	}
	nowSec := float64(now.UnixNano()) / 1e9
	if nowSec-status.Beat <= StaleSeconds || (status.BusyUntil > 0 && nowSec <= status.BusyUntil) {
		return &status, StateLive, ""
	}
	if status.PID > 0 && ProcessRunning(status.PID) {
		return &status, StateUnresponsive, ""
	}
	return nil, StateDown, "status is stale and the process is gone"
}

// ProcessRunning は pid のプロセスが Vectorworks として動いているか。テストで差し替える。
var ProcessRunning = vectorworksRunning

// vectorworksRunning は pid のプロセスがあり、実行ファイルの名前に "vectorworks" を含むか。
//
// 名前も確かめるのは、異常終了で残った印の pid が別のプロセスに再利用されたとき、
// いつまでも「応えない」と判定し続けないため。名前が取れないとき（権限など）は、プロセスが
// あることだけで動いているとみなす（止まったと誤るほうが呼ぶ側の誤った回復を招く）。
func vectorworksRunning(pid int) bool {
	image, exists := processImage(pid)
	if !exists {
		return false
	}
	return image == "" || strings.Contains(strings.ToLower(image), "vectorworks")
}

// Bridge は見つけたスプール 1 つ。State は StateLive か StateUnresponsive。
type Bridge struct {
	Dir    string
	Status *Status
	State  State
}

// Find は候補を順に調べ、StateLive のところを返す。無ければ最初の StateUnresponsive の
// ところを返す。どちらも無ければ ErrNotRunning（Searched に調べた場所と理由が入る）。
func Find(candidates []string, now time.Time) (*Bridge, []Searched, error) {
	searched := make([]Searched, 0, len(candidates))
	var fallback *Bridge
	fallbackAt := -1
	for _, dir := range candidates {
		status, state, reason := ReadStatus(dir, now)
		switch state {
		case StateLive:
			return &Bridge{Dir: dir, Status: status, State: state}, searched, nil
		case StateUnresponsive:
			if fallback == nil {
				fallback = &Bridge{Dir: dir, Status: status, State: state}
				fallbackAt = len(searched)
			}
			reason = "unresponsive"
		}
		searched = append(searched, Searched{Dir: dir, Reason: reason})
	}
	if fallback != nil {
		// 返す場所は「使わなかった場所」から外す。
		return fallback, append(searched[:fallbackAt], searched[fallbackAt+1:]...), nil
	}
	return nil, searched, ErrNotRunning
}

// Searched は調べた場所 1 つと、使わなかった理由。
type Searched struct {
	Dir    string `json:"dir"`
	Reason string `json:"reason"`
}

// CheckProtocol は版が一致するか。
func (b *Bridge) CheckProtocol() error {
	if b.Status.Protocol != ProtocolVersion {
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
// timeout を過ぎても、生存の印の busy_id がこの要求で busy_until が未来なら待ち続ける
// （長く走る道具は、走り出す前にいつまでかかりうるかを書く）。待つのを諦めたときは
// 置いた要求を取り下げる。
//
// ブリッジが StateUnresponsive でも要求を置いて timeout まで待つ（受け付けが戻れば処理
// される）。諦めたときの理由は、その時点の状態で ErrNotRunning / ErrUnresponsive /
// ErrTimeout に分ける（呼ぶ側が起動し直すべきか、待てばよいかを判定できるように）。
func (b *Bridge) Call(tool string, args json.RawMessage, timeout time.Duration) (*Response, error) {
	if err := b.CheckProtocol(); err != nil {
		return nil, err
	}
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	id := NewID(time.Now())
	payload, err := json.Marshal(struct {
		ID   string          `json:"id"`
		Tool string          `json:"tool"`
		Args json.RawMessage `json:"args"`
	}{id, tool, args})
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	if len(payload) > MaxRequestBytes {
		return nil, fmt.Errorf("request is too large (%d bytes, limit %d)", len(payload), MaxRequestBytes)
	}

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
		if now.After(deadline) && !b.busyWith(id, now) {
			// 置いたままの要求を取り下げる（あとで読み取られて、誰も待たない応答が残らないように）。
			_ = os.Remove(requestPath)
			switch _, state, _ := ReadStatus(b.Dir, now); state {
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

// busyWith は、プラグインがいまこの要求を処理中で、締切がまだ来ていないか。
func (b *Bridge) busyWith(id string, now time.Time) bool {
	text, err := os.ReadFile(filepath.Join(b.Dir, StatusFile))
	if err != nil {
		return false
	}
	var status Status
	if json.Unmarshal(text, &status) != nil {
		return false
	}
	return status.BusyID == id && float64(now.UnixNano())/1e9 <= status.BusyUntil
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
