package main

import (
	"fmt"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/edition"
	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/spool"
)

type sessionCmd struct {
	Start sessionStartCmd `cmd:"" help:"Occupy the bridge and print the session."`
	End   sessionEndCmd   `cmd:"" help:"End the session."`
}

func (sessionCmd) Help() string {
	return edition.Expand(`
A session occupies the bridge: while it lasts, the plug-in runs a tool that
changes the drawing or Vectorworks (kind write or app, such as quit; see
"${cmd} tools") only for the calls that carry it (--session, or ${prefix}SESSION). Other
calls of such a tool exit with 23 and do not run. Without a session, nobody
occupies the bridge, and such a tool does not run at all (23). A tool that only
reads (kind read, such as ping) and "${cmd} tools" run for every call, in a
session or not (but while Vectorworks is quitting, every tool exits with 23
except "${cmd} tools", "session start" and "session end", and a tool the plug-in
does not have exits with 22; see "${cmd} help call").

	${cmd} session start                     # {"session":"<ID>"}
	${cmd} call <TOOL> --session <ID>        # a tool of kind write or app
	${cmd} session end --session <ID>

The plug-in issues one session at a time and keeps it in memory only. Nothing
ends it but "session end" and the end of Vectorworks: a session that is never
ended keeps the bridge occupied until Vectorworks restarts. Quitting
Vectorworks ends the session, so after "call quit" and "launch", start a new
one. With the old one, a tool of kind write or app and "session end" exit with
23; a tool that only reads does not look at the session and runs.
`)
}

// sessionTimeout は session start と session end が共有する待つ上限（環境変数も同じ VW2026_SESSION_TIMEOUT）。
type sessionTimeout struct {
	Timeout float64 `env:"${prefix}SESSION_TIMEOUT" default:"30" placeholder:"SECONDS" help:"How long to wait for the response (default ${default})."`
}

type sessionStartCmd struct {
	sessionTimeout `embed:""`
}

func (sessionStartCmd) Help() string {
	return edition.Expand(`
Start asks the plug-in for a session and prints it as {"session":"<ID>"}. It
exits with 23 when another session occupies the bridge. If start exits with 4,
the plug-in may have issued a session that nobody knows; it lasts until
Vectorworks restarts.
`)
}

func (c *sessionStartCmd) run(e *env) int {
	return callReserved(e, spool.ToolSessionStart, "", c.Timeout)
}

type sessionEndCmd struct {
	Session string `env:"${prefix}SESSION" placeholder:"ID" help:"Session to end (required)."`

	sessionTimeout `embed:""`
}

func (sessionEndCmd) Help() string {
	return edition.Expand(`
End ends the session and prints {"ended":true}. It exits with 23 when the
session is not the one that occupies the bridge, for example after
Vectorworks has restarted.
`)
}

func (c *sessionEndCmd) run(e *env) int {
	// kong の required は、空の環境変数も値とみなすので、ここで確かめる。
	if c.Session == "" {
		fmt.Fprintln(e.stderr, edition.Command+": session end needs --session or "+edition.EnvPrefix+"SESSION")
		return exitUsage
	}
	return callReserved(e, spool.ToolSessionEnd, c.Session, c.Timeout)
}
