// Package fakeplugin はプラグイン側の受け付けを真似る（docs/protocol.md「プラグイン側の義務」）。
// spool と vw2026 の単体テストだけが使う。
package fakeplugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/spool"
)

// Handler は道具 1 つの呼び出しに応える。
type Handler func(tool string, args json.RawMessage) spool.Response

// NewDir は 0700 のスプールを作る。
func NewDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), spool.SpoolDirName)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

// HoldLock はロックファイルを掴む（Vectorworks が動いている）。返す関数で放す（終了を真似る）。
// テストの終わりにも放す。
func HoldLock(t *testing.T, dir string) (release func()) {
	t.Helper()
	unlock, err := lock(filepath.Join(dir, spool.LockFile))
	if err != nil {
		t.Fatal(err)
	}
	released := false
	release = func() {
		if !released {
			released = true
			unlock()
		}
	}
	t.Cleanup(release)
	return release
}

// WriteStatus は印を書く（プラグインはロックを取ったときに 1 度だけ書く）。
func WriteStatus(t *testing.T, dir string, extra map[string]any) {
	t.Helper()
	status := map[string]any{
		"version":  "0",
		"branch":   "main",
		"protocol": spool.ProtocolVersion,
		"pid":      os.Getpid(),
	}
	for k, v := range extra {
		status[k] = v
	}
	data, _ := json.Marshal(status)
	if err := os.WriteFile(filepath.Join(dir, spool.StatusFile), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// Start はロックを掴み、印を書いて、要求に handle で応え続ける。テストの終わりに止める。
func Start(t *testing.T, dir string, handle Handler) {
	t.Helper()
	release := HoldLock(t, dir)
	WriteStatus(t, dir, nil)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			case <-time.After(10 * time.Millisecond):
			}
			serveOnce(dir, handle)
		}
	}()
	t.Cleanup(func() {
		close(stop)
		<-done
		release()
	})
}

// serveOnce は受け付け 1 回。要求を名前の昇順で読み、消してから実行する。
func serveOnce(dir string, handle Handler) {
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), spool.RequestSuffix) {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		id := strings.TrimSuffix(name, spool.RequestSuffix)
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			// 呼ぶ側が取り下げた。黙って飛ばす。
			continue
		}
		if os.Remove(path) != nil {
			// 消せない要求は実行しない（残すと次の回にまた実行してしまう。消えていたなら
			// 呼ぶ側が取り下げた）。
			continue
		}
		var request struct {
			Tool string          `json:"tool"`
			Args json.RawMessage `json:"args"`
		}
		var response spool.Response
		if json.Unmarshal(data, &request) != nil || request.Tool == "" {
			response = spool.Response{OK: false, Error: "unreadable request"}
		} else {
			response = handle(request.Tool, request.Args)
		}
		out, _ := json.Marshal(response)
		temp := filepath.Join(dir, id+spool.ResponseSuffix+spool.TempSuffix)
		_ = os.WriteFile(temp, out, 0o600)
		_ = os.Rename(temp, filepath.Join(dir, id+spool.ResponseSuffix))
	}
}
