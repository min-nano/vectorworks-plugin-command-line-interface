package install

import (
	"bytes"
	"errors"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/edition"
)

// 取得の進捗を標準エラーに書き込む。端末でなければ 1 行ずつ追加し、端末ならば同じ行を書き直して
// 改行で閉じる。大きさが判明しない本文（chunked）では量のみを示す。
func TestDownloadProgress(t *testing.T) {
	originals := [2]time.Duration{progressEvery, progressEveryTerminal}
	progressEvery, progressEveryTerminal = 0, 0
	t.Cleanup(func() {
		progressEvery = originals[0]
		progressEveryTerminal = originals[1]
	})
	h := newHarness(t)
	data := bytes.Repeat([]byte("x"), 3<<20)
	mux := http.NewServeMux()
	mux.HandleFunc("/known.zip", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.Write(data)
	})
	mux.HandleFunc("/unknown.zip", func(w http.ResponseWriter, r *http.Request) {
		// 先に送出して、Content-Length を付けない。
		w.(http.Flusher).Flush()
		w.Write(data)
	})
	inner := h.server.Config.Handler
	h.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, pattern := mux.Handler(r); pattern != "" {
			mux.ServeHTTP(w, r)
			return
		}
		inner.ServeHTTP(w, r)
	})
	sum := sha256Hex(data)
	for name, c := range map[string]struct {
		path     string
		terminal bool
		want     []string
	}{
		"log":      {"/known.zip", false, []string{"(3.0 MiB)\n", "MiB of 3.0 MiB (", "%)\n", edition.Command + ": downloaded 3.0 MiB\n"}},
		"terminal": {"/known.zip", true, []string{"(3.0 MiB)\n", "\r" + edition.Command + ": ", "%)", "\r" + edition.Command + ": downloaded 3.0 MiB", "\n"}},
		"unknown":  {"/unknown.zip", false, []string{h.url("/unknown.zip") + "\n", edition.Command + ": downloaded 3.0 MiB\n"}},
	} {
		t.Run(name, func(t *testing.T) {
			o, stderr := h.capturing()
			o.Terminal = c.terminal
			if err := o.download(h.url(c.path), filepath.Join(t.TempDir(), "x.zip"), sum); err != nil {
				t.Fatal(err)
			}
			got := stderr.String()
			for _, want := range c.want {
				if !strings.Contains(got, want) {
					t.Errorf("%q lacks %q", got, want)
				}
			}
			if !c.terminal && strings.Contains(got, "\r") {
				t.Errorf("%q rewrites a line outside a terminal", got)
			}
		})
	}
}

// 端末で書き直す行が以前より短ければ、残りを空白で消去する。失敗で終了すれば、そこまでの量を示す。
func TestProgressLine(t *testing.T) {
	var stderr bytes.Buffer
	p := &progress{w: &stderr, terminal: true, total: -1}
	p.line("a line longer than the last one")
	p.finish(errors.New("cut"))
	if got := stderr.String(); got != "\r"+edition.Command+": a line longer than the last one\r"+edition.Command+": stopped after 0.0 MiB          \n" {
		t.Fatalf("%q", got)
	}
}
