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

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/edition"
	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/install"
	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/launch"
	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/spool"
)

// version はビルド時に -ldflags "-X main.version=…" で埋め込む。
var version = "dev"

// 終了コード（cli.Help の一覧と対）。1 は慣例の通り汎用の失敗に充て、呼び出し元が分岐する必要のある
// 結果のみに別の番号を与える。ツールが返した失敗は 20 番台に配置し、1 の位を CLI 側と統一する
// （21 は汎用の失敗、22 は使用方法の誤り、23 は受け付けたが実行されない）。20 番台はツールや占有の
// 失敗としてプラグインが応答したもの、1 桁はそれ以外（no_wait と壊れた応答を含む）。
//
// 番号は呼び出し元との取り決めであるため、この振り直し（リリース前）の後は変更しない。
// 使用しなくなった番号は別の意味に再利用しない（旧い一覧に従う呼び出し元の分岐が、黙って別の
// 意味になるため）。
const (
	exitOK = 0
	// その他（書き込めない・起動できないなど）
	exitFailure = 1
	// 使用方法の誤り
	exitUsage = 2
	// 待機しきれなかった（no_wait を含む）。要求は実行されない。Vectorworks は実行中である
	// （周期処理が届かない重い処理の最中など）。exitDown と区別するのは、呼び出し元が再起動せずに
	// 待機すればよいと判定できるようにするため。
	exitTimeout = 3
	// 待機を中止した時点で要求をプラグインが既に受け取っており、応答が届かなかった。
	// exitTimeout（実行されない）と区別するのは、書き込みツールを呼び出した側が二重に
	// 操作しないようにするため。
	exitNoResponse = 4
	// ブリッジが見つからない
	exitDown = 5
	// Vectorworks が実行中であるため、install / uninstall が何もしなかった（呼び出し元が
	// 終了させてから再実行する）。
	exitRunning = 6

	// 以下はプラグインが失敗を応答した（応答は届いている）。

	// internal を応答した。
	exitToolFailure = 21
	// invalid_request・unknown_tool・invalid_args を応答した。
	exitToolUsage = 22
	// 実行せずに拒否された（occupied・no_session・busy。他が占有している・付与した占有が
	// 終了している・占有の中でのみ呼び出せるツールを占有せずに呼び出した・Vectorworks が処理中）。
	// 要求は実行されていない。
	exitBusy = 23
)

// env は外部との接点。テストが差し替える。
type env struct {
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
	start  func(argv []string) error
	// スプールの場所。本番環境では常に spool.Dir（実行ファイルの場所から導出する）であり、外部から
	// 変更する指定は持たない（プラグインはモジュールの場所から導出した <Plug-Ins>/cli/spool のみを
	// 参照するため、別の場所を指定できても要求が届かない。docs/protocol.md「スプールの場所」）。
	// テストは擬似プラグインのスプールを返す。
	spool func() (string, error)
	// install / uninstall の作業領域（<Work>）。本番環境では install.WorkDir（利用者ごとの
	// 一時フォルダの下）。
	workDir func() (string, error)
	// 既定の <Plug-Ins>。本番環境では install.DefaultPluginsDir。インストールされた CLI（自身の
	// インストール先の <Plug-Ins> を使用する）と、初回スクリプトが --plugins-dir を渡した場合は
	// 使用しない。
	pluginsDir func() (string, error)
	// 実行中の CLI の実行ファイル。本番環境では os.Executable。install / uninstall が、差し替える
	// フォルダの中にあれば退避する。
	executable func() (string, error)
}

func main() {
	os.Exit(run(os.Args[1:], defaultEnv()))
}

// defaultEnv は本番環境における外部との接点。テストは使用せず、場所をいずれも一時ディレクトリに
// 差し替える。
func defaultEnv() env {
	return env{
		stdin:      os.Stdin,
		stdout:     os.Stdout,
		stderr:     os.Stderr,
		start:      launch.Start,
		spool:      spool.Dir,
		workDir:    install.WorkDir,
		pluginsDir: install.DefaultPluginsDir,
		executable: os.Executable,
	}
}

