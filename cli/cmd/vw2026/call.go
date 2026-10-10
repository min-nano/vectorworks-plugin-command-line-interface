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
whole response. When the tool fails, call writes its code and reason to the
standard error and exits with 1; with --raw the response also carries the
code (unknown_tool, invalid_args, invalid_request, no_wait, or internal;
docs/protocol.md). A failure with a missing or unknown code is a malformed
response, so call exits with 6 for it.

When --timeout runs out, call stops waiting, withdraws the request, and
exits with 4: the tool did not run. If the plug-in had already taken the
request, call waits a few more seconds for the response, and exits with 8 if
none comes: the tool may have run. A no_wait response also means that the
tool did not run, so call exits with 4 for it, not with 1. A call that is
killed while waiting leaves a request that will not run either.
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
	response, err := bridge.Call(c.Tool, payload, seconds(c.Timeout))
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
	case errors.Is(err, spool.ErrNoResponse):
		return exitNoResponse
	default:
		return exitFailure
	}
}
