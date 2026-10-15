package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alecthomas/kong"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/fakeplugin"
	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/launch"
	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/spool"
)

type result struct {
	code   int
	stdout string
	stderr string
}

// envVars は、コマンドが読む環境変数のすべて。
var envVars = []string{
	"VW2026_SPOOL", "VW2026_APP", "VW2026_SESSION",
	"VW2026_CALL_RAW", "VW2026_CALL_TIMEOUT", "VW2026_SESSION_TIMEOUT",
}

func invoke(t *testing.T, vars map[string]string, stdin string, started *[]string, args ...string) result {
	t.Helper()
	return invokeStart(t, vars, stdin, func(argv []string) error {
		if started != nil {
			*started = argv
		}
		return nil
	}, args...)
}

// invokeStart は invoke と同じで、起動（env.start）を差し替える。起動の失敗を確かめるため。
func invokeStart(t *testing.T, vars map[string]string, stdin string, start func(argv []string) error, args ...string) result {
	t.Helper()
	// kong は環境変数を os から直接読むので、テストごとに置き直す（並行には走らせない）。
	for _, name := range envVars {
		value, ok := vars[name]
		// 終わったら元に戻させる
		t.Setenv(name, value)
		if !ok {
			// 空の値も値になる（空のタイムアウトは使い方の誤り）ので、渡さないものは消す。
			os.Unsetenv(name)
		}
	}
	var stdout, stderr bytes.Buffer
	code := run(args, env{
		stdin:  strings.NewReader(stdin),
		stdout: &stdout,
		stderr: &stderr,
		start:  start,
	})
	return result{code, stdout.String(), stderr.String()}
}

