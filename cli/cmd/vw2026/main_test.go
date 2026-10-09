package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/fakeplugin"
	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/spool"
)

type result struct {
	code   int
	stdout string
	stderr string
}

func invoke(t *testing.T, vars map[string]string, stdin string, started *[]string, args ...string) result {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(args, env{
		stdin:  strings.NewReader(stdin),
		stdout: &stdout,
		stderr: &stderr,
		getenv: func(name string) string { return vars[name] },
		now:    time.Now,
		start: func(argv []string) error {
			if started != nil {
				*started = argv
			}
			return nil
		},
	})
	return result{code, stdout.String(), stderr.String()}
}

func echoBridge(t *testing.T) string {
	dir := fakeplugin.NewDir(t)
	fakeplugin.Start(t, dir, func(tool string, args json.RawMessage) spool.Response {
		if tool == "fail" {
			return spool.Response{OK: false, Error: "boom"}
		}
		out, _ := json.Marshal(map[string]any{"tool": tool, "args": args})
		return spool.Response{OK: true, Result: out}
	})
	return dir
}

func TestStatusLive(t *testing.T) {
	dir := echoBridge(t)
	r := invoke(t, map[string]string{"VW2026_SPOOL": dir}, "", nil, "status")
	if r.code != exitOK || !strings.Contains(r.stdout, `"live":true`) || !strings.Contains(r.stdout, `"branch":"main"`) {
		t.Fatalf("%+v", r)
	}
}

func TestStatusDown(t *testing.T) {
	r := invoke(t, map[string]string{"VW2026_SPOOL": filepath.Join(t.TempDir(), "none")}, "", nil, "status")
	if r.code != exitDown || !strings.Contains(r.stdout, `"live":false`) || !strings.Contains(r.stdout, `"reason":"not found"`) {
		t.Fatalf("%+v", r)
	}
}

func TestCallPrintsResult(t *testing.T) {
	dir := echoBridge(t)
	r := invoke(t, nil, "", nil, "call", "layers", `{"a":1}`, "--spool", dir)
	if r.code != exitOK || strings.TrimSpace(r.stdout) != `{"args":{"a":1},"tool":"layers"}` {
		t.Fatalf("%+v", r)
	}
}

func TestCallArgsFromStdin(t *testing.T) {
	dir := echoBridge(t)
	r := invoke(t, map[string]string{"VW2026_SPOOL": dir}, `{"b":2}`, nil, "call", "x", "-")
	if r.code != exitOK || !strings.Contains(r.stdout, `"b":2`) {
		t.Fatalf("%+v", r)
	}
}

func TestCallToolError(t *testing.T) {
	dir := echoBridge(t)
	r := invoke(t, map[string]string{"VW2026_SPOOL": dir}, "", nil, "call", "fail")
	if r.code != exitToolErr || r.stdout != "" || !strings.Contains(r.stderr, "boom") {
		t.Fatalf("%+v", r)
	}
	raw := invoke(t, map[string]string{"VW2026_SPOOL": dir}, "", nil, "call", "--raw", "fail")
	if raw.code != exitToolErr || !strings.Contains(raw.stdout, `"ok":false`) {
		t.Fatalf("%+v", raw)
	}
}

func TestToolsIsCallOfTools(t *testing.T) {
	dir := echoBridge(t)
	r := invoke(t, map[string]string{"VW2026_SPOOL": dir}, "", nil, "tools")
	if r.code != exitOK || !strings.Contains(r.stdout, `"tool":"tools"`) {
		t.Fatalf("%+v", r)
	}
}

func TestCallUsageErrors(t *testing.T) {
	for _, args := range [][]string{{"call"}, {"call", "x", "[1]"}, {"call", "x", "not json"}, {"nope"}, {}} {
		r := invoke(t, nil, "", nil, args...)
		if r.code != exitUsage {
			t.Errorf("%v: code %d", args, r.code)
		}
	}
}

func TestCallWhenDown(t *testing.T) {
	r := invoke(t, map[string]string{"VW2026_SPOOL": filepath.Join(t.TempDir(), "none")}, "", nil, "call", "ping")
	if r.code != exitDown {
		t.Fatalf("%+v", r)
	}
}

