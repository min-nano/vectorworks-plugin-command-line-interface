package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/iotest"
	"time"

	"github.com/alecthomas/kong"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/edition"
	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/fakeplugin"
	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/spool"
)

type result struct {
	code   int
	stdout string
	stderr string
}

// envVars は、コマンドが読み取る環境変数の全て。コマンドの定義（kong のタグ）から収集する（手で
// 列挙すると、指定を追加した際に追記し忘れる）。
// テストを実行する前のパッケージの初期化では生成しない（kong のタグを壊した場合、初期化の panic で
// 全てのテストが実行されなくなる代わりに、使用するテストが名前付きで失敗するように）。
var envVars = sync.OnceValue(func() []string {
	seen := map[string]bool{}
	var names []string
	eachFlag(func(_ *kong.Node, flag *kong.Flag) {
		for _, name := range flag.Envs {
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	})
	return names
})

// eachFlag は、コマンド（kong の葉）ごとに、その指定（親の指定を含む）を順に fn へ渡す。
func eachFlag(fn func(command *kong.Node, flag *kong.Flag)) {
	for _, node := range kong.Must(&cli{}, grammarOptions()...).Model.Leaves(true) {
		for _, group := range node.AllFlags(false) {
			for _, flag := range group {
				fn(node, flag)
			}
		}
	}
}

// opts は、テストが run に与える外部との接点。零値は、何も無いスプール・環境変数なし・空の
// 標準入力・成功する起動。
type opts struct {
	// スプール。空ならば何も無い一時ディレクトリとする（利用者の実際のスプールに触れないように）。
	dir string
	// スプールの場所を導出する関数（env.spool）。dir より優先する。導出できない場合を検証するため。
	locate func() (string, error)
	// 環境変数。envVars に挙げたものの内、ここに無いものは削除する。
	vars map[string]string
	// 標準入力。nil ならば空。
	stdin io.Reader
	// 標準出力。nil ならば書き込んだものを result.stdout に収集する（書き込めない場合を
	// 検証するため）。
	stdout io.Writer
	// 起動（env.start）。nil ならば成功し、渡された引数を started に書き込む。
	start   func(argv []string) error
	started *[]string
	// install / uninstall の場所（env.pluginsDir・env.workDir・env.executable）。空ならば
	// 一時ディレクトリ（executable はその中の擬似ファイル）とする（開発者の計算機の実際の
	// <Plug-Ins>・<Work> に触れないように）。
	plugins    string
	work       string
	executable string
}

func invoke(t *testing.T, o opts, args ...string) result {
	t.Helper()
	if o.locate == nil {
		dir := o.dir
		if dir == "" {
			dir = filepath.Join(t.TempDir(), "none")
		}
		o.locate = func() (string, error) { return dir, nil }
	}
	if o.stdin == nil {
		o.stdin = strings.NewReader("")
	}
	if o.start == nil {
		o.start = func(argv []string) error {
			if o.started != nil {
				*o.started = argv
			}
			return nil
		}
	}
	if o.plugins == "" {
		o.plugins = filepath.Join(t.TempDir(), "Plug-Ins")
	}
	if o.work == "" {
		o.work = filepath.Join(t.TempDir(), "work")
	}
	if o.executable == "" {
		o.executable = filepath.Join(t.TempDir(), edition.Command)
		if err := os.WriteFile(o.executable, nil, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	// 渡した名前がコマンドの読み取らない環境変数ならば（指定の名前を変更したにもかかわらずテストを
	// 修正し忘れた）、黙って破棄せずに失敗させる。
	known := map[string]bool{}
	for _, name := range envVars() {
		known[name] = true
	}
	for name := range o.vars {
		if !known[name] {
			t.Fatalf("no command reads %s", name)
		}
	}
	// kong は環境変数を os から直接読み取るため、テストごとに設定し直す（並行には実行しない）。
	for _, name := range envVars() {
		value, ok := o.vars[name]
		// 終了後に元に戻させる
		t.Setenv(name, value)
		if !ok {
			// 空の値も値となる（空のタイムアウトは使用方法の誤り）ため、渡さないものは削除する。
			os.Unsetenv(name)
		}
	}
	var stdout, stderr bytes.Buffer
	if o.stdout == nil {
		o.stdout = &stdout
	}
	code := run(args, env{
		stdin:      o.stdin,
		stdout:     o.stdout,
		stderr:     &stderr,
		start:      o.start,
		spool:      o.locate,
		workDir:    func() (string, error) { return o.work, nil },
		pluginsDir: func() (string, error) { return o.plugins, nil },
		executable: func() (string, error) { return o.executable, nil },
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
		case "explain":
			return spool.Response{OK: false, Code: spool.CodeInternal, Error: "open", Detail: json.RawMessage(`{"reason":"documents_open"}`)}
		}
		out, _ := json.Marshal(map[string]any{"tool": tool, "args": args})
		return spool.Response{OK: true, Result: out}
	})
	return dir
}

func TestStatusLive(t *testing.T) {
	dir := echoBridge(t)
	r := invoke(t, opts{dir: dir}, "status")
	if r.code != exitOK || !strings.Contains(r.stdout, `"running":true`) {
		t.Fatalf("%+v", r)
	}
}

func TestStatusDown(t *testing.T) {
	r := invoke(t, opts{}, "status")
	if r.code != exitDown || !strings.Contains(r.stdout, `"running":false`) || !strings.Contains(r.stdout, `"reason":"not found"`) {
		t.Fatalf("%+v", r)
	}
}

func TestCallPrintsResult(t *testing.T) {
	dir := echoBridge(t)
	r := invoke(t, opts{dir: dir}, "call", "layers", `{"a":1}`)
	if r.code != exitOK || strings.TrimSpace(r.stdout) != `{"args":{"a":1},"tool":"layers"}` {
		t.Fatalf("%+v", r)
	}
}

// 数値は解釈しないため、float64 に収まらない数値も JSON として正しければそのまま渡す（範囲を
// 検証するのはプラグイン）。
func TestCallPassesNumbersUnchanged(t *testing.T) {
	dir := echoBridge(t)
	r := invoke(t, opts{dir: dir}, "call", "layer_objects", `{"offset":1e400,"limit":2.0000000000000001}`)
	if r.code != exitOK || !strings.Contains(r.stdout, `"offset":1e400`) ||
		!strings.Contains(r.stdout, `"limit":2.0000000000000001`) {
		t.Fatalf("%+v", r)
	}
}

func TestCallArgsFromStdin(t *testing.T) {
	dir := echoBridge(t)
	r := invoke(t, opts{dir: dir, stdin: strings.NewReader(`{"b":2}`)}, "call", "x", "-")
	if r.code != exitOK || !strings.Contains(r.stdout, `"b":2`) {
		t.Fatalf("%+v", r)
	}
}

// 標準入力を読み取れなければ、要求を配置せずに 1 で終了する。応答する擬似プラグインは使用せず、
// ロックのみを保持しておき、終了後に要求も待機標識も残存していないことを確認する。
func TestCallArgsFromUnreadableStdin(t *testing.T) {
	dir, _ := fakeplugin.NewHeldDir(t)
	broken := iotest.ErrReader(errors.New("broken pipe"))
	r := invoke(t, opts{dir: dir, stdin: broken}, "call", "x", "-")
	if r.code != exitFailure || r.stdout != "" || !strings.Contains(r.stderr, "read stdin: broken pipe") {
		t.Fatalf("%+v", r)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if name := entry.Name(); name != spool.LockFile {
			t.Errorf("left %s in the spool", name)
		}
	}
}

// ツールの失敗は、種別ごとの終了コードで終了し、理由を標準エラーに書き込む。標準出力には
// detail があればそれのみを出力し（無ければ何も出力しない）、--raw ならば応答全体を出力する。
func TestCallToolError(t *testing.T) {
	dir := echoBridge(t)
	for _, c := range []struct {
		tool   string
		code   int
		stderr string
		stdout string
		// raw は --raw の出力に含まれるもの。
		raw string
	}{
		{"fail", exitToolFailure, "internal: boom", "", `"code":"internal"`},
		{
			"explain", exitToolFailure, "internal: open",
			"{\"reason\":\"documents_open\"}\n", `"detail":{"reason":"documents_open"}`,
		},
		{"picky", exitToolUsage, "invalid_args: no such argument", "", `"code":"invalid_args"`},
	} {
		t.Run(c.tool, func(t *testing.T) {
			r := invoke(t, opts{dir: dir}, "call", c.tool)
			if r.code != c.code || r.stdout != c.stdout || !strings.Contains(r.stderr, c.stderr) {
				t.Fatalf("%+v", r)
			}
			raw := invoke(t, opts{dir: dir}, "call", "--raw", c.tool)
			if raw.code != c.code || !strings.Contains(raw.stdout, `"ok":false`) || !strings.Contains(raw.stdout, c.raw) {
				t.Fatalf("%+v", raw)
			}
		})
	}
}

// failingWriter は書き込めない標準出力（ディスクが一杯・読み取る側がパイプを閉じた）を模倣する。
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("no space left on device") }

// 成功したツールの結果を書き込めなければ、0 ではなく失敗で終了する。ツールが失敗した場合は、
// detail を書き込めなくてもツールの終了コードを保持する（書き込めなかったことと、ツールの失敗の
// 理由は標準エラーに書き込む）。
func TestCallCannotWriteOutput(t *testing.T) {
	dir := echoBridge(t)
	for _, c := range []struct {
		args []string
		code int
		// stderr は、書き込めなかったこと以外に標準エラーに含まれるもの。
		stderr string
	}{
		{[]string{"call", "ping"}, exitFailure, ""},
		{[]string{"call", "--raw", "ping"}, exitFailure, ""},
		{[]string{"call", "explain"}, exitToolFailure, "internal: open"},
		{[]string{"call", "--raw", "explain"}, exitToolFailure, "internal: open"},
	} {
		r := invoke(t, opts{dir: dir, stdout: failingWriter{}}, c.args...)
		if r.code != c.code || !strings.Contains(r.stderr, "write output: no space left on device") ||
			!strings.Contains(r.stderr, c.stderr) {
			t.Errorf("%v: %+v", c.args, r)
		}
	}
}

func TestCallUsageErrors(t *testing.T) {
	for _, args := range [][]string{{"call"}, {"call", "x", "[1]"}, {"call", "x", "not json"}, {"call", "x", "null"}, {"call", "x", `{"a":1e}`}, {"tools", "x"}, {"nope"}, {}} {
		r := invoke(t, opts{}, args...)
		if r.code != exitUsage {
			t.Errorf("%v: code %d", args, r.code)
		}
	}
}

// tools は予約ツール @tools を呼び出し、その結果を出力する。call は名前をそのまま渡すため、
// call tools は予約ツールではなく、名前が tools のツールを呼び出す（予約ツールの名前は @ で始まり、
// ツールの一覧の名前と重複しない）。
func TestTools(t *testing.T) {
	dir := echoBridge(t)
	r := invoke(t, opts{dir: dir}, "tools")
	if r.code != exitOK || strings.TrimSpace(r.stdout) != `{"args":{},"tool":"`+spool.ToolTools+`"}` {
		t.Fatalf("%+v", r)
	}
	r = invoke(t, opts{dir: dir}, "call", "tools")
	if r.code != exitOK || strings.TrimSpace(r.stdout) != `{"args":{},"tool":"tools"}` {
		t.Fatalf("call tools should call a tool named tools: %+v", r)
	}
}

func TestCallWhenDown(t *testing.T) {
	r := invoke(t, opts{}, "call", "ping")
	if r.code != exitDown {
		t.Fatalf("%+v", r)
	}
}

func TestLaunchSkipsWhenLive(t *testing.T) {
	dir := echoBridge(t)
	var started []string
	r := invoke(t, opts{dir: dir, started: &started}, "launch")
	if r.code != exitOK || started != nil || !strings.Contains(r.stdout, `"launched":false`) {
		t.Fatalf("%+v %v", r, started)
	}
}

func TestLaunchStartsApp(t *testing.T) {
	app := fakeApp(t)
	var started []string
	r := invoke(t, opts{vars: map[string]string{edition.EnvPrefix + "APP": app}, started: &started}, "launch")
	if r.code != exitOK || len(started) != 1 || started[0] != app {
		t.Fatalf("%+v %v", r, started)
	}
}

func TestLaunchStartFails(t *testing.T) {
	app := fakeApp(t)
	failed := func([]string) error { return errors.New("exec format error") }
	r := invoke(t, opts{vars: map[string]string{edition.EnvPrefix + "APP": app}, start: failed}, "launch")
	if r.code != exitFailure || r.stdout != "" || !strings.Contains(r.stderr, "exec format error") {
		t.Fatalf("%+v", r)
	}
}

// 起動対象を決定できなければ、起動を試みずに失敗する。
func TestLaunchAppNotFound(t *testing.T) {
	var started []string
	missing := filepath.Join(t.TempDir(), "missing")
	vars := map[string]string{edition.EnvPrefix + "APP": missing}
	r := invoke(t, opts{vars: vars, started: &started}, "launch")
	if r.code != exitFailure || started != nil || !strings.Contains(r.stderr, missing) ||
		!strings.Contains(r.stderr, "point --app ("+edition.EnvPrefix+"APP) to") {
		t.Fatalf("%+v %v", r, started)
	}
}

// fakeApp は起動の対象とする実行ファイルを作成する（launch.Command はファイルが存在することのみを
// 確認する）。
func fakeApp(t *testing.T) string {
	t.Helper()
	app := filepath.Join(t.TempDir(), "fake-vw")
	if err := os.WriteFile(app, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return app
}

// version と --version は同じものを出力する。
func TestVersion(t *testing.T) {
	for _, arg := range []string{"version", "--version"} {
		r := invoke(t, opts{}, arg)
		if r.code != exitOK || !strings.Contains(r.stdout, fmt.Sprintf(`"protocol":%d`, spool.ProtocolVersion)) {
			t.Fatalf("%s: %+v", arg, r)
		}
	}
}

func TestCallWhileBusyTimesOut(t *testing.T) {
	dir, _ := fakeplugin.NewHeldDir(t)
	r := invoke(t, opts{dir: dir}, "call", "ping", "--timeout", "0.3")
	if r.code != exitTimeout {
		t.Fatalf("%+v", r)
	}
}

// 終了を待機する呼び出し元は status を繰り返すため、応答しなくてもロックが保持されている間は
// 実行中と応答し、解放されたら停止したと応答する（保存の確認のダイアログを停止と誤認しない）。
func TestStatusWhileBusyUntilTheProcessEnds(t *testing.T) {
	dir, quit := fakeplugin.NewHeldDir(t)
	r := invoke(t, opts{dir: dir}, "status")
	if r.code != exitOK || !strings.Contains(r.stdout, `"running":true`) {
		t.Fatalf("a held lock must count as running: %+v", r)
	}
	quit()
	r = invoke(t, opts{dir: dir}, "status")
	if r.code != exitDown || !strings.Contains(r.stdout, `"running":false`) {
		t.Fatalf("%+v", r)
	}
}

func TestLaunchDoesNotTakeTimeout(t *testing.T) {
	r := invoke(t, opts{}, "launch", "--timeout", "5")
	if r.code != exitUsage {
		t.Fatalf("%+v", r)
	}
}

func TestHelp(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"--help"}, {"-h"}} {
		r := invoke(t, opts{}, args...)
		if r.code != exitOK || !strings.Contains(r.stdout, "call") || !strings.Contains(r.stdout, "exit with") {
			t.Fatalf("%v: %+v", args, r)
		}
	}
	for _, args := range [][]string{{"help", "call"}, {"call", "--help"}, {"call", "x", "-h"}} {
		r := invoke(t, opts{}, args...)
		if r.code != exitOK || !strings.Contains(r.stdout, "--timeout") || !strings.Contains(r.stdout, "withdraws the request") {
			t.Fatalf("%v: %+v", args, r)
		}
	}
	// 引数なしは使用方法の誤りであり、ヘルプは標準エラーへ。
	if r := invoke(t, opts{}); r.code != exitUsage || r.stdout != "" ||
		!strings.Contains(r.stderr, "Usage:") {
		t.Fatalf("%+v", r)
	}
	if r := invoke(t, opts{}, "help", "nope"); r.code != exitUsage {
		t.Fatalf("%+v", r)
	}
}

