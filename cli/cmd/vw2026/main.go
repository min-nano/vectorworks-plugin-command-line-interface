// vw2026 は、Vectorworks で動いているブリッジ（cli プラグイン）へ道具の呼び出しを
// 1 つずつ届けるコマンドである。
//
// **指示されたとおりに 1 回だけ動くプリミティブな入口**で、セッションの見分け・占有・
// 再試行は持たない（docs/design.md「CLI はプリミティブに保つ」）。それらは呼ぶ側が担う。
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
)

const usage = `usage: vw2026 <command> [options]

commands:
  status                 ブリッジが動いているかを返す（版は call ping）
  call <tool> [args]     道具を 1 つ呼ぶ（args は JSON オブジェクト。"-" で標準入力から）
  wait                   ブリッジが動き出す（--down なら止まる）まで待つ
  launch                 Vectorworks を起動する（待たない。待つなら続けて wait）
  version                この CLI の版と、受け渡しの版を返す

common options:
  --spool <dir>          スプールの場所（既定 VW2026_SPOOL、無ければ <CLI>/spool）
  --timeout <seconds>    待つ上限（call / wait）
`

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
		fmt.Fprint(e.stderr, usage)
		return exitUsage
	}
	command, rest := args[0], args[1:]
	switch command {
	case "status":
		return cmdStatus(rest, e)
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
		fmt.Fprintf(e.stderr, "vw2026: unknown command %q\n\n%s", command, usage)
		return exitUsage
	}
}

// common はどのコマンドにもある指定。
type common struct {
	spool   string
	timeout float64
}

// newFlags は指定を用意する。defaultTimeout が 0 なら --timeout を持たない（launch・status）。
func newFlags(name string, e env, defaultTimeout float64) (*flag.FlagSet, *common) {
	c := &common{}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(e.stderr)
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

func cmdStatus(args []string, e env) int {
	fs, c := newFlags("status", e, 0)
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

func cmdCall(args []string, e env) int {
	fs, c := newFlags("call", e, 30)
	raw := fs.Bool("raw", false, "print the whole response (ok/result/error)")
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage
	}
	if len(positional) == 0 || len(positional) > 2 {
		fmt.Fprint(e.stderr, "usage: vw2026 call <tool> [args-json | -]\n")
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

func cmdWait(args []string, e env) int {
	fs, c := newFlags("wait", e, 120)
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

// cmdLaunch は起動するだけで待たない。動き出すのを待つなら、呼ぶ側が続けて wait を呼ぶ。
func cmdLaunch(args []string, e env) int {
	fs, c := newFlags("launch", e, 0)
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
