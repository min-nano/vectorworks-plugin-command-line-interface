package main

// The command line is parsed by kong. Each command is a struct whose tags and
// Help method describe it next to its Run method; kong builds the help output
// from them, and docgen.go builds doc.go (go generate) from the same model.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/alecthomas/kong"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/launch"
	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/spool"
)

// version はビルド時に -ldflags "-X main.version=…" で埋め込む。
var version = "dev"

// 終了コード（cli.Help の一覧と対）。
const (
	exitOK      = 0
	exitToolErr = 1 // 道具が失敗を返した（応答は届いている）
	exitUsage   = 2 // 使い方の誤り
	exitDown    = 3 // ブリッジが見つからない
	// 待ちきれなかった。Vectorworks は動いている（モーダル・undo の記録の最中など）。
	// exitDown と分けるのは、呼ぶ側が起動し直さずに待てばよいと判定できるように。
	exitTimeout = 4
	// 5 は使わない（protocol 2 までは「作法の版が違う」だった。呼ぶ側の分岐を変えないよう
	// 番号を詰めない）。
	exitFailure = 6 // そのほか（書き込めない・起動できない等）
	// 7 は install / uninstall（未実装）が「Vectorworks が動いているので行えない」に使う。
)

// exitStatus carries an exit code out of a Run method, which kong lets
// return only an error.
type exitStatus int

func (s exitStatus) Error() string { return fmt.Sprintf("exit status %d", int(s)) }

// status turns an exit code into the error a Run method returns.
func status(code int) error {
	if code == exitOK {
		return nil
	}
	return exitStatus(code)
}

type env struct {
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
	start  func(argv []string) error
}

func main() {
	os.Exit(run(os.Args[1:], env{
		stdin:  os.Stdin,
		stdout: os.Stdout,
		stderr: os.Stderr,
		start:  launch.Start,
	}))
}

// --- the command line -------------------------------------------------------

// cli is the whole command line. Its Help method opens "vw2026 --help" and
// doc.go.
type cli struct {
	globals

	Status  statusCmd  `cmd:"" help:"Report whether the bridge is running."`
	Call    callCmd    `cmd:"" help:"Call one tool."`
	Wait    waitCmd    `cmd:"" help:"Wait until the bridge starts or stops."`
	Launch  launchCmd  `cmd:"" help:"Start Vectorworks."`
	Version versionCmd `cmd:"" help:"Print the versions of the CLI and the protocol."`
}

// globals are the flags every command takes.
type globals struct {
	Spool string `help:"Spool directory. The default is <CLI>/spool; another one is for tests, as the plug-in never reads it." env:"VW2026_SPOOL" placeholder:"DIR"`
}

func (cli) Help() string {
	return `
Vw2026 sends tool calls, one at a time, to the bridge that the cli plug-in
runs inside Vectorworks 2026.

It is a primitive: each run does exactly what it is told once. It does not
tell sessions apart, take exclusive use of Vectorworks, lock across
sessions, retry, or interpret the tools; the caller does those
(docs/design.md). The bridge and vw2026 exchange files in a spool by the
protocol in docs/protocol.md.

Each command prints one line of JSON to the standard output, writes the
reason for a failure to the standard error, and reports the outcome in its
exit status. Flags may come before or after the positional arguments.

	vw2026 status
	vw2026 call tools
	vw2026 launch && vw2026 wait

To update the plug-in, the caller combines the commands (install is not
implemented yet; see docs/plugin/install-and-update.md):

	vw2026 call quit && vw2026 wait --down && vw2026 install && vw2026 launch && vw2026 wait

The commands exit with

	0  success
	1  the tool reported a failure (reason on the standard error; with --raw also on the standard output)
	2  wrong usage
	3  the bridge is not running
	4  timed out (call withdrew its request; Vectorworks is running)
	5  unused (meant "protocol mismatch" up to protocol 2; the numbers are not reused)
	6  any other failure (cannot write, cannot start, cannot locate the spool)
	7  Vectorworks is running, so install or uninstall did nothing (planned)

Vectorworks can be running, holding the lock, while the plug-in defers the
requests, for example during a modal dialog or while undo is being recorded
(docs/protocol.md). Status does not tell this state apart; call and "wait
--down" wait up to --timeout and then exit with 4. Recover from 3 and 4
differently: 3 means Vectorworks is not running, while 4 means it is running
but did not answer in time, so launching it again or reinstalling the plug-in
does not help.
`
}

// newParser builds the kong parser. Help and parse errors go to the writers of
// e, and kong's exits unwind to run through exitPanic so tests can observe them.
func newParser(grammar *cli, e env) (*kong.Kong, error) {
	return kong.New(grammar,
		kong.Name("vw2026"),
		kong.Writers(e.stdout, e.stderr),
		kong.Exit(func(code int) { panic(exitPanic(code)) }),
		kong.ConfigureHelp(kong.HelpOptions{WrapUpperBound: 80}),
		kong.Bind(&e),
	)
}

type exitPanic int

