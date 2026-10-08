// vw2026 は、Vectorworks で動いているブリッジ（cli プラグイン）へ道具の呼び出しを
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
	// Vectorworks は動いているがブリッジが応えない（モーダル・undo の記録・長い処理の最中）。
	// exitDown と分けるのは、呼ぶ側が起動し直さずに待てばよいと判定できるように。
	exitUnresponsive = 7
)

const usage = `usage: vw2026 <command> [options]

commands:
  status                 ブリッジが動いているかと、生存の印を返す
  tools                  呼べる道具の一覧を返す
  call <tool> [args]     道具を 1 つ呼ぶ（args は JSON オブジェクト。"-" で標準入力から）
  wait                   ブリッジが動き出す（--down なら Vectorworks が止まる）まで待つ
  launch                 Vectorworks を起動する
  version                この CLI の版と、受け渡しの版を返す

common options:
  --channel <name>       相手のプラグインの系列 stable / dev（既定 VW2026_CHANNEL、無ければ stable）
  --spool <dir>          スプールを直接指定する（既定 VW2026_SPOOL。探索しない）
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
		fmt.Fprintf(e.stderr, "vw2026: unknown command %q\n\n%s", command, usage)
		return exitUsage
	}
}

// common はどのコマンドにもある指定。
type common struct {
	channel string
	spool   string
	timeout float64
}

func newFlags(name string, e env, defaultTimeout float64) (*flag.FlagSet, *common) {
	c := &common{}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(e.stderr)
	fs.StringVar(&c.channel, "channel", e.getenv("VW2026_CHANNEL"), "plugin channel (stable / dev)")
	fs.StringVar(&c.spool, "spool", e.getenv("VW2026_SPOOL"), "spool directory")
	fs.Float64Var(&c.timeout, "timeout", envSeconds(e, "VW2026_TIMEOUT", defaultTimeout), "seconds")
	return fs, c
}

func envSeconds(e env, name string, fallback float64) float64 {
	var value float64
	if _, err := fmt.Sscanf(e.getenv(name), "%g", &value); err == nil && value > 0 {
		return value
	}
	return fallback
}

// valid は指定が正しいか。誤っていれば標準エラーへ理由を書いて false。
func (c *common) valid(e env) bool {
	if c.spool == "" && spool.SpoolName(c.channel) == "" {
		fmt.Fprintf(e.stderr, "vw2026: unknown channel %q (stable / dev)\n", c.channel)
		return false
	}
	return true
}

func (c *common) candidates() []string {
	return spool.Candidates(c.channel, c.spool)
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
	if !c.valid(e) {
		return exitUsage
	}
	bridge, searched, err := spool.Find(c.candidates(), e.now())
	if err != nil {
		_ = emit(e, map[string]any{"live": false, "state": spool.StateDown, "searched": searched})
		for _, s := range searched {
			if s.Shell != nil {
				// 殻は読み込まれているので「起動しているか」を問う文言は誤り。理由は shell に渡す。
				fmt.Fprintf(e.stderr, "vw2026: the plug-in is loaded but its payload is not running (see \"shell\" in %s)\n", s.Dir)
				return exitDown
			}
		}
		fmt.Fprintln(e.stderr, "vw2026: the bridge is not running (is Vectorworks started with the plug-in?)")
		return exitDown
	}
	out := bridgeJSON(bridge)
	if err := bridge.CheckProtocol(); err != nil {
		_ = emit(e, out)
		fmt.Fprintf(e.stderr, "vw2026: %v\n", err)
		return exitProtocol
	}
	if bridge.State == spool.StateUnresponsive {
		// 本体が受け付けの途中で動かなくなった（serve_failed 等）ときも印は古びて pid は残るので、
		// ダイアログで見送っている場合と区別できるよう、新しい殻の診断があれば載せる。
		if shell := spool.ReadShell(bridge.Dir, e.now()); shell != nil {
			out["shell"] = shell
			_ = emit(e, out)
			fmt.Fprintln(e.stderr, "vw2026: Vectorworks is running but its payload is not running (see \"shell\")")
			return exitUnresponsive
		}
		_ = emit(e, out)
		fmt.Fprintln(e.stderr, "vw2026: Vectorworks is running but the bridge is not responding (a dialog may be open)")
		return exitUnresponsive
	}
	return emit(e, out)
}

// bridgeJSON は見つけたブリッジの出力（status / wait / launch で同じ形）。
func bridgeJSON(bridge *spool.Bridge) map[string]any {
	return map[string]any{
		"live":   bridge.State == spool.StateLive,
		"state":  bridge.State,
		"spool":  bridge.Dir,
		"status": bridge.Status.Raw,
	}
}

// --- call / tools -----------------------------------------------------------

func cmdCall(args []string, e env) int {
	fs, c := newFlags("call", e, 30)
	raw := fs.Bool("raw", false, "print the whole response (id/ok/result/error)")
	positional, err := parseInterspersed(fs, args)
	if err != nil || !c.valid(e) {
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

	bridge, _, err := spool.Find(c.candidates(), e.now())
	if err != nil {
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
	var protocol *spool.ProtocolError
	switch {
	case errors.As(err, &protocol):
		return exitProtocol
	case errors.Is(err, spool.ErrNotRunning):
		return exitDown
	case errors.Is(err, spool.ErrUnresponsive):
		return exitUnresponsive
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
	if !c.valid(e) {
		return exitUsage
	}
	deadline := e.now().Add(seconds(c.timeout))
	for {
		// 動き出すのは StateLive になったとき、止まるのは StateDown になったとき（pid の
		// プロセスが無くなったとき）。StateUnresponsive はどちらでもない（保存の確認の
		// ダイアログを開いている間に「止まった」と誤らないように）。
		bridge, _, err := spool.Find(c.candidates(), e.now())
		if *down && err != nil {
			return emit(e, map[string]any{"live": false, "state": spool.StateDown})
		}
		if !*down && err == nil && bridge.State == spool.StateLive {
			return emit(e, bridgeJSON(bridge))
		}
		if e.now().After(deadline) {
			if err == nil && bridge.State == spool.StateUnresponsive {
				fmt.Fprintln(e.stderr, "vw2026: timed out waiting (Vectorworks is running but the bridge is not responding)")
			} else {
				fmt.Fprintln(e.stderr, "vw2026: timed out waiting")
			}
			return exitTimeout
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// --- launch -----------------------------------------------------------------

func cmdLaunch(args []string, e env) int {
	fs, c := newFlags("launch", e, 0)
	app := fs.String("app", e.getenv("VW2026_APP"), "application or executable to start")
	if _, err := parseInterspersed(fs, args); err != nil {
		return exitUsage
	}
	if !c.valid(e) {
		return exitUsage
	}
	if bridge, _, err := spool.Find(c.candidates(), e.now()); err == nil {
		out := bridgeJSON(bridge)
		out["launched"] = false
		if bridge.State == spool.StateUnresponsive {
			// 動いている Vectorworks を起動し直す理由は無い（ダイアログを閉じれば応える）。
			_ = emit(e, out)
			fmt.Fprintln(e.stderr, "vw2026: Vectorworks is running but the bridge is not responding (a dialog may be open)")
			return exitUnresponsive
		}
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
	if c.timeout <= 0 {
		return emit(e, map[string]any{"launched": true, "command": argv})
	}
	// 起動を見届ける（--timeout 秒まで）。
	return cmdWait([]string{"--timeout", fmt.Sprint(c.timeout), "--channel", c.channel, "--spool", c.spool}, e)
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
