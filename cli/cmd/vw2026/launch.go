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
Windows: the plug-in is not installed, or Vectorworks is still starting or
already quitting (run status in a loop to tell these apart). Launch finds a
running Vectorworks with tasklist; if tasklist fails, launch starts the
application as if Vectorworks were not running. On macOS, an application
started with "open -a" (one ending in .app) is brought to the front instead,
so launch prints {"launched":true,...} and exits with 0; run status to tell
whether the bridge is running. An executable given with --app on macOS is
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
			fmt.Fprintln(e.stderr, "vw2026: Vectorworks is running but the bridge is not (is the plug-in installed, or is Vectorworks starting or quitting?)")
			return exitDown
		}
		fmt.Fprintf(e.stderr, "vw2026: launch: %v\n", err)
		return exitFailure
	}
	return emit(e, map[string]any{"launched": true, "command": argv})
}
