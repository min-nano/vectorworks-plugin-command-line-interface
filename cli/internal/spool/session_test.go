package spool_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/fakeplugin"
	. "github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/spool"
)

// writeKinds は占有の中でのみ呼び出せるツール（種類 write）を 1 つ有するツールの一覧。
var writeKinds = map[string]string{"edit": "write"}

// occupied と no_session は実行していないので、ツールの失敗ではなく ErrOccupied / ErrNoSession に
// なる。種類 read のツールは占有と照合しない。
func TestCallRefusedBySession(t *testing.T) {
	dir := fakeplugin.NewDir(t)
	fakeplugin.StartWithKinds(t, dir, writeKinds, echo)
	bridge := Open(dir)
	response, err := bridge.Call(ToolSessionStart, nil, "", 5*time.Second)
	var started struct {
		Session string `json:"session"`
	}
	if err != nil || !response.OK || json.Unmarshal(response.Result, &started) != nil || !ValidID(started.Session) {
		t.Fatalf("%+v %v", response, err)
	}
	if _, err := bridge.Call("edit", nil, "", 5*time.Second); !errors.Is(err, ErrOccupied) {
		t.Fatalf("want ErrOccupied, got %v", err)
	}
	// ツールの種類を渡しても、quit は種類 app のまま（占有の中でのみ呼び出せる）。
	if _, err := bridge.Call("quit", nil, "", 5*time.Second); !errors.Is(err, ErrOccupied) {
		t.Fatalf("want ErrOccupied for quit, got %v", err)
	}
	if response, err := bridge.Call("layers", nil, "", 5*time.Second); err != nil || !response.OK {
		t.Fatalf("%+v %v", response, err)
	}
	if response, err := bridge.Call(ToolSessionEnd, nil, started.Session, 5*time.Second); err != nil || !response.OK {
		t.Fatalf("%+v %v", response, err)
	}
	if _, err := bridge.Call("edit", nil, started.Session, 5*time.Second); !errors.Is(err, ErrNoSession) {
		t.Fatalf("want ErrNoSession, got %v", err)
	}
	if _, err := bridge.Call("edit", nil, "", 5*time.Second); !errors.Is(err, ErrNoSession) {
		t.Fatalf("want ErrNoSession, got %v", err)
	}
}

// busy は実行していないので、ツールの失敗ではなく ErrBusy になる。理由（プラグインの error）を
// エラーの文言に含める（終了の依頼・他の要求の処理を呼び出し元が判別できるように）。
func TestCallRefusedAsBusy(t *testing.T) {
	for _, reason := range []string{"Vectorworks is quitting", ""} {
		dir := fakeplugin.NewDir(t)
		fakeplugin.Start(t, dir, func(string, json.RawMessage) Response {
			return Response{Code: CodeBusy, Error: reason}
		})
		_, err := Open(dir).Call("edit", nil, "", 5*time.Second)
		if !errors.Is(err, ErrBusy) || !strings.Contains(err.Error(), reason) {
			t.Fatalf("want ErrBusy with the reason %q, got %v", reason, err)
		}
		// 理由が空ならば、区切りのみを残さない。
		if strings.Contains(err.Error(), ": )") {
			t.Fatalf("an empty reason left a separator: %v", err)
		}
	}
}

// Vectorworks を再起動すると占有は消失し、古いセッション ID は ErrNoSession になる。
func TestSessionEndsWithRestart(t *testing.T) {
	dir := fakeplugin.NewDir(t)
	stop := fakeplugin.StartWithKinds(t, dir, writeKinds, echo)
	response, err := Open(dir).Call(ToolSessionStart, nil, "", 5*time.Second)
	if err != nil || !response.OK {
		t.Fatalf("%+v %v", response, err)
	}
	var started struct {
		Session string `json:"session"`
	}
	_ = json.Unmarshal(response.Result, &started)
	stop()
	fakeplugin.StartWithKinds(t, dir, writeKinds, echo)
	if _, err := Open(dir).Call("edit", nil, started.Session, 5*time.Second); !errors.Is(err, ErrNoSession) {
		t.Fatalf("want ErrNoSession, got %v", err)
	}
	if response, err := Open(dir).Call(ToolSessionStart, nil, "", 5*time.Second); err != nil || !response.OK {
		t.Fatalf("%+v %v", response, err)
	}
}
