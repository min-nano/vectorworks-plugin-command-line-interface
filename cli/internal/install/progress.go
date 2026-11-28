package install

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/edition"
)

// 進捗を書き込む間隔（docs/plugin/install-and-update.md「処理の流れ」）。端末では同じ行を
// 書き直すため短く、端末でなければ（ログに残る）1 行ずつ追加するため長くする。テストが縮小する。
var (
	progressEvery         = 10 * time.Second
	progressEveryTerminal = 200 * time.Millisecond
)

// progress は、取得した量を計数して、進捗を w（標準エラー）に書き込む io.Writer。
// 標準出力は JSON の 1 行のみとするため、ここには書き込まない。
type progress struct {
	w        io.Writer
	terminal bool
	// 全体の大きさ（不明ならば -1）
	total int64
	done  int64
	last  time.Time
	// 端末で最後に書き込んだ行の長さ（書き直す際に、短くなった分を空白で消去する）
	width int
}

// startProgress は取得の開始の行を書き込み、計数する progress を返す。
func (o *Options) startProgress(ref string, total int64) *progress {
	p := &progress{w: o.stderr(), terminal: o.Terminal, total: total, last: time.Now()}
	if total >= 0 {
		fmt.Fprintf(p.w, edition.Command+": downloading %s (%s)\n", ref, mebibytes(total))
	} else {
		fmt.Fprintf(p.w, edition.Command+": downloading %s\n", ref)
	}
	return p
}

func (p *progress) Write(b []byte) (int, error) {
	p.done += int64(len(b))
	every := progressEvery
	if p.terminal {
		every = progressEveryTerminal
	}
	if now := time.Now(); now.Sub(p.last) >= every {
		p.last = now
		p.line(p.status())
	}
	return len(b), nil
}

// finish は終了の行を書き込む（端末では書き直していた行を置換し、改行で閉じる）。
func (p *progress) finish(err error) {
	text := "downloaded " + mebibytes(p.done)
	if err != nil {
		text = "stopped after " + mebibytes(p.done)
	}
	p.line(text)
	if p.terminal {
		fmt.Fprintln(p.w)
	}
}

func (p *progress) status() string {
	if p.total <= 0 {
		return mebibytes(p.done)
	}
	return fmt.Sprintf("%s of %s (%d%%)", mebibytes(p.done), mebibytes(p.total), p.done*100/p.total)
}

// line は 1 行を書き込む。端末では行の先頭へ戻って書き直し、前より短ければ残りを空白で消去する
// （VT の制御列は、Windows の古いコンソールで機能しないため使用しない）。
func (p *progress) line(text string) {
	text = edition.Command + ": " + text
	if !p.terminal {
		fmt.Fprintln(p.w, text)
		return
	}
	pad := ""
	if len(text) < p.width {
		pad = strings.Repeat(" ", p.width-len(text))
	}
	p.width = len(text)
	fmt.Fprint(p.w, "\r"+text+pad)
}

func mebibytes(n int64) string {
	return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
}
