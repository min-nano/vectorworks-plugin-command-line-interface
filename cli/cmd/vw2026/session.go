package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/spool"
)

// sessionEnv は占有の印を子へ渡す環境変数。call が読む。
const sessionEnv = "VW2026_SESSION"

type sessionCmd struct {
	Command []string `arg:"" passthrough:"" placeholder:"COMMAND" help:"Command to run while occupying the bridge, after \"--\"."`
}

func (sessionCmd) Help() string {
	return `
Session occupies the bridge, runs the command, and ends the occupation when
the command exits. While the bridge is occupied, the plug-in runs only the
calls of this session: "vw2026 call" in the command (and in what it starts)
finds the session in VW2026_SESSION. Other calls exit with 9 and do not run;
"call tools" and "call ping" are answered for everyone. Without a session,
nobody occupies the bridge and every call runs, as before.

	vw2026 session -- claude
	vw2026 session -- sh -c 'vw2026 call layers && vw2026 call classes'

The occupation lives only as long as this process. If it dies, the operating
system releases it at once, so a crashed caller never blocks the bridge; the
calls the command still makes then exit with 9 and do not run. Session can
start before Vectorworks and lasts across "call quit" and "launch".

Session exits with the exit code of the command, with 9 when another session
occupies the bridge, and with 2 when it is already inside a session.
`
}

func (c *sessionCmd) run(g *globals, e *env) int {
	if os.Getenv(sessionEnv) != "" {
		// 中で占有し直すと、外の占有に断られて動かない。
		fmt.Fprintln(e.stderr, "vw2026: already in a session")
		return exitUsage
	}
	// kong は passthrough の引数に "--" を残す。
	command := c.Command
	if len(command) > 0 && command[0] == "--" {
		command = command[1:]
	}
	if len(command) == 0 {
		fmt.Fprintln(e.stderr, "vw2026: session needs a command (run \"vw2026 help session\")")
		return exitUsage
	}
	dir := g.dir(e)
	if dir == "" {
		return exitFailure
	}
	session, err := spool.HoldSession(dir)
	if err != nil {
		fmt.Fprintf(e.stderr, "vw2026: %v\n", err)
		if errors.Is(err, spool.ErrBusy) {
			return exitBusy
		}
		return exitFailure
	}
	defer session.Release()

	// Ctrl-C は子にも届く。こちらは子が終わるのを待ち、占有を終えてから終わる。
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)

	cmd := exec.Command(command[0], command[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = e.stdin, e.stdout, e.stderr
	cmd.Env = append(os.Environ(), sessionEnv+"="+session.ID)
	err = cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return exitOK
	case errors.As(err, &exit) && exit.ExitCode() >= 0:
		return exit.ExitCode()
	default:
		fmt.Fprintf(e.stderr, "vw2026: %v\n", err)
		return exitFailure
	}
}
