package main

import (
	"errors"
	"fmt"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/launch"
)

type launchCmd struct {
	App string `env:"VW2026_APP" placeholder:"NAME-OR-PATH" help:"Application or executable to start."`
}

func (launchCmd) Help() string {
	return `
Launch starts Vectorworks unless the bridge is already running. It does not
wait for the bridge; run status in a loop next to wait for it ("vw2026 help
status").

It prints one of

	{"launched":true,"command":[...]}
	{"running":true,"spool":"...","launched":false}

The default application is "Vectorworks 2026" on macOS, started with
"open -a", and %ProgramFiles%\Vectorworks 2026\Vectorworks2026.exe on
Windows. Launch does not search other places; name them with --app. A value
that does not end in .app is started as an executable.

When Vectorworks is running but the bridge is not, launch exits with 5 on
Windows. Either the plug-in is not loaded (for example, it is not installed, or
it was not enabled in the dialog about plug-ins from unidentified developers),
or Vectorworks is still starting or already quitting. Status in a loop tells
only the start apart, by turning to 0. If it stays at 5, either the plug-in is
not loaded, or Vectorworks has quit and launch can be run again. Launch finds a
running Vectorworks with tasklist; if tasklist fails, launch starts the
application as if Vectorworks were not running.

On macOS, the default application and a value ending in .app are started with
"open -a", which brings a running Vectorworks to the front instead, so launch
prints {"launched":true,...} and exits with 0; run status to tell whether the
bridge is running. On any OS but Windows, an executable given with --app is
started without checking whether Vectorworks is running.
`
}

// run does not wait: waiting is the caller's choice
// (docs/design.md「CLI はプリミティブに保つ」).
func (c *launchCmd) run(e *env) int {
	bridge := open(e)
	if bridge == nil {
		return exitFailure
	}
	if bridge.Running {
		out := bridgeJSON(bridge)
		out["launched"] = false
		return emit(e, out)
	}
	argv, err := launch.Command(c.App)
	if err != nil {
		fmt.Fprintf(e.stderr, "vw2026: %v\n", err)
		return exitFailure
	}
	if err := e.start(argv); err != nil {
		if errors.Is(err, launch.ErrAlreadyRunning) {
			fmt.Fprintln(e.stderr, "vw2026: Vectorworks is running but the bridge is not (the plug-in is not loaded, or Vectorworks is starting or quitting; see `vw2026 help launch`)")
			return exitDown
		}
		fmt.Fprintf(e.stderr, "vw2026: launch: %v\n", err)
		return exitFailure
	}
	return emit(e, map[string]any{"launched": true, "command": argv})
}
