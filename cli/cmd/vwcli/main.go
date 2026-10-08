// vwcli は、Vectorworks で動いているブリッジ（min-nano_cli プラグイン）へ道具の呼び出しを
// 1 つずつ届けるコマンドである。
//
// **指示されたとおりに 1 回だけ動くプリミティブな入口**で、セッションの見分け・占有・
// 再試行は持たない（docs/design.md「CLI はプリミティブに保つ」）。それらは呼ぶ側
// （MCP のラッパーなど、別のプロジェクト）が担う。
//
// 出力は標準出力へ JSON 1 行、失敗の説明は標準エラーへ。成否は終了コードで返す
// （docs/cli.md）。
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

// 終了コード（docs/cli.md と対）。
const (
	exitOK       = 0
	exitToolErr  = 1 // 道具が失敗を返した（応答は届いている）
	exitUsage    = 2 // 使い方の誤り
	exitDown     = 3 // ブリッジが見つからない
	exitTimeout  = 4 // 応答を待ちきれなかった
	exitProtocol = 5 // プラグインと CLI の版が違う
	exitFailure  = 6 // そのほか（書き込めない・起動できない等）
)

const usage = `usage: vwcli <command> [options]

commands:
  status                 ブリッジが動いているかと、生存の印を返す
  tools                  呼べる道具の一覧を返す
  call <tool> [args]     道具を 1 つ呼ぶ（args は JSON オブジェクト。"-" で標準入力から）
  wait                   ブリッジが動き出す（--down なら止まる）まで待つ
  launch                 Vectorworks を起動する
  version                この CLI の版と、受け渡しの版を返す

common options:
  --plugin <name>        相手のプラグイン名（既定 VWCLI_PLUGIN、無ければ min-nano_cli）
  --spool <dir>          スプールを直接指定する（既定 VWCLI_SPOOL。探索しない）
  --timeout <seconds>    待つ上限
`

type env struct {
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
	getenv func(string) string
	now    func() time.Time
	start  func(argv []string) error
}

func main() {
	os.Exit(run(os.Args[1:], env{
		stdin:  os.Stdin,
		stdout: os.Stdout,
		stderr: os.Stderr,
		getenv: os.Getenv,
		now:    time.Now,
		start:  launch.Start,
	}))
}

func run(args []string, e env) int {
	if len(args) == 0 {
		fmt.Fprint(e.stderr, usage)
		return exitUsage
	}
	command, rest := args[0], args[1:]
	switch command {
	case "status":
		return cmdStatus(rest, e)
	case "tools":
		return cmdCall(append([]string{"tools"}, rest...), e)
	case "call":
		return cmdCall(rest, e)
	case "wait":
		return cmdWait(rest, e)
	case "launch":
		return cmdLaunch(rest, e)
	case "version", "--version":
		return emit(e, map[string]any{"version": version, "protocol": spool.ProtocolVersion})
	case "help", "-h", "--help":
		fmt.Fprint(e.stdout, usage)
		return exitOK
	default:
		fmt.Fprintf(e.stderr, "vwcli: unknown command %q\n\n%s", command, usage)
		return exitUsage
	}
}

// common はどのコマンドにもある指定。
type common struct {
	plugin  string
	spool   string
	timeout float64
}

func newFlags(name string, e env, defaultTimeout float64) (*flag.FlagSet, *common) {
	c := &common{}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(e.stderr)
	fs.StringVar(&c.plugin, "plugin", e.getenv("VWCLI_PLUGIN"), "plugin name")
	fs.StringVar(&c.spool, "spool", e.getenv("VWCLI_SPOOL"), "spool directory")
	fs.Float64Var(&c.timeout, "timeout", envSeconds(e, "VWCLI_TIMEOUT", defaultTimeout), "seconds")
	return fs, c
}

func envSeconds(e env, name string, fallback float64) float64 {
	var value float64
	if _, err := fmt.Sscanf(e.getenv(name), "%g", &value); err == nil && value > 0 {
		return value
	}
	return fallback
}

