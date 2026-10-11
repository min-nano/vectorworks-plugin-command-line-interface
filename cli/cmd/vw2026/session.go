package main

import (
	"fmt"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/spool"
)

type sessionCmd struct {
	Start sessionStartCmd `cmd:"" help:"Occupy the bridge and print the session."`
	End   sessionEndCmd   `cmd:"" help:"End the session."`
}

func (sessionCmd) Help() string {
	return `
A session occupies the bridge: while it lasts, the plug-in runs only the calls
that carry it (--session, or VW2026_SESSION). Other calls exit with 9 and do
not run; "call tools" and "call ping" are answered for everyone. Without a
session, nobody occupies the bridge and every call runs, except quit, which
runs only in a session.

	vw2026 session start                     # {"session":"<ID>"}
	vw2026 call layers --session <ID>
	vw2026 session end --session <ID>

The plug-in issues one session at a time and keeps it in memory only. Nothing
ends it but "session end" and the end of Vectorworks: a session that is never
ended keeps the bridge occupied until Vectorworks restarts. Quitting
Vectorworks ends the session, so after "call quit" and "launch", start a new
one; calls with the old one exit with 9.
`
}

type sessionStartCmd struct {
	Timeout float64 `default:"30" placeholder:"SECONDS" help:"How long to wait for the response (default ${default})."`
}

func (sessionStartCmd) Help() string {
	return `
Start asks the plug-in for a session and prints it as {"session":"<ID>"}. It
exits with 9 when another session occupies the bridge. If start exits with 8,
the plug-in may have issued a session that nobody knows; it lasts until
Vectorworks restarts.
`
}

func (c *sessionStartCmd) run(g *globals, e *env) int {
	return callReserved(g, e, spool.ToolSessionStart, "", c.Timeout)
}

type sessionEndCmd struct {
	Session string  `env:"VW2026_SESSION" placeholder:"ID" help:"Session to end (required)."`
	Timeout float64 `default:"30" placeholder:"SECONDS" help:"How long to wait for the response (default ${default})."`
}

func (sessionEndCmd) Help() string {
	return `
End ends the session and prints {"ended":true}. It exits with 9 when the
session is not the one that occupies the bridge, for example after
Vectorworks has restarted.
`
}

func (c *sessionEndCmd) run(g *globals, e *env) int {
	// kong の required は、空の環境変数も値とみなすので、ここで確かめる。
	if c.Session == "" {
		fmt.Fprintln(e.stderr, "vw2026: session end needs --session or VW2026_SESSION")
		return exitUsage
	}
	return callReserved(g, e, spool.ToolSessionEnd, c.Session, c.Timeout)
}

// callReserved は予約された道具を引数なしで呼び、結果を出す。出力・終了コードは call と同じ。
func callReserved(g *globals, e *env, tool, session string, timeout float64) int {
	c := callCmd{Tool: tool, Session: session, Timeout: timeout}
	return c.run(g, e)
}
