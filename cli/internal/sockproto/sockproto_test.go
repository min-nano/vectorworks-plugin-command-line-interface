package sockproto

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/spool"
)

// fake はプラグイン側の手順（prototypes/socket/src/SocketServe.cpp）を Go で写した偽物。
type fake struct {
	calls   atomic.Int32
	handled chan struct{}
}

// startFake は dir で待ち受ける。hold が閉じられるまで受け取らない（受け付けが見送られている）。
// respond が偽なら、受け取りの行を送ったあと応答しない。
func startFake(t *testing.T, dir string, hold <-chan struct{}, respond bool) *fake {
	t.Helper()
	listener, err := net.Listen("unix", filepath.Join(dir, SocketFile))
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	done := make(chan struct{})
	t.Cleanup(func() {
		close(done)
		listener.Close()
	})
	f := &fake{handled: make(chan struct{}, 16)}
	go func() {
		if hold != nil {
			<-hold
		}
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			f.handle(conn, respond, done)
			f.handled <- struct{}{}
		}
	}()
	return f
}

func (f *fake) handle(conn net.Conn, respond bool, done <-chan struct{}) {
	defer conn.Close()
	r := &lines{conn: conn}
	line, _ := r.next(time.Now().Add(time.Second))
	if line == nil {
		return
	}
	if len(r.pending) > 0 {
		_, _ = conn.Write([]byte(`{"ok":false,"code":"no_wait","error":"cancelled"}` + "\n"))
		return
	}
	if _, err := conn.Write(append(TakenLine, '\n')); err != nil {
		return
	}
	extra := make([]byte, 1)
	_ = conn.SetReadDeadline(time.Now().Add(time.Millisecond))
	if n, _ := conn.Read(extra); n > 0 {
		_, _ = conn.Write([]byte(`{"ok":false,"code":"no_wait","error":"cancelled"}` + "\n"))
		return
	}
	f.calls.Add(1)
	if !respond {
		<-done
		return
	}
	_, _ = conn.Write([]byte(`{"ok":true,"result":{"pong":true}}` + "\n"))
}

func TestCallRoundTrip(t *testing.T) {
	dir := t.TempDir()
	f := startFake(t, dir, nil, true)
	response, err := Call(dir, []byte(`{"tool":"ping"}`), 5*time.Second, time.Second)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !response.OK || string(response.Result) != `{"pong":true}` {
		t.Fatalf("response = %+v", response)
	}
	if f.calls.Load() != 1 {
		t.Fatalf("calls = %d", f.calls.Load())
	}
}

func TestCallNotRunning(t *testing.T) {
	_, err := Call(t.TempDir(), []byte(`{"tool":"ping"}`), time.Second, time.Second)
	if !errors.Is(err, spool.ErrNotRunning) {
		t.Fatalf("err = %v, want ErrNotRunning", err)
	}
}

// 受け付けが見送られている間に締切が来たら、あとで受け付けが戻っても実行されない。
func TestCallDeferredIsNotRun(t *testing.T) {
	dir := t.TempDir()
	hold := make(chan struct{})
	f := startFake(t, dir, hold, true)
	_, err := Call(dir, []byte(`{"tool":"ping"}`), 200*time.Millisecond, time.Second)
	if !errors.Is(err, spool.ErrTimeout) {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}
	close(hold)
	select {
	case <-f.handled:
	case <-time.After(5 * time.Second):
		t.Fatal("the fake did not handle the connection")
	}
	if f.calls.Load() != 0 {
		t.Fatalf("calls = %d, want 0", f.calls.Load())
	}
}

// 受け取りの行が届いたあとに応答が来なければ、実行されたかもしれない。
func TestCallTakenButSilent(t *testing.T) {
	dir := t.TempDir()
	f := startFake(t, dir, nil, false)
	_, err := Call(dir, []byte(`{"tool":"ping"}`), 200*time.Millisecond, 100*time.Millisecond)
	if !errors.Is(err, spool.ErrNoResponse) {
		t.Fatalf("err = %v, want ErrNoResponse", err)
	}
	if f.calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", f.calls.Load())
	}
}

// パスに ASCII でない文字があっても、その名前のソケットができ、つなげる（Windows が sun_path を
// UTF-8 で読むか）。
func TestCallNonASCIIPath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "日")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	startFake(t, dir, nil, true)
	if _, err := os.Stat(filepath.Join(dir, SocketFile)); err != nil {
		t.Fatalf("stat: %v", err)
	}
	if _, err := Call(dir, []byte(`{"tool":"ping"}`), 5*time.Second, time.Second); err != nil {
		t.Fatalf("Call: %v", err)
	}
}

// この計算機のスプールのソケットのパスの長さを CI のログに出す。
func TestSocketPathLength(t *testing.T) {
	dir, err := spool.DefaultDir()
	if err != nil {
		t.Skip(err)
	}
	path := filepath.Join(dir, SocketFile)
	t.Logf("spool socket path is %d bytes: %s", len(path), path)
}