func (c *common) candidates() []string {
	return spool.Candidates(c.plugin, c.spool)
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

func cmdStatus(args []string, e env) int {
	fs, c := newFlags("status", e, 0)
	if _, err := parseInterspersed(fs, args); err != nil {
		return exitUsage
	}
	bridge, searched, err := spool.Find(c.candidates(), e.now())
	if err != nil {
		_ = emit(e, map[string]any{"live": false, "searched": searched})
		fmt.Fprintln(e.stderr, "vwcli: the bridge is not running (is Vectorworks started with the plug-in?)")
		return exitDown
	}
	out := map[string]any{"live": true, "spool": bridge.Dir, "status": bridge.Status.Raw}
	if err := bridge.CheckProtocol(); err != nil {
		_ = emit(e, out)
		fmt.Fprintf(e.stderr, "vwcli: %v\n", err)
		return exitProtocol
	}
	return emit(e, out)
}

// --- call / tools -----------------------------------------------------------

func cmdCall(args []string, e env) int {
	fs, c := newFlags("call", e, 30)
	raw := fs.Bool("raw", false, "print the whole response (id/ok/result/error)")
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage
	}
	if len(positional) == 0 || len(positional) > 2 {
		fmt.Fprint(e.stderr, "usage: vwcli call <tool> [args-json | -]\n")
		return exitUsage
	}
	tool := positional[0]
	var payload json.RawMessage
	if len(positional) == 2 {
		text := positional[1]
		if text == "-" {
			data, readErr := io.ReadAll(e.stdin)
			if readErr != nil {
				fmt.Fprintf(e.stderr, "vwcli: read stdin: %v\n", readErr)
				return exitFailure
			}
			text = string(data)
		}
		var probe map[string]any
		if json.Unmarshal([]byte(text), &probe) != nil || probe == nil {
			fmt.Fprint(e.stderr, "vwcli: args must be a JSON object\n")
			return exitUsage
		}
		payload = json.RawMessage(strings.TrimSpace(text))
	}

	bridge, _, err := spool.Find(c.candidates(), e.now())
	if err != nil {
		fmt.Fprintln(e.stderr, "vwcli: the bridge is not running (try `vwcli status`)")
		return exitDown
	}
	response, err := bridge.Call(tool, payload, seconds(c.timeout))
	if err != nil {
		fmt.Fprintf(e.stderr, "vwcli: %v\n", err)
		return codeFor(err)
	}
	if *raw {
		_ = emit(e, response)
	} else if response.OK {
		_ = emitRaw(e, response.Result)
	}
	if !response.OK {
		fmt.Fprintf(e.stderr, "vwcli: %s: %s\n", tool, response.Error)
		return exitToolErr
	}
	return exitOK
}

func codeFor(err error) int {
	var protocol *spool.ProtocolError
	switch {
	case errors.As(err, &protocol):
		return exitProtocol
	case errors.Is(err, spool.ErrNotRunning):
		return exitDown
	case errors.Is(err, spool.ErrTimeout):
		return exitTimeout
	default:
		return exitFailure
	}
}

// --- wait -------------------------------------------------------------------

func cmdWait(args []string, e env) int {
	fs, c := newFlags("wait", e, 120)
	down := fs.Bool("down", false, "wait until the bridge stops instead")
	if _, err := parseInterspersed(fs, args); err != nil {
		return exitUsage
	}
	deadline := e.now().Add(seconds(c.timeout))
	for {
		bridge, _, err := spool.Find(c.candidates(), e.now())
		live := err == nil
		if live != *down {
			if live {
				return emit(e, map[string]any{"live": true, "spool": bridge.Dir, "status": bridge.Status.Raw})
			}
			return emit(e, map[string]any{"live": false})
		}
		if e.now().After(deadline) {
			fmt.Fprintln(e.stderr, "vwcli: timed out waiting")
			return exitTimeout
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// --- launch -----------------------------------------------------------------

func cmdLaunch(args []string, e env) int {
	fs, c := newFlags("launch", e, 0)
	app := fs.String("app", e.getenv("VWCLI_APP"), "application or executable to start")
	if _, err := parseInterspersed(fs, args); err != nil {
		return exitUsage
	}
	if bridge, _, err := spool.Find(c.candidates(), e.now()); err == nil {
		return emit(e, map[string]any{"launched": false, "live": true, "spool": bridge.Dir})
	}
	argv, err := launch.Command(*app)
	if err != nil {
		fmt.Fprintf(e.stderr, "vwcli: %v\n", err)
		return exitFailure
	}
	if err := e.start(argv); err != nil {
		if errors.Is(err, launch.ErrAlreadyRunning) {
			fmt.Fprintln(e.stderr, "vwcli: Vectorworks is running but the bridge is not (is the plug-in installed?)")
			return exitDown
		}
		fmt.Fprintf(e.stderr, "vwcli: launch: %v\n", err)
		return exitFailure
	}
	if c.timeout <= 0 {
		return emit(e, map[string]any{"launched": true, "command": argv})
	}
	// 起動を見届ける（--timeout 秒まで）。
	return cmdWait([]string{"--timeout", fmt.Sprint(c.timeout), "--plugin", c.plugin, "--spool", c.spool}, e)
}

// --- 出力 -------------------------------------------------------------------

func emit(e env, value any) int {
	data, err := json.Marshal(value)
	if err != nil {
		fmt.Fprintf(e.stderr, "vwcli: encode output: %v\n", err)
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
