package main

import (
	"fmt"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/edition"
	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/spool"
)

type statusCmd struct{}

func (statusCmd) Help() string {
	return edition.Expand(`
Status reports whether the bridge is running. The answer depends only on
whether the plug-in holds the lock file (bridge.lock) in the spool; see
docs/protocol.md. Status does not tell a busy Vectorworks from a responsive
one. Use "${cmd} call ping" for the version of the plug-in.

It prints one of

	{"running":true,"spool":"..."}
	{"running":false,"spool":"...","reason":"..."}

and exits with 5 when the bridge is not running, writing the spool it looked at
to the standard error too (a ${cmd} run from outside the folder of the plug-in
looks at another spool; see "${cmd} --help").

The reason is "not found" when there is no spool, "not running" when nobody
holds the lock, and starts with "cannot check the lock:" when the lock cannot
be checked (it cannot be opened, or, on macOS and Linux, it is not a regular
file); status reports the bridge as not running then too.

${cmd} does not wait for the bridge to start or stop; run status in a loop
and decide on its exit status, not on its output. Stop on 1 too (the spool
cannot be located), or the loop will not end. In sh, for up to 120 seconds:

	# until the bridge starts (code: 0 started, 5 not yet, 1 failed)
	for i in $(seq 120); do
	    ${cmd} status >/dev/null 2>&1; code=$?
	    [ "$code" -ne 5 ] && break
	    sleep 1
	done

	# until the bridge stops (code: 5 stopped, 0 not yet, 1 failed)
	for i in $(seq 120); do
	    ${cmd} status >/dev/null 2>&1; code=$?
	    [ "$code" -ne 0 ] && break
	    sleep 1
	done

In PowerShell, the same with $LASTEXITCODE:

	# until the bridge starts
	for ($i = 0; $i -lt 120; $i++) {
	    ${cmd} status *> $null
	    if ($LASTEXITCODE -ne 5) { break }
	    Start-Sleep 1
	}

	# until the bridge stops
	for ($i = 0; $i -lt 120; $i++) {
	    ${cmd} status *> $null
	    if ($LASTEXITCODE -ne 0) { break }
	    Start-Sleep 1
	}

Vectorworks keeps the lock while it shows the dialog that asks to save the
drawings after "call quit", so status does not take that dialog for a stop.
If the user cancels the dialog, the loop that waits for the stop runs out.
`)
}

func (statusCmd) run(e *env) int {
	bridge := open(e)
	if bridge == nil {
		return exitFailure
	}
	code := emit(e, bridgeJSON(bridge))
	if !bridge.Running {
		fmt.Fprintf(e.stderr, edition.Command+": the bridge is not running (is Vectorworks started with the plug-in? spool: %s)\n", bridge.Dir)
		return exitDown
	}
	return code
}

// bridgeJSON はブリッジの状態の出力（status / launch で同一の形式）。
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