func run(args []string, e env) (code int) {
	switch {
	case len(args) == 0:
		// A bare "vw2026" is a usage error: show the help on the standard error.
		e.stdout = e.stderr
		defer func() { code = exitUsage }()
		args = []string{"--help"}
	case args[0] == "help":
		// "vw2026 help [<command>]" is "vw2026 [<command>] --help".
		args = append(args[1:len(args):len(args)], "--help")
	case args[0] == "--version":
		args[0] = "version"
	}

	defer func() {
		if r := recover(); r != nil {
			p, ok := r.(exitPanic)
			if !ok {
				panic(r)
			}
			code = int(p)
		}
	}()

	var grammar cli
	parser, err := newParser(&grammar, e)
	if err != nil {
		fmt.Fprintf(e.stderr, "vw2026: %v\n", err)
		return exitFailure
	}
	ctx, err := parser.Parse(args)
	if err != nil {
		fmt.Fprintf(e.stderr, "vw2026: %v (run \"vw2026 --help\")\n", err)
		return exitUsage
	}
	err = ctx.Run(&grammar.globals)
	var s exitStatus
	switch {
	case err == nil:
		return exitOK
	case errors.As(err, &s):
		return int(s)
	default:
		fmt.Fprintf(e.stderr, "vw2026: %v\n", err)
		return exitFailure
	}
}

// open はスプールを決めて、ブリッジの状態を判定する。場所が決まらなければ標準エラーへ
// 理由を書いて nil。
func (g *globals) open(e *env) *spool.Bridge {
	dir := g.Spool
	if dir == "" {
		var err error
		if dir, err = spool.DefaultDir(); err != nil {
			fmt.Fprintf(e.stderr, "vw2026: %v (set VW2026_SPOOL)\n", err)
			return nil
		}
	}
	return spool.Open(dir)
}

func seconds(value float64) time.Duration {
	return time.Duration(value * float64(time.Second))
}

// --- status -----------------------------------------------------------------

type statusCmd struct{}

func (statusCmd) Help() string {
	return `
Status reports whether the bridge is running. The answer depends only on
whether the plug-in holds the lock file (bridge.lock) in the spool; see
docs/protocol.md. Status does not tell a busy Vectorworks from a responsive
one. Use "vw2026 call ping" for the version of the plug-in.

It prints one of

	{"running":true,"spool":"..."}
	{"running":false,"spool":"...","reason":"..."}

and exits with 3 when the bridge is not running.
`
}

func (statusCmd) Run(g *globals, e *env) error {
	bridge := g.open(e)
	if bridge == nil {
		return status(exitFailure)
	}
	code := emit(e, bridgeJSON(bridge))
	if !bridge.Running {
		fmt.Fprintln(e.stderr, "vw2026: the bridge is not running (is Vectorworks started with the plug-in?)")
		return status(exitDown)
	}
	return status(code)
}

// bridgeJSON はブリッジの状態の出力（status / wait / launch で同じ形）。
func bridgeJSON(bridge *spool.Bridge) map[string]any {
	out := map[string]any{
		"running": bridge.Running,
		"spool":   bridge.Dir,
	}
	if !bridge.Running {
		out["reason"] = bridge.Reason
	}
	return out
}

// --- call -------------------------------------------------------------------

type callCmd struct {
	Tool    string  `arg:"" help:"Tool to call (\"vw2026 call tools\" lists them)."`
	Args    string  `arg:"" optional:"" name:"args" help:"Arguments as a JSON object, or - to read them from the standard input."`
	Raw     bool    `help:"Print the whole response (ok, result, and error)."`
	Timeout float64 `default:"30" placeholder:"SECONDS" help:"How long to wait for the response (default ${default})."`
}

func (callCmd) Help() string {
	return `
Call calls one tool of the bridge and prints its result. "vw2026 call tools"
lists the tools; docs/plugin/tools.md describes them.

The arguments are a JSON object. "-" reads them from the standard input.
Without them, the tool is called with no arguments. Call does not check the
arguments against the tool; the plug-in does.

	vw2026 call layers '{"include_sheets":false}'
	echo '{"layer":"1F"}' | vw2026 call layer_objects -

On success, call prints the result of the tool. With --raw it prints the
whole response. When the tool fails, call writes the reason to the standard
error and exits with 1. When --timeout runs out, call withdraws the request
and exits with 4.
`
}

func (c *callCmd) Run(g *globals, e *env) error {
	var payload json.RawMessage
	if c.Args != "" {
		text := c.Args
		if text == "-" {
			data, err := io.ReadAll(e.stdin)
			if err != nil {
				fmt.Fprintf(e.stderr, "vw2026: read stdin: %v\n", err)
				return status(exitFailure)
			}
			text = string(data)
		}
		// 中身は道具ごとに解釈しない（プラグイン側の仕事）。オブジェクトであることだけを確かめる。
		var probe map[string]any
		if json.Unmarshal([]byte(text), &probe) != nil || probe == nil {
			fmt.Fprint(e.stderr, "vw2026: args must be a JSON object\n")
			return status(exitUsage)
		}
		payload = json.RawMessage(strings.TrimSpace(text))
	}

	bridge := g.open(e)
	if bridge == nil {
		return status(exitFailure)
	}
	if !bridge.Running {
		fmt.Fprintln(e.stderr, "vw2026: the bridge is not running (try `vw2026 status`)")
		return status(exitDown)
	}
	response, err := bridge.Call(c.Tool, payload, seconds(c.Timeout))
	if err != nil {
		fmt.Fprintf(e.stderr, "vw2026: %v\n", err)
		return status(codeFor(err))
	}
	if c.Raw {
		_ = emit(e, response)
	} else if response.OK {
		_ = emitRaw(e, response.Result)
	}
	if !response.OK {
		fmt.Fprintf(e.stderr, "vw2026: %s: %s\n", c.Tool, response.Error)
		return status(exitToolErr)
	}
	return nil
}

