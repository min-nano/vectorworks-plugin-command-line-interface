package main

import (
	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/edition"
	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/spool"
)

type toolsCmd struct {
	Timeout float64 `env:"${prefix}TOOLS_TIMEOUT" default:"30" placeholder:"SECONDS" help:"How long to wait for the response (default ${default})."`
}

func (toolsCmd) Help() string {
	return edition.Expand(`
Tools prints the tools that "${cmd} call" can call, as the plug-in lists them;
docs/plugin/tools.md describes them.

	{"protocol":7,"tools":[{"name":"ping","description":"...","inputSchema":{...},"kind":"read"},...]}

Each tool has its name, a description, the JSON Schema of its arguments
(inputSchema), and its kind: read (only reads), write (changes the drawing),
or app (works on Vectorworks, its files, or its windows, but does not change
the drawing). A tool of kind write or app runs only in a session (see
"${cmd} help session").

Tools runs for every call, in a session or not, and even while Vectorworks is
quitting. It exits as call does ("${cmd} help call"): 5 when the bridge is not
running, 3 when it did not answer in time, and 23 when the plug-in is busy.
`)
}

func (c *toolsCmd) run(e *env) int {
	return callReserved(e, spool.ToolTools, "", c.Timeout)
}
