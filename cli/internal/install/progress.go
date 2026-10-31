package install

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// 進み具合を書く間隔（docs/plugin/install-and-update.md「流れ」）。端末では同じ行を書き
// 直すので短く、端末でなければ（ログに残る）1 行ずつ足すので長くする。テストが縮める。
var (
	progressEvery         = 10 * time.Second
	progressEveryTerminal = 200 * time.Millisecond
)

// progress は、取得した量を数えて、進み具合を w（標準エラー）に書く io.Writer。標準出力は JSON の
// 1 行だけにするので、ここには書かない。
type progress struct {
	w        io.Writer
	terminal bool
	// 全体の大きさ（分からなければ -1）
	total int64
	done  int64
	last  time.Time
	// 端末で最後に書いた行の長さ（書き直すときに、短くなった分を空白で消す）
	width int
}

// startProgress は取得の始めの行を書き、数える progress を返す。
func (o *Options) startProgress(ref string, total int64) *progress {
	p := &progress{w: o.stderr(), terminal: o.Terminal, total: total, last: time.Now()}
	if total >= 0 {
		fmt.Fprintf(p.w, "vw2026: downloading %s (%s)\n", ref, mebibytes(total))
	} else {
		fmt.Fprintf(p.w, "vw2026: downloading %s\n", ref)
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

// finish は終わりの行を書く（端末では書き直していた行を置き換え、改行で閉じる）。
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

// line は 1 行を書く。端末では行の頭へ戻って書き直し、前より短ければ残りを空白で消す（VT の
// 制御列は、Windows の古いコンソールで効かないので使わない）。
func (p *progress) line(text string) {
	text = "vw2026: " + text
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
