// The usage of each command is written in its *command definition next to the
// function that runs it. help.go builds the usage summary, "vw2026 help", and
// doc.go (go generate) from those definitions.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/launch"
	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/spool"
)

// version はビルド時に -ldflags "-X main.version=…" で埋め込む。
var version = "dev"

// 終了コード（help.go の exitStatusTopic と対）。
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

type env struct {
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
	getenv func(string) string
	start  func(argv []string) error
}

func main() {
	os.Exit(run(os.Args[1:], env{
		stdin:  os.Stdin,
		stdout: os.Stdout,
		stderr: os.Stderr,
		getenv: os.Getenv,
		start:  launch.Start,
	}))
}

func run(args []string, e env) int {
	if len(args) == 0 {
		printUsage(e.stderr)
		return exitUsage
	}
	name, rest := args[0], args[1:]
	switch name {
	case "help", "-h", "--help":
		return cmdHelp(rest, e)
	case "--version":
		name = "version"
	}
	cmd := lookup(name)
	if cmd == nil || cmd.Run == nil {
		fmt.Fprintf(e.stderr, "vw2026: unknown command %q\n\n", name)
		printUsage(e.stderr)
		return exitUsage
	}
	return cmd.Run(cmd, rest, e)
}

// common はどのコマンドにもある指定。
type common struct {
	spool   string
	timeout float64
}

// newFlags は指定を用意する。defaultTimeout が 0 なら --timeout を持たない（launch・status）。
// -h で出す使い方は cmd の定義から作る。
func newFlags(cmd *command, e env, defaultTimeout float64) (*flag.FlagSet, *common) {
	c := &common{}
	fs := flag.NewFlagSet(cmd.Name(), flag.ContinueOnError)
	fs.SetOutput(e.stderr)
	fs.Usage = func() { printFlagUsage(cmd, fs) }
	fs.StringVar(&c.spool, "spool", e.getenv("VW2026_SPOOL"), "spool directory")
	if defaultTimeout > 0 {
		fs.Float64Var(&c.timeout, "timeout", defaultTimeout, "seconds")
	}
	return fs, c
}

// open はスプールを決めて、ブリッジの状態を判定する。場所が決まらなければ標準エラーへ
// 理由を書いて nil。
func (c *common) open(e env) *spool.Bridge {
	dir := c.spool
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

// parseInterspersed は位置引数と指定が混ざっていても受け付ける（`call vw.layers --timeout 5`）。
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return positional, nil
		}
		positional = append(positional, args[0])
		args = args[1:]
	}
}

// --- status -----------------------------------------------------------------

var statusCommand = &command{
	UsageLine: "vw2026 status [--spool <dir>]",
	Short:     "report whether the bridge is running",
	Long: `
Status reports whether the bridge is running. The answer depends only on
whether the plug-in holds the lock file (bridge.lock) in the spool; see
docs/protocol.md. Status does not tell a busy Vectorworks from a responsive
one (see "vw2026 help exit-status"). Use "vw2026 call ping" for the version
of the plug-in.

It prints one of

	{"running":true,"spool":"..."}
	{"running":false,"spool":"...","reason":"..."}

and exits with 3 when the bridge is not running.

The --spool flag selects the spool; see "vw2026 help spool".
`,
	Run: cmdStatus,
}