func TestLaunchSkipsWhenLive(t *testing.T) {
	dir := echoBridge(t)
	var started []string
	r := invoke(t, map[string]string{"VW2026_SPOOL": dir}, "", &started, "launch")
	if r.code != exitOK || started != nil || !strings.Contains(r.stdout, `"launched":false`) {
		t.Fatalf("%+v %v", r, started)
	}
}

func TestLaunchStartsApp(t *testing.T) {
	app := filepath.Join(t.TempDir(), "fake-vw")
	if err := os.WriteFile(app, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	var started []string
	r := invoke(t, map[string]string{"VW2026_SPOOL": filepath.Join(t.TempDir(), "none"), "VW2026_APP": app}, "", &started, "launch")
	if r.code != exitOK || len(started) != 1 || started[0] != app {
		t.Fatalf("%+v %v", r, started)
	}
}

func TestWaitTimesOut(t *testing.T) {
	r := invoke(t, map[string]string{"VW2026_SPOOL": filepath.Join(t.TempDir(), "none")}, "", nil, "wait", "--timeout", "0.3")
	if r.code != exitTimeout {
		t.Fatalf("%+v", r)
	}
	down := invoke(t, map[string]string{"VW2026_SPOOL": filepath.Join(t.TempDir(), "none")}, "", nil, "wait", "--down")
	if down.code != exitOK || !strings.Contains(down.stdout, `"live":false`) {
		t.Fatalf("%+v", down)
	}
}

func TestVersion(t *testing.T) {
	r := invoke(t, nil, "", nil, "version")
	if r.code != exitOK || !strings.Contains(r.stdout, `"protocol":1`) {
		t.Fatalf("%+v", r)
	}
}

// unresponsiveSpool は、Vectorworks は動いている（ロックが掴まれている）が印が古いスプールを
// 作る（保存の確認のダイアログを開いている間など）。返す関数で終了を真似る。
func unresponsiveSpool(t *testing.T) (string, func()) {
	t.Helper()
	dir := fakeplugin.NewDir(t)
	release := fakeplugin.HoldLock(t, dir)
	fakeplugin.WriteStatus(t, dir, (spool.StaleSeconds+5)*time.Second, nil)
	return dir, release
}

func TestStatusUnresponsive(t *testing.T) {
	dir, _ := unresponsiveSpool(t)
	r := invoke(t, map[string]string{"VW2026_SPOOL": dir}, "", nil, "status")
	if r.code != exitUnresponsive || !strings.Contains(r.stdout, `"state":"unresponsive"`) || !strings.Contains(r.stdout, `"live":false`) {
		t.Fatalf("%+v", r)
	}
}

func TestCallWhileUnresponsiveTimesOutAsUnresponsive(t *testing.T) {
	dir, _ := unresponsiveSpool(t)
	r := invoke(t, map[string]string{"VW2026_SPOOL": dir}, "", nil, "call", "ping", "--timeout", "0.3")
	if r.code != exitUnresponsive {
		t.Fatalf("%+v", r)
	}
}

func TestWaitDownWaitsForTheProcess(t *testing.T) {
	dir, quit := unresponsiveSpool(t)
	r := invoke(t, map[string]string{"VW2026_SPOOL": dir}, "", nil, "wait", "--down", "--timeout", "0.3")
	if r.code != exitTimeout {
		t.Fatalf("unresponsive must not count as down: %+v", r)
	}
	quit()
	r = invoke(t, map[string]string{"VW2026_SPOOL": dir}, "", nil, "wait", "--down", "--timeout", "0.3")
	if r.code != exitOK || !strings.Contains(r.stdout, `"state":"down"`) {
		t.Fatalf("%+v", r)
	}
}

func TestLaunchSkipsWhenUnresponsive(t *testing.T) {
	dir, _ := unresponsiveSpool(t)
	var started []string
	r := invoke(t, map[string]string{"VW2026_SPOOL": dir}, "", &started, "launch")
	if r.code != exitUnresponsive || started != nil || !strings.Contains(r.stdout, `"launched":false`) {
		t.Fatalf("%+v %v", r, started)
	}
}

func TestLaunchDoesNotTakeTimeout(t *testing.T) {
	r := invoke(t, map[string]string{"VW2026_SPOOL": filepath.Join(t.TempDir(), "none")}, "", nil, "launch", "--timeout", "5")
	if r.code != exitUsage {
		t.Fatalf("%+v", r)
	}
}