// TestEveryCommandRuns は、kong に列挙したコマンドが全て command を満たすことを検証する
// （満たさなければ run の型アサーションが実行時に失敗する）。
func TestEveryCommandRuns(t *testing.T) {
	for _, node := range kong.Must(&cli{}, grammarOptions()...).Model.Leaves(true) {
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
		spool.ErrOccupied:   exitBusy,
		spool.ErrNoSession:  exitBusy,
		spool.ErrNoResponse: exitNoResponse,
		// 区別すべき結果でなければ汎用の失敗
		spool.ErrMalformedResponse: exitFailure,
		errors.New("disk full"):    exitFailure,
	} {
		if got := codeFor(fmt.Errorf("%w (detail)", err)); got != want {
			t.Errorf("%v: got %d, want %d", err, got, want)
		}
	}
}

// session start がセッション ID を出力し、占有の中でのみ呼び出せるツール（quit）はその
// セッション ID の call のみが実行される。読み取りツール（layers・ping）は誰の call でも
// 実行される。end の後は quit が占有の外として拒否される。
func TestSessionStartCallEnd(t *testing.T) {
	dir := echoBridge(t)
	r := invoke(t, opts{dir: dir}, "session", "start")
	var started struct {
		Session string `json:"session"`
	}
	if r.code != exitOK || json.Unmarshal([]byte(r.stdout), &started) != nil || started.Session == "" {
		t.Fatalf("%+v", r)
	}
	if r := invoke(t, opts{dir: dir}, "session", "start"); r.code != exitBusy {
		t.Fatalf("a second session should be refused: %+v", r)
	}
	if r := invoke(t, opts{dir: dir}, "call", "quit"); r.code != exitBusy {
		t.Fatalf("quit outside the session should be refused: %+v", r)
	}
	if r := invoke(t, opts{dir: dir}, "call", "layers"); r.code != exitOK {
		t.Fatalf("a read tool is for everyone: %+v", r)
	}
	if r := invoke(t, opts{dir: dir}, "call", "ping"); r.code != exitOK {
		t.Fatalf("ping is for everyone: %+v", r)
	}
	inSession := map[string]string{edition.EnvPrefix + "SESSION": started.Session}
	if r := invoke(t, opts{dir: dir, vars: inSession}, "call", "quit"); r.code != exitOK {
		t.Fatalf("quit in the session (from "+edition.EnvPrefix+"SESSION): %+v", r)
	}
	if r := invoke(t, opts{dir: dir, vars: inSession}, "session", "end"); r.code != exitOK ||
		strings.TrimSpace(r.stdout) != `{"ended":true}` {
		t.Fatalf("%+v", r)
	}
	if r := invoke(t, opts{dir: dir, vars: inSession}, "call", "quit"); r.code != exitBusy {
		t.Fatalf("the ended session should be refused: %+v", r)
	}
	if r := invoke(t, opts{dir: dir}, "call", "quit"); r.code != exitBusy {
		t.Fatalf("quit outside a session should be refused: %+v", r)
	}
}