func codeFor(err error) int {
	switch {
	case errors.Is(err, spool.ErrNotRunning):
		return exitDown
	case errors.Is(err, spool.ErrTimeout):
		return exitTimeout
	default:
		return exitFailure
	}
}

// --- wait -------------------------------------------------------------------

type waitCmd struct {
	Down    bool    `help:"Wait until the bridge stops instead."`
	Timeout float64 `default:"120" placeholder:"SECONDS" help:"How long to wait (default ${default})."`
}

func (waitCmd) Help() string {
	return `
Wait waits until the bridge starts, that is, until the plug-in takes the lock.
With --down it waits until the bridge stops (the lock is released). It prints
the same output as status, or exits with 4 when --timeout runs out.

Vectorworks keeps the lock while it shows the dialog that asks to save the
drawings, so "wait --down" does not take that dialog for a stop.
`
}

func (c *waitCmd) Run(g *globals, e *env) error {
	deadline := time.Now().Add(seconds(c.Timeout))
	for {
		// 動き出す・止まるはロックだけで決まる。保存の確認のダイアログを開いている間も
		// ロックは掴まれたままなので、--down がそれを「止まった」と誤らない。
		bridge := g.open(e)
		if bridge == nil {
			return status(exitFailure)
		}
		if bridge.Running != c.Down {
			return status(emit(e, bridgeJSON(bridge)))
		}
		if time.Now().After(deadline) {
			fmt.Fprintln(e.stderr, "vw2026: timed out waiting")
			return status(exitTimeout)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// --- launch -----------------------------------------------------------------

type launchCmd struct {
	App string `env:"VW2026_APP" placeholder:"NAME-OR-PATH" help:"Application or executable to start."`
}

func (launchCmd) Help() string {
	return `
Launch starts Vectorworks unless the bridge is already running. It does not
wait for the bridge; run "vw2026 wait" next to wait for it.

	vw2026 launch && vw2026 wait

It prints one of

	{"launched":true,"command":[...]}
	{"running":true,"spool":"...","launched":false}

The default application is "Vectorworks 2026" on macOS, started with
"open -a", and %ProgramFiles%\Vectorworks 2026\Vectorworks2026.exe on
Windows. Launch does not search other places; name them with --app. A value
that does not end in .app is started as an executable.

When Vectorworks is running but the bridge is not (the plug-in is not
installed), launch exits with 3.
`
}

// Run does not wait: waiting is the caller's choice
// (docs/design.md「CLI はプリミティブに保つ」).
func (c *launchCmd) Run(g *globals, e *env) error {
	bridge := g.open(e)
	if bridge == nil {
		return status(exitFailure)
	}
	if bridge.Running {
		out := bridgeJSON(bridge)
		out["launched"] = false
		return status(emit(e, out))
	}
	argv, err := launch.Command(c.App)
	if err != nil {
		fmt.Fprintf(e.stderr, "vw2026: %v\n", err)
		return status(exitFailure)
	}
	if err := e.start(argv); err != nil {
		if errors.Is(err, launch.ErrAlreadyRunning) {
			fmt.Fprintln(e.stderr, "vw2026: Vectorworks is running but the bridge is not (is the plug-in installed?)")
			return status(exitDown)
		}
		fmt.Fprintf(e.stderr, "vw2026: launch: %v\n", err)
		return status(exitFailure)
	}
	return status(emit(e, map[string]any{"launched": true, "command": argv}))
}

// --- version ----------------------------------------------------------------

type versionCmd struct{}

func (versionCmd) Help() string {
	return `
Version prints the version of this CLI and the version of the protocol it
speaks (docs/protocol.md).

	{"version":"abc1234","protocol":3}
`
}

func (versionCmd) Run(e *env) error {
	return status(emit(e, map[string]any{"version": version, "protocol": spool.ProtocolVersion}))
}

// --- 出力 -------------------------------------------------------------------

func emit(e *env, value any) int {
	data, err := json.Marshal(value)
	if err != nil {
		fmt.Fprintf(e.stderr, "vw2026: encode output: %v\n", err)
		return exitFailure
	}
	return emitRaw(e, data)
}

func emitRaw(e *env, data []byte) int {
	if len(data) == 0 {
		data = []byte("{}")
	}
	fmt.Fprintf(e.stdout, "%s\n", data)
	return exitOK
}
