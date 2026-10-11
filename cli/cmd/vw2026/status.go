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

and exits with 3 when the bridge is not running.
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

// bridgeJSON はブリッジの状態の出力（status / wait / launch で同じ形）。
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
