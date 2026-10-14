package sockproto

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
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
	// 1 バイト届いた・閉じた（EOF）なら、呼ぶ側は待つのをやめている。
	if _, err := conn.Read(extra); !errors.Is(err, os.ErrDeadlineExceeded) {
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

// rawServer は 1 つだけ受け取り、serve に渡す（手順に従わないプラグインを作るため）。
func rawServer(t *testing.T, dir string, serve func(conn net.Conn, r *lines)) {
	t.Helper()
	listener, err := net.Listen("unix", filepath.Join(dir, SocketFile))
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := &lines{conn: conn}
		if line, _ := r.next(time.Now().Add(5 * time.Second)); line != nil {
			serve(conn, r)
		}
	}()
}

// readCancel は呼ぶ側が待つのをやめた 1 バイトを待つ。
func readCancel(conn net.Conn) bool {
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, _ := conn.Read(make([]byte, 1))
	return n == 1
}

// 受け取りの行の前にプラグインが閉じたら（Vectorworks が終わったなど）、実行されていない。
func TestCallClosedWithoutTaken(t *testing.T) {
	dir := t.TempDir()
	rawServer(t, dir, func(net.Conn, *lines) {})
	_, err := Call(dir, []byte(`{"tool":"ping"}`), 5*time.Second, time.Second)
	if !errors.Is(err, spool.ErrNotRunning) {
		t.Fatalf("err = %v, want ErrNotRunning", err)
	}
}

// 受け取りの行のあとにプラグインが閉じたら、実行されたかもしれない。
func TestCallClosedAfterTaken(t *testing.T) {
	dir := t.TempDir()
	rawServer(t, dir, func(conn net.Conn, _ *lines) {
		_, _ = conn.Write(append(TakenLine, '\n'))
	})
	_, err := Call(dir, []byte(`{"tool":"ping"}`), 5*time.Second, time.Second)
	if !errors.Is(err, spool.ErrNoResponse) {
		t.Fatalf("err = %v, want ErrNoResponse", err)
	}
}

// 待つのをやめた 1 バイトと入れ違いに受け取りの行が届いたら、猶予の間に応答を受け取る。
func TestCallTakenAfterCancel(t *testing.T) {
	dir := t.TempDir()
	rawServer(t, dir, func(conn net.Conn, _ *lines) {
		if readCancel(conn) {
			_, _ = conn.Write([]byte(string(TakenLine) + "\n" + `{"ok":true,"result":{}}` + "\n"))
		}
	})
	response, err := Call(dir, []byte(`{"tool":"ping"}`), 100*time.Millisecond, 5*time.Second)
	if err != nil || !response.OK {
		t.Fatalf("Call = %+v, %v", response, err)
	}
}

// 待つのをやめた 1 バイトを、受け取りの行より前にプラグインが見たら no_wait で応える。
func TestCallNoWaitAfterCancel(t *testing.T) {
	dir := t.TempDir()
	rawServer(t, dir, func(conn net.Conn, _ *lines) {
		if readCancel(conn) {
			_, _ = conn.Write([]byte(`{"ok":false,"code":"no_wait","error":"cancelled"}` + "\n"))
		}
	})
	_, err := Call(dir, []byte(`{"tool":"ping"}`), 100*time.Millisecond, 5*time.Second)
	if !errors.Is(err, spool.ErrNoWait) {
		t.Fatalf("err = %v, want ErrNoWait", err)
	}
}

// 受け付けが見送られていると、ソケットの受け取り口を超える要求は送り切れずに締切が来る。改行まで
// 届かないので実行されない。
func TestCallRequestNotSent(t *testing.T) {
	dir := t.TempDir()
	hold := make(chan struct{})
	f := startFake(t, dir, hold, true)
	request := []byte(`{"tool":"ping","args":{"pad":"` + strings.Repeat("x", 8<<20) + `"}}`)
	_, err := Call(dir, request, 200*time.Millisecond, time.Second)
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

func TestDecode(t *testing.T) {
	tests := []struct {
		line string
		ok   bool
		err  error
	}{
		{`{"ok":true,"result":{}}`, true, nil},
		{`{"ok":false,"code":"internal","error":"x"}`, false, nil},
		{`{"ok":false,"code":"no_wait","error":"x"}`, false, spool.ErrNoWait},
		{`{"ok":false,"code":"busy","error":"x"}`, false, spool.ErrBusy},
		{`{"ok":false,"code":"no_session","error":"x"}`, false, spool.ErrNoSession},
		{`{"ok":false,"code":"nope","error":"x"}`, false, spool.ErrMalformedResponse},
		{`{"ok":`, false, spool.ErrMalformedResponse},
	}
	for _, test := range tests {
		response, err := decode([]byte(test.line))
		if test.err != nil {
			if !errors.Is(err, test.err) {
				t.Errorf("decode(%s) err = %v, want %v", test.line, err, test.err)
			}
			continue
		}
		if err != nil || response.OK != test.ok {
			t.Errorf("decode(%s) = %+v, %v", test.line, response, err)
		}
	}
}
