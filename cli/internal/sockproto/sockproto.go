// Package sockproto は試作である（prototypes/socket/README.md）。スプールのファイルの代わりに
// AF_UNIX のソケットで要求を送る、呼ぶ側の手順を確かめる。マージしない。
//
// 手順（プラグイン側は prototypes/socket/src/SocketServe.cpp）:
//
//  1. bridge.sock につなぎ、要求の JSON を 1 行（改行で終わる）で送る。
//  2. プラグインは要求を読み終えたら、実行する前に受け取りの行（{"taken":true}）を送る。送れな
//     ければ（呼ぶ側が死んだ・閉じた）実行しない。
//  3. 応答の JSON が 1 行届き、プラグインが閉じる。
//
// 待つのをやめるときは 1 バイト送ってから、受け取りの行が届いているかを見る。プラグインは受け取りの
// 行を送った「あと」にその 1 バイトを確かめるので、届いていなければ実行されないと分かる。
package sockproto

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/spool"
)

// SocketFile はスプールの中のソケットの名前。
const SocketFile = "bridge.sock"

// TakenLine はプラグインが実行する前に送る受け取りの行（改行を除く）。
var TakenLine = []byte(`{"taken":true}`)

// cancelPeek は、待つのをやめる 1 バイトを送ったあと、既に届いている受け取りの行を読むための
// 時間。同じ計算機の中のソケットは送った時点で相手の受け取り口に入るので、届いていれば待たずに
// 読める。テストが縮めない。
const cancelPeek = 50 * time.Millisecond

// Call は道具を 1 つ呼び、応答を待つ。request は改行を含まない JSON。
//
//   - つなげない: spool.ErrNotRunning
//   - 受け取りの行が届く前に締切が来た: 1 バイト送ってやめ、spool.ErrTimeout（実行されない）
//   - 受け取りの行が届いたあとに締切が来た: grace だけ応答を待ち、届かなければ
//     spool.ErrNoResponse（実行されたかもしれない）
func Call(dir string, request []byte, timeout, grace time.Duration) (*spool.Response, error) {
	deadline := time.Now().Add(timeout)
	conn, err := net.DialTimeout("unix", dir+string(os.PathSeparator)+SocketFile, timeout)
	if err != nil {
		return nil, fmt.Errorf("%w (%v)", spool.ErrNotRunning, err)
	}
	defer conn.Close()
	notRun := fmt.Errorf("%w (%s)", spool.ErrTimeout, timeout)

	// 受け付けが見送られていると、大きな要求は送り切る前に締切が来る。改行まで届かない要求は
	// 実行されない。
	_ = conn.SetWriteDeadline(deadline)
	if _, err := conn.Write(append(request, '\n')); err != nil {
		return nil, notRun
	}

	r := &lines{conn: conn}
	taken := false
	for {
		line, err := r.next(deadline)
		if err != nil {
			if taken {
				return nil, fmt.Errorf("%w (the bridge closed after taking it)", spool.ErrNoResponse)
			}
			return nil, fmt.Errorf("%w (the bridge closed without taking it)", spool.ErrNotRunning)
		}
		if line == nil {
			break
		}
		if !taken && bytes.Equal(line, TakenLine) {
			taken = true
			continue
		}
		return decode(line)
	}

	if !taken {
		// 待つのをやめる。送れなければプラグインは居ないので、実行されない。
		_ = conn.SetWriteDeadline(time.Now().Add(cancelPeek))
		if _, err := conn.Write([]byte{'c'}); err != nil {
			return nil, notRun
		}
		line, _ := r.next(time.Now().Add(cancelPeek))
		if line == nil {
			return nil, notRun
		}
		if !bytes.Equal(line, TakenLine) {
			return decode(line)
		}
	}
	if line, _ := r.next(time.Now().Add(grace)); line != nil {
		return decode(line)
	}
	return nil, fmt.Errorf("%w (%s; it may have run)", spool.ErrNoResponse, timeout)
}

// decode は応答の行を読む。code の扱いは spool.Bridge.Call と同じ。
func decode(line []byte) (*spool.Response, error) {
	var response spool.Response
	if err := json.Unmarshal(line, &response); err != nil {
		return nil, fmt.Errorf("%w (%v)", spool.ErrMalformedResponse, err)
	}
	if response.OK {
		return &response, nil
	}
	switch response.Code {
	case spool.CodeNoWait:
		return nil, spool.ErrNoWait
	case spool.CodeBusy:
		return nil, spool.ErrBusy
	case spool.CodeNoSession:
		return nil, spool.ErrNoSession
	case spool.CodeInvalidRequest, spool.CodeUnknownTool, spool.CodeInvalidArgs, spool.CodeInternal:
		return &response, nil
	default:
		return nil, fmt.Errorf("%w (unknown code %q)", spool.ErrMalformedResponse, response.Code)
	}
}

// lines は接続から行を読む。締切で途切れた行の続きも持つ。
type lines struct {
	conn    net.Conn
	pending []byte
}

// next は until までに届いた 1 行を返す（改行を除く）。届かなければ nil, nil。相手が閉じたら
// エラー。
func (l *lines) next(until time.Time) ([]byte, error) {
	buffer := make([]byte, 64<<10)
	for {
		if i := bytes.IndexByte(l.pending, '\n'); i >= 0 {
			line := l.pending[:i]
			l.pending = l.pending[i+1:]
			return line, nil
		}
		_ = l.conn.SetReadDeadline(until)
		n, err := l.conn.Read(buffer)
		l.pending = append(l.pending, buffer[:n]...)
		if err == nil {
			continue
		}
		if bytes.IndexByte(l.pending, '\n') >= 0 {
			continue
		}
		if errors.Is(err, os.ErrDeadlineExceeded) {
			return nil, nil
		}
		return nil, err
	}
}
