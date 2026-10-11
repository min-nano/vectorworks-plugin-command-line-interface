package spool_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/fakeplugin"
	. "github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/spool"
)

// busy と no_session は実行していないので、道具の失敗ではなく ErrBusy / ErrNoSession になる。
func TestCallRefusedBySession(t *testing.T) {
	dir := fakeplugin.NewDir(t)
	fakeplugin.Start(t, dir, echo)
	bridge := Open(dir)
	response, err := bridge.Call(ToolSessionStart, nil, "", 5*time.Second)
	var started struct {
		Session string `json:"session"`
	}
	if err != nil || !response.OK || json.Unmarshal(response.Result, &started) != nil || !ValidID(started.Session) {
		t.Fatalf("%+v %v", response, err)
	}
	if _, err := bridge.Call("layers", nil, "", 5*time.Second); !errors.Is(err, ErrBusy) {
		t.Fatalf("want ErrBusy, got %v", err)
	}
	if response, err := bridge.Call(ToolSessionEnd, nil, started.Session, 5*time.Second); err != nil || !response.OK {
		t.Fatalf("%+v %v", response, err)
	}
	if _, err := bridge.Call("layers", nil, started.Session, 5*time.Second); !errors.Is(err, ErrNoSession) {
		t.Fatalf("want ErrNoSession, got %v", err)
	}
}

// Vectorworks を起動し直すと占有は消え、古い印は ErrNoSession になる。
func TestSessionEndsWithRestart(t *testing.T) {
	dir := fakeplugin.NewDir(t)
	stop := fakeplugin.Start(t, dir, echo)
	response, err := Open(dir).Call(ToolSessionStart, nil, "", 5*time.Second)
	if err != nil || !response.OK {
		t.Fatalf("%+v %v", response, err)
	}
	var started struct {
		Session string `json:"session"`
	}
	_ = json.Unmarshal(response.Result, &started)
	stop()
	fakeplugin.Start(t, dir, echo)
	if _, err := Open(dir).Call("layers", nil, started.Session, 5*time.Second); !errors.Is(err, ErrNoSession) {
		t.Fatalf("want ErrNoSession, got %v", err)
	}
	if response, err := Open(dir).Call("layers", nil, "", 5*time.Second); err != nil || !response.OK {
		t.Fatalf("%+v %v", response, err)
	}
}
