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

// 終了コード（cli.Help の一覧と対）。1 は慣例どおり汎用の失敗に充て、呼ぶ側が分岐する
// 必要のある結果だけに別の番号を与える。道具が返した失敗は 20 番台に置き、1 の位を CLI 側と
// 合わせる（21 は汎用の失敗、22 は使い方の誤り、23 は受け付けたが実行されない）。20 番台は
// 道具や占有の失敗としてプラグインが答えたもの、1 桁はそれ以外（no_wait と壊れた応答を含む）。
//
// 番号は呼ぶ側との約束なので、この振り直し（リリース前）のあとは変えない。使わなくなった番号は
// 別の意味に再利用しない（旧い表に従う呼ぶ側の分岐が、黙って別の意味になるため）。
const (
	exitOK = 0
	// そのほか（書き込めない・起動できない等）
	exitFailure = 1
	// 使い方の誤り
	exitUsage = 2
	// 待ちきれなかった（no_wait を含む）。要求は実行されない。Vectorworks は動いている
	// （刻みが届かない重い処理の最中など）。exitDown と分けるのは、呼ぶ側が起動し直さずに
	// 待てばよいと判定できるように。
	exitTimeout = 3
	// 待つのをやめたときに要求をプラグインが既に受け取っていて、応答が届かなかった。exitTimeout
	// （実行されない）と分けるのは、書く道具を呼んだ側が二重に操作しないように。
	exitNoResponse = 4
	// ブリッジが見つからない
	exitDown = 5
	// 6 は install / uninstall（未実装）が「Vectorworks が動いているので行えない」に使う。

	// 以下はプラグインが失敗を答えた（応答は届いている）。

	// internal を答えた。
	exitToolFailure = 21
	// invalid_request・unknown_tool・invalid_args を答えた。
	exitToolUsage = 22
	// 実行せずに断られた（occupied・no_session・busy。ほかが占有している・載せた占有が終わって
	// いる・占有の中でだけ呼べる道具を占有せずに呼んだ・Vectorworks が処理中）。要求は実行されて
	// いない。
	exitBusy = 23
)

// env は外の世界との接点。テストが差し替える。
type env struct {
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
	start  func(argv []string) error
	// スプールの場所。本番は常に spool.DefaultDir で、外から変える指定は持たない（プラグインは
	// <CLI>/spool しか見ないので、別の場所を指せても要求が届かない。docs/protocol.md
	// 「スプールの場所」）。テストは偽のプラグインのスプールを返す。
	spool func() (string, error)
}

func main() {
	os.Exit(run(os.Args[1:], env{
		stdin:  os.Stdin,
		stdout: os.Stdout,
		stderr: os.Stderr,
		start:  launch.Start,
		spool:  spool.DefaultDir,
	}))
}

// cli is the whole command line.
type cli struct {
	Status  statusCmd  `cmd:"" help:"Report whether the bridge is running."`
	Call    callCmd    `cmd:"" help:"Call one tool."`
	Launch  launchCmd  `cmd:"" help:"Start Vectorworks."`
	Session sessionCmd `cmd:"" help:"Occupy the bridge, or end the occupation."`
	Version versionCmd `cmd:"" help:"Print the versions of the CLI and the protocol."`
}

func (cli) Help() string {
	return `
Vw2026 sends tool calls, one at a time, to the bridge that the cli plug-in
runs inside Vectorworks 2026.

It is a primitive: each run does exactly what it is told once. It does not
retry, wait for a session to be free, or interpret the tools; the caller
does those (docs/design.md). Exclusive use of the bridge is the plug-in's:
vw2026 only asks it for a session and carries the session in the calls
("vw2026 help session"). The bridge and vw2026 exchange files in a spool by the
protocol in docs/protocol.md. The spool is <CLI>/spool
(docs/plugin/identifiers.md), and nothing changes it: the plug-in looks only
there.

Each command prints one line of JSON to the standard output, writes the
reason for a failure to the standard error, and reports the outcome in its
exit status. Flags may come before or after the positional arguments.

Each flag that takes a setting can also be set by an environment variable,
shown in the help of the command ("vw2026 help <command>"), for example
VW2026_CALL_TIMEOUT for "call --timeout". A flag on the command line takes
precedence over its variable. A variable that is set but empty is a value too:
an empty timeout is wrong usage. For a switch such as --raw, the variable takes
true or false (or 1 or 0), and --no-raw turns it off again.

	vw2026 status
	vw2026 call tools
	vw2026 launch

vw2026 does not wait for the bridge to start or stop: the caller runs status
in a loop ("vw2026 help status").

To update the plug-in, the caller combines the commands (install is not
implemented yet; see docs/plugin/install-and-update.md):

	vw2026 session start             # quit runs only in a session
	vw2026 call quit --session <ID>  # then wait for the bridge to stop
	vw2026 install
	vw2026 launch                    # then wait for the bridge to start

The commands exit with

	0  success
	1  any other failure (cannot write, cannot start, cannot locate the spool)
	2  wrong usage
	3  timed out, or refused for a missing wait file (the tool did not run and will not run;
	   Vectorworks is running)
	4  the plug-in took the request but no response came (the tool may have run)
	5  the bridge is not running
	6  Vectorworks is running, so install or uninstall did nothing (planned)
	21 the tool reported a failure (internal)
	22 the tool reported wrong usage (invalid_request, unknown_tool, or invalid_args)
	23 refused (the tool did not run): another session occupies the bridge, the
	   given one has ended, a tool that runs only in a session was called outside
	   one, or Vectorworks is busy

The codes from 21 are failures that the plug-in answered for the tool, the
session, or a busy Vectorworks; the codes below 20 are everything else,
including a no_wait response and a malformed one. The last digit of a code from
21 matches the code below 20 of the same kind (1 failure, 2 wrong usage, 3 did
not run). For 21 to 23 the reason is on the standard error; for 21 and 22, with
--raw, the response is also on the standard output.

Vectorworks can be running, holding the lock, without answering: the plug-in
answers only when Vectorworks runs its timer, which it may not do in the middle
of a long operation or in some of its dialogs (docs/protocol.md). Status reports
this state as running; call waits up to --timeout and then exits with 3.
Recover from 5 and 3 differently: 5 means Vectorworks is not running, while 3
means it is running but did not answer in time, so launching it again or
reinstalling the plug-in does not help. Tell 3 and 4 apart before calling a
tool that changes the drawing again: after 3 the tool did not run, while after
4 it may have.
`
}

// command is what every command struct implements. kong's own Run convention
// returns only an error, so the commands use this instead and return the exit
// code directly.
type command interface {
	run(e *env) int
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
	// kong asks to exit after printing the help
	exited := -1
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
	return cmd.run(&e)
}

// open はスプールを決めて、ブリッジの状態を判定する。場所が決まらなければ標準エラーへ
// 理由を書いて nil。
func open(e *env) *spool.Bridge {
	dir, err := e.spool()
	if err != nil {
		fmt.Fprintf(e.stderr, "vw2026: cannot locate the spool: %v\n", err)
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

// emitRaw は data をそのまま 1 行で書く。結果の無い成功（result が無い・null）は {} にする
// （出力はいつも JSON のオブジェクト 1 行）。
func emitRaw(e *env, data []byte) int {
	if len(data) == 0 || string(data) == "null" {
		data = []byte("{}")
	}
	fmt.Fprintf(e.stdout, "%s\n", data)
	return exitOK
}
