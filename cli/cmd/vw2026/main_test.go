package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alecthomas/kong"

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
	// kong は環境変数を os から直接読むので、テストごとに置き直す（並行には走らせない）。
	for _, name := range []string{"VW2026_SPOOL", "VW2026_APP"} {
		t.Setenv(name, vars[name])
	}
	var stdout, stderr bytes.Buffer
	code := run(args, env{
		stdin:  strings.NewReader(stdin),
		stdout: &stdout,
		stderr: &stderr,
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
	if r.code != exitOK || !strings.Contains(r.stdout, `"running":true`) {
		t.Fatalf("%+v", r)
	}
}

func TestStatusDown(t *testing.T) {
	r := invoke(t, map[string]string{"VW2026_SPOOL": filepath.Join(t.TempDir(), "none")}, "", nil, "status")
	if r.code != exitDown || !strings.Contains(r.stdout, `"running":false`) || !strings.Contains(r.stdout, `"reason":"not found"`) {
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

func TestCallUsageErrors(t *testing.T) {
	for _, args := range [][]string{{"call"}, {"call", "x", "[1]"}, {"call", "x", "not json"}, {"tools"}, {"nope"}, {}} {
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
	if down.code != exitOK || !strings.Contains(down.stdout, `"running":false`) {
		t.Fatalf("%+v", down)
	}
}

func TestVersion(t *testing.T) {
	r := invoke(t, nil, "", nil, "version")
	if r.code != exitOK || !strings.Contains(r.stdout, `"protocol":3`) {
		t.Fatalf("%+v", r)
	}
}

// busySpool は、Vectorworks は動いている（ロックが掴まれている）が応えないスプールを作る
// （保存の確認のダイアログを開いている間など）。返す関数で終了を真似る。
func busySpool(t *testing.T) (string, func()) {
	t.Helper()
	dir := fakeplugin.NewDir(t)
	release := fakeplugin.HoldLock(t, dir)
	return dir, release
}

func TestCallWhileBusyTimesOut(t *testing.T) {
	dir, _ := busySpool(t)
	r := invoke(t, map[string]string{"VW2026_SPOOL": dir}, "", nil, "call", "ping", "--timeout", "0.3")
	if r.code != exitTimeout {
		t.Fatalf("%+v", r)
	}
}

func TestWaitDownWaitsForTheProcess(t *testing.T) {
	dir, quit := busySpool(t)
	r := invoke(t, map[string]string{"VW2026_SPOOL": dir}, "", nil, "wait", "--down", "--timeout", "0.3")
	if r.code != exitTimeout {
		t.Fatalf("a held lock must not count as down: %+v", r)
	}
	quit()
	r = invoke(t, map[string]string{"VW2026_SPOOL": dir}, "", nil, "wait", "--down", "--timeout", "0.3")
	if r.code != exitOK || !strings.Contains(r.stdout, `"running":false`) {
		t.Fatalf("%+v", r)
	}
}

func TestLaunchDoesNotTakeTimeout(t *testing.T) {
	r := invoke(t, map[string]string{"VW2026_SPOOL": filepath.Join(t.TempDir(), "none")}, "", nil, "launch", "--timeout", "5")
	if r.code != exitUsage {
		t.Fatalf("%+v", r)
	}
}

func TestHelp(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"--help"}, {"-h"}} {
		r := invoke(t, nil, "", nil, args...)
		if r.code != exitOK || !strings.Contains(r.stdout, "call") || !strings.Contains(r.stdout, "exit with") {
			t.Fatalf("%v: %+v", args, r)
		}
	}
	for _, args := range [][]string{{"help", "call"}, {"call", "--help"}, {"call", "x", "-h"}} {
		r := invoke(t, nil, "", nil, args...)
		if r.code != exitOK || !strings.Contains(r.stdout, "--timeout") || !strings.Contains(r.stdout, "withdraws the request") {
			t.Fatalf("%v: %+v", args, r)
		}
	}
	// 引数なしは使い方の誤りで、ヘルプは標準エラーへ。
	if r := invoke(t, nil, "", nil); r.code != exitUsage || r.stdout != "" || !strings.Contains(r.stderr, "Usage:") {
		t.Fatalf("%+v", r)
	}
	if r := invoke(t, nil, "", nil, "help", "nope"); r.code != exitUsage {
		t.Fatalf("%+v", r)
	}
}

func TestVersionFlag(t *testing.T) {
	if r := invoke(t, nil, "", nil, "--version"); r.code != exitOK || !strings.Contains(r.stdout, `"protocol":3`) {
		t.Fatalf("%+v", r)
	}
}

// TestEveryCommandRuns は、kong に並べたコマンドがすべて command を満たすことを確かめる
// （満たさなければ run の型アサーションが実行時に落ちる）。
func TestEveryCommandRuns(t *testing.T) {
	for _, node := range kong.Must(&cli{}).Model.Leaves(true) {
		if _, ok := node.Target.Addr().Interface().(command); !ok {
			t.Errorf("%s does not implement command", node.Name)
		}
	}
}