func echoBridge(t *testing.T) string {
	dir := fakeplugin.NewDir(t)
	fakeplugin.Start(t, dir, func(tool string, args json.RawMessage) spool.Response {
		switch tool {
		case "fail":
			return spool.Response{OK: false, Code: spool.CodeInternal, Error: "boom"}
		case "picky":
			return spool.Response{OK: false, Code: spool.CodeInvalidArgs, Error: "no such argument"}
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
	if r.code != exitToolFailure || r.stdout != "" || !strings.Contains(r.stderr, "internal: boom") {
		t.Fatalf("%+v", r)
	}
	raw := invoke(t, map[string]string{"VW2026_SPOOL": dir}, "", nil, "call", "--raw", "fail")
	if raw.code != exitToolFailure || !strings.Contains(raw.stdout, `"ok":false`) || !strings.Contains(raw.stdout, `"code":"internal"`) {
		t.Fatalf("%+v", raw)
	}
}

func TestCallToolUsageError(t *testing.T) {
	dir := echoBridge(t)
	r := invoke(t, map[string]string{"VW2026_SPOOL": dir}, "", nil, "call", "picky")
	if r.code != exitToolUsage || r.stdout != "" || !strings.Contains(r.stderr, "invalid_args: no such argument") {
		t.Fatalf("%+v", r)
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
	app := fakeApp(t)
	var started []string
	r := invoke(t, map[string]string{"VW2026_SPOOL": filepath.Join(t.TempDir(), "none"), "VW2026_APP": app}, "", &started, "launch")
	if r.code != exitOK || len(started) != 1 || started[0] != app {
		t.Fatalf("%+v %v", r, started)
	}
}

// 動いている Vectorworks にプラグインが入っていない（Windows で起動を断られた）なら、
// ブリッジが止まっているときと同じ 5 で終わる。
func TestLaunchWhenRunningWithoutBridge(t *testing.T) {
	app := fakeApp(t)
	r := invokeStart(t, map[string]string{"VW2026_SPOOL": filepath.Join(t.TempDir(), "none"), "VW2026_APP": app}, "",
		func([]string) error { return fmt.Errorf("start: %w", launch.ErrAlreadyRunning) }, "launch")
	if r.code != exitDown || r.stdout != "" || !strings.Contains(r.stderr, "plug-in installed") {
		t.Fatalf("%+v", r)
	}
}

func TestLaunchStartFails(t *testing.T) {
	app := fakeApp(t)
	r := invokeStart(t, map[string]string{"VW2026_SPOOL": filepath.Join(t.TempDir(), "none"), "VW2026_APP": app}, "",
		func([]string) error { return errors.New("exec format error") }, "launch")
	if r.code != exitFailure || r.stdout != "" || !strings.Contains(r.stderr, "exec format error") {
		t.Fatalf("%+v", r)
	}
}

// 起動するものが決まらなければ、起動を試みずに失敗する。
func TestLaunchAppNotFound(t *testing.T) {
	var started []string
	missing := filepath.Join(t.TempDir(), "missing")
	r := invoke(t, map[string]string{"VW2026_SPOOL": filepath.Join(t.TempDir(), "none"), "VW2026_APP": missing}, "", &started, "launch")
	if r.code != exitFailure || started != nil || !strings.Contains(r.stderr, missing) {
		t.Fatalf("%+v %v", r, started)
	}
}

// fakeApp は起動の対象にする実行ファイルを作る（launch.Command はファイルがあることだけを見る）。
func fakeApp(t *testing.T) string {
	t.Helper()
	app := filepath.Join(t.TempDir(), "fake-vw")
	if err := os.WriteFile(app, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return app
}

func TestVersion(t *testing.T) {
	r := invoke(t, nil, "", nil, "version")
	if r.code != exitOK || !strings.Contains(r.stdout, `"protocol":5`) {
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

// 終了を待つ呼ぶ側は status を繰り返すので、応えなくてもロックが掴まれている間は
// 動いていると答え、放されたら止まったと答える（保存の確認のダイアログを止まったと誤らない）。
func TestStatusWhileBusyUntilTheProcessEnds(t *testing.T) {
	dir, quit := busySpool(t)
	r := invoke(t, map[string]string{"VW2026_SPOOL": dir}, "", nil, "status")
	if r.code != exitOK || !strings.Contains(r.stdout, `"running":true`) {
		t.Fatalf("a held lock must count as running: %+v", r)
	}
	quit()
	r = invoke(t, map[string]string{"VW2026_SPOOL": dir}, "", nil, "status")
	if r.code != exitDown || !strings.Contains(r.stdout, `"running":false`) {
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
	if r := invoke(t, nil, "", nil, "--version"); r.code != exitOK || !strings.Contains(r.stdout, `"protocol":5`) {
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

func TestCodeFor(t *testing.T) {
	for err, want := range map[error]int{
		spool.ErrNotRunning: exitDown,
		spool.ErrTimeout:    exitTimeout,
		spool.ErrNoWait:     exitTimeout,
		spool.ErrBusy:       exitBusy,
		spool.ErrNoSession:  exitBusy,
		spool.ErrNoResponse: exitNoResponse,
	} {
		if got := codeFor(fmt.Errorf("%w (detail)", err)); got != want {
			t.Errorf("%v: got %d, want %d", err, got, want)
		}
	}
}

// session start が印を出し、その印の call だけが実行される。end のあとは印の無い call が実行される。
func TestSessionStartCallEnd(t *testing.T) {
	dir := echoBridge(t)
	vars := map[string]string{"VW2026_SPOOL": dir}
	r := invoke(t, vars, "", nil, "session", "start")
	var started struct {
		Session string `json:"session"`
	}
	if r.code != exitOK || json.Unmarshal([]byte(r.stdout), &started) != nil || started.Session == "" {
		t.Fatalf("%+v", r)
	}
	if r := invoke(t, vars, "", nil, "session", "start"); r.code != exitBusy {
		t.Fatalf("a second session should be refused: %+v", r)
	}
	if r := invoke(t, vars, "", nil, "call", "layers"); r.code != exitBusy {
		t.Fatalf("a call outside the session should be refused: %+v", r)
	}
	if r := invoke(t, vars, "", nil, "call", "ping"); r.code != exitOK {
		t.Fatalf("ping is for everyone: %+v", r)
	}
	if r := invoke(t, vars, "", nil, "call", "layers", "--session", started.Session); r.code != exitOK {
		t.Fatalf("%+v", r)
	}
	inSession := map[string]string{"VW2026_SPOOL": dir, "VW2026_SESSION": started.Session}
	if r := invoke(t, inSession, "", nil, "call", "quit"); r.code != exitOK {
		t.Fatalf("quit in the session (from VW2026_SESSION): %+v", r)
	}
	if r := invoke(t, inSession, "", nil, "session", "end"); r.code != exitOK || strings.TrimSpace(r.stdout) != `{"ended":true}` {
		t.Fatalf("%+v", r)
	}
	if r := invoke(t, inSession, "", nil, "call", "layers"); r.code != exitBusy {
		t.Fatalf("the ended session should be refused: %+v", r)
	}
	if r := invoke(t, vars, "", nil, "call", "layers"); r.code != exitOK {
		t.Fatalf("%+v", r)
	}
	if r := invoke(t, vars, "", nil, "call", "quit"); r.code != exitBusy {
		t.Fatalf("quit outside a session should be refused: %+v", r)
	}
}

func TestSessionEndNeedsSession(t *testing.T) {
	dir := echoBridge(t)
	if r := invoke(t, map[string]string{"VW2026_SPOOL": dir}, "", nil, "session", "end"); r.code != exitUsage {
		t.Fatalf("%+v", r)
	}
}

// 終了コードは呼ぶ側との約束なので、定数の名前ではなく番号そのものを固定する。
func TestExitCodeNumbers(t *testing.T) {
	for name, c := range map[string][2]int{
		"OK":          {exitOK, 0},
		"Failure":     {exitFailure, 1},
		"Usage":       {exitUsage, 2},
		"Timeout":     {exitTimeout, 3},
		"NoResponse":  {exitNoResponse, 4},
		"Down":        {exitDown, 5},
		"ToolFailure": {exitToolFailure, 21},
		"ToolUsage":   {exitToolUsage, 22},
		"Busy":        {exitBusy, 23},
	} {
		if c[0] != c[1] {
			t.Errorf("exit%s = %d, want %d", name, c[0], c[1])
		}
	}
}

func TestToolCodeFor(t *testing.T) {
	for code, want := range map[string]int{
		spool.CodeInvalidRequest: exitToolUsage,
		spool.CodeUnknownTool:    exitToolUsage,
		spool.CodeInvalidArgs:    exitToolUsage,
		spool.CodeInternal:       exitToolFailure,
		"something_new":          exitFailure,
	} {
		if got := toolCodeFor(code); got != want {
			t.Errorf("toolCodeFor(%q) = %d, want %d", code, got, want)
		}
	}
}

// 指定は環境変数でも与えられ、両方あれば引数が勝つ。
func TestFlagsFromEnv(t *testing.T) {
	dir := echoBridge(t)
	raw := map[string]string{"VW2026_SPOOL": dir, "VW2026_CALL_RAW": "true"}
	if r := invoke(t, raw, "", nil, "call", "fail"); r.code != exitToolFailure || !strings.Contains(r.stdout, `"code":"internal"`) {
		t.Fatalf("VW2026_CALL_RAW: %+v", r)
	}
	if r := invoke(t, raw, "", nil, "call", "fail", "--no-raw"); r.code != exitToolFailure || r.stdout != "" {
		t.Fatalf("--no-raw over VW2026_CALL_RAW: %+v", r)
	}

	busy, _ := busySpool(t)
	short := map[string]string{"VW2026_SPOOL": busy, "VW2026_CALL_TIMEOUT": "0.3"}
	if r := invoke(t, short, "", nil, "call", "ping"); r.code != exitTimeout {
		t.Fatalf("VW2026_CALL_TIMEOUT: %+v", r)
	}
	if r := invoke(t, map[string]string{"VW2026_SPOOL": busy, "VW2026_CALL_TIMEOUT": "60"}, "", nil, "call", "ping", "--timeout", "0.3"); r.code != exitTimeout {
		t.Fatalf("--timeout over VW2026_CALL_TIMEOUT: %+v", r)
	}
	if r := invoke(t, map[string]string{"VW2026_SPOOL": busy, "VW2026_SESSION_TIMEOUT": "0.3"}, "", nil, "session", "start"); r.code != exitTimeout {
		t.Fatalf("VW2026_SESSION_TIMEOUT: %+v", r)
	}
	if r := invoke(t, map[string]string{"VW2026_SPOOL": busy, "VW2026_CALL_TIMEOUT": ""}, "", nil, "call", "ping"); r.code != exitUsage {
		t.Fatalf("an empty timeout: %+v", r)
	}
}

// TestEveryFlagHasEnv は、ヘルプを除くすべての指定に環境変数があり、invoke がそれを
// 置き直すこと、オン・オフの指定が --no-<flag> を持つことを確かめる（指定を足して環境変数を付け忘れないように）。
func TestEveryFlagHasEnv(t *testing.T) {
	known := map[string]bool{}
	for _, name := range envVars {
		known[name] = true
	}
	for _, node := range kong.Must(&cli{}).Model.Leaves(true) {
		for _, group := range node.AllFlags(false) {
			for _, flag := range group {
				if flag.Name == "help" {
					continue
				}
				// オン・オフの指定は --no-<flag> で打ち消せるようにする（環境変数でオンにしたものを戻すため）。
				if flag.IsBool() && flag.Tag.Negatable == "" {
					t.Errorf("%s --%s is not negatable (add negatable:\"\")", node.Path(), flag.Name)
				}
				if len(flag.Envs) == 0 {
					t.Errorf("%s --%s has no environment variable", node.Path(), flag.Name)
				}
				for _, name := range flag.Envs {
					if !known[name] {
						t.Errorf("%s is missing from envVars", name)
					}
				}
			}
		}
	}
}

// TestEnvNamesDoNotCollide は、VW2026_<コマンド>_ で始まる環境変数を、そのコマンド（とその下の
// コマンド）の指定だけが使うことを確かめる。<コマンド> はどの深さのコマンドでもよい（session も
// session end も）。全体で使う値に VW2026_SESSION_X のような名前を付けると、session コマンドの指定 X と
// 区別できなくなるため（CLAUDE.md「Go」）。
func TestEnvNamesDoNotCollide(t *testing.T) {
	app := kong.Must(&cli{})
	// コマンドの道筋（"session end"）ごとの接頭辞（"VW2026_SESSION_END_"）。
	prefixes := map[string]string{}
	var walk func(nodes []*kong.Node)
	walk = func(nodes []*kong.Node) {
		for _, node := range nodes {
			path := node.Path()
			prefixes[path] = "VW2026_" + strings.ToUpper(strings.NewReplacer(" ", "_", "-", "_").Replace(path)) + "_"
			walk(node.Children)
		}
	}
	walk(app.Model.Children)
	for _, leaf := range app.Model.Leaves(true) {
		for _, group := range leaf.AllFlags(false) {
			for _, flag := range group {
				for _, name := range flag.Envs {
					for path, prefix := range prefixes {
						within := leaf.Path() == path || strings.HasPrefix(leaf.Path(), path+" ")
						if strings.HasPrefix(name, prefix) && !within {
							t.Errorf("%s --%s uses %s, which belongs to %s", leaf.Path(), flag.Name, name, path)
						}
					}
				}
			}
		}
	}
}
