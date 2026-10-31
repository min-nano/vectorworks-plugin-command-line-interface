package main

import (
	"errors"
	"fmt"
	"runtime"
	"strings"

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
	vw2026 uninstall my-plugin --plugins-dir <Plug-Ins>

Uninstall does not remove the folder that this vw2026 runs from, such as cli
for the vw2026 on the PATH (<Plug-Ins>/cli/bin/vw2026): once cli is
uninstalled, vw2026 is not used any more. It changes nothing, exits with 2,
and shows the first-time script of the installed tag to run instead, which
uninstalls cli with a vw2026 of that tag outside the folder (and leaves nothing
behind). On macOS and on Windows:

	curl -fsSL ` + releases + `download/<TAG>/get-vw2026.sh | sh -s -- uninstall cli@<TAG>
	& ([scriptblock]::Create((irm ` + releases + `download/<TAG>/get-vw2026.ps1))) uninstall cli@<TAG>

A prerelease is deleted when its pull request closes; then use the script of
latest, which uninstalls with the vw2026 of latest:

	curl -fsSL ` + releases + `latest/download/get-vw2026.sh | sh -s -- uninstall cli

Uninstall first moves the folder out of <Plug-Ins>, then runs the uninstall
hook of the installed version (for cli, it takes vw2026 off the PATH), then
deletes the folder. If the hook fails, the folder goes back. If uninstall
stops after moving the folder (Ctrl-C, power loss), run it again: it goes on
from the hook.

Like install, uninstall changes nothing while Vectorworks is running with the
bridge (exit status 6), and a plug-in other than cli is uninstalled only from a
folder of plug-ins that has cli. Uninstalling cli does not refuse while other
plug-ins remain, but without vw2026 they can no longer be uninstalled; see
"remaining".

It prints one line of JSON with one of these outcomes:

	{"outcome":"uninstalled","name":"my-plugin","hook":{...},
	 "remaining":[...],"left":[...]}
	{"outcome":"not_installed","name":"...","remaining":[...],"left":[]}
	{"outcome":"vectorworks_running",...}
	{"outcome":"failed","message":"...","command":"..."}

"remaining" lists the plug-ins that vw2026 installed and that remain in
<Plug-Ins>. "left" lists the moved-away copies of vw2026 (left by an update on
Windows) that could not be deleted; the temporary folder of the user holds
them, and Windows cleans it up. "command" is the first-time script (of the
installed tag) to run when this vw2026 runs from the folder.

Exit status: 0 success (including not_installed), 1 failure, 2 wrong usage
(including a vw2026 that runs from the folder), 6 Vectorworks is running.
`
}

func (c *uninstallCmd) run(e *env) int {
	options, code := deployOptions(e, c.PluginsDir)
	if code != exitOK {
		return code
	}
	result, err := install.Uninstall(options, c.Name)
	var inside *install.RunsInsideError
	switch {
	case errors.Is(err, install.ErrRunning):
		fmt.Fprintf(e.stderr, "vw2026: %v\n", err)
		emit(e, result)
		return exitRunning
	case errors.As(err, &inside):
		// 入れた cli の vw2026 では外さず、入っているタグの初回のスクリプトと CLI で外すよう案内する。
		// PR のプレリリースは PR を閉じると消えるので、latest で外すコマンドも示す。
		command := uninstallScript(runtime.GOOS, c.Name, inside.Tag, c.PluginsDir)
		fmt.Fprintf(e.stderr, "vw2026: %v. vw2026 is not used once %s is uninstalled; run the first-time script "+
			"of the installed tag instead:\n\t%s\nIf that release no longer exists (a prerelease is deleted when "+
			"its pull request closes), use latest:\n\t%s\n",
			err, c.Name, command, uninstallScript(runtime.GOOS, c.Name, "latest", c.PluginsDir))
		emit(e, map[string]any{"outcome": "failed", "name": c.Name, "message": err.Error(), "command": command})
		return exitUsage
	case err != nil:
		return deployFailed(e, c.Name, err)
	}
	return emit(e, result)
}

// releases は、初回のスクリプトを置くリリースの場所。
const releases = "https://github.com/min-nano/vectorworks-plugin-command-line-interface/releases/"

// uninstallScript は、goos で tag の初回のスクリプトに uninstall <name>@<tag> を渡すコマンド（スクリプトは
// そのタグの CLI で外す）。tag が latest なら latest のスクリプトと CLI で外す（uninstall <name>）。
// --plugins-dir を渡されていれば、そのまま渡す（シェルの引用符で囲む）。
func uninstallScript(goos, name, tag, pluginsDir string) string {
	base, target := releases+"download/"+tag+"/", name+"@"+tag
	if tag == "latest" {
		base, target = releases+"latest/download/", name
	}
	if goos == "windows" {
		command := "& ([scriptblock]::Create((irm " + base + "get-vw2026.ps1))) uninstall " + target
		if pluginsDir != "" {
			command += " --plugins-dir '" + strings.ReplaceAll(pluginsDir, "'", "''") + "'"
		}
		return command
	}
	command := "curl -fsSL " + base + "get-vw2026.sh | sh -s -- uninstall " + target
	if pluginsDir != "" {
		command += " --plugins-dir '" + strings.ReplaceAll(pluginsDir, "'", `'\''`) + "'"
	}
	return command
}
