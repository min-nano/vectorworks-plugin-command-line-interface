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
	Raw     bool    `env:"VW2026_CALL_RAW" help:"Print the whole response (ok, result, and error)."`
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
and 23 for busy and no_session, as described below, and prints nothing to the
standard output even with --raw. A failure with a missing or unknown code is
a malformed response, so call exits with 1 for it. So does a response file
that stays broken (not JSON) for a second; call removes it without waiting for
--timeout.

When --timeout runs out, call stops waiting, withdraws the request, and
exits with 3: the tool did not run. If the plug-in had already taken the
request, call waits a few more seconds for the response, and exits with 4 if
none comes: the tool may have run. A no_wait response also means that the
tool did not run, so call exits with 3 for it, not with 21 or 22. A call that
is killed while waiting leaves a request that will not run either.

While another session occupies the bridge (see "vw2026 help session"), call
exits with 23: the tool did not run. So it does when the session it calls in
has ended, and for quit outside a session.
`
}

func (c *callCmd) run(g *globals, e *env) int {
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

	bridge := g.open(e)
	if bridge == nil {
		return exitFailure
	}
	if !bridge.Running {
		fmt.Fprintln(e.stderr, "vw2026: the bridge is not running (try `vw2026 status`)")
		return exitDown
	}
	response, err := bridge.Call(c.Tool, payload, c.Session, seconds(c.Timeout))
	if err != nil {
		fmt.Fprintf(e.stderr, "vw2026: %v\n", err)
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
	case errors.Is(err, spool.ErrBusy), errors.Is(err, spool.ErrNoSession):
		return exitBusy
	default:
		return exitFailure
	}
}
