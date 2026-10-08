// Package spool は、プラグイン側のブリッジとファイルで受け渡しをする作法（docs/protocol.md）の
// CLI 側の実装である。
//
// 作法の真実は docs/protocol.md で、プラグイン側（C++）とこのパッケージはその対になる。
// どちらかを変えたら仕様書と両方を直し、形を変えたなら ProtocolVersion を上げる。
//
// このパッケージは**排他を持たない**。同時に複数のプロセスが要求を置いても受け渡しは
// 壊れないが、図面に対する操作の順序や占有は呼ぶ側（たとえば MCP のラッパー）の責任である
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
	TempSuffix     = ".tmp"

	// ProtocolVersion は受け渡しの版。要求／応答／生存の印の形を変えたら上げる。
	ProtocolVersion = 1

	// StaleSeconds より古い生存の印は「動いていない」と判定する。プラグイン側は数秒ごとに
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
	Plugin    string  `json:"plugin"`
	Version   string  `json:"version"`
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

// ReadStatus はその場所の生存の印を読む。有効でなければ nil と理由。
//
// 有効とは: 持ち主と権限が安全で、印が読めて、beat が now から StaleSeconds 以内か、
// busy_until が未来であること（長く走る道具の最中、プラグインは印を書き直せない）。
func ReadStatus(dir string, now time.Time) (*Status, string) {
	if reason := checkSafe(dir); reason != "" {
		return nil, reason
	}
	text, err := os.ReadFile(filepath.Join(dir, StatusFile))
	if err != nil {
		return nil, "no status file"
	}
	var status Status
	if err := json.Unmarshal(text, &status); err != nil {
		// 書きかけを読んだか、壊れている。どちらも「ここではない」。
		return nil, "unreadable status file"
	}
	status.Raw = append(json.RawMessage(nil), text...)
	if status.Beat <= 0 {
		return nil, "status has no beat"
	}
	nowSec := float64(now.UnixNano()) / 1e9
	if nowSec-status.Beat > StaleSeconds && (status.BusyUntil <= 0 || nowSec > status.BusyUntil) {
		return nil, "status is stale"
	}
	return &status, ""
}

// Bridge は見つけたスプール 1 つ。
type Bridge struct {
	Dir    string
	Status *Status
}

// Find は候補を順に調べ、有効な生存の印があるところを返す。見つからなければ
// ErrNotRunning（Searched に調べた場所と理由が入る）。
func Find(candidates []string, now time.Time) (*Bridge, []Searched, error) {
	searched := make([]Searched, 0, len(candidates))
	for _, dir := range candidates {
		status, reason := ReadStatus(dir, now)
		if status != nil {
			return &Bridge{Dir: dir, Status: status}, searched, nil
		}
		searched = append(searched, Searched{Dir: dir, Reason: reason})
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
			if status, _ := ReadStatus(b.Dir, now); status == nil {
				return nil, fmt.Errorf("%w (stopped while waiting for %s)", ErrNotRunning, tool)
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
