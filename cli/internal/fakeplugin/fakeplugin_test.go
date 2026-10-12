package fakeplugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/spool"
)

// put は呼ぶ側を真似て、待つ印を掴んでから要求を置く。待つ印はテストの終わりに放す。
func put(t *testing.T, dir, id, text string) {
	t.Helper()
	unlock, err := lock(filepath.Join(dir, id+spool.WaitSuffix))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(unlock)
	write(t, dir, id+spool.RequestSuffix, text)
}

func write(t *testing.T, dir, name, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func exists(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}

func count(t *testing.T, dir, suffix string) int {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "*"+suffix))
	if err != nil {
		t.Fatal(err)
	}
	return len(matches)
}

// 回の始めに並べた要求はすべて処理し（件数の上限は無い）、その後に置かれた要求は次の回に回す。
func TestServeTakesOnlyRequestsListedAtStart(t *testing.T) {
	dir := NewDir(t)
	const listed = 20
	for i := range listed {
		put(t, dir, fmt.Sprintf("%04d", i), `{"tool":"ping"}`)
	}
	added := false
	s := &server{dir: dir, handle: func(string, json.RawMessage) spool.Response {
		if !added {
			added = true
			put(t, dir, "9999", `{"tool":"ping"}`)
		}
		return spool.Response{OK: true}
	}}
	t.Cleanup(s.release)
	s.serveOnce()
	if got := count(t, dir, spool.ResponseSuffix); got != listed {
		t.Fatalf("want %d responses, got %d", listed, got)
	}
	if !exists(dir, "9999"+spool.RequestSuffix) {
		t.Fatal("a request put during the round should wait for the next one")
	}
}
