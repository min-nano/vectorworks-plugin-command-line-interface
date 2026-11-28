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

	{"protocol":...,"tools":[{"name":"ping","description":"...","inputSchema":{...},"kind":"read"},...]}

Each tool has its name, a description, the JSON Schema of its arguments
(inputSchema), and its kind: read (only reads), write (changes the drawing),
or app (works on Vectorworks, its files, or its windows, but does not change
the drawing). A tool of kind write or app runs only in a session (see
"${cmd} help session").

Tools runs for every call, in a session or not, and even while Vectorworks is
quitting. On Windows, while a tool shows a dialog, the plug-in refuses it with
busy like every other call, and tools exits with 23. It prints and exits as call
does, with the same exit codes ("${cmd} help call").
`)
}

func (c *toolsCmd) run(e *env) int {
	return callReserved(e, spool.ToolTools, "", c.Timeout)
}