// cli is the whole command line.
type cli struct {
	Status    statusCmd    `cmd:"" help:"Report whether the bridge is running."`
	Call      callCmd      `cmd:"" help:"Call one tool."`
	Tools     toolsCmd     `cmd:"" help:"List the tools that call can call."`
	Launch    launchCmd    `cmd:"" help:"Start Vectorworks."`
	Session   sessionCmd   `cmd:"" help:"Occupy the bridge, or end the occupation."`
	Install   installCmd   `cmd:"" help:"Install or update a plug-in (NAME@TAG)."`
	Uninstall uninstallCmd `cmd:"" help:"Uninstall a plug-in."`
	Version   versionCmd   `cmd:"" help:"Print the versions of the CLI and the protocol."`
}

func (cli) Help() string {
	return edition.Expand(`
${Cmd} sends tool calls, one at a time, to the bridge that the cli plug-in
runs inside ${product}.

It is a primitive: each run does exactly what it is told once. It does not
retry, wait for a session to be free, or interpret the tools; the caller
does those (docs/design.md). Exclusive use of the bridge is the plug-in's:
${cmd} only asks it for a session and carries the session in the calls
("${cmd} help session"). The bridge and ${cmd} exchange files in a spool by the
protocol in docs/protocol.md. The spool is the folder spool in the folder of
the cli plug-in (<Plug-Ins>/cli/spool; docs/plugin/identifiers.md). ${cmd}
finds it from where its own executable is (<Plug-Ins>/cli/bin/${cmd}, after
following links; if they cannot be followed, from the path as it is), and
nothing changes it: the plug-in finds the same folder from where its module
is, and looks only there. A copy of ${cmd} run from
outside that folder (for example, from an unpacked zip) looks for the spool
next to its own folder, finds no bridge there, and reports the bridge as not
running; the commands show the spool they looked at.

Each command prints one line of JSON to the standard output, writes the
reason for a failure to the standard error, and reports the outcome in its
exit status. Flags may come before or after the positional arguments.

Each flag that takes a setting can also be set by an environment variable,
shown in the help of the command ("${cmd} help <command>"), for example
${prefix}CALL_TIMEOUT for "call --timeout". A flag on the command line takes
precedence over its variable. A variable that is set but empty is a value too:
an empty timeout is wrong usage. For a switch such as --raw, the variable takes
true or false (or 1 or 0), and --no-raw turns it off again.

	${cmd} status
	${cmd} tools
	${cmd} launch

${cmd} does not wait for the bridge to start or stop: the caller runs status
in a loop ("${cmd} help status").

${cmd} installs, updates, and uninstalls the plug-ins ("${cmd} help
install"). It updates cli itself too; the first-time script installs cli and
uninstalls it ("${cmd} help uninstall"). It changes nothing while Vectorworks
is running with the bridge, and it does not quit Vectorworks. To update the plug-in while
Vectorworks runs, the caller combines the commands. Quit runs only in a
session and only while no drawing is open, so the caller saves and closes the
drawings first. End the session on
each failure: otherwise it stays until Vectorworks quits, and other callers
are refused. In sh, with the waits of "${cmd} help status":

	# "session" in the output of ${cmd} session start
	id=...
	# with drawings open, quit fails and lists them in "detail"
	${cmd} call quit --session "$id"; code=$?
	if [ "$code" -ne 0 ]; then ${cmd} session end --session "$id"; exit "$code"; fi
	# until the bridge stops
	for i in $(seq 120); do
	    ${cmd} status >/dev/null 2>&1; code=$?
	    [ "$code" -ne 0 ] && break
	    sleep 1
	done
	# 5: stopped. 0: ran out, because a drawing was opened after quit checked and
	# the dialog that asks to save was canceled or left open; exit with 3 (timed
	# out), not 0. 1: the spool cannot be located; session end then fails for
	# the same reason, and the session stays until Vectorworks quits.
	case $code in
	    5) ;;
	    0) ${cmd} session end --session "$id"; exit 3 ;;
	    *) ${cmd} session end --session "$id"; exit "$code" ;;
	esac
	${cmd} install cli@latest && ${cmd} launch || exit
	# until the bridge starts (0: started; 5: ran out, or 1: failed)
	for i in $(seq 120); do
	    ${cmd} status >/dev/null 2>&1; code=$?
	    [ "$code" -ne 5 ] && break
	    sleep 1
	done
	exit "$code"

The session ends when Vectorworks quits, so take a new one after the restart.

The commands exit with

	0  success
	1  any other failure (cannot write, cannot start, cannot locate the spool,
	   cannot write the output of a success: a tool or install has run then)
	2  wrong usage
	3  timed out, or refused for a missing wait file (the tool did not run and will not run;
	   Vectorworks is running)
	4  the plug-in took the request but no response came (the tool may have run)
	5  the bridge is not running (or this ${cmd} looks at another spool)
	6  Vectorworks is running, so install or uninstall did nothing
	21 the tool reported a failure (internal)
	22 the tool reported wrong usage (invalid_request, unknown_tool, or invalid_args)
	23 refused (the tool did not run): another session occupies the bridge, the
	   given one has ended, a tool that runs only in a session was called outside
	   one, or Vectorworks is busy

The codes from 21 are failures that the plug-in answered for the tool, the
session, or a busy Vectorworks; the codes below 20 are everything else,
including a no_wait response and a malformed one. The last digit of a code from
21 matches the code below 20 of the same kind (1 failure, 2 wrong usage, 3 did
not run). For 21 to 23 the reason is on the standard error; for 21 and 22, the
detail of the failure, if the tool gives one, is on the standard output (with
--raw, the whole response is).

Vectorworks can be running, holding the lock, without answering: the plug-in
answers only when Vectorworks runs its timer, which it may not do in the middle
of a long operation or in some of its dialogs (docs/protocol.md). Status reports
this state as running; call waits up to --timeout and then exits with 3.
Recover from 5 and 3 differently: 5 means the bridge is not running
(Vectorworks is not running, or it is running without the bridge; see "${cmd}
help launch"), while 3 means the bridge is running but did not answer in time,
so launching Vectorworks again or reinstalling the plug-in does not help. A 5
can also mean that this ${cmd} runs from outside the folder of the plug-in and
looks at another spool: before launching Vectorworks again, check the spool
that the command shows (on the standard error, and in the output of status).
Tell 3 and 4 apart before calling a tool that changes the drawing again: after
3 the tool did not run, while after 4 it may have.
`)
}

