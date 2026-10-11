package main

import (
	"fmt"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/spool"
)

type statusCmd struct{}

func (statusCmd) Help() string {
	return `
Status reports whether the bridge is running. The answer depends only on
whether the plug-in holds the lock file (bridge.lock) in the spool; see
docs/protocol.md. Status does not tell a busy Vectorworks from a responsive
one. Use "vw2026 call ping" for the version of the plug-in.

It prints one of

	{"running":true,"spool":"..."}
	{"running":false,"spool":"...","reason":"..."}

and exits with 5 when the bridge is not running.

vw2026 does not wait for the bridge to start or stop; run status in a loop
and decide on its exit status, not on its output. Stop on 1 too (the spool
cannot be located), or the loop will not end. In sh, for up to 120 seconds:

	# until the bridge starts (code: 0 started, 5 not yet, 1 failed)
	for i in $(seq 120); do
	    vw2026 status >/dev/null 2>&1; code=$?
	    [ "$code" -ne 5 ] && break
	    sleep 1
	done

	# until the bridge stops (code: 5 stopped, 0 not yet, 1 failed)
	for i in $(seq 120); do
	    vw2026 status >/dev/null 2>&1; code=$?
	    [ "$code" -ne 0 ] && break
	    sleep 1
	done

In PowerShell, the same with $LASTEXITCODE:

	for ($i = 0; $i -lt 120; $i++) {
	    vw2026 status *> $null
	    if ($LASTEXITCODE -ne 5) { break }   # -ne 0 to wait until it stops
	    Start-Sleep 1
	}

Vectorworks keeps the lock while it shows the dialog that asks to save the
drawings after "call quit", so status does not take that dialog for a stop.
If the user cancels the dialog, the loop that waits for the stop runs out.
`
}

func (statusCmd) run(g *globals, e *env) int {
	bridge := g.open(e)
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

// bridgeJSON はブリッジの状態の出力（status / launch で同じ形）。
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
