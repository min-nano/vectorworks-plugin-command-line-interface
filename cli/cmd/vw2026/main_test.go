package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/spool"
)

// startBridge はスプールを用意し、要求に handle で応える偽のプラグインを走らせる。
func startBridge(t *testing.T, handle func(tool string, args json.RawMessage) spool.Response) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "p-bridge")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	status, _ := json.Marshal(map[string]any{
		"plugin": "p", "version": "0", "protocol": spool.ProtocolVersion,
		"beat": time.Now().Unix(), "pid": 1,
	})
	if err := os.WriteFile(filepath.Join(dir, spool.StatusFile), status, 0o600); err != nil {
		t.Fatal(err)
	}
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
			entries, _ := os.ReadDir(dir)
			for _, entry := range entries {
				name := entry.Name()
				if !strings.HasSuffix(name, spool.RequestSuffix) {
					continue
				}
				data, _ := os.ReadFile(filepath.Join(dir, name))
				_ = os.Remove(filepath.Join(dir, name))
				var request struct {
					ID   string          `json:"id"`
					Tool string          `json:"tool"`
					Args json.RawMessage `json:"args"`
				}
				_ = json.Unmarshal(data, &request)
				response := handle(request.Tool, request.Args)
				response.ID = request.ID
				out, _ := json.Marshal(response)
				_ = os.WriteFile(filepath.Join(dir, request.ID+spool.ResponseSuffix), out, 0o600)
			}
		}
	}()
	t.Cleanup(func() { close(stop); <-done })
	return dir
}

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
	return startBridge(t, func(tool string, args json.RawMessage) spool.Response {
		if tool == "fail" {
			return spool.Response{OK: false, Error: "boom"}
		}
		out, _ := json.Marshal(map[string]any{"tool": tool, "args": args})
		return spool.Response{OK: true, Result: out}
	})
}

func TestStatusLive(t *testing.T) {
	dir := echoBridge(t)
	r := invoke(t, map[string]string{"VW2026_SPOOL": dir}, "", nil, "status")
	if r.code != exitOK || !strings.Contains(r.stdout, `"live":true`) || !strings.Contains(r.stdout, `"plugin":"p"`) {
		t.Fatalf("%+v", r)
	}
}

func TestStatusDown(t *testing.T) {
	r := invoke(t, map[string]string{"VW2026_SPOOL": filepath.Join(t.TempDir(), "none")}, "", nil, "status")
	if r.code != exitDown || !strings.Contains(r.stdout, `"live":false`) {
		t.Fatalf("%+v", r)
	}
}

func TestStatusDownReportsShellDiagnostic(t *testing.T) {
	dir := t.TempDir()
	shell, _ := json.Marshal(map[string]any{
		"plugin": "cli", "protocol": spool.ProtocolVersion, "beat": float64(time.Now().Unix()), "pid": 1,
		"error": map[string]any{"code": "load_failed", "message": "x"},
	})
	if err := os.WriteFile(filepath.Join(dir, spool.ShellFile), shell, 0o600); err != nil {
		t.Fatal(err)
	}
	r := invoke(t, map[string]string{"VW2026_SPOOL": dir}, "", nil, "status")
	if r.code != exitDown || !strings.Contains(r.stdout, `"code":"load_failed"`) || !strings.Contains(r.stderr, "payload is not running") {
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

func TestUnknownChannel(t *testing.T) {
	r := invoke(t, map[string]string{"VW2026_CHANNEL": "nightly"}, "", nil, "status")
	if r.code != exitUsage || !strings.Contains(r.stderr, "unknown channel") {
		t.Fatalf("%+v", r)
	}
}

func TestVersion(t *testing.T) {
	r := invoke(t, nil, "", nil, "version")
	if r.code != exitOK || !strings.Contains(r.stdout, `"protocol":1`) {
		t.Fatalf("%+v", r)
	}
}

// unresponsiveSpool は、印が古いが pid の Vectorworks は動いているスプールを作る
// （保存の確認のダイアログを開いている間など）。running を偽にすると終了を真似る。
func unresponsiveSpool(t *testing.T) (string, *bool) {
	t.Helper()
	running := true
	saved := spool.ProcessRunning
	spool.ProcessRunning = func(int) bool { return running }
	t.Cleanup(func() { spool.ProcessRunning = saved })
	dir := filepath.Join(t.TempDir(), "p-bridge")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	status, _ := json.Marshal(map[string]any{
		"plugin": "p", "version": "0", "protocol": spool.ProtocolVersion,
		"beat": time.Now().Unix() - spool.StaleSeconds - 5, "pid": 4242,
	})
	if err := os.WriteFile(filepath.Join(dir, spool.StatusFile), status, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, &running
}

func TestStatusUnresponsive(t *testing.T) {
	dir, _ := unresponsiveSpool(t)
	r := invoke(t, map[string]string{"VW2026_SPOOL": dir}, "", nil, "status")
	if r.code != exitUnresponsive || !strings.Contains(r.stdout, `"state":"unresponsive"`) || !strings.Contains(r.stdout, `"live":false`) {
		t.Fatalf("%+v", r)
	}
}

func TestStatusUnresponsiveReportsShellDiagnostic(t *testing.T) {
	dir, _ := unresponsiveSpool(t)
	shell, _ := json.Marshal(map[string]any{
		"plugin": "p", "protocol": spool.ProtocolVersion, "beat": float64(time.Now().Unix()), "pid": 4242,
		"error": map[string]any{"code": "serve_failed", "message": "x"},
	})
	if err := os.WriteFile(filepath.Join(dir, spool.ShellFile), shell, 0o600); err != nil {
		t.Fatal(err)
	}
	r := invoke(t, map[string]string{"VW2026_SPOOL": dir}, "", nil, "status")
	if r.code != exitUnresponsive || !strings.Contains(r.stdout, `"code":"serve_failed"`) || !strings.Contains(r.stderr, "payload is not running") {
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
	dir, running := unresponsiveSpool(t)
	r := invoke(t, map[string]string{"VW2026_SPOOL": dir}, "", nil, "wait", "--down", "--timeout", "0.3")
	if r.code != exitTimeout {
		t.Fatalf("unresponsive must not count as down: %+v", r)
	}
	*running = false
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