// grammarOptions は、コマンドの定義を読み込むのに必要な kong の指定（名前と、タグに差し込む版ごとの
// 値）。テストも同じものでコマンドの定義を読み込む。
func grammarOptions() []kong.Option {
	return []kong.Option{kong.Name(edition.Command), kong.Vars(edition.Vars())}
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
		args = append([]string{"version"}, args[1:]...)
	}

	var grammar cli
	// kong asks to exit after printing the help
	exited := -1
	parser, err := kong.New(&grammar, append(grammarOptions(),
		kong.Writers(e.stdout, e.stderr),
		kong.Exit(func(code int) { exited = code }),
		kong.ConfigureHelp(kong.HelpOptions{WrapUpperBound: 80}),
	)...)
	if err != nil {
		fmt.Fprintf(e.stderr, edition.Command+": %v\n", err)
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
		fmt.Fprintf(e.stderr, edition.Command+": %v (run \""+edition.Command+" --help\")\n", err)
		return exitUsage
	}
	cmd := ctx.Selected().Target.Addr().Interface().(command)
	return cmd.run(&e)
}

// open はスプールを決定し、ブリッジの状態を判定する。場所を決定できなければ標準エラーへ理由を
// 書き込んで nil。
func open(e *env) *spool.Bridge {
	dir, err := e.spool()
	if err != nil {
		fmt.Fprintf(e.stderr, edition.Command+": cannot locate the spool: %v\n", err)
		return nil
	}
	return spool.Open(dir)
}

func seconds(value float64) time.Duration {
	return time.Duration(value * float64(time.Second))
}

// emit は値を JSON 1 行で標準出力へ書き込む。
func emit(e *env, value any) int {
	data, err := json.Marshal(value)
	if err != nil {
		fmt.Fprintf(e.stderr, edition.Command+": encode output: %v\n", err)
		return exitFailure
	}
	return emitRaw(e, data)
}

// emitRaw は data をそのまま 1 行で書き込む。書き込めなければ（ディスクが一杯など）失敗の
// 終了コードを返す（呼び出し元のスクリプトが、空や途中までの出力を正しいものとして
// 扱わないように）。読み取る側が閉じたパイプへの書き込みは、macOS と Linux では Go の既定の通り
// SIGPIPE で終了し、ここへは到達しない（Windows では誤りとして到達する）。
func emitRaw(e *env, data []byte) int {
	if _, err := fmt.Fprintf(e.stdout, "%s\n", data); err != nil {
		fmt.Fprintf(e.stderr, edition.Command+": write output: %v\n", err)
		return exitFailure
	}
	return exitOK
}
