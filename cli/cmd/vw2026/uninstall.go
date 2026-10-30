package main

import (
	"errors"
	"fmt"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/install"
)

type uninstallCmd struct {
	Name       string `arg:"" help:"Plug-in to uninstall, such as cli."`
	PluginsDir string `env:"VW2026_PLUGINS_DIR" placeholder:"DIR" help:"Folder of plug-ins (default: the Plug-Ins folder of Vectorworks 2026 for this user)."`
}

func (uninstallCmd) Help() string {
	return `
Uninstall removes a plug-in that vw2026 installed
(docs/plugin/install-and-update.md). It reads only the record in
<Plug-Ins>/<NAME>/, not the registries, so it works offline and for a plug-in
that left the registry. It removes a folder only when the record names the
folder and the module of the record is there; otherwise it removes nothing.

	vw2026 uninstall my-plugin
	vw2026 uninstall cli                          # removes vw2026 itself too
	vw2026 uninstall cli --plugins-dir <Plug-Ins>

Uninstall first moves the folder out of <Plug-Ins>, then runs the uninstall
hook of the installed version (for cli, it takes vw2026 off the PATH), then
deletes the folder. If the hook fails, the folder goes back. If uninstall
stops after moving the folder (Ctrl-C, power loss), run it again: it goes on
from the hook. After "uninstall cli" stopped like that, vw2026 is gone from
the PATH: give "uninstall cli" to the first-time script instead.

Like install, uninstall changes nothing while Vectorworks is running with the
bridge (exit status 6), and a plug-in other than cli is uninstalled only from a
folder of plug-ins that has cli. "uninstall cli" does not refuse while other
plug-ins remain, but without vw2026 they can no longer be uninstalled; see
"remaining".

It prints one of

	{"outcome":"uninstalled","name":"cli","hook":{...},"remaining":[...],"left":[...]}
	{"outcome":"not_installed","name":"...","remaining":[...],"left":[]}
	{"outcome":"vectorworks_running",...}
	{"outcome":"failed","message":"..."}

"remaining" lists the plug-ins that vw2026 installed and that remain in
<Plug-Ins>. "left" lists the moved-away copies of vw2026 that could not be
deleted: on Windows, "uninstall cli" run by the vw2026 in the folder cannot
delete itself, and leaves it in the temporary folder of the user, where
Windows cleans it up (the next install deletes it too).

Exit status: 0 success (including not_installed), 1 failure, 2 wrong usage, 6
Vectorworks is running.
`
}

func (c *uninstallCmd) run(e *env) int {
	options, code := deployOptions(e, c.PluginsDir)
	if code != exitOK {
		return code
	}
	result, err := install.Uninstall(options, c.Name)
	if err != nil {
		if errors.Is(err, install.ErrRunning) {
			fmt.Fprintf(e.stderr, "vw2026: %v\n", err)
			emit(e, result)
			return exitRunning
		}
		return deployFailed(e, c.Name, err)
	}
	return emit(e, result)
}
