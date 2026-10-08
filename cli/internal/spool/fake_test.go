package spool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// workSuffix は確保した要求の名前。プラグイン側だけが使うので、CLI の定数には置かない。
const workSuffix = ".work"

// fakePlugin はプラグイン側の受け付けを真似る（docs/protocol.md の「プラグイン側の義務」）。
type fakePlugin struct {
	t        *testing.T
	dir      string
	stop     chan struct{}
	done     chan struct{}
	handle   func(id, tool string, args map[string]any) Response
	protocol int
}

func newSpoolDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), SpoolName("stable"))
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeStatus(t *testing.T, dir string, status map[string]any) {
	t.Helper()
	data, _ := json.Marshal(status)
	if err := os.WriteFile(filepath.Join(dir, StatusFile), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func liveStatus(extra map[string]any) map[string]any {
	status := map[string]any{
		"plugin":   "test",
		"version":  "0",
		"protocol": ProtocolVersion,
		"beat":     float64(time.Now().Unix()),
		"pid":      1,
	}
	for k, v := range extra {
		status[k] = v
	}
	return status
}

func startFake(t *testing.T, dir string, handle func(id, tool string, args map[string]any) Response) *fakePlugin {
	t.Helper()
	f := &fakePlugin{t: t, dir: dir, stop: make(chan struct{}), done: make(chan struct{}), handle: handle}
	writeStatus(t, dir, liveStatus(nil))
	go f.loop()
	t.Cleanup(func() {
		close(f.stop)
		<-f.done
	})
	return f
}

func (f *fakePlugin) loop() {
	defer close(f.done)
	for {
		select {
		case <-f.stop:
			return
		case <-time.After(20 * time.Millisecond):
		}
		entries, _ := os.ReadDir(f.dir)
		var names []string
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), RequestSuffix) {
				names = append(names, entry.Name())
			}
		}
		sort.Strings(names)
		for _, name := range names {
			id := strings.TrimSuffix(name, RequestSuffix)
			// rename で確保してから読む。失敗したら別の橋が先に確保したか、呼ぶ側が取り下げた
			// ので、黙って飛ばす（「読めなかった要求」として偽の失敗を返さない）。
			path := filepath.Join(f.dir, id+workSuffix)
			if claim(filepath.Join(f.dir, name), path) != nil {
				continue
			}
			data, err := os.ReadFile(path)
			_ = os.Remove(path)
			var request struct {
				ID   string         `json:"id"`
				Tool string         `json:"tool"`
				Args map[string]any `json:"args"`
			}
			var response Response
			if err != nil || json.Unmarshal(data, &request) != nil || request.ID != id {
				response = Response{ID: id, OK: false, Error: "unreadable request"}
			} else {
				response = f.handle(request.ID, request.Tool, request.Args)
				response.ID = request.ID
			}
			out, _ := json.Marshal(response)
			_ = os.WriteFile(filepath.Join(f.dir, id+ResponseSuffix+TempSuffix), out, 0o600)
			_ = os.Rename(filepath.Join(f.dir, id+ResponseSuffix+TempSuffix), filepath.Join(f.dir, id+ResponseSuffix))
		}
	}
}
