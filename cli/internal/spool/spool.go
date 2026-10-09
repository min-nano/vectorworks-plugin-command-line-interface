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
	LockFile       = "bridge.lock"
	TempSuffix     = ".tmp"

	// ProtocolVersion は受け渡しの版。スプールに置くものの形を変えたら上げる。表示のためだけで、
	// 実行時に照合はしない（CLI とプラグインは同じ zip から同時に入り、更新は Vectorworks の
	// 終了後にしか行わないので、両側は常に同じビルドである。docs/protocol.md「版」）。
	ProtocolVersion = 3

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

// Response は応答 1 件。OK が false のときだけ Error に理由が入る。id はファイル名が持つ。
type Response struct {
	OK     bool            `json:"ok"`
	Error  string          `json:"error,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
}

// ErrNotRunning はブリッジが見つからない（Vectorworks が起動していない・プラグインが
// 読み込まれていない・場所が食い違っている）。
var ErrNotRunning = errors.New("bridge is not running")

// ErrTimeout は、Vectorworks は動いている（ロックが掴まれている）が締切までに応答が
// 無かった。受け付けが見送られている（モーダルダイアログ・undo の記録の最中）ことが多い。
var ErrTimeout = errors.New("timed out waiting for the response")

// Bridge はスプール 1 つと、その状態。
type Bridge struct {
	Dir     string
	Running bool   // プラグインがロックを掴んでいる（Vectorworks が動いている）
	Reason  string // 動いていない理由
}

// Open はその場所のブリッジの状態を判定する（docs/protocol.md「生存の判定」）。
//
// 生死はロックだけで決まる。受け付けが見送られている間も、ロックは掴まれたまま。
func Open(dir string) *Bridge {
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return &Bridge{Dir: dir, Reason: "not found"}
	}
	if !lockHeld(filepath.Join(dir, LockFile)) {
		return &Bridge{Dir: dir, Reason: "not running"}
	}
	return &Bridge{Dir: dir, Running: true}
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
// 受け付けが見送られていても要求を置いて timeout まで待つ（受け付けが戻れば処理される）。
// 待つのを諦めたときは置いた要求を取り下げ、理由をその時点のロックで ErrNotRunning /
// ErrTimeout に分ける（呼ぶ側が起動し直すべきか、待てばよいかを判定できるように）。
func (b *Bridge) Call(tool string, args json.RawMessage, timeout time.Duration) (*Response, error) {
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
		if time.Now().After(deadline) {
			// 置いたままの要求を取り下げる（あとで読み取られて、誰も待たない応答が残らないように）。
			_ = os.Remove(requestPath)
			if !Open(b.Dir).Running {
				return nil, fmt.Errorf("%w (stopped while waiting for %s)", ErrNotRunning, tool)
			}
			return nil, fmt.Errorf("%w (%s, %s; a dialog may be open)", ErrTimeout, tool, timeout)
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