func TestSessionEndNeedsSession(t *testing.T) {
	dir := echoBridge(t)
	if r := invoke(t, opts{dir: dir}, "session", "end"); r.code != exitUsage {
		t.Fatalf("%+v", r)
	}
}

// 終了コードは呼び出し元との取り決めであるため、定数の名前ではなく番号そのものを固定する。
func TestExitCodeNumbers(t *testing.T) {
	for name, c := range map[string][2]int{
		"OK":          {exitOK, 0},
		"Failure":     {exitFailure, 1},
		"Usage":       {exitUsage, 2},
		"Timeout":     {exitTimeout, 3},
		"NoResponse":  {exitNoResponse, 4},
		"Down":        {exitDown, 5},
		"Running":     {exitRunning, 6},
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

// 指定は環境変数でも与えられ、両方あれば引数が優先される。
func TestFlagsFromEnv(t *testing.T) {
	dir := echoBridge(t)
	raw := map[string]string{edition.EnvPrefix + "CALL_RAW": "true"}
	if r := invoke(t, opts{dir: dir, vars: raw}, "call", "fail"); r.code != exitToolFailure ||
		!strings.Contains(r.stdout, `"code":"internal"`) {
		t.Fatalf(edition.EnvPrefix+"CALL_RAW: %+v", r)
	}
	r := invoke(t, opts{dir: dir, vars: raw}, "call", "fail", "--no-raw")
	if r.code != exitToolFailure || r.stdout != "" {
		t.Fatalf("--no-raw over "+edition.EnvPrefix+"CALL_RAW: %+v", r)
	}

	busy, _ := fakeplugin.NewHeldDir(t)
	short := map[string]string{edition.EnvPrefix + "CALL_TIMEOUT": "0.3"}
	if r := invoke(t, opts{dir: busy, vars: short}, "call", "ping"); r.code != exitTimeout {
		t.Fatalf(edition.EnvPrefix+"CALL_TIMEOUT: %+v", r)
	}
	long := map[string]string{edition.EnvPrefix + "CALL_TIMEOUT": "60"}
	r = invoke(t, opts{dir: busy, vars: long}, "call", "ping", "--timeout", "0.3")
	if r.code != exitTimeout {
		t.Fatalf("--timeout over "+edition.EnvPrefix+"CALL_TIMEOUT: %+v", r)
	}
	session := map[string]string{edition.EnvPrefix + "SESSION_TIMEOUT": "0.3"}
	if r := invoke(t, opts{dir: busy, vars: session}, "session", "start"); r.code != exitTimeout {
		t.Fatalf(edition.EnvPrefix+"SESSION_TIMEOUT: %+v", r)
	}
	empty := map[string]string{edition.EnvPrefix + "CALL_TIMEOUT": ""}
	if r := invoke(t, opts{dir: busy, vars: empty}, "call", "ping"); r.code != exitUsage {
		t.Fatalf("an empty timeout: %+v", r)
	}
}

// スプールの場所を変更する指定は無い（プラグインはモジュールの場所から導出した
// <Plug-Ins>/cli/spool のみを参照するため、別の場所を指定できても要求が届かない）。環境変数を
// 読み取らないことは spool の TestDirIgnoresEnvironment。
func TestSpoolFlagIsUsageError(t *testing.T) {
	if r := invoke(t, opts{}, "status", "--spool", t.TempDir()); r.code != exitUsage {
		t.Fatalf("%+v", r)
	}
}

// 停止していると応答する場合、導出したスプールの場所を標準エラーに示す（フォルダの外で実行した
// vw2026 が、場所の取り違えと区別できるように）。status は標準出力の JSON の spool にも示す。
func TestNotRunningShowsTheSpool(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "spool")
	for _, args := range [][]string{
		{"call", "ping"},
		{"tools"},
		{"session", "start"},
		{"session", "end", "--session", "x"},
	} {
		r := invoke(t, opts{dir: dir}, args...)
		if r.code != exitDown || r.stdout != "" || !strings.Contains(r.stderr, dir) {
			t.Errorf("%v: %+v", args, r)
		}
	}
	quoted, _ := json.Marshal(dir)
	r := invoke(t, opts{dir: dir}, "status")
	if r.code != exitDown || !strings.Contains(r.stdout, `"spool":`+string(quoted)) ||
		!strings.Contains(r.stderr, dir) {
		t.Errorf("status: %+v", r)
	}
}

// 待機の間にブリッジが停止した（ロックが解放された）場合も、導出したスプールの場所を示して 5 で
// 終了する。
func TestCallStoppedWhileWaitingShowsTheSpool(t *testing.T) {
	dir, quit := fakeplugin.NewHeldDir(t)
	stopped := make(chan struct{})
	// 要求が配置されてから（call が待機を開始してから）ロックを解放する。call はロックを締切の
	// 時点で確認するため、締切より前に解放すればよく、締切には余裕を持たせる。
	go func() {
		defer close(stopped)
		for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
			if requests, _ := filepath.Glob(filepath.Join(dir, "*"+spool.RequestSuffix)); len(requests) > 0 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		quit()
	}()
	r := invoke(t, opts{dir: dir}, "call", "ping", "--timeout", "2")
	// quit の呼び出しの完了を待機する（テストの後処理も quit を呼び出すため、競合させない）。
	<-stopped
	if r.code != exitDown || r.stdout != "" || !strings.Contains(r.stderr, "stopped while waiting") ||
		!strings.Contains(r.stderr, dir) {
		t.Fatalf("%+v", r)
	}
}

// スプールの場所を導出できなければ（実行ファイルの場所が不明など）、スプールを参照する
// コマンドはいずれの箇所で失敗したかと理由を標準エラーに書き込んで 1 で終了する。
func TestSpoolNotLocated(t *testing.T) {
	locate := func() (string, error) {
		return "", errors.New("lstat /nowhere/bin/" + edition.Command + ": no such file or directory")
	}
	for _, args := range [][]string{
		{"status"},
		{"call", "ping"},
		{"tools"},
		{"launch"},
		{"session", "start"},
		{"session", "end", "--session", "x"},
	} {
		r := invoke(t, opts{locate: locate}, args...)
		want := edition.Command + ": cannot locate the spool: lstat /nowhere/bin/" + edition.Command + ": no such file or directory"
		if r.code != exitFailure || r.stdout != "" || !strings.Contains(r.stderr, want) {
			t.Errorf("%v: %+v", args, r)
		}
	}
}

// TestEveryFlagHasEnv は、ヘルプと隠し指定を除く全ての指定に環境変数があり、オン・オフの指定が
// --no-<flag> を持つことを検証する（指定を追加して環境変数を付け忘れないように。invoke が設定し直す
// 環境変数は、envVars がコマンドの定義から収集する）。
func TestEveryFlagHasEnv(t *testing.T) {
	eachFlag(func(node *kong.Node, flag *kong.Flag) {
		if flag.Name == "help" {
			return
		}
		// 隠し指定（初回スクリプトのみが一時フォルダの CLI に渡す --plugins-dir）は、環境変数を
		// 持たない（持つと、インストールされた CLI にも作用してしまう）。
		if flag.Hidden {
			if len(flag.Envs) != 0 {
				t.Errorf("%s --%s is hidden but has an environment variable", node.Path(), flag.Name)
			}
			return
		}
		// オン・オフの指定は --no-<flag> で打ち消せるようにする（環境変数でオンにしたものを戻すため）。
		if flag.IsBool() && flag.Tag.Negatable == "" {
			t.Errorf("%s --%s is not negatable (add negatable:\"\")", node.Path(), flag.Name)
		}
		if len(flag.Envs) == 0 {
			t.Errorf("%s --%s has no environment variable", node.Path(), flag.Name)
		}
	})
}

// TestEnvNamesDoNotCollide は、VW2026_<コマンド>_ で始まる環境変数を、そのコマンド（とその下の
// コマンド）の指定のみが使用することを検証する。<コマンド> はいずれの深さのコマンドでもよい
// （session も session end も）。全体で使用する値に VW2026_SESSION_X のような名前を付けると、
// session コマンドの指定 X と区別できなくなるため（CLAUDE.md「Go」）。
func TestEnvNamesDoNotCollide(t *testing.T) {
	app := kong.Must(&cli{}, grammarOptions()...)
	// コマンドの経路（"session end"）ごとの接頭辞（"VW2026_SESSION_END_"）。
	prefixes := map[string]string{}
	var walk func(nodes []*kong.Node)
	walk = func(nodes []*kong.Node) {
		for _, node := range nodes {
			path := node.Path()
			prefixes[path] = edition.EnvPrefix + strings.ToUpper(strings.NewReplacer(" ", "_", "-", "_").Replace(path)) + "_"
			walk(node.Children)
		}
	}
	walk(app.Model.Children)
	eachFlag(func(leaf *kong.Node, flag *kong.Flag) {
		for _, name := range flag.Envs {
			for path, prefix := range prefixes {
				within := leaf.Path() == path || strings.HasPrefix(leaf.Path(), path+" ")
				if strings.HasPrefix(name, prefix) && !within {
					t.Errorf("%s --%s uses %s, which belongs to %s", leaf.Path(), flag.Name, name, path)
				}
			}
		}
	})
}

// 本番環境における外部との接点は、いずれも設定されている（main は run に渡すのみ）。場所を導出する
// 関数は呼び出さない（開発者の計算機の実際の場所を導出するのみであっても、テストからは
// 触れないようにする）。
func TestDefaultEnv(t *testing.T) {
	e := defaultEnv()
	if e.stdin != os.Stdin || e.stdout != os.Stdout || e.stderr != os.Stderr || e.start == nil ||
		e.spool == nil || e.workDir == nil || e.pluginsDir == nil || e.executable == nil {
		t.Fatalf("%+v", e)
	}
}
