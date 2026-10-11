package main

import (
	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/spool"
)

type versionCmd struct{}

func (versionCmd) Help() string {
	return `
Version prints the version of this CLI and the version of the protocol it
speaks (docs/protocol.md).

	{"version":"abc1234","protocol":5}
`
}

func (versionCmd) run(g *globals, e *env) int {
	return emit(e, map[string]any{"version": version, "protocol": spool.ProtocolVersion})
}
