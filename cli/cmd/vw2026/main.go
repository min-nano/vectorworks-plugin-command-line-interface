// Vw2026 sends tool calls, one at a time, to the bridge that the cli plug-in
// runs inside Vectorworks 2026. Run "vw2026 --help" for its usage.
//
// Each command lives in its own file (status.go, call.go, ...) as a kong
// struct: its flags, its help (the help tag and the Help method), and its run
// method, side by side. kong builds the help output from them.
//
// To build and test, in the cli module:
//
//	go vet ./...
//	go test ./...
//	go build -ldflags "-X main.version=$(git describe --always)" -o vw2026 ./cmd/vw2026
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
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
	// 待つのをやめたときに要求をプラグインが既に受け取っていて、応答が届かなかった。exitTimeout
	// （実行されない）と分けるのは、書く道具を呼んだ側が二重に操作しないように。
	exitNoResponse = 8
	// 占有に断られた（ほかが占有している・載せた占有が終わっている・quit を占有せずに呼んだ）。
	// 要求は実行されていない。
	exitBusy = 9
)

// env は外の世界との接点。テストが差し替える。
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

// cli is the whole command line.
type cli struct {
	globals

	Status  statusCmd  `cmd:"" help:"Report whether the bridge is running."`
	Call    callCmd    `cmd:"" help:"Call one tool."`
	Wait    waitCmd    `cmd:"" help:"Wait until the bridge starts or stops."`
	Launch  launchCmd  `cmd:"" help:"Start Vectorworks."`
	Session sessionCmd `cmd:"" help:"Occupy the bridge, or end the occupation."`
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

	vw2026 session start                  # quit runs only in a session
	vw2026 call quit --session <ID> && vw2026 wait --down && vw2026 install && vw2026 launch && vw2026 wait

The commands exit with

	0  success
	1  the tool reported a failure (reason on the standard error; with --raw also on the standard output)
	2  wrong usage
	3  the bridge is not running
	4  timed out (the tool did not run and will not run; Vectorworks is running)
	5  unused (meant "protocol mismatch" up to protocol 2; the numbers are not reused)
	6  any other failure (cannot write, cannot start, cannot locate the spool)
	7  Vectorworks is running, so install or uninstall did nothing (planned)
	8  the plug-in took the request but no response came (the tool may have run)
	9  refused by the session: another one occupies the bridge, the given one has ended,
	   or quit was called outside a session (the tool did not run)

Vectorworks can be running, holding the lock, while the plug-in defers the
requests, for example during a modal dialog or while undo is being recorded
(docs/protocol.md). Status does not tell this state apart; call and "wait
--down" wait up to --timeout and then exit with 4. Recover from 3 and 4
differently: 3 means Vectorworks is not running, while 4 means it is running
but did not answer in time, so launching it again or reinstalling the plug-in
does not help. Tell 4 and 8 apart before calling a tool that changes the
drawing again: after 4 the tool did not run, while after 8 it may have.
`
}

// command is what every command struct implements. kong's own Run convention
// returns only an error, so the commands use this instead and return the exit
// code directly.
type command interface {
	run(g *globals, e *env) int
}

func run(args []string, e env) int {
	usageError := false
	switch {
	case len(args) == 0:
		// A bare "vw2026" is a usage error: show the help on the standard error.
		usageError = true
		e.stdout = e.stderr
		args = []string{"--help"}
	case args[0] == "help":
		// "vw2026 help [<command>]" is "vw2026 [<command>] --help".
		args = append(args[1:len(args):len(args)], "--help")
	case args[0] == "--version":
		args[0] = "version"
	}

	var grammar cli
	exited := -1 // kong asks to exit after printing the help
	parser, err := kong.New(&grammar,
		kong.Name("vw2026"),
		kong.Writers(e.stdout, e.stderr),
		kong.Exit(func(code int) { exited = code }),
		kong.ConfigureHelp(kong.HelpOptions{WrapUpperBound: 80}),
	)
	if err != nil {
		fmt.Fprintf(e.stderr, "vw2026: %v\n", err)
		return exitFailure
	}
	ctx, err := parser.Parse(args)
	switch {
	case exited >= 0:
		// The help was printed. Parse goes on after it and may report a missing
		// command, which does not matter here.
		if usageError {
			return exitUsage
		}
		return exited
	case err != nil:
		fmt.Fprintf(e.stderr, "vw2026: %v (run \"vw2026 --help\")\n", err)
		return exitUsage
	}
	cmd := ctx.Selected().Target.Addr().Interface().(command)
	return cmd.run(&grammar.globals, &e)
}

// dir はスプールの場所を決める。決まらなければ標準エラーへ理由を書いて空。
func (g *globals) dir(e *env) string {
	if g.Spool != "" {
		return g.Spool
	}
	dir, err := spool.DefaultDir()
	if err != nil {
		fmt.Fprintf(e.stderr, "vw2026: %v (set VW2026_SPOOL)\n", err)
		return ""
	}
	return dir
}

// open はスプールを決めて、ブリッジの状態を判定する。場所が決まらなければ nil。
func (g *globals) open(e *env) *spool.Bridge {
	dir := g.dir(e)
	if dir == "" {
		return nil
	}
	return spool.Open(dir)
}

func seconds(value float64) time.Duration {
	return time.Duration(value * float64(time.Second))
}

// emit は値を JSON 1 行で標準出力へ書く。
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