// cmdStatus runs statusCommand.
func cmdStatus(cmd *command, args []string, e env) int {
	fs, c := newFlags(cmd, e, 0)
	if _, err := parseInterspersed(fs, args); err != nil {
		return exitUsage
	}
	bridge := c.open(e)
	if bridge == nil {
		return exitFailure
	}
	code := emit(e, bridgeJSON(bridge))
	if !bridge.Running {
		fmt.Fprintln(e.stderr, "vw2026: the bridge is not running (is Vectorworks started with the plug-in?)")
		return exitDown
	}
	return code
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

// --- call / tools -----------------------------------------------------------

var callCommand = &command{
	UsageLine: "vw2026 call <tool> [args-json | -] [--raw] [--timeout <seconds>] [--spool <dir>]",
	Short:     "call one tool",
	Long: `
Call calls one tool of the bridge and prints its result. "vw2026 call tools"
lists the tools; docs/plugin/tools.md describes them.

The arguments are a JSON object. "-" reads them from the standard input.
Without them, the tool is called with no arguments. Call does not check the
arguments against the tool; the plug-in does.

	vw2026 call layers '{"include_sheets":false}'
	echo '{"layer":"1F"}' | vw2026 call layer_objects -

On success, call prints the result of the tool. With --raw it prints the
whole response (ok, result, and error). When the tool fails, call writes the
reason to the standard error and exits with 1.

The --timeout flag (default 30 seconds) limits the wait for the response.
When it runs out, call withdraws the request and exits with 4.
`,
	Run: cmdCall,
}

// cmdCall runs callCommand.
func cmdCall(cmd *command, args []string, e env) int {
	fs, c := newFlags(cmd, e, 30)
	raw := fs.Bool("raw", false, "print the whole response (ok/result/error)")
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage
	}
	if len(positional) == 0 || len(positional) > 2 {
		fs.Usage()
		return exitUsage
	}
	tool := positional[0]
	var payload json.RawMessage
	if len(positional) == 2 {
		text := positional[1]
		if text == "-" {
			data, readErr := io.ReadAll(e.stdin)
			if readErr != nil {
				fmt.Fprintf(e.stderr, "vw2026: read stdin: %v\n", readErr)
				return exitFailure
			}
			text = string(data)
		}
		var probe map[string]any
		if json.Unmarshal([]byte(text), &probe) != nil || probe == nil {
			fmt.Fprint(e.stderr, "vw2026: args must be a JSON object\n")
			return exitUsage
		}
		payload = json.RawMessage(strings.TrimSpace(text))
	}

	bridge := c.open(e)
	if bridge == nil {
		return exitFailure
	}
	if !bridge.Running {
		fmt.Fprintln(e.stderr, "vw2026: the bridge is not running (try `vw2026 status`)")
		return exitDown
	}
	response, err := bridge.Call(tool, payload, seconds(c.timeout))
	if err != nil {
		fmt.Fprintf(e.stderr, "vw2026: %v\n", err)
		return codeFor(err)
	}
	if *raw {
		_ = emit(e, response)
	} else if response.OK {
		_ = emitRaw(e, response.Result)
	}
	if !response.OK {
		fmt.Fprintf(e.stderr, "vw2026: %s: %s\n", tool, response.Error)
		return exitToolErr
	}
	return exitOK
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

var waitCommand = &command{
	UsageLine: "vw2026 wait [--down] [--timeout <seconds>] [--spool <dir>]",
	Short:     "wait until the bridge starts or stops",
	Long: `
Wait waits until the bridge starts, that is, until the plug-in takes the lock.
With --down it waits until the bridge stops (the lock is released). It prints
the same output as status.

The --timeout flag (default 120 seconds) limits the wait. When it runs out,
wait exits with 4.

Vectorworks keeps the lock while it shows the dialog that asks to save the
drawings, so "wait --down" does not take that dialog for a stop.
`,
	Run: cmdWait,
}

// cmdWait runs waitCommand.
func cmdWait(cmd *command, args []string, e env) int {
	fs, c := newFlags(cmd, e, 120)
	down := fs.Bool("down", false, "wait until Vectorworks stops instead")
	if _, err := parseInterspersed(fs, args); err != nil {
		return exitUsage
	}
	deadline := time.Now().Add(seconds(c.timeout))
	for {
		// 動き出す・止まるはロックだけで決まる。保存の確認のダイアログを開いている間も
		// ロックは掴まれたままなので、--down がそれを「止まった」と誤らない。
		bridge := c.open(e)
		if bridge == nil {
			return exitFailure
		}
		if bridge.Running != *down {
			return emit(e, bridgeJSON(bridge))
		}
		if time.Now().After(deadline) {
			fmt.Fprintln(e.stderr, "vw2026: timed out waiting")
			return exitTimeout
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// --- launch -----------------------------------------------------------------

var launchCommand = &command{
	UsageLine: "vw2026 launch [--app <name-or-path>] [--spool <dir>]",
	Short:     "start Vectorworks",
	Long: `
Launch starts Vectorworks unless the bridge is already running. It does not
wait for the bridge; run "vw2026 wait" next to wait for it.

	vw2026 launch && vw2026 wait

It prints one of

	{"launched":true,"command":[...]}
	{"running":true,"spool":"...","launched":false}

The --app flag (or VW2026_APP) names the application. The default is
"Vectorworks 2026" on macOS, started with "open -a", and
%ProgramFiles%\Vectorworks 2026\Vectorworks2026.exe on Windows. Launch does
not search other places; name them with --app. A value that does not end in
.app is started as an executable.

When Vectorworks is running but the bridge is not (the plug-in is not
installed), launch exits with 3.
`,
	Run: cmdLaunch,
}

// cmdLaunch runs launchCommand. It does not wait: waiting is the caller's
// choice (docs/design.md「CLI はプリミティブに保つ」).
func cmdLaunch(cmd *command, args []string, e env) int {
	fs, c := newFlags(cmd, e, 0)
	app := fs.String("app", e.getenv("VW2026_APP"), "application or executable to start")
	if _, err := parseInterspersed(fs, args); err != nil {
		return exitUsage
	}
	bridge := c.open(e)
	if bridge == nil {
		return exitFailure
	}
	if bridge.Running {
		out := bridgeJSON(bridge)
		out["launched"] = false
		return emit(e, out)
	}
	argv, err := launch.Command(*app)
	if err != nil {
		fmt.Fprintf(e.stderr, "vw2026: %v\n", err)
		return exitFailure
	}
	if err := e.start(argv); err != nil {
		if errors.Is(err, launch.ErrAlreadyRunning) {
			fmt.Fprintln(e.stderr, "vw2026: Vectorworks is running but the bridge is not (is the plug-in installed?)")
			return exitDown
		}
		fmt.Fprintf(e.stderr, "vw2026: launch: %v\n", err)
		return exitFailure
	}
	return emit(e, map[string]any{"launched": true, "command": argv})
}

// --- version ----------------------------------------------------------------

var versionCommand = &command{
	UsageLine: "vw2026 version",
	Short:     "print the versions of the CLI and the protocol",
	Long: `
Version prints the version of this CLI and the version of the protocol it
speaks (docs/protocol.md).

	{"version":"abc1234","protocol":3}
`,
	Run: cmdVersion,
}

// cmdVersion runs versionCommand.
func cmdVersion(cmd *command, args []string, e env) int {
	fs := flag.NewFlagSet(cmd.Name(), flag.ContinueOnError)
	fs.SetOutput(e.stderr)
	fs.Usage = func() { printFlagUsage(cmd, fs) }
	if _, err := parseInterspersed(fs, args); err != nil {
		return exitUsage
	}
	return emit(e, map[string]any{"version": version, "protocol": spool.ProtocolVersion})
}

// --- 出力 -------------------------------------------------------------------

func emit(e env, value any) int {
	data, err := json.Marshal(value)
	if err != nil {
		fmt.Fprintf(e.stderr, "vw2026: encode output: %v\n", err)
		return exitFailure
	}
	return emitRaw(e, data)
}

func emitRaw(e env, data []byte) int {
	if len(data) == 0 {
		data = []byte("{}")
	}
	fmt.Fprintf(e.stdout, "%s\n", data)
	return exitOK
}
