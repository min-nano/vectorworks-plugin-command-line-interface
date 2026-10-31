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
)

// 取得の進み具合を標準エラーに書く。端末でなければ 1 行ずつ足し、端末なら同じ行を書き直して改行で
// 閉じる。大きさが分からない本文（chunked）では量だけを示す。
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
		// 先に送り出して、Content-Length を付けない。
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
		"log":      {"/known.zip", false, []string{"(3.0 MiB)\n", "MiB of 3.0 MiB (", "%)\n", "vw2026: downloaded 3.0 MiB\n"}},
		"terminal": {"/known.zip", true, []string{"(3.0 MiB)\n", "\rvw2026: ", "%)", "\rvw2026: downloaded 3.0 MiB", "\n"}},
		"unknown":  {"/unknown.zip", false, []string{h.url("/unknown.zip") + "\n", "vw2026: downloaded 3.0 MiB\n"}},
	} {
		t.Run(name, func(t *testing.T) {
			var stderr bytes.Buffer
			o := h.options()
			o.Stderr = &stderr
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

// 端末で書き直す行が前より短ければ、残りを空白で消す。失敗で終われば、そこまでの量を示す。
func TestProgressLine(t *testing.T) {
	var stderr bytes.Buffer
	p := &progress{w: &stderr, terminal: true, total: -1}
	p.line("a line longer than the last one")
	p.finish(errors.New("cut"))
	if got := stderr.String(); got != "\rvw2026: a line longer than the last one\rvw2026: stopped after 0.0 MiB          \n" {
		t.Fatalf("%q", got)
	}
}
