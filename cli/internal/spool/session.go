package spool

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// 占有に使うファイル（docs/protocol.md「占有」）。プラグイン側と対。
const (
	SessionLock = "session.lock" // 占有している呼ぶ側が掴み続ける
	SessionFile = "session.json" // 占有している呼ぶ側の印。session.lock が掴まれている間だけ意味を持つ
)

// ErrBusy は、ほかの呼ぶ側がブリッジを占有している（要求は実行されていない）。
var ErrBusy = errors.New("the bridge is occupied by another session")

// ErrNoSession は、要求に載せた占有の印が、いま占有している呼ぶ側のものではない。占有が
// 終わったあとに送った（包んだ vw2026 session が先に終わった）ときに起きる。要求は実行されて
// いない。
var ErrNoSession = errors.New("the session has ended")

// sessionGrace は、session.lock を掴めないときに掴み直す間。プラグインは要求ごとに
// session.lock を掴めるか一瞬だけ試すので、その間に重なっても占有を断らないように。
var sessionGrace = time.Second

// Session は占有 1 つ。Release で終える。
type Session struct {
	ID      string
	dir     string
	release func()
}

// HoldSession はブリッジを占有する（docs/protocol.md「占有」）。session.lock を掴み、印を
// session.json に公開する。ほかの呼ぶ側が占有していれば ErrBusy。
//
// スプールが無ければ作る（Vectorworks を起動する前から占有できるように）。占有は呼ぶ側が
// 掴むので、Vectorworks を起動し直しても続く。
func HoldSession(dir string) (*Session, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create the spool: %w", err)
	}
	deadline := time.Now().Add(sessionGrace)
	var release func()
	for {
		unlock, err := holdLock(filepath.Join(dir, SessionLock))
		if err == nil {
			release = unlock
			break
		}
		if !lockBusy(err) {
			return nil, fmt.Errorf("hold %s: %w", SessionLock, err)
		}
		if time.Now().After(deadline) {
			return nil, ErrBusy
		}
		time.Sleep(50 * time.Millisecond)
	}
	id := NewID(time.Now())
	data, _ := json.Marshal(struct {
		Session string `json:"session"`
	}{id})
	if err := writeAtomically(filepath.Join(dir, SessionFile), data); err != nil {
		release()
		return nil, err
	}
	return &Session{ID: id, dir: dir, release: release}, nil
}

// Release は占有を終える。印を消してから session.lock を放す（放したあとに古い印が
// 読まれないように）。
func (s *Session) Release() {
	_ = os.Remove(filepath.Join(s.dir, SessionFile))
	s.release()
}

// SessionHeld は、いずれかの呼ぶ側がブリッジを占有しているか。
func SessionHeld(dir string) bool {
	return lockHeld(filepath.Join(dir, SessionLock))
}
