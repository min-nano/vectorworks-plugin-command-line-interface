package spool_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/fakeplugin"
	. "github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/spool"
)

// 占有の中の呼び出しは実行され、ほかの呼び出しは busy で断られる。tools と ping は誰にでも答える。
func TestSessionAdmitsOnlyItsCalls(t *testing.T) {
	dir := fakeplugin.NewDir(t)
	fakeplugin.Start(t, dir, echo)
	session, err := HoldSession(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Release()
	if !Open(dir).Session {
		t.Fatal("the bridge should be occupied")
	}
	if response, err := Open(dir).Call("layers", nil, session.ID, 5*time.Second); err != nil || !response.OK {
		t.Fatalf("the session's call should run: %+v %v", response, err)
	}
	for _, id := range []string{"", "other"} {
		if _, err := Open(dir).Call("layers", nil, id, 5*time.Second); !errors.Is(err, ErrBusy) {
			t.Fatalf("session %q: want ErrBusy, got %v", id, err)
		}
	}
	if response, err := Open(dir).Call("ping", nil, "", 5*time.Second); err != nil || !response.OK {
		t.Fatalf("ping should be answered: %+v %v", response, err)
	}
}

// 占有が終わったら、印の無い呼び出しは実行され、終わった占有の印の呼び出しは no_session で断られる。
func TestSessionEnded(t *testing.T) {
	dir := fakeplugin.NewDir(t)
	fakeplugin.Start(t, dir, echo)
	session, err := HoldSession(dir)
	if err != nil {
		t.Fatal(err)
	}
	session.Release()
	if Open(dir).Session {
		t.Fatal("the bridge should not be occupied")
	}
	if _, err := os.Stat(filepath.Join(dir, SessionFile)); !os.IsNotExist(err) {
		t.Fatal("the session file should be removed")
	}
	if _, err := Open(dir).Call("layers", nil, session.ID, 5*time.Second); !errors.Is(err, ErrNoSession) {
		t.Fatalf("want ErrNoSession, got %v", err)
	}
	if response, err := Open(dir).Call("layers", nil, "", 5*time.Second); err != nil || !response.OK {
		t.Fatalf("%+v %v", response, err)
	}
}

// 占有は 1 つだけ。2 つ目は ErrBusy。
func TestSessionIsExclusive(t *testing.T) {
	SetSessionGrace(t, 200*time.Millisecond)
	dir := fakeplugin.NewDir(t)
	first, err := HoldSession(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := HoldSession(dir); !errors.Is(err, ErrBusy) {
		t.Fatalf("want ErrBusy, got %v", err)
	}
	first.Release()
	second, err := HoldSession(dir)
	if err != nil {
		t.Fatalf("the bridge should be free after the release: %v", err)
	}
	second.Release()
}

// スプールが無くても占有できる（Vectorworks を起動する前から）。
func TestSessionCreatesSpool(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "spool")
	session, err := HoldSession(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Release()
	var published struct {
		Session string `json:"session"`
	}
	text, err := os.ReadFile(filepath.Join(dir, SessionFile))
	if err != nil || json.Unmarshal(text, &published) != nil || published.Session != session.ID || !ValidID(session.ID) {
		t.Fatalf("unexpected session file %q (%v) for %s", text, err, session.ID)
	}
}
