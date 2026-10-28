package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/spool"
)

type callCmd struct {
	Tool    string  `arg:"" help:"Tool to call (\"vw2026 call tools\" lists them)."`
	Args    string  `arg:"" optional:"" name:"args" help:"Arguments as a JSON object, or - to read them from the standard input."`
	Raw     bool    `env:"VW2026_CALL_RAW" negatable:"" help:"Print the whole response (ok, result, and error)."`
	Timeout float64 `env:"VW2026_CALL_TIMEOUT" default:"30" placeholder:"SECONDS" help:"How long to wait for the response (default ${default})."`
	Session string  `env:"VW2026_SESSION" placeholder:"ID" help:"Session to call in (\"vw2026 session start\" prints it)."`
}

func (callCmd) Help() string {
	return `
Call calls one tool of the bridge and prints its result. "vw2026 call tools"
lists the tools; docs/plugin/tools.md describes them.

The arguments are a JSON object. "-" reads them from the standard input.
Without them, the tool is called with no arguments. Call does not check the
arguments against the tool; the plug-in does. Call only refuses a request
that the plug-in would not read (over 1 MiB, or nested deeper than 64).

	vw2026 call layers '{"include_sheets":false}'
	echo '{"layer":"1F"}' | vw2026 call layer_objects -

On success, call prints the result of the tool. With --raw it prints the
whole response. When the tool fails, call writes its code and reason to the
standard error and exits with 22 for wrong usage (invalid_request,
unknown_tool, or invalid_args) or 21 for any other failure (internal); with
--raw the response also carries the code (docs/protocol.md). The codes that
mean the tool did not run are not tool failures: call exits with 3 for no_wait
and 23 for occupied, no_session, and busy, as described below, and prints nothing to the
standard output even with --raw. A failure with a missing or unknown code is
a malformed response, so call exits with 1 for it. So does a response file
that is not a valid response object (not JSON, or JSON of the wrong shape);
call removes it without waiting for --timeout.

When the bridge is not running, call exits with 5 and writes the spool it
looked at to the standard error, so that a vw2026 run from outside the folder
of the plug-in (see "vw2026 --help") can be told from a stopped Vectorworks.

When --timeout runs out, call stops waiting, withdraws the request, and
exits with 3: the tool did not run. If the plug-in had already taken the
request, call waits a few more seconds for the response, and exits with 4 if
none comes: the tool may have run. A no_wait response also means that the
tool did not run, so call exits with 3 for it, not with 21 or 22. A call that
is killed while waiting leaves a request that will not run either.

A tool that changes the drawing or Vectorworks (kind write or app, such as
quit; see "call tools") runs only in a session (see "vw2026 help session").
Called outside its session, it exits with 23: the tool did not run. So it does
when another session occupies the bridge, when the session it calls in has
ended, and when there is no session at all. A tool that only reads (kind read,
such as ping) runs for every call.

While Vectorworks is quitting (after "call quit", while it asks whether to save
the drawings), the plug-in refuses a tool of kind write or app with busy, and
call exits with 23: the tool did not run. In the rare case that a tool lets
Vectorworks run its events, the plug-in refuses every other call with busy until
the tool returns. The reason is on the standard error. Call again later; call
does not retry by itself.
`
}

func (c *callCmd) run(e *env) int {
	var payload json.RawMessage
	if c.Args != "" {
		text := c.Args
		if text == "-" {
			data, err := io.ReadAll(e.stdin)
			if err != nil {
				fmt.Fprintf(e.stderr, "vw2026: read stdin: %v\n", err)
				return exitFailure
			}
			text = string(data)
		}
		// 中身は道具ごとに解釈しない（プラグイン側の仕事）。オブジェクトであることだけを確かめる。
		var probe map[string]any
		if json.Unmarshal([]byte(text), &probe) != nil || probe == nil {
			fmt.Fprint(e.stderr, "vw2026: args must be a JSON object\n")
			return exitUsage
		}
		payload = json.RawMessage(strings.TrimSpace(text))
	}

	bridge := open(e)
	if bridge == nil {
		return exitFailure
	}
	// 止まっていると答えるときは、求めたスプールの場所を示す。フォルダの外で動かした vw2026 は
	// Vectorworks が動いていても止まっていると答えるので、場所の取り違えと見分けられるように
	// （docs/protocol.md「スプールの場所」）。
	if !bridge.Running {
		fmt.Fprintf(e.stderr, "vw2026: the bridge is not running (spool: %s; try `vw2026 status`)\n", bridge.Dir)
		return exitDown
	}
	response, err := bridge.Call(c.Tool, payload, c.Session, seconds(c.Timeout))
	if err != nil {
		if errors.Is(err, spool.ErrNotRunning) {
			fmt.Fprintf(e.stderr, "vw2026: %v (spool: %s)\n", err, bridge.Dir)
		} else {
			fmt.Fprintf(e.stderr, "vw2026: %v\n", err)
		}
		return codeFor(err)
	}
	if c.Raw {
		_ = emit(e, response)
	} else if response.OK {
		_ = emitRaw(e, response.Result)
	}
	if !response.OK {
		fmt.Fprintf(e.stderr, "vw2026: %s: %s: %s\n", c.Tool, response.Code, response.Error)
		return toolCodeFor(response.Code)
	}
	return exitOK
}

// toolCodeFor は道具の失敗の code を終了コードにする。
func toolCodeFor(code string) int {
	switch code {
	case spool.CodeInvalidRequest, spool.CodeUnknownTool, spool.CodeInvalidArgs:
		return exitToolUsage
	case spool.CodeInternal:
		return exitToolFailure
	default:
		// spool が新しい code を通すようになってここを直し忘れたら、壊れた応答と同じに扱う。
		return exitFailure
	}
}

func codeFor(err error) int {
	switch {
	case errors.Is(err, spool.ErrNotRunning):
		return exitDown
	case errors.Is(err, spool.ErrTimeout), errors.Is(err, spool.ErrNoWait):
		return exitTimeout
	case errors.Is(err, spool.ErrNoResponse):
		return exitNoResponse
	case errors.Is(err, spool.ErrBusy), errors.Is(err, spool.ErrOccupied), errors.Is(err, spool.ErrNoSession):
		return exitBusy
	default:
		return exitFailure
	}
}
